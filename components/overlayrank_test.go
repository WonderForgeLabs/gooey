package components

import (
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/render"
)

// A notification is never hidden by a menu (#439).
//
// This is the user-visible claim; gooey/overlayrank_test.go pins the
// mechanism. Three separate places asserted this ordering in PROSE and
// nothing asserted it in a test, which is how #437 reversed all three
// and left the suite green: toast.go said being the root's last child
// "puts every toast above the page", the markup reference said tooltips
// paint above toasts too, and menu_live_test.go's comment said "the
// toast layer is topmost".
//
// THE FIXTURE DECLARES THE HOSTS FIRST AND THE MENUBAR LAST, which is
// not incidental — it is the arrangement the framework tells an author
// to use, so its dropdown covers the page. Under declaration order that
// is exactly the arrangement in which a toast loses.

// rankPage is a page-wide stack: a ToastHost and an AdornmentLayer
// declared BEFORE a MenuBar, all over the same rect.
type rankPage struct {
	gooey.Base
	kids []gooey.Component
}

func (p *rankPage) ChildComponents() []gooey.Component { return p.kids }
func (p *rankPage) Render(*gooey.Frame)                {}
func (p *rankPage) Measure(a gooey.Size) gooey.Size    { return a }
func (p *rankPage) Arrange(b gooey.Rect) {
	p.Base.Arrange(b)
	for _, k := range p.kids {
		gooey.ArrangeChild(k, b)
	}
}

// TestAToastIsNotHiddenByAnOpenMenu is #439 as reported.
//
// THE GEOMETRY IS THE HARD PART, and getting it wrong makes this pass
// against the bug. Toasts stack DOWNWARD from the host's top edge, one
// row each, right-aligned; a dropdown hangs from row 1 down the LEFT.
// So the first toast sits on row 0 beside the bar and never touches the
// dropdown at all — the first version of this test asserted over an
// empty intersection. Three toasts put rows 1 and 2 inside the
// dropdown's rows, and item text wide enough to reach the right margin
// puts them inside its columns.
func TestAToastIsNotHiddenByAnOpenMenu(t *testing.T) {
	const w, h = 40, 12
	host := &ToastHost{}
	bar := &MenuBar{Menus: []Menu{{
		Title: "_File",
		Items: []MenuItem{
			{Text: strings.Repeat("A", 34), Action: gooey.Command(func() {})},
			{Text: strings.Repeat("B", 34), Action: gooey.Command(func() {})},
			{Text: strings.Repeat("C", 34), Action: gooey.Command(func() {})},
		},
	}}}
	// The bar is declared LAST — the shape the framework used to instruct
	// and no longer does. Kept because it is the WORST case for this
	// test: it is exactly the arrangement in which document order would
	// put the dropdown above the toast, so a rank that did not work shows
	// up as the toast vanishing.
	page := &rankPage{kids: []gooey.Component{host, bar}}

	c := gooey.NewComposer(page, w, h)
	t.Cleanup(c.Close)
	c.Frame()
	bar.Open(0, nil)
	c.Frame()
	host.Show("one")
	host.Show("two")
	third := host.Show("THREE")
	c.Frame()
	f, painted := c.Frame()

	drop := bar.DropdownBounds()
	if drop.W == 0 {
		t.Fatal("the dropdown is not open: nothing below was tested")
	}
	// ASK THE TOAST WHERE IT IS rather than re-deriving it. The first
	// version computed gooey.Rect{X: w - 7, Y: 2, W: 7, H: 1} by hand —
	// render.StringWidth("THREE")+2 and the arrange stride — which is the
	// rect the overlap precondition below vouches for. Re-derived, it
	// would keep vouching after toast geometry moved, and the failure
	// message would name a row the toast is no longer on. Found in review
	// of #456.
	toast := third.Bounds()
	if toast.W == 0 {
		t.Fatal("the third toast has no bounds: it was never arranged")
	}
	// Prove the overlap before asserting who won it. A test whose two
	// rects do not intersect passes no matter which one paints on top.
	if !rectsOverlap(drop, toast) {
		t.Fatalf("the dropdown %v and the third toast %v do not overlap: this fixture cannot see the bug", drop, toast)
	}
	if got := render.RowText(f.Cells, toast.Y); !strings.Contains(got, "THREE") {
		t.Errorf("a toast on a row the open dropdown covers is not visible — it is painting underneath.\nrow %d: %q\n%s",
			toast.Y, got, framePlane(f, w, h))
	}
	// THE DAMAGE COUNT, which the cell assertion above cannot be. Ranking
	// MOVED this number and CLAUDE.md is explicit that when a change moves
	// a count, that is the change: before ranks the toast sat in the
	// ordinary layer AHEAD of the lifted dropdown, so the dropdown's
	// covered paint never forced it. Now the toast is later in c.paint,
	// intersects, and is forced to repaint in the same frame.
	//
	// Zero is what this frame must report. Nothing was dirtied between
	// the two Frame() calls above, so a non-zero count means something on
	// this page repaints every frame while a menu is open — which would
	// make the assertion above pass for the wrong reason, by repainting
	// the toast unconditionally rather than by ordering it correctly.
	// Added in review of #456.
	if painted != 0 {
		t.Errorf("a settled frame with an open menu and three toasts repainted %d components, want 0 — "+
			"the ordering above is being bought by a per-frame repaint", painted)
	}
}

// atAdornmentRank is a leaf that paints a row of text and claims
// gooey.OverlayRankAdornment — the rank AdornmentLayer claims.
//
// WHY A STUB AND NOT AdornmentLayer ITSELF, since that is the real
// customer. A layer only paints through an adornment, and an adornment
// is hard to get onto the screen in a Composer fixture for two
// independent reasons, both verified here rather than guessed: a nil
// Anchor() with no PointerFollower is an ORPHAN and the layer drops it
// in the same Arrange it was added in; and making it FREE instead does
// not help, because a free adornment is placed against the pointer and
// FocusManager.Pointer() reports seen=false in a test with no mouse
// events, so the layer parks it at a zero rect. Anchoring it to a real
// component gets it correct bounds and STILL paints nothing here — the
// layer's structural re-sync needs hosting this fixture does not
// provide. That machinery has its own tests (validation_test.go,
// dragghost_test.go, tooltip_test.go); none of it is the rank.
//
// So the boundary is asserted at the rank itself, which is the thing
// under test. The previous version of this test compared the two
// OverlayRank() ints — it asserted 20 > 10 and nothing about painting,
// and passed with the comparator reversed or the ordering removed
// entirely, because it never reached orderPaint. Found in review of
// #456.
// It writes at a rect IT owns rather than at Bounds(). rankPage hands
// every child the whole page and ignores Layout.Left/Top — those are
// Canvas attached properties — so positioning through layout would put
// this on column 0 and never over the toast at all, which is how the
// first draft of this test came to assert over cells the two never
// shared. Owning the rect keeps the fixture about paint ORDER, which is
// the only thing under test here.
type atAdornmentRank struct {
	gooey.Base
	at   gooey.Rect
	text string
}

func (a *atAdornmentRank) OverlaysPage()                   {}
func (a *atAdornmentRank) OverlayRank() int                { return gooey.OverlayRankAdornment }
func (a *atAdornmentRank) Measure(s gooey.Size) gooey.Size { return s }
func (a *atAdornmentRank) Render(f *gooey.Frame) {
	f.Cells.SetString(a.at.X, a.at.Y, a.text, render.Style{})
}

// TestAnAdornmentIsAboveAToast is the other rank boundary, and it is not
// covered by the test above: with only two ranks in play a single
// "overlays win" rule would pass it.
//
// The adornment-ranked component is declared FIRST and the toast host
// second, so DOCUMENT ORDER puts the toast on top. Only the rank can
// reverse that, which is what makes this an ordering assertion.
func TestAnAdornmentIsAboveAToast(t *testing.T) {
	const w, h = 40, 8
	host := &ToastHost{}
	c := gooey.NewComposer(&rankPage{kids: []gooey.Component{host}}, w, h)
	t.Cleanup(c.Close)
	c.Frame()
	toast := host.Show("TOASTTOAST")
	c.Frame()
	tb := toast.Bounds()
	if tb.W == 0 {
		t.Fatal("the toast has no bounds: it was never arranged")
	}
	c.Close()

	// Rebuild with the adornment-ranked leaf writing exactly onto the
	// toast's cells — the only place the two can be compared.
	mark := &atAdornmentRank{at: tb, text: strings.Repeat("M", tb.W)}
	host2 := &ToastHost{}
	c2 := gooey.NewComposer(&rankPage{kids: []gooey.Component{mark, host2}}, w, h)
	t.Cleanup(c2.Close)
	c2.Frame()
	host2.Show("TOASTTOAST")
	f, _ := c2.Frame()

	got := render.RowText(f.Cells, tb.Y)
	if !strings.Contains(got, strings.Repeat("M", tb.W)) {
		t.Errorf("a component at OverlayRankAdornment sitting on a toast's cells is not what is on screen — "+
			"it is painting underneath, reversing what docs/markup-reference.md states.\nrow %d: %q\n"+
			"mark rank %d, toast rank %d\n%s",
			tb.Y, got, rankOf(t, mark), rankOf(t, host2), framePlane(f, w, h))
	}
}

// TestTheOverlayHostsClaimTheRanksTheyDocument is the OTHER half of the
// claim above, and the split is deliberate rather than belt-and-braces.
//
// The cell test proves that a HIGHER RANK PAINTS LAST. It cannot prove
// that AdornmentLayer is the thing holding the higher rank, because it
// asserts through a stub that names OverlayRankAdornment itself —
// mutation M1, ranking the real layer at the popup floor, passes the
// whole suite without this test. That is the same hole the review of
// #456 found in the version that compared two ints and nothing else,
// and moving to cells relocated it rather than closing it.
//
// So: one test for "ranks order paint" (cells, above), one for "these
// hosts claim these ranks" (below). Neither substitutes for the other,
// and each has a mutation that fires only it.
func TestTheOverlayHostsClaimTheRanksTheyDocument(t *testing.T) {
	for _, tc := range []struct {
		name string
		w    gooey.Component
		want int
	}{
		{"AdornmentLayer", &AdornmentLayer{}, gooey.OverlayRankAdornment},
		{"ToastHost", &ToastHost{}, gooey.OverlayRankToast},
	} {
		if got := rankOf(t, tc.w); got != tc.want {
			t.Errorf("%s ranks %d, want %d — the ordering docs/markup-reference.md "+
				"and this package's godoc both state comes from these constants, "+
				"and a host that claims a different one reverses it silently",
				tc.name, got, tc.want)
		}
	}
	if gooey.OverlayRankAdornment <= gooey.OverlayRankToast {
		t.Errorf("OverlayRankAdornment (%d) does not outrank OverlayRankToast (%d): "+
			"a tooltip would paint under a toast",
			gooey.OverlayRankAdornment, gooey.OverlayRankToast)
	}
	if gooey.OverlayRankToast <= gooey.OverlayRankPopup {
		t.Errorf("OverlayRankToast (%d) does not outrank OverlayRankPopup (%d): "+
			"#439 as reported", gooey.OverlayRankToast, gooey.OverlayRankPopup)
	}
}

// rankOf asks through the interface the Composer uses, so a host that
// stopped implementing it at all fails here rather than silently
// dropping to the floor.
func rankOf(t *testing.T, w gooey.Component) int {
	t.Helper()
	r, ok := w.(gooey.OverlayRanker)
	if !ok {
		t.Fatalf("%T is not an OverlayRanker: it is not in the overlay layer at all", w)
	}
	return r.OverlayRank()
}

func framePlane(f *gooey.Frame, w, h int) string {
	var b strings.Builder
	for y := 0; y < h; y++ {
		b.WriteString(render.RowText(f.Cells, y))
		b.WriteByte('\n')
	}
	return b.String()
}
