package bk

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hoffa/bk/internal/crypt"
	"github.com/hoffa/bk/internal/git"
	"github.com/hoffa/bk/internal/util"
)

const (
	backupSentinel = "BK_BACKUP.json"
	latestFile     = "latest.json"
	versionsDir    = "versions"
	encExt         = ".age" // suffix on an encrypted bundle
)

// backupMeta is the content of BK_BACKUP.json: a stable opaque id (a sentinel
// for auto-discovery of backups on mounted volumes) and the keyring, so the
// backup is self-describing -- a restore needs only the target and the password,
// not the source machine's config.
type backupMeta struct {
	ID  string
	Key crypt.Keyring
}

// latestMeta is the content of latest.json: a pointer to the current version
// plus the refs fingerprint it captured, used to skip a sync when nothing has
// changed.
type latestMeta struct {
	Path        string
	ContentHash string
	SyncedAt    time.Time
}

// readLatest reads latest.json from backupDir.
func readLatest(backupDir string) (*latestMeta, error) {
	data, err := os.ReadFile(filepath.Join(backupDir, latestFile))
	if err != nil {
		return nil, err
	}

	var l latestMeta
	if err := json.Unmarshal(data, &l); err != nil {
		return nil, fmt.Errorf("parse %s: %w", latestFile, err)
	}

	return &l, nil
}

// writeLatest atomically writes latest.json to backupDir.
func writeLatest(backupDir string, l latestMeta) error {
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}

	return util.AtomicWrite(filepath.Join(backupDir, latestFile), append(data, '\n'), 0644)
}

// syncBackup creates a new full bundle of repoPath and appends it to the backup
// at backupDir. It never overwrites existing versions: a uniquely-named bundle
// and its sha256 sidecar are written, then latest.json is atomically updated to
// point at the new bundle. The backup is initialized on first sync. If the
// repo's refs are unchanged since the last sync, it does nothing.
// It reports whether a new version was written (false means already up to date).
func syncBackup(ctx context.Context, repoPath, backupDir string, kr crypt.Keyring) (bool, error) {
	backupDir, err := filepath.Abs(backupDir)
	if err != nil {
		return false, err
	}

	if err := initBackup(backupDir, kr); err != nil {
		return false, err
	}

	refsHash, err := git.RefsHash(ctx, repoPath)
	if err != nil {
		return false, err
	}

	if prev, err := readLatest(backupDir); err == nil && prev.ContentHash == refsHash {
		return false, nil // already up to date
	}

	// Sortable, nanosecond-resolution UTC name. Versions are append-only, so
	// refuse to reuse a name already on disk rather than overwrite it. Syncs to a
	// backup are serialized and the nanosecond stamp makes a real collision
	// practically impossible, so an existing name means something is wrong --
	// error rather than clobber a previous version.
	base := fmt.Sprintf("bk-%s.bundle%s", time.Now().UTC().Format("20060102T150405.000000000Z"), encExt)
	bundlePath := filepath.Join(backupDir, versionsDir, base)
	tmpPlain := bundlePath + ".plain" // plaintext bundle, deleted before return
	tmpEnc := bundlePath + ".tmp"     // encrypted, renamed into place

	if _, err := os.Stat(bundlePath); err == nil {
		return false, fmt.Errorf("version %s already exists; refusing to overwrite", base)
	} else if !os.IsNotExist(err) {
		return false, err
	}

	// Build + verify the plaintext bundle, encrypt it to the recipient, and keep
	// only the encrypted copy. All under temp names so a partial/corrupt file
	// never appears under the final name (or gets referenced below).
	if err := git.SafeCreateBundle(ctx, repoPath, tmpPlain); err != nil {
		_ = os.Remove(tmpPlain)
		return false, err
	}

	if err := crypt.EncryptFile(tmpEnc, tmpPlain, kr.Public); err != nil {
		_ = os.Remove(tmpPlain)
		_ = os.Remove(tmpEnc)

		return false, err
	}

	_ = os.Remove(tmpPlain)

	sum, err := util.SHA256(tmpEnc)
	if err != nil {
		_ = os.Remove(tmpEnc)
		return false, err
	}

	if err := os.Rename(tmpEnc, bundlePath); err != nil {
		_ = os.Remove(tmpEnc)
		return false, err
	}

	// Sidecar before latest.json; latest.json is updated last so it only ever
	// points at a fully-written bundle with a complete sidecar.
	if err := util.WriteSHA256Sum(bundlePath, sum); err != nil {
		return false, err
	}

	rel := filepath.ToSlash(filepath.Join(versionsDir, base))
	if err := writeLatest(backupDir, latestMeta{
		Path:        rel,
		ContentHash: refsHash,
		SyncedAt:    time.Now().UTC(),
	}); err != nil {
		return false, err
	}

	return true, nil
}

// Restore writes the latest version of the backup at backupDir into dst, after
// checking the sha256 sidecar. dst must not already exist. The password decrypts
// the bundle (ignored for legacy unencrypted backups).
func Restore(ctx context.Context, backupDir, dst, password string) error {
	backupDir, err := filepath.Abs(backupDir)
	if err != nil {
		return err
	}

	// Refuse to write into an existing path, so a restore never clobbers data.
	if _, err := os.Stat(dst); err == nil {
		return fmt.Errorf("restore path already exists: %s", dst)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat restore path: %w", err)
	}

	meta, err := loadBackupMeta(backupDir)
	if err != nil {
		return fmt.Errorf("not a backup directory (%s): %w", backupDir, err)
	}

	latest, err := readLatest(backupDir)
	if err != nil {
		return fmt.Errorf("read %s: %w", latestFile, err)
	}

	rel := latest.Path
	if rel == "" {
		return fmt.Errorf("no versions found in backup %s", backupDir)
	}

	bundlePath := filepath.Join(backupDir, filepath.FromSlash(rel))

	want, err := util.ReadSHA256Sum(bundlePath)
	if err != nil {
		return err
	}

	got, err := util.SHA256(bundlePath)
	if err != nil {
		return err
	}

	if got != want {
		return fmt.Errorf("sha256 mismatch for %s:\n  want %s\n  got  %s", rel, want, got)
	}

	// Legacy unencrypted backups clone straight from the stored bundle.
	if !strings.HasSuffix(rel, encExt) {
		return git.Clone(ctx, bundlePath, dst)
	}

	id, err := meta.Key.Identity(password)
	if err != nil {
		return err
	}

	// Decrypt to a temp plaintext bundle, then clone from it.
	f, err := os.CreateTemp("", "bk-restore-*.bundle")
	if err != nil {
		return err
	}

	tmp := f.Name()
	_ = f.Close()

	defer func() { _ = os.Remove(tmp) }()

	if err := crypt.DecryptFile(tmp, bundlePath, id); err != nil {
		return err
	}

	return git.Clone(ctx, tmp, dst)
}

// backupDirUsable reports whether dir is safe to use as a backup target: it
// doesn't exist, is empty, or already contains our sentinel. A non-empty
// directory without the sentinel is someone else's data, never a backup.
func backupDirUsable(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return true, nil
	}

	if err != nil {
		return false, err
	}

	for _, e := range entries {
		if e.Name() == backupSentinel {
			return true, nil
		}
	}

	return len(entries) == 0, nil
}

// initBackup ensures backupDir is an initialized backup: it creates versions/
// and, if BK_BACKUP.json is absent, writes one with a fresh id. An existing
// sentinel is left untouched so the id is stable across syncs. It refuses to
// write into a non-empty directory that isn't already a backup.
func initBackup(dir string, kr crypt.Keyring) error {
	if ok, err := backupDirUsable(dir); err != nil {
		return err
	} else if !ok {
		return fmt.Errorf("%s exists and is not a bk backup (not empty); refusing to write", dir)
	}

	if err := os.MkdirAll(filepath.Join(dir, versionsDir), 0755); err != nil {
		return err
	}

	sentinel := filepath.Join(dir, backupSentinel)
	if _, err := os.Stat(sentinel); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}

	id, err := util.RandHex(16)
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(backupMeta{ID: id, Key: kr}, "", "  ")
	if err != nil {
		return err
	}

	return util.AtomicWrite(sentinel, append(data, '\n'), 0644)
}

// loadBackupMeta reads and parses BK_BACKUP.json from dir.
func loadBackupMeta(dir string) (*backupMeta, error) {
	data, err := os.ReadFile(filepath.Join(dir, backupSentinel))
	if err != nil {
		return nil, err
	}

	var m backupMeta
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse %s: %w", backupSentinel, err)
	}

	return &m, nil
}
