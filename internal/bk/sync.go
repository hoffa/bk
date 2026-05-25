package bk

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Sync backs up a single configured entry. On the first sync (empty ID) it
// initializes the target and records its id; afterwards it verifies the target's
// id matches before syncing. A target that isn't present is reported as
// ErrTargetAbsent (e.g. an unplugged drive), which callers typically treat as a
// skip rather than a failure. It updates e.ID and e.RefsHash in place and reports
// whether a new version was written.
func Sync(ctx context.Context, e *Entry) (bool, error) {
	target, err := filepath.Abs(e.Target)
	if err != nil {
		return false, err
	}

	if e.ID == "" {
		// First sync: the parent (e.g. a mount point) must exist so we create the
		// backup on the intended volume, not somewhere a missing mount used to be.
		if _, err := os.Stat(filepath.Dir(target)); errors.Is(err, os.ErrNotExist) {
			return false, ErrTargetAbsent
		} else if err != nil {
			return false, err
		}

		if err := initBackup(target); err != nil {
			return false, err
		}

		meta, err := loadBackupMeta(target)
		if err != nil {
			return false, err
		}

		e.ID = meta.ID
	} else {
		if _, err := os.Stat(target); errors.Is(err, os.ErrNotExist) {
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
	}

	synced, err := syncBackup(ctx, e.Source, target)
	if err != nil {
		return false, err
	}
	// Cache the synced refs + time so currency and last-sync time are known while
	// the target is absent.
	if l, err := readLatest(target); err == nil {
		e.RefsHash = l.RefsHash
		e.SyncedAt = l.SyncedAt.UTC().Format(time.RFC3339)
	}

	return synced, nil
}
