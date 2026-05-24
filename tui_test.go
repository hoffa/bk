package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func newModel(statuses ...backupStatus) tuiModel {
	return tuiModel{statuses: statuses, syncing: map[string]bool{}}
}

func status(source, target string, st entryState) backupStatus {
	return backupStatus{syncEntry: syncEntry{Source: source, Target: target}, state: st}
}

func TestModelInit(t *testing.T) {
	if newModel().Init() == nil {
		t.Fatal("Init returned nil cmd")
	}
}

func TestModelRefreshNoAutoSync(t *testing.T) {
	m := newModel()
	updated, _ := m.Update(refreshMsg{status("/a", "/b", stateUnsynced)})
	if len(updated.(tuiModel).syncing) != 0 {
		t.Error("auto-sync off: should not start syncs")
	}
}

func TestModelRefreshAutoSync(t *testing.T) {
	m := newModel()
	m.autoSync = true
	stale := status("/a", "/b", stateUnsynced)
	updated, cmd := m.Update(refreshMsg{stale, status("/c", "/d", stateSynced)})
	mm := updated.(tuiModel)
	if !mm.syncing[entryKey(stale.syncEntry)] {
		t.Error("auto-sync on: stale entry should be syncing")
	}
	if cmd == nil {
		t.Error("expected sync command")
	}
}

func TestModelToggleAutoSync(t *testing.T) {
	stale := status("/a", "/b", stateUnsynced)
	m := newModel(stale)
	keyA := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")}

	updated, cmd := m.Update(keyA)
	mm := updated.(tuiModel)
	if !mm.autoSync {
		t.Fatal("pressing a should enable auto-sync")
	}
	if !mm.syncing[entryKey(stale.syncEntry)] {
		t.Error("enabling auto-sync should start syncing the stale entry")
	}
	if cmd == nil {
		t.Error("expected sync command on enable")
	}

	off, _ := mm.Update(keyA)
	if off.(tuiModel).autoSync {
		t.Error("pressing a again should disable auto-sync")
	}
}

func TestModelSyncResultError(t *testing.T) {
	s := status("/a", "/b", stateUnsynced)
	s.ID = "x"
	m := newModel(s)
	m.syncing[entryKey(s.syncEntry)] = true
	updated, _ := m.Update(syncResultMsg{entry: s.syncEntry, err: errUsage})
	mm := updated.(tuiModel)
	if mm.syncing[entryKey(s.syncEntry)] {
		t.Error("syncing flag should be cleared")
	}
	if mm.statuses[0].state != stateError {
		t.Errorf("state = %q, want error", mm.statuses[0].state.label())
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

	m := newModel(status(repo, target, stateUnsynced))
	m.statuses[0].syncEntry = e
	m.syncing[entryKey(e)] = true
	updated, _ := m.Update(syncResultMsg{entry: e, err: nil})
	if updated.(tuiModel).statuses[0].state != stateSynced {
		t.Errorf("state = %q, want synced", updated.(tuiModel).statuses[0].state.label())
	}
}

func TestModelQuit(t *testing.T) {
	keys := []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune("q")},
		{Type: tea.KeyCtrlC},
		{Type: tea.KeyEsc},
	}
	for _, k := range keys {
		updated, cmd := newModel(status("/a", "/b", stateSynced)).Update(k)
		if !updated.(tuiModel).quitting {
			t.Errorf("key %q: expected quitting", k.String())
		}
		if cmd == nil || cmd() != tea.QuitMsg(struct{}{}) {
			t.Errorf("key %q: expected tea.Quit command", k.String())
		}
	}
}

func TestModelView(t *testing.T) {
	m := newModel(status("/src", "/dst", stateSynced))
	v := m.View()
	for _, want := range []string{"/src", "/dst", "synced", "⏺", "auto-sync", "q: quit"} {
		if !strings.Contains(v, want) {
			t.Errorf("view missing %q:\n%s", want, v)
		}
	}

	m.syncing[entryKey(m.statuses[0].syncEntry)] = true
	if !strings.Contains(m.View(), "syncing") {
		t.Errorf("view should show syncing:\n%s", m.View())
	}

	if !strings.Contains(newModel().View(), "no backups configured") {
		t.Error("empty view should show add hint")
	}

	m.quitting = true
	if m.View() != "" {
		t.Errorf("quitting view should be empty, got %q", m.View())
	}
}

func TestRelTime(t *testing.T) {
	cases := []struct {
		t    time.Time
		want string
	}{
		{time.Time{}, "never"},
		{time.Now().Add(-30 * time.Second), "just now"},
		{time.Now().Add(-5 * time.Minute), "5m ago"},
		{time.Now().Add(-3 * time.Hour), "3h ago"},
		{time.Now().Add(-50 * time.Hour), "2d ago"},
	}
	for _, c := range cases {
		if got := relTime(c.t); got != c.want {
			t.Errorf("relTime = %q, want %q", got, c.want)
		}
	}
}

func TestRefreshCmd(t *testing.T) {
	useTempConfig(t)
	repo := initRepo(t)
	target := filepath.Join(t.TempDir(), "backup")
	if err := addCmd([]string{repo, target}); err != nil {
		t.Fatal(err)
	}
	statuses, ok := refreshCmd()().(refreshMsg)
	if !ok {
		t.Fatal("refreshCmd did not return refreshMsg")
	}
	if len(statuses) != 1 || statuses[0].state != stateUnsynced {
		t.Fatalf("statuses = %v, want one never-synced", statuses)
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
