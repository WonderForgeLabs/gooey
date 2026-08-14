package components

import (
	"bytes"
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/graphics"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
	"github.com/WonderForgeLabs/gooey/term"
)

// Image's tier test.
//
// A pixel component chooses its tier from the Frame, and the choice is
// three conditions: an encoder AND a cell width AND a cell height. The
// failure these pin is the one with no symptom to grep for — a protocol
// pinned without capabilities leaves CellW at zero, the placement branch
// is taken anyway, and the halfblock that would have painted the cells
// never runs. Nothing errors, nothing appears in screen_text (placements
// are not cells, composer.go), and the rectangle is simply blank.
//
// So these assert the POSITIVE: that at a zero cell size an Image draws
// halfblock runes into the buffer and records no placement at all.

// imageTier composes one Image beside a caption over a terminal with the
// given cell metrics and a pinned protocol. Cell size is the only thing
// that varies across the cases below — same tree, same encoder — which is
// what makes them a discriminating set rather than four spellings of one
// assertion.
func imageTier(cellW, cellH int, enc graphics.Encoder) (*gooey.Composer, *Image) {
	im := &Image{Src: prop.NewSource(swatch(200)), Cols: Cells(6), Rows: Cells(3)}
	// An image asks for a cell rectangle and means it; without this the
	// stack stretches it and the bounds stop being predictable.
	im.LayoutProps().HAlign = gooey.AlignStart
	root := &VStack{Children: []gooey.Component{im, &Text{Content: Str("caption")}}}
	c := gooey.NewComposer(root, 30, 8)
	c.SetCaps(term.Caps{Cols: 30, Rows: 8, CellW: cellW, CellH: cellH, Color: render.TrueColor})
	c.SetGraphics(enc)
	return c, im
}

// halfblockCells counts the cells of r holding the halfblock rune — what
// graphics.DrawHalfblock writes, and the only trace the cell tier leaves.
func halfblockCells(c *gooey.Composer, r gooey.Rect) int {
	n := 0
	for y := r.Y; y < r.Y+r.H; y++ {
		for x := r.X; x < r.X+r.W; x++ {
			if c.Cells().At(x, y).Rune == '▀' {
				n++
			}
		}
	}
	return n
}

// Each condition of the guard is load-bearing on its own: CellH is as
// fatal as CellW (an encoder scales to rows*CellH just as it scales to
// cols*CellW), so a guard that checks only one of them has to fail here.
// The last case is the discrimination arm — without it "always draw
// cells" would pass every other line in this test.
func TestImageTierNeedsAnEncoderAndACellSize(t *testing.T) {
	cases := []struct {
		name         string
		cellW, cellH int
		wantPixel    bool
	}{
		{"no cell size at all", 0, 0, false},
		{"cell height unknown", 8, 0, false},
		{"cell width unknown", 0, 16, false},
		{"both known", 8, 16, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, im := imageTier(tc.cellW, tc.cellH, graphics.Kitty{})
			f, _ := c.Frame()
			r := im.Bounds()
			if r.W <= 0 || r.H <= 0 {
				t.Fatalf("the image was arranged into %dx%d cells; the test proves nothing", r.W, r.H)
			}
			ink := halfblockCells(c, r)

			if tc.wantPixel {
				if n := len(f.Placements()); n != 1 {
					t.Fatalf("a capable terminal recorded %d placements, want 1", n)
				}
				// The pixel tier deliberately leaves the cells alone: the
				// placement composites over them. This is what makes the
				// halfblock counts below falsifiable.
				if ink != 0 {
					t.Fatalf("the pixel tier also drew %d halfblock cells", ink)
				}
				return
			}
			if n := len(f.Placements()); n != 0 {
				t.Fatalf("with cell size %dx%d the image recorded %d placements; "+
					"an encoder scales to cols*CellW x rows*CellH, so that is an image of zero pixels "+
					"over cells halfblock never got to paint", tc.cellW, tc.cellH, n)
			}
			if ink != r.W*r.H {
				t.Fatalf("with cell size %dx%d the image drew %d of %d halfblock cells; "+
					"the fallback did not run and the rectangle is blank",
					tc.cellW, tc.cellH, ink, r.W*r.H)
			}
		})
	}
}

// The end of the chain, on the wire, for the protocol that cannot survive
// a zero cell size: Sixel.Encode refuses rather than emit its eighteen
// empty bytes (graphics/sixel.go), so before the guard this frame did not
// merely draw nothing — it failed the flush. Now nothing reaches the pixel
// plane and the cells carry the picture instead.
func TestImageWithoutACellSizeFlushesCellsNotSixel(t *testing.T) {
	c, im := imageTier(0, 0, graphics.Sixel{})
	c.Frame()
	var buf bytes.Buffer
	if err := c.Flush(&buf); err != nil {
		t.Fatalf("the flush failed instead of falling back to cells: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "\x1bP") {
		t.Fatalf("a sixel DCS went on the wire at a zero cell size:\n%q", out)
	}
	if !strings.Contains(out, "▀") {
		t.Fatalf("no halfblock reached the terminal; the image's %v cells are blank:\n%q",
			im.Bounds(), out)
	}
}
