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
// # Why the SVG is authored in output pixels
//
// frame.svg substitutes its own width and height and matches the viewBox
// to them 1:1, so a 1.5-unit stroke is 1.5 pixels at every pane size. A
// fixed viewBox scaled to fit would make stroke thickness a function of
// the pane's size — thin in a wide pane, fat in a narrow one. That is the
// tell of a scaled bitmap, and avoiding it is the whole reason the art is
// vector and rasterized per size rather than drawn once.
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
	"bytes"
	"fmt"
	"image"
	"io/fs"
	"strconv"
	"strings"
	"sync"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/graphics"
	"github.com/WonderForgeLabs/gooey/imagefmt/svg"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/render"
)

// File is the frame's line art, relative to the editor root.
const File = "components/panel/frame.svg"

// Pane is a titled container drawn with pixel line art.
type Pane struct {
	gooey.Base

	Title string
	Child gooey.Component

	art    *Art
	style  render.Style
	attach []gooey.Component
}

// Art rasterizes and caches frames. One per app: the cache is keyed by
// size and colour, and panes of the same size share an entry.
type Art struct {
	fsys fs.FS

	mu    sync.Mutex
	cache map[string]*frame
}

// frame is one rasterized frame, already sliced into the ring.
type frame struct {
	top, bottom, left, right image.Image
}

func NewArt(fsys fs.FS) *Art { return &Art{fsys: fsys} }

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
func (p *Pane) Measure(avail gooey.Size) gooey.Size {
	if p.Child != nil {
		inner := gooey.Size{W: max(0, avail.W-2), H: max(0, avail.H-2)}
		gooey.MeasureChild(p.Child, inner)
	}
	return avail
}

func (p *Pane) Arrange(b gooey.Rect) {
	p.Base.Arrange(b)
	if p.Child == nil {
		return
	}
	gooey.ArrangeChild(p.Child, gooey.Rect{
		X: b.X + 1, Y: b.Y + 1,
		W: max(0, b.W-2), H: max(0, b.H-2),
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
	fr, err := p.art.frame(b.W*cw, b.H*ch, cw, ch, p.style.Fg)
	if err != nil {
		// A missing or broken asset must not leave a pane with no edges at
		// all; the cell tier is the same shape in runes.
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
func (p *Pane) drawTitle(f *gooey.Frame) {
	if p.Title == "" {
		return
	}
	b := p.Bounds()
	label := " " + p.Title + " "
	if len(label) > b.W-4 {
		return
	}
	st := p.style
	st.Bold = true
	f.Cells.SetString(b.X+2, b.Y, label, st)
}

// renderCells is the universal tier: the same shape, in runes, in the
// same cells.
func (p *Pane) renderCells(f *gooey.Frame) {
	b := p.Bounds()
	st := p.style
	f.Cells.Set(b.X, b.Y, '╭', st)
	f.Cells.Set(b.X+b.W-1, b.Y, '╮', st)
	f.Cells.Set(b.X, b.Y+b.H-1, '╰', st)
	f.Cells.Set(b.X+b.W-1, b.Y+b.H-1, '╯', st)
	for x := b.X + 1; x < b.X+b.W-1; x++ {
		f.Cells.Set(x, b.Y, '─', st)
		f.Cells.Set(x, b.Y+b.H-1, '─', st)
	}
	for y := b.Y + 1; y < b.Y+b.H-1; y++ {
		f.Cells.Set(b.X, y, '│', st)
		f.Cells.Set(b.X+b.W-1, y, '│', st)
	}
	p.drawTitle(f)
}

// frame rasterizes the line art at pxW x pxH and slices it into the ring.
func (a *Art) frame(pxW, pxH, cellW, cellH int, fg render.Color) (*frame, error) {
	key := fmt.Sprintf("%dx%d#%02x%02x%02x", pxW, pxH, fg.R, fg.G, fg.B)
	a.mu.Lock()
	defer a.mu.Unlock()
	if fr, ok := a.cache[key]; ok {
		return fr, nil
	}
	src, err := fs.ReadFile(a.fsys, File)
	if err != nil {
		return nil, fmt.Errorf("panel: %s: %w", File, err)
	}
	doc := expand(string(src), pxW, pxH, fg)
	whole, err := svg.RasterizeAt(bytes.NewReader([]byte(doc)), pxW, pxH)
	if err != nil {
		return nil, fmt.Errorf("panel: %s: %w", File, err)
	}
	fr := &frame{
		top:    crop(whole, 0, 0, pxW, cellH),
		bottom: crop(whole, 0, pxH-cellH, pxW, cellH),
		left:   crop(whole, 0, cellH, cellW, pxH-2*cellH),
		right:  crop(whole, pxW-cellW, cellH, cellW, pxH-2*cellH),
	}
	if a.cache == nil {
		a.cache = map[string]*frame{}
	}
	a.cache[key] = fr
	return fr, nil
}

// expand substitutes the geometry and the colour. Plain string
// replacement rather than text/template: the substitutions are four
// numbers and a colour, and the document has to stay readable as an SVG
// that a designer can open.
func expand(doc string, w, h int, fg render.Color) string {
	if fg == (render.Color{}) {
		fg = render.RGB(0x6a, 0x6a, 0x7a)
	}
	// The title hairline sits just below the top edge, at the bottom of
	// the first cell row — which is where a one-row title bar ends.
	titleY := 2
	if h > 8 {
		titleY = h / 8
		if titleY < 2 {
			titleY = 2
		}
	}
	for _, s := range []struct{ k, v string }{
		{"{{W_1_5}}", ftoa(float64(w) - 1.5)},
		{"{{H_1_5}}", ftoa(float64(h) - 1.5)},
		{"{{W_7}}", strconv.Itoa(w - 7)},
		{"{{TITLE_Y}}", strconv.Itoa(titleY)},
		{"{{W}}", strconv.Itoa(w)},
		{"{{H}}", strconv.Itoa(h)},
		{"currentColor", fmt.Sprintf("#%02x%02x%02x", fg.R, fg.G, fg.B)},
	} {
		doc = strings.ReplaceAll(doc, s.k, s.v)
	}
	return doc
}

func ftoa(f float64) string { return strconv.FormatFloat(f, 'f', 2, 64) }

// crop returns a view of the rasterized frame. SubImage shares pixels
// rather than copying, so slicing a frame costs four headers.
func crop(img image.Image, x, y, w, h int) image.Image {
	if w < 1 || h < 1 {
		return image.NewRGBA(image.Rect(0, 0, 1, 1))
	}
	type subImager interface {
		SubImage(image.Rectangle) image.Image
	}
	if si, ok := img.(subImager); ok {
		return si.SubImage(image.Rect(x, y, x+w, y+h))
	}
	return img
}

var _ = components.Str // keep the components import honest for the cell tier's style type
