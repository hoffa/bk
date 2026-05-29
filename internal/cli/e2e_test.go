package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// bkBin is the path to the bk binary built once for the black-box e2e tests.
var bkBin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "bk-e2e-*")
	if err != nil {
		panic(err)
	}

	bkBin = filepath.Join(dir, "bk")

	// Safety net: keep all tests off the real ~/.config/bk; individual tests
	// may still override BK_CONFIG via t.Setenv.
	_ = os.Setenv("BK_CONFIG", filepath.Join(dir, "config.json"))
	_ = os.Setenv("BK_PASSWORD", "test-password") // non-interactive add/restore

	build := exec.Command("go", "build", "-o", bkBin, "github.com/hoffa/bk")

	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "e2e build failed:", err)

		_ = os.RemoveAll(dir)

		os.Exit(1)
	}

	code := m.Run()
	_ = os.RemoveAll(dir)

	os.Exit(code)
}

// runBin runs the built binary and returns its combined output and exit code.
func runBin(t *testing.T, args ...string) (string, int) {
	t.Helper()

	out, err := exec.Command(bkBin, args...).CombinedOutput()
	if err == nil {
		return string(out), 0
	}

	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return string(out), ee.ExitCode()
	}

	t.Fatalf("run %v: %v", args, err)

	return "", 0
}

func TestE2E(t *testing.T) {
	// Isolate the config; the subprocess inherits this env.
	t.Setenv("BK_CONFIG", filepath.Join(t.TempDir(), "config.json"))

	repo := initRepo(t)
	backup := filepath.Join(t.TempDir(), "backup")
	restore := filepath.Join(t.TempDir(), "restored")

	var id string

	t.Run("init ok", func(t *testing.T) {
		if out, code := runBin(t, "init"); code != 0 {
			t.Fatalf("exit %d, want 0\n%s", code, out)
		}
	})

	t.Run("add ok", func(t *testing.T) {
		out, code := runBin(t, "add", repo, backup)
		if code != 0 {
			t.Fatalf("exit %d, want 0\n%s", code, out)
		}

		id = strings.TrimSpace(out)
		if id == "" {
			t.Fatal("add did not print an id")
		}
	})

	t.Run("sync by id ok", func(t *testing.T) {
		if out, code := runBin(t, "sync", id[:6]); code != 0 {
			t.Fatalf("exit %d, want 0\n%s", code, out)
		}
	})

	t.Run("restore ok", func(t *testing.T) {
		if out, code := runBin(t, "restore", backup, restore); code != 0 {
			t.Fatalf("exit %d, want 0\n%s", code, out)
		}

		if log := output(t, restore, "git", "log", "--oneline"); !strings.Contains(log, "first") {
			t.Fatalf("restored repo missing commit:\n%s", log)
		}
	})

	t.Run("restore existing target fails", func(t *testing.T) {
		out, code := runBin(t, "restore", backup, restore) // restore now exists
		if code != 1 {
			t.Fatalf("exit %d, want 1\n%s", code, out)
		}

		if !strings.Contains(out, "already exists") {
			t.Fatalf("missing error message:\n%s", out)
		}
	})

	t.Run("no args is usage", func(t *testing.T) {
		out, code := runBin(t)
		if code != 2 {
			t.Fatalf("exit %d, want 2\n%s", code, out)
		}

		if !strings.Contains(out, "usage: bk") {
			t.Fatalf("usage missing:\n%s", out)
		}
	})

	t.Run("unknown command is usage", func(t *testing.T) {
		if _, code := runBin(t, "bogus"); code != 2 {
			t.Fatalf("exit %d, want 2", code)
		}
	})

	t.Run("sync with too many args is usage", func(t *testing.T) {
		if _, code := runBin(t, "sync", "a", "b"); code != 2 {
			t.Fatalf("exit %d, want 2", code)
		}
	})
}
