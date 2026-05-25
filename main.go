// Command bk makes versioned, verifiable backups of git repositories using git
// bundles. It registers repo -> backup-dir pairs in a global config, syncs them
// (skipping unchanged repos), and shows their currency in a live dashboard.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/hoffa/bk/internal/bk"
)

// errUsage signals a usage error: the message has already been printed and the
// process should exit with status 2.
var errUsage = errors.New("usage")

func usage() {
	fmt.Fprint(os.Stderr, "usage: bk <command> <args>\n\ncommands:\n  add <repo-path> <backup-dir>          register a repo -> backup-dir pair in the config\n  sync                                  all configured backups\n  status                                show the state of every configured backup\n  restore <backup-dir> <restore-path>   restore a backup's latest")
}

func statusCmd(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	_ = fs.Parse(args) // flag.ExitOnError handles parse errors

	if fs.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "usage: bk status")
		return errUsage
	}

	statuses, err := evalAll(ctx)
	if err != nil {
		return err
	}

	if len(statuses) == 0 {
		fmt.Println("no backups configured; add one with: bk add <repo> <backup-dir>")
		return nil
	}

	return printStatus(os.Stdout, statuses)
}

func syncCmd(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("sync", flag.ExitOnError)
	_ = fs.Parse(args) // flag.ExitOnError handles parse errors

	if fs.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "usage: bk sync")
		return errUsage
	}

	cfg, err := bk.Load()
	if err != nil {
		return err
	}

	if len(cfg.Sync) == 0 {
		path, _ := bk.ConfigPath()
		return fmt.Errorf("no sync entries in %s; add one with: bk add <repo> <backup-dir>", path)
	}

	// Sync each entry, reporting progress. A missing target (unplugged drive) is
	// a skip, not a failure; other errors are reported but don't stop the rest.
	var (
		failed int
		dirty  bool
	)

	for i := range cfg.Sync {
		e := &cfg.Sync[i]

		before := *e
		switch synced, err := bk.Sync(ctx, e); {
		case errors.Is(err, bk.ErrTargetAbsent):
			fmt.Printf("skip %s -> %s: target not present\n", e.Source, e.Target)
		case err != nil:
			fmt.Fprintf(os.Stderr, "error %s -> %s: %v\n", e.Source, e.Target, err)

			failed++
		case synced:
			fmt.Printf("synced %s -> %s\n", e.Source, e.Target)
		default:
			fmt.Printf("up to date %s -> %s\n", e.Source, e.Target)
		}

		if *e != before {
			dirty = true // first-sync id and/or refs hash recorded
		}
	}

	if dirty {
		if err := cfg.Save(); err != nil {
			return fmt.Errorf("save config: %w", err)
		}
	}

	if failed > 0 {
		return fmt.Errorf("%d of %d entries failed", failed, len(cfg.Sync))
	}

	return nil
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

	cfg, err := bk.Load()
	if err != nil {
		return err
	}

	// Pure config: the target is initialized on first sync, so it need not be
	// present now.
	if err := cfg.Add(source, target); err != nil {
		return err
	}

	if err := cfg.Save(); err != nil {
		return err
	}

	fmt.Printf("added %s -> %s (run 'bk sync' to back up)\n", source, target)

	return nil
}

func restoreCmd(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("restore", flag.ExitOnError)
	_ = fs.Parse(args) // flag.ExitOnError handles parse errors

	if fs.NArg() != 2 {
		fmt.Fprintln(os.Stderr, "usage: bk restore <backup-dir> <restore-path>")
		return errUsage
	}

	if err := bk.Restore(ctx, fs.Arg(0), fs.Arg(1)); err != nil {
		return err
	}

	fmt.Printf("restored %s -> %s\n", fs.Arg(0), fs.Arg(1))

	return nil
}

// run dispatches a command and returns an error; errUsage means usage was
// already printed.
func run(ctx context.Context, args []string) error {
	if len(args) < 1 {
		return dashboard(ctx, os.Stdout)
	}

	cmd, rest := args[0], args[1:]
	switch cmd {
	case "sync":
		return syncCmd(ctx, rest)
	case "add":
		return addCmd(rest)
	case "status":
		return statusCmd(ctx, rest)
	case "restore":
		return restoreCmd(ctx, rest)
	default:
		usage()
		return errUsage
	}
}

func main() {
	os.Exit(bkMain(os.Args[1:]))
}

// bkMain runs a command and returns the process exit code. It is split from main
// so deferred cleanup (signal handling) runs before os.Exit.
func bkMain(args []string) int {
	// Cancel in-flight git operations on the first Ctrl-C / SIGTERM.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	switch err := run(ctx, args); {
	case err == nil:
		return 0
	case errors.Is(err, errUsage):
		return 2
	default:
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
}
