package main

// The Scene component and the Show that owns it.
//
// Scene is a leaf that paints a framebuffer, and it is the only thing in
// this app that ever repaints. That is worth stating plainly because it
// is the claim gooey makes and this is the loudest possible test of it:
// thirty frames a second, every cell in the raster changing, and the
// border around it, the title on it and the help line under it are all
// clean nodes that the composer skips. Nothing declared that. Scene's
// Render reads `frame` and theirs do not.

import (
	"image"
	"strings"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/graphics"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
)

// A framebuffer smaller than this is not a demo, it is a progress bar.
const (
	minCols = 20
	minRows = 6
)

type Scene struct {
	gooey.Base
	gooey.FocusState

	show *Show

	// buf is reused across frames. Reallocating a framebuffer thirty
	// times a second is the kind of thing that shows up as a stutter and
	// gets blamed on the terminal.
	buf  *image.RGBA
	w, h int
}

func (s *Scene) Measure(avail gooey.Size) gooey.Size {
	return gooey.Size{W: fit(avail.W, 80, minCols), H: fit(avail.H, 24, minRows)}
}

// Render draws the current effect at the current frame.
//
// The frame counter is read FIRST and unconditionally. A Get behind an
// early return drops out of the dependency set on the frames where it
// does not run, and the component goes deaf — no error, no panic, a
// still picture. Hoisting is the whole discipline.
func (s *Scene) Render(f *gooey.Frame) {
	t := s.show.frame.Get()
	which := s.show.effect.Get()
	msg := s.show.message.Get()

	b := s.Bounds()
	if b.W <= 0 || b.H <= 0 {
		return
	}
	// The framebuffer is the cell rect at DOUBLE height: every cell is
	// one '▀' whose foreground is its top pixel and background its
	// bottom.
	if s.buf == nil || s.w != b.W || s.h != b.H*2 {
		s.w, s.h = b.W, b.H*2
		s.buf = image.NewRGBA(image.Rect(0, 0, s.w, s.h))
	}

	all := effects()
	all[clamp(which, 0, len(all)-1)].Draw(s.buf, t)
	if msg != "" {
		scroller(s.buf, msg, t)
	}
	graphics.DrawHalfblock(f.Cells, s.buf, b.X, b.Y, b.W, b.H)
}

// HandleKey is here rather than in a KeyBinding because the Scene is the
// focus stop and these keys belong to it. The deck-level keys — quit,
// help — are KeyBindings in scene.gooey, where a reader can see them.
func (s *Scene) HandleKey(ev input.KeyEvent) bool {
	switch {
	case ev.Key == input.KeyRight, ev.Key == input.KeyRune && ev.Rune == 'n':
		s.show.Next()
		return true
	case ev.Key == input.KeyLeft, ev.Key == input.KeyRune && ev.Rune == 'p':
		s.show.Prev()
		return true
	}
	return false
}

func fit(avail, pref, floor int) int {
	n := avail
	if n <= 0 || n > 1<<12 {
		n = pref
	}
	if n < floor {
		n = floor
	}
	return n
}

func clamp(n, lo, hi int) int {
	if hi < lo {
		return lo
	}
	if n < lo {
		return lo
	}
	if n > hi {
		return hi
	}
	return n
}

// RegisterScene grants the markup a <Scene> element. It is one map entry
// on the context rather than an addition to the framework's vocabulary,
// which is the seam an example is supposed to use.
func RegisterScene(ctx *markup.Context, show *Show) {
	if ctx.Components == nil {
		ctx.Components = map[string]markup.Builder{}
	}
	ctx.Components["Scene"] = func(e markup.Element, c *markup.Context) (gooey.Component, error) {
		return &Scene{show: show}, nil
	}
}

func names() string {
	all := effects()
	out := make([]string, len(all))
	for i, e := range all {
		out[i] = e.Name
	}
	return strings.Join(out, " · ")
}

var _ = prop.NewSource[int]

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
