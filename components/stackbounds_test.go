package components

// A stack whose children want more room than it has must not arrange
// them outside itself.
//
// Nothing in the framework clips a component to its arranged rect —
// render.Cells.SetString clips to the BUFFER, which is the screen, not
// the parent. So a rect is a promise: paint here and nowhere else. A
// container that hands a child a rect outside its own bounds has
// already lost, and the child cannot detect it: Text.Render clips
// diligently to Text.Bounds(), which is exactly the wrong rectangle.
//
// The zero-size case was already handled (vstack.go's early return, and
// its comment names this hazard precisely — "the subtree keeps a rect
// with real area outside its parent's bounds and paints there"). The
// OVERFLOW case was not: a stack with real area whose children simply
// want more of it walks `y` straight past its own bottom edge.
//
// Found in apps/wysiwyg, where the generated-markup pane is two
// <Text> in a <VStack> inside a fixed 12-row Grid row. It stayed hidden
// because it needs content longer than the pane's author imagined, and
// generated markup — deep indentation, long attribute lists — exceeds
// that on the first non-trivial document. Nobody hand-writes a test
// case that long, so this file generates one.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
)

// generatedMarkup emits the kind of text the editor's markup pane shows:
// nested elements, indentation that grows with depth, and attribute
// lists long enough to pass any sane pane width. Built rather than
// pasted — a hand-written literal is short enough to miss the bug,
// which is how the bug survived.
func generatedMarkup(depth int) string {
	var b strings.Builder
	b.WriteString("<Gooey xmlns=\"wonderforge.io/gooey/2026\">\n")
	for i := 0; i < depth; i++ {
		pad := strings.Repeat("  ", i+1)
		fmt.Fprintf(&b, "%s<VStack Name=\"Generated%d\" Gap=\"1\" Width=\"40\" Margin=\"1,1,1,1\" HAlign=\"Start\">\n", pad, i)
	}
	fmt.Fprintf(&b, "%s<Text Style=\"dim\">a generated leaf whose line is comfortably wider than any pane</Text>\n",
		strings.Repeat("  ", depth+1))
	for i := depth - 1; i >= 0; i-- {
		fmt.Fprintf(&b, "%s</VStack>\n", strings.Repeat("  ", i+1))
	}
	b.WriteString("</Gooey>\n")
	return b.String()
}

// THE SECOND BUG, and the one that produced the reported symptom.
//
// A stack measured against a generous avail and then arranged into a
// smaller rect. That is not exotic — a Grid measuring its children
// against the screen and arranging them into a fixed track does it on
// every frame — but it is the only shape that reveals the defect, which
// is why an earlier repro that measured and arranged with the SAME
// extent came back green and nearly closed the investigation.
//
// v.sizes is what each child WANTED, not a budget. Arrange walked `y`
// by those cached heights with no bound, so children past the edge were
// arranged outside the stack, and nothing downstream clips them.
//
// The witness was a Border's bottom chrome row reading "╰  </Canvas>" —
// corner intact, rule overwritten by text from the pane above it.
func TestStacksClampToTheirArrangedRectWhenMeasuredLarger(t *testing.T) {
	for _, tc := range []struct {
		name string
		run  func() (parts []gooey.Rect, parent gooey.Rect)
	}{
		{"vstack", func() ([]gooey.Rect, gooey.Rect) {
			a := &Text{Content: Str(generatedMarkup(8))}
			b := &Text{Content: Str(generatedMarkup(8))}
			s := &VStack{Children: []gooey.Component{a, b}}
			s.Measure(gooey.Size{W: 40, H: 40}) // generous
			pane := gooey.Rect{X: 2, Y: 3, W: 40, H: 10}
			s.Arrange(pane) // then squeezed
			return []gooey.Rect{a.Bounds(), b.Bounds()}, pane
		}},
		{"hstack", func() ([]gooey.Rect, gooey.Rect) {
			long := strings.Repeat("wide-column-content ", 12)
			a := &Text{Content: Str(long)}
			b := &Text{Content: Str(long)}
			s := &HStack{Children: []gooey.Component{a, b}}
			s.Measure(gooey.Size{W: 400, H: 4})
			pane := gooey.Rect{X: 5, Y: 1, W: 30, H: 4}
			s.Arrange(pane)
			return []gooey.Rect{a.Bounds(), b.Bounds()}, pane
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parts, parent := tc.run()
			for i, got := range parts {
				if outside(got, parent) {
					t.Errorf("child %d arranged at %+v, outside the stack's own %+v\n"+
						"the measure cache is what the child WANTED; Arrange may be handed "+
						"less, and nothing clips a component to its parent", i, got, parent)
				}
			}
		})
	}
}

// The clamp must not cost the first child the room that does exist —
// otherwise "fix the overflow" becomes "blank the pane", which would
// pass the test above just as well.
func TestAClampedStackStillFillsTheRoomItHas(t *testing.T) {
	a := &Text{Content: Str(generatedMarkup(8))}
	b := &Text{Content: Str(generatedMarkup(8))}
	s := &VStack{Children: []gooey.Component{a, b}}
	s.Measure(gooey.Size{W: 40, H: 40})
	pane := gooey.Rect{X: 2, Y: 3, W: 40, H: 10}
	s.Arrange(pane)

	if got := a.Bounds(); got.Y != pane.Y || got.H != pane.H {
		t.Errorf("first child = %+v, want it to fill the pane (y=%d h=%d) — it wanted "+
			"more than the pane has, so it should get all of it", got, pane.Y, pane.H)
	}
	if got := b.Bounds(); got.H != 0 {
		t.Errorf("second child = %+v, want h=0 — there is no room left, and a "+
			"zero-area rect is the only honest answer", got)
	}
}

// THE FIRST BUG. A Grid whose FIXED tracks alone want more than it has.
//
// Starving the star tracks is not enough: `remaining` floors at zero,
// so the stars collapse correctly and the fixed tracks keep their full
// demand anyway. offsets() then walks the cumulative total past the
// grid's own edge and every later track is arranged outside it.
//
// apps/wysiwyg's shell is Rows="1,1*,12,1" — fourteen rows of fixed
// demand — so on a terminal shorter than that the 12-row markup pane
// runs past the bottom and the status bar is arranged entirely
// off-screen. That is the reported "text just overflows its bounds".
//
// Ranged over heights rather than pinned at one, because the boundary
// is the interesting part: at 15 and 14 it fits exactly, and only below
// does it overrun. A single-height test would sit on one side of that
// and prove nothing about the other.
func TestGridDoesNotArrangeTracksPastItsOwnEdge(t *testing.T) {
	rows, err := ParseGridLens("1,1*,12,1")
	if err != nil {
		t.Fatal(err)
	}
	cols, _ := ParseGridLens("1*")

	for _, h := range []int{24, 15, 14, 13, 10, 6, 2, 1} {
		cells := []*Text{
			{Content: Str("title")}, {Content: Str("preview")},
			{Content: Str("markup")}, {Content: Str("status")},
		}
		g := &Grid{Rows: rows, Cols: cols}
		for i, c := range cells {
			c.LayoutProps().Row = i
			g.Children = append(g.Children, c)
		}

		screen := gooey.Rect{X: 0, Y: 0, W: 40, H: h}
		g.Measure(gooey.Size{W: screen.W, H: screen.H})
		g.Arrange(screen)

		for i, c := range cells {
			if got := c.Bounds(); outside(got, screen) {
				t.Errorf("screen H=%d: row %d arranged at %+v, outside the grid's own %+v\n"+
					"nothing clips a component to its parent, so this row paints over its "+
					"neighbours or off-screen entirely", h, i, got, screen)
			}
		}
	}
}

// The clamp must take room away from the LAST tracks, not from every
// track proportionally. A fixed track means "this many cells", and a
// grid out of room should show the first tracks at their stated size
// and lose the last — the same way clipping text keeps the first lines.
// Scaling everything down would silently violate every fixed size on
// the page to honour a total that cannot be met.
func TestGridClampTruncatesTheStraddlingTrackNotAllOfThem(t *testing.T) {
	rows, _ := ParseGridLens("1,1*,12,1")
	cols, _ := ParseGridLens("1*")
	cells := []*Text{
		{Content: Str("title")}, {Content: Str("preview")},
		{Content: Str("markup")}, {Content: Str("status")},
	}
	g := &Grid{Rows: rows, Cols: cols}
	for i, c := range cells {
		c.LayoutProps().Row = i
		g.Children = append(g.Children, c)
	}
	screen := gooey.Rect{X: 0, Y: 0, W: 40, H: 10}
	g.Measure(gooey.Size{W: screen.W, H: screen.H})
	g.Arrange(screen)

	// The 1-cell title keeps its stated size.
	if got := cells[0].Bounds(); got.H != 1 || got.Y != 0 {
		t.Errorf("the leading fixed row = %+v, want y=0 h=1 — a fixed track that fits "+
			"must not be shrunk to make room for one that does not", got)
	}
	// The star row starves, as it already did.
	if got := cells[1].Bounds(); got.H != 0 {
		t.Errorf("the star row = %+v, want h=0", got)
	}
	// The straddling row takes exactly what is left, not zero and not 12.
	if got := cells[2].Bounds(); got.H != 9 {
		t.Errorf("the straddling row = %+v, want h=9 (the 9 rows that remain); "+
			"collapsing it to zero would blank the pane, and 12 is the overrun", got)
	}
	// Everything past the edge gets nothing.
	if got := cells[3].Bounds(); got.H != 0 {
		t.Errorf("the row past the edge = %+v, want h=0", got)
	}
}

// outside reports whether child sits anywhere outside parent.
func outside(child, parent gooey.Rect) bool {
	if child.W <= 0 || child.H <= 0 {
		return false // an empty rect paints nothing, wherever it is
	}
	return child.X < parent.X || child.Y < parent.Y ||
		child.X+child.W > parent.X+parent.W ||
		child.Y+child.H > parent.Y+parent.H
}

func TestVStackDoesNotArrangeChildrenOutsideItself(t *testing.T) {
	src := generatedMarkup(6)
	lines := strings.Count(src, "\n")
	if lines < 12 {
		t.Fatalf("the generated markup is only %d lines; it must exceed the pane to exercise the bug", lines)
	}

	// The editor's pane, to scale: a fixed-height slot holding text that
	// is much taller than it.
	head := &Text{Content: Str("tree summary")}
	body := &Text{Content: Str(src)}
	stack := &VStack{Children: []gooey.Component{head, body}}

	pane := gooey.Rect{X: 2, Y: 3, W: 40, H: 12}
	stack.Measure(gooey.Size{W: pane.W, H: pane.H})
	stack.Arrange(pane)

	for i, c := range []*Text{head, body} {
		if got := c.Bounds(); outside(got, pane) {
			t.Errorf("child %d arranged at %+v, outside the stack's own %+v\n"+
				"nothing clips a component to its parent, so this child paints over "+
				"whatever sits below the pane", i, got, pane)
		}
	}
}

func TestHStackDoesNotArrangeChildrenOutsideItself(t *testing.T) {
	// The same defect on the other axis: x walks past the right edge.
	long := strings.Repeat("wide-column-content ", 12)
	cols := []*Text{
		{Content: Str(long)}, {Content: Str(long)}, {Content: Str(long)},
	}
	stack := &HStack{Gap: 1, Children: []gooey.Component{cols[0], cols[1], cols[2]}}

	pane := gooey.Rect{X: 5, Y: 1, W: 30, H: 4}
	stack.Measure(gooey.Size{W: pane.W, H: pane.H})
	stack.Arrange(pane)

	for i, c := range cols {
		if got := c.Bounds(); outside(got, pane) {
			t.Errorf("child %d arranged at %+v, outside the stack's own %+v", i, got, pane)
		}
	}
}

// The overflowing child must still be given the room that DOES exist,
// not collapsed to nothing — a pane that clips its content still shows
// the first screenful. This is what separates the fix from "clamp
// everything to zero", which would also pass the tests above.
func TestVStackStillFillsTheRoomItHasBeforeClipping(t *testing.T) {
	body := &Text{Content: Str(generatedMarkup(6))}
	stack := &VStack{Children: []gooey.Component{&Text{Content: Str("header")}, body}}
	pane := gooey.Rect{X: 0, Y: 0, W: 40, H: 12}
	stack.Measure(gooey.Size{W: pane.W, H: pane.H})
	stack.Arrange(pane)

	if got := body.Bounds(); got.H <= 0 {
		t.Fatalf("the overflowing child got %+v — clipping must not erase it, or the "+
			"pane shows nothing at all", got)
	}
	// header takes row 0; the body should get the rest of the pane.
	if got := body.Bounds(); got.Y+got.H != pane.Y+pane.H {
		t.Errorf("the overflowing child got %+v, want it to run to the pane's bottom "+
			"edge (y=%d); a clip that stops short wastes rows the pane has",
			got, pane.Y+pane.H)
	}
}

// A child that fits is untouched. Without this, "clamp to the parent"
// could silently change every ordinary layout in the framework — and
// the damage-count contract tests would be the ones to notice, loudly
// and much later.
func TestVStackLeavesFittingChildrenExactlyWhereTheyWere(t *testing.T) {
	a := &Text{Content: Str("one")}
	b := &Text{Content: Str("two")}
	stack := &VStack{Children: []gooey.Component{a, b}}

	pane := gooey.Rect{X: 4, Y: 2, W: 20, H: 10}
	stack.Measure(gooey.Size{W: pane.W, H: pane.H})
	stack.Arrange(pane)

	if got := a.Bounds(); got.Y != 2 || got.H != 1 {
		t.Errorf("first child = %+v, want y=2 h=1", got)
	}
	if got := b.Bounds(); got.Y != 3 || got.H != 1 {
		t.Errorf("second child = %+v, want y=3 h=1", got)
	}
}

// TestGridDoesNotArrangeColumnsPastItsOwnEdge is the COLUMN-AXIS half of
// TestGridDoesNotArrangeTracksPastItsOwnEdge.
//
// It exists because the two axes are separate code paths that merely look
// alike: Arrange resolves rows and columns with independent calls, so a
// clamp applied to one and forgotten on the other is a live possibility
// that a row-only test cannot see. Every Grid test in this file drove the
// row axis with a star track, which left the column axis asserted by
// nothing at all.
func TestGridDoesNotArrangeColumnsPastItsOwnEdge(t *testing.T) {
	cols, err := ParseGridLens("1,1*,12,1")
	if err != nil {
		t.Fatal(err)
	}
	rows, _ := ParseGridLens("1*")

	for _, w := range []int{24, 15, 14, 13, 10, 6, 2, 1} {
		cells := []*Text{
			{Content: Str("a")}, {Content: Str("b")},
			{Content: Str("c")}, {Content: Str("d")},
		}
		g := &Grid{Rows: rows, Cols: cols}
		for i, c := range cells {
			c.LayoutProps().Col = i
			g.Children = append(g.Children, c)
		}

		screen := gooey.Rect{X: 0, Y: 0, W: w, H: 6}
		g.Measure(gooey.Size{W: screen.W, H: screen.H})
		g.Arrange(screen)

		for i, c := range cells {
			if got := c.Bounds(); outside(got, screen) {
				t.Errorf("screen W=%d: col %d arranged at %+v, outside the grid's own %+v\n"+
					"nothing clips a component to its parent, so this column paints over its "+
					"neighbours or off-screen entirely", w, i, got, screen)
			}
		}
	}
}

// TestGridWithNoStarTrackStillClamps covers the weight == 0 branch of
// distributeStars — the one that returns clampToExtent(out, extent)
// directly instead of dividing anything up.
//
// A grid of only fixed tracks is the case where over-allocation is most
// obvious and least defended: the sizes are stated outright, so if they
// sum past the extent there is no star to absorb the difference and the
// arithmetic simply walks off the end. Every other Grid test here carries
// a star track, so that early return was reached by nothing.
func TestGridWithNoStarTrackStillClamps(t *testing.T) {
	rows, err := ParseGridLens("1,12,1") // 14 cells of fixed track, no star
	if err != nil {
		t.Fatal(err)
	}
	cols, _ := ParseGridLens("8")

	for _, h := range []int{20, 14, 13, 8, 2, 1, 0} {
		cells := []*Text{
			{Content: Str("head")}, {Content: Str("body")}, {Content: Str("foot")},
		}
		g := &Grid{Rows: rows, Cols: cols}
		for i, c := range cells {
			c.LayoutProps().Row = i
			g.Children = append(g.Children, c)
		}

		screen := gooey.Rect{X: 0, Y: 0, W: 8, H: h}
		g.Measure(gooey.Size{W: screen.W, H: screen.H})
		g.Arrange(screen)

		for i, c := range cells {
			if got := c.Bounds(); outside(got, screen) {
				t.Errorf("all-fixed rows at H=%d: row %d arranged at %+v, outside %+v\n"+
					"with no star track there is nothing to absorb the overflow, so an "+
					"unclamped grid walks straight off its own bottom edge", h, i, got, screen)
			}
		}
	}
}
