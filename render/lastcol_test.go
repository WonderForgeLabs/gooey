package render

import "testing"

// TestAWideLeadInTheLastColumnIsReportedDisplaced closes the blind spot
// in the model's own instrument.
//
// Displacement is defined against the NEXT cell — "this cell's terminal
// column is not its index" — so a glyph that overflows the END of the
// row has nothing after it to push, and the loop walked off the edge
// reporting the row faithful. The row is not faithful: the terminal
// either wraps the glyph to column 0 of the next line or drops it.
//
// This is the one case the docs call the remaining sharp edge, and an
// instrument that cannot see it makes the documentation worse than
// useless: a test asserting `!bad` passed on exactly the arrangement
// the sharp edge describes.
func TestAWideLeadInTheLastColumnIsReportedDisplaced(t *testing.T) {
	b := NewBuffer(3, 1)
	b.Clear()
	// Direct assignment, because that is now the ONLY way to reach it —
	// see the two tests below.
	b.Cells[2] = Cell{Rune: '世'}

	if got := TerminalWidth(b, 0); got != 4 {
		t.Fatalf("the row measures %d columns on a 3-wide buffer, want 4 — "+
			"the fixture does not overflow", got)
	}
	x, by, bad := Displaced(b, 0)
	if !bad {
		t.Fatal("Displaced calls the row faithful while its last glyph " +
			"needs a column the buffer does not have")
	}
	if x != 2 || by != 1 {
		t.Errorf("Displaced reports cell %d over by %d, want cell 2 over by 1",
			x, by)
	}
}

// TestNeitherWriterLeavesAWideLeadInTheLastColumn is the other half:
// the sharp edge above must not be reachable through the public writers.
// Both answer with a space, which is the only value that draws correctly
// in the single column actually available.
func TestNeitherWriterLeavesAWideLeadInTheLastColumn(t *testing.T) {
	for _, tc := range []struct {
		name  string
		write func(b *Buffer)
	}{
		{"Set", func(b *Buffer) { b.Set(2, 0, '世', Style{}) }},
		{"SetString", func(b *Buffer) { b.SetString(2, 0, "世", Style{}) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := NewBuffer(3, 1)
			b.Clear()
			tc.write(b)

			if got := b.At(2, 0).Width(); got > 1 {
				t.Errorf("the last column holds a %d-column glyph", got)
			}
			if _, _, bad := Displaced(b, 0); bad {
				t.Error("the row overflows the buffer")
			}
		})
	}
}
