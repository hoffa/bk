package main

import (
	"fmt"
	"strings"
	"text/tabwriter"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// tickInterval is how often the TUI reloads the config and re-checks backups.
const tickInterval = 2 * time.Second

// entryKey identifies a configured entry stably across config reloads, so
// syncing state survives entries being added or removed between ticks.
func entryKey(e syncEntry) string {
	return e.Source + "\x00" + e.Target
}

// tuiModel is the live status dashboard. By default it only shows status; auto
// sync is opt-in via a key. It is a thin view over the core: each tick it
// reloads the config and re-evaluates via statusAll, and (when auto sync is on)
// syncs out-of-date entries via syncConfigured. All work happens off the UI
// goroutine via commands.
type tuiModel struct {
	statuses []backupStatus
	syncing  map[string]bool
	autoSync bool
	quitting bool
}

type (
	tickMsg       time.Time
	refreshMsg    []backupStatus
	syncResultMsg struct {
		entry syncEntry
		err   error
	}
)

// runTUI runs the live status dashboard until the user quits.
func runTUI() error {
	m := tuiModel{syncing: map[string]bool{}}
	_, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}

func (m tuiModel) Init() tea.Cmd {
	return tea.Batch(refreshCmd(), tickCmd())
}

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			m.quitting = true
			return m, tea.Quit
		case "a":
			m.autoSync = !m.autoSync
			if m.autoSync {
				return m, m.startSyncs()
			}
			return m, nil
		}

	case tickMsg:
		// Reload config + re-check (picks up entries added elsewhere, plugged-in
		// drives, and new commits).
		return m, tea.Batch(refreshCmd(), tickCmd())

	case refreshMsg:
		m.statuses = []backupStatus(msg)
		if m.autoSync {
			return m, m.startSyncs()
		}
		return m, nil

	case syncResultMsg:
		k := entryKey(msg.entry)
		delete(m.syncing, k)
		if msg.err == nil {
			persistEntry(msg.entry) // record first-sync id + refs hash to the config
		}
		// Re-evaluate so the row reflects reality (errTargetAbsent, etc.) rather
		// than forcing an error color on a transient failure.
		for i := range m.statuses {
			if entryKey(m.statuses[i].syncEntry) == k {
				m.statuses[i] = evalStatus(msg.entry)
				break
			}
		}
		return m, nil
	}

	return m, nil
}

// startSyncs kicks off syncs for every out-of-date entry not already syncing.
func (m tuiModel) startSyncs() tea.Cmd {
	var cmds []tea.Cmd
	for _, s := range m.statuses {
		k := entryKey(s.syncEntry)
		// Only sync targets that are actually connected.
		if s.present && s.state.needsSync() && !m.syncing[k] {
			m.syncing[k] = true
			cmds = append(cmds, syncEntryCmd(s.syncEntry))
		}
	}
	return tea.Batch(cmds...)
}

func (m tuiModel) View() string {
	if m.quitting {
		return ""
	}

	var b strings.Builder
	if len(m.statuses) == 0 {
		b.WriteString("no backups configured; add one with: bk add <repo> <backup-dir>\n")
		return b.String()
	}

	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	for _, s := range m.statuses {
		indicator, label := dot(true, s.state, s.present), s.label()
		if m.syncing[entryKey(s.syncEntry)] {
			indicator, label = colorize("36", "⏺"), "syncing…" // cyan
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", indicator, s.Source, s.Target, label, relTime(s.lastSync))
	}
	_ = tw.Flush()

	mode := "off"
	if m.autoSync {
		mode = "on"
	}
	_, _ = fmt.Fprintf(&b, "\nauto-sync: %s  ·  a: toggle auto-sync   q: quit\n", mode)
	return b.String()
}

// relTime renders a sync time as a short relative string.
func relTime(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

// refreshCmd reloads the config and evaluates every entry off the UI goroutine.
func refreshCmd() tea.Cmd {
	return func() tea.Msg {
		statuses, err := statusAll()
		if err != nil {
			return refreshMsg(nil)
		}
		return refreshMsg(statuses)
	}
}

// syncEntryCmd syncs one entry off the UI goroutine, working on a copy so the
// result can be applied back in Update without a data race.
func syncEntryCmd(e syncEntry) tea.Cmd {
	return func() tea.Msg {
		ec := e
		_, err := syncConfigured(&ec)
		return syncResultMsg{entry: ec, err: err}
	}
}

func tickCmd() tea.Cmd {
	return tea.Tick(tickInterval, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// persistEntry writes a synced entry's id and refs hash back to the config,
// merging into the current on-disk config so a concurrent `bk add` isn't
// clobbered.
func persistEntry(e syncEntry) {
	cfg, _, err := loadConfig()
	if err != nil {
		return
	}
	changed := false
	for i := range cfg.Sync {
		c := &cfg.Sync[i]
		if c.Source != e.Source || c.Target != e.Target {
			continue
		}
		if c.ID == "" && e.ID != "" {
			c.ID = e.ID
			changed = true
		}
		if e.RefsHash != "" && c.RefsHash != e.RefsHash {
			c.RefsHash = e.RefsHash
			changed = true
		}
	}
	if changed {
		_ = saveConfig(cfg)
	}
}
