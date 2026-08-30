package gooey

import (
	"testing"

	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// The overlay tier: #430, reported from a running wysiwyg as "the menu
// gets stuff drawn over it when opened."
//
// Z-order is document order, which is right for everything laid out and
// wrong for everything that hangs over. A popup surface is the last child
// OF ITS OWNER — components/popup.go says so — and that buys nothing once
// the owner has a later sibling: Composer.Frame forces a repaint only of
// nodes LATER in the walk than a painter, so a later sibling paints over
// the open popup and nothing puts it back.
//
// Written against the MARKER rather than against components.Popup,
// because the composer is what is being fixed and components is a
// different package. filler below is the smallest thing that behaves like
// a popup surface: a leaf that fills its rect and can be dirtied on
// demand.

// filler paints one rune over the whole of its bounds, so "who owns this
// cell" is readable straight off the frame. Its Render reads rev, which
// is the handle a test uses to repaint exactly this component and
// nothing else.
type filler struct {
	Base
	ch  rune
	rev *prop.Property[int]
}

func newFiller(ch rune) *filler { return &filler{ch: ch, rev: prop.NewSource(0)} }

func (f *filler) bump() { f.rev.Set(f.rev.Get() + 1) }

func (f *filler) Measure(Size) Size { return Size{W: 4, H: 1} }

func (f *filler) Render(fr *Frame) {
	f.rev.Get() // the subscription: bump() repaints this node and no other
	b := f.Bounds()
	for y := b.Y; y < b.Y+b.H; y++ {
		for x := b.X; x < b.X+b.W; x++ {
			fr.Cells.Set(x, y, f.ch, render.Style{})
		}
	}
}

// overlayFiller is a filler that claims the overlay tier.
type overlayFiller struct{ filler }

func newOverlayFiller(ch rune) *overlayFiller {
	return &overlayFiller{filler: filler{ch: ch, rev: prop.NewSource(0)}}
}

func (o *overlayFiller) OverlaysComposition() {}

// stack places every child at the SAME rect, so the overlap is total and
// the winner is whoever painted last. A layout policy would only add a
// second thing that could be wrong.
type stack struct {
	Base
	kids []Component
}

func (s *stack) ChildComponents() []Component { return s.kids }
func (s *stack) Measure(avail Size) Size      { return avail }
func (s *stack) Render(*Frame)                {}
func (s *stack) Arrange(b Rect) {
	s.Base.Arrange(b)
	for _, k := range s.kids {
		ArrangeChild(k, Rect{X: b.X, Y: b.Y, W: 4, H: 1})
	}
}

func topRow(t *testing.T, f *Frame) string {
	t.Helper()
	return render.RowText(f.Cells, 0)[:4]
}

// TestAnOverlayIsNotPaintedOverByALaterSibling is the pin, and it fails
// on the tree exactly as it was before the fix.
//
// The shape is the reported one: an overlay declared EARLY (a popup
// surface hangs off a MenuBar near the top of a canvas) and an ordinary
// component declared LATE over the same cells. Dirtying the late one is
// what a designer edit, a realized list row, or a bound value does.
func TestAnOverlayIsNotPaintedOverByALaterSibling(t *testing.T) {
	over := newOverlayFiller('O')
	late := newFiller('L')
	c := NewComposer(&stack{kids: []Component{over, late}}, 8, 2)
	f, _ := c.Frame()

	if got := topRow(t, f); got != "OOOO" {
		t.Fatalf("the first frame reads %q; the overlay is declared FIRST and must "+
			"still end up on top, or this test cannot mean anything", got)
	}

	late.bump()
	f, _ = c.Frame()
	if got := topRow(t, f); got != "OOOO" {
		t.Errorf("after the later sibling repainted the row reads %q. An open "+
			"overlay was painted over and nothing put it back: the paint loop "+
			"forces only nodes LATER in z-order, so being last among its owner's "+
			"children is not enough — an overlay has to be on the overlay TIER", got)
	}
}

// TestAnOverlayStillPaintsOverAnEarlierSibling is the other direction.
// Without it the test above passes against a composer that simply never
// repaints the late sibling at all.
func TestAnOverlayStillPaintsOverAnEarlierSibling(t *testing.T) {
	early := newFiller('E')
	over := newOverlayFiller('O')
	c := NewComposer(&stack{kids: []Component{early, over}}, 8, 2)
	f, _ := c.Frame()
	if got := topRow(t, f); got != "OOOO" {
		t.Fatalf("first frame reads %q, want the overlay on top", got)
	}
	early.bump()
	f, _ = c.Frame()
	if got := topRow(t, f); got != "OOOO" {
		t.Errorf("after the earlier sibling repainted the row reads %q, want the "+
			"overlay still on top", got)
	}
}

// TestHoistingIsStableAmongOverlays pins the half a sort would have got
// wrong: the tree still decides which of two overlays is on top. Only
// whether an overlay is above the DOCUMENT is taken away from it.
func TestHoistingIsStableAmongOverlays(t *testing.T) {
	first := newOverlayFiller('1')
	second := newOverlayFiller('2')
	late := newFiller('L')
	c := NewComposer(&stack{kids: []Component{first, second, late}}, 8, 2)
	f, _ := c.Frame()
	if got := topRow(t, f); got != "2222" {
		t.Fatalf("first frame reads %q; the later of two overlays must be on top", got)
	}
	late.bump()
	f, _ = c.Frame()
	if got := topRow(t, f); got != "2222" {
		t.Errorf("after a document repaint the row reads %q; hoisting must keep the "+
			"overlays in tree order relative to each other", got)
	}
}

// TestATreeWithNoOverlayKeepsDocumentOrder is the no-op guarantee. The
// hoist runs on every structural re-sync of every composition in the
// repo, and the overwhelming majority hold no overlay at all.
func TestATreeWithNoOverlayKeepsDocumentOrder(t *testing.T) {
	early := newFiller('E')
	late := newFiller('L')
	c := NewComposer(&stack{kids: []Component{early, late}}, 8, 2)
	f, _ := c.Frame()
	if got := topRow(t, f); got != "LLLL" {
		t.Fatalf("first frame reads %q; without an overlay the later sibling wins", got)
	}
	early.bump()
	f, _ = c.Frame()
	if got := topRow(t, f); got != "LLLL" {
		t.Errorf("after the earlier sibling repainted the row reads %q; document "+
			"order still decides when nothing claims the overlay tier", got)
	}
}

// TestAnOverlaysSubtreeIsHoistedWithIt — the marker is on a container in
// this one, so the claim is about the SUBTREE and not about one node.
// Hoisting the parent and leaving its children in the document would put
// the box above the page and its contents below it.
func TestAnOverlaysSubtreeIsHoistedWithIt(t *testing.T) {
	inner := newFiller('I')
	group := &overlayStack{stack: stack{kids: []Component{inner}}}
	late := newFiller('L')
	c := NewComposer(&stack{kids: []Component{group, late}}, 8, 2)
	f, _ := c.Frame()
	if got := topRow(t, f); got != "IIII" {
		t.Fatalf("first frame reads %q, want the overlay group's child on top", got)
	}
	late.bump()
	f, _ = c.Frame()
	if got := topRow(t, f); got != "IIII" {
		t.Errorf("after a document repaint the row reads %q; a hoisted overlay "+
			"must take its subtree with it, or the box floats and its contents "+
			"stay behind", got)
	}
}

// overlayStack is a container on the overlay tier.
type overlayStack struct{ stack }

func (o *overlayStack) OverlaysComposition() {}
