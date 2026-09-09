// Package panel is a titled pane whose frame is LINE ART on the pixel
// plane rather than box-drawing characters on the cell plane.
//
// # Why the frame is sliced into a ring
//
// Placements composite OVER the cell plane, so an image spanning the pane
// would bury the pane's own contents. `components.ButtonChrome` already
// solved this for a pill: generate the shape whole, then slice it into the
// rectangles that are not content. A frame slices naturally into four —
// top edge, bottom edge, and the two side columns — and the interior is
// never covered by a placement at all, so everything inside stays on the
// cell plane where text belongs.
//
// Transparency does the rest of the work, and it only became available
// with the encoder's alpha handling: sixel writes no pixel where alpha is
// low, so the rounded corners and the gaps between strokes leave their
// cells alone instead of stamping black.
//
// # Why the art is drawn, not templated
//
// The frame used to be an SVG file whose width, height and colour were
// substituted into the markup as strings and re-parsed per size. It now
// draws through paint/, which is gooey's bridge to fogleman/gg, for three
// reasons that are all measurable:
//
//   - Cost. Measured on a Xeon E5-2650 v4, one uncached 80x24 pane took
//     20.1 ms through oksvg and takes 1.4 ms through gg; 120x40 went from
//     39.7 ms to 2.8 ms, and the allocation per frame from 2.97 MB to
//     1.21 MB. Roughly 14x, and every bit of it on the UI goroutine. The
//     cache hides this in steady state — a hit is ~1.3 us either way —
//     but it is paid in full on first paint and once per distinct size a
//     resize drags the pane through. String substitution was never the
//     expensive half: it was 67 us of the 20 ms. Re-PARSING was.
//   - Checkability. The geometry is Go constants the compiler sees. In
//     the old path a mistyped placeholder simply failed to substitute and
//     shipped `{{W_1_5}}` into an attribute, which oksvg reads as zero —
//     no error anywhere, just a frame with no border.
//   - Escaping. Values went into markup as unescaped strings. Nothing
//     reaches that path from user input today, which is the only reason
//     it was never wrong.
//
// The geometry is unchanged and still authored in OUTPUT PIXELS: the
// canvas is exactly cols*cellW by rows*cellH, so a 1.5-pixel stroke is
// 1.5 pixels at every pane size. Fixing the art at one size and scaling
// it would make stroke thickness a function of the pane's size — thin in
// a wide pane, fat in a narrow one — which is the tell of a scaled bitmap
// and the thing this whole approach exists to avoid.
//
// The picture is NOT bit-identical to the old one and cannot be — two
// rasterizers antialias differently.
//
// A PIXEL-DIFF AGAINST THE SVG USED TO BE QUOTED HERE and it is gone
// rather than updated. It was measured in #253 against frame.svg, a file
// this tree no longer contains, so nobody can re-run it; and #254 then
// moved the hairline from the canvas's h/8 to the bottom of the top
// CELL, which invalidates the figures about the hairline and the corner
// count with them, since the diff was one measurement. A number nobody
// can reproduce, describing a picture the code no longer draws, reads as
// evidence and is worse than no number.
//
// THESE ARE RE-RUNNABLE FROM THIS TREE, which is what a figure here has
// to be. On a 40x12 pane of 8x16 cells the hairline is 306 pixels wide
// between the side strokes; before #254's colour fix all 306 were below
// sixel's keep-threshold and the encoder's byte stream was IDENTICAL to
// the same canvas with no hairline drawn at all (80 bytes either way).
// It is 107 bytes now. TestTheHairlineReachesTheSixelStream runs exactly
// that comparison through graphics.Sixel.Encode, so the figures above
// are a description of a test rather than a memory of a session.
// Raised in review of #474.
//
// # The cell tier is not a fallback
//
// Without a graphics protocol or a known cell size, the pane draws the
// same shape in box-drawing runes and occupies the SAME cells. That is
// ColorPicker's and ButtonChrome's rule: layout is identical everywhere
// and only the drawing differs, so a pane cannot move when a terminal
// turns out not to speak sixel.
package panel

import (
	"fmt"
	"image"
	"sync"

	"github.com/fogleman/gg"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/graphics"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/paint"
	"github.com/WonderForgeLabs/gooey/render"
)

// The frame's geometry, in output pixels. These were the numbers written
// into frame.svg's attributes; they are constants now, which is the point
// of the rewrite.
const (
	// borderWidth is the rounded rectangle's stroke. The rectangle is
	// inset by half of it so the stroke's outer edge lands ON the pane's
	// boundary rather than half outside it and clipped.
	borderWidth = 1.5
	// cornerRadius is the rounded rectangle's rx/ry, clamped for a pane
	// too small to carry it.
	cornerRadius = 6.0
	// hairlineInset is how far in from each side the title hairline
	// starts, and hairlineFade is what makes it read as a division
	// rather than a second border.
	//
	// A FADE, NOT AN OPACITY, and the difference is the whole of one
	// review finding. It used to be an alpha, and sixel has no alpha
	// channel at all: graphics/sixel.go keeps a pixel only at
	// a >= 0x8000 and writes nothing for the rest, so a 0.4-alpha
	// stroke is 102/255 and every pixel of it was discarded. Measured
	// on a 40x12 pane of 8x16 cells: 306 hairline pixels, 306 dropped,
	// none kept — the flourish stayed invisible under sixel after the
	// y-coordinate fix, which is #254's own symptom on that protocol.
	//
	// Raising the alpha does not help and it is worth knowing why
	// before someone tries it. A kept pixel is painted OPAQUE at its
	// un-premultiplied colour, so any alpha at or above the threshold
	// renders the line at FULL fg — a second border, which is the thing
	// the fade exists to avoid. Sixel can only carry a fainter line as
	// a DIMMER COLOUR, so that is what the line now is.
	hairlineInset = 7.0
	hairlineWidth = 1.0
	hairlineFade  = 0.4
)

// defaultStroke is the frame's colour when the style carries none. It was
// the SVG's currentColor substitution.
var defaultStroke = render.RGB(0x6a, 0x6a, 0x7a)

// Pane is a titled container drawn with pixel line art.
type Pane struct {
	gooey.Base

	Title string
	Child gooey.Component
	// Pad is cells of breathing room INSIDE the frame, on top of the one
	// cell the ring itself occupies. Zero is legal and means the content
	// touches the frame, which is what a dense list wants; 1 is what
	// prose and forms want.
	//
	// It is padding rather than the child's margin because the frame owns
	// it: the whole reason a pane looks cramped is the relation between
	// its border and its content, and that is the pane's business, not
	// each child's.
	Pad int

	art    *Art
	style  render.Style
	attach []gooey.Component
}

// Art rasterizes and caches frames. One per app: the cache is keyed by
// size and colour, and panes of the same size share an entry.
type Art struct {
	mu    sync.Mutex
	cache map[string]*frame
}

// frame is one rasterized frame, already sliced into the ring.
type frame struct {
	top, bottom, left, right image.Image
}

func NewArt() *Art { return &Art{} }

// Builder registers the pane as <Panel Title="..."> with one child.
func Builder(art *Art) markup.Builder {
	return func(e markup.Element, ctx *markup.Context) (gooey.Component, error) {
		kids, attach, err := markup.BuildChildren(e, ctx)
		if err != nil {
			return nil, err
		}
		if len(kids) > 1 {
			return nil, fmt.Errorf("markup: <Panel> takes one child, got %d", len(kids))
		}
		p := &Pane{Title: e.Attrs["Title"], art: art, style: ctx.Styles[e.Attrs["Style"]], attach: attach}
		if len(kids) == 1 {
			p.Child = kids[0]
		}
		return p, nil
	}
}

// Attachments returns the non-visual children — a KeyBinding written
// inside a <Panel> has to reach the framework, and dropping it would be
// silent.
func (p *Pane) Attachments() []gooey.Component { return p.attach }

func (p *Pane) ChildComponents() []gooey.Component {
	if p.Child == nil {
		return nil
	}
	return []gooey.Component{p.Child}
}

// Measure reserves the ring: one cell on every side, exactly as a
// <Border> does. The pixel and cell tiers agree on this, which is what
// makes the two interchangeable without moving anything.
// inset is the ring plus the padding, per side.
func (p *Pane) inset() int { return 1 + max(0, p.Pad) }

func (p *Pane) Measure(avail gooey.Size) gooey.Size {
	if p.Child != nil {
		d := 2 * p.inset()
		gooey.MeasureChild(p.Child, gooey.Size{W: max(0, avail.W-d), H: max(0, avail.H-d)})
	}
	return avail
}

func (p *Pane) Arrange(b gooey.Rect) {
	p.Base.Arrange(b)
	if p.Child == nil {
		return
	}
	in := p.inset()
	// The title sits ON the top edge, so a padded pane gets its first
	// content row below the frame rather than behind the title.
	gooey.ArrangeChild(p.Child, gooey.Rect{
		X: b.X + in, Y: b.Y + in,
		W: max(0, b.W-2*in), H: max(0, b.H-2*in),
	})
}

// Render places the four slices, or draws the cell tier.
//
// The placements are recorded HERE, from Render, so the Composer files
// them under this pane's paint node and diffs them per node: a pane whose
// title changes replaces its own images and a neighbour's repaint sends
// nothing. That is what owning a paint node already means; none of it is
// code here.
func (p *Pane) Render(f *gooey.Frame) {
	b := p.Bounds()
	if b.W < 2 || b.H < 2 {
		return
	}
	cw, ch := f.CellW, f.CellH
	if f.Graphics == nil || cw <= 0 || ch <= 0 {
		p.renderCells(f)
		return
	}
	fr, err := p.art.frame(b.W, b.H, cw, ch, p.style.Fg, p.style.Bg)
	if err != nil {
		// A canvas that cannot be built must not leave a pane with no edges
		// at all; the cell tier is the same shape in runes.
		p.renderCells(f)
		return
	}
	f.Place(graphics.Placement{Img: fr.top, Col: b.X, Row: b.Y, Cols: b.W, Rows: 1})
	f.Place(graphics.Placement{Img: fr.bottom, Col: b.X, Row: b.Y + b.H - 1, Cols: b.W, Rows: 1})
	if b.H > 2 {
		f.Place(graphics.Placement{Img: fr.left, Col: b.X, Row: b.Y + 1, Cols: 1, Rows: b.H - 2})
		f.Place(graphics.Placement{Img: fr.right, Col: b.X + b.W - 1, Row: b.Y + 1, Cols: 1, Rows: b.H - 2})
	}
	p.drawTitle(f)
}

// drawTitle puts the title on the CELL plane, over the top edge's
// placement. Text is what a terminal draws best, and rasterizing a font
// into the frame would trade crisp glyphs for a picture of glyphs.
//
// Both tiers call this: the pixel tier because its title belongs in
// runes even though its frame does not, and the cell tier because it is
// the same title in the same cells. It is the one piece the two share
// unconditionally, which is why components.DrawBoxTitle takes no box.
//
// A title too wide for the pane is now CLIPPED rather than dropped —
// this used to skip the whole label, and matching <Border>, which has
// always clipped, is the point of sharing the helper. Bold is this
// pane's own decision and stays here.
func (p *Pane) drawTitle(f *gooey.Frame) {
	st := p.style
	st.Bold = true
	components.DrawBoxTitle(f.Cells, p.Bounds(), p.Title, st)
}

// renderCells is the universal tier: the same shape, in runes, in the
// same cells — literally components.DrawBoxRunes, the same call
// <Border> and the menu dropdown make, so "the same shape" is a shared
// function rather than a promise in a comment.
func (p *Pane) renderCells(f *gooey.Frame) {
	components.DrawBoxRunes(f.Cells, p.Bounds(), p.style)
	p.drawTitle(f)
}

// frame draws the line art for a pane of cols x rows cells and slices it
// into the ring, caching the result.
//
// The key carries the CELL size as well as the pane's size in cells. The
// pixel dimensions alone are not enough: 40 cells at 8px and 20 cells at
// 16px are the same 320-pixel canvas but slice into different rings, and
// the old key — which was written in pixels — would have handed the first
// pane's slices to the second.
func (a *Art) frame(cols, rows, cellW, cellH int, fg, bg render.Color) (*frame, error) {
	if fg == (render.Color{}) {
		fg = defaultStroke
	}
	// THE GROUND IS PART OF THE KEY, because the hairline is composited
	// against it at draw time now (see over). Two panes with the same
	// foreground on different backgrounds are different pictures, and a
	// key that omitted the ground would hand the first one's slices to
	// the second — the same defect the cell size in this key was added
	// for. Raised in review of #474.
	key := fmt.Sprintf("%dx%d@%dx%d#%02x%02x%02x/%02x%02x%02x%t",
		cols, rows, cellW, cellH, fg.R, fg.G, fg.B, bg.R, bg.G, bg.B, bg.Set)
	a.mu.Lock()
	defer a.mu.Unlock()
	if fr, ok := a.cache[key]; ok {
		return fr, nil
	}
	fr, err := drawFrame(cols, rows, cellW, cellH, fg, bg)
	if err != nil {
		return nil, err
	}
	if a.cache == nil {
		a.cache = map[string]*frame{}
	}
	a.cache[key] = fr
	return fr, nil
}

// drawFrame is the whole of the art: a rounded rectangle and one hairline,
// on a canvas that is transparent everywhere else.
//
// Transparency is load-bearing rather than incidental. The encoder writes
// no pixel where alpha is low, so the rounded corners and the empty
// interior leave their cells alone instead of stamping black — which is
// what lets the pane's own text live inside this frame on the cell plane.
// A gg context starts fully transparent and nothing here fills it, so that
// property holds by construction; a Clear() or a background fill would end
// it.
func drawFrame(cols, rows, cellW, cellH int, fg, bg render.Color) (*frame, error) {
	dc, err := drawCanvas(cols, rows, cellW, cellH, fg, bg)
	if err != nil {
		return nil, err
	}
	top, bottom, left, right := paint.Ring(dc.Image(), cellW, cellH)
	return &frame{top: top, bottom: bottom, left: left, right: right}, nil
}

// drawCanvas is the drawing alone, before the ring is cut. It is separate
// so a test can look at the WHOLE figure: the slices are SubImage views,
// and re-widening one of them back to the canvas silently returns the
// slice, which is exactly the harness bug that made an early A/B of this
// change agree with itself.
func drawCanvas(cols, rows, cellW, cellH int, fg, bg render.Color) (*gg.Context, error) {
	dc, err := paint.Canvas(cols, rows, cellW, cellH)
	if err != nil {
		return nil, fmt.Errorf("panel: %w", err)
	}
	w, h := float64(dc.Width()), float64(dc.Height())

	// The frame proper. Inset by half the stroke; the radius is clamped
	// because gg draws a rounded rectangle whose radius exceeds half its
	// side as overlapping arcs, where SVG's rx is defined to clamp.
	rw, rh := w-borderWidth, h-borderWidth
	if rw > 0 && rh > 0 {
		r := min(cornerRadius, min(rw/2, rh/2))
		dc.DrawRoundedRectangle(borderWidth/2, borderWidth/2, rw, rh, r)
		stroke(fg, borderWidth).Apply(dc)
		dc.Stroke()
	}

	// The hairline inside the top edge — the one flourish, and the detail
	// that reads as "modern" rather than "boxed".
	//
	// Conditional, because a pane whose cells are shorter than the border
	// and the hairline stacked has nowhere to put it. Drawing it anyway
	// would put it under the border, where it is either invisible or a
	// thickening of it — and an invisible flourish is the defect this
	// arithmetic was fixed for.
	//
	// MEASURED, and the measurement is that ignoring `ok` here changes NO
	// PIXEL: the guard only fires for a cell 2 pixels tall or less, and
	// the border's 1.5-pixel stroke already saturates row 0 at every x
	// the hairline would reach, so drawing a dimmed line at y=0 over it
	// changes nothing. An A/B of the two canvases at cellH 1 and 2
	// differs by zero pixels. So this branch is not load-bearing for the
	// output today; it is load-bearing for the CONTRACT, which hairlineY
	// states and TestACellTooShortForBothGetsNoHairline asserts directly,
	// and it stops being a no-op the moment borderWidth or hairlineInset
	// moves.
	// Written down rather than left for the next person to re-derive.
	if y, ok := hairlineY(cellH); ok {
		dc.DrawLine(hairlineInset, y, w-hairlineInset, y)
		s := hairlineStroke(fg, bg)
		s.Apply(dc)
		dc.Stroke()
	}

	return dc, nil
}

// hairlineY is where the title hairline sits, in pixels down from the top
// of the canvas, and whether there is room for it at all.
//
// IT TAKES THE CELL HEIGHT, NOT THE CANVAS HEIGHT, and that is the whole
// of #254. frame.svg was given h/8 where h is the canvas in PIXELS, so on
// an 80x24 pane of 8x16 cells the line landed at y=48 — three cell rows
// down. Ring's top slice is `crop(img, 0, 0, w, cellH)`, exactly one cell
// tall, so the line was cut away and never placed: the flourish the
// package comment describes at length had been invisible for its whole
// life, except for a one-pixel stub at each end where it crossed the side
// slices. PR #253 ported the arithmetic verbatim on purpose — that change
// claimed "same output", and fixing the picture inside it would have made
// the claim unfalsifiable — and split the fix out as #254.
//
// WHICH PICTURE IS THE INTENT was the open question, and the answer is
// the code's own name for it. drawTitle puts the title on the CELL plane
// over the top edge's placement (components.DrawBoxTitle), so the title
// lives in the top cell row — the same row Ring's top slice covers. A
// "title hairline" is the rule under that row, so it sits at the BOTTOM
// of the top cell: inside the top edge, below the title, and a division
// rather than a second border. The package comment's description is the
// intent; the arithmetic was the accident.
//
// The stroke is CENTRED on the returned y, so both halves have to fit:
// the top half clear of the border's stroke, the bottom half inside the
// slice. Returning a bool rather than clamping is deliberate — a clamped
// value would place the line somewhere it does not belong and look like a
// decision, where "no room" is the honest answer for a cell that cannot
// hold both.
func hairlineY(cellH int) (float64, bool) {
	lo := borderWidth + hairlineWidth/2
	hi := float64(cellH) - hairlineWidth/2
	if hi < lo {
		return 0, false
	}
	return hi, true
}

// stroke is the pen shared by both figures. Cap and Join are stated rather
// than left at their zero values: gg's zero LineCap is Round and its zero
// LineJoin is Round, while SVG's defaults — and paint.ParseLineCap's
// default for an omitted attribute — are Flat/Butt and Miter/Bevel. A
// Stroke literal that omits them does not draw what the markup spelling of
// the same stroke draws.
func stroke(fg render.Color, thickness float64) paint.Stroke {
	return paint.Stroke{
		Brush:     gg.NewSolidPattern(paint.Color(fg)),
		Thickness: thickness,
		Cap:       gg.LineCapButt,
		Join:      gg.LineJoinBevel,
		Fallback:  fg,
	}
}

// hairlineStroke is the rule's stroke, extracted so both of its colour
// fields can be asserted together.
//
// THEY HAVE TO AGREE, and they did not. Setting Brush alone left
// Fallback at full-brightness fg — paint.Stroke.Fallback is documented as
// "the single colour this stroke becomes on a terminal with no pixel
// protocol", and Apply never reads it, so it is dormant while this
// package's cell tier goes through DrawBoxRunes. Dormant is exactly why
// it would be wrong the day somebody gives the cell tier a rule: nothing
// would have been drawing it, so nothing would have noticed it was the
// border's colour. Raised in review of #474, and unobservable through
// the canvas — which is why this is a function a test can hold rather
// than three lines inside drawCanvas.
func hairlineStroke(fg, bg render.Color) paint.Stroke {
	rule := over(fg, bg, hairlineFade)
	s := stroke(fg, hairlineWidth)
	s.Brush = gg.NewSolidPattern(paint.Color(rule))
	s.Fallback = rule
	return s
}

// over is the hairline's colour: fg composited onto the pane's own ground
// at the given fraction, OPAQUE.
//
// Opaque because sixel has no alpha channel — graphics/sixel.go writes no
// pixel below half alpha, so the old 0.4-alpha stroke was discarded
// wholesale, which is #254's own symptom on the protocol most terminals
// reach for. A kept pixel is painted at its un-premultiplied colour, so
// the only way to carry a fainter line there is a dimmer COLOUR.
//
// AGAINST THE GROUND, not against black. This function scaled the
// channels and called the result "unchanged over the dark ground this
// palette is drawn for" — true only for a ground that is pure black.
// Measured on the app's own "dim" pane colour (140,140,150): against
// #1e1e2e the alpha stroke composited to (74,74,88) and the scaled one
// gives (56,56,60), roughly half the contrast against the ground, on the
// tiers where the flourish already worked. Compositing here reaches the
// same answer as the terminal did, for every ground rather than one of
// them. An unset Bg means black, which is what the terminal would have
// composited against anyway.
//
// WHAT EACH TIER ACTUALLY DRAWS, since the sentence this replaced was
// wrong about it and measurably so:
//
//   - sixel now writes the rule; before this change it wrote nothing.
//   - kitty/iTerm transmit through png.Encode, which un-premultiplies, so
//     the terminal composited the old stroke itself. Same picture, now
//     computed here.
//   - halfblock is UNCHANGED — it discards alpha and reads the
//     premultiplied channels, so the old stroke and this one differ by
//     zero cells. It drew a ~3% darkening then and draws it now, because
//     a one-pixel rule averaged into a cell's worth of source rows keeps
//     a fraction of its strength.
//   - the rune tier draws no hairline at all.
//
// The cost is stated rather than hidden: an opaque rule COVERS what it
// crosses instead of tinting it, and the top cell row it sits in is the
// row DrawBoxTitle writes the title into. That was already true of the
// border's own 1.5-pixel stroke at the top of the same cell.
func over(fg, bg render.Color, f float64) render.Color {
	if !bg.Set {
		bg = render.RGB(0, 0, 0)
	}
	mix := func(a, b uint8) uint8 {
		return uint8(float64(a)*f + float64(b)*(1-f) + 0.5)
	}
	return render.Color{
		R:   mix(fg.R, bg.R),
		G:   mix(fg.G, bg.G),
		B:   mix(fg.B, bg.B),
		Set: true,
	}
}
