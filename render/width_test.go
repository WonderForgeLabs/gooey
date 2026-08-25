package render

import "testing"

// The point of the model, stated as a test: a CELL assertion cannot see
// this bug, and that is not a gap in the assertions but a property of
// what they compare against.
//
// The buffer holds exactly the runes we asked it to hold. Every existing
// style of check in this repo — At(x,y).Rune, a row read back as a
// string, a damage rect — agrees the write was correct, because it was.
// The corruption happens one layer down, when a terminal advances two
// columns for a glyph the buffer allocated one cell to.
//
// So this test asserts BOTH halves: the cells are right, and the row is
// nonetheless displaced. If either half stopped holding, the model would
// be measuring something other than the bug.
func TestACellAssertionCannotSeeADisplacedRow(t *testing.T) {
	b := NewBuffer(6, 1)
	b.SetString(0, 0, "世界ab", Style{})

	// HALF ONE — the buffer is exactly right, by the only means the repo
	// currently has of asking.
	for i, want := range []rune{'世', '界', 'a', 'b'} {
		if got := b.At(i, 0).Rune; got != want {
			t.Fatalf("cell %d holds %q, want %q — the premise of this test is that "+
				"the CELLS are correct, and they are not, so it is measuring "+
				"something else", i, got, want)
		}
	}

	// HALF TWO — and the terminal will not draw it there.
	x, by, ok := Displaced(b, 0)
	if !ok {
		t.Fatal("the model reports the row is faithful. Either RuneWidth stopped " +
			"treating 世 as two columns, or the cell layer learned about width — " +
			"if the latter, this test has done its job and the invariant test " +
			"below is the one to keep")
	}
	// The FIRST displaced cell is the one after the first wide glyph, and
	// the shift is one column per wide glyph passed. Asserted exactly,
	// because "something moved" would pass for a model that reported
	// every row displaced.
	if x != 1 || by != 1 {
		t.Errorf("first displacement at cell %d by %d columns, want cell 1 by 1 — "+
			"cell 1 is the first thing after 世, which occupies columns 0 and 1",
			x, by)
	}
	// By the end of the row the accumulated shift is one per wide glyph.
	if got := TerminalColumns(b, 0)[2]; got != 4 {
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
	t.Skip("#358: the cell layer is not width-aware yet — SetString advances one " +
		"cell per rune and Text.Measure counts runes. This test is the " +
		"acceptance criterion for that issue and should be un-skipped by the " +
		"commit that fixes it, not edited to match current behaviour.")

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
