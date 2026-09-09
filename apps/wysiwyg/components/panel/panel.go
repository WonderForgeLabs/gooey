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
// EVERY FIGURE HERE IS RE-RUNNABLE FROM THIS TREE, which is the bar a
// number in a comment has to clear: a measurement nobody can reproduce,
// describing a picture the code no longer draws, reads as evidence and
// is worse than no number. (The pixel-diff against frame.svg that used
// to stand here failed that bar twice over — the file is gone and the
// hairline has moved since.)
//
// On a 40x12 pane of 8x16 cells the hairline is 306 pixels wide
// between the side strokes; a translucent stroke puts all 306 below
// sixel's keep-threshold and the encoder's byte stream is IDENTICAL to
// the same canvas with no hairline drawn at all — 80 bytes either way,
// against 107 for the opaque one. TestTheHairlineReachesTheSixelStream
// runs exactly that comparison through graphics.Sixel.Encode, at one
// canvas geometry with only the stroke's alpha changed, so the figures
// above are a description of a test rather than a memory of a session.
//
// # Two strokes, chosen by the encoder
//
// The rule is the one place the tiers draw different PICTURES rather
// than the same picture through different wires, and the reason is the
// paragraph above: sixel discards a translucent pixel instead of dimming
// it. So the pane asks graphics.OpaqueEncoder and strokes accordingly —
// a dimmer opaque colour where alpha cannot travel, the translucent
// stroke where the terminal will composite it.
//
// The first fix for #254 made every tier opaque, and that was the wrong
// trade in both directions. Kitty and iTerm2 transmit through
// png.Encode, which un-premultiplies, so the terminal composites the
// rule against ITS OWN background — an answer no arithmetic here can
// improve on, because Pane has no BackgroundProperty and the only ground
// this package can name is black.
//
// THE APP'S OWN CHROME IS WHERE THAT IS TRUE, and this paragraph used to
// state it flat: "no ANCESTOR of a Panel in apps/wysiwyg declares a
// Background". `over` retracts that fifteen hundred lines down, for the
// case that matters — main.go registers "Panel" on docCtx, so a DOCUMENT
// may put a Panel inside a Background-bearing container, and the
// designer renders one today. Two paragraphs in one file disagreeing
// about the same fact is worse than either being wrong alone, so this
// one is now the narrow claim and `over` carries the general one.
// Raised in review of #474.
//
// Two tiers that were already right were spent to mend the one that was
// not. Learning the terminal's own background — an OSC 11 query — is
// filed on #259.
//
// The cost of the split is one more bit in the Art cache key, which is
// where every "same shape, different picture" question in this package
// ends up.
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
	"image/color"
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
	// a DIMMER COLOUR.
	//
	// SO THE CONSTANT HAS TWO READINGS, one per tier, and the sentence
	// that stood here gave it one ("so that is what the line now is").
	// On sixel it is a MIX FRACTION handed to over(); on kitty and
	// iTerm2 it is an ALPHA handed to fade(), which is what it was
	// before #254 and is again. The reader most likely to meet this
	// comment is the one changing 0.4, and both readings move together
	// when they do. Raised in review of #474.
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
	// THE ENCODER DECIDES WHICH PICTURE THIS IS. Sixel carries no alpha
	// and drops a translucent stroke outright, so it needs the rule as a
	// dimmer OPAQUE colour; kitty and iTerm2 transmit through png.Encode,
	// which un-premultiplies, so the terminal composites the translucent
	// stroke against its OWN background — a colour this process never
	// learns and therefore cannot better. Drawing one opaque picture for
	// all three spent the two tiers that already worked to fix the one
	// that did not. See graphics.OpaqueEncoder.
	_, opaque := f.Graphics.(graphics.OpaqueEncoder)
	fr, err := p.art.frame(b.W, b.H, cw, ch, p.style.Fg, p.style.Bg, opaque)
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
func (a *Art) frame(cols, rows, cellW, cellH int, fg, bg render.Color, opaque bool) (*frame, error) {
	if fg == (render.Color{}) {
		fg = defaultStroke
	}
	// THE GROUND IS NORMALIZED BEFORE IT IS KEYED, both ways, because a
	// key finer than the picture buys a second 1.4ms raster and a second
	// cache entry for the identical bytes.
	//
	// It reaches the PICTURE only on the opaque tier, so everywhere else
	// it is normalized out of the KEY: two panes on different backgrounds
	// are the same canvas when the stroke carries its own alpha. And
	// over() maps an unset ground to black, so render.Color{} and
	// RGB(0,0,0) were two keys for one canvas — the `%t` on bg.Set said
	// they were different pictures and they never were. fg has had the
	// same treatment three lines up since before this.
	//
	// THE KEY IS NORMALIZED; THE GROUND PASSED DOWN IS NOT. Erasing bg
	// before drawCanvas also erased it from hairlineStroke's Fallback,
	// which is the colour the stroke becomes where there are no pixels —
	// so a pane that declared a Bg got a black-composited Fallback on the
	// composited tier. The picture is unaffected, the value being
	// computed and discarded, which is exactly the argument
	// hairlineStroke's own doc rejects for the opaque branch: dormant is
	// why it would be wrong the day somebody gives the cell tier a rule.
	keyBg := bg
	if !opaque {
		keyBg = render.Color{}
	}
	if !keyBg.Set {
		keyBg = render.RGB(0, 0, 0)
	}
	key := fmt.Sprintf("%dx%d@%dx%d#%02x%02x%02x/%02x%02x%02x/%t",
		cols, rows, cellW, cellH, fg.R, fg.G, fg.B,
		keyBg.R, keyBg.G, keyBg.B, opaque)
	a.mu.Lock()
	defer a.mu.Unlock()
	if fr, ok := a.cache[key]; ok {
		return fr, nil
	}
	fr, err := drawFrame(cols, rows, cellW, cellH, fg, bg, opaque)
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
func drawFrame(cols, rows, cellW, cellH int, fg, bg render.Color, opaque bool) (*frame, error) {
	dc, err := drawCanvas(cols, rows, cellW, cellH, fg, bg, opaque)
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
func drawCanvas(cols, rows, cellW, cellH int, fg, bg render.Color, opaque bool) (*gg.Context, error) {
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
	// BOTH GUARDS DECIDE THE PICTURE, on their own axis, and neither is
	// a formality.
	//
	// tall: the rule sits at the bottom of the top CELL, so a cell two
	// pixels tall or less has no room under the border for it. On sixel
	// the rule is opaque and COVERS what it crosses rather than tinting
	// it — drawn without this guard, the top row's red goes 255 → 102
	// across the hairline's span.
	//
	// wide: the line runs inset-to-inset, so a narrow canvas gives
	// DrawLine an x1 near or left of its x0 — a dot or a reversed
	// segment, which gg strokes as a short dash floating in a pane that
	// was supposed to have a rule under its title. hairlineSpan carries
	// the threshold and why it is where it is.
	y, tall := hairlineY(cellH)
	x0, x1, wide := hairlineSpan(w)
	if tall && wide {
		dc.DrawLine(x0, y, x1, y)
		s := hairlineStroke(fg, bg, opaque)
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

// hairlineSpan is how far the rule reaches across the canvas, and
// whether it reaches at all — hairlineY's counterpart on the other axis,
// and written to the same shape on purpose.
//
// The rule is inset from both sides, so a canvas narrower than the two
// insets together has x1 LEFT OF x0. gg does not refuse that: it strokes
// the segment between them, which paints a short dash centred in a pane
// whose title has no rule under it — a mark that looks like a rendering
// fault rather than an absent flourish.
//
// A LEGIBILITY FLOOR, NOT A DEGENERACY ONE, and the first version was
// the latter. `x1 > x0` refuses only the reversed span, which draws the
// SAME dash one pixel the other side of the threshold: review of #474
// measured a 15-pixel canvas drawing a 1-pixel rule and a 16-pixel one
// (2 columns at cellW 8, the size every fixture in this package uses)
// drawing 2 — a dot centred in the top cell row, under a title that
// cannot be drawn at all, since DrawBoxTitle starts two columns in. The
// guard refused the picture on one side of its boundary and drew it on
// the other, which is a boundary in the wrong place rather than a rule.
//
// The floor is hairlineInset itself, and it is derived rather than
// chosen: a mark shorter than the gap holding it off each edge reads as
// a dot between two spaces, not as a line across a pane. It needs no new
// constant, and it moves with the inset if the inset ever moves.
//
// Returning a bool rather than clamping, for hairlineY's reason: a
// clamped span would place a line somewhere it does not belong and look
// like a decision, where "no room" is the honest answer.
func hairlineSpan(w float64) (x0, x1 float64, ok bool) {
	x0, x1 = hairlineInset, w-hairlineInset
	return x0, x1, x1-x0 >= hairlineInset
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
// border's colour. It is unobservable through the canvas, which is why
// this is a function a test can hold rather than three lines inside
// drawCanvas.
func hairlineStroke(fg, bg render.Color, opaque bool) paint.Stroke {
	s := stroke(fg, hairlineWidth)
	if !opaque {
		// TRANSLUCENT, where the terminal composites. gg's pattern
		// painter wants ALPHA-PREMULTIPLIED channels, which is why the
		// RGB is scaled here as well as A being set: handing it a
		// straight colour at low alpha paints a line too bright by
		// 1/alpha.
		//
		// Fallback is the OPAQUE colour, not this one. It is the single
		// colour the stroke becomes where there are no pixels at all, and
		// a terminal with no protocol has nothing to composite against
		// either.
		s.Brush = gg.NewSolidPattern(fade(fg, hairlineFade))
		s.Fallback = over(fg, bg, hairlineFade)
		return s
	}
	rule := over(fg, bg, hairlineFade)
	s.Brush = gg.NewSolidPattern(paint.Color(rule))
	s.Fallback = rule
	return s
}

// over is the hairline's colour ON THE OPAQUE TIER ONLY: fg composited
// onto a ground at the given fraction, with no alpha left in it.
//
// It exists because sixel has no alpha channel — sixel.go writes no pixel
// below half alpha, so the 0.4-alpha stroke was discarded wholesale,
// which is #254's own symptom on the protocol most terminals reach for. A
// kept pixel is painted at its un-premultiplied colour, so the only way
// to carry a fainter line there is a dimmer COLOUR.
//
// THE GROUND HERE IS A GUESS, in every case, and black is the guess.
//
// Pane is not a gooey.HasBackground, so nothing fills its bounds: on the
// pixel tier the only cells in the top row that ever receive p.style.Bg
// are the ones DrawBoxTitle writes (`cells.SetString(r.X+2, r.Y,
// " "+t+" ", style)`, components/box.go). Mid-span — where the rule
// lives, and where every sample in this package's tests reads — what is
// behind it is Composer.clearStyle's answer, the nearest ANCESTOR with a
// background, or the terminal's own default when there is none.
//
// IN A DOCUMENT THE GUESS IS PROVABLY WRONG, not merely unpinned.
// main.go registers "Panel" on docCtx deliberately ("a document is
// entitled to a framed region"), and Background is authorable on Border,
// Canvas, Grid, HStack and VStack — so
// `<VStack Background="#282c34"><Panel/></VStack>` is a document the
// designer renders today.
//
// THAT LIST IS DERIVED, not remembered: it read "VStack, Grid and
// Canvas" for a review round while Border and HStack had carried the
// attribute all along, which is a hand-written list doing what a
// hand-written list does. TestTheBackgroundElementsAreTheOnesTheRegistrySays
// reads markup.BuiltinElements() and fails if this sentence and the
// registry ever name different sets. Raised in review of #474. The framework fills the
// pane's cells with #282c34 and this function composites against black
// anyway: (56,56,60) laid over (40,44,52). That is #254's own contrast
// complaint, in the tree the app exists to render. Measured in review of
// #474.
//
// THERE IS NO SEAM TO FIX IT WITH TODAY, and that is the fact worth
// carrying rather than the apology. Composer.clearStyle is unexported
// and takes a *paintNode; and a component cannot read the answer back
// off the frame either, because Pane has ChildComponents — a chrome-only
// container pre-clears NOTHING, so the cells under it at Render time
// hold whatever was there rather than its ancestor's ground. Closing
// this needs a framework accessor for "the ground my bounds will clear
// to", which is a core change and not a panel one. Learning the
// TERMINAL's background — an OSC 11 query, the other half of the same
// question — is filed on #259.
//
// What keeps the guess from being a regression is that this colour now
// reaches only the tier that forces one: kitty and iTerm2 composite in
// the terminal and never call this.
//
// WHAT EACH TIER DRAWS:
//
//   - sixel takes this colour, opaque, and writes the rule where it used
//     to write nothing.
//   - kitty/iTerm2 take fade() instead and are UNCHANGED from before
//     #254: they transmit through png.Encode, which un-premultiplies, so
//     the terminal composites the translucent stroke against its own
//     background. That is a better answer than anything computable here,
//     and drawing one opaque picture for all three threw it away.
//   - there is NO halfblock tier here. Halfblock IS the nil encoder,
//     and Pane.Render returns to renderCells the moment f.Graphics is
//     nil — before any placement — so graphics.DrawHalfblock is never
//     reached from this package.
//   - the rune tier, which is where a terminal with no protocol actually
//     lands, draws no hairline at all.
//
// The cost is stated rather than hidden: an opaque rule COVERS what it
// crosses instead of tinting it, and the top cell row it sits in is the
// row DrawBoxTitle writes the title into. That was already true of the
// border's own 1.5-pixel stroke at the top of the same cell, and it is
// now true on sixel only.
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

// fade returns a colour at the given opacity, ALPHA-PREMULTIPLIED, which
// is what color.RGBA means and what gg's pattern painter composites with.
// Handing it a straight colour at a low alpha paints a washed-out line
// too bright by 1/opacity.
//
// This is the stroke for a protocol that can carry alpha, and it is what
// the pane drew before #254's sixel fix made every tier opaque. It came
// back when that fix turned out to have spent kitty and iTerm2 — where
// the TERMINAL composites, against its own background — to buy sixel a
// line it was discarding. See graphics.OpaqueEncoder and over above.
//
// A doc comment separated from what it documents is invisible to gofmt
// and to vet, and `go doc -all -u` is what shows it; the guard is #470's
// TestNoDocCommentNamesTheDeclarationBelowIt, scoped to markup/ today
// and widened to the tree in #483.
func fade(c render.Color, a float64) color.Color {
	return color.RGBA{
		R: uint8(float64(c.R)*a + 0.5),
		G: uint8(float64(c.G)*a + 0.5),
		B: uint8(float64(c.B)*a + 0.5),
		A: uint8(255*a + 0.5),
	}
}
