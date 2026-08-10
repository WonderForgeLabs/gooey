// propdemo shows the dependency-property graph driving the retained
// tree: the whole scene is one computed property, so frames render only
// when something the UI actually read has changed.
//
// Keys:
//
//	a / b   bump source a / b
//	m       toggle which source the "detail" computed watches
//	q       quit
//
// The proof to watch for: with mode=a, hammering 'b' renders NOTHING —
// the events counter catches up only at the next 1 Hz tick, because b
// is not a recorded dependency of the scene. Toggle 'm' and the roles
// swap instantly.
//
// The keys are <KeyBinding> declarations in propdemo.gooey bound to
// viewmodel commands, so this file has no key switch at all: input
// arrives as one decoded event stream and Composer.Handle routes it.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

func main() {
	// --- viewmodel: source and computed properties ---
	count := prop.NewSource(0)
	mode := prop.NewSource("a")
	a := prop.NewSource(0)
	b := prop.NewSource(0)
	stats := prop.NewSource("")

	detail := prop.NewComputed(func() string {
		if mode.Get() == "a" {
			return fmt.Sprintf("watching a = %d   (b is invisible to me)", a.Get())
		}
		return fmt.Sprintf("watching b = %d   (a is invisible to me)", b.Get())
	})
	countLabel := prop.NewComputed(func() string {
		return fmt.Sprintf("count (ticks 1/s) : %d", count.Get())
	})
	modeLabel := prop.NewComputed(func() string {
		return fmt.Sprintf("mode              : %s", mode.Get())
	})

	var app *gooey.App
	ctx := &markup.Context{
		Values: map[string]any{
			"CountLabel": countLabel, "ModeLabel": modeLabel,
			"Detail": detail, "Stats": stats,
			// Commands are the whole event surface. Bumping the unwatched
			// source still runs its command and still Sets the property —
			// what it does not do is dirty anything the scene read.
			"BumpA": gooey.Command(func() { a.Set(a.Get() + 1) }),
			"BumpB": gooey.Command(func() { b.Set(b.Get() + 1) }),
			"ToggleMode": gooey.Command(func() {
				if mode.Get() == "a" {
					mode.Set("b")
				} else {
					mode.Set("a")
				}
			}),
			"Quit": gooey.Command(func() { app.Quit() }),
		},
		Styles: map[string]render.Style{
			"panel":  {Fg: render.RGB(120, 90, 220)},
			"accent": {Fg: render.RGB(255, 170, 60), Bold: true},
			"dim":    {Fg: render.RGB(140, 140, 150)},
		},
	}

	dir := "cmd/propdemo"
	if _, err := os.Stat(filepath.Join(dir, "propdemo.gooey")); err != nil {
		exe, _ := os.Executable()
		dir = filepath.Dir(exe)
	}

	// The whole runtime: the App owns the terminal, the decoder, the
	// frame loop and the signal story; the page is content it builds and
	// rebuilds. Everything below is this demo's own behavior.
	app = gooey.NewApp(markup.Page(os.DirFS(dir), "propdemo.gooey", ctx))

	events := 0
	app.OnEvent(func(ev input.Event) {
		if ev.IsKey() {
			events++
		}
	})
	// The 1 Hz tick is the app's clock, not the tree's, so it runs
	// through the dispatcher rather than as a <Timer>: it must keep
	// ticking across a hot reload.
	app.Every(time.Second, func() { count.Set(count.Get() + 1) })
	// Stats describe the PREVIOUS frame, and setting them here folds
	// their repaint into the frame about to happen.
	app.BeforeFrame(func() {
		stats.Set(fmt.Sprintf("events=%d  frames=%d  detail evals=%d  widgets painted last frame=%d",
			events, app.Frames(), detail.Evals(), app.PaintedLastFrame()))
	})

	if err := app.Run(context.Background()); err != nil {
		gooey.Exit(err)
	}
}
