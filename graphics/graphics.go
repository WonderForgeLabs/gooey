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

// IDEncoder is an Encoder whose images have IDENTITY: one transmitted
// image can later be re-placed, replaced, or removed by referring to it,
// without the pixels going down the wire again. Only the Kitty protocol
// has this; sixel and iTerm2 write pixels into the cell grid and then
// forget them, which is the difference the incremental flush is built
// around.
//
// A host that has one can move an image for the price of a control
// sequence and delete one outright. A host that does not must repaint the
// CELLS under a vanished image to erase it, and must re-send an image
// whose cells were repainted for any other reason.
//
// It is a second interface rather than more methods on Encoder so that
// "can this protocol address a placement?" stays a type assertion — the
// same no-reflection shape as every other capability question here.
type IDEncoder interface {
	Encoder
	// Transmit sends the image under id and displays it at the cursor.
	// Re-transmitting a live id replaces its pixels.
	Transmit(out *[]byte, id int, img image.Image, cols, rows, cellW, cellH int) error
	// Place displays an already-transmitted image at the cursor again,
	// sending no pixels. This is what makes a moved image cheap.
	Place(out *[]byte, id, cols, rows int)
	// Delete removes id's placements from the screen. With data, the
	// stored pixels are freed too — right when the image itself is going
	// away, wrong when it is only moving.
	Delete(out *[]byte, id int, data bool)
}

// Placement is a deferred image draw: the component tree records placements
// during the render pass; the frame flush emits them after the cell
// plane, so pixel content composites over the already-painted cells.
type Placement struct {
	Img        image.Image
	Col, Row   int // top-left, in cells
	Cols, Rows int // size, in cells
}

// SameSpot reports whether two placements occupy the same cells at the
// same size — everything about a placement except which image it shows.
func (p Placement) SameSpot(q Placement) bool {
	return p.Col == q.Col && p.Row == q.Row && p.Cols == q.Cols && p.Rows == q.Rows
}

// SameImage reports whether two placements show the same image value.
//
// Identity is interface equality: images are pointers in every practical
// case (image.NewRGBA, the stdlib decoders, graphics.Scale), so this is a
// pointer comparison. A non-comparable image would make == panic, and the
// recover turns that into "cannot tell" — which costs a retransmission
// and nothing else, since every caller uses the answer only to skip work.
func (p Placement) SameImage(q Placement) (same bool) {
	defer func() { recover() }() //nolint:errcheck // a panic here means "not comparable"
	return p.Img == q.Img
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
