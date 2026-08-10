package markup

import (
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

func buildOne(t *testing.T, src string, ctx *Context) gooey.Widget {
	t.Helper()
	if ctx.Styles == nil {
		ctx.Styles = map[string]render.Style{}
	}
	w, err := Build([]byte(src), ctx)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	return w
}

func TestCanvasAttachedPropertiesParse(t *testing.T) {
	src := `<Gooey xmlns="wonderforge.io/gooey/2026">
	  <Canvas>
	    <Text Canvas.Left="5" Canvas.Top="2" Name="a">hi</Text>
	    <Text Name="b">origin</Text>
	  </Canvas>
	</Gooey>`
	ctx := &Context{Values: map[string]any{}}
	w := buildOne(t, src, ctx)

	c, ok := w.(*gooey.Canvas)
	if !ok {
		t.Fatalf("root is %T, want *gooey.Canvas", w)
	}
	if len(c.Children) != 2 {
		t.Fatalf("canvas has %d children, want 2", len(c.Children))
	}
	a, err := Find[*gooey.Text](ctx, "a")
	if err != nil {
		t.Fatal(err)
	}
	if l := a.LayoutProps(); l.Left != 5 || l.Top != 2 {
		t.Errorf("Canvas.Left/Top parsed as %d,%d; want 5,2", l.Left, l.Top)
	}
	b, _ := Find[*gooey.Text](ctx, "b")
	if l := b.LayoutProps(); l.Left != 0 || l.Top != 0 {
		t.Errorf("child without attached properties got %d,%d; want 0,0", l.Left, l.Top)
	}

	if got := renderToString(t, w, 12, 4); !strings.Contains(got, "     hi") {
		t.Errorf("canvas did not place the child at its offset:\n%s", got)
	}
}

func TestCanvasAttachedPropertyRejectsNonNumbers(t *testing.T) {
	src := `<Gooey xmlns="wonderforge.io/gooey/2026">
	  <Canvas><Text Canvas.Left="middle">x</Text></Canvas>
	</Gooey>`
	_, err := Build([]byte(src), &Context{Values: map[string]any{}})
	if err == nil {
		t.Fatal("expected a load-time error for a non-numeric Canvas.Left")
	}
	if !strings.Contains(err.Error(), "Canvas.Left") {
		t.Errorf("error does not name the attribute: %v", err)
	}
}

func TestCheckboxBindsCheckedTwoWay(t *testing.T) {
	auto := prop.NewSource(false)
	src := `<Gooey xmlns="wonderforge.io/gooey/2026">
	  <Checkbox Checked="{{.Auto}}" Label="auto" Name="cb"/>
	</Gooey>`
	ctx := &Context{Values: map[string]any{"Auto": auto}}
	buildOne(t, src, ctx)

	cb, err := Find[*gooey.Checkbox](ctx, "cb")
	if err != nil {
		t.Fatal(err)
	}
	if cb.IsChecked() {
		t.Error("checkbox started checked")
	}
	// Widget → viewmodel.
	cb.Toggle()
	if !auto.Get() {
		t.Error("toggling the checkbox did not reach the bound property")
	}
	// Viewmodel → widget: the same handle, not a copy.
	auto.Set(false)
	if cb.IsChecked() {
		t.Error("checkbox did not follow the property back")
	}
}

func TestCheckboxLabelAcceptsABinding(t *testing.T) {
	label := prop.NewSource("first")
	src := `<Gooey xmlns="wonderforge.io/gooey/2026">
	  <Checkbox Checked="{{.On}}" Label="{{.Label}}"/>
	</Gooey>`
	ctx := &Context{Values: map[string]any{"On": prop.NewSource(true), "Label": label}}
	w := buildOne(t, src, ctx)

	if got := renderToString(t, w, 20, 1); !strings.Contains(got, "[x] first") {
		t.Errorf("rendered %q, want the bound label", got)
	}
	label.Set("second")
	if got := renderToString(t, w, 20, 1); !strings.Contains(got, "[x] second") {
		t.Errorf("rendered %q after the label changed", got)
	}
}

func TestGaugeAndSparklineBind(t *testing.T) {
	cpu := prop.NewSource(75)
	hist := prop.NewSource([]float64{10, 50, 90})
	src := `<Gooey xmlns="wonderforge.io/gooey/2026">
	  <VStack>
	    <Gauge Value="{{.CPU}}" Label="cpu " Name="g"/>
	    <Sparkline Values="{{.Hist}}" Height="2" Name="s"/>
	  </VStack>
	</Gooey>`
	ctx := &Context{Values: map[string]any{"CPU": cpu, "Hist": hist}}
	w := buildOne(t, src, ctx)

	g, err := Find[*gooey.Gauge](ctx, "g")
	if err != nil {
		t.Fatal(err)
	}
	s, err := Find[*gooey.Sparkline](ctx, "s")
	if err != nil {
		t.Fatal(err)
	}
	if s.Rows != 2 {
		t.Errorf("Height=2 parsed as Rows=%d", s.Rows)
	}
	out := renderToString(t, w, 40, 3)
	if !strings.Contains(out, "cpu") || !strings.Contains(out, "75%") {
		t.Errorf("gauge did not render its label and readout:\n%s", out)
	}
	if !strings.Contains(out, "█") {
		t.Errorf("sparkline drew no blocks:\n%s", out)
	}
	_ = g
}

func TestColorPickerBindsATypedColorProperty(t *testing.T) {
	accent := prop.NewSource(render.RGB(255, 170, 60))
	src := `<Gooey xmlns="wonderforge.io/gooey/2026">
	  <ColorPicker Value="{{.Accent}}" Name="p"/>
	</Gooey>`
	ctx := &Context{Values: map[string]any{"Accent": accent}}
	buildOne(t, src, ctx)

	p, err := Find[*gooey.ColorPicker](ctx, "p")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := p.Hex(), "#FFAA3C"; got != want {
		t.Errorf("picker shows %q, want %q", got, want)
	}
	accent.Set(render.RGB(0, 0, 0))
	if got, want := p.Hex(), "#000000"; got != want {
		t.Errorf("picker did not follow the bound property: %q, want %q", got, want)
	}
}

// A wrongly-typed viewmodel property is a LOAD-time error naming both
// types, not a render-time surprise — the no-reflection contract means
// the type assertion at build is the whole check.
func TestTypedBindingsFailAtLoadWithBothTypes(t *testing.T) {
	cases := []struct {
		name, src, want string
		values          map[string]any
	}{
		{
			name:   "checkbox",
			src:    `<Checkbox Checked="{{.Nope}}"/>`,
			want:   "*prop.Property[bool]",
			values: map[string]any{"Nope": prop.NewSource("a string")},
		},
		{
			name:   "gauge",
			src:    `<Gauge Value="{{.Nope}}"/>`,
			want:   "*prop.Property[int]",
			values: map[string]any{"Nope": prop.NewSource(1.5)},
		},
		{
			name:   "colorpicker",
			src:    `<ColorPicker Value="{{.Nope}}"/>`,
			want:   "*prop.Property[github.com/WonderForgeLabs/gooey/render.Color]",
			values: map[string]any{"Nope": prop.NewSource("#fff")},
		},
		{
			name:   "sparkline",
			src:    `<Sparkline Values="{{.Nope}}"/>`,
			want:   "*prop.Property[[]float64]",
			values: map[string]any{"Nope": prop.NewSource([]int{1})},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			full := `<Gooey xmlns="wonderforge.io/gooey/2026">` + tc.src + `</Gooey>`
			_, err := Build([]byte(full), &Context{Values: tc.values})
			if err == nil {
				t.Fatal("expected a load-time type error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not name the wanted type %q", err, tc.want)
			}
		})
	}
}

// A bound Style is a live handle, so a computed style over an accent
// color restyles its widgets through the ordinary property graph — the
// closest thing gooey has to theming, with no styling system involved.
func TestStyleAttributeAcceptsABinding(t *testing.T) {
	accent := prop.NewSource(render.RGB(255, 170, 60))
	styled := prop.NewComputed(func() render.Style {
		return render.Style{Fg: accent.Get()}
	})
	src := `<Gooey xmlns="wonderforge.io/gooey/2026">
	  <Border Title="t" Style="{{.Accent}}" Name="b">
	    <Text Style="{{.Accent}}" Bold="true" Name="t">x</Text>
	  </Border>
	</Gooey>`
	ctx := &Context{Values: map[string]any{"Accent": styled}}
	buildOne(t, src, ctx)

	b, err := Find[*gooey.Border](ctx, "b")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := b.Style.Get().Fg, render.RGB(255, 170, 60); got != want {
		t.Errorf("border style Fg = %v, want %v", got, want)
	}
	// The handle is live: changing the source changes what the widget
	// reads, with no rebuild.
	accent.Set(render.RGB(0, 128, 255))
	if got, want := b.Style.Get().Fg, render.RGB(0, 128, 255); got != want {
		t.Errorf("bound style did not follow the source: %v, want %v", got, want)
	}
	// Bold="true" composes over a bound style instead of replacing it.
	txt, _ := Find[*gooey.Text](ctx, "t")
	st := txt.Style.Get()
	if !st.Bold {
		t.Error("Bold=true was lost on a bound style")
	}
	if got, want := st.Fg, render.RGB(0, 128, 255); got != want {
		t.Errorf("bound style Fg under Bold = %v, want %v", got, want)
	}
}

func TestStyleAttributeStillDoesNamedLookup(t *testing.T) {
	src := `<Gooey xmlns="wonderforge.io/gooey/2026">
	  <Text Style="accent" Name="t">x</Text>
	</Gooey>`
	ctx := &Context{
		Values: map[string]any{},
		Styles: map[string]render.Style{"accent": {Fg: render.RGB(1, 2, 3)}},
	}
	buildOne(t, src, ctx)
	txt, _ := Find[*gooey.Text](ctx, "t")
	if got, want := txt.Style.Get().Fg, render.RGB(1, 2, 3); got != want {
		t.Errorf("named style lookup = %v, want %v", got, want)
	}
}

// Canvas.* joins Grid.* as an attached property that applies to the
// INSTANCE and is not passed through into an Include's context.
func TestCanvasAttachedPropertiesAreNotPassedIntoIncludes(t *testing.T) {
	for _, k := range []string{"Canvas.Left", "Canvas.Top", "Grid.Row", "Width"} {
		if !layoutAttr(k) {
			t.Errorf("layoutAttr(%q) = false; it must not cross the control boundary", k)
		}
	}
	for _, k := range []string{"Title", "Value", "Canvas"} {
		if layoutAttr(k) {
			t.Errorf("layoutAttr(%q) = true; ordinary attributes must pass through", k)
		}
	}
}
