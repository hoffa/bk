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

// entryState is a configured backup's currency. It is the shared core model
// behind both `bk status` and the dashboard. Whether the target is currently
// connected is tracked separately (backupStatus.present).
type entryState int

const (
	stateChecking entryState = iota // not yet evaluated (zero value)
	stateSynced                     // up to date
	stateStale                      // repo changed since last sync
	stateUnsynced                   // never synced
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

// backupStatus is an entry's currency plus presence and display details.
type backupStatus struct {
	syncEntry
	state    entryState
	present  bool      // target currently reachable
	versions int       // only when present and a valid backup
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
		out = append(out, evalStatus(e))
	}
	return out, nil
}

// evalStatus computes one entry's currency and presence without modifying
// anything. When the target is present its latest.json is authoritative; when
// absent, currency is inferred from the cached refs hash (best effort).
func evalStatus(e syncEntry) backupStatus {
	s := backupStatus{syncEntry: e}

	target, err := filepath.Abs(e.Target)
	if err != nil {
		s.state = stateError
		return s
	}
	s.present = statExists(target)

	if e.ID == "" {
		s.state = stateUnsynced
		return s
	}

	if !s.present {
		// Offline: judge currency from the cached last-synced refs.
		s.state = stateStale
		if e.RefsHash != "" {
			if rh, err := repoRefsHash(e.Source); err == nil && rh == e.RefsHash {
				s.state = stateSynced
			}
		}
		return s
	}

	// Present: the target is authoritative.
	meta, err := loadBackupMeta(target)
	if err != nil {
		s.state = stateError
		return s
	}
	if meta.ID != e.ID {
		s.state = stateError
		return s
	}
	latest, err := readLatest(target)
	if err != nil {
		s.state = stateStale
		return s
	}
	s.lastSync = latest.SyncedAt
	if bundles, _ := filepath.Glob(filepath.Join(target, versionsDir, "*.bundle")); bundles != nil {
		s.versions = len(bundles)
	}
	if rh, err := repoRefsHash(e.Source); err == nil && rh == latest.RefsHash {
		s.state = stateSynced
	} else {
		s.state = stateStale
	}
	return s
}

// evalEntry returns just an entry's currency state.
func evalEntry(e syncEntry) entryState {
	return evalStatus(e).state
}

func statExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
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
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", id, s.Source, s.Target, statusCode(s.state, s.present), versions, last)
	}
	return tw.Flush()
}
