// Package components holds gooey's built-in components: the leaves
// (Text, Button, Checkbox, TextBox, Gauge, Sparkline, ProgressBar,
// Spinner, Toggle, Segmented, ColorPicker, Image), the non-visual
// Timer, and the containers (VStack, HStack, Grid, Border, Canvas,
// ItemsView, StatusBar, ButtonBar, Tabs).
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

// Str, Sty and Col wrap literals as source properties — every visual
// property in the component model is a *prop.Property[T], whether it
// came from a literal, a viewmodel source, or a computed binding.
func Str(s string) *prop.Property[string]             { return prop.NewSource(s) }
func Sty(s render.Style) *prop.Property[render.Style] { return prop.NewSource(s) }
func Col(c render.Color) *prop.Property[render.Color] { return prop.NewSource(c) }

// measuredAt reads a container's per-child measure cache from Arrange,
// tolerating a cache the Measure pass never filled.
//
// Arrange CAN be reached without a Measure: gooey.ArrangeChild sends a
// Collapsed child straight to Arrange at a zero rect so its subtree
// zeroes its bounds, while gooey.MeasureChild returns for the same
// child without ever calling Measure. A container that is Collapsed on
// the frame it first appears — a hidden Tabs page, a
// Visibility="Collapsed" panel — therefore arranges on an empty cache,
// and indexing it blindly panicked. Every child is legitimately zero in
// that state, and the next Measure refills the cache.
func measuredAt(cache []gooey.Size, i int) gooey.Size {
	if i < 0 || i >= len(cache) {
		return gooey.Size{}
	}
	return cache[i]
}

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

func getColor(p *prop.Property[render.Color]) render.Color {
	if p == nil {
		return render.Color{}
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

// paintBanner paints the one-row banner the three overlays share — the
// tooltip's tip, the validation marker's floating message, and a toast:
// the row filled edge to edge in the banner style, with " msg " written
// over it, clipped to the row. It returns the style it actually used, so
// a caller that decorates further (the tooltip's dim gesture hint) can
// derive from the same resolved value.
//
// msg is a PARAMETER, and that is the whole point of the signature. The
// `Get` that produced it is the paint node's SUBSCRIPTION to the text,
// and it has to run in the caller, above the caller's own early returns.
// A helper that read the property itself — after this function's bounds
// check, or after an empty-message check — would drop the dependency
// edge on exactly the frames where the check fails, and the component
// would go deaf to its own text with no error and no panic to find it
// by, just a stale cell (CLAUDE.md, "Dependencies are recorded by the
// `Get` that actually runs"). Taking the already-read string makes that
// mistake unavailable.
//
// def is the style for a caller whose own Style is the zero value; the
// three overlays disagree about that default on purpose (reverse for a
// tip, reverse+bold for a toast, white-on-error-red for a marker), so it
// is asked for rather than assumed.
//
// Fill THEN write, which is the tooltip's and the marker's order rather
// than the toast's write-then-pad. On today's buffer the two are
// observationally identical — render.Buffer.SetString advances exactly
// one cell per rune and clipRunes clips by rune count, so "pad from the
// unclipped rune length to the right edge" covers precisely the cells
// the write did not — but the equivalence is a coincidence of that
// arithmetic, and it is the write-then-pad form that has to be re-derived
// whenever the clip rule or the cell advance changes. Filling first makes
// "the whole rectangle carries the banner style" true by construction.
func paintBanner(f *gooey.Frame, b gooey.Rect, msg string, st, def render.Style) render.Style {
	if st == (render.Style{}) {
		st = def
	}
	for x := b.X; x < b.X+b.W; x++ {
		f.Cells.Set(x, b.Y, ' ', st)
	}
	f.Cells.SetString(b.X, b.Y, clipRunes(" "+msg+" ", b.W), st)
	return st
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
