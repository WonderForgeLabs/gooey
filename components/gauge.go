package components

import (
	"fmt"
	"strings"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// Gauge is a labelled horizontal meter for a 0-100 value, colored by the
// shared threshold ramp. Setting Style overrides the ramp entirely, for
// a gauge whose color should mean something else.
//
// Promoted from cmd/sysmon.
type Gauge struct {
	gooey.Base
	Value *prop.Property[int] // 0-100; clamped on read
	Label *prop.Property[string]
	Style *prop.Property[render.Style] // nil: color by threshold
	Width int                          // preferred width in cells; 0 = 34
}

func (g *Gauge) value() int {
	if g.Value == nil {
		return 0
	}
	return clamp(g.Value.Get(), 0, 100)
}

func (g *Gauge) Measure(avail gooey.Size) gooey.Size {
	w := g.Width
	if w == 0 {
		w = 34
	}
	return gooey.Size{W: min(w, avail.W), H: min(1, avail.H)}
}

func (g *Gauge) Render(f *gooey.Frame) {
	b := g.Bounds()
	v := g.value()
	st := thresholdStyle(v)
	if g.Style != nil {
		st = g.Style.Get()
	}
	label := getStr(g.Label)
	// Reserve the label and the trailing " 100%" readout; whatever is
	// left is bar.
	const readout = 5
	barW := b.W - len([]rune(label)) - readout - 1
	if barW < 0 {
		barW = 0
	}
	fill := v * barW / 100
	var sb strings.Builder
	for i := 0; i < barW; i++ {
		if i < fill {
			sb.WriteRune('█')
		} else {
			sb.WriteRune('░')
		}
	}
	x := b.X
	f.Cells.SetString(x, b.Y, clipRunes(label, b.W), styleDim)
	x += len([]rune(label))
	f.Cells.SetString(x, b.Y, sb.String(), st)
	x += barW
	f.Cells.SetString(x, b.Y, clipRunes(fmt.Sprintf(" %3d%%", v), max(0, b.X+b.W-x)), st)
}
