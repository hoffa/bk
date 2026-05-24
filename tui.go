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
// state/syncing survive entries being added or removed between ticks.
func entryKey(e syncEntry) string {
	return e.Source + "\x00" + e.Target
}

// tuiModel is the live dashboard. It is a thin view over the core model: each
// tick it reloads the config, re-evaluates every entry via evalEntry, and syncs
// any that need it via syncConfigured. All git/filesystem work happens off the
// UI goroutine via commands; state and syncing are keyed by entryKey.
type tuiModel struct {
	entries  []syncEntry
	states   map[string]entryState
	syncing  map[string]bool
	quitting bool
}

type (
	tickMsg    time.Time
	refreshMsg struct {
		entries []syncEntry
		states  map[string]entryState
	}
	syncResultMsg struct {
		entry syncEntry
		err   error
	}
)

// runTUI runs the live watch dashboard until the user quits.
func runTUI() error {
	m := tuiModel{
		states:  map[string]entryState{},
		syncing: map[string]bool{},
	}
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
		}

	case tickMsg:
		// Reload config + re-check (picks up entries added in another terminal,
		// plugged-in drives, and new commits).
		return m, tea.Batch(refreshCmd(), tickCmd())

	case refreshMsg:
		m.entries = msg.entries
		m.states = msg.states
		var cmds []tea.Cmd
		for _, e := range m.entries {
			k := entryKey(e)
			if m.states[k].needsSync() && !m.syncing[k] {
				m.syncing[k] = true
				cmds = append(cmds, syncEntryCmd(e))
			}
		}
		return m, tea.Batch(cmds...)

	case syncResultMsg:
		k := entryKey(msg.entry)
		delete(m.syncing, k)
		if msg.err != nil {
			m.states[k] = stateError
		} else {
			m.states[k] = evalEntry(msg.entry)
			persistID(msg.entry) // record a first-sync id back to the config
		}
		return m, nil
	}

	return m, nil
}

func (m tuiModel) View() string {
	if m.quitting {
		return ""
	}

	var b strings.Builder
	b.WriteString("bk — watching, q to quit\n\n")
	if len(m.entries) == 0 {
		b.WriteString("no backups configured; add one with: bk add <repo> <backup-dir>\n")
		return b.String()
	}

	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	for _, e := range m.entries {
		k := entryKey(e)
		st := m.states[k] // missing key -> stateChecking (zero value)
		indicator, label := dot(true, st), st.label()
		if m.syncing[k] {
			indicator, label = colorize("36", "⏺"), "syncing…" // cyan
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", indicator, e.Source, e.Target, label)
	}
	_ = tw.Flush()
	return b.String()
}

// refreshCmd reloads the config and evaluates every entry off the UI goroutine.
func refreshCmd() tea.Cmd {
	return func() tea.Msg {
		cfg, _, err := loadConfig()
		if err != nil {
			return refreshMsg{}
		}
		states := make(map[string]entryState, len(cfg.Sync))
		for _, e := range cfg.Sync {
			states[entryKey(e)] = evalEntry(e)
		}
		return refreshMsg{entries: cfg.Sync, states: states}
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

// persistID writes a first-sync id back to the config, merging into the current
// on-disk config so a concurrent `bk add` isn't clobbered.
func persistID(e syncEntry) {
	if e.ID == "" {
		return
	}
	cfg, _, err := loadConfig()
	if err != nil {
		return
	}
	changed := false
	for i := range cfg.Sync {
		if cfg.Sync[i].Source == e.Source && cfg.Sync[i].Target == e.Target && cfg.Sync[i].ID == "" {
			cfg.Sync[i].ID = e.ID
			changed = true
		}
	}
	if changed {
		_ = saveConfig(cfg)
	}
}
