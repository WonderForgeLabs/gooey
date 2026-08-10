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
package main

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

type line struct {
	level, text string
}

func main() {
	path := "cmd/markuplog/logview.gooey"
	if len(os.Args) > 1 {
		path = os.Args[1]
	}
	path, _ = filepath.Abs(path)

	// --- viewmodel: identical to cmd/logview ---
	lines := prop.NewSource([]line{})
	frozen := prop.NewSource([]line{})
	follow := prop.NewSource(true)
	filter := prop.NewSource("")

	visible := prop.NewComputed(func() []line {
		var src []line
		if follow.Get() {
			src = lines.Get()
		} else {
			src = frozen.Get()
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
		Widgets: map[string]markup.Builder{
			// Lines="{{.Visible}}" crosses into the widget as a typed
			// property handle, the same hand-off a UserControl attribute
			// makes — so the markup names the dependency instead of the
			// builder closing over it silently.
			"LogPane": func(e markup.Element, c *markup.Context) (gooey.Widget, error) {
				v, err := c.BindingValue(e.Attrs["Lines"])
				if err != nil {
					return nil, fmt.Errorf("LogPane Lines: %w", err)
				}
				src, ok := v.(*prop.Property[[]line])
				if !ok {
					return nil, fmt.Errorf("LogPane Lines: got %T, want *prop.Property[[]line]", v)
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

	// A hot reload builds NEW widgets, so anything resolved by name has
	// to be resolved again — the handle from before the swap points at a
	// composition that is no longer on screen. This hook fires for the
	// first attach too, so there is one place that does it.
	var stats *gooey.Text
	reloads := -1 // the initial attach is not a reload
	app.OnSwap(func(gooey.Widget) {
		reloads++
		stats, _ = markup.Find[*gooey.Text](ctx, "stats")
	})

	// The log generator is the app's own clock: it must keep producing
	// lines across a hot reload, so it belongs to the App rather than to
	// the tree (a <Timer> would be replaced along with the tree).
	app.Every(130*time.Millisecond, func() { lines.Set(append(lines.Get(), nextLine())) })

	app.BeforeFrame(func() {
		if stats == nil || stats.Content == nil {
			return
		}
		stats.Content.Set(fmt.Sprintf("lines arrived=%d   frames=%d   hot reloads=%d%s",
			lineCount, app.Frames(), reloads, lastErr))
	})

	if err := app.Run(context.Background()); err != nil {
		gooey.Exit(err)
	}
}

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
		ls = ls[len(ls)-b.H:]
	}
	for i, l := range ls {
		s := fmt.Sprintf("%-5s %s", l.level, l.text)
		if len(s) > b.W {
			s = s[:b.W]
		}
		f.Cells.SetString(b.X, b.Y+i, s, styles[l.level])
	}
}

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
