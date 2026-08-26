package render

import (
	"strings"
	"testing"
)

// The third instrument: what reaches the TERMINAL.
//
// The cell tests prove the buffer allocates a wide glyph two cells, and
// the model proves cell index still equals column. Neither looks at the
// bytes, and the bytes are where this could still go wrong — a
// continuation cell that got emitted would write the glyph's own tail
// over its right half, and the cell plane would be perfectly correct
// while the screen was not.
func TestAContinuationCellPutsNothingOnTheWire(t *testing.T) {
	b := NewBuffer(6, 1)
	b.SetString(0, 0, "世ab", Style{})

	f := NewFlusher()
	out := string(f.Encode(nil, b, TrueColor))

	// The glyph goes out once. Twice would be the continuation cell
	// emitting a copy; zero would be the skip having eaten the glyph
	// itself rather than its tail.
	if n := strings.Count(out, "世"); n != 1 {
		t.Errorf("世 appears %d times in the flush, want 1 — a continuation cell "+
			"must emit nothing, and must not swallow the glyph it continues", n)
	}
	// The replacement character is what utf8.AppendRune produces for a
	// negative rune, so its presence is the precise signature of the
	// continuation having been written out as text.
	if strings.ContainsRune(out, '�') {
		t.Error("the flush contains U+FFFD: the Continuation sentinel was encoded " +
			"as a rune instead of skipped")
	}
	if !strings.Contains(out, "ab") {
		t.Error("the text after the wide glyph never reached the wire")
	}
}

// And the cursor arithmetic the skip exists to protect.
//
// The flusher addresses runs with CUP at the cell index, which is only
// correct while cell index equals column. This is the assertion that ties
// the two halves together: after a wide glyph, a run starting at cell 4
// must be addressed as column 4 (1-based: 5), not as column 3 — the
// number a rune-counting buffer would have produced.
func TestARunAfterAWideGlyphIsAddressedAtItsRealColumn(t *testing.T) {
	b := NewBuffer(8, 1)
	b.SetString(0, 0, "世界", Style{}) // cells 0..3, columns 0..3

	f := NewFlusher()
	f.Encode(nil, b, TrueColor) // first flush is full; prime the diff

	// Now change only the tail, so the diff emits one run there.
	b.SetString(4, 0, "Z", Style{})
	out := string(f.Encode(nil, b, TrueColor))

	if !strings.Contains(out, "\x1b[1;5H") {
		t.Errorf("the run holding Z is not addressed at column 5 (1-based for "+
			"cell 4). Got:\n%q\n\nA rune-counting buffer would have put Z in "+
			"cell 2 and addressed it at column 3, which is inside 界.", out)
	}
}
