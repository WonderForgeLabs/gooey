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

// TestATitleTooWideIsClippedNotDropped pins the behaviour change that came
// with sharing components.DrawBoxTitle: a title with no room used to be
// skipped entirely, and now it clips, which is what <Border> has always
// done.
//
// Both halves matter and neither implies the other. That the label appears
// at all is the change; that the far corner survives is the bug the change
// had to avoid re-introducing, because the obvious way to stop dropping a
// title is to write it and let it run over the edge — past the pane's own
// bounds, therefore outside this paint node's damage rect, where the
// Composer's sweep can never clean it.
//
// The assertions are on the pane's OWN top row rather than on a rune count,
// so they stay true if the label's budget is ever re-derived.
func TestATitleTooWideIsClippedNotDropped(t *testing.T) {
	const title = "Files And Folders And More Files"

	p := &Pane{
		Title: title,
		Child: &components.Text{Content: components.Str("inside")},
		art:   NewArt(),
		style: render.Style{Fg: render.RGB(0x6c, 0x9c, 0xff)},
	}
	p.LayoutProps().Height = 8
	c := gooey.NewComposer(&components.VStack{Children: []gooey.Component{p}}, 30, 10)
	c.SetCaps(term8x16(30, 10))
	c.Frame()

	b := p.Bounds()
	if len(title) <= b.W {
		t.Fatalf("the title fits in %d columns, so this test is not exercising the clip", b.W)
	}

	var row strings.Builder
	for x := b.X; x < b.X+b.W; x++ {
		row.WriteRune(c.Cells().At(x, b.Y).Rune)
	}
	got := row.String()

	// Not dropped: the label is there, inset one border cell and one pad.
	if !strings.HasPrefix(got, "╭─ Files") {
		t.Errorf("top row is %q; a title too wide is now clipped, not skipped, so it "+
			"should open ╭─ then the start of %q", got, title)
	}
	// Clipped, not overrun: the far corner and the cell before it are still
	// border. This is what fails if the label is written past its budget.
	if !strings.HasSuffix(got, "─╮") {
		t.Errorf("top row is %q; the title has run into the far corner, which paints "+
			"outside the pane's damage rect", got)
	}
	// And the whole label really was truncated.
	if strings.Contains(got, title) {
		t.Errorf("top row is %q; it carries the full %d-column title inside a %d-column pane",
			got, len(title), b.W)
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
	const cols, rows = 40, 6
	dc, err := drawCanvas(cols, rows, cw, ch, render.RGB(0xff, 0xff, 0xff))
	if err != nil {
		t.Fatal(err)
	}
	// A 16-pixel cell has room for the border and the hairline both, so
	// what is measured below is the hairline and not the border.
	yf, ok := hairlineY(ch)
	if !ok {
		t.Fatalf("no hairline at all for a cell %d pixels tall", ch)
	}
	y := int(yf)
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

// TestTheHairlineIsDimAndOPAQUE is both halves of the colour, and they
// pull in opposite directions on purpose.
//
// Dim, or it is a second border rather than a division. Opaque, or sixel
// never writes it: graphics/sixel.go keeps a pixel only at a >= 0x8000
// and the old 0.4 ALPHA was 102/255, so every pixel of the line was
// discarded. A test that checked only the channels would pass against
// exactly the bug that was shipped.
func TestTheHairlineIsDimAndOPAQUE(t *testing.T) {
	white := render.RGB(0xff, 0xff, 0xff)
	r, _, _, a := dim(white, hairlineFade).RGBA()

	if a < sixelKeep {
		t.Errorf("the hairline's alpha is %#04x and sixel keeps a pixel only at "+
			"%#04x — every pixel of the line is discarded before it reaches the "+
			"wire, which is #254's own symptom on that protocol", a, sixelKeep)
	}
	if got, want := r>>8, uint32(102); got != want {
		t.Errorf("white faded to %d%% has red %d, want %d: the line renders at its "+
			"own colour once sixel keeps it, so the colour is the only thing left "+
			"that can make it read as a rule rather than a border",
			int(hairlineFade*100), got, want)
	}
	// NOT VACUOUS: an OPAQUE FULL-BRIGHTNESS line clears the threshold
	// too, and it is the second border this fade exists to avoid. The
	// arm above only means something if the dimming is real.
	if fr, _, _, _ := dim(white, 1).RGBA(); r >= fr {
		t.Errorf("the faded red is %d and the undimmed red is %d — nothing was "+
			"dimmed, so the line is the border's own colour", r>>8, fr>>8)
	}
}

// sixelKeep is graphics/sixel.go's threshold, restated here because that
// is the number this package has to clear. It is deliberately a literal
// and not an import: the encoder does not export it, and a copy that
// drifts is caught by TestTheWholeHairlineClearsTheSixelThreshold below
// failing against a canvas the encoder would in fact have kept.
const sixelKeep = 0x8000

// TestTheWholeHairlineClearsTheSixelThreshold is the finding measured on
// the real canvas rather than on the constant.
//
// Sixel has no alpha channel: a pixel below half alpha is simply never
// written. Before this change the hairline was stroked at alpha 102, so
// on a 40x12 pane of 8x16 cells all 306 of its pixels were dropped and
// the sixel byte stream was identical with the flourish and without it.
// The y-coordinate fix put the line inside the placed slice; this is
// what puts it on the wire.
func TestTheWholeHairlineClearsTheSixelThreshold(t *testing.T) {
	const cols, rows, cw, ch = 40, 12, 8, 16
	dc, err := drawCanvas(cols, rows, cw, ch, render.RGB(0xff, 0xff, 0xff))
	if err != nil {
		t.Fatal(err)
	}
	y, ok := hairlineY(ch)
	if !ok {
		t.Fatalf("no hairline at all for a cell %d pixels tall", ch)
	}

	// The span BETWEEN the side strokes, which is the hairline alone —
	// including the ends, since butt caps mean the first and last
	// columns are as fully covered as the middle.
	inked, dropped := 0, 0
	for x := int(hairlineInset); x < cols*cw-int(hairlineInset); x++ {
		_, _, _, a := dc.Image().At(x, int(y)).RGBA()
		if a == 0 {
			continue
		}
		inked++
		if a < sixelKeep {
			dropped++
		}
	}
	if inked == 0 {
		t.Fatal("there is no hairline on the canvas at all, so counting what sixel " +
			"would drop from it asserts nothing")
	}
	if dropped != 0 {
		t.Errorf("%d of the hairline's %d pixels are below sixel's %#04x threshold "+
			"and are never written — the flourish is invisible on that protocol, "+
			"which is the defect the y-coordinate fix was for",
			dropped, inked, sixelKeep)
	}

	// AND STILL DIMMER THAN THE BORDER, measured at the DRAW SITE and not
	// on the constant. TestTheHairlineIsDimAndOPAQUE checks what dim
	// returns; it cannot see drawCanvas passing it the wrong fraction,
	// and "opaque" has an obvious wrong way to satisfy it — stroke the
	// line at full fg, which sixel keeps happily and which is the second
	// border the fade exists to avoid. Measured: without this arm that
	// mutation is silent.
	x := cols * cw / 2
	rule, _, _, _ := dc.Image().At(x, int(y)).RGBA()
	edge, _, _, _ := dc.Image().At(x, 0).RGBA()
	if edge == 0 {
		t.Fatal("no border ink on the top row, so there is nothing to be dimmer than")
	}
	if rule >= edge {
		t.Errorf("the rule is red %d and the border is red %d at the same x — a "+
			"rule the colour of the border is a second border, which is the whole "+
			"reason it is faded", rule>>8, edge>>8)
	}
}

// TestTheHairlineSurvivesTheRing is #254, and it is the assertion the
// package had never made.
//
// The flourish drawFrame draws is only a flourish if it is PLACED. Ring
// cuts the canvas into four rectangles and the interior is never placed
// at all, so a line drawn below the top slice is generated, measured,
// cached — and thrown away. hairlineY was given the canvas height in
// pixels and returned h/8, three cell rows down on an 80x24 pane, so for
// its entire life the line existed everywhere except on screen.
//
// MEASURED THROUGH Ring, not through drawCanvas. A test that reads the
// full canvas passes against the bug: the ink is there, it is just not in
// any slice. That is the distinction the whole issue is about, and the
// reason drawCanvas's own comment warns that re-widening a slice silently
// hands the slice back.
func TestTheHairlineSurvivesTheRing(t *testing.T) {
	// THE SAME SIX HEIGHTS TestTheHairlineClearsTheBorder uses. That one
	// covers six and never goes through Ring; this one went through Ring
	// at exactly one. The two halves of "in the slice and clear of the
	// border" were each checked over a set the other did not, so a cell
	// height where the arithmetic put the line one pixel past the slice
	// had only the helper's word for it.
	for _, ch := range []int{8, 12, 16, 20, 24, 32} {
		const cw = 8
		const cols, rows = 40, 6
		f, err := drawFrame(cols, rows, cw, ch, render.RGB(0xff, 0xff, 0xff))
		if err != nil {
			t.Fatalf("cell height %d: %v", ch, err)
		}
		y, ok := hairlineY(ch)
		if !ok {
			t.Errorf("cell height %d: no hairline, though there is room for one", ch)
			continue
		}
		row := int(y)

		b := f.top.Bounds()
		if row < b.Min.Y || row >= b.Max.Y {
			t.Errorf("cell height %d: the hairline is at y=%d and the ring's top "+
				"slice covers y in [%d,%d) — the line is drawn onto a part of the "+
				"canvas no placement ever shows, which is exactly #254",
				ch, row, b.Min.Y, b.Max.Y)
			continue
		}

		// MID-SPAN, away from both corners and both side slices: the old
		// arithmetic left a stub at each extreme end where the line
		// crossed the left and right slices, so a sample near an end
		// passes against the bug.
		x := cols * cw / 2
		if _, _, _, a := f.top.At(x, row).RGBA(); a>>8 == 0 {
			t.Errorf("cell height %d: the ring's top slice has no ink at x=%d,y=%d, "+
				"so the hairline is absent from the only rectangle that gets placed",
				ch, x, row)
		}
	}
}

// TestTheHairlineClearsTheBorder is the other side of the placement: the
// line has to be INSIDE the top slice and BELOW the border's stroke, and
// a fix that satisfies the first by merging the two into one thick edge
// is not a fix. The border is inset by half its width, so its stroke
// occupies y in [0, borderWidth]; the hairline's own stroke is centred on
// hairlineY, so its top edge must clear that.
func TestTheHairlineClearsTheBorder(t *testing.T) {
	for _, ch := range []int{8, 12, 16, 20, 24, 32} {
		y, ok := hairlineY(ch)
		if !ok {
			t.Errorf("cell height %d: no hairline, though there is room for one", ch)
			continue
		}
		if top := y - hairlineWidth/2; top < borderWidth {
			t.Errorf("cell height %d: the hairline's top edge is at %.2f and the "+
				"border's stroke reaches %.2f — they overlap, so the flourish "+
				"reads as a thicker border rather than a division", ch, top, borderWidth)
		}
		if bot := y + hairlineWidth/2; bot > float64(ch) {
			t.Errorf("cell height %d: the hairline's bottom edge is at %.2f, past the "+
				"end of the ring's top slice at %d — the line is clipped", ch, bot, ch)
		}
	}
}

// TestTheHairlineCostsExactlyOnePixelRowOfTheTitleCell is the answer to
// "does the rule cross the title's descenders", as far as this repo can
// answer it.
//
// It does, and it cannot not: Ring's top slice is exactly one cell tall,
// so a rule under the title has nowhere to be except inside the row the
// title occupies — putting it in the next row is #254 again, drawn onto
// a part of the canvas no placement shows. The border's own 1.5-pixel
// stroke already crosses the same glyphs at the top of the same row, so
// the shape of the question is not new; what this change made sharper is
// that an opaque rule COVERS rather than tints, which is also the only
// form sixel can carry.
//
// EYEBALLING IT IS NOT AVAILABLE HERE and saying so is the point: the
// glyphs are the terminal's, drawn from a font this process never sees,
// so how deep a descender reaches into the cell is not a fact the repo
// holds. What IS checkable is the bound — the rule takes the LAST pixel
// row and no more, so whatever it crosses, it crosses one row of. A
// change that widened it, or floated it up off the bottom edge into the
// x-height, would take more and this fails.
func TestTheHairlineCostsExactlyOnePixelRowOfTheTitleCell(t *testing.T) {
	for _, ch := range []int{8, 12, 16, 20, 24, 32} {
		y, ok := hairlineY(ch)
		if !ok {
			t.Errorf("cell height %d: no hairline, though there is room for one", ch)
			continue
		}
		top, bot := y-hairlineWidth/2, y+hairlineWidth/2
		if bot != float64(ch) {
			t.Errorf("cell height %d: the rule's bottom edge is at %.2f, not flush "+
				"with the cell's %d — a rule that floats up off the bottom sits in "+
				"the title's x-height instead of under its baseline", ch, bot, ch)
		}
		if n := bot - top; n != 1 {
			t.Errorf("cell height %d: the rule covers %.2f pixel rows of the title's "+
				"cell, want 1 — every row it takes is a row of glyph it hides",
				ch, n)
		}
	}
}

// TestACellTooShortForBothGetsNoHairline pins the third state.
//
// hairlineY returns a bool rather than clamping, and this is why: at a
// cell height where the border's stroke and the hairline's cannot both
// fit, a clamped value would place the line under the border, where it
// either does not show or thickens it. "No room" is the honest answer,
// and drawCanvas skips the stroke entirely — an invisible flourish is the
// defect #254 exists for, and reintroducing it at small cell sizes would
// be the same bug in a corner.
func TestACellTooShortForBothGetsNoHairline(t *testing.T) {
	// borderWidth 1.5 + hairlineWidth 1.0 needs 2.5 pixels; a 2-pixel
	// cell cannot hold both.
	if y, ok := hairlineY(2); ok {
		t.Errorf("a 2-pixel cell reports a hairline at %.2f, but the border's stroke "+
			"alone reaches %.2f", y, borderWidth)
	}
	// And the canvas still draws, with no line on it. "It did not error"
	// is not that assertion: ignoring the bool draws the hairline at y=0,
	// which errors nowhere and lands on top of the border.
	const cw, ch = 4, 2
	const cols = 10
	dc, err := drawCanvas(cols, 4, cw, ch, render.RGB(0xff, 0xff, 0xff))
	if err != nil {
		t.Fatalf("a pane with %dx%d cells does not draw at all: %v", cw, ch, err)
	}
	alpha := func(x, y int) int {
		_, _, _, a := dc.Image().At(x, y).RGBA()
		return int(a >> 8)
	}
	// TWO POINTS ON THE SAME BORDER STROKE, one of them inside the span a
	// hairline would cover and one outside it. The rounded rectangle's
	// straight top segment runs between the corner arcs, so both samples
	// sit on identical coverage; hairlineInset is where the line would
	// start, so only the second can gain ink from one.
	const outside, inside = int(hairlineInset) - 2, cols * cw / 2
	// BOTH ENDS. `outside >= hairlineInset` is the only half that was
	// written and it is 5 >= 7 — a compile-time constant, so the guard
	// could never fire in either direction. The half that can actually
	// go wrong is the other one: hairlineInset dropping below 2.0 puts
	// the reference sample off the left edge of the canvas, where At()
	// returns the zero colour and the `bare == 0` fatal below fires with
	// a message about missing border ink that would be a lie.
	if outside >= int(hairlineInset) || outside < 0 {
		t.Fatalf("the reference sample at x=%d is not a reference: it must be off "+
			"the hairline's span, which starts at %d, and on the canvas",
			outside, int(hairlineInset))
	}
	bare, span := alpha(outside, 0), alpha(inside, 0)
	if bare == 0 {
		t.Fatalf("no border ink at x=%d,y=%d, so there is nothing to compare "+
			"against and this assertion is vacuous", outside, 0)
	}
	if span != bare {
		t.Errorf("the top row has alpha %d at x=%d and %d at x=%d — the extra ink is "+
			"a hairline drawn where hairlineY reported there was no room for one, "+
			"which puts it under the border where it thickens the edge instead of "+
			"dividing anything", span, inside, bare, outside)
	}
}

// TestTheHairlineTracksTheCellNotTheCanvas is the discriminating arm, and
// it is the one that would have caught #254 the day it was written.
//
// The old arithmetic read the CANVAS height, so the line moved when the
// pane grew rows — which is precisely what put it outside the top slice.
// Two panes of the same cell size and different heights must place it in
// the same place, because the slice it has to land in is the same size in
// both.
func TestTheHairlineTracksTheCellNotTheCanvas(t *testing.T) {
	const cw, ch = 8, 16
	y, ok := hairlineY(ch)
	if !ok {
		t.Fatal("no hairline for a 16-pixel cell")
	}
	short := y

	// THE HELPER ARM USED TO CALL hairlineY(ch) TWICE and compare the
	// results, which is a pure function against its own argument: it
	// could not fail under any mutation, and the test's headline claim
	// rested on it. hairlineY takes only the cell height, so
	// "independent of the canvas" is not a question its signature can
	// even be asked — what IS worth pinning about the helper is that it
	// is not a constant, since a constant would satisfy the seam arm
	// below at every row count and still be wrong.
	if other, ok := hairlineY(ch * 2); !ok || other == y {
		t.Fatalf("hairlineY(%d) and hairlineY(%d) both report %.2f — the line does "+
			"not track the cell height at all, and the seam arm below would pass "+
			"against a constant", ch, ch*2, y)
	}

	// Through the real seam, because the bug was about what drawCanvas
	// passed it and not about the helper.
	ink := func(rows int) bool {
		dc, err := drawCanvas(40, rows, cw, ch, render.RGB(0xff, 0xff, 0xff))
		if err != nil {
			t.Fatal(err)
		}
		_, _, _, a := dc.Image().At(40*cw/2, int(short)).RGBA()
		return a>>8 > 0
	}
	for _, rows := range []int{4, 6, 12, 24, 40} {
		if !ink(rows) {
			t.Errorf("a %d-row pane has no hairline at y=%.0f, where a 6-row pane of "+
				"the same cell size does — the line is following the canvas again",
				rows, short)
		}
	}
}
