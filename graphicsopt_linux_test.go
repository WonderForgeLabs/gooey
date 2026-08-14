package gooey

import (
	"image"
	"image/color"
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey/graphics"
	"github.com/WonderForgeLabs/gooey/prop"
)

// pixelLeaf is components.Image reduced to what this test needs: a leaf
// that records a placement when there is a protocol and degrades into
// cells when there is not. The root module cannot import components, and
// the behavior under test is the Frame's, not the component's.
//
// The omission of `f.CellW > 0` that the real Image carries
// (components/image.go, issue #251) is DELIBERATE and must stay. What
// this file pins is App.caps backfilling a cell size for a pinned
// protocol; an unguarded leaf is the harsher probe of that, because a
// regression there reaches the raster-header assertion below instead of
// silently landing on the cell tier one layer earlier.
type pixelLeaf struct {
	Base
	img *prop.Property[image.Image]
}

func (p *pixelLeaf) Measure(avail Size) Size { return Size{W: 4, H: 2} }

func (p *pixelLeaf) Render(f *Frame) {
	src := p.img.Get()
	r := p.Bounds()
	if f.Graphics != nil {
		f.Place(graphics.Placement{Img: src, Col: r.X, Row: r.Y, Cols: r.W, Rows: r.H})
		return
	}
	graphics.DrawHalfblock(f.Cells, src, r.X, r.Y, r.W, r.H)
}

func solid(w, h int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: 220, G: 120, B: 40, A: 255})
		}
	}
	return img
}

// The end of the chain, on the wire. Pinning sixel with no capabilities
// behind it is the configuration a demo actually ships — a page that
// states its own protocol, started from a script or a supervisor whose
// terminal is not the one being looked at. It used to put an eighteen-byte
// sixel of zero pixels on the wire and leave the cells beneath it unpainted:
// a black rectangle, no error, nothing in `screen_text` to see, because
// placements are not cells (composer.go:679) and never appear there for a
// WORKING sixel either.
func TestPinnedSixelEmitsRealPixels(t *testing.T) {
	root := &pixelLeaf{img: prop.NewSource(solid(8, 8))}
	app, tty := newTestApp(t, root, WithGraphics(graphics.Sixel{}))
	start(t, app)
	tty.waitForFrame(t)

	if !tty.waitForBytes(t, "\x1bP0;0;0q") {
		t.Fatal("no sixel DCS reached the terminal")
	}
	// The raster header carries the pixel size, which is where a missing
	// cell size showed up as `"1;1;0;0`. The leaf is the root, so it is
	// arranged over the whole 40×10 pty: 400×200 px at the assumed 10×20.
	if !strings.Contains(tty.text(), "\"1;1;400;200") {
		t.Errorf("the sixel raster is not 40*10 x 10*20 px; a zero there is the bug:\n%q",
			firstSixel(tty.text()))
	}
}

func firstSixel(s string) string {
	i := strings.Index(s, "\x1bP")
	if i < 0 {
		return "(no DCS at all)"
	}
	return s[i:min(i+48, len(s))]
}
