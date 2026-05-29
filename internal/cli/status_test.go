package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hoffa/bk/internal/bk"
)

func TestEvalAll(t *testing.T) {
	useTempConfig(t)
	repo := initRepo(t)
	target := filepath.Join(t.TempDir(), "backup")

	if err := run(t.Context(), []string{"add", repo, target}); err != nil {
		t.Fatal(err)
	}

	statuses, err := evalAll(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	if len(statuses) != 1 || statuses[0].State != bk.StateUnsynced {
		t.Fatalf("statuses = %+v, want one never-synced", statuses)
	}
}

func TestPrintStatus(t *testing.T) {
	var buf bytes.Buffer

	statuses := []bk.Status{
		{Entry: bk.Entry{Source: "/a", Target: "/b", ID: "0123456789abcdef0123"}, State: bk.StateSynced, Present: true, Versions: 3},
		{Entry: bk.Entry{Source: "/c", Target: "/d", ID: "feed"}, State: bk.StateUnsynced},
	}
	if err := printStatus(&buf, statuses); err != nil {
		t.Fatal(err)
	}

	out := buf.String()
	// Headerless TSV: full id, tab-separated fields.
	for _, want := range []string{"0123456789abcdef0123", "SYNCED_ONLINE", "NEW", "/a", "/d"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}

	if strings.Contains(out, "SOURCE") {
		t.Errorf("expected no header row:\n%s", out)
	}

	// Column order: id, state, source, target, last sync.
	if !strings.Contains(out, "0123456789abcdef0123\tSYNCED_ONLINE\t/a\t/b\t") {
		t.Errorf("unexpected column order:\n%s", out)
	}
}

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
