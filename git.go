package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// repoRefsHash returns a fingerprint of all the repo's refs (object name +
// ref name for every ref). It changes whenever any ref a --all bundle would
// capture changes, so it can be used to skip a sync when nothing has changed:
// matching fingerprints mean identical refs, hence an identical bundle.
func repoRefsHash(repoPath string) (string, error) {
	cmd := exec.Command("git", "for-each-ref", "--format=%(objectname) %(refname)")
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git for-each-ref: %w", err)
	}
	sum := sha256.Sum256(out)
	return hex.EncodeToString(sum[:]), nil
}

// verifyBundle checks that the bundle at bundlePath is a valid git bundle.
//
// "git bundle verify" must run inside a git repository, even for a
// self-contained bundle with no prerequisites, so it runs in a throwaway empty
// repo. This keeps verification self-contained and usable when no source repo
// is available (e.g. before a restore).
func verifyBundle(bundlePath string) error {
	abs, err := filepath.Abs(bundlePath)
	if err != nil {
		return err
	}

	tmp, err := os.MkdirTemp("", "bk-verify-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	if out, err := exec.Command("git", "init", "-q", tmp).CombinedOutput(); err != nil {
		return fmt.Errorf("git init for verify: %w\n%s", err, out)
	}

	verify := exec.Command("git", "bundle", "verify", abs)
	verify.Dir = tmp
	if out, err := verify.CombinedOutput(); err != nil {
		return fmt.Errorf("git bundle verify: %w\n%s", err, out)
	}
	return nil
}

// createBundle creates a git bundle from the repo at repoPath, writing it to
// bundlePath, and then verifies the resulting bundle.
func createBundle(repoPath, bundlePath string) error {
	create := exec.Command("git", "bundle", "create", bundlePath, "--all")
	create.Dir = repoPath
	if out, err := create.CombinedOutput(); err != nil {
		return fmt.Errorf("git bundle create: %w\n%s", err, out)
	}

	if err := verifyBundle(bundlePath); err != nil {
		return err
	}

	return nil
}

// restoreBundle verifies the bundle at bundlePath and then restores it into a
// new repository at restorePath by cloning from the bundle.
func restoreBundle(bundlePath, restorePath string) error {
	if _, err := os.Stat(restorePath); err == nil {
		return fmt.Errorf("restore path already exists: %s", restorePath)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat restore path: %w", err)
	}

	if err := verifyBundle(bundlePath); err != nil {
		return err
	}

	clone := exec.Command("git", "clone", bundlePath, restorePath)
	if out, err := clone.CombinedOutput(); err != nil {
		return fmt.Errorf("git clone from bundle: %w\n%s", err, out)
	}
	return nil
}
