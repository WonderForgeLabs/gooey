package gooey

import (
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey/render"
	"github.com/WonderForgeLabs/gooey/term"
)

// The two public paint paths must agree about what is on top (#438).
//
// #437 lifted overlays in Composer and left gooey.Compose walking
// ChildComponents in document order, so the framework had TWO exported
// answers to one question — and #430 still reproduced verbatim on the
// one-shot path.
//
// That is worse than a stale path. Compose is what cmd/pixels,
// cmd/typeahead --dump and around nineteen test helpers across
// components/, markup/ and the root compose with, so any future
// overlay-bearing fixture asserted through it would look green while
// encoding the bug.
//
// The issue framed this as implement-or-document, and named the cost of
// implementing: a second copy of the z-order rule, which the next change
// to that rule has to find. So the rule is EXTRACTED rather than copied
// — overlayOf is the one implementation of membership-and-rank, and
// appendByRank the one implementation of ordering; both paths call both.
// #439 added ranks days later, which is exactly the change that would
// have had to find two copies. (This comment named "paintOrder", which
// has never existed — git grep finds nothing and a reader following it
// finds no such symbol. Corrected in review of #457, along with the
// ordering half, which was still two implementations at the time.)

// oneShotStripe fills its bounds with a rune. Same shape as rankedStripe
// in overlayrank_test.go — naming the FILE because "above" was wrong in
// two directions: rankedStripe is in another file, and this file's own
// ranked fixture is below. Kept separate so a change to one test's
// fixture cannot silently retune the other's.
type oneShotStripe struct {
	Base
	ch   rune
	kids []Component
}

func (s *oneShotStripe) Measure(a Size) Size { return a }
func (s *oneShotStripe) Render(f *Frame) {
	b := s.Bounds()
	for y := b.Y; y < b.Y+b.H; y++ {
		f.Cells.SetString(b.X, y, strings.Repeat(string(s.ch), b.W), render.Style{})
	}
}
func (s *oneShotStripe) ChildComponents() []Component { return s.kids }
func (s *oneShotStripe) Arrange(r Rect) {
	s.Base.Arrange(r)
	for _, k := range s.kids {
		ArrangeChild(k, r)
	}
}

type oneShotOverlay struct{ oneShotStripe }

func (o *oneShotOverlay) OverlaysPage() {}

type oneShotRanked struct {
	oneShotStripe
	rank int
}

func (o *oneShotRanked) OverlaysPage()    {}
func (o *oneShotRanked) OverlayRank() int { return o.rank }

func oneShotCaps() term.Caps { return term.Caps{Cols: 12, Rows: 3} }

// TestComposeLiftsOverlaysTheWayComposerDoes is #430 asked of the
// one-shot path: an overlay declared BEFORE an ordinary sibling that
// covers it.
func TestComposeLiftsOverlaysTheWayComposerDoes(t *testing.T) {
	pop := &oneShotOverlay{oneShotStripe{ch: 'P'}}
	after := &oneShotStripe{ch: '@'} // declared later, overlaps entirely
	root := &oneShotStripe{ch: '.', kids: []Component{pop, after}}

	f := Compose(root, oneShotCaps(), nil)
	if got := render.RowText(f.Cells, 0); !strings.HasPrefix(got, "P") {
		t.Errorf("gooey.Compose painted an ordinary later sibling over an Overlay: row %q.\n"+
			"This is #430 on the one-shot path — the exact string the overlay-layer spec quotes as the failure", got)
	}
}

// TestBothPaintPathsAgree is the assertion that matters, and it is
// deliberately a COMPARISON rather than two separate expectations. Two
// exported paths that disagree about z-order is the defect; either one
// being individually wrong is a symptom.
func TestBothPaintPathsAgree(t *testing.T) {
	build := func() Component {
		return &oneShotStripe{ch: '.', kids: []Component{
			&oneShotOverlay{oneShotStripe{ch: 'P'}},
			&oneShotStripe{ch: '@'},
		}}
	}
	caps := oneShotCaps()

	one := render.RowText(Compose(build(), caps, nil).Cells, 0)

	c := NewComposer(build(), caps.Cols, caps.Rows)
	t.Cleanup(c.Close)
	fr, _ := c.Frame()
	retained := render.RowText(fr.Cells, 0)

	// NON-VACUITY FIRST. This test compares two strings and asserts
	// nothing about either, so a later change to oneShotStripe's
	// Measure/Arrange — or to ArrangeChild — that left both paths
	// painting a blank row would keep it green while it checked
	// nothing. The lift is what it exists to observe, so the overlay's
	// rune has to be at the front. Raised in review of #457.
	if !strings.HasPrefix(one, "P") {
		t.Fatalf("the fixture stopped exercising the lift — row %q does not start with the "+
			"overlay's rune, so the comparison below would pass vacuously", one)
	}

	if one != retained {
		t.Errorf("the two public paint paths disagree about what is on top:\n"+
			"  gooey.Compose  %q\n  Composer.Frame %q", one, retained)
	}
}

// TestBothPaintPathsAgreeOnRanks is finding 8's gap, and it is the arm
// that discriminates the two ORDERINGS rather than the two lifts.
//
// TestBothPaintPathsAgree uses one unranked overlay, so it cannot tell a
// bucket pass from a sort from a coin flip. TestComposeHonoursTheOverlayRank
// pins the one-shot side alone. Until the ordering was shared, the two
// implementations — a bucket pass here, a sort.SliceStable there — were
// never checked against each other at all.
//
// Thirteen nodes on purpose: Go's pdqsort short-circuits to insertion
// sort below twelve, which is stable by accident, so a smaller fixture
// agrees with itself under either rule. The ranks alternate so document
// order and rank order disagree everywhere.
func TestBothPaintPathsAgreeOnRanks(t *testing.T) {
	const n = 13
	build := func() Component {
		kids := make([]Component, 0, n)
		for i := 0; i < n; i++ {
			// Two ranks, alternating: every even-indexed child must end
			// up behind every odd-indexed one, and within each rank the
			// declaration order has to survive.
			kids = append(kids, &oneShotRanked{
				oneShotStripe{ch: rune('a' + i)}, 1 + i%2,
			})
		}
		return &oneShotStripe{ch: '.', kids: kids}
	}
	caps := oneShotCaps()

	one := render.RowText(Compose(build(), caps, nil).Cells, 0)
	c := NewComposer(build(), caps.Cols, caps.Rows)
	t.Cleanup(c.Close)
	fr, _ := c.Frame()
	retained := render.RowText(fr.Cells, 0)

	// The last child of the HIGHEST rank paints last, so it is what a
	// full-width stripe leaves on the row. n=13 makes index 12 even,
	// hence rank 1 — so the winner is the last odd index, 11 => 'l'.
	if !strings.HasPrefix(one, "l") {
		t.Fatalf("the one-shot path did not order by rank: row %q, want the last rank-2 "+
			"child ('l') on top", one)
	}
	if one != retained {
		t.Errorf("the two paint paths order equal ranks differently:\n"+
			"  gooey.Compose  %q\n  Composer.Frame %q", one, retained)
	}
}

// TestComposeHonoursTheOverlayRank — the rank is part of the rule, so a
// path that lifts but does not rank is still a second answer. This is
// the arm that would go red if the extraction had copied only the lift.
func TestComposeHonoursTheOverlayRank(t *testing.T) {
	high := &oneShotRanked{oneShotStripe{ch: 'H'}, 2}
	low := &oneShotRanked{oneShotStripe{ch: 'L'}, 1}
	// High declared FIRST, so document order alone would lose.
	root := &oneShotStripe{ch: '.', kids: []Component{high, low}}

	if got := render.RowText(Compose(root, oneShotCaps(), nil).Cells, 0); !strings.HasPrefix(got, "H") {
		t.Errorf("gooey.Compose ignored the overlay rank: row %q", got)
	}
}

// TestComposeKeepsALiftedSubtreeTogether. An overlay CONTAINER's
// children must come with it — leaving them behind paints them under the
// very surface they belong to, which is the reason membership is
// inherited rather than asked per node.
func TestComposeKeepsALiftedSubtreeTogether(t *testing.T) {
	inner := &oneShotStripe{ch: 'I'}
	pop := &oneShotOverlay{oneShotStripe{ch: 'P', kids: []Component{inner}}}
	after := &oneShotStripe{ch: '@'}
	root := &oneShotStripe{ch: '.', kids: []Component{pop, after}}

	if got := render.RowText(Compose(root, oneShotCaps(), nil).Cells, 0); !strings.HasPrefix(got, "I") {
		t.Errorf("an overlay container's child did not come up with it: row %q — "+
			"want the child on top of its own parent, both above the page", got)
	}
}

// TestComposeStillPaintsAPlainTreeInDocumentOrder is the guard against
// the fix: a tree with no overlay in it must be completely unaffected,
// or every one of the ~19 helpers that composes through this path has
// quietly changed meaning.
func TestComposeStillPaintsAPlainTreeInDocumentOrder(t *testing.T) {
	root := &oneShotStripe{ch: '.', kids: []Component{
		&oneShotStripe{ch: 'A'},
		&oneShotStripe{ch: 'B'}, // later sibling wins, as always
	}}
	if got := render.RowText(Compose(root, oneShotCaps(), nil).Cells, 0); !strings.HasPrefix(got, "B") {
		t.Errorf("a tree with no overlay no longer paints in document order: row %q", got)
	}
}
