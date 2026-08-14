// Tests for the pane's two tiers and for the art behind the pixel one.
//
// The pane draws through paint/ rather than through a templated SVG, and
// what has to survive that is not "it still compiles": the ring geometry,
// the interior staying on the cell plane, the cell tier occupying exactly
// the same cells, and the damage the pane costs when something near it
// changes. Every repaint claim below is pinned with the count Composer
// returns, because a bounds assertion or a cell assertion passes just as
// well when the whole tree repainted.
package panel

import (
	"bytes"
	"image"
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/graphics"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
	"github.com/WonderForgeLabs/gooey/term"
)

// term8x16 is a conventional terminal cell. The art is generated against
// whatever the terminal reports, so the size is a parameter of the test.
func term8x16(cols, rows int) term.Caps {
	return term.Caps{Cols: cols, Rows: rows, CellW: 8, CellH: 16, Color: render.TrueColor}
}

// page puts a pane over a text line, so there is a neighbour whose repaint
// can be provoked without touching the pane. enc nil is the cell tier.
func page(enc graphics.Encoder) (*gooey.Composer, *Pane, *prop.Property[string]) {
	below := prop.NewSource("footer")
	p := &Pane{
		Title: "Files",
		Child: &components.Text{Content: components.Str("inside")},
		art:   NewArt(),
		style: render.Style{Fg: render.RGB(0x6c, 0x9c, 0xff)},
	}
	p.LayoutProps().Height = 8
	root := &components.VStack{Children: []gooey.Component{p, &components.Text{Content: below}}}
	c := gooey.NewComposer(root, 30, 10)
	c.SetCaps(term8x16(30, 10))
	if enc != nil {
		c.SetGraphics(enc)
	}
	return c, p, below
}

func flush(t *testing.T, c *gooey.Composer) string {
	t.Helper()
	var buf bytes.Buffer
	c.Frame()
	if err := c.Flush(&buf); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

// ---- the pixel tier ----

// TestPixelTierPlacesFourSlicesAndNothingElse is the ring claim: four
// placements, covering the perimeter, and an interior that carries no
// image at all so the pane's own text is drawn by the terminal.
func TestPixelTierPlacesFourSlicesAndNothingElse(t *testing.T) {
	c, p, _ := page(graphics.Kitty{})
	c.Frame()

	f, _ := c.Frame()
	pl := f.Placements()
	if len(pl) != 4 {
		t.Fatalf("the pane placed %d images, want 4 (top, bottom, left, right)", len(pl))
	}
	b := p.Bounds()
	want := []graphics.Placement{
		{Col: b.X, Row: b.Y, Cols: b.W, Rows: 1},
		{Col: b.X, Row: b.Y + b.H - 1, Cols: b.W, Rows: 1},
		{Col: b.X, Row: b.Y + 1, Cols: 1, Rows: b.H - 2},
		{Col: b.X + b.W - 1, Row: b.Y + 1, Cols: 1, Rows: b.H - 2},
	}
	for i, w := range want {
		g := pl[i]
		if g.Col != w.Col || g.Row != w.Row || g.Cols != w.Cols || g.Rows != w.Rows {
			t.Errorf("slice %d at %d,%d %dx%d cells, want %d,%d %dx%d",
				i, g.Col, g.Row, g.Cols, g.Rows, w.Col, w.Row, w.Cols, w.Rows)
		}
		if g.Img == nil {
			t.Errorf("slice %d has no image", i)
		}
	}
	// Nothing covers the interior. Asserted as a rectangle test rather than
	// inferred from the four above, because that is the property the pane
	// exists to have: text inside a pane is drawn by the terminal.
	interior := image.Rect(b.X+1, b.Y+1, b.X+b.W-1, b.Y+b.H-1)
	if interior.Empty() {
		t.Fatal("the interior is empty, so the check below is vacuous")
	}
	for i, g := range pl {
		r := image.Rect(g.Col, g.Row, g.Col+g.Cols, g.Row+g.Rows)
		if !r.Intersect(interior).Empty() {
			t.Errorf("slice %d %v reaches into the interior %v", i, r, interior)
		}
	}
	// And the child really is on the cell plane inside it.
	if got := c.Cells().At(b.X+1, b.Y+1).Rune; got != 'i' {
		t.Errorf("the first interior cell holds %q, want the child's text", got)
	}
}

// TestPixelTierTitleIsOnTheCellPlane. The title sits over the top slice as
// runes, not as rasterized glyphs.
func TestPixelTierTitleIsOnTheCellPlane(t *testing.T) {
	c, p, _ := page(graphics.Kitty{})
	c.Frame()
	b := p.Bounds()
	var got strings.Builder
	for x := b.X + 2; x < b.X+2+len(" Files "); x++ {
		got.WriteRune(c.Cells().At(x, b.Y).Rune)
	}
	if got.String() != " Files " {
		t.Errorf("the top edge reads %q, want the title on the cell plane", got.String())
	}
}

// TestANeighbourRepaintLeavesTheArtAlone is the damage pin. Setting a
// sibling's text repaints exactly one component — the sibling — and puts
// no image on the wire, because the pane's paint node never re-ran.
func TestANeighbourRepaintLeavesTheArtAlone(t *testing.T) {
	c, _, below := page(graphics.Kitty{})
	flush(t, c)

	below.Set("changed")
	f, painted := c.Frame()
	if painted != 1 {
		t.Fatalf("a sibling's text change repainted %d components, want 1", painted)
	}
	// The pane's four slices are still in the frame — they are its node's
	// output, reused — but nothing was retransmitted.
	if n := len(f.Placements()); n != 4 {
		t.Errorf("the frame carries %d placements, want the pane's 4 still there", n)
	}
	var buf bytes.Buffer
	if err := c.Flush(&buf); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(buf.String(), "a=T,f=100,"); n != 0 {
		t.Errorf("a sibling's repaint transmitted %d images, want 0:\n%q", n, buf.String())
	}

	// Discrimination. "No images went on the wire" is satisfied just as
	// well by a pane that can never transmit at all, so provoke a repaint
	// that MUST reach the terminal: a resize changes the pane's size in
	// cells, which is a different raster.
	c.Resize(28, 10)
	_, painted = c.Frame()
	if painted < 2 {
		t.Fatalf("a resize repainted %d components; the frame below is not "+
			"evidence of anything if the pane did not repaint", painted)
	}
	buf.Reset()
	if err := c.Flush(&buf); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(buf.String(), "a=T,f=100,"); n == 0 {
		t.Error("a resize transmitted no images at all, so the zero above " +
			"does not distinguish a quiet pane from a mute one")
	}
}

// ---- the cell tier ----

// TestCellTierDrawsTheSameShapeInRunes. With no graphics encoder the pane
// is box-drawing characters, and it must occupy exactly the cells the
// pixel tier occupies — a pane cannot move because a terminal turns out
// not to speak a pixel protocol.
func TestCellTierDrawsTheSameShapeInRunes(t *testing.T) {
	cell, cp, _ := page(nil)
	cell.Frame()
	pix, pp, _ := page(graphics.Kitty{})
	pix.Frame()

	if cp.Bounds() != pp.Bounds() {
		t.Fatalf("the pane is at %+v on the cell tier and %+v on the pixel tier; "+
			"layout has to be identical everywhere", cp.Bounds(), pp.Bounds())
	}
	cb := cp.Child.(*components.Text).Bounds()
	pb := pp.Child.(*components.Text).Bounds()
	if cb != pb {
		t.Errorf("the child is at %+v on the cell tier and %+v on the pixel tier", cb, pb)
	}

	b := cp.Bounds()
	for _, c := range []struct {
		x, y int
		want rune
	}{
		{b.X, b.Y, '╭'},
		{b.X + b.W - 1, b.Y, '╮'},
		{b.X, b.Y + b.H - 1, '╰'},
		{b.X + b.W - 1, b.Y + b.H - 1, '╯'},
		{b.X + b.W - 2, b.Y, '─'},
		{b.X, b.Y + 1, '│'},
		{b.X + b.W - 1, b.Y + 1, '│'},
	} {
		if got := cell.Cells().At(c.x, c.y).Rune; got != c.want {
			t.Errorf("cell tier at %d,%d is %q, want %q", c.x, c.y, got, c.want)
		}
	}
	// And no image was placed at all.
	f, _ := cell.Frame()
	if n := len(f.Placements()); n != 0 {
		t.Errorf("the cell tier placed %d images, want 0", n)
	}
}

// TestCellTierIsChosenByTheTierTest walks the three conditions that select
// it, one at a time.
//
// What it pins is the OUTCOME, not the guard: measured by A/B, deleting
// `cw <= 0 || ch <= 0` from Render leaves every arm passing, because
// paint.Canvas then refuses the zero cell size and the error path lands on
// renderCells anyway. The two are redundant with each other by design —
// belt and braces on the one condition that would otherwise draw a pane
// with no edges at all. Removing BOTH is what turns this red, which is the
// property worth having.
func TestCellTierIsChosenByTheTierTest(t *testing.T) {
	for _, c := range []struct {
		name       string
		enc        graphics.Encoder
		cellW      int
		cellH      int
		wantPixels bool
	}{
		{"no encoder", nil, 8, 16, false},
		{"no cell width", graphics.Kitty{}, 0, 16, false},
		{"no cell height", graphics.Kitty{}, 8, 0, false},
		{"all three", graphics.Kitty{}, 8, 16, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			p := &Pane{Title: "T", art: NewArt(), Child: &components.Text{Content: components.Str("x")}}
			p.LayoutProps().Height = 8
			comp := gooey.NewComposer(&components.VStack{Children: []gooey.Component{p}}, 30, 10)
			comp.SetCaps(term.Caps{Cols: 30, Rows: 10, CellW: c.cellW, CellH: c.cellH, Color: render.TrueColor})
			if c.enc != nil {
				comp.SetGraphics(c.enc)
			}
			f, _ := comp.Frame()
			got := len(f.Placements()) > 0
			if got != c.wantPixels {
				t.Fatalf("placements=%v, want %v", got, c.wantPixels)
			}
			// Whichever tier ran, the frame is drawn: the corner is either a
			// rune or covered by a slice. A tier test that fell through to
			// neither would leave the pane with no edges at all.
			if !got {
				if r := comp.Cells().At(p.Bounds().X, p.Bounds().Y).Rune; r != '╭' {
					t.Errorf("no placement AND no rune at the corner: %q", r)
				}
			}
		})
	}
}

// ---- the art ----

// TestArtCachesBySizeCellSizeAndColour. The cache key is what makes the
// 1.4 ms draw a 1.3 us lookup, and getting it wrong is invisible: a stale
// entry is a correctly-shaped frame of the wrong size.
func TestArtCachesBySizeCellSizeAndColour(t *testing.T) {
	a := NewArt()
	fg := render.RGB(0x6c, 0x9c, 0xff)
	first, err := a.frame(20, 6, 8, 16, fg)
	if err != nil {
		t.Fatal(err)
	}
	again, err := a.frame(20, 6, 8, 16, fg)
	if err != nil {
		t.Fatal(err)
	}
	if first != again {
		t.Error("the same size and colour rasterized twice")
	}
	other, err := a.frame(20, 6, 8, 16, render.RGB(0xff, 0, 0))
	if err != nil {
		t.Fatal(err)
	}
	if other == first {
		t.Error("a different colour reused the cached frame")
	}

	// The one that a pixel-sized key would have got wrong: 40 cells of 8px
	// and 20 cells of 16px are the same 320-pixel canvas, and they slice
	// into different rings.
	wide, err := a.frame(40, 6, 8, 16, fg)
	if err != nil {
		t.Fatal(err)
	}
	tall, err := a.frame(20, 6, 16, 16, fg)
	if err != nil {
		t.Fatal(err)
	}
	if wide == tall {
		t.Fatal("two panes with the same pixel canvas but different cell sizes " +
			"shared a cache entry")
	}
	if got, want := wide.left.Bounds().Dx(), 8; got != want {
		t.Errorf("the 8px-cell pane's left slice is %d px wide, want %d", got, want)
	}
	if got, want := tall.left.Bounds().Dx(), 16; got != want {
		t.Errorf("the 16px-cell pane's left slice is %d px wide, want %d", got, want)
	}
}

// TestFrameRefusesAnUnprobedTerminal. paint.Canvas is what catches a cell
// size of zero; the pane must pass that error up rather than hand back a
// frame of empty images, and the message has to name the cause.
func TestFrameRefusesAnUnprobedTerminal(t *testing.T) {
	a := NewArt()
	fr, err := a.frame(20, 6, 0, 0, render.RGB(1, 2, 3))
	if err == nil {
		t.Fatal("a zero cell size produced a frame")
	}
	if fr != nil {
		t.Error("an error came back alongside a frame")
	}
	for _, want := range []string{"panel", "cell size"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

// TestDrawnCanvasIsInkedOnTheEdgeAndClearInside is the picture itself,
// asserted where it matters: the border is opaque on the boundary, the
// interior is untouched so the encoder writes no pixel there, and the
// rounded corners are clear.
func TestDrawnCanvasIsInkedOnTheEdgeAndClearInside(t *testing.T) {
	const cw, ch = 8, 16
	const cols, rows = 40, 12
	dc, err := drawCanvas(cols, rows, cw, ch, render.RGB(0x6c, 0x9c, 0xff))
	if err != nil {
		t.Fatal(err)
	}
	img := dc.Image()
	w, h := cols*cw, rows*ch
	alpha := func(x, y int) int {
		_, _, _, a := img.At(x, y).RGBA()
		return int(a >> 8)
	}

	// The four edges, sampled at their midpoints, are fully inked.
	for _, p := range [][2]int{{w / 2, 0}, {w / 2, h - 1}, {0, h / 2}, {w - 1, h / 2}} {
		if a := alpha(p[0], p[1]); a != 255 {
			t.Errorf("the edge at %d,%d has alpha %d, want 255", p[0], p[1], a)
		}
	}
	// The stroke is 1.5 px: the second pixel in is half covered and the
	// third is empty. This is the claim that the art is authored in output
	// pixels — a scaled bitmap would smear it.
	if a := alpha(w/2, 1); a < 120 || a > 136 {
		t.Errorf("the second pixel of the top stroke has alpha %d, want about half "+
			"of 255 for a 1.5 px stroke", a)
	}
	if a := alpha(w/2, 2); a != 0 {
		t.Errorf("the third pixel of the top stroke has alpha %d, want 0", a)
	}

	// The corner pixel is OUTSIDE a 6 px round, so it is clear — this is
	// what leaves the terminal's own cell showing through.
	if a := alpha(0, 0); a != 0 {
		t.Errorf("the corner pixel has alpha %d, want 0 for a rounded corner", a)
	}
	// The interior is untouched everywhere.
	for _, p := range [][2]int{{w / 2, h / 2}, {cw + 1, ch + 1}, {w - cw - 2, h - ch - 2}} {
		if a := alpha(p[0], p[1]); a != 0 {
			t.Errorf("the interior at %d,%d has alpha %d; a filled interior would "+
				"bury the pane's own text", p[0], p[1], a)
		}
	}
}

// TestTheHairlineHasFlatEnds pins the one gg-versus-SVG default that would
// otherwise change the picture silently. gg's zero LineCap is Round, so a
// Stroke literal that omits Cap draws half a pen-width PAST each end of
// every line; SVG's default, and paint.ParseLineCap's default for an
// omitted attribute, is flat. Measured at the hairline's left end.
func TestTheHairlineHasFlatEnds(t *testing.T) {
	const cw, ch = 8, 16
	// Six rows keeps the hairline (h/8 = 12) clear of the top stroke, so
	// what is measured is the hairline and not the border.
	const cols, rows = 40, 6
	dc, err := drawCanvas(cols, rows, cw, ch, render.RGB(0xff, 0xff, 0xff))
	if err != nil {
		t.Fatal(err)
	}
	y := int(hairlineY(rows * ch))
	alpha := func(x int) int {
		_, _, _, a := dc.Image().At(x, y).RGBA()
		return int(a >> 8)
	}
	if a := alpha(int(hairlineInset)); a == 0 {
		t.Fatalf("no hairline at x=%d,y=%d at all, so the cap check below is vacuous",
			int(hairlineInset), y)
	}
	if a := alpha(int(hairlineInset) - 1); a != 0 {
		t.Errorf("there is ink at x=%d (alpha %d), one pixel BEFORE the hairline's "+
			"declared start — the cap is round or square, not flat",
			int(hairlineInset)-1, a)
	}
}

// TestHairlineOpacityIsPremultiplied. gg's pattern painter composites with
// alpha-premultiplied colour, which is what color.RGBA means. A straight
// colour with a low alpha would paint a line too bright by 1/opacity, and
// it would still be a line — so this measures the value, not its presence.
func TestHairlineOpacityIsPremultiplied(t *testing.T) {
	white := render.RGB(0xff, 0xff, 0xff)
	c, ok := fade(white, hairlineOpacity).(interface {
		RGBA() (uint32, uint32, uint32, uint32)
	})
	if !ok {
		t.Fatal("fade did not return a colour")
	}
	r, _, _, a := c.RGBA()
	if got, want := a>>8, uint32(102); got != want {
		t.Errorf("alpha = %d, want %d (0.4 of 255)", got, want)
	}
	if got := r >> 8; got != a>>8 {
		t.Errorf("white at 40%% has red %d and alpha %d; premultiplied, an opaque "+
			"channel equals the alpha", got, a>>8)
	}
}
