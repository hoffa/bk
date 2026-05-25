// Package cli is the bk command-line and dashboard front-end. It parses
// arguments, drives the core (internal/bk) per entry, and renders results. main
// is a thin shell over Main.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"image/color"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"charm.land/lipgloss/v2"
	"golang.org/x/term"

	"github.com/hoffa/bk/internal/bk"
)

// errUsage signals a usage error: the message has already been printed and the
// process should exit with status 2.
var errUsage = errors.New("usage")

// command is one subcommand. args is the usage hint shown after the name; run
// receives the arguments after the command name.
type command struct {
	name    string
	args    string
	summary string
	run     func(ctx context.Context, args []string) error
}

// commands is the dispatch + help table; usage() is generated from it. It's a
// function (not a var) to avoid a package init cycle: the command funcs
// transitively reference the table via fixedArgs.
func commands() []command {
	return []command{
		{"init", "[--force]", "set the backup password (run once)", initCmd},
		{"add", "<repo-path> <backup-dir>", "register a repo -> backup-dir pair", addCmd},
		{"sync", "", "back up every configured repo", syncCmd},
		{"status", "", "show the state of every backup", statusCmd},
		{"remove", "<id>", "remove a backup from the config (id from 'bk status')", removeCmd},
		{"restore", "<backup-dir> <restore-path>", "restore a backup's latest", restoreCmd},
	}
}

func usage() {
	fmt.Fprint(os.Stderr, "usage: bk <command> [args]\n\ncommands:\n")

	for _, c := range commands() {
		fmt.Fprintf(os.Stderr, "  %-36s %s\n", strings.TrimSpace(c.name+" "+c.args), c.summary)
	}
}

// fixedArgs parses raw (catching unknown flags / -h) and requires exactly n
// positional arguments, printing the command's usage line otherwise.
func fixedArgs(name string, raw []string, n int) ([]string, error) {
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	_ = fs.Parse(raw) // flag.ExitOnError handles parse errors

	if fs.NArg() != n {
		fmt.Fprintf(os.Stderr, "usage: bk %s\n", usageLine(name))
		return nil, errUsage
	}

	return fs.Args(), nil
}

// usageLine returns "<name> <args>" for a command, the single source of its hint.
func usageLine(name string) string {
	for _, c := range commands() {
		if c.name == name {
			return strings.TrimSpace(name + " " + c.args)
		}
	}

	return name
}

// colorEnabled reports whether to emit color to w (a terminal, NO_COLOR unset).
func colorEnabled(w io.Writer) bool {
	return os.Getenv("NO_COLOR") == "" && isTerminal(w)
}

// paint colors s for w when color is enabled, otherwise returns it unchanged.
func paint(w io.Writer, c color.Color, s string) string {
	if !colorEnabled(w) {
		return s
	}

	return lipgloss.NewStyle().Foreground(c).Render(s)
}

// readPassword returns the backup password from $BK_PASSWORD (handy for scripts
// and tests), otherwise it prompts on the terminal without echoing.
func readPassword(prompt string) (string, error) {
	if p := os.Getenv("BK_PASSWORD"); p != "" {
		return p, nil
	}

	fmt.Fprint(os.Stderr, prompt)

	b, err := term.ReadPassword(int(os.Stdin.Fd()))

	fmt.Fprintln(os.Stderr)

	return string(b), err
}

// readNewPassword prompts for a new backup password, confirmed twice. Any
// non-empty password is accepted.
func readNewPassword() (string, error) {
	if p := os.Getenv("BK_PASSWORD"); p != "" {
		return p, nil
	}

	pw, err := readPassword("Set a backup password: ")
	if err != nil {
		return "", err
	}

	if pw == "" {
		return "", errors.New("password cannot be empty")
	}

	again, err := readPassword("Confirm password: ")
	if err != nil {
		return "", err
	}

	if pw != again {
		return "", errors.New("passwords do not match")
	}

	return pw, nil
}

func initCmd(_ context.Context, args []string) error {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	force := fs.Bool("force", false, "replace an existing key (existing backups become unreadable)")
	_ = fs.Parse(args) // flag.ExitOnError handles parse errors

	if fs.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "usage: bk init [--force]")
		return errUsage
	}

	cfg, err := bk.Load()
	if err != nil {
		return err
	}

	if cfg.HasKey() && !*force {
		return errors.New("already initialized (use --force to set a new password, abandoning existing backups)")
	}

	if cfg.HasKey() {
		fmt.Fprintln(os.Stderr, paint(os.Stderr, lipgloss.Yellow, "WARNING:")+" --force creates a NEW key. Existing backups stay locked to the")
		fmt.Fprintln(os.Stderr, "OLD password and bk will not use them; wipe or replace their targets to")
		fmt.Fprintln(os.Stderr, "back up again under the new password. This cannot be undone.")
	}

	pw, err := readNewPassword()
	if err != nil {
		return err
	}

	if err := cfg.SetPassword(pw); err != nil {
		return err
	}

	if err := cfg.Save(); err != nil {
		return err
	}

	fmt.Println("Initialized. Backups are encrypted with this password.")
	fmt.Println("Save it somewhere safe -- if you lose it, backups cannot be recovered.")

	return nil
}

func addCmd(ctx context.Context, args []string) error {
	a, err := fixedArgs("add", args, 2)
	if err != nil {
		return err
	}

	source, err := filepath.Abs(a[0])
	if err != nil {
		return err
	}

	target, err := filepath.Abs(a[1])
	if err != nil {
		return err
	}

	// Fail fast if the source isn't a git repo, rather than at the first sync.
	if err := bk.CheckRepo(ctx, source); err != nil {
		return err
	}

	cfg, err := bk.Load()
	if err != nil {
		return err
	}

	if !cfg.HasKey() {
		return errors.New("no backup password set; run 'bk init' first")
	}

	// Pure config: the target is initialized on first sync, so it need not be
	// present now.
	id, err := cfg.Add(source, target)
	if err != nil {
		return err
	}

	if err := cfg.Save(); err != nil {
		return err
	}

	fmt.Println(id)

	return nil
}

func syncCmd(ctx context.Context, args []string) error {
	if _, err := fixedArgs("sync", args, 0); err != nil {
		return err
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

		firstSync := e.Backup == nil
		synced, err := bk.Sync(ctx, e, cfg.Key)

		switch {
		case errors.Is(err, bk.ErrTargetAbsent):
			fmt.Printf("%s %s -> %s: target not present\n", paint(os.Stdout, lipgloss.BrightBlack, "skip"), e.Source, e.Target)
		case err != nil:
			fmt.Fprintf(os.Stderr, "%s %s -> %s: %v\n", paint(os.Stderr, lipgloss.Red, "error"), e.Source, e.Target, err)

			failed++
		case synced:
			fmt.Printf("%s %s -> %s\n", paint(os.Stdout, lipgloss.Green, "synced"), e.Source, e.Target)
		default:
			fmt.Printf("%s %s -> %s\n", paint(os.Stdout, lipgloss.BrightBlack, "up to date"), e.Source, e.Target)
		}

		// A first sync records the backup cache; a new version updates it.
		if err == nil && (firstSync || synced) {
			dirty = true
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

func statusCmd(ctx context.Context, args []string) error {
	if _, err := fixedArgs("status", args, 0); err != nil {
		return err
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

func removeCmd(_ context.Context, args []string) error {
	a, err := fixedArgs("remove", args, 1)
	if err != nil {
		return err
	}

	cfg, err := bk.Load()
	if err != nil {
		return err
	}

	e, err := cfg.Match(a[0])
	if err != nil {
		return err
	}

	// Config entry only; the backup data on the target is left in place. Silent
	// on success.
	cfg.Remove(e.ID)

	return cfg.Save()
}

func restoreCmd(ctx context.Context, args []string) error {
	a, err := fixedArgs("restore", args, 2)
	if err != nil {
		return err
	}

	pw, err := readPassword("Enter backup password: ")
	if err != nil {
		return err
	}

	if err := bk.Restore(ctx, a[0], a[1], pw); err != nil {
		return err
	}

	fmt.Printf("restored %s -> %s\n", a[0], a[1])

	return nil
}

// run dispatches a command and returns an error; errUsage means usage was
// already printed.
func run(ctx context.Context, args []string) error {
	if len(args) < 1 {
		return dashboard(ctx, os.Stdout)
	}

	name, rest := args[0], args[1:]
	for _, c := range commands() {
		if c.name == name {
			return c.run(ctx, rest)
		}
	}

	usage()

	return errUsage
}

// Main runs a bk invocation and returns the process exit code. It owns signal
// handling so main can be a one-line shell.
func Main(args []string) int {
	// Cancel in-flight git operations on the first Ctrl-C / SIGTERM.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	switch err := run(ctx, args); {
	case err == nil:
		return 0
	case errors.Is(err, errUsage):
		return 2
	default:
		fmt.Fprintln(os.Stderr, paint(os.Stderr, lipgloss.Red, "error:"), err)
		return 1
	}
}
