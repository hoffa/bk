package bk

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/hoffa/bk/internal/crypt"
)

const testPassword = "test-password"

// testKey is the keyring used across bk tests (generated once).
var testKey, _ = crypt.NewKeyring(testPassword)

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

	return path
}
