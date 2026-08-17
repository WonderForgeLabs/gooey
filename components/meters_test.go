package components

import (
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// The fill meters truncate rather than round: fill = v*w/100 in integer
// arithmetic. That boundary is invisible to a bounds assertion and to a
// damage count, and an off-by-one in it compiles fine — so it is pinned
// here as literal rows, at the values where rounding and truncation
// disagree. Gauge reserves a cell of breathing room after the readout
// (25 track cells at width 34 with a 3-rune label); ProgressBar spends
// the whole remainder (26). 50% is the case that matters most: 12 of 25
// and 13 of 26, both truncated down.
func TestFillMeterTruncatesAtTheBoundary(t *testing.T) {
	const w = 34
	for _, tc := range []struct {
		v           int
		gauge, pbar string
	}{
		{0, "cpu░░░░░░░░░░░░░░░░░░░░░░░░░   0%", "cpu░░░░░░░░░░░░░░░░░░░░░░░░░░   0%"},
		{1, "cpu░░░░░░░░░░░░░░░░░░░░░░░░░   1%", "cpu░░░░░░░░░░░░░░░░░░░░░░░░░░   1%"},
		{49, "cpu████████████░░░░░░░░░░░░░  49%", "cpu████████████░░░░░░░░░░░░░░  49%"},
		{50, "cpu████████████░░░░░░░░░░░░░  50%", "cpu█████████████░░░░░░░░░░░░░  50%"},
		{51, "cpu████████████░░░░░░░░░░░░░  51%", "cpu█████████████░░░░░░░░░░░░░  51%"},
		{99, "cpu████████████████████████░  99%", "cpu█████████████████████████░  99%"},
		{100, "cpu█████████████████████████ 100%", "cpu██████████████████████████ 100%"},
	} {
		buf := render.NewBuffer(w, 1)
		g := &Gauge{Value: prop.NewSource(tc.v), Label: prop.NewSource("cpu")}
		g.Arrange(gooey.Rect{X: 0, Y: 0, W: w, H: 1})
		g.Render(&gooey.Frame{Cells: buf})
		if got := row(buf, 0); got != tc.gauge {
			t.Errorf("Gauge at %d%%:\n got %q\nwant %q", tc.v, got, tc.gauge)
		}

		buf = render.NewBuffer(w, 1)
		p := &ProgressBar{Value: prop.NewSource(tc.v), Label: prop.NewSource("cpu")}
		p.Arrange(gooey.Rect{X: 0, Y: 0, W: w, H: 1})
		p.Render(&gooey.Frame{Cells: buf})
		if got := row(buf, 0); got != tc.pbar {
			t.Errorf("ProgressBar at %d%%:\n got %q\nwant %q", tc.v, got, tc.pbar)
		}
	}
}

// Gauge paints its empty half in the value's own style; ProgressBar dims
// it. That is the one thing the shared track deliberately does not share,
// so it needs its own pin — the golden rows above compare runes only.
func TestFillMeterEmptyHalfStyleDiffersByComponent(t *testing.T) {
	// Cell 4 is inside the track and, at 0%, inside its empty half.
	const w = 34

	buf := render.NewBuffer(w, 1)
	g := &Gauge{Value: prop.NewSource(0), Label: prop.NewSource("cpu")}
	g.Arrange(gooey.Rect{X: 0, Y: 0, W: w, H: 1})
	g.Render(&gooey.Frame{Cells: buf})
	if got, want := buf.At(4, 0).Style, thresholdStyle(0); got != want {
		t.Errorf("Gauge empty cell style = %+v, want the value's style %+v", got, want)
	}

	buf = render.NewBuffer(w, 1)
	p := &ProgressBar{Value: prop.NewSource(0), Label: prop.NewSource("cpu")}
	p.Arrange(gooey.Rect{X: 0, Y: 0, W: w, H: 1})
	p.Render(&gooey.Frame{Cells: buf})
	if got := buf.At(4, 0).Style; got != styleDim {
		t.Errorf("ProgressBar empty cell style = %+v, want styleDim %+v", got, styleDim)
	}
}

// Both meters default to 34 cells wide and one row, and clamp to what is
// offered.
func TestFillMeterMeasure(t *testing.T) {
	for _, tc := range []struct {
		pref  int
		avail gooey.Size
		want  gooey.Size
	}{
		{0, gooey.Size{W: 80, H: 24}, gooey.Size{W: 34, H: 1}},
		{0, gooey.Size{W: 10, H: 24}, gooey.Size{W: 10, H: 1}},
		{12, gooey.Size{W: 80, H: 24}, gooey.Size{W: 12, H: 1}},
		{12, gooey.Size{W: 80, H: 0}, gooey.Size{W: 12, H: 0}},
	} {
		if got := (&Gauge{Width: tc.pref}).Measure(tc.avail); got != tc.want {
			t.Errorf("Gauge{Width:%d}.Measure(%+v) = %+v, want %+v", tc.pref, tc.avail, got, tc.want)
		}
		if got := (&ProgressBar{Width: tc.pref}).Measure(tc.avail); got != tc.want {
			t.Errorf("ProgressBar{Width:%d}.Measure(%+v) = %+v, want %+v", tc.pref, tc.avail, got, tc.want)
		}
	}
}

// A nil Value reads 0 rather than panicking — the meters are usable
// straight out of a struct literal.
func TestFillMeterNilValue(t *testing.T) {
	buf := render.NewBuffer(34, 1)
	g := &Gauge{}
	g.Arrange(gooey.Rect{X: 0, Y: 0, W: 34, H: 1})
	g.Render(&gooey.Frame{Cells: buf})
	if got, want := row(buf, 0), "░░░░░░░░░░░░░░░░░░░░░░░░░░░░   0%"; got != want {
		t.Errorf("nil-Value Gauge row = %q, want %q", got, want)
	}

	buf = render.NewBuffer(34, 1)
	p := &ProgressBar{}
	p.Arrange(gooey.Rect{X: 0, Y: 0, W: 34, H: 1})
	p.Render(&gooey.Frame{Cells: buf})
	if got, want := row(buf, 0), "░░░░░░░░░░░░░░░░░░░░░░░░░░░░░   0%"; got != want {
		t.Errorf("nil-Value ProgressBar row = %q, want %q", got, want)
	}
}

// The shared track is a plain function called from inside each
// component's own Render, so the Get on Value still lands in that
// component's paint node and nowhere else: moving one meter repaints
// exactly one component, not both.
func TestFillMeterValueRepaintsOnlyItsOwnComponent(t *testing.T) {
	gv, pv := prop.NewSource(10), prop.NewSource(10)
	root := &VStack{Children: []gooey.Component{
		&Gauge{Value: gv, Label: prop.NewSource("g")},
		&ProgressBar{Value: pv, Label: prop.NewSource("p")},
	}}
	c := gooey.NewComposer(root, 40, 4)
	c.Frame()

	gv.Set(90)
	if _, painted := c.Frame(); painted != 1 {
		t.Fatalf("Gauge value change painted %d components, want exactly 1", painted)
	}
	pv.Set(90)
	if _, painted := c.Frame(); painted != 1 {
		t.Fatalf("ProgressBar value change painted %d components, want exactly 1", painted)
	}
}
