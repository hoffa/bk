package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// syncEntry is one configured backup: a source repo and a target backup dir.
// ID is empty until the first sync, which initializes the target and records
// its BK_BACKUP.json id (trust on first use). Later syncs match against it so
// we never write into a wrong or replaced target.
type syncEntry struct {
	Source string `json:"source"`
	Target string `json:"target"`
	ID     string `json:"id,omitempty"`
}

type config struct {
	Sync []syncEntry `json:"sync"`
}

// errTargetAbsent means a target path does not exist, e.g. an unplugged drive.
var errTargetAbsent = errors.New("target not present")

// configPath resolves the global config location: BK_CONFIG overrides
// everything, otherwise $XDG_CONFIG_HOME/bk/config.json (default ~/.config).
// Override per-invocation with BK_CONFIG=/path bk ...
func configPath() (string, error) {
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
	var dirty bool
	for i := range cfg.Sync {
		e := &cfg.Sync[i]
		hadID := e.ID != ""
		switch synced, err := syncConfigured(e); {
		case errors.Is(err, errTargetAbsent):
			fmt.Printf("skip %s -> %s: target not present\n", e.Source, e.Target)
		case err != nil:
			fmt.Fprintf(os.Stderr, "error %s -> %s: %v\n", e.Source, e.Target, err)
			failed++
		case synced:
			fmt.Printf("synced %s -> %s\n", e.Source, e.Target)
		default:
			fmt.Printf("up to date %s -> %s\n", e.Source, e.Target)
		}
		if !hadID && e.ID != "" {
			dirty = true // first sync recorded the target's id
		}
	}
	if dirty {
		if err := saveConfig(cfg); err != nil {
			return fmt.Errorf("save config: %w", err)
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d entries failed", failed, len(cfg.Sync))
	}
	return nil
}

// syncConfigured syncs one entry. On the first sync (empty id) it initializes
// the target and records its id; afterwards it verifies the target's id matches
// before syncing. A target that isn't present is reported as errTargetAbsent.
// It reports whether a new version was written.
func syncConfigured(e *syncEntry) (bool, error) {
	target, err := filepath.Abs(e.Target)
	if err != nil {
		return false, err
	}

	if e.ID == "" {
		// First sync: the parent (e.g. a mount point) must exist so we create
		// the backup on the intended volume, not somewhere a missing mount used
		// to be.
		if _, err := os.Stat(filepath.Dir(target)); errors.Is(err, os.ErrNotExist) {
			return false, errTargetAbsent
		} else if err != nil {
			return false, err
		}
		if err := initBackup(target); err != nil {
			return false, err
		}
		meta, err := loadBackupMeta(target)
		if err != nil {
			return false, err
		}
		e.ID = meta.ID
	} else {
		if _, err := os.Stat(target); errors.Is(err, os.ErrNotExist) {
			return false, errTargetAbsent
		} else if err != nil {
			return false, err
		}
		meta, err := loadBackupMeta(target)
		if err != nil {
			return false, fmt.Errorf("not a valid backup: %w", err)
		}
		if meta.ID != e.ID {
			return false, fmt.Errorf("id mismatch: expected %s, found %s (wrong target?)", e.ID, meta.ID)
		}
	}

	return syncBackup(e.Source, target)
}
