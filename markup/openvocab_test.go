package markup

import (
	"maps"
	"slices"
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

// TestTheMenuFamilyKeepsItsExactSet is the PARTITION check that
// catalogen cannot make, and it exists because the mitigation this
// repo's own comments claimed for that gap did not hold.
//
// catalogen.checkPseudo compares a pseudo-element's declared Attrs
// against the host Build's UNION of child reads — buildMenuBar reads
// <Menu>'s Title and <MenuItem>'s Text, and the AST carries no record of
// which element each read came off. So an attribute declared on the
// WRONG sibling passes in BOTH directions: it is in the union, so
// over-declaration sees nothing, and the union is covered, so
// checkPseudoPool sees nothing either.
//
// Measured on this branch: adding a `Text` AttrSpec to defMenu left
// markup, markup/internal/catalogen and apps/wysiwyg all green, and
//
//	<Menu Text="ghost"><MenuItem Text="Open"/></Menu>
//
// loaded clean and was silently dropped — the exact class this package's
// doc comment says nothing but catalogen catches, reintroduced inside
// the element family this PR adds. The designer grid gains a dead row
// with it, because Grant.AttrsFor reads spec.Attrs directly.
//
// THE MITIGATION THAT WAS CLAIMED DOES NOT WORK.
// TestASelectedMenuOffersItsTitle and TestASelectedMenuItemOffersItsAttributes
// are PRESENCE loops that return on the first match, so they catch an
// attribute MOVING off its element and cannot see one being ADDED.
// Presence is not partition.
//
// SO THE SETS ARE LITERAL HERE, and that is the whole mechanism —
// deriving them from defMenu/defMenuItem, in this package, would assert
// the declaration against itself and pass on any gain. An attribute
// added to either element is a decision that has to be made in a failing
// test. Raised in review of #454.
func TestTheMenuFamilyKeepsItsExactSet(t *testing.T) {
	specs := map[string]ElementSpec{}
	for _, e := range BuiltinElements() {
		specs[e.Name] = e
	}
	for _, tc := range []struct {
		el, parent string
		want       []string
	}{
		{"Menu", "MenuBar", []string{"Title"}},
		// Icon and IconRune arrived with this PR, and this line is the
		// decision that had to be made in a failing test rather than a
		// side effect: the pin landed one PR down the stack and went red
		// here on the rebase, naming both attributes. That is what the
		// literal set is for — a derived one would have absorbed them
		// silently, which is the whole defect it guards.
		{"MenuItem", "Menu", []string{
			"Checked", "Command", "Gesture", "Icon", "IconRune",
			"Separator", "Text"}},
	} {
		t.Run(tc.el, func(t *testing.T) {
			spec, ok := specs[tc.el]
			if !ok {
				t.Fatalf("<%s> missing from the catalog", tc.el)
			}
			got := map[string]bool{}
			for _, a := range AttrsFor(spec, tc.parent) {
				got[a.Name] = true
			}
			// Name is universal and joined onto everything; it is not
			// part of what either element declares.
			delete(got, "Name")
			for _, n := range tc.want {
				if !got[n] {
					t.Errorf("<%s> no longer offers %q inside <%s>", tc.el, n, tc.parent)
				}
				delete(got, n)
			}
			for _, n := range slices.Sorted(maps.Keys(got)) {
				t.Errorf("<%s> offers %q inside <%s> and this set does not list it. "+
					"buildMenuBar reads the whole family's attributes through one "+
					"Build, so catalogen cannot tell an attribute declared on the "+
					"wrong sibling from one declared on the right one — a <%s %s=…> "+
					"would load clean and be silently dropped. If the attribute is "+
					"real, buildMenuBar has to read it off THIS element and this "+
					"list has to name it.", tc.el, n, tc.parent, tc.el, n)
			}
		})
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
