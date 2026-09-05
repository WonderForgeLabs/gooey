package markup

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/prop"
)

// TestAnUnknownMenuAttributeIsALoadError closes the hole that made
// <Menu> and <MenuItem> the only elements in the vocabulary with
// NEITHER direction of the drift check guarded.
//
// checkAttrs runs inside build(), and these two never reach it —
// buildMenuBar reads them straight off e.Children, which is what
// "consumed as data" means. So `<MenuItem Frobnicate="yes"/>` loaded
// clean: the builder read the names it knew and every other attribute
// was accepted and silently dropped. That is the silent-drop defect the
// declared vocabulary exists to prevent, reachable through the one
// corner of the vocabulary the loader never looked at.
//
// Both elements are checked, and the separator case separately: the
// Separator short-circuit returns before the rest of the item is read,
// so a check placed after it would skip exactly the items a typo is
// easiest to make on.
func TestAnUnknownMenuAttributeIsALoadError(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{
			"on <Menu>",
			`<Gooey><MenuBar><Menu Title="F" Bogus="x"><MenuItem Text="Open"/></Menu></MenuBar></Gooey>`,
			"Bogus",
		},
		{
			"on <MenuItem>",
			`<Gooey><MenuBar><Menu Title="F"><MenuItem Text="Open" Frobnicate="yes"/></Menu></MenuBar></Gooey>`,
			"Frobnicate",
		},
		{
			"on a separator item, which returns before the rest is read",
			`<Gooey><MenuBar><Menu Title="F"><MenuItem Separator="true" Nonsense="1"/></Menu></MenuBar></Gooey>`,
			"Nonsense",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Build([]byte(tc.src), &Context{})
			if err == nil {
				t.Fatalf("accepted and silently dropped %s", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("the error does not name the attribute: %v", err)
			}
		})
	}
}

// TestTheMenuVocabularyStillLoadsWhatItDeclares is the other half, and
// it is not ceremony: a check that rejects everything would pass the
// test above. Every attribute the two elements declare is exercised
// here, so an over-tight vocabulary fails rather than looking correct.
// TestAPseudoElementRefusesName is the one universal that was NOT
// correctly withheld, and it is the same silent-drop class the declared
// vocabulary exists to close.
//
// Name is hoisted ABOVE the TakesLayout gate in both vocabulary
// (attrcheck.go) and AttrsFor (catalog.go), because addressability is
// not a layout property. But buildMenuBar consumes its children as data
// and never calls named(), so <MenuItem Name="Save"> loaded clean and
// ctx.Named stayed empty forever — accepted, dropped, no error anywhere.
//
// The designer made that reachable AND put the row first (KindIdentity
// sorts into CategoryDesign at rank 0), so the fix for #429 turned a
// latent hole into an inviting one. Found in review of #454.
func TestAPseudoElementRefusesName(t *testing.T) {
	for _, tc := range []struct{ name, doc string }{
		{"menu", `<Menu Title="_File" Name="Zonk"><MenuItem Text="Open" Command="{{.Op}}"/></Menu>`},
		{"item", `<Menu Title="_File"><MenuItem Text="Open" Name="Zork" Command="{{.Op}}"/></Menu>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fsys := fstest.MapFS{"p.gooey": {Data: []byte(
				`<Gooey><VStack><MenuBar Name="bar">` + tc.doc + `</MenuBar></VStack></Gooey>`)}}
			ctx := &Context{Values: map[string]any{"Op": func() {}}}
			_, err := Load(fsys, "p.gooey", ctx)
			if err == nil {
				t.Fatalf("Name was accepted on a pseudo-element; ctx.Named has %d entries and neither is it", len(ctx.Named))
			}
			// THE ASSERTION IS ON THE TAIL, and the obvious one is
			// unfireable. The format is
			// `markup: <%s %s=%q>: no such attribute%s`, so the message
			// ALWAYS echoes the offending attribute — for this input
			// strings.Contains(err, "Name") is true whatever suggest()
			// does, and it passed identically before and after the
			// attrcheck fix. It was checking the message prefix, not the
			// vocabulary.
			//
			// What has to hold is that nothing AFTER "no such attribute"
			// mentions Name: not the near-miss suggestion, not the
			// exhaustive listing. That is the half that regressed —
			// suggest() ranges over allowed's KEYS and the key was
			// present at value false — and it is what keeps the two
			// halves, refusing it and not advertising it, from drifting
			// apart again. Found in review of #454.
			msg := err.Error()
			_, tail, ok := strings.Cut(msg, "no such attribute")
			if !ok {
				t.Fatalf("the refusal is not the vocabulary one, so the tail below means nothing: %v", err)
			}
			if strings.Contains(tail, "Name") {
				t.Errorf("the message advertises Name on the element that just refused it — "+
					"a reader who follows it lands back on the same error:\n\t%s", msg)
			}
		})
	}
}

// TestTheCatalogDoesNotOfferNameOnAPseudoElement is the other half, and
// it is not redundant: the loader and the catalog are separate gates and
// only one of them is what a property grid reads. A grid offering a row
// the loader refuses is worse than either alone — it invites the edit
// and then rejects the file.
func TestTheCatalogDoesNotOfferNameOnAPseudoElement(t *testing.T) {
	ctx := &Context{Values: map[string]any{}}
	var checked int
	for _, e := range ctx.Catalog() {
		if !e.Pseudo {
			continue
		}
		checked++
		for _, a := range AttrsFor(e, "") {
			if a.Kind == KindIdentity {
				t.Errorf("the catalog offers %q on <%s>, which builds no component to address", a.Name, e.Name)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no pseudo-elements in the catalog: this test asserted nothing")
	}
	// The other direction, so the check cannot pass by the identity
	// attribute having vanished for everyone.
	var found bool
	for _, e := range ctx.Catalog() {
		if e.Pseudo || e.Name != "Button" {
			continue
		}
		for _, a := range AttrsFor(e, "") {
			if a.Kind == KindIdentity {
				found = true
			}
		}
	}
	if !found {
		t.Error("<Button> is no longer offered an identity attribute either: Name was withheld from everything")
	}
}

func TestTheMenuVocabularyStillLoadsWhatItDeclares(t *testing.T) {
	src := `<Gooey><MenuBar>
	  <Menu Title="_File">
	    <MenuItem Text="Open" Gesture="ctrl+o" Command="Go"/>
	    <MenuItem Separator="true"/>
	    <MenuItem Text="Wrap" Checked="{{.On}}"/>
	  </Menu>
	</MenuBar></Gooey>`
	if _, err := Build([]byte(src), &Context{
		Values:   map[string]any{"On": prop.NewSource(true)},
		Handlers: map[string]gooey.Action{"Go": gooey.Command(func() {})},
	}); err != nil {
		t.Fatalf("a document using every declared menu attribute does not load: %v", err)
	}
}

// TestARestrictedContainerDoesNotHideARealElement pins the conjunct in
// markNested, and it needs a host-declared fixture because NOTHING IN
// THE BOX CAN FAIL IT. Every restricted container shipped today names
// pseudo-children, so dropping the Pseudo conjunct passes the whole
// suite — a guard that cannot fail is not a guard, so the case it
// guards has to be constructed.
//
// The rule it protects: ModeRestricted says "this container accepts
// only these", and Nested says "this element is accepted only inside a
// container that names it". Those are converses, not equivalents. A
// host declaring a toolbar with Only: ["Button"] is an ordinary,
// reasonable container — and reading the Only list alone would mark
// <Button> nested and vanish it from every palette, silently. That is
// the same class of failure Nested exists to remove, inverted.
func TestARestrictedContainerDoesNotHideARealElement(t *testing.T) {
	ctx := &Context{Elements: map[string]*ElementDef{
		"HostBar": {
			Name:     "HostBar",
			Proto:    &components.Text{},
			Known:    true,
			Children: ChildSpec{Mode: ModeRestricted, Only: []string{"Button"}},
			Build: func(e Element, ctx *Context) (gooey.Component, error) {
				return &components.Text{}, nil
			},
		},
	}}
	var button, hostBar ElementSpec
	var found bool
	for _, e := range ctx.Catalog() {
		switch e.Name {
		case "Button":
			button, found = e, true
		case "HostBar":
			hostBar = e
		}
	}
	if !found {
		t.Fatal("<Button> is not in the catalog: the fixture proves nothing")
	}
	// The fixture has to actually restrict, or the assertion below holds
	// for the boring reason.
	if hostBar.Children.Mode != ModeRestricted || len(hostBar.Children.Only) != 1 {
		t.Fatalf("the host container did not reach the catalog restricted: %+v", hostBar.Children)
	}
	if button.Pseudo {
		t.Fatal("<Button> is Pseudo, so this fixture cannot distinguish the two rules")
	}
	if button.Nested {
		t.Error("<Button> was marked Nested because a host container restricts itself to it: " +
			"the palette would stop offering an ordinary element, silently. " +
			"markNested is reading the converse of the property it claims.")
	}
	// And the pseudo-children of the SAME catalog are still marked, so a
	// fix cannot be "stop marking anything".
	for _, e := range ctx.Catalog() {
		if e.Name == "MenuItem" && !e.Nested {
			t.Error("<MenuItem> is no longer Nested: the derivation was widened into uselessness")
		}
	}
}

// TestMarkNestedIsIdempotent pins the difference between deriving and
// accumulating, which is invisible in the shipped catalog.
//
// markNested runs TWICE over overlapping data — BuiltinElements marks,
// then Catalog re-runs over a list seeded from it — so a conditional
// set would carry a true from the first pass through a second pass that
// would not have produced it. Failure scenario: a host shadows <Tabs>
// with an unrestricted declaration of its own, nothing in the assembled
// catalog restricts to <Tab>, and <Tab> stays hidden anyway because the
// builtin pass already marked it.
func TestMarkNestedIsIdempotent(t *testing.T) {
	specs := []ElementSpec{
		// Pre-marked, exactly as a spec arriving from BuiltinElements
		// would be, and nothing here names it.
		{Name: "Orphan", Pseudo: true, Nested: true},
		{Name: "Holder", Children: ChildSpec{Mode: ModeMany}},
	}
	markNested(specs)
	if specs[0].Nested {
		t.Error("markNested left Nested set on an element no container names: " +
			"it is accumulating across passes rather than deriving, so the " +
			"field's DERIVED claim does not hold")
	}
}

// TestAHostElementWithNoProtoIsNotPseudo is the case the builtin guard
// cannot see, and the whole reason Pseudo takes two conjuncts.
//
// TestDeclaredElementsCarryAProtoOrSayWhyNot forces a Proto-less element
// to say why — Opaque, or a ParsedBy naming its reader — but it ranges
// over definedElements(), the BUILTIN registry, and never sees anything
// a host puts in Context.Elements. A host def with a real Build and no
// Proto is legal and nothing rejects it.
//
// Deriving Pseudo from the nil alone would make such a def silently
// pseudo, and every consequence is silent: dropped from any palette
// filtering Nested, and — in the wysiwyg designer — unselectable
// through, because pairAgrees refuses to pair a pseudo-element with the
// component it actually built. Nothing in this repo trips it today,
// which is exactly why it needs a test rather than a comment.
func TestAHostElementWithNoProtoIsNotPseudo(t *testing.T) {
	ctx := &Context{Elements: map[string]*ElementDef{
		// A perfectly ordinary host element: it builds a component, it
		// just does not hand the registry a prototype to derive axes
		// from. Nothing anywhere requires it to.
		"HostThing": {
			Name:     "HostThing",
			Known:    true,
			Children: ChildSpec{Mode: ModeMany},
			Build: func(e Element, ctx *Context) (gooey.Component, error) {
				return &components.Text{}, nil
			},
		},
	}}
	var got ElementSpec
	var found bool
	for _, e := range ctx.Catalog() {
		if e.Name == "HostThing" {
			got, found = e, true
		}
	}
	if !found {
		t.Fatal("the host element is not in the catalog: the fixture proves nothing")
	}
	if got.Pseudo {
		t.Error("a host element with a real Build and no Proto was marked Pseudo: " +
			"nothing requires a registered def to say why it has no Proto, so this " +
			"is derivable from a nil nobody checks. It would drop out of every " +
			"palette filtering Nested and stop the designer selecting through it, " +
			"silently.")
	}
	// And the builtins that ARE pseudo still are, so a fix cannot be
	// "stop deriving it".
	for _, e := range ctx.Catalog() {
		if e.Name == "MenuItem" && !e.Pseudo {
			t.Error("<MenuItem> is no longer Pseudo: the derivation was narrowed into uselessness")
		}
	}
}
