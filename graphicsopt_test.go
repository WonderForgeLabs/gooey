package gooey

import (
	"testing"

	"github.com/WonderForgeLabs/gooey/graphics"
	"github.com/WonderForgeLabs/gooey/term"
)

// Pinning a protocol has to pin a cell size with it. Sixel scales its
// raster by CellW/CellH, and the environment ladder — the caps an app
// gets without a probe — can report a color depth but never a cell size.
// The combination used to be silent: WithGraphics(Sixel{}) with no caps
// emitted a zero-pixel image over cells that the halfblock path never got
// to paint, i.e. a black rectangle and no error.
func TestPinnedProtocolGetsACellSize(t *testing.T) {
	for _, tc := range []struct {
		name  string
		opts  []Option
		wantW int
	}{
		{"sixel pinned, no caps", []Option{WithGraphics(graphics.Sixel{})}, term.DefaultCellW},
		{"kitty pinned, no caps", []Option{WithGraphics(graphics.Kitty{})}, term.DefaultCellW},
		{"iterm2 pinned, no caps", []Option{WithGraphics(graphics.ITerm2{})}, term.DefaultCellW},
		// A nil encoder is the halfblock fallback: pixel content becomes
		// cells and no protocol ever asks how big one is.
		{"halfblock pinned", []Option{WithGraphics(nil)}, 0},
		// Nothing pinned and no capabilities: no protocol either, so there
		// is nothing for a cell size to serve.
		{"no graphics at all", nil, 0},
		// Capabilities that NAME a protocol need the same guarantee, even
		// though nothing was pinned — a hand-built Caps skips Detect, which
		// is where the rule used to live alone.
		{"caps say sixel", []Option{WithCaps(term.Caps{Sixel: true})}, term.DefaultCellW},
		// An explicit cell size is never second-guessed.
		{"caps carry their own", []Option{
			WithGraphics(graphics.Sixel{}),
			WithCaps(term.Caps{CellW: 7, CellH: 15}),
		}, 7},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := NewApp(Tree(&label{}), tc.opts...)
			a.cols, a.rows = 80, 24
			c := a.caps()
			if c.CellW != tc.wantW {
				t.Fatalf("CellW = %d, want %d", c.CellW, tc.wantW)
			}
			if c.Cols != 80 || c.Rows != 24 {
				t.Errorf("caps lost the terminal size: %dx%d", c.Cols, c.Rows)
			}
		})
	}
}

// Two hosts pinned a protocol and then hand-wrote the Caps that pinning
// implies — apps/wysiwyg and paint/cmd/plates both passed
// `WithCaps(term.Caps{CellW: 10, CellH: 20, Color: term.DetectColorDepth()})`
// beside their `WithGraphics(enc)`. Dropping those is only safe if
// App.caps computes the same thing. CellW is the half already pinned
// above; this is the other half, the Color those hosts supplied by hand.
//
// The environment is neutralized first, and that is the whole reason this
// test can fail at all. render.TrueColor is the ZERO value of ColorDepth,
// so on any developer machine reporting COLORTERM=truecolor — this repo's
// author's, and mine — `term.Caps{}.Color`, `DetectColorDepth()` and a
// caps() that had stopped defaulting Color are three indistinguishable
// zeros. Written without the Setenv block, this test passed under a
// mutation that deleted the very default it exists to pin.
func TestPinnedProtocolNeedsNoHandWrittenCaps(t *testing.T) {
	depth256(t)
	if term.DetectColorDepth() == 0 {
		t.Fatal("the environment still reports the zero ColorDepth, so every " +
			"assertion below would hold whether or not caps() defaults Color")
	}
	byHand := term.Caps{CellW: term.DefaultCellW, CellH: term.DefaultCellH, Color: term.DetectColorDepth()}

	caps := func(opts ...Option) term.Caps {
		t.Helper()
		a := NewApp(Tree(&label{}), opts...)
		a.cols, a.rows = 80, 24
		return a.caps()
	}

	// Compared as whole structs, so a field added to term.Caps later
	// cannot be silently dropped from the no-caps path.
	got := caps(WithGraphics(graphics.Sixel{}))
	want := caps(WithGraphics(graphics.Sixel{}), WithCaps(byHand))
	if got != want {
		t.Errorf("pinning a protocol alone gives %+v; spelling the caps out gives "+
			"%+v. A host that drops its hand-written Caps must get the same "+
			"composition, or #322 moved a rule and changed behaviour with it.", got, want)
	}
}

// depth256 pins DetectColorDepth to a NON-ZERO answer by emptying every
// variable its ladder consults (term/color.go: colorDepthFrom and
// knownTrueColorTerminal) and then declaring a 256-color TERM.
//
// Enumerated rather than derived, which this repo normally refuses — but
// the list is the ladder's own, three functions away in the same repo, and
// the failure mode is loud: a variable missed here leaves DetectColorDepth
// at TrueColor, which is zero, which the caller's own guard rejects.
func depth256(t *testing.T) {
	t.Helper()

	for _, v := range []string{
		"COLORTERM",
		"WT_SESSION", "KITTY_WINDOW_ID", "ALACRITTY_SOCKET", "ALACRITTY_WINDOW_ID",
		"KONSOLE_VERSION", "WEZTERM_EXECUTABLE", "GHOSTTY_RESOURCES_DIR",
		"TERM_PROGRAM", "LC_TERMINAL", "VTE_VERSION",
	} {
		t.Setenv(v, "")
	}
	t.Setenv("TERM", "xterm-256color")
}
