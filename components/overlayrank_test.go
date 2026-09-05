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
	// Declared LAST, as the framework instructs, so its dropdown covers
	// the page. That is exactly the arrangement in which document order
	// puts it above the toast.
	page := &rankPage{kids: []gooey.Component{host, bar}}

	c := gooey.NewComposer(page, w, h)
	t.Cleanup(c.Close)
	c.Frame()
	bar.Open(0, nil)
	c.Frame()
	host.Show("one")
	host.Show("two")
	host.Show("THREE")
	c.Frame()
	f, _ := c.Frame()

	drop := bar.DropdownBounds()
	if drop.W == 0 {
		t.Fatal("the dropdown is not open: nothing below was tested")
	}
	// Prove the overlap before asserting who won it. A test whose two
	// rects do not intersect passes no matter which one paints on top.
	toast := gooey.Rect{X: w - 7, Y: 2, W: 7, H: 1}
	if !rectsOverlap(drop, toast) {
		t.Fatalf("the dropdown %v and the third toast %v do not overlap: this fixture cannot see the bug", drop, toast)
	}
	if got := render.RowText(f.Cells, toast.Y); !strings.Contains(got, "THREE") {
		t.Errorf("a toast on a row the open dropdown covers is not visible — it is painting underneath.\nrow %d: %q\n%s",
			toast.Y, got, framePlane(f, w, h))
	}
}


// TestAnAdornmentIsAboveAToast is the other rank boundary, and it is not
// covered by the test above: with only two ranks in play a single
// "overlays win" rule would pass it.
func TestAnAdornmentIsAboveAToast(t *testing.T) {
	ln, an := nodeRank(t, &AdornmentLayer{}), nodeRank(t, &ToastHost{})
	if !(ln > an) {
		t.Errorf("the adornment layer ranks %d and the toast host %d: a tooltip would paint under a toast, "+
			"reversing what docs/markup-reference.md states", ln, an)
	}
}

// nodeRank asks the two components what they claim, through the
// interface the Composer uses. Asserted here rather than on cells
// because an AdornmentLayer with no adornment paints nothing at all —
// and a cell assertion that needs a live Tooltip would be testing
// Tooltip's hover machinery, not the ordering.
func nodeRank(t *testing.T, w gooey.Component) int {
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
