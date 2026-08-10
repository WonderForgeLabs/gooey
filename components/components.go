// Package components holds gooey's built-in components: the leaves
// (Text, Button, Checkbox, TextBox, Gauge, Sparkline, ProgressBar,
// Spinner, Toggle, Segmented, ColorPicker, Image), the non-visual
// Timer, and the containers (VStack, HStack, Grid, Border, Canvas,
// ItemsView, StatusBar, ButtonBar).
//
// It imports the root package and the root never imports it. That
// direction is the point: these components are ordinary
// gooey.Component implementations embedding gooey.Base, holding no
// privilege an application's own components lack. Delete the whole
// package and the framework still stands — which is the claim this
// split is here to keep honest.
//
// The framework contracts they implement — Component, Container, Base,
// Layout, MeasureChild/ArrangeChild, Frame, FocusState, HoverState,
// Command, Startable — all live in the root package.
//
// Several of these were promoted out of the demos, where each was
// first written and proven: a demo-local component becomes a built-in
// only once its shape has stopped changing, so the demos are the
// design process and this package is the result. Promotion is never a
// copy. Every promoted component gains what a demo one could skip —
// gooey.Base (so it participates in layout, margins, alignment,
// visibility, and the Grid/Canvas attached properties), bindable
// property handles instead of plain fields, and, where it is
// interactive, focus and mouse participation.
package components

import (
	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// Str and Sty wrap literals as source properties — every visual
// property in the component model is a *prop.Property[T], whether it
// came from a literal, a viewmodel source, or a computed binding.
func Str(s string) *prop.Property[string]             { return prop.NewSource(s) }
func Sty(s render.Style) *prop.Property[render.Style] { return prop.NewSource(s) }

func getStr(p *prop.Property[string]) string {
	if p == nil {
		return ""
	}
	return p.Get()
}

func getSty(p *prop.Property[render.Style]) render.Style {
	if p == nil {
		return render.Style{}
	}
	return p.Get()
}

func getInt(p *prop.Property[int]) int {
	if p == nil {
		return 0
	}
	return p.Get()
}

// gapBefore reports whether a gap should be charged before this child.
// A Collapsed child occupies NOTHING — so it must not drag its gap along
// either, and it must not leave a gap behind it. Keying off "is this
// index > 0" charges both; keying off "has a child actually taken space
// yet" is what makes Collapsed mean what it says.
func gapBefore(w gooey.Component, placedAny bool) bool {
	if !placedAny {
		return false
	}
	l := gooey.LayoutOf(w)
	return l == nil || l.Visibility != gooey.Collapsed
}

func collapsed(w gooey.Component) bool {
	l := gooey.LayoutOf(w)
	return l != nil && l.Visibility == gooey.Collapsed
}

func clipRunes(s string, w int) string {
	if w <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= w {
		return s
	}
	return string(r[:w])
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func clampF(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
