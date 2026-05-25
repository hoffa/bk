package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/hoffa/bk/internal/bk"
)

// tickInterval is how often the TUI reloads the config and re-checks backups.
const tickInterval = 2 * time.Second

// muted dims secondary text (separators, timestamps, the help bar) so the dots
// and paths stand out.
var muted = lipgloss.NewStyle().Foreground(lipgloss.BrightBlack)

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
		case "s":
			// One-shot: sync every out-of-date connected entry now, without
			// turning on continuous auto-sync.
			return m, m.startSyncs()
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
		b.WriteString(muted.Render("no backups configured; add one with: bk add <repo> <backup-dir>") + "\n")

		lines++
	} else {
		home, _ := os.UserHomeDir()

		// Manual column widths from visible text: the colored dot contains ANSI
		// escapes (and is a single cell), so we pad source/target by length.
		srcW, tgtW := 0, 0
		for _, s := range m.statuses {
			srcW = max(srcW, len(withTilde(s.Source, home)))
			tgtW = max(tgtW, len(withTilde(s.Target, home)))
		}

		for _, s := range m.statuses {
			d := statusDot(s.State, s.Present)
			if m.syncing[entryKey(s.Entry)] {
				d = dot(lipgloss.Cyan, false)
			}

			_, _ = fmt.Fprintf(&b, "%s %-*s %s %-*s  %s\n",
				d, srcW, withTilde(s.Source, home), muted.Render("→"),
				tgtW, withTilde(s.Target, home), muted.Render(relTime(s.LastSync)))
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
	mode := muted.Render("off")
	if m.autoSync {
		mode = lipgloss.NewStyle().Foreground(lipgloss.Green).Render("on")
	}

	left := muted.Render("auto-sync: ") + mode
	right := muted.Render("s: sync  a: toggle auto-sync  q: quit")

	// Measure visible width (Width ignores the color escapes) to size the gap so
	// the help stays flush-right.
	gap := max(m.width-lipgloss.Width(left)-lipgloss.Width(right), 2)

	bar := left + strings.Repeat(" ", gap) + right

	// Push the bar to the bottom of the screen when the height is known.
	blanks := max(1, m.height-bodyLines-1)

	return strings.Repeat("\n", blanks) + bar
}

// withTilde abbreviates a home-relative path with "~" for display. Config paths
// are absolute, so this is purely cosmetic and reversible.
func withTilde(p, home string) string {
	if home == "" {
		return p
	}

	if p == home {
		return "~"
	}

	sep := string(os.PathSeparator)
	if rest, ok := strings.CutPrefix(p, home+sep); ok {
		return "~" + sep + rest
	}

	return p
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
// result can be applied back in Update without a data race. Encryption needs only
// the public keyring (loaded from the config), so no password is ever required.
func syncEntryCmd(ctx context.Context, e bk.Entry) tea.Cmd {
	return func() tea.Msg {
		ec := e

		cfg, err := bk.Load()
		if err != nil {
			return syncResultMsg{entry: ec, err: err}
		}

		_, err = bk.Sync(ctx, &ec, cfg.Key)

		return syncResultMsg{entry: ec, err: err}
	}
}

func tickCmd() tea.Cmd {
	return tea.Tick(tickInterval, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// persistEntry writes a freshly-synced entry's learned Backup cache back to the
// config, matched by the stable entry id, so a concurrent `bk add` (a different
// entry) isn't clobbered.
func persistEntry(e bk.Entry) {
	if e.Backup == nil {
		return
	}

	cfg, err := bk.Load()
	if err != nil {
		return
	}

	changed := false

	for i := range cfg.Sync {
		if cfg.Sync[i].ID == e.ID {
			cfg.Sync[i].Backup = e.Backup
			changed = true
		}
	}

	if changed {
		_ = cfg.Save()
	}
}
