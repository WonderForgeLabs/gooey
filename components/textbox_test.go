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
	v := prop.NewSource("世界")
	tb := &TextBox{Text: v}
	tb.SetFocused(true)
	f := gooey.Compose(tb, term.Caps{Cols: 10, Rows: 1}, nil)
	tb.setCaret(len([]rune("世界")))
	f = gooey.Compose(tb, term.Caps{Cols: 10, Rows: 1}, nil)

	if got, want := render.SpanText(f.Cells, 0, 0, 10), "世界█     "; got != want {
		t.Errorf("rendered %q, want %q — the two glyphs occupy FOUR columns, so "+
			"the caret belongs in column 4", got, want)
	}
	// AND THE COLUMN MODEL AGREES WITH THE ROW, which the string alone
	// cannot say: a buffer column must be a terminal column, or
	// everything right of the glyph is drawn one column off and the
	// displaced cells are CLEAN, so nothing repaints over them.
	//
	// Through render.Displaced rather than a loop over TerminalColumns:
	// a continuation cell's recorded column is where the cursor sits
	// MID-GLYPH, which is legitimately not its index, so the raw loop
	// calls every correct wide row displaced.
	if x, by, bad := render.Displaced(f.Cells, 0); bad {
		t.Errorf("cell %d is drawn %d columns off, so everything right of it is "+
			"displaced: %q", x, by, render.RowText(f.Cells, 0))
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
