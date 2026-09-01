package render

import "testing"

// TestRestylingAcrossAClipEdgeKeepsTheGlyph is the documented idiom run
// over the configuration that used to eat a character.
//
// docs/learn/howto/howto-custom-draw.md tells every custom-Render author
// to restyle with `b.SetCell(x, y, b.At(x, y).WithStyle(st))`, and a row
// highlight runs that across a span. When the span's clip ends one column
// short of an enclosing paint's wide glyph, SetCell took its
// wide-PLACEMENT branch, found it could not put the tail down, and wrote
// a space — losing content, not style, through the one API the docs
// point at.
//
// The pair is already on the screen in that case, so there is nothing to
// place: only the lead's style is written, and the tail is left alone
// because the flusher skips continuation cells and the lead's style
// covers both columns anyway.
func TestRestylingAcrossAClipEdgeKeepsTheGlyph(t *testing.T) {
	b := NewBuffer(10, 1)
	b.Clear()
	b.SetString(8, 0, "世", Style{}) // the enclosing paint owns columns 8 and 9

	before := RowText(b, 0)
	if want := "        世"; before != want {
		t.Fatalf("the fixture reads %q, want %q — it does not set up the case",
			before, want)
	}

	// A descendant clipped to 0..8: column 8 is ours, column 9 is not.
	prev := b.Clip(Rect{X: 0, Y: 0, W: 9, H: 1})
	for x := 0; x < 9; x++ {
		c := b.At(x, 0)
		st := c.Style
		st.Reverse = true
		b.SetCell(x, 0, c.WithStyle(st))
	}
	b.Unclip(prev)

	if got := RowText(b, 0); got != before {
		t.Errorf("restyling the row through the documented idiom changed it from "+
			"%q to %q — SetCell deleted a glyph it was only asked to recolour",
			before, got)
	}
	if got := b.At(8, 0); !got.Style.Reverse {
		t.Errorf("the lead at column 8 did not take the new style: %+v", got)
	}
	if got := b.At(9, 0).Rune; got != Continuation {
		t.Errorf("column 9 is %q, not the tail of the pair at column 8", got)
	}
	// And the row still says what the screen will show.
	if got := TerminalWidth(b, 0); got != 10 {
		t.Errorf("the row measures %d columns, want 10: %q", got, RowText(b, 0))
	}
	if x, by, bad := Displaced(b, 0); bad {
		t.Errorf("cell %d is displaced by %d columns", x, by)
	}
}

// TestPlacingAWideGlyphAtTheClipEdgeStillAnswersASpace bounds the
// exception. The restyle path must not become a licence to place a NEW
// wide glyph whose tail falls outside the clip — that would leave a lead
// with no continuation in a neighbour's cells, which is the injury
// healSeam exists to repair.
func TestPlacingAWideGlyphAtTheClipEdgeStillAnswersASpace(t *testing.T) {
	b := NewBuffer(10, 1)
	b.Clear()
	prev := b.Clip(Rect{X: 0, Y: 0, W: 9, H: 1})
	b.SetCell(8, 0, Cell{Rune: '世'})
	b.Unclip(prev)

	if got := b.At(8, 0).Rune; got != ' ' {
		t.Errorf("column 8 holds %q; a wide glyph with no room for its tail must "+
			"answer a space", got)
	}
	if got := b.At(9, 0).Rune; got == Continuation {
		t.Error("column 9 took a continuation for a lead that was never placed")
	}
}
