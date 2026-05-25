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

	if e.Backup == nil {
		// Never synced. The first sync creates the target, so "reachable" means
		// the parent (e.g. a mount point) exists -- not the target itself, which
		// won't exist yet. Without this, auto-sync would never fire on a fresh add.
		s.Present = util.Exists(filepath.Dir(target))

		// If the target already exists but isn't safe to initialize (non-empty,
		// not a backup), surface an error instead of NEW so we don't keep trying
		// to write into it.
		if util.Exists(target) {
			if ok, err := backupDirUsable(target); err != nil || !ok {
				s.State = StateError
				return s
			}
		}

		s.State = StateUnsynced

		return s
	}

	if !s.Present {
		// Offline: judge currency from the cached last-synced refs, and show the
		// cached last-sync time (how stale this unplugged copy is).
		s.State = StateStale
		s.LastSync = e.Backup.SyncedAt

		if e.Backup.ContentHash != "" {
			switch rh, err := git.RefsHash(ctx, e.Source); {
			case err != nil:
				s.State = StateError // source unreadable (e.g. deleted repo)
			case rh == e.Backup.ContentHash:
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

	if meta.ID != e.Backup.ID {
		s.State = StateError
		return s
	}

	// A target we can't encrypt to (e.g. a legacy target with an empty keyring)
	// can't be synced; report it here rather than only at sync time.
	if meta.Key.Public == "" {
		s.State = StateError
		return s
	}

	latest, err := readLatest(target)
	if err != nil {
		s.State = StateStale
		return s
	}

	s.LastSync = latest.SyncedAt
	if bundles, _ := filepath.Glob(filepath.Join(target, versionsDir, "*.bundle"+encExt)); bundles != nil {
		s.Versions = len(bundles)
	}

	switch rh, err := git.RefsHash(ctx, e.Source); {
	case err != nil:
		s.State = StateError // source unreadable (e.g. deleted repo)
	case rh == latest.ContentHash:
		s.State = StateSynced
	default:
		s.State = StateStale
	}

	return s
}
