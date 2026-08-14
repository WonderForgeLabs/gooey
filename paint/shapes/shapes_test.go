// Tests for the markup vocabulary. gg's drawing is not retested here —
// paint's own suite already declines to retest it, on the grounds that
// the module's design claim is that it adds no vocabulary of its own.
// What is left is exactly the code that can be wrong: the load-time
// checks the framework will not perform for a registered component, the
// two-tier rule, the ring geometry, and the cache key panel got wrong
// once already.
//
// Every load-error case asserts the SHAPE of the message rather than
// err != nil. Nearly everything in this repo fails at load, so existence
// proves almost nothing about which check caught it — a builder that
// rejected every document would pass an err != nil suite completely.
package shapes

import (
	"fmt"
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/graphics"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
	"github.com/WonderForgeLabs/gooey/term"
)

func ctx() *markup.Context {
	return &markup.Context{
		Components: map[string]markup.Builder{"Figure": Builder()},
		Styles:     map[string]render.Style{"ink": {Fg: render.RGB(0x6c, 0x9c, 0xff)}},
	}
}

func page(body string) []byte {
	return []byte(`<Gooey xmlns="wonderforge.io/gooey/2026">` + body + `</Gooey>`)
}

func buildPage(t *testing.T, body string) gooey.Component {
	t.Helper()
	w, err := markup.Build(page(body), ctx())
	if err != nil {
		t.Fatalf("build %s: %v", body, err)
	}
	return w
}

// ---- load errors ----

// TestLoadErrors is the whole point of a registered element validating
// itself. Context.spec returns AttrsKnown=false for anything in
// Components and checkProps exempts it outright, so none of these would
// be caught by the framework: every one of them would otherwise be a
// silently ignored attribute or a silently dropped child.
func TestLoadErrors(t *testing.T) {
	for _, c := range []struct{ name, body, want string }{
		{
			"unknown attribute on Figure",
			`<Figure Slize="Ring"><Line Stroke="#fff"/></Figure>`,
			"no such attribute",
		}, {
			"bad Slice value",
			`<Figure Slice="Half"><Line Stroke="#fff"/></Figure>`,
			"want Full or Ring",
		}, {
			"unknown attribute on a shape",
			`<Figure><Line Strkoe="#fff"/></Figure>`,
			"no such attribute",
		}, {
			// The message has to list the vocabulary, because the reader
			// is staring at one line of a file with no catalog to consult.
			"the message names what the element does take",
			`<Figure><Line Strkoe="#fff"/></Figure>`,
			"StrokeDashArray",
		}, {
			"a geometry attribute from another shape",
			`<Figure><Line Center="0.5,0.5" Stroke="#fff"/></Figure>`,
			"no such attribute",
		}, {
			"a shape that paints nothing",
			`<Figure><Line From="0,0" To="1,1"/></Figure>`,
			"paints nothing",
		}, {
			"zero stroke thickness",
			`<Figure><Line Stroke="#fff" StrokeThickness="0"/></Figure>`,
			"wider than zero",
		}, {
			"a colour that is not a colour",
			`<Figure><Line Stroke="cornflower"/></Figure>`,
			"#rrggbb",
		}, {
			"a dash array that is not numbers",
			`<Figure><Line Stroke="#fff" StrokeDashArray="4,wide"/></Figure>`,
			"StrokeDashArray",
		}, {
			"an unknown line cap",
			`<Figure><Line Stroke="#fff" StrokeLineCap="Blunt"/></Figure>`,
			"want Flat, Square or Round",
		}, {
			"an unknown line join",
			`<Figure><Line Stroke="#fff" StrokeLineJoin="Sharp"/></Figure>`,
			"want Miter, Bevel or Round",
		}, {
			"a point that is not a point",
			`<Figure><Line From="0.5" Stroke="#fff"/></Figure>`,
			`want two numbers`,
		}, {
			"a rect that is not four numbers",
			`<Figure><Rectangle Rect="0,0,1" Stroke="#fff"/></Figure>`,
			"want four numbers",
		}, {
			"a polyline with one point",
			`<Figure><Polyline Points="0.5,0.5" Stroke="#fff"/></Figure>`,
			"at least two x,y pairs",
		}, {
			"a shape with children",
			`<Figure><Line Stroke="#fff"><Text>x</Text></Line></Figure>`,
			"takes no children",
		}, {
			"a property element Figure does not have",
			`<Figure Slice="Ring"><Figure.Stroke><SolidColorBrush Color="#fff"/></Figure.Stroke><Line Stroke="#fff"/></Figure>`,
			"a brush is a property of the SHAPE",
		}, {
			"two content children",
			`<Figure><Line Stroke="#fff"/><Text>a</Text><Text>b</Text></Figure>`,
			"at most one content child",
		}, {
			// A shape never reaches markup.build, so core's checkProps
			// never sees it. This check was missing in the first version
			// of this package and <Rectangle.Fil> loaded clean with the
			// brush inside it silently never applied — found in review.
			"a property element a shape does not have",
			`<Figure><Rectangle Stroke="#fff"><Rectangle.Bogus><SolidColorBrush Color="#000"/></Rectangle.Bogus></Rectangle></Figure>`,
			"does not accept the property element",
		}, {
			"a near-miss property element on a shape",
			`<Figure><Rectangle Stroke="#fff"><Rectangle.Fil><SolidColorBrush Color="#000"/></Rectangle.Fil></Rectangle></Figure>`,
			"does not accept the property element",
		}, {
			// A pen attribute with no pen. Inert would be defensible;
			// silent is not, because the only symptom is a shape with no
			// outline and no explanation.
			"StrokeThickness with no Stroke",
			`<Figure><Ellipse Fill="#fff" StrokeThickness="8"/></Figure>`,
			"there is no pen to apply it to",
		}, {
			"StrokeDashArray with no Stroke",
			`<Figure><Ellipse Fill="#fff" StrokeDashArray="4,2"/></Figure>`,
			"there is no pen to apply it to",
		}, {
			"an unknown brush element",
			`<Figure><Line><Line.Stroke><TartanBrush/></Line.Stroke></Line></Figure>`,
			"unknown brush",
		}, {
			"a brush given twice",
			`<Figure><Line Stroke="#fff"><Line.Stroke><SolidColorBrush Color="#000"/></Line.Stroke></Line></Figure>`,
			"keep one",
		}, {
			// The rule paint's own doc states: a gradient has no
			// cell-plane equivalent, so the author says what it collapses
			// to. Averaging the stops would be the guess the doc forbids,
			// and the mistake would only surface on a terminal with no
			// sixel — which is nobody's development terminal.
			"a gradient with no Fallback",
			`<Figure><Line><Line.Stroke><LinearGradientBrush><GradientStop Color="#fff" Offset="0"/><GradientStop Color="#000" Offset="1"/></LinearGradientBrush></Line.Stroke></Line></Figure>`,
			"needs a Fallback",
		}, {
			"a gradient with one stop",
			`<Figure><Line><Line.Stroke><LinearGradientBrush Fallback="#fff"><GradientStop Color="#fff" Offset="0"/></LinearGradientBrush></Line.Stroke></Line></Figure>`,
			"at least two",
		}, {
			"a gradient stop outside 0..1",
			`<Figure><Line><Line.Stroke><LinearGradientBrush Fallback="#fff"><GradientStop Color="#fff" Offset="0"/><GradientStop Color="#000" Offset="2"/></LinearGradientBrush></Line.Stroke></Line></Figure>`,
			"a fraction of the gradient",
		}, {
			"a non-stop inside a gradient",
			`<Figure><Line><Line.Stroke><LinearGradientBrush Fallback="#fff"><Text>x</Text></LinearGradientBrush></Line.Stroke></Line></Figure>`,
			"GradientStop",
		}, {
			"an unknown attribute on a brush",
			`<Figure><Line><Line.Stroke><LinearGradientBrush Fallback="#fff" Angle="45"><GradientStop Color="#fff" Offset="0"/><GradientStop Color="#000" Offset="1"/></LinearGradientBrush></Line.Stroke></Line></Figure>`,
			"no such attribute",
		}, {
			"an unknown attribute on a gradient stop",
			`<Figure><Line><Line.Stroke><LinearGradientBrush Fallback="#fff"><GradientStop Color="#fff" Stop="0"/><GradientStop Color="#000" Offset="1"/></LinearGradientBrush></Line.Stroke></Line></Figure>`,
			"no such attribute",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := markup.Build(page(c.body), ctx())
			if err == nil {
				t.Fatalf("loaded; this has to fail at LOAD time, not draw nothing later")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error %q does not mention %q", err, c.want)
			}
		})
	}
}

// TestTheUniversalSurfaceStillLoads is the other direction, and it is
// not decoration: figureAttrs is a closed set, so a check that forgot to
// join in markup.UniversalAttrs would reject every laid-out figure while
// every test above still passed.
func TestTheUniversalSurfaceStillLoads(t *testing.T) {
	buildPage(t, `<Grid Rows="*,*" Cols="*"><Figure Name="f" Grid.Row="1" Width="20" Margin="1" HAlign="Center" Style="ink"><Line Stroke="#6c9cff"/></Figure></Grid>`)
}

// TestKeyBindingInsideAFigureReachesTheFramework pins the attachment
// split. A builder that ignored the attach half of BuildChildren would
// drop every KeyBinding written inside a figure, in silence — the same
// class of defect as a dropped attribute.
func TestKeyBindingInsideAFigureReachesTheFramework(t *testing.T) {
	fired := false
	c := ctx()
	c.Values = map[string]any{"Go": gooey.Command(func() { fired = true })}
	w, err := markup.Build(page(`<Figure><Line Stroke="#fff"/><KeyBinding Gesture="g" Command="{{.Go}}"/></Figure>`), c)
	if err != nil {
		t.Fatal(err)
	}
	comp := gooey.NewComposer(w, 20, 6)
	comp.Frame()
	if !comp.HandleKey(input.Rune('g')) {
		t.Fatal("the KeyBinding did not fire; it was dropped rather than attached")
	}
	if !fired {
		t.Fatal("the binding fired but the command did not run")
	}
}

// ---- the two tiers ----

func fig(t *testing.T, body string, cols, rows int, enc graphics.Encoder, cellW, cellH int) (*gooey.Composer, *gooey.Frame) {
	t.Helper()
	w := buildPage(t, body)
	c := gooey.NewComposer(w, cols, rows)
	c.SetCaps(term.Caps{Cols: cols, Rows: rows, CellW: cellW, CellH: cellH, Color: render.TrueColor})
	c.SetGraphics(enc)
	f, _ := c.Frame()
	return c, f
}

// TestPixelTierPlacesOneImageForAFullFigure: a full-slice figure is one
// placement covering its own cells, which is what makes it one paint
// node and one diffable unit.
func TestPixelTierPlacesOneImageForAFullFigure(t *testing.T) {
	_, f := fig(t, `<Figure><Ellipse Stroke="#6c9cff" StrokeThickness="2"/></Figure>`, 20, 6, graphics.Sixel{}, 8, 16)
	p := f.Placements()
	if len(p) != 1 {
		t.Fatalf("a full figure placed %d images, want 1", len(p))
	}
	if p[0].Cols != 20 || p[0].Rows != 6 {
		t.Fatalf("placement covers %dx%d cells, want the figure's 20x6", p[0].Cols, p[0].Rows)
	}
	if b := p[0].Img.Bounds(); b.Dx() != 20*8 || b.Dy() != 6*16 {
		t.Fatalf("the canvas is %dx%d px, want 160x96 — art rasterized at any other size is resampled by the pixel pipeline", b.Dx(), b.Dy())
	}
}

// TestRingPlacesFourSlicesAndLeavesTheInteriorAlone is paint.Ring's
// whole reason for existing: placements composite OVER the cells, so a
// figure with contents must not place an image across them.
func TestRingPlacesFourSlicesAndLeavesTheInteriorAlone(t *testing.T) {
	_, f := fig(t, `<Figure Slice="Ring"><Rectangle Stroke="#6c9cff" StrokeThickness="2"/><Text>inside</Text></Figure>`, 20, 6, graphics.Sixel{}, 8, 16)
	p := f.Placements()
	if len(p) != 4 {
		t.Fatalf("a ring placed %d images, want 4 (top, bottom, left, right)", len(p))
	}
	for _, q := range p {
		if q.Rows == 1 {
			continue // an edge row
		}
		if q.Cols != 1 {
			t.Fatalf("a side slice is %d cells wide, want 1", q.Cols)
		}
	}
	// The child's text is on the cell plane and no placement covers it.
	if got := rowOf(f.Cells, 1, 20); !strings.Contains(got, "inside") {
		t.Fatalf("the interior row is %q; the ring's contents must stay on the cell plane", got)
	}
	for _, q := range p {
		if q.Col > 0 && q.Col < 19 && q.Row > 0 && q.Row < 5 {
			t.Fatalf("a placement lands inside the ring at %d,%d — it would bury the text", q.Col, q.Row)
		}
	}
}

// TestCellTierDrawsTheSameCells is the two-tier rule: nothing moves when
// the terminal turns out not to speak sixel. It also pins that the
// figure places NOTHING there — a placement with no encoder is a frame
// the flusher cannot emit.
func TestCellTierDrawsTheSameCells(t *testing.T) {
	_, f := fig(t, `<Figure><Rectangle Stroke="#6c9cff" StrokeThickness="3"/></Figure>`, 20, 6, nil, 0, 0)
	if len(f.Placements()) != 0 {
		t.Fatalf("the cell tier recorded %d pixel placements", len(f.Placements()))
	}
	// A rectangle inked to the boundary must reach the corners.
	for _, p := range []struct{ x, y int }{{0, 0}, {19, 0}, {0, 5}, {19, 5}} {
		if got := f.Cells.At(p.x, p.y).Rune; !isShade(got) || got == ' ' {
			t.Errorf("corner cell (%d,%d) is %q, want an inked block rune", p.x, p.y, got)
		}
	}
	// And the middle of the figure must not be.
	if got := f.Cells.At(10, 3).Rune; got != ' ' {
		t.Errorf("the interior of an unfilled rectangle is %q, want a blank", got)
	}
}

// TestCellTierInksThinStrokes: a hairline covers a percent or two of a
// cell, and a linear map from coverage to five buckets rounds that to
// nothing. Every thin stroke in the sample pages would vanish — the
// tier would be silently useless for exactly the art it is most needed
// for.
func TestCellTierInksThinStrokes(t *testing.T) {
	_, f := fig(t, `<Figure><Line From="0,0.5" To="1,0.5" Stroke="#6c9cff" StrokeThickness="1"/></Figure>`, 20, 6, nil, 0, 0)
	row := rowOf(f.Cells, 3, 20)
	if strings.TrimSpace(row) == "" {
		t.Fatalf("a 1-pixel line left row 3 blank: %q", row)
	}
}

// TestCellTierTellsThicknessesApart pins the values strokes.gooey
// actually uses, and it is the assertion that makes that page worth
// capturing: agg renders the cell plane only, so three thicknesses that
// quantize to the same rune are three identical rows in every GIF of the
// one page whose entire subject is stroke thickness.
//
// The values are 1, 5 and 15 rather than a rounder 1, 3, 7 because the
// arithmetic does not allow the latter, and that is worth recording. A
// horizontal line straddles a cell boundary, so a stroke of t pixels
// peaks at about t/32 of a nominal 8x16 cell — measured, 0.031, 0.094
// and 0.219 for 1, 3 and 7. Four non-blank shades cannot separate those
// under any monotone map, and the first attempt here (a linear one) put
// all three in the lightest bucket. The cube root is what makes a
// hairline visible at all; choosing plate values with real separation is
// what makes the page legible. Both were needed, and neither alone was
// enough.
func TestCellTierTellsThicknessesApart(t *testing.T) {
	seen := map[rune]float64{}
	for _, th := range []float64{1, 5, 15} {
		body := fmt.Sprintf(`<Figure><Line From="0,0.5" To="1,0.5" Stroke="#6c9cff" StrokeThickness="%g"/></Figure>`, th)
		_, f := fig(t, body, 20, 6, nil, 0, 0)
		got := f.Cells.At(10, 3).Rune
		if got == ' ' {
			t.Fatalf("thickness %g left the middle row blank", th)
		}
		if prev, dup := seen[got]; dup {
			t.Fatalf("thickness %g and %g both quantize to %q; the cell tier cannot tell them apart", prev, th, got)
		}
		seen[got] = th
	}
}

// TestCellTierTakesTheDeclaredFallbackOfAGradient: the cell tier redraws
// with brushes replaced by their declared fallback, so the ink is the
// colour the author named and not a mean of the stops.
func TestCellTierTakesTheDeclaredFallbackOfAGradient(t *testing.T) {
	want := render.RGB(0x7a, 0xd3, 0xa0)
	_, f := fig(t, `<Figure><Rectangle Rect="0,0,1,1"><Rectangle.Fill><LinearGradientBrush StartPoint="0,0" EndPoint="1,0" Fallback="#7ad3a0"><GradientStop Color="#ff0000" Offset="0"/><GradientStop Color="#0000ff" Offset="1"/></LinearGradientBrush></Rectangle.Fill></Rectangle></Figure>`, 20, 6, nil, 0, 0)
	if got := f.Cells.At(10, 3).Style.Fg; got != want {
		t.Fatalf("the cell tier inked %+v, want the declared Fallback %+v; a mean of #ff0000 and #0000ff would be neither", got, want)
	}
}

// TestAForcedProtocolWithNoCellSizeStillDraws is issue #251's symptom
// reached from here: a forced protocol with no capability probe leaves
// CellW at zero, and a component that branched on f.Graphics alone
// places no image and draws no cells — a blank region with no error
// anywhere.
//
// What this test pins is the BEHAVIOUR, and it cannot pin the mechanism,
// which is worth stating because the name it originally carried claimed
// otherwise. Two independent things produce the outcome here: Render's
// three-part guard, and paint.Canvas refusing a canvas of zero pixels so
// that raster returns an error and Render falls back anyway. Deleting
// the guard's `|| fr.CellW <= 0 || fr.CellH <= 0` was measured against
// this test and it still passed — the redundancy is real and deliberate,
// and it is precisely the second net that components.Image lacks.
func TestAForcedProtocolWithNoCellSizeStillDraws(t *testing.T) {
	for _, c := range []struct{ w, h int }{{0, 20}, {10, 0}, {0, 0}} {
		_, f := fig(t, `<Figure><Rectangle Stroke="#6c9cff" StrokeThickness="3"/></Figure>`, 20, 6, graphics.Sixel{}, c.w, c.h)
		if len(f.Placements()) != 0 {
			t.Errorf("cell %dx%d: placed an image with no cell size to rasterize into", c.w, c.h)
		}
		if got := f.Cells.At(0, 0).Rune; got == ' ' {
			t.Errorf("cell %dx%d: the region is blank — the encoder was checked and the cell size was not", c.w, c.h)
		}
	}
}

// TestRasterKeyCarriesTheCellSize is panel's bug, pinned before it can
// be made again here. 40 cells at 8px and 20 cells at 16px are the same
// 320-pixel canvas; a key written in pixels hands the first figure's
// slices to the second.
func TestRasterKeyCarriesTheCellSize(t *testing.T) {
	w := buildPage(t, `<Figure Slice="Ring"><Rectangle Stroke="#6c9cff" StrokeThickness="2"/></Figure>`)
	f, ok := w.(*Figure)
	if !ok {
		t.Fatalf("root is %T, want *Figure", w)
	}
	a, err := f.raster(40, 6, 8, 16, false)
	if err != nil {
		t.Fatal(err)
	}
	keyA := f.memoKey
	b, err := f.raster(20, 6, 16, 16, false)
	if err != nil {
		t.Fatal(err)
	}
	if keyA == f.memoKey {
		t.Fatal("both rasters share a memo key; the key does not carry the cell size")
	}
	if a.left.Bounds().Dx() == b.left.Bounds().Dx() {
		t.Fatalf("both rings have %d-pixel sides; the slices should differ with the cell width", a.left.Bounds().Dx())
	}
	// And the memo does hit when the key repeats, or it is not a cache.
	again, err := f.raster(20, 6, 16, 16, false)
	if err != nil {
		t.Fatal(err)
	}
	if again != b {
		t.Fatal("the same size redrew instead of hitting the memo")
	}
}

// TestAFillOnlyShapeIsNotInsetByAPhantomPen: Stroke.Thickness defaults
// to 1 whether or not a Stroke was declared, and the geometry used to
// inset by half of it unconditionally — so every fill-only shape landed
// half a pixel inside where the markup put it, with nothing to say why.
// Found in review, on brushes.gooey's own RadialGradientBrush plate.
//
// The assertion is the ALPHA of the boundary pixel, not the extent of
// the ink. A first attempt asserted "the leftmost inked column is 0" and
// passed with the bug restored: a half-pixel inset antialiases column 0
// to half coverage, which is still ink. The half pixel is the whole
// defect, so the test has to be able to see a half pixel.
func TestAFillOnlyShapeIsNotInsetByAPhantomPen(t *testing.T) {
	edgeAlpha := func(body string) uint32 {
		t.Helper()
		w := buildPage(t, body)
		f := w.(*Figure)
		r, err := f.raster(20, 6, nominalCellW, nominalCellH, false)
		if err != nil {
			t.Fatal(err)
		}
		_, _, _, a := r.img.At(0, 3*nominalCellH).RGBA()
		return a
	}
	// A fill that covers the whole figure must cover the whole figure:
	// the boundary pixel is fully opaque, not half-covered by a pen that
	// is never drawn.
	if got := edgeAlpha(`<Figure><Rectangle Rect="0,0,1,1" Fill="#ffffff"/></Figure>`); got != 0xffff {
		t.Errorf("the boundary pixel of a fill-only rectangle has alpha %d, want 65535; a pen that is never drawn inset the fill by half its width", got)
	}
	// The discriminating half: a shape that DOES have a pen must still
	// be inset, or this is a deleted feature rather than a fix. An
	// 8-pixel stroke centred on the boundary would put four pixels
	// outside the canvas; inset, its outer edge lands exactly on it, so
	// the boundary pixel is fully opaque for a different reason — and
	// pixel 4 is inside the stroke either way. What separates them is
	// the far side: uninset, the stroke's outer half is clipped away and
	// the rectangle reads 4 pixels narrower.
	if got := edgeAlpha(`<Figure><Rectangle Rect="0,0,1,1" Stroke="#ffffff" StrokeThickness="8"/></Figure>`); got != 0xffff {
		t.Errorf("the boundary pixel of a stroked rectangle has alpha %d, want 65535; the stroke's outer edge must land ON the boundary rather than half outside it", got)
	}
}

// ---- damage ----

// TestAFigureDoesNotRepaintWhenItsNeighbourChanges is the claim paint
// makes and the only kind of assertion that can pin it: a bounds check
// or a "the cell says X" check passes just as well when the entire tree
// repainted.
//
// It matters more here than for an ordinary component. A figure's
// repaint is a rasterization and, at the pixel tier, a re-encode of the
// image — so a figure that repainted with its neighbours would make
// every keystroke in a page of plates cost the whole page.
func TestAFigureDoesNotRepaintWhenItsNeighbourChanges(t *testing.T) {
	label := prop.NewSource("before")
	c := ctx()
	c.Values = map[string]any{"Label": label}
	w, err := markup.Build(page(`<Grid Rows="Auto,*"><Text Grid.Row="0">{{.Label}}</Text><Figure Grid.Row="1"><Ellipse Stroke="#6c9cff" StrokeThickness="2"/></Figure></Grid>`), c)
	if err != nil {
		t.Fatal(err)
	}
	comp := gooey.NewComposer(w, 20, 8)
	comp.SetCaps(term.Caps{Cols: 20, Rows: 8, CellW: 8, CellH: 16, Color: render.TrueColor})
	comp.SetGraphics(graphics.Sixel{})
	comp.Frame()

	label.Set("after")
	f, painted := comp.Frame()
	if painted != 1 {
		t.Fatalf("a neighbour's change painted %d components, want exactly 1 (the Text)", painted)
	}
	// The figure's placement survives the frame it did not repaint —
	// placements are filed under the paint node that recorded them, so a
	// component that did not run must not lose its image.
	if len(f.Placements()) != 1 {
		t.Fatalf("the figure's placement count is %d after a frame it did not paint, want 1", len(f.Placements()))
	}
}

// TestAFigureRepaintsWhenItIsResized is the discriminating half. Without
// it the test above passes for a figure that never repaints at all,
// which is a different bug wearing the same green.
func TestAFigureRepaintsWhenItIsResized(t *testing.T) {
	w := buildPage(t, `<Figure><Ellipse Stroke="#6c9cff" StrokeThickness="2"/></Figure>`)
	comp := gooey.NewComposer(w, 20, 8)
	comp.SetCaps(term.Caps{Cols: 20, Rows: 8, CellW: 8, CellH: 16, Color: render.TrueColor})
	comp.SetGraphics(graphics.Sixel{})
	comp.Frame()

	comp.Resize(30, 8)
	f, painted := comp.Frame()
	if painted == 0 {
		t.Fatal("a resize repainted nothing; the figure is deaf to its own bounds")
	}
	p := f.Placements()
	if len(p) != 1 || p[0].Cols != 30 {
		t.Fatalf("after a resize the placement covers %v, want 30 cells wide", p)
	}
	if b := p[0].Img.Bounds(); b.Dx() != 30*8 {
		t.Fatalf("the canvas is %d px wide after a resize to 30 cells, want 240 — a memo that ignored the size would return the old %d", b.Dx(), 20*8)
	}
}

// ---- helpers ----

func isShade(r rune) bool {
	for _, s := range shades {
		if r == s {
			return true
		}
	}
	return false
}

func rowOf(b *render.Buffer, y, cols int) string {
	var sb strings.Builder
	for x := 0; x < cols; x++ {
		sb.WriteRune(b.At(x, y).Rune)
	}
	return sb.String()
}
