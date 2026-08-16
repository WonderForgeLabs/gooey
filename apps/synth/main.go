// synth: a polyphonic synthesiser with a visualiser, in a terminal.
//
//	cd apps/synth && go build -o synth . && ./synth
//
// It is the last stop on the chronology in the gooey introduction — the
// point where a rectangle full of letters turns out to be able to make
// and show sound — and it is a real instrument rather than an animation
// of one: the samples going to the speaker are the samples being drawn.
//
// # What it demonstrates about the framework
//
//   - UI-goroutine confinement, at the hardest end. The audio goroutine
//     runs at 48 kHz and never touches the property graph; a Startable
//     samples it on a frame clock and does one Set (engine.go, synth.go).
//   - Damage. That one Set repaints the visualiser and the on-screen
//     keyboard, and nothing else — the border, the title and the help
//     line are painted once. Pinned by a damage-count test.
//   - Pixels out of cells. Every bar and the scope trace are drawn into
//     a framebuffer and blitted as '▀', which is two pixels per cell
//     (viz.go).
//   - Dispatch order. Board is the focus stop, so note keys reach it
//     first; everything it declines bubbles to the KeyBindings that are
//     visible in synth.gooey.
//
// # Sound
//
// Output is raw PCM piped to `pacat`, so the audio backend is one exec
// and one io.Writer and the root module gains no dependency. On a
// machine with no sound server the instrument still runs, silently, and
// says so on the status line.
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
	fps := flag.Int("fps", 30, "visualiser frames per second")
	flag.Parse()

	s := NewSynth(*fps)
	s.eng.Start()
	defer s.eng.Close()

	ctx := s.Context()
	s.app = gooey.NewApp(markup.Page(os.DirFS("."), "synth.gooey", ctx))

	if *addr != "" {
		srv, err := mcp.Serve(s.app, mcp.Options{Addr: *addr, Context: ctx, Name: "gooey-synth"})
		if err != nil {
			gooey.Exit(err)
		}
		defer srv.Close()
	}
	_ = control.NewService(s.app, ctx)

	// The engine's failure is reported on the status line rather than
	// being fatal: an instrument you can see and cannot hear is a
	// legitimate thing to be looking at, and on a machine with no sound
	// server it is the only thing available.
	s.app.Post(func() {
		if err := s.eng.Err(); err != nil {
			s.status.Set("silent — " + err.Error())
		}
	})

	if err := s.app.Run(context.Background()); err != nil {
		gooey.Exit(err)
	}
}
