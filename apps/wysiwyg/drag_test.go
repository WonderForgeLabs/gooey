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

// TestOnlyAChildOfACanvasCanBeDragged is the boundary, asserted for all
// three parents rather than only the one that works.
//
// Free geometry is a property of the PARENT. A child of a <Grid> has
// Grid.Row/Grid.Col, so a drag there means RE-CELLING; a child of a
// <VStack> has no geometry at all — its position is its index, so a drag
// means REORDER. Both are different gestures with different answers and
// neither is implemented. Refusing is the honest behaviour: writing
// Canvas.Left onto a child of a VStack would be silently discarded by
// applyLayout, which is the defect the catalog work exists to delete.
func TestOnlyAChildOfACanvasCanBeDragged(t *testing.T) {
	for _, tc := range []struct {
		parent string
		kind   string
		drag   bool
	}{
		{"Canvas", "free", true},
		{"VStack", "reorder", false},
		{"Grid", "re-cell", false},
	} {
		ed, c, _ := designerPageCounting(t)
		ed.doc().Elem = tc.parent
		ed.doc().Kids = []*node{
			{Elem: "Text", Body: "aaaa", Attrs: map[string]string{"Name": "A"}},
		}
		if tc.parent == "Canvas" {
			ed.doc().Kids[0].Attrs["Canvas.Left"] = "1"
			ed.doc().Kids[0].Attrs["Canvas.Top"] = "1"
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

		// The refusal has to be COMPLETE: no Canvas.Left may appear on a
		// child whose parent cannot honour it.
		if !tc.drag {
			if _, ok := n.Attrs["Canvas.Left"]; ok {
				t.Errorf("dragging under a <%s> wrote Canvas.Left, which applyLayout discards",
					tc.parent)
			}
		}
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
