package components

import (
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// The z-order claim, from the OTHER side of the owner.
//
// Popup's doc comment USED TO state the rule as a fact of the design:
// the surface was "a leaf child the owner returns from ChildComponents
// (LAST, because document order is z-order)". That sentence is gone —
// #437 replaced it with the gooey.Overlay marker, and #439 corrected
// the parenthetical, so do not go looking for it in popup.go. It is
// quoted here as the claim these tests were written against, because
// toyPage is still built to satisfy it: it declares the owner last, with
// that reason in a comment, and every other test in this package
// inherits the arrangement.
//
// Which means the whole suite only ever asked whether the surface is
// above its OWNER'S siblings. Being last among the owner's children buys
// exactly that and nothing more: Composer.Frame walks c.nodes in
// depth-first pre-order and forces a repaint only of nodes LATER in that
// order than a painter beneath them, so a popup stays on top only while
// its owner is the last thing in the document.
//
// Put anything after the owner that overlaps the dropdown and the
// guarantee is gone — every frame in which that sibling repaints paints
// over the open popup, with nothing to force the surface down again.
// Reported against apps/wysiwyg's designer canvas, where a MenuBar sits
// among a Gauge, an ItemsView and a Border: the dropdown paints on the
// frame that opens it and the next repaint of the island erases it.
//
// zOrderPage is toyPage with the owner moved off the end, which is the
// only difference that matters.
func zOrderPage() (*toyOwner, *prop.Property[string], gooey.Component) {
	owner := &toyOwner{}
	// The popup drops at {Y: 1, W: 6, H: 2}, so this Text overlaps it —
	// and it is declared AFTER the owner, which is the whole fixture.
	// Its Content is a source so the test can dirty this one component
	// without touching anything else.
	content := Str(strings.Repeat("@", 12))
	over := gooey.L(&Text{Content: content}, gooey.Layout{Top: 1})
	page := &Canvas{Children: []gooey.Component{owner, over}}
	return owner, content, page
}

func TestAnOpenPopupSurvivesALaterSiblingRepainting(t *testing.T) {
	owner, content, page := zOrderPage()
	c := gooey.NewComposer(page, 20, 5)
	c.Focus().SetFocus(owner)
	// The first frame paints the whole tree, and the number is recorded
	// here to give the pin below its margin: a fix that bought z-order
	// by repainting everything would report FOUR on the sibling frame,
	// not two, and every RowText assertion in this file would still be
	// green. Four is what makes two mean something.
	if _, all := c.Frame(); all != 4 {
		t.Fatalf("the first frame painted %d components, want 4 — the "+
			"fixture changed, and the damage pin below is calibrated "+
			"against this number", all)
	}

	if !c.HandleKey(input.Named(input.KeyEnter)) {
		t.Fatal("enter on the focused owner was not consumed")
	}
	// ONE component painted to open the popup: the surface itself. The
	// owner did not repaint, and neither did the sibling.
	if _, opened := c.Frame(); opened != 1 {
		t.Errorf("opening the popup repainted %d components, want 1 (the "+
			"surface). Anything more means opening an overlay disturbs "+
			"nodes it does not cover", opened)
	}

	// The popup is on screen. Without this the test could pass against a
	// popup that never painted at all.
	if got := render.RowText(c.Cells(), 1); !strings.HasPrefix(got, "POPUP!") {
		t.Fatalf("row 1 = %q right after opening, want the popup over the content", got)
	}

	// Now dirty ONLY the sibling declared after the owner. Nothing has
	// touched the popup, so nothing should move it.
	content.Set(strings.Repeat("%", 12))
	_, painted := c.Frame()

	// THE PIN THE CELL ASSERTIONS CANNOT BE. Per CLAUDE.md, "a bounds
	// assertion or a 'the cell says X' assertion passes just as well
	// when the entire tree repainted, so it proves nothing about
	// damage" — and the obvious wrong fix for this bug, force-repainting
	// the whole tree every frame, satisfies every RowText check in this
	// file. The count is what separates the two.
	//
	// TWO: the sibling that was dirtied, and the popup surface the
	// z-ordered pass forces above it. Not one — the surface has to
	// repaint or the sibling's new cells cover it. Not the node count —
	// that is the regression this exists to catch. The PR body's "no
	// damage count in the repo moved" is this number.
	// Added in review of #437.
	if painted != 2 {
		t.Errorf("dirtying one sibling repainted %d components, want 2 "+
			"(the sibling, and the popup surface forced above it). A "+
			"larger number means z-order was bought by repainting more "+
			"than the overlap requires", painted)
	}

	if got := render.RowText(c.Cells(), 1); !strings.HasPrefix(got, "POPUP!") {
		t.Fatalf("row 1 = %q after a later sibling repainted. The popup is still "+
			"open and nothing asked it to move; a component declared after its "+
			"owner has painted over it, and the z-ordered pass cannot put it back "+
			"because forcing only ever runs forward", got)
	}
}

// TestADismissedPopupStillUncoversALaterSibling is the other half, and
// without it the fix above could be "never let anything paint over the
// surface's rect", which would strand the popup's cells on screen after
// it closed.
func TestADismissedPopupStillUncoversALaterSibling(t *testing.T) {
	owner, _, page := zOrderPage()
	c := gooey.NewComposer(page, 20, 5)
	c.Focus().SetFocus(owner)
	c.Frame()
	c.HandleKey(input.Named(input.KeyEnter))
	c.Frame()

	c.HandleKey(input.Named(input.KeyEsc))
	c.Frame()
	if owner.popup().IsOpen() {
		t.Fatal("esc did not close the popup")
	}
	if got := render.RowText(c.Cells(), 1); !strings.HasPrefix(got, strings.Repeat("@", 6)) {
		t.Errorf("row 1 after dismiss = %q; the sibling underneath did not come back", got)
	}
}

// deepOwner is the case "declare it last" could never reach even in
// principle: the popup's owner is buried, and the component that has to
// end up UNDER the dropdown is a sibling of the owner's grandparent.
//
// This is the shipped shell's shape, not a contrivance — apps/wysiwyg
// puts its MenuBar inside a Panel inside a Grid, and the dock it drops
// its menus over is a sibling of that Panel. No ordering of the owner's
// own children says anything about it, which is why the lift in
// orderPaint is global rather than within the overlay's parent.
type deepOwner struct {
	gooey.Base
	kid gooey.Component
}

func (d *deepOwner) ChildComponents() []gooey.Component { return []gooey.Component{d.kid} }
func (d *deepOwner) Measure(a gooey.Size) gooey.Size    { return d.kid.Measure(a) }
func (d *deepOwner) Arrange(r gooey.Rect)               { d.Base.Arrange(r); gooey.ArrangeChild(d.kid, r) }
func (d *deepOwner) Render(*gooey.Frame)                {}

func TestAnOverlayClearsASiblingOfItsGrandparent(t *testing.T) {
	owner := &toyOwner{}
	nested := &deepOwner{kid: &deepOwner{kid: owner}}
	content := Str(strings.Repeat("@", 12))
	page := &Canvas{Children: []gooey.Component{
		nested,
		gooey.L(&Text{Content: content}, gooey.Layout{Top: 1}),
	}}

	c := gooey.NewComposer(page, 20, 5)
	c.Focus().SetFocus(owner)
	c.Frame()
	if !c.HandleKey(input.Named(input.KeyEnter)) {
		t.Fatal("enter on the focused owner was not consumed")
	}
	c.Frame()
	if got := render.RowText(c.Cells(), 1); !strings.HasPrefix(got, "POPUP!") {
		t.Fatalf("row 1 = %q; the dropdown of a buried owner is under a component "+
			"its grandparent's sibling painted", got)
	}

	content.Set(strings.Repeat("%", 12))
	c.Frame()
	if got := render.RowText(c.Cells(), 1); !strings.HasPrefix(got, "POPUP!") {
		t.Fatalf("row 1 = %q after that sibling repainted", got)
	}
}

// TestAnOverlayLiftsItsWholeSubtree is the pin for the inheritance in
// orderPaint, and nothing else in the suite can be it: every overlay the
// framework ships today is a LEAF, so `n.parent.overlay` never decides
// anything and could be deleted with the suite still green.
//
// The failure it guards is quiet and specific. An overlay that is a
// container, ordered on its own, would paint in the overlay layer while
// its children stayed behind in the ordinary one — so the surface would
// land on top of its own contents and the popup would show as an empty
// box. Whoever writes the first container overlay finds this here rather
// than on screen.
func TestAnOverlayLiftsItsWholeSubtree(t *testing.T) {
	// The box sits ON row 1 and its child fills it, so the child's cells
	// and the ordinary sibling's are the same cells — which is what makes
	// "who painted last" the only thing this test can be reading.
	box := &overlayBox{kid: &Text{Content: Str("INNER!")}}
	content := Str(strings.Repeat("@", 12))
	page := &Canvas{Children: []gooey.Component{
		gooey.L(box, gooey.Layout{Top: 1}),
		gooey.L(&Text{Content: content}, gooey.Layout{Top: 1}),
	}}

	c := gooey.NewComposer(page, 20, 5)
	c.Frame()
	if got := render.RowText(c.Cells(), 1); !strings.HasPrefix(got, "INNER!") {
		t.Fatalf("row 1 = %q; the overlay's own child is under the ordinary "+
			"sibling, so the subtree did not come up with its root", got)
	}

	content.Set(strings.Repeat("%", 12))
	c.Frame()
	if got := render.RowText(c.Cells(), 1); !strings.HasPrefix(got, "INNER!") {
		t.Fatalf("row 1 = %q after the ordinary sibling repainted", got)
	}
}

// overlayBox is a CONTAINER overlay — the shape the framework does not
// ship yet and the interface has to support, since a real overlay grows
// contents sooner or later.
type overlayBox struct {
	gooey.Base
	kid gooey.Component
}

func (o *overlayBox) OverlaysPage()                      {}
func (o *overlayBox) ChildComponents() []gooey.Component { return []gooey.Component{o.kid} }
func (o *overlayBox) Measure(a gooey.Size) gooey.Size    { return gooey.Size{W: 8, H: 1} }
func (o *overlayBox) Arrange(r gooey.Rect) {
	o.Base.Arrange(r)
	gooey.ArrangeChild(o.kid, r)
}
func (o *overlayBox) Render(*gooey.Frame) {}
