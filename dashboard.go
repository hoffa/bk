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

// statusCode is the short ASCII code for a state + presence. A trailing "?"
// means unverified: the target is absent, so the verdict is inferred from the
// last sync recorded in the config rather than confirmed against the target.
func statusCode(s entryState, present bool) string {
	q := ""
	if !present {
		q = "?"
	}
	switch s {
	case stateSynced:
		return "OK" + q
	case stateStale:
		return "STALE" + q
	case stateUnsynced:
		return "NEW"
	case stateError:
		return "ERR"
	default:
		return "--"
	}
}

// badge renders a status code as a fixed-width cell, colored as a background
// badge (like a test runner's PASS/FAIL) when color is enabled.
func badge(color bool, s entryState, present bool) string {
	return badgeText(color, bgColor(s), statusCode(s, present))
}

// badgeText renders text as a fixed-width badge with the given ANSI code.
func badgeText(color bool, code, text string) string {
	cell := fmt.Sprintf(" %-6s ", text) // 8 visible columns, regardless of color
	if !color {
		return cell
	}
	return "\033[" + code + "m" + cell + "\033[0m"
}

// bgColor is the ANSI attribute (fg;bg) for a state's badge.
func bgColor(s entryState) string {
	switch s {
	case stateSynced:
		return "30;42" // black on green
	case stateStale:
		return "30;43" // black on yellow
	case stateError:
		return "97;41" // white on red
	default: // unsynced, checking
		return "30;47" // black on grey
	}
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
