package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestStatusAll(t *testing.T) {
	useTempConfig(t)
	repo := initRepo(t)
	okTarget := filepath.Join(t.TempDir(), "ok")

	// ok: added and synced.
	if err := addCmd([]string{repo, okTarget}); err != nil {
		t.Fatal(err)
	}
	if err := syncAll(); err != nil {
		t.Fatal(err)
	}

	// never synced (id empty) and absent (id set, target missing).
	cfg, _, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	neverTarget := filepath.Join(t.TempDir(), "new")
	absentTarget := filepath.Join(t.TempDir(), "gone", "x")
	cfg.Sync = append(cfg.Sync,
		syncEntry{Source: repo, Target: neverTarget},
		syncEntry{Source: repo, Target: absentTarget, ID: "deadbeef"},
	)
	if err := saveConfig(cfg); err != nil {
		t.Fatal(err)
	}

	statuses, err := statusAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 3 {
		t.Fatalf("got %d statuses, want 3", len(statuses))
	}

	by := map[string]backupStatus{}
	for _, s := range statuses {
		by[s.Target] = s
	}

	ok := by[okTarget]
	if ok.state != stateSynced || !ok.present {
		t.Errorf("ok target state = %q present=%v, want synced+present", ok.state.label(), ok.present)
	}
	if ok.versions != 1 {
		t.Errorf("ok target versions = %d, want 1", ok.versions)
	}
	if ok.lastSync.IsZero() {
		t.Error("ok target lastSync is zero")
	}
	if by[neverTarget].state != stateUnsynced {
		t.Errorf("never target state = %q, want %q", by[neverTarget].state.label(), stateUnsynced.label())
	}
	// id set but target absent and no refs cache -> out of date, offline.
	if absent := by[absentTarget]; absent.present || absent.state != stateStale {
		t.Errorf("absent target state = %q present=%v, want out of date offline", absent.state.label(), absent.present)
	}
}

func TestEvalEntryErrors(t *testing.T) {
	repo := initRepo(t)

	// id set but target has no BK_BACKUP.json.
	notBackup := t.TempDir()
	if s := evalEntry(syncEntry{Source: repo, Target: notBackup, ID: "abc"}); s != stateError {
		t.Errorf("not-a-backup state = %q, want error", s.label())
	}

	// id set, valid backup, but a different id.
	mismatch := filepath.Join(t.TempDir(), "b")
	if err := initBackup(mismatch); err != nil {
		t.Fatal(err)
	}
	if s := evalEntry(syncEntry{Source: repo, Target: mismatch, ID: "not-the-real-id"}); s != stateError {
		t.Errorf("id-mismatch state = %q, want error", s.label())
	}
}

func TestPrintStatus(t *testing.T) {
	var buf bytes.Buffer
	statuses := []backupStatus{
		{syncEntry: syncEntry{Source: "/a", Target: "/b", ID: "0123456789abcdef0123"}, state: stateSynced, present: true, versions: 3},
		{syncEntry: syncEntry{Source: "/c", Target: "/d"}, state: stateUnsynced},
	}
	if err := printStatus(&buf, statuses); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"SOURCE", "TARGET", "0123456789ab", "OK", "NEW", "/a", "/d"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	// Short id is truncated to 12 chars (full id would be 20 here).
	if strings.Contains(out, "0123456789abcdef0123") {
		t.Errorf("expected truncated id, got full:\n%s", out)
	}
}

func TestShort(t *testing.T) {
	if got := short("abcdefgh", 3); got != "abc" {
		t.Errorf("short = %q, want abc", got)
	}
	if got := short("ab", 5); got != "ab" {
		t.Errorf("short = %q, want ab", got)
	}
}
