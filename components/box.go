package components

import (
	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/render"
)

// The rounded box-drawing set. Named so a reader of the loops below can
// see which corner is which without counting glyphs.
const (
	boxTopLeft     = '╭'
	boxTopRight    = '╮'
	boxBottomLeft  = '╰'
	boxBottomRight = '╯'
	boxHorizontal  = '─'
	boxVertical    = '│'
)

// DrawBoxRunes paints a rounded box on the outline of r, in runes, on
// the cell plane. It is pure arithmetic over a render.Buffer: it reads
// and writes no properties, records no dependency, and owns no paint
// node — the caller's Render is still the paint node, and calling this
// from inside one changes nothing about what that node depends on.
//
// It takes a *render.Buffer rather than a *gooey.Frame on purpose. The
// box has nothing to say about the pixel plane, and a component that has
// a buffer but no frame (a cell-tier fallback, a test) can still use it.
//
// A degenerate rect paints NOTHING rather than a small box. With W or H
// at zero the far-edge arithmetic (r.X+r.W-1) walks BACKWARDS and the
// corners land outside the rect — outside the calling node's damage
// rect, so the composer never cleans them and the scar is permanent.
// Zero size happens routinely: a Visible component inside a Collapsed
// ancestor (a hidden Tabs page) is arranged into nothing while staying
// paintable. Painting only inside your own bounds is the damage
// contract, and this is the one place four call sites now keep it.
//
// W or H of 1 or 2 is not degenerate and does paint: the edge loops
// simply run zero times, leaving the corners (which at W==1 collapse
// onto the same column, last write winning). Every cell written is
// inside r.
func DrawBoxRunes(cells *render.Buffer, r gooey.Rect, style render.Style) {
	if cells == nil || r.W <= 0 || r.H <= 0 {
		return
	}
	for x := r.X + 1; x < r.X+r.W-1; x++ {
		cells.Set(x, r.Y, boxHorizontal, style)
		cells.Set(x, r.Y+r.H-1, boxHorizontal, style)
	}
	for y := r.Y + 1; y < r.Y+r.H-1; y++ {
		cells.Set(r.X, y, boxVertical, style)
		cells.Set(r.X+r.W-1, y, boxVertical, style)
	}
	cells.Set(r.X, r.Y, boxTopLeft, style)
	cells.Set(r.X+r.W-1, r.Y, boxTopRight, style)
	cells.Set(r.X, r.Y+r.H-1, boxBottomLeft, style)
	cells.Set(r.X+r.W-1, r.Y+r.H-1, boxBottomRight, style)
}

// DrawBoxTitle writes title into the top edge of a box occupying r,
// padded with one space on each side and clipped to fit.
//
// It is separate from DrawBoxRunes rather than a parameter of it for two
// reasons. A title often wants its own style — the caller passes a bold
// or accented one while the chrome stays plain — and a caller whose box
// is drawn on the PIXEL plane still wants its title in runes
// (apps/wysiwyg's Pane does exactly that), so the title has to be
// callable without the box.
//
// # Clip, do not skip
//
// The label starts at r.X+2 and may not touch the far corner, nor the
// edge cell before it: the box must still read as a box on the right.
// That budget is r.W-6 runes of title. When the budget is zero or less
// there is no room for a title AT ALL and nothing is written — not even
// the two pad spaces, which at r.W of 4 or 5 would land on the far
// corner and rub it out. (Spaces are the worst thing to write out of
// place: they read as "nothing painted" and overwrite just the same.)
//
// This is the reconciliation of two behaviors that had drifted apart:
// core's Border clipped a long title, apps/wysiwyg's Pane dropped it
// entirely. Clipping wins — a truncated title still says which pane you
// are looking at, and a vanished one says nothing while looking like a
// bug — so Pane now clips too.
func DrawBoxTitle(cells *render.Buffer, r gooey.Rect, title string, style render.Style) {
	if cells == nil || title == "" || r.H <= 0 {
		return
	}
	t := clipCols(title, r.W-6)
	if t == "" {
		return
	}
	cells.SetString(r.X+2, r.Y, " "+t+" ", style)
}
