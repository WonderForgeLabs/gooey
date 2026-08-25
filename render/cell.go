// Package render provides the cell plane: a buffer of styled character
// cells and the ANSI diff/flush path that puts them on screen.
package render

// Color is a 24-bit RGB color. Zero value means "terminal default".
type Color struct {
	R, G, B uint8
	Set     bool
}

func RGB(r, g, b uint8) Color { return Color{r, g, b, true} }

type Style struct {
	Fg, Bg    Color
	Bold      bool
	Dim       bool
	Underline bool
	Reverse   bool
}

type Cell struct {
	Rune  rune
	Style Style
}

// Buffer is a W×H grid of cells — one frame of the cell plane.
type Buffer struct {
	W, H  int
	Cells []Cell

	// clip is the half-open region Set may write, ALWAYS inside the
	// buffer — Clip intersects, so a clip can only ever narrow. That
	// invariant is what lets Set test the clip INSTEAD of the buffer
	// rather than as well as it, which is why clipping costs nothing:
	// the same four comparisons that already bounded every write now
	// bound it to the painting component instead of to the screen.
	//
	// Use NewBuffer. A zero-value Buffer has an empty clip and silently
	// discards every write — which is why remember() below was changed
	// to stop building one by hand.
	cx0, cy0, cx1, cy1 int
}

func NewBuffer(w, h int) *Buffer {
	b := &Buffer{W: w, H: h, Cells: make([]Cell, w*h), cx1: w, cy1: h}
	b.Clear()
	return b
}

// Clip narrows the writable region to r and returns what it was, so the
// caller can put it back. The Composer brackets every component's Render
// with a pair of these.
//
// It INTERSECTS with the current clip rather than replacing it, so
// nesting can only ever narrow — a component cannot widen its way out to
// a neighbour's cells by clipping to something larger than it was given.
func (b *Buffer) Clip(r Rect) Rect {
	prev := b.ClipRect()
	b.cx0, b.cy0 = max(b.cx0, r.X), max(b.cy0, r.Y)
	b.cx1, b.cy1 = min(b.cx1, r.X+r.W), min(b.cy1, r.Y+r.H)
	// An empty intersection must stay empty rather than invert, or the
	// comparisons in Set would admit everything.
	b.cx1, b.cy1 = max(b.cx0, b.cx1), max(b.cy0, b.cy1)
	return prev
}

// Unclip restores a region taken from Clip. Separate from Clip because
// restoring must NOT intersect: the whole point is to widen back out.
func (b *Buffer) Unclip(r Rect) {
	b.cx0, b.cy0, b.cx1, b.cy1 = r.X, r.Y, r.X+r.W, r.Y+r.H
}

// ClipRect is the region Set will currently accept.
func (b *Buffer) ClipRect() Rect {
	return Rect{X: b.cx0, Y: b.cy0, W: b.cx1 - b.cx0, H: b.cy1 - b.cy0}
}

func (b *Buffer) Clear() {
	for i := range b.Cells {
		b.Cells[i] = Cell{Rune: ' '}
	}
}

// Set writes one cell, and DROPS the write when it falls outside the
// current clip.
//
// Dropping rather than growing the buffer is the whole fix for #357. The
// cells beyond a component's rect belong to its neighbours, and those
// neighbours are CLEAN — their paint nodes did not invalidate — so
// nothing ever repaints over a stray write. It survives until something
// unrelated dirties the victim, which is why the symptom shows up as
// "stray characters in a pane that never fixes itself".
//
// The clip is kept inside the buffer by Clip, so testing it also tests
// the buffer: four comparisons, exactly as many as bounding to the
// screen alone used to cost.
func (b *Buffer) Set(x, y int, r rune, s Style) {
	if x < b.cx0 || y < b.cy0 || x >= b.cx1 || y >= b.cy1 {
		return
	}
	b.Cells[y*b.W+x] = Cell{Rune: r, Style: s}
}

func (b *Buffer) At(x, y int) Cell {
	if x < 0 || y < 0 || x >= b.W || y >= b.H {
		return Cell{Rune: ' '}
	}
	return b.Cells[y*b.W+x]
}

// SetString writes a string starting at (x,y), clipped to the buffer.
func (b *Buffer) SetString(x, y int, str string, s Style) {
	for _, r := range str {
		b.Set(x, y, r, s)
		x++
	}
}
