// Tutorial 3 — binding and state: sources, computeds, and the rule that
// decides whether a Get is a read or a subscription.
//
//	cd docs/learn/examples/03-binding-and-state && go run .
//
// Walkthrough: docs/learn/03-binding-and-state.md
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

func main() {
	var app *gooey.App

	// --- sources: settable state. Set marks dependents dirty and
	// computes nothing. ---
	count := prop.NewSource(0)
	noisy := prop.NewSource(0)
	watch := prop.NewSource(false)
	report := prop.NewSource("press [ measure ] to sample the graph")

	// --- computeds: derived state. Every Get made DURING this function's
	// evaluation becomes a dependency of the computed. ---
	label := prop.NewComputed(func() string {
		return fmt.Sprintf("count = %d", count.Get())
	})

	// watched re-wires itself: while watch is false it never reads noisy,
	// so noisy.Set reaches nobody and nothing repaints. Flip watch and
	// the next evaluation records noisy as a dependency.
	watched := prop.NewComputed(func() string {
		if watch.Get() {
			return fmt.Sprintf("watching noisy = %d  (its Sets now repaint this line)", noisy.Get())
		}
		return "not watching noisy  (its Sets reach nobody)"
	})

	// measure reads the same properties from OUTSIDE any evaluation, so
	// these Gets subscribe to nothing — same method, opposite meaning,
	// decided purely by the call site.
	measure := func() {
		report.Set(fmt.Sprintf(
			"count=%d noisy=%d watch=%v | evals: label=%d watched=%d | last frame painted %d widget(s)",
			count.Get(), noisy.Get(), watch.Get(),
			// PaintedLastFrame is the damage count of the frame just
			// flushed — an ordinary int on the App, not a property, so
			// reading it here subscribes to nothing.
			label.Evals(), watched.Evals(), app.PaintedLastFrame()))
	}

	ctx := &markup.Context{
		Values: map[string]any{
			"Label":       label,
			"Watched":     watched,
			"Report":      report,
			"Increment":   gooey.Command(func() { count.Set(count.Get() + 1) }),
			"Bump":        gooey.Command(func() { noisy.Set(noisy.Get() + 1) }),
			"ToggleWatch": gooey.Command(func() { watch.Set(!watch.Get()) }),
			"Measure":     gooey.Command(measure),
			"Quit":        gooey.Command(func() { app.Quit() }),
		},
		Styles: map[string]render.Style{
			"panel":  {Fg: render.RGB(120, 90, 220)},
			"accent": {Fg: render.RGB(255, 170, 60), Bold: true},
			"dim":    {Fg: render.RGB(140, 140, 150)},
		},
	}

	// The App is the run loop: it owns the terminal, the input decoder,
	// frame scheduling and the hot-reload swap. markup.Page is its
	// content — it loads "app.gooey" and rebuilds the tree whenever the
	// file changes, with these viewmodel properties carrying the state
	// across the swap.
	app = gooey.NewApp(markup.Page(os.DirFS("."), "app.gooey", ctx))
	if err := app.Run(context.Background()); err != nil {
		gooey.Exit(err)
	}
}
