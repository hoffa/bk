package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"text/tabwriter"
	"time"
)

// entryState is a configured backup's sync state. It is the shared core model
// behind both `bk status` and the dashboard.
type entryState int

const (
	stateChecking entryState = iota // not yet evaluated (zero value)
	stateSynced                     // up to date
	stateStale                      // repo changed since last sync
	stateUnsynced                   // never synced
	stateAbsent                     // target not present (e.g. unplugged)
	stateError                      // misconfigured or unreadable
)

func (s entryState) label() string {
	switch s {
	case stateSynced:
		return "synced"
	case stateStale:
		return "out of date"
	case stateUnsynced:
		return "never synced"
	case stateAbsent:
		return "absent"
	case stateChecking:
		return "checking"
	default:
		return "error"
	}
}

// needsSync reports whether syncing the entry would create a new version.
func (s entryState) needsSync() bool {
	return s == stateStale || s == stateUnsynced
}

// evalEntry computes an entry's state without modifying anything. It reads the
// source repo only when needed to tell synced from out of date.
func evalEntry(e syncEntry) entryState {
	target, err := filepath.Abs(e.Target)
	if err != nil {
		return stateError
	}

	if e.ID == "" {
		// Never synced; syncable only if the parent (e.g. a mount) is present.
		if _, err := os.Stat(filepath.Dir(target)); err != nil {
			return stateAbsent
		}
		return stateUnsynced
	}

	if _, err := os.Stat(target); err != nil {
		return stateAbsent
	}
	meta, err := loadBackupMeta(target)
	if err != nil {
		return stateError
	}
	if meta.ID != e.ID {
		return stateError
	}
	latest, err := readLatest(target)
	if err != nil {
		return stateStale
	}
	refsHash, err := repoRefsHash(e.Source)
	if err != nil {
		return stateError
	}
	if latest.RefsHash == refsHash {
		return stateSynced
	}
	return stateStale
}

// backupStatus is an entry's state plus display details for `bk status`.
type backupStatus struct {
	syncEntry
	state    entryState
	versions int
	lastSync time.Time // zero if unknown
}

// statusAll returns the state of every configured entry.
func statusAll() ([]backupStatus, error) {
	cfg, _, err := loadConfig()
	if err != nil {
		return nil, err
	}
	out := make([]backupStatus, 0, len(cfg.Sync))
	for _, e := range cfg.Sync {
		s := backupStatus{syncEntry: e, state: evalEntry(e)}
		// versions/last-sync only exist once the target is a valid backup.
		if s.state == stateSynced || s.state == stateStale {
			target, _ := filepath.Abs(e.Target)
			bundles, _ := filepath.Glob(filepath.Join(target, versionsDir, "*.bundle"))
			s.versions = len(bundles)
			if l, err := readLatest(target); err == nil {
				s.lastSync = l.SyncedAt
			}
		}
		out = append(out, s)
	}
	return out, nil
}

// short returns the first n characters of s.
func short(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// printStatus renders entry statuses as an aligned table.
func printStatus(w io.Writer, statuses []backupStatus) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	// Buffered writes; any error surfaces on Flush.
	_, _ = fmt.Fprintln(tw, "ID\tSOURCE\tTARGET\tSTATE\tVERSIONS\tLAST SYNC")
	for _, s := range statuses {
		id, versions, last := "-", "-", "-"
		if s.ID != "" {
			id = short(s.ID, 12)
		}
		if s.versions > 0 {
			versions = strconv.Itoa(s.versions)
		}
		if !s.lastSync.IsZero() {
			last = s.lastSync.Format("2006-01-02 15:04:05Z")
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", id, s.Source, s.Target, s.state.label(), versions, last)
	}
	return tw.Flush()
}
