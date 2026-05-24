package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	backupSentinel = "BK_BACKUP.json"
	latestFile     = "latest.json"
	versionsDir    = "versions"
)

// backupMeta is the content of BK_BACKUP.json: a stable opaque id, used as a
// sentinel for auto-discovery of backups on mounted volumes.
type backupMeta struct {
	ID string `json:"id"`
}

// latestMeta is the content of latest.json: a pointer to the current version
// plus the refs fingerprint it captured, used to skip a sync when nothing has
// changed.
type latestMeta struct {
	Path     string    `json:"path"`
	RefsHash string    `json:"refs_hash"`
	SyncedAt time.Time `json:"synced_at"`
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
	return atomicWriteFile(filepath.Join(backupDir, latestFile), append(data, '\n'), 0644)
}

// syncBackup creates a new full bundle of repoPath and appends it to the backup
// at backupDir. It never overwrites existing versions: a uniquely-named bundle
// and its sha256 sidecar are written, then latest.json is atomically updated to
// point at the new bundle. The backup is initialized on first sync. If the
// repo's refs are unchanged since the last sync, it does nothing.
// It reports whether a new version was written (false means already up to date).
func syncBackup(repoPath, backupDir string) (bool, error) {
	backupDir, err := filepath.Abs(backupDir)
	if err != nil {
		return false, err
	}
	if err := initBackup(backupDir); err != nil {
		return false, err
	}

	refsHash, err := repoRefsHash(repoPath)
	if err != nil {
		return false, err
	}
	if prev, err := readLatest(backupDir); err == nil && prev.RefsHash == refsHash {
		return false, nil // already up to date
	}

	// Unique, sortable name: UTC timestamp + random suffix to avoid collisions
	// between syncs landing in the same second (no existing state is read).
	ts := time.Now().UTC().Format("20060102T150405Z")
	suffix, err := randHex(3)
	if err != nil {
		return false, err
	}
	base := fmt.Sprintf("bk-%s-%s.bundle", ts, suffix)
	bundlePath := filepath.Join(backupDir, versionsDir, base)
	tmpBundle := bundlePath + ".tmp"

	// Write + verify the bundle under a temp name first so a partial or corrupt
	// bundle never appears under its final name (or gets referenced below).
	if err := createBundle(repoPath, tmpBundle); err != nil {
		_ = os.Remove(tmpBundle)
		return false, err
	}
	sum, err := sha256File(tmpBundle)
	if err != nil {
		_ = os.Remove(tmpBundle)
		return false, err
	}
	if err := os.Rename(tmpBundle, bundlePath); err != nil {
		_ = os.Remove(tmpBundle)
		return false, err
	}

	// Sidecar before latest.json; latest.json is updated last so it only ever
	// points at a fully-written, verified bundle with a complete sidecar.
	sidecar := fmt.Sprintf("%s  %s\n", sum, base)
	if err := atomicWriteFile(bundlePath+".sha256", []byte(sidecar), 0644); err != nil {
		return false, err
	}

	rel := filepath.ToSlash(filepath.Join(versionsDir, base))
	if err := writeLatest(backupDir, latestMeta{
		Path:     rel,
		RefsHash: refsHash,
		SyncedAt: time.Now().UTC(),
	}); err != nil {
		return false, err
	}

	return true, nil
}

// restoreBackup restores the latest version of the backup at backupDir into
// restorePath, after checking the sha256 sidecar and verifying the bundle.
func restoreBackup(backupDir, restorePath string) error {
	backupDir, err := filepath.Abs(backupDir)
	if err != nil {
		return err
	}
	if _, err := loadBackupMeta(backupDir); err != nil {
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

	want, err := readSidecarSum(bundlePath + ".sha256")
	if err != nil {
		return err
	}
	got, err := sha256File(bundlePath)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("sha256 mismatch for %s:\n  want %s\n  got  %s", rel, want, got)
	}

	if err := restoreBundle(bundlePath, restorePath); err != nil {
		return err
	}

	fmt.Printf("restored %s -> %s\n", rel, restorePath)
	return nil
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
func initBackup(dir string) error {
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

	id, err := randHex(16)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(backupMeta{ID: id}, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteFile(sentinel, append(data, '\n'), 0644)
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

// readSidecarSum returns the hex digest from a "<hash>  <name>" sha256 sidecar.
func readSidecarSum(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return "", fmt.Errorf("empty sha256 sidecar: %s", path)
	}
	return fields[0], nil
}

// sha256File returns the hex-encoded sha256 of the file at path.
func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// atomicWriteFile writes data to path atomically by writing a temp file in the
// same directory, fsyncing it, and renaming it over path.
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".bk-tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() { _ = os.Remove(tmp) }() // no-op once the rename succeeds

	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp, perm); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// randHex returns n random bytes as a hex string.
func randHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
