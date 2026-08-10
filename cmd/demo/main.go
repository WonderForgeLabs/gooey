// demo is the pixel-plane demo: an image in a retained, damage-tracked
// tree, drawn through whichever graphics protocol the terminal speaks.
//
//	demo                 detect the protocol, run interactively
//	demo --mode=sixel    force one (kitty|sixel|iterm2|halfblock)
//	demo --dump          render one frame to stdout, no tty control
//	demo --hold=2s       exit after a while instead of on a key
//
// It runs on the App/Composer path, which is the point of it. Pixel
// content used to work only through the one-shot Compose path — every
// frame retransmitted every image — so an interactive app got halfblock
// and nothing else. Now placements are owned by the paint node that
// recorded them and diffed like cells: press space and only the picture
// changes hands, press + and a protocol with placement identity moves it
// without re-sending a byte of PNG.
//
// The footer reports the previous frame's damage in both currencies:
// components repainted, and bytes actually written to the terminal.
package main

import (
	"context"
	"flag"
	"fmt"
	"image"
	"image/color"
	"math"
	"os"
	"strings"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/graphics"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
	"github.com/WonderForgeLabs/gooey/term"
)

func main() {
	mode := flag.String("mode", "", "force graphics mode: kitty|sixel|iterm2|halfblock")
	dump := flag.Bool("dump", false, "render one frame to stdout (no tty control)")
	hold := flag.Duration("hold", 0, "exit after this duration instead of waiting for a quit key")
	flag.Parse()

	enc, forced, err := encoderFor(*mode)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	vm := newModel()
	if *dump {
		// The one-shot path: compose once, write everything, no damage
		// tracking and no terminal to give back.
		caps := term.Caps{Cols: 80, Rows: 24, CellW: 10, CellH: 20, Color: render.TrueColor}
		gooey.Compose(vm.tree(), caps, enc).Flush(os.Stdout)
		fmt.Println()
		return
	}

	opts := []gooey.Option{gooey.WithoutMouse()}
	if forced {
		// A forced protocol still needs a cell size, which only a probe
		// can know; sixel scales by it, so assume a common 10×20.
		opts = append(opts,
			gooey.WithGraphics(enc),
			gooey.WithCaps(term.Caps{CellW: 10, CellH: 20, Color: term.DetectColorDepth()}))
	} else {
		opts = append(opts, gooey.WithCapabilityProbe())
	}

	app := gooey.NewApp(gooey.Tree(vm.tree()), opts...)
	vm.bind(app)
	app.BeforeFrame(vm.refreshStats)
	if *hold > 0 {
		app.Every(*hold, app.Quit)
	}
	gooey.Exit(app.Run(context.Background()))
}

func encoderFor(mode string) (enc graphics.Encoder, forced bool, err error) {
	switch mode {
	case "":
		return nil, false, nil // capabilities decide
	case "kitty":
		return graphics.Kitty{}, true, nil
	case "sixel":
		return graphics.Sixel{}, true, nil
	case "iterm2":
		return graphics.ITerm2{}, true, nil
	case "halfblock":
		return nil, true, nil
	default:
		return nil, false, fmt.Errorf("unknown mode: %s", mode)
	}
}

// model is the viewmodel. Everything the keys change is a property, so
// each gesture dirties exactly the components that read it — the image
// for space and +/-, the spacer for the shifts, the footer for the stats.
type model struct {
	phase   *prop.Property[int]
	size    *prop.Property[int]
	offset  *prop.Property[int]
	img     *prop.Property[image.Image]
	pad     *prop.Property[string]
	stats   *prop.Property[string]
	mode    *prop.Property[string]
	picture *components.Image
	app     *gooey.App
}

func newModel() *model {
	m := &model{
		phase:  prop.NewSource(0),
		size:   prop.NewSource(12),
		offset: prop.NewSource(0),
		stats:  prop.NewSource(""),
		mode:   prop.NewSource("detecting…"),
	}
	// The image is COMPUTED from the phase, so pressing space produces a
	// genuinely different image.Image — which is what the placement diff
	// keys "replace" off, as opposed to the same picture in a new spot.
	m.img = prop.NewComputed(func() image.Image { return logo(m.phase.Get()) })
	m.pad = prop.NewComputed(func() string { return strings.Repeat(" ", m.offset.Get()) })
	return m
}

func (m *model) tree() gooey.Component {
	accent := render.Style{Fg: render.RGB(255, 170, 60), Bold: true}
	dim := render.Style{Fg: render.RGB(140, 140, 150)}

	m.picture = &components.Image{
		Src:  m.img,
		Cols: prop.NewComputed(func() int { return m.size.Get() * 2 }),
		Rows: m.size,
	}

	info := &components.VStack{Children: []gooey.Component{
		&components.Text{Content: components.Str("gooey — the pixel plane, damage-tracked"), Style: components.Sty(accent)},
		&components.Text{},
		&components.Text{Content: m.mode, Style: components.Sty(render.Style{Bold: true})},
		&components.Text{},
		&components.Text{Content: components.Str("space  new image      (replace)"), Style: components.Sty(dim)},
		&components.Text{Content: components.Str("+ -    resize         (move)"), Style: components.Sty(dim)},
		&components.Text{Content: components.Str("[ ]    shift          (move)"), Style: components.Sty(dim)},
		&components.Text{Content: components.Str("h      hide / show    (remove)"), Style: components.Sty(dim)},
		&components.Text{Content: components.Str("q      quit"), Style: components.Sty(dim)},
	}}

	body := &components.VStack{Gap: 1, Children: []gooey.Component{
		&components.HStack{Gap: 2, Children: []gooey.Component{
			&components.Text{Content: m.pad},
			m.picture,
			info,
		}},
		&components.Text{Content: m.stats, Style: components.Sty(dim)},
	}}

	root := &components.Border{
		Child: body,
		Title: components.Str("gooey"),
		Style: components.Sty(render.Style{Fg: render.RGB(120, 90, 220)}),
	}
	// Bindings hang off the ROOT: this page has no focus stop, and
	// dispatch starts at the focused component or, failing that, here.
	bind := func(gesture input.KeyEvent, fn func()) {
		root.Attach(&gooey.KeyBinding{Gesture: gesture, Command: fn})
	}
	bind(input.KeyEvent{Key: input.KeyRune, Rune: ' '}, func() { m.phase.Set(m.phase.Get() + 1) })
	bind(input.KeyEvent{Key: input.KeyRune, Rune: '+'}, func() { m.resize(2) })
	bind(input.KeyEvent{Key: input.KeyRune, Rune: '='}, func() { m.resize(2) })
	bind(input.KeyEvent{Key: input.KeyRune, Rune: '-'}, func() { m.resize(-2) })
	bind(input.KeyEvent{Key: input.KeyRune, Rune: ']'}, func() { m.shift(2) })
	bind(input.KeyEvent{Key: input.KeyRune, Rune: '['}, func() { m.shift(-2) })
	bind(input.KeyEvent{Key: input.KeyRune, Rune: 'h'}, m.toggle)
	bind(input.KeyEvent{Key: input.KeyRune, Rune: 'q'}, func() { m.app.Quit() })
	return root
}

func (m *model) bind(a *gooey.App) { m.app = a }

func (m *model) resize(d int) { m.size.Set(clamp(m.size.Get()+d, 4, 20)) }
func (m *model) shift(d int)  { m.offset.Set(clamp(m.offset.Get()+d, 0, 20)) }

// toggle flips the image's visibility. Visibility is a plain field rather
// than a property — the Composer notices the change during its own sweep
// — so nothing schedules the frame that would notice it, and the app has
// to ask for one.
func (m *model) toggle() {
	l := m.picture.LayoutProps()
	if l.Visibility == gooey.Visible {
		l.Visibility = gooey.Hidden
	} else {
		l.Visibility = gooey.Visible
	}
	m.app.Invalidate()
}

// refreshStats runs before each frame, reporting the PREVIOUS one — the
// byte count is only known once the frame has been written. Setting the
// properties here folds their repaint into the frame about to happen.
//
// Both Sets are guarded by a comparison. Set is unconditional: it dirties
// its dependents whether or not the value changed, so an unguarded
// per-frame Set would repaint the footer forever.
func (m *model) refreshStats() {
	c := m.app.Composer()
	if c == nil {
		return
	}
	name := "halfblock"
	if enc := c.Graphics(); enc != nil {
		name = enc.Name()
	}
	caps := c.Caps()
	mode := fmt.Sprintf("protocol: %s   cell %d×%d px   color %s",
		name, caps.CellW, caps.CellH, caps.Color)
	if mode != m.mode.Get() {
		m.mode.Set(mode)
	}
	stats := fmt.Sprintf("last frame: %d component(s) repainted, %d bytes written   ·   %d frames",
		m.app.PaintedLastFrame(), m.app.FlushBytes(), m.app.Frames())
	if stats != m.stats.Get() {
		m.stats.Set(stats)
	}
}

func clamp(v, lo, hi int) int {
	return max(lo, min(hi, v))
}

// logo renders a plasma-ish gradient with a ring, phase-shifted — enough
// color variety to show quantization differences between the protocols,
// and enough motion to make a replaced placement obvious.
func logo(phase int) image.Image {
	const w, h = 240, 240
	p := float64(phase) * 0.7
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			fx, fy := float64(x)/w, float64(y)/h
			r := uint8(90 + 120*math.Sin(fx*6+p)*math.Cos(fy*4+p) + 45)
			g := uint8(60 + 160*fy)
			b := uint8(200 - 120*fx)
			dx, dy := fx-0.5, fy-0.5
			dist := math.Sqrt(dx*dx + dy*dy)
			if dist > 0.30 && dist < 0.38 {
				r, g, b = 255, 220, 120
			}
			img.Set(x, y, color.RGBA{r, g, b, 255})
		}
	}
	return img
}
