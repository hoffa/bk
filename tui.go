package main

import (
	"fmt"
	"strings"
	"text/tabwriter"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// tickInterval is how often the TUI re-checks every backup.
const tickInterval = 2 * time.Second

// tuiModel is the live dashboard. It is a thin view over the core model: it
// holds entries + their states and drives evalEntry / syncConfigured via
// commands. All git/filesystem work happens off the UI goroutine.
type tuiModel struct {
	entries  []syncEntry
	states   []entryState
	syncing  []bool
	quitting bool
}

type (
	tickMsg       time.Time
	statesMsg     []entryState
	syncResultMsg struct {
		i     int
		entry syncEntry
		err   error
	}
)

// runTUI runs the live watch dashboard until the user quits.
func runTUI() error {
	cfg, _, err := loadConfig()
	if err != nil {
		return err
	}
	if len(cfg.Sync) == 0 {
		fmt.Println("no backups configured; add one with: bk add <repo> <backup-dir>")
		return nil
	}
	m := tuiModel{
		entries: cfg.Sync,
		states:  make([]entryState, len(cfg.Sync)), // stateChecking (zero value)
		syncing: make([]bool, len(cfg.Sync)),
	}
	_, err = tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}

func (m tuiModel) Init() tea.Cmd {
	return tea.Batch(evalCmd(m.entries), tickCmd())
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
		// Re-check everything (picks up plugged-in drives, new commits).
		return m, tea.Batch(evalCmd(m.entries), tickCmd())

	case statesMsg:
		m.states = []entryState(msg)
		var cmds []tea.Cmd
		for i, st := range m.states {
			if st.needsSync() && !m.syncing[i] {
				m.syncing[i] = true
				cmds = append(cmds, syncEntryCmd(i, m.entries[i]))
			}
		}
		return m, tea.Batch(cmds...)

	case syncResultMsg:
		m.syncing[msg.i] = false
		idWasEmpty := m.entries[msg.i].ID == ""
		m.entries[msg.i] = msg.entry
		if msg.err != nil {
			m.states[msg.i] = stateError
		} else {
			m.states[msg.i] = evalEntry(m.entries[msg.i])
		}
		if idWasEmpty && msg.entry.ID != "" {
			// First sync recorded the target's id; persist it.
			_ = saveConfig(&config{Sync: m.entries})
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
	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	for i, e := range m.entries {
		st := stateChecking
		if i < len(m.states) {
			st = m.states[i]
		}
		indicator, label := dot(true, st), st.label()
		if i < len(m.syncing) && m.syncing[i] {
			indicator, label = colorize("36", "⏺"), "syncing…" // cyan
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", indicator, e.Source, e.Target, label)
	}
	_ = tw.Flush()
	return b.String()
}

// evalCmd evaluates every entry's state off the UI goroutine. It works on a
// snapshot so it never races with Update mutating the model's entries.
func evalCmd(entries []syncEntry) tea.Cmd {
	snapshot := append([]syncEntry(nil), entries...)
	return func() tea.Msg {
		states := make([]entryState, len(snapshot))
		for i, e := range snapshot {
			states[i] = evalEntry(e)
		}
		return statesMsg(states)
	}
}

// syncEntryCmd syncs one entry off the UI goroutine, working on a copy so the
// result can be applied back in Update without a data race.
func syncEntryCmd(i int, e syncEntry) tea.Cmd {
	return func() tea.Msg {
		ec := e
		_, err := syncConfigured(&ec)
		return syncResultMsg{i: i, entry: ec, err: err}
	}
}

func tickCmd() tea.Cmd {
	return tea.Tick(tickInterval, func(t time.Time) tea.Msg { return tickMsg(t) })
}
