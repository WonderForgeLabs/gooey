// Package paint is the bridge between gooey's pixel plane and a real 2D
// graphics library. It is deliberately thin.
//
// # Why there is no Brush type here
//
// Because github.com/fogleman/gg already has one, and everything around
// it: gg.Pattern is a brush, gg.NewLinearGradient and gg.NewRadialGradient
// are gradient brushes, gg.NewSurfacePattern is an image brush, and
// SetLineWidth / SetLineCap / SetLineJoin / SetDash are a pen. Wrapping
// those in a parallel vocabulary of our own would add a layer that can
// only lose expressiveness and go stale.
//
// So a caller draws with gg directly. What this package carries is the
// part gg cannot know about:
//
//   - a canvas measured in TERMINAL CELLS, because a component's size is
//     cells and its art has to be rasterized in the pixels those cells
//     actually occupy;
//   - colour interop with render.Color, so a style and a brush can be the
//     same colour;
//   - Ring, which slices a frame into the four rectangles that are not
//     content — the geometry gooey's placement model requires and gg has
//     no reason to know about;
//   - Fallback, which is how pixel art degrades to the cell plane.
//
// # The markup names follow MAUI
//
// Where these become element attributes, they are spelled as MAUI spells
// them — Stroke, StrokeThickness, StrokeDashArray, StrokeLineCap,
// StrokeLineJoin, Fill, Padding — and brushes are property elements:
//
//	<Panel.Stroke>
//	  <LinearGradientBrush StartPoint="0,0" EndPoint="1,1">
//	    <GradientStop Color="#6c9cff" Offset="0"/>
//	    <GradientStop Color="#242430" Offset="1"/>
//	  </LinearGradientBrush>
//	</Panel.Stroke>
//
// ParseStroke and ParseBrush below are what read those, so every element
// that takes a stroke spells it identically.
package paint

import (
	"fmt"
	"image"
	"image/color"
	"strconv"
	"strings"

	"github.com/fogleman/gg"

	"github.com/WonderForgeLabs/gooey/render"
)

// Canvas is a gg context sized for a rectangle of terminal cells.
//
// The pixel size is cols*cellW by rows*cellH, which is the resolution the
// terminal will actually display — so art drawn here is rasterized at 1:1
// and never resampled. A caller that guesses a size instead gets its work
// scaled by the pixel pipeline, which is the blur that makes terminal
// graphics look like a screenshot of graphics.
func Canvas(cols, rows, cellW, cellH int) (*gg.Context, error) {
	w, h := cols*cellW, rows*cellH
	if w < 1 || h < 1 {
		return nil, fmt.Errorf("paint: canvas of %dx%d cells at %dx%d px per cell is empty "+
			"(an unprobed terminal reports a zero cell size)", cols, rows, cellW, cellH)
	}
	return gg.NewContext(w, h), nil
}

// Color converts a gooey colour to the standard one gg and image/draw
// take. Nothing is lost: both are 8 bits per channel.
func Color(c render.Color) color.Color { return color.RGBA{R: c.R, G: c.G, B: c.B, A: 0xff} }

// FromColor is the reverse, for deriving a cell-plane style from a brush.
func FromColor(c color.Color) render.Color {
	r, g, b, _ := c.RGBA()
	return render.RGB(uint8(r>>8), uint8(g>>8), uint8(b>>8))
}

// Stroke is a pen, spelled the way MAUI spells one. It exists to be
// PARSED — the fields are exactly the attributes an element declares — and
// Apply hands it straight to gg rather than reimplementing any of it.
type Stroke struct {
	// Brush is what the line is painted with: a solid colour, a gradient,
	// an image. Nil means the caller's current colour stands.
	Brush gg.Pattern
	// Thickness is the line width in pixels. MAUI's StrokeThickness.
	Thickness float64
	// Dash is MAUI's StrokeDashArray, in pixels: on, off, on, off…
	Dash []float64
	// Cap and Join are MAUI's StrokeLineCap and StrokeLineJoin.
	Cap  gg.LineCap
	Join gg.LineJoin
	// Fallback is the single colour this stroke becomes on a terminal with
	// no pixel protocol. A gradient has no cell-plane equivalent, so the
	// caller states what it collapses to rather than having a mean of its
	// stops guessed for it.
	Fallback render.Color
}

// Apply sets a gg context's pen from this stroke. Everything here is a
// gg call; the value of the type is that it can be parsed from markup and
// passed around, not that it does anything gg does not.
func (s Stroke) Apply(dc *gg.Context) {
	if s.Brush != nil {
		dc.SetStrokeStyle(s.Brush)
	}
	if s.Thickness > 0 {
		dc.SetLineWidth(s.Thickness)
	}
	dc.SetLineCap(s.Cap)
	dc.SetLineJoin(s.Join)
	if len(s.Dash) > 0 {
		dc.SetDash(s.Dash...)
	} else {
		dc.SetDash() // no arguments clears it
	}
}

// Ring slices a frame into the four rectangles that are NOT its interior:
// the top edge row, the bottom edge row, and the two side columns between
// them.
//
// This is the geometry gooey's placement model requires and the reason it
// lives here rather than in a component. Placements composite OVER the
// cell plane, so an image spanning a pane would bury the pane's own text;
// components.ButtonChrome established the answer for a pill, and a frame
// slices the same way. The interior is never covered by a placement at
// all, so everything inside stays on the cell plane where a terminal draws
// text best.
//
// The slices are SubImage views where the source supports it, so this
// costs four headers rather than four copies.
func Ring(img image.Image, cellW, cellH int) (top, bottom, left, right image.Image) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	return crop(img, 0, 0, w, cellH),
		crop(img, 0, h-cellH, w, cellH),
		crop(img, 0, cellH, cellW, h-2*cellH),
		crop(img, w-cellW, cellH, cellW, h-2*cellH)
}

func crop(img image.Image, x, y, w, h int) image.Image {
	if w < 1 || h < 1 {
		return image.NewRGBA(image.Rect(0, 0, 1, 1))
	}
	type subImager interface{ SubImage(image.Rectangle) image.Image }
	if si, ok := img.(subImager); ok {
		b := img.Bounds()
		return si.SubImage(image.Rect(b.Min.X+x, b.Min.Y+y, b.Min.X+x+w, b.Min.Y+y+h))
	}
	return img
}

// ---- markup parsing, MAUI's spelling ----

// ParseBrush reads a brush from an attribute value. MAUI accepts a colour
// where a brush is expected — Stroke="Red" is a SolidColorBrush — and so
// does this: a hex literal is the common case and needing a property
// element for it would make the simple thing verbose.
//
// Gradients come from property elements instead, because they have
// structure; see ParseGradient.
func ParseBrush(s string) (gg.Pattern, render.Color, error) {
	c, err := ParseColor(s)
	if err != nil {
		return nil, render.Color{}, err
	}
	return gg.NewSolidPattern(Color(c)), c, nil
}

// ParseColor reads #rgb or #rrggbb — the one colour literal gooey's
// markup already has, kept identical here so a Stroke and a Style are
// written the same way.
func ParseColor(s string) (render.Color, error) {
	h := strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(h) == 3 {
		h = string([]byte{h[0], h[0], h[1], h[1], h[2], h[2]})
	}
	if len(h) != 6 {
		return render.Color{}, fmt.Errorf("want #rgb or #rrggbb, got %q", s)
	}
	n, err := strconv.ParseUint(h, 16, 32)
	if err != nil {
		return render.Color{}, fmt.Errorf("want #rgb or #rrggbb, got %q", s)
	}
	return render.RGB(uint8(n>>16), uint8(n>>8), uint8(n)), nil
}

// ParseDashArray reads MAUI's StrokeDashArray: comma or space separated
// lengths, alternating on and off.
func ParseDashArray(s string) ([]float64, error) {
	f := strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' })
	if len(f) == 0 {
		return nil, nil
	}
	out := make([]float64, 0, len(f))
	for _, p := range f {
		v, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
		if err != nil {
			return nil, fmt.Errorf("StrokeDashArray %q: %w", s, err)
		}
		if v < 0 {
			return nil, fmt.Errorf("StrokeDashArray %q: lengths cannot be negative", s)
		}
		out = append(out, v)
	}
	return out, nil
}

// ParseLineCap reads MAUI's StrokeLineCap: Flat, Square, Round. MAUI's
// "Flat" is gg's Butt — the name differs, the geometry does not, and the
// markup uses MAUI's word.
func ParseLineCap(s string) (gg.LineCap, error) {
	switch strings.TrimSpace(s) {
	case "", "Flat", "Butt":
		return gg.LineCapButt, nil
	case "Square":
		return gg.LineCapSquare, nil
	case "Round":
		return gg.LineCapRound, nil
	}
	return 0, fmt.Errorf("StrokeLineCap %q: want Flat, Square or Round", s)
}

// ParseLineJoin reads MAUI's StrokeLineJoin: Miter, Bevel, Round.
//
// gg has no miter join — its LineJoin is Round or Bevel — so Miter is
// accepted and drawn as Bevel rather than rejected. Refusing a name MAUI
// authors reflexively write would be worse than the small visual
// difference at a corner, and silence about it would be worse still,
// which is what this comment is for.
func ParseLineJoin(s string) (gg.LineJoin, error) {
	switch strings.TrimSpace(s) {
	case "", "Miter", "Bevel":
		return gg.LineJoinBevel, nil
	case "Round":
		return gg.LineJoinRound, nil
	}
	return 0, fmt.Errorf("StrokeLineJoin %q: want Miter, Bevel or Round", s)
}

// GradientStop is one stop of a gradient brush — MAUI's <GradientStop
// Color= Offset=/>.
type GradientStop struct {
	Color  render.Color
	Offset float64
}

// LinearGradient builds MAUI's LinearGradientBrush. StartPoint and
// EndPoint are RELATIVE (0..1) as they are in MAUI, so a brush is
// independent of the size of the thing it paints; they are multiplied by
// the target rectangle here.
func LinearGradient(w, h float64, x0, y0, x1, y1 float64, stops []GradientStop) gg.Pattern {
	g := gg.NewLinearGradient(x0*w, y0*h, x1*w, y1*h)
	for _, s := range stops {
		g.AddColorStop(s.Offset, Color(s.Color))
	}
	return g
}

// RadialGradient builds MAUI's RadialGradientBrush: Center and Radius,
// again relative to the target.
func RadialGradient(w, h float64, cx, cy, r float64, stops []GradientStop) gg.Pattern {
	d := w
	if h > d {
		d = h
	}
	g := gg.NewRadialGradient(cx*w, cy*h, 0, cx*w, cy*h, r*d)
	for _, s := range stops {
		g.AddColorStop(s.Offset, Color(s.Color))
	}
	return g
}
