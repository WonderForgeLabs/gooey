package components

import (
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/term"
)

func at(w gooey.Component, left, top int) gooey.Component {
	l := gooey.LayoutOf(w)
	l.Left, l.Top = left, top
	return w
}

func canvasFrame(root gooey.Component, cols, rows int) *gooey.Frame {
	return gooey.Compose(root, term.Caps{Cols: cols, Rows: rows}, nil)
}

func dump(f *gooey.Frame, cols, rows int) string {
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
	c := &Canvas{Children: []gooey.Component{
		at(a, 0, 0),
		at(b, 5, 2),
	}}
	canvasFrame(c, 10, 4)

	if got, want := a.Bounds(), (gooey.Rect{X: 0, Y: 0, W: 1, H: 1}); got != want {
		t.Errorf("child at (0,0): bounds %+v, want %+v", got, want)
	}
	if got, want := b.Bounds(), (gooey.Rect{X: 5, Y: 2, W: 1, H: 1}); got != want {
		t.Errorf("child at (5,2): bounds %+v, want %+v", got, want)
	}
}

// The offset is relative to the Canvas, not to the screen — otherwise
// a Canvas could not be nested inside any other layout.
func TestCanvasOffsetsAreRelativeToTheCanvas(t *testing.T) {
	inner := &Text{Content: Str("X")}
	c := &Canvas{Children: []gooey.Component{at(inner, 2, 1)}}
	// A Border puts the canvas at (1,1) with a 1-cell frame all round.
	root := &Border{Child: c}
	canvasFrame(root, 12, 6)

	if got, want := inner.Bounds(), (gooey.Rect{X: 3, Y: 2, W: 1, H: 1}); got != want {
		t.Errorf("nested canvas child: bounds %+v, want %+v (canvas origin + offset)", got, want)
	}
}

// A child positioned near the right edge is measured against the space
// that is actually left, so it clips itself instead of overhanging.
func TestCanvasConstrainsChildrenToRemainingSpace(t *testing.T) {
	long := &Text{Content: Str("ABCDEFGHIJ")}
	c := &Canvas{Children: []gooey.Component{at(long, 6, 0)}}
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
	c := &Canvas{Children: []gooey.Component{
		at(under, 0, 0),
		at(over, 1, 0),
	}}
	f := canvasFrame(c, 6, 1)

	if got, want := dump(f, 6, 1), "XabX  \n"; got != want {
		t.Errorf("overlap frame: %q, want %q (later sibling on top)", got, want)
	}
}

// Epic #26 acceptance (c), inverted from the test that used to pin the
// deficiency: an occluded component repainting ALONE must not end up on
// top of its occluder. The Composer's z-ordered pass forces the clean
// occluder — later in tree order, therefore above — to repaint over it
// in the same frame: exactly 2 components, and tree order still wins.
func TestCanvasOverlapRepaintRepaintsTheOccluderAbove(t *testing.T) {
	text := Str("XXXX")
	under := &Text{Content: text}
	over := &Text{Content: Str("ab")}
	c := &Canvas{Children: []gooey.Component{
		at(under, 0, 0),
		at(over, 1, 0),
	}}
	comp := gooey.NewComposer(c, 6, 1)
	f, _ := comp.Frame()
	if got, want := dump(f, 6, 1), "XabX  \n"; got != want {
		t.Fatalf("first frame: %q, want %q", got, want)
	}

	// Dirty ONLY the occluded component.
	text.Set("YYYY")
	f, painted := comp.Frame()
	if painted != 2 {
		t.Fatalf("painted %d components, want exactly 2 (the occluded text + its forced occluder)", painted)
	}
	if got, want := dump(f, 6, 1), "YabY  \n"; got != want {
		t.Errorf("after repainting the occluded component: %q, want %q — "+
			"the occluder must repaint above the new content", got, want)
	}

	// And the frame after is settled: forcing is per-frame, not a leak.
	if _, painted = comp.Frame(); painted != 0 {
		t.Errorf("settled frame painted %d components, want 0", painted)
	}
}

// Canvas is a container: it must not pre-clear its own bounds, or a
// repaint of the canvas would wipe children whose paint nodes are clean.
// (This is the bug that once blanked pane interiors.)
func TestCanvasPaintsNoChromeOfItsOwn(t *testing.T) {
	c := &Canvas{Children: []gooey.Component{at(&Text{Content: Str("keep")}, 1, 0)}}
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
	c := &Canvas{Children: []gooey.Component{at(&Text{Content: Str("x")}, 1, 1)}}
	if got, want := c.Measure(gooey.Size{W: 20, H: 8}), (gooey.Size{W: 20, H: 8}); got != want {
		t.Errorf("Measure = %+v, want %+v", got, want)
	}
}

// Canvas children keep the rest of the FrameworkElement contract:
// Collapsed still removes them, and margins still apply on top of the
// absolute offset.
func TestCanvasChildrenKeepLayoutSemantics(t *testing.T) {
	hidden := &Text{Content: Str("gone")}
	gooey.L(hidden, gooey.Layout{Visibility: gooey.Collapsed, Left: 0, Top: 0})
	margined := &Text{Content: Str("m")}
	gooey.L(margined, gooey.Layout{Margin: gooey.Thickness{L: 2}, Left: 1, Top: 1})

	c := &Canvas{Children: []gooey.Component{hidden, margined}}
	f := canvasFrame(c, 8, 3)

	if got := dump(f, 8, 3); strings.Contains(got, "gone") {
		t.Errorf("collapsed canvas child painted: %q", got)
	}
	// Offset 1 plus a 2-cell left margin puts it at x=3.
	if got, want := margined.Bounds().X, 3; got != want {
		t.Errorf("margined child x = %d, want %d (offset + margin)", got, want)
	}
}
