package input

import (
	"strings"
	"testing"
)

// The LAST-CHANCE contract, which decodeidle_test.go could not observe
// even in principle.
//
// That file's exhaustive sweep is over 1- and 2-byte inputs, and the
// rule that strands lives at 3–5 bytes: splitPasteMarker has a
// three-byte floor. So nothing in the suite could see repeated-idle
// behaviour at all, and a buffer that waits under idle FOREVER looked
// exactly like one that waits correctly for one timeout.
//
// DecodeFinal is the second timeout expressed as an API. Under it the
// exception list shrinks from "split paste marker, or open paste" to
// "open paste", and that is the whole of the difference — pinned here in
// both directions, because a fix that removed the grace instead of
// bounding it would pass every liveness assertion in this file while
// reintroducing #419.

func assertFinalProgress(t *testing.T, b []byte) {
	t.Helper()
	_, n, ok := DecodeFinal(b)
	if n == 0 && !ok {
		t.Fatalf("DecodeFinal(%q) consumed nothing and produced nothing: two "+
			"escape timeouts have gone by with this buffer unchanged, so no "+
			"byte is coming that could resolve it and the drain loop stops "+
			"here permanently", b)
	}
}

// TestFinalDecodeResolvesTheTypedMarkerPrefix is the reported case, and
// it is a person at a keyboard rather than a terminal mid-write: press
// Esc, then type `[`, then `2`.
//
// Under idle those three bytes are a strict prefix of ESC [ 200 ~ and
// are held — correctly, for one timeout. Held past that, the Esc is
// never delivered, the next unrelated keystroke is absorbed into the CSI
// parse (ESC [ 2 a is one unmapped CSI, four bytes, no event at all),
// and DecodeEvents re-arms its escape timer every 40ms for the rest of
// the process's life. #440.
func TestFinalDecodeResolvesTheTypedMarkerPrefix(t *testing.T) {
	typed := []byte("\x1b[2")

	// The grace is real and still there — this is the half that must not
	// regress. Without it, #419: a split marker resolves to Esc and the
	// paste arrives as a burst of keystrokes.
	if _, n, ok := Decode(typed, true); n != 0 || ok {
		t.Fatalf("Decode(%q, idle=true) = (n=%d, ok=%v), want the paste grace "+
			"to hold it for the first timeout", typed, n, ok)
	}

	ev, n, ok := DecodeFinal(typed)
	if !ok || n != 1 {
		t.Fatalf("DecodeFinal(%q) = (n=%d, ok=%v), want the Esc key alone", typed, n, ok)
	}
	if !ev.IsKey() || ev.Key.Key != KeyEsc {
		t.Fatalf("DecodeFinal(%q) produced %#v, want Esc", typed, ev)
	}

	// Consuming ONLY the Esc is what makes the rest arrive as itself.
	// The user typed `[` and `2`; they come through as those two keys,
	// not swallowed with the escape.
	rest := typed[n:]
	for _, want := range []rune{'[', '2'} {
		ev, n, ok := DecodeFinal(rest)
		if !ok || !ev.IsKey() || ev.Key.Rune != want {
			t.Fatalf("draining %q gave (%#v, n=%d, ok=%v), want the %q key",
				rest, ev, n, ok, want)
		}
		rest = rest[n:]
	}
	if len(rest) != 0 {
		t.Errorf("%q left over after the drain", rest)
	}
}

// The liveness sweeps, run again through the stronger deadline. Every
// input that makes progress under idle must still make progress under
// final — the deadline only ever REMOVES reasons to wait — and the
// marker prefixes that idle is allowed to hold must now resolve too.
func TestFinalDecodeAlwaysMakesProgress(t *testing.T) {
	for i := range 256 {
		assertFinalProgress(t, []byte{byte(i)})
		for j := range 256 {
			assertFinalProgress(t, []byte{byte(i), byte(j)})
		}
	}
}

// Deeper than the exhaustive sweep can afford, over the bytes that
// actually build escape sequences — decodeidle_test.go's alphabet
// extended in BOTH directions: to five bytes, and by one byte.
//
// THE INHERITED ALPHABET COULD NOT SPELL THE THING THIS FILE IS ABOUT.
// It has no '2', and every strict prefix of \x1b[200~ / \x1b[201~ needs
// a '2' at index 2 — so `splitPasteMarker` returned true for **zero** of
// the sweep's ~1.1M inputs and the grace arm was unreachable at any
// length. Extending only the length moved the walk across the right
// range while leaving it unable to build anything in it, which is the
// same failure this PR diagnoses in the OLD two-byte sweep, one level
// up: a fixture that cannot express the bug. Adding '2' takes the hits
// from 0 to 4 and the cost from 16^5 to 17^5. Measured in review of
// #445.
func TestFinalDecodeMakesProgressOnNestedEscapes(t *testing.T) {
	alpha := []byte{0x1b, '[', 'O', '<', 'M', 'm', ';', '~', '0', '1', '2', 'a', 0x00, 0x7f, 0x80, 0xff, ' '}
	buf := make([]byte, 5)
	for _, v := range alpha {
		for _, w := range alpha {
			for _, x := range alpha {
				buf[0], buf[1], buf[2] = v, w, x
				assertFinalProgress(t, buf[:3])
				for _, y := range alpha {
					buf[3] = y
					assertFinalProgress(t, buf[:4])
					for _, z := range alpha {
						buf[4] = z
						assertFinalProgress(t, buf[:5])
					}
				}
			}
		}
	}
}

// TestFinalDecodeHoldsNoMarkerPrefix is the UPPER bound, the complement
// of TestTheIdleExceptionIsExactlyThePasteMarker next door.
//
// The walk there proves the idle exception is exactly the marker
// prefixes. This one proves that under final NONE of them is left: every
// input beginning ESC [ and continuing in parameter bytes resolves.
// Without it, a change that made DecodeFinal a synonym for Decode would
// turn no test in this file red except the named case above.
//
// NAMED FOR WHAT THE WALK CAN BUILD, which is narrower than "waits for
// nothing but an open paste" — it only ever appends 0x30–0x3f, so '~'
// never lands and \x1b[200~ is unreachable from here. The open-paste
// half is TestFinalDecodeStillWaitsForAnOpenPaste's job. Renamed in
// review of #445.
func TestFinalDecodeHoldsNoMarkerPrefix(t *testing.T) {
	var walk func(b []byte)
	walk = func(b []byte) {
		if len(b) > len(pasteStart) {
			return
		}
		if _, n, ok := DecodeFinal(b); n == 0 && !ok {
			t.Errorf("DecodeFinal(%q) waits for more bytes. Two escape timeouts "+
				"have already gone by unchanged, so nothing can arrive to resolve "+
				"it and every later keystroke strands behind it", b)
		}
		// Only parameter bytes keep decodeCSI in the arm that can wait.
		for c := 0x30; c < 0x40; c++ {
			walk(append(append([]byte(nil), b...), byte(c)))
		}
	}
	walk([]byte{0x1b, '['})
}

// The one exception final does NOT withdraw, and the floor that keeps
// the test above from passing by having deleted the paste support
// entirely. An OPEN paste — the six-byte marker complete, the payload's
// end marker not yet arrived — waits however long it takes, because
// delivering the prefix truncates the paste SILENTLY and a user who
// pastes 40KB and receives 8KB has no way to tell. A wedge is at least
// visible. See input/paste.go and input.DecodeFinal.
func TestFinalDecodeStillWaitsForAnOpenPaste(t *testing.T) {
	open := []byte(pasteStart + strings.Repeat("x", 64))
	if _, n, ok := DecodeFinal(open); n != 0 || ok {
		t.Fatalf("DecodeFinal on an open paste = (n=%d, ok=%v), want it to keep "+
			"waiting. Resolving it here delivers a truncated paste with nothing "+
			"to tell the user the rest was dropped", n, ok)
	}
	// And it completes normally once the end marker lands, so the wait
	// above is a wait rather than a wedge in the ordinary case.
	ev, _, ok := DecodeFinal(append(append([]byte(nil), open...), []byte(pasteEnd)...))
	if !ok || !ev.IsPaste() {
		t.Fatalf("the completed paste decoded as (%#v, ok=%v), want a paste event", ev, ok)
	}
}
