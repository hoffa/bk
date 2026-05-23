package main

import (
	"fmt"
	"os"
)

func usage() {
	fmt.Fprintf(os.Stderr, `usage: bk <command> [args]

commands:
  create-bundle <repo-path> <bundle-path>     create and verify a bundle from a repo
  restore-bundle <bundle-path> <restore-path>  verify a bundle and restore it to a new repo
`)
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
		if len(args) != 2 {
			usage()
			os.Exit(2)
		}
		err = createBundle(args[0], args[1])
	case "restore-bundle":
		if len(args) != 2 {
			usage()
			os.Exit(2)
		}
		err = restoreBundle(args[0], args[1])
	default:
		usage()
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
