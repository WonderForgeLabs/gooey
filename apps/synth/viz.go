package main

// The visualiser: spectrum bars with falling peak caps, and a scope,
// drawn as pixels rather than as cells.
//
// Pixels, because a bar made of cell rows steps in whole characters and
// a twenty-eight band analyser then has about twelve usable heights.
// Every cell here is '▀' — top half foreground, bottom half background —
// which doubles the vertical resolution for free, and graphics.DrawHalfblock
// is the framework's own path for exactly that.
//
// The look is deliberate. Falling peak caps over rising bars, a hot
// gradient from green through amber to red, and an oscilloscope under
// it: that is the shape of the media players everyone had in 1999, and
// it is that shape because it reads correctly at a glance, which is
// still the only thing a meter has to do.

import (
	"image"
	"image/color"
	"math"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/graphics"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

type Viz struct {
	gooey.Base

	synth *Synth

	buf  *image.RGBA
	w, h int

	// caps are the falling peak markers, one per band. They live here
	// rather than in the engine because they are a property of the
	// PICTURE, not of the sound: change the frame rate and they should
	// fall at the same speed in seconds, not in frames — which is why
	// the decay below is scaled by the frame interval.
	caps [Bands]float64
}

func (v *Viz) Measure(avail gooey.Size) gooey.Size {
	return gooey.Size{W: fit(avail.W, 60, 20), H: fit(avail.H, 12, 4)}
}

// Render reads rev — the once-per-frame handle the sampler Sets — and
// that read is the whole subscription. Nothing else on the page reads
// it, so nothing else on the page repaints while this is running.
func (v *Viz) Render(f *gooey.Frame) {
	v.synth.rev.Get()

	b := v.Bounds()
	if b.W <= 0 || b.H <= 0 {
		return
	}
	if v.buf == nil || v.w != b.W || v.h != b.H*2 {
		v.w, v.h = b.W, b.H*2
		v.buf = image.NewRGBA(image.Rect(0, 0, v.w, v.h))
	}
	snap := v.synth.snap

	fillRGBA(v.buf, color.RGBA{8, 10, 16, 255})

	// Two thirds bars, one third scope. The scope is the small one
	// because it is the decoration; the bars are what you actually read.
	split := v.h * 2 / 3
	v.drawBars(snap, split)
	v.drawScope(snap, split, v.h)

	graphics.DrawHalfblock(f.Cells, v.buf, b.X, b.Y, b.W, b.H)
}

func (v *Viz) drawBars(s Snapshot, bottom int) {
	w := v.w
	gap := 1
	bw := (w - gap*(Bands-1)) / Bands
	if bw < 1 {
		bw, gap = 1, 0
	}
	x0 := (w - (bw*Bands + gap*(Bands-1))) / 2

	for i := 0; i < Bands; i++ {
		val := s.Spectrum[i]
		if val > v.caps[i] {
			v.caps[i] = val
		} else {
			// ~0.9 per second at 30 fps. A cap that falls per FRAME
			// changes speed when the frame rate does, which is the classic
			// way a visualiser looks wrong on a fast machine.
			v.caps[i] -= 0.9 / 30
			if v.caps[i] < val {
				v.caps[i] = val
			}
		}

		top := bottom - int(val*float64(bottom))
		x := x0 + i*(bw+gap)
		for y := top; y < bottom; y++ {
			c := heat(1 - float64(y)/float64(bottom))
			for dx := 0; dx < bw; dx++ {
				setPx(v.buf, x+dx, y, c)
			}
		}
		// the cap
		cy := bottom - int(v.caps[i]*float64(bottom)) - 1
		for dx := 0; dx < bw; dx++ {
			setPx(v.buf, x+dx, cy, color.RGBA{230, 236, 248, 255})
		}
	}
}

func (v *Viz) drawScope(s Snapshot, top, bottom int) {
	h := bottom - top
	if h < 2 {
		return
	}
	mid := top + h/2
	for x := 0; x < v.w; x++ {
		setPx(v.buf, x, mid, color.RGBA{28, 34, 48, 255})
	}
	prev := -1
	for x := 0; x < v.w; x++ {
		i := x * ScopeLen / v.w
		y := mid - int(s.Scope[i]*float64(h/2-1))
		if prev >= 0 {
			// Join consecutive samples, or a fast waveform draws as a
			// dotted cloud rather than a trace.
			lo, hi := prev, y
			if lo > hi {
				lo, hi = hi, lo
			}
			for yy := lo; yy <= hi; yy++ {
				setPx(v.buf, x, yy, color.RGBA{110, 230, 170, 255})
			}
		}
		prev = y
	}
}

// heat is the green→amber→red gradient. It is a function of HEIGHT
// rather than of level, so a tall bar is red at the top and green at the
// bottom — the same bar, read two ways.
func heat(t float64) color.RGBA {
	t = clampF(t, 0, 1)
	switch {
	case t < 0.55:
		u := t / 0.55
		return color.RGBA{uint8(60 + 150*u), 220, uint8(90 - 40*u), 255}
	case t < 0.8:
		u := (t - 0.55) / 0.25
		return color.RGBA{uint8(210 + 40*u), uint8(220 - 60*u), 40, 255}
	default:
		u := (t - 0.8) / 0.2
		return color.RGBA{250, uint8(160 - 110*u), uint8(40 + 30*u), 255}
	}
}

func setPx(dst *image.RGBA, x, y int, c color.RGBA) {
	b := dst.Bounds()
	if x < 0 || y < 0 || x >= b.Dx() || y >= b.Dy() {
		return
	}
	dst.SetRGBA(x, y, c)
}

func fillRGBA(dst *image.RGBA, c color.RGBA) {
	b := dst.Bounds()
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			dst.SetRGBA(x, y, c)
		}
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

// Keys is the on-screen keyboard: a two-octave strip that lights up
// under whatever is sounding. It reads the same rev handle, so it is a
// second component that repaints on the frame clock and a third that
// does not.
type Keys struct {
	gooey.Base
	synth *Synth
}

func (k *Keys) Measure(avail gooey.Size) gooey.Size {
	return gooey.Size{W: fit(avail.W, 60, 20), H: 3}
}

func (k *Keys) Render(f *gooey.Frame) {
	k.synth.rev.Get()
	held := k.synth.HeldNotes()

	b := k.Bounds()
	white := render.Style{Fg: render.RGB(200, 206, 220)}
	black := render.Style{Fg: render.RGB(110, 118, 134)}
	lit := render.Style{Fg: render.RGB(20, 24, 32), Bg: render.RGB(120, 230, 180), Bold: true}

	x := b.X
	for _, key := range keyboard {
		st := white
		if key.sharp {
			st = black
		}
		if held[key.note] {
			st = lit
		}
		// Three cells go down starting at x, so the LAST one is x+2 and
		// it has to be inside the pane: x+2 <= b.X+b.W-1. The guard was
		// `x+2 > b.X+b.W`, which allows x+2 == b.X+b.W and puts one cell
		// into whatever is to the right of this component. A leaf that
		// writes outside its bounds is the hardest kind of rendering bug
		// to find, because the cell it lands on belongs to a node that
		// is clean and will not repaint over it.
		if x+2 >= b.X+b.W {
			break
		}
		f.Cells.SetString(x, b.Y, " "+string(key.label)+" ", st)
		f.Cells.SetString(x, b.Y+1, " "+noteName(key.note)+" ",
			render.Style{Fg: render.RGB(96, 104, 120)})
		x += 3
	}
}

func RegisterViz(ctx *markup.Context, s *Synth) {
	if ctx.Components == nil {
		ctx.Components = map[string]markup.Builder{}
	}
	ctx.Components["Viz"] = func(e markup.Element, c *markup.Context) (gooey.Component, error) {
		return &Viz{synth: s}, nil
	}
	ctx.Components["Keys"] = func(e markup.Element, c *markup.Context) (gooey.Component, error) {
		return &Keys{synth: s}, nil
	}
}

var _ = math.Abs
var _ = prop.NewSource[int]
