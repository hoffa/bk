package cli

import (
	"context"
	"fmt"
	"io"
	"time"

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

// printStatus writes entry statuses as headerless TSV (the TUI is the pretty
// view; this is the scriptable one). No header means every line is a record, so
// cut/awk/read consume it without skipping. Columns are, in order: id, source,
// target, state, last sync (RFC 3339, or empty if never).
func printStatus(w io.Writer, statuses []bk.Status) error {
	for _, s := range statuses {
		var last string
		if !s.LastSync.IsZero() {
			last = s.LastSync.Format(time.RFC3339)
		}

		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", s.ID, s.Source, s.Target, statusCode(s.State, s.Present), last)
	}

	return nil
}
