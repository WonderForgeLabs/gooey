package gooey

import (
	"testing"

	"github.com/WonderForgeLabs/gooey/prop"
)

func text(s string) *Text { return &Text{Content: prop.NewSource(s)} }

func TestMarginAndAlignment(t *testing.T) {
	centered := text("hi")
	root := &VStack{Children: []Widget{
		L(centered, Layout{Margin: MH(0, 1), HAlign: AlignCenter}),
	}}
	root.Measure(Size{20, 10})
	root.Arrange(Rect{0, 0, 20, 10})

	b := centered.Bounds()
	// "hi" is 2 wide, centered in 20 → x=9; vertical margin 1 → y=1.
	if b.X != 9 || b.Y != 1 || b.W != 2 || b.H != 1 {
		t.Fatalf("bounds = %+v, want {9 1 2 1}", b)
	}
}

func TestExplicitSizeAndEndAlignment(t *testing.T) {
	w := text("x")
	root := &VStack{Children: []Widget{
		L(w, Layout{Width: 5, HAlign: AlignEnd}),
	}}
	root.Measure(Size{12, 4})
	root.Arrange(Rect{0, 0, 12, 4})
	b := w.Bounds()
	if b.W != 5 || b.X != 7 {
		t.Fatalf("bounds = %+v, want W=5 X=7", b)
	}
}

func TestCollapsedTakesNoSpace(t *testing.T) {
	a, b, c := text("a"), text("b"), text("c")
	root := &VStack{Children: []Widget{a, L(b, Layout{Visibility: Collapsed}), c}}
	root.Measure(Size{10, 10})
	root.Arrange(Rect{0, 0, 10, 10})
	if got := c.Bounds().Y; got != 1 {
		t.Fatalf("c.Y = %d, want 1 (collapsed b must occupy no row)", got)
	}
	if s := root.Measure(Size{10, 10}); s.H != 2 {
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
		Children: []Widget{
			L(head, Layout{Row: 0, Col: 0, ColSpan: 2}),
			L(side, Layout{Row: 1, Col: 0}),
			L(body, Layout{Row: 1, Col: 1}),
		},
	}
	g.Measure(Size{30, 10})
	g.Arrange(Rect{0, 0, 30, 10})

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
		Children: []Widget{
			L(a, Layout{Col: 0}),
			L(b, Layout{Col: 1}),
		},
	}
	g.Measure(Size{40, 3})
	g.Arrange(Rect{0, 0, 40, 3})
	if ab, bb := a.Bounds(), b.Bounds(); ab.W != 10 || bb.W != 30 || bb.X != 10 {
		t.Fatalf("a=%+v b=%+v, want 10/30 split", ab, bb)
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
			L(mid, Layout{Visibility: Collapsed})
		}
		return &VStack{Gap: 1, Children: []Widget{
			&Text{Content: Str("a")},
			mid,
			&Text{Content: Str("c")},
		}}
	}
	if got, want := mk(false).Measure(Size{10, 10}).H, 5; got != want {
		t.Errorf("three visible children with Gap=1: H = %d, want %d", got, want)
	}
	// Three rows minus the collapsed row minus its gap: 2 texts + 1 gap.
	if got, want := mk(true).Measure(Size{10, 10}).H, 3; got != want {
		t.Errorf("middle child Collapsed: H = %d, want %d (no row AND no gap)", got, want)
	}
}

// The same for the horizontal stack, and for a collapsed child in the
// leading position — where the bug would leave a gap before the first
// thing actually drawn.
func TestCollapsedChildCostsNoGapHorizontally(t *testing.T) {
	first := &Text{Content: Str("xx")}
	L(first, Layout{Visibility: Collapsed})
	h := &HStack{Gap: 2, Children: []Widget{
		first,
		&Text{Content: Str("yy")},
	}}
	if got, want := h.Measure(Size{20, 4}).W, 2; got != want {
		t.Errorf("leading Collapsed child with Gap=2: W = %d, want %d", got, want)
	}
	h.Arrange(Rect{0, 0, 20, 4})
	if got, want := h.Children[1].(*Text).Bounds().X, 0; got != want {
		t.Errorf("child after a leading Collapsed sibling arranged at x=%d, want %d", got, want)
	}
}

// Hidden is the other half of the contract: it still occupies its space,
// gap included, and only declines to paint.
func TestHiddenChildStillCostsItsGap(t *testing.T) {
	mid := &Text{Content: Str("b")}
	L(mid, Layout{Visibility: Hidden})
	v := &VStack{Gap: 1, Children: []Widget{
		&Text{Content: Str("a")},
		mid,
		&Text{Content: Str("c")},
	}}
	if got, want := v.Measure(Size{10, 10}).H, 5; got != want {
		t.Errorf("Hidden child: H = %d, want %d (Hidden still occupies space)", got, want)
	}
}
