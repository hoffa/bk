package main

import (
	"fmt"
	"io"
	"os"
)

// dashboard is the `bk` (no args) entry point. On a terminal it runs the live
// watch TUI; otherwise (piped/CI) it prints a one-shot status snapshot.
// Neither syncs by default — use `bk sync`, or toggle auto-sync in the TUI.
func dashboard(w io.Writer) error {
	if isTerminal(w) {
		return runTUI()
	}

	statuses, err := statusAll()
	if err != nil {
		return err
	}
	if len(statuses) == 0 {
		_, _ = fmt.Fprintln(w, "no backups configured; add one with: bk add <repo> <backup-dir>")
		return nil
	}
	return printStatus(w, statuses)
}

// dot returns the status indicator, colored when enabled.
func dot(color bool, s entryState) string {
	const c = "⏺"
	if !color {
		return c
	}
	switch s {
	case stateSynced:
		return colorize("32", c) // green
	case stateStale:
		return colorize("33", c) // yellow
	case stateUnsynced:
		return colorize("2;33", c) // dim yellow (muted)
	case stateChecking:
		return colorize("2", c) // dim
	default:
		return colorize("31", c) // red
	}
}

func colorize(code, s string) string {
	return "\033[" + code + "m" + s + "\033[0m"
}

// isTerminal reports whether w is a character device (a terminal).
func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}
