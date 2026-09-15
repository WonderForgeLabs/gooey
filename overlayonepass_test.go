package gooey

import (
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey/prop"
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
//
// The comparison is of two whole rendered rows, so it can see ANY picture
// difference — not only z-order. What makes this fixture a z-order test is
// oneShotStripe.Render writing exactly b.W runes at b.X: every component
// fills its own rect, so nothing but order can differ. An overlay leaf
// that filled only PART of its rect would make the same comparison fail on
// the pre-clear instead, which is what
// TestBothPaintPathsAgreeOnLeafOcclusion below is for. Said explicitly
// because the earlier wording ("compares z-order and nothing else")
// invited changing the fixture on a guarantee the test does not give.
// Raised in review of #457.
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

// TestBothPaintPathsAgreeOnAVisibleHiddenOrCollapsedRoot is finding 1 of
// #457's round 2, and it is the arm that catches the last picture
// difference between the paths.
//
// A ROOT IS THE ONLY PLACE THIS IS OBSERVABLE. Compose and Composer both
// call root.Arrange directly, bypassing the ArrangeChild sandwich, so a
// root keeps full-screen bounds whatever its Visibility says and its
// children keep theirs. Below the root ArrangeChild zeroes a Collapsed
// child's rect, every fill in paintOne becomes a no-op, and the two
// paths cannot be told apart — which is why a differential harness over
// random trees only found this once the ROOT carried a visibility.
//
// ALL THREE VISIBILITIES, not Collapsed alone. Collapsed was the one
// that diverged, but asserting it by itself would pass against a
// Compose that painted nothing for any of them. Visible and Hidden are
// what say the fixture paints at all, and the Hidden row carries the
// argument for the fix chosen: neither path honours a visibility on a
// root, so Hidden does not hide the child either. Collapsed pruning the
// subtree was the single exception to that rule, on one path only.
//
// The child's rune is what is compared, not the root's: the root is a
// container whose own Render is gated by paintable(), so on Hidden and
// Collapsed only the child can put anything on the row. A fixture whose
// root painted would agree for the wrong reason.
func TestBothPaintPathsAgreeOnAVisibleHiddenOrCollapsedRoot(t *testing.T) {
	build := func(v Visibility) Component {
		root := &oneShotStripe{ch: 'k', kids: []Component{
			&oneShotStripe{ch: 'c'},
		}}
		root.LayoutProps().Visibility = v
		return root
	}
	caps := oneShotCaps()

	for _, v := range []Visibility{Visible, Hidden, Collapsed} {
		one := render.RowText(Compose(build(v), caps, nil).Cells, 0)

		c := NewComposer(build(v), caps.Cols, caps.Rows)
		fr, _ := c.Frame()
		retained := render.RowText(fr.Cells, 0)
		c.Close()

		// NON-VACUITY, per visibility. Two blank rows compare equal, so
		// without this a fixture that stopped painting would satisfy
		// every assertion below.
		if strings.TrimSpace(one) == "" && strings.TrimSpace(retained) == "" {
			t.Errorf("both paths painted an empty row for a %v root, so the "+
				"comparison is vacuous — the fixture is no longer exercising "+
				"anything", v)
			continue
		}
		if one != retained {
			t.Errorf("the two paint paths disagree on a %v root:\n"+
				"  gooey.Compose  %q\n  Composer.Frame %q\n"+
				"Neither path applies the ArrangeChild sandwich to a root, so "+
				"neither honours its Visibility — a prune on one side only is "+
				"a blank frame against a painted one", v, one, retained)
		}
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

// oneShotPartial is components.Popup's shape: an overlay LEAF whose Render
// writes part of its rect and relies on the pre-clear for the rest.
// popupSurface (components/popup.go) documents its own opacity as coming
// from exactly that.
type oneShotPartial struct {
	Base
	mark string
}

func (p *oneShotPartial) Measure(a Size) Size { return a }
func (p *oneShotPartial) OverlaysPage()       {}
func (p *oneShotPartial) Render(f *Frame) {
	b := p.Bounds()
	f.Cells.SetString(b.X, b.Y, p.mark, render.Style{})
}

// TestBothPaintPathsAgreeOnLeafOcclusion is the half TestBothPaintPathsAgree
// cannot see, because its fixture fills every rect.
//
// Compose delivered POSITION WITHOUT OCCLUSION: it lifted the overlay to
// the front and then let the sibling beneath show through the cells the
// overlay's Render did not write. Measured before the fix:
//
//	gooey.Compose   "XX@@@@@@@@@@"
//	Composer.Frame  "XX          "
//
// Composer pre-clears every leaf to the nearest ancestor's background;
// Compose had no equivalent, because it has no paint nodes to walk up
// through. collectPaint now carries that background DOWN beside
// parentOverlay/parentRank, at no extra walk.
//
// Not a live break when it was found — cmd/typeahead --dump never opens
// its popup — but latent in the way #438 was filed about: an
// overlay-bearing fixture asserted through Compose would look green while
// encoding a see-through popup, with the doc comment saying the paths
// agree. Raised in review of #457.
func TestBothPaintPathsAgreeOnLeafOcclusion(t *testing.T) {
	build := func() Component {
		return &oneShotStripe{ch: '.', kids: []Component{
			&oneShotPartial{mark: "XX"},
			&oneShotStripe{ch: '@'}, // declared later, would show through
		}}
	}
	caps := oneShotCaps()

	one := render.RowText(Compose(build(), caps, nil).Cells, 0)

	c := NewComposer(build(), caps.Cols, caps.Rows)
	t.Cleanup(c.Close)
	fr, _ := c.Frame()
	retained := render.RowText(fr.Cells, 0)

	// NON-VACUITY: the overlay must still be lifted, or this compares two
	// pictures of the sibling and passes without testing occlusion.
	if !strings.HasPrefix(one, "XX") {
		t.Fatalf("the fixture stopped exercising the lift — row %q", one)
	}
	if one != retained {
		t.Errorf("a lifted leaf occludes on one path and not the other:\n"+
			"  gooey.Compose  %q\n  Composer.Frame %q\n"+
			"Compose is positioning the overlay without clearing behind it, so a "+
			"components.Popup renders see-through here and opaque under Composer.",
			one, retained)
	}
}

// oneShotPanel is a container that DECLARES a background, so a leaf
// inside it has an ancestor colour to pre-clear against.
type oneShotPanel struct {
	oneShotStripe
	bg *prop.Property[render.Color]
}

func (p *oneShotPanel) BackgroundProperty() *prop.Property[render.Color] { return p.bg }

// TestAOneShotLeafClearsToItsAncestorsBackground pins the half the
// occlusion test cannot see.
//
// Dropping the ancestor walk in collectPaint — clearing every leaf to the
// terminal default instead of the nearest declared Background — is SILENT
// against every other test in this file, because none of their fixtures
// declares a background. That is the same hole Composer.clearStyle exists
// to fill: a Text inside a coloured panel must not punch a
// default-coloured hole when it pre-clears.
//
// Asserted as a COMPARISON against Composer, like its neighbours, plus a
// direct check on the colour so a future change that made both paths
// clear to the default would not agree its way to green. Raised in review
// of #457.
func TestAOneShotLeafClearsToItsAncestorsBackground(t *testing.T) {
	blue := render.Color{Set: true, R: 0, G: 0, B: 200}
	build := func() Component {
		return &oneShotPanel{
			oneShotStripe: oneShotStripe{ch: '.', kids: []Component{
				&oneShotPartial{mark: "XX"},
				&oneShotStripe{ch: '@'},
			}},
			bg: prop.NewSource(blue),
		}
	}
	caps := oneShotCaps()

	f := Compose(build(), caps, nil)
	c := NewComposer(build(), caps.Cols, caps.Rows)
	t.Cleanup(c.Close)
	fr, _ := c.Frame()

	// A cell the overlay cleared but did not write: column 5 of row 0.
	got := f.Cells.At(5, 0)
	want := fr.Cells.At(5, 0)
	if got.Style.Bg != want.Style.Bg {
		t.Errorf("the two paths clear a lifted leaf to different backgrounds:\n"+
			"  gooey.Compose  %+v\n  Composer.Frame %+v", got.Style.Bg, want.Style.Bg)
	}
	// And the colour is the PANEL's, not the terminal default — otherwise
	// both paths agreeing on "default" would pass the comparison above
	// while punching a hole in the panel.
	if got.Style.Bg != blue {
		t.Errorf("a leaf inside a coloured panel pre-cleared to %+v, want the panel's %+v — "+
			"the nearest ancestor's background is not reaching collectPaint",
			got.Style.Bg, blue)
	}
}

// oneShotBare is a CONTAINER that declares a background and paints no
// chrome of its own. Everything it puts on screen comes from the
// framework's fill, which is what makes it the fixture for the fill
// rule: if the branch is missing, this component is INVISIBLE rather
// than wrong-coloured, and whatever painted under it shows through.
type oneShotBare struct {
	Base
	bg *prop.Property[render.Color]
}

func (b *oneShotBare) Measure(a Size) Size                              { return a }
func (b *oneShotBare) Render(*Frame)                                    {}
func (b *oneShotBare) ChildComponents() []Component                     { return nil }
func (b *oneShotBare) BackgroundProperty() *prop.Property[render.Color] { return b.bg }

// TestBothPaintPathsFillAContainerWhoseBackgroundIsCleared is the branch
// paintOne did not have.
//
// A background handle whose colour is UNSET still fills — with the
// nearest ancestor's background — and that is not an implementation
// detail: it is written into the HasBackground interface's own doc, and
// it is what makes clearing a background at runtime ERASE the old fill
// instead of stranding it. Composer has the branch (composer.go, the
// `else` beside `if col.Set`); paintOne had only the `col.Set` half, so
// under Compose the container declared a surface and painted nothing.
//
// The fixture puts the cleared container over a stripe, because a fill
// that only ever runs on a blank buffer is unobservable — a one-shot
// compose starts from nothing, so "fills with the ancestor's background"
// and "does not fill at all" agree on every cell no earlier sibling
// touched. The stripe is what makes the two answers different, and it is
// also the real shape: an overlapping sibling is exactly what a declared
// surface exists to occlude.
//
// Asserted as a COMPARISON against Composer plus a direct check on the
// rune, so a future change that stopped both paths filling would not
// agree its way to green. Raised in review of #457.
func TestBothPaintPathsFillAContainerWhoseBackgroundIsCleared(t *testing.T) {
	blue := render.Color{Set: true, R: 0, G: 0, B: 200}
	build := func() Component {
		return &oneShotPanel{
			oneShotStripe: oneShotStripe{ch: '.', kids: []Component{
				&oneShotStripe{ch: '@'},
				// Declared LAST, so it paints over the stripe on both
				// paths — this is ordinary document order, not the
				// overlay layer.
				&oneShotBare{bg: prop.NewSource(render.Color{})},
			}},
			bg: prop.NewSource(blue),
		}
	}
	caps := oneShotCaps()

	f := Compose(build(), caps, nil)
	c := NewComposer(build(), caps.Cols, caps.Rows)
	t.Cleanup(c.Close)
	fr, _ := c.Frame()

	got, want := render.RowText(f.Cells, 0), render.RowText(fr.Cells, 0)
	if got != want {
		t.Errorf("the two paths disagree about a container whose background was cleared:\n"+
			"  gooey.Compose  %q\n  Composer.Frame %q", got, want)
	}
	if strings.ContainsRune(got, '@') {
		t.Errorf("row 0 is %q — the stripe beneath a declared surface is still "+
			"showing through, so the container did not fill", got)
	}
	// And it filled to the PANEL's colour, not the terminal default,
	// which is the half "it covered the stripe" cannot see.
	if bg := f.Cells.At(5, 0).Style.Bg; bg != blue {
		t.Errorf("the cleared container filled with %+v, want the panel's %+v — "+
			"an unset colour must take the nearest ancestor's background", bg, blue)
	}
}

// TestBothPaintPathsBlankAHiddenContainersBounds is the third branch,
// and it is here because fixing only the one the review named would have
// left a reader finding two of three.
//
// Composer's pre-clear is one if/else-if chain — leaf, then HIDDEN
// container, then declared background — and the middle arm is not a
// nicety: a hidden container's chrome has to leave the screen the way a
// hidden leaf's content does. paintOne never saw the case at all,
// because collectPaint gated COLLECTION on paintable(w) while Composer
// gates only Render. So a component that should paint a blank rect and
// nothing else was dropped instead.
//
// Same fixture logic as the cleared-background test above: on a blank
// one-shot buffer, "blank the rect" and "do nothing" agree everywhere no
// earlier sibling painted, so the stripe is what makes the branch
// observable. Raised in review of #457.
func TestBothPaintPathsBlankAHiddenContainersBounds(t *testing.T) {
	blue := render.Color{Set: true, R: 0, G: 0, B: 200}
	build := func() Component {
		// A stripe rather than the bare container, because it RENDERS.
		// Hidden is two claims — blank the bounds, and run no Render —
		// and a fixture that paints nothing can only see the first.
		hidden := &oneShotStripe{ch: '#'}
		L(hidden, Layout{Visibility: Hidden})
		return &oneShotPanel{
			oneShotStripe: oneShotStripe{ch: '.', kids: []Component{
				&oneShotStripe{ch: '@'},
				hidden,
			}},
			bg: prop.NewSource(blue),
		}
	}
	caps := oneShotCaps()

	f := Compose(build(), caps, nil)
	c := NewComposer(build(), caps.Cols, caps.Rows)
	t.Cleanup(c.Close)
	fr, _ := c.Frame()

	got, want := render.RowText(f.Cells, 0), render.RowText(fr.Cells, 0)
	if got != want {
		t.Errorf("the two paths disagree about a hidden container's bounds:\n"+
			"  gooey.Compose  %q\n  Composer.Frame %q", got, want)
	}
	if strings.ContainsAny(got, "@#") {
		t.Errorf("row 0 is %q — a hidden container either left the sibling "+
			"beneath it on screen (@) or painted its own chrome (#); it must "+
			"blank its bounds and Render nothing", got)
	}
}

// TestTheBucketPassSurvivesGrowingItsBucketList is the panic, and it is
// the leak fix's own bug: the clear loop pairs prev[i] with bs[i] by
// INDEX, and those are the same array only until the bucket list grows.
//
//	prev := *buckets          // the OLD header
//	...
//	keep = len(bs[i].items)   // from the NEW array once append grew it
//	clear(items[keep:cap(items)])
//
// `append` on a full slice reallocates, so after a frame that adds a
// rank, prev and bs are different arrays and the pairing is between
// unrelated buckets. When the new bucket at i holds more items than the
// old one's CAPACITY — three where the first frame put one — the slice
// expression is items[3:1] and the process dies with "slice bounds out
// of range". Not a leak, not a wrong picture: a panic on the retained
// paint path, which is every frame of every app that opens a second kind
// of overlay.
//
// THE SHAPE IS ORDINARY. Frame 1 is a page with one popup open. Frame 2
// is the same page with a toast and a tooltip up as well, and three
// items in the popup rank. That is `cmd/toolkit` doing what its overlays
// tab exists to demonstrate.
func TestTheBucketPassSurvivesGrowingItsBucketList(t *testing.T) {
	type item struct {
		rank int
		mark string
	}
	rankOf := func(it *item) int { return it.rank }
	var buckets []rankBucket[*item]

	// FRAME 1: one rank, one item. The bucket list is one long and its
	// single bucket's items has capacity 1.
	first := []*item{{rank: 0, mark: "popup"}}
	appendByRank(make([]*item, 0, len(first)), first, rankOf, &buckets)
	if len(buckets) != 1 {
		t.Fatalf("the first frame left %d buckets, want 1 — the fixture is not "+
			"the narrow state this test needs", len(buckets))
	}
	if c := cap(buckets[0].items); c > 2 {
		t.Fatalf("the first frame's bucket has capacity %d, which is too much "+
			"slack for the second frame to overrun. The arm depends on the "+
			"OLD bucket being smaller than the new one at the same index", c)
	}

	// FRAME 2, AND THE ORDER OF THIS SLICE IS THE WHOLE FIXTURE.
	//
	// While bs and prev are still the same array, every write to bs[i]
	// lands in prev[i] too, so the two stay in step and the pairing is
	// accidentally right. What breaks it is a rank REVISITED after the
	// bucket list has grown: the append that adds the second rank
	// reallocates, and from then on bs[0] is a copy that prev[0] no
	// longer tracks. Two more rank-0 items then push bs[0].items past
	// the capacity prev[0].items was frozen at, and the clear loop asks
	// for items[3:1].
	//
	// Document order interleaves ranks exactly like this — a popup, a
	// toast, then more of the popup's own subtree — so this is not a
	// contrived permutation. GROUPING the ranks instead lets every
	// rank-0 append happen before the growth, and the bug does not fire:
	// the first version of this fixture did that and passed against the
	// unfixed code, which is the reason the ordering is spelled out
	// here rather than left to look arbitrary.
	second := []*item{
		{rank: 0, mark: "popup"},
		{rank: 10, mark: "toast"},
		{rank: 0, mark: "popup"},
		{rank: 0, mark: "popup"},
	}
	got := appendByRank(make([]*item, 0, len(second)), second, rankOf, &buckets)

	if len(got) != len(second) {
		t.Fatalf("the second pass returned %d items, want %d", len(got), len(second))
	}
	// AND THE ORDER IS STILL THE ORDER, because a panic fix that quietly
	// stopped bucketing would pass a test that only checked it did not
	// crash.
	want := []string{"popup", "popup", "popup", "toast"}
	for i, it := range got {
		if it.mark != want[i] {
			t.Errorf("item %d is %q, want %q — the growth path has stopped "+
				"ordering by rank", i, it.mark, want[i])
		}
	}
}

// TestTheBucketPassRetainsNothingPastItsOwnItems is the leak the reuse
// buys, checked structurally rather than with a finalizer.
//
// appendByRank keeps its buckets between calls — that is what makes it
// allocation-free — and items holds *paintNode on the Composer's path,
// which outlives no frame. Two ways a dead node stayed reachable:
//
//   - a bucket the call did not reach (last frame had five ranks, this
//     one has two), and
//   - the TAIL of a bucket it did reach (last frame put ten nodes in a
//     rank, this one put three, and seven pointers sit past len in the
//     same backing array).
//
// Neither is overwritten until that slot is used again, which for a rank
// that stops occurring is never. Clearing to CAP rather than to len is
// the fix; len is what the next call resets, cap is what the collector
// sees.
//
// Reading past len is exactly what a leak check has to do, so this test
// slices to cap deliberately. Raised in review of #457.
func TestTheBucketPassRetainsNothingPastItsOwnItems(t *testing.T) {
	type item struct {
		rank int
		mark string
	}
	rankOf := func(it *item) int { return it.rank }
	var buckets []rankBucket[*item]

	// A wide frame: five ranks, several items each.
	var wide []*item
	for r := 0; r < 5; r++ {
		for n := 0; n < 4; n++ {
			wide = append(wide, &item{rank: r, mark: "wide"})
		}
	}
	appendByRank(make([]*item, 0, len(wide)), wide, rankOf, &buckets)

	// A narrow one: two ranks, one item each. Everything else is dead.
	narrow := []*item{{rank: 0, mark: "narrow"}, {rank: 1, mark: "narrow"}}
	got := appendByRank(make([]*item, 0, len(narrow)), narrow, rankOf, &buckets)
	if len(got) != 2 {
		t.Fatalf("the second pass returned %d items, want 2", len(got))
	}

	// NON-VACUITY: the buckets must actually still be reused, or there is
	// nothing that COULD retain and this test passes for the wrong reason.
	if cap(buckets) < 5 {
		t.Fatalf("the buckets were not reused (cap %d); this test cannot see a leak", cap(buckets))
	}

	live := map[*item]bool{}
	for _, it := range narrow {
		live[it] = true
	}
	held := 0
	for i := 0; i < cap(buckets); i++ {
		b := buckets[:cap(buckets)][i]
		for _, it := range b.items[:cap(b.items)] {
			if it != nil && !live[it] {
				held++
			}
		}
	}
	if held != 0 {
		t.Errorf("the bucket pass still references %d items from the previous call — "+
			"on the Composer's path those are *paintNode from a tree that no longer "+
			"exists, held until the slot happens to be reused", held)
	}
}

// TestTheComposerSlicesRetainNothingPastTheirOwnNodes is the same leak as
// TestTheBucketPassRetainsNothingPastItsOwnItems in the three places the
// comment beside `buckets` named without checking.
//
// c.paint, c.lifted, c.nodes and c.over were reset with `[:0]` and never
// cleared, so every *paintNode a shrunk tree used to have stayed
// reachable past len until its slot was written again — which for a list
// that shrinks and stays small is never. The buckets case was worse per
// rank and this one is worse per APP: a Dynamic list going from ten
// thousand rows to ten is an ordinary thing to do, and a five-rank frame
// is not.
//
// Reading past len is what a leak check has to do, so this slices to cap
// deliberately. The non-vacuity arm is the one that matters: without a
// real shrink there is nothing that COULD be retained and the assertion
// passes for the wrong reason. Raised in review of #456.
func TestTheComposerSlicesRetainNothingPastTheirOwnNodes(t *testing.T) {
	root := &oneShotStripe{ch: '.'}
	for i := 0; i < 40; i++ {
		root.kids = append(root.kids, &oneShotOverlay{oneShotStripe{ch: 'o'}})
	}
	c := NewComposer(root, 12, 3)
	c.SetCaps(oneShotCaps())
	c.Frame()

	wideNodes := len(c.nodes)
	root.kids = root.kids[:1]
	c.InvalidateStructure()
	c.Frame()
	if len(c.nodes) >= wideNodes {
		t.Fatalf("the tree did not shrink (%d nodes, was %d), so nothing could be "+
			"retained and this test cannot see the leak", len(c.nodes), wideNodes)
	}

	live := map[*paintNode]bool{}
	for _, n := range c.nodes {
		live[n] = true
	}
	for _, tc := range []struct {
		name string
		s    []*paintNode
	}{
		{"c.nodes", c.nodes},
		{"c.paint", c.paint},
		{"c.lifted", c.lifted},
		{"c.over", c.over},
	} {
		if cap(tc.s) <= len(tc.s) {
			continue // nothing past len; no slot to hold anything
		}
		held := 0
		for _, n := range tc.s[:cap(tc.s)][len(tc.s):] {
			if n != nil && !live[n] {
				held++
			}
		}
		if held != 0 {
			t.Errorf("%s still references %d *paintNode past its own length — nodes "+
				"from a tree that no longer exists, held until the slot happens to "+
				"be reused. Reset it with clearToCap", tc.name, held)
		}
	}
}

// TestEveryReusedSliceInComposerClearsToCap is the derived half of
// TestTheComposerSlicesRetainNothingPastTheirOwnNodes, and the half that
// can fail on a site nobody thought of.
//
// That test reads four slices by name, and its element type is
// []*paintNode — which structurally excludes c.startable and both
// []graphics.Placement resets, the two that held decoded image.Image.
// Three of the four sites this PR fixed were invisible to it. A table of
// known slices only ever fails on the ones already known, so this reads
// composer.go instead and fails on the NEXT one: any `x = x[:0]` is a
// reset that keeps its backing array, and must be clearToCap or say in a
// `retains nothing:` comment why the elements are safe to keep.
//
// The scan is over the AST, not the text, because `s = s[:0]` appears in
// clearToCap's own doc comment describing the shape it replaces — a grep
// for it reports the documentation as a violation.
func TestEveryReusedSliceInComposerClearsToCap(t *testing.T) {
	src, err := os.ReadFile("composer.go")
	if err != nil {
		t.Fatalf("read composer.go: %v", err)
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "composer.go", src, 0)
	if err != nil {
		t.Fatalf("parse composer.go: %v", err)
	}
	lines := strings.Split(string(src), "\n")
	text := func(e ast.Expr) string {
		return string(src[fset.Position(e.Pos()).Offset:fset.Position(e.End()).Offset])
	}
	// The justification may sit on the assignment's own line or on any of
	// the comment lines immediately above it.
	justified := func(line int) bool {
		for i := line - 1; i >= 0; i-- {
			if strings.Contains(lines[i], "retains nothing:") {
				return true
			}
			if i != line-1 && !strings.HasPrefix(strings.TrimSpace(lines[i]), "//") {
				return false
			}
		}
		return false
	}

	cleared := 0
	ast.Inspect(file, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		// PAIRWISE, because `a, b = a[:0], -1` is a reset too. This
		// bailed on len(Lhs) != 1, which made components/typeahead.go's
		// `t.buf, t.last = t.buf[:0], -1` unexaminable — and t.buf is
		// []rune, so the exemption was ACCIDENTAL rather than derived,
		// which is the distinction the tree-wide guard below argues for
		// in its own doc. The same reset written over a
		// []gooey.Component would have been a silent hole in a guard
		// whose stated subject is every reset there is. Unequal lengths
		// are `a, b = f()`, where no Rhs is a slice expression anyway.
		// Raised in review of #456.
		if !ok || as.Tok != token.ASSIGN || len(as.Lhs) != len(as.Rhs) {
			return true
		}
		for i, rhs := range as.Rhs {
			lhs := text(as.Lhs[i])
			if call, ok := rhs.(*ast.CallExpr); ok {
				if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "clearToCap" {
					cleared++
				}
				continue
			}
			sl, ok := rhs.(*ast.SliceExpr)
			if !ok || sl.Low != nil || sl.Max != nil {
				continue
			}
			hi, ok := sl.High.(*ast.BasicLit)
			if !ok || hi.Value != "0" || text(sl.X) != lhs {
				continue
			}
			pos := fset.Position(as.Pos())
			if justified(pos.Line) {
				continue
			}
			t.Errorf("composer.go:%d resets %s with %s[:0], which truncates len and leaves "+
				"the backing array holding every element past it. Use clearToCap(%s), or "+
				"say why the elements are safe to keep in a `retains nothing:` comment",
				pos.Line, lhs, lhs, lhs)
		}
		return true
	})
	if cleared == 0 {
		t.Errorf("found no clearToCap assignment in composer.go at all — the four this " +
			"PR converted should be here, so the scan above proved nothing")
	}
}

// TestEveryReusedSliceThatHoldsAReferenceClearsToCap is the same rule as
// TestEveryReusedSliceInComposerClearsToCap, asked of the whole tree —
// which is where the argument clearToCap's own doc makes actually lives.
//
// That argument is about a Dynamic list shrinking from ten thousand rows
// to ten and pinning ~9,990 components nobody can reach, and nothing in
// it is about composer.go. Live resets outside that file hold exactly
// what it describes: FocusManager's order, watchers and mnemonics,
// refilled by m.walk on the same structural re-sync that drives
// orderPaint; ItemsView's kids, which IS the windowed list;
// AdornmentLayer's filter-in-place, where the tail is the dropped
// tooltip or the finished drag ghost; ButtonBar's cut, whose element
// holds the button it hid. A guard scoped to one file leaves a general
// defect fixed in one place, which is the shape this PR argues against
// elsewhere. Raised in review of #456.
//
// THE EXEMPTION IS DERIVED, NOT LISTED, because a list of "slices that
// are fine" is the enumeration this repo keeps deleting. A reset is
// exempt when its ELEMENT TYPE cannot hold a reference — resolved
// through the tree's own type declarations, recursively — so []int,
// []gooey.Size and []cutMember each answer for themselves and a new
// value-typed slice needs no annotation at all. The classifier FAILS
// CLOSED: a type it cannot resolve is treated as holding a reference, so
// being wrong costs a comment rather than a silent hole.
//
// A string counts as a value here, deliberately. It does point at
// backing bytes, but those are bounded by the string and are not a
// component tree; counting them would flag every []string reset in the
// repo for a few bytes each.
func TestEveryReusedSliceThatHoldsAReferenceClearsToCap(t *testing.T) {
	parsed := parseTree(t)
	types := typeIndex(parsed)
	fields := sliceFieldsByDir(parsed)

	retaining, cleared, safe := 0, 0, 0
	for _, p := range parsed {
		here := declSite{dir: filepath.Dir(p.path), imports: importDirs(p.file)}
		elems := fields[here.dir]
		lines := strings.Split(string(p.src), "\n")
		text := func(e ast.Expr) string {
			return string(p.src[p.fset.Position(e.Pos()).Offset:p.fset.Position(e.End()).Offset])
		}
		// Every base a clear(…) or clearToCap(…) names, scoped to the
		// FUNCTION that names it. Not per statement, because the
		// adornment filter clears its tail after the loop that refilled
		// it and the two are one reset; not per file, which is what
		// this was and is the wider mistake — one clear(x…) anywhere in
		// a file exempted EVERY `x = x[:0]` in it, including one on a
		// path that never reaches the clear, and `components/adorn.go`
		// had just acquired such a clear. Function scope keeps the one
		// case the width was added for and drops the rest. Narrowed in
		// review of #456.
		//
		// The residual cost is worth knowing before you add a reset.
		// The scope is keyed on source TEXT, so `c.kids` in two methods
		// of one type is two keys now but one within either; and a
		// clear inside a closure counts for the whole enclosing
		// function, because the closure's own scope is not where a
		// caller reads the reset. If your new reset is not covered by a
		// clear in the SAME function, this guard will tell you — which
		// is the property the file-wide version did not have.
		clearsIn := func(fn ast.Node) map[string]bool {
			found := map[string]bool{}
			ast.Inspect(fn, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok || len(call.Args) != 1 {
					return true
				}
				id, ok := call.Fun.(*ast.Ident)
				if !ok || (id.Name != "clear" && id.Name != "clearToCap") {
					return true
				}
				arg := call.Args[0]
				if sl, ok := arg.(*ast.SliceExpr); ok {
					arg = sl.X
				}
				found[text(arg)] = true
				return true
			})
			return found
		}

		// The reset walk runs per function too, so `clears` below is
		// the enclosing function's and no other's. A reset outside any
		// function body is not expressible in Go, so nothing is skipped
		// by only visiting FuncDecls.
		for _, decl := range p.file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			clears := clearsIn(fn)
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				as, ok := n.(*ast.AssignStmt)
				// PAIRWISE — see the composer-only guard above, which had
				// the same bail and the same blind spot.
				if !ok || len(as.Lhs) != len(as.Rhs) {
					return true
				}
				for i, rhs := range as.Rhs {
					sl, ok := rhs.(*ast.SliceExpr)
					if !ok || sl.Low != nil || sl.Max != nil || sl.High == nil {
						continue
					}
					hi, ok := sl.High.(*ast.BasicLit)
					if !ok || hi.Value != "0" {
						continue
					}
					base := text(sl.X)
					// The element type is looked up by the LAST segment:
					// c.gonePlacements is the gonePlacements field.
					name := base
					if i := strings.LastIndex(name, "."); i >= 0 {
						name = name[i+1:]
					}
					elem, known := elems[name]
					if known && elem != nil && !holdsAReference(elem, here, types, 0) {
						safe++
						continue
					}
					retaining++
					if clears[base] || clears[text(as.Lhs[i])] {
						cleared++
						continue
					}
					pos := p.fset.Position(sl.Pos())
					if retainsNothingAbove(lines, pos.Line) {
						continue
					}
					t.Errorf("%s:%d resets %s with [:0], and its elements can hold a "+
						"reference (%s). That truncates len and leaves the backing array "+
						"holding everything past it — for a list that shrinks and stays "+
						"small, until nothing. Clear to cap (clearToCap here, "+
						"clear(x[:cap(x)]) in another package), or say why the elements "+
						"are safe to keep in a `retains nothing:` comment. NOTE: the "+
						"reset is in module %s, but this check lives in the ROOT "+
						"module's suite and walks the whole tree — `go test ./...` in "+
						"%s will stay green, so this is the only place it goes red",
						p.path, pos.Line, base, elemDesc(elem, known),
						owningModule(p.path), owningModule(p.path))
				}
				return true
			})
		}
	}

	// NON-VACUITY IN BOTH DIRECTIONS. A classifier answering "holds a
	// reference" for everything reports a clean tree the moment every
	// site is cleared; one answering "safe" for everything reports a
	// clean tree forever.
	if cleared == 0 {
		t.Error("no reset in the tree was found cleared to cap, so this scan proved " +
			"nothing — every site this PR converted should be counted here")
	}
	if safe == 0 {
		t.Error("the classifier called no element type safe, so every exemption is " +
			"coming from a comment rather than from the type — the value-typed " +
			"resets in vstack.go, canvas.go and render/flush.go should be here")
	}
	t.Logf("resets examined: %d can hold a reference (%d cleared), %d cannot",
		retaining, cleared, safe)
}

// owningModule is the nearest ancestor directory of path holding a
// go.mod, as a repo-relative path ("." for the root module).
//
// It exists for the FAILURE MESSAGE, not for the scan. The corpus here
// is the whole tree, which is the right scope — a slice reset retains
// components wherever it is written — but it means a reset added in
// packs/temporal-core reddens the ROOT module's suite, in a package the
// contributor never opened, while that module's own `go test ./...`
// says nothing. Naming the module is what closes the distance between
// where the defect is and where the red appears. Raised in review of
// #456.
func owningModule(path string) string {
	for dir := filepath.Dir(path); ; dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		if dir == "." || dir == string(filepath.Separator) {
			return "."
		}
	}
}

// goFile is one parsed file, kept with the bytes it was parsed from so a
// report can quote the source rather than the AST.
type goFile struct {
	path string
	src  []byte
	fset *token.FileSet
	file *ast.File
}

// parseTree is every non-test Go file this repo owns — nested modules
// included, vendor excluded.
//
// DOT-DIRECTORIES ARE PRUNED AT EVERY DEPTH, for the reason CLAUDE.md's
// verify loop gives: .claude/worktrees/ holds whole checkouts of this
// repo and is untracked, so a top-anchored filter passes in CI and walks
// into somebody else's tree on a developer's machine.
func parseTree(t *testing.T) []goFile {
	t.Helper()
	var out []goFile
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path == "." {
				return nil
			}
			if n := d.Name(); strings.HasPrefix(n, ".") || n == "vendor" || n == "testdata" {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, src, parser.ParseComments)
		if err != nil {
			return nil // not this guard's business; the compiler owns it
		}
		out = append(out, goFile{path: path, src: src, fset: fset, file: f})
		return nil
	})
	if err != nil {
		t.Fatalf("walking the tree: %v", err)
	}
	if len(out) < 200 {
		t.Fatalf("the walk found %d Go files, far fewer than this tree holds — it is "+
			"looking somewhere else and every check below is vacuous", len(out))
	}
	return out
}

// declSite is a type declaration and the context its own body resolves
// in: the directory it was declared in, and the import names visible to
// the file holding it.
type declSite struct {
	expr    ast.Expr
	dir     string
	imports map[string]string
}

// typeIndex maps (directory, type name) to what that type is underneath.
//
// KEYED BY DIRECTORY, not by bare name, and that is not fussiness: this
// repo declares Color twice — render/cell.go and the generated
// grpc/gen/…/types.pb.go — and a name-keyed index has to call the
// collision unresolvable, which fails closed onto every render.Cell in
// the tree. The package a selector names is resolved through the citing
// file's own imports, so render.Cell means render's.
func typeIndex(files []goFile) map[[2]string]declSite {
	out := map[[2]string]declSite{}
	for _, p := range files {
		dir := filepath.Dir(p.path)
		imports := importDirs(p.file)
		for _, d := range p.file.Decls {
			g, ok := d.(*ast.GenDecl)
			if !ok || g.Tok != token.TYPE {
				continue
			}
			for _, sp := range g.Specs {
				if ts, ok := sp.(*ast.TypeSpec); ok {
					out[[2]string{dir, ts.Name.Name}] = declSite{
						expr: ts.Type, dir: dir, imports: imports,
					}
				}
			}
		}
	}
	return out
}

// importDirs maps the name a file refers to each import by — its alias,
// or the last segment of its path — to the directory that import lives
// in, for this repo's own packages. Anything outside the repo maps
// nowhere, and the classifier reads that as unresolvable.
func importDirs(f *ast.File) map[string]string {
	const mod = "github.com/WonderForgeLabs/gooey"
	out := map[string]string{}
	for _, im := range f.Imports {
		path := strings.Trim(im.Path.Value, `"`)
		name := path
		if i := strings.LastIndex(name, "/"); i >= 0 {
			name = name[i+1:]
		}
		if im.Name != nil {
			name = im.Name.Name
		}
		switch {
		case path == mod:
			out[name] = "."
		case strings.HasPrefix(path, mod+"/"):
			out[name] = strings.TrimPrefix(path, mod+"/")
		}
	}
	return out
}

// sliceFieldsByDir maps a directory to the slice ELEMENT type of every
// name declared in it — struct fields and package-level vars alike.
//
// By directory rather than by file because a package is a directory:
// c.gonePlacements is declared in composer.go and reset in
// placements.go, and a per-file index would call it unresolvable. A name
// declared twice with different element types maps to nil, which reads
// as unresolvable.
func sliceFieldsByDir(files []goFile) map[string]map[string]ast.Expr {
	out := map[string]map[string]ast.Expr{}
	shape := map[string]string{}
	for _, p := range files {
		dir := filepath.Dir(p.path)
		if out[dir] == nil {
			out[dir] = map[string]ast.Expr{}
		}
		record := func(name string, typ ast.Expr) {
			arr, ok := typ.(*ast.ArrayType)
			if !ok || arr.Len != nil {
				return
			}
			s := string(p.src[p.fset.Position(arr.Elt.Pos()).Offset:p.fset.Position(arr.Elt.End()).Offset])
			key := dir + " " + name
			if was, seen := shape[key]; seen && was != s {
				out[dir][name] = nil
				return
			}
			shape[key] = s
			out[dir][name] = arr.Elt
		}
		ast.Inspect(p.file, func(n ast.Node) bool {
			switch d := n.(type) {
			case *ast.Field:
				for _, nm := range d.Names {
					record(nm.Name, d.Type)
				}
			case *ast.ValueSpec:
				if d.Type == nil {
					return true
				}
				for _, nm := range d.Names {
					record(nm.Name, d.Type)
				}
			}
			return true
		})
	}
	return out
}

// holdsAReference answers whether a value of type e can keep something
// else alive: a pointer, an interface, a map, a slice, a channel, a
// func, or a struct or array of any of those.
//
// Unresolvable answers true — a type outside this repo, or a name this
// walk cannot place. The depth cap answers true too: a type cyclic
// through a value field cannot exist in Go, so reaching the cap means
// the resolution is wrong and the safe reading is the strict one.
func holdsAReference(e ast.Expr, at declSite, types map[[2]string]declSite, depth int) bool {
	if e == nil || depth > 12 {
		return true
	}
	switch t := e.(type) {
	case *ast.StarExpr, *ast.InterfaceType, *ast.MapType, *ast.ChanType,
		*ast.FuncType, *ast.Ellipsis:
		return true
	case *ast.ArrayType:
		if t.Len == nil {
			return true // a slice header points at a backing array
		}
		return holdsAReference(t.Elt, at, types, depth+1)
	case *ast.StructType:
		for _, f := range t.Fields.List {
			if holdsAReference(f.Type, at, types, depth+1) {
				return true
			}
		}
		return false
	case *ast.ParenExpr:
		return holdsAReference(t.X, at, types, depth+1)
	case *ast.Ident:
		switch t.Name {
		case "bool", "string", "int", "int8", "int16", "int32", "int64",
			"uint", "uint8", "uint16", "uint32", "uint64", "uintptr",
			"float32", "float64", "complex64", "complex128", "byte", "rune":
			return false
		case "any", "error":
			return true
		}
		next, ok := types[[2]string{at.dir, t.Name}]
		if !ok {
			return true
		}
		return holdsAReference(next.expr, next, types, depth+1)
	case *ast.SelectorExpr:
		pkg, ok := t.X.(*ast.Ident)
		if !ok {
			return true
		}
		dir, ok := at.imports[pkg.Name]
		if !ok {
			return true
		}
		next, ok := types[[2]string{dir, t.Sel.Name}]
		if !ok {
			return true
		}
		return holdsAReference(next.expr, next, types, depth+1)
	}
	return true
}

// elemDesc says what the classifier decided and why, so a report names
// the type rather than only the line.
func elemDesc(elem ast.Expr, known bool) string {
	if !known || elem == nil {
		return "its element type could not be resolved, and unresolvable reads as " +
			"retaining here"
	}
	var b strings.Builder
	if err := printer.Fprint(&b, token.NewFileSet(), elem); err != nil {
		return "element type unprintable"
	}
	return "elements are " + b.String()
}

// retainsNothingAbove reports whether the reset at this line carries a
// `retains nothing:` justification on it or in the comment block
// directly above it.
func retainsNothingAbove(lines []string, line int) bool {
	for i := line - 1; i >= 0; i-- {
		if strings.Contains(lines[i], "retains nothing:") {
			return true
		}
		if i != line-1 && !strings.HasPrefix(strings.TrimSpace(lines[i]), "//") {
			return false
		}
	}
	return false
}
