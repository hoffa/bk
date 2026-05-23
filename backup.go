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
	latestFile     = "latest.txt"
	versionsDir    = "versions"
)

// backupMeta is the content of BK_BACKUP.json: a stable opaque id (for
// auto-discovery of backups on mounted volumes) and a human-friendly name.
type backupMeta struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// syncBackup creates a new full bundle of repoPath and appends it to the backup
// at backupDir. It never overwrites existing versions: a uniquely-named bundle
// and its sha256 sidecar are written, then latest.txt is atomically updated to
// point at the new bundle. The backup is initialized on first sync.
func syncBackup(repoPath, backupDir, name string) error {
	backupDir, err := filepath.Abs(backupDir)
	if err != nil {
		return err
	}
	if err := initBackup(backupDir, name); err != nil {
		return err
	}

	// Unique, sortable name: UTC timestamp + random suffix to avoid collisions
	// between syncs landing in the same second (no existing state is read).
	ts := time.Now().UTC().Format("20060102T150405Z")
	suffix, err := randHex(3)
	if err != nil {
		return err
	}
	base := fmt.Sprintf("bk-%s-%s.bundle", ts, suffix)
	bundlePath := filepath.Join(backupDir, versionsDir, base)
	tmpBundle := bundlePath + ".tmp"

	// Write + verify the bundle under a temp name first so a partial or corrupt
	// bundle never appears under its final name (or gets referenced below).
	if err := createBundle(repoPath, tmpBundle); err != nil {
		os.Remove(tmpBundle)
		return err
	}
	sum, err := sha256File(tmpBundle)
	if err != nil {
		os.Remove(tmpBundle)
		return err
	}
	if err := os.Rename(tmpBundle, bundlePath); err != nil {
		os.Remove(tmpBundle)
		return err
	}

	// Sidecar before latest.txt; latest.txt is updated last so it only ever
	// points at a fully-written, verified bundle with a complete sidecar.
	sidecar := fmt.Sprintf("%s  %s\n", sum, base)
	if err := atomicWriteFile(bundlePath+".sha256", []byte(sidecar), 0644); err != nil {
		return err
	}

	rel := filepath.ToSlash(filepath.Join(versionsDir, base))
	if err := atomicWriteFile(filepath.Join(backupDir, latestFile), []byte(rel+"\n"), 0644); err != nil {
		return err
	}

	fmt.Printf("synced %s -> %s/%s\n", repoPath, backupDir, rel)
	return nil
}

// restoreBackup restores the latest version of the backup at backupDir into
// restorePath, after checking the sha256 sidecar and verifying the bundle.
func restoreBackup(backupDir, restorePath string) error {
	backupDir, err := filepath.Abs(backupDir)
	if err != nil {
		return err
	}
	meta, err := loadBackupMeta(backupDir)
	if err != nil {
		return fmt.Errorf("not a backup directory (%s): %w", backupDir, err)
	}

	data, err := os.ReadFile(filepath.Join(backupDir, latestFile))
	if err != nil {
		return fmt.Errorf("read %s: %w", latestFile, err)
	}
	rel := strings.TrimSpace(string(data))
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

	fmt.Printf("restored %q (%s) -> %s\n", meta.Name, rel, restorePath)
	return nil
}

// initBackup ensures backupDir is an initialized backup: it creates versions/
// and, if BK_BACKUP.json is absent, writes one with a fresh id. An existing
// sentinel is left untouched so the id is stable across syncs.
func initBackup(dir, name string) error {
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
	if name == "" {
		name = filepath.Base(dir)
	}
	data, err := json.MarshalIndent(backupMeta{ID: id, Name: name}, "", "  ")
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
	defer f.Close()

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
	defer os.Remove(tmp) // no-op once the rename succeeds

	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
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
