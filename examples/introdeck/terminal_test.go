package main

import (
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/render"
)

// The capture contract, which is behavioural rather than visual and so
// has no damage count to pin it:
//
//   - while INERT the Terminal must decline every key, or the deck stops
//     advancing on any slide that has a guest on it;
//   - a click must capture, because it is the only discoverable way in
//     and its absence was reported as "slide 5 doesn't accept input";
//   - ctrl+alt+ANY and ctrl+] must both toggle, because a recorded pty
//     cannot carry a mouse report and the keyboard path has to survive.
func TestInertTerminalDeclinesKeysSoTheDeckKeepsMoving(t *testing.T) {
	term := NewTerminal("true", 20, 5)
	for _, ev := range []input.KeyEvent{
		{Key: input.KeyRune, Rune: 'n'},
		{Key: input.KeyRight},
		{Key: input.KeyRune, Rune: 'q'},
		{Key: input.KeyEnter},
	} {
		if term.HandleKey(ev) {
			t.Errorf("inert terminal consumed %v — the deck cannot advance past this slide", ev)
		}
	}
}

func TestClickCaptures(t *testing.T) {
	term := NewTerminal("true", 20, 5)
	if term.live.Get() {
		t.Fatal("a terminal must start inert, or a slide traps the presenter on arrival")
	}
	if !term.HandleMouse(input.MouseEvent{Kind: input.MousePress, X: 2, Y: 2}) {
		t.Fatal("a press on an inert terminal must be consumed and must capture")
	}
	if !term.live.Get() {
		t.Fatal("press did not capture")
	}
	// A second press must NOT release: releasing on click is how you lose
	// a capture by clicking inside the thing you are using.
	term.HandleMouse(input.MouseEvent{Kind: input.MousePress, X: 3, Y: 3})
	if !term.live.Get() {
		t.Fatal("a press while captured released the capture")
	}
}

func TestReleaseChords(t *testing.T) {
	release := []input.KeyEvent{
		{Key: input.KeyRune, Rune: 'x', Mods: input.ModCtrl | input.ModAlt},
		{Key: input.KeyRune, Rune: 'q', Mods: input.ModCtrl | input.ModAlt},
		{Key: input.KeyEsc, Mods: input.ModCtrl | input.ModAlt},
		{Key: input.KeyRune, Rune: ']', Mods: input.ModCtrl},
	}
	for _, ev := range release {
		if !isRelease(ev) {
			t.Errorf("%v is not recognised as a release chord", ev)
		}
	}
	for _, ev := range []input.KeyEvent{
		{Key: input.KeyRune, Rune: 'x', Mods: input.ModCtrl},
		{Key: input.KeyRune, Rune: 'x', Mods: input.ModAlt},
		{Key: input.KeyRune, Rune: ']'},
		{Key: input.KeyRune, Rune: '}', Mods: input.ModCtrl},
	} {
		if isRelease(ev) {
			t.Errorf("%v must NOT release — it is a key the guest needs", ev)
		}
	}

	// And the toggle works from either side, which is what gives a
	// recorded run (no mouse) a way in as well as out.
	term := NewTerminal("true", 20, 5)
	for i, want := range []bool{true, false, true} {
		if !term.HandleKey(release[0]) {
			t.Fatalf("step %d: release chord not consumed", i)
		}
		if term.live.Get() != want {
			t.Fatalf("step %d: live = %v, want %v", i, term.live.Get(), want)
		}
	}
}

// The guest must not receive a mouse report it never asked for: those
// bytes land on its stdin and it types them into itself.
func TestMouseIsNotForwardedUnlessTheGuestAskedForIt(t *testing.T) {
	term := NewTerminal("true", 20, 5)
	term.live.Set(true)
	if got := term.encodeMouse(input.MouseEvent{Kind: input.MousePress, X: 1, Y: 1}); got != nil {
		t.Fatalf("encoded %q for a guest with tracking off", got)
	}
}

// The caret. gooey hides the real terminal cursor for the whole frame,
// so a hosted shell sitting at its prompt had nothing on screen to say
// it was waiting for a key — every island read as a screenshot. The
// caret has to be PAINTED, and painted on top of the guest's own cell so
// it does not erase the character under it.
func TestTheGuestsCaretIsPainted(t *testing.T) {
	term := NewTerminal("true", 20, 5)
	term.Arrange(gooey.Rect{X: 3, Y: 2, W: 20, H: 5})
	if _, err := term.scr.Write([]byte("$ ls")); err != nil {
		t.Fatal(err)
	}

	f := &gooey.Frame{Cells: render.NewBuffer(40, 12)}
	term.Render(f)

	// The guest's cursor is at column 4 of its own screen, which is
	// column 3+4 of the frame.
	cx, cy := term.scr.Cursor()
	if cx != 4 || cy != 0 {
		t.Fatalf("guest cursor at (%d,%d), want (4,0)", cx, cy)
	}
	at := f.Cells.At(3+cx, 2+cy)
	if !at.Style.Underline {
		t.Errorf("an INERT island should mark the caret with an underline; got %+v", at.Style)
	}

	// Captured, the caret goes solid — and the cell under it survives,
	// because a caret that overwrote the character would eat the guest's
	// output every time it stopped over a letter.
	term.live.Set(true)
	if _, err := term.scr.Write([]byte("\x1b[1;1H")); err != nil {
		t.Fatal(err)
	}
	f = &gooey.Frame{Cells: render.NewBuffer(40, 12)}
	term.Render(f)
	at = f.Cells.At(3, 2)
	if !at.Style.Reverse {
		t.Errorf("a CAPTURED island should mark the caret with reverse video; got %+v", at.Style)
	}
	if at.Rune != '$' {
		t.Errorf("the caret erased the cell under it: got %q, want '$'", at.Rune)
	}
}

// A guest that hid its cursor must not get one drawn for it: a
// full-screen program hides the caret while it repaints, and a host that
// ignored that would paint a caret wherever the last byte happened to
// land.
func TestAHiddenCursorIsNotPainted(t *testing.T) {
	term := NewTerminal("true", 20, 5)
	term.Arrange(gooey.Rect{X: 0, Y: 0, W: 20, H: 5})
	if _, err := term.scr.Write([]byte("abc\x1b[?25l")); err != nil {
		t.Fatal(err)
	}
	term.live.Set(true)

	f := &gooey.Frame{Cells: render.NewBuffer(40, 12)}
	term.Render(f)
	if at := f.Cells.At(3, 0); at.Style.Reverse || at.Style.Underline {
		t.Errorf("caret painted for a guest that sent DECTCEM off: %+v", at.Style)
	}
}
