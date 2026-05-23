package main

import (
	"flag"
	"fmt"
	"os"
)

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
		os.Exit(2)
	}
	return syncBackup(fs.Arg(0), fs.Arg(1))
}

func restoreCmd(args []string) error {
	fs := flag.NewFlagSet("restore", flag.ExitOnError)
	_ = fs.Parse(args) // flag.ExitOnError handles parse errors

	if fs.NArg() != 2 {
		fmt.Fprintln(os.Stderr, "usage: bk restore <backup-dir> <restore-path>")
		os.Exit(2)
	}
	return restoreBackup(fs.Arg(0), fs.Arg(1))
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	cmd, args := os.Args[1], os.Args[2:]

	var err error
	switch cmd {
	case "sync":
		err = syncCmd(args)
	case "restore":
		err = restoreCmd(args)
	default:
		usage()
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
