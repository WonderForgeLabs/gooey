package components

import (
	"strings"
	"testing"
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
	"github.com/WonderForgeLabs/gooey/term"
)

func textBox(t *testing.T, s string) (*TextBox, *prop.Property[string]) {
	t.Helper()
	v := prop.NewSource(s)
	tb := &TextBox{Text: v}
	gooey.Compose(tb, term.Caps{Cols: 20, Rows: 1}, nil)
	tb.setCaret(len([]rune(s)))
	return tb, v
}

func TestTextBoxTypingInsertsAtTheCaret(t *testing.T) {
	tb, v := textBox(t, "abc")
	tb.HandleKey(input.Rune('d'))
	if got, want := v.Get(), "abcd"; got != want {
		t.Errorf("typing at the end: %q, want %q", got, want)
	}
	// Move into the middle and insert there.
	tb.HandleKey(input.Named(input.KeyLeft))
	tb.HandleKey(input.Named(input.KeyLeft))
	tb.HandleKey(input.Rune('X'))
	if got, want := v.Get(), "abXcd"; got != want {
		t.Errorf("mid-string insert: %q, want %q", got, want)
	}
	if got, want := tb.Caret(), 3; got != want {
		t.Errorf("caret after insert = %d, want %d", got, want)
	}
}

func TestTextBoxBackspaceAndDelete(t *testing.T) {
	tb, v := textBox(t, "abcd")
	tb.HandleKey(input.Named(input.KeyBackspace))
	if got, want := v.Get(), "abc"; got != want {
		t.Errorf("backspace: %q, want %q", got, want)
	}
	tb.HandleKey(input.Named(input.KeyHome))
	tb.HandleKey(input.Named(input.KeyDelete))
	if got, want := v.Get(), "bc"; got != want {
		t.Errorf("delete at the start: %q, want %q", got, want)
	}
	// Backspace at the start is a consumed no-op, not a page gesture.
	before := v.Get()
	if !tb.HandleKey(input.Named(input.KeyBackspace)) {
		t.Error("backspace at the start should be consumed")
	}
	if v.Get() != before {
		t.Errorf("backspace at the start changed the text to %q", v.Get())
	}
}

func TestTextBoxCaretMovementAndClamping(t *testing.T) {
	tb, _ := textBox(t, "ab")
	tb.HandleKey(input.Named(input.KeyHome))
	if got := tb.Caret(); got != 0 {
		t.Errorf("home: caret = %d, want 0", got)
	}
	tb.HandleKey(input.Named(input.KeyLeft)) // already at 0
	if got := tb.Caret(); got != 0 {
		t.Errorf("left at the start: caret = %d, want 0", got)
	}
	tb.HandleKey(input.Named(input.KeyEnd))
	if got := tb.Caret(); got != 2 {
		t.Errorf("end: caret = %d, want 2", got)
	}
	tb.HandleKey(input.Named(input.KeyRight))
	if got := tb.Caret(); got != 2 {
		t.Errorf("right at the end: caret = %d, want 2", got)
	}
}

// The bound text can change underneath the component; the caret must not be
// left pointing past the end.
func TestTextBoxCaretClampsWhenTextShrinksExternally(t *testing.T) {
	tb, v := textBox(t, "abcdef")
	if got := tb.Caret(); got != 6 {
		t.Fatalf("caret = %d, want 6", got)
	}
	v.Set("ab") // a viewmodel reset
	if got, want := tb.Caret(), 2; got != want {
		t.Errorf("caret after an external shrink = %d, want %d", got, want)
	}
	tb.HandleKey(input.Rune('c'))
	if got, want := v.Get(), "abc"; got != want {
		t.Errorf("typing after an external reset: %q, want %q", got, want)
	}
}

func TestTextBoxChangedRunsOnEditsButNotOnCaretMoves(t *testing.T) {
	tb, _ := textBox(t, "ab")
	edits := 0
	tb.Changed = gooey.Command(func() { edits++ })

	tb.HandleKey(input.Rune('c'))
	tb.HandleKey(input.Named(input.KeyBackspace))
	if edits != 2 {
		t.Errorf("Changed ran %d times over two edits, want 2", edits)
	}
	tb.HandleKey(input.Named(input.KeyLeft))
	tb.HandleKey(input.Named(input.KeyHome))
	tb.HandleKey(input.Named(input.KeyEnd))
	if edits != 2 {
		t.Errorf("Changed ran %d times; caret moves are not edits", edits)
	}
}

// Keys the box does not use must bubble, or the page loses enter/esc
// while the query line has focus — which is exactly how finder works.
func TestTextBoxDeclinesPageGestures(t *testing.T) {
	tb, _ := textBox(t, "")
	for _, ev := range []input.KeyEvent{
		input.Named(input.KeyEnter),
		input.Named(input.KeyEsc),
		input.Named(input.KeyUp),
		input.Named(input.KeyDown),
		input.Named(input.KeyTab),
		{Key: input.KeyRune, Rune: 'c', Mods: input.ModCtrl},
	} {
		if tb.HandleKey(ev) {
			t.Errorf("TextBox consumed %v, which belongs to the page", ev)
		}
	}
}

func TestTextBoxRendersPromptTextAndCaret(t *testing.T) {
	v := prop.NewSource("hi")
	tb := &TextBox{
		Text:        v,
		Prompt:      Str("> "),
		AccentStyle: Sty(render.Style{Fg: render.RGB(255, 170, 60)}),
	}
	tb.SetFocused(true)
	f := gooey.Compose(tb, term.Caps{Cols: 10, Rows: 1}, nil)
	tb.setCaret(2)
	f = gooey.Compose(tb, term.Caps{Cols: 10, Rows: 1}, nil)

	if got, want := render.SpanText(f.Cells, 0, 0, 10), "> hi█     "; got != want {
		t.Errorf("rendered %q, want %q", got, want)
	}
}

// A field narrower than its content scrolls to keep the caret visible.
func TestTextBoxScrollsToKeepTheCaretVisible(t *testing.T) {
	v := prop.NewSource("abcdefghijklmnop")
	tb := &TextBox{Text: v}
	tb.SetFocused(true)
	tb.setCaret(len("abcdefghijklmnop")) // caret at the end, as after typing
	f := gooey.Compose(tb, term.Caps{Cols: 6, Rows: 1}, nil)

	// Caret is at the end, so the tail is what shows.
	if got := render.SpanText(f.Cells, 0, 0, 6); !strings.Contains(got, "p") {
		t.Errorf("narrow field showed %q; the caret end must stay visible", got)
	}
}

func TestTextBoxClickPlacesTheCaret(t *testing.T) {
	v := prop.NewSource("abcdef")
	tb := &TextBox{Text: v, Prompt: Str("> ")}
	gooey.Compose(tb, term.Caps{Cols: 20, Rows: 1}, nil)

	if !tb.HandleMouse(input.MouseEvent{Kind: input.MousePress, X: 5, Y: 0}) {
		t.Fatal("click was not handled")
	}
	// x=5 minus the 2-cell prompt: caret 3.
	if got, want := tb.Caret(), 3; got != want {
		t.Errorf("click at x=5 put the caret at %d, want %d", got, want)
	}
}

// Editing repaints only the text box — the damage guarantee.
func TestTextBoxEditRepaintsOnlyItself(t *testing.T) {
	v := prop.NewSource("")
	tb := &TextBox{Text: v}
	root := &VStack{Children: []gooey.Component{&Text{Content: Str("a")}, tb, &Text{Content: Str("b")}}}
	comp := gooey.NewComposer(root, 20, 4)
	if _, painted := comp.Frame(); painted != 4 {
		t.Fatalf("first frame painted %d, want 4", painted)
	}
	tb.HandleKey(input.Rune('x'))
	if _, painted := comp.Frame(); painted != 1 {
		t.Errorf("typing painted %d components, want exactly 1", painted)
	}
	// A caret move is damage too, and just as local.
	tb.HandleKey(input.Named(input.KeyHome))
	if _, painted := comp.Frame(); painted != 1 {
		t.Errorf("a caret move painted %d components, want exactly 1", painted)
	}
}

func TestTextBoxWithoutTextIsInert(t *testing.T) {
	tb := &TextBox{}
	gooey.Compose(tb, term.Caps{Cols: 10, Rows: 1}, nil)
	if tb.HandleKey(input.Rune('a')) {
		t.Error("an unbound TextBox consumed a key")
	}
}

// TestPastingCRLFDoesNotDoubleSpaceTheLineBreaks is the Windows and
// web-clipboard case, which is most of the pastes a single-line field
// actually sees.
//
// oneLine turns a newline into a space so a multi-line payload does not
// smuggle a line break into a one-line value. It did that per RUNE, and
// a CRLF is TWO runes, so every line break from a Windows editor, a
// browser textarea or an RDP clipboard arrived as two spaces. The value
// is wrong in a way nothing reports and the user cannot see — trailing
// and doubled spaces look like whitespace they typed. Found in the
// review of #391 (issue #419).
//
// A LONE \r STAYS ONE SPACE, and that is the discriminating half: the
// fix is "CR LF is one break", not "drop CR", so a classic-Mac payload
// and a payload that really does contain a bare carriage return keep
// their separator.
func TestPastingCRLFDoesNotDoubleSpaceTheLineBreaks(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{"crlf", "one\r\ntwo", "one two"},
		{"crlf twice", "a\r\nb\r\nc", "a b c"},
		{"lone lf", "one\ntwo", "one two"},
		{"lone cr", "one\rtwo", "one two"},
		{"lf then cr is two breaks", "a\n\rb", "a  b"},
		{"tab", "one\ttwo", "one two"},
		{"blank line survives as two", "a\r\n\r\nb", "a  b"},
		{"nul is dropped", "a\x00b", "ab"},
	} {
		tb, v := textBox(t, "")
		tb.HandlePaste(input.PasteEvent{Text: tc.in})
		if got := v.Get(); got != tc.want {
			t.Errorf("%s: pasting %q gave %q, want %q", tc.name, tc.in, got, tc.want)
		}
	}
}

// TestTextBoxRendersAWideGlyphInItsOwnColumns is the fixture the reader
// above could not hold, which is the whole of #516: a rune-per-cell read
// builds "世�界�" for a row the terminal draws as "世界", so a
// wide-glyph assertion failed for a reason that was not the bug and
// nobody wrote one. Every fixture in this package stayed ASCII, and an
// ASCII fixture agrees with itself under the rune rule and the column
// rule alike — it passes against the defect either way.
//
// TWO GLYPHS, FOUR COLUMNS, TWO RUNES, which is CLAUDE.md's recipe for
// pinning one of these. The caret sits after them, so its column is the
// assertion: a TextBox advancing one column per rune puts it at 2, and
// the glyphs it overwrote are gone — the row read "  █" until #519 was
// fixed, and this test carried a t.Skip citing it until then.
func TestTextBoxRendersAWideGlyphInItsOwnColumns(t *testing.T) {
	// THE SKIP RETIRED HERE, which is what it was built to do. The base
	// branch (#520) carried a t.Skip citing #519 behind a condition that
	// stopped firing the moment all three rows read correctly, so the
	// claim could not outlive the fix even if the fixer never opened
	// this file. THIS is the commit that fixes it: all three rows read
	// correctly, the skip's condition is false, and it comes out rather
	// than sitting here as a branch that can no longer be taken.
	//
	// THE THREE SHAPES STAY. They are the base branch's, not this
	// branch's — #520 widened the fixture from the focused caret row to
	// all three after review pointed out that a fix correcting the
	// unfocused path and leaving the caret column wrong would have kept
	// the file dark behind a green check. Merging the skip away must not
	// merge the coverage away with it, which is what taking this side of
	// the conflict wholesale would have done.
	const (
		wantCaret = "世界█     "
		wantPlain = "世界      "
		wantMixed = "a世b      "
	)
	compose := func(text string, focused bool) string {
		tb := &TextBox{Text: prop.NewSource(text)}
		tb.SetFocused(focused)
		if focused {
			tb.setCaret(len([]rune(text)))
		}
		f := gooey.Compose(tb, term.Caps{Cols: 10, Rows: 1}, nil)
		return render.SpanText(f.Cells, 0, 0, 10)
	}
	caret := compose("世界", true)
	plain := compose("世界", false)
	mixed := compose("a世b", false)

	// THE STRING IS THE ONLY PIN HERE, and the two assertions that used
	// to stand beside it are gone for opposite reasons.
	//
	// A loop over TerminalColumns asserting col == i was false of a
	// CORRECT wide row — a continuation cell's recorded column is where
	// the cursor sits mid-glyph, which is legitimately not its index —
	// so it could not run. render.Displaced replaced it and cannot
	// FAIL: #519 blanked the orphaned lead through healSeam, so the row
	// was wrong without being displaced. Measured against the render
	// this commit fixes, all three cases of this fixture:
	//
	//	"世界" unfocused -> " 界       "  displaced=false
	//	"世界" focused   -> "  █       "  displaced=false
	//	"a世b" unfocused -> "a b       "  displaced=false
	//
	// Nor is it reachable for any component test: Buffer.Set and
	// SetString lay the continuation themselves, and render/cell.go says
	// of the remaining displacement branch that it is only reachable by
	// assigning Cells directly. An assertion that cannot fail measures
	// nothing, and a second one beside a real pin reads as corroboration
	// it is not supplying. Raised in review of #520.
	if got, want := caret, wantCaret; got != want {
		t.Errorf("rendered %q, want %q — the two glyphs occupy FOUR columns, so "+
			"the caret belongs in column 4", got, want)
	}
	if got, want := plain, wantPlain; got != want {
		t.Errorf("unfocused, rendered %q, want %q — the glyphs occupy their own "+
			"columns with no caret to make room for", got, want)
	}
	if got, want := mixed, wantMixed; got != want {
		t.Errorf("unfocused, rendered %q, want %q — a narrow glyph either side "+
			"of a wide one is the arrangement a per-rune advance loses in the "+
			"middle rather than at the end", got, want)
	}
}

// TestTextBoxClickLandsOnTheGlyphUnderTheColumn is #519's third site.
// The renderer and the click have to agree about where a character is,
// and they agreed only while every rune was one column wide: indexAt
// read `scroll + column`, which walks one character per COLUMN, so a
// click past a wide glyph landed one character right per glyph passed.
//
// "a世b" occupies four columns — a, then 世 across two, then b — so the
// column-to-rune map is the assertion. Both columns of the glyph answer
// with the glyph: half of one is not a position a caret can take.
func TestTextBoxClickLandsOnTheGlyphUnderTheColumn(t *testing.T) {
	v := prop.NewSource("a世b")
	tb := &TextBox{Text: v}
	gooey.Compose(tb, term.Caps{Cols: 20, Rows: 1}, nil)

	for _, c := range []struct {
		col, want int
		why       string
	}{
		{0, 0, "the ascii head"},
		{1, 1, "the glyph's first column"},
		{2, 1, "and its second — the same character"},
		{3, 2, "the ascii tail, four columns in but only three runes"},
	} {
		if !tb.HandleMouse(input.MouseEvent{Kind: input.MousePress, X: c.col, Y: 0}) {
			t.Fatalf("click at column %d was not handled", c.col)
		}
		if got := tb.Caret(); got != c.want {
			t.Errorf("a click at column %d put the caret at rune %d, want %d — %s",
				c.col, got, c.want, c.why)
		}
	}
}

// TestTextBoxScrollsByColumnsOverWideGlyphs is #519's second site. The
// window is avail CELLS and scrollFor indexed it in runes, so a field of
// CJK scrolled by half a field: the caret left the window the function
// exists to keep it inside.
//
// Six columns over four glyphs (eight columns) is the discriminating
// shape — a rune-counted window of six would think the whole value fits.
func TestTextBoxScrollsByColumnsOverWideGlyphs(t *testing.T) {
	v := prop.NewSource("東西南北")
	tb := &TextBox{Text: v}
	tb.SetFocused(true)
	tb.setCaret(4) // one past the last rune, where typing leaves it
	row := func() string {
		f := gooey.Compose(tb, term.Caps{Cols: 6, Rows: 1}, nil)
		if x, by, bad := render.Displaced(f.Cells, 0); bad {
			t.Fatalf("cell %d is drawn %d columns off: %q", x, by,
				render.RowText(f.Cells, 0))
		}
		return render.SpanText(f.Cells, 0, 0, 6)
	}

	// Four glyphs are eight columns and the caret owns a ninth, so the
	// window holds the last two glyphs and the caret.
	if got, want := row(), "南北█ "; got != want {
		t.Errorf("caret at the end showed %q, want %q — six columns hold two "+
			"glyphs and the caret, not three glyphs", got, want)
	}
	tb.HandleKey(input.Named(input.KeyHome))
	if got, want := row(), "東西南"; got != want {
		t.Errorf("home showed %q, want %q — the window pulled back to the start "+
			"and holds exactly three glyphs, the caret sitting ON the first one "+
			"rather than after the last", got, want)
	}
}

// TestTheScrollWindowIsWalkedNotResummed is a COST assertion, which is
// an unusual shape here and the only one that catches this defect: the
// window scrollFor returns was right before this fix and is right after
// it, so every assertion about the answer passes over the bug.
//
// The two conditions it replaced re-summed a shrinking tail on every
// step — O(n²) — and they ran inside the TextBox's paint node, on the UI
// goroutine. Measured on the branch under review: 1.36 s for a caret at
// the end of 10,000 ASCII runes, 3.76 s over CJK, 19.6 s for a full
// Compose of a 20,000-rune value. Not a slow frame; a hard freeze, on
// End, on a click, on a paste, or on the first render of a prefilled
// field.
//
// THE BUDGET IS DELIBERATELY ENORMOUS. Walking left from the caret costs
// microseconds here, and the quadratic form costs about five seconds at
// this size, so half a second sits two orders of magnitude above the fix
// and an order of magnitude below the defect. A tighter bound would buy
// nothing and start flaking on a loaded shared runner; this one cannot
// pass against the shape it exists to reject.
func TestTheScrollWindowIsWalkedNotResummed(t *testing.T) {
	const n = 20000
	runes := []rune(strings.Repeat("a", n))
	start := time.Now()
	got := scrollFor(runes, 0, n, 20)
	took := time.Since(start)

	// The answer first: a budget over a wrong window proves nothing.
	if want := n - 19; got != want {
		t.Fatalf("scrollFor put the window at %d, want %d — twenty columns "+
			"hold nineteen runes and the caret's own column", got, want)
	}
	if took > 500*time.Millisecond {
		t.Errorf("scrollFor took %v over %d runes, want well under 500ms; "+
			"that is the re-summing shape #521's review measured at seconds "+
			"on the paint path", took, n)
	}
}

// TestDraggingPastTheLeftEdgeKeepsSelecting is the gesture
// HandleMouseMove's doc comment promises — "dragging past the field's
// edge keeps working" — and an intermediate version of #519 removed it.
//
// The mechanism is the subtle part. indexAt answering a column left of
// the field with an index that is off the window is not a caret the user
// cannot see: scrollFor's `if caret < cur { cur = caret }` pulls the
// window onto it before the frame is drawn. Clamping to the first
// VISIBLE rune instead pinned the caret at the window's left edge on
// every drag, so a selection could never reach text that had scrolled
// off — the measured selection was [21,24) where it should be [9,24).
//
// BOTH WIDTHS, because the walk is in columns: over CJK each drag of two
// columns is ONE glyph, and a rune-counted walk would move two.
func TestDraggingPastTheLeftEdgeKeepsSelecting(t *testing.T) {
	for _, tc := range []struct {
		name     string
		value    string
		drags    int
		wantLo   int
		wantHi   int
		wantLeft string
	}{
		{"ascii", "abcdefghijklmnopqrstuvwxyz", 6, 9, 24, "jklmno"},
		// Eight glyphs are sixteen columns; the caret at the end puts the
		// window on the last three. Two columns left is one glyph back.
		{"wide", "東西南北東西南北", 2, 4, 7, "東西南"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := prop.NewSource(tc.value)
			tb := &TextBox{Text: v}
			tb.SetFocused(true)
			tb.setCaret(len([]rune(tc.value)))
			gooey.Compose(tb, term.Caps{Cols: 6, Rows: 1}, nil)

			tb.HandleMouse(input.MouseEvent{Kind: input.MousePress, X: 3, Y: 0, Button: input.ButtonLeft})
			var row string
			for i := 0; i < tc.drags; i++ {
				tb.HandleMouseMove(input.MouseEvent{X: -2, Y: 0, Button: input.ButtonLeft})
				f := gooey.Compose(tb, term.Caps{Cols: 6, Rows: 1}, nil)
				row = render.SpanText(f.Cells, 0, 0, 6)
			}
			lo, hi, on := tb.Selection()
			if !on || lo != tc.wantLo || hi != tc.wantHi {
				t.Errorf("after %d drags to column -2 the selection is [%d,%d) on=%v, "+
					"want [%d,%d) — the caret is pinned at the window's left edge, "+
					"so text that scrolled off cannot be selected",
					tc.drags, lo, hi, on, tc.wantLo, tc.wantHi)
			}
			if row != tc.wantLeft {
				t.Errorf("the window ended at %q, want %q — the off-window caret "+
					"is what pulls it left on the next frame", row, tc.wantLeft)
			}
		})
	}
}

// TestTheCaretIsVisibleOnAWideGlyphAtTheWindowsEdge pins the column the
// caret reserves against the columns the renderer needs for it.
//
// A caret ON a rune is drawn by reversing that rune's glyph, and Render
// stops on a glyph's FULL width — so a two-column glyph needs two
// columns reserved. scrollFor reserved exactly one, the two disagreed,
// and the glyph the caret was riding fell off the right edge: the user
// typed at a position with no visible caret anywhere in the field.
//
// Reverse ON SOME CELL is the assertion rather than a row string,
// because the row reads the same whether the caret is drawn or not.
func TestTheCaretIsVisibleOnAWideGlyphAtTheWindowsEdge(t *testing.T) {
	v := prop.NewSource("a東西b")
	tb := &TextBox{Text: v}
	tb.SetFocused(true)
	tb.setCaret(2) // on 西, reachable with Home then two rights
	f := gooey.Compose(tb, term.Caps{Cols: 4, Rows: 1}, nil)

	found := -1
	for x := 0; x < 4; x++ {
		if f.Cells.At(x, 0).Style.Reverse {
			found = x
			break
		}
	}
	if found < 0 {
		t.Fatalf("row %q carries no reversed cell: the caret is on 西 and "+
			"nothing on screen says so", render.SpanText(f.Cells, 0, 0, 4))
	}
	if got := f.Cells.At(found, 0).Rune; got != '西' {
		t.Errorf("the reversed cell holds %q, want 西 — the caret sits ON the "+
			"character it precedes", got)
	}
}

// TestACombiningMarkSurvivesTheRuneItDecorates is #519's defect one
// Unicode category over, reported against the fix for it.
//
// A combining mark is zero columns wide, so advancing the paint cursor
// by the rune's width did not advance it at all and the NEXT rune
// overwrote the cell the mark had just been written into: decomposed
// "éx" painted as "ex". The accent was gone from the buffer, not
// merely misplaced.
//
// THE TRAILING RUNE IS THE DISCRIMINATOR. "é" alone survived the
// bug, because nothing came after it to do the overwriting — a fixture
// without the "x" passes against the defect.
func TestACombiningMarkSurvivesTheRuneItDecorates(t *testing.T) {
	const decomposed = "éx" // "éx", written as e + U+0301
	v := prop.NewSource(decomposed)
	tb := &TextBox{Text: v}
	f := gooey.Compose(tb, term.Caps{Cols: 8, Rows: 1}, nil)

	if got, want := render.SpanText(f.Cells, 0, 0, 3), "éx "; got != want {
		t.Errorf("rendered %q, want %q — the mark is written into its lead's "+
			"cell, and the rune after it must not take that cell back",
			got, want)
	}
	if got := f.Cells.At(0, 0).Cluster; got != "é" {
		t.Errorf("cell 0 holds cluster %q, want %q — width and content have to "+
			"come from the same thing", got, "é")
	}

	// A TRAILING MARK LEAVES NO STRAY. Folding alone is not enough: a mark
	// that also went through the ordinary write landed in the column after
	// its lead, and only the rune that followed it painted over the litter.
	// With nothing following, the litter stays.
	trailing := &TextBox{Text: prop.NewSource("é")}
	f = gooey.Compose(trailing, term.Caps{Cols: 8, Rows: 1}, nil)
	if got := f.Cells.At(1, 0); got.Rune != ' ' || got.Cluster != "" {
		t.Errorf("cell 1 holds %q/%q, want a blank — the mark belongs in its "+
			"lead's cell and nowhere else", got.Rune, got.Cluster)
	}

	// AND THE CARET ON THE MARK IS THE CLUSTER REVERSED. Folding the mark
	// into its lead skips the write that carries the caret style, so
	// without an arm of its own the caret vanishes for exactly one
	// arrow-key press per combining mark in the value.
	on := &TextBox{Text: prop.NewSource("éx")}
	on.SetFocused(true)
	on.setCaret(1) // the mark itself
	f = gooey.Compose(on, term.Caps{Cols: 8, Rows: 1}, nil)
	if !f.Cells.At(0, 0).Style.Reverse {
		t.Error("the caret is on the combining mark and cell 0 is not reversed: " +
			"nothing on screen says where the caret is")
	}
}

// TestMovingTheScrollWindowRepaintsOnlyTheField is the damage-count pin
// CLAUDE.md asks for whenever a change moves a repaint: "a damage-count
// assertion is the only pin for a repaint claim". Scrolling the window
// is a caret move, and a caret move is local — the field's own paint
// node reads the caret property and nothing else does.
//
// gooey.Compose returns only the frame, so the sibling scroll tests
// cannot make this assertion at all; a Composer is what carries the
// count.
func TestMovingTheScrollWindowRepaintsOnlyTheField(t *testing.T) {
	v := prop.NewSource("東西南北")
	tb := &TextBox{Text: v}
	tb.SetFocused(true)
	tb.setCaret(4)
	root := &VStack{Children: []gooey.Component{&Text{Content: Str("a")}, tb}}
	comp := gooey.NewComposer(root, 6, 2)
	if _, painted := comp.Frame(); painted != 3 {
		t.Fatalf("first frame painted %d, want 3", painted)
	}

	tb.HandleKey(input.Named(input.KeyHome))
	f, painted := comp.Frame()
	if painted != 1 {
		t.Errorf("moving the scroll window painted %d components, want exactly 1", painted)
	}
	// And it really did move, so the count above is over a frame that
	// had work to do.
	if got, want := render.SpanText(f.Cells, 0, 1, 6), "東西南"; got != want {
		t.Errorf("after Home the window shows %q, want %q", got, want)
	}
}

// TestAnEmojiPresentationSequenceKeepsItsColumns is the arm the first fix
// for #519 needed and did not have.
//
// A variation selector is zero-width, so the rune walk folded it into the
// cell in front — but the fold WIDENS the lead: StringWidth("⚠️") is 2
// where StringWidth("⚠") is 1. The cursor had already advanced by 1, so
// the fold's write laid a Continuation in the column the next rune was
// about to take, healSeam blanked the orphaned lead, and "⚠️x" painted as
// " x". #519's own symptom, in a different Unicode category, introduced
// by the fix for it.
//
// U+0301 CANNOT SEE THIS, which is why the sibling test above does not:
// an accent leaves its lead one column wide, so it is the one mark whose
// fold does not change the answer. Found in the review of #521.
func TestAnEmojiPresentationSequenceKeepsItsColumns(t *testing.T) {
	const warn = "⚠️" // ⚠ + VS16: one rune of width 1, one cluster of width 2
	if render.StringWidth(warn) == render.RuneWidth([]rune(warn)[0]) {
		t.Fatalf("the fixture does not discriminate: %q measures %d columns and "+
			"its lead rune measures %d. This test is about a fold that WIDENS "+
			"its lead", warn, render.StringWidth(warn), render.RuneWidth([]rune(warn)[0]))
	}
	tb := &TextBox{Text: prop.NewSource(warn + "x")}
	f := gooey.Compose(tb, term.Caps{Cols: 8, Rows: 1}, nil)

	if got := f.Cells.At(0, 0); got.Cluster != warn {
		t.Errorf("cell 0 holds rune %q cluster %q, want the whole cluster %q — "+
			"the emoji was erased from a field showing its own value",
			got.Rune, got.Cluster, warn)
	}
	if got, want := render.SpanText(f.Cells, 0, 0, 4), warn+"x "; got != want {
		t.Errorf("rendered %q, want %q", got, want)
	}
}

// TestAZWJFamilyOccupiesTheSameColumnsAsInAText is finding 5's own
// measurement: the field and the label have to agree about one string.
//
// A rune walk cannot, because a cluster's width is not the sum of its
// runes' widths. The family below is one 2-column cluster to SetString
// and was three 2-column cells here — the same frame, four columns
// apart, with render.Displaced blind to it because every cell was
// individually self-consistent. Found in the review of #521.
func TestAZWJFamilyOccupiesTheSameColumnsAsInAText(t *testing.T) {
	const family = "\U0001F468‍\U0001F469‍\U0001F467" // 👨‍👩‍👧
	want := render.StringWidth(family + "x")
	if want >= len([]rune(family+"x"))*2 {
		t.Fatalf("the fixture does not discriminate: %q measures %d columns, "+
			"which a per-rune walk could also produce", family, want)
	}
	tb := &TextBox{Text: prop.NewSource(family + "x")}
	f := gooey.Compose(tb, term.Caps{Cols: 12, Rows: 1}, nil)

	// THE CELLS, NOT THE ROW STRING. RowText reassembles the row and
	// StringWidth then re-segments it, so the family reads as one
	// cluster whichever way it was painted — the two disagreements
	// cancel and the measurement agrees with the bug. What differs is
	// how many COLUMNS of the buffer the value occupies, which is what
	// displaces everything to its right.
	end := 0
	for x := 0; x < 12; x++ {
		if c := f.Cells.At(x, 0); c.Rune != ' ' || c.Cluster != "" {
			end = x + c.Width()
		}
	}
	if end != want {
		t.Errorf("the field lays %q across %d buffer columns; a Text lays the "+
			"same string across %d. One of them is wrong about the row, and a "+
			"reader cannot tell which from the cells — each one is "+
			"self-consistent, which is why render.Displaced cannot see it",
			family+"x", end, want)
	}
}

// TestTheCaretSurvivesAWindowThatOpensOnACombiningMark pins the arm
// scrollFor's snap exists for.
//
// scrollFor's `if caret < cur { cur = caret }` could put the window's
// first rune on a zero-width mark. Render then had no lead for it to
// join, skipped it — and skipped the caret arm with it, so a focused
// field showed no caret anywhere. Found in the review of #521.
func TestTheCaretSurvivesAWindowThatOpensOnACombiningMark(t *testing.T) {
	const value = "abcdefghij" + "é" + "xyz0123456789"
	tb := &TextBox{Text: prop.NewSource(value)}
	tb.SetFocused(true)
	tb.setCaret(len([]rune(value)))
	gooey.Compose(tb, term.Caps{Cols: 6, Rows: 1}, nil)
	tb.setCaret(11) // the mark itself, with the window well to its right
	f := gooey.Compose(tb, term.Caps{Cols: 6, Rows: 1}, nil)

	reversed := false
	for x := 0; x < 6; x++ {
		if f.Cells.At(x, 0).Style.Reverse {
			reversed = true
			break
		}
	}
	if !reversed {
		t.Errorf("no cell in the field is reversed with the caret at 11 of %q: "+
			"the user is typing into a field whose caret is nowhere on screen. "+
			"Row: %q", value, render.RowText(f.Cells, 0))
	}
}

// TestAClickLandsOnAClusterBoundaryNotInsideOne is finding 4.
//
// The forward walk stopped the moment its column budget ran out and
// never stepped past the zero-width runes belonging to the cluster it
// had just passed, so a click on the column after a folded accent
// answered with the MARK's index. Typing there put the typed rune
// between "e" and its accent; backspace deleted the "e" and left an
// orphan mark. Render folds those runes into one cell, so the click
// model and the paint model disagreed about how many positions that cell
// has. Found in the review of #521.
func TestAClickLandsOnAClusterBoundaryNotInsideOne(t *testing.T) {
	const decomposed = "éx"
	tb := &TextBox{Text: prop.NewSource(decomposed)}
	gooey.Compose(tb, term.Caps{Cols: 8, Rows: 1}, nil)

	tb.HandleMouse(input.MouseEvent{Kind: input.MousePress, X: 1})
	if got, want := tb.Caret(), 2; got != want {
		t.Errorf("a click on column 1 of %q answers caret %d, want %d — %d is "+
			"between the e and its accent, which is a position the screen does "+
			"not have", decomposed, got, want, got)
	}
}

// TestAClickAfterTheValueShrankDoesNotPanic is finding 2, and it is the
// one that killed the process.
//
// t.scroll is derived state left over from the last paint, and indexAt
// indexed the value with it unclamped. A bound value that shrinks
// between a paint and a click — a viewmodel reset, a hot reload; the
// scenario Caret()'s own doc names — then walked off the end from the UI
// goroutine. Found in the review of #521.
func TestAClickAfterTheValueShrankDoesNotPanic(t *testing.T) {
	v := prop.NewSource(strings.Repeat("abcdefghijklm", 2))
	tb := &TextBox{Text: v, Prompt: prop.NewSource("> ")}
	tb.setCaret(len([]rune(v.Get())))
	gooey.Compose(tb, term.Caps{Cols: 10, Rows: 1}, nil)
	if tb.scroll == 0 {
		t.Fatalf("the fixture never scrolled, so a stale scroll cannot be stale " +
			"here and this test measures nothing")
	}

	v.Set("ab") // no repaint: t.scroll still points past the new end
	tb.HandleMouse(input.MouseEvent{Kind: input.MousePress, X: 0})

	if got := tb.Caret(); got < 0 || got > 2 {
		t.Errorf("caret %d is outside the new value", got)
	}
}
