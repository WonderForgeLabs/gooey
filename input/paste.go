package input

import (
	"bytes"
	"strings"
)

// Bracketed paste (DECSET 2004).
//
// Without it a paste is INDISTINGUISHABLE from typing: the terminal
// writes the clipboard's bytes onto the tty exactly as if the user had
// hit every key, so a multi-line paste arrives as a burst in which each
// newline is an Enter — and Enter means "activate" in every app in this
// repo. Pasting three lines of markup into a designer would fire three
// buttons. There is no decoder cleverness that recovers from this,
// because the bytes really are identical; the terminal has to bracket
// them, which is what mode 2004 asks it to do.
//
// With the mode on, the terminal wraps the payload:
//
//	ESC [ 200 ~   <arbitrary bytes>   ESC [ 201 ~
//
// and everything between is CONTENT, never a gesture. That is the whole
// contract, and it is why a paste is ONE event carrying a string rather
// than N key events: a caller that had to reassemble N keys back into a
// string would be solving the same problem the terminal just solved, and
// would get the boundary wrong the first time a payload contained a tab.
//
// The bytes are taken VERBATIM. A terminal implementing 2004 is required
// to filter ESC [ 201 ~ out of the payload, so the terminator is
// unambiguous, and no other byte in the middle means anything to us —
// a pasted ESC is a pasted ESC.

// PasteEvent is one bracketed paste. Text is the payload with the
// brackets removed and nothing else done to it: no trimming, no newline
// normalisation, no filtering. What a consumer does with a control byte
// inside it is the consumer's policy, and it must have one — a TextBox
// that inserts the payload raw will happily insert a NUL.
type PasteEvent struct{ Text string }

// pasteStart and pasteEnd are the brackets, spelled once.
const (
	pasteStart = "\x1b[200~"
	pasteEnd   = "\x1b[201~"
)

func PasteOf(p PasteEvent) Event { return Event{Kind: EventPaste, Paste: p} }

func (e Event) IsPaste() bool { return e.Kind == EventPaste }

// decodePaste is called from decodeCSI once the opening bracket has been
// recognised. rest is everything after it, and n is how many bytes the
// bracket itself took.
//
// The one deliberate departure from the rest of this decoder: an
// unterminated paste keeps WAITING even when idle is true, where a
// truncated CSI resolves to the Esc key. idle exists to resolve an
// AMBIGUITY — a lone ESC and the start of a sequence are the same byte —
// and there is no ambiguity here. ESC [ 200 ~ is six bytes that nothing
// else spells and that no keyboard can produce, so the only reading is
// "a paste whose end has not arrived yet", and a large paste crossing
// many 128-byte reads will routinely take longer than the 40ms escape
// timeout to complete.
//
// The cost of that choice, stated plainly: a terminal that sends an
// opening bracket and never closes it wedges the decoder, which then
// holds every subsequent keystroke in the pending buffer. The
// alternative — giving up after some cap and delivering the prefix — is
// worse, because it silently TRUNCATES a paste, and a user who pastes
// 40KB of markup and gets 8KB of it has no way to tell. A wedge is at
// least visible.
// splitPasteMarker reports whether b is an incomplete bracket — a strict
// prefix of one of the two markers, long enough to be nothing else.
//
// THE THREE-BYTE FLOOR IS THE WHOLE DESIGN. "\x1b" and "\x1b[" are
// genuinely indistinguishable from the Esc key, which is the ambiguity
// idle was introduced to settle, so they must go on answering Esc; from
// the third byte the buffer is ESC [ followed by a digit, which nothing
// but a CSI spells. That floor is also what keeps decodeidle_test.go's
// exhaustive check over every 1- and 2-byte input true by construction
// rather than by review.
//
// What waiting costs, stated as plainly as the wedge above: a genuinely
// truncated CSI — an F9 whose "~" never arrives — is held instead of
// being delivered as Esc.
//
// THAT COST WAS UNDERSTATED HERE, and the sentence that follows replaces
// one claiming the hold "is not stranded, because the next byte from the
// terminal resolves it either way". There is a case with no next byte:
// ESC [ 2 is not only half a marker, it is Esc, `[`, `2` typed by a
// person, and nothing more is coming. The hold was then permanent — no
// Esc, the following keystroke swallowed into the CSI parse, and the
// decoder waking every 40ms for the life of the process (#440). The wait
// is now bounded at term.PasteMarkerGrace consecutive timeouts, after
// which DecodeFinal withdraws this exception. An OPEN paste is still not
// on that scale, for the reason above.
func splitPasteMarker(b []byte) bool {
	if len(b) < 3 {
		return false
	}
	s := string(b)
	return (strings.HasPrefix(pasteStart, s) || strings.HasPrefix(pasteEnd, s)) &&
		len(s) < len(pasteStart)
}

func decodePaste(rest []byte, n int) (Event, int, bool) {
	// bytes.Index, NOT strings.Index(string(rest), …), and the
	// conversion is the whole point rather than a style preference.
	//
	// This function is called once per read while a paste is open, on a
	// buffer that has grown by the chunk each time, so the SEARCH is
	// quadratic by construction — that is inherent to a stateless
	// decoder and is not what this changes. What string(rest) added on
	// top was a full COPY of the buffer per call: draining a 1MB paste
	// allocated on the order of four gigabytes and spent most of its
	// time in the collector. Measured on one machine, one call pattern:
	// 64KB 14ms, 256KB 149ms, 1MB 2.3s before; 1.2ms, 15ms, 227ms after.
	// The shape is unchanged and the constant is an order of magnitude
	// smaller. Found in the review of #391 (issue #419), which reported
	// the symptom as the decoder blocking for seconds on a large paste.
	//
	// The residual quadratic wants a decoder that remembers how far it
	// has already searched, which is an API with state where this
	// package deliberately has none. TestDrainingALargePasteDoesNotCopyItPerRead
	// pins the copy, because the copy is the part that made a paste feel
	// like a hang.
	i := bytes.Index(rest, []byte(pasteEnd))
	if i < 0 {
		return Event{}, 0, false // incomplete: read more
	}
	return PasteOf(PasteEvent{Text: string(rest[:i])}), n + i + len(pasteEnd), true
}
