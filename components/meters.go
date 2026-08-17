package components

import (
	"fmt"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// Shared threshold ramp for the meters. Values are percentages, so the
// thresholds are absolute rather than configurable; an app that wants
// different semantics sets Style and colors it itself.
const (
	ThresholdWarn = 50 // at or above: warn
	ThresholdCrit = 80 // at or above: critical
)

var (
	styleGood = render.Style{Fg: render.RGB(110, 220, 130)}
	styleWarn = render.Style{Fg: render.RGB(230, 190, 80)}
	styleCrit = render.Style{Fg: render.RGB(240, 90, 90), Bold: true}
	styleDim  = render.Style{Fg: render.RGB(140, 140, 150)}
	// styleAccent marks a control's own structure — the edges of a
	// focused Segmented, a ButtonBar's overflow indicator. It is not part
	// of the value ramp above; it says "this is chrome", not "this is
	// how bad the number is".
	styleAccent = render.Style{Fg: render.RGB(120, 170, 250)}
)

// thresholdStyle is the good/warn/crit ramp shared by Gauge and
// Sparkline, so a value means the same color in both.
func thresholdStyle(v int) render.Style {
	switch {
	case v >= ThresholdCrit:
		return styleCrit
	case v >= ThresholdWarn:
		return styleWarn
	}
	return styleGood
}

// The fill meters — Gauge and ProgressBar's determinate mode — share a
// track, a readout and a default size, for the same reason they share the
// ramp above: a percentage should look like the same percentage in both.
// What they do NOT share is how wide the track is, and that stays at the
// call sites: a Gauge reserves a cell of breathing room after its label,
// a ProgressBar spends the whole remainder.
const (
	// meterWidth is the preferred width of a fill meter in cells.
	meterWidth = 34
	// meterReadout is the cells reserved for the trailing " 100%".
	// %3d never widens past three digits here because meterValue clamps
	// to 0-100, so the readout is exactly this many runes, always.
	meterReadout = 5
)

// meterValue reads a 0-100 property, nil-safe and clamped. It is called
// from the component's own Render, so the Get inside it lands in that
// component's paint node and nowhere else.
func meterValue(p *prop.Property[int]) int {
	return clamp(getInt(p), 0, 100)
}

// meterSize is the fill meters' Measure: one row, the preferred width or
// the default, never more than offered.
func meterSize(w int, avail gooey.Size) gooey.Size {
	if w == 0 {
		w = meterWidth
	}
	return gooey.Size{W: min(w, avail.W), H: min(1, avail.H)}
}

// renderFillMeter paints a w-cell track at (x,y): v percent of it filled,
// the rest empty, truncating rather than rounding so the boundary sits
// where every caller already had it. Returns the cells consumed, so the
// readout that follows can position itself.
//
// Passing the same style twice paints a single-colored track, which is
// what Gauge does; ProgressBar dims its empty half.
func renderFillMeter(f *gooey.Frame, x, y, w, v int, fillSt, emptySt render.Style) int {
	if w <= 0 {
		return 0
	}
	fill := v * w / 100
	for i := 0; i < w; i++ {
		r, st := '░', emptySt
		if i < fill {
			r, st = '█', fillSt
		}
		f.Cells.Set(x+i, y, r, st)
	}
	return w
}

// renderMeterReadout writes the trailing percentage at (x,y), clipped to
// w cells — w <= 0 writes nothing, which is the whole guard a caller with
// no room left needs.
func renderMeterReadout(f *gooey.Frame, x, y, w, v int, st render.Style) {
	f.Cells.SetString(x, y, clipRunes(fmt.Sprintf(" %3d%%", v), w), st)
}
