// pixels is the pixel-plane demo: an image in a retained, damage-tracked
// tree, drawn through whichever graphics protocol the terminal speaks.
//
//	pixels                 detect the protocol, run interactively
//	pixels --mode=sixel    force one (kitty|sixel|iterm2|halfblock)
//	pixels --dump          render one frame to stdout, no tty control
//	pixels --hold=2s       exit after a while instead of on a key
//
// It runs on the App/Composer path, which is the point of it. Pixel
// content used to work only through the one-shot Compose path — every
// frame retransmitted every image — so an interactive app got halfblock
// and nothing else. Now placements are owned by the paint node that
// recorded them and diffed like cells: press space and only the picture
// changes hands, press + and a protocol with placement identity moves it
// without re-sending a byte of PNG.
//
// The shell is markup (pixels.gooey) and hot-reloads: the layout, the
// help text, the styles and every key gesture live there, and this file
// is left holding the image itself, the properties the gestures move,
// and the footer's arithmetic. The protocol is a page-level declaration
// too — <Gooey Graphics="sixel"> beats --mode — because which pixels a
// page needs is a fact about its artwork rather than about the machine.
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
	"github.com/WonderForgeLabs/gooey/cmd/internal/demomain"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
	"github.com/WonderForgeLabs/gooey/term"
)

const pageFile = "pixels.gooey"

func main() {
	mode := flag.String("mode", "", "force graphics mode: kitty|sixel|iterm2|halfblock")
	dump := flag.Bool("dump", false, "render one frame to stdout (no tty control)")
	hold := flag.Duration("hold", 0, "exit after this duration instead of waiting for a quit key")
	flag.Parse()

	dir := demomain.MarkupFS("pixels", pageFile)

	// The page settles the protocol if it has an opinion; --mode is the
	// fallback for a page that says nothing, which is this one. Settings
	// are read BEFORE the App is built because pinning a protocol is a
	// construction option, not something a component can be asked for —
	// and for the same reason a hot reload cannot change it.
	settings, err := markup.ReadPageSettings(dir, pageFile)
	if err != nil {
		gooey.Exit(err)
	}
	want := settings.Graphics
	if want == "" {
		want = *mode
	}
	enc, forced, err := demomain.EncoderFor(want)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	vm := newModel()
	ctx := vm.ctx()
	if *dump {
		// The one-shot path: compose once, write everything, no damage
		// tracking and no terminal to give back.
		root, err := markup.Load(dir, pageFile, ctx)
		if err != nil {
			gooey.Exit(err)
		}
		caps := term.Caps{Cols: 80, Rows: 24, CellW: term.DefaultCellW, CellH: term.DefaultCellH, Color: render.TrueColor}
		gooey.Compose(root, caps, enc).Flush(os.Stdout)
		fmt.Println()
		return
	}

	// Probe, or pin the protocol along with the cell size a pinned one
	// still needs — sixel scales by it, and a zero CellW emits a
	// well-formed image of no pixels. That pair is identical in every
	// demo with a --mode flag, so it lives in demomain; the only thing
	// this demo adds is WithoutMouse.
	opts := append([]gooey.Option{gooey.WithoutMouse()}, demomain.GraphicsOptions(enc, forced)...)

	app := gooey.NewApp(markup.Page(dir, pageFile, ctx), opts...)
	vm.bind(app)
	app.BeforeFrame(vm.refreshStats)
	if *hold > 0 {
		app.Every(*hold, app.Quit)
	}
	gooey.Exit(app.Run(context.Background()))
}

// model is the viewmodel. Everything the keys change is a property, so
// each gesture dirties exactly the components that read it — the image
// for space and +/-, the spacer for the shifts, the footer for the stats.
type model struct {
	phase  *prop.Property[int]
	size   *prop.Property[int]
	offset *prop.Property[int]
	img    *prop.Property[image.Image]
	cols   *prop.Property[int]
	pad    *prop.Property[string]
	stats  *prop.Property[string]
	mode   *prop.Property[string]
	shown  *prop.Property[gooey.Visibility]
	app    *gooey.App
}

func newModel() *model {
	m := &model{
		phase:  prop.NewSource(0),
		size:   prop.NewSource(12),
		offset: prop.NewSource(0),
		stats:  prop.NewSource(""),
		mode:   prop.NewSource("detecting…"),
		shown:  prop.NewSource(gooey.Visible),
	}
	// The image is COMPUTED from the phase, so pressing space produces a
	// genuinely different image.Image — which is what the placement diff
	// keys "replace" off, as opposed to the same picture in a new spot.
	m.img = prop.NewComputed(func() image.Image { return logo(m.phase.Get()) })
	m.cols = prop.NewComputed(func() int { return m.size.Get() * 2 })
	m.pad = prop.NewComputed(func() string { return strings.Repeat(" ", m.offset.Get()) })
	return m
}

// ctx is the page's whole surface: the handles its bindings resolve to,
// the commands its <KeyBinding>s name, and the styles it asks for by
// name. Everything that used to be a Go literal in a component tree.
func (m *model) ctx() *markup.Context {
	return &markup.Context{
		Values: map[string]any{
			"Picture": m.img,
			"Cols":    m.cols,
			"Rows":    m.size,
			"Pad":     m.pad,
			"Mode":    m.mode,
			"Stats":   m.stats,
			"Shown":   m.shown,

			"NewImage":    gooey.Command(func() { m.phase.Set(m.phase.Get() + 1) }),
			"Bigger":      gooey.Command(func() { m.resize(2) }),
			"Smaller":     gooey.Command(func() { m.resize(-2) }),
			"ShiftRight":  gooey.Command(func() { m.shift(2) }),
			"ShiftLeft":   gooey.Command(func() { m.shift(-2) }),
			"ToggleImage": gooey.Command(m.toggle),
			"Quit":        gooey.Command(func() { m.app.Quit() }),
		},
		Styles: map[string]render.Style{
			"panel":  {Fg: render.RGB(120, 90, 220)},
			"accent": {Fg: render.RGB(255, 170, 60), Bold: true},
			"dim":    {Fg: render.RGB(140, 140, 150)},
		},
	}
}

func (m *model) bind(a *gooey.App) { m.app = a }

func (m *model) resize(d int) { m.size.Set(clamp(m.size.Get()+d, 4, 20)) }
func (m *model) shift(d int)  { m.offset.Set(clamp(m.offset.Get()+d, 0, 20)) }

// toggle flips the image's visibility. Visibility is a bound PROPERTY
// here, not a field the demo reaches in and edits: the Composer arms an
// observer for a bound source, so a Set schedules the frame that notices
// it. Hidden rather than Collapsed keeps the picture's space reserved —
// which is why the handle is a Visibility and not a bool, whose markup
// conversion is Visible/Collapsed.
func (m *model) toggle() {
	if m.shown.Get() == gooey.Visible {
		m.shown.Set(gooey.Hidden)
	} else {
		m.shown.Set(gooey.Visible)
	}
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
