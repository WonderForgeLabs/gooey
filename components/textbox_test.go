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

	if got, want := rowText(f, 0, 0, 10), "> hi█     "; got != want {
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
	if got := rowText(f, 0, 0, 6); !strings.Contains(got, "p") {
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
		return rowText(f, 0, 0, 10)
	}
	caret := compose("世界", true)
	plain := compose("世界", false)
	mixed := compose("a世b", false)
	// ONE TABLE, AND NO SKIP. The table is the base branch's (#520) and
	// stays: `want` used to be set three times and read nowhere while
	// three bespoke t.Errorf blocks re-spelled the constants by hand, so
	// a fourth shape could get a tripwire and no assertion, or the
	// reverse, with nothing red. The `why` column is the only part of
	// those three blocks that differed.
	//
	// THE SKIP LOOP IS WHAT DOES NOT SURVIVE THE MERGE, and deleting it
	// is this commit's job rather than an accident of resolving a
	// conflict. #520 wrote it to retire by CONDITION — it fired only
	// while a row still read the documented buggy render — and said in
	// its own comment that #519's fixing commit deletes the whole loop
	// rather than leaving a branch that can no longer be taken. This is
	// that commit: all three rows read correctly here, so the condition
	// is false and the loop is gone. Taking either side of the conflict
	// wholesale would have been wrong — HEAD's side loses the two shapes
	// #520 added after review, and the incoming side reinstates a skip
	// citing an issue this branch closes.
	shapes := []struct{ got, want, shape, why string }{
		{caret, wantCaret, "the focused row with the caret after both glyphs",
			"the two glyphs occupy FOUR columns, so the caret belongs in column 4"},
		{plain, wantPlain, "the unfocused row",
			"the glyphs occupy their own columns with no caret to make room for"},
		{mixed, wantMixed, "the unfocused mixed-width row",
			"a narrow glyph either side of a wide one is the arrangement a " +
				"per-rune advance loses in the middle rather than at the end"},
	}

	// THE STRING IS THE ONLY PIN HERE, and the two assertions that used
	// to stand beside it are gone for opposite reasons.
	//
	// A loop over TerminalColumns asserting col == i was false of a
	// CORRECT wide row — a continuation cell's recorded column is where
	// the cursor sits mid-glyph, which is legitimately not its index —
	// so it could not run. render.Displaced replaced it and cannot FAIL:
	// #519 blanked the orphaned lead through healSeam, so the row was
	// wrong WITHOUT being displaced. Measured against the render this
	// commit fixes, all three cases of this fixture:
	//
	//	"世界" unfocused -> " 界       "  displaced=false
	//	"世界" focused   -> "  █       "  displaced=false
	//	"a世b" unfocused -> "a b       "  displaced=false
	//
	// Nor is it reachable for any component test: Buffer.Set and
	// SetString lay the continuation themselves, and render/cell.go says
	// of the remaining displacement branch that it is only reachable by
	// assigning Cells directly.
	for _, tw := range shapes {
		if tw.got != tw.want {
			t.Errorf("%s rendered %q, want %q — %s", tw.shape, tw.got, tw.want, tw.why)
		}
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

// TestADragDoesNotWalkTheWholeValue is a COST assertion over the INPUT
// path, and it is the same shape and the same reason as
// TestTheScrollWindowIsWalkedNotResummed above: the caret indexAt
// returns was right before this fix and is right after it, so every
// assertion about the answer passes over the defect.
//
// #519's fix put a whole-value segmentation into indexAt — a
// `string(runes)` copy plus two []int with an entry per cluster, on
// every call — and HandleMouseMove calls it on every motion event, on
// the UI goroutine, where motion arrives in bursts. Measured on the
// version this replaces: 202µs per call at 1,000 runes, 1.95ms at
// 10,000, 27.3ms at 100,000, 51.6ms at 200,000. The same O(len) shape
// this branch had already taken out of scrollFor, relocated one file
// over. Raised in review of #521.
//
// THE CARET SITS MID-VALUE, AND THAT IS THE WHOLE FIXTURE. It sat at
// the END until #521's review measured what that cost: the window then
// holds the last 39 runes, so every string(runes[lo:to]) inside the call
// is ~103 runes and the O(len) the test is named for is not on any path
// it walks. The test passed against the defect. Mid-value the same 100
// events took 317ms here and 826ms on the review's runner, against this
// same 500ms budget — so the fixture, not the budget, was what made this
// green.
//
// BOTH DIRECTIONS, because they had different bills and only one of them
// was bounded by the offset. Dragging LEFT of the field walks a cluster
// at a time, and each step called clusterStartAt and caretCols, each of
// which copied the tail of the value: 21.9ms per motion event at 100,000
// runes, against 3.74ms for the forward walk. Measured after the fix, on
// this fixture: 1.1ms forward and 9.3ms left, for all hundred events.
//
// A HUNDRED EVENTS, because one call of the defective shape is already
// slow but a drag is not one call — and because a per-call figure over a
// single sample is what a loaded runner turns into a flake. The budget
// is one order of magnitude over the measurement and two under the
// defect, which is the room a shared runner needs; the earlier version
// of this sentence claimed that spread while the fixture was hiding the
// defect entirely.
func TestADragDoesNotWalkTheWholeValue(t *testing.T) {
	for _, tc := range []struct {
		name string
		x    int
		want int
	}{
		// The window holds the 39 runes before the caret plus its own
		// column, so column 20 is that many runes in from its left edge.
		{"forward", 20, 200000/2 - 39 + 20},
		// Ten columns left of the field is ten runes left of the window.
		{"drag left", -10, 200000/2 - 39 - 10},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const n = 200000
			v := prop.NewSource(strings.Repeat("a", n))
			tb := &TextBox{Text: v}
			tb.SetFocused(true)
			tb.setCaret(n / 2)
			gooey.Compose(tb, term.Caps{Cols: 40, Rows: 1}, nil)
			tb.HandleMouse(input.MouseEvent{Kind: input.MousePress, X: 39, Y: 0, Button: input.ButtonLeft})

			start := time.Now()
			for range 100 {
				tb.HandleMouseMove(input.MouseEvent{X: tc.x, Y: 0, Button: input.ButtonLeft})
			}
			took := time.Since(start)

			// The answer first: a budget over a wrong caret proves nothing.
			if got := tb.Caret(); got != tc.want {
				t.Fatalf("a drag to column %d put the caret at %d, want %d", tc.x, got, tc.want)
			}
			if took > 500*time.Millisecond {
				t.Errorf("100 motion events over a %d-rune value took %v, want well under "+
					"500ms; that is the whole-value segmentation #521's review measured "+
					"at 51.6ms a call on the UI goroutine", n, took)
			}
		})
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

// reversedText is the text of the reversed cells in the first w columns
// of row 0, in order.
//
// Cell.Text() rather than Cell.Rune, because a reversed cell may hold a
// multi-rune cluster and a rune would report only its lead — which is
// the distinction three of the tests below exist to make. A wide
// cluster's continuation cell carries no text, so a two-column glyph
// still contributes its cluster once.
func reversedText(f *gooey.Frame, w int) string {
	var b strings.Builder
	for x := 0; x < w; x++ {
		if c := f.Cells.At(x, 0); c.Style.Reverse {
			b.WriteString(c.Text())
		}
	}
	return b.String()
}

// TestAnEmojiPresentationSequenceScrollsByItsColumnsNotItsRunes is
// #519's defect on the SCROLL path, reported against the fix for it.
//
// render.RuneWidth('⚠') is 1 and VS16 is zero, so window arithmetic
// that sums rune widths counted "⚠️" as ONE column where Render, now
// cluster-based, draws TWO. The window was sized for twice the content
// it could hold and never moved: a field of six emoji in six columns
// stayed at scroll 0 and showed no caret at all, so a user typing past
// column 6 saw nothing appear. Measured in review of #521 —
//
//	VS16 before: scroll=0, row "⚠️⚠️⚠️", no caret block
//	CJK control: scroll=4, row "東東█ "
//
// — which is why the CJK arm is here: it passed against the bug, so an
// assertion that only exercised it would have agreed with it.
func TestAnEmojiPresentationSequenceScrollsByItsColumnsNotItsRunes(t *testing.T) {
	for _, tc := range []struct {
		name    string
		glyph   string
		wantRow string
	}{
		{"emoji presentation sequence", "⚠️", "⚠️⚠️█ "},
		{"CJK control", "東", "東東█ "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value := strings.Repeat(tc.glyph, 6) // six clusters, twelve columns
			runes := []rune(value)
			tb := &TextBox{Text: prop.NewSource(value)}
			tb.SetFocused(true)
			tb.setCaret(len(runes)) // past the end, where the block is drawn
			f := gooey.Compose(tb, term.Caps{Cols: 6, Rows: 1}, nil)

			if tb.scroll == 0 {
				t.Errorf("a six-column window over twelve columns of %q did not "+
					"scroll at all: the window is being measured in runes, and "+
					"%d of them is not %d columns", tc.glyph, len(runes),
					render.StringWidth(value))
			}
			if got := render.SpanText(f.Cells, 0, 0, 6); got != tc.wantRow {
				t.Errorf("row %q, want %q — the caret is past the last glyph and "+
					"the block that draws it has to be inside the field",
					got, tc.wantRow)
			}
		})
	}
}

// TestTheCaretIsVisibleOnAnEmojiPresentationSequenceAtTheWindowsEdge is
// TestTheCaretIsVisibleOnAWideGlyphAtTheWindowsEdge one Unicode
// category over.
//
// The caret ON a character is drawn by reversing the CLUSTER it is in,
// and Render stops on that cluster's full width — so the columns
// scrollFor reserves for it have to be the cluster's too. Reserving
// render.RuneWidth(runes[i]) is 1 for the lead of "⚠️", because VS16 is
// zero-width, so the window kept a single column for a glyph needing
// two and the glyph the caret was riding fell off the right edge:
//
//	VS16 before: row "x ", no reversed cell
//	CJK control: row "東",  reversed "東"
//
// Measured in review of #521. The assertion is the reversed CLUSTER
// rather than a reversed rune, because the lead of "⚠️" reverses under
// the bug too — only its width is wrong.
func TestTheCaretIsVisibleOnAnEmojiPresentationSequenceAtTheWindowsEdge(t *testing.T) {
	for _, tc := range []struct{ name, value, want string }{
		{"emoji presentation sequence", "x⚠️", "⚠️"},
		{"CJK control", "x東", "東"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tb := &TextBox{Text: prop.NewSource(tc.value)}
			tb.SetFocused(true)
			tb.setCaret(1) // on the wide glyph, one Right from Home
			f := gooey.Compose(tb, term.Caps{Cols: 2, Rows: 1}, nil)

			if got := reversedText(f, 2); got != tc.want {
				t.Errorf("the reversed cells spell %q, want %q; the row reads %q. "+
					"The caret is on the second character of %q in a field two "+
					"columns wide, so that character is the whole field and it "+
					"has to be both drawn and reversed",
					got, tc.want, render.SpanText(f.Cells, 0, 0, 2), tc.value)
			}
		})
	}
}

// TestTheWindowOpensOnAClusterBoundaryAndStillFits pins the two halves
// of scrollFor's start against each other.
//
// They were in tension: windowFloor stopped at the first glyph that
// would not fit, and the snap that follows it walked LEFT off a
// zero-width rune onto the lead — re-adding exactly the column
// windowFloor had excluded. On decomposed "áxy" in three columns that
// put the whole value on screen with nowhere left for the caret:
//
//	NFD before:    scroll=0, row "áxy", no caret block
//	ASCII control: scroll=1, row "xy█"
//
// Measured in review of #521; NFD is not exotic, macOS hands filenames
// over decomposed. The scroll index is asserted as well as the row
// because the row alone cannot see a window that opens INSIDE the "á":
// Render drops the orphaned mark and paints "xy█" either way, while
// indexAt still segments from the start of the value, so the two
// disagree about where the user just clicked.
func TestTheWindowOpensOnAClusterBoundaryAndStillFits(t *testing.T) {
	for _, tc := range []struct {
		name, value string
		wantScroll  int
	}{
		{"decomposed", "áxy", 2},
		{"ASCII control", "axy", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runes := []rune(tc.value)
			tb := &TextBox{Text: prop.NewSource(tc.value)}
			tb.SetFocused(true)
			tb.setCaret(len(runes)) // past the end
			f := gooey.Compose(tb, term.Caps{Cols: 3, Rows: 1}, nil)

			if tb.scroll != tc.wantScroll {
				t.Errorf("the window over %q opens at rune %d, want %d — %d is "+
					"not a cluster boundary, so Render and indexAt would answer "+
					"different questions about column 0", tc.value, tb.scroll,
					tc.wantScroll, tb.scroll)
			}
			if got := render.SpanText(f.Cells, 0, 0, 3); got != "xy█" {
				t.Errorf("row %q, want \"xy█\" — three columns cannot hold %q and "+
					"the caret both, and the caret is the half that may not be "+
					"dropped", got, tc.value)
			}
		})
	}
}

// TestASelectionOverHalfAClusterHighlightsTheWholeGlyph is the
// selection arm's half of the cluster rule.
//
// It tested the cluster's FIRST rune (`i >= lo && i < hi`) while the
// caret arm beside it tested containment, so a selection covering only
// the zero-width half of a cluster highlighted nothing — and `selected`
// suppresses the caret arm, so the field showed neither selection nor
// caret:
//
//	[1,2) before: reversed "",  row "éx"
//	[1,3) before: reversed "x", row "éx"
//	ASCII control [1,2): reversed "x"
//
// Measured in review of #521. Half a cluster is not a state a cell can
// draw; reversing the whole glyph is the only answer one has, which is
// the answer the caret arm already gives.
func TestASelectionOverHalfAClusterHighlightsTheWholeGlyph(t *testing.T) {
	// "éx" decomposed: e, U+0301, x. Rune 1 is the mark, and it is
	// inside the é the user sees.
	const nfd = "éx"
	for _, tc := range []struct {
		name, value   string
		caret, anchor int
		want          string
	}{
		{"the mark alone", nfd, 1, 2, "e\u0301"},
		{"the mark and the rune after it", nfd, 1, 3, "e\u0301x"},
		{"ASCII control", "ex", 1, 2, "x"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tb := &TextBox{Text: prop.NewSource(tc.value)}
			tb.SetFocused(true)
			tb.setCaret(tc.caret)
			tb.setAnchor(tc.anchor)
			f := gooey.Compose(tb, term.Caps{Cols: 4, Rows: 1}, nil)

			if got := reversedText(f, 4); got != tc.want {
				t.Errorf("selection [%d,%d) over %q reverses %q, want %q; the row "+
					"reads %q. A selection the user made and cannot see is worse "+
					"than no selection: the caret arm is suppressed while one is "+
					"live, so the field shows nothing at all",
					min(tc.caret, tc.anchor), max(tc.caret, tc.anchor), tc.value,
					got, tc.want, render.SpanText(f.Cells, 0, 0, 4))
			}
		})
	}
}

// family is a four-person ZWJ emoji: SEVEN runes, whose rune widths sum
// to eight, drawn in TWO columns. It is the fixture that separates the
// three units this file has to keep apart — runes, rune-width sums and
// grapheme columns — because it disagrees with both of the first two.
const family = "\U0001F469‍\U0001F469‍\U0001F467‍\U0001F466"

// clusterBoundaries lists every grapheme boundary of runes[:end],
// segmented from index 0.
//
// It is DELIBERATELY the slow, obvious answer — the O(len(value)) walk
// that #521 removed from the paint path — because a reference the
// implementation could share a shortcut with would agree with the bug.
func clusterBoundaries(runes []rune, end int) []int {
	var bs []int
	idx := 0
	render.EachCluster(string(runes[:end]), func(cluster string, _, _, _ int) bool {
		bs = append(bs, idx)
		idx += len([]rune(cluster))
		return true
	})
	return append(bs, end)
}

// widthVocabulary is a value of each shape whose runes, rune-width sum
// and column count can disagree, for the two grid tests below.
//
// Each entry is here because it breaks a different estimate: the family
// over-counts under a rune sum and under-counts under a rune COUNT, the
// VS16 emoji under-counts under both, CJK agrees with a rune sum and
// not with a rune count, and the decomposed run has more runes than
// either. A grid over only one of them would agree with itself.
func widthVocabulary() []string {
	return []string{
		"abcdefghij" + family + "xyz0123456789",
		strings.Repeat(family, 5),
		strings.Repeat("⚠️", 8),
		strings.Repeat("é", 10),
		strings.Repeat("abc"+family+"東⚠️", 3),
		// LONGER THAN clusterSlack, AND FLAGS, which are two coverage
		// holes in one entry. Every other string here is shorter than
		// the 64-rune lookback, so eachClusterFrom's re-synchronising
		// branch — the whole reason clusterSlack exists — was never
		// executed by any test in this PR. And a regional-indicator
		// boundary is the one a lookback cannot re-synchronise on at
		// all: it is decided by the parity of the run, so a segmenter
		// restarted mid-run pairs every flag from there one rune out.
		// Measured against the implementation before the fix, this entry
		// reddened TestTheScrollWindowAlwaysOpensOnAClusterBoundary at
		// caret 65 — the window opened inside a cluster, Render painted
		// "🇸🇺" for a value holding only "🇺🇸", and indexAt answered a
		// click on column 0 with rune 64. Two runs, because the parity
		// question is about the run and not about the flag. Raised in
		// review of #521.
		strings.Repeat("🇺🇸", 40) + strings.Repeat("🇬🇧", 4),
	}
}

// TestTheWindowFloorIsTheLeftmostFittingClusterBoundary is a grid
// against the definition rather than a fixture against a symptom.
//
// windowFloor answers "the leftmost index a window of avail columns can
// start at and still show runes[:end] with reserve to spare", and that
// sentence is checkable directly: walk every grapheme boundary of the
// span from the left and take the first whose remaining text fits. The
// implementation may not do it that way — an O(len) walk on the paint
// path is the 1.4 s freeze this PR exists to have removed — but it has
// to AGREE with it.
//
// A grid rather than examples because the three defects found in review
// of #521 were each one example away from each other: a rune-width sum
// that stopped too far right on a ZWJ family, a candidate returned
// without snapping to its cluster's start, and an expansion that never
// looked left of a candidate that had already overshot. All three are
// invisible to a fixture chosen for any one of them.
func TestTheWindowFloorIsTheLeftmostFittingClusterBoundary(t *testing.T) {
	for vi, v := range widthVocabulary() {
		runes := []rune(v)
		for end := 0; end <= len(runes); end++ {
			bs := clusterBoundaries(runes, end)
			for avail := 1; avail <= 10; avail++ {
				for reserve := 0; reserve <= 2; reserve++ {
					want := end
					for _, b := range bs {
						if reserve+render.StringWidth(string(runes[b:end])) <= avail {
							want = b
							break
						}
					}
					if got := windowFloor(runes, end, reserve, avail); got != want {
						t.Fatalf("value %d, end=%d reserve=%d avail=%d: windowFloor "+
							"says %d, the leftmost fitting cluster boundary is %d. "+
							"%q is %d columns and the window holds %d",
							vi, end, reserve, avail, got, want,
							string(runes[want:end]),
							render.StringWidth(string(runes[want:end])), avail-reserve)
					}
				}
			}
		}
	}
}

// TestTheScrollWindowAlwaysOpensOnAClusterBoundary pins the property
// Render and indexAt both depend on and neither can check.
//
// Render re-segments from t.scroll; indexAt segments from 0. While the
// window opens on a boundary the two see the same glyphs in the same
// columns. While it does not, they disagree about what is in the first
// column, so a click there answers with a character off-screen to the
// left and the window jumps on the next frame — and NOTHING about the
// painted row says so, which is why this is a grid over scrollFor
// rather than an assertion about cells.
//
// The second half is the reason scrollFor exists at all: the caret's
// own cluster is never left of the window.
func TestTheScrollWindowAlwaysOpensOnAClusterBoundary(t *testing.T) {
	for vi, v := range widthVocabulary() {
		runes := []rune(v)
		boundary := map[int]bool{}
		for _, b := range clusterBoundaries(runes, len(runes)) {
			boundary[b] = true
		}
		for avail := 1; avail <= 8; avail++ {
			for cur := 0; cur <= len(runes); cur++ {
				for caret := 0; caret <= len(runes); caret++ {
					got := scrollFor(runes, cur, caret, avail)
					if !boundary[got] {
						t.Fatalf("value %d, avail=%d cur=%d caret=%d: the window opens "+
							"at rune %d, which is inside a cluster — Render would paint "+
							"a glyph %q does not contain, and indexAt would answer a "+
							"click on column 0 with a different rune",
							vi, avail, cur, caret, got, v)
					}
					if cs := clusterStartAt(runes, caret); cs < got {
						t.Fatalf("value %d, avail=%d cur=%d caret=%d: the window opens at "+
							"rune %d, right of the caret's own cluster at %d — the user "+
							"is typing at a position off the left of the field",
							vi, avail, cur, caret, got, cs)
					}
				}
			}
		}
	}
}

// TestAFourPersonFamilyIsWindowedByItsColumnsNotItsRunes is the two
// grids above reduced to the three rows a user would see.
//
// A four-person family is seven runes, a rune-width sum of eight, and
// two columns — so every arithmetic that is not the cluster's own gets
// it wrong, and each arm here is one of the ways that was measured in
// review of #521:
//
//	three families in eight columns: showed two, scrolled the third away
//	the caret on the x after one:    opened the window inside the family
//	the caret inside the family:     counted the family's columns twice
//
// The rows are asserted whole because the defect is a glyph that is on
// screen or is not; the CJK and ASCII spellings of the same shapes are
// already covered by the fixtures above, which is what makes these
// three arms about the FAMILY rather than about wide text.
func TestAFourPersonFamilyIsWindowedByItsColumnsNotItsRunes(t *testing.T) {
	const around = "abcdefghij" + family + "xyz0123456789"
	for _, tc := range []struct {
		name       string
		value      string
		caret      int
		cols       int
		wantScroll int
		wantRow    string
	}{
		{
			// 21 runes, 6 columns: all three fit in 8 with the caret's
			// column to spare, so none of them may be scrolled off.
			"three families and the caret fit whole",
			strings.Repeat(family, 3), 21, 8, 0,
			strings.Repeat(family, 3) + "█ ",
		},
		{
			// The family is two columns and the x is one: 'j', the
			// family and the x fit in four. Answering with the rune the
			// column walk stopped on opened the window inside the
			// family, which paints a TWO-person family instead.
			"the window opens at the family's own start",
			around, 17, 4, 9, "j" + family + "x",
		},
		{
			// The caret is inside the family — an arrow key steps by
			// rune. Its width is reserved once, as the caret's, and the
			// span the floor measures ends where its cluster begins;
			// counting it in both scrolled "ij" away for nothing.
			"the caret's own cluster is counted once",
			around, 11, 4, 8, "ij" + family,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tb := &TextBox{Text: prop.NewSource(tc.value)}
			tb.SetFocused(true)
			tb.setCaret(tc.caret)
			f := gooey.Compose(tb, term.Caps{Cols: tc.cols, Rows: 1}, nil)

			if tb.scroll != tc.wantScroll {
				t.Errorf("the window opens at rune %d, want %d", tb.scroll, tc.wantScroll)
			}
			if got := render.SpanText(f.Cells, 0, 0, tc.cols); got != tc.wantRow {
				t.Errorf("row %q, want %q — a family is seven runes and a "+
					"rune-width sum of eight, and neither of those is the two "+
					"columns it occupies", got, tc.wantRow)
			}
		})
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

// TestAFlagRunCostsTheSameWhateverItsLength is a COST assertion shaped
// as a SCALING one, and the shape is the point: a budget in
// milliseconds measures the runner, a ratio between two runs on the same
// machine in the same process measures the algorithm.
//
// eachClusterFrom walks off a regional-indicator run before it starts,
// because UAX #29 GB12/GB13 decide a flag boundary by the parity of the
// whole run and a segmenter restarted mid-run pairs every flag from
// there one rune out. Walking to the run's OWN START restores the
// parity and makes the segmented span as long as the run — so the fix
// for a correctness defect put an O(run) cost on the paint path and on
// every column of a drag. Measured, 1,000 clusterStartAt calls:
//
//	                     500 flags   5,000 flags   ratio
//	walk to the start      53.1 ms      517.1 ms    9.7x
//	drop to even parity     7.6 ms       10.7 ms    1.4x
//
// Any EVEN offset into the run is a true pair boundary, so the parity
// survives while the span stays bounded by clusterSlack. The residual
// cost is the walk that FINDS the run's start, which is a rune
// comparison per step and segments nothing.
//
// FOUR TIMES is the budget because both halves are wrong by more than
// that: 2.8x above the measurement and 2.4x below the defect, on a
// quantity that does not move with the machine. Raised in review of
// #521.
func TestAFlagRunCostsTheSameWhateverItsLength(t *testing.T) {
	cost := func(pairs int) time.Duration {
		runes := []rune(strings.Repeat("\U0001F1FA\U0001F1F8", pairs))
		start := time.Now()
		for range 1000 {
			clusterStartAt(runes, len(runes)/2)
		}
		return time.Since(start)
	}
	short, long := cost(500), cost(5000)
	if long > 4*short {
		t.Errorf("1,000 clusterStartAt calls cost %v over 5,000 flags against %v "+
			"over 500 — %.1fx for ten times the run, want under 4x. The "+
			"regional-indicator walk-back is segmenting the run rather than "+
			"dropping to the nearest even offset in it, which is O(run) on "+
			"the paint path", long, short, float64(long)/float64(short))
	}
}

// TestAValueThatOpensWithACombiningMarkIsPaintedAndCarets pins the two
// halves of one refusal: TextBox.Render skipped a zero-width cluster
// outright, so a value beginning with a combining mark lost that
// character on screen AND the caret on it was drawn nowhere.
//
// THE ORACLE IS THE FRAMEWORK'S OWN WRITER, not a fixture string, which
// is the whole point of the assertion. render.Buffer.SetString is what
// every other component paints text through, and it gives such a cluster
// its own column; a TextBox that disagrees is a character in the bound
// property with nothing on screen, which is #519 one Unicode category
// over. Measured before the fix: the field painted "abc       " against
// SetString's "\u0301abc      ", with zero reversed cells in a focused
// field. Raised in review of #521.
//
// THE SECOND ARM IS NOT IMPLIED BY THE FIRST. Painting the mark gives it
// a cell; it does not follow that the caret arm reaches it, because that
// arm tests containment against a cluster the loop might still have
// passed over. A field the user is typing into with no caret anywhere is
// the injury, and only a reversed-cell count sees it.
func TestAValueThatOpensWithACombiningMarkIsPaintedAndCarets(t *testing.T) {
	const value = "\u0301abc"
	const cols = 10

	tb := &TextBox{Text: prop.NewSource(value)}
	tb.SetFocused(true)
	tb.setCaret(0)
	f := gooey.Compose(tb, term.Caps{Cols: cols, Rows: 1}, nil)

	want := render.NewBuffer(cols, 1)
	want.SetString(0, 0, value, render.Style{})
	if got, w := render.RowText(f.Cells, 0), render.RowText(want, 0); got != w {
		t.Errorf("a value opening with a combining mark painted %q, want %q — "+
			"what render.Buffer.SetString writes for the same string. A "+
			"character in the bound property and absent from the screen is "+
			"the defect #519 is about", got, w)
	}

	reversed := 0
	for x := 0; x < cols; x++ {
		if f.Cells.At(x, 0).Style.Reverse {
			reversed++
		}
	}
	if reversed != 1 {
		t.Errorf("%d cells reversed with the caret at 0 of %q, want 1: the user "+
			"is typing into a focused field whose caret is nowhere on screen. "+
			"Row: %q", reversed, value, render.RowText(f.Cells, 0))
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
