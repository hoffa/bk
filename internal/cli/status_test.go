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
		{Entry: bk.Entry{Source: "/c", Target: "/d"}, State: bk.StateUnsynced},
	}
	if err := printStatus(&buf, statuses); err != nil {
		t.Fatal(err)
	}

	out := buf.String()
	for _, want := range []string{"SOURCE", "TARGET", "0123456789ab", "OK", "NEW", "/a", "/d"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}

	// Short id is truncated to 12 chars (full id would be 20 here).
	if strings.Contains(out, "0123456789abcdef0123") {
		t.Errorf("expected truncated id, got full:\n%s", out)
	}
}

func TestShort(t *testing.T) {
	if got := short("abcdefgh", 3); got != "abc" {
		t.Errorf("short = %q, want abc", got)
	}

	if got := short("ab", 5); got != "ab" {
		t.Errorf("short = %q, want ab", got)
	}
}
