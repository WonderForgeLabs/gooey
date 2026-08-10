package components

import (
	"image"
	"image/color"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/graphics"
	"github.com/WonderForgeLabs/gooey/render"
)

// Pixel chrome: the first component whose DECORATION, rather than its
// content, is pixel content.
//
// Image places a picture because a picture is what it is for. A button
// has no picture; it has a shape the cell grid cannot draw — a rounded,
// shaded pill. So the chrome is generated in code at the exact pixel size
// the terminal reports for its cells, and placed around the label.
//
// The geometry is the whole trick. Placements composite OVER the cell
// plane, so an image spanning the button would bury its own text. The
// pill is therefore generated whole and then SLICED into the four
// rectangles that are not the label: the top edge row, the bottom edge
// row, and the two end caps of the middle row. The label stays on the
// cell plane in the window between the caps, painted over a background
// matching the pill's interior so the seam does not read as a hole.
//
// Everything else follows the rules already in place. The four
// placements are recorded from Render, so the Composer files them under
// this button's paint node and diffs them per node: a hover that changes
// the shading replaces four images, a neighbour's repaint sends nothing,
// and a button that turns Hidden has its images deleted (kitty) or the
// cells it vacated damaged (sixel/iTerm2). None of that is code here —
// it is what owning a paint node already means.
//
// The tiers are ColorPicker's, not a fallback ladder: every terminal
// gets a three-row pill, and the question is only what draws it. With a
// graphics protocol and a known cell size, pixels. Without either, the
// same shape in box-drawing runes — which is not a degraded pill but the
// universal one, and the reason a pixel button lays out identically
// everywhere.

// ButtonChrome selects how a Button draws itself.
type ButtonChrome uint8

const (
	// ChromeCell is the one-row "[ label ]" button.
	ChromeCell ButtonChrome = iota
	// ChromePixel is the three-row pill: pixel imagery where the
	// terminal has a graphics protocol and a known cell size, box-drawing
	// runes everywhere else.
	ChromePixel
)

// ParseButtonChrome resolves a chrome by name for markup. The bool is
// the load-error signal.
func ParseButtonChrome(s string) (ButtonChrome, bool) {
	switch s {
	case "", "cell":
		return ChromeCell, true
	case "pixel":
		return ChromePixel, true
	}
	return ChromeCell, false
}

// ButtonChromeNames is every chrome ParseButtonChrome accepts.
var ButtonChromeNames = []string{"cell", "pixel"}

// pillRows is the pixel chrome's height: edge, label, edge.
const pillRows = 3

// buttonVisual is the button's paint-relevant state, gathered in one
// place because it is both what the chrome is generated from and the
// cache key that keeps a repaint from regenerating identical pixels.
type buttonVisual struct {
	disabled bool
	focused  bool
	hovered  bool
	pressed  bool
}

// visual reads the state properties in the same order and with the same
// short circuit as the cell renderer: a disabled button asks nothing
// further, so it subscribes to nothing further, and hovering a disabled
// button repaints nothing.
func (b *Button) visual() buttonVisual {
	v := buttonVisual{disabled: b.disabled()}
	if v.disabled {
		v.focused = b.IsFocused()
		return v
	}
	v.hovered = b.IsHovered()
	v.focused = b.IsFocused()
	v.pressed = b.pressed().Get()
	return v
}

// pillKey identifies one generated pill. Images are compared by pointer
// on the wire (graphics.Placement.SameImage), so handing back the SAME
// image for an unchanged state is what makes a repaint free — and
// handing back a different one for a changed state is what makes the
// hover visible.
type pillKey struct {
	cols, rows   int
	cellW, cellH int
	state        buttonVisual
}

// pill is the four placements a pixel button records, in a fixed order
// so the per-node diff lines them up by index across repaints.
type pill struct{ top, bottom, left, right image.Image }

// pillPalette is the shading a state is drawn in: a vertical ramp from
// face to edge, plus the outline.
type pillPalette struct {
	top, bottom, outline color.RGBA
	// interior is the color the label cells are backed with, so the cell
	// plane and the pixel plane meet without a seam.
	interior render.Color
}

func rgba(r, g, b uint8) color.RGBA { return color.RGBA{r, g, b, 255} }

// palette maps state to shading. Raised for rest, brighter for hover,
// inverted (dark at the top) for pressed so the button reads as pushed
// in, gray and flat for disabled — the same vocabulary the cell button
// spells with Dim, Underline, Reverse and Bold.
func palette(v buttonVisual) pillPalette {
	switch {
	case v.disabled:
		return pillPalette{
			top: rgba(78, 78, 86), bottom: rgba(58, 58, 64),
			outline: rgba(96, 96, 104), interior: render.RGB(68, 68, 75),
		}
	case v.pressed:
		return pillPalette{
			top: rgba(58, 44, 120), bottom: rgba(92, 74, 178),
			outline: rgba(190, 170, 255), interior: render.RGB(75, 59, 149),
		}
	case v.hovered:
		return pillPalette{
			top: rgba(150, 122, 255), bottom: rgba(96, 72, 200),
			outline: rgba(214, 200, 255), interior: render.RGB(123, 97, 227),
		}
	default:
		return pillPalette{
			top: rgba(124, 98, 226), bottom: rgba(74, 56, 160),
			outline: rgba(160, 140, 235), interior: render.RGB(99, 77, 193),
		}
	}
}

// drawPill renders the whole button face at pixel resolution: a rounded
// rectangle with a vertical gradient and a one-pixel outline, plus a
// brighter ring when focused. Nothing is loaded from disk — the shape is
// arithmetic, which is what lets it be generated at whatever cell size
// the terminal turns out to have.
func drawPill(w, h int, v buttonVisual) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	p := palette(v)
	radius := min(h/2, w/2)
	if radius < 1 {
		radius = 1
	}
	for y := 0; y < h; y++ {
		// The gradient runs top to bottom across the WHOLE pill, so the
		// four slices taken out of it still line up into one object.
		t := 0.0
		if h > 1 {
			t = float64(y) / float64(h-1)
		}
		face := color.RGBA{
			R: lerp(p.top.R, p.bottom.R, t),
			G: lerp(p.top.G, p.bottom.G, t),
			B: lerp(p.top.B, p.bottom.B, t),
			A: 255,
		}
		for x := 0; x < w; x++ {
			d, inside := roundedEdge(x, y, w, h, radius)
			if !inside {
				continue // transparent: the terminal's own background
			}
			c := face
			if d <= 1 {
				c = p.outline
			} else if v.focused && d <= 2 {
				c = brighten(p.outline)
			}
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

func lerp(a, b uint8, t float64) uint8 {
	return uint8(float64(a) + (float64(b)-float64(a))*t)
}

func brighten(c color.RGBA) color.RGBA {
	return color.RGBA{
		R: uint8(min(255, int(c.R)+60)),
		G: uint8(min(255, int(c.G)+60)),
		B: uint8(min(255, int(c.B)+60)),
		A: 255,
	}
}

// roundedEdge reports how far a pixel is from the pill's edge, and
// whether it is inside at all. Corners are quarter circles of the given
// radius; everywhere else the distance is the plain rectangular one.
func roundedEdge(x, y, w, h, radius int) (int, bool) {
	// Distance to the nearest side, ignoring corners.
	d := min(min(x, w-1-x), min(y, h-1-y))
	// Which corner box, if any, this pixel is in.
	cx, cy := -1, -1
	if x < radius {
		cx = radius
	} else if x >= w-radius {
		cx = w - 1 - radius
	}
	if y < radius {
		cy = radius
	} else if y >= h-radius {
		cy = h - 1 - radius
	}
	if cx < 0 || cy < 0 {
		return d, true
	}
	dx, dy := x-cx, y-cy
	dist := isqrt(dx*dx + dy*dy)
	if dist > radius {
		return 0, false
	}
	return min(d, radius-dist), true
}

// isqrt is an integer square root — enough for a corner test, and it
// keeps the shape generator free of float rounding at the edges.
func isqrt(n int) int {
	r := 0
	for (r+1)*(r+1) <= n {
		r++
	}
	return r
}

// slice copies a rectangle out of the pill into its own zero-origin
// image. A copy rather than image.SubImage because the graphics encoders
// walk an image from 0,0 and because a distinct image value per slot is
// what the placement diff compares.
func slice(src *image.RGBA, x, y, w, h int) image.Image {
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for j := 0; j < h; j++ {
		for i := 0; i < w; i++ {
			dst.SetRGBA(i, j, src.RGBAAt(x+i, y+j))
		}
	}
	return dst
}

// pillFor returns the four slices for this geometry and state, from the
// cache when it has them. The cache is per button and unbounded only in
// the number of states a button can be in, which is four.
func (b *Button) pillFor(k pillKey) pill {
	if b.pills == nil {
		b.pills = map[pillKey]pill{}
	}
	if p, ok := b.pills[k]; ok {
		return p
	}
	w, h := k.cols*k.cellW, k.rows*k.cellH
	whole := drawPill(w, h, k.state)
	mid := k.cellH
	p := pill{
		top:    slice(whole, 0, 0, w, k.cellH),
		bottom: slice(whole, 0, (k.rows-1)*k.cellH, w, k.cellH),
		left:   slice(whole, 0, mid, k.cellW, k.cellH),
		right:  slice(whole, w-k.cellW, mid, k.cellW, k.cellH),
	}
	b.pills[k] = p
	return p
}

// renderPixel draws the three-row pill. It is called only from
// Button.Render, after the state reads that make this button's paint
// node depend on focus, hover, press and CanExecute.
func (b *Button) renderPixel(f *gooey.Frame, v buttonVisual) {
	r := b.Bounds()
	// Below three rows or three columns there is no pill to draw, in
	// either tier: fall back to the flat label rather than a shape with
	// no inside.
	if r.H < pillRows || r.W < 3 {
		b.renderLabel(f, v)
		return
	}
	if f.Graphics == nil || f.CellW <= 0 || f.CellH <= 0 {
		b.renderPillCells(f, v)
		return
	}
	p := b.pillFor(pillKey{
		cols: r.W, rows: pillRows, cellW: f.CellW, cellH: f.CellH, state: v,
	})
	// Order is fixed: the per-node diff pairs placements by index, so a
	// state change must produce the same four slots in the same order or
	// it would read as removals and additions instead of replacements.
	f.Place(graphics.Placement{Img: p.top, Col: r.X, Row: r.Y, Cols: r.W, Rows: 1})
	f.Place(graphics.Placement{Img: p.bottom, Col: r.X, Row: r.Y + 2, Cols: r.W, Rows: 1})
	f.Place(graphics.Placement{Img: p.left, Col: r.X, Row: r.Y + 1, Cols: 1, Rows: 1})
	f.Place(graphics.Placement{Img: p.right, Col: r.X + r.W - 1, Row: r.Y + 1, Cols: 1, Rows: 1})

	b.paintPixelLabel(f, v)
}

// paintPixelLabel writes the label into the window between the end caps,
// backed with the pill's interior color so the cell plane and the pixel
// plane read as one object.
func (b *Button) paintPixelLabel(f *gooey.Frame, v buttonVisual) {
	r := b.Bounds()
	st := getSty(b.Style)
	st.Bg = palette(v).interior
	st.Fg = render.RGB(255, 255, 255)
	if v.disabled {
		st.Dim = true
		st.Fg = render.RGB(190, 190, 196)
	}
	if v.pressed {
		st.Bold = true
	}
	inner := r.W - 2
	label := centerRunes(getStr(b.Content), inner)
	f.Cells.SetString(r.X+1, r.Y+1, label, st)
}

// renderPillCells is the universal tier: the same three-row pill in box
// runes. It is what a terminal without a graphics protocol shows, and
// what a pixel button falls back to when the cell size is unknown — the
// probe having timed out under a recording pty, say.
func (b *Button) renderPillCells(f *gooey.Frame, v buttonVisual) {
	r := b.Bounds()
	st := getSty(b.Style)
	if v.disabled {
		st.Dim = true
	}
	if v.hovered {
		st.Underline = true
	}
	if v.focused {
		st.Reverse = true
	}
	label := st
	if v.pressed {
		label.Bold = true
	}
	inner := r.W - 2
	f.Cells.Set(r.X, r.Y, '╭', st)
	f.Cells.Set(r.X, r.Y+2, '╰', st)
	f.Cells.Set(r.X+r.W-1, r.Y, '╮', st)
	f.Cells.Set(r.X+r.W-1, r.Y+2, '╯', st)
	for i := 0; i < inner; i++ {
		f.Cells.Set(r.X+1+i, r.Y, '─', st)
		f.Cells.Set(r.X+1+i, r.Y+2, '─', st)
	}
	f.Cells.Set(r.X, r.Y+1, '│', st)
	f.Cells.Set(r.X+r.W-1, r.Y+1, '│', st)
	f.Cells.SetString(r.X+1, r.Y+1, centerRunes(getStr(b.Content), inner), label)
}

// centerRunes pads s into exactly w cells, centred, clipping when it
// does not fit.
func centerRunes(s string, w int) string {
	if w <= 0 {
		return ""
	}
	rs := []rune(s)
	if len(rs) >= w {
		return string(rs[:w])
	}
	left := (w - len(rs)) / 2
	out := make([]rune, 0, w)
	for i := 0; i < left; i++ {
		out = append(out, ' ')
	}
	out = append(out, rs...)
	for len(out) < w {
		out = append(out, ' ')
	}
	return string(out)
}
