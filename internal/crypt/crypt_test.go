package crypt_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/hoffa/bk/internal/crypt"
)

func TestKeyringRoundTrip(t *testing.T) {
	const pw = "correct horse battery staple"

	kr, err := crypt.NewKeyring(pw)
	if err != nil {
		t.Fatal(err)
	}

	if kr.Public == "" || kr.PrivateEncrypted == "" {
		t.Fatalf("empty keyring: %+v", kr)
	}

	// Right password unwraps the identity.
	if _, err := kr.Identity(pw); err != nil {
		t.Fatalf("Identity with correct password: %v", err)
	}

	// Wrong password is rejected.
	if _, err := kr.Identity("nope"); !errors.Is(err, crypt.ErrWrongPassword) {
		t.Fatalf("Identity with wrong password = %v, want ErrWrongPassword", err)
	}
}

func TestEncryptDecryptFile(t *testing.T) {
	const pw = "hunter2"

	kr, err := crypt.NewKeyring(pw)
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	plain := filepath.Join(dir, "data")
	enc := filepath.Join(dir, "data.age")
	out := filepath.Join(dir, "data.out")

	want := []byte("the quick brown fox\x00\x01\x02 binary too")
	if err := os.WriteFile(plain, want, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := crypt.EncryptFile(enc, plain, kr.Public); err != nil {
		t.Fatalf("EncryptFile: %v", err)
	}

	// Ciphertext differs from plaintext.
	if ct, _ := os.ReadFile(enc); string(ct) == string(want) {
		t.Fatal("ciphertext equals plaintext")
	}

	id, err := kr.Identity(pw)
	if err != nil {
		t.Fatal(err)
	}

	if err := crypt.DecryptFile(out, enc, id); err != nil {
		t.Fatalf("DecryptFile: %v", err)
	}

	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}

	if string(got) != string(want) {
		t.Fatalf("round trip mismatch: got %q want %q", got, want)
	}
}

func TestDecryptWrongIdentity(t *testing.T) {
	kr1, _ := crypt.NewKeyring("a")
	kr2, _ := crypt.NewKeyring("b")

	dir := t.TempDir()
	plain := filepath.Join(dir, "data")
	enc := filepath.Join(dir, "data.age")

	if err := os.WriteFile(plain, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := crypt.EncryptFile(enc, plain, kr1.Public); err != nil {
		t.Fatal(err)
	}

	id2, _ := kr2.Identity("b")
	if err := crypt.DecryptFile(filepath.Join(dir, "out"), enc, id2); err == nil {
		t.Fatal("decrypt with a different identity should fail")
	}
}
