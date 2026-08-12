package markup

import (
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/validate"
)

// The open-vocabulary contract, written BEFORE the ElementDef
// restructure so it is red if that restructure gets it wrong.
//
// <Validate> is the one element whose attribute vocabulary is not a
// fixed set: it is validateBuiltins ∪ Context.Rules, resolved live. In a
// scheme where every element declares its Attrs as a struct literal,
// that is the element a mechanical restructure would silently flatten to
// the builtin fifteen.
//
// Flattening it would not merely under-report. NOW THAT UNKNOWN
// ATTRIBUTES ARE REJECTED, it would make the loader REFUSE VALID MARKUP:
// a host registering an Email rule would find <Validate Email="true"/>
// rejected, by the exact mechanism built to make attribute mistakes
// visible. The guard would fire on correct input, which is worse than
// the silent drop it replaced.
//
// So this asserts both halves — that the rule is offered by the catalog
// AND that markup using it loads — because either alone can pass while
// the element is broken.

func emailRule() RuleFunc {
	return func(string) (validate.Rule[string], error) {
		return validate.Pattern(`^[^@\s]+@[^@\s]+$`, "not an email"), nil
	}
}

// TestOpenVocabularyElementAcceptsContextRules is the anti-flattening
// test. It must keep passing through the restructure.
func TestOpenVocabularyElementAcceptsContextRules(t *testing.T) {
	ctx := &Context{
		Values: map[string]any{"S": prop.NewSource("")},
		Rules:  map[string]RuleFunc{"Email": emailRule()},
	}

	// Half 1: the catalog offers the host's rule, tagged as the host's.
	var found *AttrSpec
	for _, e := range ctx.Catalog() {
		if e.Name != "Validate" {
			continue
		}
		if !e.Open {
			t.Error("<Validate> must be Open: its vocabulary is validateBuiltins ∪ Context.Rules")
		}
		for i := range e.Attrs {
			if e.Attrs[i].Name == "Email" {
				found = &e.Attrs[i]
			}
		}
	}
	if found == nil {
		t.Fatal("<Validate> did not offer the host's Email rule — the vocabulary was flattened to the builtins")
	}
	if found.Origin != OriginRegistered {
		t.Errorf("<Validate Email> origin = %s, want registered: a host rule is not a builtin one", found.Origin)
	}

	// Half 2: and markup using it actually loads. A catalog that offers
	// an attribute the loader then rejects is worse than one that offers
	// nothing.
	src := `<Gooey><TextBox Text="{{.S}}"><Validate Email="true"/></TextBox></Gooey>`
	if _, err := Build([]byte(src), ctx); err != nil {
		t.Fatalf("valid markup using a registered rule was rejected: %v", err)
	}
}

// TestOpenVocabularyStillRejectsUnknownRules — Open must not mean "takes
// anything". A name in neither the builtins nor Context.Rules is still
// an error, and the error still names the live vocabulary.
func TestOpenVocabularyStillRejectsUnknownRules(t *testing.T) {
	ctx := &Context{
		Values: map[string]any{"S": prop.NewSource("")},
		Rules:  map[string]RuleFunc{"Email": emailRule()},
	}
	src := `<Gooey><TextBox Text="{{.S}}"><Validate Emial="true"/></TextBox></Gooey>`
	_, err := Build([]byte(src), ctx)
	if err == nil {
		t.Fatal("an unknown rule must still be rejected")
	}
	if !strings.Contains(err.Error(), "Email") {
		t.Errorf("the error should name the live vocabulary including host rules, got: %v", err)
	}
}

// TestDeclaredVocabularyElementsKeepTheirExactSet — <Companion> is the
// other element that already declares its own vocabulary. Its set is
// static, which makes it the case where a literal restructure could
// quietly CHANGE behaviour while looking like it preserved it: adding a
// layout attribute it deliberately omits, or dropping one of its own.
func TestDeclaredVocabularyElementsKeepTheirExactSet(t *testing.T) {
	var comp ElementSpec
	for _, e := range BuiltinElements() {
		if e.Name == "Companion" {
			comp = e
		}
	}
	if comp.Name == "" {
		t.Fatal("<Companion> missing from the catalog")
	}
	got := map[string]bool{}
	for _, a := range comp.Attrs {
		got[a.Name] = true
	}
	// Exactly companionAttrs, minus Name which is universal.
	for n := range companionAttrs {
		if n == "Name" {
			continue
		}
		if !got[n] {
			t.Errorf("<Companion> lost declared attribute %q", n)
		}
		delete(got, n)
	}
	for n := range got {
		t.Errorf("<Companion> gained attribute %q, which companionAttrs does not declare", n)
	}
	// And the deliberate omission: a non-visual element has no bounds, so
	// the layout surface must not be joined onto it.
	for _, a := range AttrsFor(comp, "Grid") {
		if a.Name == "Width" || a.Name == "Grid.Row" {
			t.Errorf("<Companion> offers %q; companionAttrs omits layout attributes on purpose", a.Name)
		}
	}
}
