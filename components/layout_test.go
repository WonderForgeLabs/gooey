package components

import (
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

func text(s string) *Text { return &Text{Content: prop.NewSource(s)} }

func TestMarginAndAlignment(t *testing.T) {
	centered := text("hi")
	root := &VStack{Children: []gooey.Component{
		gooey.L(centered, gooey.Layout{Margin: gooey.MH(0, 1), HAlign: gooey.AlignCenter}),
	}}
	root.Measure(gooey.Size{W: 20, H: 10})
	root.Arrange(gooey.Rect{X: 0, Y: 0, W: 20, H: 10})

	b := centered.Bounds()
	// "hi" is 2 wide, centered in 20 → x=9; vertical margin 1 → y=1.
	if b.X != 9 || b.Y != 1 || b.W != 2 || b.H != 1 {
		t.Fatalf("bounds = %+v, want {9 1 2 1}", b)
	}
}

func TestExplicitSizeAndEndAlignment(t *testing.T) {
	w := text("x")
	root := &VStack{Children: []gooey.Component{
		gooey.L(w, gooey.Layout{Width: 5, HAlign: gooey.AlignEnd}),
	}}
	root.Measure(gooey.Size{W: 12, H: 4})
	root.Arrange(gooey.Rect{X: 0, Y: 0, W: 12, H: 4})
	b := w.Bounds()
	if b.W != 5 || b.X != 7 {
		t.Fatalf("bounds = %+v, want W=5 X=7", b)
	}
}

func TestCollapsedTakesNoSpace(t *testing.T) {
	a, b, c := text("a"), text("b"), text("c")
	root := &VStack{Children: []gooey.Component{a, gooey.L(b, gooey.Layout{Visibility: gooey.Collapsed}), c}}
	root.Measure(gooey.Size{W: 10, H: 10})
	root.Arrange(gooey.Rect{X: 0, Y: 0, W: 10, H: 10})
	if got := c.Bounds().Y; got != 1 {
		t.Fatalf("c.Y = %d, want 1 (collapsed b must occupy no row)", got)
	}
	if s := root.Measure(gooey.Size{W: 10, H: 10}); s.H != 2 {
		t.Fatalf("stack height = %d, want 2", s.H)
	}
}

func TestGridAutoStarFixed(t *testing.T) {
	head := text("header")
	body := text("body")
	side := text("side")
	g := &Grid{
		Rows: []GridLen{Auto(), Star(1)},
		Cols: []GridLen{Fixed(10), Star(1)},
		Children: []gooey.Component{
			gooey.L(head, gooey.Layout{Row: 0, Col: 0, ColSpan: 2}),
			gooey.L(side, gooey.Layout{Row: 1, Col: 0}),
			gooey.L(body, gooey.Layout{Row: 1, Col: 1}),
		},
	}
	g.Measure(gooey.Size{W: 30, H: 10})
	g.Arrange(gooey.Rect{X: 0, Y: 0, W: 30, H: 10})

	if b := head.Bounds(); b.Y != 0 || b.W != 30 {
		t.Fatalf("head bounds = %+v, want row0 spanning full width", b)
	}
	if b := side.Bounds(); b.Y != 1 || b.X != 0 || b.W != 10 || b.H != 9 {
		t.Fatalf("side bounds = %+v, want {0 1 10 9}", b)
	}
	if b := body.Bounds(); b.X != 10 || b.W != 20 || b.H != 9 {
		t.Fatalf("body bounds = %+v, want {10 1 20 9}", b)
	}
}

func TestGridStarWeights(t *testing.T) {
	a, b := text("a"), text("b")
	g := &Grid{
		Cols: []GridLen{Star(1), Star(3)},
		Children: []gooey.Component{
			gooey.L(a, gooey.Layout{Col: 0}),
			gooey.L(b, gooey.Layout{Col: 1}),
		},
	}
	g.Measure(gooey.Size{W: 40, H: 3})
	g.Arrange(gooey.Rect{X: 0, Y: 0, W: 40, H: 3})
	if ab, bb := a.Bounds(), b.Bounds(); ab.W != 10 || bb.W != 30 || bb.X != 10 {
		t.Fatalf("a=%+v b=%+v, want 10/30 split", ab, bb)
	}
}

// A Grid that is Collapsed on the frame it first appears is ARRANGED
// without ever having been Measured: ArrangeChild sends a Collapsed
// child straight to Arrange at a zero rect, while MeasureChild returns
// without calling Measure at all. The track cache is therefore empty
// when Arrange reads it, and reading it by index used to panic — so
// every hidden Tabs page rooted in a Grid, and every
// Visibility="Collapsed" Grid, crashed on its first frame.
func TestCollapsedGridArrangesWithoutMeasure(t *testing.T) {
	body := text("body")
	g := &Grid{
		Rows:     []GridLen{Star(1)},
		Cols:     []GridLen{Auto(), Star(1)},
		Children: []gooey.Component{gooey.L(body, gooey.Layout{Row: 0, Col: 1})},
	}
	gooey.LayoutOf(g).Visibility = gooey.Collapsed
	c := gooey.NewComposer(&VStack{Children: []gooey.Component{g}}, 20, 6)
	// Panicked here before Arrange grew its zero-rect guard AND
	// distributeStars grew its track-cache padding. Either alone keeps
	// this up, so the line pins the pair rather than one of them.
	c.Frame()

	if b := body.Bounds(); b.W != 0 || b.H != 0 {
		t.Fatalf("a collapsed grid's child has bounds %+v, want nothing", b)
	}
	// And it comes back: made visible, the ordinary measure/arrange
	// sandwich fills the cache and the child gets its cell.
	gooey.LayoutOf(g).Visibility = gooey.Visible
	c.Invalidate()
	c.Frame()
	if b := body.Bounds(); b.W == 0 || b.H == 0 {
		t.Fatalf("after becoming visible the child has bounds %+v, want a real cell", b)
	}
}

// Collapsing a laid-out Grid has to take its whole subtree off screen.
// The Composer erases a component's cells by noticing its BOUNDS
// changed, so a descendant that keeps the rect it had is never swept
// and stays painted over whatever replaced it — which is what a hidden
// Tabs page rooted in a Grid did, leaving the outgoing page's text
// showing through the incoming one.
//
// The Grid is deliberately NOT at the screen origin and its tracks are
// Auto. Both matter: Auto tracks come out of the MEASURE cache, which an
// Arrange into nothing does not refresh, and the offset is what makes
// the stale track offsets hand the child back its exact old ABSOLUTE
// rect. A Grid at 0,0 with star tracks hides the bug, because stars
// shrink with the extent and the bounds change anyway.
//
// Bounds are the mechanism, so they are asserted — but bounds alone do
// not prove the subtree stopped painting, which is the actual claim.
// The damage rectangles are what prove it, and the screen is the
// user-visible consequence.
func TestCollapsedGridZeroesItsSubtree(t *testing.T) {
	body := text("body")
	inner := &VStack{Children: []gooey.Component{body}}
	g := &Grid{
		Rows:     []GridLen{Auto(), Auto()},
		Cols:     []GridLen{Auto()},
		Children: []gooey.Component{gooey.L(inner, gooey.Layout{Row: 1})},
	}
	root := &VStack{Children: []gooey.Component{text("header"), g}}
	c := gooey.NewComposer(root, 20, 6)
	c.Frame()
	c.Frame() // settle: the next frame's damage is only what the collapse costs
	live := body.Bounds()
	if live.W == 0 || live.H == 0 {
		t.Fatalf("the child starts at %+v, want a real cell", live)
	}

	gooey.LayoutOf(g).Visibility = gooey.Collapsed
	c.Invalidate()
	f, _ := c.Frame()

	// Zero AREA, not zero on both axes: a stack panel arranged into
	// nothing still hands its children the height its measure cache
	// remembers, so a leaf legitimately ends at W=0,H=1. Nothing paints
	// through a zero-width rect, and the bounds still CHANGED, which is
	// what wakes the sweep.
	if b := body.Bounds(); b.W > 0 && b.H > 0 {
		t.Errorf("after collapsing the grid its child still covers cells: %+v", b)
	}
	// The damage count that matters is the count of rectangles with AREA
	// that still sit inside the collapsed subtree's old footprint. The
	// collapse must repaint the vacated region to nothing, not repaint
	// the subtree in place: a descendant that kept its rect shows up
	// here as a live rectangle, and that is exactly the regression.
	// CONTAINED in the vacated rect, not merely overlapping it: an
	// ancestor that legitimately spans the screen (the root stack)
	// overlaps everything and is not what this is about. A rectangle
	// that fits inside the footprint the subtree just gave up is a node
	// that kept its slot — the regression, exactly.
	stale := 0
	for _, d := range c.Damage() {
		if d.W > 0 && d.H > 0 &&
			d.X >= live.X && d.X+d.W <= live.X+live.W &&
			d.Y >= live.Y && d.Y+d.H <= live.Y+live.H {
			stale++
		}
	}
	if stale != 0 {
		t.Errorf("collapsing the grid left %d component(s) still painting inside the vacated rect %+v (damage %+v)",
			stale, live, c.Damage())
	}
	if b := inner.Bounds(); b.W > 0 && b.H > 0 {
		t.Errorf("the intermediate container kept its cells: %+v", b)
	}
	if row := rowText(f, live.Y, 20); strings.TrimSpace(row) != "" {
		t.Errorf("the collapsed subtree is still on screen: row %d = %q", live.Y, row)
	}

	// And it settles: with nothing else changing, the frame after the
	// collapse repaints nothing at all.
	if _, n := c.Frame(); n != 0 {
		t.Errorf("the frame after the collapse repainted %d components, want 0", n)
	}
}

// A bound Visibility keeps working underneath a Collapsed ancestor.
//
// This is the failure that would be silent: the subscription for a bound
// Visibility lives in the Composer's per-node observer computed, whose
// EVALUATION is the subscription (the call-site rule). MeasureChild also
// syncs the field, but as a plain read that records nothing — and a
// Collapsed ancestor means MeasureChild is never called on the subtree
// at all, and Grid.Arrange returns early without touching the children's
// slots. If the subscription rode on either of those, a page hidden once
// would come back permanently stale, and no test asserting bounds or
// pixels would notice.
//
// It does not ride on either: Frame re-evaluates every node's observer
// at the TOP of the frame, before Measure and Arrange run, over the node
// list built from the component tree — which a collapse does not prune.
func TestBoundVisibilitySurvivesACollapsedAncestor(t *testing.T) {
	show := prop.NewSource(true)
	leaf := text("leaf")
	gooey.LayoutOf(leaf).BindVisibilityBool(show)
	g := &Grid{
		Rows:     []GridLen{Auto()},
		Children: []gooey.Component{&VStack{Children: []gooey.Component{leaf}}},
	}
	c := gooey.NewComposer(&VStack{Children: []gooey.Component{g}}, 20, 6)
	c.Frame()
	if b := leaf.Bounds(); b.W == 0 || b.H == 0 {
		t.Fatalf("the leaf starts at %+v, want a real cell", b)
	}

	gooey.LayoutOf(g).Visibility = gooey.Collapsed
	c.Invalidate()
	c.Frame()

	// Flip the bound SOURCE while the whole subtree is collapsed — the
	// window in which a dropped subscription is invisible.
	show.Set(false)
	c.Frame()
	if got := gooey.LayoutOf(leaf).Visibility; got != gooey.Collapsed {
		t.Fatalf("a Set under a collapsed ancestor did not reach the leaf: Visibility = %v, want Collapsed", got)
	}

	// Re-showing the ancestor must honour the value set while it was
	// away, not the one it had when it went away.
	gooey.LayoutOf(g).Visibility = gooey.Visible
	c.Invalidate()
	c.Frame()
	if b := leaf.Bounds(); b.W > 0 && b.H > 0 {
		t.Errorf("the leaf came back visible at %+v despite show=false", b)
	}
	show.Set(true)
	c.Frame()
	if b := leaf.Bounds(); b.W == 0 || b.H == 0 {
		t.Errorf("the leaf did not come back when show flipped to true: %+v", b)
	}
}

// A component paints inside its own bounds and nowhere else. That is
// the damage contract, not a nicety: the Composer erases a component by
// filling the rect it remembers, so a cell written outside that rect is
// a scar no sweep can ever reach.
//
// A degenerate rect is where it goes wrong, and a degenerate rect is
// ordinary — a Visible component inside a Collapsed ancestor (a hidden
// Tabs page) is arranged into nothing while staying paintable, and a
// row of a full stack can run out of height with width to spare. A
// Render that writes its row at bounds.Y without looking writes it into
// whatever is there. Border is the worst of them: with W or H at zero
// its far-edge arithmetic (r.X+r.W-1) walks BACKWARDS, so the corners
// land outside the rect on both axes.
//
// Zero-height-with-width is the case the shipped pages do not reach on
// their own, which is exactly why it is pinned here rather than left to
// cmd/toolkitdemo's screen-compare tests.
func TestZeroRectComponentsPaintNothing(t *testing.T) {
	for _, tc := range []struct {
		name string
		w    gooey.Component
	}{
		{"Border", &Border{Title: Str("title"), Style: Sty(render.Style{}), Child: text("inner")}},
		{"Button", &Button{Content: Str("press me")}},
		{"PixelButton", &Button{Content: Str("press me"), Chrome: ChromePixel}},
		{"Checkbox", &Checkbox{Checked: prop.NewSource(true), Label: Str("on")}},
		{"Gauge", &Gauge{Value: prop.NewSource(70), Label: Str("load ")}},
	} {
		for _, r := range []gooey.Rect{
			{X: 2, Y: 1, W: 0, H: 0},  // collapsed away entirely
			{X: 2, Y: 1, W: 20, H: 0}, // width to spare, no row to use it on
			{X: 2, Y: 1, W: 0, H: 1},  // a row, no columns
		} {
			buf := render.NewBuffer(30, 5)
			f := &gooey.Frame{Cells: buf}
			tc.w.Arrange(r)
			tc.w.Render(f)
			for y := 0; y < buf.H; y++ {
				for x := 0; x < buf.W; x++ {
					if c := buf.At(x, y); c != (render.Cell{Rune: ' '}) {
						t.Errorf("%s arranged at %+v painted %q at (%d,%d) — outside its own bounds",
							tc.name, r, string(c.Rune), x, y)
					}
				}
			}
		}
	}
}

// A stack arranged into a degenerate slot zeroes its children, the same
// contract Grid keeps. Only ONE axis of a stack's child rect comes from
// the arrange rect — the other comes from the measure cache, which an
// Arrange into nothing does not refresh — so a stack squeezed flat on
// its MAIN axis hands every child a rect with real area: full measured
// width in a zero-width HStack, full measured height in a zero-height
// VStack. That rect sits outside the parent's bounds, the child paints
// into it, and no sweep reaches those cells.
//
// A star track that resolves to nothing is the ordinary way in: a Grid
// with Cols="*,30" at 30 columns gives the star column zero, and an
// HStack in it used to paint its whole row over the neighbour.
func TestDegenerateStackZeroesItsChildren(t *testing.T) {
	for _, tc := range []struct {
		name string
		rect gooey.Rect
		mk   func(gooey.Component) gooey.Component
	}{
		{"HStack zero width", gooey.Rect{X: 2, Y: 1, W: 0, H: 3},
			func(c gooey.Component) gooey.Component { return &HStack{Children: []gooey.Component{c}} }},
		{"VStack zero height", gooey.Rect{X: 2, Y: 1, W: 20, H: 0},
			func(c gooey.Component) gooey.Component { return &VStack{Children: []gooey.Component{c}} }},
	} {
		leaf := text("hello world")
		p := tc.mk(leaf)
		p.Measure(gooey.Size{W: 30, H: 3})
		p.Arrange(tc.rect)
		if b := leaf.Bounds(); b.W > 0 && b.H > 0 {
			t.Errorf("%s: the child kept %+v, a rect with area outside its parent %+v", tc.name, b, tc.rect)
		}
		buf := render.NewBuffer(30, 5)
		leaf.Render(&gooey.Frame{Cells: buf})
		for y := 0; y < buf.H; y++ {
			for x := 0; x < buf.W; x++ {
				if c := buf.At(x, y); c != (render.Cell{Rune: ' '}) {
					t.Errorf("%s: the child painted %q at (%d,%d)", tc.name, string(c.Rune), x, y)
				}
			}
		}
	}
}

// A Border narrower than its title's padding must not write the padding
// anyway. The title starts at r.X+2, so below four columns those two
// spaces land past the far edge — outside the node's own damage rect,
// where they erase a neighbour's cells for good. Spaces are the whole
// hazard here: they read as "nothing painted" and scar just the same.
func TestNarrowBorderKeepsItsTitleInBounds(t *testing.T) {
	for w := 1; w <= 8; w++ {
		b := &Border{Title: Str("title"), Style: Sty(render.Style{}), Child: text("x")}
		r := gooey.Rect{X: 3, Y: 1, W: w, H: 3}
		b.Arrange(r)
		buf := render.NewBuffer(20, 5)
		for y := 0; y < buf.H; y++ {
			for x := 0; x < buf.W; x++ {
				buf.Set(x, y, '#', render.Style{}) // anything not '#' was written here
			}
		}
		b.Render(&gooey.Frame{Cells: buf})
		for y := 0; y < buf.H; y++ {
			for x := 0; x < buf.W; x++ {
				if buf.At(x, y).Rune == '#' {
					continue
				}
				if x < r.X || x >= r.X+r.W || y < r.Y || y >= r.Y+r.H {
					t.Errorf("a %d-wide Border wrote %q at (%d,%d) — outside %+v",
						w, string(buf.At(x, y).Rune), x, y, r)
				}
			}
		}
	}
}

// A Collapsed child occupies nothing — including the gap it would
// otherwise have brought with it. Charging the gap made "Collapsed takes
// no space" false in any gapped stack, which is the whole point of
// Collapsed as distinct from Hidden.
func TestCollapsedChildCostsNoGap(t *testing.T) {
	mk := func(collapse bool) *VStack {
		mid := &Text{Content: Str("b")}
		if collapse {
			gooey.L(mid, gooey.Layout{Visibility: gooey.Collapsed})
		}
		return &VStack{Gap: 1, Children: []gooey.Component{
			&Text{Content: Str("a")},
			mid,
			&Text{Content: Str("c")},
		}}
	}
	if got, want := mk(false).Measure(gooey.Size{W: 10, H: 10}).H, 5; got != want {
		t.Errorf("three visible children with Gap=1: H = %d, want %d", got, want)
	}
	// Three rows minus the collapsed row minus its gap: 2 texts + 1 gap.
	if got, want := mk(true).Measure(gooey.Size{W: 10, H: 10}).H, 3; got != want {
		t.Errorf("middle child gooey.Collapsed: H = %d, want %d (no row AND no gap)", got, want)
	}
}

// The same for the horizontal stack, and for a collapsed child in the
// leading position — where the bug would leave a gap before the first
// thing actually drawn.
func TestCollapsedChildCostsNoGapHorizontally(t *testing.T) {
	first := &Text{Content: Str("xx")}
	gooey.L(first, gooey.Layout{Visibility: gooey.Collapsed})
	h := &HStack{Gap: 2, Children: []gooey.Component{
		first,
		&Text{Content: Str("yy")},
	}}
	if got, want := h.Measure(gooey.Size{W: 20, H: 4}).W, 2; got != want {
		t.Errorf("leading gooey.Collapsed child with Gap=2: W = %d, want %d", got, want)
	}
	h.Arrange(gooey.Rect{X: 0, Y: 0, W: 20, H: 4})
	if got, want := h.Children[1].(*Text).Bounds().X, 0; got != want {
		t.Errorf("child after a leading gooey.Collapsed sibling arranged at x=%d, want %d", got, want)
	}
}

// Hidden is the other half of the contract: it still occupies its space,
// gap included, and only declines to paint.
func TestHiddenChildStillCostsItsGap(t *testing.T) {
	mid := &Text{Content: Str("b")}
	gooey.L(mid, gooey.Layout{Visibility: gooey.Hidden})
	v := &VStack{Gap: 1, Children: []gooey.Component{
		&Text{Content: Str("a")},
		mid,
		&Text{Content: Str("c")},
	}}
	if got, want := v.Measure(gooey.Size{W: 10, H: 10}).H, 5; got != want {
		t.Errorf("gooey.Hidden child: H = %d, want %d (gooey.Hidden still occupies space)", got, want)
	}
}
