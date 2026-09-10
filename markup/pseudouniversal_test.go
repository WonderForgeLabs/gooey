package markup

import (
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
)

// isUniversal answers from universalAttrs rather than from a list here,
// so a ninth universal joins every assertion below on the commit that
// declares it. Every test in this file derives its cases the same way,
// for the reason CLAUDE.md's Verify section gives about written-down
// sets: a list in a test is stale the first time someone adds a row, and
// the failure is silent — the loop still runs, just over less.
func isUniversal(name string) bool {
	for _, a := range universalAttrs {
		if a.Name == name {
			return true
		}
	}
	return false
}

// pseudoSpecs is every pseudo-element the builtin vocabulary declares.
// <Tab>, <Menu> and <MenuItem> today; the point of asking the catalog is
// that a fourth is covered without this file being edited.
func pseudoSpecs(t *testing.T) []ElementSpec {
	t.Helper()
	var out []ElementSpec
	for _, s := range (&Context{}).Catalog() {
		if s.Pseudo {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		t.Fatal("the catalog reports no pseudo-element at all, so every " +
			"assertion in this file would pass over an empty set")
	}
	return out
}

// TestNoPseudoElementAcceptsAUniversalAttribute is issue #461, and the
// shape of it is that TWO GATES DISAGREED IN OPPOSITE DIRECTIONS.
//
// A pseudo-element builds no component of its own: its parent's Build
// reads it as data and draws the result itself, so it never reaches
// named() and never reaches applyLayout(). Every universal attribute is
// therefore accepted by nothing.
//
// Grant.AttrsFor (catalog.go) already knew that — it withholds the
// layout set on !TakesLayout and withholds Name on Pseudo, so the
// designer's property grid offers a pseudo-element no universal row at
// all. checkAttrs did not: it returns early on !AttrsKnown, and <Tab> is
// the one pseudo-element whose attributes are not enumerable. So
// `<Tab Name="Zonk">` loaded clean, was dropped, and had no designer
// surface that would have revealed it — strictly less discoverable than
// before #454 removed the row.
//
// THE GATE IS Pseudo, NOT !TakesLayout, and that distinction is the
// whole reason this is a fix rather than a design change. `AttrsKnown:
// false` says the element's OWN vocabulary cannot be enumerated, and
// refusing anything from it would invent a rule the catalog cannot
// support. Pseudo is not part of that vocabulary: it is an AFFIRMATIVE
// derived fact (a nil Proto AND a stated reason, elementdef.go), and it
// entails that no universal applies. !TakesLayout is the opposite kind
// of fact — absent-by-default. A host's Context.Components builder has
// no Proto either, so TakesLayout is false for it while the framework
// still applies Margin to the component it returns; refusing the layout
// set off that absence would break working apps. Only the affirmative
// fact is safe to refuse from.
func TestNoPseudoElementAcceptsAUniversalAttribute(t *testing.T) {
	ctx := &Context{}
	for _, s := range pseudoSpecs(t) {
		for _, u := range universalAttrs {
			e := Element{Name: s.Name, Attrs: map[string]string{u.Name: "x"}}
			err := checkAttrs(e, ctx)
			if err == nil {
				t.Errorf("<%s %s=\"x\"> is accepted and honoured by nothing: "+
					"%s builds no component, so the attribute reaches neither "+
					"named() nor applyLayout(), and Grant.AttrsFor already "+
					"withholds the row — the two gates must agree or the "+
					"designer offers what the loader refuses",
					s.Name, u.Name, s.Name)
				continue
			}
			if !strings.Contains(err.Error(), u.Name) {
				t.Errorf("<%s %s=\"x\">: the error does not name the "+
					"attribute: %v", s.Name, u.Name, err)
			}
		}
	}
}

// TestTheDesignerOffersNoUniversalRowOnAPseudoElement is the other gate,
// asserted over the same derived set. Without it "the two gates agree"
// is a claim about one of them: the refusal above would still pass if
// AttrsFor started offering Name again, and the disagreement would be
// back with the loader on the other side of it.
func TestTheDesignerOffersNoUniversalRowOnAPseudoElement(t *testing.T) {
	for _, s := range pseudoSpecs(t) {
		for _, a := range grantOf("").AttrsFor(s) {
			if isUniversal(a.Name) {
				t.Errorf("the property grid offers <%s> a %s row, which the "+
					"loader refuses", s.Name, a.Name)
			}
		}
	}
}

// TestAnOpaqueElementsOwnAttributesAreStillNotJudged is the arm that
// says the fix did not quietly become something bigger.
//
// <Tab> carries //gooey:catalog-opaque because <Tabs> parses it, so its
// own surface genuinely is unknowable to the catalog. Refusing a
// universal on it is a statement about the UNIVERSAL SET; teaching
// checkAttrs to refuse anything else would be a statement about <Tab>,
// which the catalog cannot back. A guard that over-reaches here fails
// closed on markup a future <Tabs> reader would accept, and this is
// where that shows up.
func TestAnOpaqueElementsOwnAttributesAreStillNotJudged(t *testing.T) {
	src := `<Gooey><Tabs><Tab Header="a" Frobnicate="yes"><Text>x</Text></Tab></Tabs></Gooey>`
	if _, err := Build([]byte(src), &Context{}); err != nil {
		t.Errorf("an attribute outside the universal set was refused on an "+
			"element whose vocabulary is not enumerable: %v", err)
	}
}

// TestAPseudoElementStillLoadsWhatItsParentReads is the must-load half.
// A refusal that fired on everything would satisfy the two tests above
// and break every page in the repo; only this one notices.
func TestAPseudoElementStillLoadsWhatItsParentReads(t *testing.T) {
	for _, tc := range []struct{ name, src string }{
		{"<Tab>", `<Gooey><Tabs><Tab Header="a"><Text>x</Text></Tab></Tabs></Gooey>`},
		{"<Menu> and <MenuItem>", `<Gooey><MenuBar><Menu Title="F"><MenuItem Text="Open"/></Menu></MenuBar></Gooey>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Build([]byte(tc.src), &Context{}); err != nil {
				t.Errorf("the refusal reached an attribute the parent's "+
					"builder actually reads: %v", err)
			}
		})
	}
}

// TestAnUnenumerableElementThatBuildsOneKeepsItsUniversals is the case
// this tree did not have, and it is here because a mutation went SILENT
// without it.
//
// Swapping refuseUniversal's gate from spec.Pseudo to !TakesLayout(spec)
// broke nothing in the suite, and the comment beside that gate asserts
// the swap would break working apps. That claim was unpinned.
//
// THE DISCRIMINATING SHAPE IS NARROW, and finding it is the whole
// exercise. It needs an element ctx.spec RESOLVES — a Context.Components
// entry returns !ok, so both gates short-circuit and the first attempt at
// this test could not tell them apart — with AttrsKnown false so the
// early return is reached, a nil Proto so TakesLayout is false, and NO
// stated reason, so Pseudo is false. That is a HOST's ElementDef with a
// real Build and no Proto, which is exactly the case ElementSpec.Pseudo
// names as the reason its derivation takes both conjuncts.
//
// Such an element BUILDS A REAL COMPONENT, and build() runs applyLayout
// and applyTooltipShorthand on whatever comes back (markup.go), so its
// universals are honoured. !TakesLayout cannot tell it from a <Tab>;
// Pseudo can.
//
// BOTH HALVES ARE ASSERTED, not just the load. "It still loads" would
// pass against a build that accepted the attribute and dropped it —
// which is the very defect #461 is about, so the absence of an error
// proves nothing on its own here.
func TestAnUnenumerableElementThatBuildsOneKeepsItsUniversals(t *testing.T) {
	ctx := &Context{Elements: map[string]*ElementDef{
		"LogPane": {
			Name:  "LogPane",
			Known: false, // its attributes are the builder's business
			Doc:   "A host element with a real Build and no Proto.",
			Build: func(e Element, ctx *Context) (gooey.Component, error) {
				return &components.Text{}, nil
			},
		},
	}}
	spec, ok := ctx.spec("LogPane")
	if !ok || spec.AttrsKnown || spec.Pseudo || TakesLayout(spec) {
		t.Fatalf("the fixture is not the discriminating shape — it needs "+
			"resolved/!AttrsKnown/!Pseudo/!TakesLayout and is %v/%v/%v/%v, so "+
			"the two gates would agree about it and this test could not see "+
			"the difference",
			ok, !spec.AttrsKnown, !spec.Pseudo, !TakesLayout(spec))
	}
	root, err := Build([]byte(`<Gooey><LogPane Name="Pane" Margin="2"/></Gooey>`), ctx)
	if err != nil {
		t.Fatalf("a host element that builds a real component was refused a "+
			"universal attribute the framework applies to it: %v", err)
	}
	if root == nil {
		t.Fatal("Build returned no root")
	}
	w := ctx.Named["Pane"]
	if w == nil {
		t.Fatalf("Name was accepted and dropped: ctx.Named holds %d entries",
			len(ctx.Named))
	}
	hl, ok := w.(gooey.HasLayout)
	if !ok {
		t.Fatalf("the host element's component has no Layout, so Margin "+
			"could not have been honoured either: %T", w)
	}
	if m := hl.LayoutProps().Margin; m.L != 2 {
		t.Errorf("Margin was accepted and dropped: %+v", m)
	}
}

// TestTheTabNameCaseIsRefusedThroughAWholeLoad is #461's own reproducer,
// spelled as the user meets it. The three tests above call checkAttrs
// directly, which proves the gate and not the wiring: checkAttrs runs
// inside build(), and #454's finding was precisely that <Menu> and
// <MenuItem> never reached it. An assertion on the function cannot see a
// caller that does not call it.
func TestTheTabNameCaseIsRefusedThroughAWholeLoad(t *testing.T) {
	ctx := &Context{}
	src := `<Gooey><Tabs><Tab Header="a" Name="Zonk"><Text>x</Text></Tab></Tabs></Gooey>`
	_, err := Build([]byte(src), ctx)
	if err == nil {
		t.Fatalf("<Tab Name=\"Zonk\"> loaded clean and named %d elements — "+
			"accepted, dropped, and invisible in the designer too",
			len(ctx.Named))
	}
	if !strings.Contains(err.Error(), "Name") || !strings.Contains(err.Error(), "Tab") {
		t.Errorf("the error names neither the attribute nor the element: %v", err)
	}
}
