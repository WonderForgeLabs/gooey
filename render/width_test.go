package render

import "testing"

// The fix and the instrument that proves it, in one test.
//
// Before #358 a cell assertion COULD NOT SEE this bug, and that was not a
// gap in the assertions but a property of what they compare against: the
// buffer held exactly the runes we asked for, and the corruption happened
// one layer down when the terminal advanced two columns for a glyph given
// one cell.
//
// Both halves are still asserted, because the second is what keeps the
// first honest. Half one is the new cell layout; half two builds the OLD
// layout by hand and requires the model to still call it displaced. A
// model that had quietly stopped detecting anything would pass half one
// on its own.
func TestAWideGlyphClaimsItsSecondColumn(t *testing.T) {
	b := NewBuffer(6, 1)
	b.SetString(0, 0, "世界ab", Style{})

	// HALF ONE — a wide glyph now OWNS the column it covers, so the row
	// is four columns of content in four cells, not four runes in four
	// cells. The continuations are the fix made visible.
	for i, want := range []rune{'世', Continuation, '界', Continuation} {
		if got := b.At(i, 0).Rune; got != want {
			t.Fatalf("cell %d holds %q, want %q — a wide glyph must claim the cell "+
				"its second column covers", i, got, want)
		}
	}
	// And 'a' lands where layout would put it: column 4, not column 2.
	if got := b.At(4, 0).Rune; got != 'a' {
		t.Fatalf("cell 4 holds %q, want 'a' — the text after two wide glyphs "+
			"belongs at column 4", got)
	}

	// HALF TWO — and the model can still see a row that IS displaced,
	// which is what makes half one worth asserting. Built by hand with
	// Set, because SetString will no longer produce one: this is the
	// shape the cell plane had before the fix, and any code path that
	// writes runes without consulting width recreates it.
	bad := NewBuffer(6, 1)
	for i, r := range []rune{'世', '界', 'a', 'b'} {
		bad.Set(i, 0, r, Style{})
	}
	x, by, ok := Displaced(bad, 0)
	if !ok {
		t.Fatal("the model reports a hand-built one-rune-per-cell row as faithful. " +
			"It cannot then be trusted to report a real one, and the invariant " +
			"test below would be passing vacuously")
	}
	// Asserted exactly, because "something moved" would also pass for a
	// model that called every row displaced.
	if x != 1 || by != 1 {
		t.Errorf("first displacement at cell %d by %d columns, want cell 1 by 1 — "+
			"cell 1 is the first thing after 世, which occupies columns 0 and 1",
			x, by)
	}
	if got := TerminalColumns(bad, 0)[2]; got != 4 {
		t.Errorf("cell 2 ('a') lands in terminal column %d, want 4 — two wide "+
			"glyphs ahead of it, each taking two columns", got)
	}
}

// The invariant the fix has to establish, and the direct pin for #358.
//
// A buffer column should BE a terminal column. Where that holds, a
// component arranged at column i paints at column i and layout means what
// it says; where it does not, everything rightward of a wide glyph is
// displaced and the damaged cells are CLEAN — nobody invalidated them —
// so nothing ever repaints over the mess.
func TestABufferColumnIsATerminalColumn(t *testing.T) {
	b := NewBuffer(6, 1)
	b.SetString(0, 0, "世界ab", Style{})

	if x, by, ok := Displaced(b, 0); ok {
		t.Errorf("cell %d is drawn %d columns right of where the buffer puts it; "+
			"a wide glyph must consume the cells it covers", x, by)
	}
	if got := TerminalWidth(b, 0); got != b.W {
		t.Errorf("row occupies %d terminal columns for a %d-cell buffer — the "+
			"overflow is pushed off the right edge", got, b.W)
	}
}

// The model must not cry wolf. An ASCII row is faithful, and a model that
// reported otherwise would make the test above vacuous by always firing.
func TestAnAsciiRowIsFaithful(t *testing.T) {
	b := NewBuffer(6, 1)
	b.SetString(0, 0, "abcdef", Style{})

	if x, by, ok := Displaced(b, 0); ok {
		t.Errorf("an all-ASCII row reported cell %d displaced by %d; the model "+
			"fires on rows that are correct, so it cannot be trusted on rows "+
			"that are not", x, by)
	}
	if got := TerminalWidth(b, 0); got != 6 {
		t.Errorf("TerminalWidth = %d, want 6", got)
	}
}

// Width itself, at the boundaries rather than the centre — one case per
// class, because a table that got wide runes right and combining marks
// wrong would still pass a test built only from CJK.
func TestRuneAndStringWidth(t *testing.T) {
	for _, c := range []struct {
		in   string
		want int
		why  string
	}{
		{"a", 1, "ASCII"},
		{"世", 2, "East Asian Wide"},
		{"→", 1, "an arrow is narrow despite being non-ASCII"},
		{"é", 1, "e plus a combining acute is one column, not two"},
		{"🇯🇵", 2, "a flag is TWO regional indicators and two columns — the case " +
			"go-runewidth would have needed a separate clustering answer for"},
		{"👍", 2, "emoji presentation"},
	} {
		if got := StringWidth(c.in); got != c.want {
			t.Errorf("StringWidth(%q) = %d, want %d — %s", c.in, got, c.want, c.why)
		}
	}

	// RuneWidth is the per-rune question and deliberately answers the
	// flag differently: each regional indicator is one column alone.
	// Pinned so the difference is a decision on the record rather than a
	// surprise at the first multi-rune glyph.
	if got := RuneWidth('世'); got != 2 {
		t.Errorf("RuneWidth('世') = %d, want 2", got)
	}
	if got := RuneWidth('a'); got != 1 {
		t.Errorf("RuneWidth('a') = %d, want 1", got)
	}
}
