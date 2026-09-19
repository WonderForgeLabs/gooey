package gooey

import (
	"image"
	"testing"

	"github.com/WonderForgeLabs/gooey/graphics"
	"github.com/WonderForgeLabs/gooey/render"
)

// #357: a component must not be able to write another component's cells.
//
// This is worse than a rendering artifact, and the reason is damage
// tracking. A stray write lands in cells whose owner is CLEAN — its paint
// node did not invalidate — so nothing ever repaints over the corruption.
// It survives until something unrelated dirties the victim, which is why
// it is seen from the far end as "stray characters in a pane that never
// fixes itself".

// overflowing paints a run of r across its own bounds and `under` cells
// BEFORE them, which is what a container showing a window onto oversized
// content does without virtualizing.
//
// Overflowing BACKWARDS is deliberate and the test does not work without
// it. Document order is paint order WITHIN A LAYER — these are ordinary
// siblings, neither is a gooey.Overlay, so the ordinary rule is the one
// that applies (#437 lifted overlays into a second layer; #439 ranks it).
// So a component that overflows
// FORWARD is painted over by the neighbour it corrupted, and the
// neighbour's cells end up correct whether or not anything clipped — the
// first version of this test asserted exactly that and passed with the
// clip removed. The victim has to paint FIRST and be overwritten, which
// is also the real-world shape: the stray cells belong to a component
// that already painted and is now clean, so nothing repaints them.
type overflowing struct {
	Base
	r     rune
	under int
}

func (o *overflowing) Measure(Size) Size { return Size{W: 1, H: 1} }
func (o *overflowing) Render(f *Frame) {
	b := o.Bounds()
	for x := b.X - o.under; x < b.X+b.W; x++ {
		f.Cells.Set(x, b.Y, o.r, render.Style{})
	}
}

// twoUp puts a and b side by side at a fixed split, so "outside my
// bounds" and "inside my neighbour's" are the same cells.
type twoUp struct {
	Base
	a, b  Component
	split int
}

func (t *twoUp) ChildComponents() []Component { return []Component{t.a, t.b} }
func (t *twoUp) Measure(avail Size) Size      { return avail }
func (t *twoUp) Arrange(r Rect) {
	t.Base.Arrange(r)
	t.a.Arrange(Rect{X: r.X, Y: r.Y, W: t.split, H: r.H})
	t.b.Arrange(Rect{X: r.X + t.split, Y: r.Y, W: r.W - t.split, H: r.H})
}
func (t *twoUp) Render(*Frame) {}

func TestAComponentCannotPaintOverItsNeighboursCells(t *testing.T) {
	victim := &overflowing{r: 'L'}
	overflower := &overflowing{r: 'R', under: 4}
	c := NewComposer(&twoUp{a: victim, b: overflower, split: 4}, 10, 1)
	f, _ := c.Frame()

	// The half that must still work. A clip that also rejected legitimate
	// writes would satisfy the assertion below while breaking every
	// component in the framework, so it is checked first.
	if got := rowOf(f.Cells, 0, 4, 10); got != "RRRRRR" {
		t.Fatalf("the overflowing component painted %q inside its OWN bounds, want %q — "+
			"the clip is rejecting writes it should accept", got, "RRRRRR")
	}
	// The half #357 is about: cells 0..3 belong to a component that has
	// already painted and is now clean.
	if got := rowOf(f.Cells, 0, 0, 4); got != "LLLL" {
		t.Errorf("cells 0..3 are %q, want %q — the right component painted over its "+
			"neighbour. Those cells are clean, so nothing will ever repaint them", got, "LLLL")
	}
}

// A clip must not be escapable by clipping WIDER, or a component could
// opt itself back out of the guard.
func TestClipOnlyEverNarrows(t *testing.T) {
	b := render.NewBuffer(10, 1)
	b.Clip(render.Rect{X: 2, Y: 0, W: 4, H: 1})
	b.Clip(render.Rect{X: 0, Y: 0, W: 10, H: 1}) // try to widen back out
	b.Set(0, 0, 'x', render.Style{})
	b.Set(9, 0, 'x', render.Style{})
	b.Set(3, 0, 'y', render.Style{})
	if b.At(0, 0).Rune == 'x' || b.At(9, 0).Rune == 'x' {
		t.Error("a second Clip widened the region: a component can escape its own clip")
	}
	if b.At(3, 0).Rune != 'y' {
		t.Error("the intersection rejected a write inside BOTH rects")
	}
}

// placing renders one image over a cell rect of its choosing, so a test
// can ask for a placement larger than the component.
type placing struct {
	Base
	cols, rows int
}

func (p *placing) Measure(Size) Size { return Size{W: 1, H: 1} }
func (p *placing) Render(f *Frame) {
	b := p.Bounds()
	f.Place(graphics.Placement{
		Img:  image.NewRGBA(image.Rect(0, 0, p.cols*10, p.rows*20)),
		Col:  b.X,
		Row:  b.Y,
		Cols: p.cols,
		Rows: p.rows,
	})
}

// Cells are only half of it. A sixel or kitty image is composited by the
// TERMINAL, so no cell-plane bounds check can catch one that overhangs —
// a clip covering text and not pictures would look like it works right
// up until the first component with an image.
func TestAPlacementIsClippedToItsComponentToo(t *testing.T) {
	wide := &placing{cols: 9, rows: 1}
	c := NewComposer(&twoUp{a: wide, b: &overflowing{r: 'R'}, split: 4}, 10, 1)
	f, _ := c.Frame()

	ps := f.Placements()
	if len(ps) != 1 {
		t.Fatalf("got %d placements, want 1 — the placement was dropped entirely "+
			"rather than cropped, which loses a partly-visible image", len(ps))
	}
	if ps[0].Col != 0 || ps[0].Cols != 4 {
		t.Errorf("placement covers cols %d..%d, want 0..4 — a 9-cell image from a "+
			"4-cell component overhangs its neighbour, and the terminal composites "+
			"it over whatever is there", ps[0].Col, ps[0].Col+ps[0].Cols)
	}
	// Cropped in PIXELS as well, or the encoder scales the whole picture
	// into the narrower box and the image is squashed rather than clipped.
	if got := ps[0].Img.Bounds().Dx(); got != 40 {
		t.Errorf("cropped image is %dpx wide, want 40 (4 of 9 cells at 10px) — "+
			"the cell rect was trimmed but the image was not", got)
	}
}

// rowOf reads cells [x0,x1) of row y as a string.
func rowOf(b *render.Buffer, y, x0, x1 int) string {
	out := make([]rune, 0, x1-x0)
	for x := x0; x < x1; x++ {
		out = append(out, b.At(x, y).Rune)
	}
	return string(out)
}

// The clip must be put BACK after every Render, and the failure mode if
// it is not is silent in the opposite direction: the Composer's own
// writes — a pre-clear, restoreUnder sweeping a vacated overlay rect —
// happen OUTSIDE any component's Render and legitimately touch cells all
// over the screen. Leave the last component's clip installed and those
// writes are dropped, so a vacated popup never gets cleaned up and the
// stale rect stays on screen.
//
// Asserting the rect rather than a symptom because the symptom needs a
// second frame and an overlay to appear at all; the invariant is what
// actually has to hold.
func TestTheClipIsRestoredAfterEveryRender(t *testing.T) {
	c := NewComposer(&twoUp{
		a: &overflowing{r: 'L'}, b: &overflowing{r: 'R', under: 4}, split: 4,
	}, 10, 1)
	f, _ := c.Frame()
	if got, want := f.Cells.ClipRect(), (render.Rect{X: 0, Y: 0, W: 10, H: 1}); got != want {
		t.Fatalf("after the frame the clip is %+v, want the whole buffer %+v — "+
			"the Composer's own writes (pre-clears, restoreUnder) run outside "+
			"any Render and would be silently dropped", got, want)
	}
}

// The cost claim in #357: bounding a write to the painting component
// rather than to the screen is the SAME four comparisons, so clipping is
// free and can therefore be framework-wide instead of opt-in.
//
// BOTH ARMS WRITE THE SAME CELLS. The first version of this benchmark let
// the clipped arm write across the whole buffer, so most of its writes
// were rejected and it skipped the cell store entirely — it came out
// twice as FAST, which measured "a rejected write is cheap" rather than
// the cost of the check. Every write below lands inside the region, so
// the only difference between the arms is which four values the
// comparison uses.
func benchSet(b *testing.B, clip bool) {
	buf := render.NewBuffer(200, 50)
	r := render.Rect{X: 10, Y: 5, W: 100, H: 20}
	if clip {
		buf.Clip(r)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Set(r.X+i%r.W, r.Y+i%r.H, 'x', render.Style{})
	}
}

func BenchmarkBufferSetClipped(b *testing.B)   { benchSet(b, true) }
func BenchmarkBufferSetUnclipped(b *testing.B) { benchSet(b, false) }
