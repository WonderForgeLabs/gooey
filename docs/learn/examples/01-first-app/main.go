// Tutorial 1 — your first gooey app: one markup file, one viewmodel,
// and gooey.App, with hot reload wired in.
//
// Run it from this directory so os.DirFS(".") finds app.gooey:
//
//	cd docs/learn/examples/01-first-app && go run .
//
// Walkthrough: docs/learn/01-first-app.md
package main

import (
	"context"
	"os"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

func main() {
	// --- viewmodel: the state and the commands markup binds to ---
	var app *gooey.App
	greeting := prop.NewSource("hello, gooey")

	ctx := &markup.Context{
		Values: map[string]any{
			"Greeting": greeting,
			"Quit":     gooey.Command(func() { app.Quit() }),
		},
		Styles: map[string]render.Style{
			"panel":  {Fg: render.RGB(120, 90, 220)},
			"accent": {Fg: render.RGB(255, 170, 60), Bold: true},
			"dim":    {Fg: render.RGB(140, 140, 150)},
		},
	}

	// --- the app ---
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
