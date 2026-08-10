// Tutorial 1 — your first gooey app: one markup file, one viewmodel,
// and the host loop, with hot reload wired in.
//
// Run it from this directory so os.DirFS(".") finds app.gooey:
//
//	cd examples/01-first-app && go run .
//
// Walkthrough: docs/learn/01-first-app.md
package main

import (
	"fmt"
	"os"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
	"github.com/WonderForgeLabs/gooey/term"
)

func main() {
	// --- viewmodel: the state and the commands markup binds to ---
	running := true
	greeting := prop.NewSource("hello, gooey")

	ctx := &markup.Context{
		Values: map[string]any{
			"Greeting": greeting,
			"Quit":     gooey.Command(func() { running = false }),
		},
		Styles: map[string]render.Style{
			"panel":  {Fg: render.RGB(120, 90, 220)},
			"accent": {Fg: render.RGB(255, 170, 60), Bold: true},
			"dim":    {Fg: render.RGB(140, 140, 150)},
		},
	}

	// --- load the markup ---
	fsys := os.DirFS(".")
	tree, err := markup.Load(fsys, "app.gooey", ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	// --- the host loop ---
	screen, err := term.Open()
	if err != nil {
		fmt.Fprintln(os.Stderr, "no tty:", err)
		os.Exit(1)
	}
	cols, rows := screen.Size()

	var comp *gooey.Composer
	needsFrame := true
	attach := func(w gooey.Widget) {
		comp = gooey.NewComposer(w, cols, rows)
		comp.OnInvalidate(func() { needsFrame = true })
		needsFrame = true
	}
	attach(tree)

	// Hot reload: the watcher runs on its own goroutine, so it hands the
	// rebuilt tree over a channel and the loop attaches it.
	swaps := make(chan gooey.Widget, 1)
	stopWatch := markup.Watch(fsys, "app.gooey", ctx, func(w gooey.Widget) { swaps <- w })
	defer stopWatch()

	if err := screen.Raw(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer screen.Restore()

	events := make(chan input.Event, 16)
	go term.DecodeEvents(screen, events)

	for running {
		if needsFrame {
			comp.Frame()
			comp.Flush(screen.File())
			needsFrame = false
		}
		select {
		case w := <-swaps:
			attach(w)
		case ev := <-events:
			comp.Handle(ev)
		}
	}
}
