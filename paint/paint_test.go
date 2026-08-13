// Tests for the parsing and geometry half of paint. The drawing half is
// gg's and is not retested here — the package's whole design claim is that it
// adds no vocabulary of its own — so what is left is exactly the code that
// CAN be wrong: the MAUI attribute readers, the two places gg's vocabulary
// does not line up with MAUI's, the canvas size guard, and Ring's geometry.
//
// Every error case asserts the SHAPE of the message rather than err != nil.
// Almost everything in this repo fails at load, so existence proves nothing
// about which check caught it: a ParseColor that rejected every input would
// pass an err != nil suite completely.
package paint

import (
	"image"
	"strings"
	"testing"

	"github.com/fogleman/gg"

	"github.com/WonderForgeLabs/gooey/render"
)

func TestParseColorAcceptsBothLiteralForms(t *testing.T) {
	for _, c := range []struct {
		in   string
		want render.Color
	}{
		{"#6c9cff", render.RGB(0x6c, 0x9c, 0xff)},
		{"#000000", render.RGB(0, 0, 0)},
		{"#ffffff", render.RGB(0xff, 0xff, 0xff)},
		// #rgb doubles each nibble, so #f0a is #ff00aa and NOT #0f0a00 or
		// any other packing. Asserting the expansion rather than just
		// "no error" is the point: a wrong expansion parses fine.
		{"#f0a", render.RGB(0xff, 0x00, 0xaa)},
		{"#abc", render.RGB(0xaa, 0xbb, 0xcc)},
		// Outer whitespace is trimmed, because an attribute value written
		// across a line in markup arrives padded.
		{"  #6c9cff\n", render.RGB(0x6c, 0x9c, 0xff)},
	} {
		got, err := ParseColor(c.in)
		if err != nil {
			t.Errorf("ParseColor(%q) failed: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseColor(%q) = %+v, want %+v", c.in, got, c.want)
		}
	}
}

func TestParseColorRejectsJunkAtLoadTime(t *testing.T) {
	for _, in := range []string{
		"",           // an omitted attribute must not silently mean black
		"#",          //
		"#12",        // too short for either form
		"#12345",     // between the two forms, which is nobody's colour
		"#1234567",   // too long
		"#gggggg",    // right length, not hex
		"#12345g",    // one bad nibble is still bad
		"red",        // MAUI's named colours are not implemented; say so
		"rgb(1,2,3)", //
		"#-12345",    // a sign must not sneak through ParseUint
		"#ff_ff_ff",  // underscores are only legal at base 0
	} {
		_, err := ParseColor(in)
		if err == nil {
			t.Errorf("ParseColor(%q) succeeded; a colour literal that is not "+
				"#rgb or #rrggbb has to fail at LOAD time, not paint as black later", in)
			continue
		}
		// The message has to name the accepted forms and echo what it got,
		// because it is read by someone staring at one attribute in a file.
		if !strings.Contains(err.Error(), "#rgb") || !strings.Contains(err.Error(), "#rrggbb") {
			t.Errorf("ParseColor(%q) error %q does not name the accepted forms", in, err)
		}
	}
}

// TestParseBrushTakesAColourLiteral pins MAUI's shorthand: Stroke="#6c9cff" is
// a SolidColorBrush, so the simple case needs no property element.
func TestParseBrushTakesAColourLiteral(t *testing.T) {
	pat, c, err := ParseBrush("#6c9cff")
	if err != nil {
		t.Fatalf("ParseBrush: %v", err)
	}
	if want := render.RGB(0x6c, 0x9c, 0xff); c != want {
		t.Errorf("fallback colour = %+v, want %+v", c, want)
	}
	if pat == nil {
		t.Fatal("ParseBrush returned a nil pattern")
	}
	if got := FromColor(pat.ColorAt(0, 0)); got != c {
		t.Errorf("the pattern paints %+v, want the parsed colour %+v", got, c)
	}
	if _, _, err := ParseBrush("nope"); err == nil {
		t.Error("ParseBrush accepted a non-colour; the colour error must propagate")
	}
}

func TestColorRoundTrips(t *testing.T) {
	for _, c := range []render.Color{
		render.RGB(0, 0, 0),
		render.RGB(0xff, 0xff, 0xff),
		render.RGB(0x6c, 0x9c, 0xff),
		render.RGB(1, 2, 3),
	} {
		if got := FromColor(Color(c)); got != c {
			t.Errorf("FromColor(Color(%+v)) = %+v; both sides are 8 bits per "+
				"channel, so nothing may be lost", c, got)
		}
	}
}

func TestParseDashArrayTakesCommasAndSpaces(t *testing.T) {
	for _, c := range []struct {
		in   string
		want []float64
	}{
		{"", nil},
		{"4,2", []float64{4, 2}},
		{"4 2", []float64{4, 2}},
		{"4, 2", []float64{4, 2}},
		{" 4 , 2 , 1 ", []float64{4, 2, 1}},
		{"2.5,0.5", []float64{2.5, 0.5}},
		// Zero is legal — MAUI uses it with a round cap to draw dots.
		{"0,4", []float64{0, 4}},
	} {
		got, err := ParseDashArray(c.in)
		if err != nil {
			t.Errorf("ParseDashArray(%q) failed: %v", c.in, err)
			continue
		}
		if len(got) != len(c.want) {
			t.Errorf("ParseDashArray(%q) = %v, want %v", c.in, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("ParseDashArray(%q) = %v, want %v", c.in, got, c.want)
				break
			}
		}
	}
}

func TestParseDashArrayRejectsNegativesAndJunk(t *testing.T) {
	for _, c := range []struct{ in, wants string }{
		// A negative length is the interesting one: it parses as a float, so
		// only the explicit guard catches it, and gg would take it and draw
		// nothing at all.
		{"4,-2", "negative"},
		{"-1", "negative"},
		{"4,x", "StrokeDashArray"},
		{"4,,2", "StrokeDashArray"}, // FieldsFunc drops the empty field, so
		// this one actually succeeds as {4,2}; see the check below.
	} {
		_, err := ParseDashArray(c.in)
		if c.in == "4,,2" {
			if err != nil {
				t.Errorf("ParseDashArray(%q) failed: %v; a doubled separator is "+
					"harmless and FieldsFunc already drops it", c.in, err)
			}
			continue
		}
		if err == nil {
			t.Errorf("ParseDashArray(%q) succeeded, want an error", c.in)
			continue
		}
		if !strings.Contains(err.Error(), c.wants) {
			t.Errorf("ParseDashArray(%q) error %q does not mention %q, so it does "+
				"not say which rule was broken", c.in, err, c.wants)
		}
	}
}

// TestParseLineCapIncludingMAUIsFlat pins the first of the two places where
// MAUI's vocabulary and gg's do not line up. MAUI says Flat, gg says Butt;
// the geometry is identical and the markup uses MAUI's word, so both spellings
// have to land on the same value or a MAUI author's file is rejected.
func TestParseLineCapIncludingMAUIsFlat(t *testing.T) {
	for _, c := range []struct {
		in   string
		want gg.LineCap
	}{
		{"", gg.LineCapButt}, // omitted attribute -> MAUI's default
		{"Flat", gg.LineCapButt},
		{"Butt", gg.LineCapButt},
		{"Square", gg.LineCapSquare},
		{"Round", gg.LineCapRound},
		{" Round ", gg.LineCapRound},
	} {
		got, err := ParseLineCap(c.in)
		if err != nil {
			t.Errorf("ParseLineCap(%q) failed: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseLineCap(%q) = %v, want %v", c.in, got, c.want)
		}
	}
	// Discrimination: the three named caps must be three DIFFERENT values, or
	// the table above would pass with every arm returning the same thing.
	flat, _ := ParseLineCap("Flat")
	square, _ := ParseLineCap("Square")
	round, _ := ParseLineCap("Round")
	if flat == square || square == round || flat == round {
		t.Errorf("the caps collapsed: Flat=%v Square=%v Round=%v", flat, square, round)
	}
	for _, bad := range []string{"None", "flat", "Butt Cap", "0"} {
		_, err := ParseLineCap(bad)
		if err == nil {
			t.Errorf("ParseLineCap(%q) succeeded; an unknown cap has to fail at load", bad)
			continue
		}
		if !strings.Contains(err.Error(), "StrokeLineCap") {
			t.Errorf("ParseLineCap(%q) error %q does not name the attribute", bad, err)
		}
	}
}

// TestParseLineJoinAcceptsMiterAndDrawsItAsBevel pins the second and sharper
// deviation, and it is a deliberate lie in the good sense: gg has no miter
// join at all, so Miter is ACCEPTED and drawn as Bevel. MAUI authors write
// Miter reflexively and rejecting it would be worse than a slightly different
// corner — but the substitution is invisible at runtime, so this test is the
// only thing that says it is on purpose. If gg ever grows a miter join, this
// test fails and that is correct: the decision needs revisiting, not the
// expectation quietly updating.
func TestParseLineJoinAcceptsMiterAndDrawsItAsBevel(t *testing.T) {
	miter, err := ParseLineJoin("Miter")
	if err != nil {
		t.Fatalf("ParseLineJoin(%q) failed: %v; MAUI's default join must be accepted", "Miter", err)
	}
	bevel, err := ParseLineJoin("Bevel")
	if err != nil {
		t.Fatalf("ParseLineJoin(%q) failed: %v", "Bevel", err)
	}
	if miter != bevel {
		t.Errorf("Miter = %v, Bevel = %v; gg has no miter join, so Miter is "+
			"documented as collapsing to Bevel", miter, bevel)
	}
	if miter != gg.LineJoinBevel {
		t.Errorf("Miter = %v, want gg.LineJoinBevel", miter)
	}
	empty, _ := ParseLineJoin("")
	if empty != gg.LineJoinBevel {
		t.Errorf("the omitted attribute = %v, want gg.LineJoinBevel (MAUI's default is Miter)", empty)
	}
	round, err := ParseLineJoin("Round")
	if err != nil {
		t.Fatalf("ParseLineJoin(%q) failed: %v", "Round", err)
	}
	// Discrimination: Round has to be distinguishable, or "everything is
	// Bevel" would satisfy every assertion above.
	if round == bevel {
		t.Error("Round and Bevel are the same value; the join is not being read at all")
	}
	for _, bad := range []string{"None", "miter", "Mitre", "0"} {
		_, err := ParseLineJoin(bad)
		if err == nil {
			t.Errorf("ParseLineJoin(%q) succeeded; an unknown join has to fail at load", bad)
			continue
		}
		if !strings.Contains(err.Error(), "StrokeLineJoin") {
			t.Errorf("ParseLineJoin(%q) error %q does not name the attribute", bad, err)
		}
	}
}

// TestCanvasRefusesAZeroCellSize is the guard that turns a silent black frame
// into a load error. A terminal that was never probed reports 0x0 pixels per
// cell; without this, Canvas would hand back a 0x0 context and everything
// drawn into it would vanish with no error anywhere.
func TestCanvasRefusesAZeroCellSize(t *testing.T) {
	for _, c := range []struct{ cols, rows, cw, ch int }{
		{20, 10, 0, 0}, // never probed
		{20, 10, 8, 0}, // half probed
		{20, 10, 0, 16},
		{0, 10, 8, 16}, // and a zero-sized component
		{20, 0, 8, 16},
	} {
		dc, err := Canvas(c.cols, c.rows, c.cw, c.ch)
		if err == nil {
			t.Errorf("Canvas(%d,%d,%d,%d) succeeded, want an empty-canvas error",
				c.cols, c.rows, c.cw, c.ch)
			continue
		}
		if dc != nil {
			t.Errorf("Canvas(%d,%d,%d,%d) returned a context alongside its error",
				c.cols, c.rows, c.cw, c.ch)
		}
		if !strings.Contains(err.Error(), "cell size") {
			t.Errorf("Canvas error %q does not point at the cause (an unprobed "+
				"terminal), which is the whole reason the guard exists", err)
		}
	}
}

// TestCanvasIsSizedInPixelsNotCells pins the arithmetic the doc comment
// promises: art is rasterized at 1:1 for the cells it will occupy, so a caller
// never gets its work resampled by the pixel pipeline.
func TestCanvasIsSizedInPixelsNotCells(t *testing.T) {
	dc, err := Canvas(20, 10, 8, 16)
	if err != nil {
		t.Fatalf("Canvas: %v", err)
	}
	if got, want := dc.Width(), 20*8; got != want {
		t.Errorf("width = %d, want %d (cols*cellW, not cols)", got, want)
	}
	if got, want := dc.Height(), 10*16; got != want {
		t.Errorf("height = %d, want %d (rows*cellH, not rows)", got, want)
	}
}

// TestRingSlicesTheFrameAndLeavesTheInteriorAlone is the geometry claim in
// Ring's comment, asserted rather than described.
//
// Placements composite OVER the cell plane, so an image spanning a pane would
// bury the pane's own text. The ring is the fix: four edge rectangles, and an
// interior that is never covered at all so a terminal can keep drawing text
// there. Three things have to hold together — the four cover the whole
// perimeter, they do not overlap each other, and none of them touches the
// interior. Any one alone is satisfiable by a wrong answer.
func TestRingSlicesTheFrameAndLeavesTheInteriorAlone(t *testing.T) {
	const cellW, cellH = 8, 16
	const cols, rows = 20, 10
	img := image.NewRGBA(image.Rect(0, 0, cols*cellW, rows*cellH))

	top, bottom, left, right := Ring(img, cellW, cellH)
	parts := map[string]image.Rectangle{
		"top":    top.Bounds(),
		"bottom": bottom.Bounds(),
		"left":   left.Bounds(),
		"right":  right.Bounds(),
	}

	want := map[string]image.Rectangle{
		"top":    image.Rect(0, 0, cols*cellW, cellH),
		"bottom": image.Rect(0, (rows-1)*cellH, cols*cellW, rows*cellH),
		"left":   image.Rect(0, cellH, cellW, (rows-1)*cellH),
		"right":  image.Rect((cols-1)*cellW, cellH, cols*cellW, (rows-1)*cellH),
	}
	for name, w := range want {
		if got := parts[name]; got != w {
			t.Errorf("%s = %v, want %v", name, got, w)
		}
	}

	// Pairwise disjoint. A slice that overlapped its neighbour would paint a
	// corner twice, which for a translucent brush is visible.
	names := []string{"top", "bottom", "left", "right"}
	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			a, b := parts[names[i]], parts[names[j]]
			if !a.Intersect(b).Empty() {
				t.Errorf("%s %v overlaps %s %v", names[i], a, names[j], b)
			}
		}
	}

	// The interior is untouched by all four.
	interior := image.Rect(cellW, cellH, (cols-1)*cellW, (rows-1)*cellH)
	if interior.Empty() {
		t.Fatal("the interior rectangle is empty, so the check below is vacuous")
	}
	for _, name := range names {
		if !parts[name].Intersect(interior).Empty() {
			t.Errorf("%s %v reaches into the interior %v, which must stay on the "+
				"cell plane where the terminal draws text", name, parts[name], interior)
		}
	}

	// And together the four plus the interior account for every pixel: nothing
	// in the frame is left unclassified. Counted rather than reasoned about,
	// because an off-by-one at a corner is exactly what this would hide.
	area := interior.Dx() * interior.Dy()
	for _, name := range names {
		area += parts[name].Dx() * parts[name].Dy()
	}
	if got, want := area, cols*cellW*rows*cellH; got != want {
		t.Errorf("the four edges plus the interior cover %d px, want the whole "+
			"frame's %d px", got, want)
	}
}

// TestRingIsRelativeToTheSourceBoundsOrigin pins the `b.Min` arithmetic in
// crop. A caller passing a SubImage — which is what Ring itself returns, so
// nesting is natural — has a non-zero Min, and dropping it would slice from
// the wrong place while still returning correctly sized rectangles.
func TestRingIsRelativeToTheSourceBoundsOrigin(t *testing.T) {
	const cellW, cellH = 8, 16
	full := image.NewRGBA(image.Rect(0, 0, 40*cellW, 20*cellH))
	sub := full.SubImage(image.Rect(5*cellW, 3*cellH, 25*cellW, 13*cellH))

	top, _, _, right := Ring(sub, cellW, cellH)
	if got, want := top.Bounds().Min, (image.Point{X: 5 * cellW, Y: 3 * cellH}); got != want {
		t.Errorf("top starts at %v, want the source's own origin %v", got, want)
	}
	if got, want := right.Bounds().Max.X, 25*cellW; got != want {
		t.Errorf("right ends at x=%d, want the source's right edge %d", got, want)
	}
}

// TestRingSurvivesAFrameTooSmallToHaveOne. Below three cells tall there is no
// middle band, so left and right have a negative height. crop returns a 1x1
// placeholder rather than panicking on an invalid Rect, because this is
// reachable from a component the user simply resized small.
func TestRingSurvivesAFrameTooSmallToHaveOne(t *testing.T) {
	const cellW, cellH = 8, 16
	for _, rows := range []int{1, 2} {
		img := image.NewRGBA(image.Rect(0, 0, 4*cellW, rows*cellH))
		top, bottom, left, right := Ring(img, cellW, cellH)
		for name, p := range map[string]image.Image{
			"top": top, "bottom": bottom, "left": left, "right": right,
		} {
			if p == nil {
				t.Errorf("rows=%d: %s is nil", rows, name)
				continue
			}
			if b := p.Bounds(); b.Dx() < 1 || b.Dy() < 1 {
				t.Errorf("rows=%d: %s has empty bounds %v; a degenerate slice must "+
					"still be a usable image", rows, name, b)
			}
		}
	}
}

// TestGradientsAreRelativeToTheTarget pins MAUI's coordinate convention:
// StartPoint and EndPoint are 0..1, so a brush is independent of the size of
// the thing it paints and the multiply happens here. A caller that got this
// wrong would see gradients that look right at one size and collapse at
// another, which is not a failure any type checks.
func TestGradientsAreRelativeToTheTarget(t *testing.T) {
	black, white := render.RGB(0, 0, 0), render.RGB(0xff, 0xff, 0xff)
	stops := []GradientStop{{Color: black, Offset: 0}, {Color: white, Offset: 1}}

	// The endpoints are sampled one pixel inside the target, so the
	// interpolation lands a hair short of the stop itself — measured, 254 not
	// 255. The claim under test is where the gradient SPANS, not the
	// rasterizer's last step, so the comparison carries a small tolerance and
	// the discrimination checks below are what keep that honest.
	const tol = 4
	near := func(a, b render.Color) bool {
		d := func(x, y uint8) int {
			if x > y {
				return int(x) - int(y)
			}
			return int(y) - int(x)
		}
		return d(a.R, b.R) <= tol && d(a.G, b.G) <= tol && d(a.B, b.B) <= tol
	}

	const w, h = 200, 100
	lin := LinearGradient(w, h, 0, 0, 1, 0, stops)
	if got := FromColor(lin.ColorAt(0, h/2)); !near(got, black) {
		t.Errorf("linear gradient at the start point = %+v, want %+v", got, black)
	}
	if got := FromColor(lin.ColorAt(w-1, h/2)); !near(got, white) {
		t.Errorf("linear gradient at the end point = %+v, want %+v; EndPoint=1 "+
			"has to mean the far edge of a %d-wide target, not one pixel across", got, white, w)
	}
	// Discrimination: the middle must be far from both ends, or a gradient
	// that returned one flat colour would satisfy one of the two assertions
	// above and a tolerance wide enough to hide the bug would satisfy both.
	mid := FromColor(lin.ColorAt(w/2, h/2))
	if near(mid, black) || near(mid, white) {
		t.Errorf("the midpoint is %+v, which is one of the endpoints; the gradient "+
			"is not interpolating across the target", mid)
	}

	rad := RadialGradient(w, h, 0.5, 0.5, 0.5, stops)
	if got := FromColor(rad.ColorAt(w/2, h/2)); !near(got, black) {
		t.Errorf("radial gradient at its centre = %+v, want the first stop %+v", got, black)
	}
	if near(FromColor(rad.ColorAt(w/2, h/2)), FromColor(rad.ColorAt(0, h/2))) {
		t.Error("the radial gradient paints its centre and its edge the same colour")
	}
}

// TestStrokeApplyClearsADashItDoesNotHave. Apply is nearly all delegation, but
// this one line is a decision: an empty Dash must CLEAR gg's dash state rather
// than leave whatever the previous stroke set. Contexts are reused across
// strokes, so "no dash" has to mean no dash.
func TestStrokeApplyClearsADashItDoesNotHave(t *testing.T) {
	dc, err := Canvas(10, 4, 8, 16)
	if err != nil {
		t.Fatalf("Canvas: %v", err)
	}
	dc.SetColor(Color(render.RGB(0xff, 0xff, 0xff)))

	// The dash is 20 on, 20 off rather than something tight, so a gap is
	// genuinely empty. Measured, a 4-on 2-off dash at this line width leaves
	// its gaps at alpha 0xbf — the rasterizer bleeds across two pixels — which
	// is indistinguishable from the antialiased END of a solid line (0xc3).
	// A threshold between those two numbers is not a test, it is a coin.
	Stroke{Thickness: 2, Dash: []float64{20, 20}}.Apply(dc)
	dc.DrawLine(0, 4, float64(dc.Width()), 4)
	dc.Stroke()

	gapX := -1
	for x := 0; x < dc.Width(); x++ {
		if _, _, _, a := dc.Image().At(x, 4).RGBA(); a == 0 {
			gapX = x
			break
		}
	}
	if gapX < 0 {
		t.Fatal("the dashed line has no empty pixel, so the check below cannot " +
			"tell a cleared dash from an uncleared one")
	}

	// Same context, same pen except for the dash: the second line must be
	// continuous where the first had a hole.
	Stroke{Thickness: 2}.Apply(dc)
	dc.DrawLine(0, 20, float64(dc.Width()), 20)
	dc.Stroke()
	if _, _, _, a := dc.Image().At(gapX, 20).RGBA(); a != 0xffff {
		t.Errorf("the solid stroke is not opaque at x=%d (alpha %#x), where the "+
			"dashed one had a gap — so an empty Dash did not clear the previous "+
			"stroke's dash pattern", gapX, a)
	}
}
