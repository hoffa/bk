package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
)

// errUsage signals a usage error: the message has already been printed and the
// process should exit with status 2.
var errUsage = errors.New("usage")

func usage() {
	fmt.Fprint(os.Stderr, `usage: bk <command> <args>

commands:
  sync <repo-path> <backup-dir>         back up a repo into a versioned backup dir
  restore <backup-dir> <restore-path>   restore a backup's latest version
`)
}

func syncCmd(args []string) error {
	fs := flag.NewFlagSet("sync", flag.ExitOnError)
	_ = fs.Parse(args) // flag.ExitOnError handles parse errors

	if fs.NArg() != 2 {
		fmt.Fprintln(os.Stderr, "usage: bk sync <repo-path> <backup-dir>")
		return errUsage
	}
	return syncBackup(fs.Arg(0), fs.Arg(1))
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
		usage()
		return errUsage
	}

	cmd, rest := args[0], args[1:]
	switch cmd {
	case "sync":
		return syncCmd(rest)
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
