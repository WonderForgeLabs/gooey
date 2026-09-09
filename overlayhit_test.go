package gooey

import (
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey/render"
)

// Hit-testing asks the SAME question paint does (#465).
//
// overlayrank_test.go's TestARankOrdersHitTestingAsWellAsPaint is the
// headline — two overlays differing only in rank, one answer. These are
// the rest of the surface, and each is a way the walk could agree there
// and be wrong everywhere else: the plain lift without a rank, the
// ordinary layer where position still decides, depth, transparency, and
// the cost the whole design was constrained by.
//
// A NOTE ON THE FIXTURE. twoKids arranges every child to the SAME rect,
// so all of them contain (0,0) and the answer is decided entirely by the
// ordering rule rather than by geometry. That is deliberate: a fixture
// where the winner is the only component under the pointer passes for a
// walk with no ordering at all.

// TestAnOverlayTakesThePressFromALaterOrdinarySibling is the reported
// shape of #465 with no rank in it.
//
// The rank test is the harder case and this is the commoner one: an
// overlay host declared early, an ordinary component declared after it.
// Paint lifts the overlay above the whole page; the walk used to return
// the later sibling, because a later sibling was the whole rule.
//
// Both arms matter. Without the second, a walk that simply preferred the
// FIRST child would pass this and be wrong about everything else.
func TestAnOverlayTakesThePressFromALaterOrdinarySibling(t *testing.T) {
	over := &overlayStripe{stripe{ch: 'O'}}
	page := &stripe{ch: 'P'}

	t.Run("overlay declared first", func(t *testing.T) {
		root := &twoKids{kids: []Component{over, page}}
		NewComposer(root, 12, 3).Frame()
		if hit := NewFocusManager(root).HitTest(0, 0); hit != Component(over) {
			t.Errorf("HitTest returned %T, want the overlay: it paints above the page "+
				"from anywhere, so it takes the press from anywhere. A later ordinary "+
				"sibling winning is #465.", hit)
		}
	})

	t.Run("overlay declared after the page", func(t *testing.T) {
		root := &twoKids{kids: []Component{page, over}}
		NewComposer(root, 12, 3).Frame()
		if hit := NewFocusManager(root).HitTest(0, 0); hit != Component(over) {
			t.Errorf("HitTest returned %T, want the overlay — the answer must not depend "+
				"on where the host is declared, in EITHER direction", hit)
		}
	})
}

// TestPositionStillDecidesInsideTheOrdinaryLayer is the half #465 must
// not have broken.
//
// Document order is still the whole rule for everything that is not
// lifted, and it is still the tiebreak inside the lifted layer. A walk
// that started answering by rank alone would return either component
// here — both have rank 0 — so this is what says the comparison falls
// through to position rather than to whichever it met first.
func TestPositionStillDecidesInsideTheOrdinaryLayer(t *testing.T) {
	first := &stripe{ch: 'A'}
	last := &stripe{ch: 'B'}
	root := &twoKids{kids: []Component{first, last}}
	NewComposer(root, 12, 3).Frame()

	if hit := NewFocusManager(root).HitTest(0, 0); hit != Component(last) {
		t.Errorf("HitTest returned %T, want the LATER sibling: neither component is "+
			"lifted, so document order is still the entire answer and the later one "+
			"paints on top", hit)
	}
}

// TestEqualRanksFallBackToPosition is the tiebreak, and it is the arm
// that separates "compare on rank" from "compare on rank, then
// position".
//
// Two overlays at the SAME rank. appendByRank keeps equal ranks in
// encounter order — the property it is a bucket pass rather than a sort
// to make structural — so the later one paints last and must take the
// press.
func TestEqualRanksFallBackToPosition(t *testing.T) {
	first := &rankedStripe{stripe{ch: 'A', rank: OverlayRankToast}}
	last := &rankedStripe{stripe{ch: 'B', rank: OverlayRankToast}}
	root := &twoKids{kids: []Component{first, last}}
	NewComposer(root, 12, 3).Frame()

	if hit := NewFocusManager(root).HitTest(0, 0); hit != Component(last) {
		t.Errorf("HitTest returned %T, want the later of two EQUAL ranks. Rank is not "+
			"the whole comparison — equal ranks keep document order, which is what "+
			"appendByRank guarantees for paint and what this walk has to match", hit)
	}
}

// TestTheDeepestComponentStillWins pins the guarantee a design surface
// depends on, and it is the one most at risk from the rewrite: the walk
// stopped returning from the middle of the recursion, so "deepest" is
// now a consequence of pre-order numbering rather than of the traversal.
//
// A child's index is larger than its parent's, so it wins on position.
// (Which side of the bounds test the numbering sits on is NOT what makes
// that true — that was measured and is an equivalent change; see the
// comment in hitTest. What makes it true is that nodes are numbered in
// visit order and visit order is pre-order.)
func TestTheDeepestComponentStillWins(t *testing.T) {
	leaf := &stripe{ch: 'L'}
	inner := &twoKids{kids: []Component{leaf}}
	root := &twoKids{kids: []Component{inner}}
	NewComposer(root, 12, 3).Frame()

	if hit := NewFocusManager(root).HitTest(0, 0); hit != Component(leaf) {
		t.Errorf("HitTest returned %T, want the leaf. Children paint over their "+
			"ancestors, so the deepest component containing the cell is the hit — "+
			"click-to-select in the wysiwyg designer is built on exactly this", hit)
	}
}

// clearStripe is a page-spanning host that wants no press of its own,
// which is what ToastHost and AdornmentLayer are.
type clearStripe struct {
	overlayStripe
	kids []Component
}

func (c *clearStripe) ChildComponents() []Component { return c.kids }
func (c *clearStripe) HitTestTransparent() bool     { return true }
func (c *clearStripe) Arrange(b Rect) {
	c.Base.Arrange(b)
	for _, k := range c.kids {
		ArrangeChild(k, b)
	}
}

// TestATransparentOverlayHostPassesThePressToItsOwnChild is the case the
// framework's own overlays are, and the one the architecture doc now
// promises for an interactive adorner.
//
// The host is lifted and transparent; the child inside it is lifted (by
// inheritance) and not transparent. So the press must reach the CHILD,
// not the host and not the page underneath — which is three different
// wrong answers, one for each thing the walk could get wrong about
// transparency.
func TestATransparentOverlayHostPassesThePressToItsOwnChild(t *testing.T) {
	inside := &stripe{ch: 'T'}
	host := &clearStripe{kids: []Component{inside}}
	page := &stripe{ch: 'P'}
	root := &twoKids{kids: []Component{host, page}}
	NewComposer(root, 12, 3).Frame()

	switch hit := NewFocusManager(root).HitTest(0, 0); hit {
	case Component(inside):
	case Component(host):
		t.Error("HitTest returned the transparent HOST. A page-spanning host that " +
			"answers HitTestTransparent must never be the hit — it would eat every " +
			"click on the page")
	case Component(page):
		t.Error("HitTest returned the page beneath. Transparency is about the host's " +
			"own surface and not its subtree: what is INSIDE a lifted host is still " +
			"lifted, so a toast or an interactive adorner takes the press it paints over")
	default:
		t.Errorf("HitTest returned %T, want the child inside the transparent host", hit)
	}

	// AND AN EMPTY TRANSPARENT HOST MUST LOSE TO THE PAGE. The arm above
	// cannot see transparency break on its own: the child is deeper, so
	// it out-positions the host whether or not the host is skipped, and
	// disabling the check entirely leaves that assertion green. Measured
	// — it was silent. A transparent overlay with NOTHING under the
	// pointer inside it is the discriminating shape, because it is the
	// one where honouring the flag is the only reason the press goes
	// anywhere else.
	empty := &clearStripe{}
	root = &twoKids{kids: []Component{empty, page}}
	NewComposer(root, 12, 3).Frame()
	if hit := NewFocusManager(root).HitTest(0, 0); hit != Component(page) {
		t.Errorf("HitTest returned %T, want the page. An empty HitTestTransparent "+
			"overlay spanning the screen must not take the press — it is lifted, so "+
			"it beats the page on every other axis and the flag is the only thing "+
			"stopping it eating every click", hit)
	}
}

// TestTheHitWalkInheritsTheRankNotJustTheMembership is the mutation the
// PR's own matrix scored membership for and rank not at all.
//
// W1 (ask membership per node instead of inheriting it) was caught. The
// SEPARATE mutation
//
//	hitTest(kid, x, y, depth+1, overlay, 0, order, best)
//
// — inherit membership, drop the rank — was SILENT across the whole root
// module. Every other fixture either has one rank in it or puts the
// deciding component at the lifting root itself, so nothing saw a
// rank-20 host's CHILD fall to rank 0.
//
// The discriminating shape is the one TestALiftedSubtreeIsNotSplitBy-
// ItsChildsRank uses for paint, asked of the click: a host at
// OverlayRankAdornment declared FIRST with an ordinary child inside,
// against a host at OverlayRankToast declared second. Paint puts the
// child last, because a lifted subtree is contiguous and carries its
// root's rank; the walk must agree, or the click goes to the toast while
// the adornment is what the user can see.
//
// The child is an ORDINARY stripe on purpose — not an Overlay. Its whole
// claim to rank 20 is inheritance, so a walk that asks each node for
// itself gives it rank 0 and the plain layer, and the fixture reports the
// difference rather than a coincidence. Raised in review of #478.
func TestTheHitWalkInheritsTheRankNotJustTheMembership(t *testing.T) {
	inside := &stripe{ch: 'A'}
	adorn := &rankedBox{
		stripe: stripe{ch: 'B', rank: OverlayRankAdornment},
		kids:   []Component{inside},
	}
	toast := &rankedStripe{stripe{ch: 'T', rank: OverlayRankToast}}

	root := &twoKids{kids: []Component{adorn, toast}}
	c := NewComposer(root, 12, 3)
	t.Cleanup(c.Close)
	f, _ := c.Frame()

	// PAINT SAYS THE CHILD, and reading it back is what keeps this test
	// from asserting the walk agrees with itself.
	if got := render.RowText(f.Cells, 0); !strings.HasPrefix(got, "A") {
		t.Fatalf("row 0 = %q, want the adornment's child on top — if paint does not "+
			"put it there, the claim this test makes about input is not the claim "+
			"about agreement", got)
	}

	if hit := NewFocusManager(root).HitTest(0, 0); hit != Component(inside) {
		t.Errorf("HitTest returned %T, want the ordinary child inside the "+
			"rank-%d host. It is an overlay only by INHERITANCE and ranked only "+
			"by inheritance; a walk that carries membership down and leaves the "+
			"rank behind gives it rank %d, loses to the rank-%d toast declared "+
			"after it, and hands the press to a component painting underneath.",
			hit, OverlayRankAdornment, OverlayRankPopup, OverlayRankToast)
	}
}

// escapeBox is an overlay container that arranges its child OUTSIDE its
// own rect — the shape Popup.ArrangeSurface produces, where the owner
// picks a rectangle for the surface with no regard for its own.
type escapeBox struct {
	stripe
	kid Component
	at  Rect
}

func (e *escapeBox) OverlaysPage()                {}
func (e *escapeBox) ChildComponents() []Component { return []Component{e.kid} }
func (e *escapeBox) Arrange(r Rect) {
	// ITS OWN BOUNDS ARE ONE ROW. twoKids hands every child the whole
	// rect, so without this the owner contains the child after all and
	// the fixture cannot express "outside its parent".
	e.Base.Arrange(Rect{X: r.X, Y: r.Y, W: r.W, H: 1})
	ArrangeChild(e.kid, e.at)
}

// TestAnOverlayOutsideItsParentPaintsAndIsNotHit pins the one place the
// two planes still disagree, so it cannot quietly become something else.
//
// This is a divergence, not a bug, and the distinction is the reason it
// is pinned rather than fixed here. Paint walks c.paint flat and clips
// each node to ITS OWN bounds; the hit walk prunes on bounds at EVERY
// node, so a subtree whose parent does not contain the point is never
// entered. A surface placed outside its owner's rect — which is exactly
// what ArrangeSurface exists to do — therefore paints and cannot be hit.
//
// Nothing shipped is in that position without also holding capture:
// popupSurface is the only non-transparent Overlay in the tree whose
// bounds can escape its parent, and Popup.Open takes the pointer. So the
// resolution taken in #478 was to say what the walk does, at
// FocusManager.HitTest and the three other files that were claiming more
// than it does. Descending into a lifted subtree regardless of the
// ancestor prune is the other resolution and is a behaviour change.
//
// The test asserts BOTH halves. Without the paint arm it would pass
// against a walk that had stopped painting the escaped child too, which
// is agreement of the useless kind. Raised in review of #478.
func TestAnOverlayOutsideItsParentPaintsAndIsNotHit(t *testing.T) {
	inside := &stripe{ch: 'S'}
	// The owner occupies row 0; its child is placed on row 1, which the
	// owner's own bounds do not contain.
	owner := &escapeBox{
		stripe: stripe{ch: ' '},
		kid:    inside,
		at:     Rect{X: 0, Y: 1, W: 6, H: 1},
	}
	page := &stripe{ch: 'P'}

	root := &twoKids{kids: []Component{owner, page}}
	c := NewComposer(root, 12, 3)
	t.Cleanup(c.Close)
	f, _ := c.Frame()

	// PAINT: the escaped child owns its cells, because paint clips it to
	// its own rect and asks nothing about its parent's.
	if got := render.RowText(f.Cells, 1); !strings.HasPrefix(got, "SSSSSS") {
		t.Fatalf("row 1 = %q — the escaped child did not paint, so the divergence "+
			"this test is about is not the one on screen", got)
	}

	// INPUT: it is not hit, because the walk pruned at the owner.
	if hit := NewFocusManager(root).HitTest(0, 1); hit == Component(inside) {
		t.Error("HitTest reached a child arranged outside its parent's bounds. That " +
			"is a BEHAVIOUR CHANGE and a good one, but it is not what mouse.go, " +
			"docs/architecture.md, components/popup.go and components/menu.go now " +
			"say — update the contract sentence in all four, and say why the " +
			"ancestor prune was dropped.")
	}
	if hit := NewFocusManager(root).HitTest(0, 1); hit != Component(page) {
		t.Errorf("HitTest returned %T for a cell the escaped child paints and the "+
			"page also covers; want the page, which is what the ancestor prune "+
			"leaves as the answer", hit)
	}
}

// TestTheHitWalkAllocatesNothing pins the cost the design was
// constrained by, because it is the reason #465 could not simply ask the
// Composer for c.paint and index it.
//
// HitTest runs on every motion report, and ?1003h sends one per cell
// crossed. The rewrite threads its running best through a pointer and
// numbers nodes with an int rather than collecting candidates, and none
// of that is visible in any behavioural assertion — a version that built
// a slice per event would pass every other test in this file.
//
// AllocsPerRun and not a benchmark: a benchmark reports a number nobody
// reads, and the claim here is a zero, which is a test.
//
// THIS COMMENT WAS SITTING ABOVE A DIFFERENT FUNCTION until review of
// #478 — TestTheHitWalkInheritsTheRankNotJustTheMembership, inserted
// between it and the function it names, which left that test with two
// stacked doc blocks and this one with none. gofmt and vet are both
// blind to it. Third occurrence in this story (the rebase note in
// TestTheHitTestExemptionIsLineScoped and qualifierRes's header were the
// first two), so it is filed rather than only fixed:
// https://github.com/WonderForgeLabs/gooey/issues/483.
func TestTheHitWalkAllocatesNothing(t *testing.T) {
	over := &rankedStripe{stripe{ch: 'O', rank: OverlayRankToast}}
	inner := &twoKids{kids: []Component{&stripe{ch: 'A'}, &stripe{ch: 'B'}}}
	root := &twoKids{kids: []Component{over, inner, &stripe{ch: 'P'}}}
	NewComposer(root, 12, 3).Frame()
	m := NewFocusManager(root)

	// NON-VACUITY: a walk that returned nil immediately would also
	// allocate nothing.
	if hit := m.HitTest(0, 0); hit != Component(over) {
		t.Fatalf("the fixture does not hit what it should (%T), so the allocation "+
			"count below is measuring the wrong walk", hit)
	}
	if n := testing.AllocsPerRun(100, func() { m.HitTest(0, 0) }); n != 0 {
		t.Errorf("HitTest allocated %v times per call, want 0. It runs on every motion "+
			"report — ?1003h sends one per cell crossed — so per-event garbage here is "+
			"paid on every pointer move across the screen", n)
	}
}
