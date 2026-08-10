// cardsdemo: the "just XAML" UserControl demo. Every panel on screen is
// an instance of card.gooey — a markup-only control resolved by
// convention (ctx.Includes), never registered, with no code-behind —
// and card.gooey itself instantiates badge.gooey, proving markup-only
// controls nest. The page context here has Values and Styles only:
// there is no Widgets map and no setup func anywhere in this app.
//
// Attributes are the entire control contract: literals (Title, Caption)
// pass as strings, bindings (Value, Trend) pass as live property
// handles — so four instances of one control show four different
// ticking data streams. All three .gooey files hot-reload; editing
// card.gooey restyles every card at once, state intact.
package main

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

var sparks = []rune("▁▂▃▄▅▆▇█")

// metric is a ticking value with a trend history — one per card.
type metric struct {
	value *prop.Property[float64]
	hist  []float64
	fmt   string
}

func newMetric(start float64, format string) *metric {
	return &metric{value: prop.NewSource(start), hist: []float64{start}, fmt: format}
}

func (m *metric) tick(drift float64) {
	v := m.value.Get() + (rand.Float64()-0.5)*drift
	if v < 0 {
		v = 0
	}
	m.value.Set(v)
	m.hist = append(m.hist, v)
	if len(m.hist) > 24 {
		m.hist = m.hist[1:]
	}
}

// label and trend are computeds over the metric's source — the card
// binds these handles through its attributes.
func (m *metric) label() *prop.Property[string] {
	return prop.NewComputed(func() string { return fmt.Sprintf(m.fmt, m.value.Get()) })
}

func (m *metric) trend() *prop.Property[string] {
	return prop.NewComputed(func() string {
		m.value.Get() // subscribe; hist rides along on the same tick
		lo, hi := m.hist[0], m.hist[0]
		for _, v := range m.hist {
			lo, hi = min(lo, v), max(hi, v)
		}
		var sb strings.Builder
		for _, v := range m.hist {
			i := 0
			if hi > lo {
				i = int((v - lo) / (hi - lo) * float64(len(sparks)-1))
			}
			sb.WriteRune(sparks[i])
		}
		return sb.String()
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

	dir := "cmd/cardsdemo"
	if _, err := os.Stat(filepath.Join(dir, "dashboard.gooey")); err != nil {
		exe, _ := os.Executable()
		dir = filepath.Dir(exe)
	}
	fsys := os.DirFS(dir)
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
