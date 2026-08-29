package term

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// escapesWritten runs fn against a Screen whose "tty" is a real file, and
// returns everything the Screen wrote to it. A regular file rather than a
// pipe because Restore CLOSES the tty, and a pipe whose reader has not
// drained would block the close.
func escapesWritten(t *testing.T, fn func(*Screen)) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tty")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	fn(FromFile(f))
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestEnablePasteSetsMode2004(t *testing.T) {
	got := escapesWritten(t, func(s *Screen) { s.EnablePaste() })
	if got != "\x1b[?2004h" {
		t.Errorf("EnablePaste wrote %q, want the DECSET 2004 sequence", got)
	}
}

// THE UNRECOVERABLE MISTAKE. Leaving bracketed paste on after exit
// corrupts the user's SHELL: every paste they make afterwards is
// preceded by a literal "[200~" and followed by "[201~", on a command
// line that has nothing to do with the app that did it and no way to
// connect the two. Restore disables it UNCONDITIONALLY — not "if we
// enabled it" — for the same reason it disables mouse reporting that
// way, and this test is the pin on both.
func TestRestoreDisablesPasteAndMouseUnconditionally(t *testing.T) {
	// Note what is NOT called here: neither EnablePaste nor EnableMouse.
	// A Restore that only undid what it had turned on would pass a test
	// that enabled them first, and would still strand a terminal whose
	// mode was set before this Screen existed (a suspend/resume, a
	// crashed predecessor, a hosted guest).
	got := escapesWritten(t, func(s *Screen) { s.Restore() })

	for _, want := range []string{"\x1b[?2004l", "\x1b[?1000l"} {
		if !strings.Contains(got, want) {
			t.Errorf("Restore did not write %q; the terminal is left in that mode after exit.\nwrote: %q", want, got)
		}
	}
	// And the ORDER is part of the contract: the escapes have to go out
	// while the fd is still open, so anything written after the close is
	// lost. Reaching the alt-screen exit proves the paste disable
	// preceded it.
	if !strings.Contains(got, "\x1b[?1049l") {
		t.Errorf("Restore did not leave the alternate screen; wrote %q", got)
	}
}
