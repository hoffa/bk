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

// Backup states reported by status.
const (
	stateOK          = "ok"
	stateNeverSynced = "never synced"
	stateAbsent      = "absent"
	stateNotBackup   = "not a backup"
	stateIDMismatch  = "id mismatch"
)

// backupStatus is the computed state of one configured entry.
type backupStatus struct {
	syncEntry
	state    string
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
		out = append(out, entryStatus(e))
	}
	return out, nil
}

// entryStatus inspects one entry's target without modifying anything.
func entryStatus(e syncEntry) backupStatus {
	s := backupStatus{syncEntry: e}

	if e.ID == "" {
		s.state = stateNeverSynced
		return s
	}

	target, err := filepath.Abs(e.Target)
	if err != nil {
		s.state = stateAbsent
		return s
	}
	if _, err := os.Stat(target); err != nil {
		s.state = stateAbsent
		return s
	}
	meta, err := loadBackupMeta(target)
	if err != nil {
		s.state = stateNotBackup
		return s
	}
	if meta.ID != e.ID {
		s.state = stateIDMismatch
		return s
	}

	s.state = stateOK
	bundles, _ := filepath.Glob(filepath.Join(target, versionsDir, "*.bundle"))
	s.versions = len(bundles)
	if l, err := readLatest(target); err == nil {
		s.lastSync = l.SyncedAt
	}
	return s
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
		if s.state == stateOK {
			versions = strconv.Itoa(s.versions)
		}
		if !s.lastSync.IsZero() {
			last = s.lastSync.Format("2006-01-02 15:04:05Z")
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", id, s.Source, s.Target, s.state, versions, last)
	}
	return tw.Flush()
}
