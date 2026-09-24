package components

import (
	"math"
	"slices"
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

	if got, want := render.RowText(f.Cells, 0), "> hi█     "; got != want {
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
	if got := render.RowText(f.Cells, 0); !strings.Contains(got, "p") {
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
		return render.RowText(f.Cells, 0)
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
	// is false and the loop is gone. Taking either side of the first
	// conflict wholesale would have been wrong — this branch's side lost
	// the two shapes #520 added after review, and the base's side
	// reinstated a skip citing an issue this branch closes. The
	// resolution is settled here; a later merge of the same base is the
	// same decision again, and the only thing the base has changed in
	// this function since is dropping a provenance marker.
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

// TestARepaintDoesNotWalkTheWholeValue is the third of the three cost
// assertions, over the PAINT path, and it is the one that was missing.
//
// Render segmented `string(runes[t.scroll:])` — the whole tail, copied
// on every paint. Its own doc argued the bound could not be computed,
// because a rune sum is neither an upper nor a lower bound on its
// clusters' widths and guessing short drops glyphs off the right of the
// field. True of guessing and false of WALKING: spanForCols doubles the
// span until the walk has passed the column asked for, so it never
// guesses and never touches more than the window shows.
//
// Measured on this tree, a 40-column focused field with the caret
// mid-value, one repaint per frame, 100 frames:
//
//	  runes   before    after
//	  1,000    88 µs   103 µs
//	 10,000   149 µs    95 µs
//	100,000   558 µs    99 µs
//	200,000   997 µs    94 µs
//
// A RATIO BETWEEN TWO LENGTHS, not a budget in milliseconds, for
// TestTheScrollWindowIsWalkedNotResummed's reason one file up: the ratio
// measures the algorithm and a millisecond measures the runner. Before
// the fix it is 11x for 200x the value; the budget of 4 is 4x above the
// flat measurement and 2.8x below the defect.
//
// THE ROW FIRST, because a budget over a field painting nothing is
// trivially met — the exact hazard spanForCols introduces, since a span
// guessed short paints a short row rather than failing.
func TestARepaintDoesNotWalkTheWholeValue(t *testing.T) {
	cost := func(n int) (time.Duration, string, string) {
		v := prop.NewSource(strings.Repeat("a", n))
		st := prop.NewSource(render.Style{})
		tb := &TextBox{Text: v, Style: st}
		tb.SetFocused(true)
		tb.setCaret(n / 2)
		c := gooey.NewComposer(tb, 40, 1)
		f, _ := c.Frame()
		row := render.RowText(f.Cells, 0)
		// THROUGH THE HELPER, which is the file's one inventory of
		// this scan: the hand-rolled version here re-wrote the
		// composer's own 40, and a frame widened later grows a tail of
		// phantom blanks that hold no reversed cell — so the count
		// came back SHORT rather than erroring. Counting cells also
		// says "one reversed cell" where the question is which glyph
		// the caret is on, and a wide one occupies two. Raised in
		// review of #521.
		caret := reversedText(t, f)
		const frames = 100
		start := time.Now()
		for i := range frames {
			// A REAL REPAINT, not a re-read of a clean node: Render is
			// a paint node, so a Frame() over an unchanged graph paints
			// nothing at all and would time the Composer's damage
			// bookkeeping instead.
			st.Set(render.Style{Bold: i%2 == 0})
			c.Frame()
		}
		return time.Since(start) / frames, row, caret
	}
	short, shortRow, shortCaret := cost(1000)
	long, longRow, longCaret := cost(200000)

	if want := strings.Repeat("a", 40); shortRow != want || longRow != want {
		t.Fatalf("the field painted %q at 1,000 runes and %q at 200,000, want "+
			"%q for both — a span walked short paints a short row, so a cost "+
			"assertion over it would be met by painting less", shortRow, longRow, want)
	}
	// ONE SPELLING OF THE ANSWER, compared and printed, so the message
	// cannot outlive the comparison: the value is a run of "a", so the
	// caret is on one of them wherever in the value it sits.
	const onCaret = "a"
	if shortCaret != onCaret || longCaret != onCaret {
		t.Fatalf("the caret is on %q at 1,000 runes and %q at 200,000, want the "+
			"one glyph %q each: the caret is mid-value, so a window that stopped "+
			"short of it would paint a full row with the caret nowhere on screen "+
			"— which the row assertion above cannot see", shortCaret, longCaret, onCaret)
	}
	if long > 4*short {
		t.Errorf("a repaint costs %v at 200,000 runes against %v at 1,000 — "+
			"%.1fx for two hundred times the value, want under 4x. Render is "+
			"segmenting the whole tail rather than the span spanForCols bounds, "+
			"which is O(len(value)) on the paint path, per keystroke, on the UI "+
			"goroutine", long, short, float64(long)/float64(short))
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

// TestADragDoesNotWalkAZeroWidthRun is TestADragDoesNotWalkTheWholeValue
// over the vocabulary that defeats its bound, and it is the FOURTH walk
// of this shape on this path — windowFloor's expansion, spanForCols'
// doubling and clusterStartAt's lookback are the other three, and this
// one had no cap until #521's review.
//
// indexAt's forward walk doubles its span until the walk has passed the
// column asked for. Every cluster contributes at least one column, so
// over any ordinary vocabulary that happens within a few rounds — but a
// value that is ONE cluster reports one column however far the span
// reaches, so the doubling ran to len(runes) and re-segmented a larger
// prefix each round. Measured per call, one click five columns into a
// 20-column field:
//
//	n         capped    uncapped    ASCII
//	  1,000   11.4µs    190µs       9.8µs
//	 10,000   11.0µs    1.90ms      15.7µs
//	 50,000   11.2µs    8.05ms      8.4µs
//	200,000   11.3µs    32.6ms      8.3µs
//
// Once per MOUSE MOTION REPORT while a drag is live, on the UI
// goroutine: ten reports of a drag over a 200,000-rune value is a third
// of a second of input latency, and the run is reachable by paste.
//
// A RATIO, NOT A FIGURE, for the reason the ASCII sibling gives: a
// wall-clock threshold is a property of the machine.
//
// THE ANSWER AND THE COST MOVE TOGETHER HERE, and that is deliberate
// rather than one assertion wearing two hats. Both lengths below are
// past the cap — (off+1)*clusterSlack is 1344 runes at column 20 — so
// the walk stops inside a cluster it cannot finish and answers with
// that cluster's start, where the uncapped walk reached the end of the
// value and answered len(runes). Removing the cap therefore changes
// BOTH observables, so both are asserted with Errorf and both report:
// the cost claim is not a proxy for the answer, and neither hides the
// other's failure. Below the cap the answer is unchanged, which
// TestAClickPastAShortZeroWidthRunLandsAtTheEnd is for.
// Raised in review of #521.
func TestADragDoesNotWalkAZeroWidthRun(t *testing.T) {
	drag := func(n int) (time.Duration, int) {
		v := prop.NewSource("a" + strings.Repeat("\u0301", n))
		tb := &TextBox{Text: v}
		tb.SetFocused(true)
		tb.setCaret(0)
		gooey.Compose(tb, term.Caps{Cols: 40, Rows: 1}, nil)
		tb.HandleMouse(input.MouseEvent{Kind: input.MousePress, X: 0, Y: 0, Button: input.ButtonLeft})
		const moves = 100
		start := time.Now()
		for range moves {
			tb.HandleMouseMove(input.MouseEvent{X: 20, Y: 0, Button: input.ButtonLeft})
		}
		return time.Since(start) / moves, tb.Caret()
	}
	short, shortCaret := drag(5000)
	long, longCaret := drag(200000)

	for _, tc := range []struct {
		n, got int
	}{{5000, shortCaret}, {200000, longCaret}} {
		if tc.got != 0 {
			t.Errorf("a drag to column 20 over a %d-rune value that is one "+
				"cluster put the caret at %d, want 0 — the cluster's own "+
				"start, which is the furthest left boundary this file can "+
				"prove anything about. len(runes) is the answer the "+
				"UNCAPPED walk gave, and it reached it by segmenting the "+
				"whole value", tc.n, tc.got)
		}
	}
	if long > 4*short {
		t.Errorf("a motion event over a 200,000-rune zero-width run costs %v "+
			"against %v over a 5,000-rune one — %.1fx for forty times the "+
			"value, want under 4x. indexAt's forward walk doubles its span "+
			"until the walk passes the column asked for, and a value that is "+
			"one cluster never gets there, so the span runs to len(runes): "+
			"O(len(value)) per motion report, on the UI goroutine",
			long, short, float64(long)/float64(short))
	}
}

// TestAClickPastAShortZeroWidthRunLandsAtTheEnd is the other side of
// that cap: BELOW it the walk still reaches the end of the value, and
// the answer to a click past the last painted column is still the end.
//
// The cap is (off+1)*clusterSlack runes, so at column 20 it bites at
// 1344 — a value shorter than that is answered exactly, however many of
// its runes are zero-width. Without this the cap could be tightened to
// nothing and only the cost arm above would notice.
func TestAClickPastAShortZeroWidthRunLandsAtTheEnd(t *testing.T) {
	const n = 1000
	v := "a" + strings.Repeat("\u0301", n)
	runes := []rune(v)
	tb := &TextBox{Text: prop.NewSource(v)}
	tb.SetFocused(true)
	tb.setCaret(0)
	gooey.Compose(tb, term.Caps{Cols: 40, Rows: 1}, nil)
	if got := tb.indexAt(20); got != len(runes) {
		t.Errorf("a click at column 20 of a field painting %d runes in one "+
			"column answered %d, want %d — the value ends at column 1, so "+
			"this is a click in the empty part of the field and the caret "+
			"belongs at the end", len(runes), got, len(runes))
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

	if got := reversedText(t, f); got != "西" {
		t.Errorf("the reversed cells of %q hold %q, want 西 — the caret sits ON "+
			"the character it precedes, and an empty answer means nothing on "+
			"screen says where it is", render.RowText(f.Cells, 0), got)
	}
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

			if got := reversedText(t, f); got != tc.want {
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

			if got := reversedText(t, f); got != tc.want {
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
		// OPENS WITH A BARE COMBINING MARK, which is a cluster the
		// segmenter reports at ZERO columns. Every other entry here
		// opens on something with a column of its own —
		// strings.Repeat("é", 10) opens with `e` — so no grid arm could
		// reach the one place Render and the window disagree by
		// construction: Render advances by max(w, 1) so the mark is
		// painted at all, and windowFloor summed the raw width, so it
		// believed in a column Render would not give it. In a
		// four-column field the window never scrolled and the caret
		// block fell outside the bounds — a focused field showing a
		// value and no caret. Measured in review of #521.
		"\u0301abc" + family + "\u0301\u0301xy",
	}
}

// longClusterVocabulary is widthVocabulary's entries plus one whose
// CLUSTERS are longer than clusterSlack — a different hole from "a
// string longer than clusterSlack", which widthVocabulary's flag entry
// already covers.
//
// Every widthVocabulary cluster is a handful of runes, so windowFloor's
// and spanForCols' reach — (cols+1)*clusterSlack until review of #521
// round 8 — was always ample and no grid could reach the branch where
// it is not. Four clusters of 101 runes, one column each: at avail 2
// the leftmost fitting boundary is 202 runes left of `end` and that old
// arithmetic reached only 192, so this entry fails against it and
// passes against spanReach's.
//
// SEPARATE FROM widthVocabulary, AND THAT IS A COST DECISION rather
// than a taxonomy one. TestTheScrollWindowAlwaysOpensOnAClusterBoundary
// runs avail x cur x caret over every entry, so it is QUADRATIC in the
// string's length — adding 404 runes there took the package's suite
// from 5 seconds to over 150. The property that grid owns is covered
// for this vocabulary by
// TestTheWindowOpensOnTheStartOfALongCluster, which asks the one
// question rather than 3 million.
func longClusterVocabulary() []string {
	return append(widthVocabulary(), strings.Repeat("a"+strings.Repeat("\u0301", 100), 4))
}

// clusterCols is what the SCREEN costs for a span: every cluster's
// width, each floored at one column.
//
// NOT render.StringWidth, and the difference is the whole of #521's
// round-5 finding 1. StringWidth counts a zero-width cluster as 0, which
// is the right answer about Unicode and the wrong one about this
// component: Render advances by max(w, 1) (`components/textbox.go`, the
// arm added so a value opening with a combining mark is painted at all),
// so a leading mark occupies a column on screen. The oracle used
// StringWidth, so it encoded windowFloor's model rather than Render's
// and AGREED WITH THE DEFECT — a grid over six vocabulary entries stayed
// green over a field that scrolled one column short.
//
// Like clusterBoundaries, this is deliberately the slow obvious walk. A
// reference that shared the implementation's shortcut would share its
// bug.
func clusterCols(runes []rune) int {
	cols := 0
	render.EachCluster(string(runes), func(cluster string, _, _, _ int) bool {
		cols += max(render.StringWidth(cluster), 1)
		return true
	})
	return cols
}

// TestTheWindowFloorIsTheLeftmostFittingClusterBoundary is a grid
// against the definition rather than a fixture against a symptom.
//
// TestAClickAnswersTheClusterItsColumnPaints is the paint/click
// agreement, asserted column by column against this file's own oracle
// rather than against either walk.
//
// Render and indexAt segment from the SAME boundary — `t.scroll`, which
// TestTheScrollWindowAlwaysOpensOnAClusterBoundary pins — so they can
// only disagree about a column if one of them stops walking before the
// other. That is what a span bound sized by the CLICKED COLUMN does:
// `off < avail` for every click inside the field, so indexAt's reach
// was by construction narrower than the one Render painted from, and at
// the cap the two answered different clusters for the same column.
// Measured on ("a" + U+0301 x 100) x 40 in a 20-column field at scroll
// 0, before the fix:
//
//	column 13 painted the cluster at rune 1313, indexAt said 808
//	column 10 painted the cluster at rune 1010, indexAt said 606
//	column  5 painted the cluster at rune  505, indexAt said 303
//
// Reached through the cap rather than through a mid-cluster t.scroll,
// so no grid over scrollFor could see it. Raised in review of #521.
//
// THE ORACLE IS clusterBoundaries + clusterCols, which segment the
// whole value from rune 0 and know nothing about either bound. Deriving
// the expected column map from spanForCols would pin the walk against
// itself.
//
// BOTH HALVES ARE COMPARED TO IT, and for a while only one was. The
// name promises an AGREEMENT and the doc above argues it from two
// walks, but the loop asserted indexAt alone and used the frame only to
// print a row in the failure message — so a Render that stopped short
// of the oracle was invisible here, which is the state review of #521
// round 10 measured. The two halves catch different mutations and that
// is the reason to keep both:
//
//	indexAt half — catches a span bound sized by the CLICKED column
//	               (the cap defect above: column 13 answered 808).
//	paint  half — catches spanForCols' adaptive reach. Replacing the
//	               `if reach := spanReach(cols, widest)` branch with
//	               `_ = widest` leaves the whole components suite green
//	               without it: the field paints 14 of 20 columns and
//	               truncates the thirteenth cluster to 95 runes, a glyph
//	               the value does not contain. Measured both ways.
//
// A continuation column is skipped rather than asserted empty on its
// own: Cell.Text() answers "" for one, so folding it into the same
// comparison would make every wide cluster's tail look like a hole.
func TestAClickAnswersTheClusterItsColumnPaints(t *testing.T) {
	const cols = 20
	v := strings.Repeat("a"+strings.Repeat("\u0301", 100), 40)
	runes := []rune(v)

	tb := &TextBox{Text: prop.NewSource(v)}
	tb.SetFocused(true)
	tb.setCaret(0)
	f := gooey.Compose(tb, term.Caps{Cols: cols, Rows: 1}, nil)
	b := tb.Bounds()

	// Which cluster owns each column, counted from the window.
	bs := clusterBoundaries(runes, len(runes))
	owner := map[int]int{}
	col := 0
	for k := 0; k+1 < len(bs) && col < cols; k++ {
		if bs[k] < tb.scroll {
			continue
		}
		for range clusterCols(runes[bs[k]:bs[k+1]]) {
			if col < cols {
				owner[col] = bs[k]
			}
			col++
		}
	}
	if len(owner) < cols {
		t.Fatalf("the oracle filled only %d of %d columns from scroll %d — the "+
			"fixture is not wide enough to ask the question", len(owner), cols, tb.scroll)
	}
	// The oracle's cluster TEXT, so the paint half compares what the
	// cell holds rather than an index derived from it.
	next := map[int]int{}
	for k := 0; k+1 < len(bs); k++ {
		next[bs[k]] = bs[k+1]
	}
	for c := 0; c < cols; c++ {
		if got := tb.indexAt(b.X + c); got != owner[c] {
			t.Errorf("a click on column %d answered rune %d, but that column "+
				"paints the cluster starting at rune %d. Render and indexAt "+
				"walk from the same window, so this is one of them stopping "+
				"short of the other: the click lands on a character that is "+
				"not under the pointer. Row: %q",
				c, got, owner[c], render.RowText(f.Cells, 0))
		}
		if c > 0 && owner[c] == owner[c-1] {
			continue // a continuation column of a wide cluster
		}
		want := string(runes[owner[c]:next[owner[c]]])
		if got := f.Cells.At(b.X+c, 0).Text(); got != want {
			t.Errorf("column %d paints %q (%d runes), but the oracle says that "+
				"column holds the cluster at rune %d, which is %d runes. Render "+
				"stopped short of the boundary the value actually has, so the "+
				"field shows a glyph the text does not contain. Row: %q",
				c, got, len([]rune(got)), owner[c], len([]rune(want)),
				render.RowText(f.Cells, 0))
		}
	}
}

// TestTheWindowOpensOnTheStartOfALongCluster is
// TestTheScrollWindowAlwaysOpensOnAClusterBoundary's property over the
// one vocabulary that grid cannot afford — see longClusterVocabulary for
// why it is not simply an entry there.
//
// It found a real one. eachClusterFrom DROPS its own first cluster
// whenever it re-synchronised, so clusterStartAt's single fixed-lookback
// pass reported nothing at all for an index more than clusterSlack runes
// into a cluster, and answered the untouched index — an arithmetic
// number wearing a boundary's name. Measured on six clusters of 101
// runes: clusterStartAt(65) was 65 and the window opened there, inside
// the glyph, which is the paint/click disagreement the grid's own doc
// calls the thing the cluster-boundary design exists to prevent.
// clusterEndAt(65) was 66 against a cluster ending at 101, one function
// over and the same fixed span. Both retry now, bounded by spanReach.
//
// THE CARETS ARE THE INTERESTING ONES, not a sweep: rune 0 is the
// cluster's own start, 65 is past the lookback inside the first
// cluster, 150 is past it inside the second, and len is the end.
func TestTheWindowOpensOnTheStartOfALongCluster(t *testing.T) {
	const marks = 100
	v := strings.Repeat("a"+strings.Repeat("\u0301", marks), 4)
	runes := []rune(v)
	boundary := map[int]bool{}
	for _, b := range clusterBoundaries(runes, len(runes)) {
		boundary[b] = true
	}
	for _, caret := range []int{0, 65, 150, len(runes)} {
		for avail := 1; avail <= 4; avail++ {
			for _, cur := range []int{0, 65, len(runes)} {
				got := scrollFor(runes, cur, caret, avail)
				if !boundary[got] {
					t.Errorf("avail=%d cur=%d caret=%d: the window opens at rune %d, "+
						"which is inside a cluster — Render would paint a glyph the "+
						"value does not contain, and indexAt would answer a click on "+
						"column 0 with a different rune", avail, cur, caret, got)
				}
				if cs := clusterStartAt(runes, caret); cs < got {
					t.Errorf("avail=%d cur=%d caret=%d: the window opens at rune %d, "+
						"right of the caret's own cluster at %d — the user is typing "+
						"at a position off the left of the field",
						avail, cur, caret, got, cs)
				}
			}
		}
	}
	// AND THE TWO PRIMITIVES DIRECTLY, because the window above can be
	// right for a reason that is not theirs.
	if got := clusterStartAt(runes, 65); got != 0 {
		t.Errorf("clusterStartAt(65) = %d, want 0 — 65 is %d runes into a "+
			"%d-rune cluster, which is more than clusterSlack, so a single "+
			"fixed lookback re-synchronises inside it and reports nothing",
			got, 65, marks+1)
	}
	if got := clusterEndAt(runes, 65); got != marks+1 {
		t.Errorf("clusterEndAt(65) = %d, want %d — a cluster longer than "+
			"clusterSlack does not fit the span this used to walk, so it "+
			"answered the arithmetic i+1", got, marks+1)
	}
}

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
	for vi, v := range longClusterVocabulary() {
		runes := []rune(v)
		for end := 0; end <= len(runes); end++ {
			bs := clusterBoundaries(runes, end)
			for avail := 1; avail <= 10; avail++ {
				for reserve := 0; reserve <= 2; reserve++ {
					want := end
					for _, b := range bs {
						if reserve+clusterCols(runes[b:end]) <= avail {
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
							clusterCols(runes[want:end]), avail-reserve)
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
	// THE WHOLE CLUSTER, spelled with the escape rather than the glyph:
	// the fixture is DECOMPOSED and a precomposed literal here would look
	// identical in the source and compare unequal.
	if want, got := "e\u0301", reversedText(t, f); got != want {
		t.Errorf("the caret is on the combining mark and the reversed cells "+
			"hold %q, want the whole cluster %q: the caret belongs to the one "+
			"glyph on screen, and nothing reversed means nothing on screen says "+
			"where it is", got, want)
	}
}

// TestAFieldWithNoRoomLeftStillSubscribesToItsStyles is the pin for the
// Style/InvalidStyle hoist, and a damage count is the only instrument
// that can make it — CLAUDE.md: "a damage-count assertion is the only
// pin for a repaint claim".
//
// Render's early returns are where the Get-order rule bites: a Get below
// one drops out of the dependency set on the frames that take it, and
// the component goes deaf to that property with no error and no panic.
// The commit that hoisted these two argued it costs no observable
// STALENESS, which is true of the pixels — a prompt filling the field
// paints nothing that reads either style. It is not true of the
// SUBSCRIPTION, and that is observable: with the Gets hoisted, setting
// the style invalidates the paint node and the next frame repaints 1;
// with them back below the `avail <= 0` return, it repaints 0. Nothing
// else in this package could see that, which is why a mutation moving
// them back was silent.
//
// THE PROMPT IS THE MECHANISM. It is clipped to the field's width, so a
// prompt as wide as the field leaves avail == 0 and Render returns
// before anything below it runs.
func TestAFieldWithNoRoomLeftStillSubscribesToItsStyles(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  func(sty, invalid *prop.Property[render.Style])
		err  string
	}{
		{"Style", func(sty, _ *prop.Property[render.Style]) {
			sty.Set(render.Style{Bold: true})
		}, ""},
		// InvalidStyle is read only on the error path, so this arm needs
		// the error set for the Get to run at all.
		{"InvalidStyle", func(_, invalid *prop.Property[render.Style]) {
			invalid.Set(render.Style{Underline: true})
		}, "no"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sty := prop.NewSource(render.Style{})
			invalid := prop.NewSource(render.Style{})
			tb := &TextBox{
				Text:         prop.NewSource("abcdef"),
				Prompt:       prop.NewSource(">>>>"),
				Error:        prop.NewSource(tc.err),
				Style:        sty,
				InvalidStyle: invalid,
			}
			c := gooey.NewComposer(tb, 4, 1)
			c.Frame()

			// THE PREMISE FIRST: a field with room left would repaint for
			// an unrelated reason and this would prove nothing.
			if got := render.RowText(c.Cells(), 0); got != ">>>>" {
				t.Fatalf("the field painted %q, want the prompt filling all four "+
					"columns — with room to spare Render never reaches its "+
					"avail check and this measures nothing", got)
			}
			tc.set(sty, invalid)
			if _, painted := c.Frame(); painted != 1 {
				t.Errorf("setting %s repainted %d components, want 1: Render "+
					"returned before reading it, so the paint node never "+
					"subscribed and the field is deaf to that property",
					tc.name, painted)
			}
		})
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

	if reversedText(t, f) == "" {
		t.Errorf("no cell in the field is reversed with the caret at 11 of %q: "+
			"the user is typing into a field whose caret is nowhere on screen. "+
			"Row: %q", value, render.RowText(f.Cells, 0))
	}
}

// TestAFlagRunSegmentsNoMoreThanItsSlack is a COST assertion shaped as a
// RATIO, and the shape is the point: a budget in milliseconds measures
// the runner, a ratio between two runs on the same machine in the same
// process measures the algorithm.
//
// WHAT IS BOUNDED IS THE SEGMENTED SPAN, and the name this replaces
// claimed more than that. eachClusterFrom walks off a
// regional-indicator run before it starts, because UAX #29 GB12/GB13
// decide a flag boundary by the parity of the whole run and a segmenter
// restarted mid-run pairs every flag from there one rune out. Walking to
// the run's OWN START restores the parity and makes the SEGMENTED span
// as long as the run — an O(run) segmentation on the paint path and on
// every column of a drag. Dropping back to the nearest EVEN offset keeps
// the parity with the span bounded by clusterSlack.
//
// The residual is the walk that FINDS the run's start, and that walk is
// still O(run) — a rune comparison per step, segmenting nothing. So the
// cost does grow with the run, and the old assertion's two sample points
// simply sat on the flat part of that curve. Measured on this tree,
// 1,000 clusterStartAt calls:
//
//	   500 pairs    7.8 ms
//	 5,000 pairs   10.9 ms   1.4x
//	50,000 pairs   40.3 ms   3.7x
//
// A third grid point reddens the old form, and it went red once here at
// two — a 4x budget over a 1.4x measurement is a thin margin on a loaded
// runner, which is the objection this PR's own round 4 made about a
// sibling.
//
// THE CLAIM THAT IS TRUE, AND THE ONE WORTH PINNING: a call costs a scan
// of the run, not a SEGMENTATION of it. So 100 calls cost less than ONE
// segmentation of the same run, where the defect made each call cost
// about one. Measured at 50,000 pairs, three runs: 0.69, 0.70, 0.72 of a
// segmentation for a hundred calls. The budget of ten is 14x above the
// measurement and 10x below the defect, and both the numerator and the
// denominator move with the machine together.
func TestAFlagRunSegmentsNoMoreThanItsSlack(t *testing.T) {
	const pairs = 50000
	runes := []rune(strings.Repeat("\U0001F1FA\U0001F1F8", pairs))

	t0 := time.Now()
	render.EachCluster(string(runes), func(string, int, int, int) bool { return true })
	oneSegmentation := time.Since(t0)

	t1 := time.Now()
	for range 100 {
		clusterStartAt(runes, len(runes)/2)
	}
	hundredCalls := time.Since(t1)

	if hundredCalls > 10*oneSegmentation {
		t.Errorf("100 clusterStartAt calls into a run of %d flags cost %v "+
			"against %v to segment that run ONCE — %.1f segmentations for a "+
			"hundred calls, want under 10. The regional-indicator walk-back is "+
			"segmenting the run rather than dropping to the nearest even offset "+
			"in it, which puts an O(run) segmentation on the paint path and on "+
			"every column of a drag", pairs, hundredCalls, oneSegmentation,
			float64(hundredCalls)/float64(oneSegmentation))
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

	if got := reversedText(t, f); got != "\u0301" {
		t.Errorf("the reversed cells hold %q with the caret at 0 of %q, want the "+
			"mark itself: an empty answer is a user typing into a focused field "+
			"whose caret is nowhere on screen, and any other answer is the caret "+
			"on the wrong cluster. Row: %q",
			got, value, render.RowText(f.Cells, 0))
	}

	// AND THE THIRD ARM IS WHERE THE SLACK RUNS OUT. Both arms above run
	// in ten columns for four runes, so the window never has to decide
	// anything and the same injury hides. windowFloor summed the raw
	// cluster width where Render advances by max(w, 1), so it believed in
	// a column Render would not give it; at a width with no spare column
	// the field does not scroll, the caret block's x < b.X+b.W is false,
	// and the user is again typing into a field with no caret in it.
	//
	// THE ORACLE IS THE ASCII CONTROL, not a written-down row. The two
	// values differ only in whether their first cluster is zero-width to
	// the segmenter, and Render gives both of them one column — so every
	// observable here has to be IDENTICAL, and a row spelled out in the
	// fixture would pin today's scroll arithmetic rather than that
	// agreement. Measured before the fix: the mark row was "\u0301abc"
	// at scroll 0 with no caret block, the control "abc█" at scroll 1
	// with one.
	narrow := func(v string) (int, string, int) {
		tb := &TextBox{Text: prop.NewSource(v)}
		tb.SetFocused(true)
		tb.setCaret(len([]rune(v)))
		f := gooey.Compose(tb, term.Caps{Cols: 4, Rows: 1}, nil)
		row := render.RowText(f.Cells, 0)
		return tb.scroll, row, strings.Count(row, "█")
	}
	markScroll, markRow, markCarets := narrow(value)
	ctlScroll, ctlRow, ctlCarets := narrow("xabc")
	if markCarets == 0 {
		t.Errorf("a four-column field showing %q paints no caret at all (row "+
			"%q, scroll %d) while the same shape in ASCII paints %d (row %q, "+
			"scroll %d) — the window kept a column Render does not give it, so "+
			"it never scrolled and the caret fell outside the bounds",
			value, markRow, markScroll, ctlCarets, ctlRow, ctlScroll)
	}
	if markScroll != ctlScroll || markRow != ctlRow || markCarets != ctlCarets {
		t.Errorf("%q renders (scroll %d, row %q, %d caret blocks) and %q renders "+
			"(scroll %d, row %q, %d) — Render gives the leading cluster one "+
			"column in both, so the two have to agree",
			value, markScroll, markRow, markCarets,
			"xabc", ctlScroll, ctlRow, ctlCarets)
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

// TestAClickOnAValueOpeningWithACombiningMarkIsNotOffByOne is the click
// half of round-5 finding 1, and the half the window grid cannot reach.
//
// indexAt disagreed WITH ITSELF. Its backward walk goes through
// caretCols, which floors a zero-width cluster at one column; its
// forward walk three lines down summed the raw width. So on a value
// opening with a bare combining mark — a cluster Render paints into a
// column and the segmenter reports as zero — every column answered one
// index late and index 0 was unreachable by mouse:
//
//	click col 0 -> caret 1     (column 0 paints the mark, which is index 0)
//	click col 1 -> caret 2
//	click col 2 -> caret 3
//
// EVERY COLUMN, NOT ONE, which is why this asserts the whole row rather
// than a single click: an off-by-one that starts at column 0 is a
// different fault from a click landing inside a cluster
// (TestAClickLandsOnAClusterBoundaryNotInsideOne, above), and a single
// probe cannot tell which one it caught.
func TestAClickOnAValueOpeningWithACombiningMarkIsNotOffByOne(t *testing.T) {
	const leading = "\u0301abc"
	tb := &TextBox{Text: prop.NewSource(leading)}
	gooey.Compose(tb, term.Caps{Cols: 10, Rows: 1}, nil)

	for col := 0; col < 4; col++ {
		tb.HandleMouse(input.MouseEvent{Kind: input.MousePress, X: col})
		if got := tb.Caret(); got != col {
			t.Errorf("a click on column %d of %q answers caret %d, want %d — "+
				"the leading mark is drawn into column 0 by Render and counted "+
				"as no column by the forward walk, so every click is one index "+
				"late and index 0 cannot be reached at all",
				col, leading, got, col)
		}
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

// TestAFieldWithOneUsableColumnStillShowsItsCaret is the avail == 1
// corner the three other caret-visibility pins do not reach.
//
// The full-width stop refuses a two-column cluster when one column is
// left, which is right — half a glyph is not drawable. What was wrong
// is what happens next: the trailing block caret only fired when the
// caret was past the LAST RUNE, so when the refused cluster was the
// caret's own, a focused field showed no caret anywhere.
//
// THE PROMPT ARM IS THE ONE THAT MATTERS. A one-column field is a
// curiosity; a three-column field with a "> " prompt is an ordinary
// shape and reaches the same avail == 1. Both are here because the
// first is the minimal case and the second is the reachable one — and
// an assertion that only had the first would read as being about a
// degenerate width. Raised in review of #521.
func TestAFieldWithOneUsableColumnStillShowsItsCaret(t *testing.T) {
	for _, tc := range []struct {
		name   string
		cols   int
		prompt string
		want   string
	}{
		{"a one-column field", 1, "", "█"},
		{"a two-column prompt in three columns", 3, "> ", "> █"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tb := &TextBox{
				Text:   prop.NewSource("東西南北"),
				Prompt: prop.NewSource(tc.prompt),
			}
			tb.SetFocused(true)
			tb.setCaret(1) // on 西, a two-column cluster
			f := gooey.Compose(tb, term.Caps{Cols: tc.cols, Rows: 1}, nil)
			if got := render.RowText(f.Cells, 0); got != tc.want {
				t.Errorf("row = %q, want %q — the refused cluster is the "+
					"caret's own, so with no block caret the field shows a "+
					"focused user nothing at all", got, tc.want)
			}
		})
	}
}

// TestAnArrowKeyStepsByClusterNotByRune is the fourth site: indexAt
// quantises a click to a cluster boundary and Render reverses the whole
// cluster, so a caret between a rune and its accent is a position the
// screen cannot show and the mouse cannot reach.
//
// THE ASSERTION IS THE POSITION, NOT THE PIXELS, and that is the whole
// difficulty — caret 0 and caret 1 render IDENTICALLY, which is why
// this went five rounds unnoticed. So it reads the caret directly, and
// the frame assertion beside it only confirms that the two positions
// were indistinguishable on screen.
//
// The destructive half is what makes it a defect rather than a nicety:
// at caret 1, typing put the typed rune between "e" and its accent and
// backspace left an orphan mark leading the value.
func TestAnArrowKeyStepsByClusterNotByRune(t *testing.T) {
	const decomposed = "éx" // é as e + U+0301, then x

	tb := &TextBox{Text: prop.NewSource(decomposed)}
	tb.SetFocused(true)
	tb.setCaret(0)

	tb.HandleKey(input.Named(input.KeyRight))
	if got := tb.Caret(); got != 2 {
		t.Errorf("one right arrow from 0 put the caret at %d, want 2 — 1 is "+
			"between the e and its accent, which no click can reach and no "+
			"cell can show", got)
	}
	tb.HandleKey(input.Named(input.KeyLeft))
	if got := tb.Caret(); got != 0 {
		t.Errorf("a left arrow back put the caret at %d, want 0 — the two "+
			"directions have to agree or the caret drifts into the cluster "+
			"from the right instead", got)
	}

	// AND THE REASON IT IS INVISIBLE, measured rather than asserted from
	// the model: the two positions paint the same row with the same
	// reversed cell, so nothing on screen distinguishes the safe caret
	// from the destructive one.
	tb.setCaret(0)
	at0 := render.RowText(gooey.Compose(tb, term.Caps{Cols: 8, Rows: 1}, nil).Cells, 0)
	tb.setCaret(1)
	at1 := render.RowText(gooey.Compose(tb, term.Caps{Cols: 8, Rows: 1}, nil).Cells, 0)
	if at0 != at1 {
		t.Fatalf("caret 0 paints %q and caret 1 paints %q — they differ, so "+
			"the mid-cluster position is visible after all and the argument "+
			"above for skipping it is not the one this test is making",
			at0, at1)
	}
}

// TestWordMotionAndDoubleClickLandOnClusterBoundaries is the other three
// producers of a caret position, and the arrow key was only the first.
//
// class() sorts a combining mark into classPunct — unicode.IsSpace,
// IsLetter and IsDigit are all false for an Mn — so the run boundary the
// word walk stops at falls BETWEEN a base letter and its accent. Every
// destructive outcome moveKey's doc enumerates for the arrow was still
// reachable through ctrl+arrow, and selectWord added one the arrow path
// does not have: Render's overlap arm reverses the WHOLE cluster for a
// selection covering half of it, so the highlight and the range that
// replaces it disagreed on screen.
//
// THE ASSERTIONS ARE ON BOUNDARIES, not on particular indices, because
// which word a class walk picks is not this test's subject and would
// pin the class table by accident. A position p is a boundary when
// clusterStartAt(p) == p. Raised in review of #521.
func TestWordMotionAndDoubleClickLandOnClusterBoundaries(t *testing.T) {
	onBoundary := func(runes []rune, p int) bool {
		// NO `p == len(runes) ||` HERE: clusterStartAt returns i
		// unchanged for i >= len(runes), so the end already answers
		// true and the disjunct could never change the answer — the
		// same dead-branch shape round 9 of this branch wrote down and
		// round 13 removed from TestADeleteNeverLeavesTheCaretInside
		// ACluster. Raised in review of #521.
		return clusterStartAt(runes, p) == p
	}
	for _, tc := range []struct {
		name  string
		value string
	}{
		{"an accent on the second letter", "ab́"},
		{"an accent on the first", "áb"},
		{"two accented letters", "áb́"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runes := []rune(tc.value)
			for i := 0; i <= len(runes); i++ {
				if got := wordLeft(runes, i); !onBoundary(runes, got) {
					t.Errorf("wordLeft(%d) = %d, which is inside the cluster "+
						"starting at %d — a caret there lets typing move the "+
						"accent onto the typed rune and backspace delete the "+
						"base", i, got, clusterStartAt(runes, got))
				}
				if got := wordRight(runes, i); !onBoundary(runes, got) {
					t.Errorf("wordRight(%d) = %d, which is inside the cluster "+
						"starting at %d", i, got, clusterStartAt(runes, got))
				}
			}

			// DOUBLE-CLICK, through the component, because selectWord
			// reads the caret and writes both edges.
			for i := 0; i < len(runes); i++ {
				tb, _ := textBox(t, tc.value)
				tb.setCaret(i)
				tb.selectWord()
				lo, hi := tb.anchor.Get(), tb.Caret()
				if !onBoundary(runes, lo) || !onBoundary(runes, hi) {
					t.Errorf("a double-click at %d selected [%d,%d), and one edge "+
						"is inside a cluster — Render reverses the whole cluster, "+
						"so the highlight covers what the replacement would not",
						i, lo, hi)
				}
			}
		})
	}
}

// TestARepaintDoesNotWalkAZeroWidthRun is the ratio test one vocabulary
// over, and it is the vocabulary that had nothing pinning it.
//
// render.RuneWidth is 0 for a combining mark, so a value that is ONE
// long cluster defeated both of the branch's window bounds at once:
// windowFloor's candidate walk never grew its column total and ran to
// index 0, and its left expansion never overflowed `avail` so it
// expanded to rune 0 too, re-collecting — and re-copying into a string —
// a larger span every round. spanForCols doubled to len(runes) for the
// same reason. Measured before the bounds, per repaint in a 20-column
// field: 0.54ms at 1,000 runes, 5.4ms at 10,000, 29.8ms at 50,000,
// against a flat 0.06ms for ASCII of the same rune count. On the UI
// goroutine inside a paint node, and reachable by paste — oneLine
// strips control characters and not combining marks.
//
// A RATIO, NOT A FIGURE, for the reason the ASCII sibling gives: a
// wall-clock threshold is a property of the machine. Raised in review
// of #521.
//
// TWO CARETS, BECAUSE THE TWO BOUNDS GUARD OPPOSITE DIRECTIONS and one
// fixture cannot see both. With the caret at the END the window is
// already at the tail, so spanForCols starts within clusterSlack of
// len(runes) and returns on its first round however it is capped: that
// arm sees only windowFloor's leftward expansion. With the caret at the
// START the expansion has nowhere to go and the whole cost is the
// rightward render walk, which is spanForCols'. Measured with the other
// bound left in place, per repaint at 50,000 runes: removing
// windowFloor's floor takes the caret-at-end arm to 0.68s, and removing
// spanForCols' cap takes the caret-at-start arm to 15.2ms against
// 0.66ms — while leaving the caret-at-end arm at 0.76ms, unchanged.
// Delete either bound with only the other arm present and nothing goes
// red.
func TestARepaintDoesNotWalkAZeroWidthRun(t *testing.T) {
	cost := func(n, caret int) (time.Duration, *gooey.Frame) {
		v := prop.NewSource("a" + strings.Repeat("́", n))
		st := prop.NewSource(render.Style{})
		tb := &TextBox{Text: v, Style: st}
		tb.SetFocused(true)
		tb.setCaret(caret)
		c := gooey.NewComposer(tb, 20, 1)
		c.Frame()
		const frames = 40
		var f *gooey.Frame
		start := time.Now()
		for i := range frames {
			st.Set(render.Style{Bold: i%2 == 0})
			f, _ = c.Frame()
		}
		return time.Since(start) / frames, f
	}
	// A FRAME THAT PAINTS NOTHING IS CHEAP, which is how this test was
	// green over the injury one level up. windowFloor returned a raw
	// arithmetic index mid-cluster, Render opened eachClusterFrom inside
	// that cluster and reported only the skipped fragment — zero-width —
	// and the field painted the caret block and 19 spaces. That costs
	// nothing and the ratio held: measured, the caret-at-end arm ran at
	// 0.75ms against 0.82ms with the value entirely absent from both
	// frames. So the arm asserts it has a frame worth timing FIRST, on
	// BOTH n, and a cost claim over a blank field is not a cost claim.
	// The rule is TestARepaintDoesNotWalkTheWholeValue's already; this
	// is the same rule over the vocabulary that can violate it.
	// Raised in review of #521.
	measure := func(t *testing.T, n, caret int) time.Duration {
		t.Helper()
		d, f := cost(n, caret)
		row := render.RowText(f.Cells, 0)
		if !strings.HasPrefix(row, "a") {
			t.Fatalf("over a %d-rune zero-width run the field painted %q, which "+
				"does not begin with the value's own first rune — the window "+
				"opened inside the cluster and Render reported the skipped "+
				"fragment. There is nothing here to make a cost claim about: "+
				"a field that paints no value is cheap whatever the walk does",
				n, row)
		}
		// THE CARET HAS TWO SHAPES AND THIS ARM SEES BOTH OF THEM.
		// Render reverses the cluster the caret is ON, and paints an
		// accent block PAST the last painted cluster — so the
		// caret-at-start arm produces a reversed cell and the
		// caret-at-end arm a '█', and an assertion written for either
		// one alone is vacuous on the other rather than red. Both
		// spellings mean the same thing here, which is the only claim
		// this makes: the caret is somewhere on the row.
		if reversedText(t, f) == "" && !strings.ContainsRune(row, '\u2588') {
			t.Fatalf("over a %d-rune zero-width run with the caret at %d the "+
				"frame has neither a reversed cell nor a caret block, so the "+
				"user is typing into a focused field whose caret is nowhere "+
				"on screen. Row: %q", n, caret, row)
		}
		return d
	}
	// costRepeats is how many times each side is measured before the
	// smallest is taken. Five is enough that a single preemption cannot
	// decide the result, and cheap: the whole test stays under a
	// handful of seconds because `cost` is already batched.
	const costRepeats = 5
	// bestPair measures BOTH sides, INTERLEAVED, and keeps each side's
	// minimum. It measured all five short samples and then all five
	// long ones, which is two blocks of wall-clock separated in time: a
	// neighbour's job starting between them lands on every long sample
	// and no short one, and a minimum cannot remove noise that every
	// sample of one side shares. That is how this arm went red at 8.1x
	// on a PR touching neither this package nor anything it imports,
	// with the min-of-5 from #563 already in place. Alternating puts
	// both sides under the same load, so the ratio is the walk's and
	// not the runner's. #571.
	bestPair := func(t *testing.T, caret func(int) int) (short, long time.Duration) {
		t.Helper()
		short, long = time.Duration(math.MaxInt64), time.Duration(math.MaxInt64)
		for range costRepeats {
			short = min(short, measure(t, 1000, caret(1000)))
			long = min(long, measure(t, 50000, caret(50000)))
		}
		return short, long
	}
	for _, tc := range []struct {
		name  string
		caret func(n int) int
		bound string
	}{
		{"caret at the end", func(n int) int { return n }, "windowFloor's left expansion"},
		{"caret at the start", func(int) int { return 0 }, "spanForCols' doubling"},
	} {
		// THE MINIMUM OF SEVERAL RUNS, NOT ONE SAMPLE, and minimum
		// rather than mean because interference is one-directional:
		// a preempted goroutine measures SLOWER and never faster, so
		// the smallest observation is the closest this can get to the
		// cost without the runner's other tenants in it. `cost` already
		// averages 40 frames, and a mean is exactly the estimator a
		// single long preemption moves — averaging spreads the outlier
		// over the sample instead of discarding it.
		//
		// MEASURED, and that is why this is here rather than a
		// robustness flourish: on the shared vSphere pool this arm
		// reported 5.2x and 10.8x against a ~600µs baseline while the
		// same commit measured ~1x locally, and reddened a PR whose
		// diff touched neither this package nor anything it imports
		// (#563). A 600µs baseline cannot carry a 4x threshold off one
		// observation.
		//
		// WHAT THE 4x ENCODES is the noise budget and nothing else. A
		// walk that terminates on the bound is O(1) in n and should
		// measure ~1x for a fifty-fold value; the defect this catches
		// is O(len(value)) and measures in the tens. Everything between
		// 1 and 4 is headroom, not tolerance for a partial fix.
		short, long := bestPair(t, tc.caret)
		if long > 4*short {
			t.Errorf("with the %s, a repaint costs %v over a 50,000-rune "+
				"zero-width run against %v over a 1,000-rune one — %.1fx for "+
				"fifty times the value, want under 4x. A window bound that sums "+
				"rune WIDTHS does not terminate on a run of zero-width runes, so "+
				"the walk is O(len(value)) on the paint path, per keystroke, on "+
				"the UI goroutine; this arm is the one %s bounds",
				tc.name, long, short, float64(long)/float64(short), tc.bound)
		}
	}
}

// TestADeleteNeverLeavesTheCaretInsideACluster is the edit path's half
// of the rule moveKey's doc states, and it was missing.
//
// The arrows were changed to step by cluster because a mid-cluster
// caret is a position from which typing moves an accent onto the typed
// rune and backspace reattaches an orphan mark to its neighbour.
// backspace and delete splice ONE RUNE out and set the caret from the
// OLD index, so both reach exactly that position on a value where the
// removed rune was the base of a cluster that is not at the end. The
// selection arms of the same switch reach it through a different join:
// deleteSelection splices [lo,hi) out, and lo is a boundary in the OLD
// value rather than the new one — see its doc.
// Measured on this branch before the fix: "ab" + U+0301, caret 1,
// delete gives "á" with the caret at 1, and typing Z then gives "aŹ" —
// the accent migrated onto the typed rune, which is the first harm
// moveKey's doc enumerates.
//
// snapOut's doc exempts setText, and that exemption is about INSERTION:
// a pasted rune joining the PRECEDING cluster would be pulled backwards
// by a snap. These two arms insert nothing, so the argument does not
// reach them. Raised in review of #521.
//
// The second assertion is the one that matters to a user. The caret
// arm was widened from `caret == i` to containment in this same branch,
// so a mid-cluster caret and a boundary caret now paint IDENTICALLY —
// asserting the index alone would pin a rule whose violation has no
// visible symptom.
func TestADeleteNeverLeavesTheCaretInsideACluster(t *testing.T) {
	for _, tc := range []struct {
		name     string
		start    string
		caret    int
		selRight int // shift+right presses before the key, to select
		key      input.KeyEvent
		want     string // after the delete, then typing Z
	}{
		// The delete arm snaps RIGHTWARD, so the caret ends after the
		// "á" glyph and the typed rune lands behind it. Leftward put
		// it before the glyph, which walked the caret back over
		// something nothing had deleted — review of #521, round 13.
		{"delete takes the base out from under a following mark",
			"ab́", 1, 0, input.Named(input.KeyDelete), "áZ"},
		{"backspace takes the base out from under a following mark",
			"ab́", 2, 0, input.Named(input.KeyBackspace), "Zá"},
		// AND ONE THE UI CAN ACTUALLY PRODUCE. The row above starts at
		// caret 2 in "ab"+U+0301, whose boundaries are [0 1 3] — a
		// mid-cluster start, which is the position this branch taught
		// the arrows, clicks, word motion and double-click NOT to
		// produce, so only setCaret can reach it and a reader cannot
		// tell whether the snap guards a reachable state. Three
		// regional-indicator pairs have boundaries [0 2 4 6], so two
		// right arrows land on 4 honestly; the backspace then re-pairs
		// the flags — un-snapped the caret is 3, mid-cluster, which no
		// other edit-path test reaches. Raised in review of #521.
		{"backspace re-pairs regional indicators under the caret",
			"🇺🇸🇺🇸🇺🇸", 4, 0,
			input.Named(input.KeyBackspace),
			"🇺🇸Z🇺🇺🇸"},

		// AND THE SELECTION ARMS OF THE SAME SWITCH, which the three
		// rows above do not reach: they press a bare key, and the
		// first thing each selection arm does is hand off to
		// deleteSelection. "x"+"👩"+ZWJ+"a"+"👨" has
		// boundaries [0 1 3 4 5], so the caret starts on a real one and
		// ONE shift+right selects exactly the "a" — every index here
		// is produced by a gesture rather than by setCaret. Deleting
		// joins the two emoji into one cluster, [0 1 4], and the
		// un-snapped caret sits at 3 inside it, where typing puts Z
		// between the ZWJ and the second emoji.
		//
		// THE LEADING "x" IS THE MEASUREMENT, not decoration. Without
		// it the fused cluster starts at rune 0, so a leftward snap
		// lands on the left wall and the three rows agree whichever
		// direction the code chose — which is how round 14's fix came
		// to snap the forward-delete arm backwards with this table
		// green. The wants below now differ BY DIRECTION, which is the
		// distinction they exist to encode. Raised in review of #521,
		// rounds 14 and 15.
		{"a selection delete joins the clusters either side of it",
			"x👩‍a👨", 3, 1,
			input.Named(input.KeyDelete),
			"x👩‍👨Z"},
		{"a selection backspace joins them and stays behind the glyph",
			"x👩‍a👨", 3, 1,
			input.Named(input.KeyBackspace),
			"xZ👩‍👨"},
		{"and ctrl+x, which travels nowhere and goes left with backspace",
			"x👩‍a👨", 3, 1,
			ctrlRune('x'),
			"xZ👩‍👨"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := prop.NewSource(tc.start)
			tb := &TextBox{Text: v}
			tb.SetFocused(true)
			tb.setCaret(tc.caret)
			for range tc.selRight {
				tb.HandleKey(input.KeyEvent{Key: input.KeyRight, Mods: input.ModShift})
			}
			tb.HandleKey(tc.key)

			runes := []rune(v.Get())
			// NO `|| caret == len(runes)` HERE: clusterBoundaries ends
			// with append(bs, end), so the end IS in the slice and a
			// second conjunct guarding it could never change the
			// answer — a conjunct no mutation can be caught removing,
			// which is the rule round 9 of this branch wrote down.
			// Raised in review of #521.
			if !slices.Contains(clusterBoundaries(runes, len(runes)), tb.Caret()) {
				t.Errorf("the caret is at %d in %q, whose cluster boundaries are "+
					"%v — inside a cluster, which is the position the arrows "+
					"were changed to stop reaching", tb.Caret(), v.Get(),
					clusterBoundaries(runes, len(runes)))
			}
			tb.HandleKey(input.Rune('Z'))
			if v.Get() != tc.want {
				t.Errorf("typing after the delete gave %q, want %q — a mark that "+
					"lands on the typed rune instead of its own base is what a "+
					"mid-cluster caret does", v.Get(), tc.want)
			}
		})
	}
}

// TestReplacingASelectionInsertsWhereItWas is the counterfactual for
// the snap the test above pins, and without it the exemption is
// unfireable.
//
// deleteSelection takes `snap` from its caller because the answer
// differs by caller, and the three DELETING callers are now pinned. The
// two INSERTING ones were not: with replace and paste switched to
// snap the whole suite stayed green, so the parameter could have been
// collapsed to an unconditional snap and nothing would have said so —
// the shape this branch has spent four rounds removing.
//
// The value is the one the delete rows use, and the difference is the
// whole point. Deleting the selected "a" fuses the two emoji, so the
// caret must leave the cluster; typing over the same selection puts the
// rune BETWEEN them, and a snapped caret would pull it in front of
// both. That is snapOut's setText exemption stated as an outcome
// rather than as a rule. Raised in review of #521, round 14.
func TestReplacingASelectionInsertsWhereItWas(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  input.KeyEvent
	}{
		{"typing", input.Rune('Z')},
		{"paste", ctrlRune('v')},
	} {
		t.Run(tc.name, func(t *testing.T) {
			SetKillBuffer("Z")
			v := prop.NewSource("\U0001F469\u200da\U0001F468")
			tb := &TextBox{Text: v}
			tb.SetFocused(true)
			tb.setCaret(2)
			tb.HandleKey(input.KeyEvent{Key: input.KeyRight, Mods: input.ModShift})
			tb.HandleKey(tc.key)

			const want = "\U0001F469\u200dZ\U0001F468"
			if v.Get() != want {
				t.Errorf("replacing the selection gave %q, want %q — the rune "+
					"belongs where the selection was, between the two emoji; a "+
					"snapped caret puts it in front of both", v.Get(), want)
			}
		})
	}
}

// TestAForwardDeleteNeverWalksTheCaretBackwards is the other half of
// the snap, and the half a single press cannot see.
//
// Round 12's fix snapped both delete arms leftward. Backspace travels
// left, so that is right there; `delete` travels right, and leftward
// walked the caret back over a glyph nothing had deleted — so the NEXT
// press destroyed a character the user had already moved past, and the
// value was left opening with an orphan mark, which is the state
// moveKey's doc names as the harm the cluster rule removes. Measured in
// review of #521, round 13:
//
//	"ab" + U+0301 + "cd", right arrow -> caret 1 (a real boundary)
//	delete  -> "ácd" caret 0   <- back over the á nothing deleted
//	delete  -> "́cd"  caret 0   <- 'a' gone, orphan mark leads the value
//
// snapOut's own contract is "outward in the direction it was
// travelling", and the delete arm was the one caller contradicting it.
//
// TWO PRESSES, because one cannot tell the two directions apart by
// outcome: both leave the caret on a boundary, and only the second
// press says which boundary was the right one.
func TestAForwardDeleteNeverWalksTheCaretBackwards(t *testing.T) {
	for _, tc := range []struct {
		name     string
		start    string
		rights   int // right arrows, to reach a boundary honestly
		selRight int // then shift+right presses, to select
		want     string
	}{
		{"the bare key", "ab́cd", 1, 0, "ád"},
		// AND THE SELECTION ARM, which routes through deleteSelection
		// and which this test did not reach until the fix for round
		// 14 put a second direction behind that seam. Same shape: the
		// splice fuses "👩"+ZWJ and "👨" into one
		// cluster, so the caret must leave it, and leftward leaves it
		// over a glyph nothing deleted — the second press then
		// destroys the "👩" and the value is left holding an
		// orphan ZWJ. Raised in review of #521, round 15.
		{"delete with a selection", "x👩‍a👨", 2, 1,
			"x👩‍👨"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := prop.NewSource(tc.start)
			tb := &TextBox{Text: v}
			tb.SetFocused(true)
			for range tc.rights {
				tb.HandleKey(input.Named(input.KeyRight))
			}
			for range tc.selRight {
				tb.HandleKey(input.KeyEvent{Key: input.KeyRight, Mods: input.ModShift})
			}

			before := tb.Caret()
			tb.HandleKey(input.Named(input.KeyDelete))
			if tb.Caret() < before {
				t.Fatalf("a forward delete moved the caret from %d to %d, over a glyph "+
					"it did not delete — the value is now %q", before, tb.Caret(), v.Get())
			}
			tb.HandleKey(input.Named(input.KeyDelete))
			if v.Get() != tc.want {
				t.Errorf("after two forward deletes the value is %q, want %q: the "+
					"second press consumed a glyph the caret had already moved past",
					v.Get(), tc.want)
			}
		})
	}
}
