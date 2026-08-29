package input

import "testing"

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
// when idle, EVERY non-empty input must either consume a byte or produce
// an event.
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
