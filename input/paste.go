package input

import "strings"

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
func decodePaste(rest []byte, n int) (Event, int, bool) {
	i := strings.Index(string(rest), pasteEnd)
	if i < 0 {
		return Event{}, 0, false // incomplete: read more
	}
	return PasteOf(PasteEvent{Text: string(rest[:i])}), n + i + len(pasteEnd), true
}
