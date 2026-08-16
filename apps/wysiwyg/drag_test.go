package main

import (
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/input"
)

// MOVE, and the numbers are the point.
//
// Per-motion damage and on-release damage are pinned SEPARATELY because
// they are the only thing that can tell the fast path from the slow one.
// A drag that wrote markup per motion would re-mount the whole designer
// subtree and still look correct on screen — every bounds assertion would
// pass. Only the count says which path ran.

func motion(c *gooey.Composer, x, y int) bool {
	return c.HandleMouse(input.MouseEvent{
		Kind: input.MouseMove, Button: input.ButtonNone, X: x, Y: y,
	})
}

func release(c *gooey.Composer, x, y int) bool {
	return c.HandleMouse(input.MouseEvent{
		Kind: input.MouseRelease, Button: input.ButtonLeft, X: x, Y: y,
	})
}

// dragFixture is a Canvas root holding two sized Texts, so there is
// something to drag and something for it to damage on the way.
func dragFixture(t *testing.T) (*editor, *gooey.Composer, *int) {
	t.Helper()
	ed, c, frames := designerPageCounting(t)
	ed.doc().Kids = []*node{
		{Elem: "Text", Body: "aaaa", Attrs: map[string]string{"Name": "A", "Canvas.Left": "1", "Canvas.Top": "1"}},
		{Elem: "Text", Body: "bbbb", Attrs: map[string]string{"Name": "B", "Canvas.Left": "1", "Canvas.Top": "6"}},
	}
	ed.rebuild()
	if !strings.HasPrefix(ed.status.Get(), "✓") {
		t.Fatalf("fixture does not build: %s", ed.status.Get())
	}
	c.Frame()
	return ed, c, frames
}

// TestDraggingMovesTheElementAndCommitsOnRelease is the feature.
func TestDraggingMovesTheElementAndCommitsOnRelease(t *testing.T) {
	ed, c, frames := dragFixture(t)
	a := docKid(ed, 0)
	b0 := a.(interface{ Bounds() gooey.Rect }).Bounds()

	if !press(c, b0.X, b0.Y) {
		t.Fatal("the press was not consumed")
	}
	if ed.sel != ed.doc().Kids[0] {
		t.Fatalf("the press selected %s, want <Text A>", nodeName(ed.sel))
	}
	before := *frames
	if !motion(c, b0.X+4, b0.Y+3) {
		t.Fatal("a motion during a drag was not consumed")
	}
	if *frames == before {
		t.Error("the drag wrote Layout.Left/Top and never asked for a frame: the element does " +
			"not move, and nothing reports why (Layout fields are outside the property graph)")
	}
	c.Frame()

	moved := a.(interface{ Bounds() gooey.Rect }).Bounds()
	if moved.X != b0.X+4 || moved.Y != b0.Y+3 {
		t.Fatalf("the element is at %v after a 4,3 drag, want %d,%d", moved, b0.X+4, b0.Y+3)
	}
	// MID-DRAG THE DOCUMENT IS UNTOUCHED. That is what makes the release
	// the only writer, and what stops a save racing a motion.
	if got := ed.doc().Kids[0].Attrs["Canvas.Left"]; got != "1" {
		t.Errorf("mid-drag the document already says Canvas.Left=%q: a motion wrote markup", got)
	}

	release(c, b0.X+4, b0.Y+3)
	if got := ed.doc().Kids[0].Attrs["Canvas.Left"]; got != "5" {
		t.Errorf("after release Canvas.Left is %q, want \"5\"", got)
	}
	if got := ed.doc().Kids[0].Attrs["Canvas.Top"]; got != "4" {
		t.Errorf("after release Canvas.Top is %q, want \"4\"", got)
	}
	if !strings.Contains(ed.source.Get(), `Canvas.Left="5"`) {
		t.Errorf("the move is not in the saved document:\n%s", ed.source.Get())
	}
}

// TestAMotionCostsFarLessThanARebuild is the pin that tells the fast path
// from the slow one, and it is a COMPARISON rather than a constant.
//
// An absolute per-motion number would move whenever the fixture's layout
// changes. What must never change is the RELATIONSHIP: a motion repaints
// the moved element and what its old and new rects uncover, while a
// rebuild re-mounts the entire designer subtree. If a motion ever costs
// as much as a rebuild, the incremental path silently fell back.
func TestAMotionCostsFarLessThanARebuild(t *testing.T) {
	ed, c, _ := dragFixture(t)
	a := docKid(ed, 0)
	b0 := a.(interface{ Bounds() gooey.Rect }).Bounds()

	press(c, b0.X, b0.Y)
	settle(t, c)

	motion(c, b0.X+3, b0.Y+2)
	_, perMotion := c.Frame()
	if perMotion == 0 {
		t.Fatal("a motion repainted nothing: the element did not move, so the count below " +
			"measures nothing")
	}
	if _, again := c.Frame(); again != 0 {
		t.Fatalf("the frame after a motion repainted %d with nothing changed: the count is not "+
			"damage", again)
	}

	release(c, b0.X+3, b0.Y+2)
	_, onRelease := c.Frame()

	t.Logf("per-motion %d, on-release %d", perMotion, onRelease)
	if perMotion >= onRelease {
		t.Errorf("a motion repainted %d components and the release repainted %d: a motion is "+
			"supposed to be the CHEAP path — if it costs as much as the rebuild, it is writing "+
			"markup or calling rebuild() per pointer report", perMotion, onRelease)
	}
	// And an absolute ceiling, loose enough to survive fixture changes and
	// tight enough to catch a re-mount: the designer holds two elements
	// plus the surface, so a motion touching more than a handful of
	// components is repainting things it did not move.
	if perMotion > 8 {
		t.Errorf("a motion repainted %d components; damage %v", perMotion, c.Damage())
	}
}

// TestDraggingOverAnotherElementCostsMore, measured rather than assumed.
//
// The bounds sweep force-repaints whatever the vacated rect uncovered
// (Composer.restoreUnder), so dragging ACROSS something should cost more
// than dragging over blank surface. The open question was whether that
// grows badly enough to matter. It does not — it is bounded by what the
// element actually overlaps, not by the size of the tree — but the number
// is recorded here so a change that makes it grow with tree size has
// something to fail against.
func TestDraggingOverAnotherElementCostsMore(t *testing.T) {
	ed, c, _ := dragFixture(t)
	a := docKid(ed, 0)
	b := docKid(ed, 1)
	ab := a.(interface{ Bounds() gooey.Rect }).Bounds()
	bb := b.(interface{ Bounds() gooey.Rect }).Bounds()

	press(c, ab.X, ab.Y)
	settle(t, c)

	// Straight down onto the second element.
	motion(c, ab.X, bb.Y)
	_, over := c.Frame()
	if over == 0 {
		t.Fatal("dragging onto the other element repainted nothing")
	}
	t.Logf("per-motion over another element: %d", over)

	// The claim: bounded by overlap, not by tree size. Two elements plus
	// the surface and its chrome is the whole designer here, so anything
	// approaching a re-mount is the incremental path having failed.
	if over > 10 {
		t.Errorf("a motion across one other element repainted %d components; damage %v: the "+
			"cost is supposed to be bounded by what the element uncovers, not by the tree",
			over, c.Damage())
	}
	release(c, ab.X, bb.Y)
}

// TestAPressAndReleaseWithoutMotionIsAClickNotAMove.
//
// Selecting something is a press and a release. If that counted as a move
// it would rewrite Canvas.Left/Top to the values they already hold and
// spend a full rebuild on every click — and prop.Set does not compare, so
// "the same value" is not free.
func TestAPressAndReleaseWithoutMotionIsAClickNotAMove(t *testing.T) {
	ed, c, _ := dragFixture(t)
	a := docKid(ed, 0)
	b := a.(interface{ Bounds() gooey.Rect }).Bounds()

	press(c, b.X, b.Y)
	settle(t, c)
	release(c, b.X, b.Y)
	_, painted := c.Frame()

	if painted != 0 {
		t.Errorf("a click repainted %d components: the release treated it as a move and rebuilt "+
			"(damage %v)", painted, c.Damage())
	}
	if ed.sel != ed.doc().Kids[0] {
		t.Errorf("the click lost the selection (%s)", nodeName(ed.sel))
	}
}

// TestWhatADragMeansIsDecidedByTheParent is the boundary, asserted for
// all three parents rather than only the ones that work.
//
// Free geometry is a property of the PARENT. A child of a <Canvas> has
// Canvas.Left/Canvas.Top and goes wherever the pointer goes; a child of a
// <Grid> has Grid.Row/Grid.Col and SNAPS to a cell; a child of a <VStack>
// has no geometry at all — its position is its index, so a drag there
// means REORDER, which is deferred.
//
// The refusal has to be complete AND AUDIBLE. Writing Canvas.Left onto a
// child of a VStack would be silently discarded by applyLayout, which is
// the defect the catalog work exists to delete — but saying nothing at
// all is the same failure one step later, because "I dragged it and
// nothing happened" is what a broken editor looks like too.
func TestWhatADragMeansIsDecidedByTheParent(t *testing.T) {
	for _, tc := range []struct {
		parent string
		kind   string
		drag   bool
		// wrote is the attribute a successful drag must leave behind, and
		// "" means the drag must leave NO positioning attribute at all.
		wrote string
	}{
		{"Canvas", DragFree, true, "Canvas.Left"},
		{"Grid", DragCell, true, "Grid.Row"},
		{"VStack", DragOrder, false, ""},
	} {
		ed, c, _ := designerPageCounting(t)
		ed.doc().Elem = tc.parent
		ed.doc().Kids = []*node{
			{Elem: "Text", Body: "aaaa", Attrs: map[string]string{"Name": "A"}},
		}
		switch tc.parent {
		case "Canvas":
			ed.doc().Kids[0].Attrs["Canvas.Left"] = "1"
			ed.doc().Kids[0].Attrs["Canvas.Top"] = "1"
		case "Grid":
			ed.doc().Attrs["Rows"] = "2,2,2"
			ed.doc().Attrs["Cols"] = "8,8"
		}
		ed.rebuild()
		if !strings.HasPrefix(ed.status.Get(), "✓") {
			t.Fatalf("<%s> fixture does not build: %s", tc.parent, ed.status.Get())
		}
		c.Frame()

		n := ed.doc().Kids[0]
		if got := ed.dragKind(n); got != tc.kind {
			t.Errorf("under a <%s> the drag kind is %q, want %q", tc.parent, got, tc.kind)
		}

		comp := ed.componentFor(n)
		if comp == nil {
			t.Fatalf("<%s>: the child was not built", tc.parent)
		}
		b := comp.(interface{ Bounds() gooey.Rect }).Bounds()
		if b.W == 0 {
			t.Fatalf("<%s>: the child was never arranged", tc.parent)
		}
		press(c, b.X, b.Y)
		if ed.drag.active() != tc.drag {
			t.Errorf("under a <%s> a press began drag=%v, want %v", tc.parent, ed.drag.active(), tc.drag)
		}
		motion(c, b.X+3, b.Y+2)
		release(c, b.X+3, b.Y+2)

		if !tc.drag {
			// No positioning attribute of ANY parent's vocabulary may
			// appear on a child whose parent cannot honour it.
			for _, a := range []string{"Canvas.Left", "Canvas.Top", "Grid.Row", "Grid.Col"} {
				if _, ok := n.Attrs[a]; ok {
					t.Errorf("dragging under a <%s> wrote %s, which applyLayout discards",
						tc.parent, a)
				}
			}
			// AND IT SAID SO. A silent refusal is indistinguishable from
			// a broken editor.
			if hint := ed.dragHint.Get(); hint == "" {
				t.Errorf("a refused drag under a <%s> said nothing at all: the user has no way "+
					"to tell it from an editor that does not work", tc.parent)
			} else if !strings.Contains(hint, tc.parent) {
				t.Errorf("the refusal under a <%s> does not name the container that decided it: %q",
					tc.parent, hint)
			}
			// And the status bar shows it, through the binding the page
			// reads rather than around it.
			if got := ed.statusText.Get(); got != ed.dragHint.Get() {
				t.Errorf("the status bar shows %q while the hint is %q", got, ed.dragHint.Get())
			}
			continue
		}
		if _, ok := n.Attrs[tc.wrote]; !ok {
			t.Errorf("a drag under a <%s> wrote no %s: nothing was committed", tc.parent, tc.wrote)
		}
		if hint := ed.dragHint.Get(); hint != "" {
			t.Errorf("a drag that worked under a <%s> left a refusal on screen: %q", tc.parent, hint)
		}
	}
}

// gridFixture is a <Grid> root with three rows and two columns and one
// <Text> in the top-left cell, plus the probed cell rectangles so a test
// can name a cell by its TERMINAL coordinates rather than by asking the
// code under test where it thinks the cell is.
//
// THE RECTANGLES ARE READ OFF Bounds(), which is the oracle rule: an
// expected post-drag position computed by the same route the drag
// computes the actual one cannot fail. Here the grid's own arranged
// children are the source, and the drag never sees them.
func gridFixture(t *testing.T) (*editor, *gooey.Composer, *node, [][]gooey.Rect) {
	t.Helper()
	ed, c, _ := designerPageCounting(t)
	ed.doc().Elem = "Grid"
	ed.doc().Attrs["Rows"] = "2,2,2"
	ed.doc().Attrs["Cols"] = "8,8"
	ed.doc().Kids = []*node{
		{Elem: "Text", Body: "aaaa", Attrs: map[string]string{"Name": "A", "Grid.Row": "0", "Grid.Col": "0"}},
	}
	ed.rebuild()
	if !strings.HasPrefix(ed.status.Get(), "✓") {
		t.Fatalf("the grid fixture does not build: %s", ed.status.Get())
	}
	c.Frame()

	n := ed.doc().Kids[0]
	comp := ed.componentFor(n)
	if comp == nil {
		t.Fatal("the grid's child was not built")
	}
	// The cell rectangles, taken from the GRID itself: its bounds and its
	// declared tracks, sliced the way the markup declares them. Written
	// out here rather than borrowed from ed.gridCells, because a fixture
	// that asked the code under test where the cells are could not catch
	// the code under test being wrong about it.
	gb := ed.componentFor(ed.doc()).(interface{ Bounds() gooey.Rect }).Bounds()
	if gb.W == 0 || gb.H == 0 {
		t.Fatalf("the <Grid> root was never arranged (%v)", gb)
	}
	cells := make([][]gooey.Rect, 3)
	for r := range cells {
		cells[r] = make([]gooey.Rect, 2)
		for col := range cells[r] {
			cells[r][col] = gooey.Rect{X: gb.X + col*8, Y: gb.Y + r*2, W: 8, H: 2}
		}
	}
	// The fixture's own claim about itself: the child really is where the
	// arithmetic above says cell (0,0) is.
	b := comp.(interface{ Bounds() gooey.Rect }).Bounds()
	if b.X != cells[0][0].X || b.Y != cells[0][0].Y {
		t.Fatalf("the child is at %v but cell (0,0) is %v: the fixture's cell arithmetic does "+
			"not describe this grid, and every coordinate below is meaningless", b, cells[0][0])
	}
	return ed, c, n, cells
}

// TestAGridDragSnapsToTheCellDURINGTheDrag is the decided behaviour, and
// the mid-drag assertion is the whole point of it.
//
// A ghost that floated under the pointer and jumped into a cell on release
// would pass every after-the-fact assertion in this file. It would also be
// a preview that lies about what the release is going to do, which is what
// people report as a bug — so the position is read WHILE THE BUTTON IS
// STILL DOWN, and it must already be the cell's.
func TestAGridDragSnapsToTheCellDURINGTheDrag(t *testing.T) {
	ed, c, n, cells := gridFixture(t)
	comp := ed.componentFor(n)
	target := cells[2][1]

	press(c, cells[0][0].X, cells[0][0].Y)
	if !ed.drag.active() {
		t.Fatal("a press on a child of a <Grid> began no drag")
	}
	// The frame counter, re-bound here so it counts only the motion below.
	// Layout.Row/Col are plain int fields outside the property graph, so a
	// snap that writes them and never calls App.Invalidate schedules no
	// frame and the element does not move — with no error anywhere. The
	// bounds assertion below cannot see that, because this test drives
	// Composer.Frame() by hand and would lay out regardless.
	frames := 0
	ed.bindPicking(
		func(x, y int) gooey.Component { return c.Focus().HitTest(x, y) },
		func() { frames++ },
	)
	// Aim at the MIDDLE of the target cell, so the assertion is about the
	// snap rather than about landing exactly on a boundary.
	motion(c, target.X+3, target.Y+1)
	if frames == 0 {
		t.Error("the snap wrote Layout.Row/Col and never asked for a frame: in the running app " +
			"the element does not move, and nothing reports why")
	}
	c.Frame()

	got := comp.(interface{ Bounds() gooey.Rect }).Bounds()
	if got.X != target.X || got.Y != target.Y {
		t.Errorf("mid-drag the element is at (%d,%d), want the top-left of cell (2,1) at "+
			"(%d,%d): the preview is not snapped, so the release will move it again",
			got.X, got.Y, target.X, target.Y)
	}
	// And the DOCUMENT is untouched until the release, exactly as the free
	// drag is: the fast path writes Layout, not markup.
	if v := n.Attrs["Grid.Row"]; v != "0" {
		t.Errorf("mid-drag the document already says Grid.Row=%q: a motion wrote markup", v)
	}

	release(c, target.X+3, target.Y+1)
	if v := n.Attrs["Grid.Row"]; v != "2" {
		t.Errorf("after release Grid.Row is %q, want \"2\"", v)
	}
	if v := n.Attrs["Grid.Col"]; v != "1" {
		t.Errorf("after release Grid.Col is %q, want \"1\"", v)
	}
	if !strings.Contains(ed.source.Get(), `Grid.Row="2"`) {
		t.Errorf("the re-cell is not in the saved document:\n%s", ed.source.Get())
	}
	// No Canvas.Left may have crept in: the parent decides the vocabulary.
	if _, ok := n.Attrs["Canvas.Left"]; ok {
		t.Error("a grid drag wrote Canvas.Left, which a <Grid> silently discards")
	}
}

// TestAGridMotionInsideOneCellCostsNothing.
//
// A terminal reports motion per cell, and a grid cell is many terminal
// cells wide — so most of a grid drag is motion that changes no Row or
// Col. Spending a frame on each would make the snap the expensive path
// exactly where it should be the cheap one.
func TestAGridMotionInsideOneCellCostsNothing(t *testing.T) {
	ed, c, _, cells := gridFixture(t)
	target := cells[1][1]

	press(c, cells[0][0].X, cells[0][0].Y)
	motion(c, target.X+1, target.Y)
	settle(t, c)

	// Re-bind to a FRESH counter, so what is counted is only the motion
	// below and not the press and first motion that set it up. Counting
	// frames REQUESTED rather than frames painted is the pin that matters
	// here: Layout.Row/Col are outside the property graph, so a snap that
	// asked for a frame it did not need would still repaint nothing on a
	// composer the test drives by hand.
	frames := 0
	ed.bindPicking(
		func(x, y int) gooey.Component { return c.Focus().HitTest(x, y) },
		func() { frames++ },
	)
	// Every remaining cell of the same grid cell.
	for dx := 2; dx < target.W; dx++ {
		motion(c, target.X+dx, target.Y)
	}
	if frames != 0 {
		t.Errorf("%d frames were requested for motion that never left cell (1,1): the snap is "+
			"asking for a repaint per terminal cell instead of per grid cell", frames)
	}
	if _, painted := c.Frame(); painted != 0 {
		t.Errorf("motion inside one cell repainted %d components; damage %v", painted, c.Damage())
	}
}

// TestAGridMotionCostsFarLessThanARebuild is the same COMPARISON the free
// drag is pinned by, and for the same reason: a snap that wrote markup per
// motion would look identical on screen and only the count says so.
func TestAGridMotionCostsFarLessThanARebuild(t *testing.T) {
	_, c, _, cells := gridFixture(t)

	press(c, cells[0][0].X, cells[0][0].Y)
	settle(t, c)

	motion(c, cells[1][1].X+3, cells[1][1].Y+1)
	_, perMotion := c.Frame()
	if perMotion == 0 {
		t.Fatal("a snap to another cell repainted nothing: the element did not move, so the " +
			"count below measures nothing")
	}
	if _, again := c.Frame(); again != 0 {
		t.Fatalf("the frame after a snap repainted %d with nothing changed: the count is not "+
			"damage", again)
	}

	release(c, cells[1][1].X+3, cells[1][1].Y+1)
	_, onRelease := c.Frame()

	t.Logf("grid per-motion %d, on-release %d", perMotion, onRelease)
	if perMotion >= onRelease {
		t.Errorf("a snap repainted %d components and the release repainted %d: a motion is the "+
			"CHEAP path — if it costs as much as the rebuild it is writing markup or calling "+
			"rebuild() per pointer report", perMotion, onRelease)
	}
}

// TestTwoElementsMayShareACell is the ACCEPTED cost of snap-to-cell, and
// it is pinned so that a later "helpful" collision rule has something to
// fail against.
//
// Grid renders an overlap and reports nothing, which is why this was the
// acceptable trade: the result is visible on screen. Bumping the second
// element to a free cell would move an element the user did not drag, and
// refusing the drop would make the gesture fail for a reason the pointer
// gives no cue about.
func TestTwoElementsMayShareACell(t *testing.T) {
	ed, c, _, cells := gridFixture(t)
	ed.doc().Kids = append(ed.doc().Kids, &node{
		Elem: "Text", Body: "bbbb",
		Attrs: map[string]string{"Name": "B", "Grid.Row": "2", "Grid.Col": "1"},
	})
	ed.rebuild()
	c.Frame()

	a := ed.doc().Kids[0]
	occupied := cells[2][1]
	press(c, cells[0][0].X, cells[0][0].Y)
	motion(c, occupied.X+3, occupied.Y+1)
	release(c, occupied.X+3, occupied.Y+1)

	if a.Attrs["Grid.Row"] != "2" || a.Attrs["Grid.Col"] != "1" {
		t.Errorf("the dragged element landed at row %q col %q instead of the occupied cell "+
			"(2,1): something moved it out of the way, and the user did not ask for that",
			a.Attrs["Grid.Row"], a.Attrs["Grid.Col"])
	}
	b := ed.doc().Kids[1]
	if b.Attrs["Grid.Row"] != "2" || b.Attrs["Grid.Col"] != "1" {
		t.Errorf("the element that was already in the cell was moved to row %q col %q: a drag "+
			"must never relocate something it did not touch",
			b.Attrs["Grid.Row"], b.Attrs["Grid.Col"])
	}
	if !strings.HasPrefix(ed.status.Get(), "✓") {
		t.Errorf("two elements in one cell stopped the document building: %s", ed.status.Get())
	}
}

// TestAGridDragOutsideTheGridSnapsToTheNEARESTCell.
//
// Overshooting is what a pointer does. Refusing the motion would freeze
// the element at whatever cell it was last in and give the user no cue
// why; snapping to the nearest cell keeps the gesture recoverable without
// letting go.
func TestAGridDragOutsideTheGridSnapsToTheNEARESTCell(t *testing.T) {
	ed, c, n, cells := gridFixture(t)
	comp := ed.componentFor(n)
	last := cells[2][1]

	press(c, cells[0][0].X, cells[0][0].Y)
	// Well past the bottom-right corner of the whole grid.
	motion(c, last.X+last.W+20, last.Y+last.H+20)
	c.Frame()

	got := comp.(interface{ Bounds() gooey.Rect }).Bounds()
	if got.X != last.X || got.Y != last.Y {
		t.Errorf("dragged past the grid the element is at (%d,%d), want the nearest cell (2,1) "+
			"at (%d,%d)", got.X, got.Y, last.X, last.Y)
	}
	release(c, last.X+last.W+20, last.Y+last.H+20)
	if n.Attrs["Grid.Row"] != "2" || n.Attrs["Grid.Col"] != "1" {
		t.Errorf("after release the element is at row %q col %q, want 2,1",
			n.Attrs["Grid.Row"], n.Attrs["Grid.Col"])
	}
}

// TestTheGridCellProbeLeavesTheTreeWhereItFoundIt.
//
// gridCells walks the dragged component through every cell to read the
// slot rectangles back out of the real Grid.Arrange. That mutates the LIVE
// Layout and the LIVE bounds, and a probe that did not put them back would
// leave the element drawn in the last cell it probed until the next frame
// — a flash on every press, and worse for anything reading bounds in
// between.
func TestTheGridCellProbeLeavesTheTreeWhereItFoundIt(t *testing.T) {
	ed, c, n, cells := gridFixture(t)
	comp := ed.componentFor(n)
	before := comp.(interface{ Bounds() gooey.Rect }).Bounds()
	l := gooey.LayoutOf(comp)
	// The fields the probe writes, listed rather than compared whole:
	// gooey.Layout holds a func (the bound-visibility source) and is
	// therefore not comparable.
	type probed struct {
		row, col, rowSpan, colSpan int
		w, h                       int
		hAlign, vAlign             gooey.Align
		margin                     gooey.Thickness
	}
	snap := func() probed {
		return probed{l.Row, l.Col, l.RowSpan, l.ColSpan, l.Width, l.Height,
			l.HAlign, l.VAlign, l.Margin}
	}
	beforeLayout := snap()

	press(c, cells[0][0].X, cells[0][0].Y)
	if !ed.drag.active() {
		t.Fatal("no drag began, so no probe ran and this test asserts nothing")
	}
	if got := comp.(interface{ Bounds() gooey.Rect }).Bounds(); got != before {
		t.Errorf("after the probe the element is arranged at %v, want %v where it started",
			got, before)
	}
	if got := snap(); got != beforeLayout {
		t.Errorf("the probe left the Layout changed: %+v, want %+v", got, beforeLayout)
	}
	// And the next real layout agrees. Composer.Frame lays out
	// unconditionally, so a probe that left the Layout wrong in a way the
	// field check above missed would show up here as the element in a
	// different cell.
	c.Frame()
	if got := comp.(interface{ Bounds() gooey.Rect }).Bounds(); got != before {
		t.Errorf("the frame after the press arranged the element at %v, want %v: a press that "+
			"moves nothing moved it", got, before)
	}
}

// TestADragSurvivesThePointerLeavingTheDesigner.
//
// The press captures the pane implicitly (DispatchMouse sets the captor
// from the frozen-retargeted hit before routing), so motion outside the
// designer still arrives. Without that a drag would stop dead at the pane
// edge, which is where a user naturally overshoots.
func TestADragSurvivesThePointerLeavingTheDesigner(t *testing.T) {
	ed, c, _ := dragFixture(t)
	a := docKid(ed, 0)
	b := a.(interface{ Bounds() gooey.Rect }).Bounds()
	pane := findPreview(c.Root())
	pb := pane.(interface{ Bounds() gooey.Rect }).Bounds()

	press(c, b.X, b.Y)
	// Well outside the designer, over the properties pane.
	if !motion(c, pb.X+pb.W+10, b.Y+2) {
		t.Fatal("a motion outside the designer was not delivered to the drag: the implicit " +
			"capture is not holding, so a drag stops at the pane edge")
	}
	c.Frame()
	if !ed.drag.moved {
		t.Error("the drag recorded no movement from a motion outside the pane")
	}
	// Back inside, and the position follows the pointer rather than the
	// last in-bounds cell — the offset is computed from the PRESS, so a
	// dropped or coalesced report cannot make it drift.
	motion(c, b.X+2, b.Y+2)
	c.Frame()
	moved := a.(interface{ Bounds() gooey.Rect }).Bounds()
	if moved.X != b.X+2 || moved.Y != b.Y+2 {
		t.Errorf("after wandering out and back the element is at %v, want %d,%d",
			moved, b.X+2, b.Y+2)
	}
	release(c, b.X+2, b.Y+2)
}

// TestDraggingIsDESIGNModeOnly — in LIVE the clicks belong to the guest.
func TestDraggingIsDESIGNModeOnly(t *testing.T) {
	ed, c, _ := dragFixture(t)
	a := docKid(ed, 0)
	b := a.(interface{ Bounds() gooey.Rect }).Bounds()

	pressD(c)
	c.Frame()
	if ed.design.Get() {
		t.Fatal("'d' did not reach ToggleMode")
	}
	b = a.(interface{ Bounds() gooey.Rect }).Bounds()
	press(c, b.X, b.Y)
	motion(c, b.X+4, b.Y+4)
	release(c, b.X+4, b.Y+4)
	if ed.drag.active() {
		t.Error("a LIVE-mode press began a drag")
	}
	if got := ed.doc().Kids[0].Attrs["Canvas.Left"]; got != "1" {
		t.Errorf("a LIVE-mode drag moved the element (Canvas.Left=%q)", got)
	}
}

// TestADragNeverGoesNegative. Canvas.Left cannot be negative in the
// document, so letting the live offset go there would put the element
// somewhere the release could not record — it would snap back on commit.
func TestADragNeverGoesNegative(t *testing.T) {
	ed, c, _ := dragFixture(t)
	a := docKid(ed, 0)
	b := a.(interface{ Bounds() gooey.Rect }).Bounds()

	press(c, b.X, b.Y)
	motion(c, b.X-20, b.Y-20)
	c.Frame()
	l := gooey.LayoutOf(a)
	if l.Left < 0 || l.Top < 0 {
		t.Errorf("the live offset went negative (%d,%d): the release cannot record it and the "+
			"element would snap back on commit", l.Left, l.Top)
	}
	release(c, b.X-20, b.Y-20)
	if got := ed.doc().Kids[0].Attrs["Canvas.Left"]; strings.HasPrefix(got, "-") {
		t.Errorf("a negative Canvas.Left reached the document: %q", got)
	}
}
