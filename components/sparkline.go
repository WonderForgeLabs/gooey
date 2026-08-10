package components

import (
	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

var sparkRunes = []rune(" ▁▂▃▄▅▆▇█")

// Sparkline plots a series of 0-100 values as stacked block rows, most
// recent on the right, colored per column by the threshold ramp. Rows
// defaults to 1; the series is tail-cropped to the arranged width, so a
// window that shrinks shows recent history rather than compressing it.
//
// Promoted from cmd/sysmon.
type Sparkline struct {
	gooey.Base
	Values *prop.Property[[]float64] // each 0-100
	Rows   int                       // 0 = 1
	Width  int                       // preferred width; 0 = 40
	Style  *prop.Property[render.Style]
}

func (s *Sparkline) rows() int {
	if s.Rows <= 0 {
		return 1
	}
	return s.Rows
}

func (s *Sparkline) Measure(avail gooey.Size) gooey.Size {
	w := s.Width
	if w == 0 {
		w = 40
	}
	return gooey.Size{W: min(w, avail.W), H: min(s.rows(), avail.H)}
}

func (s *Sparkline) Render(f *gooey.Frame) {
	if s.Values == nil {
		return
	}
	b := s.Bounds()
	vs := s.Values.Get()
	if b.W <= 0 || b.H <= 0 {
		return
	}
	if len(vs) > b.W {
		vs = vs[len(vs)-b.W:]
	}
	rows := min(s.rows(), b.H)
	for i, v := range vs {
		v = clampF(v, 0, 100)
		st := thresholdStyle(int(v))
		if s.Style != nil {
			st = s.Style.Get()
		}
		// Split the value across the rows, bottom-up: each row shows the
		// eighth-of-a-cell remainder of the level that falls inside it.
		level := v / 100 * float64(rows)
		for r := 0; r < rows; r++ {
			frac := level - float64(rows-1-r)
			ch := sparkRunes[0]
			if frac >= 1 {
				ch = sparkRunes[8]
			} else if frac > 0 {
				ch = sparkRunes[int(frac*8)]
			}
			f.Cells.Set(b.X+i, b.Y+r, ch, st)
		}
	}
}
