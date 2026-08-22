// markuplog is logview authored in markup: the UI loads from
// logview.gooey, bindings resolve against the same property-graph
// viewmodel, and the file is hot-reloaded — edit it while the app runs
// and the tree rebuilds in place with all state (log buffer, pause,
// filter) intact, because the viewmodel properties are the durable
// thing and the tree is disposable.
//
// It is the MARKUP flavor of cmd/logview, which composes the same
// viewmodel as a Go tree. Here even the key surface is declarative:
// space/f/q are <KeyBinding> elements bound to viewmodel commands, so
// this file holds no key handling at all — events arrive decoded and
// Composer.Handle routes them.
//
// The viewmodel below is a deliberate copy of cmd/logview's: the two
// files exist to be read side by side, and DRYing that out would delete
// the comparison. The synthetic traffic is shared instead
// (cmd/internal/logdata) — it is random test data, not a lesson.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/cmd/internal/logdata"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

func main() {
	path := "cmd/markuplog/logview.gooey"
	if len(os.Args) > 1 {
		path = os.Args[1]
	}
	path, _ = filepath.Abs(path)

	// --- viewmodel: identical to cmd/logview ---
	lines := prop.NewSource([]logdata.Line{})
	frozen := prop.NewSource([]logdata.Line{})
	follow := prop.NewSource(true)
	filter := prop.NewSource("")

	visible := prop.NewComputed(func() []logdata.Line {
		var src []logdata.Line
		if follow.Get() {
			src = lines.Get()
		} else {
			src = frozen.Get()
		}
		if f := filter.Get(); f != "" {
			kept := make([]logdata.Line, 0, len(src))
			for _, l := range src {
				if l.Level == f {
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

	// --- commands: the viewmodel side of the declarative key surface.
	// Each <KeyBinding Command="{{.X}}"/> in logview.gooey resolves to
	// one of these funcs at load time — a handle, not a name lookup.
	var app *gooey.App
	togglePause := gooey.Command(func() {
		if follow.Get() {
			frozen.Set(lines.Get())
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

	// --- markup context: the binding registry ---
	ctx := &markup.Context{
		Values: map[string]any{
			"Header": header, "Visible": visible,
			"TogglePause": togglePause,
			"CycleFilter": cycleFilter,
			"Quit":        gooey.Command(func() { app.Quit() }),
		},
		Styles: map[string]render.Style{
			"panel":  {Fg: render.RGB(120, 90, 220)},
			"accent": {Fg: render.RGB(255, 170, 60), Bold: true},
			"dim":    {Fg: render.RGB(140, 140, 150)},
		},
		Components: map[string]markup.Builder{
			// Lines="{{.Visible}}" crosses into the component as a typed
			// property handle, the same hand-off a UserControl attribute
			// makes — so the markup names the dependency instead of the
			// builder closing over it silently.
			"LogPane": func(e markup.Element, c *markup.Context) (gooey.Component, error) {
				v, err := c.BindingValue(e.Attrs["Lines"])
				if err != nil {
					return nil, fmt.Errorf("LogPane Lines: %w", err)
				}
				src, ok := v.(*prop.Property[[]logdata.Line])
				if !ok {
					return nil, fmt.Errorf("LogPane Lines: got %T, want *prop.Property[[]logdata.Line]", v)
				}
				return &logPane{src: src}, nil
			},
		},
	}

	// lastErr carries the most recent hot-reload failure into the stats
	// line. A broken markup file used to fail silently — the UI simply
	// stopped changing — which is the worst possible feedback to get
	// while editing one.
	var lastErr string

	// fs.FS is the seam: os.DirFS here for hot reload; an embed.FS
	// would drop in unchanged (watching degrades to a no-op there).
	app = gooey.NewApp(markup.Page(os.DirFS(filepath.Dir(path)), filepath.Base(path), ctx),
		gooey.WithErrorHandler(func(err error) { lastErr = "   reload failed: " + err.Error() }))

	// A hot reload builds NEW components, so anything resolved by name has
	// to be resolved again — the handle from before the swap points at a
	// composition that is no longer on screen. This hook fires for the
	// first attach too, so there is one place that does it.
	var stats *components.Text
	reloads := -1 // the initial attach is not a reload
	app.OnSwap(func(gooey.Component) {
		reloads++
		stats, _ = markup.Find[*components.Text](ctx, "stats")
	})

	// The log generator is the app's own clock: it must keep producing
	// lines across a hot reload, so it belongs to the App rather than to
	// the tree (a <Timer> would be replaced along with the tree).
	app.Every(130*time.Millisecond, func() { lines.Set(append(lines.Get(), logdata.Next())) })

	app.BeforeFrame(func() {
		if stats == nil || stats.Content == nil {
			return
		}
		stats.Content.Set(fmt.Sprintf("lines arrived=%d   frames=%d   hot reloads=%d%s",
			logdata.Count(), app.Frames(), reloads, lastErr))
	})

	// Same shape as cmd/logview's ending on purpose. The two are the same
	// app built both ways, and docs/learn/howto/howto-keybindings.md tells
	// readers to diff them — so every line that differs for a reason other
	// than Go-vs-markup is noise in the comparison they were told to make.
	gooey.Exit(app.Run(context.Background()))
}

type logPane struct {
	gooey.Base
	src *prop.Property[[]logdata.Line]
}

func (p *logPane) Measure(avail gooey.Size) gooey.Size { return avail }

// levelStyles is package-level because Render is a paint node: building
// the map inside it allocates on every repaint of the pane.
var levelStyles = map[string]render.Style{
	"ERROR": {Fg: render.RGB(240, 90, 90), Bold: true},
	"WARN":  {Fg: render.RGB(230, 190, 80)},
	"INFO":  {},
	"DEBUG": {Fg: render.RGB(120, 120, 130)},
}

func (p *logPane) Render(f *gooey.Frame) {
	b := p.Bounds()
	ls := p.src.Get()
	if len(ls) > b.H {
		ls = ls[len(ls)-b.H:]
	}
	for i, l := range ls {
		s := fmt.Sprintf("%-5s %s", l.Level, l.Text)
		if len(s) > b.W {
			s = s[:b.W]
		}
		f.Cells.SetString(b.X, b.Y+i, s, levelStyles[l.Level])
	}
}
