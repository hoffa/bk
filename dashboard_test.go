package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestStatusCode(t *testing.T) {
	cases := []struct {
		s       entryState
		present bool
		want    string
	}{
		{stateSynced, true, "OK"},
		{stateSynced, false, "OK?"},
		{stateStale, true, "STALE"},
		{stateStale, false, "STALE?"}, // "?" = unverified (offline)
		{stateUnsynced, true, "NEW"},
		{stateError, true, "ERR"},
	}
	for _, c := range cases {
		if got := statusCode(c.s, c.present); got != c.want {
			t.Errorf("statusCode(%s, present=%v) = %q, want %q", c.s.label(), c.present, got, c.want)
		}
	}
}

func TestBadge(t *testing.T) {
	// Plain badge: ASCII only, fixed 7-column width, no escapes.
	for _, s := range []entryState{stateSynced, stateStale, stateUnsynced, stateError} {
		plain := badge(false, s, true)
		if len(plain) != 8 {
			t.Errorf("plain badge for %s = %q (width %d, want 8)", s.label(), plain, len(plain))
		}
		if strings.Contains(plain, "\033[") {
			t.Errorf("plain badge should have no color: %q", plain)
		}
	}
	if !strings.Contains(badge(false, stateSynced, true), "OK") {
		t.Error("badge missing code text")
	}
	// Colored badge wraps the code in an ANSI background.
	if c := badge(true, stateError, true); !strings.Contains(c, "ERR") || !strings.Contains(c, "\033[") {
		t.Errorf("colored badge = %q", c)
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

	// absent target with an id but no refs cache reads as out of date (offline).
	st := evalStatus(syncEntry{Source: repo, Target: filepath.Join(t.TempDir(), "gone"), ID: "x"})
	if st.present {
		t.Error("missing target should not be present")
	}
	if st.state != stateStale {
		t.Errorf("absent uncached state = %q, want out of date", st.state.label())
	}

	// absent target whose cached refs match the source reads as synced (offline).
	rh, err := repoRefsHash(repo)
	if err != nil {
		t.Fatal(err)
	}
	st = evalStatus(syncEntry{Source: repo, Target: filepath.Join(t.TempDir(), "gone"), ID: "x", RefsHash: rh})
	if st.present || st.state != stateSynced {
		t.Errorf("absent cached-current state = %q present=%v, want synced offline", st.state.label(), st.present)
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
