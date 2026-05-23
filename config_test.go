package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// useTempConfig points the config at a temp file for the duration of the test.
func useTempConfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("BK_CONFIG", path)
	return path
}

func TestConfigRoundTrip(t *testing.T) {
	useTempConfig(t)

	// Missing config loads as empty.
	cfg, _, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Sync) != 0 {
		t.Fatalf("expected empty config, got %d entries", len(cfg.Sync))
	}

	cfg.Sync = append(cfg.Sync, syncEntry{Source: "/a", Target: "/b", ID: "deadbeef"})
	if err := saveConfig(cfg); err != nil {
		t.Fatal(err)
	}

	got, _, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Sync) != 1 || got.Sync[0] != cfg.Sync[0] {
		t.Fatalf("round trip mismatch: %+v", got.Sync)
	}
}

func TestAddAndSyncAll(t *testing.T) {
	useTempConfig(t)
	repo := initRepo(t)
	target := filepath.Join(t.TempDir(), "backup")

	if err := addCmd([]string{repo, target}); err != nil {
		t.Fatalf("add: %v", err)
	}

	// add is pure config: it doesn't touch the target, and the id is empty.
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("add should not create the target, stat err = %v", err)
	}
	cfg, _, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Sync) != 1 || cfg.Sync[0].ID != "" {
		t.Fatalf("expected one entry with empty id, got %+v", cfg.Sync)
	}

	// Adding the same pair again is rejected.
	if err := addCmd([]string{repo, target}); err == nil {
		t.Fatal("expected duplicate add to fail")
	}

	// First sync initializes the target, backs up, and records the id.
	if err := syncAll(); err != nil {
		t.Fatalf("syncAll: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, latestFile)); err != nil {
		t.Fatalf("first sync did not create %s: %v", latestFile, err)
	}
	bundles, _ := filepath.Glob(filepath.Join(target, versionsDir, "*.bundle"))
	if len(bundles) != 1 {
		t.Fatalf("after first sync: got %d bundles, want 1", len(bundles))
	}
	cfg, _, err = loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Sync[0].ID == "" {
		t.Fatal("first sync did not record the target id")
	}

	// A second sync after a new commit appends another version.
	mustRun(t, repo, "git", "commit", "--allow-empty", "-qm", "second")
	if err := syncAll(); err != nil {
		t.Fatalf("syncAll 2: %v", err)
	}
	bundles, _ = filepath.Glob(filepath.Join(target, versionsDir, "*.bundle"))
	if len(bundles) != 2 {
		t.Fatalf("after second sync: got %d bundles, want 2", len(bundles))
	}
}

func TestSyncSkipsWhenUnchanged(t *testing.T) {
	useTempConfig(t)
	repo := initRepo(t)
	target := filepath.Join(t.TempDir(), "backup")
	if err := addCmd([]string{repo, target}); err != nil {
		t.Fatal(err)
	}

	if err := syncAll(); err != nil {
		t.Fatal(err)
	}
	// No repo changes -> second sync is a no-op, no new version.
	if err := syncAll(); err != nil {
		t.Fatal(err)
	}
	bundles, _ := filepath.Glob(filepath.Join(target, versionsDir, "*.bundle"))
	if len(bundles) != 1 {
		t.Fatalf("unchanged repo should not add a version, got %d", len(bundles))
	}
}

func TestSyncAllFirstSyncParentAbsent(t *testing.T) {
	useTempConfig(t)
	// Parent dir does not exist -> treated as absent (e.g. drive not mounted),
	// not an error, and nothing is created.
	cfg := &config{Sync: []syncEntry{{
		Source: initRepo(t),
		Target: filepath.Join(t.TempDir(), "nope", "backup"),
	}}}
	if err := saveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	if err := syncAll(); err != nil {
		t.Fatalf("absent parent should be skipped, got %v", err)
	}
}

func TestSyncAllEmpty(t *testing.T) {
	useTempConfig(t)
	if err := syncAll(); err == nil || !strings.Contains(err.Error(), "no sync entries") {
		t.Fatalf("want no-entries error, got %v", err)
	}
}

func TestSyncAllSkipsAbsentTarget(t *testing.T) {
	useTempConfig(t)
	cfg := &config{Sync: []syncEntry{{
		Source: initRepo(t),
		Target: filepath.Join(t.TempDir(), "missing"), // never created
		ID:     "whatever",
	}}}
	if err := saveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	// Absent target is skipped, not an error.
	if err := syncAll(); err != nil {
		t.Fatalf("absent target should be skipped, got %v", err)
	}
}

func TestSyncAllIDMismatch(t *testing.T) {
	useTempConfig(t)
	repo := initRepo(t)
	target := filepath.Join(t.TempDir(), "backup")
	if err := initBackup(target); err != nil {
		t.Fatal(err)
	}
	cfg := &config{Sync: []syncEntry{{Source: repo, Target: target, ID: "not-the-real-id"}}}
	if err := saveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	// Wrong id is a hard failure.
	if err := syncAll(); err == nil || !strings.Contains(err.Error(), "failed") {
		t.Fatalf("want failure on id mismatch, got %v", err)
	}
	// And nothing was written.
	bundles, _ := filepath.Glob(filepath.Join(target, versionsDir, "*.bundle"))
	if len(bundles) != 0 {
		t.Fatalf("expected no bundles on mismatch, got %d", len(bundles))
	}
}

func TestSyncAllNotABackupTarget(t *testing.T) {
	useTempConfig(t)
	target := t.TempDir() // exists but has no BK_BACKUP.json
	cfg := &config{Sync: []syncEntry{{Source: initRepo(t), Target: target, ID: "x"}}}
	if err := saveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	if err := syncAll(); err == nil {
		t.Fatal("expected failure for non-backup target")
	}
}

func TestConfigPathDefaults(t *testing.T) {
	t.Setenv("BK_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg")
	got, err := configPath()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join("/tmp/xdg", "bk", "config.json"); got != want {
		t.Fatalf("configPath = %q, want %q", got, want)
	}

	t.Setenv("BK_CONFIG", "/explicit/path.json")
	if got, _ := configPath(); got != "/explicit/path.json" {
		t.Fatalf("BK_CONFIG override = %q", got)
	}
}
