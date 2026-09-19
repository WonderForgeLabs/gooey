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
	// which is what makes half one worth asserting.
	//
	// Built by assigning Cells directly, because NEITHER writer will
	// produce one any more: SetString reserves the columns a glyph
	// covers, and Set repairs the seam when a write lands on half of
	// one. That is the point of both, and it makes the raw slice the
	// only way to obtain this row.
	//
	// Which is not a contrivance — it is precisely what an external
	// cell-copy loop does. components/itemsview.go:873 and
	// apps/introdeck/terminal.go:255 both copy cell by cell and can clip
	// mid-glyph, so this shape stays reachable from real code and the
	// model has to keep reporting it.
	bad := NewBuffer(6, 1)
	for i, r := range []rune{'世', '界', 'a', 'b'} {
		bad.Cells[i] = Cell{Rune: r}
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
	// THE FIXTURE LIST IS THE TEST. "世界ab" alone passed against two real
	// defects because of what it happens not to contain: it ends in
	// ASCII, so a trailing Continuation never reached TerminalWidth's
	// last-cell arithmetic, and it holds no VS16 cluster, so a cell that
	// reserves by the cluster's width and draws by its first rune's never
	// arose. Both were found by review, not here. Every shape that can
	// break the invariant belongs in this list.
	for _, c := range []struct {
		in  string
		w   int
		why string
	}{
		{"世界ab", 6, "the original: wide glyphs then ASCII"},
		{"ab世界", 6, "ENDING in a wide glyph, so the last cell is a Continuation"},
		{"世", 2, "nothing but a wide glyph"},
		{"⚠️x", 4, "VS16 emoji presentation: two columns carried by the " +
			"SECOND rune of the cluster"},
		{"🏳️‍🌈x", 4, "a ZWJ sequence — four runes, two columns"},
		{"éx", 3, "a combining mark: one column, and it must not claim two"},
		{"abcd", 4, "the ASCII control"},
	} {
		b := NewBuffer(c.w, 1)
		b.SetString(0, 0, c.in, Style{})

		if x, by, ok := Displaced(b, 0); ok {
			t.Errorf("%q (%s): cell %d is drawn %d columns right of where the "+
				"buffer puts it; a wide glyph must consume the cells it covers",
				c.in, c.why, x, by)
		}
		if got := TerminalWidth(b, 0); got != b.W {
			t.Errorf("%q (%s): row occupies %d terminal columns for a %d-cell "+
				"buffer — the overflow is pushed off the right edge",
				c.in, c.why, got, b.W)
		}
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

// ClipCols is exported because two packages need the same clipping rule
// (components for its paint sites, cmd/browser for markdown), and a
// second hand-rolled cluster loop is how the two quietly disagree. Its
// own package tests it directly rather than leaving it to the callers'
// suites, since it is now API.
func TestClipColsNeverSplitsAWideGlyph(t *testing.T) {
	for _, c := range []struct {
		in   string
		w    int
		want string
		why  string
	}{
		{"世界ab", 3, "世", "界 would reach column 4, past the budget — so the odd column stays empty"},
		{"世界ab", 4, "世界", "two glyphs fit exactly"},
		{"世界ab", 5, "世界a", "and the ascii tail fills the odd column"},
		{"abcd", 2, "ab", "the ascii case, where columns and runes agree"},
		{"世界", 9, "世界", "a budget wider than the string returns it whole"},
		{"世界", 0, "", "a zero budget draws nothing"},
		{"世界", -1, "", "and so does a negative one, which a caller reaches by " +
			"subtracting a margin from a rect narrower than it"},
	} {
		if got := ClipCols(c.in, c.w); got != c.want {
			t.Errorf("ClipCols(%q, %d) = %q, want %q — %s", c.in, c.w, got, c.want, c.why)
		}
	}
}

// The property behind the table: whatever the input, the result fits.
// A table can only assert the cases someone thought of.
func TestClipColsAlwaysFitsItsBudget(t *testing.T) {
	for _, s := range []string{"世界ab", "a世b界c", "🇺🇸ab", "héllo", "", "  "} {
		for w := 0; w <= 12; w++ {
			if got := StringWidth(ClipCols(s, w)); got > w {
				t.Errorf("ClipCols(%q, %d) is %d columns wide", s, w, got)
			}
		}
	}
}

// TestSpanTextReadsAWideGlyphInTheMiddleOfASpan is the readback the
// twelve packages of #516 each hand-rolled and each got wrong the same
// way: writing Cell.Rune builds "世�界�" for a row a terminal
// draws as "世界", so no fixture in those packages could hold a wide
// glyph and be asserted on.
//
// THE FIXTURE IS THE POINT, not the helper. Converting a reader and
// asserting on ASCII changes no claim — an ASCII row reads the same
// under either rule. The row below is two glyphs of four COLUMNS and two
// runes, which is the shape CLAUDE.md prescribes for pinning one of
// these.
func TestSpanTextReadsAWideGlyphInTheMiddleOfASpan(t *testing.T) {
	b := NewBuffer(10, 1)
	b.SetString(0, 0, "ab世界cd", Style{})

	// THE OLD READER, spelled out here so the difference is measured
	// rather than asserted about. This is the body every one of those
	// helpers had.
	var old []rune
	for x := 0; x < 8; x++ {
		old = append(old, b.At(x, 0).Rune)
	}
	if string(old) == "ab世界cd" {
		t.Fatal("a rune-per-cell read already returns the row, so this test " +
			"measures nothing — either SetString stopped laying continuation " +
			"markers or Continuation stopped being a rune")
	}

	if got, want := SpanText(b, 0, 0, 8), "ab世界cd"; got != want {
		t.Errorf("SpanText over the whole span = %q, want %q (the rune-per-cell "+
			"read gives %q)", got, want, string(old))
	}
	if got, want := SpanText(b, 2, 0, 4), "世界"; got != want {
		t.Errorf("SpanText over the two glyphs = %q, want %q", got, want)
	}
	if got, want := SpanText(b, 0, 0, 2), "ab"; got != want {
		t.Errorf("SpanText over the ascii head = %q, want %q", got, want)
	}
}

// TestSpanTextCutThroughAGlyphReadsShortOrLong pins the two edges
// SpanText's doc names, because a caller that trusts w to be the width
// of the result is wrong at both of them and the failure is a fixture
// that never matches.
func TestSpanTextCutThroughAGlyphReadsShortOrLong(t *testing.T) {
	b := NewBuffer(10, 1)
	b.SetString(0, 0, "a世b", Style{})

	// Starting ON the continuation: the glyph's own cell is outside the
	// span, and a continuation carries no text.
	if got, want := SpanText(b, 2, 0, 2), "b"; got != want {
		t.Errorf("a span starting on a continuation cell = %q, want %q — the "+
			"glyph before it is outside the span and half a glyph is not "+
			"drawable", got, want)
	}
	// Ending on the glyph's FIRST cell: that cell holds the whole glyph.
	got := SpanText(b, 0, 0, 2)
	if got != "a世" {
		t.Errorf("a span ending on a glyph's first cell = %q, want %q", got, "a世")
	}
	if w := StringWidth(got); w != 3 {
		t.Errorf("that span asked for 2 columns and reads %d wide (%q); a caller "+
			"measuring the result must use StringWidth rather than assume w",
			w, got)
	}
}

// TestSpanTextTakesXBeforeY is the order pin, and it exists because the
// signature is four ints: a transposed call compiles, and by the padding
// rule below it returns spaces rather than panicking, so the only thing
// that fails is a fixture somewhere else with a message blaming the
// component it was reading.
//
// The fixture makes the two answers DIFFERENT strings rather than
// asserting one: row 0 and row 1 hold different text, so reading
// (x=1, y=0) and (x=0, y=1) cannot agree.
func TestSpanTextTakesXBeforeY(t *testing.T) {
	b := NewBuffer(4, 2)
	b.SetString(0, 0, "abcd", Style{})
	b.SetString(0, 1, "efgh", Style{})

	if got, want := SpanText(b, 1, 0, 2), "bc"; got != want {
		t.Errorf("SpanText(b, 1, 0, 2) = %q, want %q. The arguments are (x, y, w), "+
			"matching Buffer.At(x, y) and Buffer.SetString(x, y, …); reading %q "+
			"means they were taken as (y, x, w).", got, want, "ef")
	}
	if got := RowText(b, 1); got != "efgh" {
		t.Errorf("RowText(b, 1) = %q, want %q — RowText is the whole span of its "+
			"row and must pass its y through in the same position", got, "efgh")
	}
}

// TestSpanTextPadsWhereTheBufferIsNot pins the third edge, which the doc
// used to leave to Buffer.At.
//
// It is not a curiosity: every reader #516 converts reads a FIXED extent
// — 40 columns of a dropdown, the caller's cols × rows — so a surface
// that moves, or a composer resized in a later edit, silently turns the
// tail of one of those reads into blanks. An assertion shaped
// `!strings.Contains(got, …)` passes on blank input, which is the class
// CLAUDE.md calls a check that can quietly report the wrong answer. The
// contract is padding; this is what says so.
func TestSpanTextPadsWhereTheBufferIsNot(t *testing.T) {
	b := NewBuffer(4, 1)
	b.SetString(0, 0, "ab", Style{})

	// THE OVERSHOOT IS DERIVED, not written down beside the fixture that
	// produces it: x+w-b.W is 6 today and stays right when the fixture
	// moves, which is the difference CLAUDE.md draws between a number
	// and a sample of one. "ENDS past" rather than "runs past", too —
	// the span's head is inside the buffer and only its tail is not,
	// which is what makes the first two columns blanks-from-the-buffer
	// and the rest blanks-from-the-rule.
	const x, w = 2, 8
	if got, want := SpanText(b, x, 0, w), "        "; got != want {
		t.Errorf("a span ending %d columns past the buffer = %q, want %q",
			x+w-b.W, got, want)
	}
	if got, want := SpanText(b, -2, 0, 4), "  ab"; got != want {
		t.Errorf("a span starting left of column 0 = %q, want %q", got, want)
	}
	if got, want := SpanText(b, 0, 9, 4), "    "; got != want {
		t.Errorf("a span on row 9 of a one-row buffer = %q, want %q", got, want)
	}
}

// TestBufferTextIsEveryRowNewlineTerminated pins the two things a
// caller of a whole-buffer read depends on and cannot see from the
// signature: that it reads EVERY row, and that the last one carries its
// newline like the rest.
//
// THE LAST NEWLINE IS THE HALF WORTH PINNING. Without it a dump missing
// its final row is a PREFIX of the correct one, and every
// strings.Contains assertion over such a dump passes — which is the
// class CLAUDE.md calls a check that can quietly report the wrong
// answer. With it the two strings simply differ.
//
// A WIDE GLYPH IN THE FIXTURE, because a buffer reader that walked cells
// instead of delegating to RowText would put render.Continuation in the
// middle of row 1 and still pass an ASCII-only test — the defect #516
// exists for, one level up.
func TestBufferTextIsEveryRowNewlineTerminated(t *testing.T) {
	b := NewBuffer(4, 3)
	b.SetString(0, 0, "ab", Style{})
	b.SetString(0, 1, "世界", Style{})

	const want = "ab  \n世界\n    \n"
	if got := BufferText(b); got != want {
		t.Errorf("BufferText = %q, want %q: every row, each ending in a newline, "+
			"and a wide glyph read as itself rather than as a rune and a "+
			"continuation marker", got, want)
	}
	if got := BufferText(nil); got != "" {
		t.Errorf("BufferText(nil) = %q, want the empty string — RowText answers a "+
			"nil buffer the same way and this is the loop over it", got)
	}
}

// TestANonPositiveWidthIsTheEmptyStringNotBlanks is the shape the
// off-buffer enumeration went past: three out-of-range shapes, then nil,
// and never `w <= 0`.
//
// SPLIT OUT FROM THE PADDING TEST, because most of the ways it can go
// red are about NOT padding, and the first line CI prints is the test's
// name. Folded in with five other contracts, a failure here reports the
// padding contract breaking.
//
// A width can ARRIVE as a difference — an extent minus an origin, a
// remaining budget — so a negative one is a value a caller produces
// rather than a caller error. And the hazard is the padding
// paragraph's, at the other end: "" passes `!strings.Contains(got, …)`
// and "the row is empty" exactly as blanks do, so a span that silently
// collapsed to nothing reads as a component that drew nothing.
func TestANonPositiveWidthIsTheEmptyStringNotBlanks(t *testing.T) {
	b := NewBuffer(4, 1)
	b.SetString(0, 0, "ab", Style{})

	// AGAINST A LIVE BUFFER, which is what these two add. The only
	// assertion that existed was SpanText(nil, 0, 0, 0), and since the
	// width guard runs BEFORE the nil guard it never reached a buffer.
	if got := SpanText(b, 0, 0, 0); got != "" {
		t.Errorf("a zero-width span of a LIVE buffer = %q, want empty — zero "+
			"columns of a terminal is nothing, not one blank, which is the "+
			"answer ClipCols gives the same question", got)
	}
	if got := SpanText(b, 0, 0, -1); got != "" {
		t.Errorf("a negative-width span of a LIVE buffer = %q, want empty", got)
	}
	// THE ONE ARRANGEMENT WHERE THE GUARD CHANGES AN ANSWER rather than
	// restating what the loop already does. For a live buffer
	// `for i := 0; i < w` with w <= 0 returns "" on its own, so removing
	// the guard leaves the two assertions above green — measured, which
	// is why this third one is here. Nil is different: the guard below
	// would hand strings.Repeat a count of -1, and that PANICS. The
	// order of the two guards is therefore load-bearing, not incidental.
	if got := SpanText(nil, 0, 0, -1); got != "" {
		t.Errorf("a negative-width span of a nil buffer = %q, want empty — "+
			"without the width guard ahead of the nil guard this is a panic "+
			"in strings.Repeat, inside render with the caller off the stack", got)
	}
	// Nil AND zero-width at once, which the width guard answers first —
	// asserted so the two rules cannot disagree about their overlap.
	if got := SpanText(nil, 0, 0, 0); got != "" {
		t.Errorf("a zero-width span of a nil buffer = %q, want empty", got)
	}
}

// TestAnAbsentBufferIsAnsweredThreeDifferentWays holds the disagreement,
// which is a claim of its own rather than a corollary of padding.
//
// SpanText pads — a nil buffer is the most out of range a span can be,
// and Buffer.At dereferences b.W, so without the guard it panicked
// inside render with At on the stack rather than the caller. RowText
// answers EMPTY, because a row of no buffer has no width to pad to, and
// the guard has to live in RowText since `b.W` is evaluated in the
// argument list before SpanText is entered. TerminalColumns answers
// nothing at all, because a per-cell map has no blank cell to report.
//
// Three functions, three answers, one input: asserted together so the
// asymmetry is chosen rather than noticed later.
func TestAnAbsentBufferIsAnsweredThreeDifferentWays(t *testing.T) {
	b := NewBuffer(4, 1)
	b.SetString(0, 0, "ab", Style{})

	if got, want := SpanText(nil, 0, 0, 3), "   "; got != want {
		t.Errorf("a span of a nil buffer = %q, want %q — the out-of-range "+
			"contract is blanks, and a nil buffer is the most out of range a "+
			"span can be", got, want)
	}
	if got := RowText(nil, 0); got != "" {
		t.Errorf("RowText of a nil buffer = %q, want empty — the guard has to be "+
			"in RowText, since b.W is read before SpanText is entered", got)
	}
	if got := TerminalColumns(nil, 0); len(got) != 0 {
		t.Errorf("TerminalColumns of a nil buffer = %v, want empty", got)
	}
	// The same question about a row rather than a buffer, because that is
	// where SpanText and TerminalColumns visibly disagree on live input.
	if got := TerminalColumns(b, 9); len(got) != 0 {
		t.Errorf("TerminalColumns on row 9 of a one-row buffer = %v, want empty — "+
			"the two functions answer an out-of-range row differently and that is "+
			"the point", got)
	}
	// AND RowText ON THE SAME ROW, which was only INHERITED from
	// SpanText and is the answer the converted sites actually meet:
	// components/menuicon_test.go reads RowText(f.Cells, r.Y+1) off a
	// bounds rect, so a dropdown that stopped painting hands it a row of
	// blanks rather than an error — and an absence assertion over blanks
	// cannot fail. A nil buffer is empty and an out-of-range row of a
	// REAL one is padded; those are different answers to "there is
	// nothing here", so both are chosen here rather than one of them
	// being read off the delegation. Raised in review of #520.
	if got, want := RowText(b, 9), "    "; got != want {
		t.Errorf("RowText on row 9 of a one-row buffer = %q, want %q — blanks of "+
			"the buffer's width, not empty. A reader that drifted off the surface "+
			"pads, which is exactly why an absence assertion over one cannot "+
			"fail", got, want)
	}
}
