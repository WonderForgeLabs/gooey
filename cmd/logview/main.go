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
	"context"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
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
	var app *gooey.App
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
	quit := gooey.Command(func() { app.Quit() })

	// --- retained tree, composed in Go ---
	dim := render.Style{Fg: render.RGB(140, 140, 150)}
	statsP := prop.NewSource("")
	// The pane is a components.ItemsView in scroll mode: no Selected
	// binding, a Scroll offset anchored to the tail. What used to be a
	// hand-rolled Render loop is a projection (one line → one row value
	// map) and a template (one Text per row); the windowing, the scroll
	// keys and the wheel are the view's. The conditional dependency that
	// is this demo's point is untouched: the item source reads `visible`,
	// so while paused the live buffer is out of the graph and appends
	// cost zero renders and zero evaluations.
	pane := &components.ItemsView{
		Items:    components.Items(visible, projectLine),
		Scroll:   scroll,
		Template: lineTemplate,
	}
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

	// --- the run loop is the framework's ---
	//
	// gooey.Tree is the Content for a tree built in Go and never
	// replaced: the terminal, the decoder, the dispatcher, the frame
	// scheduling and a teardown that joins the decoder all belong to App
	// from here. cmd/markuplog — the markup flavor of this same
	// viewmodel — has done it this way all along; the two files now
	// differ only where the point of the pairing is, in how the tree is
	// authored.
	app = gooey.NewApp(gooey.Tree(root))

	// The log generator is the app's own clock, not the tree's. A
	// <Timer> component would be the other choice, and is the wrong one
	// here for the same reason it is in markuplog: the firehose has to
	// outlive any one composition.
	app.Every(130*time.Millisecond, func() { lines.Set(append(lines.Get(), nextLine())) })

	// Stats about the PREVIOUS frame, folded into the one about to
	// happen — which is precisely what BeforeFrame is for. Setting a
	// property from AfterFrame would schedule another frame and the app
	// would never settle. App.Frames() has already counted this frame by
	// the time hooks run, and PaintedLastFrame() is still the previous
	// one's damage, so both numbers mean exactly what they did when this
	// block lived inside the hand-rolled loop.
	app.BeforeFrame(func() {
		statsP.Set(fmt.Sprintf("lines arrived=%d   frames rendered=%d   view evals=%d   components painted last frame=%d",
			lineCount, app.Frames(), visible.Evals(), app.PaintedLastFrame()))
	})

	gooey.Exit(app.Run(context.Background()))
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

// The scrolling pane that used to live here is components.ItemsView in
// scroll mode. What remains is the part that was never about scrolling:
// what a LINE is on screen — one projected row and one template.

var levelStyles = map[string]render.Style{
	"ERROR": {Fg: render.RGB(240, 90, 90), Bold: true},
	"WARN":  {Fg: render.RGB(230, 190, 80)},
	"INFO":  {},
	"DEBUG": {Fg: render.RGB(120, 120, 130)},
}

// projectLine is the projection: the visual decisions the old Render
// loop made per line, as row values a template binds.
func projectLine(l line) map[string]any {
	return map[string]any{
		"Text":  fmt.Sprintf("%-5s %s", l.level, l.text),
		"Style": levelStyles[l.level],
	}
}

// lineTemplate is the Go spelling of an <ItemsView.ItemTemplate>: one
// row is one Text bound to the row's live handles.
func lineTemplate(values map[string]any) (gooey.Component, error) {
	text, ok := values["Text"].(*prop.Property[string])
	if !ok {
		return nil, fmt.Errorf("Text is %T", values["Text"])
	}
	style, ok := values["Style"].(*prop.Property[render.Style])
	if !ok {
		return nil, fmt.Errorf("Style is %T", values["Style"])
	}
	return &components.Text{Content: text, Style: style}, nil
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
