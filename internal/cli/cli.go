// Package cli is the bk command-line front-end. It parses arguments, drives the
// core (internal/bk) per entry, and renders scriptable output. main is a thin
// shell over Main.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/term"

	"github.com/hoffa/bk/internal/bk"
	"github.com/hoffa/bk/internal/crypt"
)

// errUsage signals a usage error: the message has already been printed and the
// process should exit with status 2.
var errUsage = errors.New("usage")

// errReported signals failure that has already been communicated through the
// command's own output (e.g. per-entry ERROR rows from `bk sync`): exit non-zero
// without printing anything further.
var errReported = errors.New("reported")

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
		{"add", "<repo-path> <backup-dir>", "register a repo -> backup-dir pair", addCmd},
		{"sync", "[<id>]", "back up configured repos, or one by id prefix", syncCmd},
		{"status", "", "show the state of every backup", statusCmd},
		{"rm", "[<id>]", "remove a backup from the config (id from 'bk status')", rmCmd},
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

// optionalID parses a command that accepts zero or one id prefix.
func optionalID(name string, raw []string) (string, error) {
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	_ = fs.Parse(raw) // flag.ExitOnError handles parse errors

	if fs.NArg() > 1 {
		fmt.Fprintf(os.Stderr, "usage: bk %s\n", usageLine(name))
		return "", errUsage
	}

	if fs.NArg() == 0 {
		return "", nil
	}

	return fs.Arg(0), nil
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
	id, err := optionalID("sync", args)
	if err != nil {
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

	entries := make([]*bk.Entry, 0, len(cfg.Sync))
	if id != "" {
		e, err := cfg.Match(id)
		if err != nil {
			return err
		}

		entries = append(entries, e)
	} else {
		for i := range cfg.Sync {
			entries = append(entries, &cfg.Sync[i])
		}
	}

	// Sync each entry, emitting "<id>\t<status>\t<msg>" TSV like `bk status` (the
	// scriptable surface). status uses the same vocabulary as `bk status`: a
	// reached target is reported online, an absent one (unplugged drive) falls
	// back to its cached offline verdict -- not a failure. A real failure is
	// ERROR with the detail in msg (one-lined so it can't break the TSV); it
	// doesn't stop the rest, but makes the run exit non-zero.
	var (
		failed int
		dirty  bool
	)

	oneLine := strings.NewReplacer("\t", " ", "\n", " ", "\r", " ")

	var password *string

	newKeyring := func() (crypt.Keyring, error) {
		if password == nil {
			pw, err := readNewPassword()
			if err != nil {
				return crypt.Keyring{}, err
			}

			password = &pw
		}

		return crypt.NewKeyring(*password)
	}

	for _, e := range entries {
		firstSync := e.Backup == nil
		synced, err := bk.Sync(ctx, e, newKeyring)

		var status, msg string

		switch {
		case err != nil && !errors.Is(err, bk.ErrTargetAbsent):
			status, msg = "ERROR", oneLine.Replace(err.Error())
			failed++
		default:
			// Success, or target absent: report the entry's resulting status the
			// same way `bk status` would (online if reached, the cached offline
			// verdict if it wasn't present).
			s := bk.Eval(ctx, *e)
			status = statusCode(s.State, s.Present)
		}

		fmt.Printf("%s\t%s\t%s\n", e.ID, status, msg)

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
		return errReported
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

func rmCmd(_ context.Context, args []string) error {
	id, err := optionalID("rm", args)
	if err != nil {
		return err
	}

	cfg, err := bk.Load()
	if err != nil {
		return err
	}

	if id == "" {
		switch len(cfg.Sync) {
		case 0:
			return errors.New("no backups configured")
		case 1:
			id = cfg.Sync[0].ID
		default:
			return errors.New("more than one backup configured; pass an id from 'bk status'")
		}
	}

	e, err := cfg.Match(id)
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
		usage()
		return errUsage
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
	case errors.Is(err, errReported):
		return 1
	default:
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
}
