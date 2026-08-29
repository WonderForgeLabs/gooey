package main

// Replay of a pty transcript through render.Screen — the "extract the
// final frame" half of docs/learn/howto/howto-testing.md.
//
// NOT by searching the log for the last \x1b[H. The flush is incremental:
// only a FULL frame starts with a cursor-home, and after the first one the
// log holds differences, so hunting the bytes finds the first frame or
// nothing (issue #183). render.Screen is an io.Writer that models a
// terminal; feeding it the whole log and asking what is on screen is the
// only honest read.
//
// Driven by TestReplayTranscript, which is skipped unless a transcript has
// been produced — capturing one needs a pty, which `go test` has not.

import (
	"os"
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey/render"
)

// TestReplayTranscript renders a captured session and prints the final
// frame. It is a TOOL rather than an assertion: the assertions about the
// dock, the menus and the browser are the Composer-level tests, which run
// everywhere. This exists so a human (or a report) can see the real
// binary's output.
func TestReplayTranscript(t *testing.T) {
	path := os.Getenv("GOOEY_TRANSCRIPT")
	if path == "" {
		t.Skip("set GOOEY_TRANSCRIPT to a script(1) log to replay it")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	cols, rows := 150, 44
	if v := os.Getenv("GOOEY_COLS"); v != "" {
		cols = atoiOr(v, cols)
	}
	if v := os.Getenv("GOOEY_ROWS"); v != "" {
		rows = atoiOr(v, rows)
	}
	// Cut at the LEAVE-ALTERNATE-SCREEN, which Screen.Restore writes
	// (term/term.go:240). Without this the replay ends on the shell prompt
	// the terminal returns to, and the transcript reads as an empty app —
	// the app's whole session happened on the alternate screen and was
	// discarded one byte before the end.
	if i := strings.LastIndex(string(b), "\x1b[?1049l"); i >= 0 {
		b = b[:i]
	}
	sc := render.NewScreen(cols, rows)
	if _, err := sc.Write(b); err != nil {
		t.Fatalf("replay: %v", err)
	}
	var sb strings.Builder
	for y := 0; y < rows; y++ {
		sb.WriteString(strings.TrimRight(sc.Row(y), " "))
		sb.WriteByte('\n')
	}
	t.Logf("final frame (%dx%d):\n%s", cols, rows, sb.String())
}

func atoiOr(s string, def int) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return def
		}
		n = n*10 + int(r-'0')
	}
	if n == 0 {
		return def
	}
	return n
}
