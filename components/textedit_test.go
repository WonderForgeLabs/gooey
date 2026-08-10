package components

import (
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/term"
)

func shift(k input.Key) input.KeyEvent {
	return input.KeyEvent{Key: k, Mods: input.ModShift}
}

func ctrl(k input.Key) input.KeyEvent {
	return input.KeyEvent{Key: k, Mods: input.ModCtrl}
}

func ctrlShift(k input.Key) input.KeyEvent {
	return input.KeyEvent{Key: k, Mods: input.ModCtrl | input.ModShift}
}

// selText is the selected substring, or "" when there is none.
func selText(tb *TextBox) string {
	lo, hi, ok := tb.Selection()
	if !ok {
		return ""
	}
	return string([]rune(tb.Text.Get())[lo:hi])
}

func TestWordMovement(t *testing.T) {
	tb, _ := textBox(t, "hello brave.new world")
	tb.HandleKey(input.Named(input.KeyHome))

	want := []int{5, 11, 12, 15, 21} // hello | brave | . | new | world
	for i, w := range want {
		tb.HandleKey(ctrl(input.KeyRight))
		if got := tb.Caret(); got != w {
			t.Fatalf("ctrl+right #%d put the caret at %d, want %d", i+1, got, w)
		}
	}
	tb.HandleKey(ctrl(input.KeyRight))
	if got := tb.Caret(); got != 21 {
		t.Errorf("ctrl+right at the end moved to %d, want to stay at 21", got)
	}

	back := []int{16, 12, 11, 6, 0}
	for i, w := range back {
		tb.HandleKey(ctrl(input.KeyLeft))
		if got := tb.Caret(); got != w {
			t.Fatalf("ctrl+left #%d put the caret at %d, want %d", i+1, got, w)
		}
	}
}

func TestShiftExtendsASelection(t *testing.T) {
	tb, _ := textBox(t, "hello world")
	tb.HandleKey(input.Named(input.KeyHome))

	tb.HandleKey(shift(input.KeyRight))
	tb.HandleKey(shift(input.KeyRight))
	if got := selText(tb); got != "he" {
		t.Fatalf("two shift+rights selected %q, want %q", got, "he")
	}
	tb.HandleKey(shift(input.KeyLeft))
	if got := selText(tb); got != "h" {
		t.Fatalf("shift+left back over the selection left %q, want %q", got, "h")
	}
	tb.HandleKey(ctrlShift(input.KeyRight))
	if got := selText(tb); got != "hello" {
		t.Fatalf("ctrl+shift+right selected %q, want the word", got)
	}
	tb.HandleKey(shift(input.KeyEnd))
	if got := selText(tb); got != "hello world" {
		t.Fatalf("shift+end selected %q, want everything", got)
	}
	// An unshifted arrow drops the selection and collapses to its edge.
	tb.HandleKey(input.Named(input.KeyLeft))
	if got := selText(tb); got != "" {
		t.Errorf("a plain arrow left the selection %q in place", got)
	}
	if got := tb.Caret(); got != 0 {
		t.Errorf("collapsing left put the caret at %d, want the selection's start", got)
	}
}

func TestTypingReplacesTheSelection(t *testing.T) {
	tb, v := textBox(t, "hello world")
	tb.HandleKey(input.Named(input.KeyHome))
	tb.HandleKey(ctrlShift(input.KeyRight)) // select "hello"

	tb.HandleKey(input.Rune('b'))
	if got, want := v.Get(), "b world"; got != want {
		t.Fatalf("typing over a selection gave %q, want %q", got, want)
	}
	if selText(tb) != "" {
		t.Error("the selection survived the edit that replaced it")
	}
	if got := tb.Caret(); got != 1 {
		t.Errorf("caret = %d, want 1 (just past what was typed)", got)
	}
}

func TestBackspaceAndDeleteRemoveTheSelection(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  input.KeyEvent
	}{
		{"backspace", input.Named(input.KeyBackspace)},
		{"delete", input.Named(input.KeyDelete)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tb, v := textBox(t, "hello world")
			tb.HandleKey(input.Named(input.KeyHome))
			tb.HandleKey(ctrlShift(input.KeyRight))
			tb.HandleKey(tc.key)
			if got, want := v.Get(), " world"; got != want {
				t.Fatalf("gave %q, want %q", got, want)
			}
			if got := tb.Caret(); got != 0 {
				t.Errorf("caret = %d, want the selection's start", got)
			}
		})
	}
}

func TestCutCopyPasteThroughTheKillBuffer(t *testing.T) {
	SetKillBuffer("")
	tb, v := textBox(t, "hello world")
	tb.HandleKey(input.Named(input.KeyHome))
	tb.HandleKey(ctrlShift(input.KeyRight)) // "hello"

	if !tb.HandleKey(ctrlRune('c')) {
		t.Fatal("copy with a selection was not consumed")
	}
	if got := KillBuffer(); got != "hello" {
		t.Fatalf("kill buffer = %q, want %q", got, "hello")
	}
	if v.Get() != "hello world" {
		t.Error("copy changed the text")
	}

	if !tb.HandleKey(ctrlRune('x')) {
		t.Fatal("cut was not consumed")
	}
	if got, want := v.Get(), " world"; got != want {
		t.Fatalf("after cut: %q, want %q", got, want)
	}

	tb.HandleKey(input.Named(input.KeyEnd))
	tb.HandleKey(ctrlRune('v'))
	if got, want := v.Get(), " worldhello"; got != want {
		t.Fatalf("after paste: %q, want %q", got, want)
	}
	if got := tb.Caret(); got != len(" worldhello") {
		t.Errorf("caret = %d, want the end of the pasted text", got)
	}

	// The buffer is shared: a second box pastes what the first cut.
	other, ov := textBox(t, "")
	other.HandleKey(ctrlRune('v'))
	if got := ov.Get(); got != "hello" {
		t.Errorf("a second TextBox pasted %q, want the shared kill buffer", got)
	}
}

// ctrl+c only claims the key when there is something to copy, or a
// focused field would swallow the framework quit key.
func TestCopyWithoutASelectionBubbles(t *testing.T) {
	SetKillBuffer("keep")
	tb, _ := textBox(t, "hello")
	if tb.HandleKey(ctrlRune('c')) {
		t.Fatal("ctrl+c with no selection was consumed; the quit key must bubble out")
	}
	if KillBuffer() != "keep" {
		t.Error("a no-op copy clobbered the kill buffer")
	}
}

func TestSelectionRendersReversed(t *testing.T) {
	v := prop.NewSource("abcdef")
	tb := &TextBox{Text: v}
	tb.SetFocused(true)
	gooey.Compose(tb, term.Caps{Cols: 10, Rows: 1}, nil)
	tb.HandleKey(input.Named(input.KeyHome))
	tb.HandleKey(shift(input.KeyRight))
	tb.HandleKey(shift(input.KeyRight))
	f := gooey.Compose(tb, term.Caps{Cols: 10, Rows: 1}, nil)

	var rev strings.Builder
	for x := 0; x < 6; x++ {
		if f.Cells.At(x, 0).Style.Reverse {
			rev.WriteRune(f.Cells.At(x, 0).Rune)
		}
	}
	if got := rev.String(); got != "ab" {
		t.Errorf("reversed cells spell %q, want the selection %q", got, "ab")
	}
}

// Horizontal scroll has to work on BOTH sides once the caret can move
// backwards: walking left out of the window must pull it back.
func TestScrollFollowsTheCaretBothWays(t *testing.T) {
	v := prop.NewSource("abcdefghijklmnop")
	tb := &TextBox{Text: v}
	tb.SetFocused(true)
	tb.setCaret(16)
	row := func() string {
		f := gooey.Compose(tb, term.Caps{Cols: 6, Rows: 1}, nil)
		var sb strings.Builder
		for x := 0; x < 6; x++ {
			sb.WriteRune(f.Cells.At(x, 0).Rune)
		}
		return sb.String()
	}
	if got := row(); !strings.HasSuffix(strings.TrimRight(got, " "), "p█") {
		t.Fatalf("caret at the end showed %q, want the tail with the caret", got)
	}
	tb.HandleKey(input.Named(input.KeyHome))
	if got := row(); !strings.HasPrefix(got, "abcdef") {
		t.Fatalf("home showed %q, want the window pulled back to the start", got)
	}
	tb.setCaret(10)
	if got := row(); !strings.ContainsRune(got, 'k') {
		t.Fatalf("caret at 10 showed %q, want the window to contain it", got)
	}
}

func TestDragSelectsThroughCapture(t *testing.T) {
	v := prop.NewSource("hello world")
	tb := &TextBox{Text: v}
	other := &Text{Content: Str("elsewhere")}
	c := gooey.NewComposer(&VStack{Children: []gooey.Component{tb, other}}, 20, 4)
	c.Frame()
	y := tb.Bounds().Y

	c.HandleMouse(press(0, y))
	// The pointer leaves the field entirely; capture keeps the drag alive.
	c.HandleMouse(dragMove(5, other.Bounds().Y))
	if got := selText(tb); got != "hello" {
		t.Fatalf("dragging outside the field selected %q, want %q", got, "hello")
	}
	c.HandleMouse(release(5, other.Bounds().Y))
	if got := selText(tb); got != "hello" {
		t.Errorf("the selection was lost on release: %q", got)
	}
	if c.Focus().Captured() != nil {
		t.Error("the implicit capture was not given back")
	}
}

func TestDoubleClickSelectsAWord(t *testing.T) {
	v := prop.NewSource("hello brave world")
	tb := &TextBox{Text: v}
	c := gooey.NewComposer(&VStack{Children: []gooey.Component{tb}}, 20, 3)
	c.Frame()
	y := tb.Bounds().Y

	// Two clicks inside the interval, over the middle word.
	c.HandleMouse(press(8, y))
	c.HandleMouse(release(8, y))
	if got := selText(tb); got != "" {
		t.Fatalf("a single click selected %q, want nothing", got)
	}
	c.HandleMouse(press(8, y))
	c.HandleMouse(release(8, y))
	if got := selText(tb); got != "brave" {
		t.Fatalf("double click selected %q, want %q", got, "brave")
	}

	// Whitespace and punctuation are runs of their own kind.
	v.Set("a.b")
	tb.SetCaret(1)
	tb.selectWord()
	if got := selText(tb); got != "." {
		t.Errorf("double click on punctuation selected %q, want %q", got, ".")
	}
}

// Caret movement is damage like any other property change, and just as
// local — the contract the spec names.
func TestCaretMovementRepaintsOneComponent(t *testing.T) {
	v := prop.NewSource("hello world")
	tb := &TextBox{Text: v}
	root := &VStack{Children: []gooey.Component{&Text{Content: Str("a")}, tb, &Text{Content: Str("b")}}}
	c := gooey.NewComposer(root, 20, 4)
	c.Frame()

	for _, ev := range []input.KeyEvent{
		input.Named(input.KeyEnd),
		input.Named(input.KeyHome),
		input.Named(input.KeyRight),
		ctrl(input.KeyRight),
		shift(input.KeyLeft),
		input.Named(input.KeyEnd),
	} {
		tb.HandleKey(ev)
		if _, painted := c.Frame(); painted != 1 {
			t.Fatalf("%v painted %d components, want exactly 1", ev, painted)
		}
	}
	// A movement key that cannot move anything is not damage at all.
	tb.HandleKey(input.Named(input.KeyEnd))
	if _, painted := c.Frame(); painted != 0 {
		t.Errorf("end at the end painted %d components, want 0", painted)
	}
	tb.HandleKey(input.Named(input.KeyRight))
	if _, painted := c.Frame(); painted != 0 {
		t.Errorf("right at the end painted %d components, want 0", painted)
	}
}
