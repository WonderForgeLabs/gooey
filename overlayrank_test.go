package gooey

import (
	"io/fs"
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
// shows up as the wrong rune.
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

// TestARankOrdersPaintAndNotHitTesting is the divergence the ranks
// CREATE, pinned rather than described.
//
// #437 lifted overlays out of document order for paint and left
// hit-testing alone, calling that a gap. A rank widens it into a
// contradiction an author can hit: paint answers by rank, hitTest still
// walks ChildComponents in REVERSE (mouse.go), so the later sibling
// wins the click. Declare a ranked host FIRST — which every
// author-facing doc now says is free — and the two planes disagree.
//
// Under the retired "declare it last" rule they agreed, because the
// thing on top was also the thing hit-testing found first. That is why
// this test arrives with the ranks and not with #437: the freedom is
// what makes the disagreement reachable.
//
// It is a TEST and not a paragraph because the two answers live in
// different files with no shared symbol between them — nothing about
// changing one drags the other into review. Raised in review of #456,
// where the docs granted the freedom and said nothing about the click.
func TestARankOrdersPaintAndNotHitTesting(t *testing.T) {
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

	// Same tree, same frame, opposite answer. If this ever returns `over`
	// the divergence closed — which would be good news, and would make
	// every page carrying the caveat wrong rather than merely stale.
	//
	// THE LIST IS DERIVED, NOT WRITTEN DOWN, and that is the whole of the
	// difference. It was a seven-file literal in this message, kept in
	// step by hand; review of #456 grepped for it and found FOUR more
	// pages carrying the same caveat — component.go's own Overlay doc
	// ("IT MOVES PAINT, NOT INPUT"), docs/specs/2026-09-05-overlay-ranks.md,
	// docs/specs/2026-08-30-overlay-layer.md and
	// docs/specs/2026-09-05-one-shot-overlay-order.md. A literal had
	// already been wrong twice before that (the two learn pages, then
	// this file's own CLAUDE.md entry), each time silently, each time
	// caught only because somebody happened to look. Eleven is not a
	// better number to maintain than seven; deriving it is the fix.
	//
	// citingPages is the derivation, and naming this test is what puts a
	// page on the list. The four pages that carried the caveat without
	// citing the test were edited to cite it, so the anchor covers them —
	// a caveat added later without the citation is invisible here, which
	// is the residual gap and the reason the convention is written into
	// CLAUDE.md's paragraph rather than only here. Raised in review of
	// #456.
	m := NewFocusManager(root)
	hit := m.HitTest(0, 0)
	if hit == Component(over) {
		t.Fatalf("hit-testing now agrees with paint — the ranked overlay took the "+
			"cell it paints. Delete the divergence caveat from these pages "+
			"rather than this test:\n\t%s",
			strings.Join(citingPages(t), "\n\t"))
	}
	if hit != Component(under) {
		t.Errorf("hit-testing returned %T, want the later-declared overlay: it walks "+
			"document order in reverse and knows nothing about ranks", hit)
	}
}

// citingPages walks the tree for every page naming this test, which is
// the convention that puts a page on the divergence list. It replaces a
// literal enumeration in the failure message above; see the comment
// there for why, and for the one gap it leaves.
//
// The walk excludes this file — it is the pin, not a page carrying the
// caveat — and prunes dot-directories at EVERY depth, not just the top:
// .claude/worktrees/ holds whole checkouts of this repo, so a walk
// anchored only at the root reports the same page several times on a
// developer machine and once in CI. vendor/ is pruned because it cannot
// carry this caveat and is most of the tree.
func citingPages(t *testing.T) []string {
	t.Helper()
	const self = "overlayrank_test.go"
	var pages []string
	err := filepath.WalkDir(".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != "." && (strings.HasPrefix(d.Name(), ".") || d.Name() == "vendor") {
				return fs.SkipDir
			}
			return nil
		}
		switch filepath.Ext(p) {
		case ".go", ".md":
		default:
			return nil
		}
		if d.Name() == self {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if strings.Contains(string(b), "TestARankOrdersPaintAndNotHitTesting") {
			pages = append(pages, filepath.ToSlash(p))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking for pages citing this test: %v", err)
	}
	sort.Strings(pages)
	return pages
}

// TestTheDivergenceListIsNotEmpty is the non-vacuity guard on the
// derivation above, and it is not ceremony. citingPages walks for a
// literal string; a rename of this test, a walk rooted elsewhere, or a
// prune that swallows its own target all produce an EMPTY list, and an
// empty list makes the failure message above say "delete the caveat from
// these pages:" followed by nothing — advice that reads as "there is
// nothing to do" at the exact moment there is most to do. Failing here
// instead says which half broke.
//
// The floor is a floor rather than a count, for the reason CLAUDE.md's
// Verify section gives about numbers in prose: an exact figure is a
// sample taken once, and this one was 7 until review of #456 measured
// 11. What must hold is that the walk reaches the caveat's home pages at
// all.
func TestTheDivergenceListIsNotEmpty(t *testing.T) {
	pages := citingPages(t)
	if len(pages) < 8 {
		t.Fatalf("the derived divergence list holds %d pages: %v\nA caveat this "+
			"widely repeated cannot have shrunk to that, so suspect the "+
			"derivation — a renamed test, a walk rooted elsewhere, or a prune "+
			"that swallowed its own target — before believing the pages went "+
			"away", len(pages), pages)
	}
	// THE THREE SURFACES THE CAVEAT MUST REACH, by kind rather than by
	// name: the framework's own doc comments, the reference and spec
	// prose, and the learn path where the freedom is GRANTED to a
	// first-time reader. Losing one whole kind is the regression a total
	// count hides, and the learn pages are exactly what an earlier
	// literal list had missed.
	kinds := map[string]bool{}
	for _, p := range pages {
		switch {
		case strings.HasPrefix(p, "docs/learn/"):
			kinds["learn"] = true
		case strings.HasPrefix(p, "docs/"):
			kinds["docs"] = true
		case strings.HasSuffix(p, ".go"):
			kinds["code"] = true
		}
	}
	for _, want := range []string{"learn", "docs", "code"} {
		if !kinds[want] {
			t.Errorf("no %s page cites this test, so the divergence caveat there "+
				"(if any) will outlive the behaviour: %v", want, pages)
		}
	}
}
