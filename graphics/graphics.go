// Package graphics answers the "N rendering modes" question.
//
// The finding: there is ONE cell renderer (the render package) and N
// *graphics protocols* for pixel content. Text, borders, styling — the
// entire component tree — always renders to the cell plane. Only pixel
// content (Image, future Canvas) needs a protocol, chosen at startup by
// capability detection:
//
//	kitty     — Kitty graphics protocol (kitty, Ghostty, WezTerm)
//	sixel     — DEC sixel (xterm, foot, Windows Terminal ≥1.22, VTE ≥0.76, mlterm, Konsole)
//	iterm2    — OSC 1337 inline images (iTerm2, WezTerm, mintty)
//	halfblock — universal fallback: image rendered INTO the cell plane
//	            as ▀ runes with 24-bit fg/bg (2 pixels per cell)
//
// The first three are Encoders: they emit escape bytes and the pixels
// live on a plane the terminal composites over the cells. Halfblock is
// not an Encoder — it degrades pixel content into cells, which is why
// Frame treats "no encoder" as "draw into the buffer instead".
package graphics

import "image"

// Encoder emits a pixel image at the current cursor position, sized to
// cols×rows terminal cells (cellW×cellH is the cell size in pixels).
type Encoder interface {
	Name() string
	Encode(out *[]byte, img image.Image, cols, rows, cellW, cellH int) error
}

// Placement is a deferred image draw: the component tree records placements
// during the render pass; the frame flush emits them after the cell
// plane, so pixel content composites over the already-painted cells.
type Placement struct {
	Img        image.Image
	Col, Row   int // top-left, in cells
	Cols, Rows int // size, in cells
}

// Scale returns img resized to w×h pixels, nearest-neighbor.
func Scale(img image.Image, w, h int) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	sb := img.Bounds()
	for y := 0; y < h; y++ {
		sy := sb.Min.Y + y*sb.Dy()/h
		for x := 0; x < w; x++ {
			sx := sb.Min.X + x*sb.Dx()/w
			dst.Set(x, y, img.At(sx, sy))
		}
	}
	return dst
}
