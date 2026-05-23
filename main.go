package main

import (
	"flag"
	"fmt"
	"os"
)

func usage() {
	fmt.Fprint(os.Stderr, `usage: bk <command> [flags]

commands:
  create-bundle   -repo <path> -out <bundle>     create and verify a bundle from a repo
  restore-bundle  -bundle <bundle> -to <path>    verify a bundle and restore it to a new repo

run "bk <command> -h" for command flags
`)
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
