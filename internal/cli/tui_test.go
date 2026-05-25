package cli

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/hoffa/bk/internal/bk"
)

func newModel(statuses ...bk.Status) tuiModel {
	return tuiModel{
		ctx:      context.Background(),
		cancel:   func() {},
		statuses: statuses,
		syncing:  map[string]bool{},
	}
}

func status(source, target string, st bk.State) bk.Status {
	// present by default; auto-sync only acts on connected targets.
	return bk.Status{Entry: bk.Entry{Source: source, Target: target}, State: st, Present: true}
}

func TestModelInit(t *testing.T) {
	if newModel().Init() == nil {
		t.Fatal("Init returned nil cmd")
	}
}

func TestModelRefreshNoAutoSync(t *testing.T) {
	m := newModel()

	updated, _ := m.Update(refreshMsg{status("/a", "/b", bk.StateUnsynced)})
	if len(updated.(tuiModel).syncing) != 0 {
		t.Error("auto-sync off: should not start syncs")
	}
}

func TestModelRefreshAutoSync(t *testing.T) {
	m := newModel()
	m.autoSync = true
	stale := status("/a", "/b", bk.StateUnsynced)
	updated, cmd := m.Update(refreshMsg{stale, status("/c", "/d", bk.StateSynced)})

	mm := updated.(tuiModel)
	if !mm.syncing[entryKey(stale.Entry)] {
		t.Error("auto-sync on: stale entry should be syncing")
	}

	if cmd == nil {
		t.Error("expected sync command")
	}
}

func TestModelToggleAutoSync(t *testing.T) {
	stale := status("/a", "/b", bk.StateUnsynced)
	m := newModel(stale)
	keyA := tea.KeyPressMsg{Code: 'a', Text: "a"}

	updated, cmd := m.Update(keyA)

	mm := updated.(tuiModel)
	if !mm.autoSync {
		t.Fatal("pressing a should enable auto-sync")
	}

	if !mm.syncing[entryKey(stale.Entry)] {
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

func TestModelSyncResultClearsSyncing(t *testing.T) {
	s := status("/a", "/b", bk.StateUnsynced)
	s.ID = "x"
	m := newModel(s)
	m.syncing[entryKey(s.Entry)] = true

	updated, _ := m.Update(syncResultMsg{entry: s.Entry, err: errUsage})
	if updated.(tuiModel).syncing[entryKey(s.Entry)] {
		t.Error("syncing flag should be cleared after a result")
	}
}

func TestModelSyncResultSynced(t *testing.T) {
	useTempConfig(t)
	repo := initRepo(t)
	target := filepath.Join(t.TempDir(), "backup")

	// add sets up the keyring; then create a real synced backup.
	if err := addCmd([]string{repo, target}); err != nil {
		t.Fatal(err)
	}

	cfg, err := bk.Load()
	if err != nil {
		t.Fatal(err)
	}

	e := cfg.Sync[0]
	if _, err := bk.Sync(t.Context(), &e, cfg.Key); err != nil {
		t.Fatal(err)
	}

	m := newModel(status(repo, target, bk.StateUnsynced))
	m.statuses[0].Entry = e
	m.syncing[entryKey(e)] = true

	updated, _ := m.Update(syncResultMsg{entry: e, err: nil})
	if updated.(tuiModel).statuses[0].State != bk.StateSynced {
		t.Errorf("state = %q, want synced", updated.(tuiModel).statuses[0].State.Label())
	}
}

func TestModelQuit(t *testing.T) {
	keys := []tea.KeyPressMsg{
		{Code: 'q', Text: "q"},
		{Code: 'c', Mod: tea.ModCtrl},
		{Code: tea.KeyEscape},
	}
	for _, k := range keys {
		updated, cmd := newModel(status("/a", "/b", bk.StateSynced)).Update(k)
		if !updated.(tuiModel).quitting {
			t.Errorf("key %q: expected quitting", k.String())
		}

		if cmd == nil || cmd() != (tea.QuitMsg{}) {
			t.Errorf("key %q: expected tea.Quit command", k.String())
		}
	}
}

func TestModelView(t *testing.T) {
	m := newModel(status("/src", "/dst", bk.StateSynced))

	v := m.View().Content
	for _, want := range []string{"/src", "/dst", statusDotChar, "auto-sync", "q: quit"} {
		if !strings.Contains(v, want) {
			t.Errorf("view missing %q:\n%s", want, v)
		}
	}

	// Syncing recolors the dot (cyan), so the rendered view changes.
	m.syncing[entryKey(m.statuses[0].Entry)] = true
	if m.View().Content == v {
		t.Error("syncing should change the status dot")
	}

	if !strings.Contains(newModel().View().Content, "no backups configured") {
		t.Error("empty view should show add hint")
	}

	m.quitting = true
	if m.View().Content != "" {
		t.Errorf("quitting view should be empty, got %q", m.View().Content)
	}
}

func TestWithTilde(t *testing.T) {
	const home = "/home/u"

	cases := map[string]string{
		home:                "~",           // the home dir itself
		home + "/code/proj": "~/code/proj", // under home
		"/var/data":         "/var/data",   // elsewhere
		"/home/user2/x":     "/home/user2/x",
	}
	for in, want := range cases {
		if got := withTilde(in, home); got != want {
			t.Errorf("withTilde(%q) = %q, want %q", in, got, want)
		}
	}

	// No home: unchanged.
	if got := withTilde("/a/b", ""); got != "/a/b" {
		t.Errorf("withTilde with empty home = %q, want /a/b", got)
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

	statuses, ok := refreshCmd(t.Context())().(refreshMsg)
	if !ok {
		t.Fatal("refreshCmd did not return refreshMsg")
	}

	if len(statuses) != 1 || statuses[0].State != bk.StateUnsynced {
		t.Fatalf("statuses = %v, want one never-synced", statuses)
	}
}

func TestSyncEntryCmd(t *testing.T) {
	useTempConfig(t)
	repo := initRepo(t)
	target := filepath.Join(t.TempDir(), "backup")

	// add sets up the keyring; syncEntryCmd loads it from the config.
	if err := addCmd([]string{repo, target}); err != nil {
		t.Fatal(err)
	}

	cfg, err := bk.Load()
	if err != nil {
		t.Fatal(err)
	}

	r, ok := syncEntryCmd(t.Context(), cfg.Sync[0])().(syncResultMsg)
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

	cfg := &bk.Config{Sync: []bk.Entry{{Source: "/a", Target: "/b"}}}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	persistEntry(bk.Entry{Source: "/a", Target: "/b", ID: "newid"})

	got, err := bk.Load()
	if err != nil {
		t.Fatal(err)
	}

	if got.Sync[0].ID != "newid" {
		t.Errorf("config id = %q, want newid", got.Sync[0].ID)
	}
}
