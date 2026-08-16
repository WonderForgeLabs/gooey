package main

// The board: the grid of channels and steps, the meters, and the keys.
//
// It is drawn as CELLS rather than as pixels, unlike the visualiser next
// to it, and the difference is the point. A step grid is a table of
// discrete states — on, off, playing — and a table wants characters:
// they line up, they read at a distance, and they survive a recording
// that only captures the cell plane. The scope beside it is a continuous
// signal, so it gets the halfblock framebuffer.
//
// Same frame, same app, two rendering strategies, chosen by what the
// data is.

import (
	"image"
	"image/color"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/graphics"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/render"
)

type Board struct {
	gooey.Base
	gooey.FocusState

	app *Board2
}

// Board2 is the app state the components share. Named apart from Board
// so the component and the model are never confused for one another in
// a method receiver, which they were for twenty minutes.
type Board2 = App

func (b *Board) Measure(avail gooey.Size) gooey.Size {
	return gooey.Size{W: fit(avail.W, 64, 24), H: len(b.app.mix.Kit()) + 2}
}

func (b *Board) Render(f *gooey.Frame) {
	b.app.rev.Get()
	sel := b.app.sel.Get()
	snap := b.app.snap

	r := b.Bounds()
	head := render.Style{Fg: render.RGB(120, 130, 150)}
	name := render.Style{Fg: render.RGB(206, 212, 224)}
	nameSel := render.Style{Fg: render.RGB(255, 220, 140), Bold: true}
	off := render.Style{Fg: render.RGB(58, 64, 80)}
	on := render.Style{Fg: render.RGB(110, 210, 250), Bold: true}
	now := render.Style{Fg: render.RGB(20, 24, 32), Bg: render.RGB(250, 200, 90), Bold: true}
	muted := render.Style{Fg: render.RGB(200, 90, 90)}

	const nameW = 11
	const gridX = nameW + 3

	// The step ruler, so a bar is countable without counting.
	for s := 0; s < Steps; s++ {
		st := head
		if snap.Playing && s == snap.Step {
			st = now
		}
		lbl := "·"
		if s%4 == 0 {
			lbl = string(rune('1' + s/4))
		}
		f.Cells.SetString(r.X+gridX+s*2, r.Y, lbl, st)
	}
	f.Cells.SetString(r.X, r.Y, "step", head)

	for c, snd := range b.app.mix.Kit() {
		y := r.Y + 1 + c
		if y >= r.Y+r.H {
			break
		}
		ch := snap.Channels[c]

		st := name
		if c == sel {
			st = nameSel
		}
		if ch.Mute {
			st = muted
		}
		f.Cells.SetString(r.X, y, string(snd.Key)+" "+pad(snd.Name, nameW-2), st)

		for s := 0; s < Steps; s++ {
			cell, style := "·", off
			if ch.Steps[s] {
				cell, style = "■", on
			}
			if snap.Playing && s == snap.Step {
				style = now
				if !ch.Steps[s] {
					cell = " "
				}
			}
			f.Cells.SetString(r.X+gridX+s*2, y, cell, style)
		}

		// A four-cell meter per channel, on the right. Four cells is not
		// much, and it is enough: the only question a channel meter has
		// to answer at a glance is "is this one making the noise".
		mx := r.X + gridX + Steps*2 + 2
		for i := 0; i < 4; i++ {
			lit := ch.Meter > float64(i+1)/5
			glyph, mst := "▁", off
			if lit {
				glyph = "█"
				mst = render.Style{Fg: meterColor(i)}
			}
			f.Cells.SetString(mx+i, y, glyph, mst)
		}
		f.Cells.SetString(mx+6, y, gainBar(ch.Gain), render.Style{Fg: render.RGB(130, 140, 158)})
	}
}

func meterColor(i int) render.Color {
	switch i {
	case 3:
		return render.RGB(250, 110, 90)
	case 2:
		return render.RGB(250, 200, 90)
	}
	return render.RGB(110, 210, 150)
}

func gainBar(g float64) string {
	n := int(g / 1.5 * 6)
	out := ""
	for i := 0; i < 6; i++ {
		if i < n {
			out += "▪"
			continue
		}
		out += "·"
	}
	return out
}

func pad(s string, w int) string {
	r := []rune(s)
	if len(r) >= w {
		return string(r[:w])
	}
	for len(r) < w {
		r = append(r, ' ')
	}
	return string(r)
}

// HandleKey owns the pad keys and the step keys. Everything else is
// declined so it bubbles to the KeyBindings, which is where the
// transport lives, visibly, in markup.
func (b *Board) HandleKey(ev input.KeyEvent) bool {
	if ev.Key == input.KeyUp {
		b.app.Move(-1)
		return true
	}
	if ev.Key == input.KeyDown {
		b.app.Move(1)
		return true
	}
	if ev.Key != input.KeyRune || ev.Has(input.ModCtrl) {
		return false
	}
	// 1..8 hit a pad.
	for c, s := range b.app.mix.Kit() {
		if ev.Rune == s.Key {
			b.app.mix.Hit(c)
			b.app.sel.Set(c)
			return true
		}
	}
	// qwertyuiasdfghjk toggles the sixteen steps of the selected channel.
	// Two rows of eight, because sixteen keys in one row runs off the
	// home row and nobody can find step fourteen.
	if i := stepKey(ev.Rune); i >= 0 {
		b.app.mix.ToggleStep(b.app.sel.Get(), i)
		return true
	}
	return false
}

const stepKeys = "qwertyuiasdfghjk"

func stepKey(r rune) int {
	for i, k := range stepKeys {
		if k == r {
			return i
		}
	}
	return -1
}

// Scope is the continuous half of the display: the master output, drawn
// into a framebuffer and blitted as '▀'.
type Scope struct {
	gooey.Base
	app *App

	buf  *image.RGBA
	w, h int
}

func (s *Scope) Measure(avail gooey.Size) gooey.Size {
	return gooey.Size{W: fit(avail.W, 60, 20), H: fit(avail.H, 6, 2)}
}

func (s *Scope) Render(f *gooey.Frame) {
	s.app.rev.Get()

	b := s.Bounds()
	if b.W <= 0 || b.H <= 0 {
		return
	}
	if s.buf == nil || s.w != b.W || s.h != b.H*2 {
		s.w, s.h = b.W, b.H*2
		s.buf = image.NewRGBA(image.Rect(0, 0, s.w, s.h))
	}
	snap := s.app.snap

	for y := 0; y < s.h; y++ {
		for x := 0; x < s.w; x++ {
			s.buf.SetRGBA(x, y, color.RGBA{8, 10, 16, 255})
		}
	}
	mid := s.h / 2
	for x := 0; x < s.w; x++ {
		s.buf.SetRGBA(x, mid, color.RGBA{26, 32, 44, 255})
	}
	prev := -1
	for x := 0; x < s.w; x++ {
		v := snap.Scope[x*ScopeLen/s.w]
		y := mid - int(v*float64(mid-1))
		lo, hi := y, y
		if prev >= 0 {
			lo, hi = min(prev, y), max(prev, y)
		}
		for yy := lo; yy <= hi; yy++ {
			if yy >= 0 && yy < s.h {
				s.buf.SetRGBA(x, yy, color.RGBA{110, 230, 170, 255})
			}
		}
		prev = y
	}
	graphics.DrawHalfblock(f.Cells, s.buf, b.X, b.Y, b.W, b.H)
}

func RegisterBoard(ctx *markup.Context, a *App) {
	if ctx.Components == nil {
		ctx.Components = map[string]markup.Builder{}
	}
	ctx.Components["Board"] = func(e markup.Element, c *markup.Context) (gooey.Component, error) {
		return &Board{app: a}, nil
	}
	ctx.Components["Scope"] = func(e markup.Element, c *markup.Context) (gooey.Component, error) {
		return &Scope{app: a}, nil
	}
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
