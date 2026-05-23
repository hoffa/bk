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
	if ok.state != stateOK {
		t.Errorf("ok target state = %q, want %q", ok.state, stateOK)
	}
	if ok.versions != 1 {
		t.Errorf("ok target versions = %d, want 1", ok.versions)
	}
	if ok.lastSync.IsZero() {
		t.Error("ok target lastSync is zero")
	}
	if by[neverTarget].state != stateNeverSynced {
		t.Errorf("never target state = %q, want %q", by[neverTarget].state, stateNeverSynced)
	}
	if by[absentTarget].state != stateAbsent {
		t.Errorf("absent target state = %q, want %q", by[absentTarget].state, stateAbsent)
	}
}

func TestStatusMismatchAndNotBackup(t *testing.T) {
	repo := initRepo(t)

	// id set but target has no BK_BACKUP.json.
	notBackup := t.TempDir()
	if s := entryStatus(syncEntry{Source: repo, Target: notBackup, ID: "abc"}); s.state != stateNotBackup {
		t.Errorf("state = %q, want %q", s.state, stateNotBackup)
	}

	// id set, valid backup, but a different id.
	mismatch := filepath.Join(t.TempDir(), "b")
	if err := initBackup(mismatch); err != nil {
		t.Fatal(err)
	}
	if s := entryStatus(syncEntry{Source: repo, Target: mismatch, ID: "not-the-real-id"}); s.state != stateIDMismatch {
		t.Errorf("state = %q, want %q", s.state, stateIDMismatch)
	}
}

func TestPrintStatus(t *testing.T) {
	var buf bytes.Buffer
	statuses := []backupStatus{
		{syncEntry: syncEntry{Source: "/a", Target: "/b", ID: "0123456789abcdef0123"}, state: stateOK, versions: 3},
		{syncEntry: syncEntry{Source: "/c", Target: "/d"}, state: stateNeverSynced},
	}
	if err := printStatus(&buf, statuses); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"SOURCE", "TARGET", "0123456789ab", stateOK, stateNeverSynced, "/a", "/d"} {
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
