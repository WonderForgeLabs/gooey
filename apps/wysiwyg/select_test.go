package main

import (
	"strings"
	"testing"
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/markup"
)

// pressEsc is the SelectParent gesture, through the page's own KeyBinding
// rather than by calling ed.selectParent: a binding on the wrong element
// never fires, and a direct call would pass for a page that has none.
func pressEsc(c *gooey.Composer) bool {
	return c.Handle(input.KeyOf(input.KeyEvent{Key: input.KeyEsc}))
}

// Click-to-select, against the SHIPPED page and the real editor.
//
// "i can't select the item on designer" was one absent layer rather than
// three bugs: the pane implemented gooey.Frozen and nothing else, so the
// press the framework carefully retargeted to it had nowhere to land.
// Everything below is therefore end-to-end through Composer.HandleMouse —
// the same call the terminal's SGR report makes — because the part worth
// testing is the routing, and a direct call to selectAt would skip it.

// designerPage is designPage plus the wiring main() does: the composer is
// what can answer HitTest, and it does not exist until after the page is
// built.
func designerPage(t *testing.T) (*editor, *gooey.Composer) {
	ed, c, _ := designerPageCounting(t)
	return ed, c
}

// designerPageCounting also returns how many frames the editor ASKED for.
//
// The counter is the pin on the trap that would otherwise cost an hour:
// Layout.Left/Top are plain int fields, so a drag that writes one and
// does not call App.Invalidate schedules no frame and the element does
// not move — with no error. In a test the frame is driven by hand, so
// without counting the request the assertion would pass on code that
// never asked.
func designerPageCounting(t *testing.T) (*editor, *gooey.Composer, *int) {
	t.Helper()
	ed, c := designPage(t)
	frames := 0
	ed.bindPicking(
		func(x, y int) gooey.Component { return c.Focus().HitTest(x, y) },
		func() { frames++ },
	)
	return ed, c, &frames
}

// docKid is the component built for root.Kids[i].
// docKid is the component built for the USER'S ROOT's i'th child.
//
// Two hops, and the first one is the wrapping model: docRoot is the built
// SURFACE, whose only child is the user's root, whose children are the
// document nodes.
func docKid(ed *editor, i int) gooey.Component {
	return childComponents(childComponents(ed.docRoot)[0])[i]
}

// under is the tests' OWN containment check, written out rather than
// reusing componentPath: it guards the preconditions of the walk, and a
// guard sharing the walk's implementation cannot catch the walk being
// wrong about what is inside what.
func under(root, w gooey.Component) bool {
	if root == w {
		return true
	}
	for _, k := range childComponents(root) {
		if under(k, w) {
			return true
		}
	}
	return false
}

func press(c *gooey.Composer, x, y int) bool {
	return c.HandleMouse(input.MouseEvent{
		Kind: input.MousePress, Button: input.ButtonLeft, X: x, Y: y,
	})
}

// clickClock PINS the click clock and returns the way to advance it.
//
// The count on a synthesized click is a function of wall time
// (FocusManager.clickCount), and a test that raced 400ms of it would be a
// test that fails on a loaded machine and blames the designer. With the
// clock stopped, two clicks in a row are ALWAYS a double click and
// advancing past the interval always breaks the sequence — so which
// gesture a test is making becomes a decision it states rather than a
// timing it hopes for.
//
// Taken from c.Focus() at call time rather than captured: the composer
// builds a new FocusManager whenever the tree is swapped.
func clickClock(c *gooey.Composer) func(time.Duration) {
	at := time.Unix(0, 0)
	c.Focus().Now = func() time.Time { return at }
	return func(d time.Duration) { at = at.Add(d) }
}

// singleClick is a press and its release. A click is SYNTHESIZED by the
// dispatcher on the release, so a test that only presses never produces
// one at all — which is why most of this file can press alone and stay
// unaffected by the drill gesture.
func singleClick(c *gooey.Composer, x, y int) {
	press(c, x, y)
	release(c, x, y)
}

// doubleClick is two clicks with no time between them, on a pinned clock.
//
// IT DELIBERATELY ASSERTS NOTHING. The count that makes the second click
// a double click is the framework's, and a helper that checked it would
// be checking the framework rather than the editor — while giving every
// test that calls it a second way to fail that has nothing to do with
// what it claims. What proves the gesture really arrived is the SINGLE
// click arm in TestADoubleClickDrillsExactlyOneLevel: one click provably
// does not drill, so a pair that does drill can only have arrived as
// Count 2.
//
// Call clickClock first, and advance past the interval between separate
// gestures — two double-clicks back to back with no advance are four
// clicks counting 1,2,1,2, which is two double-clicks only by accident.
func doubleClick(t *testing.T, c *gooey.Composer, x, y int) {
	t.Helper()
	singleClick(c, x, y)
	singleClick(c, x, y)
}

// settle frames until nothing repaints, so a later count is damage and
// not leftovers from composition.
func settle(t *testing.T, c *gooey.Composer) {
	t.Helper()
	for i := 0; i < 5; i++ {
		if _, painted := c.Frame(); painted == 0 {
			return
		}
	}
	t.Fatal("the composition never settled; no damage count taken from it means anything")
}

// paneOrigin is the designer's top-left cell, which is also where the
// document's own root is arranged (Pane.Arrange).
func paneOrigin(t *testing.T, c *gooey.Composer) (int, int) {
	t.Helper()
	pane := findPreview(c.Root())
	if pane == nil {
		t.Fatal("the shipped page does not mount the designer")
	}
	b := pane.(interface{ Bounds() gooey.Rect }).Bounds()
	if b.W == 0 || b.H == 0 {
		t.Fatalf("the designer was never arranged (%v)", b)
	}
	return b.X, b.Y
}

// widen gives the starting document's <Text> a size.
//
// A <Text> carries its content as its BODY, node.markup emits no body, and
// a zero-size component is never hit (component.go's hitTest) — so the
// editor's own T1 is on screen as nothing at all and cannot be pressed.
// That is a real gap in the editor rather than a test inconvenience: a
// palette that adds an unpressable element is the next thing to fix, and
// it is noted in the report rather than papered over here.
func widen(t *testing.T, ed *editor, c *gooey.Composer) {
	t.Helper()
	ed.doc().Kids[0].Attrs["Width"] = "6"
	ed.doc().Kids[0].Attrs["Height"] = "1"
	ed.rebuild()
	if !strings.HasPrefix(ed.status.Get(), "✓") {
		t.Fatalf("the widened document does not build: %s", ed.status.Get())
	}
	c.Frame()
}

// TestClickingTheDesignerSelectsWhatIsUnderThePointer is the feature.
//
// The starting document is <Canvas><Text T1/><Button B1/></Canvas>, so
// there are two nodes to tell apart and the assertion can be made in both
// directions — a test that only ever clicked one would pass for an
// implementation that returned a constant.
func TestClickingTheDesignerSelectsWhatIsUnderThePointer(t *testing.T) {
	ed, c := designerPage(t)
	widen(t, ed, c)
	btn := findButton(c.Root())
	if btn == nil {
		t.Fatal("the starting document has no Button to click")
	}
	b := btn.Bounds()
	if b.W == 0 || b.H == 0 {
		t.Fatalf("the Button was never arranged (%v): the pointer cannot be over it", b)
	}

	ed.setSelection(ed.doc().Kids[0])
	if !press(c, b.X, b.Y) {
		t.Fatal("a press in the designer was not consumed: it reached nothing that handles it")
	}
	if ed.sel != ed.doc().Kids[1] {
		t.Fatalf("a press on the Button selected %s, want the <Button> (<Button Name=%q>)",
			nodeName(ed.sel), ed.doc().Kids[1].Attrs["Name"])
	}

	// And back to the Text, which is what makes the 1 above a measurement.
	x, y := paneOrigin(t, c)
	tx, ty := x+2, y+1 // Canvas.Left=2, Canvas.Top=1
	if got := c.Focus().HitTest(tx, ty); !under(docKid(ed, 0), got) {
		t.Fatalf("(%d,%d) is not on the <Text> (%T): this press asserts nothing", tx, ty, got)
	}
	press(c, tx, ty)
	if ed.sel != ed.doc().Kids[0] {
		t.Errorf("a press on the Text selected %s, want the <Text>", nodeName(ed.sel))
	}
}

// nestedPage is a document whose second node has a child of its own:
//
//	<Canvas Root>
//	  <Text T1/>
//	  <Border B1><Text Inner/></Border>
//
// <Border> takes exactly one child, which is why addSelected seeds one.
// It returns the cells of the inner <Text> and of the Border's own chrome.
func nestedPage(t *testing.T) (ed *editor, c *gooey.Composer, innerX, innerY, chromeX, chromeY int) {
	t.Helper()
	ed, c = designerPage(t)
	ed.doc().Kids = []*node{
		{Elem: "Text", Attrs: map[string]string{"Name": "T1", "Canvas.Left": "0", "Canvas.Top": "0", "Width": "6", "Height": "1"}},
		{Elem: "Border", Attrs: map[string]string{"Name": "B1", "Canvas.Left": "0", "Canvas.Top": "4"},
			Kids: []*node{{Elem: "Text", Attrs: map[string]string{"Name": "Inner", "Width": "6", "Height": "1"}}}},
	}
	ed.rebuild()
	if !strings.HasPrefix(ed.status.Get(), "✓") {
		t.Fatalf("the fixture document does not build: %s", ed.status.Get())
	}
	c.Frame()

	border := docKid(ed, 1)
	kids := childComponents(border)
	if len(kids) == 0 {
		t.Fatal("the <Border> built with no child: there is no nested hit to make")
	}
	cb := kids[0].(interface{ Bounds() gooey.Rect }).Bounds()
	bb := border.(interface{ Bounds() gooey.Rect }).Bounds()
	if cb.W == 0 || bb.W == 0 {
		t.Fatalf("nothing was arranged: border %v child %v", bb, cb)
	}
	// Confirm the inner cell really is nested, or every assertion made
	// from it is about a hit that never descended.
	if got := c.Focus().HitTest(cb.X, cb.Y); got == border {
		t.Fatalf("HitTest returned the <Border> itself at the child's cell (%d,%d): "+
			"a press there does not exercise the ancestor walk", cb.X, cb.Y)
	}
	return ed, c, cb.X, cb.Y, bb.X, bb.Y
}

// TestAPressSelectsTheOUTERMOSTNodeInTheScope is the selection POLICY, and
// its claim has now inverted TWICE. The history matters because the test
// name is in two PRs' reviews and a reader tracking it will otherwise
// reconstruct it wrong:
//
//  1. TestAPressOnANestedChildSelectsItsTopLevelNode — while ed.sel was
//     an int index into root.Kids, a flat index could not name a nested
//     node, so a press had to climb to the top-level kid.
//  2. TestAPressSelectsTheDEEPESTNodeItLandsOn — when the surface became
//     chrome, the user's own root became chain[1] and everything they
//     place is below it, so climbing to the top selected the root
//     whatever you clicked. The policy went to the deepest node.
//  3. This one. Deepest-first has a hole that gets worse as documents
//     nest: a <Border> is covered entirely by its own child, so every
//     press inside it selected the <Text> and the Border was reachable
//     only through its one-cell chrome — effectively unselectable, and
//     after the drag work effectively unmovable. Shallow-first (Blend,
//     Figma, Illustrator) has no such hole, because double-click drills
//     and Escape walks back up.
//
// The container's own chrome still selects the container under all three,
// which is why that arm alone could never have caught the change.
func TestAPressSelectsTheOUTERMOSTNodeInTheScope(t *testing.T) {
	ed, c, ix, iy, bx, by := nestedPage(t)

	ed.setSelection(ed.doc().Kids[0])
	press(c, ix, iy)
	if ed.sel != ed.doc().Kids[1] {
		t.Errorf("a press on the <Text> INSIDE the <Border> selected %s, want the <Border>: "+
			"a container covered by its own child has to be selectable by pressing it",
			nodeName(ed.sel))
	}

	// The container's own chrome — the border rune in its top-left corner
	// — selects the container too, which is the case that agrees under
	// every policy this file has had.
	ed.setSelection(ed.doc().Kids[0])
	press(c, bx, by)
	if ed.sel != ed.doc().Kids[1] {
		t.Errorf("a press on the <Border>'s own chrome selected %s, want the <Border>", nodeName(ed.sel))
	}

	// AND THE INNER NODE IS STILL REACHABLE, which is what makes the
	// inversion an improvement rather than a trade. Without this arm the
	// test above passes for a designer that can no longer select anything
	// nested at all.
	ed.setSelection(ed.doc().Kids[0])
	doubleClick(t, c, ix, iy)
	if ed.sel != ed.doc().Kids[1].Kids[0] {
		t.Errorf("a double-click on the <Text> inside the <Border> selected %s, want the inner "+
			"<Text>: shallow-first with no way down is a designer that cannot reach its own "+
			"nesting", nodeName(ed.sel))
	}
}

// TestADoubleClickDrillsExactlyOneLevel.
//
// One level, not all the way to the deepest hit — drilling straight to the
// bottom would put the intermediate containers back out of reach, which is
// the defect shallow-first exists to remove. Three nested nodes is the
// smallest fixture where "one level" and "all the way" differ.
func TestADoubleClickDrillsExactlyOneLevel(t *testing.T) {
	ed, c := designerPage(t)
	ed.doc().Kids = []*node{
		{Elem: "Border", Attrs: map[string]string{"Name": "B1", "Canvas.Left": "0", "Canvas.Top": "0"},
			Kids: []*node{{Elem: "Border", Attrs: map[string]string{"Name": "B2"},
				Kids: []*node{{Elem: "Text", Attrs: map[string]string{"Name": "Deep", "Width": "6", "Height": "1"}}}}}},
	}
	ed.rebuild()
	if !strings.HasPrefix(ed.status.Get(), "✓") {
		t.Fatalf("the fixture document does not build: %s", ed.status.Get())
	}
	c.Frame()

	outer := ed.doc().Kids[0]
	mid := outer.Kids[0]
	deep := mid.Kids[0]
	b := ed.docCtx.Named["Deep"].(interface{ Bounds() gooey.Rect }).Bounds()
	if b.W == 0 {
		t.Fatalf("the inner <Text> was never arranged (%v)", b)
	}

	advance := clickClock(c)
	ed.setSelection(nil)

	// A SINGLE click stops at the outermost. This is the discriminator:
	// without it every assertion below passes for an implementation that
	// selects the deepest hit on any click at all.
	singleClick(c, b.X, b.Y)
	if ed.sel != outer {
		t.Fatalf("a single click three levels deep selected %s, want the outer <Border>",
			nodeName(ed.sel))
	}

	advance(time.Second)
	doubleClick(t, c, b.X, b.Y)
	if ed.sel != mid {
		t.Fatalf("one double-click selected %s, want the MIDDLE <Border>: a drill that goes "+
			"straight to the deepest hit puts the containers back out of reach",
			nodeName(ed.sel))
	}

	// And the gesture COMPOSES: the scope moved with the selection, so the
	// next double-click drills from there rather than repeating itself.
	advance(time.Second)
	doubleClick(t, c, b.X, b.Y)
	if ed.sel != deep {
		t.Errorf("a second double-click selected %s, want the innermost <Text>: the drill "+
			"scope did not follow the selection down", nodeName(ed.sel))
	}

	// A third is a no-op rather than a fall-off-the-end.
	advance(time.Second)
	doubleClick(t, c, b.X, b.Y)
	if ed.sel != deep {
		t.Errorf("a double-click with nothing below it selected %s, want the innermost <Text>",
			nodeName(ed.sel))
	}
}

// TestEscapeSelectsTheParentAndTheNextClickAgrees.
//
// Escape's spelling is Microsoft's — "To select the parent of a current
// selection in the designer, press the ESC key" — and NOT Figma's, where
// Escape deselects entirely. The difference is load-bearing here: a
// designer whose Escape clears the selection gives no way back up a tree
// a double-click drilled into.
//
// The second half is the part a naive implementation fails. Escape has to
// move the DRILL SCOPE, not only the selection: if it moved the selection
// up and left the scope where it was, the very next single click at the
// same cell would drill straight back in and Escape would look like it
// did nothing.
func TestEscapeSelectsTheParentAndTheNextClickAgrees(t *testing.T) {
	ed, c, ix, iy, _, _ := nestedPage(t)
	border := ed.doc().Kids[1]
	inner := border.Kids[0]

	advance := clickClock(c)
	ed.setSelection(nil)
	doubleClick(t, c, ix, iy)
	if ed.sel != inner {
		t.Fatalf("the drill did not reach the inner <Text> (%s); escaping from it proves nothing",
			nodeName(ed.sel))
	}

	if !pressEsc(c) {
		t.Fatal("esc was not consumed: the SelectParent binding is not on the page root")
	}
	if ed.sel != border {
		t.Fatalf("esc selected %s, want the <Border> that holds it", nodeName(ed.sel))
	}

	// THE COHERENCE ARM.
	advance(time.Second)
	singleClick(c, ix, iy)
	if ed.sel != border {
		t.Errorf("after esc, a click at the same cell selected %s, want the <Border> again: "+
			"esc moved the selection out and left the drill scope in, so the click drilled "+
			"straight back", nodeName(ed.sel))
	}

	// Up to the user's root, and then esc STOPS. The root's parent is the
	// surface, which is chrome and must never be selected.
	pressEsc(c)
	if ed.sel != ed.doc() {
		t.Fatalf("esc from the <Border> selected %s, want the user's root", nodeName(ed.sel))
	}
	pressEsc(c)
	if ed.sel != ed.doc() {
		t.Errorf("esc at the user's root selected %s: it walked into the design surface, which "+
			"is the editor's workspace and is never saved", nodeName(ed.sel))
	}
}

// TestClickingOutsideTheScopePopsOutOfIt.
//
// Having drilled into one <Border>, a click on a SIBLING must select the
// sibling — not nothing, and not something inside it. This is the "click
// outside the group to leave it" every direct-manipulation editor has, and
// here it falls out of the scope walk rather than needing a reset rule,
// which is the whole reason the scope is derived from the selection
// instead of being a field somebody has to remember to clear.
// The fixture nests the two siblings inside a COMMON container:
//
//	<Canvas Root>
//	  <Canvas Group>
//	    <Border B1><Text In1/></Border>
//	    <Border B2><Text In2/></Border>
//
// which is what makes the assertion sharp. With them at the top level,
// popping out ONE level to the shared container and popping out ALL the
// way to the root give the same answer, so the test would pass for an
// implementation that reset the scope to the root whenever it missed.
// Inside a group the two differ: one level selects <Border B2>, all the
// way selects <Canvas Group>.
func TestClickingOutsideTheScopePopsOutOfIt(t *testing.T) {
	ed, c := designerPage(t)
	ed.doc().Kids = []*node{
		{Elem: "Canvas", Attrs: map[string]string{"Name": "Group", "Canvas.Left": "0", "Canvas.Top": "0"},
			Kids: []*node{
				{Elem: "Border", Attrs: map[string]string{"Name": "B1", "Canvas.Left": "0", "Canvas.Top": "0"},
					Kids: []*node{{Elem: "Text", Attrs: map[string]string{"Name": "In1", "Width": "6", "Height": "1"}}}},
				{Elem: "Border", Attrs: map[string]string{"Name": "B2", "Canvas.Left": "0", "Canvas.Top": "6"},
					Kids: []*node{{Elem: "Text", Attrs: map[string]string{"Name": "In2", "Width": "6", "Height": "1"}}}},
			}},
	}
	ed.rebuild()
	if !strings.HasPrefix(ed.status.Get(), "✓") {
		t.Fatalf("the fixture document does not build: %s", ed.status.Get())
	}
	c.Frame()

	group := ed.doc().Kids[0]
	one, two := group.Kids[0], group.Kids[1]
	b1 := ed.docCtx.Named["In1"].(interface{ Bounds() gooey.Rect }).Bounds()
	b2 := ed.docCtx.Named["In2"].(interface{ Bounds() gooey.Rect }).Bounds()
	if b1.W == 0 || b2.W == 0 {
		t.Fatalf("the fixture was not arranged: %v %v", b1, b2)
	}

	advance := clickClock(c)
	ed.setSelection(nil)
	// Two levels down: the group, then the first <Border>, then its Text.
	doubleClick(t, c, b1.X, b1.Y)
	advance(time.Second)
	doubleClick(t, c, b1.X, b1.Y)
	if ed.sel != one.Kids[0] {
		t.Fatalf("the drill into the first <Border> reached %s, want its inner <Text>; the click "+
			"below proves nothing", nodeName(ed.sel))
	}

	advance(time.Second)
	singleClick(c, b2.X, b2.Y)
	if ed.sel != two {
		t.Errorf("drilled inside the first <Border>, a click on the SECOND selected %s, want "+
			"<Border B2>: the scope popped to the wrong level — one level out is the group the "+
			"two share, and anything further is a reset the user did not ask for",
			nodeName(ed.sel))
	}
}

// TestAPressOnBareCanvasSelectsNothing pins the decided outcome.
//
// The surface is the editor's WORKSPACE, not part of the document — it is
// never serialized, so selecting it would point the properties grid at
// something the user cannot save and offer attributes that never reach
// their file. An empty grid is the honest answer.
//
// This REPLACES the earlier "a press on nothing selects the container".
// That was decided while the surface was still the user's own root, where
// selecting it was informative; under the wrapping model the same press
// would select chrome. The behaviour change is deliberate and is not
// shimmed for compatibility.
func TestAPressOnBareCanvasSelectsNothing(t *testing.T) {
	ed, c := designerPage(t)

	// THE ROOT HAS TO BE GIVEN A SIZE FOR THIS TEST TO EXIST AT ALL, and
	// that is a finding rather than a fixture detail: a <Canvas> measures
	// the space it is offered, so the user's root fills the surface and
	// there is NO bare surface to press. With the default document every
	// press lands on the root or on something in it, and selecting the
	// root is correct — it is a real element the user saves.
	//
	// So the empty state is reachable exactly when the root does not cover
	// the surface. Sizing it here is what creates the uncovered region.
	ed.doc().Attrs["Width"] = "10"
	ed.doc().Attrs["Height"] = "4"
	ed.doc().Attrs["HAlign"] = "Start"
	ed.doc().Attrs["VAlign"] = "Start"
	ed.rebuild()
	c.Frame()

	rootComp := childComponents(ed.docRoot)[0]
	rb := rootComp.(interface{ Bounds() gooey.Rect }).Bounds()
	if rb.W == 0 || rb.H == 0 {
		t.Fatalf("the user's root was never arranged (%v)", rb)
	}
	// A cell inside the designer but OUTSIDE the user's root.
	x, y := rb.X+rb.W+2, rb.Y+rb.H+2
	pane := findPreview(c.Root())
	pb := pane.(interface{ Bounds() gooey.Rect }).Bounds()
	if x >= pb.X+pb.W || y >= pb.Y+pb.H {
		t.Fatalf("the probe cell (%d,%d) is outside the designer %v", x, y, pb)
	}
	hit := c.Focus().HitTest(x, y)
	if under(rootComp, hit) {
		t.Fatalf("(%d,%d) is inside the user's root after all (%T): this press asserts nothing",
			x, y, hit)
	}

	ed.setSelection(ed.doc().Kids[1])
	if !press(c, x, y) {
		t.Error("a press on the designer's bare surface was not consumed; in DESIGN mode the " +
			"picture swallows its own clicks whether or not they hit a node")
	}
	if ed.sel != nil {
		t.Errorf("a press on the bare surface selected %s, want nothing", nodeName(ed.sel))
	}
	// Nothing selected has to be USABLE, not merely stored: the inspector
	// resolves to no target and offers no rows, rather than panicking or
	// falling back to the surface.
	if _, _, target := ed.target(); target != nil {
		t.Errorf("with nothing selected the inspector still targets <%s>", target.Elem)
	}
	if rows := ed.attrRows(); len(rows) != 0 {
		t.Errorf("with nothing selected the properties grid offers %d rows", len(rows))
	}
	// And the keyboard must be able to get BACK out of the empty state, or
	// a stray click on the background would strand the user.
	ed.selectNext(1)
	if ed.sel != ed.doc().Kids[0] {
		t.Errorf("ctrl+n out of the empty state selected %s, want the first node", nodeName(ed.sel))
	}
}

// TestAPressOnTheUsersRootSelectsIt is the other half, and it is the case
// the DEFAULT document actually produces.
//
// A <Canvas> root fills the surface, so a press on what looks like empty
// background is a press on the root — a real element that reaches the
// saved file. Selecting it is right; it is only the SURFACE that must
// select nothing.
func TestAPressOnTheUsersRootSelectsIt(t *testing.T) {
	ed, c := designerPage(t)
	x, y := paneOrigin(t, c)
	hit := c.Focus().HitTest(x, y)
	for i := range ed.doc().Kids {
		if under(docKid(ed, i), hit) {
			t.Fatalf("(%d,%d) is on a child, not the root: this press asserts nothing", x, y)
		}
	}
	ed.setSelection(nil)
	press(c, x, y)
	if ed.sel != ed.doc() {
		t.Errorf("a press on the root's own area selected %s, want the user's root <%s>",
			nodeName(ed.sel), ed.doc().Elem)
	}
}

// TestTheDesignerSelectsInDESIGNModeOnly is the mode guard, and the
// EMPTY-CANVAS press is the arm with teeth.
//
// A press on the Button in LIVE mode is consumed by the Button, so a pane
// with no guard at all would still pass that arm. A press on the Canvas's
// background is consumed by nobody and BUBBLES up to the pane — which is
// exactly the path a missing guard takes to steal a live app's clicks.
func TestTheDesignerSelectsInDESIGNModeOnly(t *testing.T) {
	ed, c := designerPage(t)
	if !ed.design.Get() {
		t.Fatal("the editor did not start in DESIGN mode")
	}
	pressD(c)
	c.Frame()
	if ed.design.Get() {
		t.Fatal("'d' did not reach ToggleMode; the LIVE arm below would be a DESIGN arm")
	}

	x, y := paneOrigin(t, c)
	ed.setSelection(ed.doc().Kids[1])
	press(c, x, y)
	if ed.sel != ed.doc().Kids[1] {
		t.Errorf("a LIVE-mode press on the document's background moved the selection to %s: "+
			"the designer is taking clicks that belong to the app being tried", nodeName(ed.sel))
	}
	if btn := findButton(c.Root()); btn != nil {
		b := btn.Bounds()
		press(c, b.X, b.Y)
		if ed.sel != ed.doc().Kids[1] {
			t.Errorf("a LIVE-mode press on the Button moved the selection to %s", nodeName(ed.sel))
		}
	}

	// Discrimination: the gesture must WORK once the mode goes back, or the
	// LIVE assertions above pass for something that never worked. A press
	// on bare canvas is no longer a usable probe for that — it now clears
	// the selection in DESIGN, which is indistinguishable from "the press
	// did nothing" — so the probe presses an ELEMENT instead.
	pressD(c)
	c.Frame()
	if btn := findButton(c.Root()); btn != nil {
		b := btn.Bounds()
		ed.setSelection(nil)
		press(c, b.X, b.Y)
		if ed.sel != ed.doc().Kids[1] {
			t.Fatalf("back in DESIGN a press on the Button selected %s, want the <Button>: the "+
				"LIVE assertions above pass for a gesture that never worked", nodeName(ed.sel))
		}
	}
}

// TestSelectingCostsTheSameFiveComponentsWhicheverGestureMovesIt is the
// damage pin, and it is taken on a CONTROLLED document on purpose.
//
// A selection change is not a document change: the markup is identical, so
// nothing in the designer may repaint. What may repaint is what is DERIVED
// from the selection — the PROPERTIES grid, which is AttrsFor(selection).
//
// The shipped document's two nodes are a <Text> and a <Button>, and those
// have DIFFERENT NUMBERS OF ATTRIBUTES, so selecting between them makes the
// grid re-row: ItemsView rebuilds its rows, every new row component is new
// and paints, and the changed bounds force the panel and the page's root
// Grid above them. Measured, that is 49 components — all of it correct, and
// all of it a function of the catalog rather than of this gesture. Pinning
// 49 would make an unrelated attribute added to <Button> fail this test and
// blame the pointer.
//
// So the number is taken where it is a property of the MECHANISM: two
// nodes of the same element, where the row shape does not change and the
// only damage is what genuinely differs. Five, itemised in the constant.
//
// The pointer and the keyboard are asserted EQUAL rather than each against
// the constant, which is the catalog-independent half: ctrl+n and a press
// are one call to setSelection, and if they ever cost different numbers one
// of them stopped using it.
func TestSelectingCostsTheSameFiveComponentsWhicheverGestureMovesIt(t *testing.T) {
	ed, c := designerPage(t)
	// Two <Text> nodes: one element, one attribute set, so the grid re-rows
	// nothing and the damage is the values that actually differ.
	ed.doc().Kids = []*node{
		{Elem: "Text", Attrs: map[string]string{"Name": "T1", "Canvas.Left": "2", "Canvas.Top": "1", "Width": "6", "Height": "1"}},
		{Elem: "Text", Attrs: map[string]string{"Name": "T2", "Canvas.Left": "2", "Canvas.Top": "3", "Width": "6", "Height": "1"}},
	}
	ed.rebuild()
	c.Frame()
	b := docKid(ed, 1).(interface{ Bounds() gooey.Rect }).Bounds()
	if b.W == 0 {
		t.Fatalf("the second <Text> was never arranged (%v)", b)
	}

	ed.setSelection(ed.doc().Kids[0])
	settle(t, c)
	press(c, b.X, b.Y)
	_, pointer := c.Frame()
	if ed.sel != ed.doc().Kids[1] {
		t.Fatalf("the press did not move the selection (%s); the count below is of nothing", nodeName(ed.sel))
	}
	if pointer != selectionDamage {
		t.Errorf("moving the selection with the pointer repainted %d components, want %d: damage %v",
			pointer, selectionDamage, c.Damage())
	}
	if _, again := c.Frame(); again != 0 {
		t.Fatalf("the next frame repainted %d with nothing changed: the count above is not damage", again)
	}

	ed.setSelection(ed.doc().Kids[0])
	settle(t, c)
	if !c.Handle(input.KeyOf(input.KeyEvent{Key: input.KeyRune, Rune: 'n', Mods: input.ModCtrl})) {
		t.Fatal("ctrl+n was not consumed: the keyboard selection binding is gone")
	}
	_, keyboard := c.Frame()
	if ed.sel != ed.doc().Kids[1] {
		t.Fatalf("ctrl+n did not move the selection (%s)", nodeName(ed.sel))
	}
	if keyboard != pointer {
		t.Errorf("ctrl+n repainted %d components and the pointer repainted %d: the two gestures "+
			"no longer share setSelection", keyboard, pointer)
	}
}

// selectionDamage is one selection change between two nodes of the same
// element, MEASURED rather than chosen:
//
//	{5 3 36 33}    the TOOLBOX list. It has nothing to do with the
//	               selection — PaletteItems reads the same revision
//	               counter the properties grid does, and ed.palette is
//	               fixed after newEditor, so this repaint is pure waste.
//	               Left alone here because removing that Get is a change
//	               to a different feature's damage profile; see the report.
//	{115 2 44 31}  the PROPERTIES ItemsView.
//	three cells    the row values that actually differ between the two
//	               nodes (Name, Canvas.Top) plus the row the highlight
//	               re-styles.
//
// What is NOT in it is the point: no rect anywhere inside the designer.
const selectionDamage = 5

// TestSelectingNeverRepaintsTheDesigner is the claim the count cannot make.
//
// A count is wrong by a number; this is wrong by a PLACE. It is asserted on
// the shipped document — the expensive case, where the grid re-rows and 49
// components repaint — because that is the case where a stray designer
// repaint would hide inside a large legitimate number.
func TestSelectingNeverRepaintsTheDesigner(t *testing.T) {
	ed, c := designerPage(t)
	widen(t, ed, c)
	pane := findPreview(c.Root())
	pb := pane.(interface{ Bounds() gooey.Rect }).Bounds()
	btn := findButton(c.Root())
	if btn == nil {
		t.Fatal("the starting document has no Button to click")
	}
	b := btn.Bounds()

	ed.setSelection(ed.doc().Kids[0])
	settle(t, c)
	press(c, b.X, b.Y)
	if _, painted := c.Frame(); painted == 0 {
		t.Fatal("selecting repainted nothing at all: this test would pass for a gesture that " +
			"does not work")
	}
	if ed.sel != ed.doc().Kids[1] {
		t.Fatalf("the press did not move the selection (%s)", nodeName(ed.sel))
	}
	for _, r := range c.Damage() {
		if within(r, pb) {
			t.Errorf("selecting repainted inside the designer (%v of %v): the document was "+
				"rebuilt to show a different set of rows in a pane beside it", r, pb)
		}
	}
}

func within(r, outer gooey.Rect) bool {
	return r.X >= outer.X && r.Y >= outer.Y &&
		r.X+r.W <= outer.X+outer.W && r.Y+r.H <= outer.Y+outer.H
}

// TestTheKeyboardStillSelects is the regression guard on the gesture that
// already worked. Adding a pointer gesture must not cost the keyboard one,
// and everything in this repo has to stay keyboard-operable because mouse
// events cannot be injected through a recording pty at all.
func TestTheKeyboardStillSelects(t *testing.T) {
	ed, c := designerPage(t)
	n := len(ed.doc().Kids)
	if n < 2 {
		t.Fatalf("the starting document has %d nodes; cycling asserts nothing", n)
	}
	ed.setSelection(ed.doc().Kids[0])

	ctrl := func(r rune) bool {
		return c.Handle(input.KeyOf(input.KeyEvent{Key: input.KeyRune, Rune: r, Mods: input.ModCtrl}))
	}
	if !ctrl('n') || ed.sel != ed.doc().Kids[1] {
		t.Fatalf("ctrl+n left the selection at %s, want the <Button>", nodeName(ed.sel))
	}
	if !ctrl('p') || ed.sel != ed.doc().Kids[0] {
		t.Fatalf("ctrl+p left the selection at %s, want the <Text>", nodeName(ed.sel))
	}
	// And the properties grid follows it, which is the point of selecting.
	ed.setSelection(ed.doc().Kids[1]) // the Button
	btnRows := rowNames(ed)
	ed.setSelection(ed.doc().Kids[0]) // the Text
	if textRows := rowNames(ed); textRows == btnRows {
		t.Error("the inspector offers the same attributes for the <Text> and the <Button>: " +
			"the selection is stored but nothing reads it")
	}
	// Through the PROPERTY, not around it: the screen reads the computed.
	if ed.attrItems.Get().Len() == 0 {
		t.Error("the properties grid is empty after a keyboard selection")
	}
}

func rowNames(ed *editor) string {
	s := ""
	for _, r := range ed.attrRows() {
		s += r.name + ","
	}
	return s
}

// TestTheBuiltTreeCorrespondsToTheDocument is the assumption mapNodes
// rests on, stated where it can fail loudly.
//
// The inversion is POSITIONAL at every depth: the i'th built child is what
// markup.Build made of the i'th document child. That holds because nothing
// sits between a container and its children in the built tree — but it is
// an invariant of the markup loader, not of this editor, so it is asserted
// rather than trusted.
func TestTheBuiltTreeCorrespondsToTheDocument(t *testing.T) {
	ed := newEditor(editorFS())
	ed.rebuild()
	// docRoot is the built SURFACE. Its one child is the user's root, and
	// THAT one's children are the document nodes — the extra hop is the
	// wrapping model, and asserting it here is what keeps the inversion
	// honest about which level is which.
	if got := len(childComponents(ed.docRoot)); got != 1 {
		t.Fatalf("the built surface has %d children, want 1 (the user's root)", got)
	}
	builtRoot := childComponents(ed.docRoot)[0]
	if got := len(childComponents(builtRoot)); got != len(ed.doc().Kids) {
		t.Fatalf("the built root has %d children for %d document nodes: positional selection "+
			"would pick a neighbour of whatever was clicked", got, len(ed.doc().Kids))
	}
	if ed.nodeOf[ed.docRoot] != ed.root {
		t.Error("the built surface does not map to the surface node")
	}
	if ed.nodeOf[builtRoot] != ed.doc() {
		t.Error("the built root does not map to the user's root")
	}
	// Names are not what the mapping USES — that is the point, since a user
	// can rename or delete one — but they are what can check it.
	for i, kid := range ed.doc().Kids {
		name := kid.Attrs["Name"]
		comp := ed.docCtx.Named[name]
		if comp != docKid(ed, i) {
			t.Errorf("child %d is not the component built for <%s Name=%q>", i, kid.Elem, name)
		}
		if ed.nodeOf[comp] != kid {
			t.Errorf("<%s Name=%q> is not what its component maps back to", kid.Elem, name)
		}
	}

	// A document that does NOT build must drop the mapping rather than
	// leave the previous tree's components addressed by this document.
	stale := ed.docCtx.Named["T1"]
	ed.doc().Kids = append(ed.doc().Kids, &node{Elem: "NotAnElement", Attrs: map[string]string{}})
	ed.rebuild()
	if _, err := markup.Build([]byte(ed.source.Get()), ed.docCtx); err == nil {
		t.Fatal("the fixture document builds after all; the arm below asserts nothing")
	}
	if ed.docRoot != nil || ed.nodeOf != nil {
		t.Error("a failed build left the previous tree mapped to this document's nodes: a press " +
			"would select a node the user is not looking at")
	}
	if got := ed.nodeAt(stale); got != nil {
		t.Errorf("with no mapping a press resolved to <%s>, want nothing", got.Elem)
	}
}

// TestDeletingWalksTheSelectionAndEmptiesIt.
//
// Added because a mutation SURVIVED: `ed.sel = nil` when the parent goes
// empty could be changed to `ed.sel = p` and every test still passed. The
// behaviour was written and never pinned, which is the same "green means
// nothing" failure this branch keeps finding — so the deletion walk is
// asserted here at each of its three landings.
//
// The empty case is the one an INDEX could not express at all: the old
// code left selected at -1, which meant "the container", so deleting the
// last child silently promoted the selection to the root.
func TestDeletingWalksTheSelectionAndEmptiesIt(t *testing.T) {
	ed, _ := designerPage(t)
	if len(ed.doc().Kids) != 2 {
		t.Fatalf("the starting document has %d nodes; this test wants the shipped two", len(ed.doc().Kids))
	}
	first, second := ed.doc().Kids[0], ed.doc().Kids[1]

	// Deleting a node with a successor selects the successor — the node
	// that took its place, not the one before it.
	ed.setSelection(first)
	ed.deleteSelected()
	if ed.sel != second {
		t.Fatalf("deleting the first node selected %s, want the node that took its place",
			nodeName(ed.sel))
	}

	// Deleting the LAST remaining node leaves NOTHING selected — not the
	// container it was in.
	ed.deleteSelected()
	if len(ed.doc().Kids) != 0 {
		t.Fatalf("the document still holds %d nodes", len(ed.doc().Kids))
	}
	if ed.sel != nil {
		t.Errorf("deleting the last node selected %s, want nothing: the selection was promoted "+
			"to a node the user did not choose", nodeName(ed.sel))
	}
	if _, _, target := ed.target(); target != nil {
		t.Errorf("after deleting everything the inspector still targets <%s>", target.Elem)
	}

	// And deleting with nothing selected is a no-op rather than a panic.
	ed.deleteSelected()

	// Deleting from the END selects the new end, which is the third
	// landing and the one an off-by-one would reach.
	ed.doc().Kids = []*node{
		{Elem: "Text", Body: "a", Attrs: map[string]string{"Name": "A"}},
		{Elem: "Text", Body: "b", Attrs: map[string]string{"Name": "B"}},
	}
	ed.setSelection(ed.doc().Kids[1])
	ed.deleteSelected()
	if ed.sel != ed.doc().Kids[0] {
		t.Errorf("deleting the last of two selected %s, want the remaining node", nodeName(ed.sel))
	}
}

// nodeName describes a selection for a failure message. Nil is the empty
// state and has to read as such rather than as a crash.
func nodeName(n *node) string {
	if n == nil {
		return "nothing"
	}
	if name := n.Attrs["Name"]; name != "" {
		return "<" + n.Elem + " Name=" + name + ">"
	}
	return "<" + n.Elem + ">"
}

// TestTheWalkReportsTheWholeChainNotJustTheTopLevelNode is the SHAPE, and
// it is asserted separately from what the policy does with it.
//
// The walk has now outlived three policies — climb to the top-level kid,
// take the deepest, index from the drill scope — and changed for none of
// them, which is the claim this test exists to keep true. Every policy so
// far has been an index into this chain; a walk that collapsed nesting
// would have made the shallow-first inversion impossible.
func TestTheWalkReportsTheWholeChainNotJustTheTopLevelNode(t *testing.T) {
	ed, c := designerPage(t)
	ed.doc().Kids = []*node{
		{Elem: "Border", Attrs: map[string]string{"Name": "B1", "Canvas.Left": "0", "Canvas.Top": "0"},
			Kids: []*node{{Elem: "Border", Attrs: map[string]string{"Name": "B2"},
				Kids: []*node{{Elem: "Text", Attrs: map[string]string{"Name": "Deep", "Width": "6", "Height": "1"}}}}}},
	}
	ed.rebuild()
	if !strings.HasPrefix(ed.status.Get(), "✓") {
		t.Fatalf("the fixture document does not build: %s", ed.status.Get())
	}
	c.Frame()

	deep := ed.docCtx.Named["Deep"]
	if deep == nil {
		t.Fatal("the inner <Text> was not built")
	}
	b := deep.(interface{ Bounds() gooey.Rect }).Bounds()
	if b.W == 0 {
		t.Fatalf("the inner <Text> was never arranged (%v)", b)
	}
	hit := c.Focus().HitTest(b.X, b.Y)
	if hit != deep {
		t.Fatalf("HitTest returned %T at the inner Text's cell, not the Text: the chain below "+
			"would be measured from the wrong place", hit)
	}

	chain := ed.nodeChain(hit)
	// SURFACE, user root, then the three nested nodes: the surface is in
	// the chain (it is where the walk starts) and is excluded by the
	// POLICY, not by the walk.
	want := []*node{ed.root, ed.doc(), ed.doc().Kids[0], ed.doc().Kids[0].Kids[0], ed.doc().Kids[0].Kids[0].Kids[0]}
	if len(chain) != len(want) {
		t.Fatalf("the walk reported %d nodes for a hit three deep, want %d: it collapses nesting "+
			"the selection model needs", len(chain), len(want))
	}
	for i := range want {
		if chain[i] != want[i] {
			t.Errorf("chain[%d] is <%s>, want <%s>: the chain is not outermost-first",
				i, chain[i].Elem, want[i].Elem)
		}
	}

	// And the POLICY indexes into it from the drill scope, which with
	// nothing drilled is the user's root — so chain[2], the outermost of
	// the three nested nodes. The surface is chain[0] and is excluded by
	// the policy's floor rather than by the walk.
	ed.setSelection(nil)
	press(c, b.X, b.Y)
	if ed.sel != ed.doc().Kids[0] {
		t.Errorf("a press three levels deep selected %s, want the OUTERMOST <Border>",
			nodeName(ed.sel))
	}
	if ed.sel == ed.root {
		t.Error("the surface was selected: it is chrome and must never be")
	}
}

// TestTheWalkStopsWhereTheTreeStopsCorresponding is mapNodes' guard.
//
// Where a node's built children do not line up one-for-one with its
// document children, pairing them off by index would map a node onto a
// component it has nothing to do with. The descent stops instead, so a hit
// below resolves to the last node that was certainly right.
func TestTheWalkStopsWhereTheTreeStopsCorresponding(t *testing.T) {
	ed, c := designerPage(t)
	// <Tabs> with one <Tab>: the Tab's header strip means the built tree
	// below Tabs is not one component per document child.
	ed.doc().Kids = []*node{
		{Elem: "Tabs", Attrs: map[string]string{"Name": "T", "Canvas.Left": "0", "Canvas.Top": "0"},
			Kids: []*node{{Elem: "Tab", Attrs: map[string]string{"Header": "one"},
				Kids: []*node{{Elem: "Text", Attrs: map[string]string{"Name": "Inside", "Width": "6", "Height": "1"}}}}}},
	}
	ed.rebuild()
	if !strings.HasPrefix(ed.status.Get(), "✓") {
		t.Fatalf("the fixture document does not build: %s", ed.status.Get())
	}
	c.Frame()

	// FIRST, establish that the guard actually fires here — otherwise this
	// test would pass on a correspondence that never broke, which is the
	// shape of a green test asserting nothing. Measured: <Tab> carries one
	// document child and builds a component with none.
	tab := ed.doc().Kids[0].Kids[0]
	inner := ed.docCtx.Named["Inside"]
	if inner == nil {
		t.Fatal("the inner <Text> was not built at all")
	}
	if ed.nodeOf[inner] != nil {
		t.Fatalf("the inversion descended past <Tab> after all: this document does not exercise "+
			"the guard, and the assertions below prove nothing about it (mapped to <%s>)",
			ed.nodeOf[inner].Elem)
	}
	if len(tab.Kids) == len(childComponents(ed.docCtx.Named["T"])) {
		t.Log("note: the counts happen to agree at <Tabs>; the stop is below it")
	}

	// The guarantee: the walk degrades to the last node it was certainly
	// right about rather than mis-pairing, and that node is still
	// selectable.
	tabs := docKid(ed, 0)
	tb := tabs.(interface{ Bounds() gooey.Rect }).Bounds()
	if tb.W == 0 {
		t.Fatalf("<Tabs> was never arranged (%v)", tb)
	}
	hit := c.Focus().HitTest(tb.X+1, tb.Y+1)
	if hit == nil || !under(tabs, hit) {
		t.Fatalf("the probe cell is not inside <Tabs> (%T): this test asserts nothing", hit)
	}
	chain := ed.nodeChain(hit)
	if len(chain) < 3 || chain[0] != ed.root || chain[1] != ed.doc() || chain[2] != ed.doc().Kids[0] {
		t.Fatalf("the walk lost the <Tabs> inside a container it cannot map through: %v", chain)
	}
	// Nothing the walk reports may be a node it could not verify.
	for _, n := range chain {
		if n == ed.doc().Kids[0].Kids[0].Kids[0] {
			t.Error("the walk reported the node below the unverifiable pairing: it mapped a " +
				"document node onto a component that merely sits at the same offset")
		}
	}
	press(c, tb.X+1, tb.Y+1)
	if ed.sel != ed.doc().Kids[0] {
		t.Errorf("a press inside <Tabs> selected %s, want the <Tabs>: a container the inversion "+
			"cannot descend must still be selectable itself, and the press must not fall through "+
			"to something the walk could not verify", nodeName(ed.sel))
	}
}

// TestCompOfIsExactlyNodeOfInverted is the pin the inverse map needs.
//
// componentFor used to SEARCH nodeOf, so it could not disagree with it.
// It reads compOf now, which is a second structure that can — and a
// disagreement is not a crash: it is the drag, the guide and the
// selection marker all landing on a different element than the one the
// user is pointing at, silently, for as long as the two stay out of
// step.
//
// So this asserts the relation rather than a lookup: every pair in
// nodeOf appears inverted in compOf, and compOf holds nothing else. The
// second half matters because an entry that OUTLIVES its node — a
// rebuild that repopulated one map and not the other — is exactly the
// stale pointer the editor's whole staleness discipline exists to
// prevent, and it would answer confidently.
func TestCompOfIsExactlyNodeOfInverted(t *testing.T) {
	ed, c, _ := designerPageCounting(t)
	ed.doc().Kids = []*node{
		{Elem: "VStack", Attrs: map[string]string{"Name": "V"}, Kids: []*node{
			{Elem: "Text", Body: "aa", Attrs: map[string]string{"Name": "A"}},
			{Elem: "Text", Body: "bb", Attrs: map[string]string{"Name": "B"}},
		}},
		{Elem: "Text", Body: "cc", Attrs: map[string]string{"Name": "C"}},
	}
	ed.rebuild()
	c.Frame()

	if len(ed.nodeOf) < 4 {
		t.Fatalf("only %d components mapped; the fixture is too small to "+
			"tell an inverted map from an empty one", len(ed.nodeOf))
	}
	if len(ed.compOf) != len(ed.nodeOf) {
		t.Errorf("compOf holds %d entries and nodeOf %d — one walk wrote "+
			"both, so they cannot differ in size without mapNodes being "+
			"wrong", len(ed.compOf), len(ed.nodeOf))
	}
	for comp, n := range ed.nodeOf {
		if got := ed.compOf[n]; got != comp {
			t.Errorf("nodeOf maps %T to <%s>, but compOf maps <%s> back to "+
				"%T — componentFor would answer with the wrong component",
				comp, n.Elem, n.Elem, got)
		}
	}
	for n := range ed.compOf {
		if _, ok := ed.nodeOf[ed.compOf[n]]; !ok {
			t.Errorf("compOf holds <%s>, whose component is not in nodeOf: "+
				"a stale entry that survived a rebuild", n.Elem)
		}
	}

	// And the accessor agrees with the search it replaced.
	for comp, n := range ed.nodeOf {
		if got := ed.componentFor(n); got != comp {
			t.Errorf("componentFor(<%s>) returned %T, want %T",
				n.Elem, got, comp)
		}
	}

	// A rebuild must replace both, not add to them.
	before := len(ed.compOf)
	ed.doc().Kids = ed.doc().Kids[:1]
	ed.rebuild()
	c.Frame()
	if len(ed.compOf) >= before {
		t.Errorf("after removing a subtree compOf holds %d entries, was %d: "+
			"the map is accumulating rather than being rebuilt", len(ed.compOf), before)
	}
	if len(ed.compOf) != len(ed.nodeOf) {
		t.Errorf("after a rebuild compOf has %d entries and nodeOf %d",
			len(ed.compOf), len(ed.nodeOf))
	}
}

// TestATripleClickDrillsOneLevelPerClickPastTheFirst pins what a THIRD
// rapid click does, which is the thing nothing asserted and a comment in
// input/mouse.go asserted wrongly.
//
// The drill fires on `count >= 2` and moves the scope down one level
// each time it fires. Under a click ceiling of 2 a rapid third click
// restarted the sequence at 1 and drilled nothing; under a ceiling of 3
// it reports 3 and drills again. So raising the ceiling DID change this
// gesture — a triple click selects two levels down where it used to
// select one — while input.MouseEvent's doc said it had not. Found in
// the review of #391 (issue #419).
//
// The behaviour is kept rather than clamped, because "one level per
// click past the first" is the same composition a repeated double-click
// already has, and clamping would make the third click of a fast triple
// mean nothing at all. What is fixed is the claim; this is what stops it
// drifting again.
//
// THE SINGLE AND DOUBLE ARMS ARE THE DISCRIMINATOR. Without them a drill
// that went straight to the deepest hit on any click would satisfy the
// triple assertion alone.
func TestATripleClickDrillsOneLevelPerClickPastTheFirst(t *testing.T) {
	ed, c := designerPage(t)
	ed.doc().Kids = []*node{
		{Elem: "Border", Attrs: map[string]string{"Name": "B1", "Canvas.Left": "0", "Canvas.Top": "0"},
			Kids: []*node{{Elem: "Border", Attrs: map[string]string{"Name": "B2"},
				Kids: []*node{{Elem: "Text", Attrs: map[string]string{"Name": "Deep", "Width": "6", "Height": "1"}}}}}},
	}
	ed.rebuild()
	if !strings.HasPrefix(ed.status.Get(), "✓") {
		t.Fatalf("the fixture document does not build: %s", ed.status.Get())
	}
	c.Frame()

	outer := ed.doc().Kids[0]
	mid := outer.Kids[0]
	deep := mid.Kids[0]
	b := ed.docCtx.Named["Deep"].(interface{ Bounds() gooey.Rect }).Bounds()
	if b.W == 0 {
		t.Fatalf("the inner <Text> was never arranged (%v)", b)
	}

	advance := clickClock(c)
	for _, tc := range []struct {
		clicks int
		want   *node
		what   string
	}{
		{1, outer, "the outermost, because a single click does not drill"},
		{2, mid, "one level down"},
		{3, deep, "two levels down — one per click past the first"},
	} {
		advance(time.Second) // a new gesture, not a continuation
		ed.setSelection(nil)
		for i := 0; i < tc.clicks; i++ {
			singleClick(c, b.X, b.Y)
		}
		if ed.sel != tc.want {
			t.Errorf("%d rapid clicks selected %s, want %s (%s)",
				tc.clicks, nodeName(ed.sel), nodeName(tc.want), tc.what)
		}
	}

	// A FOURTH restarts the sequence at 1, which the ceiling guarantees,
	// so it drills nothing further — and it does NOT pop back out
	// either. The drill scope followed the selection down, so a count-1
	// click re-selects the outermost node WITHIN THAT SCOPE, which is
	// where the third click already left it. Two levels is therefore the
	// deepest a single burst reaches however long it goes on, and the
	// way back up is Escape rather than more clicking.
	advance(time.Second)
	ed.setSelection(nil)
	for i := 0; i < 6; i++ {
		singleClick(c, b.X, b.Y)
	}
	if ed.sel != deep {
		t.Errorf("six rapid clicks selected %s, want the same two-levels-down node a "+
			"triple reaches: past the ceiling the count restarts at 1, and a count-1 "+
			"click re-selects inside the scope the drill moved to", nodeName(ed.sel))
	}
}
