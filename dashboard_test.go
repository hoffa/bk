package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestDot(t *testing.T) {
	if got := dot(false, stateSynced); got != "⏺" {
		t.Errorf("no-color dot = %q, want plain ⏺", got)
	}
	for _, s := range []entryState{stateSynced, stateStale, stateUnsynced, stateAbsent, stateError} {
		got := dot(true, s)
		if !strings.Contains(got, "⏺") || !strings.Contains(got, "\033[") {
			t.Errorf("colored dot for %s = %q", s.label(), got)
		}
	}
}

func TestEvalEntryStates(t *testing.T) {
	useTempConfig(t)
	repo := initRepo(t)
	target := filepath.Join(t.TempDir(), "backup")

	// never synced: id empty, parent exists.
	if s := evalEntry(syncEntry{Source: repo, Target: target}); s != stateUnsynced {
		t.Errorf("fresh entry state = %q, want never synced", s.label())
	}

	// absent: id empty, parent missing.
	missing := filepath.Join(t.TempDir(), "gone", "backup")
	if s := evalEntry(syncEntry{Source: repo, Target: missing}); s != stateAbsent {
		t.Errorf("missing-parent state = %q, want absent", s.label())
	}

	// synced after a sync, stale after a new commit.
	if err := addCmd([]string{repo, target}); err != nil {
		t.Fatal(err)
	}
	if err := syncAll(); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if s := evalEntry(cfg.Sync[0]); s != stateSynced {
		t.Errorf("after sync state = %q, want synced", s.label())
	}
	mustRun(t, repo, "git", "commit", "--allow-empty", "-qm", "second")
	if s := evalEntry(cfg.Sync[0]); s != stateStale {
		t.Errorf("after commit state = %q, want out of date", s.label())
	}
}

func TestDashboardNonTTYStatus(t *testing.T) {
	useTempConfig(t)
	// A non-terminal writer prints a one-shot status snapshot (no TUI, no sync).
	var buf bytes.Buffer
	if isTerminal(&buf) {
		t.Fatal("bytes.Buffer should not be a terminal")
	}
	if err := dashboard(&buf); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "no backups configured") {
		t.Fatalf("unexpected output:\n%s", buf.String())
	}

	// With an entry, it prints the status table and does not sync.
	repo := initRepo(t)
	target := filepath.Join(t.TempDir(), "backup")
	if err := addCmd([]string{repo, target}); err != nil {
		t.Fatal(err)
	}
	buf.Reset()
	if err := dashboard(&buf); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), target) {
		t.Errorf("status missing target:\n%s", buf.String())
	}
	if _, err := readLatest(target); err == nil {
		t.Error("dashboard should not have synced (no latest.json expected)")
	}
}
