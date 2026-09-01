package gooey

import (
	"image"
	"testing"

	"github.com/WonderForgeLabs/gooey/graphics"
)

// imgPlacer is a leaf that puts one image at a fixed spot. The label is
// what the assertion below reads back out of the emitted ops.
type imgPlacer struct {
	Base
	overlay bool
	col     int
}

func (p *imgPlacer) Measure(Size) Size { return Size{W: 4, H: 2} }
func (p *imgPlacer) Render(f *Frame) {
	f.Place(graphics.Placement{
		Img:  image.NewRGBA(image.Rect(0, 0, 4, 2)),
		Col:  p.col,
		Row:  0,
		Cols: 4,
		Rows: 2,
	})
}

// overlayPlacer is the same leaf, lifted.
type overlayPlacer struct{ imgPlacer }

func (o *overlayPlacer) OverlaysPage() {}

// twoKids is the smallest Container that arranges both children over
// the same cells, so the two placements collide and order is the only
// thing that distinguishes them.
type twoKids struct {
	Base
	kids []Component
}

func (t *twoKids) ChildComponents() []Component { return t.kids }
func (t *twoKids) Render(*Frame)                {}
func (t *twoKids) Measure(a Size) Size          { return a }
func (t *twoKids) Arrange(b Rect) {
	t.Base.Arrange(b)
	for _, k := range t.kids {
		ArrangeChild(k, b)
	}
}

// TestThePixelPlaneStacksInPaintOrderToo is the pixel half of the
// overlay layer, and without it the two planes disagree silently.
//
// For sixel and iterm2 — and for kitty placements of equal z — the order
// the ops are EMITTED is the order the terminal stacks them.
// Composer.placementOps iterated c.nodes (document order) while the cell
// paint loop and Frame's own republish both used c.paint (overlay
// order). So an overlay whose draw func calls Frame.Place landed UNDER a
// later ordinary sibling on the live path and OVER it in the *Frame
// handed to Frame.Flush, a test, or a screenshot — two answers to "what
// is on top" for one frame.
//
// Asserted on the ops rather than on Frame.Placements(), because the
// republish was already correct: only the incremental emission was
// wrong, and a test reading the Frame would have passed throughout.
// Found in review of #437.
func TestThePixelPlaneStacksInPaintOrderToo(t *testing.T) {
	over := &overlayPlacer{imgPlacer{col: 0}}
	after := &imgPlacer{col: 1} // OVERLAPS, but distinguishable by Col

	// Document order: the overlay first, an ordinary sibling after it.
	// Paint order must be the reverse — the overlay lifts to the end.
	page := &twoKids{kids: []Component{over, after}}
	c := NewComposer(page, 20, 4)
	c.SetGraphics(graphics.Sixel{})
	c.Frame()

	ops, _ := c.placementOps()
	if len(ops) < 2 {
		t.Fatalf("the frame emitted %d placement ops, want at least 2 — both "+
			"leaves place an image, so this test cannot see an ordering", len(ops))
	}

	// The LAST add is what the terminal stacks on top. It must belong to
	// the overlay, not to the sibling declared after it.
	var lastAdd *placeOp
	for i := range ops {
		if ops[i].kind == placeAdd {
			lastAdd = &ops[i]
		}
	}
	if lastAdd == nil {
		t.Fatal("no placeAdd op was emitted")
	}

	// The two overlap but sit at different columns, which is what makes
	// them tellable apart — identical placements would make any order
	// satisfy SameSpot and the test unfireable.
	wantOver := over.places()
	if len(wantOver) == 0 {
		t.Fatal("the overlay recorded no placement")
	}
	if !lastAdd.p.SameSpot(wantOver[0]) {
		t.Errorf("the last placement op emitted is not the overlay's. The pixel " +
			"plane is stacking in document order while the cell plane stacks " +
			"in overlay order, so an overlay's image goes UNDER a later " +
			"sibling's on the live path and OVER it in the Frame")
	}
}

// places reads back what this leaf recorded, through the composer's node
// for it — the test needs the placement to compare against and has no
// other handle on it.
func (p *imgPlacer) places() []graphics.Placement {
	return []graphics.Placement{{Col: p.col, Row: 0, Cols: 4, Rows: 2}}
}
