package components

import (
	"testing"

	"github.com/WonderForgeLabs/gooey"
)

// orderAnchor is an ordinary bounded leaf: the thing an adornment is
// pinned to. It has no behaviour beyond having bounds, which is the only
// property the layer asks it about.
type orderAnchor struct{ gooey.Base }

func (a *orderAnchor) Measure(_ gooey.Size) gooey.Size { return gooey.Size{W: 6, H: 1} }
func (a *orderAnchor) Render(*gooey.Frame)             {}

// orderAdorn is the minimal ordinary adornment — anchored (not a
// PointerFollower, so it is not exempt from the anchor sweep) and
// orphanable, so the drop is observable as a COUNT and not only as a
// vanished rect. Both observables matter below: a zero rect alone would
// also be produced by a persistent adornment being hidden, which is a
// different outcome with a different recovery.
type orderAdorn struct {
	gooey.Base
	anchor  gooey.Component
	orphans int
}

func (p *orderAdorn) Anchor() gooey.Component { return p.anchor }
func (p *orderAdorn) Place(against, _ gooey.Rect) gooey.Rect {
	return gooey.Rect{X: against.X, Y: against.Y + 1, W: 3, H: 1}
}
func (p *orderAdorn) Measure(_ gooey.Size) gooey.Size { return gooey.Size{W: 3, H: 1} }
func (p *orderAdorn) Render(*gooey.Frame)             {}
func (p *orderAdorn) orphaned()                       { p.orphans++ }

// orderPage arranges its children in document order, one row each, and
// gives the layer the whole page. Document order is the variable under
// test, so the page must not reorder anything itself.
type orderPage struct {
	gooey.Base
	kids []gooey.Component
}

func (r *orderPage) ChildComponents() []gooey.Component { return r.kids }
func (r *orderPage) Render(*gooey.Frame)                {}
func (r *orderPage) Measure(a gooey.Size) gooey.Size    { return a }
func (r *orderPage) Arrange(b gooey.Rect) {
	r.Base.Arrange(b)
	y := b.Y
	for _, k := range r.kids {
		if _, isLayer := k.(*AdornmentLayer); isLayer {
			gooey.ArrangeChild(k, b)
			continue
		}
		gooey.ArrangeChild(k, gooey.Rect{X: b.X, Y: y, W: 6, H: 1})
		y++
	}
}

// adornOrderRun composes one page and returns what became of the
// adornment after three frames. Three, not one: the question is whether
// a later frame recovers the placement, and one frame cannot tell a
// permanent drop from a frame's lag.
func adornOrderRun(t *testing.T, layerFirst bool) (gooey.Rect, int) {
	t.Helper()
	anchor := &orderAnchor{}
	ad := &orderAdorn{anchor: anchor}
	c := adornOrderPage(t, layerFirst, anchor, ad)
	c.Frame()
	c.Frame()
	c.Frame()
	return ad.Bounds(), ad.orphans
}

// adornOrderPage builds the page and hands the composer back unframed,
// so a caller that needs an event between frames — the follower arm
// below needs a motion report before the pointer exists — can put one
// there. The adornment is supplied rather than constructed here: the
// two exemptions are the same fixture with one method added.
func adornOrderPage(t *testing.T, layerFirst bool, anchor gooey.Component, ad Adornment) *gooey.Composer {
	t.Helper()
	layer := &AdornmentLayer{}
	layer.Add(ad)
	kids := []gooey.Component{anchor, layer}
	if layerFirst {
		kids = []gooey.Component{layer, anchor}
	}
	c := gooey.NewComposer(&orderPage{kids: kids}, 30, 6)
	t.Cleanup(c.Close)
	return c
}

// persistAdorn is orderAdorn with the layer's second contract added and
// nothing else changed: a PersistentAdornment, the shape
// ValidationMarker's popup has (components/validation.go:129).
type persistAdorn struct{ orderAdorn }

func (p *persistAdorn) AdornmentPersists() bool { return true }

// followAdorn is orderAdorn with the layer's third contract added: a
// PointerFollower, the shape DragGhost has. Its anchor stays nil on
// purpose — Arrange handles a follower BEFORE it consults Anchor, so a
// nil there is the assertion that it never asks.
type followAdorn struct{ orderAdorn }

func (p *followAdorn) FollowsPointer() bool { return true }

// TestAnAdornmentLayerDeclaredBeforeItsAnchorLosesTheAdornment is the
// test docs/markup-reference.md's hosting rule had no pin for, and the
// reason that rule changed from "anywhere in the tree" to "after the
// content it adorns". Raised in review of #456.
//
// THE LIFT IS PAINT-ONLY, and that is the whole of it. Composer.orderPaint
// moves an overlay's paint to the end; layout still walks
// ChildComponents in document order. AdornmentLayer.Arrange re-anchors by
// reading each anchor's CURRENT bounds, so a layer arranged before its
// anchors reads bounds that do not exist yet — and anchorBounds reports a
// zero rect as `!ok`, the same answer it gives for an anchor that has
// left the tree. The adornment is orphaned and removed from l.adorns, so
// no later frame brings it back.
//
// BOTH ARMS ARE THE MEASUREMENT. The first arm alone would be a test of
// nothing: an adornment can fail to place for a dozen reasons, and only
// the second arm — same components, same page, same frames, opposite
// declaration order — shows that the ORDER is what decided it.
//
// This passes against the defect on the common path, which is why no
// existing test caught it: a Tooltip adds its adornment on hover, by
// which time the anchor has been arranged many times. It bites the case
// nobody writes a test for — an adornment added before the first frame.
func TestAnAdornmentLayerDeclaredBeforeItsAnchorLosesTheAdornment(t *testing.T) {
	after, dropped := adornOrderRun(t, true)
	if dropped == 0 {
		t.Errorf("the layer declared BEFORE its anchor kept the adornment "+
			"(bounds %+v). If the layer now tolerates an anchor that has no "+
			"bounds yet, docs/markup-reference.md's hosting rule can go back "+
			"to \"anywhere in the tree\" and this arm should be deleted rather "+
			"than relaxed", after)
	}
	if after != (gooey.Rect{}) {
		t.Errorf("the orphaned adornment still holds bounds %+v; it was dropped "+
			"from the layer, so nothing arranges it again", after)
	}

	// THE DISCRIMINATOR. Same tree, reversed.
	placed, kept := adornOrderRun(t, false)
	if kept != 0 {
		t.Fatalf("the layer declared AFTER its anchor orphaned the adornment "+
			"%d times too, so declaration order is not what the arm above "+
			"measured and this test proves nothing about it", kept)
	}
	if placed.W == 0 || placed.H == 0 {
		t.Fatalf("the layer declared AFTER its anchor placed the adornment at "+
			"%+v, which has no area — the control arm does not work, so the "+
			"arm above is not evidence about ordering", placed)
	}
	if placed.Y != 1 {
		t.Errorf("the adornment asks for the row below its anchor and landed at "+
			"%+v; the anchor occupies row 0", placed)
	}
}

// TestAPersistentAdornmentSurvivesALayerDeclaredFirst is the first of
// the two exemptions the test above does NOT measure, and the reason
// docs/markup-reference.md's hosting rule names the case it applies to
// rather than stating it of adornments generally. Raised in review of
// #456.
//
// Arrange's second path (components/adorn.go:243) keeps a
// PersistentAdornment whose anchor is in the tree but has no bounds
// yet, parking it at the anchor's origin for the frame and placing it
// properly on the next one. An anchor that has never been arranged is
// exactly that case, so the layer-first ordering the arm above measures
// as fatal is, for a persistent adornment, a one-frame delay.
//
// This is the arm ValidationMarker needs. Its markerPopup is the repo's
// only PersistentAdornment, and nothing went red if the persist branch
// stopped being taken: the branch's own tests hide a bounded anchor
// rather than an unarranged one, which reaches it by a different door.
func TestAPersistentAdornmentSurvivesALayerDeclaredFirst(t *testing.T) {
	anchor := &orderAnchor{}
	ad := &persistAdorn{orderAdorn{anchor: anchor}}
	c := adornOrderPage(t, true, anchor, ad)
	c.Frame()
	c.Frame()
	c.Frame()

	if ad.orphans != 0 {
		t.Errorf("the persistent adornment was orphaned %d times under a "+
			"layer declared first; the persist branch exists so that it is "+
			"not, and ValidationMarker loses its message when it is",
			ad.orphans)
	}
	if got, want := ad.Bounds(), (gooey.Rect{X: 0, Y: 1, W: 3, H: 1}); got != want {
		t.Errorf("the persistent adornment settled at %+v, want %+v — the "+
			"first frame parks it at the anchor's origin, and the frames "+
			"after it place it against real bounds", got, want)
	}
}

// TestAPointerFollowerIsUnaffectedByDeclarationOrder is the second
// exemption. A follower has no anchor, so none of the questions the
// order decides — has the anchor been arranged, is it still reachable —
// is one the layer asks about it (components/adorn.go:227, the first
// branch, before Anchor is consulted). Raised in review of #456.
//
// The motion report is not scenery: without a pointer seen, a follower
// is arranged to a zero rect on every frame, which is the same rect a
// DROPPED adornment leaves behind. Placing it somewhere the pointer
// chose is what distinguishes "the order did not matter" from "the
// order killed it and the harness could not tell".
func TestAPointerFollowerIsUnaffectedByDeclarationOrder(t *testing.T) {
	anchor := &orderAnchor{}
	ad := &followAdorn{}
	c := adornOrderPage(t, true, anchor, ad)
	c.Frame()
	c.HandleMouse(motion(4, 2))
	c.Frame()
	c.Frame()

	if ad.orphans != 0 {
		t.Errorf("the pointer follower was orphaned %d times under a layer "+
			"declared first; it has no anchor to be early for", ad.orphans)
	}
	if got, want := ad.Bounds(), (gooey.Rect{X: 4, Y: 3, W: 3, H: 1}); got != want {
		t.Errorf("the follower settled at %+v, want %+v — the row below the "+
			"pointer's cell, which is what its Place asks for", got, want)
	}
}
