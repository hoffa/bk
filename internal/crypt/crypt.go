// Package crypt is bk's at-rest encryption: backups are encrypted to a public
// recipient (so day-to-day syncing needs no secret), and the matching identity
// is wrapped under the user's password (so only a restore needs it). The
// password is never stored -- only the public recipient and the wrapped key,
// both of which are useless without the password.
package crypt

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"

	"filippo.io/age"
)

// ErrWrongPassword means the password didn't unwrap the identity.
var ErrWrongPassword = errors.New("incorrect password")

// Keyring is the stored encryption material: the public recipient bundles are
// encrypted to, and the X25519 identity wrapped (scrypt) under the password.
// Neither reveals anything without the password.
type Keyring struct {
	Recipient  string // age public recipient, "age1..."
	WrappedKey string // identity encrypted to the password, base64
}

// NewKeyring generates a fresh identity and wraps it under password.
func NewKeyring(password string) (Keyring, error) {
	id, err := age.GenerateX25519Identity()
	if err != nil {
		return Keyring{}, err
	}

	wrapped, err := wrap(id.String(), password)
	if err != nil {
		return Keyring{}, err
	}

	return Keyring{Recipient: id.Recipient().String(), WrappedKey: wrapped}, nil
}

// Identity unwraps the decryption identity, returning ErrWrongPassword if the
// password is wrong.
func (k Keyring) Identity(password string) (*age.X25519Identity, error) {
	scrypt, err := age.NewScryptIdentity(password)
	if err != nil {
		return nil, err
	}

	raw, err := base64.StdEncoding.DecodeString(k.WrappedKey)
	if err != nil {
		return nil, fmt.Errorf("decode wrapped key: %w", err)
	}

	r, err := age.Decrypt(bytes.NewReader(raw), scrypt)
	if err != nil {
		return nil, ErrWrongPassword
	}

	secret, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}

	return age.ParseX25519Identity(string(secret))
}

// EncryptFile encrypts the file at src to dst for recipient.
func EncryptFile(dst, src, recipient string) error {
	r, err := age.ParseX25519Recipient(recipient)
	if err != nil {
		return err
	}

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()

	w, err := age.Encrypt(out, r)
	if err != nil {
		return err
	}

	if _, err := io.Copy(w, in); err != nil {
		return err
	}

	if err := w.Close(); err != nil {
		return err
	}

	return out.Close()
}

// DecryptFile decrypts the file at src to dst using identity.
func DecryptFile(dst, src string, id *age.X25519Identity) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	r, err := age.Decrypt(in, id)
	if err != nil {
		return err
	}

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()

	if _, err := io.Copy(out, r); err != nil { //nolint:gosec // bundle is verified after decrypt
		return err
	}

	return out.Close()
}

// wrap encrypts secret under password with age's scrypt (passphrase) recipient.
func wrap(secret, password string) (string, error) {
	r, err := age.NewScryptRecipient(password)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer

	w, err := age.Encrypt(&buf, r)
	if err != nil {
		return "", err
	}

	if _, err := io.WriteString(w, secret); err != nil {
		return "", err
	}

	if err := w.Close(); err != nil {
		return "", err
	}

	return base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}
