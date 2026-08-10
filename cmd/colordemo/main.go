// colordemo is the visual chapter: absolute layout, capability-adaptive
// color, and a component whose experience changes with the terminal.
//
// Three things are on screen at once:
//
//   - A ColorPicker bound to one Accent property. Its channel bars are
//     smooth gradients on a truecolor terminal, banded on a 256-color
//     one, and a plain fill meter with an ANSI color NAME on a 16-color
//     one — the component asks the Frame what it is painting onto.
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
//	colordemo                 detect the terminal's color depth
//	colordemo --depth=256     force a tier (truecolor|256|16)
//	colordemo --hold=3s       exit after a while instead of on q
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
	"github.com/WonderForgeLabs/gooey/term"
)

func main() {
	depthFlag := flag.String("depth", "", "force color depth: truecolor|256|16")
	hold := flag.Duration("hold", 0, "exit after this duration instead of waiting for q")
	flag.Parse()

	screen, err := term.Open()
	if err != nil {
		fmt.Fprintln(os.Stderr, "no tty:", err)
		os.Exit(1)
	}
	cols, rows := screen.Size()

	// Capability detection, and the flag that overrides it. Forcing a
	// tier is what makes the difference recordable: a GIF recorder
	// renders truecolor, so the only way to show what a 256-color
	// terminal does is to emit what a 256-color terminal would get.
	//
	// Color depth only — deliberately NOT the full Screen.Detect()
	// handshake. Detect abandons a pending tty read when the terminal
	// does not answer, and that orphaned reader then competes with the
	// input decoder for the next bytes, swallowing the first keystrokes.
	// A keyboard-driven demo cannot afford that; the graphics handshake
	// belongs to cmd/demo, which reads one key and exits. Color depth is
	// a pure environment read, so it costs nothing and steals nothing.
	caps := term.Caps{Cols: cols, Rows: rows, Color: term.DetectColorDepth()}
	detected, forced := caps.Color, false
	if *depthFlag != "" {
		d, ok := render.ParseColorDepth(*depthFlag)
		if !ok {
			fmt.Fprintf(os.Stderr, "unknown depth %q (want truecolor, 256, or 16)\n", *depthFlag)
			os.Exit(2)
		}
		caps.Color, forced = d, true
	}

	// --- viewmodel: one source property, everything else derived ---
	accent := prop.NewSource(render.RGB(255, 170, 60))

	accentStyle := prop.NewComputed(func() render.Style {
		return render.Style{Fg: accent.Get(), Bold: true}
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
		shown := render.Approximate(c, caps.Color)
		return fmt.Sprintf("depth %s [%s]   %d×%d cells   picked #%02X%02X%02X → shown #%02X%02X%02X",
			caps.Color, src, caps.Cols, caps.Rows,
			c.R, c.G, c.B, shown.R, shown.G, shown.B)
	})

	running := true
	ctx := &markup.Context{
		Values: map[string]any{
			"Accent":      accent,
			"AccentStyle": accentStyle,
			"Tint0":       tints[0], "Tint1": tints[1], "Tint2": tints[2],
			"Tint3": tints[3], "Tint4": tints[4],
			"Status": status,
			"Quit":   gooey.Command(func() { running = false }),
		},
		Styles: map[string]render.Style{
			"dim": {Fg: render.RGB(140, 140, 150)},
		},
		Components: map[string]markup.Builder{
			// Demo-local: it exists to EXPLAIN the tiers, which is a
			// teaching job, not a general-purpose control.
			"TierStrip": func(markup.Element, *markup.Context) (gooey.Component, error) {
				return &tierStrip{accent: accent}, nil
			},
		},
	}

	dir := "cmd/colordemo"
	if _, err := os.Stat(filepath.Join(dir, "colordemo.gooey")); err != nil {
		exe, _ := os.Executable()
		dir = filepath.Dir(exe)
	}
	fsys := os.DirFS(dir)
	tree, err := markup.Load(fsys, "colordemo.gooey", ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	needsFrame := true
	var comp *gooey.Composer
	attach := func(w gooey.Component) {
		comp = gooey.NewComposer(w, cols, rows)
		// The capabilities reach every component's Render through the Frame.
		comp.SetCaps(caps)
		comp.OnInvalidate(func() { needsFrame = true })
		needsFrame = true
	}
	attach(tree)
	swaps := make(chan gooey.Component, 1)
	stopWatch := markup.Watch(fsys, "colordemo.gooey", ctx, func(w gooey.Component) { swaps <- w })
	defer stopWatch()

	if err := screen.Raw(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer screen.Restore()
	screen.EnableMouse()

	events := make(chan input.Event, 32)
	go term.DecodeEvents(screen, events)

	var deadline <-chan time.Time
	if *hold > 0 {
		deadline = time.After(*hold)
	}

	for running {
		if needsFrame {
			comp.Frame()
			comp.Flush(screen.File())
			needsFrame = false
		}
		select {
		case <-deadline:
			running = false
		case w := <-swaps:
			attach(w)
		case ev := <-events:
			comp.Handle(ev)
		}
	}
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
