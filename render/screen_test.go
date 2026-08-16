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

// The line-editing sequences. These are the ones whose ABSENCE spliced an
// editor's inserted line on top of the line already there, and until now
// nothing pinned them — the package doc named the bug and no test could
// have caught it coming back.
//
// Each case asserts the whole row, because that is where these go wrong:
// an off-by-one in the copy leaves a duplicated or dropped character at
// one end, and an assertion on one cell is blind to exactly that.
func TestLineEditingSequences(t *testing.T) {
	for _, tc := range []struct {
		name, in, want string
	}{
		// ICH opens a hole AT the cursor and pushes the rest right; the
		// characters shifted off the end are gone.
		{"ICH opens a hole", "abcdef\x1b[1;3H\x1b[2@", "ab  cdef"},
		{"ICH past the edge clamps", "abcdef\x1b[1;3H\x1b[99@", "ab      "},
		// DCH closes one and pulls the rest left, blanking the tail.
		{"DCH closes a hole", "abcdef\x1b[1;3H\x1b[2P", "abef    "},
		{"DCH past the edge clamps", "abcdef\x1b[1;3H\x1b[99P", "ab      "},
		// ECH blanks in place — no shift at all. Getting this one wrong
		// as a DCH is why they are tested next to each other.
		{"ECH blanks without shifting", "abcdef\x1b[1;3H\x1b[2X", "ab  ef  "},
		// The default parameter is 1, not 0. A zero would make every one
		// of these a no-op and the screen would merely look untouched.
		{"ICH defaults to one", "abcdef\x1b[1;3H\x1b[@", "ab cdef "},
		{"DCH defaults to one", "abcdef\x1b[1;3H\x1b[P", "abdef   "},
	} {
		s := NewScreen(8, 1)
		if _, err := s.Write([]byte(tc.in)); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got := padTo(s.Row(0), 8); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
}

// IL, DL and the scrolling region, on rows rather than columns. IL/DL are
// relative to the region, which is the half a test on a default region
// cannot see: with the region ignored, `DECSTBM 2;3` then `DL` would pull
// row 4 up into row 3 instead of leaving it alone.
func TestInsertDeleteLineRespectTheScrollingRegion(t *testing.T) {
	rows := func(s *Screen) [4]string {
		var out [4]string
		for i := range out {
			out[i] = padTo(s.Row(i), 2)
		}
		return out
	}

	full := NewScreen(2, 4)
	full.Write([]byte("aa\r\nbb\r\ncc\r\ndd"))
	full.Write([]byte("\x1b[2;1H\x1b[L")) // IL at row 2, whole screen
	if got, want := rows(full), [4]string{"aa", "  ", "bb", "cc"}; got != want {
		t.Errorf("IL on the full screen: %q, want %q", got, want)
	}

	full2 := NewScreen(2, 4)
	full2.Write([]byte("aa\r\nbb\r\ncc\r\ndd"))
	full2.Write([]byte("\x1b[2;1H\x1b[M")) // DL at row 2
	if got, want := rows(full2), [4]string{"aa", "cc", "dd", "  "}; got != want {
		t.Errorf("DL on the full screen: %q, want %q", got, want)
	}

	// Region rows 2..3. Row 4 is outside it and must not move.
	reg := NewScreen(2, 4)
	reg.Write([]byte("aa\r\nbb\r\ncc\r\ndd"))
	reg.Write([]byte("\x1b[2;3r\x1b[2;1H\x1b[M"))
	if got, want := rows(reg), [4]string{"aa", "cc", "  ", "dd"}; got != want {
		t.Errorf("DL inside a 2..3 region: %q, want %q", got, want)
	}

	// A delete of the whole region clears it and touches nothing else.
	wipe := NewScreen(2, 4)
	wipe.Write([]byte("aa\r\nbb\r\ncc\r\ndd"))
	wipe.Write([]byte("\x1b[2;3r\x1b[2;1H\x1b[9M"))
	if got, want := rows(wipe), [4]string{"aa", "  ", "  ", "dd"}; got != want {
		t.Errorf("DL larger than its region: %q, want %q", got, want)
	}

	// DECSTBM homes the cursor to the top of the region it just set.
	home := NewScreen(2, 4)
	home.Write([]byte("\x1b[3;4r"))
	if x, y := home.Cursor(); x != 0 || y != 2 {
		t.Errorf("after DECSTBM 3;4 the cursor is (%d,%d), want (0,2)", x, y)
	}
}

// Back-color-erase. A guest sets a background and erases to the end of
// the line to paint a status bar; if the erase blanks to the DEFAULT
// style the bar is invisible and the guest has no way to know.
//
// The cases are split by which erase does it, because they were four
// separate blanking loops and only two of them agreed.
func TestErasesUseTheCurrentBackground(t *testing.T) {
	blue := RGB(0, 0, 238)

	for _, tc := range []struct {
		name, in string
		at       [2]int
	}{
		{"EL to end of line", "\x1b[44m\x1b[K", [2]int{3, 0}},
		{"ED to end of screen", "\x1b[44m\x1b[J", [2]int{3, 1}},
		{"ECH in place", "\x1b[44m\x1b[4X", [2]int{2, 0}},
		{"ICH's new hole", "ab\x1b[44m\x1b[1;1H\x1b[2@", [2]int{0, 0}},
		{"DCH's vacated tail", "abcd\x1b[44m\x1b[1;1H\x1b[2P", [2]int{7, 0}},
		{"the rows a scroll opens", "\x1b[44m\x1b[2;1H\x1b[M", [2]int{0, 3}},
	} {
		s := NewScreen(8, 4)
		if _, err := s.Write([]byte(tc.in)); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got := s.Buf.At(tc.at[0], tc.at[1]).Style.Bg; got != blue {
			t.Errorf("%s: cell %v has bg %+v, want %+v", tc.name, tc.at, got, blue)
		}
	}

	// ...but not every attribute. Underline on a blank cell draws a rule
	// across the erased region, which is why BCE carries the colours and
	// Reverse and drops the rest.
	s := NewScreen(8, 1)
	s.Write([]byte("\x1b[1;4;7;44m\x1b[K"))
	got := s.Buf.At(3, 0).Style
	if got.Underline || got.Bold || got.Dim {
		t.Errorf("erased cell kept a glyph attribute: %+v", got)
	}
	if !got.Reverse || got.Bg != blue {
		t.Errorf("erased cell lost reverse or background: %+v", got)
	}
}

// Every CSI final that assigns s.x or s.y has to cancel the deferred
// wrap, and the three easy ones to miss are IL, DL and DECSTBM. Without
// the cancel, a row filled edge to edge followed by one of those makes
// the NEXT printable character scroll the whole screen — the exact
// failure deferred wrap was added to prevent, reached sideways.
func TestCursorMotionCancelsADeferredWrap(t *testing.T) {
	// The Z is written with NOTHING between it and the motion. An
	// intervening CUP would cancel the wrap by itself and every arm here
	// would pass no matter what the motion did — which is what the first
	// draft of this test did, and it went green against the unfixed code.
	for _, tc := range []struct{ name, motion string }{
		{"CUP", "\x1b[1;1H"},
		{"IL", "\x1b[L"},
		{"DL", "\x1b[M"},
		{"DECSTBM", "\x1b[1;3r"},
	} {
		s := NewScreen(4, 3)
		s.Write([]byte("aaaa" + tc.motion + "Z")) // fill row 0 edge to edge, move, print
		if got := padTo(s.Row(0), 4); got[0] != 'Z' {
			t.Errorf("%s: row 0 is %q, want it to start with Z — a stale wrap "+
				"advanced the line before the write landed", tc.name, got)
		}
	}

	// And the other half: a sequence that is NOT a motion must leave the
	// pending wrap armed. Without this the test above is satisfied by
	// clearing wrapNext unconditionally, which would break every
	// full-width row this package emits.
	s := NewScreen(4, 3)
	s.Write([]byte("aaaa\x1b[0mZ")) // SGR moves nothing
	if got := padTo(s.Row(0), 4); got != "aaaa" {
		t.Errorf("SGR cancelled a pending wrap: row 0 is %q, want %q", got, "aaaa")
	}
	if got := padTo(s.Row(1), 4); got[0] != 'Z' {
		t.Errorf("SGR cancelled a pending wrap: row 1 is %q, want it to start with Z", got)
	}
}

// Row() reports the row up to its last non-blank cell, which is what a
// test asserting on text wants and not what a test asserting on the
// SHAPE of an edit wants: "abef" and "abef    " are the same string to
// it, and the difference is precisely what an off-by-one produces.
func padTo(s string, n int) string {
	for len(s) < n {
		s += " "
	}
	return s
}
