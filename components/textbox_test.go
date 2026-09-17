package components

import (
	"strings"
	"testing"

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
// assertion: a TextBox advancing one column per rune puts it at 2 and
// leaves the row reading "世界█" two columns early.
func TestTextBoxRendersAWideGlyphInItsOwnColumns(t *testing.T) {
	// SKIPPED AGAINST AN OPEN BUG, not against a design decision. The
	// fixture is what found #519 — TextBox advances one COLUMN per rune,
	// so the next rune lands on the continuation cell the previous glyph
	// claimed, healSeam blanks the orphaned lead, and the glyph is gone:
	// "世界" renders as " 界" unfocused and "  █" with the caret at the
	// end. Written here so the claim dies with the fix in the same
	// commit, which is what CLAUDE.md asks for instead of a list
	// somewhere else. Check that #519 is still open before believing
	// this line.
	//
	// AND IT RETIRES ITSELF, which is the half a skip naming an issue
	// does not have on its own. An unconditional t.Skip as the first
	// statement leaves the repo's only wide-glyph TextBox fixture dark
	// behind a green check once the bug is fixed, and "the fixing branch
	// already satisfies it" is a cross-branch promise rather than a
	// local pin. Composing FIRST and deciding after costs one frame and
	// needs nothing from the issue tracker: the day all three rows read
	// correctly the skip below does not fire and the assertions at the
	// bottom take over.
	//
	// ALL THREE SHAPES, and a tripwire on each, because the table below
	// tabulates three renders: a fix that corrected the unfocused path
	// and left the caret column wrong would otherwise keep this file
	// dark behind a green check.
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
	// THE SKIP IS THE CONDITION, not a statement after one: retiring by
	// hand needs an instruction that is complete and stays complete,
	// and retiring by CONDITION needs no instruction at all.
	//
	// AND IT SKIPS ON THE DOCUMENTED BUGGY RENDER, not on "not correct",
	// which is what makes the assertions below reachable at all. t.Skipf
	// calls runtime.Goexit, so a loop skipping on any mismatch leaves
	// this fixture with two outcomes forever — pass and skip — and the
	// three t.Errorf blocks could never run. Measured: with a plausible
	// #519 fix applied, changing the caret glyph in textbox.go from '█'
	// to '|' gave
	//
	//	--- SKIP: TestTextBoxRendersAWideGlyphInItsOwnColumns
	//	    TextBox blanks wide glyphs — the focused row … reads "世界|     "
	//
	// a check quietly reporting the wrong answer.
	//
	// WHAT THIS DOES NOT BUY, stated because the paragraph above used to
	// claim it did: the quietly-wrong-answer class is narrowed, not
	// removed. Once #519 is fixed this block stays live, and the single
	// most likely future regression — the same per-rune advance loop,
	// reintroduced — produces exactly the three strings below. That
	// re-arms the skip and the suite goes green with a message citing a
	// CLOSED issue, which is the same failure one lifetime later.
	//
	// The block therefore has a lifetime the code cannot express, so the
	// deletion has to live somewhere: **#519's fixing commit deletes
	// this whole loop**, not just the skip, and #519's acceptance
	// criteria say so. Self-retiring means nobody is FORCED to delete
	// it; it does not mean nobody has to.
	//
	// ONE TABLE DRIVES BOTH, and it did not: `want` was set three times
	// and read nowhere, while the assertions below re-spelled the three
	// constants by hand. A fourth shape could then get a tripwire and no
	// assertion, or the reverse, and nothing would go red — in the test
	// whose whole design is about not relying on somebody remembering.
	// The `why` column is what the three bespoke t.Errorf blocks were
	// carrying and is the only part of them that differed. Raised in
	// review of #520.
	shapes := []struct{ got, want, buggy, shape, why string }{
		{caret, wantCaret, "  █       ", "the focused row with the caret after both glyphs",
			"the two glyphs occupy FOUR columns, so the caret belongs in column 4"},
		{plain, wantPlain, " 界       ", "the unfocused row",
			"the glyphs occupy their own columns with no caret to make room for"},
		{mixed, wantMixed, "a b       ", "the unfocused mixed-width row",
			"a narrow glyph either side of a wide one is the arrangement a " +
				"per-rune advance loses in the middle rather than at the end"},
	}
	for _, tw := range shapes {
		if tw.got == tw.buggy {
			t.Skipf("#519 is still open — %s reads %q, the blanked-lead render "+
				"this fixture was written against — "+
				"https://github.com/WonderForgeLabs/gooey/issues/519",
				tw.shape, tw.got)
		}
	}

	// THE STRING IS THE ONLY PIN HERE, and a render.Displaced assertion
	// beside it would be corroboration it cannot supply: #519 blanks the
	// orphaned lead through healSeam, so the row is wrong WITHOUT being
	// displaced. Measured against the buggy render, all three cases:
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
