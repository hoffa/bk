package cli

import (
	"context"
	"fmt"
	"image/color"
	"io"
	"os"

	"charm.land/lipgloss/v2"

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

// statusDotChar is the fat status dot. U+25CF is present in virtually every
// font, so it stays broadly compatible.
const statusDotChar = "●"

// statusDot maps a backup's currency + presence to a colored dot: green synced,
// yellow stale, red error, grey never-synced. An absent (offline) target is
// dimmed (muted green / muted yellow) so present/absent reads at a glance while
// still showing the cached verdict.
func statusDot(s bk.State, present bool) string {
	switch s {
	case bk.StateError:
		return dot(lipgloss.Red, false)
	case bk.StateSynced:
		return dot(lipgloss.Green, !present)
	case bk.StateStale:
		return dot(lipgloss.Yellow, !present)
	default: // never synced / checking
		return dot(lipgloss.BrightBlack, false)
	}
}

// dot renders the status dot in color c; faint dims it (for offline targets).
// Bubble Tea downsamples the color to the terminal's profile and drops it under
// NO_COLOR, leaving a plain dot.
func dot(c color.Color, faint bool) string {
	style := lipgloss.NewStyle().Foreground(c)
	if faint {
		style = style.Faint(true)
	}

	return style.Render(statusDotChar)
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
