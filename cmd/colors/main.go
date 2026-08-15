// colors is the visual chapter: absolute layout, capability-adaptive
// color, and a component whose experience changes with the terminal.
//
// Three things are on screen at once:
//
//   - A ColorPicker bound to one Accent property. Its channel bars are
//     smooth gradients on a truecolor terminal, banded on a 256-color
//     one, and a plain fill meter with an ANSI color NAME on a 16-color
//     one — the component asks the Frame what it is painting onto. On a
//     terminal with a graphics protocol the bars are placed as real
//     pixel gradients over their cells, markers and all.
//   - A tier strip that simulates all three at once by pre-approximating
//     a gradient with the same function the flush uses, so the cost of
//     each tier is visible side by side on one terminal.
//   - A Canvas holding both, plus an overlapping cascade of swatches at
//     absolute coordinates — five instances of swatch.gooey, a
//     markup-only control resolved by name through Context.Includes.
//
// Everything is styled by ONE property: Style="{{.AccentStyle}}" in the
// markup is a live handle onto a computed over the picked color, so
// editing a channel restyles the page through the property graph.
//
//	colors                    probe the terminal: color depth, graphics, cell size
//	colors --depth=256        force a color tier (truecolor|256|16)
//	colors --graphics=kitty   force the pixel tier (kitty|sixel|iterm2|cells)
//	colors --hold=3s          exit after a while instead of on q
//
// The run loop is gooey.App's: terminal acquisition, the input decoder,
// the frame scheduler and the hot-reload watcher are the framework's job,
// and the only thing this file reads back OUT of the composition is the
// capability line at the bottom of the page — which is a property of the
// composition rather than of the viewmodel, so it is read once on the
// first frame and published into ordinary source properties.
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
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

func main() {
	depthFlag := flag.String("depth", "", "force color depth: truecolor|256|16")
	gfxFlag := flag.String("graphics", "", "force the pixel tier: kitty|sixel|iterm2|cells (default: ask the terminal)")
	hold := flag.Duration("hold", 0, "exit after this duration instead of waiting for q")
	flag.Parse()

	// Forcing a tier is what makes the difference recordable: a GIF
	// recorder renders truecolor cells, so the only way to show what a
	// 256-color terminal — or one without a graphics protocol — does is to
	// emit what that terminal would get.
	forcedDepth, depthForced := render.TrueColor, false
	if *depthFlag != "" {
		d, ok := render.ParseColorDepth(*depthFlag)
		if !ok {
			fmt.Fprintf(os.Stderr, "unknown depth %q (want truecolor, 256, or 16)\n", *depthFlag)
			os.Exit(2)
		}
		forcedDepth, depthForced = d, true
	}
	// The pixel tier: by default the terminal's answer stands (that is the
	// component's contract — the choice is the terminal's, not the
	// author's); the flag exists to force a protocol on, or off with
	// "cells", for recordings and side-by-side comparison.
	gfx, gfxForced, err := encoderFor(*gfxFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	// --- viewmodel: one source property, everything else derived ---
	accent := prop.NewSource(render.RGB(255, 170, 60))

	// The page is a filled surface, not a frame on the terminal default:
	// Background="#12121e" on the Border is painted by the framework,
	// and every leaf that pre-clears against it clears to this color.
	// Leaves that draw text set the same Bg so their glyph cells sit
	// flush on the fill (cells have no alpha).
	panel := render.RGB(0x12, 0x12, 0x1e)

	accentStyle := prop.NewComputed(func() render.Style {
		return render.Style{Fg: accent.Get(), Bold: true, Bg: panel}
	})
	// The swatch cascade: the same color at descending brightness. Each
	// is its own computed, so each is its own paint dependency.
	tints := make([]*prop.Property[render.Style], 5)
	for i := range tints {
		scale := 1.0 - 0.18*float64(i)
		tints[i] = prop.NewComputed(func() render.Style {
			return render.Style{Fg: scaleColor(accent.Get(), scale)}
		})
	}

	// What the composition turned out to be, as properties. Capabilities
	// are not viewmodel state — nothing can bind them and no markup can
	// ask for them — so the status line reads them off the Composer on the
	// first frame and Sets them here, where a computed can see them.
	depth := prop.NewSource(render.TrueColor)
	depthNote := prop.NewSource("detected")
	gfxName := prop.NewSource("cells")
	status := prop.NewComputed(func() string {
		// Every Get runs unconditionally: a read behind an `if` drops out
		// of the dependency set on the frames it does not execute.
		c, d, note, name := accent.Get(), depth.Get(), depthNote.Get(), gfxName.Get()
		shown := render.Approximate(c, d)
		return fmt.Sprintf("depth %s [%s]   gfx %s   picked #%02X%02X%02X → shown #%02X%02X%02X",
			d, note, name, c.R, c.G, c.B, shown.R, shown.G, shown.B)
	})

	var app *gooey.App
	ctx := &markup.Context{
		Values: map[string]any{
			"Accent":      accent,
			"AccentStyle": accentStyle,
			"Tint0":       tints[0], "Tint1": tints[1], "Tint2": tints[2],
			"Tint3": tints[3], "Tint4": tints[4],
			"Status": status,
			"Quit":   gooey.Command(func() { app.Quit() }),
		},
		Styles: map[string]render.Style{
			"dim": {Fg: render.RGB(140, 140, 150), Bg: panel},
		},
		Components: map[string]markup.Builder{
			// Demo-local: it exists to EXPLAIN the tiers, which is a
			// teaching job, not a general-purpose control.
			"TierStrip": func(markup.Element, *markup.Context) (gooey.Component, error) {
				return &tierStrip{accent: accent}, nil
			},
		},
	}

	dir := "cmd/colors"
	if _, err := os.Stat(filepath.Join(dir, "colors.gooey")); err != nil {
		exe, _ := os.Executable()
		dir = filepath.Dir(exe)
	}
	fsys := os.DirFS(dir)
	// <Swatch/> resolves to swatch.gooey by convention: a markup-only
	// control needs no registration, only somewhere to be found.
	ctx.Includes = fsys

	// "Let capabilities decide" is a probe, not an absence: without one
	// the app has a color depth and no graphics answer at all, so the
	// picker's pixel tier could never appear. The probe runs inside Run,
	// before the decoder starts, so its replies cannot interleave with
	// input events.
	opts := []gooey.Option{gooey.WithCapabilityProbe()}
	if gfxForced {
		// Pinning is all it takes: App.caps supplies the assumed cell size
		// a pinned protocol needs.
		opts = append(opts, gooey.WithGraphics(gfx))
	}
	// The extra name is what makes editing the CONTROL hot-reload too: a
	// watcher cannot infer an <Include>, because resolving one needs the
	// build that has not happened yet.
	app = gooey.NewApp(markup.Page(fsys, "colors.gooey", ctx, "swatch.gooey"), opts...)
	if *hold > 0 {
		app.Every(*hold, app.Quit)
	}

	told := false
	app.BeforeFrame(func() {
		c := app.Composer()
		caps := c.Caps()
		if !told {
			told = true
			name := "cells"
			if enc := c.Graphics(); enc != nil {
				name = enc.Name()
			}
			gfxName.Set(name)
			if depthForced {
				// Read BEFORE the override below, so the note can still
				// report what the terminal actually said.
				depthNote.Set(fmt.Sprintf("forced (terminal reports %s)", caps.Color))
			}
		}
		// Re-applied rather than set once: a hot reload builds a fresh
		// Composer from the app's own caps, which never carry the flag.
		if depthForced && caps.Color != forcedDepth {
			caps.Color = forcedDepth
			c.SetCaps(caps)
		}
		// Guarded because prop.Set does not compare: an unconditional Set
		// would invalidate the status line on every single frame.
		if got := c.Caps().Color; depth.Get() != got {
			depth.Set(got)
		}
	})

	if err := app.Run(context.Background()); err != nil {
		gooey.Exit(err)
	}
}

// encoderFor resolves --graphics. "cells" is a real answer, not the
// absence of one: it forces the universal tier, which is what you want
// when checking what the page looks like with no pixel plane at all.
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
	}
	return nil, false, fmt.Errorf("unknown graphics %q (want kitty, sixel, iterm2, or cells)", mode)
}

// scaleColor multiplies a color's channels, staying in range.
func scaleColor(c render.Color, f float64) render.Color {
	scale := func(v uint8) uint8 {
		n := float64(v) * f
		if n > 255 {
			n = 255
		}
		if n < 0 {
			n = 0
		}
		return uint8(n)
	}
	return render.RGB(scale(c.R), scale(c.G), scale(c.B))
}

// tierStrip draws one gradient three times: once exactly, once through
// the 256-color quantizer, and once through the 16-color one. All three
// rows are painted with truecolor values — the quantization is applied
// in the COMPONENT rather than at the wire — so a single terminal can show
// what the other two classes of terminal would do with the same colors.
//
// It is a simulation and says so: on a terminal that really is 256-color,
// the flush quantizes all three rows and the top two converge, which is
// itself the honest demonstration.
type tierStrip struct {
	gooey.Base
	accent *prop.Property[render.Color]
}

func (s *tierStrip) Measure(avail gooey.Size) gooey.Size {
	return gooey.Size{W: min(44, avail.W), H: min(3, avail.H)}
}

func (s *tierStrip) Render(f *gooey.Frame) {
	b := s.Bounds()
	c := s.accent.Get()
	const labelW = 11
	rows := []struct {
		name  string
		depth render.ColorDepth
	}{
		{"truecolor", render.TrueColor},
		{"256", render.Color256},
		{"16", render.Color16},
	}
	barW := b.W - labelW
	for r, row := range rows {
		if r >= b.H {
			break
		}
		y := b.Y + r
		f.Cells.SetString(b.X, y, fmt.Sprintf("%-*s", labelW, row.name),
			render.Style{Fg: render.RGB(140, 140, 150)})
		for i := 0; i < barW; i++ {
			// Sweep brightness across the bar, then show what that tier
			// would actually display for it.
			swept := scaleColor(c, float64(i)/float64(max(1, barW-1)))
			f.Cells.Set(b.X+labelW+i, y, '█',
				render.Style{Fg: render.Approximate(swept, row.depth)})
		}
	}
}
