// cards: the "just XAML" UserControl demo. Every panel on screen is
// an instance of card.gooey — a markup-only control resolved by
// convention (ctx.Includes), never registered, with no code-behind —
// and card.gooey itself instantiates badge.gooey, proving markup-only
// controls nest. The page context here has Values and Styles only:
// there is no Components map and no setup func anywhere in this app.
//
// Attributes are the entire control contract, and card.gooey DECLARES
// that contract in markup: four <x:Property> elements give it typed,
// defaulted, partly-required dependency properties. Literals (Title,
// Caption) coerce into fresh per-instance sources, the Value binding
// passes the dashboard's live string handle straight through
// type-checked — so four instances of one control show four different
// ticking data streams, and a misspelled attribute is now a load error
// instead of an attribute nothing reads. Still zero Go code for the
// control. All three .gooey files hot-reload; editing card.gooey
// restyles every card at once, state intact.
//
// The trend is a <Sparkline> declared inside card.gooey rather than a
// string of block runes the viewmodel built: what a card receives is the
// SERIES, and the plotting is the framework's. That one attribute is
// where the declaration system runs out, and the reason is written on
// the declaration — see card.gooey.
package main

import (
	"context"
	"fmt"
	"math/rand"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/cmd/internal/demomain"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// metric is a ticking value with a trend history — one per card. Both
// fields are property handles: the history is what the card's Sparkline
// plots, so it has to be able to invalidate that component the same way
// the value invalidates the big number.
type metric struct {
	value *prop.Property[float64]
	hist  *prop.Property[[]float64]
	fmt   string
}

func newMetric(start float64, format string) *metric {
	return &metric{
		value: prop.NewSource(start),
		hist:  prop.NewSource([]float64{start}),
		fmt:   format,
	}
}

func (m *metric) tick(drift float64) {
	v := m.value.Get() + (rand.Float64()-0.5)*drift
	if v < 0 {
		v = 0
	}
	m.value.Set(v)
	// A fresh slice per tick: prop.Set hands the same backing array to
	// every reader, and appending in place would mutate a value the
	// Sparkline may already be plotting.
	h := append(append([]float64(nil), m.hist.Get()...), v)
	if len(h) > 24 {
		h = h[1:]
	}
	m.hist.Set(h)
}

// label and trend are computeds over the metric's sources — the card
// binds these handles through its attributes.
func (m *metric) label() *prop.Property[string] {
	return prop.NewComputed(func() string { return fmt.Sprintf(m.fmt, m.value.Get()) })
}

// sparkFloor is where a Sparkline's lowest DRAWN bar starts.
//
// Sparkline plots an absolute 0-100 scale and picks its glyph from
// int(v/100*8) over " ▁▂▃▄▅▆▇█", so v=0 is a blank cell. These metrics
// are unbounded (requests per second, p99 milliseconds) and want the
// autoscaling a trend line normally has: the window's own minimum should
// read as ▁, not as a hole in the plot. Rescaling onto [12.5, 100] puts
// the minimum in the first drawn bucket and the maximum in the last.
const sparkFloor = 12.5

// trend rescales the history window onto the band Sparkline plots. It is
// the one thing a series has to say in Go, because Sparkline's scale is
// absolute and this metric's is relative to its own window.
func (m *metric) trend() *prop.Property[[]float64] {
	return prop.NewComputed(func() []float64 {
		h := m.hist.Get()
		lo, hi := h[0], h[0]
		for _, v := range h {
			lo, hi = min(lo, v), max(hi, v)
		}
		out := make([]float64, len(h))
		for i, v := range h {
			t := 0.0
			if hi > lo {
				t = (v - lo) / (hi - lo)
			}
			out[i] = sparkFloor + t*(100-sparkFloor)
		}
		return out
	})
}

func main() {
	reqs := newMetric(1200, "%.0f")
	lat := newMetric(38, "%.1f")
	errs := newMetric(3, "%.0f")
	gors := newMetric(86, "%.0f")

	var app *gooey.App
	ticking := prop.NewSource(true)
	ctx := &markup.Context{
		Values: map[string]any{
			"Ticking": ticking,
			"Reqs":    reqs.label(), "ReqsTrend": reqs.trend(),
			"Lat": lat.label(), "LatTrend": lat.trend(),
			"Errs": errs.label(), "ErrsTrend": errs.trend(),
			"Gors": gors.label(), "GorsTrend": gors.trend(),
			"Advance": gooey.Command(func() {
				reqs.tick(180)
				lat.tick(9)
				errs.tick(2)
				gors.tick(12)
			}),
			"Quit": gooey.Command(func() { app.Quit() }),
		},
		Styles: map[string]render.Style{
			"panel": {Fg: render.RGB(120, 90, 220)},
			"big":   {Fg: render.RGB(255, 170, 60), Bold: true},
			"trend": {Fg: render.RGB(110, 220, 130)},
			"badge": {Fg: render.RGB(140, 140, 150)},
			"dim":   {Fg: render.RGB(140, 140, 150)},
		},
		// The whole mechanism: unknown elements resolve to .gooey files
		// here by convention. <Card/> → card.gooey, <Badge/> → badge.gooey.
	}

	fsys := demomain.MarkupFS("cards", "dashboard.gooey")
	ctx.Includes = fsys

	// One page, three files: the two controls it instantiates are named
	// here because a rebuild has to be triggered by edits to any of them.
	// Editing card.gooey restyles every card at once, state intact — the
	// App rebuilds the tree and the viewmodel properties carry across.
	//
	// The <Timer> that advances the metrics lives in the markup, so its
	// lifetime is the composition's: the App closes the outgoing Composer
	// on every swap, which is what keeps a replaced tree from ticking on.
	app = gooey.NewApp(markup.Page(fsys, "dashboard.gooey", ctx, "card.gooey", "badge.gooey"))
	if err := app.Run(context.Background()); err != nil {
		gooey.Exit(err)
	}
}
