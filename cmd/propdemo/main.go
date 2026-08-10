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
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
	"github.com/WonderForgeLabs/gooey/term"
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

	running := true
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
			"Quit": gooey.Command(func() { running = false }),
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
	fsys := os.DirFS(dir)
	tree, err := markup.Load(fsys, "propdemo.gooey", ctx)
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

	// --- damage-tracked composer: each widget's paint is its own
	// graph node; only widgets whose read properties changed repaint.
	needsFrame := true
	var comp *gooey.Composer
	attach := func(w gooey.Widget) {
		comp = gooey.NewComposer(w, cols, rows)
		comp.OnInvalidate(func() { needsFrame = true })
		needsFrame = true
	}
	attach(tree)
	swaps := make(chan gooey.Widget, 1)
	stopWatch := markup.Watch(fsys, "propdemo.gooey", ctx, func(w gooey.Widget) { swaps <- w })
	defer stopWatch()

	if err := screen.Raw(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer screen.Restore()
	screen.EnableMouse()

	evs := make(chan input.Event, 32)
	go term.DecodeEvents(screen, evs)

	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	events, frames, lastPainted := 0, 0, 0

	for running {
		if needsFrame {
			frames++
			// Stats show the PREVIOUS frame's damage count; setting
			// them before Frame() folds the stats repaint into this
			// frame, and clearing needsFrame after consumes it.
			stats.Set(fmt.Sprintf("events=%d  frames=%d  detail evals=%d  widgets painted last frame=%d",
				events, frames, detail.Evals(), lastPainted))
			_, lastPainted = comp.Frame()
			comp.Flush(screen.File())
			needsFrame = false
		}
		select {
		case <-tick.C:
			count.Set(count.Get() + 1)
		case w := <-swaps:
			attach(w)
		case ev := <-evs:
			if ev.IsKey() {
				events++
			}
			comp.Handle(ev)
		}
	}
}
