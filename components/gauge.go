package components

import (
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

func (g *Gauge) value() int { return meterValue(g.Value) }

func (g *Gauge) Measure(avail gooey.Size) gooey.Size { return meterSize(g.Width, avail) }

func (g *Gauge) Render(f *gooey.Frame) {
	b := g.Bounds()
	v := g.value()
	st := thresholdStyle(v)
	if g.Style != nil {
		st = g.Style.Get()
	}
	label := getStr(g.Label)
	// Nothing to paint into: a Visible component inside a Collapsed
	// ancestor is arranged to nothing, and writing a row at b.Y
	// anyway puts cells outside this node's damage rect, where the
	// Composer's sweep will never clean them. The state reads above
	// stay above the guard — the Get-order rule.
	if b.W <= 0 || b.H <= 0 {
		return
	}
	// Reserve the label, a cell of breathing room after it, and the
	// trailing " 100%" readout; whatever is left is bar. A Gauge colors
	// its empty half with the value's own style rather than dimming it,
	// so the track reads as one meter.
	// Written and measured as the SAME string. This used to paint
	// clipCols(label, b.W) and then advance by len([]rune(label)) — the
	// unclipped one — so a label wider than the gauge moved the bar off
	// the right of its own bounds, and a label with any wide glyph in it
	// moved the bar left of where the text actually ended.
	shown := clipCols(label, b.W)
	shownW := render.StringWidth(shown)
	barW := b.W - shownW - meterReadout - 1
	x := b.X
	f.Cells.SetString(x, b.Y, shown, styleDim)
	x += shownW
	x += renderFillMeter(f, x, b.Y, barW, v, st, st)
	renderMeterReadout(f, x, b.Y, b.X+b.W-x, v, st)
}
