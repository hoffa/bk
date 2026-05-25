package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/hoffa/bk/internal/bk"
)

// dashboard is the `bk` (no args) entry point. On a terminal it runs the live
// watch TUI; otherwise (piped/CI) it prints a one-shot status snapshot.
// Neither syncs by default -- use `bk sync`, or toggle auto-sync in the TUI.
func dashboard(ctx context.Context, w io.Writer) error {
	if isTerminal(w) {
		return runTUI(ctx)
	}

	statuses, err := evalAll(ctx)
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
func statusCode(s bk.State, present bool) string {
	q := ""
	if !present {
		q = "?"
	}

	switch s {
	case bk.StateSynced:
		return "OK" + q
	case bk.StateStale:
		return "STALE" + q
	case bk.StateUnsynced:
		return "NEW"
	case bk.StateError:
		return "ERROR"
	default:
		return "--"
	}
}

// badgeWidth is the longest code ("STALE?") plus a space of padding each side.
const badgeWidth = 8

// badge renders a status code as a fixed-width cell, colored as a background
// badge (like a test runner's PASS/FAIL) when color is enabled.
func badge(color bool, s bk.State, present bool) string {
	return badgeText(color, badgeColor(s), statusCode(s, present))
}

// badgeText renders text left-aligned in a fixed-width badge using the given
// foreground color plus reverse video, so the color becomes the background and
// the text is the terminal's own background color -- consistent and
// theme-adaptive for every state.
func badgeText(color bool, ansiColor, text string) string {
	cell := fmt.Sprintf(" %-*s", badgeWidth-1, text) // leading space, padded right
	if !color {
		return cell
	}

	return "\033[" + ansiColor + ";7m" + cell + "\033[0m"
}

// badgeColor is the ANSI foreground color for a state's badge (reverse video
// turns it into the background).
func badgeColor(s bk.State) string {
	switch s {
	case bk.StateSynced:
		return "32" // green
	case bk.StateStale:
		return "33" // yellow
	case bk.StateError:
		return "31" // red
	default: // unsynced, checking
		return "90" // grey
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
