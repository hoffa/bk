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
		{bk.StateSynced, true, "OK"},
		{bk.StateSynced, false, "OK?"},
		{bk.StateStale, true, "STALE"},
		{bk.StateStale, false, "STALE?"}, // "?" = unverified (offline)
		{bk.StateUnsynced, true, "NEW"},
		{bk.StateError, true, "ERROR"},
	}
	for _, c := range cases {
		if got := statusCode(c.s, c.present); got != c.want {
			t.Errorf("statusCode(%s, present=%v) = %q, want %q", c.s.Label(), c.present, got, c.want)
		}
	}
}

func TestBadge(t *testing.T) {
	// Every badge has a fixed visible width (lipgloss.Width ignores any color
	// escapes) and carries its code text.
	for _, s := range []bk.State{bk.StateSynced, bk.StateStale, bk.StateUnsynced, bk.StateError} {
		b := badge(s, true)
		if w := lipgloss.Width(b); w != badgeWidth {
			t.Errorf("badge for %s visible width = %d, want %d", s.Label(), w, badgeWidth)
		}
	}

	if !strings.Contains(badge(bk.StateSynced, true), "OK") {
		t.Errorf("badge missing code text: %q", badge(bk.StateSynced, true))
	}

	if !strings.Contains(badge(bk.StateError, true), "ERROR") {
		t.Errorf("error badge missing code text: %q", badge(bk.StateError, true))
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
	if err := addCmd([]string{repo, target}); err != nil {
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
