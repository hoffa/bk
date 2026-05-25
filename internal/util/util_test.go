package util_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hoffa/bk/internal/util"
)

func TestSHA256Sum(t *testing.T) {
	dir := t.TempDir()
	bundle := filepath.Join(dir, "x.bundle")

	if err := util.WriteSHA256Sum(bundle, "deadbeef"); err != nil {
		t.Fatal(err)
	}

	// The sidecar is sha256sum format: "<hash>  <base name>".
	raw, err := os.ReadFile(bundle + ".sha256")
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(raw), "deadbeef") || !strings.Contains(string(raw), "x.bundle") {
		t.Fatalf("sidecar = %q", raw)
	}

	got, err := util.ReadSHA256Sum(bundle)
	if err != nil {
		t.Fatal(err)
	}

	if got != "deadbeef" {
		t.Fatalf("got %q, want deadbeef", got)
	}

	// Missing sidecar errors.
	if _, err := util.ReadSHA256Sum(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("expected error for missing sidecar")
	}

	// Empty sidecar errors.
	empty := filepath.Join(dir, "e.bundle")
	if err := os.WriteFile(empty+".sha256", nil, 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := util.ReadSHA256Sum(empty); err == nil {
		t.Fatal("expected error for empty sidecar")
	}
}

func TestExists(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f")
	if util.Exists(p) {
		t.Error("missing file should not exist")
	}

	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if !util.Exists(p) {
		t.Error("written file should exist")
	}
}

func TestSHA256(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(p, []byte("abc"), 0o644); err != nil {
		t.Fatal(err)
	}

	// echo -n abc | sha256sum
	const want = "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"

	got, err := util.SHA256(p)
	if err != nil {
		t.Fatal(err)
	}

	if got != want {
		t.Fatalf("sha256 = %s, want %s", got, want)
	}

	if _, err := util.SHA256(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestAtomicWrite(t *testing.T) {
	p := filepath.Join(t.TempDir(), "out")
	if err := util.AtomicWrite(p, []byte("data"), 0o644); err != nil {
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
	if err := util.AtomicWrite(p, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}

	if got, _ := os.ReadFile(p); string(got) != "new" {
		t.Fatalf("got %q, want new", got)
	}
}

func TestRandHex(t *testing.T) {
	s, err := util.RandHex(8)
	if err != nil {
		t.Fatal(err)
	}

	if len(s) != 16 {
		t.Fatalf("RandHex(8) len = %d, want 16", len(s))
	}

	if other, _ := util.RandHex(8); other == s {
		t.Fatal("RandHex returned identical values")
	}
}
