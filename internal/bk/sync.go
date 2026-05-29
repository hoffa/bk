package bk

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/hoffa/bk/internal/crypt"
	"github.com/hoffa/bk/internal/git"
)

// CheckRepo verifies that path is a readable git repository. It lets `add` fail
// fast with a clear message instead of surfacing a raw git error at first sync.
func CheckRepo(ctx context.Context, path string) error {
	if _, err := git.RefsHash(ctx, path); err != nil {
		return fmt.Errorf("not a git repository: %s", path)
	}

	return nil
}

// Sync backs up a single configured entry. On the first sync it initializes the
// target with the entry's ID and a keyring from newKeyring; afterwards it
// verifies the target's id matches before syncing. A target that isn't present
// is reported as ErrTargetAbsent (e.g. an unplugged drive), which callers
// typically treat as a skip rather than a failure. Bundles are encrypted to the
// target's own stored keyring, so every version in a target stays decryptable.
// It fills in e.Backup (refs, time) in place and reports whether a new version
// was written.
func Sync(ctx context.Context, e *Entry, newKeyring func() (crypt.Keyring, error)) (bool, error) {
	if e.ID == "" {
		return false, errors.New("missing entry id")
	}

	target, err := filepath.Abs(e.Target)
	if err != nil {
		return false, err
	}

	if e.Backup == nil {
		// First sync: the parent (e.g. a mount point) must exist so we create the
		// backup on the intended volume, not somewhere a missing mount used to be.
		if _, err := os.Stat(filepath.Dir(target)); errors.Is(err, os.ErrNotExist) {
			return false, ErrTargetAbsent
		} else if err != nil {
			return false, err
		}

		kr, err := newKeyring()
		if err != nil {
			return false, err
		}

		if err := initBackup(target, e.ID, kr); err != nil {
			return false, err
		}
	} else if _, err := os.Stat(target); errors.Is(err, os.ErrNotExist) {
		return false, ErrTargetAbsent
	} else if err != nil {
		return false, err
	}

	meta, err := loadBackupMeta(target)
	if err != nil {
		return false, fmt.Errorf("not a valid backup: %w", err)
	}

	if meta.ID != e.ID {
		return false, fmt.Errorf("id mismatch: expected %s, found %s (wrong target?)", e.ID, meta.ID)
	}

	// Encrypt to the target's own keyring (kr seeds a new target; an existing one
	// keeps the keyring it was created with).
	synced, err := syncBackup(ctx, e.Source, target, e.ID, meta.Key)
	if err != nil {
		return false, err
	}

	if e.Backup == nil {
		e.Backup = &Backup{}
	}

	// Cache the synced refs + time so currency and last-sync time are known while
	// the target is absent.
	if l, err := readLatest(target); err == nil {
		e.Backup.ContentHash = l.ContentHash
		e.Backup.SyncedAt = l.SyncedAt
	}

	return synced, nil
}
