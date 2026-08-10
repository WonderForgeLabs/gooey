package components

import (
	"bytes"
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/graphics"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
	"github.com/WonderForgeLabs/gooey/term"
)

// The picker's pixel tier is placement-lifecycle work, so like the pixel
// button's tests these assert byte signatures and placement geometry, not
// pixels: does each bar own a slot, does a gesture replace exactly the
// bars whose look changed, and does a cell-only terminal see none of it.

// pixelPicker is a picker between two text lines: three paint nodes, one
// of which owns placements. The label is a source so a neighbour repaint
// can be provoked.
func pixelPicker(enc graphics.Encoder) (*gooey.Composer, *ColorPicker, *prop.Property[string]) {
	label := prop.NewSource("caption")
	v := prop.NewSource(render.RGB(10, 20, 30))
	p := &ColorPicker{Value: v}
	root := &VStack{Children: []gooey.Component{
		&Text{Content: Str("above")},
		p,
		&Text{Content: label},
	}}
	c := gooey.NewComposer(root, 30, 8)
	c.SetCaps(term8x16(30, 8))
	c.SetGraphics(enc)
	return c, p, label
}

func TestColorPickerPixelTierPlacesOneBarPerChannel(t *testing.T) {
	c, p, _ := pixelPicker(graphics.Kitty{})
	out := flush(t, c)

	if n := strings.Count(out, "a=T,f=100,"); n != 3 {
		t.Fatalf("the first frame transmitted %d images, want 3 (one per bar):\n%q", n, out)
	}
	f, _ := c.Frame()
	pl := f.Placements()
	if len(pl) != 3 {
		t.Fatalf("the frame carries %d placements, want 3", len(pl))
	}
	b := p.Bounds()
	for ch, q := range pl {
		if q.Col != b.X+pickerLabelW || q.Row != b.Y+ch {
			t.Errorf("bar %d placed at %d,%d, want %d,%d", ch, q.Col, q.Row, b.X+pickerLabelW, b.Y+ch)
		}
		if q.Cols != p.barWidth() || q.Rows != 1 {
			t.Errorf("bar %d spans %dx%d cells, want %dx1", ch, q.Cols, q.Rows, p.barWidth())
		}
	}
	// The cells beneath the image are still the cell-tier gradient: they
	// are what a protocol without placement identity repaints from when a
	// bar moves or vanishes.
	if got := c.Cells().At(b.X+pickerLabelW+1, b.Y).Rune; got != '█' {
		t.Fatalf("the cell under the bar image holds %q, want the cell-tier gradient", got)
	}
}

// Selecting a channel changes only the marker's weight on the row it left
// and the row it landed on: two bars replaced under their existing ids,
// the third untouched.
func TestColorPickerPixelChannelMoveReplacesOnlyTheAffectedBars(t *testing.T) {
	c, p, _ := pixelPicker(graphics.Kitty{})
	// The picker is the page's only focus stop, so it starts focused and
	// the first frame already carries the active marker.
	flush(t, c)

	// A repaint whose state did not change reuses the cached images, and
	// pointer identity makes the flush free: dirty is not damage.
	p.SetFocused(true)
	if out := flush(t, c); out != "" {
		t.Fatalf("a state-identical repaint went on the wire:\n%q", out)
	}

	p.HandleKey(input.Named(input.KeyDown))
	out := flush(t, c)
	if n := strings.Count(out, "a=T,f=100,"); n != 2 {
		t.Fatalf("a channel move retransmitted %d bars, want 2:\n%q", n, out)
	}
	if strings.Contains(out, "i=4,") {
		t.Fatalf("a channel move allocated new placement ids instead of replacing:\n%q", out)
	}
}

// Editing a value replaces all three bars — the moved marker on its own
// row, the re-based sweep on the other two — and repaints exactly the
// picker: the damage pin, unchanged by the tier.
func TestColorPickerPixelEditRepaintsOnlyThePicker(t *testing.T) {
	c, p, _ := pixelPicker(graphics.Kitty{})
	flush(t, c) // starts focused: the picker is the only focus stop

	p.HandleKey(input.Named(input.KeyRight))
	if _, painted := c.Frame(); painted != 1 {
		t.Fatalf("a color edit painted %d components, want exactly 1", painted)
	}
	var buf bytes.Buffer
	if err := c.Flush(&buf); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if n := strings.Count(out, "a=T,f=100,"); n != 3 {
		t.Fatalf("a value edit retransmitted %d bars, want 3:\n%q", n, out)
	}
	if strings.Contains(out, "i=4,") {
		t.Fatalf("a value edit allocated new placement ids instead of replacing:\n%q", out)
	}
}

// A neighbour repainting must not touch the picker's images — per-node
// placement ownership, stated in bytes.
func TestColorPickerPixelBarsSurviveANeighbourRepaint(t *testing.T) {
	c, _, label := pixelPicker(graphics.Kitty{})
	flush(t, c)

	label.Set("a different caption")
	if out := flush(t, c); strings.Contains(out, "\x1b_G") {
		t.Fatalf("an unrelated repaint disturbed the bars:\n%q", out)
	}
}

// Sixel has no placement identity: the bars go out as DCS payloads, and
// their erasure path is the cell damage the diff files — which is why the
// cells beneath stay painted.
func TestColorPickerPixelBarsUnderSixel(t *testing.T) {
	c, _, _ := pixelPicker(graphics.Sixel{})
	out := flush(t, c)
	if n := strings.Count(out, "\x1bP0;0;0q"); n != 3 {
		t.Fatalf("sixel emitted %d images, want 3:\n%q", n, out)
	}
	if strings.Contains(out, "\x1b_G") {
		t.Fatalf("sixel flush contains kitty escapes:\n%q", out)
	}
}

// A replace-in-place — same rectangles, new pixels — has no placement id
// to lean on under sixel: the new payloads themselves must go out, or a
// channel move would be invisible. Same contract the pixel button pins
// for hover.
func TestColorPickerPixelChannelMoveRetransmitsUnderSixel(t *testing.T) {
	c, p, _ := pixelPicker(graphics.Sixel{})
	flush(t, c) // starts focused: the picker is the only focus stop

	p.HandleKey(input.Named(input.KeyDown))
	out := flush(t, c)
	if n := strings.Count(out, "\x1bP0;0;0q"); n != 2 {
		t.Fatalf("a channel move re-sent %d bars under sixel, want 2:\n%q", n, out)
	}
}

// Without a protocol, or without a known cell size, the picker is the
// cell-tier component it always was: zero placements, so a cell-only
// terminal's output is untouched by the pixel code.
func TestColorPickerWithoutGraphicsPlacesNothing(t *testing.T) {
	c, _, _ := pixelPicker(nil)
	f, _ := c.Frame()
	if len(f.Placements()) != 0 {
		t.Fatal("a composition without a protocol recorded placements")
	}

	// A protocol but no cell size: the probe timed out. Fall back too.
	v := prop.NewSource(render.RGB(10, 20, 30))
	p := &ColorPicker{Value: v}
	c2 := gooey.NewComposer(&VStack{Children: []gooey.Component{p}}, 30, 8)
	c2.SetCaps(term.Caps{Cols: 30, Rows: 8, Color: render.TrueColor}) // no CellW/CellH
	c2.SetGraphics(graphics.Kitty{})
	f2, _ := c2.Frame()
	if len(f2.Placements()) != 0 {
		t.Fatal("an unknown cell size recorded placements anyway")
	}
}
