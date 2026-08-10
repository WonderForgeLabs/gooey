// How-to: draw anything — a custom component with its own Render.
//
// The wave below is the whole pattern at small size: a leaf that paints
// arbitrary cells inside its bounds, reads one property while painting
// (which is its entire damage declaration), adapts to the terminal's
// color depth through Frame.Caps, and animates itself with a Startable
// goroutine that posts to the UI loop instead of touching the graph.
//
//	cd docs/learn/examples/howto-custom-draw && go run .
//
// Walkthrough: docs/learn/howto/howto-custom-draw.md
package main

import (
	"context"
	"math"
	"os"
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// Eighth-block runes, empty to full. Index i is a cell filled i/8 from
// the bottom.
var blocks = []rune(" ▁▂▃▄▅▆▇█")

// wave is a leaf component: it embeds gooey.Base for bounds and layout
// attributes, and implements Measure and Render itself.
type wave struct {
	gooey.Base
	phase *prop.Property[int]
}

func (w *wave) Measure(avail gooey.Size) gooey.Size {
	// A want, not a promise: take the full width, up to six rows tall.
	return gooey.Size{W: avail.W, H: min(6, avail.H)}
}

func (w *wave) Render(f *gooey.Frame) {
	b := w.Bounds()
	// This Get is the whole damage declaration: Render runs inside this
	// component's paint node, so phase becomes a dependency, and every
	// Set repaints this wave and nothing else.
	p := w.phase.Get()

	// Frame.Caps says what the terminal can show. On truecolor the wave
	// gets a per-column gradient; on 256/16 colors a gradient would just
	// band, so one honest color is better.
	truecolor := f.Depth() == render.TrueColor

	for x := 0; x < b.W; x++ {
		v := (math.Sin(float64(x+p)*0.18) + 1) / 2 // 0..1
		st := render.Style{Fg: render.RGB(120, 200, 140)}
		if truecolor {
			st.Fg = render.RGB(uint8(60+180*v), uint8(200-80*v), 200)
		}
		eighths := int(v * float64(b.H) * 8) // column height, in 1/8 cells
		for y := 0; y < b.H; y++ {
			e := eighths - 8*(b.H-1-y)
			if e <= 0 {
				continue // untouched cells keep the pre-cleared panel fill
			}
			f.Cells.Set(b.X+x, b.Y+y, blocks[min(e, 8)], st)
		}
	}
}

// Start makes the wave animate itself. The Composer discovers Startable
// elements when the composition goes live and stops them on Close (hot
// reload, quit). The goroutine never touches the property graph — it
// posts a closure to the UI loop, where Set is safe.
func (w *wave) Start(post func(func())) (stop func()) {
	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		tk := time.NewTicker(80 * time.Millisecond)
		defer tk.Stop()
		for {
			select {
			case <-done:
				return
			case <-tk.C:
				post(func() { w.phase.Set(w.phase.Get() + 1) })
			}
		}
	}()
	// Close AND join: a tick that already won the select still posts
	// before stop returns, so after stop there are no further posts.
	return func() {
		close(done)
		<-stopped
	}
}

func main() {
	var app *gooey.App

	ctx := &markup.Context{
		Values: map[string]any{
			"Quit": gooey.Command(func() { app.Quit() }),
		},
		Styles: map[string]render.Style{
			"panel": {Fg: render.RGB(120, 90, 220)},
			"dim":   {Fg: render.RGB(150, 150, 165)},
		},
		Components: map[string]markup.Builder{
			"Wave": func(e markup.Element, c *markup.Context) (gooey.Component, error) {
				return &wave{phase: prop.NewSource(0)}, nil
			},
		},
	}

	app = gooey.NewApp(markup.Page(os.DirFS("."), "app.gooey", ctx))
	if err := app.Run(context.Background()); err != nil {
		gooey.Exit(err)
	}
}
