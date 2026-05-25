// Package util holds small, stdlib-only helpers reused across bk: file
// existence checks, file hashing, atomic writes, and random hex ids.
package util

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
)

// Exists reports whether path exists.
func Exists(path string) bool {
	_, err := os.Stat(path)

	return err == nil
}

// SHA256 returns the hex-encoded sha256 of the file at path.
func SHA256(path string) (string, error) {
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

// AtomicWrite writes data to path atomically: it writes a temp file in the same
// directory, fsyncs it, then renames it over path. A reader sees either the old
// file or the complete new one, never a partial write.
func AtomicWrite(path string, data []byte, perm os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".fileutil-tmp-*")
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

// RandHex returns n random bytes encoded as a 2n-character hex string.
func RandHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}

	return hex.EncodeToString(b), nil
}
