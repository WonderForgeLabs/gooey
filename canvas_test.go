package gooey

import (
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey/term"
)

func at(w Widget, left, top int) Widget {
	l := layoutOf(w)
	l.Left, l.Top = left, top
	return w
}

func canvasFrame(root Widget, cols, rows int) *Frame {
	return Compose(root, term.Caps{Cols: cols, Rows: rows}, nil)
}

func dump(f *Frame, cols, rows int) string {
	var sb strings.Builder
	for y := 0; y < rows; y++ {
		for x := 0; x < cols; x++ {
			sb.WriteRune(f.Cells.At(x, y).Rune)
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}

func TestCanvasArrangesChildrenAtAbsoluteOffsets(t *testing.T) {
	a := &Text{Content: Str("A")}
	b := &Text{Content: Str("B")}
	c := &Canvas{Children: []Widget{
		at(a, 0, 0),
		at(b, 5, 2),
	}}
	canvasFrame(c, 10, 4)

	if got, want := a.Bounds(), (Rect{0, 0, 1, 1}); got != want {
		t.Errorf("child at (0,0): bounds %+v, want %+v", got, want)
	}
	if got, want := b.Bounds(), (Rect{5, 2, 1, 1}); got != want {
		t.Errorf("child at (5,2): bounds %+v, want %+v", got, want)
	}
}

// The offset is relative to the Canvas, not to the screen — otherwise
// a Canvas could not be nested inside any other layout.
func TestCanvasOffsetsAreRelativeToTheCanvas(t *testing.T) {
	inner := &Text{Content: Str("X")}
	c := &Canvas{Children: []Widget{at(inner, 2, 1)}}
	// A Border puts the canvas at (1,1) with a 1-cell frame all round.
	root := &Border{Child: c}
	canvasFrame(root, 12, 6)

	if got, want := inner.Bounds(), (Rect{3, 2, 1, 1}); got != want {
		t.Errorf("nested canvas child: bounds %+v, want %+v (canvas origin + offset)", got, want)
	}
}

// A child positioned near the right edge is measured against the space
// that is actually left, so it clips itself instead of overhanging.
func TestCanvasConstrainsChildrenToRemainingSpace(t *testing.T) {
	long := &Text{Content: Str("ABCDEFGHIJ")}
	c := &Canvas{Children: []Widget{at(long, 6, 0)}}
	f := canvasFrame(c, 10, 1)

	if got, want := long.Bounds().W, 4; got != want {
		t.Errorf("width at offset 6 of 10: %d, want %d", got, want)
	}
	if got, want := dump(f, 10, 1), "      ABCD\n"; got != want {
		t.Errorf("frame:\n%q\nwant:\n%q", got, want)
	}
}

// Absolute positioning means overlap is legal. Paint order is tree
// order: the later sibling wins.
func TestCanvasOverlapPaintsInTreeOrder(t *testing.T) {
	under := &Text{Content: Str("XXXX")}
	over := &Text{Content: Str("ab")}
	c := &Canvas{Children: []Widget{
		at(under, 0, 0),
		at(over, 1, 0),
	}}
	f := canvasFrame(c, 6, 1)

	if got, want := dump(f, 6, 1), "XabX  \n"; got != want {
		t.Errorf("overlap frame: %q, want %q (later sibling on top)", got, want)
	}
}

// The honest limit, pinned so it cannot regress silently: with paint-level
// damage, an occluded widget repainting ALONE overwrites the sibling that
// was covering it. The occluder is clean, so nothing repaints it.
//
// If a future z-ordered repaint fixes this, this test should be inverted
// deliberately — it documents a known artifact, not a desired one.
func TestCanvasOverlapRepaintLeavesOccluderDamaged(t *testing.T) {
	text := Str("XXXX")
	under := &Text{Content: text}
	over := &Text{Content: Str("ab")}
	c := &Canvas{Children: []Widget{
		at(under, 0, 0),
		at(over, 1, 0),
	}}
	comp := NewComposer(c, 6, 1)
	f, _ := comp.Frame()
	if got, want := dump(f, 6, 1), "XabX  \n"; got != want {
		t.Fatalf("first frame: %q, want %q", got, want)
	}

	// Dirty ONLY the occluded widget.
	text.Set("YYYY")
	f, painted := comp.Frame()
	if painted != 1 {
		t.Fatalf("painted %d widgets, want exactly 1 (only the occluded text is dirty)", painted)
	}
	if got, want := dump(f, 6, 1), "YYYY  \n"; got != want {
		t.Errorf("after repainting the occluded widget: %q, want %q — "+
			"the occluder is clean, so it does not repaint over the new content", got, want)
	}
}

// Canvas is a container: it must not pre-clear its own bounds, or a
// repaint of the canvas would wipe children whose paint nodes are clean.
// (This is the bug that once blanked pane interiors.)
func TestCanvasPaintsNoChromeOfItsOwn(t *testing.T) {
	c := &Canvas{Children: []Widget{at(&Text{Content: Str("keep")}, 1, 0)}}
	f := canvasFrame(c, 8, 2)
	before := dump(f, 8, 2)

	c.Render(f) // painting the container directly must change nothing
	if after := dump(f, 8, 2); after != before {
		t.Errorf("Canvas.Render altered the buffer:\n%q\nwas:\n%q", after, before)
	}
}

// Canvas fills its slot rather than shrinking to a bounding box of
// absolutely-placed children.
func TestCanvasFillsItsSlot(t *testing.T) {
	c := &Canvas{Children: []Widget{at(&Text{Content: Str("x")}, 1, 1)}}
	if got, want := c.Measure(Size{20, 8}), (Size{20, 8}); got != want {
		t.Errorf("Measure = %+v, want %+v", got, want)
	}
}

// Canvas children keep the rest of the FrameworkElement contract:
// Collapsed still removes them, and margins still apply on top of the
// absolute offset.
func TestCanvasChildrenKeepLayoutSemantics(t *testing.T) {
	hidden := &Text{Content: Str("gone")}
	L(hidden, Layout{Visibility: Collapsed, Left: 0, Top: 0})
	margined := &Text{Content: Str("m")}
	L(margined, Layout{Margin: Thickness{L: 2}, Left: 1, Top: 1})

	c := &Canvas{Children: []Widget{hidden, margined}}
	f := canvasFrame(c, 8, 3)

	if got := dump(f, 8, 3); strings.Contains(got, "gone") {
		t.Errorf("collapsed canvas child painted: %q", got)
	}
	// Offset 1 plus a 2-cell left margin puts it at x=3.
	if got, want := margined.Bounds().X, 3; got != want {
		t.Errorf("margined child x = %d, want %d (offset + margin)", got, want)
	}
}
