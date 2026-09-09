package gooey

import "testing"

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
