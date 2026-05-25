// Command bk makes versioned, verifiable backups of git repositories using git
// bundles. It registers repo -> backup-dir pairs in a global config, syncs them
// (skipping unchanged repos), and shows their currency in a live dashboard.
package main

import (
	"os"

	"github.com/hoffa/bk/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:]))
}
