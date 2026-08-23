package markup

import (
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/prop"
)

// Unknown-attribute rejection. Before this, every one of these cases
// loaded cleanly and did nothing.

func TestUnknownAttributeIsALoadError(t *testing.T) {
	cases := []struct {
		name, src, want string
	}{
		{
			// The motivating case. Canvas.Left is the only spelling that
			// works; the bare form used to be dropped in silence, leaving
			// the element at the origin with nothing to explain it.
			name: "bare Left inside a Canvas",
			src:  `<Gooey><Canvas><Text Left="10">x</Text></Canvas></Gooey>`,
			want: "did you mean Canvas.Left?",
		},
		{
			name: "bare Top inside a Canvas",
			src:  `<Gooey><Canvas><Text Top="3">x</Text></Canvas></Gooey>`,
			want: "did you mean Canvas.Top?",
		},
		{
			// Spelled correctly, in the wrong place. That is a different
			// mistake from a typo and gets a different sentence.
			name: "Canvas.Left under a VStack",
			src:  `<Gooey><VStack><Text Canvas.Left="10">x</Text></VStack></Gooey>`,
			want: "contributed by a <Canvas> parent",
		},
		{
			name: "Grid.Row under a Canvas",
			src:  `<Gooey><Canvas><Text Grid.Row="1">x</Text></Canvas></Gooey>`,
			want: "contributed by a <Grid> parent",
		},
		{
			name: "misspelled element attribute",
			src:  `<Gooey><Button Conten="hi"/></Gooey>`,
			want: "did you mean Content?",
		},
		{
			name: "attribute that belongs to another element",
			src:  `<Gooey><Text Chrome="cell">x</Text></Gooey>`,
			want: "no such attribute",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Build([]byte(c.src), &Context{})
			if err == nil {
				t.Fatalf("loaded cleanly; the attribute would have been silently ignored")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error %q does not contain %q", err, c.want)
			}
		})
	}
}

// TestValidAttributesStillLoad guards the other direction: rejection is
// a behavior change and the risk is rejecting markup that was always
// correct.
func TestValidAttributesStillLoad(t *testing.T) {
	ctx := &Context{Values: map[string]any{
		"N": prop.NewSource(1),
		"S": prop.NewSource("s"),
		"F": gooey.Command(func() {}),
	}}
	srcs := []string{
		`<Gooey><Canvas><Text Canvas.Left="1" Canvas.Top="2" Width="5">x</Text></Canvas></Gooey>`,
		`<Gooey><Grid Rows="Auto" Cols="1*"><Button Grid.Row="0" Grid.Col="0" Content="ok" Click="{{.F}}"/></Grid></Gooey>`,
		`<Gooey><VStack Gap="1"><Text Name="T" Margin="1,2" HAlign="Center" Visibility="Visible">x</Text></VStack></Gooey>`,
		`<Gooey><TextBox Text="{{.S}}" Tooltip="type here"/></Gooey>`,
		`<Gooey><StatusBar Left="{{.S}}" Right="ok"/></Gooey>`,
	}
	for _, src := range srcs {
		if _, err := Build([]byte(src), ctx); err != nil {
			t.Errorf("valid markup rejected: %v\n%s", err, src)
		}
	}
}

func TestNamespacedAttributesAreLoadErrors(t *testing.T) {
	cases := []struct {
		name, src, want, reason string
	}{
		{
			name:   "standard XML namespace",
			src:    `<Gooey><Text xml:Style="accent">x</Text></Gooey>`,
			want:   "xml:Style",
			reason: "namespaced attributes are not supported",
		},
		{
			name:   "undeclared prefix",
			src:    `<Gooey><Text zz:Style="accent">x</Text></Gooey>`,
			want:   "{zz}Style",
			reason: "namespaced attributes are not supported",
		},
		{
			name:   "declared namespace",
			src:    `<Gooey xmlns:custom="example.com/custom"><Text custom:Style="accent">x</Text></Gooey>`,
			want:   "{example.com/custom}Style",
			reason: "namespaced attributes are not supported",
		},
		{
			name:   "empty namespace binding",
			src:    `<Gooey xmlns:zz=""><Text zz:Style="accent">x</Text></Gooey>`,
			want:   `namespace prefix "zz" cannot be empty`,
			reason: `namespace prefix "zz" cannot be empty`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Build([]byte(tc.src), &Context{})
			if err == nil {
				t.Fatal("namespaced attribute loaded successfully")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not identify %q", err, tc.want)
			}
			if !strings.Contains(err.Error(), tc.reason) {
				t.Errorf("error %q does not explain the namespace restriction", err)
			}
		})
	}
}

// TestRejectionSkipsWhatItCannotKnow is the honesty rule applied to
// enforcement. A registered Go builder interprets attributes however it
// likes and an opaque element's vocabulary was never enumerable, so
// validating either would be inventing a rule the catalog cannot
// support — the same reason AttrsKnown exists.
func TestRejectionSkipsWhatItCannotKnow(t *testing.T) {
	ctx := &Context{Components: map[string]Builder{
		"LogPane": func(e Element, ctx *Context) (gooey.Component, error) {
			return &components.Text{}, nil
		},
	}}
	if _, err := Build([]byte(`<Gooey><LogPane Whatever="x" Anything="y"/></Gooey>`), ctx); err != nil {
		t.Errorf("a registered component's attributes are its own business: %v", err)
	}
}

// TestOpenElementsKeepTheirOwnError — <Validate> can say more than the
// generic check can, because it knows the live rule vocabulary including
// Context.Rules. It must run instead of the generic check, not after it.
func TestOpenElementsKeepTheirOwnError(t *testing.T) {
	ctx := &Context{Values: map[string]any{"S": prop.NewSource("")}}
	_, err := Build([]byte(`<Gooey><TextBox Text="{{.S}}"><Validate Requried="true"/></TextBox></Gooey>`), ctx)
	if err == nil {
		t.Fatal("an unknown rule must still be an error")
	}
	if !strings.Contains(err.Error(), "unknown rule") {
		t.Errorf("error %q lost <Validate>'s own vocabulary message", err)
	}
}

// TestNonVisualElementsTakeNoLayoutAttributes — HasLayout is true for
// every built-in because it comes from the embedded gooey.Base, but a
// non-visual element has no bounds to place. companionAttrs says so and
// omits them deliberately; the check has to agree.
func TestNonVisualElementsTakeNoLayoutAttributes(t *testing.T) {
	ctx := &Context{Values: map[string]any{"F": gooey.Command(func() {})}}
	_, err := Build([]byte(`<Gooey><VStack><Timer Interval="1s" Tick="{{.F}}" Width="4"/></VStack></Gooey>`), ctx)
	if err == nil {
		t.Fatal("Width on a non-visual element must be rejected; it has no bounds to size")
	}
	if !strings.Contains(err.Error(), "no such attribute") {
		t.Errorf("error = %v", err)
	}
}
