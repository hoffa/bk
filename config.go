package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// syncEntry is one configured backup: a source repo, a target backup dir, and
// the id the target's BK_BACKUP.json is expected to have. The id is matched at
// sync time so we never write into the wrong or a replaced target.
type syncEntry struct {
	Source string `json:"source"`
	Target string `json:"target"`
	ID     string `json:"id"`
}

type config struct {
	Sync []syncEntry `json:"sync"`
}

// errTargetAbsent means a target path does not exist, e.g. an unplugged drive.
var errTargetAbsent = errors.New("target not present")

// configOverride, when non-empty, is the config path set by the -config flag.
// It takes precedence over the BK_CONFIG env var and the default location.
var configOverride string

// configPath resolves the global config location: the -config flag overrides
// everything, then BK_CONFIG, otherwise $XDG_CONFIG_HOME/bk/config.json
// (default ~/.config).
func configPath() (string, error) {
	if configOverride != "" {
		return configOverride, nil
	}
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

// loadConfig reads the config, returning an empty config if none exists yet.
// It also returns the resolved path for use in messages.
func loadConfig() (*config, string, error) {
	path, err := configPath()
	if err != nil {
		return nil, "", err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &config{}, path, nil
	}
	if err != nil {
		return nil, path, err
	}
	var c config
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, path, fmt.Errorf("parse %s: %w", path, err)
	}
	return &c, path, nil
}

// saveConfig atomically writes the config, creating its directory if needed.
func saveConfig(c *config) error {
	path, err := configPath()
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
	return atomicWriteFile(path, append(data, '\n'), 0644)
}

// syncAll syncs every configured entry. A target that is not present (e.g. an
// unplugged drive) is skipped with a notice; other failures are reported and
// cause a non-zero exit but do not stop the remaining entries.
func syncAll() error {
	cfg, path, err := loadConfig()
	if err != nil {
		return err
	}
	if len(cfg.Sync) == 0 {
		return fmt.Errorf("no sync entries in %s; add one with: bk add <repo> <backup-dir>", path)
	}

	var failed int
	for _, e := range cfg.Sync {
		switch err := syncConfigured(e); {
		case errors.Is(err, errTargetAbsent):
			fmt.Printf("skip %s -> %s: target not present\n", e.Source, e.Target)
		case err != nil:
			fmt.Fprintf(os.Stderr, "error %s -> %s: %v\n", e.Source, e.Target, err)
			failed++
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d entries failed", failed, len(cfg.Sync))
	}
	return nil
}

// syncConfigured verifies a target is present and has the expected id, then
// syncs the entry.
func syncConfigured(e syncEntry) error {
	target, err := filepath.Abs(e.Target)
	if err != nil {
		return err
	}
	if _, err := os.Stat(target); errors.Is(err, os.ErrNotExist) {
		return errTargetAbsent
	} else if err != nil {
		return err
	}

	meta, err := loadBackupMeta(target)
	if err != nil {
		return fmt.Errorf("not a valid backup: %w", err)
	}
	if meta.ID != e.ID {
		return fmt.Errorf("id mismatch: expected %s, found %s (wrong target?)", e.ID, meta.ID)
	}

	return syncBackup(e.Source, target)
}
