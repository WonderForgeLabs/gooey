// Tutorial 2 — layout: Grid tracks (Fixed/Auto/Star), the Grid.*
// attached properties, the universal layout attributes, and the style
// map that colors each track so the structure is visible.
//
// The whole tutorial is in app.gooey; the Go side is tutorial 1's, plus
// the per-panel entries in the Styles map.
//
//	cd docs/learn/examples/02-layout && go run .
//
// Walkthrough: docs/learn/02-layout.md
package main

import (
	"context"
	"os"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/render"
)

func main() {
	var app *gooey.App

	ctx := &markup.Context{
		Values: map[string]any{
			"Quit": gooey.Command(func() { app.Quit() }),
		},
		// Style="name" in app.gooey looks the name up here. One style
		// per track keeps the three columns tellable at a glance. The
		// filled panels' styles carry a Bg matching their Border's
		// Background: cells have no alpha, so text that wants to sit
		// flush on a fill says so in its own style.
		Styles: map[string]render.Style{
			"fixed":  {Fg: render.RGB(110, 170, 255), Bg: render.RGB(0x1c, 0x2b, 0x4a), Bold: true},
			"one":    {Fg: render.RGB(120, 220, 150), Bg: render.RGB(0x1d, 0x3a, 0x2a), Bold: true},
			"two":    {Fg: render.RGB(230, 130, 220), Bold: true},
			"accent": {Fg: render.RGB(255, 170, 60), Bold: true},
			"dim":    {Fg: render.RGB(150, 150, 165)},
		},
	}

	// The App is the run loop: it owns the terminal, the input decoder,
	// frame scheduling and the hot-reload swap. markup.Page is its
	// content — it loads "app.gooey" and rebuilds the tree whenever the
	// file changes, on the UI goroutine, with your viewmodel properties
	// carrying the state across.
	app = gooey.NewApp(markup.Page(os.DirFS("."), "app.gooey", ctx))
	if err := app.Run(context.Background()); err != nil {
		gooey.Exit(err)
	}
}
