package main

import (
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestModelInit(t *testing.T) {
	m := tuiModel{entries: []syncEntry{{Source: "/a", Target: "/b"}}, states: make([]entryState, 1), syncing: make([]bool, 1)}
	if m.Init() == nil {
		t.Fatal("Init returned nil cmd")
	}
}

func TestModelStatesStartsSync(t *testing.T) {
	m := tuiModel{
		entries: []syncEntry{{Source: "/a", Target: "/b"}, {Source: "/c", Target: "/d"}},
		states:  make([]entryState, 2),
		syncing: make([]bool, 2),
	}
	updated, cmd := m.Update(statesMsg{stateUnsynced, stateSynced})
	mm := updated.(tuiModel)
	if !mm.syncing[0] {
		t.Error("expected entry 0 (needs sync) to be marked syncing")
	}
	if mm.syncing[1] {
		t.Error("entry 1 (synced) should not be syncing")
	}
	if cmd == nil {
		t.Error("expected sync command for the stale entry")
	}
}

func TestModelSyncResultError(t *testing.T) {
	m := tuiModel{
		entries: []syncEntry{{Source: "/a", Target: "/b", ID: "x"}},
		states:  []entryState{stateUnsynced},
		syncing: []bool{true},
	}
	updated, _ := m.Update(syncResultMsg{i: 0, entry: m.entries[0], err: errUsage})
	mm := updated.(tuiModel)
	if mm.syncing[0] {
		t.Error("syncing flag should be cleared")
	}
	if mm.states[0] != stateError {
		t.Errorf("state = %q, want error", mm.states[0].label())
	}
}

func TestModelSyncResultSynced(t *testing.T) {
	useTempConfig(t)
	repo := initRepo(t)
	target := filepath.Join(t.TempDir(), "backup")

	// Create a real synced backup so evalEntry resolves to synced.
	e := syncEntry{Source: repo, Target: target}
	if _, err := syncConfigured(&e); err != nil {
		t.Fatal(err)
	}

	m := tuiModel{entries: []syncEntry{e}, states: []entryState{stateUnsynced}, syncing: []bool{true}}
	updated, _ := m.Update(syncResultMsg{i: 0, entry: e, err: nil})
	mm := updated.(tuiModel)
	if mm.states[0] != stateSynced {
		t.Errorf("state = %q, want synced", mm.states[0].label())
	}
}

func TestModelSyncResultPersistsID(t *testing.T) {
	useTempConfig(t)
	if err := saveConfig(&config{Sync: []syncEntry{{Source: "/a", Target: "/b"}}}); err != nil {
		t.Fatal(err)
	}
	m := tuiModel{
		entries: []syncEntry{{Source: "/a", Target: "/b"}}, // id empty
		states:  []entryState{stateUnsynced},
		syncing: []bool{true},
	}
	recorded := syncEntry{Source: "/a", Target: "/b", ID: "newid123"}
	updated, _ := m.Update(syncResultMsg{i: 0, entry: recorded, err: nil})
	if updated.(tuiModel).entries[0].ID != "newid123" {
		t.Error("entry id not updated in model")
	}
	cfg, _, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Sync[0].ID != "newid123" {
		t.Errorf("config id = %q, want newid123 (should persist)", cfg.Sync[0].ID)
	}
}

func TestModelQuit(t *testing.T) {
	keys := []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune("q")},
		{Type: tea.KeyCtrlC},
		{Type: tea.KeyEsc},
	}
	for _, k := range keys {
		m := tuiModel{entries: []syncEntry{{Source: "/a", Target: "/b"}}, states: make([]entryState, 1), syncing: make([]bool, 1)}
		updated, cmd := m.Update(k)
		if !updated.(tuiModel).quitting {
			t.Errorf("key %q: expected quitting", k.String())
		}
		if cmd == nil || cmd() != tea.QuitMsg(struct{}{}) {
			t.Errorf("key %q: expected tea.Quit command", k.String())
		}
	}
}

func TestModelView(t *testing.T) {
	m := tuiModel{
		entries: []syncEntry{{Source: "/src", Target: "/dst"}},
		states:  []entryState{stateSynced},
		syncing: []bool{false},
	}
	v := m.View()
	for _, want := range []string{"watching", "/src", "/dst", "synced", "⏺"} {
		if !strings.Contains(v, want) {
			t.Errorf("view missing %q:\n%s", want, v)
		}
	}

	// Syncing entries show a syncing label.
	m.states[0] = stateUnsynced
	m.syncing[0] = true
	if !strings.Contains(m.View(), "syncing") {
		t.Errorf("view should show syncing:\n%s", m.View())
	}

	// Quitting renders nothing.
	m.quitting = true
	if m.View() != "" {
		t.Errorf("quitting view should be empty, got %q", m.View())
	}
}

func TestEvalCmd(t *testing.T) {
	repo := initRepo(t)
	entries := []syncEntry{{Source: repo, Target: filepath.Join(t.TempDir(), "backup")}}
	msg := evalCmd(entries)()
	states, ok := msg.(statesMsg)
	if !ok {
		t.Fatalf("got %T, want statesMsg", msg)
	}
	if len(states) != 1 || states[0] != stateUnsynced {
		t.Fatalf("states = %v, want [never synced]", states)
	}
}

func TestSyncCmd(t *testing.T) {
	repo := initRepo(t)
	target := filepath.Join(t.TempDir(), "backup")
	msg := syncEntryCmd(0, syncEntry{Source: repo, Target: target})()
	r, ok := msg.(syncResultMsg)
	if !ok {
		t.Fatalf("got %T, want syncResultMsg", msg)
	}
	if r.err != nil {
		t.Fatalf("sync error: %v", r.err)
	}
	if r.entry.ID == "" {
		t.Error("first sync should record an id")
	}
}
