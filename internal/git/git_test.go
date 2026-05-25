package git_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/hoffa/bk/internal/git"
)

// initRepo creates a git repo with one commit and returns its path.
func initRepo(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()

	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir

		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	run("init", "-q")
	run("config", "user.email", "t@example.com")
	run("config", "user.name", "tester")

	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	run("add", ".")
	run("commit", "-qm", "first")

	return dir
}

func TestBundleRoundTrip(t *testing.T) {
	ctx := context.Background()
	repo := initRepo(t)
	bundle := filepath.Join(t.TempDir(), "x.bundle")

	if err := git.SafeCreateBundle(ctx, repo, bundle); err != nil {
		t.Fatalf("SafeCreateBundle: %v", err)
	}

	dst := filepath.Join(t.TempDir(), "clone")
	if err := git.Clone(ctx, bundle, dst); err != nil {
		t.Fatalf("Clone: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dst, ".git")); err != nil {
		t.Fatalf("clone missing .git: %v", err)
	}
}

func TestRefsHash(t *testing.T) {
	ctx := context.Background()
	repo := initRepo(t)

	h1, err := git.RefsHash(ctx, repo)
	if err != nil {
		t.Fatalf("RefsHash: %v", err)
	}

	if h1 == "" {
		t.Fatal("RefsHash returned empty")
	}

	// Stable when nothing changes.
	if h2, _ := git.RefsHash(ctx, repo); h2 != h1 {
		t.Fatalf("RefsHash changed without a repo change: %s -> %s", h1, h2)
	}

	// Changes after a new commit (the branch ref moves).
	commit := exec.Command("git", "commit", "--allow-empty", "-qm", "second")
	commit.Dir = repo

	if out, err := commit.CombinedOutput(); err != nil {
		t.Fatalf("commit: %v\n%s", err, out)
	}

	if h3, _ := git.RefsHash(ctx, repo); h3 == h1 {
		t.Fatal("RefsHash did not change after a new commit")
	}
}

func TestCloneRejectsBadBundle(t *testing.T) {
	bad := filepath.Join(t.TempDir(), "not.bundle")
	if err := os.WriteFile(bad, []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}

	// git clone validates the bundle itself, so a corrupt one is rejected.
	dst := filepath.Join(t.TempDir(), "clone")
	if err := git.Clone(context.Background(), bad, dst); err == nil {
		t.Fatal("expected clone to fail on a non-bundle file")
	}
}

func TestCloneExistingDst(t *testing.T) {
	ctx := context.Background()
	repo := initRepo(t)
	bundle := filepath.Join(t.TempDir(), "x.bundle")

	if err := git.SafeCreateBundle(ctx, repo, bundle); err != nil {
		t.Fatal(err)
	}

	// A valid bundle, but a dst that already exists -> clone fails.
	dst := filepath.Join(t.TempDir(), "dst")
	if err := os.WriteFile(dst, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := git.Clone(ctx, bundle, dst); err == nil {
		t.Fatal("expected clone to fail into an existing path")
	}
}

func TestSafeCreateBundleInvalidRepo(t *testing.T) {
	out := filepath.Join(t.TempDir(), "x.bundle")
	if err := git.SafeCreateBundle(context.Background(), t.TempDir(), out); err == nil {
		t.Fatal("expected SafeCreateBundle to fail outside a repo")
	}
}

func TestRefsHashInvalidRepo(t *testing.T) {
	if _, err := git.RefsHash(context.Background(), t.TempDir()); err == nil {
		t.Fatal("expected RefsHash to fail outside a repo")
	}
}
