package main

import (
	"testing"

	"github.com/WonderForgeLabs/gooey"
)

// What the full-height column COSTS, measured rather than assumed.
//
// The rail's ground is now a gooey.HasBackground container, and that is
// not free by construction: a container with a background handle
// overpaints its subtree when it repaints, and the Composer's z-ordered
// pass then repaints the subtree above it in the same frame. The obvious
// worry is that the rail's chrome has quietly become a second paint on
// every gesture that touches it.
//
// It has not, and the reason is the one the framework is built around:
// the column's Render reads nothing. The picture is a computed over the
// selection and the focus, so IT is what those dirty; the VStack behind
// it is clean and stays clean, and a clean node does not repaint however
// tall it is. The background is painted once and then simply continues
// to exist.
//
// Asserted on FOCUS rather than on the selection deliberately. Moving
// ed.activitySel also swaps which pane the side bar shows, so its frame
// costs eleven repaints of which the rail is one — a number that says
// almost nothing about the rail, and that would go red for any edit to
// the side bar while carrying a message about the column. Focus moves
// the two components the framework promises it moves and nothing else,
// which is the isolation this claim needs. (For the record, the
// selection frame measures the same eleven WITH the column as it did
// without it: this change costs zero repaints, not one.)
func TestFocusingTheRailRepaintsThePictureAndNotItsColumn(t *testing.T) {
	ed, root := buildPage(t)
	c := gooey.NewComposer(root, 150, 44)
	t.Cleanup(c.Close)
	c.Frame() // first frame paints everything; the claim is about a later one

	if _, painted := c.Frame(); painted != 0 {
		t.Fatalf("a settled page repainted %d components; this test cannot measure "+
			"one gesture against a page that is still moving", painted)
	}

	rail, ok := ed.ctx.Named["ActivityBar"]
	if !ok {
		t.Fatal("the page mounted no element named ActivityBar")
	}
	// The focus stop is INSIDE the column, not the column itself: the
	// VStack is a surface with no behaviour, and the Segmented under it
	// is what owns the selection, the keys and the focus. Reaching it
	// through ChildComponents rather than by type keeps this test about
	// damage rather than about how Builder is spelled.
	kids, ok := rail.(gooey.Container)
	if !ok || len(kids.ChildComponents()) == 0 {
		t.Fatalf("the rail is %T with no children; there is no picture to focus", rail)
	}
	pic := kids.ChildComponents()[0]

	// The rail holds focus at startup, so the gesture has to start by
	// moving OFF it. That is not setup to skip past — it is half the
	// measurement, because a column that joined the picture's damage
	// would show up here first.
	if c.Focus().Focused() != pic {
		t.Fatalf("the rail does not start focused (it is %T); this test moves focus "+
			"off it and back, and neither half means anything from another start",
			c.Focus().Focused())
	}
	c.Focus().FocusNext()
	_, away := c.Frame()

	if !c.Focus().SetFocus(pic) {
		t.Fatal("focus would not return to the rail, so the claim this test makes " +
			"about the frame that returns it would be vacuous")
	}
	_, back := c.Frame()

	// TWO, both times: the component gaining focus and the one losing
	// it. That is the framework's own promise — FocusState is an
	// ordinary source property, which is the whole reason moving focus
	// repaints exactly two components — and the point here is that the
	// column is not a third.
	//
	// Higher means the background handle has joined the picture's damage
	// and the rail's chrome now redraws on every focus change. Lower
	// means the picture stopped subscribing to focus and the marker no
	// longer dims when the rail loses it, which
	// TestTheMarkerDimsWhenTheRailLosesFocus checks at the image level
	// and this checks at the frame level.
	const want = 2
	if away != want || back != want {
		t.Errorf("focus off the rail repainted %d and back onto it %d, want %d each "+
			"(the picture and its counterpart). The full-height column behind it "+
			"must stay clean: it reads nothing, so nothing should dirty it",
			away, back, want)
	}
}
