package components

import (
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// row is the row as a terminal would read it, trailing blanks trimmed.
// The readback itself is render.RowText, which is where the
// continuation markers get skipped: building the string here cell by
// cell rendered them as literal runes, so no fixture in this package
// could hold a wide glyph and be asserted on.
func row(b *render.Buffer, y int) string {
	return strings.TrimRight(render.RowText(b, y), " ")
}

func TestComposerDamageIsPerComponent(t *testing.T) {
	a := prop.NewSource("aaa")
	b := prop.NewSource("bbb")
	ta := &Text{Content: a}
	tb := &Text{Content: b}
	root := &VStack{Children: []gooey.Component{ta, tb}}
	c := gooey.NewComposer(root, 20, 5)

	f, painted := c.Frame()
	if painted != 3 { // vstack + 2 texts
		t.Fatalf("first frame painted %d components, want 3", painted)
	}
	if row(f.Cells, 0) != "aaa" || row(f.Cells, 1) != "bbb" {
		t.Fatalf("buffer rows = %q,%q", row(f.Cells, 0), row(f.Cells, 1))
	}

	// Same-width change: bounds stable, so ONLY tb's node repaints.
	b.Set("BBB")
	f, painted = c.Frame()
	if painted != 1 {
		t.Fatalf("after b change painted %d components, want 1", painted)
	}
	if row(f.Cells, 0) != "aaa" || row(f.Cells, 1) != "BBB" {
		t.Fatalf("buffer rows = %q,%q", row(f.Cells, 0), row(f.Cells, 1))
	}

	// Clean frame: nothing repaints.
	if _, painted = c.Frame(); painted != 0 {
		t.Fatalf("clean frame painted %d components, want 0", painted)
	}
}

func TestComposerBoundsChangeForcesRepaintAndClears(t *testing.T) {
	a := prop.NewSource("wide contents")
	b := prop.NewSource("under")
	ta := &Text{Content: a}
	tb := &Text{Content: b}
	root := &VStack{Children: []gooey.Component{ta, tb}}
	c := gooey.NewComposer(root, 30, 5)
	c.Frame()

	// Shrinking ta narrows its bounds; the vacated cells must clear
	// and the component must repaint even though width-change also
	// re-arranges siblings.
	a.Set("thin")
	f, painted := c.Frame()
	if painted < 1 {
		t.Fatalf("painted %d components, want >= 1", painted)
	}
	if got := row(f.Cells, 0); got != "thin" {
		t.Fatalf("row0 = %q, want %q (stale cells not cleared)", got, "thin")
	}
	if got := row(f.Cells, 1); got != "under" {
		t.Fatalf("row1 = %q", got)
	}
}

func TestContainerRepaintPreservesChildCells(t *testing.T) {
	title := prop.NewSource("one")
	child := &Text{Content: prop.NewSource("content")}
	root := &Border{Title: title, Child: child}
	c := gooey.NewComposer(root, 20, 5)
	c.Frame()

	// Repainting the Border (title change) must not wipe the child's
	// cells: the Border pre-clearing its full bounds would blank the
	// interior while the clean child node never repaints.
	title.Set("two")
	f, painted := c.Frame()
	if painted != 1 {
		t.Fatalf("painted %d components, want 1 (border only)", painted)
	}
	if got := row(f.Cells, 1); got != "│content           │" {
		t.Fatalf("child row = %q — container repaint wiped child cells", got)
	}
}

func TestComposerInvalidateHookFires(t *testing.T) {
	a := prop.NewSource("x")
	root := &VStack{Children: []gooey.Component{&Text{Content: a}}}
	c := gooey.NewComposer(root, 10, 2)
	fired := 0
	c.OnInvalidate(func() { fired++ })
	c.Frame()
	a.Set("y")
	if fired == 0 {
		t.Fatal("OnInvalidate did not fire on property change")
	}
}

// Visibility is a plain field, so nothing dirties when it flips. The
// Composer's per-frame sweep catches the delta — otherwise a component
// turned Hidden at runtime stays on screen forever.
func TestHidingALeafAtRuntimeErasesIt(t *testing.T) {
	target := &Text{Content: Str("SECRET")}
	root := &VStack{Children: []gooey.Component{&Text{Content: Str("keep")}, target}}
	c := gooey.NewComposer(root, 10, 2)
	c.Frame()

	if got := row(c.Cells(), 1); got != "SECRET" {
		t.Fatalf("first frame row 1 = %q", got)
	}

	target.LayoutProps().Visibility = gooey.Hidden
	_, painted := c.Frame()
	if painted != 1 {
		t.Errorf("hiding one leaf painted %d components, want exactly 1", painted)
	}
	if got := row(c.Cells(), 1); got != "" {
		t.Errorf("row 1 after hiding = %q, want it erased", got)
	}

	// And back again.
	target.LayoutProps().Visibility = gooey.Visible
	if _, painted = c.Frame(); painted != 1 {
		t.Errorf("showing it again painted %d components, want 1", painted)
	}
	if got := row(c.Cells(), 1); got != "SECRET" {
		t.Errorf("row 1 after showing = %q, want it back", got)
	}
}

// Bound visibility must cost exactly what a literal flip costs: the Set
// schedules the frame (that is ALL it adds — the observer is not a paint
// node), and the same per-frame sweep erases and restores. Mirror of
// TestHidingALeafAtRuntimeErasesIt with a *prop.Property[Visibility].
func TestBoundVisibilityHiddenFlipMatchesLiteralDamage(t *testing.T) {
	vis := prop.NewSource(gooey.Visible)
	target := &Text{Content: Str("SECRET")}
	target.LayoutProps().BindVisibility(vis)
	root := &VStack{Children: []gooey.Component{&Text{Content: Str("keep")}, target}}
	c := gooey.NewComposer(root, 10, 2)
	fired := 0
	c.OnInvalidate(func() { fired++ })
	c.Frame()

	if got := row(c.Cells(), 1); got != "SECRET" {
		t.Fatalf("first frame row 1 = %q", got)
	}

	// The literal field flip dirties nothing — the bound Set MUST: that
	// is the entire reason the binding exists.
	vis.Set(gooey.Hidden)
	if fired == 0 {
		t.Fatal("Set on a bound Visibility did not schedule a frame")
	}
	_, painted := c.Frame()
	if painted != 1 {
		t.Errorf("hiding via binding painted %d components, want exactly 1 (same as the literal flip)", painted)
	}
	if got := row(c.Cells(), 1); got != "" {
		t.Errorf("row 1 after bound hide = %q, want it erased", got)
	}

	vis.Set(gooey.Visible)
	if _, painted = c.Frame(); painted != 1 {
		t.Errorf("showing via binding painted %d components, want 1", painted)
	}
	if got := row(c.Cells(), 1); got != "SECRET" {
		t.Errorf("row 1 after bound show = %q, want it back", got)
	}
}

// A bound Collapsed flip is a LAYOUT change — the collapsed element
// measures to zero and its siblings move. Run the identical tree twice,
// flipping one by field and one by binding, and require identical paint
// counts and identical cells frame by frame: "same damage as the literal
// sweep" as an executable statement.
func TestBoundVisibilityCollapseMatchesLiteralDamage(t *testing.T) {
	build := func() (*gooey.Composer, *Text) {
		target := &Text{Content: Str("first")}
		root := &VStack{Children: []gooey.Component{target, &Text{Content: Str("second")}}}
		return gooey.NewComposer(root, 12, 3), target
	}

	lit, litTarget := build()
	bnd, bndTarget := build()
	vis := prop.NewSource(gooey.Visible)
	bndTarget.LayoutProps().BindVisibility(vis)
	lit.Frame()
	bnd.Frame()

	steps := []gooey.Visibility{gooey.Collapsed, gooey.Visible, gooey.Hidden, gooey.Visible}
	for _, v := range steps {
		litTarget.LayoutProps().Visibility = v
		vis.Set(v)
		_, litPainted := lit.Frame()
		_, bndPainted := bnd.Frame()
		if litPainted != bndPainted {
			t.Errorf("flip to %v: literal painted %d, bound painted %d — damage counts must match", v, litPainted, bndPainted)
		}
		for y := 0; y < 3; y++ {
			if lr, br := row(lit.Cells(), y), row(bnd.Cells(), y); lr != br {
				t.Errorf("flip to %v: row %d literal %q, bound %q", v, y, lr, br)
			}
		}
	}
	if got := row(bnd.Cells(), 0); got != "first" {
		t.Fatalf("row 0 after the round trip = %q, want %q", got, "first")
	}
}

// Visibility="{{.Show}}" with a bool viewmodel property: true is
// Visible, false is Collapsed (the space is reclaimed, not reserved).
func TestBoundVisibilityBoolMapsToCollapsed(t *testing.T) {
	show := prop.NewSource(true)
	target := &Text{Content: Str("detail")}
	target.LayoutProps().BindVisibilityBool(show)
	root := &VStack{Children: []gooey.Component{target, &Text{Content: Str("footer")}}}
	c := gooey.NewComposer(root, 12, 3)
	c.Frame()
	if row(c.Cells(), 0) != "detail" || row(c.Cells(), 1) != "footer" {
		t.Fatalf("first frame rows = %q,%q", row(c.Cells(), 0), row(c.Cells(), 1))
	}

	show.Set(false)
	c.Frame()
	if got := row(c.Cells(), 0); got != "footer" {
		t.Errorf("row 0 after false = %q, want %q (collapsed reclaims the row)", got, "footer")
	}
	if got := row(c.Cells(), 1); got != "" {
		t.Errorf("row 1 after false = %q, want empty", got)
	}

	show.Set(true)
	c.Frame()
	if row(c.Cells(), 0) != "detail" || row(c.Cells(), 1) != "footer" {
		t.Errorf("rows after true = %q,%q", row(c.Cells(), 0), row(c.Cells(), 1))
	}
}

// A redundant Set on a bound Visibility schedules a frame (Set never
// compares — that is the property system's contract), but the sweep
// still sees no delta, so the frame paints NOTHING.
func TestBoundVisibilityRedundantSetPaintsNothing(t *testing.T) {
	vis := prop.NewSource(gooey.Visible)
	target := &Text{Content: Str("x")}
	target.LayoutProps().BindVisibility(vis)
	root := &VStack{Children: []gooey.Component{target}}
	c := gooey.NewComposer(root, 10, 2)
	c.Frame()

	vis.Set(gooey.Visible)
	if _, painted := c.Frame(); painted != 0 {
		t.Errorf("redundant bound Set painted %d components, want 0", painted)
	}
}

// A steady visibility must not cause repaints — the sweep compares, it
// does not dirty unconditionally.
func TestUnchangedVisibilityDoesNotRepaint(t *testing.T) {
	root := &VStack{Children: []gooey.Component{&Text{Content: Str("a")}}}
	c := gooey.NewComposer(root, 10, 2)
	c.Frame()
	if _, painted := c.Frame(); painted != 0 {
		t.Errorf("a steady frame painted %d components, want 0", painted)
	}
}

// Resize is the SIGWINCH path. A resize invalidates EVERYTHING — the
// buffer every node painted into no longer exists — so the damage
// contract inverts here: the frame after a resize must repaint the whole
// tree, and the one after that must repaint nothing.
func TestResizeRepaintsTheWholeTreeExactlyOnce(t *testing.T) {
	a := prop.NewSource("aaa")
	b := prop.NewSource("bbb")
	root := &VStack{Children: []gooey.Component{&Text{Content: a}, &Text{Content: b}}}
	c := gooey.NewComposer(root, 20, 5)
	c.Frame()

	c.Resize(40, 12)
	if cols, rows := c.Size(); cols != 40 || rows != 12 {
		t.Fatalf("size after Resize = %dx%d, want 40x12", cols, rows)
	}
	f, painted := c.Frame()
	if painted != 3 {
		t.Errorf("resize repainted %d components, want the whole tree (3)", painted)
	}
	if f.Cells.W != 40 || f.Cells.H != 12 {
		t.Errorf("buffer is %dx%d, want 40x12", f.Cells.W, f.Cells.H)
	}
	if row(f.Cells, 0) != "aaa" || row(f.Cells, 1) != "bbb" {
		t.Errorf("content did not survive the resize: %q,%q", row(f.Cells, 0), row(f.Cells, 1))
	}
	if _, painted = c.Frame(); painted != 0 {
		t.Errorf("the frame after a resize painted %d components, want 0", painted)
	}
	// A resize to the same size is not a resize.
	c.Resize(40, 12)
	if _, painted = c.Frame(); painted != 0 {
		t.Errorf("a no-op resize dirtied %d components", painted)
	}
}
