package markup

import (
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey/prop"
)

func attrCtx() *Context {
	return &Context{Values: map[string]any{
		"Level": prop.NewSource(3),
		"Name":  prop.NewSource("ada"),
	}}
}

func TestAttrResolvesATypedHandle(t *testing.T) {
	e := Element{Name: "Meter", Attrs: map[string]string{"Value": "{{.Level}}"}}
	got, err := Attr[*prop.Property[int]](attrCtx(), e, "Value")
	if err != nil {
		t.Fatal(err)
	}
	if got.Get() != 3 {
		t.Fatalf("resolved handle reads %d, want 3", got.Get())
	}
}

// The handle must be the PAGE'S property, not a copy of its value —
// otherwise a control writes into its own private cell and the page never
// sees it. Reading equal values proves nothing; writing does.
func TestAttrHandsOverTheLiveHandleNotAValue(t *testing.T) {
	ctx := attrCtx()
	e := Element{Name: "Meter", Attrs: map[string]string{"Value": "{{.Level}}"}}
	got, err := Attr[*prop.Property[int]](ctx, e, "Value")
	if err != nil {
		t.Fatal(err)
	}
	got.Set(9)
	if page := ctx.Values["Level"].(*prop.Property[int]); page.Get() != 9 {
		t.Fatalf("page property reads %d after the control set 9 — Attr handed over a copy", page.Get())
	}
}

// The gap this export exists to close, half one: absent is reported as
// absent. The copies passed "" to BindingValue and reported `"" is not a
// binding expression`, which names the machine's problem, not the
// author's.
func TestAnAbsentAttrIsReportedAsAbsent(t *testing.T) {
	ctx := attrCtx()
	for _, c := range []struct {
		name string
		e    Element
	}{
		{"missing key", Element{Name: "Meter", Attrs: map[string]string{}}},
		{"nil map", Element{Name: "Meter"}},
		{"present but empty", Element{Name: "Meter", Attrs: map[string]string{"Value": ""}}},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := Attr[*prop.Property[int]](ctx, c.e, "Value")
			if err == nil {
				t.Fatal("no error for an absent attribute")
			}
			if !strings.Contains(err.Error(), "required") {
				t.Fatalf("error %q does not say the attribute is required — an author reading this learns nothing", err)
			}
			if strings.Contains(err.Error(), "binding expression") {
				t.Fatalf("error %q blames the binding syntax for an attribute that was never written", err)
			}
		})
	}
}

// Half two: present-but-wrong is a different mistake and gets a different
// message. Paired with the absent case above deliberately — either
// assertion alone passes against an implementation that reports one error
// for both.
func TestAWrongTypedAttrNamesBothTypes(t *testing.T) {
	e := Element{Name: "Meter", Attrs: map[string]string{"Value": "{{.Name}}"}}
	_, err := Attr[*prop.Property[int]](attrCtx(), e, "Value")
	if err == nil {
		t.Fatal("a string handle satisfied *prop.Property[int]")
	}
	for _, want := range []string{"*prop.Property[string]", "*prop.Property[int]"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not name %s; an author cannot see which end is wrong", err, want)
		}
	}
	if strings.Contains(err.Error(), "required") {
		t.Fatalf("error %q reports a present attribute as missing", err)
	}
}

func TestANonBindingAttrIsNotATypeError(t *testing.T) {
	e := Element{Name: "Meter", Attrs: map[string]string{"Value": "7"}}
	_, err := Attr[*prop.Property[int]](attrCtx(), e, "Value")
	if err == nil {
		t.Fatal("a bare literal resolved as a binding")
	}
	if !strings.Contains(err.Error(), "binding expression") {
		t.Fatalf("error %q does not say the attribute is not a binding — this is the one case where blaming the syntax IS right", err)
	}
}

// A page with six <Meter>s and one typo is a hunt unless the element is
// named. Every error path carries it, which is what this iterates.
func TestEveryAttrErrorNamesTheElementAndAttribute(t *testing.T) {
	ctx := attrCtx()
	for _, c := range []struct {
		name string
		e    Element
	}{
		{"absent", Element{Name: "Meter", Attrs: map[string]string{}}},
		{"not a binding", Element{Name: "Meter", Attrs: map[string]string{"Value": "7"}}},
		{"wrong type", Element{Name: "Meter", Attrs: map[string]string{"Value": "{{.Name}}"}}},
		{"unknown path", Element{Name: "Meter", Attrs: map[string]string{"Value": "{{.Nope}}"}}},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := Attr[*prop.Property[int]](ctx, c.e, "Value")
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), "Meter") {
				t.Errorf("error %q does not name the element", err)
			}
			if !strings.Contains(err.Error(), "Value") {
				t.Errorf("error %q does not name the attribute", err)
			}
		})
	}
}
