package render

import "testing"

// TestSetWritesAWideRuneAsAPair is the regression the healSeam fix
// introduced and nothing caught.
//
// Set wrote the lead and nothing else, leaving a wide cell with no
// Continuation beside it — which is exactly the broken pair healSeam
// was added to repair. So healSeam repaired it, by blanking the glyph
// Set had written one line earlier. Every single-rune caller of Set got
// a space where it asked for a CJK or emoji character.
//
// It survived because the display-width work went in through SetString
// and every test followed it there. Set had no wide-rune case at all,
// so the two changes could collide in silence.
func TestSetWritesAWideRuneAsAPair(t *testing.T) {
	b := NewBuffer(10, 1)
	b.Clear()
	b.Set(0, 0, '世', Style{})

	if got := b.At(0, 0).Rune; got != '世' {
		t.Errorf("Set('世') left %q in its own cell", got)
	}
	if got := b.At(0, 0).Width(); got != 2 {
		t.Errorf("the cell reports %d columns, want 2", got)
	}
	if got := b.At(1, 0).Rune; got != Continuation {
		t.Errorf("the second column holds %q, want the Continuation "+
			"sentinel — without it the glyph displaces the rest of the row",
			got)
	}
	// The model's own check: nothing drawn off its own index.
	if x, by, bad := Displaced(b, 0); bad {
		t.Errorf("the row is displaced: cell %d moved by %d", x, by)
	}
}

// TestSetRefusesAWideRuneWithNoSecondColumn covers the edge the pair
// cannot fit in.
//
// A lead written with its tail outside the clip is worse than nothing:
// the tail lands in a neighbour whose paint node is clean and never
// repaints, and on a real terminal the glyph pushes the rest of the row
// along regardless. A space occupies exactly the one column the caller
// is entitled to.
func TestSetRefusesAWideRuneWithNoSecondColumn(t *testing.T) {
	b := NewBuffer(4, 1)
	b.Clear()
	b.Clip(Rect{X: 0, Y: 0, W: 2, H: 1})
	b.Set(1, 0, '世', Style{}) // the pair would need column 2, outside the clip

	if got := b.At(1, 0).Rune; got != ' ' {
		t.Errorf("the clipped cell holds %q, want a space", got)
	}
	if got := b.At(2, 0).Rune; got == Continuation {
		t.Error("Set wrote a Continuation outside its clip, into a cell " +
			"belonging to a neighbour that will never repaint it")
	}
	if x, by, bad := Displaced(b, 0); bad {
		t.Errorf("the row is displaced: cell %d moved by %d", x, by)
	}
}

// TestRestylingACellKeepsItsCluster is the defect that made selection
// highlighting narrow a glyph.
//
// A restyle is a read-modify-write, and the obvious spelling of it —
// read the cell, change Style, hand Rune and Style back to Set — cannot
// carry a cluster, because Set takes a rune and a cluster is a string.
// So "⚠️" (U+26A0 U+FE0F, two columns) came back as bare U+26A0, one
// column, and everything after it on the row shifted left. Selecting a
// row did it; deselecting undid it.
//
// The fixture is a MULTI-RUNE cluster on purpose. A plain wide rune like
// 世 survives the lossy spelling — it is one rune — so a test built on
// one would pass against the unfixed code and prove nothing.
func TestRestylingACellKeepsItsCluster(t *testing.T) {
	const warn = "⚠️" // two columns, two runes

	b := NewBuffer(10, 1)
	b.Clear()
	b.SetString(0, 0, warn+"ab", Style{})
	before := RowText(b, 0)
	if got := b.At(0, 0).Width(); got != 2 {
		t.Fatalf("the fixture is not wide: the cell reports %d columns", got)
	}

	// Exactly what a highlight does.
	c := b.At(0, 0)
	c.Style.Reverse = true
	b.SetCell(0, 0, c)

	if got := b.At(0, 0).Cluster; got != warn {
		t.Errorf("after the restyle the cell holds cluster %q, want %q",
			got, warn)
	}
	if got := b.At(0, 0).Width(); got != 2 {
		t.Errorf("the restyled cell reports %d columns, want 2 — everything "+
			"after it on the row has shifted", got)
	}
	if !b.At(0, 0).Style.Reverse {
		t.Error("the restyle did not take")
	}
	if got := RowText(b, 0); got != before {
		t.Errorf("the row now reads %q, want the unchanged %q", got, before)
	}
	if x, by, bad := Displaced(b, 0); bad {
		t.Errorf("the row is displaced: cell %d moved by %d", x, by)
	}
}

// TestSetCellRefusesALoneContinuation keeps the repair from being handed
// the very thing it repairs. A Continuation is written only as the tail
// of the pair its lead writes; accepting a bare one places the orphan
// healSeam exists to remove, and the flusher then skips that column
// forever.
func TestSetCellRefusesALoneContinuation(t *testing.T) {
	b := NewBuffer(4, 1)
	b.Clear()
	b.SetCell(1, 0, Cell{Rune: Continuation})

	if got := b.At(1, 0).Rune; got == Continuation {
		t.Error("SetCell placed a Continuation with no lead before it")
	}
}
