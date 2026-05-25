// Package git wraps the git command-line operations bk relies on. Each function
// runs a single git invocation with a context for cancellation and captures
// git's stderr into the returned error. It does no higher-level orchestration
// (verify-after-create, currency fingerprints, path safety) -- that lives in the
// caller, so this stays a thin, testable boundary around the messy shellouts.
package git

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// SafeCreateBundle writes a bundle of all refs in repoDir to bundlePath and
// verifies it before returning, so callers never have to remember to check the
// bundle themselves. On verification failure the (suspect) bundle is left on
// disk for the caller to clean up.
func SafeCreateBundle(ctx context.Context, repoDir, bundlePath string) error {
	if err := createBundle(ctx, repoDir, bundlePath); err != nil {
		return err
	}

	return verifyBundle(ctx, bundlePath)
}

// Clone clones the repository at src (a path or a bundle) into dst. Cloning a
// bundle already validates it -- git rejects a malformed or corrupt bundle --
// so no separate verify step is needed here.
func Clone(ctx context.Context, src, dst string) error {
	_, err := run(ctx, "", "clone", src, dst)

	return err
}

// RefsHash returns a fingerprint of all of repoDir's refs: the sha256 of every
// ref's object id and name. It changes whenever any ref an --all bundle would
// capture changes, so comparing it to a previous value tells you whether
// anything needs backing up -- matching hashes mean identical refs.
func RefsHash(ctx context.Context, repoDir string) (string, error) {
	out, err := run(ctx, repoDir, "for-each-ref", "--format=%(objectname) %(refname)")
	if err != nil {
		return "", err
	}

	sum := sha256.Sum256(out)

	return hex.EncodeToString(sum[:]), nil
}

// createBundle writes a bundle of all refs in repoDir to bundlePath. It does no
// verification; callers should use SafeCreateBundle.
func createBundle(ctx context.Context, repoDir, bundlePath string) error {
	_, err := run(ctx, repoDir, "bundle", "create", bundlePath, "--all")

	return err
}

// verifyBundle checks that bundlePath is a valid, self-contained git bundle.
//
// "git bundle verify" must run inside a repository even for a bundle with no
// prerequisites, so it runs in a throwaway empty repo. This keeps verification
// usable when no source repo is available (e.g. before a restore).
func verifyBundle(ctx context.Context, bundlePath string) error {
	abs, err := filepath.Abs(bundlePath)
	if err != nil {
		return err
	}

	tmp, err := os.MkdirTemp("", "bk-verify-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	if _, err := run(ctx, "", "init", "-q", tmp); err != nil {
		return err
	}

	_, err = run(ctx, tmp, "bundle", "verify", abs)

	return err
}

// run executes a git subcommand in dir (the current directory if empty) and
// returns its stdout. On failure the error includes git's stderr.
func run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir

	var stderr bytes.Buffer

	cmd.Stderr = &stderr

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w\n%s", args[0], err, stderr.String())
	}

	return out, nil
}
