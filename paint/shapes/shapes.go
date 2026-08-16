// Package shapes is paint's markup vocabulary — the elements that let a
// .gooey document draw, instead of a Go program calling gg.
//
// # Why this package exists at all
//
// paint's parsers were, until this package, dead code with tests.
// ParseBrush, ParseColor, ParseDashArray, ParseLineCap, ParseLineJoin,
// LinearGradient and RadialGradient had no caller anywhere in the tree:
// the one real consumer, apps/wysiwyg/components/panel, draws a
// figure it knows at compile time and so uses Canvas, Ring, Color and a
// Stroke literal. paint's doc says of the parsers "every element that
// takes a stroke spells it identically", and there was no element. This
// package is that element.
//
// # Where registration lives, and why it cannot live anywhere else
//
// paint is a nested module (its dependency on fogleman/gg is the whole
// reason — see docs/specs/2026-08-10-pack-distribution.md), so the root
// module cannot import it and no builtin element can ever be named
// <Ellipse>. The seam is markup.Context.Components: the host app
// registers Builder() under whatever names it wants, exactly as
// apps/wysiwyg registers <Panel>. Nothing in core changes, and an
// app that does not want a 2D graphics library does not link one.
//
// The consequence is that this package validates its own vocabulary.
// Context.spec (markup/attrcheck.go:151) returns AttrsKnown=false for
// anything in Components and checkProps exempts it outright, so the
// framework will not catch a misspelled attribute or an unknown property
// element here. The `vocabulary` table below states every element's
// surface and checkVocab sweeps a whole <Figure> subtree against it at
// LOAD time, before any parser runs — see that table's comment for why
// the check is not inside the parsers, which is where it started and
// twice failed.
//
// # One canvas per <Figure>, not one per shape
//
// A shape is not a component. <Rectangle>, <Ellipse>, <Line> and
// <Polyline> are pseudo-elements parsed by their <Figure>, the way <Tab>
// is parsed by its <Tabs>, and for the same reason plus one more: a
// figure is ONE gg canvas, ONE placement and ONE paint node, so shapes
// compose into a single picture and the damage story stays the one the
// framework already gives — a figure repaints when a figure changes, and
// its neighbours do not.
//
// # Geometry is relative, ink is in pixels
//
// Shape coordinates are fractions of the figure's box (0..1), the way
// MAUI's gradient StartPoint and EndPoint are, so a figure is
// resolution-independent and a resize redraws rather than resamples.
// StrokeThickness and CornerRadius are in PIXELS, which is panel's
// insight preserved: fix the art at one size and scale it and stroke
// thickness becomes a function of the figure's size — thin when wide,
// fat when narrow, the tell of a scaled bitmap.
//
// # The cell tier is not a fallback
//
// Without a pixel protocol the figure draws into the SAME cells at a
// nominal cell size and quantizes coverage into block runes. That is
// panel's and ButtonChrome's rule — layout is identical everywhere and
// only the drawing differs — and it is what makes these samples visible
// in an agg capture, which renders the cell plane only.
//
// The cell tier redraws every shape with its BRUSH replaced by that
// brush's declared fallback colour, so a per-shape fallback lands
// per-cell for free. A gradient must declare one: paint's doc says "a
// gradient has no cell-plane equivalent, so the caller states what it
// collapses to rather than having a mean of its stops guessed for it",
// and omitting it is a load error rather than a guess.
//
// Note that this does NOT go through paint.Stroke.Fallback, which this
// paragraph used to claim. The fallback lives on the Brush because a
// shape has two of them and Stroke.Fallback describes only a pen; see
// parseShape for the whole story, and #241 for the field's fate.
//
// The obvious alternative is graphics.DrawHalfblock, which is already
// written, gives twice the vertical resolution and full colour — and is
// wrong here. It discards alpha (graphics/halfblock.go:19 reads three
// channels and drops the fourth) and sets a Bg on every cell, so a
// canvas that is transparent everywhere but its strokes comes out as a
// solid black rectangle. That is right for a photograph, which is what
// components.Image uses it for, and it is exactly the hole-punching
// that Slice="Ring" and gg's transparent-by-default canvas exist to
// avoid. Coverage shading only ever sets Fg, so an uninked cell keeps
// whatever was underneath it.
//
// # What this package deliberately does not do
//
// Shape attributes are LITERALS. That was once forced — the binding
// resolvers were unexported, so Stroke="{{.Ink}}" could not work from
// outside package markup, and rather than reach around it with a second
// binding dialect these elements took literals and the limitation was
// reported upstream. It is no longer forced: markup.Bound,
// markup.BoundText, markup.BoundColor and markup.BoundStyle are
// exported (#266). Adopting them here is unstarted work, not a
// constraint.
package shapes

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/fogleman/gg"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/graphics"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/paint"
	"github.com/WonderForgeLabs/gooey/render"
)

// The nominal cell used to rasterize the CELL tier. There is no real one
// — f.CellW and f.CellH are zero exactly when this path runs — so a size
// has to be chosen, and 8x16 is the common terminal cell and the size
// panel's own tests measure against.
//
// It makes the cell tier an approximation of the pixel tier rather than
// a reduction of it: a 1.5-pixel stroke covers a different fraction of a
// nominal cell than of a real 10x20 one. That is the honest cost of
// having a cell tier at all, and it is stated here rather than left to
// be discovered.
const (
	nominalCellW = 8
	nominalCellH = 16
)

// shades quantize per-cell coverage. Five buckets, because the eye
// cannot read more from a block rune and a sixth would only add
// flicker at the edges of a figure during a resize.
var shades = [...]rune{' ', '░', '▒', '▓', '█'}

// Slice is what a figure does with the image it drew.
type Slice int

const (
	// SliceFull places the whole canvas over the figure's cells. The
	// figure owns every cell it covers.
	SliceFull Slice = iota
	// SliceRing places only the four rectangles that are not the
	// interior, so the interior stays on the cell plane where a terminal
	// draws text best. This is paint.Ring, and it is the reason paint
	// has a Ring at all: placements composite OVER the cells, so a
	// full-bounds image would bury whatever the figure contains.
	SliceRing
)

// Figure is one canvas of line art, sized to its own bounds in cells.
type Figure struct {
	gooey.Base

	Slice  Slice
	Shapes []Shape
	// Child is the optional content the figure draws around. It is
	// arranged into the interior — inset by the ring when the figure
	// slices one — so a <Figure Slice="Ring"> is a frame you can put
	// things in.
	Child gooey.Component

	style  render.Style
	attach []gooey.Component

	// memo is the last raster, and it is a single entry rather than a
	// map. A figure's shapes are fixed at load, so the only thing that
	// changes is its size; one entry serves the steady state and a
	// resize walks through sizes it will not revisit.
	//
	// The key carries the CELL SIZE as well as the size in cells,
	// because panel learned the hard way that it must: 40 cells at 8px
	// and 20 cells at 16px are the same 320-pixel canvas and slice into
	// different rings, so a key written in pixels hands one figure's
	// slices to another.
	//
	// No mutex: Render runs on the UI goroutine, and a Figure is not
	// shared between apps the way panel's Art cache is.
	memo    *raster
	memoKey string
}

// raster is one drawn canvas, plus the ring slices when the figure
// wants them.
type raster struct {
	img                      image.Image
	top, bottom, left, right image.Image
}

// Shape is one drawing operation. It is a value rather than an
// interface because there are four of them and there will not be forty:
// the moment this needs a plugin seam, the honest answer is that the
// document should be an SVG.
type Shape struct {
	Kind ShapeKind

	// Geometry, all of it in fractions of the figure's box.
	X, Y, W, H     float64 // Rectangle
	CX, CY, RX, RY float64 // Ellipse
	X0, Y0, X1, Y1 float64 // Line
	Points         []float64
	Closed         bool

	// CornerRadius is in PIXELS, like StrokeThickness. A radius in
	// fractions would go oval on a non-square figure.
	CornerRadius float64

	// extent is the shape's declared bounding-box measure, copied from
	// its shapeDef at parse time so path() needs no reverse lookup from
	// kind back to the table. nil means an open shape.
	extent func(s Shape, w, h float64) (float64, float64)

	Stroke paint.Stroke
	// StrokeBrush and FillBrush are nil when the shape does not paint
	// that half. They are kept unresolved because a gradient's pattern
	// depends on the canvas size, which is not known at load.
	StrokeBrush *Brush
	FillBrush   *Brush
}

// ShapeKind is which primitive a Shape is.
type ShapeKind int

const (
	KindRectangle ShapeKind = iota
	KindEllipse
	KindLine
	KindPolyline
)

// Brush is a resolved brush declaration: what it paints with at a given
// canvas size, and what it collapses to on the cell plane.
type Brush struct {
	Kind BrushKind
	// Solid is the colour for BrushSolid and also the Fallback of a
	// gradient — one field, because a solid colour IS its own fallback
	// and a second field would let the two disagree.
	Solid render.Color
	// Gradient geometry, relative like MAUI's.
	X0, Y0, X1, Y1 float64 // linear
	CX, CY, R      float64 // radial
	Stops          []paint.GradientStop
}

// BrushKind is which of paint's brush constructors a Brush resolves to.
type BrushKind int

const (
	BrushSolid BrushKind = iota
	BrushLinear
	BrushRadial
)

// Pattern resolves the brush against a canvas of w by h pixels.
func (b *Brush) Pattern(w, h float64) gg.Pattern {
	switch b.Kind {
	case BrushLinear:
		return paint.LinearGradient(w, h, b.X0, b.Y0, b.X1, b.Y1, b.Stops)
	case BrushRadial:
		return paint.RadialGradient(w, h, b.CX, b.CY, b.R, b.Stops)
	}
	return gg.NewSolidPattern(paint.Color(b.Solid))
}

// Flat is the brush as the cell plane sees it: one colour, declared.
func (b *Brush) Flat() gg.Pattern { return gg.NewSolidPattern(paint.Color(b.Solid)) }

// ---- markup ----

// Builder returns the markup.Builder for <Figure>. Register it under
// whatever name the host wants:
//
//	ctx.Components = map[string]markup.Builder{"Figure": shapes.Builder()}
func Builder() markup.Builder {
	return func(e markup.Element, ctx *markup.Context) (gooey.Component, error) {
		return build(e, ctx)
	}
}

// ---- the declared surface, and the one sweep that checks it ----
//
// Every element this package parses states its whole surface here, and
// checkVocab walks a <Figure> subtree against this table BEFORE any
// parser runs. Validation is therefore structural rather than per-parser:
// a parser cannot forget to validate, because no parser validates.
//
// This is not the shape it started in, and the reason is worth keeping.
// The first version checked attributes inside each parser. Review found
// that parseShape checked e.Attrs and not e.Props, so <Rectangle.Fil>
// loaded clean with the brush inside it silently never applied. That was
// fixed by adding a check to parseShape — and review found the SAME bug
// again one level down, in parseBrushElement and parseStops, in the same
// PR. Twice in one change is not carelessness, it is a design that makes
// forgetting the default: each new parser started unvalidated and had to
// remember to opt in.
//
// So the check moved out of the parsers entirely. Adding an element now
// means adding a row here, and a row that is missing fails loudly on the
// first document that uses the element rather than quietly on the first
// document that misspells one of its properties.
//
// It is also the shape markup/elementdef.go argues for in core: each
// element STATES its own surface, in one literal, colocated with the code
// that reads it — rather than the surface being implicit in control flow
// where nothing can check it.
type vocabRow struct {
	// attrs and props are the element's whole surface. A nil props map
	// means the element takes no property elements at all.
	attrs, props map[string]bool
	// propHint is appended to an unknown-property error where a generic
	// message would leave the author guessing. <Figure.Stroke> is the
	// motivating case: it is a reasonable thing to write and the answer
	// is that brushes belong on shapes.
	propHint string
}

var vocabulary = map[string]vocabRow{}

func init() {
	// <Figure> also accepts the universal surface — Name, Width,
	// Grid.Row and the rest — because the framework applies those AFTER
	// this builder returns (markup.build calls applyLayout). Omitting
	// them here would reject every laid-out figure.
	//
	// Attached properties cannot be scoped to the actual parent:
	// Element.parent is unexported, so a registered builder cannot tell
	// whether it sits in a <Grid>. The union is accepted and the
	// framework's own applyLayout does the work; core's narrower check
	// is not reproducible from outside the package.
	figure := set("Slice", "Style")
	for _, a := range markup.UniversalAttrs() {
		figure[a.Name] = true
	}
	for _, p := range markup.AttachedParents() {
		for _, a := range markup.AttachedAttrs(p) {
			figure[a.Name] = true
		}
	}
	vocabulary["Figure"] = vocabRow{
		attrs:    figure,
		props:    set("Behaviors"),
		propHint: "; a brush is a property of the SHAPE that uses it, e.g. <Rectangle.Fill>",
	}

	// The shapes. Each is the shared pen-and-brush surface plus its own
	// geometry, and each takes the two brush slots.
	for name, def := range shapeKinds {
		attrs := set(paintAttrs...)
		for _, a := range def.attrs {
			attrs[a] = true
		}
		vocabulary[name] = vocabRow{attrs: attrs, props: set("Fill", "Stroke")}
	}

	// The brushes and their stops, from the same literals the PARSERS
	// dispatch on. Deriving both from one table is the last hole closed:
	// with two tables, adding a brush to the dispatch and forgetting the
	// vocabulary row would leave that brush silently unvalidated — the
	// same forget-by-default this restructure exists to remove, moved up
	// one level. Measured: deleting a row made its property-element test
	// pass again.
	//
	// None of them takes a property element, and stating that as an
	// empty set rather than by omission is what makes
	// <LinearGradientBrush.StartPoint> a load error.
	for name, def := range brushKinds {
		vocabulary[name] = vocabRow{attrs: set(def.attrs...), props: set()}
	}
	vocabulary[stopElement] = vocabRow{attrs: set("Color", "Offset"), props: set()}
}

func set(names ...string) map[string]bool {
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	return m
}

// checkVocab walks an element and everything below it that this package
// owns, checking each against its declared row.
//
// An element NOT in the table is skipped rather than rejected, and the
// recursion stops there. That is what lets a <Text> or a <VStack> sit
// inside a <Figure> untouched: element identity is the framework's
// business for content, and the parsers' business for brushes, where a
// better error than "unknown element" is available. Nothing is lost — a
// <Rectangle> nested inside a <VStack> is not a shape, so it goes to
// BuildChildren and fails there as an unknown element.
func checkVocab(e markup.Element) error {
	row, known := vocabulary[e.Name]
	if !known {
		return nil
	}
	if err := unknownAttr(e.Name, e.Attrs, row.attrs); err != nil {
		return err
	}
	if err := unknownProp(e, row.props, row.propHint); err != nil {
		return err
	}
	// Property elements first, so a bad brush attribute is reported
	// before a bad child further down the document.
	names := make([]string, 0, len(e.Props))
	for name := range e.Props {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		for _, c := range e.Props[name].Children {
			if err := checkVocab(c); err != nil {
				return err
			}
		}
	}
	for _, c := range e.Children {
		if err := checkVocab(c); err != nil {
			return err
		}
	}
	return nil
}

func build(e markup.Element, ctx *markup.Context) (gooey.Component, error) {
	if err := checkVocab(e); err != nil {
		return nil, err
	}

	f := &Figure{style: ctx.Styles[e.Attrs["Style"]]}
	var err error
	if f.Slice, err = parseSlice(e.Attrs["Slice"]); err != nil {
		return nil, err
	}

	// Shapes are pseudo-elements; everything else is content, and is
	// built through the framework so a <Text> or a <KeyBinding> inside a
	// figure behaves exactly as it does anywhere else.
	var content []markup.Element
	for _, c := range e.Children {
		if _, isShape := shapeKinds[c.Name]; !isShape {
			content = append(content, c)
			continue
		}
		s, err := parseShape(c)
		if err != nil {
			return nil, err
		}
		f.Shapes = append(f.Shapes, s)
	}

	// The children keep the parent name parse stamped on them, so
	// attached-property validation still sees <Figure> as their parent.
	kids, attach, err := markup.BuildChildren(markup.Element{Name: e.Name, Children: content, Props: e.Props}, ctx)
	if err != nil {
		return nil, err
	}
	if len(kids) > 1 {
		return nil, fmt.Errorf("markup: <Figure> takes at most one content child, got %d; wrap them in a layout element", len(kids))
	}
	if len(kids) == 1 {
		f.Child = kids[0]
	}
	f.attach = attach
	return f, nil
}

func parseSlice(raw string) (Slice, error) {
	switch strings.TrimSpace(raw) {
	case "", "Full":
		return SliceFull, nil
	case "Ring":
		return SliceRing, nil
	}
	return 0, fmt.Errorf("markup: <Figure Slice=%q>: want Full or Ring", raw)
}

// shapeKinds is the ONE literal per shape element: its kind, the
// geometry attributes it adds to the shared pen surface, and how to
// measure the extent its pen is inset from. It is also the answer to "is
// this child a shape or content", which is why it is a map and not a
// switch.
//
// The extent field is what stops the degeneracy guard from being
// per-kind. Review found KindRectangle guarding its post-inset size and
// KindEllipse not — the third instance in this PR of a correct treatment
// applied to one branch and not its sibling — so the question is asked
// once, above the switch, and every kind answers it by construction.
//
// The two guards turned out to be the same inequality, which is what
// made this possible rather than merely tidy. The rectangle asked
// `W*w - t <= 0`. The ellipse needs `RX*w - t/2 <= 0`, and that is
// exactly `2*RX*w - t <= 0`. Both are `extent - pen <= 0` where extent
// is the bounding box in that dimension: a closed shape's pen straddles
// its boundary, so it consumes t from each dimension whether that
// dimension is expressed as a side or as a radius.
var shapeKinds = map[string]shapeDef{
	"Rectangle": {KindRectangle, []string{"Rect", "CornerRadius"}, rectExtent},
	"Ellipse":   {KindEllipse, []string{"Center", "RadiusX", "RadiusY"}, ellipseExtent},
	// Line and Polyline are OPEN: the pen is dragged ALONG a path rather
	// than inset from a boundary, so there is no extent for it to
	// consume and no thickness that makes them degenerate. A 200-pixel
	// line is a legitimate 200-pixel line. Polyline stays open even with
	// Closed="true" — closing the path joins its ends, it does not start
	// insetting the stroke.
	"Line":     {KindLine, []string{"From", "To"}, nil},
	"Polyline": {KindPolyline, []string{"Points", "Closed"}, nil},
}

type shapeDef struct {
	kind  ShapeKind
	attrs []string
	// extent measures a CLOSED shape's bounding box in pixels. nil marks
	// an open shape, which is never degenerate.
	extent func(s Shape, w, h float64) (float64, float64)
}

func rectExtent(s Shape, w, h float64) (float64, float64) { return s.W * w, s.H * h }

// ellipseExtent returns DIAMETERS, not radii — that is the whole reason
// one guard covers both shapes.
func ellipseExtent(s Shape, w, h float64) (float64, float64) { return 2 * s.RX * w, 2 * s.RY * h }

// brushKinds is both the brush dispatch table and the source of those
// elements' declared vocabulary — one literal, read by parseBrushElement
// and by init. shapeKinds serves the same double duty above.
var brushKinds = map[string]struct {
	kind  BrushKind
	attrs []string
}{
	"SolidColorBrush":     {BrushSolid, []string{"Color"}},
	"LinearGradientBrush": {BrushLinear, []string{"StartPoint", "EndPoint", "Fallback"}},
	"RadialGradientBrush": {BrushRadial, []string{"Center", "Radius", "Fallback"}},
}

func brushNames() []string {
	out := make([]string, 0, len(brushKinds))
	for n := range brushKinds {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// stopElement is a gradient's only child, named once so parseStops and
// the vocabulary cannot disagree about its spelling.
const stopElement = "GradientStop"

// paintAttrs is the stroke-and-fill surface every shape shares. It is
// MAUI's spelling throughout, which is paint's stated contract: "every
// element that takes a stroke spells it identically".
var paintAttrs = []string{
	"Fill", "Stroke", "StrokeDashArray", "StrokeLineCap",
	"StrokeLineJoin", "StrokeThickness",
}

func parseShape(e markup.Element) (Shape, error) {
	def := shapeKinds[e.Name]
	kind := def.kind
	if len(e.Children) > 0 {
		return Shape{}, fmt.Errorf("markup: <%s> takes no children; a brush goes in <%s.Fill> or <%s.Stroke>", e.Name, e.Name, e.Name)
	}
	s := Shape{Kind: kind, extent: def.extent}
	var err error
	switch kind {
	case KindRectangle:
		if s.X, s.Y, s.W, s.H, err = rectAttr(e, "Rect"); err != nil {
			return Shape{}, err
		}
		if s.CornerRadius, err = floatAttr(e, "CornerRadius", 0); err != nil {
			return Shape{}, err
		}
	case KindEllipse:
		if s.CX, s.CY, err = pointAttr(e, "Center", 0.5, 0.5); err != nil {
			return Shape{}, err
		}
		if s.RX, err = floatAttr(e, "RadiusX", 0.5); err != nil {
			return Shape{}, err
		}
		if s.RY, err = floatAttr(e, "RadiusY", 0.5); err != nil {
			return Shape{}, err
		}
	case KindLine:
		if s.X0, s.Y0, err = pointAttr(e, "From", 0, 0); err != nil {
			return Shape{}, err
		}
		if s.X1, s.Y1, err = pointAttr(e, "To", 1, 1); err != nil {
			return Shape{}, err
		}
	case KindPolyline:
		if s.Points, err = pointsAttr(e, "Points"); err != nil {
			return Shape{}, err
		}
		if s.Closed, err = boolAttr(e, "Closed"); err != nil {
			return Shape{}, err
		}
	}

	if s.StrokeBrush, err = brushOf(e, "Stroke"); err != nil {
		return Shape{}, err
	}
	if s.FillBrush, err = brushOf(e, "Fill"); err != nil {
		return Shape{}, err
	}
	if s.StrokeBrush == nil && s.FillBrush == nil {
		return Shape{}, fmt.Errorf(`markup: <%s> paints nothing: give it a Stroke (e.g. Stroke="#6c9cff") or a Fill`, e.Name)
	}
	// A pen attribute with no pen is a load error rather than a silently
	// inert one. It is a plausible mistake — a XAML author reasonably
	// expects StrokeThickness to imply an outline — and it has no other
	// symptom: the shape simply comes out with no border and nothing
	// anywhere says why. The same reasoning as <Rectangle> with neither
	// Stroke nor Fill, one step in.
	if s.StrokeBrush == nil {
		for _, a := range []string{"StrokeThickness", "StrokeDashArray", "StrokeLineCap", "StrokeLineJoin"} {
			if v, ok := e.Attrs[a]; ok && strings.TrimSpace(v) != "" {
				return Shape{}, fmt.Errorf(`markup: <%s %s=%q>: there is no pen to apply it to; add a Stroke (e.g. Stroke="#6c9cff") or drop the attribute`, e.Name, a, v)
			}
		}
	}

	// Cap and Join are stated rather than left at gg's zero values. gg's
	// zero LineCap is Round and its zero LineJoin is Round, while
	// paint.ParseLineCap and ParseLineJoin read an OMITTED attribute as
	// Flat/Butt and Miter/Bevel — MAUI's defaults. Going through the
	// parsers for the empty string is what keeps a Stroke literal and
	// the markup spelling of the same stroke drawing the same line;
	// panel's `stroke` helper documents the same trap from the other
	// side.
	if s.Stroke.Cap, err = paint.ParseLineCap(e.Attrs["StrokeLineCap"]); err != nil {
		return Shape{}, fmt.Errorf("markup: <%s>: %w", e.Name, err)
	}
	if s.Stroke.Join, err = paint.ParseLineJoin(e.Attrs["StrokeLineJoin"]); err != nil {
		return Shape{}, fmt.Errorf("markup: <%s>: %w", e.Name, err)
	}
	if s.Stroke.Dash, err = paint.ParseDashArray(e.Attrs["StrokeDashArray"]); err != nil {
		return Shape{}, fmt.Errorf("markup: <%s>: %w", e.Name, err)
	}
	if s.Stroke.Thickness, err = floatAttr(e, "StrokeThickness", 1); err != nil {
		return Shape{}, err
	}
	if s.Stroke.Thickness <= 0 {
		return Shape{}, fmt.Errorf("markup: <%s StrokeThickness=%q>: a stroke must be wider than zero pixels; omit Stroke to draw no outline", e.Name, e.Attrs["StrokeThickness"])
	}
	// paint.Stroke.Fallback is deliberately NOT set here, and the reason
	// belongs next to the omission rather than in a commit message.
	//
	// It was set, and the value was never read: Stroke.Apply makes gg
	// calls only (paint/paint.go:105) and nothing in this package
	// consults it. The cell tier's per-shape fallback comes from
	// Brush.Solid via brushPattern, which is the right mechanism because
	// it covers the FILL as well — Stroke.Fallback describes a pen and a
	// shape has two brushes. Leaving the assignment in would tell a
	// future reader that the field drives the degrade, which is exactly
	// the wrong place to start looking when the degrade is wrong.
	//
	// So paint.Stroke.Fallback now has no consumer anywhere in the tree,
	// which is a stronger form of the finding this PR already filed
	// against #241: the package doc lists Fallback as one of four things
	// paint carries, and its first real caller found no use for it.
	return s, nil
}

// brushOf reads one half of a shape's paint: the attribute shorthand
// (MAUI accepts a colour where a brush is expected, and so does
// paint.ParseBrush) or the property element with the structure a
// gradient needs. Both at once is a load error, because the document
// would be saying two different things and one of them would win
// silently.
func brushOf(e markup.Element, name string) (*Brush, error) {
	lit, hasAttr := e.Attrs[name]
	slot, hasProp := e.Props[name]
	if hasAttr && strings.TrimSpace(lit) == "" {
		hasAttr = false
	}
	switch {
	case hasAttr && hasProp:
		return nil, fmt.Errorf("markup: <%s> gives %s twice, as an attribute and as <%s.%s>; keep one", e.Name, name, e.Name, name)
	case hasAttr:
		c, err := paint.ParseColor(lit)
		if err != nil {
			return nil, fmt.Errorf("markup: <%s %s=%q>: %w", e.Name, name, lit, err)
		}
		return &Brush{Kind: BrushSolid, Solid: c}, nil
	case hasProp:
		return parseBrushElement(e.Name, name, slot)
	}
	return nil, nil
}

func parseBrushElement(owner, name string, slot markup.Element) (*Brush, error) {
	if len(slot.Children) != 1 {
		return nil, fmt.Errorf("markup: <%s.%s> holds exactly one brush, got %d", owner, name, len(slot.Children))
	}
	b := slot.Children[0]
	var br Brush
	var err error
	// Dispatch comes from brushKinds, the same literal the vocabulary is
	// built from. That is what makes "unknown brush" and "unvalidated
	// brush" the same impossible state: a name this switch can reach is a
	// name the table has a row for, by construction rather than by two
	// people remembering to edit two places.
	def, ok := brushKinds[b.Name]
	if !ok {
		return nil, fmt.Errorf("markup: <%s.%s>: unknown brush <%s>; want %s", owner, name, b.Name, strings.Join(brushNames(), ", "))
	}
	br.Kind = def.kind
	switch def.kind {
	case BrushSolid:
		if br.Solid, err = colorAttr(b, "Color"); err != nil {
			return nil, err
		}
		return &br, nil
	case BrushLinear:
		if br.X0, br.Y0, err = pointAttr(b, "StartPoint", 0, 0); err != nil {
			return nil, err
		}
		if br.X1, br.Y1, err = pointAttr(b, "EndPoint", 1, 1); err != nil {
			return nil, err
		}
	case BrushRadial:
		if br.CX, br.CY, err = pointAttr(b, "Center", 0.5, 0.5); err != nil {
			return nil, err
		}
		if br.R, err = floatAttr(b, "Radius", 0.5); err != nil {
			return nil, err
		}
	}

	if br.Stops, err = parseStops(b); err != nil {
		return nil, err
	}
	// A gradient MUST declare what it collapses to. paint's doc states
	// the rule and the reason: "a gradient has no cell-plane equivalent,
	// so the caller states what it collapses to rather than having a
	// mean of its stops guessed for it". Averaging the stops here would
	// be exactly that guess, and it would be invisible until someone ran
	// the document on a terminal with no sixel.
	raw, ok := b.Attrs["Fallback"]
	if !ok {
		return nil, fmt.Errorf(`markup: <%s> needs a Fallback (e.g. Fallback="#6c9cff") — it is the single colour this gradient becomes on a terminal with no pixel protocol, and there is no right way to guess it from the stops`, b.Name)
	}
	if br.Solid, err = paint.ParseColor(raw); err != nil {
		return nil, fmt.Errorf("markup: <%s Fallback=%q>: %w", b.Name, raw, err)
	}
	return &br, nil
}

func parseStops(b markup.Element) ([]paint.GradientStop, error) {
	var out []paint.GradientStop
	for _, c := range b.Children {
		if c.Name != stopElement {
			return nil, fmt.Errorf("markup: <%s> holds <%s> children only; got <%s>", b.Name, stopElement, c.Name)
		}
		col, err := colorAttr(c, "Color")
		if err != nil {
			return nil, err
		}
		off, err := floatAttr(c, "Offset", 0)
		if err != nil {
			return nil, err
		}
		if off < 0 || off > 1 {
			return nil, fmt.Errorf("markup: <%s Offset=%q>: an offset is a fraction of the gradient, 0 to 1", stopElement, c.Attrs["Offset"])
		}
		out = append(out, paint.GradientStop{Color: col, Offset: off})
	}
	if len(out) < 2 {
		return nil, fmt.Errorf("markup: <%s> needs at least two <%s> children, got %d; one stop is a solid colour and should be written as one", b.Name, stopElement, len(out))
	}
	return out, nil
}

// ---- attribute readers ----

func unknownAttr(elem string, attrs map[string]string, allowed map[string]bool) error {
	var unknown []string
	for k := range attrs {
		if !allowed[k] {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	sort.Strings(unknown)
	known := make([]string, 0, len(allowed))
	for k := range allowed {
		known = append(known, k)
	}
	sort.Strings(known)
	return fmt.Errorf("markup: <%s %s=%q>: no such attribute; <%s> takes %s",
		elem, unknown[0], attrs[unknown[0]], elem, strings.Join(known, ", "))
}

func unknownProp(e markup.Element, allowed map[string]bool, hint string) error {
	names := make([]string, 0, len(e.Props))
	for name := range e.Props {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if allowed[name] {
			continue
		}
		known := make([]string, 0, len(allowed))
		for k := range allowed {
			known = append(known, k)
		}
		sort.Strings(known)
		if len(known) == 0 {
			return fmt.Errorf("markup: <%s> does not accept the property element <%s.%s>; it takes none%s",
				e.Name, e.Name, name, hint)
		}
		return fmt.Errorf("markup: <%s> does not accept the property element <%s.%s>; it takes %s%s",
			e.Name, e.Name, name, strings.Join(known, " and "), hint)
	}
	return nil
}

func colorAttr(e markup.Element, name string) (render.Color, error) {
	raw, ok := e.Attrs[name]
	if !ok {
		return render.Color{}, fmt.Errorf(`markup: <%s> needs a %s (e.g. %s="#6c9cff")`, e.Name, name, name)
	}
	c, err := paint.ParseColor(raw)
	if err != nil {
		return render.Color{}, fmt.Errorf("markup: <%s %s=%q>: %w", e.Name, name, raw, err)
	}
	return c, nil
}

func floatAttr(e markup.Element, name string, def float64) (float64, error) {
	raw, ok := e.Attrs[name]
	if !ok || strings.TrimSpace(raw) == "" {
		return def, nil
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return 0, fmt.Errorf("markup: <%s %s=%q>: want a number", e.Name, name, raw)
	}
	return v, nil
}

func boolAttr(e markup.Element, name string) (bool, error) {
	raw, ok := e.Attrs[name]
	if !ok || strings.TrimSpace(raw) == "" {
		return false, nil
	}
	v, err := strconv.ParseBool(strings.TrimSpace(raw))
	if err != nil {
		return false, fmt.Errorf("markup: <%s %s=%q>: want true or false", e.Name, name, raw)
	}
	return v, nil
}

func pointAttr(e markup.Element, name string, dx, dy float64) (float64, float64, error) {
	raw, ok := e.Attrs[name]
	if !ok || strings.TrimSpace(raw) == "" {
		return dx, dy, nil
	}
	v, err := floats(raw)
	if err != nil || len(v) != 2 {
		return 0, 0, fmt.Errorf(`markup: <%s %s=%q>: want two numbers, "x,y", as fractions of the figure`, e.Name, name, raw)
	}
	return v[0], v[1], nil
}

func rectAttr(e markup.Element, name string) (x, y, w, h float64, err error) {
	raw, ok := e.Attrs[name]
	if !ok || strings.TrimSpace(raw) == "" {
		return 0, 0, 1, 1, nil
	}
	v, err := floats(raw)
	if err != nil || len(v) != 4 {
		return 0, 0, 0, 0, fmt.Errorf(`markup: <%s %s=%q>: want four numbers, "x,y,w,h", as fractions of the figure`, e.Name, name, raw)
	}
	return v[0], v[1], v[2], v[3], nil
}

func pointsAttr(e markup.Element, name string) ([]float64, error) {
	raw := e.Attrs[name]
	v, err := floats(raw)
	if err != nil {
		return nil, fmt.Errorf(`markup: <%s %s=%q>: want numbers, "x,y x,y …", as fractions of the figure`, e.Name, name, raw)
	}
	if len(v) < 4 || len(v)%2 != 0 {
		return nil, fmt.Errorf(`markup: <%s %s=%q>: want at least two x,y pairs`, e.Name, name, raw)
	}
	return v, nil
}

func floats(s string) ([]float64, error) {
	f := strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' || r == '\t' })
	out := make([]float64, 0, len(f))
	for _, p := range f {
		v, err := strconv.ParseFloat(p, 64)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// ---- component ----

// Attachments returns the non-visual children. A <KeyBinding> written
// inside a <Figure> has to reach the framework; dropping it would be
// silent.
func (f *Figure) Attachments() []gooey.Component { return f.attach }

func (f *Figure) ChildComponents() []gooey.Component {
	if f.Child == nil {
		return nil
	}
	return []gooey.Component{f.Child}
}

// inset is how many cells the art claims on each side. Only a ring
// claims any: a full-slice figure draws UNDER its content.
func (f *Figure) inset() int {
	if f.Slice == SliceRing {
		return 1
	}
	return 0
}

func (f *Figure) Measure(avail gooey.Size) gooey.Size {
	if f.Child != nil {
		d := 2 * f.inset()
		gooey.MeasureChild(f.Child, gooey.Size{W: max(0, avail.W-d), H: max(0, avail.H-d)})
	}
	return avail
}

func (f *Figure) Arrange(b gooey.Rect) {
	f.Base.Arrange(b)
	if f.Child == nil {
		return
	}
	in := f.inset()
	gooey.ArrangeChild(f.Child, gooey.Rect{
		X: b.X + in, Y: b.Y + in,
		W: max(0, b.W-2*in), H: max(0, b.H-2*in),
	})
}

// Render draws the figure at whichever tier the terminal supports.
//
// The guard checks all three of Graphics, CellW and CellH. Checking
// Graphics alone is issue #251: a forced protocol with no capability
// probe leaves CellW at zero, paint.Canvas correctly refuses a canvas of
// nothing, and the region goes silently blank.
func (f *Figure) Render(fr *gooey.Frame) {
	b := f.Bounds()
	if b.W < 1 || b.H < 1 {
		return
	}
	if fr.Graphics == nil || fr.CellW <= 0 || fr.CellH <= 0 {
		f.renderCells(fr)
		return
	}
	r, err := f.raster(b.W, b.H, fr.CellW, fr.CellH, false)
	if err != nil {
		// A canvas that cannot be built must not leave the figure with
		// nothing at all; the cell tier is the same shape in runes.
		f.renderCells(fr)
		return
	}
	if f.Slice == SliceRing {
		if b.W < 2 || b.H < 2 {
			return
		}
		fr.Place(graphics.Placement{Img: r.top, Col: b.X, Row: b.Y, Cols: b.W, Rows: 1})
		fr.Place(graphics.Placement{Img: r.bottom, Col: b.X, Row: b.Y + b.H - 1, Cols: b.W, Rows: 1})
		if b.H > 2 {
			fr.Place(graphics.Placement{Img: r.left, Col: b.X, Row: b.Y + 1, Cols: 1, Rows: b.H - 2})
			fr.Place(graphics.Placement{Img: r.right, Col: b.X + b.W - 1, Row: b.Y + 1, Cols: 1, Rows: b.H - 2})
		}
		return
	}
	fr.Place(graphics.Placement{Img: r.img, Col: b.X, Row: b.Y, Cols: b.W, Rows: b.H})
}

// renderCells is the universal tier: the same shape, in the same cells,
// quantized into block runes.
//
// Every cell in the figure's bounds is written, including the empty
// ones. That is deliberate: a Figure implements Container, and the
// Composer's pre-clear is a TYPE assertion (composer.go:300), so a
// container never pre-clears even when it has no children. Writing the
// blanks is what stops a shrinking figure from leaving its old runes
// behind.
func (f *Figure) renderCells(fr *gooey.Frame) {
	b := f.Bounds()
	r, err := f.raster(b.W, b.H, nominalCellW, nominalCellH, true)
	if err != nil {
		return
	}
	in := f.inset()
	for row := 0; row < b.H; row++ {
		for col := 0; col < b.W; col++ {
			// The interior of a ring belongs to the content, exactly as
			// it does on the pixel plane. Painting it here would erase
			// whatever the child drew.
			if f.Slice == SliceRing && row >= in && row < b.H-in && col >= in && col < b.W-in {
				continue
			}
			ch, ink, any := cell(r.img, col, row)
			st := f.style
			if any {
				st.Fg = ink
			}
			fr.Cells.Set(b.X+col, b.Y+row, ch, st)
		}
	}
}

// cell quantizes one cell of the nominal raster: how much of it is
// inked, and with what.
//
// The colour is the alpha-weighted mean of the straight colours under
// the cell, and it falls out of summing PREMULTIPLIED channels and
// dividing by summed alpha — which is what color.Color.RGBA already
// returns, so no per-pixel division is needed. A cell with no ink
// reports any=false rather than black, so the figure's own style shows
// through instead of a hole.
func cell(img image.Image, col, row int) (rune, render.Color, bool) {
	var sr, sg, sb, sa uint64
	x0, y0 := col*nominalCellW, row*nominalCellH
	for y := y0; y < y0+nominalCellH; y++ {
		for x := x0; x < x0+nominalCellW; x++ {
			r, g, b, a := img.At(x, y).RGBA()
			sr, sg, sb, sa = sr+uint64(r), sg+uint64(g), sb+uint64(b), sa+uint64(a)
		}
	}
	if sa == 0 {
		return shades[0], render.Color{}, false
	}
	full := float64(nominalCellW) * float64(nominalCellH) * 0xffff
	// Coverage is mapped through a cube root, not linearly, and that is
	// the difference between a legible cell tier and a useless one.
	//
	// Line art is nearly all thin: a 1-pixel stroke across a 16-pixel
	// cell covers 6% of it, a 3-pixel stroke 19%, a 7-pixel stroke 44%.
	// Linearly into four buckets the first two both round to the
	// lightest shade and the strokes page — the page whose entire
	// subject is stroke thickness — comes out as three identical rows.
	// Measured with a cube root they are ▒, ▓ and █.
	//
	// The floor of one bucket is the other half: any ink at all has to
	// be at least the lightest shade, or a hairline disappears entirely
	// and the tier silently loses the art it is most needed for.
	idx := int(math.Cbrt(float64(sa)/full)*float64(len(shades)-1)) + 1
	if idx >= len(shades) {
		idx = len(shades) - 1
	}
	ink := paint.FromColor(color.RGBA{
		R: uint8(255 * float64(sr) / float64(sa)),
		G: uint8(255 * float64(sg) / float64(sa)),
		B: uint8(255 * float64(sb) / float64(sa)),
		A: 0xff,
	})
	return shades[idx], ink, true
}

// raster draws the figure, memoizing the last one.
//
// flat selects the cell tier's colours: every brush becomes its declared
// fallback, so a gradient collapses where its author said it should and
// the per-cell mean colour then lands automatically.
func (f *Figure) raster(cols, rows, cellW, cellH int, flat bool) (*raster, error) {
	key := fmt.Sprintf("%dx%d@%dx%d/%v/%v", cols, rows, cellW, cellH, flat, f.Slice)
	if f.memo != nil && f.memoKey == key {
		return f.memo, nil
	}
	dc, err := paint.Canvas(cols, rows, cellW, cellH)
	if err != nil {
		return nil, err
	}
	f.draw(dc, flat)
	r := &raster{img: dc.Image()}
	if f.Slice == SliceRing {
		r.top, r.bottom, r.left, r.right = paint.Ring(r.img, cellW, cellH)
	}
	f.memo, f.memoKey = r, key
	return r, nil
}

// draw is the whole of the art.
//
// Nothing fills the canvas. A gg context starts fully transparent and
// stays that way outside the strokes, which is what lets the encoder
// write no pixel where alpha is low — so a figure's gaps leave their
// cells alone instead of stamping black over the text underneath.
func (f *Figure) draw(dc *gg.Context, flat bool) {
	w, h := float64(dc.Width()), float64(dc.Height())
	for _, s := range f.Shapes {
		path(dc, s, w, h)
		if s.FillBrush != nil {
			dc.SetFillStyle(brushPattern(s.FillBrush, w, h, flat))
			if s.StrokeBrush != nil {
				dc.FillPreserve()
			} else {
				dc.Fill()
			}
		}
		if s.StrokeBrush != nil {
			st := s.Stroke
			st.Brush = brushPattern(s.StrokeBrush, w, h, flat)
			st.Apply(dc)
			dc.Stroke()
		}
	}
}

func brushPattern(b *Brush, w, h float64, flat bool) gg.Pattern {
	if flat {
		return b.Flat()
	}
	return b.Pattern(w, h)
}

// pen is the width the geometry has to make room for: the stroke's, or
// zero when the shape has no stroke to draw.
func pen(s Shape) float64 {
	if s.StrokeBrush == nil {
		return 0
	}
	return s.Stroke.Thickness
}

func path(dc *gg.Context, s Shape, w, h float64) {
	// ONE degeneracy question for every closed shape, asked before the
	// switch so a new kind is covered by declaring its extent rather than
	// by remembering to guard. See shapeKinds for why the rectangle's and
	// the ellipse's guards were the same inequality all along.
	//
	// It cannot be a load-time check: a radius is relative and a
	// thickness is in pixels, so the same document is fine in a wide pane
	// and inverted in a narrow one.
	if s.extent != nil {
		ew, eh := s.extent(s, w, h)
		if t := pen(s); ew-t <= 0 || eh-t <= 0 {
			return
		}
	}
	switch s.Kind {
	case KindRectangle:
		// Inset by half the stroke so its outer edge lands ON the
		// figure's boundary rather than half outside it and clipped —
		// panel's rule, and the reason a full-bleed rectangle looks like
		// it has three sides without it.
		//
		// A shape with no pen gets no inset. Thickness defaults to 1
		// whether or not a Stroke was declared, so insetting
		// unconditionally shrank every fill-only shape by half a pixel
		// for no reason anyone could see — small, but it is the fill
		// landing somewhere other than where the markup put it.
		t := pen(s)
		x, y := s.X*w+t/2, s.Y*h+t/2
		rw, rh := s.W*w-t, s.H*h-t
		if s.CornerRadius > 0 {
			// gg draws a radius larger than half the side as
			// overlapping arcs, where SVG's rx is defined to clamp.
			dc.DrawRoundedRectangle(x, y, rw, rh, min(s.CornerRadius, min(rw/2, rh/2)))
			return
		}
		dc.DrawRectangle(x, y, rw, rh)
	case KindEllipse:
		// The same guard the rectangle has, and for a sharper reason.
		// A radius is RELATIVE and a thickness is in PIXELS, so whether
		// the inset leaves anything behind depends on the figure's size
		// and cannot be checked at load: the same document is fine in a
		// wide pane and inverted in a narrow one.
		//
		// Handing gg a negative radius is not the harmless no-op it is
		// for a rectangle. Measured: RadiusX="0.05" with
		// StrokeThickness="50" on a 160x96 canvas inks 3765 pixels
		// against 694 for a correctly proportioned ellipse — a large
		// wrong shape rather than nothing.
		t := pen(s)
		dc.DrawEllipse(s.CX*w, s.CY*h, s.RX*w-t/2, s.RY*h-t/2)
	case KindLine:
		dc.DrawLine(s.X0*w, s.Y0*h, s.X1*w, s.Y1*h)
	case KindPolyline:
		dc.MoveTo(s.Points[0]*w, s.Points[1]*h)
		for i := 2; i+1 < len(s.Points); i += 2 {
			dc.LineTo(s.Points[i]*w, s.Points[i+1]*h)
		}
		if s.Closed {
			dc.ClosePath()
		}
	}
}
