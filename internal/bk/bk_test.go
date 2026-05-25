package bk

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hoffa/bk/internal/git"
)

func TestConfigRoundTrip(t *testing.T) {
	useTempConfig(t)

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}

	if len(cfg.Sync) != 0 {
		t.Fatalf("expected empty config, got %d entries", len(cfg.Sync))
	}

	cfg.Sync = append(cfg.Sync, Entry{Source: "/a", Target: "/b", ID: "deadbeef"})
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}

	if len(got.Sync) != 1 || got.Sync[0] != cfg.Sync[0] {
		t.Fatalf("round trip mismatch: %+v", got.Sync)
	}
}

func TestAdd(t *testing.T) {
	cfg := &Config{}
	if err := cfg.Add("/a", "/b"); err != nil {
		t.Fatal(err)
	}

	if len(cfg.Sync) != 1 || cfg.Sync[0].ID != "" {
		t.Fatalf("expected one entry with empty id, got %+v", cfg.Sync)
	}

	if err := cfg.Add("/a", "/b"); err == nil {
		t.Fatal("expected duplicate add to fail")
	}
}

func TestConfigPathDefaults(t *testing.T) {
	t.Setenv("BK_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg")

	got, err := ConfigPath()
	if err != nil {
		t.Fatal(err)
	}

	if want := filepath.Join("/tmp/xdg", "bk", "config.json"); got != want {
		t.Fatalf("ConfigPath = %q, want %q", got, want)
	}

	t.Setenv("BK_CONFIG", "/explicit/path.json")

	if got, _ := ConfigPath(); got != "/explicit/path.json" {
		t.Fatalf("BK_CONFIG override = %q", got)
	}
}

func TestSyncEntry(t *testing.T) {
	ctx := t.Context()
	repo := initRepo(t)
	target := filepath.Join(t.TempDir(), "backup")

	e := Entry{Source: repo, Target: target}

	synced, err := Sync(ctx, &e)
	if err != nil || !synced {
		t.Fatalf("first sync synced=%v err=%v", synced, err)
	}

	if e.ID == "" {
		t.Fatal("first sync did not record the target id")
	}

	if _, err := os.Stat(filepath.Join(target, latestFile)); err != nil {
		t.Fatalf("first sync did not create %s: %v", latestFile, err)
	}

	// Unchanged repo -> no new version.
	if synced, err := Sync(ctx, &e); err != nil || synced {
		t.Fatalf("unchanged sync synced=%v err=%v", synced, err)
	}

	// New commit -> append a version.
	mustRun(t, repo, "git", "commit", "--allow-empty", "-qm", "second")

	if synced, err := Sync(ctx, &e); err != nil || !synced {
		t.Fatalf("post-commit sync synced=%v err=%v", synced, err)
	}

	bundles, _ := filepath.Glob(filepath.Join(target, versionsDir, "*.bundle"))
	if len(bundles) != 2 {
		t.Fatalf("got %d bundles, want 2", len(bundles))
	}
}

func TestSyncEntryAbsent(t *testing.T) {
	ctx := t.Context()
	repo := initRepo(t)

	// First sync with a missing parent (e.g. drive not mounted) -> absent.
	e := Entry{Source: repo, Target: filepath.Join(t.TempDir(), "nope", "backup")}
	if _, err := Sync(ctx, &e); !errors.Is(err, ErrTargetAbsent) {
		t.Fatalf("want ErrTargetAbsent, got %v", err)
	}

	// id set but target missing -> absent.
	e2 := Entry{Source: repo, Target: filepath.Join(t.TempDir(), "missing"), ID: "x"}
	if _, err := Sync(ctx, &e2); !errors.Is(err, ErrTargetAbsent) {
		t.Fatalf("want ErrTargetAbsent, got %v", err)
	}
}

func TestSyncEntryIDMismatch(t *testing.T) {
	ctx := t.Context()
	repo := initRepo(t)
	target := filepath.Join(t.TempDir(), "backup")

	if err := initBackup(target); err != nil {
		t.Fatal(err)
	}

	e := Entry{Source: repo, Target: target, ID: "not-the-real-id"}
	if _, err := Sync(ctx, &e); err == nil || errors.Is(err, ErrTargetAbsent) {
		t.Fatalf("want id-mismatch failure, got %v", err)
	}

	bundles, _ := filepath.Glob(filepath.Join(target, versionsDir, "*.bundle"))
	if len(bundles) != 0 {
		t.Fatalf("expected no bundles on mismatch, got %d", len(bundles))
	}
}

func TestSyncEntryNotABackup(t *testing.T) {
	ctx := t.Context()
	repo := initRepo(t)
	target := t.TempDir() // exists but has no BK_BACKUP.json

	e := Entry{Source: repo, Target: target, ID: "x"}
	if _, err := Sync(ctx, &e); err == nil {
		t.Fatal("expected failure for non-backup target")
	}
}

func TestSyncBackupRestoreRoundTrip(t *testing.T) {
	ctx := t.Context()
	repo := initRepo(t)
	backup := filepath.Join(t.TempDir(), "backup")

	if _, err := syncBackup(ctx, repo, backup); err != nil {
		t.Fatalf("sync: %v", err)
	}

	meta, err := loadBackupMeta(backup)
	if err != nil {
		t.Fatalf("load meta: %v", err)
	}

	if meta.ID == "" {
		t.Fatal("BK_BACKUP.json has empty id")
	}

	bundles, _ := filepath.Glob(filepath.Join(backup, versionsDir, "*.bundle"))
	if len(bundles) != 1 {
		t.Fatalf("got %d bundles, want 1", len(bundles))
	}

	if _, err := os.Stat(bundles[0] + ".sha256"); err != nil {
		t.Fatalf("missing sidecar: %v", err)
	}

	restore := filepath.Join(t.TempDir(), "restored")
	if err := Restore(ctx, backup, restore); err != nil {
		t.Fatalf("restore: %v", err)
	}

	if log := output(t, restore, "git", "log", "--oneline"); !strings.Contains(log, "first") {
		t.Fatalf("restored repo missing commit, log:\n%s", log)
	}
}

func TestSyncBackupAppendsStableID(t *testing.T) {
	ctx := t.Context()
	repo := initRepo(t)
	backup := filepath.Join(t.TempDir(), "backup")

	if _, err := syncBackup(ctx, repo, backup); err != nil {
		t.Fatalf("sync 1: %v", err)
	}

	first, err := loadBackupMeta(backup)
	if err != nil {
		t.Fatal(err)
	}

	mustRun(t, repo, "git", "commit", "--allow-empty", "-qm", "second")

	if _, err := syncBackup(ctx, repo, backup); err != nil {
		t.Fatalf("sync 2: %v", err)
	}

	second, err := loadBackupMeta(backup)
	if err != nil {
		t.Fatal(err)
	}

	if first.ID != second.ID {
		t.Fatalf("id changed across syncs: %s -> %s", first.ID, second.ID)
	}

	bundles, _ := filepath.Glob(filepath.Join(backup, versionsDir, "*.bundle"))
	if len(bundles) != 2 {
		t.Fatalf("got %d bundles, want 2", len(bundles))
	}

	// latest must restore the newest state (two commits).
	restore := filepath.Join(t.TempDir(), "restored")
	if err := Restore(ctx, backup, restore); err != nil {
		t.Fatalf("restore: %v", err)
	}

	if log := output(t, restore, "git", "log", "--oneline"); !strings.Contains(log, "second") {
		t.Fatalf("latest restore missing newest commit, log:\n%s", log)
	}
}

func TestSyncBackupRefusesNonEmptyTarget(t *testing.T) {
	repo := initRepo(t)
	target := t.TempDir()

	if err := os.WriteFile(filepath.Join(target, "data"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := syncBackup(t.Context(), repo, target); err == nil {
		t.Fatal("expected sync to refuse a non-empty non-backup target")
	}
}

func TestRestoreShaMismatch(t *testing.T) {
	ctx := t.Context()
	repo := initRepo(t)
	backup := filepath.Join(t.TempDir(), "backup")

	if _, err := syncBackup(ctx, repo, backup); err != nil {
		t.Fatal(err)
	}

	latest, err := readLatest(backup)
	if err != nil {
		t.Fatal(err)
	}

	bundle := filepath.Join(backup, latest.Path)
	if err := os.WriteFile(bundle, []byte("corrupt"), 0o644); err != nil {
		t.Fatal(err)
	}

	err = Restore(ctx, backup, filepath.Join(t.TempDir(), "restored"))
	if err == nil || !strings.Contains(err.Error(), "sha256 mismatch") {
		t.Fatalf("want sha256 mismatch error, got %v", err)
	}
}

func TestRestoreExistingTarget(t *testing.T) {
	ctx := t.Context()
	repo := initRepo(t)
	backup := filepath.Join(t.TempDir(), "backup")

	if _, err := syncBackup(ctx, repo, backup); err != nil {
		t.Fatal(err)
	}

	target := t.TempDir() // already exists
	if err := Restore(ctx, backup, target); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("want already-exists error, got %v", err)
	}
}

func TestRestoreNotABackup(t *testing.T) {
	err := Restore(t.Context(), t.TempDir(), filepath.Join(t.TempDir(), "restored"))
	if err == nil || !strings.Contains(err.Error(), "not a backup directory") {
		t.Fatalf("want not-a-backup error, got %v", err)
	}
}

func TestRestoreNoVersions(t *testing.T) {
	backup := filepath.Join(t.TempDir(), "backup")
	if err := initBackup(backup); err != nil {
		t.Fatal(err)
	}

	err := Restore(t.Context(), backup, filepath.Join(t.TempDir(), "restored"))
	if err == nil || !strings.Contains(err.Error(), "latest.json") {
		t.Fatalf("want missing latest.json error, got %v", err)
	}
}

func TestInitBackupRefusesNonEmpty(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "important.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := initBackup(dir); err == nil {
		t.Fatal("expected refusal to initialize a non-empty non-backup dir")
	}

	if _, err := os.Stat(filepath.Join(dir, backupSentinel)); !os.IsNotExist(err) {
		t.Error("sentinel should not have been written")
	}

	if _, err := os.Stat(filepath.Join(dir, versionsDir)); !os.IsNotExist(err) {
		t.Error("versions dir should not have been created")
	}
}

func TestInitBackupEmptyAndAdopt(t *testing.T) {
	dir := t.TempDir()
	if err := initBackup(dir); err != nil {
		t.Fatalf("empty dir should initialize: %v", err)
	}

	if err := initBackup(dir); err != nil {
		t.Fatalf("re-init should adopt: %v", err)
	}
}

func TestReadSidecarSum(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.sha256")
	if err := os.WriteFile(p, []byte("deadbeef  x.bundle\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := readSidecarSum(p)
	if err != nil {
		t.Fatal(err)
	}

	if got != "deadbeef" {
		t.Fatalf("got %q, want deadbeef", got)
	}

	empty := filepath.Join(t.TempDir(), "empty.sha256")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := readSidecarSum(empty); err == nil {
		t.Fatal("expected error for empty sidecar")
	}
}

func TestEvalStates(t *testing.T) {
	ctx := t.Context()
	repo := initRepo(t)
	target := filepath.Join(t.TempDir(), "backup")

	// Never synced (id empty, parent exists).
	if s := Eval(ctx, Entry{Source: repo, Target: target}); s.State != StateUnsynced {
		t.Errorf("fresh entry = %q, want never synced", s.State.Label())
	}

	// Absent target with an id but no refs cache -> out of date (offline).
	st := Eval(ctx, Entry{Source: repo, Target: filepath.Join(t.TempDir(), "gone"), ID: "x"})
	if st.Present || st.State != StateStale {
		t.Errorf("absent uncached = %q present=%v, want stale offline", st.State.Label(), st.Present)
	}

	// Absent target whose cached refs match the source -> synced (offline).
	rh, err := git.RefsHash(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}

	st = Eval(ctx, Entry{Source: repo, Target: filepath.Join(t.TempDir(), "gone"), ID: "x", RefsHash: rh})
	if st.Present || st.State != StateSynced {
		t.Errorf("absent cached-current = %q present=%v, want synced offline", st.State.Label(), st.Present)
	}

	// Synced after a sync, stale after a new commit.
	e := Entry{Source: repo, Target: target}
	if _, err := Sync(ctx, &e); err != nil {
		t.Fatal(err)
	}

	if s := Eval(ctx, e); s.State != StateSynced {
		t.Errorf("after sync = %q, want synced", s.State.Label())
	}

	mustRun(t, repo, "git", "commit", "--allow-empty", "-qm", "second")

	if s := Eval(ctx, e); s.State != StateStale {
		t.Errorf("after commit = %q, want out of date", s.State.Label())
	}
}

func TestEvalErrors(t *testing.T) {
	ctx := t.Context()
	repo := initRepo(t)

	// id set but target has no BK_BACKUP.json.
	if s := Eval(ctx, Entry{Source: repo, Target: t.TempDir(), ID: "abc"}); s.State != StateError {
		t.Errorf("not-a-backup = %q, want error", s.State.Label())
	}

	// id set, valid backup, but a different id.
	mismatch := filepath.Join(t.TempDir(), "b")
	if err := initBackup(mismatch); err != nil {
		t.Fatal(err)
	}

	if s := Eval(ctx, Entry{Source: repo, Target: mismatch, ID: "not-the-real-id"}); s.State != StateError {
		t.Errorf("id-mismatch = %q, want error", s.State.Label())
	}

	// Never-synced entry whose target is a non-empty non-backup dir -> error.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "junk"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if s := Eval(ctx, Entry{Source: "/x", Target: dir}); s.State != StateError {
		t.Errorf("unusable target = %q, want error", s.State.Label())
	}
}
