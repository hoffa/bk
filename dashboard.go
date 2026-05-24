package main

import (
	"fmt"
	"io"
	"os"
	"strings"
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

// statusGlyph is the status mark: a filled circle (as used by systemctl), or
// ASCII when the terminal locale isn't UTF-8. The color carries the meaning, so
// the exact glyph is cosmetic.
func statusGlyph() string {
	if utf8Locale() {
		return "●"
	}
	return "*"
}

// utf8Locale reports whether the environment advertises a UTF-8 locale.
func utf8Locale() bool {
	for _, k := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		if v := os.Getenv(k); v != "" {
			v = strings.ToUpper(v)
			return strings.Contains(v, "UTF-8") || strings.Contains(v, "UTF8")
		}
	}
	return false
}

// dot returns the status indicator, colored when enabled. Color encodes
// currency; present=false dims it to signal a disconnected target.
func dot(color bool, s entryState, present bool) string {
	c := statusGlyph()
	if !color {
		return c
	}
	dim := ""
	if !present {
		dim = "2;"
	}
	switch s {
	case stateSynced:
		return colorize(dim+"32", c) // green (dim if offline)
	case stateStale:
		return colorize(dim+"33", c) // yellow (dim if offline)
	case stateUnsynced, stateChecking:
		return colorize("2", c) // muted
	default:
		return colorize("31", c) // red — actual error
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
