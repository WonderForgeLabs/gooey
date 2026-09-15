package gooey

import (
	"os"
	"path/filepath"
	"sort"
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
// worth not doing. Whether a toast covers an open menu would then depend
// on which of the two an app happened to type last — a decision nobody
// makes deliberately, with a silent wrong answer one way round: the
// toast simply does not appear, on exactly the frames somebody most
// wanted to read it.
//
// (An earlier version of this paragraph argued from "the framework tells
// an author to declare the MenuBar LAST", which was already false —
// #437's lift is global, so the bar's position stopped mattering for its
// own dropdown too. Found in review of #456; component.go carries the
// same retraction. The conclusion does not need the premise.)
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
// The higher-ranked one is declared FIRST, which is the WORST case for
// declaration order: it is exactly the arrangement in which the document
// would put the lower-ranked one on top, so a rank that did not work
// shows up as the wrong rune. It is also the arrangement an app actually
// has — a MenuBar somewhere in the page and a page-wide ToastHost after
// it. The framework USED to tell you to declare the MenuBar last; #437's
// global lift already made that irrelevant and #443 retired the wording,
// so the fixture's shape is the worst case rather than an instruction
// being followed.
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

// TestARankOrdersHitTestingAsWellAsPaint is the INVERSION of
// TestARankOrdersPaintAndNotHitTesting, which pinned the divergence
// while it stood.
//
// That test said: paint answers by rank, hitTest walks ChildComponents
// in reverse, so declare a ranked host first — which every author-facing
// doc says is free — and the two planes disagree. It ended by naming the
// four files whose caveats would come out if hit-testing ever became
// rank-aware, and #465 is that change; the caveats came out with it.
//
// It stays a TEST and not a paragraph for the reason the old one gave:
// the two answers live in different files with no shared symbol between
// them, so nothing about changing one drags the other into review. What
// changed is the direction, not the argument.
func TestARankOrdersHitTestingAsWellAsPaint(t *testing.T) {
	// BOTH are overlays, and that is what makes the paint arm about the
	// RANK rather than about the lift. A plain leaf as the loser was the
	// first version of this fixture, and mutating overlayRank to return
	// the floor left it green: the lift alone puts any Overlay after any
	// non-overlay, so the assertion held with ranks entirely disabled.
	// Two overlays that differ ONLY in rank is the discriminating shape,
	// and it is also the shape #439 was reported as — a toast above an
	// open popup.
	//
	// The higher-ranked one is declared FIRST, so document order and rank
	// point opposite ways.
	over := &rankedStripe{stripe{ch: 'O', rank: OverlayRankToast}}
	under := &overlayStripe{stripe{ch: 'U'}}
	root := &twoKids{kids: []Component{over, under}}

	c := NewComposer(root, 12, 3)
	t.Cleanup(c.Close)
	f, _ := c.Frame()
	if got := render.RowText(f.Cells, 0); !strings.HasPrefix(got, "O") {
		t.Fatalf("PAINT: the ranked overlay was declared first and lost the cells: row %q", got)
	}

	// SAME TREE, SAME FRAME, SAME ANSWER — which is the whole claim.
	//
	// `under` is the later sibling and the lower rank, so a walk that
	// still preferred document order returns it and a walk that asks
	// overlayOf returns `over`. Nothing else in the fixture separates
	// them.
	//
	// THE PAGE LIST IS DERIVED, not written here. This message carried a
	// literal six-file list until review of #458 pointed out that the
	// test it replaced had derived exactly that list — because the
	// seven-file literal BEFORE it was missing four pages. A file
	// renamed or a caveat written next quarter would leave the advice
	// stale in the message a future reader follows.
	pages := citingPages(t, "TestARankOrdersHitTestingAsWellAsPaint")
	if len(pages) == 0 {
		t.Fatalf("no page outside this file names this test, so the message below " +
			"would be advice with nothing after it. Either the walk stopped " +
			"matching or the caveat pages stopped citing the test that polices " +
			"them; both are the failure this floor is for")
	}
	m := NewFocusManager(root)
	hit := m.HitTest(0, 0)
	if hit == Component(under) {
		t.Fatalf("HIT: the later-declared, lower-ranked overlay took the press for a "+
			"cell `over` paints. Hit-testing is back on document order alone, "+
			"which is #465 — and what these pages say about it is wrong "+
			"again:\n\t%s", strings.Join(pages, "\n\t"))
	}
	if hit != Component(over) {
		t.Errorf("hit-testing returned %T, want the higher-ranked overlay — the same one "+
			"paint put on top", hit)
	}
}

// TestANonConstantRankPartsThePlanes is the cost of OverlayRanker's
// contract, measured rather than asserted in prose.
//
// component.go says "Return a constant" and now says WHY in the sharper
// form this branch introduced: paint SAMPLES the rank at structural
// re-sync (Composer.orderPaint writes it), while hitTest reads it LIVE
// through overlayOf on every motion event. Nothing refuses a varying
// rank — OverlayRank() is a plain method on an exported interface — so
// the sentence was the whole enforcement, and the failure it warns about
// is now a PLANE DISAGREEMENT rather than a late restack. That is the
// exact divergence #465 removed, reachable only through this contract,
// and this file's own standard is that the planes "cannot part again"
// (TestARankOrdersHitTestingAsWellAsPaint).
//
// So the assertion is the disagreement itself, stated exactly. It is not
// a bug report against the framework: sampling is deliberate, and making
// paint live would cost a per-frame walk. It is a pin on the DOCUMENTED
// answer, and it goes red in both directions — if the two planes ever
// agree here, either paint started reading live or hitTest started
// reading a sample, and component.go's paragraph has to change with
// whichever it was. Raised in review of #458.
func TestANonConstantRankPartsThePlanes(t *testing.T) {
	// The same two-overlays-differing-only-in-rank fixture the agreement
	// test uses, for the same reason: a plain leaf as the loser would
	// make this about the lift rather than the rank.
	//
	// BOTH RANKED, AND THE FLIP LANDS ABOVE THE FLOOR. This used
	// `overlayStripe` for `under` and flipped `over` to -1, which
	// overlayRank CLAMPS to OverlayRankPopup — the same rank `under`
	// already had. The post-flip answer then came from beatenBy's
	// POSITION tie-break while the message below described a rank
	// comparison, so a reader debugging it would study a comparison that
	// never ran. Adornment over Toast, flipped to Popup, is a real
	// inversion at every step.
	//
	// AND `over` IS THE LATER SIBLING IN THIS FIXTURE, which is the half
	// that makes the post-flip assertion a rank claim rather than a
	// tie-break one. Position is the LAST thing beatenBy compares, after
	// layer and rank, so it decides only a tie — but a tie is exactly
	// what a walk that had stopped comparing ranks would see. With
	// `over` earlier, that walk answers `under` too and the assertion
	// passes for the wrong reason. As the later sibling, `over` wins
	// every tie, so only a live rank comparison can hand the press to
	// `under`; removing the rank arm from beatenBy reddens this, which
	// is how the arrangement was checked rather than argued. Raised in
	// review of #458.
	over := &rankedStripe{stripe{ch: 'O', rank: OverlayRankAdornment}}
	under := &rankedStripe{stripe{ch: 'U', rank: OverlayRankToast}}
	root := &twoKids{kids: []Component{under, over}}

	c := NewComposer(root, 12, 3)
	t.Cleanup(c.Close)
	f, _ := c.Frame()
	m := NewFocusManager(root)
	// THE PRECONDITION, because the whole test is a CHANGE in the
	// answer: if the planes did not agree before the flip there is
	// nothing for the flip to part.
	if got := render.RowText(f.Cells, 0); !strings.HasPrefix(got, "O") {
		t.Fatalf("before the flip the higher-ranked overlay does not own the cells: row %q", got)
	}
	if m.HitTest(0, 0) != Component(over) {
		t.Fatalf("before the flip the planes already disagree, so this test measures nothing")
	}

	// A VARYING RANK, with no structural change to force a re-sync —
	// which is what a real one would look like: a component returning a
	// value that depends on its own state, read on a frame nobody
	// rebuilt.
	over.rank = OverlayRankPopup
	f, _ = c.Frame()

	if got := render.RowText(f.Cells, 0); !strings.HasPrefix(got, "O") {
		t.Errorf("PAINT followed the new rank (row %q). component.go says paint "+
			"samples the rank at re-sync, so it must still show the overlay that "+
			"was ranked highest when the tree was last synced. If orderPaint now "+
			"runs per frame, the OverlayRanker paragraph naming this divergence "+
			"is out of date", got)
	}
	if hit := m.HitTest(0, 0); hit != Component(under) {
		t.Errorf("HIT returned %T; component.go says hitTest reads the rank live "+
			"through overlayOf, so after the flip it must answer with the other "+
			"overlay — the planes parting is the documented cost of a "+
			"non-constant rank. If the hit walk now reads a sample too, the two "+
			"planes agree again and that paragraph should say so", hit)
	}
}

// citingPages is every tracked file that names testName, minus the file
// the name is DEFINED in.
//
// It exists because the message below used to carry the list as a
// literal, and a literal list of six went into this branch two rounds
// after review of #456 had derived the same list away — the seven-file
// version it replaced was missing four pages, component.go's own Overlay
// doc among them. A page joins by citing the test, which is also the act
// that makes the page depend on it.
//
// docFilesIn is the same walk every guard in zorderdocs_test.go uses, so
// the set is tracked files only and the two cannot drift.
func citingPages(t *testing.T, testName string) []string {
	t.Helper()
	var out []string
	for _, p := range docFilesIn(t, ".") {
		if isTheDivergencePin(p) {
			continue
		}
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("reading %s: %v", p, err)
		}
		if strings.Contains(string(b), testName) {
			out = append(out, filepath.ToSlash(p))
		}
	}
	sort.Strings(out)
	return out
}

// isTheDivergencePin is the one file citingPages must not report: this
// one, where the test is DEFINED. The path is the whole path rather than
// a basename, for the reason docFilesIn's own exemption gives — a
// basename exempts a same-named file at any depth.
//
// Clean before the compare, because the walk's spelling is not the only
// one a caller has: docFilesIn(".") yields a bare `overlayrank_test.go`
// while the honesty arm below hands it `./overlayrank_test.go`, and an
// exemption that answers differently for two spellings of one file is
// the same defect one level down.
func isTheDivergencePin(p string) bool {
	return filepath.ToSlash(filepath.Clean(p)) == "overlayrank_test.go"
}

// TestTheDivergencePinExcludesItselfAndNothingElse keeps the exemption
// from becoming a class. An exemption that matches more than the one
// file it was written for is how a derived list quietly becomes a
// shorter derived list.
func TestTheDivergencePinExcludesItselfAndNothingElse(t *testing.T) {
	for _, p := range []string{
		"overlayrank_test.go", "./overlayrank_test.go",
	} {
		if !isTheDivergencePin(p) {
			t.Errorf("isTheDivergencePin(%q) is false; this file must be excluded "+
				"however the walk spells its path", p)
		}
	}
	for _, p := range []string{
		"packs/temporal-workflow/overlayrank_test.go",
		"components/overlayrank_test.go",
		"overlayrank.go",
		"overlayonepass_test.go",
	} {
		if isTheDivergencePin(p) {
			t.Errorf("isTheDivergencePin(%q) is true; the exemption is for the one "+
				"file that DEFINES the test, and anything wider silently shrinks "+
				"the list the failure message derives", p)
		}
	}
}
