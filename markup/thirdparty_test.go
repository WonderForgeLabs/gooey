package markup_test

// A component registered through Context.Components is the third-party
// case: it lives outside package markup, so it reaches the binding
// dialect only through the EXPORTED surface. These tests are written
// from outside the package deliberately — an in-package test would
// resolve the unexported helpers and prove nothing about what a
// nested-module component can actually do (#266).

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// probe is a third-party component: it keeps the handles its builder
// resolved so a test can assert they are the viewmodel's own rather than
// copies of its values.
type probe struct {
	gooey.Base
	text *prop.Property[string]
}

func (p *probe) Measure(avail gooey.Size) gooey.Size { return gooey.Size{W: avail.W, H: 1} }

func (p *probe) Render(f *gooey.Frame) {
	if p.text == nil {
		return
	}
	b := p.Bounds()
	f.Cells.SetString(b.X, b.Y, p.text.Get(), render.Style{})
}

func page(src string) fstest.MapFS {
	return fstest.MapFS{"page.gooey": {Data: []byte(src)}}
}

// TestThirdPartyBindsTypedHandle is the core of #266: a component
// outside package markup must be able to take the viewmodel's own
// *prop.Property[T] handle, not a value snapshot. Setting the source
// after the build has to be visible through the component's field, which
// is only true if the handle was SHARED.
func TestThirdPartyBindsTypedHandle(t *testing.T) {
	on := prop.NewSource(false)
	count := prop.NewSource(3)
	var gotOn *prop.Property[bool]
	var gotCount *prop.Property[int]
	ctx := &markup.Context{
		Values: map[string]any{"On": on, "Count": count},
		Components: map[string]markup.Builder{
			"Probe": func(e markup.Element, ctx *markup.Context) (gooey.Component, error) {
				var err error
				if gotOn, err = markup.Bound[bool](e, ctx, "On"); err != nil {
					return nil, err
				}
				if gotCount, err = markup.Bound[int](e, ctx, "Count"); err != nil {
					return nil, err
				}
				return &probe{}, nil
			},
		},
	}
	if _, err := markup.Load(page(`<Gooey><Probe On="{{.On}}" Count="{{.Count}}"/></Gooey>`), "page.gooey", ctx); err != nil {
		t.Fatalf("load: %v", err)
	}
	on.Set(true)
	count.Set(9)
	if !gotOn.Get() || gotCount.Get() != 9 {
		t.Fatalf("On=%v Count=%d; the builder took values, not the viewmodel's handles", gotOn.Get(), gotCount.Get())
	}
}

// TestThirdPartyBoundTypeMismatchIsALoadError keeps the type check where
// every other resolvable mistake lives. A viewmodel property of the
// wrong type must name both types at load, not panic on first paint.
func TestThirdPartyBoundTypeMismatchIsALoadError(t *testing.T) {
	ctx := &markup.Context{
		Values: map[string]any{"On": prop.NewSource(0)},
		Components: map[string]markup.Builder{
			"Probe": func(e markup.Element, ctx *markup.Context) (gooey.Component, error) {
				_, err := markup.Bound[bool](e, ctx, "On")
				return &probe{}, err
			},
		},
	}
	_, err := markup.Load(page(`<Gooey><Probe On="{{.On}}"/></Gooey>`), "page.gooey", ctx)
	if err == nil {
		t.Fatal("a bool attribute bound to an int property loaded without complaint")
	}
	if !strings.Contains(err.Error(), "prop.Property[bool]") || !strings.Contains(err.Error(), "prop.Property[int]") {
		t.Fatalf("error names neither side of the mismatch: %v", err)
	}
}

// TestThirdPartyBoundRejectsANonBinding: Bound resolves a HANDLE, so a
// literal is a load error rather than a silent zero value.
func TestThirdPartyBoundRejectsANonBinding(t *testing.T) {
	ctx := &markup.Context{
		Components: map[string]markup.Builder{
			"Probe": func(e markup.Element, ctx *markup.Context) (gooey.Component, error) {
				_, err := markup.Bound[bool](e, ctx, "On")
				return &probe{}, err
			},
		},
	}
	if _, err := markup.Load(page(`<Gooey><Probe On="true"/></Gooey>`), "page.gooey", ctx); err == nil {
		t.Fatal(`On="true" resolved as a handle; a literal in a handle position must fail at load`)
	}
}

// TestThirdPartyBoundTextInterpolates is the case Context.BindingValue
// could not already serve. "Hi {{.Who}}!" is a MIXED attribute; resolving
// it by hand through BindingValue returns the Who handle and silently
// drops the literal parts around it, which is why third parties needed
// the text RULE rather than a lookup.
func TestThirdPartyBoundTextInterpolates(t *testing.T) {
	who := prop.NewSource("world")
	var label *prop.Property[string]
	ctx := &markup.Context{
		Values: map[string]any{"Who": who},
		Components: map[string]markup.Builder{
			"Probe": func(e markup.Element, ctx *markup.Context) (gooey.Component, error) {
				var err error
				label, err = markup.BoundText(e, ctx, "Label")
				return &probe{}, err
			},
		},
	}
	if _, err := markup.Load(page(`<Gooey><Probe Label="Hi {{.Who}}!"/></Gooey>`), "page.gooey", ctx); err != nil {
		t.Fatalf("load: %v", err)
	}
	if s := label.Get(); s != "Hi world!" {
		t.Fatalf("Label=%q, want the literal parts kept around the binding", s)
	}
	who.Set("gooey")
	if s := label.Get(); s != "Hi gooey!" {
		t.Fatalf("Label=%q after Set; the computed did not track its source", s)
	}
}

// TestThirdPartyBoundTextLiteralAndAbsent: the text rule never returns
// nil, so a third-party component never has to test for both a handle
// and a raw string.
func TestThirdPartyBoundTextLiteralAndAbsent(t *testing.T) {
	var lit, missing *prop.Property[string]
	ctx := &markup.Context{
		Components: map[string]markup.Builder{
			"Probe": func(e markup.Element, ctx *markup.Context) (gooey.Component, error) {
				var err error
				if lit, err = markup.BoundText(e, ctx, "Label"); err != nil {
					return nil, err
				}
				if missing, err = markup.BoundText(e, ctx, "Nope"); err != nil {
					return nil, err
				}
				return &probe{}, nil
			},
		},
	}
	if _, err := markup.Load(page(`<Gooey><Probe Label="plain"/></Gooey>`), "page.gooey", ctx); err != nil {
		t.Fatalf("load: %v", err)
	}
	if s := lit.Get(); s != "plain" {
		t.Fatalf("Label=%q, want the literal", s)
	}
	if missing == nil {
		t.Fatal("BoundText returned nil for an absent attribute")
	}
	if s := missing.Get(); s != "" {
		t.Fatalf("absent attribute yielded %q, want the empty literal", s)
	}
}

// TestThirdPartyBoundTextUnresolvedIsALoadError: everything resolvable
// fails at load, for a registered component exactly as for a builtin.
func TestThirdPartyBoundTextUnresolvedIsALoadError(t *testing.T) {
	ctx := &markup.Context{
		Components: map[string]markup.Builder{
			"Probe": func(e markup.Element, ctx *markup.Context) (gooey.Component, error) {
				_, err := markup.BoundText(e, ctx, "Label")
				return &probe{}, err
			},
		},
	}
	if _, err := markup.Load(page(`<Gooey><Probe Label="Hi {{.Nobody}}"/></Gooey>`), "page.gooey", ctx); err == nil {
		t.Fatal("a binding to a path the context does not hold loaded without complaint")
	}
}

// TestThirdPartyBoundColor: the #rrggbb literal is part of the markup
// dialect and markup's own parser is the one that agrees with it.
// Without an exported resolver a third party either duplicates the
// parser or refuses the literal every other element accepts.
func TestThirdPartyBoundColor(t *testing.T) {
	var lit, bound, absent *prop.Property[render.Color]
	src := prop.NewSource(render.RGB(1, 2, 3))
	ctx := &markup.Context{
		Values: map[string]any{"Accent": src},
		Components: map[string]markup.Builder{
			"Probe": func(e markup.Element, ctx *markup.Context) (gooey.Component, error) {
				var err error
				if lit, err = markup.BoundColor(e, ctx, "Fill"); err != nil {
					return nil, err
				}
				if bound, err = markup.BoundColor(e, ctx, "Edge"); err != nil {
					return nil, err
				}
				if absent, err = markup.BoundColor(e, ctx, "Nope"); err != nil {
					return nil, err
				}
				return &probe{}, nil
			},
		},
	}
	if _, err := markup.Load(page(`<Gooey><Probe Fill="#ff8800" Edge="{{.Accent}}"/></Gooey>`), "page.gooey", ctx); err != nil {
		t.Fatalf("load: %v", err)
	}
	if c := lit.Get(); c != render.RGB(0xff, 0x88, 0x00) {
		t.Fatalf("Fill=%v, want #ff8800 parsed the way every builtin parses it", c)
	}
	if c := bound.Get(); c != render.RGB(1, 2, 3) {
		t.Fatalf("Edge=%v, want the viewmodel's own handle", c)
	}
	if absent != nil {
		t.Fatalf("an absent color attribute yielded %v, want nil so the caller keeps its own default", absent.Get())
	}
}

// TestThirdPartyBoundColorBadLiteralIsALoadError keeps a mistyped colour
// on the load-time side of the line.
func TestThirdPartyBoundColorBadLiteralIsALoadError(t *testing.T) {
	ctx := &markup.Context{
		Components: map[string]markup.Builder{
			"Probe": func(e markup.Element, ctx *markup.Context) (gooey.Component, error) {
				_, err := markup.BoundColor(e, ctx, "Fill")
				return &probe{}, err
			},
		},
	}
	if _, err := markup.Load(page(`<Gooey><Probe Fill="mauve"/></Gooey>`), "page.gooey", ctx); err == nil {
		t.Fatal(`Fill="mauve" loaded; a colour that cannot parse must fail at load`)
	}
}

// TestThirdPartyBoundStyle: Style= has two forms that are both part of
// the dialect — a name in Context.Styles, or a bound handle. A
// third-party component that wants to look like the rest of the page
// needs the same rule, including the reactive half.
func TestThirdPartyBoundStyle(t *testing.T) {
	want := render.Style{Fg: render.RGB(9, 0, 0)}
	var named, bound *prop.Property[render.Style]
	live := prop.NewSource(render.Style{Fg: render.RGB(0, 0, 7)})
	mk := func(dst **prop.Property[render.Style]) markup.Builder {
		return func(e markup.Element, ctx *markup.Context) (gooey.Component, error) {
			s, err := markup.BoundStyle(e, ctx)
			*dst = s
			return &probe{}, err
		}
	}
	ctx := &markup.Context{
		Values:     map[string]any{"Live": live},
		Styles:     map[string]render.Style{"accent": want},
		Components: map[string]markup.Builder{"A": mk(&named), "B": mk(&bound)},
	}
	if _, err := markup.Load(page(`<Gooey><VStack><A Style="accent"/><B Style="{{.Live}}"/></VStack></Gooey>`), "page.gooey", ctx); err != nil {
		t.Fatalf("load: %v", err)
	}
	if named.Get() != want {
		t.Fatalf(`Style="accent" resolved to %v, want the registered style`, named.Get())
	}
	live.Set(render.Style{Fg: render.RGB(0, 0, 8)})
	if got := bound.Get().Fg.B; got != 8 {
		t.Fatalf("bound style did not track its source: Fg.B=%d", got)
	}
}

// TestThirdPartyBoundHandleDrivesDamage pins the consequence that makes
// lvalue semantics worth having: a third-party component that resolved
// its attribute through the exported surface repaints when the source
// changes, and repaints ALONE. A bounds or cell assertion would pass
// just as well if the whole tree had repainted, so the count is the pin.
func TestThirdPartyBoundHandleDrivesDamage(t *testing.T) {
	label := prop.NewSource("a")
	ctx := &markup.Context{
		Values: map[string]any{"L": label},
		Components: map[string]markup.Builder{
			"Probe": func(e markup.Element, ctx *markup.Context) (gooey.Component, error) {
				h, err := markup.BoundText(e, ctx, "Label")
				if err != nil {
					return nil, err
				}
				return &probe{text: h}, nil
			},
		},
	}
	w, err := markup.Load(page(`<Gooey><VStack><Probe Label="{{.L}}"/><Text>static</Text></VStack></Gooey>`), "page.gooey", ctx)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	comp := gooey.NewComposer(w, 20, 4)
	if _, first := comp.Frame(); first < 2 {
		t.Fatalf("first frame painted %d; the count cannot discriminate", first)
	}
	label.Set("b")
	if _, painted := comp.Frame(); painted != 1 {
		t.Fatalf("painted=%d, want exactly the third-party component to repaint", painted)
	}
}

// TestThirdPartyBindsARowValueInsideAnItemTemplate is the composition
// worth pinning, because the two halves were built separately and meet
// only here.
//
// components.rowValue (components/itemsview.go) is the PRODUCER of the
// typed-handle contract: it turns each projected row value into a
// *prop.Property[T] and puts it in the row's own Values map. Bound[T] is
// the CONSUMER of that same contract. A row template inherits the page's
// Components, so a registered component can be placed in a row like any
// other element — but until #266 it could not resolve what rowValue had
// just produced for it, and a third-party cell was stuck on literals
// inside a list whose values were already perfectly good handles.
//
// The set of types rowValue produces and the set Bound[T] is instantiated
// at have to agree, and nothing mechanically forces that; this is one arm
// of the agreement, held from outside the package.
func TestThirdPartyBindsARowValueInsideAnItemTemplate(t *testing.T) {
	type row struct {
		Name string
		N    int
	}
	rows := prop.NewSource([]row{{"a", 1}, {"b", 2}})
	items := components.Items(rows, func(x row) map[string]any {
		return map[string]any{"Name": x.Name, "N": x.N}
	})
	var cells []*probe
	ctx := &markup.Context{
		Values: map[string]any{"Rows": items, "Sel": prop.NewSource(0)},
		Components: map[string]markup.Builder{
			"Cell": func(e markup.Element, ctx *markup.Context) (gooey.Component, error) {
				name, err := markup.BoundText(e, ctx, "Text")
				if err != nil {
					return nil, err
				}
				if _, err := markup.Bound[int](e, ctx, "Count"); err != nil {
					return nil, err
				}
				p := &probe{text: name}
				cells = append(cells, p)
				return p, nil
			},
		},
	}
	src := `<Gooey>
	  <ItemsView Items="{{.Rows}}" Selected="{{.Sel}}">
	    <ItemsView.ItemTemplate>
	      <Cell Text="{{.Name}} row" Count="{{.N}}"/>
	    </ItemsView.ItemTemplate>
	  </ItemsView>
	</Gooey>`
	w, err := markup.Load(page(src), "page.gooey", ctx)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	// Counted, not indexed, and that is not defensive: ItemsView.Validate
	// builds row 0 once through the template and THROWS THE COMPONENT
	// AWAY (components/itemsview.go:211). So a builder placed in a
	// template is invoked once more than there are rows, and the extra
	// instance is never arranged, never painted, and never updated — it
	// keeps its first projection forever. Indexing cells[0] reads that
	// orphan and reports a re-projection failure that did not happen.
	comp := gooey.NewComposer(w, 30, 4)
	comp.Frame()
	count := func(want string) int {
		n := 0
		for _, c := range cells {
			if c.text.Get() == want {
				n++
			}
		}
		return n
	}
	if len(cells) == 0 {
		t.Fatal("no rows were realized; the test cannot discriminate")
	}
	if count("a row") == 0 || count("b row") != 1 {
		t.Fatalf("cells=%d: rows did not interpolate their values into the literal", len(cells))
	}
	rows.Set([]row{{"z", 1}, {"b", 2}})
	comp.Frame()
	if n := count("z row"); n != 1 {
		t.Fatalf("%d live cells read %q after re-projection, want 1; the third-party cell took a value, not the row's handle", n, "z row")
	}
}
