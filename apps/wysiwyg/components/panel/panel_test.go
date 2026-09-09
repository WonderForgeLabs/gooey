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

	"github.com/fogleman/gg"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/graphics"
	"github.com/WonderForgeLabs/gooey/paint"
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
	first, err := a.frame(20, 6, 8, 16, fg, render.Color{}, true)
	if err != nil {
		t.Fatal(err)
	}
	again, err := a.frame(20, 6, 8, 16, fg, render.Color{}, true)
	if err != nil {
		t.Fatal(err)
	}
	if first != again {
		t.Error("the same size and colour rasterized twice")
	}
	other, err := a.frame(20, 6, 8, 16, render.RGB(0xff, 0, 0), render.Color{}, true)
	if err != nil {
		t.Fatal(err)
	}
	if other == first {
		t.Error("a different colour reused the cached frame")
	}

	// The one that a pixel-sized key would have got wrong: 40 cells of 8px
	// and 20 cells of 16px are the same 320-pixel canvas, and they slice
	// into different rings.
	wide, err := a.frame(40, 6, 8, 16, fg, render.Color{}, true)
	if err != nil {
		t.Fatal(err)
	}
	tall, err := a.frame(20, 6, 16, 16, fg, render.Color{}, true)
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
	fr, err := a.frame(20, 6, 0, 0, render.RGB(1, 2, 3), render.Color{}, true)
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
	dc, err := drawCanvas(cols, rows, cw, ch, render.RGB(0x6c, 0x9c, 0xff), render.Color{}, true)
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
	dc, err := drawCanvas(cols, rows, cw, ch, render.RGB(0xff, 0xff, 0xff), render.Color{}, true)
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
	r, _, _, a := paint.Color(over(white, render.Color{}, hairlineFade)).RGBA()

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
	if fr, _, _, _ := paint.Color(over(white, render.Color{}, 1)).RGBA(); r >= fr {
		t.Errorf("the faded red is %d and the undimmed red is %d — nothing was "+
			"dimmed, so the line is the border's own colour", r>>8, fr>>8)
	}
}

// TestTheRuleIsComposedAgainstTheGroundNotAgainstBlack is finding 1 of
// review #474, asserted with the review's own numbers.
//
// The first fix scaled the CHANNELS by 0.4 and called the result
// "unchanged over the dark ground this palette is drawn for". That is
// true of a ground that is pure BLACK and of no other. kitty and iTerm
// transmit through png.Encode, which un-premultiplies, so the terminal
// composited the old alpha stroke against the real cell background —
// and an opaque scaled colour ignores the ground entirely, losing
// roughly half the rule's contrast on every dark-but-not-black theme,
// on the tier where the flourish already worked.
//
// The app's own pane colour is the fixture, because the regression was
// measured on it: "dim" is (140,140,150) in apps/wysiwyg/main.go.
func TestTheRuleIsComposedAgainstTheGroundNotAgainstBlack(t *testing.T) {
	fg := render.RGB(140, 140, 150)
	for _, tc := range []struct {
		name string
		bg   render.Color
		want render.Color
	}{
		// The historical case, and the one the scaled version got right:
		// over black, compositing IS scaling.
		{"black", render.RGB(0, 0, 0), render.RGB(56, 56, 60)},
		// An unset ground means black — what the terminal would have
		// composited against with no background set.
		{"unset", render.Color{}, render.RGB(56, 56, 60)},
		// The two the review measured. These are what the terminal
		// produced from the ALPHA stroke, which is the picture this
		// change must not lose.
		{"#1e1e2e", render.RGB(0x1e, 0x1e, 0x2e), render.RGB(74, 74, 88)},
		// The review's table gives (72,74,91) here and that row cannot be
		// right: #282c34 is a BRIGHTER ground than #1e1e2e in every
		// channel, so its composite cannot come out darker in two of
		// them. Source-over gives (80,82,91) — 140*0.4 + 40*0.6 = 80.
		// The #1e1e2e row above reproduces the review exactly, so this
		// is one row of the table rather than the method.
		{"#282c34", render.RGB(0x28, 0x2c, 0x34), render.RGB(80, 82, 91)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := over(fg, tc.bg, hairlineFade); got != tc.want {
				t.Errorf("over(%v, %v, %v) = %v, want %v — the rule is not the "+
					"colour the terminal composited the translucent stroke to, so "+
					"this ground loses contrast the flourish used to have",
					fg, tc.bg, hairlineFade, got, tc.want)
			}
		})
	}

	// AND THE GROUND REACHES THE CANVAS, not just the helper. A colour
	// function nothing calls with the pane's background is the same
	// defect one level down.
	dark, err := drawCanvas(40, 12, 8, 16, fg, render.RGB(0x1e, 0x1e, 0x2e), true)
	if err != nil {
		t.Fatal(err)
	}
	black, err := drawCanvas(40, 12, 8, 16, fg, render.RGB(0, 0, 0), true)
	if err != nil {
		t.Fatal(err)
	}
	y, ok := hairlineY(16)
	if !ok {
		t.Fatal("no hairline at a 16-pixel cell")
	}
	x := 40 * 8 / 2
	dr, _, _, _ := dark.Image().At(x, int(y)).RGBA()
	br, _, _, _ := black.Image().At(x, int(y)).RGBA()
	if dr == br {
		t.Errorf("the rule is red %d on both grounds, so drawCanvas is not passing "+
			"the pane's background to over — the compositing above is a helper "+
			"nothing uses", dr>>8)
	}
}

// TestTheGroundReachesTheArtAndItsCacheKey is the other half of finding
// 1, and it is here because the compositing above is a helper until
// something calls it with the pane's own background.
//
// Two silent mutations it exists for, both measured: Render passing
// render.Color{} instead of p.style.Bg, and the cache key dropping the
// ground — which hands the first pane's slices to the second and is the
// same defect the cell size in that key was added for.
func TestTheGroundReachesTheArtAndItsCacheKey(t *testing.T) {
	fg := render.RGB(140, 140, 150)
	dark := render.RGB(0x1e, 0x1e, 0x2e)
	y, ok := hairlineY(16)
	if !ok {
		t.Fatal("no hairline at a 16-pixel cell")
	}
	// The rule's row inside the TOP SLICE, which is the only place it is
	// ever placed.
	row := int(y - hairlineWidth/2)

	// ---- the key. One Art, two grounds, two frames.
	a := NewArt()
	onBlack, err := a.frame(20, 6, 8, 16, fg, render.Color{}, true)
	if err != nil {
		t.Fatal(err)
	}
	onDark, err := a.frame(20, 6, 8, 16, fg, dark, true)
	if err != nil {
		t.Fatal(err)
	}
	x := 20 * 8 / 2
	br, _, _, _ := onBlack.top.At(x, row).RGBA()
	dr, _, _, _ := onDark.top.At(x, row).RGBA()
	if br == 0 {
		t.Fatal("no rule ink in the top slice at all, so comparing two grounds " +
			"asserts nothing")
	}
	if br == dr {
		t.Errorf("the rule is red %d on both grounds. Either the key omits the "+
			"background — in which case the second pane is handed the first one's "+
			"slices — or the ground is not reaching the drawing", br>>8)
	}

	// ---- and the pane passes its own. Its Art holds exactly the frame
	// it asked for, so the cached picture is the answer.
	pane := &Pane{
		Title: "Files",
		Child: &components.Text{Content: components.Str("inside")},
		art:   NewArt(),
		style: render.Style{Fg: fg, Bg: dark},
	}
	pane.LayoutProps().Height = 8
	c := gooey.NewComposer(&components.VStack{Children: []gooey.Component{pane}}, 30, 10)
	t.Cleanup(c.Close)
	c.SetCaps(term8x16(30, 10))
	c.SetGraphics(graphics.Sixel{})
	c.Frame()

	if n := len(pane.art.cache); n != 1 {
		t.Fatalf("the pane's Art holds %d frames after one paint, so there is no "+
			"single cached picture to read the ground out of", n)
	}
	var drawn *frame
	for _, fr := range pane.art.cache {
		drawn = fr
	}
	pr, _, _, _ := drawn.top.At(x, row).RGBA()
	if pr == br {
		t.Errorf("the pane painted its rule at red %d, the same as over BLACK, "+
			"though its style carries Bg %v — Render is not passing the pane's "+
			"own ground to the art", pr>>8, dark)
	}
}

// TestTheHairlineStrokesBothColourFieldsTheSame is finding 2, and it can
// only be asserted structurally.
//
// paint.Stroke.Fallback is "the single colour this stroke becomes on a
// terminal with no pixel protocol". Apply never reads it and this
// package's cell tier goes through DrawBoxRunes, so no canvas and no
// frame can see it — which is exactly why it sat at full-brightness fg
// while Brush carried the dimmed rule, and why nothing would notice
// until somebody gave the cell tier a rule and got the border's colour.
func TestTheHairlineStrokesBothColourFieldsTheSame(t *testing.T) {
	fg := render.RGB(140, 140, 150)
	for _, bg := range []render.Color{{}, render.RGB(0x1e, 0x1e, 0x2e)} {
		s := hairlineStroke(fg, bg, true)
		want := over(fg, bg, hairlineFade)
		if s.Fallback != want {
			t.Errorf("bg %v: the stroke's Fallback is %v and its Brush paints %v — "+
				"a cell tier drawing this rule would use the border's own colour",
				bg, s.Fallback, want)
		}
		br, bgc, bb, _ := s.Brush.ColorAt(0, 0).RGBA()
		wr, wg, wb, _ := paint.Color(want).RGBA()
		if br != wr || bgc != wg || bb != wb {
			t.Errorf("bg %v: the Brush paints (%d,%d,%d) and over() says (%d,%d,%d)",
				bg, br>>8, bgc>>8, bb>>8, wr>>8, wg>>8, wb>>8)
		}
		// NON-VACUITY: the dimmed rule must differ from fg, or both arms
		// pass for a stroke that never dimmed anything.
		if want == fg {
			t.Errorf("bg %v: the rule is the border's own colour, so agreement "+
				"between the two fields says nothing", bg)
		}
	}
}

// sixelKeep is graphics/sixel.go's threshold, restated here because that
// is the number this package has to clear.
//
// A LITERAL, and it is no longer the only thing standing behind the
// claim. The comment here used to say a drifting copy would be caught by
// the test below "failing against a canvas the encoder would in fact
// have kept" — which is not true: nothing in this package observed the
// encoder, both arms compared canvas alpha to this literal, and a
// threshold that rose would have left both green while the line was
// silently dropped again. TestTheHairlineReachesTheSixelStream runs the
// encoder, so this constant is now a convenience for a readable failure
// message rather than the evidence. Raised in review of #474.
const sixelKeep = 0x8000

// TestTheHairlineReachesTheSixelStream is the discriminating assertion:
// through the ENCODER, not against a copy of its threshold.
//
// Before the colour fix the stream for a canvas WITH the hairline was
// byte-identical to one drawn without it — the encoder dropped every
// pixel of the line, so #254's symptom survived #254's fix on the
// protocol most terminals reach for. That is the comparison, and it
// needs no constant from graphics at all.
func TestTheHairlineReachesTheSixelStream(t *testing.T) {
	const cols, rows, cw, ch = 40, 12, 8, 16
	fg := render.RGB(0xff, 0xff, 0xff)

	encode := func(t *testing.T, dc *gg.Context) []byte {
		t.Helper()
		top, _, _, _ := paint.Ring(dc.Image(), cw, ch)
		var buf []byte
		if err := (graphics.Sixel{}).Encode(&buf, top, cols, 1, cw, ch); err != nil {
			t.Fatal(err)
		}
		return buf
	}

	withRule, err := drawCanvas(cols, rows, cw, ch, fg, render.Color{}, true)
	if err != nil {
		t.Fatal(err)
	}
	// THE SAME CANVAS WITHOUT THE RULE, built by asking for a cell too
	// short to hold one — which is hairlineY's own contract and needs no
	// second drawing path that could disagree with the first.
	short := ch
	for {
		if _, ok := hairlineY(short); !ok {
			break
		}
		short--
		if short < 1 {
			t.Fatal("every cell height carries a hairline, so there is no " +
				"no-hairline canvas to compare against")
		}
	}
	without, err := drawCanvas(cols, rows, cw, short, fg, render.Color{}, true)
	if err != nil {
		t.Fatal(err)
	}

	// THE CONTROLLED ARM, at IDENTICAL geometry. The `without` canvas
	// above changes the cell height, so it changes the canvas height too
	// (192 pixels against 24 here) — the corner arcs happen to agree
	// because the radius clamps to 6.0 on both, but that is luck, not
	// construction. Drop `rows` in this test and they diverge, and
	// because the assertion is an INEQUALITY the test would then pass on
	// a geometry difference while the rule was silently gone again. That
	// is the same shape as the sixelKeep problem this test was written to
	// replace. Raised in review of #474.
	//
	// So the discriminating comparison is the two STROKES on one canvas
	// size: translucent against opaque, same cols, same rows, same cell.
	// Nothing differs but the alpha, which is the whole subject.
	alpha, err := drawCanvas(cols, rows, cw, ch, fg, render.Color{}, false)
	if err != nil {
		t.Fatal(err)
	}

	got, bare, faint := encode(t, withRule), encode(t, without), encode(t, alpha)
	if len(got) == len(bare) {
		t.Errorf("the sixel stream is %d bytes with the hairline and %d without: "+
			"the encoder is writing nothing for the rule, which is exactly the "+
			"state this PR found — the line drawn, measured, cached and never "+
			"put on the wire", len(got), len(bare))
	}
	// AND THE TRANSLUCENT STROKE IS STILL DROPPED, byte for byte. This is
	// the positive half: it says the extra bytes above are the RULE and
	// not the geometry, because the only thing changed here is the
	// stroke's alpha and the stream falls back to exactly the no-rule
	// one.
	if !bytes.Equal(faint, bare) {
		t.Errorf("the translucent stroke put %d bytes on the sixel wire against %d "+
			"for a canvas with no rule at all. If sixel has started carrying alpha "+
			"then the opaque tier is no longer needed and graphics.OpaqueEncoder "+
			"is wrong about it; if it has not, this canvas differs from the bare "+
			"one for some reason that is not the hairline, and the byte counts "+
			"below are not measuring what they say", len(faint), len(bare))
	}
	if len(got) <= len(faint) {
		t.Errorf("the opaque rule is %d bytes and the translucent one %d on the "+
			"SAME canvas geometry — the tier split buys nothing", len(got), len(faint))
	}
	t.Logf("sixel bytes: opaque rule %d, translucent rule %d, no rule %d",
		len(got), len(faint), len(bare))
}

// TestTheEncoderDecidesWhichPictureIsDrawn is the tier split itself, and
// it is the finding that #474's first fix spent two working tiers to
// mend a third.
//
// Sixel drops a translucent stroke outright, so it needs the rule as a
// dimmer OPAQUE colour composited against a ground. Kitty and iTerm2
// transmit through png.Encode, which un-premultiplies, so the TERMINAL
// composites the translucent stroke against its own background — a
// colour this process never learns and therefore cannot better. Drawing
// one opaque picture for all three replaced the terminal's answer with a
// guess, and in this repo the guess is always black, because no element
// declares a Background at all.
//
// Raised in review of #474.
func TestTheEncoderDecidesWhichPictureIsDrawn(t *testing.T) {
	fg := render.RGB(140, 140, 150)

	// ---- the stroke. Two colours, and neither is the other.
	faint := hairlineStroke(fg, render.Color{}, false)
	solid := hairlineStroke(fg, render.Color{}, true)
	_, _, _, fa := faint.Brush.ColorAt(0, 0).RGBA()
	_, _, _, sa := solid.Brush.ColorAt(0, 0).RGBA()
	// A LITERAL, not 255*hairlineFade. Derived, this arm moves with the
	// constant it is meant to pin and stays green through any change to
	// it; 102 is what alpha 0.4 premultiplies to, and a change to that
	// number is a change to how faint the rule is, which belongs in a
	// failing test rather than in a constant edit.
	const wantA = 102
	if fa>>8 != wantA {
		t.Errorf("the composited tier's stroke is alpha %d, not %d — a stroke the "+
			"terminal is meant to composite has to arrive translucent",
			fa>>8, wantA)
	}
	if sa>>8 != 255 {
		t.Errorf("the opaque tier's stroke is alpha %d, not 255 — sixel drops "+
			"anything under half and the rule vanishes again", sa>>8)
	}
	// AND THE CHANNELS, which the alpha alone does not pin. gg's pattern
	// painter wants PREMULTIPLIED colour: hand it a straight fg at alpha
	// 102 and it paints a line too bright by 1/0.4, a washed-out rule
	// that is still translucent and still passes the arm above.
	//
	// The two brushes agree on RGB by arithmetic and not by coincidence —
	// premultiplying by 0.4 IS scaling the channels by 0.4, which is what
	// compositing onto black does — so asserting them equal says the
	// composited stroke is premultiplied without restating the formula.
	fr, fgn, fb, _ := faint.Brush.ColorAt(0, 0).RGBA()
	sr, sgn, sb, _ := solid.Brush.ColorAt(0, 0).RGBA()
	if fr != sr || fgn != sgn || fb != sb {
		t.Errorf("the composited stroke paints (%d,%d,%d) and the opaque one "+
			"(%d,%d,%d). Over black they are the same arithmetic, so a difference "+
			"here means the translucent brush is not premultiplied and paints "+
			"1/%.1f too bright",
			fr>>8, fgn>>8, fb>>8, sr>>8, sgn>>8, sb>>8, hairlineFade)
	}
	// AND THE FALLBACK IS THE OPAQUE COLOUR ON BOTH. It is what the
	// stroke becomes where there are no pixels at all, and a terminal
	// with no protocol has nothing to composite against either.
	if want := over(fg, render.Color{}, hairlineFade); faint.Fallback != want {
		t.Errorf("the composited tier's Fallback is %v, want %v: the cell tier has "+
			"no alpha to hand anybody", faint.Fallback, want)
	}

	// ---- and Render asks the ENCODER, not a constant. One pane painted
	// under each protocol; its Art holds exactly the picture it asked
	// for, so the cached frame is the answer.
	paint1 := func(t *testing.T, enc graphics.Encoder) *frame {
		t.Helper()
		pane := &Pane{
			Title: "Files",
			Child: &components.Text{Content: components.Str("inside")},
			art:   NewArt(),
			style: render.Style{Fg: fg},
		}
		pane.LayoutProps().Height = 8
		c := gooey.NewComposer(&components.VStack{Children: []gooey.Component{pane}}, 30, 10)
		t.Cleanup(c.Close)
		c.SetCaps(term8x16(30, 10))
		c.SetGraphics(enc)
		c.Frame()
		if n := len(pane.art.cache); n != 1 {
			t.Fatalf("%s: the pane's Art holds %d frames after one paint, so there "+
				"is no single picture to read", enc.Name(), n)
		}
		for _, fr := range pane.art.cache {
			return fr
		}
		return nil
	}
	y, ok := hairlineY(16)
	if !ok {
		t.Fatal("no hairline at a 16-pixel cell")
	}
	row := int(y - hairlineWidth/2)
	x := 30 * 8 / 2

	onSixel := paint1(t, graphics.Sixel{})
	onKitty := paint1(t, graphics.Kitty{})
	_, _, _, salpha := onSixel.top.At(x, row).RGBA()
	_, _, _, kalpha := onKitty.top.At(x, row).RGBA()
	if salpha>>8 != 255 {
		t.Errorf("under sixel the rule's pixel is alpha %d, not 255 — Render is "+
			"not asking the encoder, and sixel will drop this line", salpha>>8)
	}
	if kalpha>>8 == 255 {
		t.Errorf("under kitty the rule's pixel is opaque. Render is drawing the " +
			"sixel picture on a protocol that carries alpha, which throws away " +
			"the terminal's own compositing for a ground this process guessed")
	}
	if kalpha == 0 {
		t.Error("under kitty the rule's pixel is fully transparent, so there is no " +
			"rule at all and the two tiers agree for the wrong reason")
	}
}

// TestTheUnsetGroundIsOneCacheEntry is finding 2 of the same review: a
// key finer than the picture buys a second 1.4ms raster for identical
// bytes.
//
// over() maps an unset ground to black, so render.Color{} and
// RGB(0,0,0) draw the SAME canvas — and the key carried bg.Set, which
// said they were different pictures. The ground does not reach the
// drawing at all on the composited tier, so two grounds are one picture
// there too.
func TestTheUnsetGroundIsOneCacheEntry(t *testing.T) {
	fg := render.RGB(140, 140, 150)
	dark := render.RGB(0x1e, 0x1e, 0x2e)

	a := NewArt()
	if _, err := a.frame(20, 6, 8, 16, fg, render.Color{}, true); err != nil {
		t.Fatal(err)
	}
	if _, err := a.frame(20, 6, 8, 16, fg, render.RGB(0, 0, 0), true); err != nil {
		t.Fatal(err)
	}
	if n := len(a.cache); n != 1 {
		t.Errorf("an unset ground and an explicit black are %d cache entries. "+
			"over() composites them identically, so this is one picture under two "+
			"keys — a second rasterization and a second entry for the same bytes", n)
	}

	// AND ON THE COMPOSITED TIER THE GROUND IS NOT IN THE PICTURE AT ALL,
	// so keying two grounds apart there is the same waste for a stronger
	// reason: the stroke carries its own alpha and the terminal supplies
	// the ground.
	b := NewArt()
	if _, err := b.frame(20, 6, 8, 16, fg, render.Color{}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := b.frame(20, 6, 8, 16, fg, dark, false); err != nil {
		t.Fatal(err)
	}
	if n := len(b.cache); n != 1 {
		t.Errorf("two grounds are %d cache entries on the tier that composites in "+
			"the terminal, where the ground reaches no pixel of the drawing", n)
	}

	// NON-VACUITY: the key still separates the two pictures it must.
	//
	// WITH NO DECLARED GROUND, which is the case that pins the tier bit
	// and the only one that does. Given a ground, the normalization above
	// already keys the two apart as a side effect — an unset ground
	// becomes black on the opaque tier and is erased to black on the
	// composited one, so the two keys differ in the bg field and the tier
	// bit could be dropped with every arm still green. Measured: without
	// this arm, hardcoding the tier in the key is SILENT. And no element
	// in apps/wysiwyg declares a Background at all, so this is not the
	// exotic case — it is every pane in the app.
	c := NewArt()
	if _, err := c.frame(20, 6, 8, 16, fg, render.Color{}, true); err != nil {
		t.Fatal(err)
	}
	if _, err := c.frame(20, 6, 8, 16, fg, render.Color{}, false); err != nil {
		t.Fatal(err)
	}
	if n := len(c.cache); n != 2 {
		t.Errorf("the opaque and composited strokes share %d cache entry for a pane "+
			"with no declared ground. They are different pictures — one carries "+
			"alpha and one does not — so a pane whose protocol changed would be "+
			"handed the other tier's slices", n)
	}

	// AND WITH ONE, for the same reason stated the other way.
	d := NewArt()
	if _, err := d.frame(20, 6, 8, 16, fg, dark, true); err != nil {
		t.Fatal(err)
	}
	if _, err := d.frame(20, 6, 8, 16, fg, dark, false); err != nil {
		t.Fatal(err)
	}
	if n := len(d.cache); n != 2 {
		t.Errorf("the opaque and composited strokes share %d cache entry on a "+
			"declared ground", n)
	}
}

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
	dc, err := drawCanvas(cols, rows, cw, ch, render.RGB(0xff, 0xff, 0xff), render.Color{}, true)
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
	// on the constant. TestTheHairlineIsDimAndOPAQUE checks what over
	// returns (it was `dim` until the ground was threaded through it, and
	// this sentence went on naming the old one — raised in review of
	// #474); it cannot see drawCanvas passing it the wrong fraction,
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
		f, err := drawFrame(cols, rows, cw, ch, render.RGB(0xff, 0xff, 0xff), render.Color{}, true)
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

		// AND THE CANVAS AGREES, which the two arms above cannot say.
		// They are algebra over constants — `bot - top` IS hairlineWidth
		// by construction and `bot == ch` restates hairlineY's own
		// formula — so neither varies with ch and the loop asserted one
		// thing six times. The rows the rule actually inks are the
		// measurement the test's name promises, and they are what would
		// change if the y ever came off the half-pixel where a 1.0
		// stroke lands on exactly one row: antialiasing would spill it
		// across two. Raised in review of #474.
		const cols, cw = 40, 8
		dc, err := drawCanvas(cols, 6, cw, ch, render.RGB(0xff, 0xff, 0xff), render.Color{}, true)
		if err != nil {
			t.Fatalf("cell height %d: %v", ch, err)
		}
		// Mid-span, so the sample is the rule alone: clear of the corner
		// arcs and of the side strokes at either end.
		x := cols * cw / 2
		inked := 0
		// From clear of the border's stroke to the bottom of the cell.
		// A variable rather than int(borderWidth): the constant is
		// untyped float, and a constant conversion of 1.5 to int does
		// not compile.
		bw := float64(borderWidth)
		for row := int(bw) + 1; row < ch; row++ {
			if _, _, _, a := dc.Image().At(x, row).RGBA(); a != 0 {
				inked++
			}
		}
		if inked != 1 {
			t.Errorf("cell height %d: the rule inks %d pixel rows of the title cell "+
				"at x=%d, want exactly 1 — a stroke spread across two rows is "+
				"antialiasing spill, and every row it takes is a row of glyph it "+
				"hides", ch, inked, x)
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
	dc, err := drawCanvas(cols, 4, cw, ch, render.RGB(0xff, 0xff, 0xff), render.Color{}, true)
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
	// THE CLAMPED RADIUS IS THE HALF THAT CAN MOVE, and the guard was
	// watching the other one. `outside >= hairlineInset` is 5 >= 7, a
	// compile-time constant that could never fire; `outside < 0` covers
	// hairlineInset dropping below 2.0. Neither is what makes this
	// sample a reference.
	//
	// It sits on the rounded rectangle's STRAIGHT top segment only
	// because cornerRadius clamps to 3.25 on this particular canvas
	// (rh = 4*2 - 1.5 = 6.5, so min(6, 3.25)). Raise the row count and
	// the clamp releases to 6.0, the corner arc reaches x≈6.75, and the
	// sample lands on partial or zero coverage — at which point the
	// `bare == 0` fatal fires with a message about missing border ink
	// that would be a lie. Derived from the same expression drawCanvas
	// uses, so the two cannot disagree. Raised in review of #474.
	rw, rh := float64(cols*cw)-borderWidth, float64(4*ch)-borderWidth
	radius := min(cornerRadius, min(rw/2, rh/2))
	if float64(outside) <= radius+borderWidth {
		t.Fatalf("the reference sample at x=%d is inside the corner arc (radius %.2f "+
			"plus the %.2f stroke), not on the straight top segment, so it does not "+
			"carry the same coverage as the sample at x=%d",
			outside, radius, borderWidth, inside)
	}
	// ONLY THE LIVE HALF. This read `outside >= int(hairlineInset) ||
	// outside < 0`, and the first disjunct is structurally x-2 >= x —
	// false for every value of hairlineInset, not merely for 5 >= 7. A
	// guard's text should say what it can do. Raised in review of #474.
	if outside < 0 {
		t.Fatalf("the reference sample is at x=%d, off the canvas: hairlineInset "+
			"(%d) has dropped below the 2 columns this sample steps back by",
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
		dc, err := drawCanvas(40, rows, cw, ch, render.RGB(0xff, 0xff, 0xff), render.Color{}, true)
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
