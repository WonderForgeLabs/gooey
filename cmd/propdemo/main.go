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
package main

import (
	"fmt"
	"os"
	"time"

	"github.com/WonderForgeLabs/gooey"
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

	// --- retained tree; every visual property is a Property[T] ---
	accent := render.Style{Fg: render.RGB(255, 170, 60), Bold: true}
	dim := render.Style{Fg: render.RGB(140, 140, 150)}
	statsP := prop.NewSource("")
	root := &gooey.Border{Title: gooey.Str("gooey props"), Style: gooey.Sty(render.Style{Fg: render.RGB(120, 90, 220)}), Child: &gooey.VStack{Gap: 1, Children: []gooey.Widget{
		&gooey.Text{Content: gooey.Str("lazy dependency-tracked properties"), Style: gooey.Sty(accent)},
		&gooey.Text{Content: countLabel},
		&gooey.Text{Content: modeLabel},
		&gooey.Text{Content: detail, Style: gooey.Sty(render.Style{Bold: true})},
		&gooey.Text{Content: statsP, Style: gooey.Sty(dim)},
		&gooey.Text{Content: gooey.Str("a/b: bump sources   m: toggle watched   q: quit\nbump the unwatched source: no frame until next tick"), Style: gooey.Sty(dim)},
	}}}

	screen, err := term.Open()
	if err != nil {
		fmt.Fprintln(os.Stderr, "no tty:", err)
		os.Exit(1)
	}
	cols, rows := screen.Size()

	// --- damage-tracked composer: each widget's paint is its own
	// graph node; only widgets whose read properties changed repaint.
	needsFrame := true
	comp := gooey.NewComposer(root, cols, rows)
	comp.OnInvalidate(func() { needsFrame = true })

	if err := screen.Raw(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer screen.Restore()

	keys := make(chan byte, 8)
	go func() {
		buf := make([]byte, 1)
		for {
			if n, err := screen.File().Read(buf); err != nil {
				return
			} else if n > 0 {
				keys <- buf[0]
			}
		}
	}()

	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	events, frames, lastPainted := 0, 0, 0

	for {
		if needsFrame {
			frames++
			// Stats show the PREVIOUS frame's damage count; setting
			// them before Frame() folds the stats repaint into this
			// frame, and clearing needsFrame after consumes it.
			statsP.Set(fmt.Sprintf("events=%d  frames=%d  detail evals=%d  widgets painted last frame=%d",
				events, frames, detail.Evals(), lastPainted))
			_, lastPainted = comp.Frame()
			comp.Flush(screen.File())
			needsFrame = false
		}
		select {
		case <-tick.C:
			count.Set(count.Get() + 1)
		case k := <-keys:
			events++
			switch k {
			case 'q', 3: // q or ctrl-c
				return
			case 'm':
				if mode.Get() == "a" {
					mode.Set("b")
				} else {
					mode.Set("a")
				}
			case 'a':
				a.Set(a.Get() + 1)
			case 'b':
				b.Set(b.Get() + 1)
			}
		}
	}
}
