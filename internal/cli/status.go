package cli

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"text/tabwriter"

	"github.com/hoffa/bk/internal/bk"
)

// evalAll loads the config and evaluates every entry. It's the front-end's
// convenience over the core's per-entry bk.Eval, used by `bk status` and the
// dashboard.
func evalAll(ctx context.Context) ([]bk.Status, error) {
	cfg, err := bk.Load()
	if err != nil {
		return nil, err
	}

	out := make([]bk.Status, 0, len(cfg.Sync))
	for _, e := range cfg.Sync {
		out = append(out, bk.Eval(ctx, e))
	}

	return out, nil
}

// short returns the first n characters of s.
func short(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}

	return s
}

// printStatus renders entry statuses as an aligned table.
func printStatus(w io.Writer, statuses []bk.Status) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	// Buffered writes; any error surfaces on Flush.
	_, _ = fmt.Fprintln(tw, "ID\tSOURCE\tTARGET\tSTATE\tVERSIONS\tLAST SYNC")

	for _, s := range statuses {
		id, versions, last := "-", "-", "-"
		if s.ID != "" {
			id = short(s.ID, 12)
		}

		if s.Versions > 0 {
			versions = strconv.Itoa(s.Versions)
		}

		if !s.LastSync.IsZero() {
			last = s.LastSync.Format("2006-01-02 15:04:05Z")
		}

		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", id, s.Source, s.Target, statusCode(s.State, s.Present), versions, last)
	}

	return tw.Flush()
}
