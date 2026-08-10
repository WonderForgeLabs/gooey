package components

import (
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
	"github.com/WonderForgeLabs/gooey/term"
)

func pickerAt(depth render.ColorDepth, c render.Color) (*ColorPicker, *prop.Property[render.Color], *gooey.Frame) {
	v := prop.NewSource(c)
	p := &ColorPicker{Value: v}
	f := gooey.Compose(p, term.Caps{Cols: 30, Rows: 5, Color: depth}, nil)
	return p, v, f
}

func rowText(f *gooey.Frame, y, w int) string {
	var sb strings.Builder
	for x := 0; x < w; x++ {
		sb.WriteRune(f.Cells.At(x, y).Rune)
	}
	return sb.String()
}

func TestColorPickerArrowsSelectChannelAndAdjustValue(t *testing.T) {
	p, v, _ := pickerAt(render.TrueColor, render.RGB(10, 20, 30))

	if got := p.Channel(); got != channelR {
		t.Fatalf("initial channel = %d, want R", got)
	}
	p.HandleKey(input.Named(input.KeyRight))
	if got := v.Get().R; got != 11 {
		t.Errorf("right on R: R = %d, want 11", got)
	}
	p.HandleKey(input.Named(input.KeyDown))
	if got := p.Channel(); got != channelG {
		t.Errorf("after down: channel = %d, want G", got)
	}
	p.HandleKey(input.Named(input.KeyLeft))
	if got := v.Get().G; got != 19 {
		t.Errorf("left on G: G = %d, want 19", got)
	}
	// Shift is the coarse step.
	p.HandleKey(input.KeyEvent{Key: input.KeyRight, Mods: input.ModShift})
	if got := v.Get().G; got != 35 {
		t.Errorf("shift+right on G: G = %d, want 35", got)
	}
	// Other channels are untouched by an edit.
	if got := v.Get().B; got != 30 {
		t.Errorf("B drifted to %d during G edits", got)
	}
}

func TestColorPickerChannelSelectionAndValuesClamp(t *testing.T) {
	p, v, _ := pickerAt(render.TrueColor, render.RGB(2, 0, 0))

	p.HandleKey(input.Named(input.KeyUp)) // already on R
	if got := p.Channel(); got != channelR {
		t.Errorf("up from R = %d, want to stay on R", got)
	}
	for i := 0; i < 5; i++ {
		p.HandleKey(input.Named(input.KeyDown))
	}
	if got := p.Channel(); got != channelB {
		t.Errorf("down past B = %d, want to stop at B", got)
	}

	p.selectChannel(channelR)
	for i := 0; i < 4; i++ {
		p.HandleKey(input.Named(input.KeyLeft))
	}
	if got := v.Get().R; got != 0 {
		t.Errorf("R underflowed to %d, want clamp at 0", got)
	}
	p.HandleKey(input.Named(input.KeyEnd))
	if got := v.Get().R; got != 255 {
		t.Errorf("end: R = %d, want 255", got)
	}
	p.HandleKey(input.KeyEvent{Key: input.KeyRight, Mods: input.ModShift})
	if got := v.Get().R; got != 255 {
		t.Errorf("R overflowed to %d, want clamp at 255", got)
	}
	p.HandleKey(input.Named(input.KeyHome))
	if got := v.Get().R; got != 0 {
		t.Errorf("home: R = %d, want 0", got)
	}
}

func TestColorPickerHJKLMatchTheArrows(t *testing.T) {
	p, v, _ := pickerAt(render.TrueColor, render.RGB(100, 100, 100))
	p.HandleKey(input.Rune('j'))
	if got := p.Channel(); got != channelG {
		t.Errorf("j: channel = %d, want G", got)
	}
	p.HandleKey(input.Rune('l'))
	if got := v.Get().G; got != 101 {
		t.Errorf("l: G = %d, want 101", got)
	}
	p.HandleKey(input.Rune('L'))
	if got := v.Get().G; got != 117 {
		t.Errorf("L (coarse): G = %d, want 117", got)
	}
	p.HandleKey(input.Rune('k'))
	if got := p.Channel(); got != channelR {
		t.Errorf("k: channel = %d, want R", got)
	}
}

// An unhandled key must bubble, or the picker would swallow the page's
// gestures (q to quit, tab to move on) while focused.
func TestColorPickerDeclinesKeysItDoesNotUse(t *testing.T) {
	p, _, _ := pickerAt(render.TrueColor, render.RGB(0, 0, 0))
	for _, ev := range []input.KeyEvent{input.Rune('q'), input.Named(input.KeyTab), input.Named(input.KeyEnter)} {
		if p.HandleKey(ev) {
			t.Errorf("picker consumed %v, which belongs to the page", ev)
		}
	}
}

func TestColorPickerHexAndBoundValue(t *testing.T) {
	p, _, _ := pickerAt(render.TrueColor, render.RGB(255, 170, 60))
	if got, want := p.Hex(), "#FFAA3C"; got != want {
		t.Errorf("Hex = %q, want %q", got, want)
	}
}

// An unbound picker must stay inert rather than panicking — markup can
// legally omit Value.
func TestColorPickerWithoutValueIsInert(t *testing.T) {
	p := &ColorPicker{}
	f := gooey.Compose(p, term.Caps{Cols: 30, Rows: 5}, nil)
	p.HandleKey(input.Named(input.KeyRight))
	p.HandleMouse(input.MouseEvent{Kind: input.MousePress, X: 5, Y: 0})
	if got := p.Hex(); got != "#808080" {
		t.Errorf("unbound picker Hex = %q, want the mid-gray default", got)
	}
	_ = f
}

// ---- capability tiers ----

// TrueColor paints a real gradient: each cell of a bar carries a
// different color, swept across that channel.
func TestColorPickerTrueColorBarsAreGradients(t *testing.T) {
	p, _, f := pickerAt(render.TrueColor, render.RGB(0, 0, 0))
	w := p.barWidth()

	first := f.Cells.At(pickerLabelW+1, 0).Style.Fg
	last := f.Cells.At(pickerLabelW+w-1, 0).Style.Fg
	if !first.Set || !last.Set {
		t.Fatal("gradient cells have no color")
	}
	if last.R <= first.R {
		t.Errorf("R bar does not ascend: first R=%d, last R=%d", first.R, last.R)
	}
	// Sweeping R must not disturb G or B along the bar.
	if first.G != 0 || last.G != 0 {
		t.Errorf("R bar leaked into G: %d..%d", first.G, last.G)
	}
	// Distinct colors across the row is what makes it a gradient rather
	// than a meter.
	seen := map[render.Color]bool{}
	for x := pickerLabelW; x < pickerLabelW+w; x++ {
		seen[f.Cells.At(x, 0).Style.Fg] = true
	}
	if len(seen) < w/2 {
		t.Errorf("only %d distinct colors across %d cells; expected a gradient", len(seen), w)
	}
}

// 16-color terminals get a fill meter instead: a gradient across 16
// buckets would be a lie. The filled part carries the ANSI color the
// value maps to, and the empty part is dim ░.
func TestColorPicker16UsesAFillMeter(t *testing.T) {
	// A mid-range R so the meter is partly filled.
	cur := render.RGB(100, 170, 60)
	p, _, f := pickerAt(render.Color16, cur)
	w := p.barWidth()
	row := rowText(f, 0, pickerLabelW+w)

	if !strings.Contains(row, "░") {
		t.Errorf("16-color bar has no empty run: %q", row)
	}
	if !strings.Contains(row, "█") {
		t.Errorf("16-color bar has no filled run: %q", row)
	}
	fill := render.Approximate(cur, render.Color16)
	seen := map[render.Color]bool{}
	for x := pickerLabelW; x < pickerLabelW+w; x++ {
		if c := f.Cells.At(x, 0).Style.Fg; c.Set {
			seen[c] = true
		}
	}
	// The filled run, the dim empty run, and the cursor cell — three
	// colors at most, and emphatically not a gradient.
	if len(seen) > 3 {
		t.Errorf("16-color bar used %d colors; expected a single fill color", len(seen))
	}
	if !seen[fill] {
		t.Errorf("16-color bar does not use the approximated color %v; got %v", fill, seen)
	}
}

// The readout is where each tier tells the truth about what the terminal
// will really display.
func TestColorPickerReadoutIsTierSpecific(t *testing.T) {
	cases := []struct {
		depth render.ColorDepth
		want  string
	}{
		{render.TrueColor, "#FFAA3C"},
		{render.Color256, "#FFAA3C → xterm 215"},
		{render.Color16, "#FFAA3C ≈ yellow"},
	}
	for _, tc := range cases {
		t.Run(tc.depth.String(), func(t *testing.T) {
			_, _, f := pickerAt(tc.depth, render.RGB(255, 170, 60))
			row := rowText(f, 4, 30)
			if !strings.Contains(row, tc.want) {
				t.Errorf("readout %q does not contain %q", row, tc.want)
			}
		})
	}
	// Truecolor must NOT claim a palette index it isn't using.
	_, _, f := pickerAt(render.TrueColor, render.RGB(255, 170, 60))
	if row := rowText(f, 4, 30); strings.Contains(row, "xterm") {
		t.Errorf("truecolor readout mentions a palette index: %q", row)
	}
}

// The swatch shows what the terminal will ACTUALLY display, which at a
// reduced depth is not the requested color.
func TestColorPickerSwatchShowsTheApproximatedColor(t *testing.T) {
	want := render.RGB(255, 170, 60)
	_, _, f := pickerAt(render.TrueColor, want)
	if got := f.Cells.At(0, 4).Style.Fg; got != want {
		t.Errorf("truecolor swatch = %v, want the exact color %v", got, want)
	}
	_, _, f16 := pickerAt(render.Color16, want)
	if got, approx := f16.Cells.At(0, 4).Style.Fg, render.Approximate(want, render.Color16); got != approx {
		t.Errorf("16-color swatch = %v, want the approximation %v", got, approx)
	}
}

// ---- pointer ----

func TestColorPickerClickSetsChannelFromPosition(t *testing.T) {
	p, v, _ := pickerAt(render.TrueColor, render.RGB(0, 0, 0))
	w := p.barWidth()

	// Click the far right of the B row: saturate blue and select it.
	if !p.HandleMouse(input.MouseEvent{Kind: input.MousePress, X: pickerLabelW + w - 1, Y: 2}) {
		t.Fatal("click on the B bar was not handled")
	}
	if got := p.Channel(); got != channelB {
		t.Errorf("click selected channel %d, want B", got)
	}
	if got := v.Get().B; got != 255 {
		t.Errorf("click at the right edge set B = %d, want 255", got)
	}
	// Far left of the R row: zero it.
	p.HandleMouse(input.MouseEvent{Kind: input.MousePress, X: pickerLabelW, Y: 0})
	if got := v.Get().R; got != 0 {
		t.Errorf("click at the left edge set R = %d, want 0", got)
	}
	// A click outside the bars (on the readout row) is not ours.
	if p.HandleMouse(input.MouseEvent{Kind: input.MousePress, X: 3, Y: 4}) {
		t.Error("picker consumed a click on the readout row")
	}
}

func TestColorPickerWheelAdjustsTheRowUnderThePointer(t *testing.T) {
	p, v, _ := pickerAt(render.TrueColor, render.RGB(100, 100, 100))

	p.HandleMouse(input.MouseEvent{Kind: input.WheelUp, X: 5, Y: 1})
	if got := v.Get().G; got != 101 {
		t.Errorf("wheel up over G: G = %d, want 101", got)
	}
	if got := p.Channel(); got != channelG {
		t.Errorf("wheel did not select the row it landed on: channel = %d", got)
	}
	p.HandleMouse(input.MouseEvent{Kind: input.WheelDown, X: 5, Y: 1})
	p.HandleMouse(input.MouseEvent{Kind: input.WheelDown, X: 5, Y: 1})
	if got := v.Get().G; got != 99 {
		t.Errorf("wheel down twice: G = %d, want 99", got)
	}
	// Shift coarsens the wheel the same way it coarsens the arrows.
	p.HandleMouse(input.MouseEvent{Kind: input.WheelUp, X: 5, Y: 1, Mods: input.ModShift})
	if got := v.Get().G; got != 115 {
		t.Errorf("shift+wheel: G = %d, want 115", got)
	}
}

// Editing a color must repaint the picker and nothing else — the damage
// guarantee, asserted as a count.
func TestColorPickerEditRepaintsOnlyItself(t *testing.T) {
	v := prop.NewSource(render.RGB(10, 10, 10))
	p := &ColorPicker{Value: v}
	root := &VStack{Children: []gooey.Component{
		&Text{Content: Str("above")},
		p,
		&Text{Content: Str("below")},
	}}
	comp := gooey.NewComposer(root, 30, 8)
	comp.SetCaps(term.Caps{Cols: 30, Rows: 8, Color: render.TrueColor})
	if _, painted := comp.Frame(); painted != 4 {
		t.Fatalf("first frame painted %d components, want 4 (stack + 3 children)", painted)
	}

	p.HandleKey(input.Named(input.KeyRight))
	if _, painted := comp.Frame(); painted != 1 {
		t.Errorf("a color edit painted %d components, want exactly 1", painted)
	}

	// Moving the selected channel is also just property damage.
	p.HandleKey(input.Named(input.KeyDown))
	if _, painted := comp.Frame(); painted != 1 {
		t.Errorf("a channel change painted %d components, want exactly 1", painted)
	}
}
