// Tutorial 2 — layout: Grid tracks (Fixed/Auto/Star), the Grid.*
// attached properties, and the universal layout attributes.
//
// The whole tutorial is in app.gooey; the Go side is tutorial 1's, with
// nothing added.
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
		Styles: map[string]render.Style{
			"panel":  {Fg: render.RGB(120, 90, 220)},
			"accent": {Fg: render.RGB(255, 170, 60), Bold: true},
			"dim":    {Fg: render.RGB(140, 140, 150)},
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
