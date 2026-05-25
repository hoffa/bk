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

// badgeWidth is the longest code ("STALE?") plus a space of padding each side.
const badgeWidth = 8

// badge renders a status code as a fixed-width reverse-video cell, like a test
// runner's PASS/FAIL. Bubble Tea downsamples the color to the terminal's profile
// (and drops it under NO_COLOR), so it degrades to plain ASCII.
func badge(s bk.State, present bool) string {
	c := color.Color(lipgloss.BrightBlack) // grey: never synced / checking

	switch s {
	case bk.StateSynced:
		c = lipgloss.Green
	case bk.StateStale:
		c = lipgloss.Yellow
	case bk.StateError:
		c = lipgloss.Red
	}

	return styleBadge(c, statusCode(s, present))
}

// styleBadge renders text left-aligned in a fixed-width cell. The color is shown
// via reverse video, so it becomes the background and the text is the terminal's
// own background color -- theme-adaptive for every state.
func styleBadge(c color.Color, text string) string {
	return lipgloss.NewStyle().
		Reverse(true).
		Foreground(c).
		Width(badgeWidth).
		Render(" " + text)
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
