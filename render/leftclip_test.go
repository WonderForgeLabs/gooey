package render

import "testing"

// TestSetStringBlanksTheVisibleHalfOfAStraddlingGlyph covers the one
// column a left clip is responsible for and cannot draw in.
//
// SetString skips a cluster that starts left of column 0. When that
// cluster is TWO columns wide and starts at -1, its second column is
// column 0 — inside the buffer, inside the string's own span, and not
// something a terminal can draw half of. Skipping it outright left
// whatever was underneath: a line scrolled one column right showed the
// previous frame's character in the gap, and because nothing else
// covers that cell, nothing ever repainted it.
//
// The fixture paints over existing content on purpose. Against a
// freshly cleared buffer the cell is already a space and the bug is
// invisible — which is why it survived.
func TestSetStringBlanksTheVisibleHalfOfAStraddlingGlyph(t *testing.T) {
	b := NewBuffer(6, 1)
	b.Clear()
	b.SetString(0, 0, "ZZZZZZ", Style{}) // the previous frame
	b.SetString(-1, 0, "世ab", Style{})   // 世 spans columns -1 and 0

	if got := b.At(0, 0).Rune; got != ' ' {
		t.Errorf("column 0 holds %q, want a space — it is the second half "+
			"of a glyph the clip dropped, so it kept the old content", got)
	}
	if got := RowText(b, 0); got != " abZZZ" {
		t.Errorf("the row reads %q, want %q", got, " abZZZ")
	}
	if x, by, bad := Displaced(b, 0); bad {
		t.Errorf("the row is displaced: cell %d moved by %d", x, by)
	}
}
