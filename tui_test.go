package main

import (
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func newModel(entries ...syncEntry) tuiModel {
	return tuiModel{
		entries: entries,
		states:  map[string]entryState{},
		syncing: map[string]bool{},
	}
}

func TestModelInit(t *testing.T) {
	if newModel().Init() == nil {
		t.Fatal("Init returned nil cmd")
	}
}

func TestModelRefreshStartsSync(t *testing.T) {
	a := syncEntry{Source: "/a", Target: "/b"}
	c := syncEntry{Source: "/c", Target: "/d"}
	m := newModel()
	updated, cmd := m.Update(refreshMsg{
		entries: []syncEntry{a, c},
		states: map[string]entryState{
			entryKey(a): stateUnsynced,
			entryKey(c): stateSynced,
		},
	})
	mm := updated.(tuiModel)
	if !mm.syncing[entryKey(a)] {
		t.Error("expected stale entry to be marked syncing")
	}
	if mm.syncing[entryKey(c)] {
		t.Error("synced entry should not be syncing")
	}
	if cmd == nil {
		t.Error("expected a sync command")
	}
}

func TestModelSyncResultError(t *testing.T) {
	e := syncEntry{Source: "/a", Target: "/b", ID: "x"}
	m := newModel(e)
	m.syncing[entryKey(e)] = true
	updated, _ := m.Update(syncResultMsg{entry: e, err: errUsage})
	mm := updated.(tuiModel)
	if mm.syncing[entryKey(e)] {
		t.Error("syncing flag should be cleared")
	}
	if mm.states[entryKey(e)] != stateError {
		t.Errorf("state = %q, want error", mm.states[entryKey(e)].label())
	}
}

func TestModelSyncResultSynced(t *testing.T) {
	useTempConfig(t)
	repo := initRepo(t)
	target := filepath.Join(t.TempDir(), "backup")

	e := syncEntry{Source: repo, Target: target}
	if _, err := syncConfigured(&e); err != nil { // create a real synced backup
		t.Fatal(err)
	}

	m := newModel(e)
	m.syncing[entryKey(e)] = true
	updated, _ := m.Update(syncResultMsg{entry: e, err: nil})
	if updated.(tuiModel).states[entryKey(e)] != stateSynced {
		t.Errorf("state = %q, want synced", updated.(tuiModel).states[entryKey(e)].label())
	}
}

func TestModelQuit(t *testing.T) {
	keys := []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune("q")},
		{Type: tea.KeyCtrlC},
		{Type: tea.KeyEsc},
	}
	for _, k := range keys {
		updated, cmd := newModel(syncEntry{Source: "/a", Target: "/b"}).Update(k)
		if !updated.(tuiModel).quitting {
			t.Errorf("key %q: expected quitting", k.String())
		}
		if cmd == nil || cmd() != tea.QuitMsg(struct{}{}) {
			t.Errorf("key %q: expected tea.Quit command", k.String())
		}
	}
}

func TestModelView(t *testing.T) {
	e := syncEntry{Source: "/src", Target: "/dst"}
	m := newModel(e)
	m.states[entryKey(e)] = stateSynced

	v := m.View()
	for _, want := range []string{"watching", "/src", "/dst", "synced", "⏺"} {
		if !strings.Contains(v, want) {
			t.Errorf("view missing %q:\n%s", want, v)
		}
	}

	m.syncing[entryKey(e)] = true
	if !strings.Contains(m.View(), "syncing") {
		t.Errorf("view should show syncing:\n%s", m.View())
	}

	// Empty model shows the add hint.
	if !strings.Contains(newModel().View(), "no backups configured") {
		t.Error("empty view should show add hint")
	}

	m.quitting = true
	if m.View() != "" {
		t.Errorf("quitting view should be empty, got %q", m.View())
	}
}

func TestRefreshCmd(t *testing.T) {
	useTempConfig(t)
	repo := initRepo(t)
	target := filepath.Join(t.TempDir(), "backup")
	if err := addCmd([]string{repo, target}); err != nil {
		t.Fatal(err)
	}

	msg, ok := refreshCmd()().(refreshMsg)
	if !ok {
		t.Fatal("refreshCmd did not return refreshMsg")
	}
	if len(msg.entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(msg.entries))
	}
	if msg.states[entryKey(msg.entries[0])] != stateUnsynced {
		t.Errorf("state = %q, want never synced", msg.states[entryKey(msg.entries[0])].label())
	}
}

func TestSyncEntryCmd(t *testing.T) {
	repo := initRepo(t)
	target := filepath.Join(t.TempDir(), "backup")
	r, ok := syncEntryCmd(syncEntry{Source: repo, Target: target})().(syncResultMsg)
	if !ok {
		t.Fatal("syncEntryCmd did not return syncResultMsg")
	}
	if r.err != nil {
		t.Fatalf("sync error: %v", r.err)
	}
	if r.entry.ID == "" {
		t.Error("first sync should record an id")
	}
}

func TestPersistID(t *testing.T) {
	useTempConfig(t)
	if err := saveConfig(&config{Sync: []syncEntry{{Source: "/a", Target: "/b"}}}); err != nil {
		t.Fatal(err)
	}
	persistID(syncEntry{Source: "/a", Target: "/b", ID: "newid"})
	cfg, _, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Sync[0].ID != "newid" {
		t.Errorf("config id = %q, want newid", cfg.Sync[0].ID)
	}
}
