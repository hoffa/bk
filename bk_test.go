package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// mustRun executes a command in dir and fails the test on error.
func mustRun(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
}

// output executes a command in dir and returns its combined output.
func output(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
	return string(out)
}

// initRepo creates a git repo with one commit and returns its path.
func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	mustRun(t, dir, "git", "init", "-q")
	mustRun(t, dir, "git", "config", "user.email", "t@example.com")
	mustRun(t, dir, "git", "config", "user.name", "tester")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello\n"), 0644); err != nil {
		t.Fatal(err)
	}
	mustRun(t, dir, "git", "add", ".")
	mustRun(t, dir, "git", "commit", "-qm", "first")
	return dir
}

func TestSyncRestoreRoundTrip(t *testing.T) {
	repo := initRepo(t)
	backup := filepath.Join(t.TempDir(), "backup")

	if err := syncBackup(repo, backup); err != nil {
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
	if err := restoreBackup(backup, restore); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if log := output(t, restore, "git", "log", "--oneline"); !strings.Contains(log, "first") {
		t.Fatalf("restored repo missing commit, log:\n%s", log)
	}
}

func TestSyncAppendsVersionsStableID(t *testing.T) {
	repo := initRepo(t)
	backup := filepath.Join(t.TempDir(), "backup")

	if err := syncBackup(repo, backup); err != nil {
		t.Fatalf("sync 1: %v", err)
	}
	first, err := loadBackupMeta(backup)
	if err != nil {
		t.Fatal(err)
	}

	mustRun(t, repo, "git", "commit", "--allow-empty", "-qm", "second")
	if err := syncBackup(repo, backup); err != nil {
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
	if err := restoreBackup(backup, restore); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if log := output(t, restore, "git", "log", "--oneline"); !strings.Contains(log, "second") {
		t.Fatalf("latest restore missing newest commit, log:\n%s", log)
	}
}

func TestRestoreShaMismatch(t *testing.T) {
	repo := initRepo(t)
	backup := filepath.Join(t.TempDir(), "backup")
	if err := syncBackup(repo, backup); err != nil {
		t.Fatal(err)
	}

	rel, err := os.ReadFile(filepath.Join(backup, latestFile))
	if err != nil {
		t.Fatal(err)
	}
	bundle := filepath.Join(backup, strings.TrimSpace(string(rel)))
	if err := os.WriteFile(bundle, []byte("corrupt"), 0644); err != nil {
		t.Fatal(err)
	}

	err = restoreBackup(backup, filepath.Join(t.TempDir(), "restored"))
	if err == nil || !strings.Contains(err.Error(), "sha256 mismatch") {
		t.Fatalf("want sha256 mismatch error, got %v", err)
	}
}

func TestRestoreExistingTarget(t *testing.T) {
	repo := initRepo(t)
	backup := filepath.Join(t.TempDir(), "backup")
	if err := syncBackup(repo, backup); err != nil {
		t.Fatal(err)
	}

	target := t.TempDir() // already exists
	err := restoreBackup(backup, target)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("want already-exists error, got %v", err)
	}
}

func TestRestoreNotABackup(t *testing.T) {
	err := restoreBackup(t.TempDir(), filepath.Join(t.TempDir(), "restored"))
	if err == nil || !strings.Contains(err.Error(), "not a backup directory") {
		t.Fatalf("want not-a-backup error, got %v", err)
	}
}

func TestRestoreNoVersions(t *testing.T) {
	backup := filepath.Join(t.TempDir(), "backup")
	if err := initBackup(backup); err != nil {
		t.Fatal(err)
	}
	// No latest.txt written yet.
	err := restoreBackup(backup, filepath.Join(t.TempDir(), "restored"))
	if err == nil || !strings.Contains(err.Error(), "latest.txt") {
		t.Fatalf("want missing latest.txt error, got %v", err)
	}
}

func TestVerifyBundleInvalid(t *testing.T) {
	bad := filepath.Join(t.TempDir(), "not.bundle")
	if err := os.WriteFile(bad, []byte("nope"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := verifyBundle(bad); err == nil {
		t.Fatal("expected verify to fail on non-bundle file")
	}
}

func TestCreateBundleInvalidRepo(t *testing.T) {
	out := filepath.Join(t.TempDir(), "x.bundle")
	if err := createBundle(t.TempDir(), out); err == nil {
		t.Fatal("expected createBundle to fail outside a repo")
	}
}

func TestSha256File(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(p, []byte("abc"), 0644); err != nil {
		t.Fatal(err)
	}
	// echo -n abc | sha256sum
	const want = "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	got, err := sha256File(p)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("sha256 = %s, want %s", got, want)
	}

	if _, err := sha256File(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestReadSidecarSum(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.sha256")
	if err := os.WriteFile(p, []byte("deadbeef  x.bundle\n"), 0644); err != nil {
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
	if err := os.WriteFile(empty, nil, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := readSidecarSum(empty); err == nil {
		t.Fatal("expected error for empty sidecar")
	}
}

func TestAtomicWriteFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "out")
	if err := atomicWriteFile(p, []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "data" {
		t.Fatalf("got %q, want data", got)
	}
	// Overwrite is atomic and replaces content.
	if err := atomicWriteFile(p, []byte("new"), 0644); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(p); string(got) != "new" {
		t.Fatalf("got %q, want new", got)
	}
}

func TestNewUUID(t *testing.T) {
	id, err := newUUID()
	if err != nil {
		t.Fatal(err)
	}
	// 8-4-4-4-12 layout.
	parts := strings.Split(id, "-")
	if len(parts) != 5 {
		t.Fatalf("uuid %q: want 5 dash-separated groups, got %d", id, len(parts))
	}
	for i, n := range []int{8, 4, 4, 4, 12} {
		if len(parts[i]) != n {
			t.Fatalf("uuid %q: group %d len %d, want %d", id, i, len(parts[i]), n)
		}
	}
	if parts[2][0] != '4' {
		t.Errorf("uuid %q: version nibble = %c, want 4", id, parts[2][0])
	}
	if c := parts[3][0]; c != '8' && c != '9' && c != 'a' && c != 'b' {
		t.Errorf("uuid %q: variant nibble = %c, want one of 89ab", id, c)
	}
	if other, _ := newUUID(); other == id {
		t.Fatal("newUUID returned identical values")
	}
}

func TestRandHex(t *testing.T) {
	s, err := randHex(8)
	if err != nil {
		t.Fatal(err)
	}
	if len(s) != 16 {
		t.Fatalf("randHex(8) len = %d, want 16", len(s))
	}
	if other, _ := randHex(8); other == s {
		t.Fatal("randHex returned identical values")
	}
}

func TestRunAddSyncRestore(t *testing.T) {
	useTempConfig(t)
	repo := initRepo(t)
	backup := filepath.Join(t.TempDir(), "backup")

	if err := run([]string{"add", repo, backup}); err != nil {
		t.Fatalf("run add: %v", err)
	}
	if err := run([]string{"sync"}); err != nil {
		t.Fatalf("run sync: %v", err)
	}
	restore := filepath.Join(t.TempDir(), "restored")
	if err := run([]string{"restore", backup, restore}); err != nil {
		t.Fatalf("run restore: %v", err)
	}
	if _, err := os.Stat(filepath.Join(restore, ".git")); err != nil {
		t.Fatalf("restored repo missing: %v", err)
	}
}

func TestRunUsageErrors(t *testing.T) {
	cases := [][]string{
		{},                      // no command
		{"bogus"},               // unknown command
		{"sync", "a"},           // too few args
		{"restore", "a"},        // too few args
		{"sync", "a", "b", "c"}, // too many args
	}
	for _, args := range cases {
		if err := run(args); !errors.Is(err, errUsage) {
			t.Errorf("run(%q) = %v, want errUsage", args, err)
		}
	}
}
