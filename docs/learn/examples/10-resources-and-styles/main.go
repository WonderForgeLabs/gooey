// Chapter 10 — scope resources and theme with styles: a page-level
// <Resource> two sibling panes both point at through a style named
// "accented", one subtree that shadows the resource AND the style with
// its own, and a runtime Set that proves the shadow is a genuinely
// different property — cycling the page's accent repaints only the
// ambient pane's two Texts, never the overridden pane's.
//
//	cd docs/learn/examples/10-resources-and-styles && go run .
//
// Walkthrough: docs/learn/10-resources-and-styles.md
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

// palette is every color the PAGE's "accent" resource cycles through.
// The overridden pane declares its own <Resource Key="accent"> inside
// its own <Border.Resources>, so it is a different *prop.Property
// entirely — nothing here ever reaches it.
var palette = []render.Color{
	render.RGB(0xff, 0xaa, 0x3c), // amber — app.gooey's starting value
	render.RGB(0xff, 0x5c, 0x8a), // rose
	render.RGB(0x5c, 0xa8, 0xff), // sky
}

func main() {
	var app *gooey.App
	var ctx *markup.Context

	idx := 0
	report := prop.NewSource("press a to cycle the page accent, then m to see what repainted")

	// cycle looks the handle up by name each time rather than capturing
	// it once, the same reason the reader demo looks up its ToastHost
	// per fire: Context.Resource serves the DOCUMENT scope of the last
	// build, and a hot-reload swap would leave a captured handle stale.
	cycle := func() {
		h, ok := ctx.Resource("accent").(*prop.Property[render.Color])
		if !ok {
			return
		}
		idx = (idx + 1) % len(palette)
		h.Set(palette[idx])
	}

	// measure samples the PREVIOUS frame's damage count — press a, then
	// m, the same two-step pattern Tutorial 3 uses, because Set only
	// marks dirty and the repaint happens on the next Frame the App
	// runs, not synchronously inside the command that called Set.
	measure := func() {
		report.Set(fmt.Sprintf(
			"last frame painted %d component(s) — the ambient pane's two Texts, never the overridden pane's",
			app.PaintedLastFrame()))
	}

	ctx = &markup.Context{
		Values: map[string]any{
			"Report":  report,
			"Cycle":   gooey.Command(cycle),
			"Measure": gooey.Command(measure),
			"Quit":    gooey.Command(func() { app.Quit() }),
		},
	}

	app = gooey.NewApp(markup.Page(os.DirFS("."), "app.gooey", ctx))
	if err := app.Run(context.Background()); err != nil {
		gooey.Exit(err)
	}
}
