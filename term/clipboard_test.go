package term

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestClipboardSeqIsOSC52OnTheClipboardSelection(t *testing.T) {
	seq, err := ClipboardSeq("hello")
	if err != nil {
		t.Fatalf("ClipboardSeq: %v", err)
	}
	want := "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte("hello")) + "\x1b\\"
	if seq != want {
		t.Errorf("seq = %q, want %q", seq, want)
	}
}

func TestClipboardSeqRefusesEmptyBecauseItWouldCLEAR(t *testing.T) {
	// Not a no-op: OSC 52 with an empty payload clears the clipboard on
	// most terminals, so "copy" on an empty selection would destroy
	// whatever the user had there, with no undo.
	if _, err := ClipboardSeq(""); err == nil {
		t.Fatal("an empty copy was accepted; it would have CLEARED the clipboard")
	}
}

func TestClipboardSeqRefusesOversizeRatherThanTruncating(t *testing.T) {
	// The bad failure: a terminal silently drops an oversize sequence,
	// so the clipboard keeps its old contents while the app says
	// "copied". Truncating instead would be the same shape — 74KB of a
	// 100KB document with nothing to say the tail is missing.
	big := strings.Repeat("x", ClipboardLimit) // base64 is 4/3, so this is over
	seq, err := ClipboardSeq(big)
	if err == nil {
		t.Fatalf("oversize copy accepted, produced %d bytes", len(seq))
	}
	if seq != "" {
		t.Error("an error was returned WITH a sequence; a caller writing it anyway would truncate")
	}
	if !strings.Contains(err.Error(), "too large") {
		t.Errorf("error %q does not say why", err)
	}
}

func TestClipboardSeqAcceptsTheLargestThingThatFits(t *testing.T) {
	// The other side of the boundary. Without this, a limit of 0 would
	// pass the test above and refuse every copy in the app.
	n := ClipboardLimit / 4 * 3 // exactly ClipboardLimit base64 bytes
	if _, err := ClipboardSeq(strings.Repeat("x", n)); err != nil {
		t.Fatalf("a payload at the limit was refused: %v", err)
	}
}

func TestClipboardSeqRoundTrips(t *testing.T) {
	// Multi-byte and control content both, because a document being
	// copied out of the designer is markup with newlines in it.
	text := "<Text>héllo</Text>\n<Button/>\n"
	seq, err := ClipboardSeq(text)
	if err != nil {
		t.Fatal(err)
	}
	payload := strings.TrimSuffix(strings.TrimPrefix(seq, "\x1b]52;c;"), "\x1b\\")
	back, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		t.Fatalf("payload is not base64: %v", err)
	}
	if string(back) != text {
		t.Errorf("round trip = %q, want %q", back, text)
	}
}

func TestClipboardCaveatNamesTheMultiplexer(t *testing.T) {
	// tmux and GNU screen swallow OSC 52 by default, and a user inside
	// one otherwise sees a confirmation and an unchanged clipboard with
	// nothing to connect the two.
	t.Setenv("STY", "")
	t.Setenv("TMUX", "/tmp/tmux-1000/default,123,0")
	if got := ClipboardCaveat(); !strings.Contains(got, "tmux") {
		t.Errorf("inside tmux the caveat is %q, want it to name tmux", got)
	}

	t.Setenv("TMUX", "")
	t.Setenv("STY", "1234.pts-0.host")
	if got := ClipboardCaveat(); !strings.Contains(got, "screen") {
		t.Errorf("inside screen the caveat is %q, want it to name screen", got)
	}

	// And the discriminating half: no multiplexer, no caveat. Without
	// this, a function returning a constant string would pass above and
	// put a false warning on every copy in a plain terminal.
	t.Setenv("TMUX", "")
	t.Setenv("STY", "")
	if got := ClipboardCaveat(); got != "" {
		t.Errorf("outside a multiplexer the caveat is %q, want empty", got)
	}
}

func TestClipboardCaveatKeysOffSTYNotTERM(t *testing.T) {
	// TERM=screen-256color is what TMUX sets too, so keying the GNU
	// screen branch off TERM would name the wrong multiplexer for most
	// of the people who saw the message.
	t.Setenv("TMUX", "")
	t.Setenv("STY", "")
	t.Setenv("TERM", "screen-256color")
	if got := ClipboardCaveat(); got != "" {
		t.Errorf("TERM=screen-256color alone produced %q; TERM is not the signal", got)
	}
}
