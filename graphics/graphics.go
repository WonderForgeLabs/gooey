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

import (
	"image"

	"golang.org/x/image/draw"
)

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

// Scale returns img resized to w×h pixels, resampled with a triangle
// (bilinear) kernel.
//
// # Why a kernel at all
//
// This was nearest-neighbour, which does not resample so much as
// SUBSAMPLE: it reads one source pixel per destination pixel and throws
// the rest away. Downscaling a terminal screenshot 3x that way discards
// eight ninths of the picture, and the ninth it keeps is chosen by a
// grid that has nothing to do with the content — so a one-pixel rule
// either survives at full strength or vanishes entirely, depending on
// where it fell. Measured against an exact area-average of the same
// image, nearest-neighbour is off by an RMSE of 13–36 (out of 255) on
// real assets; the kernel below is off by 2–4.
//
// # Why the triangle kernel and not a cubic
//
// draw.CatmullRom is the usual reach for "good resampling", and it does
// score marginally better against that reference (about 1 unit of RMSE
// out of 255 — invisible). It is the wrong filter for THIS domain for
// two measured reasons, both consequences of its negative lobes:
//
//   - It RINGS. Scaling a hard two-colour edge whose channels all lie in
//     40..200 produced 2800 destination samples outside that range, the
//     worst overshooting by 12/255. Terminal UI art is nearly all hard
//     edges, and an overshoot at a boundary is a bright or dark seam that
//     is not in the source. A triangle kernel has no negative lobes, so
//     every output sample is a convex combination of its inputs and
//     out-of-range values are not merely rare but arithmetically
//     impossible — the same measurement returns 0 for it, always.
//
//   - It INVENTS COLOURS, which the sixel encoder pays for. Sixel is
//     lossless at or below 256 distinct colours and median-cuts above
//     it, so the colour count is a threshold, not a gradient. On a GIF
//     frame reduced to a halfblock rectangle, this kernel yields 181
//     colours and CatmullRom 258 — the same picture, on opposite sides
//     of the register limit.
//
// draw.ApproxBiLinear is the other candidate and is rejected on
// measurement too. It is a fixed 2x2 tap regardless of the scale factor,
// so on any real downscale it is nearest-neighbour with a smudge: RMSE
// 10.07 against the reference where this kernel scores 2.14, and a
// checkerboard reduced by a non-integer ratio comes back with a moiré
// pattern (luma std-dev 26.8) that the true kernel resolves to the flat
// grey it should be (0.0).
//
// # Upscaling is NOT special-cased, deliberately
//
// The temptation is to keep nearest-neighbour above 1:1 — it invents no
// colours, and there is no lost information for a filter to recover, so
// interpolation buys only smoothness. It was measured and rejected as a
// default: at the ratios a terminal actually asks for (a 16x16 icon into
// a two-cell slot, 1.25x–2.5x) real anti-aliased icons come back with
// 22–69 colours, nowhere near the register limit, while blocky
// nearest-neighbour output next to the anti-aliased source it came from
// reads as a rendering fault.
//
// It does have a cost, and it is worth naming: hard-edged synthetic art
// enlarged a long way is where interpolation is most expensive per unit
// of benefit. A four-colour 16x16 block pattern taken to 300x300 becomes
// 921 colours here against 4 for nearest-neighbour, which pushes sixel
// into median cut.
//
// That crossing was measured rather than feared, by decoding the
// encoder's own output back to pixels: where it happens, the median cut
// is over colours that were themselves interpolated between a handful of
// originals, so its boxes are tight. Worst error across a whole frame is
// 1-2 units of the protocol's 0..100 per channel — under 5 of 255 — and
// the mean is 0.008 to 0.26. So the threshold is crossed and the picture
// is not visibly worse for it. If a caller ever needs a pixel-art
// enlargement to stay bit-exact, this is the function to give an option
// to, and this paragraph is the reason; it is not a reason to branch on
// the ratio by default.
//
// # Cost, which moved in both directions
//
// The old nearest-neighbour reached its source through img.At(), boxing
// a color.Color per destination pixel — 113,202 allocations for a single
// GIF frame. So ENLARGING is now cheaper than it was (a 16x16 icon into
// a two-cell slot: 76µs -> 48µs, and 8 allocations instead of 802),
// because a kernel reads Pix directly and allocates once.
//
// Reducing is dearer, and by a lot, because it is now correct: a
// downscale has to read every source pixel, where subsampling read only
// as many as it wrote. Measured on a Xeon E5-2650 v4:
//
//	713x246   -> 80x48    (screenshot into a pane)      260µs ->  6.3ms
//	400x283   -> 60x42    (gif clip frame, per frame)   171µs ->  4.7ms
//	1920x1080 -> 200x120  (photo, halfblock)           2.02ms -> 67.6ms
//
// The first two are the sizes this framework actually handles and they
// land on a paint node that re-runs only when the image, its cell size
// or its damage changes — not once a frame. The third is a real
// stutter on resize, and it is the reason to keep this in mind rather
// than consider the subject closed.
//
// A two-stage box-then-kernel reduction was prototyped against it and
// NOT taken, on measurement: it wins 4-6x on an *image.RGBA source but
// only 1.9x on *image.NRGBA — which is what image/png decodes to, and
// therefore the input that actually arrives — because the win is spent
// converting the full-size image to a form the box pass can read. It
// also costs accuracy (RMSE against an exact area average went 1.99 to
// 4.02 on the screenshot case). A mitigation that misses the common
// input and loses fidelity is not one; the shape that would work is a
// scaled-result cache at the component, which is a different change.
//
// # Alpha
//
// The resampling happens in ALPHA-PREMULTIPLIED space, which is what
// image.RGBA holds and the only space in which blending a transparent
// pixel with an opaque one is correct — interpolating un-premultiplied
// channels would drag the colour of fully transparent pixels (usually
// black) into the fringe and halo every edge. The consequence for
// consumers is that a kernel manufactures partial alpha where the source
// had none, along every transparency boundary; see the un-premultiply in
// the sixel encoder for what a format with no alpha channel then owes
// those pixels.
func Scale(img image.Image, w, h int) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	// An empty source or target is not an error — halfblock will ask for
	// a zero-column rectangle whenever its component is collapsed. The
	// scaler is undefined on empty rectangles, so answer for it: a
	// correctly sized, fully transparent image.
	if w <= 0 || h <= 0 || img.Bounds().Empty() {
		return dst
	}
	// draw.Src, not draw.Over: dst was just allocated and is transparent
	// black, so there is nothing to composite over, and Src says that
	// rather than paying to discover it.
	draw.BiLinear.Scale(dst, dst.Bounds(), img, img.Bounds(), draw.Src, nil)
	return dst
}
