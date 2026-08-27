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
	"github.com/rivo/uniseg"

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

// clipCols truncates s to w display COLUMNS.
//
// Every one of its callers passes a column budget — b.W, or b.X+b.W-x,
// or a Border's inner width — so columns were always the contract. The
// implementation counted RUNES, which is the same number only for ASCII:
// clipCols("世界ab", 3) returned "世界a", five columns into a three-column
// slot, and the overrun landed on whatever was painted next (#358). gooey
// has no clipping at the frame level (#357), so nothing downstream caught
// it.
//
// Renamed rather than fixed in place. A name that says runes over a body
// that counts columns is how the next caller reintroduces the bug, and
// the compiler makes the rename exhaustive.
//
// A wide glyph is never split: if the next cluster would exceed the
// budget, clipping stops before it. That can leave one column unused —
// correct, since half a glyph is not a thing a terminal can draw.
func clipCols(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if render.StringWidth(s) <= w {
		return s
	}
	out, used, rest := make([]byte, 0, len(s)), 0, s
	for len(rest) > 0 {
		cluster, next, cw, _ := uniseg.FirstGraphemeClusterInString(rest, -1)
		if used+cw > w {
			break
		}
		out = append(out, cluster...)
		used += cw
		rest = next
	}
	return string(out)
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
// than the toast's write-then-pad.
//
// This note used to say the two were observationally identical, because
// SetString advanced exactly one cell per rune and the clip counted
// runes, so "pad from the unclipped rune length to the right edge"
// covered precisely the cells the write did not — and it warned that the
// equivalence was a coincidence of that arithmetic, to be re-derived
// whenever the clip rule or the cell advance changed.
//
// BOTH changed, in #358. SetString now advances by display width and
// clipCols clips by columns, so the rune-length pad would leave a wide
// glyph's second column unstyled and stop short of the right edge. The
// warning was right and filling first is what made it a non-event:
// "the whole rectangle carries the banner style" is true by
// construction, not by arithmetic that has to keep agreeing.
func paintBanner(f *gooey.Frame, b gooey.Rect, msg string, st, def render.Style) render.Style {
	if st == (render.Style{}) {
		st = def
	}
	for x := b.X; x < b.X+b.W; x++ {
		f.Cells.Set(x, b.Y, ' ', st)
	}
	f.Cells.SetString(b.X, b.Y, clipCols(" "+msg+" ", b.W), st)
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
