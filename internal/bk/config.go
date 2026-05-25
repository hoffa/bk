// Package bk is the core of the backup tool. It manages the global config of
// source -> target pairs and performs the git-bundle backups, restores, and
// status checks. It does no terminal I/O: callers (the CLI and TUI) load the
// config, iterate its entries, and call Sync, Eval, or Restore per entry, then
// render and persist however they like.
package bk

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/hoffa/bk/internal/crypt"
	"github.com/hoffa/bk/internal/util"
)

// Entry is one configured backup: a source repo and a target backup dir. ID is
// empty until the first sync, which initializes the target and records its
// BK_BACKUP.json id (trust on first use); later syncs match against it so we
// never write into a wrong or replaced target. RefsHash and SyncedAt cache the
// refs fingerprint and time (RFC3339) of the last sync, so currency and last-sync
// time can be shown while the target is absent (e.g. unplugged); they are empty
// until the first sync, and the target's latest.json is authoritative when present.
type Entry struct {
	Source   string
	Target   string
	ID       string
	RefsHash string
	SyncedAt string
}

// Config is the whole on-disk config: the set of configured backups plus the
// encryption keyring (set on first add; backups are always encrypted to it).
type Config struct {
	Sync []Entry
	Key  crypt.Keyring
}

// ErrTargetAbsent means a target path does not exist, e.g. an unplugged drive.
var ErrTargetAbsent = errors.New("target not present")

// Load reads the global config, returning an empty config if none exists yet.
func Load() (*Config, error) {
	path, err := ConfigPath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &Config{}, nil
	}

	if err != nil {
		return nil, err
	}

	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	return &c, nil
}

// Save atomically writes the config, creating its directory if needed.
func (c *Config) Save() error {
	path, err := ConfigPath()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}

	return util.AtomicWrite(path, append(data, '\n'), 0644)
}

// Add appends a source -> target backup, returning an error if the exact pair is
// already configured. The target is initialized on the first Sync, so it need
// not exist yet. The caller must Save to persist.
func (c *Config) Add(source, target string) error {
	for _, e := range c.Sync {
		if e.Source == source && e.Target == target {
			return fmt.Errorf("already configured: %s -> %s", source, target)
		}
	}

	c.Sync = append(c.Sync, Entry{Source: source, Target: target})

	return nil
}

// HasKey reports whether the encryption keyring has been set up.
func (c *Config) HasKey() bool {
	return c.Key.Public != ""
}

// SetPassword creates the encryption keyring from password. Backups are then
// encrypted to it; the password is needed only to restore.
func (c *Config) SetPassword(password string) error {
	kr, err := crypt.NewKeyring(password)
	if err != nil {
		return err
	}

	c.Key = kr

	return nil
}

// ConfigPath resolves the global config location: BK_CONFIG overrides everything,
// otherwise $XDG_CONFIG_HOME/bk/config.json (default ~/.config). Override
// per-invocation with BK_CONFIG=/path bk ...
func ConfigPath() (string, error) {
	if p := os.Getenv("BK_CONFIG"); p != "" {
		return p, nil
	}

	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}

		dir = filepath.Join(home, ".config")
	}

	return filepath.Join(dir, "bk", "config.json"), nil
}
