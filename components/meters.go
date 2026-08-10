package components

import "github.com/WonderForgeLabs/gooey/render"

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
