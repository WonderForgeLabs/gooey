package gooey

import (
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
