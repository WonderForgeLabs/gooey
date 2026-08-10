// logview is a non-contrived dependency-property demo: a streaming log
// viewer with pause, in the lnav/k9s mold.
//
//	space   pause / follow
//	f       cycle level filter: all → ERROR → WARN → all
//	j/k ↑/↓ scroll the pane (pageup/pagedown by a screen, end re-follows)
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
//
// This is the Go-COMPOSITION flavor: the tree, its Grid tracks, and the
// KeyBinding attachments are all built as Go literals here. cmd/markuplog
// runs the same viewmodel with the tree authored in XML markup instead,
// and the contrast between the two files is the point of having both.
package main

import (
	"fmt"
	"math/rand"
	"os"
	"strings"
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/input"
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
	scroll := prop.NewSource(0)  // lines back from the tail; 0 = tailing

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

	// --- commands: the same func() values a markup Click= or
	// <KeyBinding Command=…> would resolve to, bound here by hand.
	running := true
	togglePause := gooey.Command(func() {
		if follow.Get() {
			frozen.Set(lines.Get()) // snapshot, then switch branch
			follow.Set(false)
		} else {
			follow.Set(true)
		}
	})
	cycleFilter := gooey.Command(func() {
		switch filter.Get() {
		case "":
			filter.Set("ERROR")
		case "ERROR":
			filter.Set("WARN")
		default:
			filter.Set("")
		}
	})
	quit := gooey.Command(func() { running = false })

	// --- retained tree, composed in Go ---
	dim := render.Style{Fg: render.RGB(140, 140, 150)}
	statsP := prop.NewSource("")
	pane := &logPane{src: visible, scroll: scroll}
	root := &components.Border{
		Title: paneTitle("logview", pane),
		Style: components.Sty(render.Style{Fg: render.RGB(120, 90, 220)}),
		// A star row is what makes the pane greedy — it takes whatever
		// the three Auto rows above it leave. Ordering inside a VStack
		// used to stand in for this, which meant the stats line could be
		// pushed off screen by a taller header.
		Child: &components.Grid{
			Rows: []components.GridLen{components.Auto(), components.Auto(), components.Auto(), components.Star(1)},
			Children: []gooey.Component{
				gooey.L(&components.Text{Content: header, Style: components.Sty(render.Style{Fg: render.RGB(255, 170, 60), Bold: true})}, gooey.Layout{Row: 0}),
				gooey.L(&components.Text{Content: components.Str("space: pause/follow   f: filter   j/k: scroll   q: quit"), Style: components.Sty(dim)}, gooey.Layout{Row: 1}),
				gooey.L(&components.Text{Content: statsP, Style: components.Sty(dim)}, gooey.Layout{Row: 2}),
				gooey.L(pane, gooey.Layout{Row: 3}),
			},
		},
	}
	// KeyBindings are non-visual attachments — the Go spelling of the
	// <KeyBinding> elements in markuplog's logview.gooey. Hung on the
	// root, they are global: dispatch reaches them after the focused
	// pane has declined the key.
	for _, kb := range []*gooey.KeyBinding{
		{Gesture: input.Rune(' '), Command: togglePause},
		{Gesture: input.Rune('f'), Command: cycleFilter},
		{Gesture: input.Rune('q'), Command: quit},
		{Gesture: input.Named(input.KeyEsc), Command: quit},
		{Gesture: input.KeyEvent{Key: input.KeyRune, Rune: 'c', Mods: input.ModCtrl}, Command: quit},
	} {
		root.Attach(kb)
	}

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
	screen.EnableMouse()

	evs := make(chan input.Event, 32)
	go term.DecodeEvents(screen, evs)

	gen := time.NewTicker(130 * time.Millisecond)
	defer gen.Stop()
	frames, lastPainted := 0, 0

	for running {
		if needsFrame {
			frames++
			statsP.Set(fmt.Sprintf("lines arrived=%d   frames rendered=%d   view evals=%d   components painted last frame=%d",
				lineCount, frames, visible.Evals(), lastPainted))
			_, lastPainted = comp.Frame()
			comp.Flush(screen.File())
			needsFrame = false
		}
		select {
		case <-gen.C:
			lines.Set(append(lines.Get(), nextLine()))
		case ev := <-evs:
			comp.Handle(ev)
		}
	}
}

// paneTitle decorates the pane's name with ● while it holds focus.
// Focus is a source property, so this computed makes focus changes
// ordinary damage.
func paneTitle(name string, w interface{ IsFocused() bool }) *prop.Property[string] {
	return prop.NewComputed(func() string {
		if w.IsFocused() {
			return "● " + name
		}
		return name
	})
}

// logPane is a third-party component: it lives outside the gooey package
// and only implements the Component interface. Reading src during Render
// is what wires it into the dependency graph. Embedding FocusState
// makes it a focus stop, which is what lets it own the scroll keys —
// they are handled here, not by a page-wide binding, and the arrows it
// consumes never reach the framework's focus navigation.
type logPane struct {
	gooey.Base
	gooey.FocusState
	src    *prop.Property[[]line]
	scroll *prop.Property[int]
}

func (p *logPane) Measure(avail gooey.Size) gooey.Size { return avail }

// scrollBy runs from a key handler, outside any evaluation — so these
// Gets read values and record no dependencies. The same Get inside
// Render is a subscription. Call site decides.
func (p *logPane) scrollBy(d int) {
	maxOff := max(0, len(p.src.Get())-p.Bounds().H)
	p.scroll.Set(max(0, min(p.scroll.Get()+d, maxOff)))
}

func (p *logPane) HandleKey(ev input.KeyEvent) bool {
	switch ev {
	case input.Rune('k'), input.Named(input.KeyUp):
		p.scrollBy(+1)
	case input.Rune('j'), input.Named(input.KeyDown):
		p.scrollBy(-1)
	case input.Named(input.KeyPageUp):
		p.scrollBy(p.Bounds().H)
	case input.Named(input.KeyPageDown):
		p.scrollBy(-p.Bounds().H)
	case input.Named(input.KeyEnd):
		p.scroll.Set(0)
	default:
		return false
	}
	return true
}

func (p *logPane) HandleMouse(ev input.MouseEvent) bool {
	switch ev.Kind {
	case input.WheelUp:
		p.scrollBy(+3)
	case input.WheelDown:
		p.scrollBy(-3)
	default:
		return false
	}
	return true
}

func (p *logPane) Render(f *gooey.Frame) {
	styles := map[string]render.Style{
		"ERROR": {Fg: render.RGB(240, 90, 90), Bold: true},
		"WARN":  {Fg: render.RGB(230, 190, 80)},
		"INFO":  {},
		"DEBUG": {Fg: render.RGB(120, 120, 130)},
	}
	b := p.Bounds()
	ls := p.src.Get()
	// The window ends `scroll` lines back from the tail. Clamping here
	// is read-only: writing the property during a paint evaluation would
	// dirty this node from inside its own evaluation.
	end := len(ls) - min(p.scroll.Get(), max(0, len(ls)-b.H))
	if start := max(0, end-b.H); start < end {
		ls = ls[start:end]
	} else {
		ls = nil
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
