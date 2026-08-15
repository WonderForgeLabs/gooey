// plates draws with paint/ from markup: three pages of plates whose
// pens, brushes and geometry are all attributes in a .gooey file, and a
// Go program that supplies nothing but a viewmodel and the registration.
//
// The registration is the part worth reading. paint is a nested module,
// so the root module cannot import it and no builtin element could ever
// be <Ellipse>; the seam is markup.Context.Components, and these four
// lines are the whole of it:
//
//	Components: map[string]markup.Builder{
//	    "Figure":       shapes.Builder(),
//	    "StrokesScene": markup.Include(fsys, "strokes.gooey"),
//	    ...
//	}
//
// After that the documents draw themselves. There is no plate list in
// Go here, which is deliberate — the version of this demo that expressed
// its samples as Go calls was written and never committed, because
// reading it taught you gg rather than gooey.
//
// # What you are looking at
//
// On a terminal with sixel, kitty or iTerm2, each plate is rasterized at
// exactly the pixel size of the cells it occupies. On any other terminal
// — including the one an asciinema capture replays in — the same figures
// draw into the same cells as block runes shaded by coverage. The header
// says which tier you got. Nothing moves between them: that is the rule
// components.ButtonChrome set and panel followed.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/graphics"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/paint/shapes"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
	"github.com/WonderForgeLabs/gooey/term"
)

const sceneCount = 3

func main() {
	// Relative to the paint module root, because that is where a nested
	// module's demo is run from: the root `./...` stops at the module
	// boundary, so this is `cd paint && go run ./cmd/plates`.
	dir := flag.String("dir", "cmd/plates", "directory holding the .gooey pages")
	mode := flag.String("mode", "", "force graphics mode: kitty|sixel|iterm2|cells")
	hold := flag.Duration("hold", 0, "exit after this duration instead of waiting for a quit key")
	flag.Parse()

	enc, forced, err := encoderFor(*mode)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	abs, err := filepath.Abs(*dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	fsys := os.DirFS(abs)

	vm := newModel()
	ctx := &markup.Context{
		Values: vm.values(),
		Styles: map[string]render.Style{
			"panel":  {Fg: render.RGB(0x6a, 0x6a, 0x7a)},
			"accent": {Fg: render.RGB(0x6c, 0x9c, 0xff), Bold: true},
			"dim":    {Fg: render.RGB(0x80, 0x80, 0x90)},
		},
		Components: map[string]markup.Builder{
			// The one element paint contributes, and the three pages that
			// use it. A page is a markup-only control: no code-behind, and
			// its attributes would become its context if it took any.
			"Figure":       shapes.Builder(),
			"StrokesScene": markup.Include(fsys, "strokes.gooey"),
			"BrushesScene": markup.Include(fsys, "brushes.gooey"),
			"RingScene":    markup.Include(fsys, "ring.gooey"),
		},
	}

	opts := []gooey.Option{gooey.WithoutMouse()}
	if forced {
		// A forced protocol still needs a cell size, and only a probe can
		// know it. Passing one is not optional: an encoder with CellW at
		// zero makes paint.Canvas refuse a canvas of nothing and the
		// figures vanish with no error anywhere (issue #251 is the same
		// bug reached from components.Image).
		opts = append(opts,
			gooey.WithGraphics(enc),
			gooey.WithCaps(term.Caps{CellW: 10, CellH: 20, Color: term.DetectColorDepth()}))
	} else {
		opts = append(opts, gooey.WithCapabilityProbe())
	}

	// Every page is watched, so editing a plate redraws it in place. The
	// scene index is a property and survives the rebuild, which is the
	// point of the split: the viewmodel is durable and the tree is not.
	app := gooey.NewApp(
		markup.Page(fsys, "plates.gooey", ctx, "strokes.gooey", "brushes.gooey", "ring.gooey"),
		opts...)
	vm.app = app
	app.BeforeFrame(vm.refresh)
	if *hold > 0 {
		app.Every(*hold, app.Quit)
	}
	gooey.Exit(app.Run(context.Background()))
}

// encoderFor mirrors cmd/pixels's flag, with one name changed: "cells"
// rather than "halfblock". A <Figure> does not degrade through
// graphics.DrawHalfblock — that helper discards alpha and would paint a
// transparent canvas solid black — so calling the mode halfblock here
// would name a mechanism this demo does not use.
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
	case "cells":
		return nil, true, nil
	default:
		return nil, false, fmt.Errorf("unknown mode: %s (want kitty, sixel, iterm2 or cells)", mode)
	}
}

// model is the viewmodel: the scene index the pages switch on, and two
// strings the header reports. Nothing about the art is here.
type model struct {
	app   *gooey.App
	scene *prop.Property[int]
	tier  *prop.Property[string]
	stats *prop.Property[string]
}

func newModel() *model {
	return &model{
		scene: prop.NewSource(0),
		tier:  prop.NewSource("detecting…"),
		stats: prop.NewSource(""),
	}
}

func (m *model) values() map[string]any {
	return map[string]any{
		"Scene":  m.scene,
		"Tier":   m.tier,
		"Stats":  m.stats,
		"Scene1": gooey.Command(func() { m.scene.Set(0) }),
		"Scene2": gooey.Command(func() { m.scene.Set(1) }),
		"Scene3": gooey.Command(func() { m.scene.Set(2) }),
		"NextScene": gooey.Command(func() {
			m.scene.Set((m.scene.Get() + 1) % sceneCount)
		}),
		"Quit": gooey.Command(func() { m.app.Quit() }),
	}
}

// refresh reports the tier and the damage count.
//
// Both Sets are guarded by a comparison, because prop.Set does not
// compare: an unguarded per-frame Set would dirty the header forever and
// the repaint count it reports would be a count of itself.
func (m *model) refresh() {
	c := m.app.Composer()
	if c == nil {
		return
	}
	caps := c.Caps()
	tier := fmt.Sprintf("tier: CELL — no pixel protocol; figures are block runes shaded by coverage   ·   cell %d×%d px", caps.CellW, caps.CellH)
	if enc := c.Graphics(); enc != nil && caps.CellW > 0 && caps.CellH > 0 {
		tier = fmt.Sprintf("tier: PIXEL via %s — each figure rasterized at %d×%d px per cell, 1:1, never resampled",
			enc.Name(), caps.CellW, caps.CellH)
	}
	if tier != m.tier.Get() {
		m.tier.Set(tier)
	}
	stats := fmt.Sprintf("last frame: %d component(s) repainted   ·   %d frames   ·   %d bytes",
		m.app.PaintedLastFrame(), m.app.Frames(), m.app.FlushBytes())
	if stats != m.stats.Get() {
		m.stats.Set(stats)
	}
}
