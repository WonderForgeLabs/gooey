// Tutorial 3 — binding and state: sources, computeds, and the rule that
// decides whether a Get is a read or a subscription.
//
//	cd examples/03-binding-and-state && go run .
//
// Walkthrough: docs/learn/03-binding-and-state.md
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
	running := true
	painted := 0 // widgets repainted by the last frame — a plain Go var

	// --- sources: settable state. Set marks dependents dirty and
	// computes nothing. ---
	count := prop.NewSource(0)
	noisy := prop.NewSource(0)
	watch := prop.NewSource(false)
	report := prop.NewSource("press [ measure ] to sample the graph")

	// --- computeds: derived state. Every Get made DURING this function's
	// evaluation becomes a dependency of the computed. ---
	label := prop.NewComputed(func() string {
		return fmt.Sprintf("count = %d", count.Get())
	})

	// watched re-wires itself: while watch is false it never reads noisy,
	// so noisy.Set reaches nobody and nothing repaints. Flip watch and
	// the next evaluation records noisy as a dependency.
	watched := prop.NewComputed(func() string {
		if watch.Get() {
			return fmt.Sprintf("watching noisy = %d  (its Sets now repaint this line)", noisy.Get())
		}
		return "not watching noisy  (its Sets reach nobody)"
	})

	// measure reads the same properties from OUTSIDE any evaluation, so
	// these Gets subscribe to nothing — same method, opposite meaning,
	// decided purely by the call site.
	measure := func() {
		report.Set(fmt.Sprintf(
			"count=%d noisy=%d watch=%v | evals: label=%d watched=%d | last frame painted %d widget(s)",
			count.Get(), noisy.Get(), watch.Get(),
			label.Evals(), watched.Evals(), painted))
	}

	ctx := &markup.Context{
		Values: map[string]any{
			"Label":       label,
			"Watched":     watched,
			"Report":      report,
			"Increment":   gooey.Command(func() { count.Set(count.Get() + 1) }),
			"Bump":        gooey.Command(func() { noisy.Set(noisy.Get() + 1) }),
			"ToggleWatch": gooey.Command(func() { watch.Set(!watch.Get()) }),
			"Measure":     gooey.Command(measure),
			"Quit":        gooey.Command(func() { running = false }),
		},
		Styles: map[string]render.Style{
			"panel":  {Fg: render.RGB(120, 90, 220)},
			"accent": {Fg: render.RGB(255, 170, 60), Bold: true},
			"dim":    {Fg: render.RGB(140, 140, 150)},
		},
	}

	fsys := os.DirFS(".")
	tree, err := markup.Load(fsys, "app.gooey", ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

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
			// painted is a plain var, not a property: writing it here
			// cannot dirty anything, so the frame does not schedule
			// another frame.
			_, painted = comp.Frame()
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
