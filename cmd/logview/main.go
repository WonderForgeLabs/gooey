// logview is a non-contrived dependency-property demo: a streaming log
// viewer with pause, in the lnav/k9s mold.
//
//	space   pause / follow
//	f       cycle level filter: all → ERROR → WARN → all
//	q       quit
//
// The property graph does the work a hand-rolled TUI does manually:
// while following, the scene depends on the live line buffer and every
// append renders. Pausing freezes a snapshot and flips a branch — the
// live buffer silently drops out of the dependency graph, so the
// firehose keeps appending with ZERO renders and zero evaluations,
// while the UI stays fully interactive (change the filter mid-pause and
// it re-renders from the frozen snapshot). Resume re-records the live
// dependency and the view catches up in one frame.
package main

import (
	"fmt"
	"math/rand"
	"os"
	"strings"
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
	"github.com/WonderForgeLabs/gooey/term"
)

type line struct {
	level, text string
}

func main() {
	// --- viewmodel ---
	lines := prop.NewSource([]line{})  // the firehose
	frozen := prop.NewSource([]line{}) // snapshot taken on pause
	follow := prop.NewSource(true)
	filter := prop.NewSource("") // "", "ERROR", "WARN"

	visible := prop.NewComputed(func() []line {
		var src []line
		if follow.Get() {
			src = lines.Get() // live: appends invalidate the scene
		} else {
			src = frozen.Get() // paused: appends are invisible here
		}
		if f := filter.Get(); f != "" {
			kept := make([]line, 0, len(src))
			for _, l := range src {
				if l.level == f {
					kept = append(kept, l)
				}
			}
			return kept
		}
		return src
	})

	header := prop.NewComputed(func() string {
		state := "FOLLOW"
		if !follow.Get() {
			state = "PAUSED"
		}
		f := filter.Get()
		if f == "" {
			f = "all"
		}
		return fmt.Sprintf("%s   filter: %-5s   showing %d lines", state, f, len(visible.Get()))
	})

	// --- retained tree ---
	dim := render.Style{Fg: render.RGB(140, 140, 150)}
	statsP := prop.NewSource("")
	pane := &logPane{src: visible}
	root := &gooey.Border{Title: gooey.Str("logview"), Style: gooey.Sty(render.Style{Fg: render.RGB(120, 90, 220)}), Child: &gooey.VStack{Children: []gooey.Widget{
		&gooey.Text{Content: header, Style: gooey.Sty(render.Style{Fg: render.RGB(255, 170, 60), Bold: true})},
		&gooey.Text{Content: gooey.Str("space: pause/follow   f: filter   q: quit"), Style: gooey.Sty(dim)},
		&gooey.Text{Content: statsP, Style: gooey.Sty(dim)},
		pane, // greedy (fills remaining height) — must come last in this VStack
	}}}

	screen, err := term.Open()
	if err != nil {
		fmt.Fprintln(os.Stderr, "no tty:", err)
		os.Exit(1)
	}
	cols, rows := screen.Size()

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

	gen := time.NewTicker(130 * time.Millisecond)
	defer gen.Stop()
	frames, lastPainted := 0, 0

	for {
		if needsFrame {
			frames++
			statsP.Set(fmt.Sprintf("lines arrived=%d   frames rendered=%d   view evals=%d   widgets painted last frame=%d",
				lineCount, frames, visible.Evals(), lastPainted))
			_, lastPainted = comp.Frame()
			comp.Flush(screen.File())
			needsFrame = false
		}
		select {
		case <-gen.C:
			lines.Set(append(lines.Get(), nextLine()))
		case k := <-keys:
			switch k {
			case 'q', 3:
				return
			case ' ':
				if follow.Get() {
					frozen.Set(lines.Get()) // snapshot, then switch branch
					follow.Set(false)
				} else {
					follow.Set(true)
				}
			case 'f':
				switch filter.Get() {
				case "":
					filter.Set("ERROR")
				case "ERROR":
					filter.Set("WARN")
				default:
					filter.Set("")
				}
			}
		}
	}
}

// logPane is a third-party widget: it lives outside the gooey package
// and only implements the Widget interface. Reading src during Render
// is what wires it into the dependency graph.
type logPane struct {
	gooey.Base
	src *prop.Property[[]line]
}

func (p *logPane) Measure(avail gooey.Size) gooey.Size { return avail }

func (p *logPane) Render(f *gooey.Frame) {
	styles := map[string]render.Style{
		"ERROR": {Fg: render.RGB(240, 90, 90), Bold: true},
		"WARN":  {Fg: render.RGB(230, 190, 80)},
		"INFO":  {},
		"DEBUG": {Fg: render.RGB(120, 120, 130)},
	}
	b := p.Bounds()
	ls := p.src.Get()
	if len(ls) > b.H {
		ls = ls[len(ls)-b.H:] // tail
	}
	for i, l := range ls {
		s := fmt.Sprintf("%-5s %s", l.level, l.text)
		if len(s) > b.W {
			s = s[:b.W]
		}
		f.Cells.SetString(b.X, b.Y+i, s, styles[l.level])
	}
}

// --- synthetic but realistic traffic ---

var lineCount int

var services = []string{"api-gateway", "auth", "billing", "search", "notifier"}

func nextLine() line {
	lineCount++
	ts := time.Now().Format("15:04:05.000")
	svc := services[rand.Intn(len(services))]
	switch r := rand.Float64(); {
	case r < 0.08:
		return line{"ERROR", fmt.Sprintf("%s %s: upstream timeout after %dms (attempt %d)", ts, svc, 800+rand.Intn(2200), 1+rand.Intn(3))}
	case r < 0.20:
		return line{"WARN", fmt.Sprintf("%s %s: retrying request, backoff %dms", ts, svc, 50<<rand.Intn(5))}
	case r < 0.35:
		return line{"DEBUG", fmt.Sprintf("%s %s: cache %s key=%s", ts, svc, pick("hit", "miss"), randKey())}
	default:
		return line{"INFO", fmt.Sprintf("%s %s: %s /v1/%s %d %dms", ts, svc, pick("GET", "POST"), pick("users", "orders", "events"), pick(200, 201, 204), 2+rand.Intn(120))}
	}
}

func pick[T any](xs ...T) T { return xs[rand.Intn(len(xs))] }
func randKey() string       { return strings.ToLower(fmt.Sprintf("%x", rand.Intn(1<<24))) }
