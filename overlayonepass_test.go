package gooey

import (
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
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

// TestOnlyAClearThatReachesCapExemptsAReset is the fixture the tree
// itself cannot supply: every real site in the repo is already written
// the right way, so the guard above is green under BOTH rules and could
// not tell the reader which one it enforces.
//
// The three rejected spellings are the point. clear(x) is the natural
// thing to reach for and clears exactly [0, len) — the partial reset the
// whole guard exists to reject — and the version of this exemption that
// stripped any slice expression off the argument accepted all three.
func TestOnlyAClearThatReachesCapExemptsAReset(t *testing.T) {
	const src = `package p

func full()      { clear(x[:cap(x)]) }
func fullTail()  { clear(x[len(x):cap(x)]) }
func named()     { x = clearToCap(x) }
func namedSlice(){ x = clearToCap(x[:0]) }
func whole()     { clear(x) }
func toLen()     { clear(x[:len(x)]) }
func toZero()    { clear(x[:0]) }
func otherCap()  { clear(x[:cap(y)]) }
func offsetTail(){ clear(x[2:cap(x)]) }
func foreignLen(){ clear(x[len(y):cap(x)]) }
func cappedMax() { x = clearToCap(x[:0:0]) }
func notAClear() { copy(x[:cap(x)], y) }
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "fixture.go", src, 0)
	if err != nil {
		t.Fatalf("the fixture does not parse: %v", err)
	}
	text := func(e ast.Expr) string {
		return src[fset.Position(e.Pos()).Offset:fset.Position(e.End()).Offset]
	}

	// ordered says the spelling's region moves with len, so the clear
	// only releases anything when it runs AFTER the reset. It is the
	// half of clearsToCapIn's answer that nothing in the tree pins: the
	// four accepted spellings are all live, and all four are already
	// written on the correct side of their reset.
	for _, tc := range []struct {
		fn      string
		want    bool
		ordered bool
	}{
		{"full", true, false},
		{"fullTail", true, true},
		{"named", true, false},
		{"namedSlice", true, false},
		{"whole", false, false},
		{"toLen", false, false},
		{"toZero", false, false},
		{"otherCap", false, false},
		{"offsetTail", false, false},
		{"foreignLen", false, false},
		// THE HEAD IS NOT ENOUGH: Low is nil here and cap(x[:0:0]) is
		// zero, so the call clears nothing while exempting the reset it
		// is written beside. Nothing in the tree spells it this way,
		// which is why the arm is here rather than in the corpus.
		{"cappedMax", false, false},
		{"notAClear", false, false},
	} {
		var decl *ast.FuncDecl
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Name.Name == tc.fn {
				decl = fd
			}
		}
		if decl == nil {
			t.Fatalf("the fixture has no func %s", tc.fn)
		}
		got := clearsToCapIn(decl, text)["x"]
		exempts := len(got) != 0
		if exempts != tc.want {
			t.Errorf("%s: clearsToCapIn exempts x = %v, want %v. %s", tc.fn, exempts, tc.want,
				map[bool]string{
					true: "this spelling does reach cap and a reset beside it is safe",
					false: "this spelling leaves elements reachable past the truncation, " +
						"which is the defect the guard is for",
				}[tc.want])
			continue
		}
		if tc.want && got[0].ordered != tc.ordered {
			t.Errorf("%s: clearsToCapIn reports ordered = %v, want %v — %s", tc.fn,
				got[0].ordered, tc.ordered,
				map[bool]string{
					true: "this spelling's low bound is len, so the region it clears " +
						"moves with the reset and only a clear AFTER it releases anything",
					false: "this spelling starts at element zero, so it names the whole " +
						"backing array whenever it runs",
				}[tc.ordered])
		}
	}
}

// The same fixture arm for the OTHER half of the pair. The reset
// matcher is a negative assertion over a tree that now satisfies it, so
// scanning the repo proves nothing about what it can SEE — and the
// spelling it could not see was live in three files. Raised in review
// of #456.
func TestTheResetMatcherSeesTheDeleteSplice(t *testing.T) {
	const src = `package p

func truncate()  { x = x[:0] }
func splice()    { x = append(x[:i], x[i+1:]...) }
func field()     { c.kids = append(c.kids[:i], c.kids[i+1:]...) }
func spliceHead(){ x = append(x[:i], x[j:]...) }
func otherBase() { x = append(x[:i], y[i+1:]...) }
func rebuild()   { x = append(x[:0], y...) }
func compact()   { x = x[:n] }
func grow()      { x = append(x, v) }
func lowBound()  { x = x[1:0] }
func pop()       { x = x[:len(x)-1] }
func popField()  { c.kids = c.kids[:len(c.kids)-1] }
func popTwo()    { x = x[:len(x)-2] }
func popOther()  { x = x[:len(y)-1] }
func popExpr()   { x = x[:len(x)-n] }
func noHigh()    { x = append(x[:], x[k:]...) }
func noLow()     { x = append(x[:i], x[:k]...) }
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "fixture.go", src, 0)
	if err != nil {
		t.Fatalf("the fixture does not parse: %v", err)
	}
	text := func(e ast.Expr) string {
		return src[fset.Position(e.Pos()).Offset:fset.Position(e.End()).Offset]
	}

	for _, tc := range []struct {
		fn   string
		want string
		why  string
	}{
		{"truncate", "x", "the plain truncation, which this guard has always read"},
		{"splice", "x", "the delete-splice: len drops, the old last element stays in " +
			"the vacated slot, and the high-water mark is what the array holds"},
		{"field", "c.kids", "the base is the whole selector, so two different " +
			"structs' kids do not collide"},
		{"spliceHead", "x", "the indices are not the point — any append of a slice " +
			"of x onto a prefix of x shortens x and leaves its tail"},
		{"otherBase", "", "appending a slice of y onto a prefix of x is not a reset " +
			"of either; it is a build"},
		{"rebuild", "", "append(x[:0], y...) REPLACES the contents rather than " +
			"shortening them, and needs its own reasoning rather than this one"},
		{"compact", "", "a compaction to a non-literal length: out of scope, and " +
			"said so in resetBase's doc rather than left in the AST"},
		{"grow", "", "growing is not resetting"},
		{"lowBound", "", "a Low bound means it is not the reset spelling"},
		{"pop", "x", "the POP: len drops by one and the popped element stays in " +
			"the vacated slot, which is the delete-splice's retention with a " +
			"different spelling"},
		{"popField", "c.kids", "and it reads a field base the same way the splice " +
			"arm does"},
		{"popTwo", "x", "any constant taken off len is the same shape; the count is " +
			"not the point"},
		{"popOther", "", "len of a DIFFERENT slice is not this idiom — it is a " +
			"compaction to a length that happens to be spelled with len"},
		{"popExpr", "", "a non-constant subtrahend is the general compaction, which " +
			"is out of scope for the reason isPopOf gives"},
		{"noHigh", "", "append(x[:], x[k:]...) GROWS and is not the removal idiom; " +
			"without requiring a written High it read as a reset of x"},
		{"noLow", "", "append(x[:i], x[:k]...) is not the removal idiom either, and " +
			"the tail argument's Low is what says so"},
	} {
		var decl *ast.FuncDecl
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Name.Name == tc.fn {
				decl = fd
			}
		}
		if decl == nil {
			t.Fatalf("the fixture has no func %s", tc.fn)
		}
		var got string
		ast.Inspect(decl.Body, func(n ast.Node) bool {
			as, ok := n.(*ast.AssignStmt)
			if !ok || len(as.Rhs) != 1 {
				return true
			}
			got, _ = resetBase(as.Rhs[0], text)
			return false
		})
		if got != tc.want {
			t.Errorf("%s: resetBase = %q, want %q — %s", tc.fn, got, tc.want, tc.why)
		}
	}
}

// clearsToCapIn is every slice base that fn clears ALL THE WAY TO CAP,
// keyed by source text. It is the exemption the reset guard below reads:
// a `x = x[:0]` is accepted when the same function clears x's whole
// backing array.
//
// resetBase names the field a statement's right side resets, or "" when
// the statement is not a reset this guard recognises.
//
// TWO SPELLINGS, and the second is why this function exists. `x = x[:0]`
// is the obvious one. `x = append(x[:i], x[i+1:]...)` is the canonical
// Go removal idiom, it retains IDENTICALLY — len drops, the old last
// element stays in the vacated slot, and the high-water mark is what
// the backing array holds — and it was invisible here because its right
// side is a call rather than a slice expression.
//
// NOT HYPOTHETICAL, and not a fixture: ToastHost.Dismiss
// (components/toast.go) and AdornmentLayer.Remove (components/adorn.go)
// both used it, both on a field of components, and this guard reported
// a clean tree over them. adorn.go is the sharper of the two — the same
// file gained a tail clear in Arrange one screen below, so the splice
// was safe only because Arrange happens to run every frame. Raised in
// review of #456.
//
// The append form is matched structurally rather than by text: two
// slice expressions over the same base, the second spread with `...`,
// each with the index the removal idiom writes.
//
// THE INDICES' VALUES ARE STILL NOT CHECKED, and the reason this
// paragraph used to give was arithmetic that does not hold.
// `append(x[:i], x[j:]...)` has length `i + (len(x) - j)`, so it
// shortens x only for `j > i`; `x = append(x[:1], x[0:]...)`
// duplicates the head and GROWS. The guard is indifferent to which,
// because either way the tail past the new length is left where it
// was — which is the property this guard is about, not deletion
// specifically. What it does require is that both indices be WRITTEN:
// `x = append(x[:], x[k:]...)` is an unambiguous grow and is not this
// idiom, and without `head.High != nil` it was classified as a reset of
// x and would have been reported as needing a clear. Nothing in the
// tree writes either shape, so this was a latent false positive beside
// a justification that was simply wrong. Raised in review of #456.
//
// THREE SPELLINGS NOW, and the third is the POP — `x = x[:len(x)-1]`,
// which retains identically and which the two arms above could not see.
// The previous version of this paragraph said the compaction shapes did
// not appear on a reused field in the tree, and that was false at the
// commit that wrote it: four live sites popped a reference off a reused
// field, two of them leaving a discarded markup subtree reachable from
// a live parent. `apps/wysiwyg/undo.go` is the counter-evidence that the
// idiom was already known to retain here — it zeroes the slot before the
// pop — so the claim was refuted inside the tree it was made about.
// Raised in review of #456. See isPopOf for why this widens to the pop
// and not to every `x = x[:n]`.
//
// STILL NARROWER THAN WHAT RETAINS: a general compaction `x = x[:n]` is
// invisible, and so is `x = append(x[:0], …)` — a one-argument append
// with no spread, which is a REBUILD rather than a reset and would need
// its own reasoning. That is scope, and it is stated rather than
// claimed to be empty, which is the mistake the paragraph above
// records.
// THE SPELLING COMES BACK WITH THE BASE, and that is the exemption's
// business rather than bookkeeping. zeroesTopIn proves one slot — the one
// a POP vacates — was released, and the guard accepted it for all three
// spellings: a function that pops-with-zero and ALSO truncates to [:0],
// or delete-splices at i, had the second reset waved through with its
// whole tail still reachable, and was counted as cleared. Not reachable
// in the tree today (the three top-zero sites are single clean pops),
// which is exactly the condition under which this file gives a narrowing
// a synthetic fixture instead of trusting the corpus. Raised in review of
// #456.
const (
	resetTruncate = "truncate" // x = x[:0]
	resetSplice   = "splice"   // x = append(x[:i], x[i+1:]...)
	resetPop      = "pop"      // x = x[:len(x)-1]

	// A POP BY MORE THAN ONE IS THE SAME RETENTION AND A DIFFERENT
	// EXEMPTION. `x = x[:len(x)-2]` leaves just as much behind, so it is
	// still a reset the guard must report; but the top-zero evidence
	// proves ONE released slot, so it cannot cover this. Splitting the
	// kind is what lets resetIsExempt say so without widening what the
	// guard matches. Raised in review of #456.
	resetPopN = "pop-by-n" // x = x[:len(x)-2]
)

func resetBase(rhs ast.Expr, text func(ast.Expr) string) (base, kind string) {
	switch e := rhs.(type) {
	case *ast.SliceExpr:
		if e.Low != nil || e.Max != nil || e.High == nil {
			return "", ""
		}
		if hi, ok := e.High.(*ast.BasicLit); ok && hi.Value == "0" {
			return text(e.X), resetTruncate
		}
		if isPopOf(e.High, text(e.X), text) {
			if popsOneOf(e.High, text(e.X), text) {
				return text(e.X), resetPop
			}
			return text(e.X), resetPopN
		}
		return "", ""

	case *ast.CallExpr:
		id, ok := e.Fun.(*ast.Ident)
		if !ok || id.Name != "append" || len(e.Args) != 2 || e.Ellipsis == token.NoPos {
			return "", ""
		}
		head, ok := e.Args[0].(*ast.SliceExpr)
		if !ok || head.Low != nil || head.Max != nil || head.High == nil {
			return "", ""
		}
		tail, ok := e.Args[1].(*ast.SliceExpr)
		if !ok || tail.Max != nil || tail.Low == nil {
			return "", ""
		}
		base := text(head.X)
		if base == "" || base != text(tail.X) {
			return "", ""
		}
		return base, resetSplice
	}
	return "", ""
}

// THE SPELLING HAS TO REACH CAP, and this used to accept three that do
// not. Stripping any slice expression off the argument meant `clear(x)`,
// `clear(x[:len(x)])` and `clear(x[:0])` all exempted a subsequent
// `x = x[:0]` — and `clear(x)` is the natural thing to reach for while
// clearing exactly [0, len), which is precisely the partial reset this
// guard exists to reject: after the truncation the elements between the
// old len and cap stay reachable. Raised in review of #456.
//
// So `clear` is accepted only as clear(x[…:cap(x)]) — the High must be
// a cap() over the same base — while `clearToCap(…)` is accepted
// whatever slice it is handed, because cap(x[:0]) is cap(x) and the
// function clears to its argument's own cap either way.
//
// text renders an expression back to source, which is how two different
// `c.kids` compare equal and a `c.kids` and a `d.kids` do not.
//
// WHAT COUNTS AS A RESET IS NARROWER THAN WHAT RETAINS, and that scope
// now lives on resetBase above, where the matching happens, rather than
// here — the argument is zeroesTopIn's, one screen down, made about the
// other exemption.
//
// IT RETURNS WHERE AND WHETHER THE ORDER MATTERS, not a bool.
// `clear(x[:cap(x)])` and `clearToCap(x)` name the whole
// backing array whenever they run, so they are order-free.
// `clear(x[len(x):cap(x)])` is not: its Low reads len, and len is
// precisely what the reset changes. Placed BEFORE the reset it clears
// the region above the OLD len — already dead — and the following
// `x = x[:0]` then leaves the entire live range past the new len and
// reachable. CLAUDE.md's clearToCap note bolds "after the refill" for
// that spelling and nothing here read it: measured, moving
// ItemsView.sync's clear (components/itemsview.go:345) above its reset
// leaves this guard green over every row component of a shrinking
// window, which is the leak clearToCap's own doc argues from. Raised in
// review of #456.
//
// The version of this paragraph that stood here named
// the delete-splice's sibling `x = append(x[:0], …)` and not the splice
// itself, and called the omission scope rather than a live miss — which
// was true of the spellings it listed and false of the one it did not:
// three fields were spliced and retaining while this guard reported a
// clean tree. Raised in review of #456, round two, and corrected in the
// round that found them.
func clearsToCapIn(fn ast.Node, text func(ast.Expr) string) map[string][]clearSite {
	found := map[string][]clearSite{}
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
		sl, sliced := arg.(*ast.SliceExpr)
		if id.Name == "clear" {
			if !sliced || sl.High == nil {
				return true // clear(x) leaves [len, cap) reachable
			}
			hi, ok := sl.High.(*ast.CallExpr)
			if !ok {
				return true
			}
			fn, ok := hi.Fun.(*ast.Ident)
			if !ok || fn.Name != "cap" || len(hi.Args) != 1 || text(hi.Args[0]) != text(sl.X) {
				return true
			}
			// AND THE LOW MUST BE THE HEAD OR len OF THE SAME BASE.
			// The paragraph below used to argue the High check made the
			// whole tail the thing being cleared "whatever Low is", and
			// that is false for every Low which is neither:
			// clear(x[2:cap(x)]) passes the cap test and leaves x[0]
			// and x[1] reachable, which is the partial clear this guard
			// exists to reject. Measured — writing
			// components/statusbar.go:75 as clear(s.kids[2:cap(s.kids)])
			// left a dropped section in s.kids[1] with both guards
			// green. `len(y)` in the Low of a clear over x went through
			// for the same reason. Nothing in the tree writes either,
			// which is why the fixture arms are the pin. Raised in
			// review of #456, round seven.
			if sl.Low != nil {
				lo, ok := sl.Low.(*ast.CallExpr)
				if !ok {
					return true
				}
				lf, ok := lo.Fun.(*ast.Ident)
				if !ok || lf.Name != "len" || len(lo.Args) != 1 ||
					text(lo.Args[0]) != text(sl.X) {
					return true
				}
			}
		}
		// LOW MUST BE NIL ON THE clearToCap ARM, and the justification
		// is why. Stripping the slice expression off the argument rests
		// on cap(x[:0]) being cap(x) — true only from the head.
		// `clearToCap(x[2:])` clears from element 2 to cap and leaves
		// x[0] and x[1] reachable, and this arm exempted a later
		// `x = x[:0]` for it. Measured in review of #456; nothing in the
		// tree writes the offset spelling, which is why the fixture arm
		// is the pin.
		//
		// NOT ON THE clear ARM, which is the mistake the first version
		// of this narrowing made: `clear(x[len(x):cap(x)])` is the
		// canonical spelling in this tree and its Low is len(x), so a
		// blanket Low == nil dropped fourteen live exemptions at once.
		// That arm is pinned from both ends INSTEAD — cap() over the
		// same base in the High, and nil or len() over the same base in
		// the Low. The sentence that stood here said the High check
		// alone made the whole tail the thing being cleared "whatever
		// Low is", and an offset Low is exactly the counterexample.
		//
		// AND MAX, FOR THE SAME REASON ONE STEP ON. The head is
		// necessary and not sufficient: a three-index slice caps the
		// result independently, so `clearToCap(x[:0:0])` has Low == nil
		// and cap 0 — it clears nothing while exempting the reset below
		// it. resetBase bails on Max at all three of its own slice
		// reads and this arm did not, which is two halves of one
		// matcher disagreeing. Nothing in the tree writes the spelling,
		// so the pin is the cappedMax fixture arm rather than the
		// corpus. The `clear` arm is unaffected: clear(s) covers
		// len(s), which is High-Low, and Max does not change it.
		// Raised in review of #456.
		ordered := false
		if sliced {
			if id.Name == "clearToCap" && (sl.Low != nil || sl.Max != nil) {
				return true
			}
			// A LOW THAT IS NOT THE HEAD makes the spelling relative to
			// len. `x[:cap(x)]` and `x[:0]` start at element zero and
			// mean the same region whenever they run; `x[len(x):…]`
			// moves with the reset.
			ordered = sl.Low != nil
			arg = sl.X
		}
		base := text(arg)
		found[base] = append(found[base], clearSite{at: call.Pos(), ordered: ordered})
		return true
	})
	return found
}

// localScope is where one declaration of a name is live: from the
// declaration itself to the end of the construct that opened its scope.
type localScope struct{ from, to token.Pos }

// inScope reports whether a reset at `at` is covered by one of a name's
// declarations.
func inScope(ss []localScope, at token.Pos) bool {
	for _, sc := range ss {
		if at > sc.from && at < sc.to {
			return true
		}
	}
	return false
}

// localSlices is every name fn DECLARES in its own body — `var x []T`
// and `x := …` alike — AND WHERE EACH DECLARATION IS LIVE.
//
// THE SCOPE IS THE HALF THAT MAKES IT SAFE, and this was a bare set of
// names until #456's review. The consumer matches on the reset's own
// base text, so a package-level slice shadowed anywhere in the function
// — a closure, an `if` init, a `for` body — made every reset of the
// OUTER one look local and be skipped:
//
//	var evalStack []*node      // package level
//	func f() {
//	    if cond { evalStack := scratch(); _ = evalStack }
//	    evalStack = evalStack[:len(evalStack)-1]   // skipped
//	}
//
// prop.evalStack is this function's own named example of the shape the
// skip must not reach, and nothing in the tree is shadowed that way
// today — which is the condition under which this file supplies a
// synthetic arm rather than trusting the corpus.
//
// THE SCOPE OPENERS ARE LISTED rather than taken as "the enclosing
// block", because an `if x := …; cond` declares into the IfStmt and not
// into the block around it. Taking the block would over-scope, and
// over-scoping is the fail-open direction.
//
// It exists because this guard's subject is a REUSED FIELD, and a bare
// identifier reset inside a function is not one: the backing array a
// local names dies with the call frame, so truncating it retains
// nothing past the call. CLAUDE.md says the same about the general
// `x = x[:n]` compaction the guard deliberately does not cover.
//
// It was not needed while sliceFieldsByDir indexed locals, because the
// local's own declaration resolved its element type and a value-typed
// one took the safe arm — accidentally, and by the same mechanism that
// let a local resolve a FIELD of the same name. Taking locals out of
// that index (round seven) left cmd/browser/markdown.go:74's
// `para = para[:0]` — a []string local in renderMarkdown — reported as
// a retaining reset. This is the deliberate half of that change rather
// than a hole it opened: a local is skipped because it is a local, not
// because its elements happened to be values.
//
// A SELECTOR IS NEVER SKIPPED here, whatever names collide: the check
// is on the reset's own base text, so `c.kids` is a field even in a
// function that also declares a local `kids`.
//
// THE SKIP RUNS BEFORE THE ESCAPE IS READ, which decides where a
// `retains nothing:` marker means anything: on a local it is
// decoration, because the reset never reaches retainsNothingAbove.
// Three in this tree were written that way and measured inert — the
// guard stayed green with their text replaced — so they say the same
// thing in prose without the token. Which sites the guard actually
// CONSUMES is derivable rather than listed: grep the marker, drop the
// ones whose reset base is a bare identifier declared in the function.
// Raised in review of #456.
func localSlices(fn *ast.FuncDecl) map[string][]localScope {
	out := map[string][]localScope{}
	// One entry per node visited, so the nil the walk hands back on the
	// way out pops exactly what its node pushed; only scope openers carry
	// a node.
	var stack []ast.Node
	innermost := func() ast.Node {
		for i := len(stack) - 1; i >= 0; i-- {
			if stack[i] != nil {
				return stack[i]
			}
		}
		return fn.Body
	}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		switch n.(type) {
		case *ast.BlockStmt, *ast.CaseClause, *ast.CommClause,
			*ast.IfStmt, *ast.ForStmt, *ast.RangeStmt,
			*ast.SwitchStmt, *ast.TypeSwitchStmt, *ast.SelectStmt,
			*ast.FuncLit:
			stack = append(stack, n)
		default:
			stack = append(stack, nil)
		}
		add := func(name string, from token.Pos) {
			out[name] = append(out[name], localScope{from: from, to: innermost().End()})
		}
		switch d := n.(type) {
		case *ast.ValueSpec:
			for _, nm := range d.Names {
				add(nm.Name, d.End())
			}
		case *ast.AssignStmt:
			if d.Tok != token.DEFINE {
				return true
			}
			for _, lhs := range d.Lhs {
				if id, ok := lhs.(*ast.Ident); ok {
					add(id.Name, d.End())
				}
			}
		}
		return true
	})
	return out
}

// TestALocalIsSeenHoweverItIsDeclared is localSlices' own fixture.
//
// The guard's live pin for the skip is cmd/browser/markdown.go's
// `para = para[:0]`, and para is a `var`. Nothing in the tree resets a
// `:=` local, so that arm is unexercised by the corpus and a mutation
// of it is silent — which is the shape a fixture test is for.
//
// THE SHADOW ARM IS THE ONE THAT FAILS OPEN, and it is the reason this
// fixture asks WHERE rather than WHETHER. A package-level slice
// shadowed in a nested scope had every reset of the outer one skipped
// as a local; nothing in the tree is shaped that way, so only a
// synthetic arm can hold it. Raised in review of #456.
func TestALocalIsSeenHoweverItIsDeclared(t *testing.T) {
	const src = `package p

var pkgLevel []int

func f(param []int) {
	var declared []int
	short := []int{}
	pair, _ := g()
	if cond {
		shadow := []int{}
		_ = shadow
	}
	_, _, _, _ = declared, short, pair, param
}
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "fixture.go", src, 0)
	if err != nil {
		t.Fatalf("the fixture does not parse: %v", err)
	}
	var fn *ast.FuncDecl
	for _, d := range file.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Name.Name == "f" {
			fn = fd
		}
	}
	if fn == nil {
		t.Fatal("the fixture has no func f")
	}
	got := localSlices(fn)
	// AT THE END OF THE BODY, which is where the guard asks: a reset
	// sits after the declarations it might be covered by, and the
	// shadow arm below is only a shadow from there.
	at := fn.Body.End() - 1
	for _, name := range []string{"declared", "short", "pair"} {
		if !inScope(got[name], at) {
			t.Errorf("localSlices does not see %q as live at the end of the "+
				"body. A reset on it would be classified as a reused field, "+
				"and the element type that answers for it is whatever else "+
				"in the directory carries that name", name)
		}
	}
	if inScope(got["pkgLevel"], at) {
		t.Error("localSlices sees pkgLevel, which fn does not declare. A " +
			"package-level slice reset in this function would be skipped as " +
			"a local — and prop.evalStack is exactly that shape")
	}
	if inScope(got["param"], at) {
		t.Error("localSlices sees param, which is a PARAMETER rather than a " +
			"declaration in the body. It is excluded for its own reason and " +
			"not pkgLevel's: truncating a parameter header retains nothing " +
			"new, because the caller still holds its own. Including it would " +
			"be harmless; what is not harmless is the other direction, where " +
			"param = param[:0] is resolved against whatever struct field in " +
			"the directory happens to share the name")
	}
	if inScope(got["shadow"], at) {
		t.Error("localSlices reports shadow live at the end of the body, " +
			"where the only declaration of it is inside an if. A " +
			"package-level slice shadowed in ANY nested scope then has its " +
			"own resets skipped as local, which is prop.evalStack's shape " +
			"and the fail-open direction")
	}
	// The `_ = shadow` line: inside the if, after the declaration, which
	// is the only window the declaration covers.
	inside := fn.Body.List[3].(*ast.IfStmt).Body.List[1].Pos()
	if !inScope(got["shadow"], inside) {
		t.Error("localSlices does not report shadow live inside the if that " +
			"declares it, so the scoping is refusing the declaration rather " +
			"than bounding it — the arm above would pass for the wrong reason")
	}
}

// clearSite is one tail-clear: where it is, and whether that matters.
// An ordered site releases the live range only when it runs AFTER the
// reset it is supposed to cover; an unordered one covers the reset from
// either side.
type clearSite struct {
	at      token.Pos
	ordered bool
}

// THIS COMMENT WAS ON A DIFFERENT DECLARATION. The blank line
// between it and the fixture below it was lost, so the whole group
// merged onto TestOnlyAClearThatReachesCapExemptsAReset and this
// test was left bare — the exact shape #483's guard is for, found
// by it on this branch's merge of main. Moved here rather than
// re-split, because this is where it documents something.
//
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
		clearsIn := func(fn ast.Node) map[string][]clearSite { return clearsToCapIn(fn, text) }

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
			zeroesTop := zeroesTopIn(fn, text)
			locals := localSlices(fn)
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				as, ok := n.(*ast.AssignStmt)
				// PAIRWISE — see the composer-only guard above, which had
				// the same bail and the same blind spot.
				if !ok || len(as.Lhs) != len(as.Rhs) {
					return true
				}
				for i, rhs := range as.Rhs {
					base, kind := resetBase(rhs, text)
					if base == "" {
						continue
					}
					// A LOCAL IS NOT A REUSED FIELD — see localSlices,
					// which answers WHERE each declaration is live, so
					// a shadow cannot exempt the package-level slice it
					// shadows.
					if !strings.Contains(base, ".") && inScope(locals[base], as.Pos()) {
						continue
					}
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
					if resetIsExempt(base, text(as.Lhs[i]), kind, as.Pos(), clears, zeroesTop) {
						cleared++
						continue
					}
					pos := p.fset.Position(rhs.Pos())
					if retainsNothingAbove(lines, pos.Line) {
						continue
					}
					t.Errorf("%s:%d resets %s, and its elements can hold a "+
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

// TestAFunctionLocalIsNotAField is the other half of sliceFieldsByDir's
// scope, and the tree cannot pin it either — a local that collides with
// a field name is a thing a contributor writes next, not a thing the
// tree holds today.
//
// Both directions are here. The NOISY one is what the measurement in
// review of #458 showed: a local `var sizes []int` anywhere in
// components/ conflicted with ButtonBar's `sizes []gooey.Size` and
// reddened four correct resets three files away. The one that matters
// is the FAIL-OPEN mirror, and it is the arm below: where the local is
// a name's only declaration in the directory, it supplied the
// resolution for a reset on a field of that name promoted from a type
// declared in another package — a value-typed local waving a
// reference-typed field through. Raised in review of #456.
func TestAFunctionLocalIsNotAField(t *testing.T) {
	const src = `package p

type holder struct{ kids []*int }

func f() {
	var kids []int
	spare := []int{}
	_, _ = kids, spare
}
`
	fset := token.NewFileSet()
	path := filepath.Join("dir", "f.go")
	f, err := parser.ParseFile(fset, path, src, 0)
	if err != nil {
		t.Fatalf("the fixture does not parse: %v", err)
	}
	got := sliceFieldsByDir([]goFile{{path: path, src: []byte(src), fset: fset, file: f}})["dir"]

	if _, ok := got["spare"]; ok {
		t.Errorf("a local declared with := was indexed as a field (%v). Every "+
			"name in a function body would be, and a value-typed one supplies "+
			"a resolution for any FIELD of the same name in the directory",
			got["spare"])
	}
	// `kids` is declared twice here: once as a field of []*int, once as a
	// local of []int. Indexing the local makes them conflict, which is
	// the noisy direction; the field's own resolution is what must
	// survive.
	if got["kids"] == nil {
		t.Errorf("holder.kids resolved to nothing. The local `var kids []int` " +
			"in the same directory conflicted with it, so an ordinary local " +
			"turns every reset on a field of that name red")
	} else if el := src[fset.Position(got["kids"].Pos()).Offset:fset.Position(got["kids"].End()).Offset]; el != "*int" {
		t.Errorf("holder.kids resolved to %q, want *int — the local's element "+
			"type answered for the field, so a value-typed local waves a "+
			"reference-typed field through", el)
	}
}

// A THREE-DECLARATION SEQUENCE, which is the ordering the conflict
// marker used to lose.
//
// The repo scan cannot pin this: every `kids []` in components/ is
// []gooey.Component today, so the classifier never reaches its own
// conflict branch and a test over the tree would pass with the bug
// present. The fixture supplies the sequence directly — value, pointer,
// value again, in one directory — and requires the name to stay
// unresolved after the third. With the marker forgotten it resolves
// back to `int`, holdsAReference answers false, and every reset on
// `kids` in that directory is waved through. Raised in review of #456.
func TestAnAmbiguousFieldStaysAmbiguous(t *testing.T) {
	srcs := []string{
		"package p\n\ntype a struct{ kids []int }\n",
		"package p\n\ntype b struct{ kids []*int }\n",
		"package p\n\ntype c struct{ kids []int }\n",
	}
	var files []goFile
	for i, src := range srcs {
		fset := token.NewFileSet()
		path := filepath.Join("dir", "f"+strconv.Itoa(i)+".go")
		f, err := parser.ParseFile(fset, path, src, 0)
		if err != nil {
			t.Fatalf("the fixture does not parse: %v", err)
		}
		files = append(files, goFile{path: path, src: []byte(src), fset: fset, file: f})
	}

	// THE PREMISE FIRST: two declarations must already disagree, or the
	// third proves nothing.
	if got := sliceFieldsByDir(files[:2])["dir"]["kids"]; got != nil {
		t.Fatalf("two conflicting declarations resolved kids to %v, want "+
			"unresolved — the fixture is not exercising the conflict branch", got)
	}
	if got := sliceFieldsByDir(files)["dir"]["kids"]; got != nil {
		t.Errorf("after a third declaration matching the FIRST shape, kids "+
			"resolved to %v again. The conflict was recorded in out[dir] and "+
			"not in the shape map, so the scan forgot a decision it had "+
			"already made — and a value-typed resurrection makes "+
			"holdsAReference answer false for every reset on this name in "+
			"the directory", got)
	}
}

// sliceFieldsByDir maps a directory to the slice ELEMENT type of every
// name declared in it — struct fields and FILE-LEVEL vars alike.
//
// File-level, and the emphasis is a repair. The var arm ran under the
// same ast.Inspect as the struct one, so every `var x []T` inside a
// function body was recorded as if it were a field — the collision
// round four closed for function PARAMETERS, reopened one node type
// over. Measured: `func reviewProbe() { var sizes []int; _ = sizes }`
// added to components/canvas.go reddens four correct, value-typed
// resets in buttonbar.go, canvas.go, hstack.go and vstack.go. The noisy
// direction is the visible one; the direction that matters is the
// mirror, where a name's only declaration in a directory is a
// value-typed local and it supplies the resolution for a reset on a
// field promoted from a type declared elsewhere — there the local waves
// the reset through. Raised in review of #456, round seven.
//
// By directory rather than by file because a package is a directory:
// c.gonePlacements is declared in composer.go and reset in
// placements.go, and a per-file index would call it unresolvable. A name
// declared twice with different element types maps to nil, which reads
// as unresolvable.
func sliceFieldsByDir(files []goFile) map[string]map[string]ast.Expr {
	out := map[string]map[string]ast.Expr{}
	shape := map[string]string{}
	// A CONFLICT IS PERMANENT, and it was not. The conflict branch below
	// used to return without touching `shape`, so the key kept the FIRST
	// declaration's shape — and a third declaration of the same name
	// matching that first shape took the normal path and wrote a
	// concrete element type back over the nil, resurrecting a resolution
	// for a name this scan had already decided was ambiguous. If the
	// surviving resolution is a value type, holdsAReference answers
	// false and every reset on that name in the directory is waved
	// through, including the one whose elements are gooey.Component.
	//
	// That is the one place this classifier failed OPEN — everywhere
	// else "unresolvable" means the strict reading — and it did it by
	// forgetting a decision it had made, with the outcome depending on
	// file-walk order. Not reachable today; the shape is the defect.
	// Raised in review of #456.
	conflicted := map[string]bool{}
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
			if conflicted[key] {
				out[dir][name] = nil
				return
			}
			if was, seen := shape[key]; seen && was != s {
				conflicted[key] = true
				out[dir][name] = nil
				return
			}
			shape[key] = s
			out[dir][name] = arr.Elt
		}
		// FILE-LEVEL VARS ONLY, walked off Decls rather than found by
		// the Inspect below — see this function's doc.
		for _, decl := range p.file.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.VAR {
				continue
			}
			for _, sp := range gd.Specs {
				vs, ok := sp.(*ast.ValueSpec)
				if !ok || vs.Type == nil {
					continue
				}
				for _, nm := range vs.Names {
					record(nm.Name, vs.Type)
				}
			}
		}
		ast.Inspect(p.file, func(n ast.Node) bool {
			switch d := n.(type) {
			case *ast.StructType:
				// STRUCT FIELDS, not every *ast.Field. An ast.Field is
				// also a function PARAMETER, a result and an interface
				// method, and collecting those put `func offsets(sizes
				// []int, …)` in components/grid.go into the same bucket
				// as ButtonBar's `sizes []gooey.Size`. That is not an
				// ambiguity about a field; it is two unrelated names.
				//
				// It was invisible while the conflict marker could be
				// undone: the grid.go parameter conflicted, the next
				// file's `[]gooey.Size` matched the retained shape and
				// resurrected it, and four resets were classified safe
				// through a resolution the scan had already rejected.
				// Making the conflict permanent is what surfaced it, and
				// the two fixes belong together — the marker without
				// this one reports four sites that are genuinely fine.
				// Raised in review of #456.
				if d.Fields == nil {
					return true
				}
				for _, f := range d.Fields.List {
					for _, nm := range f.Names {
						record(nm.Name, f.Type)
					}
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

// isPopOf reports whether high is `len(base) - <constant>` — the POP,
// which is the same retention as the truncation to zero and as the
// delete-splice, and which was invisible to both arms.
//
// NOT EVERY `x = x[:n]`, and the difference is what makes this guard
// usable. Widening to any High at all collects twenty-nine sites, and
// the great majority are LOCAL slices — a `line`, a `buf`, a scratch
// `s` — which the field lookup cannot resolve and which therefore read
// as retaining. The guard is about a REUSED FIELD, and a general
// truncation of a local says nothing about one. The pop is different:
// it is spelled over the slice's own len, so it is the shape a
// long-lived stack or child list uses, and every site it finds in this
// tree is one.
func isPopOf(high ast.Expr, base string, text func(ast.Expr) string) bool {
	bin, ok := high.(*ast.BinaryExpr)
	if !ok || bin.Op != token.SUB {
		return false
	}
	if _, ok := bin.Y.(*ast.BasicLit); !ok {
		return false
	}
	call, ok := bin.X.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return false
	}
	id, ok := call.Fun.(*ast.Ident)
	return ok && id.Name == "len" && text(call.Args[0]) == base
}

// popsOneOf is isPopOf narrowed to a subtrahend of exactly ONE, which
// is the only thing the top-zero exemption's argument is true of.
//
// isPopOf accepts `len(x) - <any constant>` because for the RESET that
// is the right question: `x = x[:len(x)-2]` retains exactly as much as
// `x = x[:len(x)-1]` does, and both are the shape a long-lived list
// uses. The exemption asks something else — "did the one slot that left
// [0, len) get released" — and that has a different answer for every
// constant but 1. Reusing one predicate for both meant three retaining
// shapes were certified as exempt; measured in review of #456:
//
//	x[len(x)-1] = nil ; x = x[:len(x)-2]   two leave, one is zeroed
//	x[len(x)-2] = nil ; x = x[:len(x)-1]   the vacated slot is untouched
//
// Both now report NO. Raised in review of #456.
func popsOneOf(high ast.Expr, base string, text func(ast.Expr) string) bool {
	bin, ok := high.(*ast.BinaryExpr)
	if !ok {
		return false
	}
	lit, ok := bin.Y.(*ast.BasicLit)
	return ok && lit.Value == "1" && isPopOf(high, base, text)
}

// zeroesTopIn is every slice base that fn zeroes the LAST SLOT of —
// `x[len(x)-1] = nil` or `= T{}` — which is the clear a POP needs and
// the whole of it.
//
// A pop shortens by one, so exactly one slot passes out of [0, len) and
// clearing to cap is clearing that slot plus a tail some other reset
// already owns. Both spellings are correct; only one of them is O(1),
// and that matters where the pop is: `prop.evalStack` is popped on every
// computed evaluation in the process, so `clear(evalStack[len:cap])`
// there would be O(depth) per pop and O(depth²) per evaluation on the
// hottest path the framework has.
//
// IT IS ALSO THE SPELLING ALREADY IN THE TREE. `apps/wysiwyg/undo.go`
// zeroes the slot before popping — it was the counter-evidence the
// review used to show the pop retains — so a guard that refused to see
// it would have reported the one file that already got this right.
// Raised in review of #456.
//
// The assigned value must be a ZERO: nil, or a composite literal with
// no elements. `x[len(x)-1] = y` is a write, not a release.
// IT RETURNS WHERE, NOT WHETHER, because the argument names an ORDER.
// "`x[len(x)-1] = nil` BEFORE the pop releases exactly what left" is
// false read the other way round: after the pop, `len(x)-1` is a LIVE
// element and the released slot is never touched. That is not a missed
// case, it is a real bug shape, and a boolean could not tell the guard
// which one it had.
//
// EVERY ZEROING, NOT THE EARLIEST, because the evidence is per SLOT. One
// zeroing proves one slot released, and a function that pops the same
// base twice with a single zero vacates a second slot nothing touched —
// which the earliest-only form certified with the first pop's evidence.
// resetIsExempt SPENDS a position per pop for that reason. Not reachable
// in the tree today: all three top-zero sites are single clean pops, so
// the mutation is silent against the corpus and
// TestTheExemptionIsScopedToTheSpellingItProves is where it goes red.
// Raised in review of #456.
func zeroesTopIn(fn ast.Node, text func(ast.Expr) string) map[string][]token.Pos {
	found := map[string][]token.Pos{}
	ast.Inspect(fn, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok || len(as.Lhs) != len(as.Rhs) {
			return true
		}
		for i, lhs := range as.Lhs {
			ix, ok := lhs.(*ast.IndexExpr)
			if !ok {
				continue
			}
			if !popsOneOf(ix.Index, text(ix.X), text) {
				continue
			}
			switch v := as.Rhs[i].(type) {
			case *ast.Ident:
				if v.Name != "nil" {
					continue
				}
			case *ast.CompositeLit:
				if len(v.Elts) != 0 {
					continue
				}
			default:
				continue
			}
			base := text(ix.X)
			found[base] = append(found[base], as.Pos())
		}
		return true
	})
	for base := range found {
		slices.Sort(found[base])
	}
	return found
}

// TestTheZeroedTopMatcherSeesOnlyAReleasedSlot is zeroesTopIn's own
// fixture, and it exists because the guard cannot supply one.
//
// Loosening either narrowing — the index shape, or the zero-value
// requirement — changes NOTHING in the tree: nothing else writes
// `x[…] = nil`, so both are unexercised by the live corpus and a
// mutation of either is silent. That is the shape a fixture test is
// for. Raised in review of #456.
func TestTheZeroedTopMatcherSeesOnlyAReleasedSlot(t *testing.T) {
	const src = `package p

func nilTop()    { x[len(x)-1] = nil }
func litTop()    { x[len(x)-1] = snapshot{} }
func fieldTop()  { h.undo[len(h.undo)-1] = snapshot{} }
func liveValue() { x[len(x)-1] = y }
func filledLit() { x[len(x)-1] = snapshot{label: "a"} }
func notTheTop() { x[i] = nil }
func otherLen()  { x[len(y)-1] = nil }
func notAPop()   { x[len(x)-n] = nil }
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "fixture.go", src, 0)
	if err != nil {
		t.Fatalf("the fixture does not parse: %v", err)
	}
	text := func(e ast.Expr) string {
		return src[fset.Position(e.Pos()).Offset:fset.Position(e.End()).Offset]
	}
	for _, tc := range []struct {
		fn   string
		want string
		why  string
	}{
		{"nilTop", "x", "the release itself: the slot that leaves [0, len) is set to nil"},
		{"litTop", "x", "and an empty composite literal is the same release for a struct element"},
		{"fieldTop", "h.undo", "the base is the whole selector, as everywhere else in this guard"},
		{"liveValue", "", "assigning a VALUE to the top slot is a write, not a release — " +
			"the element it overwrites is gone but a live one takes its place"},
		{"filledLit", "", "and a composite literal with elements is a value like any other"},
		{"notTheTop", "", "an arbitrary index is not the slot a pop releases"},
		{"otherLen", "", "len of a different slice does not name this slice's top"},
		{"notAPop", "", "a non-constant subtrahend is not the pop shape isPopOf reads, " +
			"so the reset it would exempt is not one this guard matches either"},
	} {
		var decl *ast.FuncDecl
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Name.Name == tc.fn {
				decl = fd
			}
		}
		if decl == nil {
			t.Fatalf("the fixture has no func %s", tc.fn)
		}
		got := zeroesTopIn(decl, text)
		if tc.want == "" {
			if len(got) != 0 {
				t.Errorf("%s: zeroesTopIn = %v, want nothing — %s", tc.fn, got, tc.why)
			}
			continue
		}
		if len(got[tc.want]) != 1 || len(got) != 1 {
			t.Errorf("%s: zeroesTopIn = %v, want exactly %q — %s", tc.fn, got, tc.want, tc.why)
		}
	}
}

// resetIsExempt says whether a reset the guard matched is covered by a
// clear in the same function. base is the slice the reset shortens, lhs
// the text of the statement's left side (the two differ for
// `dst = src[:0]`), and kind the spelling resetBase read.
//
// THE TOP-ZERO EXEMPTION IS THE POP'S ALONE. `clear(x[len(x):cap(x)])`
// releases the whole tail, so it covers all three spellings and clears
// stays general. zeroesTopIn proves ONE slot — the one a pop vacates —
// and nothing else: accepting it for a truncate or a delete-splice in
// the same function waves that reset's entire tail through with one
// released element as the evidence. Raised in review of #456.
//
// It is a function rather than three lines in the guard because the
// tree cannot exercise it. All three top-zero sites are single clean
// pops, so widening this back to every kind changes nothing that a walk
// of the corpus can see — the mutation is silent, and
// TestTheExemptionIsScopedToTheSpellingItProves is where it goes red.
//
// IT SPENDS THE TOP-ZERO EVIDENCE IT USES, which is the one surprising
// thing about a predicate: zeroesTop is the enclosing function's map and
// this removes the position it consumes. One zeroing is evidence about
// ONE slot, so a second pop of the same base has to find its own. Both
// callers walk a function's resets with ast.Inspect over its body, which
// visits statements in source order, so the entry spent is the earliest
// unspent zeroing that precedes this pop.
//
// SOURCE ORDER, NOT EXECUTION ORDER, and the difference is scope rather
// than a miss. Both comparisons are on token.Pos, so "after the reset"
// means further down the file — a clear below an early return, or
// inside a conditional past the reset, exempts it on paths where it
// never runs:
//
//	x = x[:0]
//	if cond {
//		return // nothing released on this path
//	}
//	clear(x[len(x):cap(x)]) // exempts the reset anyway
//
// No live site is shaped that way: every reset in the tree and the
// clear or zeroing that covers it are straight-line in one block.
// Making the check flow-sensitive is a reaching-definitions pass, a
// different instrument from this file. Stated rather than left to be
// inferred, which is the standard CLAUDE.md's `x = x[:n]` scope
// paragraph sets. Raised in review of #456, round seven.
func resetIsExempt(base, lhs, kind string, at token.Pos, clears map[string][]clearSite, zeroesTop map[string][]token.Pos) bool {
	// AN ORDERED CLEAR HAS TO FOLLOW THE RESET — see clearsToCapIn: the
	// `clear(x[len(x):cap(x)])` spelling reads len, and len is what the
	// reset changes, so before it the call clears an already-dead region
	// and releases nothing.
	for _, name := range [2]string{base, lhs} {
		for _, c := range clears[name] {
			if !c.ordered || c.at > at {
				return true
			}
		}
	}
	if kind != resetPop {
		return false
	}
	// BEFORE, NOT MERELY PRESENT. CLAUDE.md and zeroesTopIn's doc both
	// say the zero comes before the pop, and until round five nothing
	// read the order: `x = x[:len(x)-1]` followed by
	// `x[len(x)-1] = nil` nils a LIVE element, leaves the released one,
	// and was certified as the fix for itself. Raised in review of #456.
	for _, name := range [2]string{base, lhs} {
		for i, zeroed := range zeroesTop[name] {
			if zeroed < at {
				zeroesTop[name] = slices.Delete(slices.Clone(zeroesTop[name]), i, i+1)
				return true
			}
		}
	}
	return false
}

// TestTheExemptionIsScopedToTheSpellingItProves is resetIsExempt's
// fixture, and the first two arms are the shape the tree cannot supply:
// a function that pops-with-zero AND also resets some other way. Both
// of those second resets must still be reported.
func TestTheExemptionIsScopedToTheSpellingItProves(t *testing.T) {
	const src = `package p

func popAndTruncate() {
	x[len(x)-1] = nil
	x = x[:len(x)-1]
	x = x[:0]
}

func popAndSplice() {
	c.kids[len(c.kids)-1] = nil
	c.kids = c.kids[:len(c.kids)-1]
	c.kids = append(c.kids[:i], c.kids[i+1:]...)
}

func popOnly() {
	x[len(x)-1] = nil
	x = x[:len(x)-1]
}

func clearedAll() {
	x = x[:0]
	x = x[:len(x)-1]
	clear(x[len(x):cap(x)])
}

func clearBeforeReset() {
	clear(x[len(x):cap(x)])
	x = x[:0]
}

func clearWholeArrayFirst() {
	clear(x[:cap(x)])
	x = x[:0]
}

func popTwiceZeroOnce() {
	h.kids[len(h.kids)-1] = nil
	h.kids = h.kids[:len(h.kids)-1]
	h.kids = h.kids[:len(h.kids)-1]
}

func bareTruncate() { x = x[:0] }

func zeroesSomeoneElsesTop() {
	y[len(y)-1] = nil
	x = x[:len(x)-1]
}

func popTwoZeroOne() {
	h.kids[len(h.kids)-1] = nil
	h.kids = h.kids[:len(h.kids)-2]
}

func zeroWrongSlot() {
	h.kids[len(h.kids)-2] = nil
	h.kids = h.kids[:len(h.kids)-1]
}

func zeroAfterPop() {
	h.kids = h.kids[:len(h.kids)-1]
	h.kids[len(h.kids)-1] = nil
}

func clearFromAnOffset() {
	clearToCap(x[2:])
	x = x[:0]
}
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "fixture.go", src, 0)
	if err != nil {
		t.Fatalf("the fixture does not parse: %v", err)
	}
	text := func(e ast.Expr) string {
		return src[fset.Position(e.Pos()).Offset:fset.Position(e.End()).Offset]
	}

	// reported is every reset in fn that resetIsExempt does NOT cover,
	// named by the spelling resetBase read — which is what the guard
	// goes red over.
	reported := func(fn *ast.FuncDecl) []string {
		clears := clearsToCapIn(fn, text)
		zeroesTop := zeroesTopIn(fn, text)
		var left []string
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			as, ok := n.(*ast.AssignStmt)
			if !ok || len(as.Lhs) != len(as.Rhs) {
				return true
			}
			for i, rhs := range as.Rhs {
				base, kind := resetBase(rhs, text)
				if base == "" {
					continue
				}
				if resetIsExempt(base, text(as.Lhs[i]), kind, as.Pos(), clears, zeroesTop) {
					continue
				}
				left = append(left, kind)
			}
			return true
		})
		return left
	}

	for _, tc := range []struct {
		fn   string
		want []string
		why  string
	}{
		{"popAndTruncate", []string{resetTruncate}, "the pop's released slot is the pop's " +
			"alone; the truncation in the same function drops len to 0 and leaves " +
			"everything below the popped slot reachable"},
		{"popAndSplice", []string{resetSplice}, "and the delete-splice is the same " +
			"reasoning with the removal idiom's spelling — the tail past the new len " +
			"is still held"},
		{"popOnly", nil, "a clean pop is what zeroesTopIn was widened for: one slot " +
			"leaves the live range and that one slot is released"},
		{"clearedAll", nil, "clear(x[len(x):cap(x)]) AFTER the resets really does cover " +
			"every spelling, so that exemption stays general"},
		{"bareTruncate", []string{resetTruncate}, "with no clear at all there is nothing " +
			"to exempt it"},
		{"zeroesSomeoneElsesTop", []string{resetPop}, "the exemption is keyed on the " +
			"base, so releasing y's top slot says nothing about x — even a pop is " +
			"reported"},

		// THE THREE SHAPES ROUND FIVE MEASURED AS FALSELY EXEMPT. None
		// of them exists in the tree — every live top-zero site is a
		// single clean pop with the zero first — so a mutation of any
		// of the three narrowings is silent against the corpus and
		// these arms are the only place it goes red.
		{"popTwoZeroOne", []string{resetPopN}, "two slots leave [0, len) and one is " +
			"zeroed, so h.kids[len-2] stays reachable — the top-zero evidence " +
			"proves ONE released slot and cannot cover a pop by two"},
		{"zeroWrongSlot", []string{resetPop}, "the slot the pop vacated is never " +
			"touched: the index must be len(x)-1, not len(x)-<any constant>"},
		{"zeroAfterPop", []string{resetPop}, "after the pop, len(x)-1 is a LIVE " +
			"element — this nils it and leaves the released one, which is a real " +
			"bug shape the guard used to certify as its own fix"},
		{"clearFromAnOffset", []string{resetTruncate}, "clearToCap(x[2:]) clears from " +
			"element 2 to cap and leaves x[0] and x[1] reachable, so it cannot " +
			"exempt a truncation to zero"},

		// AND THE TWO ROUND SIX MEASURED, in the same shape and for the
		// same reason: every live site is already written the way that
		// happens to be safe, so both mutations are silent against the
		// corpus.
		{"clearBeforeReset", []string{resetTruncate}, "the len-relative spelling before " +
			"the reset clears the region above the OLD len, which is already dead; " +
			"the truncation then leaves the whole live range reachable"},
		{"clearWholeArrayFirst", nil, "clear(x[:cap(x)]) names the backing array from " +
			"element zero, so it releases the same region whichever side of the " +
			"reset it is on"},
		{"popTwiceZeroOnce", []string{resetPop}, "two pops vacate two slots and one " +
			"zeroing releases one: the second pop has to find its own evidence, " +
			"and the first pop's does not carry"},
	} {
		var decl *ast.FuncDecl
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Name.Name == tc.fn {
				decl = fd
			}
		}
		if decl == nil {
			t.Fatalf("the fixture has no func %s", tc.fn)
		}
		got := reported(decl)
		if !slices.Equal(got, tc.want) {
			t.Errorf("%s: unexempted resets = %v, want %v — %s", tc.fn, got, tc.want, tc.why)
		}
	}
}
