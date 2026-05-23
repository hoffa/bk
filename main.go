package main

import (
	"flag"
	"fmt"
	"os"
)

func usage() {
	fmt.Fprint(os.Stderr, `usage: bk <command> [args]

commands:
  sync [-name <name>] <repo-path> <backup-dir>   back up a repo into a versioned backup dir
  restore <backup-dir> <restore-path>            restore a backup's latest version

  create-bundle   -repo <path> -out <bundle>     create and verify a single bundle (low-level)
  restore-bundle  -bundle <bundle> -to <path>    verify and restore a single bundle (low-level)

run "bk <command> -h" for command flags
`)
}

func syncCmd(args []string) error {
	fs := flag.NewFlagSet("sync", flag.ExitOnError)
	name := fs.String("name", "", "backup display name, set on first sync (default: backup dir name)")
	fs.Parse(args)

	if fs.NArg() != 2 {
		fmt.Fprintln(os.Stderr, "usage: bk sync [-name <name>] <repo-path> <backup-dir>")
		os.Exit(2)
	}
	return syncBackup(fs.Arg(0), fs.Arg(1), *name)
}

func restoreCmd(args []string) error {
	fs := flag.NewFlagSet("restore", flag.ExitOnError)
	fs.Parse(args)

	if fs.NArg() != 2 {
		fmt.Fprintln(os.Stderr, "usage: bk restore <backup-dir> <restore-path>")
		os.Exit(2)
	}
	return restoreBackup(fs.Arg(0), fs.Arg(1))
}

func createBundleCmd(args []string) error {
	fs := flag.NewFlagSet("create-bundle", flag.ExitOnError)
	repo := fs.String("repo", ".", "path to the repository to bundle")
	out := fs.String("out", "", "path to write the bundle to")
	fs.Parse(args)

	if *out == "" {
		fs.Usage()
		os.Exit(2)
	}
	return createBundle(*repo, *out)
}

func restoreBundleCmd(args []string) error {
	fs := flag.NewFlagSet("restore-bundle", flag.ExitOnError)
	bundle := fs.String("bundle", "", "path to the bundle to restore")
	to := fs.String("to", "", "path to restore the bundle into (must not exist)")
	fs.Parse(args)

	if *bundle == "" || *to == "" {
		fs.Usage()
		os.Exit(2)
	}
	return restoreBundle(*bundle, *to)
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
	case "create-bundle":
		err = createBundleCmd(args)
	case "restore-bundle":
		err = restoreBundleCmd(args)
	default:
		usage()
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
