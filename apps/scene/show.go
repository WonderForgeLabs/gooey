package main

// Show is the state behind the demo: which effect, which frame, what the
// scroller says.
//
// The frame counter is a source property and the clock that advances it
// is a Startable. That pairing is the only legal way to animate: the
// ticker goroutine may not touch the graph, so it posts a closure and the
// UI loop runs it. Everything else follows from the Set — the Scene's
// Render read `frame`, so the Set marks it dirty, so the next composite
// repaints it and nothing else.

import (
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

type Show struct {
	app *gooey.App
	fps int

	frame   *prop.Property[int]
	effect  *prop.Property[int]
	message *prop.Property[string]
	paused  *prop.Property[bool]
}

const banner = "GOOEY - A RETAINED VISUAL TREE FOR THE TERMINAL " +
	"* EVERY CELL YOU SEE MOVING IS ONE COMPONENT REPAINTING * " +
	"THE BORDER AROUND THIS HAS NOT BEEN DRAWN SINCE THE PROGRAM STARTED * " +
	"NOBODY WROTE THAT DOWN ANYWHERE *   "

func NewShow(fps, effect int) *Show {
	if fps < 1 {
		fps = 1
	}
	if fps > 60 {
		fps = 60
	}
	return &Show{
		fps:     fps,
		frame:   prop.NewSource(0),
		effect:  prop.NewSource(clamp(effect, 0, len(effects())-1)),
		message: prop.NewSource(banner),
		paused:  prop.NewSource(false),
	}
}

func (s *Show) Next() { s.effect.Set((s.effect.Get() + 1) % len(effects())) }
func (s *Show) Prev() {
	n := len(effects())
	s.effect.Set((s.effect.Get() - 1 + n) % n)
}

// Pause stops the clock without stopping the goroutine. Stopping the
// goroutine would mean restarting it, and a Startable's stop func is a
// barrier — Close means no further posts, ever — so it is not a pause
// button, it is a shutdown.
func (s *Show) Pause() { s.paused.Set(!s.paused.Get()) }

func (s *Show) Quit() { s.app.Quit() }

// Clock is the animation. It is a Startable so the framework owns its
// lifetime, and its stop func closes AND joins: close alone lets a tick
// that already won its select post after Close, which is the flake this
// repo has fixed three times (components/timer.go:71).
type Clock struct {
	gooey.Base
	show *Show
}

func (c *Clock) Measure(gooey.Size) gooey.Size { return gooey.Size{} }
func (c *Clock) Render(*gooey.Frame)           {}

func (c *Clock) Start(post func(func())) (stop func()) {
	done := make(chan struct{})
	stopped := make(chan struct{})
	tick := time.NewTicker(time.Second / time.Duration(c.show.fps))

	go func() {
		defer close(stopped)
		defer tick.Stop()
		for {
			select {
			case <-done:
				return
			case <-tick.C:
				post(func() {
					if c.show.paused.Get() {
						return
					}
					// Not a compare-and-set: prop.Set does not compare,
					// and here that is the point — every frame is a real
					// change and must invalidate.
					c.show.frame.Set(c.show.frame.Get() + 1)
				})
			}
		}
	}()
	return func() { close(done); <-stopped }
}

func (s *Show) Context() *markup.Context {
	ctx := &markup.Context{
		Values: map[string]any{
			"Effect": prop.NewComputed(func() string {
				all := effects()
				return all[clamp(s.effect.Get(), 0, len(all)-1)].Name
			}),
			"Frame":   prop.NewComputed(func() string { return itoa(s.frame.Get()) }),
			"Names":   prop.NewComputed(func() string { return names() }),
			"Paused":  s.paused,
			"Running": prop.NewComputed(func() bool { return !s.paused.Get() }),
			"Next":    gooey.Command(s.Next),
			"Prev":    gooey.Command(s.Prev),
			"Pause":   gooey.Command(s.Pause),
			"Quit":    gooey.Command(s.Quit),
		},
		Styles: map[string]render.Style{
			"panel":    {Fg: render.RGB(90, 110, 150)},
			"headline": {Fg: render.RGB(240, 244, 252), Bold: true},
			"dim":      {Fg: render.RGB(120, 128, 145)},
			"hot":      {Fg: render.RGB(255, 190, 90), Bold: true},
		},
	}
	RegisterScene(ctx, s)
	ctx.Components["Clock"] = func(e markup.Element, c *markup.Context) (gooey.Component, error) {
		return &Clock{show: s}, nil
	}
	return ctx
}
