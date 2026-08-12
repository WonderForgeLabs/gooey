package markup

import (
	"fmt"
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
	"github.com/WonderForgeLabs/gooey/term"
)

// AttrSpec.Default claims something checkable — "writing this is the same
// as writing nothing" — so it is checked, by rendering.
//
// The check is two tests and it needs both. The first requires omission
// and the declared default to produce identical cells. On its own that
// test passes trivially for any attribute with no visible effect, which
// is the over-declaration direction: silent, and the exact failure mode
// the whole catalog exists to delete. The second test closes it by
// requiring some OTHER legal value to produce different cells — proving
// the first test could have failed. An attribute that cannot be
// discriminated must not declare a Default at all.
//
// This is stronger than the guard the attribute vocabulary settled for.
// Setting a declared attribute to an absurd value and requiring an error
// reaches 59% of rows, because KindString, KindStyle, KindText and
// KindIdentity accept anything. Rendering tests EFFECT rather than
// acceptance, so it is not capped that way — at the cost of only
// covering effects visible in a static frame, which is why durations,
// commands and runtime bindings deliberately declare no Default.

const (
	defaultsCols = 40
	defaultsRows = 12
)

// defaultsContext is the binding environment every probe builds against.
// The handles are shared between the two builds of a comparison on
// purpose: the point is to vary one attribute, so every other input has
// to be identical, and identical handles are the strongest form of that.
func defaultsContext() *Context {
	return &Context{
		Values: map[string]any{
			"S":   prop.NewSource("sample"),
			"I":   prop.NewSource(1),
			"B":   prop.NewSource(true),
			"F64": prop.NewSource([]float64{1, 4, 2, 5, 3}),
			"SS":  prop.NewSource([]string{"one", "two"}),
			"C":   prop.NewSource(render.RGB(120, 200, 140)),
			"IS": prop.NewSource(components.ItemsOf([]string{"alpha", "beta"},
				func(s string) map[string]any { return map[string]any{"Label": s} })),
			// A percentage, distinct from the index handle above. Seeding
			// a Gauge or a ProgressBar with 1 puts it at the bottom of the
			// good/warn/crit ramp, where Thresholds="true" and the plain
			// style paint the same colour — a probe that cannot see the
			// attribute it is probing.
			"Pct":  prop.NewSource(85),
			"Noop": gooey.Command(func() {}),
		},
		Styles: map[string]render.Style{"probe": {Fg: render.RGB(200, 40, 40)}},
	}
}

// bindingFor is the placeholder binding for a required attribute of a
// declared Go type. An unknown type is a hard failure rather than a skip:
// a new required attribute that this cannot seed would otherwise silently
// drop its element out of the drift check.
func bindingFor(t *testing.T, a AttrSpec) string {
	t.Helper()
	if a.Name == "Value" && a.GoType == "int" {
		// Value is a percentage on every element that declares it;
		// Selected, the other required int, is an index.
		return "{{.Pct}}"
	}
	switch a.GoType {
	case "string":
		return "{{.S}}"
	case "int":
		return "{{.I}}"
	case "bool":
		return "{{.B}}"
	case "[]float64":
		return "{{.F64}}"
	case "[]string":
		return "{{.SS}}"
	case "render.Color":
		return "{{.C}}"
	case "components.ItemSource":
		return "{{.IS}}"
	}
	t.Fatalf("no placeholder for required attribute %s of type %q — "+
		"add one, or the element silently stops being checked", a.Name, a.GoType)
	return ""
}

// literalFor is the placeholder literal for a required non-binding
// attribute.
func literalFor(a AttrSpec) string {
	switch a.Kind {
	case KindDuration:
		return "50ms"
	case KindInt:
		return "1"
	case KindBool:
		return "true"
	case KindGesture:
		return "ctrl+p"
	case KindEnum:
		if len(a.Enum) > 0 {
			return a.Enum[0]
		}
	}
	return "x"
}

// probeElement writes the element under test with every required
// attribute, slot and child seeded, plus the one attribute the probe is
// varying. An empty value omits the attribute, which is the whole point
// of the omission side of the comparison.
func probeElement(t *testing.T, def *ElementDef, attr, value string) string {
	t.Helper()
	var b strings.Builder
	fmt.Fprintf(&b, "<%s", def.Name)
	for _, a := range def.Attrs {
		if !a.Required || a.Name == attr {
			continue
		}
		if a.Binds == BindsLiteral {
			fmt.Fprintf(&b, " %s=%q", a.Name, literalFor(a))
			continue
		}
		fmt.Fprintf(&b, " %s=%q", a.Name, bindingFor(t, a))
	}
	if value != "" {
		fmt.Fprintf(&b, " %s=%q", attr, value)
	}
	b.WriteString(">")
	for _, s := range def.Slots {
		if !s.Required {
			continue
		}
		fmt.Fprintf(&b, "<%s.%s><Text>{{.Label}}</Text></%s.%s>",
			def.Name, s.Name, def.Name, s.Name)
	}
	switch def.Children.Mode {
	case ModeLeaf:
		// A leaf's body is its content, and an empty <Text/> paints
		// nothing at any alignment, size or visibility. An empty probe
		// made seven universal attributes look like they had no effect.
		b.WriteString("ab")
	case ModeOne:
		b.WriteString("<Text>one</Text>")
	case ModeMany:
		// TWO children of DIFFERENT widths. One child makes Gap
		// unobservable; two equal-width children make Uniform
		// unobservable.
		b.WriteString("<Text>one</Text><Text>seven</Text>")
	case ModeRestricted:
		for _, only := range def.Children.Only {
			fmt.Fprintf(&b, "<%s Header=\"h\"><Text>one</Text></%s>", only, only)
		}
	}
	fmt.Fprintf(&b, "</%s>", def.Name)
	return b.String()
}

// renderProbe builds src and composes it into a fixed rect, returning the
// cell plane. Compose is the one-shot path, so nothing is started and
// nothing ticks: two calls with the same input give the same cells.
func renderProbe(t *testing.T, ctx *Context, src string) *render.Buffer {
	t.Helper()
	w, err := Build([]byte("<Gooey>"+src+"</Gooey>"), ctx)
	if err != nil {
		t.Fatalf("build %s: %v", src, err)
	}
	f := gooey.Compose(w, term.Caps{Cols: defaultsCols, Rows: defaultsRows}, nil)
	return f.Cells
}

// cellsDiffer reports the first differing cell, for a message that names
// a coordinate instead of dumping two screens.
func cellsDiffer(a, b *render.Buffer) (int, int, bool) {
	for y := 0; y < defaultsRows; y++ {
		for x := 0; x < defaultsCols; x++ {
			if a.At(x, y) != b.At(x, y) {
				return x, y, true
			}
		}
	}
	return 0, 0, false
}

// harnessFor wraps the element under test in a parent that gives the
// attribute meaning. Attached properties are the reason this is not one
// string: Grid.Row means nothing outside a <Grid>, and an explicit Width
// means nothing to an element that is the only thing in its slot.
func harnessFor(attr, el string) string {
	switch {
	case strings.HasPrefix(attr, "Grid."):
		return `<Grid Rows="1*,1*" Cols="1*,1*">` + el + `<Text Grid.Row="1" Grid.Col="1">z</Text></Grid>`
	case strings.HasPrefix(attr, "Canvas."):
		return `<Canvas>` + el + `<Text Canvas.Left="8" Canvas.Top="3">z</Text></Canvas>`
	case attr == "HAlign" || attr == "VAlign":
		// Alignment needs a slot BIGGER than the content. Inside a stack
		// a child gets its measured size and there is nothing to align
		// within, so both values render the same and the probe proves
		// nothing.
		return `<Grid Rows="1*,1*" Cols="1*,1*">` + el + `</Grid>`
	case attr == "Height":
		// A vertical sibling is what an explicit height displaces.
		return `<Grid Rows="1*,1*" Cols="1*,1*"><VStack Grid.Row="0" Grid.Col="0">` +
			el + `<Text>z</Text></VStack></Grid>`
	}
	// A horizontal sibling after the element under test is what makes
	// Width, Margin and every element-own size attribute observable:
	// they move where the next thing lands.
	return `<Grid Rows="1*,1*" Cols="1*,1*"><HStack Grid.Row="0" Grid.Col="0">` +
		el + `<Text>z</Text></HStack></Grid>`
}

// defaultProbes is every declared Default in the vocabulary, paired with
// the harness it has to be judged in.
type defaultProbe struct {
	owner string // element name, or "" for the universal/attached tables
	attr  AttrSpec
	def   *ElementDef
}

func declaredDefaults(t *testing.T) []defaultProbe {
	t.Helper()
	var out []defaultProbe
	for _, def := range definedElements() {
		spec := def.spec()
		for _, a := range def.Attrs {
			if a.Default == "" {
				continue
			}
			if !TakesLayout(spec) {
				// A non-visual element paints nothing, so neither test
				// below can say anything about it. Declaring a Default
				// there would be unguarded by construction.
				t.Errorf("<%s %s> declares Default=%q but the element is "+
					"non-visual: nothing can check it", def.Name, a.Name, a.Default)
				continue
			}
			out = append(out, defaultProbe{owner: def.Name, attr: a, def: def})
		}
	}
	// The universal and attached tables belong to no element, so they are
	// probed once through a representative one — and it has to be a
	// <Border>, not a <Text>.
	//
	// A Text paints at the left edge of whatever bounds it is given, so
	// a bounds change that does not move that edge is invisible to it:
	// Grid.ColSpan="2" widens the slot and the text stays put, and the
	// probe reports "no effect" for an attribute that plainly has one. A
	// Border draws its own edges AT its bounds, so every change to the
	// rect it was arranged into shows up.
	box := elementDefs["Border"]
	for _, a := range universalAttrs {
		if a.Default != "" {
			out = append(out, defaultProbe{attr: a, def: box})
		}
	}
	for _, parent := range AttachedParents() {
		for _, a := range AttachedAttrs(parent) {
			if a.Default != "" {
				out = append(out, defaultProbe{attr: a, def: box})
			}
		}
	}
	return out
}

func (p defaultProbe) name() string {
	if p.owner == "" {
		return p.attr.Name
	}
	return p.owner + "." + p.attr.Name
}

func TestDeclaredDefaultsRenderIdenticallyToOmission(t *testing.T) {
	probes := declaredDefaults(t)
	if len(probes) == 0 {
		t.Fatal("no declared defaults: this test would pass vacuously")
	}
	for _, p := range probes {
		t.Run(p.name(), func(t *testing.T) {
			ctx := defaultsContext()
			absent := renderProbe(t, ctx, harnessFor(p.attr.Name,
				probeElement(t, p.def, p.attr.Name, "")))
			explicit := renderProbe(t, ctx, harnessFor(p.attr.Name,
				probeElement(t, p.def, p.attr.Name, p.attr.Default)))
			if x, y, differs := cellsDiffer(absent, explicit); differs {
				t.Fatalf("%s=%q is declared the default but does not render like "+
					"omission: first difference at (%d,%d)",
					p.attr.Name, p.attr.Default, x, y)
			}
		})
	}
}

// otherValue is a legal value for a that is NOT its declared default. It
// is what makes the identity test able to fail.
func otherValue(a AttrSpec) string {
	switch a.Kind {
	case KindEnum:
		for _, v := range a.Enum {
			if v != a.Default {
				return v
			}
		}
	case KindBool:
		if a.Default == "false" {
			return "true"
		}
		return "false"
	case KindInt:
		// An attached property has to stay inside the harness it is
		// judged in: Grid.Row="7" in a two-row grid is outside the grid,
		// which is a different statement from "row 1 differs from row 0".
		switch a.Name {
		case "Grid.Row", "Grid.Col":
			return "1"
		case "Grid.RowSpan", "Grid.ColSpan":
			return "2"
		case "Canvas.Left", "Canvas.Top":
			return "3"
		}
		if a.Default != "7" {
			return "7"
		}
		return "3"
	case KindString:
		// Margin is the only KindString carrying a Default; a thickness
		// of 2 moves everything it touches.
		if a.Default != "2" {
			return "2"
		}
		return "3"
	}
	return ""
}

func TestDeclaredDefaultsAreDiscriminating(t *testing.T) {
	for _, p := range declaredDefaults(t) {
		t.Run(p.name(), func(t *testing.T) {
			other := otherValue(p.attr)
			if other == "" {
				t.Fatalf("%s declares Default=%q but no other legal value can be "+
					"generated for a %s — nothing can prove the identity test could "+
					"fail, so the declaration is unguarded and must be dropped",
					p.attr.Name, p.attr.Default, p.attr.Kind)
			}
			ctx := defaultsContext()
			def := renderProbe(t, ctx, harnessFor(p.attr.Name,
				probeElement(t, p.def, p.attr.Name, p.attr.Default)))
			alt := renderProbe(t, ctx, harnessFor(p.attr.Name,
				probeElement(t, p.def, p.attr.Name, other)))
			if _, _, differs := cellsDiffer(def, alt); !differs {
				t.Fatalf("%s renders identically at %q and %q, so "+
					"TestDeclaredDefaultsRenderIdenticallyToOmission cannot fail for "+
					"this row: either the probe does not exercise the attribute, or "+
					"it has no static effect and must not declare a Default",
					p.attr.Name, p.attr.Default, other)
			}
		})
	}
}

// TestEveryDeclaredDefaultIsCategorised is the cheap half: a grid groups
// by CategoryOf, and a row that derives to Common when it is really
// Layout is a presentation bug nobody would notice.
func TestCategoryOfDerivesAndOverrides(t *testing.T) {
	for _, a := range universalAttrs {
		want := CategoryLayout
		switch a.Name {
		case "Name":
			want = CategoryDesign
		case "Tooltip":
			want = CategoryCommon
		}
		if got := CategoryOf(a); got != want {
			t.Errorf("CategoryOf(%s) = %q, want %q", a.Name, got, want)
		}
	}
	for _, parent := range AttachedParents() {
		for _, a := range AttachedAttrs(parent) {
			if got := CategoryOf(a); got != CategoryLayout {
				t.Errorf("CategoryOf(%s) = %q, want %q", a.Name, got, CategoryLayout)
			}
		}
	}
	derived := map[Kind]string{
		KindCommand:  CategoryEvents,
		KindStyle:    CategoryAppearance,
		KindColor:    CategoryAppearance,
		KindIdentity: CategoryDesign,
		KindText:     CategoryCommon,
		KindInt:      CategoryCommon,
	}
	for kind, want := range derived {
		if got := CategoryOf(AttrSpec{Kind: kind}); got != want {
			t.Errorf("CategoryOf(Kind %s) = %q, want %q", kind, got, want)
		}
	}
	// The declared field overrides the derivation, which is the only
	// reason it exists.
	if got := CategoryOf(AttrSpec{Kind: KindCommand, Category: CategoryLayout}); got != CategoryLayout {
		t.Errorf("declared Category did not override: got %q", got)
	}
}
