package render

import "testing"

// ESC ( B is how a program says "G0 is ASCII", and top says it several
// times a second. It is three bytes; consuming two leaves the B to be
// printed, which is how a hosted top ends up looking like it is shouting.
func TestCharsetDesignationIsConsumed(t *testing.T) {
	for _, in := range []string{
		"\x1b(Bhello",
		"\x1b)0hello",
		"\x1b*Bhello",
		"\x1b+Bhello",
		"\x1b#8hello",
		"\x1b Fhello",
	} {
		s := NewScreen(20, 1)
		if _, err := s.Write([]byte(in)); err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		if got := s.Row(0); got != "hello" {
			t.Errorf("%q → %q, want %q", in, got, "hello")
		}
	}
}

// The indexed SGR forms. Screen was a test model when it was written, so
// it only understood 38/48 and stashed the 256-colour index raw. Both
// gaps became visible the moment a hosted guest's cells were blitted
// into a frame: SGR 32 painted in whatever colour came before it, and
// index 4 — blue — painted as RGB(4,0,0), which is black.
func TestIndexedColorsResolveToRGB(t *testing.T) {
	blue := RGB(0, 0, 238)
	brightRed := RGB(255, 0, 0)

	for _, tc := range []struct {
		name   string
		in     string
		fg, bg Color
	}{
		{"ansi fg", "\x1b[34mx", blue, Color{}},
		{"ansi bg", "\x1b[44mx", Color{}, blue},
		{"bright fg", "\x1b[91mx", brightRed, Color{}},
		{"bright bg", "\x1b[101mx", Color{}, brightRed},
		{"256 fg", "\x1b[38;5;4mx", blue, Color{}},
		{"256 bg", "\x1b[48;5;4mx", Color{}, blue},
		{"truecolor fg", "\x1b[38;2;10;20;30mx", RGB(10, 20, 30), Color{}},
		{"default fg after ansi", "\x1b[34m\x1b[39mx", Color{}, Color{}},
		{"default bg after ansi", "\x1b[44m\x1b[49mx", Color{}, Color{}},
	} {
		s := NewScreen(4, 1)
		if _, err := s.Write([]byte(tc.in)); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		got := s.Buf.At(0, 0).Style
		if got.Fg != tc.fg || got.Bg != tc.bg {
			t.Errorf("%s: fg=%+v bg=%+v, want fg=%+v bg=%+v", tc.name, got.Fg, got.Bg, tc.fg, tc.bg)
		}
	}
}

// The attribute-off codes. Without them a guest that bolds one word
// leaves every word after it bold, which is what an editor's status line
// does to the rest of the screen.
func TestAttributeResetCodes(t *testing.T) {
	s := NewScreen(8, 1)
	if _, err := s.Write([]byte("\x1b[1;4;7mA\x1b[22;24;27mB")); err != nil {
		t.Fatal(err)
	}
	if a := s.Buf.At(0, 0).Style; !a.Bold || !a.Underline || !a.Reverse {
		t.Errorf("A: %+v, want bold+underline+reverse", a)
	}
	if b := s.Buf.At(1, 0).Style; b.Bold || b.Underline || b.Reverse || b.Dim {
		t.Errorf("B: %+v, want all off", b)
	}
}

// A three-byte escape split across two writes must not be half-consumed:
// the pending buffer exists for exactly this, and a pty hands you
// arbitrary chunk boundaries.
func TestCharsetDesignationSplitAcrossWrites(t *testing.T) {
	s := NewScreen(20, 1)
	if _, err := s.Write([]byte("\x1b(")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Write([]byte("Bhello")); err != nil {
		t.Fatal(err)
	}
	if got := s.Row(0); got != "hello" {
		t.Errorf("got %q, want %q", got, "hello")
	}
}

// A host draws this model into its own cell plane, and a cell plane has
// no cursor — gooey hides the real one for the whole frame. So the caret
// a guest would show only exists if the host paints it, and it can only
// paint it if this model reports it.
func TestCursorPositionAndVisibilityAreReported(t *testing.T) {
	s := NewScreen(20, 5)
	s.Write([]byte("abc"))
	if x, y := s.Cursor(); x != 3 || y != 0 {
		t.Fatalf("after 3 printable runes: got (%d,%d), want (3,0)", x, y)
	}
	s.Write([]byte("\x1b[3;7H"))
	if x, y := s.Cursor(); x != 6 || y != 2 {
		t.Fatalf("after CUP 3;7: got (%d,%d), want (6,2)", x, y)
	}
	if !s.CursorVisible() {
		t.Fatal("a screen with no DECTCEM should report the cursor visible")
	}
	s.Write([]byte("\x1b[?25l"))
	if s.CursorVisible() {
		t.Fatal("ESC[?25l should hide the cursor")
	}
	s.Write([]byte("\x1b[?25h"))
	if !s.CursorVisible() {
		t.Fatal("ESC[?25h should show it again")
	}
	// RIS is a full reset, and a program that died with the cursor
	// hidden must not leave the next one caretless.
	s.Write([]byte("\x1b[?25l\x1bc"))
	if !s.CursorVisible() {
		t.Fatal("RIS should restore cursor visibility")
	}
}

// Deferred wrap keeps the cursor ON the last column rather than moving
// it, so a caret painted from Cursor() must land inside the screen even
// after a row has been filled edge to edge.
func TestCursorStaysInsideTheScreenAfterAFullRow(t *testing.T) {
	s := NewScreen(8, 3)
	s.Write([]byte("12345678"))
	x, y := s.Cursor()
	if x < 0 || x >= 8 || y < 0 || y >= 3 {
		t.Fatalf("cursor (%d,%d) is outside an 8x3 screen", x, y)
	}
}
