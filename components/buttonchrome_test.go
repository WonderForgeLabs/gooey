package components

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/graphics"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
	"github.com/WonderForgeLabs/gooey/term"
)

// The pixel button is the first component whose CHROME is pixel content,
// so these tests are about the placement lifecycle rather than about
// pixels: does a state change reach the terminal, does an unrelated
// repaint leave the images alone, and does a button that vanishes take
// its images with it. The answers are byte signatures, because that is
// what the terminal actually receives.

// pixelButton is a pixel-chrome button beside a plain text label: two
// paint nodes, one of which owns placements.
func pixelButton(enc graphics.Encoder) (*gooey.Composer, *Button, *prop.Property[string]) {
	label := prop.NewSource("caption")
	b := &Button{Content: Str("Save"), Chrome: ChromePixel, Click: gooey.Command(func() {})}
	b.LayoutProps().HAlign = gooey.AlignStart
	root := &VStack{Children: []gooey.Component{b, &Text{Content: label}}}
	c := gooey.NewComposer(root, 30, 8)
	c.SetCaps(term8x16(30, 8))
	c.SetGraphics(enc)
	return c, b, label
}

// term8x16 is a conventional terminal cell: 8 by 16 pixels. The chrome
// is generated against whatever the terminal reports, so the size has to
// be a parameter of the test rather than a constant of the component.
func term8x16(cols, rows int) term.Caps {
	return term.Caps{Cols: cols, Rows: rows, CellW: 8, CellH: 16, Color: render.TrueColor}
}

func TestPixelButtonPlacesItsChromeAroundTheLabel(t *testing.T) {
	c, b, _ := pixelButton(graphics.Kitty{})
	out := flush(t, c)

	// Four slots: the top edge, the bottom edge, and the two end caps.
	for _, id := range []string{"i=1,", "i=2,", "i=3,", "i=4,"} {
		if !strings.Contains(out, "a=T,f=100,q=2,") || !strings.Contains(out, id) {
			t.Fatalf("the chrome did not transmit slot %s:\n%q", id, out)
		}
	}
	if n := strings.Count(out, "a=T,f=100,"); n != 4 {
		t.Fatalf("the first frame transmitted %d images, want 4", n)
	}

	// The label is on the CELL plane, in the window between the caps —
	// an image spanning the button would bury its own text.
	r := b.Bounds()
	if r.H != pillRows {
		t.Fatalf("a pixel button is %d rows tall, want %d", r.H, pillRows)
	}
	if got := row(c.Cells(), r.Y+1); !strings.Contains(got, "Save") {
		t.Fatalf("the label row is %q, want it to contain the label", got)
	}
	// And the caps' own columns are not written by the cell plane.
	if got := c.Cells().At(r.X, r.Y+1).Rune; got != ' ' {
		t.Fatalf("the left cap cell holds %q; the pixel plane owns it", got)
	}
}

// Hover changes the shading, so the four images are genuinely different
// pixels: the old ones are freed and new ones sent under the same ids.
func TestPixelButtonHoverRetransmitsItsChrome(t *testing.T) {
	c, b, _ := pixelButton(graphics.Kitty{})
	flush(t, c)

	b.SetHovered(true)
	out := flush(t, c)
	if n := strings.Count(out, "a=d,d=I,"); n != 4 {
		t.Fatalf("hover freed %d images, want 4:\n%q", n, out)
	}
	if n := strings.Count(out, "a=T,f=100,"); n != 4 {
		t.Fatalf("hover retransmitted %d images, want 4:\n%q", n, out)
	}
	// The ids are reused: the slots did not come and go, their contents
	// changed.
	if strings.Contains(out, "i=5,") {
		t.Fatalf("hover allocated new placement ids instead of replacing:\n%q", out)
	}
}

// A hover flip must reach the wire under every protocol, not only the
// one with placement identity. Kitty replaces by id; sixel and iTerm2
// have no ids, so the new pixels themselves are the only feedback there
// is — and a replace at the SAME spot vacates no cells and changes no
// cells beneath the edges, which is exactly the case a diff keyed off
// cell damage would lose.
func TestPixelButtonHoverRetransmitsWithoutIDs(t *testing.T) {
	for _, tc := range []struct {
		enc  graphics.Encoder
		mark string
	}{
		{graphics.Sixel{}, "\x1bP0;0;0q"},
		{graphics.ITerm2{}, "\x1b]1337;File="},
	} {
		t.Run(tc.enc.Name(), func(t *testing.T) {
			c, b, _ := pixelButton(tc.enc)
			flush(t, c)

			b.SetHovered(true)
			out := flush(t, c)
			if n := strings.Count(out, tc.mark); n != 4 {
				t.Fatalf("hover re-sent %d images under %s, want 4:\n%q", n, tc.enc.Name(), out)
			}

			b.HandleMouse(input.MouseEvent{Kind: input.MousePress, X: b.Bounds().X, Y: b.Bounds().Y})
			out = flush(t, c)
			if n := strings.Count(out, tc.mark); n != 4 {
				t.Fatalf("press re-sent %d images under %s, want 4:\n%q", n, tc.enc.Name(), out)
			}
		})
	}
}

// The reported bug, end to end: a pixel button on a page that also
// hosts a toast layer (the toolkit shape), driven through the
// DISPATCHER the way a terminal drives it. The empty full-page host
// used to win the hit test, so no mouse event ever reached the button
// and no state change ever reached the wire — under sixel there is no
// other feedback, so the button looked dead.
func TestPixelButtonMouseFeedbackReachesTheWireUnderAToastLayer(t *testing.T) {
	for _, tc := range []struct {
		enc  graphics.Encoder
		mark string
	}{
		{graphics.Sixel{}, "\x1bP0;0;0q"},
		{graphics.ITerm2{}, "\x1b]1337;File="},
	} {
		t.Run(tc.enc.Name(), func(t *testing.T) {
			b := &Button{Content: Str("Save"), Chrome: ChromePixel, Click: gooey.Command(func() {})}
			b.LayoutProps().HAlign = gooey.AlignStart
			page := &Canvas{Children: []gooey.Component{
				&VStack{Children: []gooey.Component{b}},
				&ToastHost{},
			}}
			c := gooey.NewComposer(page, 30, 8)
			c.SetCaps(term8x16(30, 8))
			c.SetGraphics(tc.enc)
			flush(t, c)

			r := b.Bounds()
			c.HandleMouse(input.MouseEvent{Kind: input.MouseMove, X: r.X + 1, Y: r.Y + 1})
			out := flush(t, c)
			if n := strings.Count(out, tc.mark); n != 4 {
				t.Fatalf("hover re-sent %d images under %s, want 4:\n%q", n, tc.enc.Name(), out)
			}
			c.HandleMouse(input.MouseEvent{Kind: input.MousePress, X: r.X + 1, Y: r.Y + 1})
			out = flush(t, c)
			if n := strings.Count(out, tc.mark); n != 4 {
				t.Fatalf("press re-sent %d images under %s, want 4:\n%q", n, tc.enc.Name(), out)
			}
		})
	}
}

// Press and release move through the same path, and releasing returns
// the button to the pixels it had at rest — proving the generated chrome
// is cached by state rather than regenerated per paint.
func TestPixelButtonPressReturnsToItsRestingChrome(t *testing.T) {
	c, b, _ := pixelButton(graphics.Kitty{})
	flush(t, c)
	rest := b.pillFor(pillKey{cols: b.Bounds().W, rows: pillRows, cellW: 8, cellH: 16})

	b.HandleMouse(input.MouseEvent{Kind: input.MousePress, X: b.Bounds().X, Y: b.Bounds().Y})
	if out := flush(t, c); !strings.Contains(out, "a=T,f=100,") {
		t.Fatalf("pressing did not change the chrome:\n%q", out)
	}
	b.HandleMouse(input.MouseEvent{Kind: input.MouseRelease, X: b.Bounds().X, Y: b.Bounds().Y})
	flush(t, c)

	again := b.pillFor(pillKey{cols: b.Bounds().W, rows: pillRows, cellW: 8, cellH: 16})
	if again.top != rest.top || again.left != rest.left {
		t.Fatal("returning to rest regenerated the chrome instead of reusing it")
	}
}

// A neighbour repainting must not touch the button's images. This is the
// per-node ownership guarantee, stated in bytes.
func TestPixelButtonChromeSurvivesANeighbourRepaint(t *testing.T) {
	c, _, label := pixelButton(graphics.Kitty{})
	flush(t, c)

	label.Set("a different caption")
	out := flush(t, c)
	if strings.Contains(out, "\x1b_G") {
		t.Fatalf("an unrelated repaint disturbed the chrome:\n%q", out)
	}
}

// A vanished button takes its images with it: deleted by id under kitty,
// erased by repainting the cells they covered under sixel.
func TestPixelButtonChromeGoesWhenTheButtonHides(t *testing.T) {
	c, b, _ := pixelButton(graphics.Kitty{})
	flush(t, c)

	b.LayoutProps().Visibility = gooey.Hidden
	out := flush(t, c)
	if n := strings.Count(out, "a=d,d=I,"); n != 4 {
		t.Fatalf("hiding deleted %d images, want 4:\n%q", n, out)
	}
}

func TestPixelButtonChromeIsErasedByCellsWithoutIDs(t *testing.T) {
	c, b, _ := pixelButton(graphics.Sixel{})
	flush(t, c)

	r := b.Bounds()
	b.LayoutProps().Visibility = gooey.Hidden
	out := flush(t, c)
	if strings.Contains(out, "\x1b_G") {
		t.Fatalf("sixel emitted a kitty delete:\n%q", out)
	}
	// The three rows the pill covered are addressed and repainted.
	for y := r.Y; y < r.Y+pillRows; y++ {
		want := fmt.Sprintf("\x1b[%d;%dH", y+1, r.X+1)
		if !strings.Contains(out, want) {
			t.Fatalf("the cells vacated at row %d were not repainted:\n%q", y, out)
		}
	}
}

// Without a protocol the button is the same three-row pill drawn in box
// runes — the universal tier, not a degraded one. It places nothing, and
// the terminal model proves the cells are what the buffer says.
func TestPixelButtonFallsBackToBoxRunes(t *testing.T) {
	c, b, _ := pixelButton(nil)
	var buf bytes.Buffer
	f, _ := c.Frame()
	if err := c.Flush(&buf); err != nil {
		t.Fatal(err)
	}
	if len(f.Placements()) != 0 {
		t.Fatal("the halfblock tier recorded pixel placements")
	}
	r := b.Bounds()
	if got := c.Cells().At(r.X, r.Y).Rune; got != '╭' {
		t.Fatalf("the fallback's top-left corner is %q, want ╭", got)
	}
	if got := row(c.Cells(), r.Y+1); !strings.Contains(got, "Save") {
		t.Fatalf("the fallback label row is %q", got)
	}

	sc := render.NewScreen(30, 8)
	sc.Write(buf.Bytes())
	for y := 0; y < 8; y++ {
		for x := 0; x < 30; x++ {
			if sc.Buf.At(x, y) != c.Cells().At(x, y) {
				t.Fatalf("the terminal differs from the buffer at %d,%d", x, y)
			}
		}
	}
}

// A known cell size is part of the capability, not a nicety: a probe
// that timed out leaves CellW at zero, and a pill generated against that
// would be zero pixels wide. The button falls back rather than placing
// nothing visible.
func TestPixelButtonFallsBackWhenTheCellSizeIsUnknown(t *testing.T) {
	b := &Button{Content: Str("Save"), Chrome: ChromePixel, Click: gooey.Command(func() {})}
	b.LayoutProps().HAlign = gooey.AlignStart
	c := gooey.NewComposer(&VStack{Children: []gooey.Component{b}}, 30, 8)
	c.SetCaps(term.Caps{Cols: 30, Rows: 8, Color: render.TrueColor}) // no CellW/CellH
	c.SetGraphics(graphics.Kitty{})
	out := flush(t, c)
	if strings.Contains(out, "\x1b_G") {
		t.Fatalf("a button with no known cell size placed an image anyway:\n%q", out)
	}
	if got := c.Cells().At(b.Bounds().X, b.Bounds().Y).Rune; got != '╭' {
		t.Fatalf("it did not fall back to box runes (top-left is %q)", got)
	}
}

// A pixel button is a Button: same keys, same conditional command, same
// disabled rule. Only the chrome is different.
func TestPixelButtonKeepsButtonSemantics(t *testing.T) {
	dirty := prop.NewSource(false)
	ran := 0
	b := &Button{
		Content: Str("Save"),
		Chrome:  ChromePixel,
		Click:   gooey.NewCommand(func() { ran++ }).When(dirty),
	}
	b.LayoutProps().HAlign = gooey.AlignStart
	c := gooey.NewComposer(&VStack{Children: []gooey.Component{b, &Text{Content: Str("x")}}}, 30, 8)
	c.SetCaps(term8x16(30, 8))
	c.SetGraphics(graphics.Kitty{})
	c.Frame()

	if b.HandleKey(input.Named(input.KeyEnter)) || ran != 0 {
		t.Fatal("a disabled pixel button ran its command")
	}
	dirty.Set(true)
	if _, painted := c.Frame(); painted != 1 {
		t.Fatalf("enabling painted %d components, want 1", painted)
	}
	if !b.HandleKey(input.Named(input.KeyEnter)) || ran != 1 {
		t.Fatalf("an enabled pixel button did not run its command (ran=%d)", ran)
	}
}

func TestParseButtonChromeRejectsUnknownNames(t *testing.T) {
	for _, name := range append([]string{""}, ButtonChromeNames...) {
		if _, ok := ParseButtonChrome(name); !ok {
			t.Fatalf("chrome %q did not resolve", name)
		}
	}
	if _, ok := ParseButtonChrome("neon"); ok {
		t.Fatal("an unknown chrome resolved instead of failing")
	}
}
