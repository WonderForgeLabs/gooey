package markup

import (
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// ElementDef.Body: which elements carry their content in the XML body.
//
// The fact was real and undeclared. defText's Doc has always said "The
// content is the element's body, not an attribute" — in PROSE, where a
// consumer cannot read it. The first consumer that needed the answer
// (the wysiwyg properties grid, deciding whether to offer a content
// row) had two options and both were bad, which is what these tests
// pin: the tempting derivation is wrong, and the fallback is a
// hardcoded name.

func bodyCtx() *Context {
	return &Context{
		Values: map[string]any{"Title": prop.NewSource("live")},
		Styles: map[string]render.Style{},
		Named:  map[string]gooey.Component{},
	}
}

// The declaration itself, read the way a palette reads it — off
// Catalog(), not off the unexported registry.
func TestTextDeclaresThatItsContentIsItsBody(t *testing.T) {
	var got *BodySpec
	for _, e := range (&Context{}).Catalog() {
		if e.Name == "Text" {
			got = e.Body
		}
	}
	if got == nil {
		t.Fatal("<Text> does not declare a Body; a consumer has no way to know its content is not an attribute")
	}
	// Binds is the field with teeth. <Text>'s body goes through
	// bindText, so {{.Path}} is a live binding there — an editor that
	// read BindsLiteral would quietly downgrade a user's binding to
	// literal text.
	if got.Binds != BindsEither {
		t.Errorf("Binds = %q, want %q: the body accepts a binding as well as a literal", got.Binds, BindsEither)
	}
	if got.Kind != KindText {
		t.Errorf("Kind = %q, want %q", got.Kind, KindText)
	}
}

// THE LOAD-BEARING ONE.
//
// ChildSpec.Mode is the derivation a consumer reaches for first, and it
// is wrong: "takes no children" and "takes body content" are different
// statements that happen to coincide on <Text>. This asserts the gap
// exists rather than asserting a count, so it keeps working as the
// vocabulary grows in either direction — and it is what stops the next
// palette from offering a content row on a dozen elements that discard
// it.
func TestChildModeIsNotTheBodyDiscriminator(t *testing.T) {
	var leaves, bodies int
	for _, e := range (&Context{}).Catalog() {
		if e.Children.Mode == ModeLeaf {
			leaves++
		}
		if e.Body != nil {
			bodies++
		}
	}
	if bodies == 0 {
		t.Fatal("no element declares a Body; this test can no longer discriminate")
	}
	if leaves <= bodies {
		t.Fatalf("ModeLeaf elements (%d) no longer outnumber body-content elements (%d); "+
			"if they have genuinely converged, delete this test rather than adjusting it — "+
			"but first check that a Body declaration was not simply forgotten", leaves, bodies)
	}
}

// A declaration nothing consumes is decoration. This is the other half:
// the body <Text> declares actually reaches the component.
func TestTheDeclaredBodyIsTheContentThatGetsBuilt(t *testing.T) {
	ctx := bodyCtx()
	buildOne(t, `<Gooey xmlns="wonderforge.io/gooey/2026"><Text Name="t">hello</Text></Gooey>`, ctx)
	txt, ok := ctx.Named["t"].(*components.Text)
	if !ok {
		t.Fatalf("Named[t] is %T, want *components.Text", ctx.Named["t"])
	}
	if got := txt.Content.Get(); got != "hello" {
		t.Errorf("content = %q, want %q", got, "hello")
	}
}

// The discrimination for BindsEither: a bound body must stay LIVE, not
// be flattened to the string it happened to hold at load. Without this,
// TestTextDeclaresThatItsContentIsItsBody passes against a declaration
// that lies about what the builder does.
func TestABoundBodyStaysLive(t *testing.T) {
	ctx := bodyCtx()
	src := prop.NewSource("live")
	ctx.Values["Title"] = src
	buildOne(t, `<Gooey xmlns="wonderforge.io/gooey/2026"><Text Name="t">{{.Title}}</Text></Gooey>`, ctx)
	txt := ctx.Named["t"].(*components.Text)
	if got := txt.Content.Get(); got != "live" {
		t.Fatalf("content = %q, want %q", got, "live")
	}
	src.Set("changed")
	if got := txt.Content.Get(); got != "changed" {
		t.Errorf("content = %q after the source changed, want %q: the body was flattened to a literal", got, "changed")
	}
}

// A spec is handed out, so a consumer must not be able to reach back
// through it and edit the registry's own definition. Attrs and Slots
// are copied for this reason; Body is a POINTER, which is the one shape
// where sharing is silent — the mutation lands in every later reader.
func TestABodySpecHandedOutIsACopy(t *testing.T) {
	first := (&Context{}).Catalog()
	for _, e := range first {
		if e.Body != nil {
			e.Body.Doc = "mutated through the returned spec"
			e.Body.Binds = BindsLiteral
		}
	}
	for _, e := range (&Context{}).Catalog() {
		if e.Body == nil {
			continue
		}
		if e.Body.Binds == BindsLiteral && e.Name == "Text" {
			t.Fatalf("<Text>'s Body was mutated through a previously returned spec")
		}
	}
}
