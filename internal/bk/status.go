package bk

import (
	"context"
	"path/filepath"
	"time"

	"github.com/hoffa/bk/internal/git"
	"github.com/hoffa/bk/internal/util"
)

// State is a configured backup's currency. Whether the target is currently
// connected is tracked separately, in Status.Present.
type State int

// Backup currency states.
const (
	StateChecking State = iota // not yet evaluated (zero value)
	StateSynced                // up to date
	StateStale                 // repo changed since last sync
	StateUnsynced              // never synced
	StateError                 // misconfigured or unreadable
)

// Label is a human-readable description of the state.
func (s State) Label() string {
	switch s {
	case StateSynced:
		return "synced"
	case StateStale:
		return "out of date"
	case StateUnsynced:
		return "never synced"
	case StateChecking:
		return "checking"
	default:
		return "error"
	}
}

// NeedsSync reports whether syncing the entry would create a new version.
func (s State) NeedsSync() bool {
	return s == StateStale || s == StateUnsynced
}

// Status is an entry's currency plus presence and display details.
type Status struct {
	Entry

	State    State
	Present  bool      // target currently reachable
	Versions int       // only when present and a valid backup
	LastSync time.Time // zero if unknown
}

// Eval computes one entry's currency and presence without modifying anything.
// When the target is present its latest.json is authoritative; when absent,
// currency is inferred from the cached refs hash (best effort).
func Eval(ctx context.Context, e Entry) Status {
	s := Status{Entry: e}

	target, err := filepath.Abs(e.Target)
	if err != nil {
		s.State = StateError
		return s
	}

	s.Present = util.Exists(target)

	if e.ID == "" {
		// Never synced. If the target exists but isn't safe to initialize
		// (non-empty, not a backup), surface an error instead of NEW so we don't
		// keep trying to write into it.
		if s.Present {
			if ok, err := backupDirUsable(target); err != nil || !ok {
				s.State = StateError
				return s
			}
		}

		s.State = StateUnsynced

		return s
	}

	if !s.Present {
		// Offline: judge currency from the cached last-synced refs.
		s.State = StateStale

		if e.RefsHash != "" {
			if rh, err := git.RefsHash(ctx, e.Source); err == nil && rh == e.RefsHash {
				s.State = StateSynced
			}
		}

		return s
	}

	// Present: the target is authoritative.
	meta, err := loadBackupMeta(target)
	if err != nil {
		s.State = StateError
		return s
	}

	if meta.ID != e.ID {
		s.State = StateError
		return s
	}

	latest, err := readLatest(target)
	if err != nil {
		s.State = StateStale
		return s
	}

	s.LastSync = latest.SyncedAt
	if bundles, _ := filepath.Glob(filepath.Join(target, versionsDir, "*.bundle")); bundles != nil {
		s.Versions = len(bundles)
	}

	if rh, err := git.RefsHash(ctx, e.Source); err == nil && rh == latest.RefsHash {
		s.State = StateSynced
	} else {
		s.State = StateStale
	}

	return s
}
