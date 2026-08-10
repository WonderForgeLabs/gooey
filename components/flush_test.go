package components

import (
	"bytes"
	"image"
	"image/color"
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/graphics"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
	"github.com/WonderForgeLabs/gooey/term"
)

// flush composes and writes one frame, reporting what went on the wire.
// Composing and flushing together is the pairing the run loop uses, and
// the byte count is only meaningful for the frame that produced it.
func flush(t *testing.T, c *gooey.Composer) string {
	t.Helper()
	var buf bytes.Buffer
	c.Frame()
	if err := c.Flush(&buf); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != c.FlushBytes() {
		t.Fatalf("FlushBytes says %d, %d bytes were written", c.FlushBytes(), buf.Len())
	}
	return buf.String()
}

// ---- the cell plane ----

func TestFlushSendsOnlyTheRowThatChanged(t *testing.T) {
	lines := make([]gooey.Component, 20)
	props := make([]*prop.Property[string], 20)
	for i := range lines {
		props[i] = prop.NewSource(strings.Repeat("x", 60))
		lines[i] = &Text{Content: props[i]}
	}
	c := gooey.NewComposer(&VStack{Children: lines}, 80, 24)

	full := len(flush(t, c))
	if full < 80*24 {
		t.Fatalf("the first frame wrote %d bytes, want a full screen", full)
	}

	props[7].Set(strings.Repeat("y", 60))
	one := len(flush(t, c))
	if one > 2*80 {
		t.Fatalf("a one-row change wrote %d bytes, want at most %d", one, 2*80)
	}
	if one*10 > full {
		t.Fatalf("a one-row change wrote %d bytes against a full frame's %d: not an order of magnitude", one, full)
	}
}

func TestFlushWritesNothingWhenNothingChanged(t *testing.T) {
	c := gooey.NewComposer(&VStack{Children: []gooey.Component{
		&Text{Content: Str("hello")},
	}}, 40, 10)
	flush(t, c)

	// Not even the synchronized-output bracket: an idle app costs zero.
	if out := flush(t, c); out != "" {
		t.Fatalf("a clean frame wrote %q, want nothing", out)
	}
}

func TestInvalidateForcesAWholeScreen(t *testing.T) {
	c := gooey.NewComposer(&Text{Content: Str("hello")}, 40, 10)
	flush(t, c)
	if out := flush(t, c); out != "" {
		t.Fatalf("expected a clean frame, wrote %q", out)
	}

	// Nothing repaints — the buffer was right all along. What was wrong
	// is the flush's belief about the terminal.
	c.Invalidate()
	var buf bytes.Buffer
	_, painted := c.Frame()
	if err := c.Flush(&buf); err != nil {
		t.Fatal(err)
	}
	if painted != 0 {
		t.Fatalf("Invalidate repainted %d components, want 0", painted)
	}
	if !strings.Contains(buf.String(), "hello") {
		t.Fatalf("the forced frame did not re-send the screen: %q", buf.String())
	}
}

func TestResizeIsAFullFrame(t *testing.T) {
	c := gooey.NewComposer(&Text{Content: Str("hello")}, 40, 10)
	flush(t, c)
	c.Resize(60, 20)
	out := flush(t, c)
	if n := strings.Count(out, "\r\n"); n != 19 {
		t.Fatalf("the frame after a resize has %d line breaks, want 19", n)
	}
}

// The terminal model is the audit: replay what the flush sent and the
// screen must equal the buffer, edit after edit.
func TestIncrementalFlushesReproduceTheBuffer(t *testing.T) {
	a, b, title := prop.NewSource("alpha"), prop.NewSource(3), prop.NewSource("one")
	tree := &Border{Title: title, Child: &VStack{Gap: 1, Children: []gooey.Component{
		&Text{Content: a},
		&Gauge{Value: b, Label: Str("cpu ")},
		&Checkbox{Label: Str("ready")},
	}}}
	c := gooey.NewComposer(tree, 40, 12)
	sc := render.NewScreen(40, 12)

	steps := []func(){
		func() { a.Set("beta") },
		func() { b.Set(80) },
		func() { title.Set("a much longer title") },
		func() { a.Set("gamma delta epsilon") },
		func() { b.Set(0) },
		func() { title.Set("two") },
	}
	for i := -1; i < len(steps); i++ {
		if i >= 0 {
			steps[i]()
		}
		var buf bytes.Buffer
		c.Frame()
		if err := c.Flush(&buf); err != nil {
			t.Fatal(err)
		}
		sc.Write(buf.Bytes())
		for y := 0; y < 12; y++ {
			for x := 0; x < 40; x++ {
				if sc.Buf.At(x, y) != c.Cells().At(x, y) {
					t.Fatalf("step %d: terminal differs from buffer at %d,%d", i, x, y)
				}
			}
		}
	}
}

// ---- the pixel plane ----

func swatch(shade uint8) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			img.Set(x, y, color.RGBA{shade, uint8(x * 60), uint8(y * 60), 255})
		}
	}
	return img
}

// pixelTree is an image beside a text label: two paint nodes, one of
// which owns a placement, so "did the other one's repaint disturb the
// image" is a question the tests can ask.
func pixelTree(enc graphics.Encoder) (*gooey.Composer, *Image, *prop.Property[string], *prop.Property[image.Image]) {
	src := prop.NewSource(swatch(200))
	label := prop.NewSource("caption")
	im := &Image{Src: src, Cols: Cells(6), Rows: Cells(3)}
	// An image must not be stretched to the stack's width: it asks for a
	// cell rectangle and means it.
	im.LayoutProps().HAlign = gooey.AlignStart
	root := &VStack{Children: []gooey.Component{im, &Text{Content: label}}}
	c := gooey.NewComposer(root, 30, 8)
	c.SetCaps(term.Caps{Cols: 30, Rows: 8, CellW: 2, CellH: 2, Color: render.TrueColor})
	c.SetGraphics(enc)
	return c, im, label, src
}

func TestKittyPlacementIsTransmittedOnceAndLeftAlone(t *testing.T) {
	c, _, label, _ := pixelTree(graphics.Kitty{})

	first := flush(t, c)
	if !strings.Contains(first, "\x1b_Ga=T,f=100,q=2,c=6,r=3,i=1,") {
		t.Fatalf("the first frame did not transmit the image under an id:\n%q", first)
	}
	if !strings.Contains(first, "\x1b[1;1H") {
		t.Fatal("the image was not positioned at its bounds")
	}

	// A neighbour repaints. The image did not change, so not one byte of
	// it goes back on the wire — that is the whole point of owning
	// placements per paint node.
	label.Set("a different caption")
	second := flush(t, c)
	if strings.Contains(second, "\x1b_G") {
		t.Fatalf("an unrelated repaint re-sent the image:\n%q", second)
	}
	if len(second) > 80 {
		t.Fatalf("a caption change wrote %d bytes", len(second))
	}
}

func TestKittyPlacementReplacedWhenTheImageChanges(t *testing.T) {
	c, _, _, src := pixelTree(graphics.Kitty{})
	flush(t, c)

	src.Set(swatch(40))
	out := flush(t, c)
	// The pixels are genuinely different, so the old ones are freed
	// (uppercase I) and new ones sent under the same id.
	if !strings.Contains(out, "\x1b_Ga=d,d=I,i=1,q=2\x1b\\") {
		t.Fatalf("a changed image did not delete the old data:\n%q", out)
	}
	if !strings.Contains(out, "a=T,f=100,q=2,c=6,r=3,i=1,") {
		t.Fatalf("a changed image was not retransmitted:\n%q", out)
	}
}

func TestKittyPlacementMovesWithoutRetransmitting(t *testing.T) {
	c, im, _, _ := pixelTree(graphics.Kitty{})
	first := flush(t, c)

	// Same image, new rectangle. Kitty keeps the pixels; only the
	// placement moves.
	im.Rows.Set(4)
	out := flush(t, c)
	if !strings.Contains(out, "\x1b_Ga=d,d=i,i=1,q=2\x1b\\") {
		t.Fatalf("the move did not clear the old placement (and must not free the data):\n%q", out)
	}
	if strings.Contains(out, "d=I") {
		t.Fatalf("a move freed the image data:\n%q", out)
	}
	if !strings.Contains(out, "\x1b_Ga=p,i=1,c=6,r=4,q=2\x1b\\") {
		t.Fatalf("the move did not re-place the stored image:\n%q", out)
	}
	if strings.Contains(out, "a=T") {
		t.Fatalf("a move retransmitted the image:\n%q", out)
	}
	if len(out) > len(first)/4 {
		t.Fatalf("a move wrote %d bytes against a transmission's %d", len(out), len(first))
	}
}

func TestKittyPlacementDeletedWhenTheComponentHides(t *testing.T) {
	c, im, _, _ := pixelTree(graphics.Kitty{})
	flush(t, c)

	im.LayoutProps().Visibility = gooey.Hidden
	out := flush(t, c)
	if !strings.Contains(out, "\x1b_Ga=d,d=I,i=1,q=2\x1b\\") {
		t.Fatalf("hiding the component did not delete its image:\n%q", out)
	}

	// And back: a new id, because the old image was freed with it.
	im.LayoutProps().Visibility = gooey.Visible
	out = flush(t, c)
	if !strings.Contains(out, "a=T,f=100,q=2,c=6,r=3,i=2,") {
		t.Fatalf("showing it again did not transmit a fresh image:\n%q", out)
	}
}

// Sixel and iTerm2 write pixels into the cell grid and cannot address
// them afterwards, so the two rules invert: a vanished image is erased by
// repainting the cells it covered, and a surviving image has to be
// re-sent whenever the cell flush writes over it.
func TestSixelPlacementIsErasedByRepaintingItsCells(t *testing.T) {
	c, im, _, _ := pixelTree(graphics.Sixel{})
	flush(t, c)

	im.LayoutProps().Visibility = gooey.Hidden
	out := flush(t, c)
	if strings.Contains(out, "\x1b_G") {
		t.Fatalf("sixel emitted a kitty delete:\n%q", out)
	}
	// Three rows of six cells, addressed and blanked.
	for _, want := range []string{"\x1b[1;1H", "\x1b[2;1H", "\x1b[3;1H"} {
		if !strings.Contains(out, want) {
			t.Fatalf("the vacated cells at %q were not repainted:\n%q", want, out)
		}
	}
}

func TestSixelPlacementIsReSentWhenCellsAreWrittenOverIt(t *testing.T) {
	src := prop.NewSource(swatch(200))
	over := prop.NewSource("")
	// A Canvas puts the text ON TOP of the image's cells, which for a
	// protocol living in the cell grid means writing that text erases
	// part of the picture.
	im := &Image{Src: src, Cols: Cells(8), Rows: Cells(4)}
	im.LayoutProps().HAlign = gooey.AlignStart
	txt := &Text{Content: over}
	txt.LayoutProps().Left, txt.LayoutProps().Top = 2, 1
	c := gooey.NewComposer(&Canvas{Children: []gooey.Component{im, txt}}, 30, 8)
	c.SetCaps(term.Caps{Cols: 30, Rows: 8, CellW: 2, CellH: 2, Color: render.TrueColor})
	c.SetGraphics(graphics.Sixel{})
	flush(t, c)

	over.Set("hi")
	out := flush(t, c)
	if !strings.Contains(out, "\x1bP0;0;0q") {
		t.Fatalf("cells written over a sixel image did not re-send it:\n%q", out)
	}

	// Text somewhere the image is not leaves it alone.
	txt.LayoutProps().Top = 6
	c.Frame()
	var discard bytes.Buffer
	c.Flush(&discard)
	over.Set("bye")
	out = flush(t, c)
	if strings.Contains(out, "\x1bP0;0;0q") {
		t.Fatalf("a change away from the image re-sent it anyway:\n%q", out)
	}
}

func TestPlacementsAreReSentAfterAnInvalidate(t *testing.T) {
	for _, enc := range []graphics.Encoder{graphics.Kitty{}, graphics.Sixel{}, graphics.ITerm2{}} {
		t.Run(enc.Name(), func(t *testing.T) {
			c, _, _, _ := pixelTree(enc)
			flush(t, c)
			if out := flush(t, c); out != "" {
				t.Fatalf("a clean frame wrote %q", out)
			}
			// The alternate screen came back blank; the images went with it.
			c.Invalidate()
			out := flush(t, c)
			if !strings.Contains(out, pixelSignature(enc)) {
				t.Fatalf("%s did not re-send its image after an invalidate:\n%q", enc.Name(), out)
			}
		})
	}
}

func pixelSignature(enc graphics.Encoder) string {
	switch enc.Name() {
	case "kitty":
		return "\x1b_G"
	case "sixel":
		return "\x1bP0;0;0q"
	default:
		return "\x1b]1337;File="
	}
}

// A component that leaves the tree in a Dynamic re-sync takes its images
// with it: nothing else will ever repaint over a plane the cell buffer
// does not describe.
func TestPlacementRemovedWhenTheComponentLeavesTheTree(t *testing.T) {
	src := prop.NewSource(swatch(200))
	box := &pixelBox{kids: []gooey.Component{
		gooey.L(&Image{Src: src, Cols: Cells(4), Rows: Cells(2)}, gooey.Layout{HAlign: gooey.AlignStart}),
		&Text{Content: Str("stays")},
	}}
	c := gooey.NewComposer(box, 30, 8)
	c.SetCaps(term.Caps{Cols: 30, Rows: 8, CellW: 2, CellH: 2, Color: render.TrueColor})
	c.SetGraphics(graphics.Kitty{})
	flush(t, c)

	box.drop()
	out := flush(t, c)
	if !strings.Contains(out, "\x1b_Ga=d,d=I,i=1,q=2\x1b\\") {
		t.Fatalf("the departed component's image was not deleted:\n%q", out)
	}
}

// pixelBox is the smallest Dynamic container: it stacks its children one
// row each and can drop the first.
type pixelBox struct {
	gooey.Base
	kids []gooey.Component
	hook func()
}

func (b *pixelBox) SetStructureHook(fn func())         { b.hook = fn }
func (b *pixelBox) ChildComponents() []gooey.Component { return b.kids }
func (b *pixelBox) Render(*gooey.Frame)                {}

func (b *pixelBox) drop() {
	b.kids = b.kids[1:]
	if b.hook != nil {
		b.hook()
	}
}

func (b *pixelBox) Measure(avail gooey.Size) gooey.Size {
	for _, k := range b.kids {
		gooey.MeasureChild(k, avail)
	}
	return avail
}

func (b *pixelBox) Arrange(r gooey.Rect) {
	b.Base.Arrange(r)
	y := r.Y
	for _, k := range b.kids {
		h := 2
		gooey.ArrangeChild(k, gooey.Rect{X: r.X, Y: y, W: r.W, H: h})
		y += h
	}
}

// Without a protocol, pixel content becomes cells and there is no pixel
// plane at all — the fallback the whole graphics story rests on.
func TestHalfblockFallbackIsUnchanged(t *testing.T) {
	c, _, _, _ := pixelTree(nil)
	out := flush(t, c)
	if strings.Contains(out, "\x1b_G") || strings.Contains(out, "\x1bP") {
		t.Fatalf("the halfblock path emitted a graphics protocol:\n%q", out)
	}
	if !strings.Contains(out, "▀") {
		t.Fatalf("the image did not degrade into halfblock cells:\n%q", out)
	}
	if got := c.Cells().At(0, 0).Rune; got != '▀' {
		t.Fatalf("cell 0,0 is %q, want the halfblock rune", got)
	}
}

// The damage contract for images, pinned: a new Src repaints the Image
// and only the Image. This is the number the whole property conversion
// of Image (docs/specs/2026-08-10-rendering-2.md, deviation 2) exists
// to make true, and it is what makes file-loaded images (markup Src)
// exactly as cheap as any other property change.
func TestSettingSrcRepaintsExactlyTheImage(t *testing.T) {
	c, _, _, src := pixelTree(graphics.Kitty{})
	flush(t, c)

	src.Set(swatch(90))
	_, painted := c.Frame()
	if painted != 1 {
		t.Fatalf("a Src change repainted %d components, want exactly the Image", painted)
	}
}
