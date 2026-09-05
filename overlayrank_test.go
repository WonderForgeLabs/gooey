package gooey

import (
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey/render"
)

// The overlay layer is RANKED, not declaration-ordered (#439).
//
// #437 lifted overlays out of document order into a second layer, which
// fixed #430 — but only popupSurface adopted the marker, so ToastHost,
// AdornmentLayer and therefore Tooltip stayed in the ordinary layer and
// fell BENEATH every open popup. That reversed three written claims at
// once: toast.go's "above the page", the markup reference's "tooltips
// paint above toasts too", and menu_live_test.go's "the toast layer is
// topmost".
//
// Adopting the marker on all three restores "above the page" and leaves
// the rest to declaration order WITHIN the layer, which is the part
// worth not doing. The framework separately tells an author to declare
// the MenuBar LAST so its dropdown covers the page; getting a toast
// above that dropdown would then also require declaring the ToastHost
// after it. Two rules pulling opposite ways, and the wrong choice is
// silent — the toast simply does not appear, on exactly the frames
// somebody most wanted to read it.
//
// So the layer carries a RANK. These tests are about the mechanism; the
// user-visible claim it exists to make true — a notification is never
// hidden by a menu — is pinned in components/overlayrank_test.go against
// the real ToastHost and MenuBar.

// stripe is a leaf that fills its bounds with one rune, so "who is on
// top" is a question the cell plane answers directly.
type stripe struct {
	Base
	ch   rune
	rank int
}

func (s *stripe) Measure(avail Size) Size { return avail }

func (s *stripe) Render(f *Frame) {
	b := s.Bounds()
	for y := b.Y; y < b.Y+b.H; y++ {
		f.Cells.SetString(b.X, y, strings.Repeat(string(s.ch), b.W), render.Style{})
	}
}

// overlayStripe and rankedStripe are separate types because Overlay is a
// MARKER: a bool field cannot express "sometimes implements the
// interface", which is the same reason the marker has an empty method.
type overlayStripe struct{ stripe }

func (o *overlayStripe) OverlaysPage() {}

type rankedStripe struct{ stripe }

func (r *rankedStripe) OverlaysPage()    {}
func (r *rankedStripe) OverlayRank() int { return r.rank }

// rankFixture composes the children over ONE rect — twoKids arranges
// every child to its own bounds — and returns the frame. Full overlap is
// what makes the first cell of row 0 the answer to "who won".
func rankFixture(t *testing.T, kids ...Component) *Frame {
	t.Helper()
	root := &twoKids{kids: kids}
	c := NewComposer(root, 12, 3)
	t.Cleanup(c.Close)
	f, _ := c.Frame()
	return f
}

func rankRow(t *testing.T, f *Frame) string {
	t.Helper()
	return render.RowText(f.Cells, 0)
}

// TestAHigherRankPaintsOverALowerOneDeclaredLater is the whole point: a
// rank beats declaration order, in the direction that matters.
//
// The higher-ranked one is declared FIRST, which is the arrangement an
// app actually has — the framework tells it to declare the MenuBar last,
// and a page-wide ToastHost sits above that in the document.
func TestAHigherRankPaintsOverALowerOneDeclaredLater(t *testing.T) {
	top := &rankedStripe{stripe{ch: 'T', rank: 2}}
	bottom := &rankedStripe{stripe{ch: 'B', rank: 1}}
	got := rankRow(t, rankFixture(t, top, bottom))
	if !strings.HasPrefix(got, "T") {
		t.Errorf("the higher-ranked overlay was declared FIRST and lost to the lower one: row %q", got)
	}
}

// TestEqualRanksKeepDocumentOrder — the rank overrides document order
// only BETWEEN ranks. Two popups still paint in the order they were
// declared, which is the limit #437 documented and this does not claim
// to fix.
func TestEqualRanksKeepDocumentOrder(t *testing.T) {
	first := &rankedStripe{stripe{ch: 'F', rank: 1}}
	second := &rankedStripe{stripe{ch: 'S', rank: 1}}
	got := rankRow(t, rankFixture(t, first, second))
	if !strings.HasPrefix(got, "S") {
		t.Errorf("two overlays of equal rank did not keep document order: row %q", got)
	}
}

// TestAnUnrankedOverlayIsRankZero. Every Overlay implementor predates
// the rank and must keep behaving exactly as it did — above the page,
// ordered among its equals by declaration. Declared AFTER the ranked
// one here, so document order alone would put it on top.
func TestAnUnrankedOverlayIsRankZero(t *testing.T) {
	ranked := &rankedStripe{stripe{ch: 'R', rank: 1}}
	plain := &overlayStripe{stripe{ch: 'P'}}
	got := rankRow(t, rankFixture(t, ranked, plain))
	if !strings.HasPrefix(got, "R") {
		t.Errorf("an unranked overlay declared later beat a ranked one: row %q — rank 0 is not the floor", got)
	}
}

// rankedBox is a ranked overlay CONTAINER that fills its own bounds, so
// it covers any child that paints before it. The combination is what
// makes the next test able to see a split subtree at all: a chrome-only
// container would paint nothing and hide the defect.
type rankedBox struct {
	stripe
	kids []Component
}

func (b *rankedBox) OverlaysPage()                {}
func (b *rankedBox) OverlayRank() int             { return b.rank }
func (b *rankedBox) ChildComponents() []Component { return b.kids }
func (b *rankedBox) Arrange(r Rect) {
	b.Base.Arrange(r)
	for _, k := range b.kids {
		ArrangeChild(k, r)
	}
}

// TestALiftedSubtreeIsNotSplitByItsChildsRank is the contiguity rule,
// and it is the one clause of orderPaint that no user-facing behaviour
// reaches — so it is pinned directly or not at all.
//
// A nested Overlay inside an already-lifted subtree must keep the
// OUTER rank. If each node answered for itself, this child (rank 0)
// would sort ahead of its parent (rank 2), the parent would paint
// AFTER it, and a parent that covers its bounds would erase the very
// child it lifted. The reordered walk cannot put it back: forcing runs
// forward only, which is the whole reason the overlay layer exists.
func TestALiftedSubtreeIsNotSplitByItsChildsRank(t *testing.T) {
	inner := &overlayStripe{stripe{ch: 'I'}} // an Overlay, rank 0
	outer := &rankedBox{stripe: stripe{ch: 'O', rank: 2}, kids: []Component{inner}}

	c := NewComposer(outer, 12, 3)
	t.Cleanup(c.Close)
	f, _ := c.Frame()

	if got := rankRow(t, f); !strings.HasPrefix(got, "I") {
		t.Errorf("a rank-0 Overlay nested inside a rank-2 one was sorted out of its parent's run, "+
			"so the parent painted over it: row %q", got)
	}
}

// TestTheOverlayLayerStillClearsThePage — the rank must not cost the
// property #437 bought. An ordinary component declared after everything
// still paints beneath the whole overlay layer.
func TestTheOverlayLayerStillClearsThePage(t *testing.T) {
	over := &rankedStripe{stripe{ch: 'O', rank: 1}}
	page := &stripe{ch: 'X'}
	got := rankRow(t, rankFixture(t, over, page))
	if !strings.HasPrefix(got, "O") {
		t.Errorf("an ordinary component declared last painted over the overlay layer: row %q", got)
	}
}
