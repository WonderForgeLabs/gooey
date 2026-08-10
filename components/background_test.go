package components

import (
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// Epic #26 acceptance (a): a child repainting alone inside a
// background-painted container paints exactly 1 component and leaves no
// hole — its pre-clear goes to the nearest ancestor's background, not to
// the terminal default.
func TestChildRepaintAloneLeavesNoHoleInBackground(t *testing.T) {
	bg := render.RGB(20, 40, 120)
	content := prop.NewSource("one")
	child := &Text{Content: content}
	root := &Border{Background: Col(bg), Child: child}
	c := gooey.NewComposer(root, 10, 5)
	c.Frame()

	content.Set("two")
	f, painted := c.Frame()
	if painted != 1 {
		t.Fatalf("child change painted %d components, want exactly 1", painted)
	}
	if got := row(f.Cells, 1); got != "│two     │" {
		t.Fatalf("child row = %q, want %q", got, "│two     │")
	}
	// The cells the child's pre-clear touched but its glyphs do not cover
	// must carry the Border's background — before this pass they cleared
	// to render.Style{} and punched a default-colored hole.
	for _, p := range []struct{ x, y int }{{5, 1}, {2, 2}, {8, 3}} {
		if got := f.Cells.At(p.x, p.y).Style.Bg; got != bg {
			t.Errorf("cell (%d,%d) bg = %+v, want the container background %+v (hole)", p.x, p.y, got, bg)
		}
	}
}

// Epic #26 acceptance (b): a container with a background repainting
// repaints its subtree — the fill covers the children's cells, and the
// z-ordered pass puts them back down on top in the same frame — and
// wipes nothing.
func TestContainerRepaintOverBackgroundRepaintsSubtreeAndWipesNothing(t *testing.T) {
	bg := render.RGB(60, 20, 20)
	title := prop.NewSource("one")
	child := &Text{Content: prop.NewSource("content")}
	root := &Border{Background: Col(bg), Title: title, Child: child}
	c := gooey.NewComposer(root, 20, 5)
	c.Frame()

	title.Set("two")
	f, painted := c.Frame()
	if painted != 2 {
		t.Fatalf("title change painted %d components, want 2 (border + its forced child)", painted)
	}
	if got := row(f.Cells, 1); got != "│content           │" {
		t.Fatalf("child row = %q — container repaint wiped child cells", got)
	}
	if got := f.Cells.At(12, 2).Style.Bg; got != bg {
		t.Errorf("interior cell bg = %+v, want %+v", got, bg)
	}
	// Chrome sits ON the fill: a Style with an unset Bg inherits it.
	if got := f.Cells.At(0, 0).Style.Bg; got != bg {
		t.Errorf("chrome corner bg = %+v, want %+v", got, bg)
	}
	if _, painted = c.Frame(); painted != 0 {
		t.Errorf("settled frame painted %d components, want 0", painted)
	}
}

// Recoloring the panel repaints the panel AND every leaf that clears
// against it — the leaf read the ancestor's Background inside its own
// paint node, so the dependency is recorded, not declared.
func TestBackgroundChangeRepaintsTheLeavesThatClearAgainstIt(t *testing.T) {
	bg := prop.NewSource(render.RGB(20, 40, 120))
	child := &Text{Content: Str("hi")}
	root := &Border{Background: bg, Child: child}
	c := gooey.NewComposer(root, 10, 4)
	c.Frame()

	next := render.RGB(120, 40, 20)
	bg.Set(next)
	f, painted := c.Frame()
	if painted != 2 {
		t.Fatalf("background change painted %d components, want 2 (border + child)", painted)
	}
	if got := f.Cells.At(6, 1).Style.Bg; got != next {
		t.Errorf("interior cell bg = %+v, want the new background %+v", got, next)
	}
}

// The gap rows of a stack belong to no child; only the container's fill
// can color them.
func TestStackBackgroundFillsTheGapCells(t *testing.T) {
	bg := render.RGB(10, 60, 30)
	root := &VStack{
		Gap:        1,
		Background: Col(bg),
		Children:   []gooey.Component{&Text{Content: Str("aa")}, &Text{Content: Str("bb")}},
	}
	c := gooey.NewComposer(root, 6, 4)
	f, _ := c.Frame()
	if got := row(f.Cells, 0); got != "aa" {
		t.Fatalf("row 0 = %q", got)
	}
	// Row 1 is the gap: no child owns it, the fill must.
	if cell := f.Cells.At(0, 1); cell.Rune != ' ' || cell.Style.Bg != bg {
		t.Errorf("gap cell = %+v, want a blank cell with bg %+v", cell, bg)
	}
	if got := row(f.Cells, 2); got != "bb" {
		t.Fatalf("row 2 = %q", got)
	}
}

// A chrome-only container keeps the cheap path: no background declared
// means no fill, no covering, and a container repaint does NOT force its
// subtree. (TestContainerRepaintPreservesChildCells pins the same thing
// from the buffer side.)
func TestChromeOnlyContainerRepaintStaysOneComponent(t *testing.T) {
	title := prop.NewSource("one")
	child := &Text{Content: prop.NewSource("content")}
	root := &Border{Title: title, Child: child}
	c := gooey.NewComposer(root, 20, 5)
	c.Frame()

	title.Set("two")
	_, painted := c.Frame()
	if painted != 1 {
		t.Fatalf("chrome-only title change painted %d components, want 1", painted)
	}
}

// The spec's addendum, third face of the same missing z-order: a
// container turned Hidden at runtime must take its chrome off the
// screen. Its clear wipes its whole bounds, and the z-ordered pass
// repaints its still-visible children above it — visibility stays
// per-element, as everywhere in layout.
func TestHidingAContainerAtRuntimeErasesItsChrome(t *testing.T) {
	child := &Text{Content: Str("content")}
	root := &Border{Title: Str("box"), Child: child}
	c := gooey.NewComposer(root, 20, 5)
	c.Frame()
	if got := c.Cells().At(0, 0).Rune; got != '╭' {
		t.Fatalf("first frame corner = %q, want the chrome", got)
	}

	root.LayoutProps().Visibility = gooey.Hidden
	// Erasure is a sweep, not a paint: the vanished container's clear
	// happens outside any paint node (toolkit wave 2 moved it there so
	// the restore pass can repaint what an overlay was covering), so the
	// only component that PAINTS is the still-visible child the clear
	// forced back down.
	f, painted := c.Frame()
	if painted != 1 {
		t.Fatalf("hiding the container painted %d components, want 1 (the forced child; the clear itself is a sweep)", painted)
	}
	if got := f.Cells.At(0, 0).Rune; got != ' ' {
		t.Errorf("corner after hiding = %q, want it erased", got)
	}
	// The child is still Visible: per-element visibility means the frame
	// keeps the content row, now without the chrome around it.
	if got := row(f.Cells, 1); got != " content" {
		t.Errorf("child row after hiding = %q, want %q (content, no chrome)", got, " content")
	}

	root.LayoutProps().Visibility = gooey.Visible
	f, painted = c.Frame()
	if painted != 1 {
		t.Errorf("showing it again painted %d components, want 1 (chrome only, child untouched)", painted)
	}
	if got := f.Cells.At(0, 0).Rune; got != '╭' {
		t.Errorf("corner after showing = %q, want the chrome back", got)
	}
}
