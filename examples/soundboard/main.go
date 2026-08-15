// soundboard: eight channels, a sixteen-step sequencer, and one stereo
// stream out.
//
//	cd examples/soundboard && go build -o soundboard . && ./soundboard
//
// The beat it appears in is the one where a rectangle full of letters
// turns out to be able to make a noise and show you the noise. It is a
// real mixer: eight voices summed in Go with per-channel gain and pan,
// and a single interleaved buffer piped to the sound server.
//
// # What it demonstrates about the framework
//
//   - Two rendering strategies, chosen by what the data IS. The step
//     grid is discrete state, so it is drawn as cells — it lines up, it
//     reads at a distance, and it survives a capture that only records
//     the cell plane. The scope is a continuous signal, so it is drawn
//     as pixels through halfblock (board.go).
//   - <Image> from a HANDLE. The badge is generated at startup and bound
//     as an image.Image, which is the same seam that stops a third party
//     putting a pixel on your screen in examples/store. On a terminal
//     with sixel it is real graphics; on one without, the framework
//     degrades it to halfblocks with no branch in this program.
//   - UI-goroutine confinement under load. A 130 BPM sequencer costs the
//     property graph thirty Sets a second, not eight thousand: the mixer
//     owns its numbers behind a mutex and a Startable copies one
//     Snapshot per frame (app.go).
//
// Audio is raw PCM piped to `pacat`, so the module gains no dependency.
// With no sound server the app still runs, silently, and says so.
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
	bpm := flag.Int("bpm", 128, "tempo")
	play := flag.Bool("play", true, "start the sequencer immediately")
	flag.Parse()

	a := NewApp(*fps, *bpm)
	a.mix.Start()
	defer a.mix.Close()

	ctx := a.Context()
	a.app = gooey.NewApp(markup.Page(os.DirFS("."), "soundboard.gooey", ctx))

	if *addr != "" {
		srv, err := mcp.Serve(a.app, mcp.Options{Addr: *addr, Context: ctx, Name: "gooey-soundboard"})
		if err != nil {
			gooey.Exit(err)
		}
		defer srv.Close()
	}
	_ = control.NewService(a.app, ctx)

	a.app.Post(func() {
		if err := a.mix.Err(); err != nil {
			a.status.Set("silent — " + err.Error())
		}
		if *play {
			a.Play()
		}
	})

	if err := a.app.Run(context.Background()); err != nil {
		gooey.Exit(err)
	}
}
