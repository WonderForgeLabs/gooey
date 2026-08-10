package gooey

import (
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
	"github.com/WonderForgeLabs/gooey/term"
)

func textBox(t *testing.T, s string) (*TextBox, *prop.Property[string]) {
	t.Helper()
	v := prop.NewSource(s)
	tb := &TextBox{Text: v}
	Compose(tb, term.Caps{Cols: 20, Rows: 1}, nil)
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

// The bound text can change underneath the widget; the caret must not be
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
	tb.Changed = func() { edits++ }

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
	f := Compose(tb, term.Caps{Cols: 10, Rows: 1}, nil)
	tb.setCaret(2)
	f = Compose(tb, term.Caps{Cols: 10, Rows: 1}, nil)

	var sb strings.Builder
	for x := 0; x < 10; x++ {
		sb.WriteRune(f.Cells.At(x, 0).Rune)
	}
	if got, want := sb.String(), "> hi█     "; got != want {
		t.Errorf("rendered %q, want %q", got, want)
	}
}

// A field narrower than its content scrolls to keep the caret visible.
func TestTextBoxScrollsToKeepTheCaretVisible(t *testing.T) {
	v := prop.NewSource("abcdefghijklmnop")
	tb := &TextBox{Text: v}
	tb.SetFocused(true)
	tb.setCaret(len("abcdefghijklmnop")) // caret at the end, as after typing
	f := Compose(tb, term.Caps{Cols: 6, Rows: 1}, nil)

	var sb strings.Builder
	for x := 0; x < 6; x++ {
		sb.WriteRune(f.Cells.At(x, 0).Rune)
	}
	// Caret is at the end, so the tail is what shows.
	if got := sb.String(); !strings.Contains(got, "p") {
		t.Errorf("narrow field showed %q; the caret end must stay visible", got)
	}
}

func TestTextBoxClickPlacesTheCaret(t *testing.T) {
	v := prop.NewSource("abcdef")
	tb := &TextBox{Text: v, Prompt: Str("> ")}
	Compose(tb, term.Caps{Cols: 20, Rows: 1}, nil)

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
	root := &VStack{Children: []Widget{&Text{Content: Str("a")}, tb, &Text{Content: Str("b")}}}
	comp := NewComposer(root, 20, 4)
	if _, painted := comp.Frame(); painted != 4 {
		t.Fatalf("first frame painted %d, want 4", painted)
	}
	tb.HandleKey(input.Rune('x'))
	if _, painted := comp.Frame(); painted != 1 {
		t.Errorf("typing painted %d widgets, want exactly 1", painted)
	}
	// A caret move is damage too, and just as local.
	tb.HandleKey(input.Named(input.KeyHome))
	if _, painted := comp.Frame(); painted != 1 {
		t.Errorf("a caret move painted %d widgets, want exactly 1", painted)
	}
}

func TestTextBoxWithoutTextIsInert(t *testing.T) {
	tb := &TextBox{}
	Compose(tb, term.Caps{Cols: 10, Rows: 1}, nil)
	if tb.HandleKey(input.Rune('a')) {
		t.Error("an unbound TextBox consumed a key")
	}
}
