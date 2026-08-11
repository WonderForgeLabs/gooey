// Tutorial 7 — app chrome: a MenuBar with alt+letter mnemonics, a
// StatusBar with bound sections, a ToastHost fired from commands, and
// tooltips through an AdornmentLayer — dressed around a fake download
// built from the wave-1 widgets (ProgressBar, Spinner, Sparkline,
// Toggle, Segmented).
//
// Run it from this directory so os.DirFS(".") finds app.gooey:
//
//	cd docs/learn/examples/07-app-chrome && go run .
//
// Walkthrough: docs/learn/07-app-chrome.md
package main

import (
	"context"
	"math"
	"os"
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

var speeds = []string{"Slow", "Normal", "Turbo"}

func main() {
	// --- viewmodel ---
	var app *gooey.App
	pct := prop.NewSource(0)
	running := prop.NewSource(true)
	speed := prop.NewSource(1) // index into speeds
	rates := prop.NewSource([]float64{})
	status := prop.NewSource("downloading")
	log := prop.NewSource("the job is running — pause it with the rocker, or open the menu with alt+j")
	clock := prop.NewSource(time.Now().Format("15:04:05"))

	var ctx *markup.Context

	// toast pops a message over the page. The host is looked up per fire
	// rather than captured, so a hot-reload swap — which rebuilds the
	// named elements — never leaves this holding a dead layer.
	toast := func(msg string) {
		if toasts, err := markup.Find[*components.ToastHost](ctx, "Toasts"); err == nil {
			toasts.Show(msg)
		}
	}

	// advance runs on the UI goroutine (a Timer posts it), so it can
	// read and set properties freely. tick is plain Go state: nothing
	// paints from it, so it does not need to be a property.
	tick := 0
	advance := func() {
		tick++
		if !running.Get() || pct.Get() >= 100 {
			return
		}
		step := clampIdx(speed.Get()) + 1 // 1..3 points per tick
		r := float64(step)*28 + 14*math.Sin(float64(tick)/3)
		rates.Set(appendCapped(rates.Get(), r, 120))
		if v := pct.Get() + step; v >= 100 {
			pct.Set(100)
			running.Set(false)
			status.Set("done")
			log.Set("finished — restart from the menu (alt+j) or ctrl+s")
			toast("download complete")
		} else {
			pct.Set(v)
		}
	}

	ctx = &markup.Context{
		Values: map[string]any{
			"Pct": pct, "Running": running, "Speed": speed, "Rates": rates,
			"Status": status, "Log": log, "Clock": clock,
			"Advance":   gooey.Command(advance),
			"TickClock": gooey.Command(func() { clock.Set(time.Now().Format("15:04:05")) }),
			// The Toggle already flipped Running by the time this runs —
			// Changed is an after-the-fact notification, not the setter.
			"RunChanged": gooey.Command(func() {
				if running.Get() {
					status.Set("downloading")
					log.Set("resumed")
				} else {
					status.Set("paused")
					log.Set("paused — the spinner parks, and the sparkline stops growing")
				}
			}),
			"SpeedChanged": gooey.Command(func() {
				log.Set("speed → " + speeds[clampIdx(speed.Get())])
			}),
			"Start": gooey.Command(func() {
				pct.Set(0)
				rates.Set(nil)
				running.Set(true)
				status.Set("downloading")
				log.Set("restarted")
			}),
			"Notify": gooey.Command(func() { toast("status: " + status.Get()) }),
			"Quit":   gooey.Command(func() { app.Quit() }),
		},
		Styles: map[string]render.Style{
			"accent": {Fg: render.RGB(255, 170, 60), Bold: true},
			"dim":    {Fg: render.RGB(140, 140, 150)},
		},
	}

	app = gooey.NewApp(markup.Page(os.DirFS("."), "app.gooey", ctx))
	if err := app.Run(context.Background()); err != nil {
		gooey.Exit(err)
	}
}

// appendCapped appends v and drops the oldest entries past n. It copies
// rather than appending in place, so the slice stored in the property
// never shares a backing array with the one it replaces.
func appendCapped(s []float64, v float64, n int) []float64 {
	out := make([]float64, 0, len(s)+1)
	out = append(out, s...)
	out = append(out, v)
	if len(out) > n {
		out = out[len(out)-n:]
	}
	return out
}

func clampIdx(i int) int {
	if i < 0 {
		return 0
	}
	if i >= len(speeds) {
		return len(speeds) - 1
	}
	return i
}
