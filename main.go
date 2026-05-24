package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

// errUsage signals a usage error: the message has already been printed and the
// process should exit with status 2.
var errUsage = errors.New("usage")

func usage() {
	fmt.Fprint(os.Stderr, `usage: bk <command> <args>

commands:
  add <repo-path> <backup-dir>          register a repo -> backup-dir pair in the config
  sync                                  sync all configured backups
  status                                show the state of every configured backup
  restore <backup-dir> <restore-path>   restore a backup's latest version
`)
}

func statusCmd(args []string) error {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	_ = fs.Parse(args) // flag.ExitOnError handles parse errors

	if fs.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "usage: bk status")
		return errUsage
	}

	statuses, err := statusAll()
	if err != nil {
		return err
	}
	if len(statuses) == 0 {
		fmt.Println("no backups configured; add one with: bk add <repo> <backup-dir>")
		return nil
	}
	return printStatus(os.Stdout, statuses)
}

func syncCmd(args []string) error {
	fs := flag.NewFlagSet("sync", flag.ExitOnError)
	_ = fs.Parse(args) // flag.ExitOnError handles parse errors

	if fs.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "usage: bk sync")
		return errUsage
	}
	return syncAll()
}

func addCmd(args []string) error {
	fs := flag.NewFlagSet("add", flag.ExitOnError)
	_ = fs.Parse(args) // flag.ExitOnError handles parse errors

	if fs.NArg() != 2 {
		fmt.Fprintln(os.Stderr, "usage: bk add <repo-path> <backup-dir>")
		return errUsage
	}

	source, err := filepath.Abs(fs.Arg(0))
	if err != nil {
		return err
	}
	target, err := filepath.Abs(fs.Arg(1))
	if err != nil {
		return err
	}

	cfg, _, err := loadConfig()
	if err != nil {
		return err
	}
	for _, e := range cfg.Sync {
		if e.Source == source && e.Target == target {
			return fmt.Errorf("already configured: %s -> %s", source, target)
		}
	}

	// Pure config: the target is initialized on first sync, so it need not be
	// present now.
	cfg.Sync = append(cfg.Sync, syncEntry{Source: source, Target: target})
	if err := saveConfig(cfg); err != nil {
		return err
	}

	fmt.Printf("added %s -> %s (run 'bk sync' to back up)\n", source, target)
	return nil
}

func restoreCmd(args []string) error {
	fs := flag.NewFlagSet("restore", flag.ExitOnError)
	_ = fs.Parse(args) // flag.ExitOnError handles parse errors

	if fs.NArg() != 2 {
		fmt.Fprintln(os.Stderr, "usage: bk restore <backup-dir> <restore-path>")
		return errUsage
	}
	return restoreBackup(fs.Arg(0), fs.Arg(1))
}

// run dispatches a command and returns an error; errUsage means usage was
// already printed.
func run(args []string) error {
	if len(args) < 1 {
		return runDashboard(os.Stdout)
	}

	cmd, rest := args[0], args[1:]
	switch cmd {
	case "sync":
		return syncCmd(rest)
	case "add":
		return addCmd(rest)
	case "status":
		return statusCmd(rest)
	case "restore":
		return restoreCmd(rest)
	default:
		usage()
		return errUsage
	}
}

func main() {
	err := run(os.Args[1:])
	switch {
	case err == nil:
	case errors.Is(err, errUsage):
		os.Exit(2)
	default:
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
