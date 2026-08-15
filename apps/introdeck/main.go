// introdeck: the gooey introduction, presented by gooey.
//
//	cd apps/introdeck && go run .
//
// NARRATION.md is the script, the slide deck and the edit list at once.
// It is parsed at load: a ```speak fence is the audio, a ```screen fence
// is the slide, and a ```gooey fence is a slide built from live
// components. One file, so the words spoken and the words on screen
// cannot drift.
//
// Two modes, one key apart. Presentation (default) is what the camera
// sees. Prompter (t) is what the presenter sees — the spoken words, the
// clock against the beat's target, the stage direction, and whether the
// audio take exists yet.
//
// It is also an MCP server, which is not a flourish: this deck was built
// by an agent with no terminal, so reading the screen back over MCP was
// the only way to know whether any of it looked right.
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
	addr := flag.String("mcp", "127.0.0.1:7777", "loopback address for the MCP server; empty disables it")
	wrap := flag.Int("wrap", 74, "wrap column for prompter text")
	start := flag.Int("beat", 0, "beat index to open on")
	flag.Parse()

	dir := os.DirFS(".")

	beats, err := ParseNarration(dir, "NARRATION.md")
	if err != nil {
		gooey.Exit(err)
	}

	deck, err := NewDeck(dir, beats, NewPlayer("audio"), *wrap, *start)
	if err != nil {
		gooey.Exit(err)
	}

	ctx := deck.Context()
	deck.app = gooey.NewApp(markup.Page(dir, "deck.gooey", ctx))

	// The same service the MCP tools call. The deck patches its own stage
	// through it, so the presentation uses the agent's door rather than a
	// private one.
	deck.svc = control.NewService(deck.app, ctx)

	// The receipt's only honest source. PaintedLastFrame is valid exactly
	// here — inside the hook, describing the frame that just went out.
	deck.app.AfterFrame(deck.publishPainted)

	if *addr != "" {
		srv, err := mcp.Serve(deck.app, mcp.Options{
			Addr:    *addr,
			Context: ctx,
			Name:    "gooey-introdeck",
		})
		if err != nil {
			gooey.Exit(err)
		}
		defer srv.Close()
	}

	// The opening beat is entered the same way every other beat is,
	// rather than being special-cased into the first frame.
	deck.app.Post(func() { deck.GoTo(deck.idx.Get()) })

	if err := deck.app.Run(context.Background()); err != nil {
		gooey.Exit(err)
	}
}
