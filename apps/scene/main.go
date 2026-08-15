// scene: a demo, in the demoscene sense.
//
//	cd apps/scene && go build -o scene . && ./scene
//
// It exists for one beat of the introduction — the point in the
// chronology where terminal programs stop being tools and start being
// showing off — and it is a real gooey app rather than a recording. The
// chrome is markup. The raster is one component. The animation is a
// Startable posting to the Dispatcher, and the repaint is a property
// read inside a Render.
//
// # Why a terminal can do this at all
//
// Every cell is drawn as '▀' with the top half painted in the
// foreground colour and the bottom half in the background colour. That
// buys two pixels per cell, so an 80×24 window is a 80×48 framebuffer,
// and with 24-bit colour that is enough for a plasma. graphics.DrawHalfblock
// is the framework's own fallback path for images; nothing here is a
// special case.
//
// # What it is demonstrating about gooey
//
// The Scene repaints thirty times a second and the border, title and
// help line beside it repaint never. Nobody wrote that down. The Scene's
// Render reads a frame counter and theirs do not, and reading a property
// while painting IS the damage declaration — so the framework already
// knows which one cell range changed.
package main

import (
	"context"
	"flag"
	"os"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/control"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/mcp"
)

func main() {
	addr := flag.String("mcp", "", "loopback address for an MCP server; empty disables it")
	fps := flag.Int("fps", 30, "frames per second")
	effect := flag.Int("effect", 0, "effect to open on")
	flag.Parse()

	dir := os.DirFS(".")
	show := NewShow(*fps, *effect)

	ctx := show.Context()
	show.app = gooey.NewApp(markup.Page(dir, "scene.gooey", ctx))

	if *addr != "" {
		srv, err := mcp.Serve(show.app, mcp.Options{
			Addr:    *addr,
			Context: ctx,
			Name:    "gooey-scene",
		})
		if err != nil {
			gooey.Exit(err)
		}
		defer srv.Close()
	}
	_ = control.NewService(show.app, ctx)

	if err := show.app.Run(context.Background()); err != nil {
		gooey.Exit(err)
	}
}
