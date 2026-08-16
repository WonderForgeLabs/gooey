package markup

import (
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/prop"
)

func boundCtx() *Context {
	return &Context{Values: map[string]any{
		"Auto": prop.NewSource(true),
		"Name": prop.NewSource("ada"),
	}}
}

// The built-in half of the rule attr.go states for third-party builders:
// an omitted required attribute is reported as OMITTED.
//
// Bound used to index e.Attrs and hand the miss straight to
// BindingValue, so `<Checkbox/>` loaded with `<Checkbox Checked="">:
// "" is not a binding expression` — an error quoting an attribute the
// author never wrote and blaming the binding syntax for the omission.
// Two built-ins (Image, Segmented) had already hand-patched their own
// way around it; the rest spoke this.
//
// The element and attribute names below come from the markup vocabulary,
// not from Bound: each case is the shortest document that reaches the
// function with the attribute missing, written out by hand.
func TestAnAbsentBoundAttributeIsReportedAsAbsent(t *testing.T) {
	for _, c := range []struct {
		src     string
		element string
		attr    string
	}{
		{`<Checkbox/>`, "Checkbox", "Checked"},
		{`<Checkbox Checked=""/>`, "Checkbox", "Checked"},
		{`<Toggle/>`, "Toggle", "Checked"},
		{`<TextBox/>`, "TextBox", "Text"},
		{`<ProgressBar/>`, "ProgressBar", "Value"},
		{`<Sparkline/>`, "Sparkline", "Values"},
		{`<ItemsView/>`, "ItemsView", "Items"},
		{`<ColorPicker/>`, "ColorPicker", "Value"},
	} {
		t.Run(c.element+"."+c.attr, func(t *testing.T) {
			_, err := Build([]byte(doc(c.src)), boundCtx())
			if err == nil {
				t.Fatalf("%s loaded with %s absent", c.src, c.attr)
			}
			msg := err.Error()
			if !strings.Contains(msg, "<"+c.element+">") {
				t.Errorf("error %q does not name the element <%s>", msg, c.element)
			}
			if !strings.Contains(msg, c.attr) {
				t.Errorf("error %q does not name the attribute %s", msg, c.attr)
			}
			if !strings.Contains(msg, "is required") {
				t.Errorf("error %q does not say %s is required — an author reading this learns nothing", msg, c.attr)
			}
			if strings.Contains(msg, "binding expression") {
				t.Errorf("error %q blames the binding syntax for an attribute that was never written", msg)
			}
			if strings.Contains(msg, c.attr+`=""`) {
				t.Errorf("error %q quotes %s back at the author as an empty attribute they did not write", msg, c.attr)
			}
		})
	}
}

// The discrimination half. Without this, "an absent attribute errors"
// passes just as well against a Bound that rejects EVERY attribute,
// and the three present-but-wrong cases pin that absent did not swallow
// the messages that were already right.
func TestPresentBoundAttributesKeepTheirOwnErrors(t *testing.T) {
	// Still builds, and still hands over the page's own handle rather
	// than a copy — reading equal values would pass against a copy.
	ctx := boundCtx()
	w := buildOne(t, doc(`<Checkbox Checked="{{.Auto}}"/>`), ctx)
	cb, ok := w.(*components.Checkbox)
	if !ok {
		t.Fatalf("root is %T, want *components.Checkbox", w)
	}
	cb.Checked.Set(false)
	if page := ctx.Values["Auto"].(*prop.Property[bool]); page.Get() {
		t.Fatal("the page property did not see the component's write — Bound handed over a copy")
	}

	for _, c := range []struct {
		name string
		src  string
		want string
	}{
		{"a literal where a binding belongs", `<Checkbox Checked="yes"/>`, `"yes" is not a binding expression`},
		{"an unresolvable path", `<Checkbox Checked="{{.Nope}}"/>`, `"Nope" not found in context`},
		{"the wrong handle type", `<Checkbox Checked="{{.Name}}"/>`, `is *prop.Property[string]; need *prop.Property[bool]`},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := Build([]byte(doc(c.src)), boundCtx())
			if err == nil {
				t.Fatalf("%s loaded clean", c.src)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error %q does not contain %q", err, c.want)
			}
			if strings.Contains(err.Error(), "is required") {
				t.Fatalf("error %q reports a written attribute as missing", err)
			}
		})
	}
}
