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
//     absolute coordinates.
//
// Everything is styled by ONE property: Style="{{.AccentStyle}}" in the
// markup is a live handle onto a computed over the picked color, so
// editing a channel restyles the page through the property graph.
//
//	colors                    probe the terminal: color depth, graphics, cell size
//	colors --depth=256        force a color tier (truecolor|256|16)
//	colors --graphics=kitty   force the pixel tier (kitty|sixel|iterm2|cells)
//	colors --hold=3s          exit after a while instead of on q
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/cmd/internal/demomain"
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

	// The flags that override capability detection. Forcing a tier is
	// what makes the difference recordable: a GIF recorder renders
	// truecolor cells, so the only way to show what a 256-color terminal
	// — or one without a graphics protocol — does is to emit what that
	// terminal would get.
	//
	// Detection itself is gooey.WithCapabilityProbe below, which runs the
	// full Screen.Detect handshake inside App.acquire rather than the
	// color-depth environment read alone: the picker's pixel tier exists
	// only where the probe finds a graphics protocol AND a cell size to
	// generate the bars against. App runs it in exactly the place this
	// file used to — after opening the terminal, before Raw and before
	// the input decoder starts — so the replies cannot interleave with
	// input events. (This demo once avoided Detect because its probe
	// abandoned a pending tty read that then stole the first keystrokes;
	// term.readUntilDA1 reads synchronously under a deadline now, so a
	// keyboard-driven demo can afford it.)
	var depth render.ColorDepth
	forced := false
	if *depthFlag != "" {
		d, ok := render.ParseColorDepth(*depthFlag)
		if !ok {
			fmt.Fprintf(os.Stderr, "unknown depth %q (want truecolor, 256, or 16)\n", *depthFlag)
			os.Exit(2)
		}
		depth, forced = d, true
	}

	// The pixel tier: by default the terminal's answer stands (that is
	// the component's contract — the choice is the terminal's, not the
	// author's); the flag exists to force a protocol on, or off with
	// "cells", for recordings and side-by-side comparison.
	var gfx graphics.Encoder
	gfxForced := false
	switch *gfxFlag {
	case "":
	case "cells":
		gfxForced = true
	case "kitty":
		gfx, gfxForced = graphics.Kitty{}, true
	case "sixel":
		gfx, gfxForced = graphics.Sixel{}, true
	case "iterm2":
		gfx, gfxForced = graphics.ITerm2{}, true
	default:
		fmt.Fprintf(os.Stderr, "unknown graphics %q (want kitty, sixel, iterm2, or cells)\n", *gfxFlag)
		os.Exit(2)
	}
	// "a forced protocol still needs a cell size" used to be a hand-written
	// 10×20 right here, in this demo and two others. App.caps owns that
	// rule now (app.go:599-635) and applies it to any pinned encoder.

	// What the terminal turned out to be, filled in from the live
	// composition on the first frame — see the BeforeFrame hook below.
	// Held as plain vars rather than properties because they are settled
	// before anything paints and never move again.
	detected, effective := render.TrueColor, render.TrueColor
	gfxName := "cells"

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

	status := prop.NewComputed(func() string {
		c := accent.Get()
		src := "detected"
		if forced {
			src = fmt.Sprintf("forced (terminal reports %s)", detected)
		}
		shown := render.Approximate(c, effective)
		return fmt.Sprintf("depth %s [%s]   gfx %s   picked #%02X%02X%02X → shown #%02X%02X%02X",
			effective, src, gfxName,
			c.R, c.G, c.B, shown.R, shown.G, shown.B)
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

	fsys := demomain.MarkupFS("colors", "colors.gooey")

	// The probe is unconditional: even under --depth or --graphics this
	// page reports what the terminal actually said ("forced (terminal
	// reports …)"), and the pixel tier needs the cell size regardless.
	// WithGraphics pins the protocol when a flag asked for one — a nil
	// encoder pins the cell tier, which is what --graphics=cells means.
	opts := []gooey.Option{gooey.WithCapabilityProbe()}
	if gfxForced {
		opts = append(opts, gooey.WithGraphics(gfx))
	}
	// markup.Page is the hot-reload seam. It rebuilds on the UI
	// goroutine, unlike the markup.Watch + swap channel this replaced,
	// which resolved bindings — property-graph work — on the watcher's
	// own goroutine.
	app = gooey.NewApp(markup.Page(fsys, "colors.gooey", ctx), opts...)

	// --hold: the same "exit after a while instead of on q" that a
	// time.After deadline in the old select gave, expressed as the app's
	// own clock. Every re-fires, but Quit is idempotent and Run has
	// already returned by then.
	if *hold > 0 {
		app.Every(*hold, app.Quit)
	}

	// Capabilities are the App's now, so both the reporting and the
	// forced depth read and write the live composition here.
	//
	// It has to be a hook rather than a one-shot: App re-derives caps
	// from the probe on every resize and on every hot reload, so a depth
	// pinned once would be silently un-pinned by the next SIGWINCH. The
	// != guard keeps SetCaps to the frames that actually need it, and
	// BeforeFrame is early enough to satisfy its "call it before the
	// frame" contract — including the very first one, so a forced tier is
	// forced from the first cell painted.
	first := true
	app.BeforeFrame(func() {
		c := app.Composer()
		caps := c.Caps()
		if first {
			first = false
			detected = caps.Color
			if enc := c.Graphics(); enc != nil {
				gfxName = enc.Name()
			}
		}
		if forced && caps.Color != depth {
			caps.Color = depth
			c.SetCaps(caps)
		}
		effective = caps.Color
	})

	gooey.Exit(app.Run(context.Background()))
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
