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
