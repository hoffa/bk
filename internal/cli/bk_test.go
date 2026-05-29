package cli

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hoffa/bk/internal/bk"
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
	t.Setenv("BK_PASSWORD", "test-password") // non-interactive restore

	// Initialize the keyring up front, as `bk init` would.
	cfg, err := bk.Load()
	if err != nil {
		t.Fatal(err)
	}

	if err := cfg.SetPassword("test-password"); err != nil {
		t.Fatal(err)
	}

	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	return path
}

func TestRunAddSyncRestore(t *testing.T) {
	useTempConfig(t)
	repo := initRepo(t)
	backup := filepath.Join(t.TempDir(), "backup")

	if err := run(t.Context(), []string{"add", repo, backup}); err != nil {
		t.Fatalf("run add: %v", err)
	}

	cfg, err := bk.Load()
	if err != nil {
		t.Fatal(err)
	}

	if err := run(t.Context(), []string{"sync", cfg.Sync[0].ID[:6]}); err != nil {
		t.Fatalf("run sync by id: %v", err)
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

func TestInit(t *testing.T) {
	t.Setenv("BK_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	t.Setenv("BK_PASSWORD", "pw")

	repo := initRepo(t)
	target := filepath.Join(t.TempDir(), "backup")

	// add before init fails (the source is a valid repo, so it's the keyring).
	if err := run(t.Context(), []string{"add", repo, target}); err == nil {
		t.Fatal("add before init should fail")
	}

	if err := run(t.Context(), []string{"init"}); err != nil {
		t.Fatalf("init: %v", err)
	}

	if cfg, _ := bk.Load(); !cfg.HasKey() {
		t.Fatal("init did not set a keyring")
	}

	// Re-init errors.
	if err := run(t.Context(), []string{"init"}); err == nil {
		t.Fatal("re-init should fail")
	}

	// add now works.
	if err := run(t.Context(), []string{"add", repo, target}); err != nil {
		t.Fatalf("add after init: %v", err)
	}
}

func TestInitForce(t *testing.T) {
	t.Setenv("BK_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	t.Setenv("BK_PASSWORD", "pw1")

	if err := run(t.Context(), []string{"init"}); err != nil {
		t.Fatal(err)
	}

	cfg, err := bk.Load()
	if err != nil {
		t.Fatal(err)
	}

	old := cfg.Key.Public

	// Re-init without --force is refused.
	if err := run(t.Context(), []string{"init"}); err == nil {
		t.Fatal("re-init without --force should fail")
	}

	// With --force it sets a new key.
	t.Setenv("BK_PASSWORD", "pw2")

	if err := run(t.Context(), []string{"init", "--force"}); err != nil {
		t.Fatalf("init --force: %v", err)
	}

	if cfg2, _ := bk.Load(); cfg2.Key.Public == old {
		t.Error("--force should generate a new key")
	}
}

func TestRunRm(t *testing.T) {
	useTempConfig(t)
	repo := initRepo(t)
	target := filepath.Join(t.TempDir(), "backup")

	if err := run(t.Context(), []string{"add", repo, target}); err != nil {
		t.Fatal(err)
	}

	cfg, err := bk.Load()
	if err != nil {
		t.Fatal(err)
	}

	// Remove by an id prefix.
	if err := run(t.Context(), []string{"rm", cfg.Sync[0].ID[:6]}); err != nil {
		t.Fatalf("rm: %v", err)
	}

	if cfg, _ := bk.Load(); len(cfg.Sync) != 0 {
		t.Fatalf("expected entry removed, got %+v", cfg.Sync)
	}

	if err := run(t.Context(), []string{"add", repo, target}); err != nil {
		t.Fatal(err)
	}

	// With only one configured backup, rm without an id removes it.
	if err := run(t.Context(), []string{"rm"}); err != nil {
		t.Fatalf("rm without id: %v", err)
	}

	if cfg, _ := bk.Load(); len(cfg.Sync) != 0 {
		t.Fatalf("expected entry removed, got %+v", cfg.Sync)
	}

	// Unknown id errors.
	if err := run(t.Context(), []string{"rm", "nope"}); err == nil {
		t.Fatal("expected error removing unknown id")
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
		{},                      // no command
		{"restore", "a"},        // too few args
		{"sync", "a", "b", "c"}, // too many args
		{"rm", "a", "b"},        // too many args
	}
	for _, args := range cases {
		if err := run(t.Context(), args); !errors.Is(err, errUsage) {
			t.Errorf("run(%q) = %v, want errUsage", args, err)
		}
	}
}
