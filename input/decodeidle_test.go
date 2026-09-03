package input

import (
	"strings"
	"testing"
)

// The idle contract, and it is the decoder's liveness property rather
// than a decoding detail.
//
// DecodeEvents drains with `for len(pend) > 0 { … if n == 0 && !ok {
// return } }`, so a Decode that consumes nothing and reports nothing is
// the loop's ONLY "wait for more bytes" signal. When idle is true there
// is no more input coming — the escape timeout has already fired — so a
// sequence that still says "incomplete" strands the buffer: every later
// keystroke is appended behind it and never decoded. The app keeps
// painting and answering its control plane, and is permanently deaf.
//
// That is a liveness bug no decoding test can see, because every
// individual byte still decodes correctly. What is broken is that the
// loop cannot make progress, so the assertion has to be about progress:
// every input in THIS SWEEP'S RANGE must either consume a byte or
// produce an event under idle.
//
// The range is the scope, not a detail. Stated as "every non-empty
// input" — which it was — the claim is false, and contradicted by
// TestTheIdleExceptionIsExactlyThePasteMarker seventy lines below:
// splitPasteMarker holds 3-to-5-byte marker prefixes, and decodePaste
// holds an open paste indefinitely. What keeps the absolute true HERE is
// splitPasteMarker's three-byte floor, which puts both exceptions
// outside the 1- and 2-byte sweep by construction. See input.Decode's
// doc for the exception list and input.DecodeFinal for which half is
// bounded. Scoped in review of #445.
func assertProgress(t *testing.T, b []byte) {
	t.Helper()
	_, n, ok := Decode(b, true)
	if n == 0 && !ok {
		t.Fatalf("Decode(%q, idle=true) consumed nothing and produced nothing: "+
			"the decoder's drain loop reads this as \"incomplete\" and stops, "+
			"stranding every keystroke behind it", b)
	}
}

// TestIdleDecodeAlwaysMakesProgress is exhaustive over the short
// sequences, which is where the escape prefixes live. Anything longer is
// reached through these.
func TestIdleDecodeAlwaysMakesProgress(t *testing.T) {
	for i := range 256 {
		assertProgress(t, []byte{byte(i)})
		for j := range 256 {
			assertProgress(t, []byte{byte(i), byte(j)})
		}
	}
}

// TestIdleDecodeMakesProgressOnNestedEscapes goes deeper than the
// exhaustive sweep can afford to, over the bytes that actually build
// escape sequences.
//
// Depth is the point: decodeEsc's alt+key branch recurses, so a strand
// can hide one level down where a two-byte sweep cannot reach it —
// ESC ESC <undecodable> resolves through three frames. Full coverage to
// four bytes is 4 billion calls; this alphabet is every byte with a role
// in the grammar (the introducers, an SGR mouse prefix and its finals, a
// parameter separator and digits, the CSI tilde, a plain rune, and the
// three bytes that decode to nothing), which is where the branches are.
func TestIdleDecodeMakesProgressOnNestedEscapes(t *testing.T) {
	alpha := []byte{0x1b, '[', 'O', '<', 'M', 'm', ';', '~', '0', '1', 'a', 0x00, 0x7f, 0x80, 0xff, ' '}
	buf := make([]byte, 4)
	for _, w := range alpha {
		for _, x := range alpha {
			for _, y := range alpha {
				for _, z := range alpha {
					buf[0], buf[1], buf[2], buf[3] = w, x, y, z
					assertProgress(t, buf[:3])
					assertProgress(t, buf[:4])
				}
			}
		}
	}
}

// TestIdleDecodeMakesProgressOnEscBeforeAMouseReport is the reported
// case, named because the exhaustive test above would find it as an
// opaque byte pair and say nothing about how a terminal produces one.
//
// A lone Esc followed by a mouse report in the same read is ordinary:
// press Escape and move the pointer, or click while a dangling Esc is
// still pending. The pair is what a double click sends after an Esc that
// had not yet timed out.
func TestIdleDecodeMakesProgressOnEscBeforeAMouseReport(t *testing.T) {
	for _, seq := range []string{
		"\x1b\x1b[<0;10;5M",   // Esc, then an SGR press — decodes, but is not a key
		"\x1b\x1b[<0;10;5m",   // Esc, then the matching release
		"\x1b\x1b[<64;1;1M",   // Esc, then a wheel report
		"\x1b\x1b[200~",       // Esc, then bracketed paste — a known shape, unmapped
		"\x1b\x1b[?1000;1006", // Esc, then a truncated mode report
	} {
		assertProgress(t, []byte(seq))
	}
}

// TestTheIdleExceptionIsExactlyThePasteMarker is the UPPER bound on the
// liveness exception, and until PR #425's review nothing asserted it.
//
// Decode's doc is careful that the (0, false)-under-idle exception is
// "the narrow thing it is: ESC [ 2 0 0 ~ and its prefixes from the third
// byte on, nothing else", and cites the exhaustive walk above as the
// enforcement. But that walk covers 1- and 2-byte inputs — precisely the
// range the exception stays OUT of, as the doc itself says. So the walk
// proves the exception does not start too early and says nothing about
// where it stops.
//
// That asymmetry is the dangerous one. Widening splitPasteMarker —
// dropping its `len(s) < len(pasteStart)` clause, or matching on a
// shorter prefix — reintroduces the stranded-buffer wedge that
// input/decode.go spends forty lines warning about, and only the paste
// tests would be in a position to notice, none of which look at what
// happens to everything ELSE.
//
// The complement is what closes it: over every input beginning ESC [ up
// to the marker's length, the ONLY ones allowed to answer (0, false)
// under idle are strict prefixes of the two markers. Exhaustive over the
// third byte and sampled thereafter, because the arm that can reach this
// only fires while every byte since ESC [ is a parameter byte.
func TestTheIdleExceptionIsExactlyThePasteMarker(t *testing.T) {
	prefix := func(b []byte) bool {
		s := string(b)
		return len(s) < len(pasteStart) &&
			(strings.HasPrefix(pasteStart, s) || strings.HasPrefix(pasteEnd, s))
	}

	waits, allowed := 0, 0
	var walk func(b []byte)
	walk = func(b []byte) {
		if len(b) > len(pasteStart) {
			return
		}
		_, n, ok := Decode(b, true)
		if n == 0 && !ok {
			waits++
			if !prefix(b) {
				t.Errorf("Decode(%q, idle=true) waits for more bytes, and %q is not a "+
					"prefix of a paste marker. Under idle no further byte is coming, so "+
					"the drain loop stops here and every later keystroke strands behind "+
					"it — the wedge the exception was scoped to avoid", b, b)
			} else {
				allowed++
			}
		}
		// Only parameter bytes keep decodeCSI in the arm that can wait,
		// so those are the ones worth descending into. The terminator and
		// everything else resolve and cannot strand.
		for c := 0x30; c < 0x40; c++ {
			walk(append(append([]byte(nil), b...), byte(c)))
		}
	}
	walk([]byte{0x1b, '['})

	// The floor, and it is the half that makes the loop mean something:
	// if NOTHING waits, the exception has been removed rather than
	// bounded, and every assertion above passes vacuously.
	if allowed == 0 {
		t.Fatal("no input waited for more bytes at all, so the paste exception is " +
			"gone — a split marker now resolves to Esc and delivers its payload as " +
			"keystrokes, which is the defect the exception exists to prevent")
	}
	if waits != allowed {
		t.Errorf("%d inputs waited, %d of them legitimately", waits, allowed)
	}
	t.Logf("%d inputs wait under idle, all of them paste-marker prefixes", allowed)
}
