package cli

import "testing"

func TestMainExitCodes(t *testing.T) {
	useTempConfig(t)

	if code := Main([]string{"bogus"}); code != 2 {
		t.Errorf("unknown command exit = %d, want 2", code)
	}

	// Empty config: status prints a hint and succeeds.
	if code := Main([]string{"status"}); code != 0 {
		t.Errorf("empty status exit = %d, want 0", code)
	}
}
