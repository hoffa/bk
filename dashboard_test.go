package main

import (
	"bytes"
	"os"
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

func TestUseColor(t *testing.T) {
	var buf bytes.Buffer
	if useColor(&buf) {
		t.Error("non-file writer should disable color")
	}
	t.Setenv("NO_COLOR", "1")
	if useColor(os.Stdout) {
		t.Error("NO_COLOR should disable color")
	}
}

func TestRunDashboardSyncError(t *testing.T) {
	useTempConfig(t)
	// Source is not a git repo but the target parent exists, so the entry is
	// never-synced (needs sync); the sync then fails.
	cfg := &config{Sync: []syncEntry{{
		Source: t.TempDir(),
		Target: filepath.Join(t.TempDir(), "backup"),
	}}}
	if err := saveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := runDashboard(&buf); err == nil {
		t.Fatalf("expected error from failed sync:\n%s", buf.String())
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

	// synced after a sync.
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

	// stale after a new commit.
	mustRun(t, repo, "git", "commit", "--allow-empty", "-qm", "second")
	if s := evalEntry(cfg.Sync[0]); s != stateStale {
		t.Errorf("after commit state = %q, want out of date", s.label())
	}
}

func TestRunDashboardEmpty(t *testing.T) {
	useTempConfig(t)
	var buf bytes.Buffer
	if err := runDashboard(&buf); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "no backups configured") {
		t.Fatalf("missing empty-state hint:\n%s", buf.String())
	}
}

func TestRunDashboardSyncs(t *testing.T) {
	useTempConfig(t)
	repo := initRepo(t)
	target := filepath.Join(t.TempDir(), "backup")
	if err := addCmd([]string{repo, target}); err != nil {
		t.Fatal(err)
	}

	// First run: entry is never-synced, so the dashboard syncs it.
	var buf bytes.Buffer
	if err := runDashboard(&buf); err != nil {
		t.Fatalf("dashboard: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, target) {
		t.Errorf("dashboard missing target %q:\n%s", target, out)
	}
	if !strings.Contains(out, "syncing") {
		t.Errorf("expected a sync to run:\n%s", out)
	}
	if evalEntry(mustEntry(t, 0)) != stateSynced {
		t.Error("entry should be synced after dashboard run")
	}

	// Second run: already synced, no sync performed.
	buf.Reset()
	if err := runDashboard(&buf); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "syncing") {
		t.Errorf("unchanged repo should not sync:\n%s", buf.String())
	}
}

// mustEntry returns the i-th configured entry.
func mustEntry(t *testing.T, i int) syncEntry {
	t.Helper()
	cfg, _, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	return cfg.Sync[i]
}
