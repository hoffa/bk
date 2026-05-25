package cli

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

	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	mustRun(t, dir, "git", "add", ".")
	mustRun(t, dir, "git", "commit", "-qm", "first")

	return dir
}

// useTempConfig points the config at a temp file for the duration of the test.
func useTempConfig(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("BK_CONFIG", path)
	t.Setenv("BK_PASSWORD", "test-password") // non-interactive add/restore

	return path
}

func TestRunAddSyncRestore(t *testing.T) {
	useTempConfig(t)
	repo := initRepo(t)
	backup := filepath.Join(t.TempDir(), "backup")

	if err := run(t.Context(), []string{"add", repo, backup}); err != nil {
		t.Fatalf("run add: %v", err)
	}

	if err := run(t.Context(), []string{"sync"}); err != nil {
		t.Fatalf("run sync: %v", err)
	}

	restore := filepath.Join(t.TempDir(), "restored")
	if err := run(t.Context(), []string{"restore", backup, restore}); err != nil {
		t.Fatalf("run restore: %v", err)
	}

	if _, err := os.Stat(filepath.Join(restore, ".git")); err != nil {
		t.Fatalf("restored repo missing: %v", err)
	}
}

func TestAddRejectsNonRepo(t *testing.T) {
	useTempConfig(t)

	err := addCmd(t.Context(), []string{t.TempDir(), filepath.Join(t.TempDir(), "backup")})
	if err == nil || !strings.Contains(err.Error(), "not a git repository") {
		t.Fatalf("want not-a-git-repository error, got %v", err)
	}
}

func TestRunSyncNoEntries(t *testing.T) {
	useTempConfig(t)

	err := run(t.Context(), []string{"sync"})
	if err == nil || errors.Is(err, errUsage) || !strings.Contains(err.Error(), "no sync entries") {
		t.Fatalf("want no-entries error, got %v", err)
	}
}

func TestRunUsageErrors(t *testing.T) {
	cases := [][]string{
		{"bogus"},               // unknown command
		{"sync", "a"},           // too few args
		{"restore", "a"},        // too few args
		{"sync", "a", "b", "c"}, // too many args
	}
	for _, args := range cases {
		if err := run(t.Context(), args); !errors.Is(err, errUsage) {
			t.Errorf("run(%q) = %v, want errUsage", args, err)
		}
	}
}
