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
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
	"github.com/WonderForgeLabs/gooey/term"
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

	running := true
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
			"Quit": gooey.Command(func() { running = false }),
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

	tree, err := markup.Load(fsys, "dashboard.gooey", ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	screen, err := term.Open()
	if err != nil {
		fmt.Fprintln(os.Stderr, "no tty:", err)
		os.Exit(1)
	}
	cols, rows := screen.Size()

	// Timers live in the tree, so their lifetime is the composition's.
	// Closing the outgoing Composer before building the next one is what
	// keeps a hot reload from leaving the replaced tree's ticker running.
	disp := gooey.NewDispatcher()
	needsFrame := true
	var comp *gooey.Composer
	attach := func(w gooey.Widget) {
		if comp != nil {
			comp.Close()
		}
		comp = gooey.NewComposer(w, cols, rows)
		comp.OnInvalidate(func() { needsFrame = true })
		comp.Start(disp)
		needsFrame = true
	}
	attach(tree)
	swaps := make(chan gooey.Widget, 1)
	stopWatch := markup.WatchAll(fsys, []string{"dashboard.gooey", "card.gooey", "badge.gooey"}, func() {
		if w, err := markup.Load(fsys, "dashboard.gooey", ctx); err == nil {
			swaps <- w
		}
	})
	defer stopWatch()

	if err := screen.Raw(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer screen.Restore()
	screen.EnableMouse()

	evs := make(chan input.Event, 16)
	go term.DecodeEvents(screen, evs)

	defer func() { comp.Close() }()

	for running {
		if needsFrame {
			comp.Frame()
			comp.Flush(screen.File())
			needsFrame = false
		}
		select {
		case <-disp.Wake():
			// Timer ticks (and any other posted work) run here, on the
			// UI goroutine, where touching properties is legal.
			disp.Drain()
		case w := <-swaps:
			attach(w)
		case ev := <-evs:
			comp.Handle(ev)
		}
	}
}
