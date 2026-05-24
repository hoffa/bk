package main

import (
	"fmt"
	"io"
	"os"
	"text/tabwriter"
)

// dashboard is the `bk` (no args) entry point. On a terminal it runs the live
// watch TUI; otherwise (piped/CI) it does a single pass so it doesn't hang.
func dashboard(w io.Writer) error {
	if isTerminal(w) {
		return runTUI()
	}
	return runDashboard(w)
}

// runDashboard renders the state of every configured backup and syncs any that
// are out of date or never synced. It is a thin presentation layer over the
// core model (evalEntry) and sync logic (syncConfigured).
func runDashboard(w io.Writer) error {
	cfg, _, err := loadConfig()
	if err != nil {
		return err
	}
	if len(cfg.Sync) == 0 {
		_, _ = fmt.Fprintln(w, "no backups configured; add one with: bk add <repo> <backup-dir>")
		return nil
	}

	color := useColor(w)
	states := make([]entryState, len(cfg.Sync))
	pending := false
	for i, e := range cfg.Sync {
		states[i] = evalEntry(e)
		pending = pending || states[i].needsSync()
	}
	renderDashboard(w, color, cfg.Sync, states)

	if !pending {
		return nil
	}

	_, _ = fmt.Fprintln(w)
	var failed int
	var dirty bool
	for i := range cfg.Sync {
		if !states[i].needsSync() {
			continue
		}
		e := &cfg.Sync[i]
		hadID := e.ID != ""
		_, _ = fmt.Fprintf(w, "syncing %s ...\n", e.Source)
		if _, err := syncConfigured(e); err != nil {
			_, _ = fmt.Fprintf(w, "  error: %v\n", err)
			states[i] = stateError
			failed++
		} else {
			states[i] = stateSynced
		}
		if !hadID && e.ID != "" {
			dirty = true // first sync recorded the target's id
		}
	}
	if dirty {
		if err := saveConfig(cfg); err != nil {
			return fmt.Errorf("save config: %w", err)
		}
	}

	_, _ = fmt.Fprintln(w)
	renderDashboard(w, color, cfg.Sync, states)
	if failed > 0 {
		return fmt.Errorf("%d of %d backups failed", failed, len(cfg.Sync))
	}
	return nil
}

// renderDashboard writes one colored ⏺ line per entry.
func renderDashboard(w io.Writer, color bool, entries []syncEntry, states []entryState) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for i, e := range entries {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", dot(color, states[i]), e.Source, e.Target, states[i].label())
	}
	_ = tw.Flush()
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

// useColor reports whether to emit ANSI colors: only to a terminal, and not
// when NO_COLOR is set.
func useColor(w io.Writer) bool {
	return os.Getenv("NO_COLOR") == "" && isTerminal(w)
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
