package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/hoffa/bk/internal/bk"
)

func TestStatusCode(t *testing.T) {
	cases := []struct {
		s       bk.State
		present bool
		want    string
	}{
		{bk.StateSynced, true, "SYNCED_ONLINE"},
		{bk.StateSynced, false, "SYNCED_OFFLINE"},
		{bk.StateStale, true, "STALE_ONLINE"},
		{bk.StateStale, false, "STALE_OFFLINE"},
		{bk.StateUnsynced, true, "NEW"},
		{bk.StateError, true, "ERROR"},
	}
	for _, c := range cases {
		if got := statusCode(c.s, c.present); got != c.want {
			t.Errorf("statusCode(%s, present=%v) = %q, want %q", c.s.Label(), c.present, got, c.want)
		}
	}
}

func TestStatusDot(t *testing.T) {
	// Every state renders the single-cell dot glyph.
	for _, s := range []bk.State{bk.StateSynced, bk.StateStale, bk.StateUnsynced, bk.StateError} {
		d := statusDot(s, true)
		if !strings.Contains(d, statusDotChar) {
			t.Errorf("dot for %s missing glyph: %q", s.Label(), d)
		}

		if w := lipgloss.Width(d); w != 1 {
			t.Errorf("dot for %s visible width = %d, want 1", s.Label(), w)
		}
	}

	// An absent (offline) target dims the dot, so it differs from the online one.
	if statusDot(bk.StateSynced, true) == statusDot(bk.StateSynced, false) {
		t.Error("offline dot should render faint, differently from online")
	}
}

func TestDashboardNonTTYStatus(t *testing.T) {
	useTempConfig(t)
	// A non-terminal writer prints a one-shot status snapshot (no TUI, no sync).
	var buf bytes.Buffer
	if isTerminal(&buf) {
		t.Fatal("bytes.Buffer should not be a terminal")
	}

	if err := dashboard(t.Context(), &buf); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(buf.String(), "no backups configured") {
		t.Fatalf("unexpected output:\n%s", buf.String())
	}

	// With an entry, it prints the status table and does not sync.
	repo := initRepo(t)

	target := filepath.Join(t.TempDir(), "backup")
	if err := addCmd(t.Context(), []string{repo, target}); err != nil {
		t.Fatal(err)
	}

	buf.Reset()

	if err := dashboard(t.Context(), &buf); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(buf.String(), target) {
		t.Errorf("status missing target:\n%s", buf.String())
	}
	// The dashboard is read-only: it must not have created/synced the target.
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Error("dashboard should not have created the target")
	}
}
