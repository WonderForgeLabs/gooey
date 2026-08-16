// Tutorial 4 — input: commands, focus, and KeyBindings that scope
// themselves by where they are declared.
//
//	cd docs/learn/examples/04-input-commands && go run .
//
// Walkthrough: docs/learn/04-input-commands.md
package main

import (
	"context"
	"os"
	"strings"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

func main() {
	var app *gooey.App

	last := prop.NewSource("ready — press tab to move focus")

	// loud is bound to the page's <Checkbox Checked="{{.Loud}}"/> — a
	// built-in, so this whole file contributes nothing but the handle.
	// Two-way in the only sense gooey has: the Checkbox's Render reads
	// this property and its toggle Sets it, so the viewmodel and the
	// component share one property rather than copies kept in sync.
	loud := prop.NewSource(false)

	// status depends on both: toggling the checkbox restyles the line
	// without any command touching it.
	status := prop.NewComputed(func() string {
		s := "last: " + last.Get()
		if loud.Get() {
			return strings.ToUpper(s)
		}
		return s
	})

	say := func(what string) gooey.Command {
		return gooey.Command(func() { last.Set(what) })
	}

	ctx := &markup.Context{
		Values: map[string]any{
			"Status":      status,
			"Loud":        loud,
			"LeftA":       say("left A"),
			"LeftB":       say("left B"),
			"RightA":      say("right A"),
			"LeftScoped":  say("s in the LEFT pane"),
			"RightScoped": say("s in the RIGHT pane"),
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
	// file changes, on the UI goroutine, with your viewmodel properties
	// carrying the state across.
	app = gooey.NewApp(markup.Page(os.DirFS("."), "app.gooey", ctx))
	if err := app.Run(context.Background()); err != nil {
		gooey.Exit(err)
	}
}
