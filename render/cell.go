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
}

func NewBuffer(w, h int) *Buffer {
	b := &Buffer{W: w, H: h, Cells: make([]Cell, w*h)}
	b.Clear()
	return b
}

func (b *Buffer) Clear() {
	for i := range b.Cells {
		b.Cells[i] = Cell{Rune: ' '}
	}
}

func (b *Buffer) Set(x, y int, r rune, s Style) {
	if x < 0 || y < 0 || x >= b.W || y >= b.H {
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
