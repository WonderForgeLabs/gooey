// introtarget: the empty app from beat 3.2, with the door open.
//
//	cd apps/introtarget && go build -o introtarget . && ./introtarget
//
// Part 4 of the introduction is an agent building an interface for a
// program that has none. This is that program. It is apps/intro —
// an app whose whole tree is one empty Text — plus the two lines that
// let something outside the process reach it:
//
//	control.NewService   the state and markup surface
//	mcp.Serve            that surface, over the wire
//
// # Why this is a second binary rather than a flag on apps/intro
//
// Beat 3.2's claim is that sixteen lines is the floor, and the slide
// shows the file. A flag would put an MCP server in the file being read
// out as "there is no user interface in any of them", which is exactly
// the kind of quiet dishonesty the deck is about not doing. It also
// cannot be done at all in that module: `mcp` is a nested module and
// apps/intro is in the root one, so importing it there is a
// doctrine change (CLAUDE.md, "Heavy dependencies live in nested
// modules").
//
// # What this deliberately does NOT declare
//
// No properties, no markup, no components. The narration for beat 4.6
// says "a second ago this program had no concept of a slide, and now it
// does", and that has to be true: everything the agent puts on screen is
// registered at runtime through the control plane, into a process that
// was compiled knowing nothing about it. If a Value were pre-declared
// here to make the demo smoother, the demo would be a lie.
package main

import (
	"context"
	"flag"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/control"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/mcp"
)

func main() {
	addr := flag.String("mcp", "127.0.0.1:7900", "loopback address for the MCP server")
	flag.Parse()

	// An empty context, not a nil one: the agent registers into it, and
	// there has to be something to register into.
	ctx := &markup.Context{Values: map[string]any{}}

	app := gooey.NewApp(gooey.Tree(&components.Text{}))
	_ = control.NewService(app, ctx)

	srv, err := mcp.Serve(app, mcp.Options{Addr: *addr, Context: ctx, Name: "gooey-introtarget"})
	if err != nil {
		gooey.Exit(err)
	}
	defer srv.Close()

	if err := app.Run(context.Background()); err != nil {
		gooey.Exit(err)
	}
}
