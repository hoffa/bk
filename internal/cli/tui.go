package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/hoffa/bk/internal/bk"
)

// tickInterval is how often the TUI reloads the config and re-checks backups.
const tickInterval = 2 * time.Second

// entryKey identifies a configured entry stably across config reloads, so
// syncing state survives entries being added or removed between ticks.
func entryKey(e bk.Entry) string {
	return e.Source + "\x00" + e.Target
}

// tuiModel is the live status dashboard. By default it only shows status; auto
// sync is opt-in via a key. It is a thin view over the core: each tick it
// reloads the config and re-evaluates via bk.Eval, and (when auto sync is on)
// syncs out-of-date entries via bk.Sync. All work happens off the UI goroutine
// via commands.
type tuiModel struct {
	// ctx is cancelled when the user quits, so in-flight git commands launched
	// as commands stop instead of running on after the UI is gone.
	ctx           context.Context
	cancel        context.CancelFunc
	statuses      []bk.Status
	syncing       map[string]bool
	autoSync      bool
	quitting      bool
	width, height int
}

type (
	tickMsg       time.Time
	refreshMsg    []bk.Status
	syncResultMsg struct {
		entry bk.Entry
		err   error
	}
)

// runTUI runs the live status dashboard until the user quits. The context is
// cancelled on quit so background syncs don't outlive the UI.
func runTUI(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	m := tuiModel{ctx: ctx, cancel: cancel, syncing: map[string]bool{}}
	_, err := tea.NewProgram(m).Run() // alt screen is set on the View

	return err
}

func (m tuiModel) Init() tea.Cmd {
	return tea.Batch(refreshCmd(m.ctx), tickCmd())
}

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			m.quitting = true
			m.cancel() // stop any in-flight syncs

			return m, tea.Quit
		case "a":
			m.autoSync = !m.autoSync
			if m.autoSync {
				return m, m.startSyncs()
			}

			return m, nil
		}

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case tickMsg:
		// Reload config + re-check (picks up entries added elsewhere, plugged-in
		// drives, and new commits).
		return m, tea.Batch(refreshCmd(m.ctx), tickCmd())

	case refreshMsg:
		m.statuses = []bk.Status(msg)
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
		// Re-evaluate so the row reflects reality (ErrTargetAbsent, etc.) rather
		// than forcing an error color on a transient failure.
		for i := range m.statuses {
			if entryKey(m.statuses[i].Entry) == k {
				m.statuses[i] = bk.Eval(m.ctx, msg.entry)
				break
			}
		}

		return m, nil
	}

	return m, nil
}

func (m tuiModel) View() tea.View {
	if m.quitting {
		return tea.NewView("")
	}

	var b strings.Builder

	lines := 0

	if len(m.statuses) == 0 {
		b.WriteString("no backups configured; add one with: bk add <repo> <backup-dir>\n")

		lines++
	} else {
		// Manual column widths from visible text: colored badges contain ANSI
		// escapes, so tabwriter can't measure them. Badges are a fixed visible
		// width, so the rest aligns when padded by source/target length.
		srcW, tgtW := 0, 0
		for _, s := range m.statuses {
			srcW = max(srcW, len(s.Source))
			tgtW = max(tgtW, len(s.Target))
		}

		for _, s := range m.statuses {
			bg := badge(s.State, s.Present)
			if m.syncing[entryKey(s.Entry)] {
				bg = styleBadge(lipgloss.Cyan, "SYNC")
			}

			_, _ = fmt.Fprintf(&b, "%s  %-*s  %-*s  %s\n", bg, srcW, s.Source, tgtW, s.Target, relTime(s.LastSync))
			lines++
		}
	}

	b.WriteString(m.statusBar(lines))

	v := tea.NewView(b.String())
	v.AltScreen = true // full-window mode

	return v
}

// startSyncs kicks off syncs for every out-of-date entry not already syncing.
func (m tuiModel) startSyncs() tea.Cmd {
	var cmds []tea.Cmd

	for _, s := range m.statuses {
		k := entryKey(s.Entry)
		// Only sync targets that are actually connected.
		if s.Present && s.State.NeedsSync() && !m.syncing[k] {
			m.syncing[k] = true

			cmds = append(cmds, syncEntryCmd(m.ctx, s.Entry))
		}
	}

	return tea.Batch(cmds...)
}

// statusBar renders the bottom row: auto-sync state flush-left, help
// flush-right, padded to the window width and pinned to the last terminal row.
func (m tuiModel) statusBar(bodyLines int) string {
	mode := "off"
	if m.autoSync {
		mode = "on"
	}

	left := "auto-sync: " + mode
	right := "a: toggle auto-sync  q: quit"

	gap := max(m.width-len(left)-len(right), 2)

	bar := left + strings.Repeat(" ", gap) + right

	// Push the bar to the bottom of the screen when the height is known.
	blanks := max(1, m.height-bodyLines-1)

	return strings.Repeat("\n", blanks) + bar
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
func refreshCmd(ctx context.Context) tea.Cmd {
	return func() tea.Msg {
		statuses, err := evalAll(ctx)
		if err != nil {
			return refreshMsg(nil)
		}

		return refreshMsg(statuses)
	}
}

// syncEntryCmd syncs one entry off the UI goroutine, working on a copy so the
// result can be applied back in Update without a data race.
func syncEntryCmd(ctx context.Context, e bk.Entry) tea.Cmd {
	return func() tea.Msg {
		ec := e
		_, err := bk.Sync(ctx, &ec)

		return syncResultMsg{entry: ec, err: err}
	}
}

func tickCmd() tea.Cmd {
	return tea.Tick(tickInterval, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// persistEntry writes a synced entry's id and refs hash back to the config,
// merging into the current on-disk config so a concurrent `bk add` isn't
// clobbered.
func persistEntry(e bk.Entry) {
	cfg, err := bk.Load()
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
		_ = cfg.Save()
	}
}
