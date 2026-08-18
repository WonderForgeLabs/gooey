package main

// App is the state the page binds to, plus the frame clock.
//
// The division is the same one every sampled source in this repo uses:
// the mixer owns its numbers behind a mutex, a Startable copies a
// Snapshot out once a frame, and exactly one property changes. The
// visible consequence is on the status line — a sixteen-step sequencer
// running at 130 BPM costs the property graph thirty Sets a second, not
// eight thousand.

import (
	"image"
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

type App struct {
	app *gooey.App
	mix *Mixer
	fps int

	rev  *prop.Property[int]
	snap Snapshot

	sel    *prop.Property[int]
	bpm    *prop.Property[int]
	status *prop.Property[string]

	// logo is an image handle, not a path. <Image Src> takes a literal
	// path or a handle the app already holds — the same rule that keeps a
	// third party from putting a pixel on your screen in apps/store —
	// and here it means the artwork is generated at startup and never
	// touches the filesystem at all.
	logo *prop.Property[image.Image]
}

func NewApp(fps, bpm int) *App {
	if fps < 5 {
		fps = 5
	}
	if fps > 60 {
		fps = 60
	}
	return &App{
		mix:    NewMixer(bpm),
		fps:    fps,
		rev:    prop.NewSource(0),
		sel:    prop.NewSource(0),
		bpm:    prop.NewSource(bpm),
		status: prop.NewSource(""),
		logo:   prop.NewSource[image.Image](makeLogo(240, 80)),
	}
}

func (a *App) Move(d int) {
	a.sel.Set(clampI(a.sel.Get()+d, 0, len(a.mix.Kit())-1))
}

func (a *App) Play() {
	if a.mix.TogglePlay() {
		a.status.Set("")
		return
	}
	a.status.Set("stopped")
}

func (a *App) Tempo(d int)    { a.bpm.Set(a.mix.Tempo(d)) }
func (a *App) Mute()          { a.mix.ToggleMute(a.sel.Get()) }
func (a *App) Gain(d float64) { a.mix.Gain(a.sel.Get(), d) }
func (a *App) Pan(d float64)  { a.mix.Pan(a.sel.Get(), d) }
func (a *App) Clear()         { a.mix.Clear(); a.status.Set("pattern cleared") }
func (a *App) Quit()          { a.app.Quit() }

// Clock is the frame clock. Nothing else in this program writes to the
// property graph on a timer.
type Clock struct {
	gooey.Base
	app *App
}

func (c *Clock) Measure(gooey.Size) gooey.Size { return gooey.Size{} }
func (c *Clock) Render(*gooey.Frame)           {}

func (c *Clock) Start(post func(func())) (stop func()) {
	done := make(chan struct{})
	stopped := make(chan struct{})
	tick := time.NewTicker(time.Second / time.Duration(c.app.fps))
	go func() {
		defer close(stopped)
		defer tick.Stop()
		for {
			select {
			case <-done:
				return
			case <-tick.C:
				// Taken here, off the UI goroutine, because it takes the
				// mixer's lock — and the UI goroutine must never block on
				// an audio lock. What crosses is a value.
				snap := c.app.mix.Snapshot()
				post(func() {
					c.app.snap = snap
					c.app.rev.Set(c.app.rev.Get() + 1)
				})
			}
		}
	}()
	return func() { close(done); <-stopped }
}

func (a *App) Context() *markup.Context {
	ctx := &markup.Context{
		Values: map[string]any{
			"Bpm": prop.NewComputed(func() string { return itoa(a.bpm.Get()) }),
			"Sel": prop.NewComputed(func() string {
				k := a.mix.Kit()
				return k[clampI(a.sel.Get(), 0, len(k)-1)].Name
			}),
			"Peak": prop.NewComputed(func() int {
				a.rev.Get()
				return int(a.snap.Peak * 100)
			}),
			"Playing": prop.NewComputed(func() bool { a.rev.Get(); return a.snap.Playing }),
			"Stopped": prop.NewComputed(func() bool { a.rev.Get(); return !a.snap.Playing }),
			"Status":  a.status,
			"Logo":    a.logo,

			"Play":     gooey.Command(a.Play),
			"Faster":   gooey.Command(func() { a.Tempo(4) }),
			"Slower":   gooey.Command(func() { a.Tempo(-4) }),
			"Mute":     gooey.Command(a.Mute),
			"Louder":   gooey.Command(func() { a.Gain(0.08) }),
			"Quieter":  gooey.Command(func() { a.Gain(-0.08) }),
			"PanLeft":  gooey.Command(func() { a.Pan(-0.12) }),
			"PanRight": gooey.Command(func() { a.Pan(0.12) }),
			"Clear":    gooey.Command(a.Clear),
			"Quit":     gooey.Command(a.Quit),
		},
		Styles: map[string]render.Style{
			"panel":    {Fg: render.RGB(90, 110, 150)},
			"headline": {Fg: render.RGB(240, 244, 252), Bold: true},
			"dim":      {Fg: render.RGB(118, 126, 142)},
			"hot":      {Fg: render.RGB(255, 190, 90), Bold: true},
			"warn":     {Fg: render.RGB(240, 120, 100), Bold: true},
		},
	}
	RegisterBoard(ctx, a)
	ctx.Components["Clock"] = func(e markup.Element, c *markup.Context) (gooey.Component, error) {
		return &Clock{app: a}, nil
	}
	return ctx
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
