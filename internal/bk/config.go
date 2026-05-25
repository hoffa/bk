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
	"strings"
	"time"

	"github.com/hoffa/bk/internal/crypt"
	"github.com/hoffa/bk/internal/util"
)

// Entry is one configured backup. ID is the entry's own stable handle, assigned
// at Add and used to refer to it (e.g. bk rm). Source and Target are what you
// declared. Backup is what we last learned from the target, cached for offline
// verification and status; it's nil until the first sync.
type Entry struct {
	ID     string
	Source string
	Target string
	Backup *Backup
}

// Backup mirrors what the config caches about a target between syncs so bk can
// verify and show status while the target is absent. The target's own
// BK_BACKUP.json / latest.json are authoritative; this is a regenerable cache.
type Backup struct {
	ID          string    // the backup's identity, for trust-on-first-use
	ContentHash string    // fingerprint of the source's content at the last sync
	SyncedAt    time.Time // last sync time
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

// Add appends a source -> target backup with a fresh handle and returns its id,
// erroring if the exact pair is already configured. The target is initialized on
// the first Sync, so it need not exist yet. The caller must Save to persist.
func (c *Config) Add(source, target string) (string, error) {
	for _, e := range c.Sync {
		if e.Source == source && e.Target == target {
			return "", fmt.Errorf("already configured: %s -> %s", source, target)
		}
	}

	id, err := util.RandHex(16)
	if err != nil {
		return "", err
	}

	c.Sync = append(c.Sync, Entry{ID: id, Source: source, Target: target})

	return id, nil
}

// Match returns the single entry whose ID starts with prefix, erroring if no
// entry matches or more than one does.
func (c *Config) Match(prefix string) (*Entry, error) {
	var found *Entry

	for i := range c.Sync {
		if strings.HasPrefix(c.Sync[i].ID, prefix) {
			if found != nil {
				return nil, fmt.Errorf("id %q is ambiguous; it matches more than one backup", prefix)
			}

			found = &c.Sync[i]
		}
	}

	if found == nil {
		return nil, fmt.Errorf("no backup with id %q", prefix)
	}

	return found, nil
}

// Remove drops the entry with the given (exact) ID.
func (c *Config) Remove(id string) {
	kept := c.Sync[:0]

	for _, e := range c.Sync {
		if e.ID != id {
			kept = append(kept, e)
		}
	}

	c.Sync = kept
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
