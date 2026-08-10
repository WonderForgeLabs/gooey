package gooey

import (
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

func row(b *render.Buffer, y int) string {
	var sb strings.Builder
	for x := 0; x < b.W; x++ {
		sb.WriteRune(b.At(x, y).Rune)
	}
	return strings.TrimRight(sb.String(), " ")
}

func TestComposerDamageIsPerWidget(t *testing.T) {
	a := prop.NewSource("aaa")
	b := prop.NewSource("bbb")
	ta := &Text{Content: a}
	tb := &Text{Content: b}
	root := &VStack{Children: []Widget{ta, tb}}
	c := NewComposer(root, 20, 5)

	f, painted := c.Frame()
	if painted != 3 { // vstack + 2 texts
		t.Fatalf("first frame painted %d widgets, want 3", painted)
	}
	if row(f.Cells, 0) != "aaa" || row(f.Cells, 1) != "bbb" {
		t.Fatalf("buffer rows = %q,%q", row(f.Cells, 0), row(f.Cells, 1))
	}

	// Same-width change: bounds stable, so ONLY tb's node repaints.
	b.Set("BBB")
	f, painted = c.Frame()
	if painted != 1 {
		t.Fatalf("after b change painted %d widgets, want 1", painted)
	}
	if row(f.Cells, 0) != "aaa" || row(f.Cells, 1) != "BBB" {
		t.Fatalf("buffer rows = %q,%q", row(f.Cells, 0), row(f.Cells, 1))
	}

	// Clean frame: nothing repaints.
	if _, painted = c.Frame(); painted != 0 {
		t.Fatalf("clean frame painted %d widgets, want 0", painted)
	}
}

func TestComposerBoundsChangeForcesRepaintAndClears(t *testing.T) {
	a := prop.NewSource("wide contents")
	b := prop.NewSource("under")
	ta := &Text{Content: a}
	tb := &Text{Content: b}
	root := &VStack{Children: []Widget{ta, tb}}
	c := NewComposer(root, 30, 5)
	c.Frame()

	// Shrinking ta narrows its bounds; the vacated cells must clear
	// and the widget must repaint even though width-change also
	// re-arranges siblings.
	a.Set("thin")
	f, painted := c.Frame()
	if painted < 1 {
		t.Fatalf("painted %d widgets, want >= 1", painted)
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
	c := NewComposer(root, 20, 5)
	c.Frame()

	// Repainting the Border (title change) must not wipe the child's
	// cells: the Border pre-clearing its full bounds would blank the
	// interior while the clean child node never repaints.
	title.Set("two")
	f, painted := c.Frame()
	if painted != 1 {
		t.Fatalf("painted %d widgets, want 1 (border only)", painted)
	}
	if got := row(f.Cells, 1); got != "│content           │" {
		t.Fatalf("child row = %q — container repaint wiped child cells", got)
	}
}

func TestComposerInvalidateHookFires(t *testing.T) {
	a := prop.NewSource("x")
	root := &VStack{Children: []Widget{&Text{Content: a}}}
	c := NewComposer(root, 10, 2)
	fired := 0
	c.OnInvalidate(func() { fired++ })
	c.Frame()
	a.Set("y")
	if fired == 0 {
		t.Fatal("OnInvalidate did not fire on property change")
	}
}

// Visibility is a plain field, so nothing dirties when it flips. The
// Composer's per-frame sweep catches the delta — otherwise a widget
// turned Hidden at runtime stays on screen forever.
func TestHidingALeafAtRuntimeErasesIt(t *testing.T) {
	target := &Text{Content: Str("SECRET")}
	root := &VStack{Children: []Widget{&Text{Content: Str("keep")}, target}}
	c := NewComposer(root, 10, 2)
	c.Frame()

	if got := row(c.frame.Cells, 1); got != "SECRET" {
		t.Fatalf("first frame row 1 = %q", got)
	}

	target.LayoutProps().Visibility = Hidden
	_, painted := c.Frame()
	if painted != 1 {
		t.Errorf("hiding one leaf painted %d widgets, want exactly 1", painted)
	}
	if got := row(c.frame.Cells, 1); got != "" {
		t.Errorf("row 1 after hiding = %q, want it erased", got)
	}

	// And back again.
	target.LayoutProps().Visibility = Visible
	if _, painted = c.Frame(); painted != 1 {
		t.Errorf("showing it again painted %d widgets, want 1", painted)
	}
	if got := row(c.frame.Cells, 1); got != "SECRET" {
		t.Errorf("row 1 after showing = %q, want it back", got)
	}
}

// A steady visibility must not cause repaints — the sweep compares, it
// does not dirty unconditionally.
func TestUnchangedVisibilityDoesNotRepaint(t *testing.T) {
	root := &VStack{Children: []Widget{&Text{Content: Str("a")}}}
	c := NewComposer(root, 10, 2)
	c.Frame()
	if _, painted := c.Frame(); painted != 0 {
		t.Errorf("a steady frame painted %d widgets, want 0", painted)
	}
}
