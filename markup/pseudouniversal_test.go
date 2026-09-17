package markup

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
)

// isUniversal is attrcheck.go's isUniversalAttr, called rather than
// copied.
//
// It was a third identical loop over universalAttrs — one in
// refuseComponentAttr, one exported as isUniversalAttr, one here — and a
// test that restates a predicate instead of calling it cannot see the
// predicate change, which is the trap this branch already hit once.
// Kept as a name because every assertion below reads better for it, and
// because the alias is one line rather than a body that can drift.
// Raised in review of #486.
//
// The derivation is still the point: a ninth universal joins every
// assertion below on the commit that declares it, for the reason
// CLAUDE.md's Verify section gives about written-down sets — a list in a
// test is stale the first time someone adds a row, and the failure is
// silent, because the loop still runs, just over less.
func isUniversal(name string) bool { return isUniversalAttr(name) }

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

// legalParent is the container the catalog says accepts this element:
// the ModeRestricted entry naming it in Children.Only.
//
// It is derived rather than written down for the usual reason, and for
// one specific to this file: checkAttrs now stands down on an element
// whose PLACEMENT is wrong, so that its parent's Build can report the
// larger fault (review of #486, finding 1). An Element built by hand
// with no parent is misplaced by that rule, and every direct-call
// assertion below silently stopped reaching the check when the gate
// landed — measured, not guessed.
func legalParent(t *testing.T, s ElementSpec) string {
	t.Helper()
	for _, p := range (&Context{}).Catalog() {
		if p.Children.Mode != ModeRestricted {
			continue
		}
		for _, n := range p.Children.Only {
			if n == s.Name {
				return p.Name
			}
		}
	}
	t.Fatalf("no element in the catalog accepts <%s> as a child, so it has "+
		"no legal home and nothing below can place it correctly. A "+
		"pseudo-element nothing names is a declaration no document can "+
		"reach — see markNested", s.Name)
	return ""
}

// advertises reports that a refusal message OFFERS the attribute it just
// rejected, which is the hazard TestAPseudoElementRefusesName exists for:
// "no such attribute; this element takes Checked, Command, ..., Name"
// sends a reader straight back to the same error.
//
// Anchored on the vocabulary clause rather than on the whole message,
// because every refusal legitimately NAMES the attribute it rejects —
// "so Name would be applied to nothing" is the message doing its job.
// The two are only distinguishable by position.
func advertises(msg, attr string) bool {
	_, tail, ok := strings.Cut(msg, "this element takes")
	return ok && strings.Contains(tail, attr)
}

// TestTheAdvertisementCheckCanActuallyFire is advertises' own arm. It is
// a NEGATIVE assertion everywhere it is used, and a negative assertion
// passes for any reason at all — including a Cut anchor that no message
// in the tree matches any more.
func TestTheAdvertisementCheckCanActuallyFire(t *testing.T) {
	bad := `markup: <MenuItem Name="Zork">: no such attribute; this element ` +
		`takes Checked, Command, Gesture, Name, Separator, Text`
	if !advertises(bad, "Name") {
		t.Errorf("advertises() does not fire on a message that plainly "+
			"advertises the refused attribute, so every use of it below is "+
			"vacuous:\n\t%s", bad)
	}
	good := `markup: <MenuItem Name="Zork">: <Menu> reads <MenuItem> as ` +
		`data, so it builds no component for Name to apply to`
	if advertises(good, "Name") {
		t.Errorf("advertises() fires on a message that only NAMES the "+
			"attribute it refuses, which every good refusal does:\n\t%s", good)
	}
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
			e := Element{
				Name:   s.Name,
				parent: legalParent(t, s),
				Attrs:  map[string]string{u.Name: "x"},
			}
			// asData: this is the READER's call — the position
			// buildTabs and buildMenuBar occupy — and it is the only
			// one the refusal applies to. See checkAttrs' gate.
			err := checkAttrs(e, ctx, true)
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
		// THE PARENT'S GRANT, not the root's. The designer calls
		// AttrsFor with the grant of the container the element is
		// actually in, and grantOf("") is the empty root grant — a
		// different question that happens to have the same answer
		// today. Raised in review of #486.
		for _, a := range grantOf(legalParent(t, s)).AttrsFor(s) {
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

// wholeLoad is one pseudo-element's pair of whole-document cases: markup
// its parent's builder must still accept, and markup carrying a
// universal that a full Build must refuse.
//
// HAND-WRITTEN, AND THAT IS NOT A LAPSE. Every other test in this file
// derives its cases, but a document is not derivable from a spec: the
// parent's own required attributes, the child's, and the nesting are all
// things only an author knows. What IS derivable is whether the table is
// COMPLETE, and that is the guard below.
type wholeLoad struct {
	loads   string
	refused string
	attr    string // the universal that `refused` carries
	// misplaced puts the element somewhere no container names it, still
	// carrying `attr`. The two faults are present at once ON PURPOSE:
	// the question is which one gets reported, and a document with only
	// the placement fault cannot ask it.
	misplaced string
	// onPseudo and onContent are the SAME document twice, with one %s
	// hole for an attribute written `Name="value"`: once on the
	// pseudo-element, once on the content the refusal tells the author to
	// move it to. They exist because "is there a child that could hold a
	// universal" is a STRUCTURAL question and the remedy is a
	// BEHAVIOURAL claim — <Tab>'s content can hold Name and cannot hold
	// Visibility, and no shape in the catalog says so. Raised in review
	// of #486.
	//
	// Empty means the element's refusal never prescribes a destination,
	// which TestTheContentRemedyIsAPlaceThatAccepts asserts rather than
	// assumes.
	onPseudo  string
	onContent string
	// propOnContent is onContent's other syntax: the same destination
	// with the hole in CHILD position, so a remedy that sends a PROPERTY
	// ELEMENT there can be followed in the spelling it names. Behaviors
	// and Resources are the two every element accepts as <X.Foo>, so
	// they are the only names that reach it; the other seven are told to
	// change spelling and land on onContent instead. Raised in review of
	// #486.
	propOnContent string
	// propOn is the same document with one %s hole INSIDE the
	// pseudo-element, for a property element written
	// `<Elem.Attr>v</Elem.Attr>`. It is the other spelling of a
	// universal, and it was accepted and dropped while the attribute
	// spelling was refused — checkProps runs from build() alone, and a
	// pseudo-element never reaches build(). Raised in review of #486.
	propOn string
}

var wholeLoadCases = map[string]wholeLoad{
	"Tab": {
		loads:         `<Gooey><Tabs><Tab Header="a"><Text>x</Text></Tab></Tabs></Gooey>`,
		refused:       `<Gooey><Tabs><Tab Header="a" Name="Zonk"><Text>x</Text></Tab></Tabs></Gooey>`,
		attr:          "Name",
		misplaced:     `<Gooey><VStack><Tab Header="a" Name="Zonk"><Text>x</Text></Tab></VStack></Gooey>`,
		onPseudo:      `<Gooey><Tabs><Tab Header="a" %s><Text>x</Text></Tab></Tabs></Gooey>`,
		onContent:     `<Gooey><Tabs><Tab Header="a"><Text %s>x</Text></Tab></Tabs></Gooey>`,
		propOnContent: `<Gooey><Tabs><Tab Header="a"><Text>%s</Text></Tab></Tabs></Gooey>`,
		propOn:        `<Gooey><Tabs><Tab Header="a">%s<Text>x</Text></Tab></Tabs></Gooey>`,
	},
	"Menu": {
		loads:     `<Gooey><MenuBar><Menu Title="F"><MenuItem Text="Open"/></Menu></MenuBar></Gooey>`,
		refused:   `<Gooey><MenuBar><Menu Title="F" Name="Zonk"><MenuItem Text="Open"/></Menu></MenuBar></Gooey>`,
		attr:      "Name",
		misplaced: `<Gooey><VStack><Menu Title="F" Name="Zonk"><MenuItem Text="Open"/></Menu></VStack></Gooey>`,
		// onPseudo WITHOUT onContent: a <Menu> holds only <MenuItem>,
		// which refuses the same universals, so there is no destination
		// and the refusal must prescribe none. That pairing is a claim in
		// its own right and the loops below read it as one.
		onPseudo: `<Gooey><MenuBar><Menu Title="F" %s><MenuItem Text="Open"/></Menu></MenuBar></Gooey>`,
		propOn:   `<Gooey><MenuBar><Menu Title="F">%s<MenuItem Text="Open"/></Menu></MenuBar></Gooey>`,
	},
	"MenuItem": {
		loads:     `<Gooey><MenuBar><Menu Title="F"><MenuItem Text="Open"/></Menu></MenuBar></Gooey>`,
		refused:   `<Gooey><MenuBar><Menu Title="F"><MenuItem Text="Open" Margin="2"/></Menu></MenuBar></Gooey>`,
		attr:      "Margin",
		misplaced: `<Gooey><VStack><MenuItem Text="Open" Margin="2"/></VStack></Gooey>`,
		// ModeLeaf: there is no content inside at all, so likewise no
		// destination and no remedy.
		onPseudo: `<Gooey><MenuBar><Menu Title="F"><MenuItem Text="Open" %s/></Menu></MenuBar></Gooey>`,
		propOn:   `<Gooey><MenuBar><Menu Title="F"><MenuItem Text="Open">%s</MenuItem></Menu></MenuBar></Gooey>`,
	},
}

// TestEveryPseudoElementIsRefusedThroughAWholeLoad is the WIRING half,
// and it is the arm that catches the defect this file exists for.
//
// The direct-call tests above prove the GATE. They cannot prove the
// CALL: checkAttrs runs inside build(), and #461 was precisely that
// buildTabs never reached it — an assertion on a function is blind to a
// caller that does not call it. The PR's own mutation matrix measured
// that: removing buildTabs' checkAttrs call is caught here and nowhere
// else.
//
// SO COVERAGE OF THIS ARM HAD TO BE DERIVED TOO, and it was not. It
// tested <Tab> by name, so a fourth pseudo-element whose parent's
// builder forgets the call reproduces #461 exactly — with the derived
// arms above green, because they call checkAttrs themselves. That is
// this file's own claim ("a fourth pseudo-element is covered without
// this file being edited") failing where it mattered most. Raised in
// review of #486.
//
// The table is hand-written and its COMPLETENESS is derived: a
// pseudo-element the catalog reports and the table omits is a Fatal, so
// a fourth arrives as a red test naming what to write rather than as
// silence.
func TestEveryPseudoElementIsRefusedThroughAWholeLoad(t *testing.T) {
	specs := pseudoSpecs(t)

	// BOTH DIRECTIONS. An entry for a name that is no longer pseudo is
	// as wrong as a missing one: it would keep asserting a refusal that
	// is no longer correct, and read as coverage while covering nothing.
	pseudo := map[string]bool{}
	for _, s := range specs {
		pseudo[s.Name] = true
		if _, ok := wholeLoadCases[s.Name]; !ok {
			t.Fatalf("the catalog reports <%s> as a pseudo-element and this "+
				"table has no case for it, so nothing checks that its "+
				"parent's builder calls checkAttrs. That is issue #461 one "+
				"element over: the derived tests above would stay green while "+
				"<%s Name=\"x\"> loaded, was dropped, and reported nothing. "+
				"Add markup that loads and markup carrying a universal",
				s.Name, s.Name)
		}
		// EVERY FIELD, not just the row. A row present with an empty
		// misplaced document builds `""`, which errors for a reason
		// that has nothing to do with placement — so
		// TestAMisplacedPseudoElementReportsItsPlacement would range
		// over it and report a pass. The completeness guard has to be
		// as wide as the table is used. Raised in review of #486 round
		// 2, alongside the finding that made misplaced derived at all.
		if wholeLoadCases[s.Name].misplaced == "" {
			t.Fatalf("<%s>'s row carries no `misplaced` document, so nothing "+
				"checks that a <%s> in the wrong container reports its "+
				"PLACEMENT rather than its attribute. Write markup that puts "+
				"it somewhere no container names, still carrying the universal",
				s.Name, s.Name)
		}
	}
	for name := range wholeLoadCases {
		if !pseudo[name] {
			t.Fatalf("this table carries a case for <%s>, which the catalog "+
				"does not report as a pseudo-element. Either it gained a "+
				"Proto — in which case its universals are honoured now and "+
				"the case asserts a refusal that would be a bug — or the name "+
				"is a typo and has been checking nothing", name)
		}
	}

	for _, s := range specs {
		tc := wholeLoadCases[s.Name]
		t.Run(s.Name, func(t *testing.T) {
			// THE MUST-LOAD HALF FIRST. A refusal that fired on
			// everything satisfies every other assertion in this file
			// and breaks every page in the repo; only this notices.
			if _, err := Build([]byte(tc.loads), &Context{}); err != nil {
				t.Errorf("the refusal reached an attribute the parent's "+
					"builder actually reads: %v", err)
			}

			ctx := &Context{}
			_, err := Build([]byte(tc.refused), ctx)
			if err == nil {
				t.Fatalf("<%s %s=…> loaded clean through a whole Build and "+
					"named %d elements — accepted, dropped, and invisible in "+
					"the designer too. The gate is right and its caller does "+
					"not reach it, which is #461 itself",
					s.Name, tc.attr, len(ctx.Named))
			}
			msg := err.Error()
			if !strings.Contains(msg, tc.attr) || !strings.Contains(msg, s.Name) {
				t.Errorf("the error names neither the attribute nor the "+
					"element: %v", err)
			}
			if advertises(msg, tc.attr) {
				t.Errorf("the refusal advertises %s on the element that just "+
					"rejected it:\n\t%s", tc.attr, msg)
			}
		})
	}
}

// TestEveryPseudoElementRefusesAUniversalTheSameWay is finding 2 of
// #486's round 1, and it is the guard that keeps the three from drifting
// back apart.
//
// refuseComponentAttr began inside checkAttrs' !AttrsKnown early return, so
// only <Tab> — the one pseudo-element whose attributes are not
// enumerable — reached it. <Menu> and <MenuItem> fell through to the
// exhaustive check and refused a universal with "no such attribute; this
// element takes Title", the wording refuseComponentAttr's own comment calls a
// lie: the attribute exists everywhere else, and the author's question is
// where to put it instead.
//
// Asserted as a SHARED SHAPE rather than as three literal strings, so
// the message can still be improved without a test edit — what may not
// happen is one element getting the improvement and the others keeping
// the lie.
func TestEveryPseudoElementRefusesAUniversalTheSameWay(t *testing.T) {
	for _, s := range pseudoSpecs(t) {
		tc := wholeLoadCases[s.Name]
		_, err := Build([]byte(tc.refused), &Context{})
		if err == nil {
			t.Errorf("<%s %s=…> was not refused at all", s.Name, tc.attr)
			continue
		}
		msg := err.Error()
		// The mechanism clause is what makes the message better than
		// "no such attribute": it says the element builds nothing, so
		// the reader knows the attribute has no home here rather than
		// believing they mistyped it.
		if !strings.Contains(msg, "builds no component") {
			t.Errorf("<%s> refuses %s without naming the mechanism, so it is "+
				"not sharing the message the other pseudo-elements give. A "+
				"reader is told the attribute does not exist, when it exists "+
				"everywhere else:\n\t%s", s.Name, tc.attr, msg)
		}
	}
}

// TestAnUnenumerableElementThatBuildsOneKeepsItsUniversals is the case
// this tree did not have, and it is here because a mutation went SILENT
// without it.
//
// Swapping refuseComponentAttr's gate from spec.Pseudo to !TakesLayout(spec)
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

// TestTheDesignerOffersNoLayoutRowWhereTheLoaderHonoursOne is the OTHER
// side of the shape above, and it is the direction Grant.AttrsFor's
// comment did not have.
//
// That comment says the grid's gate and the loader's "must agree or the
// grid offers a row that fails to load", and one direction is guarded:
// AttrsFor withholds on !TakesLayout and Context.vocabulary refuses on
// the same predicate, so nothing offered is refused. The converse is not
// guarded and cannot be from here. TakesLayout reads HasLayout, which
// ElementDef.axes derives from the PROTO — so a host def with a real
// Build and no Proto answers false, while build() runs applyLayout on
// whatever its Build returns and the component really does satisfy
// gooey.HasLayout. Measured, both arms:
//
//	Known: true   → <Host Margin="2"> is a load error, and no Margin row.
//	Known: false  → it LOADS with Margin={2 2 2 2}, and still no Margin row.
//
// The second is an attribute the loader honours that the designer cannot
// show — the same class of invisibility #461 was, one gate over, and
// not closable at the catalog layer: without a Proto there is nothing to
// ask, which is what AttrsKnown already says about the element's own
// attributes. So it is stated and pinned rather than fixed, and the pin
// is what keeps the comment honest. Raised in review of #486.
func TestTheDesignerOffersNoLayoutRowWhereTheLoaderHonoursOne(t *testing.T) {
	hostDef := func(known bool) map[string]*ElementDef {
		return map[string]*ElementDef{"LogPane": {
			Name:  "LogPane",
			Known: known,
			Doc:   "A host element with a real Build and no Proto.",
			Attrs: []AttrSpec{{Name: "Title", Kind: KindString, Origin: OriginBuiltin}},
			Build: func(e Element, ctx *Context) (gooey.Component, error) {
				return &components.Text{}, nil
			},
		}}
	}
	offers := func(spec ElementSpec, name string) bool {
		for _, a := range spec.Grants.AttrsFor(spec) {
			if a.Name == name {
				return true
			}
		}
		return false
	}

	// THE GUARDED DIRECTION. An enumerable host def refuses the row at
	// load exactly where the grid withholds it, which is the agreement
	// the comment claims.
	ctx := &Context{Elements: hostDef(true)}
	spec, ok := ctx.spec("LogPane")
	if !ok {
		t.Fatal("the fixture did not resolve")
	}
	if offers(spec, "Margin") {
		t.Error("the grid offers a Margin row for an element whose declared " +
			"surface takes no layout")
	}
	if _, err := Build([]byte(`<Gooey><LogPane Title="t" Margin="2"/></Gooey>`), ctx); err == nil {
		t.Error("an enumerable host def accepted Margin while the grid " +
			"withheld the row, so the two gates no longer agree in the " +
			"direction that IS guarded")
	}

	// AND THE UNGUARDED ONE. Drop AttrsKnown and checkAttrs stands down —
	// the element's own vocabulary is genuinely unknown — but the
	// universal set is not the element's, and applyLayout honours it off
	// the built component's type.
	ctx = &Context{Elements: hostDef(false)}
	spec, ok = ctx.spec("LogPane")
	if !ok {
		t.Fatal("the fixture did not resolve")
	}
	if TakesLayout(spec) {
		t.Fatal("the fixture is not the discriminating shape: a def with no " +
			"Proto reports TakesLayout, so both gates would agree about it")
	}
	root, err := Build([]byte(`<Gooey><LogPane Title="t" Margin="2"/></Gooey>`), ctx)
	if err != nil {
		t.Fatalf("the unenumerable host def now refuses Margin, which would "+
			"close this gap — update the comment on Grant.AttrsFor with it: %v", err)
	}
	var pane gooey.Component
	var walk func(c gooey.Component)
	walk = func(c gooey.Component) {
		if _, isText := c.(*components.Text); isText {
			pane = c
		}
		if cc, isC := c.(gooey.Container); isC {
			for _, k := range cc.ChildComponents() {
				walk(k)
			}
		}
	}
	walk(root)
	if pane == nil {
		t.Fatal("the host element built nothing findable, so the layout it " +
			"was given cannot be read back")
	}
	if m := pane.(gooey.HasLayout).LayoutProps().Margin; m.L != 2 {
		t.Errorf("Margin reached no layout (%+v), so the loader does not in "+
			"fact honour what the grid withholds", m)
	}
	if offers(spec, "Margin") {
		t.Error("the grid now offers the Margin row the loader honours, which " +
			"closes the gap — delete the second half of Grant.AttrsFor's " +
			"comment about only one direction being guarded")
	}
}

// TestAMisplacedPseudoElementReportsItsPlacement is finding 1 of #486's
// round 1, and the defect it pins is a RIGHT ANSWER TO THE WRONG
// QUESTION.
//
// checkAttrs runs before the element's own Build, so a <Tab> carrying a
// universal AND sitting outside <Tabs> got the attribute refusal — which
// advised moving the attribute onto the content inside. Following that
// advice leaves the document exactly as broken, because the actual fault
// is that the <Tab> is in a <VStack>. The diagnosis that would fix it,
// defTab.Build's "<Tab> is only valid directly inside <Tabs>", never
// printed at all.
//
// BOTH ARMS, because either alone passes against the bug. The misplaced
// document must report placement, and the correctly-placed one must
// still report the attribute — a gate that stood down always would
// satisfy the first and silently undo #461.
func TestAMisplacedPseudoElementReportsItsPlacement(t *testing.T) {
	var checked int
	for _, sp := range pseudoSpecs(t) {
		checked++
		tc := wholeLoadCases[sp.Name]

		_, err := Build([]byte(tc.misplaced), &Context{})
		if err == nil {
			t.Errorf("<%s> outside %s loaded clean", sp.Name,
				describeParent(legalParent(t, sp)))
			continue
		}
		if !strings.Contains(err.Error(), "only valid directly inside") {
			t.Errorf("the document's fault is that the <%s> is somewhere it "+
				"cannot be, and the error is about its attribute instead. The "+
				"remedy it prescribes leaves the page just as broken and the "+
				"one that would fix it is never printed:\n\t%v", sp.Name, err)
		}

		_, err = Build([]byte(tc.refused), &Context{})
		if err == nil {
			t.Errorf("standing down on placement also stood down on a "+
				"correctly placed <%s>, which is #461 undone", sp.Name)
			continue
		}
		if !strings.Contains(err.Error(), tc.attr) {
			t.Errorf("a correctly placed <%s> should still be refused %s, and "+
				"this error does not name it: %v", sp.Name, tc.attr, err)
		}
	}
	if checked == 0 {
		t.Fatal("no pseudo-element in the catalog, so this test ranged over " +
			"nothing")
	}
}

// TestARefusalPrescribesOnlyAPlaceThatExists is finding 5's property
// half. The message tells an author to "put it on the content inside",
// which is a real instruction for <Tab> and a move to nowhere for
// <MenuItem>, which is ModeLeaf and holds none.
//
// THE PREDICATE IS ACCEPTANCE, NOT EXISTENCE, and the first version's
// was existence — it filtered on ModeLeaf/ModeNone, so <Menu> (which is
// ModeRestricted) was never looked at. <Menu>'s only legal content is
// <MenuItem>, itself a pseudo-element refusing the identical attribute,
// so the remedy landed the author on a second load error and this test
// was green over it. Raised in review of #486 round 2; a place that
// refuses the attribute is not a place that exists, which is what the
// test is named for.
//
// The PROPERTY is asserted rather than the wording. Pinning the sentence
// is what made TestAPseudoElementRefusesName go red for an improvement,
// and this file should not repeat it one function over.
//
// BOTH DIRECTIONS, and the second one is why <Tab> is no longer skipped.
// This began as "an element with no destination must prescribe none" and
// `continue`d on everything else — so the element that takes
// acceptsAUniversal's default arm, the only ModeUnknown one in the
// catalog, was the one element the test named for the property never
// looked at. The converse is the same claim read the other way: an
// element whose content WOULD accept the attribute and whose refusal
// prescribes nothing withholds working advice, which is the failure
// reservedOnContent exists to make deliberate rather than accidental.
// Raised in review of #486.
func TestARefusalPrescribesOnlyAPlaceThatExists(t *testing.T) {
	var withheld, prescribed int
	for _, sp := range pseudoSpecs(t) {
		tc := wholeLoadCases[sp.Name]
		if tc.onPseudo == "" {
			t.Errorf("<%s> has no onPseudo template, so this can only check the "+
				"one attribute wholeLoadCases happens to spell in `refused` — "+
				"and the remedy is decided PER ATTRIBUTE", sp.Name)
			continue
		}
		// EVERY UNIVERSAL, not the row's one spelling. The remedy is
		// computed per attribute, so checking one of eight leaves seven
		// unread — and reservedOnContent exists precisely because two
		// attributes on one element can want different answers. Raised
		// in review of #486.
		for _, u := range refusableAttrs(t) {
			// PER ATTRIBUTE, because the answer differs by one. The
			// helper was asked once per ELEMENT and its answer reused
			// for all eight names, which is the same shape as the
			// production predicate it was written to check
			// independently — so the test agreed with the bug by
			// construction. Raised in review of #486.
			accepts := acceptsAUniversal(t, sp, u.name)
			attr := fmt.Sprintf("%s=%q", u.name, u.literal)
			_, err := Build([]byte(fmt.Sprintf(tc.onPseudo, attr)), &Context{})
			if err == nil {
				t.Errorf("<%s %s> was not refused at all", sp.Name, attr)
				continue
			}
			offered := strings.Contains(err.Error(), contentRemedy)
			// AN ATTACHED PROPERTY HAS NO DESTINATION HERE, whatever the
			// content mode says. The move is to the content, whose
			// parent is this pseudo-element, and a pseudo-element grants
			// nothing — so accepts, which reads Children.Mode, is
			// answering a question about the wrong attribute class.
			if strings.Contains(u.name, ".") {
				withheld++
				if offered {
					t.Errorf("<%s>'s refusal of the attached property %s tells the "+
						"author to put it on the content inside, whose parent is "+
						"<%s> and contributes nothing — the move is a second load "+
						"error:\n\t%v", sp.Name, u.name, sp.Name, err)
				}
				continue
			}
			if !accepts {
				withheld++
				if offered {
					t.Errorf("nothing <%s> may contain (%s) would accept %s, and its "+
						"refusal tells the author to put the attribute on the content "+
						"inside — a remedy whose destination refuses it too:\n\t%v",
						sp.Name, sp.Children.Mode, u.name, err)
				}
				continue
			}
			if _, reserved := reservedOnContent[sp.Name][u.name]; reserved {
				// A CONSIDERED EXCEPTION, which carries its own sentence
				// instead of the remedy. The other direction of that
				// table — a reservation whose destination would in fact
				// have accepted the attribute — is asserted in
				// TestTheContentRemedyIsAPlaceThatAccepts.
				continue
			}
			prescribed++
			if !offered {
				t.Errorf("<%s>'s content (%s) would accept %s, and its refusal "+
					"prescribes nowhere to put it — the author is told the "+
					"attribute went nowhere and left to guess the destination "+
					"the catalog already knows:\n\t%v",
					sp.Name, sp.Children.Mode, u.name, err)
			}
		}
	}
	if withheld == 0 {
		t.Fatal("every pseudo-element in the catalog can host a universal " +
			"somewhere inside, so the withholding direction ranged over " +
			"nothing. It is not a pass — either the modes moved or the " +
			"predicate is wrong")
	}
	if prescribed == 0 {
		t.Fatal("no pseudo-element in the catalog can host a universal inside " +
			"it, so the prescribing direction ranged over nothing — which is " +
			"how <Tab> came to be skipped by the test named for this property")
	}
}

// acceptsAUniversal reports whether the content inside sp could hold
// this attribute, which is what makes "put it on the content inside" a
// real instruction.
//
// Three answers, and the middle one is the finding. ModeLeaf/ModeNone
// hold nothing. An unrestricted container holds anything, so it holds
// something addressable. ModeRestricted holds a named set — and if every
// name in it is a pseudo-element, the content inside refuses the
// attribute for exactly the reason the outer element did.
//
// THE NAMED SET IS ASKED ABOUT THIS ATTRIBUTE, NOT ABOUT ATTRIBUTES.
// "Builds a component" is not "takes this name": <Timer>, <KeyBinding>,
// <Tooltip>, <Validate>, <TypeAhead>, <ValidationMarker> and
// <Companion> all build one and take Name, and not one of them takes a
// layout row. A pseudo-element restricted to any of those was told to
// move a Margin onto content that refuses it. Raised in review of #486.
//
// DERIVED HERE, NOT BORROWED. The rule is restated from ElementSpec —
// Name where the element builds something, the layout rows where it
// carries a Layout, its own Attrs otherwise — rather than by calling
// ctx.vocabulary, which is the function production asks. A helper that
// calls the implementation cannot disagree with it, and disagreeing is
// this file's job.
//
// THE DEFAULT ARM IS ModeUnknown AND IT IS <Tab>'S, which is the arm
// this helper was written with no sentence on — mirroring pseudoRemedy's
// own undocumented default. An opaque element's content is UNKNOWN, not
// absent: <Tabs> builds a <Tab>'s children as a page, so they are
// ordinary components and a universal lands on them. Answering "true"
// here is therefore a claim, not a fallthrough, and the caller now
// asserts BOTH directions of it rather than skipping the element that
// takes this arm. Raised in review of #486.
func acceptsAUniversal(t *testing.T, sp ElementSpec, name string) bool {
	t.Helper()
	switch sp.Children.Mode {
	case ModeLeaf, ModeNone:
		return false
	case ModeRestricted:
		for _, n := range sp.Children.Only {
			s, ok := (&Context{}).spec(n)
			if !ok {
				return true
			}
			if takesThere(s, name) {
				return true
			}
		}
		return false
	}
	return true
}

// takesThere is the acceptance rule restated over ElementSpec: a
// pseudo-element takes nothing, every element that builds one takes
// Name and the two universal property elements, an element with a
// Layout takes the layout rows, and anything else has to be in its own
// declared Attrs.
func takesThere(s ElementSpec, name string) bool {
	if s.Pseudo {
		return false
	}
	if name == "Name" || name == "Behaviors" || name == "Resources" {
		return true
	}
	if isUniversalAttr(name) {
		return TakesLayout(s)
	}
	for _, a := range s.Attrs {
		if a.Name == name {
			return true
		}
	}
	return false
}

// tagRe matches one tag as a document spells it: an optional slash, a
// name, and whatever the tag carries before its closing bracket.
var tagRe = regexp.MustCompile(`<(/?)([A-Za-z][A-Za-z0-9.]*)([^>]*)>`)

// enclosingTag is the element a document opens immediately around the
// first <name in it, read from the source text and from nothing else.
//
// It is what makes the reader assertion independent: every other way of
// answering "who reads this element" in this package is a loop over the
// catalog, and a test that runs one of those is comparing the
// implementation against a copy of itself.
func enclosingTag(t *testing.T, doc, name string) string {
	t.Helper()
	var stack []string
	for _, m := range tagRe.FindAllStringSubmatch(doc, -1) {
		tag, rest := m[2], strings.TrimSpace(m[3])
		if m[1] == "/" {
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			continue
		}
		if tag == name {
			if len(stack) == 0 {
				t.Fatalf("<%s> is the root of %q, so nothing encloses it", name, doc)
			}
			return stack[len(stack)-1]
		}
		if !strings.HasSuffix(rest, "/") {
			stack = append(stack, tag)
		}
	}
	t.Fatalf("no <%s> in %q, so this fixture cannot say who reads it", name, doc)
	return ""
}

// TestARefusalNamesTheReaderWhenTheCatalogKnowsIt is finding 6's half:
// ElementSpec now carries ParsedBy, so the message can say WHO consumed
// the element rather than "its parent".
//
// EVERY READER THE CATALOG CAN NAME, not only those carrying ParsedBy,
// and the narrower filter is what let <Tab> slip. defTab carries Opaque
// instead of ParsedBy — for a reason readsAsData records — so the
// element this whole branch exists for was the one element this test
// skipped, and it was getting the generic "its parent reads it as data"
// clause. Raised in review of #486 round 2.
//
// THE EXPECTATION IS THE DOCUMENT'S OWN TAG, which is the third answer
// this test has had and the first that is not a copy of an
// implementation. It read sp.ParsedBy first — the very field the message
// splices in, so for every element carrying one the assertion was "the
// message contains the string the message was built from" and could not
// see a wrong value; <MenuItem>'s was wrong ("MenuBar", where the
// element's own placement error says <Menu>) and this test was green
// over it. It then read legalParent, which is namingParent's first-match
// loop re-typed, so it agreed with the search rather than with the
// answer — including on the search's alphabetical bias.
//
// enclosingTag reads the fixture's SOURCE TEXT: the tag the document
// opens immediately around this element. No catalog, no spec, no second
// copy of anybody's loop — and it is the reader by definition, since a
// pseudo-element is consumed by whatever encloses it. Raised in review
// of #486.
func TestARefusalNamesTheReaderWhenTheCatalogKnowsIt(t *testing.T) {
	var checked int
	for _, sp := range pseudoSpecs(t) {
		want := enclosingTag(t, wholeLoadCases[sp.Name].refused, sp.Name)
		checked++
		tc := wholeLoadCases[sp.Name]
		_, err := Build([]byte(tc.refused), &Context{})
		if err == nil {
			t.Errorf("<%s %s=…> was not refused at all", sp.Name, tc.attr)
			continue
		}
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the catalog says <%s> is read by <%s>, and the refusal "+
				"does not name it — so the author is told the attribute went "+
				"nowhere without being told where to look:\n\t%v",
				sp.Name, want, err)
		}
	}
	if checked == 0 {
		t.Fatal("no pseudo-element in the catalog, so this test ranged over " +
			"nothing")
	}
}

// elementTagRe matches an element name as a load error spells it, which
// is an opening angle bracket and a capitalised identifier. Attribute
// values are quoted and never reach it.
var elementTagRe = regexp.MustCompile(`<([A-Z][A-Za-z0-9]*)`)

// elementsNamed is every <Element> a message mentions except the subject
// itself — the containers it points the author at.
//
// Over the MESSAGE rather than over the catalog, because the claim below
// is about what two sentences say and not about what either was built
// from. A derivation from ParsedBy or from Children.Only would agree
// with whichever of the two it was derived from and see nothing.
func elementsNamed(msg, except string) []string {
	seen := map[string]bool{}
	for _, m := range elementTagRe.FindAllStringSubmatch(msg, -1) {
		if m[1] != except {
			seen[m[1]] = true
		}
	}
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// TestAPseudoElementNamesOneContainerInBothOfItsLoadErrors is the cross
// check neither message could make about itself.
//
// A pseudo-element has two refusals an author can hit, and they are
// reached from opposite mistakes: carry a universal in the RIGHT
// container and the attribute is refused with a reason clause naming who
// reads the element; put the element in the WRONG container and its own
// Build refuses the placement, naming where it belongs. Both were
// derived from a different field — the reason clause from ParsedBy, the
// placement from a literal in the def — so nothing compared them, and
// <MenuItem> named <MenuBar> in one and <Menu> in the other. An author
// who followed the first landed on the second. Measured before the fix,
// both messages, and it is the only instrument that can see it: every
// guard over either sentence alone reads the field that sentence was
// built from. Raised in review of #486.
func TestAPseudoElementNamesOneContainerInBothOfItsLoadErrors(t *testing.T) {
	var checked int
	for _, sp := range pseudoSpecs(t) {
		tc := wholeLoadCases[sp.Name]
		_, refused := Build([]byte(tc.refused), &Context{})
		_, misplaced := Build([]byte(tc.misplaced), &Context{})
		if refused == nil || misplaced == nil {
			t.Errorf("<%s>: one of its two documents loaded, so there is no pair "+
				"to compare (refused=%v misplaced=%v)", sp.Name, refused, misplaced)
			continue
		}
		checked++
		inRefusal := elementsNamed(refused.Error(), sp.Name)
		inPlacement := elementsNamed(misplaced.Error(), sp.Name)
		if len(inRefusal) == 0 || len(inPlacement) == 0 {
			t.Errorf("<%s>: one of its two refusals names no container at all, so "+
				"the author is told something went wrong and not where to look:\n"+
				"\tattribute: %v\n\tplacement: %v", sp.Name, refused, misplaced)
			continue
		}
		if strings.Join(inRefusal, ",") != strings.Join(inPlacement, ",") {
			t.Errorf("<%s> names %v when its attribute is refused and %v when its "+
				"placement is, so the two errors send the author to different "+
				"containers — and following the first is how the second is "+
				"reached:\n\tattribute: %v\n\tplacement: %v",
				sp.Name, inRefusal, inPlacement, refused, misplaced)
		}
	}
	if checked == 0 {
		t.Fatal("no pseudo-element produced both refusals, so this test ranged " +
			"over nothing")
	}
}

// refusable is one attribute spelling that reaches a pseudo-element's
// refusal, with a literal its own grammar accepts.
type refusable struct{ name, literal string }

// refusableAttrs is every such spelling: the universals, and — since
// cannotApplyTo widened to "a dot and nothing else" — the attached
// properties.
//
// THE TWO SETS ANSWER THE REMEDY QUESTION DIFFERENTLY, which is why
// ranging over the first alone is not a smaller version of the right
// test but a test of the wrong population. A universal is carried by
// every component, so "put it on the content inside" names a
// destination that accepts it. An attached property is an instruction
// to a particular PARENT, and the content of a pseudo-element has the
// pseudo-element for a parent, which grants nothing — so the same
// sentence walks the author into a second load error. Both remedy tests
// below were written over universalAttrs and stayed green through the
// widening that made the second population reachable. Raised in review
// of #486.
func refusableAttrs(t *testing.T) []refusable {
	t.Helper()
	var out []refusable
	for _, u := range universalAttrs {
		// ASKED OF <Border>, not of the pseudo-element: see the note in
		// TestTheContentRemedyIsAPlaceThatAccepts.
		out = append(out, refusable{u.Name, validLiteralFor(t, "Border", u)})
	}
	for _, parent := range AttachedParents() {
		for _, a := range AttachedAttrs(parent) {
			out = append(out, refusable{a.Name, validLiteralFor(t, parent, a)})
		}
	}
	if len(out) == len(universalAttrs) {
		t.Fatal("the catalog declares no attached property, so every caller of " +
			"this helper ranges over universals only — which is the state the " +
			"attached remedy shipped wrong in")
	}
	return out
}

// TestTheContentRemedyIsAPlaceThatAccepts asks the remedy's question
// BEHAVIOURALLY, which is the half TestARefusalPrescribesOnlyAPlaceThatExists
// cannot reach.
//
// That test asks whether the content COULD hold a universal, from
// Children.Mode. <Tab>'s mode is unrestricted, so it answers yes and the
// element is skipped — and the answer is right in general and wrong for
// exactly one attribute. `<Tab Visibility="Hidden">` is refused with
// "put it on the content inside instead", and doing that hits
// buildTabs' own refusal:
//
//	markup: <Tab Header="a">: a tab page cannot bind its own
//	Visibility — the Tabs owns it
//
// So the author is walked from one load error to another, by advice. A
// structural question cannot see that, because nothing in the catalog
// says a <Tabs> reserves its pages' Visibility; only running the remedy
// does.
//
// EVERY UNIVERSAL, not the one the table happens to carry: the defect is
// per-attribute, so ranging over `attr` alone would have missed it in
// the same way. And the remedy is only asked of elements whose refusal
// actually offers one — an element that prescribes nothing is correct
// here and is counted, so an empty range is a Fatal rather than a pass.
// Raised in review of #486.
func TestTheContentRemedyIsAPlaceThatAccepts(t *testing.T) {
	var prescribed int
	for _, sp := range pseudoSpecs(t) {
		tc := wholeLoadCases[sp.Name]
		if tc.onPseudo == "" {
			t.Errorf("<%s> has no onPseudo template, so the loop below would "+
				"rebuild one fixed document eight times and read the loop "+
				"variable only in its own failure message", sp.Name)
			continue
		}
		for _, u := range refusableAttrs(t) {
			// ASKED OF <Border>, not of the pseudo-element. Kind alone
			// answers "x" for Margin, whose Kind is KindString and whose
			// grammar is one, two or four whole numbers — so the generic
			// value made the destination refuse for a reason that has
			// nothing to do with the remedy. narrowerThanItsKind already
			// records that fact under "Border.Margin"; borrowing it is
			// cheaper and better guarded than a second table here, and a
			// pseudo-element declares nothing for such a row to key on.
			// An attached property is asked of its GRANTING parent for
			// the same reason. See refusableAttrs.
			attr := fmt.Sprintf("%s=%q", u.name, u.literal)
			_, err := Build([]byte(fmt.Sprintf(tc.onPseudo, attr)), &Context{})
			if err == nil {
				t.Errorf("<%s %s> loaded; a universal on a pseudo-element is a load error",
					sp.Name, attr)
				continue
			}
			if !strings.Contains(err.Error(), contentRemedy) {
				continue // no destination prescribed, nothing to check
			}
			prescribed++
			if tc.onContent == "" {
				// A ROW WITH NO DESTINATION TEMPLATE IS A CLAIM that
				// this element never prescribes one. Prescribing anyway
				// is the failure, and it used to be unreachable: the
				// arm that checked it rebuilt `refused` per attribute
				// and so could only ever see the one the row spells.
				t.Errorf("<%s> prescribes the content remedy for %s and this table "+
					"has no onContent template for it, so nothing checks that the "+
					"destination accepts it:\n\t%v", sp.Name, u.name, err)
				continue
			}
			if _, err := Build([]byte(fmt.Sprintf(tc.onContent, attr)), &Context{}); err != nil {
				t.Errorf("<%s %s> is refused with \"put it on the content inside instead\", "+
					"and the content refuses it too:\n\t%v\nThe author is walked from one "+
					"load error to another by the advice. Either the remedy has to withhold "+
					"this attribute or the destination has to accept it.", sp.Name, attr, err)
			}
		}
	}
	if prescribed == 0 {
		t.Fatal("no pseudo-element prescribed the content remedy for any universal, " +
			"so this test ranged over nothing. It is not a pass — either " +
			"pseudoRemedy stopped offering it or the templates are wrong")
	}

	// AND THE OTHER DIRECTION: a reservedOnContent row withholds a
	// remedy, so a row whose destination would in fact have accepted the
	// attribute is worse than no row — it denies the author working
	// advice and reads like a considered exception. Without this, the
	// whole table could be deleted from the map and replaced with "never
	// prescribe anything" and nothing would notice.
	for elem, attrs := range reservedOnContent {
		tc, ok := wholeLoadCases[elem]
		if !ok || tc.onContent == "" {
			t.Errorf("reservedOnContent names <%s>, which this table cannot place "+
				"on any content, so the row is unchecked", elem)
			continue
		}
		for name := range attrs {
			u, ok := universalByName(name)
			if !ok {
				t.Errorf("reservedOnContent[%q] names %q, which is not a universal "+
					"attribute — the row cannot fire", elem, name)
				continue
			}
			attr := fmt.Sprintf("%s=%q", name, validLiteralFor(t, "Border", u))
			if _, err := Build([]byte(fmt.Sprintf(tc.onContent, attr)), &Context{}); err == nil {
				t.Errorf("reservedOnContent says <%s>'s content cannot take %s, and "+
					"<%s %s> on the content loads. The row withholds a remedy that "+
					"works.", elem, name, elem, attr)
			}
		}
	}
}

// universalByName is the AttrSpec for a universal, or false.
func universalByName(name string) (AttrSpec, bool) {
	for _, u := range universalAttrs {
		if u.Name == name {
			return u, true
		}
	}
	return AttrSpec{}, false
}

// nonUniversalProp is a property-element name no element in the catalog
// declares, which is what makes it the discriminating one: it is the
// shape refusePropElement refuses and refuseComponentAttr never sees.
const nonUniversalProp = "Frobnicate"

// TestEveryPseudoElementRefusesAPropertyElement is the other spelling of
// the rule this file is about, and it was the one nothing checked.
//
// `<Tab Name="Zonk">` was refused and `<Tab.Name>Zonk</Tab.Name>` was
// accepted, silently dropped, and reported nothing — the exact defect
// #461 is filed for, reached by the other syntax. The cause is a missing
// CALL rather than a missing rule: checkProps runs from build(), and a
// pseudo-element never reaches build() because its parent's builder
// consumes it as data.
//
// IT ASSERTS THE SHARED SENTENCE, not merely a refusal. Two dialects of
// the same rule is how the wording drifts, and refuseComponentAttr's comment
// already calls "no such attribute" a lie for these elements — a
// property-element refusal that said something else would reintroduce
// exactly that.
//
// Behaviors and Resources are in the range DELIBERATELY, even though
// checkProps exempts them everywhere else. That exemption is earned by
// build(): buildChildren consumes <X.Behaviors> and pushResources
// consumes <X.Resources>, both from the function a pseudo-element does
// not go through. Had the fix simply called checkProps, those two would
// have been the one pair left silent. Raised in review of #486.
func TestEveryPseudoElementRefusesAPropertyElement(t *testing.T) {
	for _, sp := range pseudoSpecs(t) {
		tc := wholeLoadCases[sp.Name]
		if tc.propOn == "" {
			t.Errorf("the catalog reports <%s> as a pseudo-element and this table "+
				"has no propOn document for it, so nothing checks that a "+
				"<%s.Name> is refused rather than dropped", sp.Name, sp.Name)
			continue
		}
		// Behaviors and Resources first, so a fix that called checkProps
		// and inherited its exemptions fails on the first two names
		// rather than somewhere in the middle of the list.
		// AND ONE NAME THE CATALOG ANSWERS NOTHING ABOUT. The range was
		// ["Behaviors", "Resources"] + universalAttrs, which is exactly
		// the set where refusePropElement and refuseComponentAttr AGREE —
		// so the one set the property-element rule refuses BEYOND the
		// attribute rule was the one set nothing exercised, and the
		// remedy walked an author from <Tab.Frobnicate> to a <Text
		// Frobnicate="…"> that is itself a load error. Raised in review
		// of #486.
		names := []string{"Behaviors", "Resources", nonUniversalProp}
		for _, u := range universalAttrs {
			names = append(names, u.Name)
		}
		for _, name := range names {
			prop := fmt.Sprintf("<%s.%s>x</%s.%s>", sp.Name, name, sp.Name, name)
			_, err := Build([]byte(fmt.Sprintf(tc.propOn, prop)), &Context{})
			if err == nil {
				t.Errorf("%s loaded: the property-element spelling is accepted and "+
					"dropped while the attribute spelling is refused, which is the "+
					"defect #461 is about reached by the other syntax", prop)
				continue
			}
			if !strings.Contains(err.Error(), "builds no component") {
				t.Errorf("%s is refused without the shared sentence, so the two "+
					"spellings have drifted into two dialects of one rule:\n\t%v",
					prop, err)
			}
			if name == nonUniversalProp {
				// THE CONTENT REMEDY IS WITHHELD, and asserted rather
				// than assumed: acceptance at the destination is only
				// derivable for a universal, so prescribing the move
				// here is advice nothing checked.
				if strings.Contains(err.Error(), contentRemedy) {
					t.Errorf("%s prescribes the content move for a name outside "+
						"universalAttrs, where nothing can say the destination "+
						"accepts it — and <Text %s=\"…\"> is itself a load "+
						"error:\n\t%v", prop, name, err)
				}
				// AND SOMETHING IS SAID ANYWAY. Withholding the content
				// move left SILENCE, which is how <Tab.Header> came to
				// be refused with "builds no component for Header to
				// apply to" and no advice — about the one attribute a
				// <Tab> genuinely takes, and a required one. The
				// attribute-on-this-element form promises nothing about
				// acceptance, so it is sayable where the content move is
				// not: if the element does not take the name, the
				// attribute gate answers with its own list rather than a
				// second blank refusal — EXCEPT on a spec whose
				// AttrsKnown is false, where checkAttrs returns before
				// that gate and the name is accepted and dropped. <Tab>
				// is the one such pseudo-element, the remedy carries the
				// caveat for it, and
				// TestARemedyOnAnUncheckedSurfaceSaysTheNameCanBeDropped
				// is what holds that. Raised in review of #486, and
				// corrected there in the round after.
				if !strings.Contains(err.Error(), "write it as an attribute on this element") {
					t.Errorf("%s is refused with no remedy at all. The content move "+
						"is rightly withheld here, but the attribute spelling on "+
						"this same element asserts nothing about a destination and "+
						"is the only form that could work:\n\t%v", prop, err)
				}
				continue
			}
			if !strings.Contains(err.Error(), contentRemedy) {
				continue // no destination prescribed, nothing to follow
			}
			// AND THE REMEDY, FOLLOWED — in the spelling it names. The
			// property-element refusal borrowed the attribute remedy
			// verbatim for its whole first round, so seven of these nine
			// told an author to "put it on the content inside" and an
			// author who moved <Tab.Name> to <Text.Name> hit checkProps'
			// refusal instead: propElements lists Name for nothing, and
			// the two property elements every element does accept are
			// Behaviors and Resources. Raised in review of #486.
			if name != "Behaviors" && name != "Resources" {
				if !strings.Contains(err.Error(), "written as an attribute") {
					t.Errorf("%s is refused with \"put it on the content inside\" and "+
						"nothing about the spelling, so the author moves the property "+
						"element and meets checkProps' refusal instead:\n\t%v", prop, err)
					continue
				}
			}
			if tc.onContent == "" {
				t.Errorf("%s prescribes the content remedy and this table has no "+
					"onContent template, so nothing checks that the destination "+
					"accepts it:\n\t%v", prop, err)
				continue
			}
			// THE DESTINATION IN THE SPELLING THE REMEDY NAMES. For
			// Behaviors and Resources that is still a property element,
			// so it goes in child position; for the other seven the
			// remedy says "written as an attribute" and it goes in the
			// attribute hole.
			doc, dest := tc.onContent, prop
			if name == "Behaviors" || name == "Resources" {
				if tc.propOnContent == "" {
					t.Errorf("%s prescribes the content remedy and this table has no "+
						"propOnContent template, so the one spelling that carries over "+
						"unchanged is unchecked:\n\t%v", prop, err)
					continue
				}
				doc = tc.propOnContent
				dest = fmt.Sprintf("<Text.%s/>", name)
			} else {
				u, ok := universalByName(name)
				if !ok {
					t.Errorf("%s prescribes the attribute spelling for %q, which is "+
						"not a universal attribute — there is nothing for the author "+
						"to write", prop, name)
					continue
				}
				dest = fmt.Sprintf("%s=%q", name, validLiteralFor(t, "Border", u))
			}
			if _, err := Build([]byte(fmt.Sprintf(doc, dest)), &Context{}); err != nil {
				t.Errorf("%s is refused with a remedy that lands on %q, and the "+
					"content refuses that too:\n\t%v", prop, dest, err)
			}
		}
	}
}

// TestAPseudoElementRefusesAnAttachedProperty is the HIGH half of the
// same defect the universals arm closes, and the reason it was missed is
// that universalAttrs looked like the whole set.
//
// The argument this file rests on is that a pseudo-element builds no
// component, so an attribute meant for a component cannot apply. Grid.Row
// is an instruction to the CONTAINER about a component, and there is
// none — so it is refusable for exactly the stated reason, and it was
// loading, being dropped, and reporting nothing. Measured on the branch
// before the widening: <Tab Name="Zonk"> refused, <Tab Grid.Row="1">
// accepted. Raised in review of #486.
//
// DERIVED FROM THE ATTACHED VOCABULARY, not from a written pair. An
// attached name is Owner.Property, and the catalog is what says which
// owners exist — so a container declaring a new attached property joins
// this loop on the commit that declares it, which is the property every
// derived test in this file is built on.
func TestAPseudoElementRefusesAnAttachedProperty(t *testing.T) {
	attached := attachedNames(t)
	if len(attached) == 0 {
		t.Fatal("the catalog declares no attached property at all, so this test " +
			"ranges over nothing — suspect the derivation before believing the " +
			"vocabulary lost Grid.Row")
	}
	for _, sp := range pseudoSpecs(t) {
		tc := wholeLoadCases[sp.Name]
		if tc.onPseudo == "" {
			t.Errorf("<%s> has no onPseudo document, so nothing puts an attached "+
				"property on it", sp.Name)
			continue
		}
		for _, name := range attached {
			doc := fmt.Sprintf(tc.onPseudo, fmt.Sprintf("%s=%q", name, "1"))
			_, err := Build([]byte(doc), &Context{})
			if err == nil {
				t.Errorf("<%s %s=\"1\"> loaded: an attached property is an "+
					"instruction to the container about a component, and <%s> "+
					"builds none — so it is dropped in silence, which is #461 "+
					"reached by the attached spelling", sp.Name, name, sp.Name)
				continue
			}
			// THE SHARED SENTENCE, for the reason the property-element
			// test gives: two dialects of one rule is how the wording
			// drifts apart, and this arm is the newest dialect.
			//
			// THE TAIL IS ASSERTED NEXT DOOR, deliberately and not by
			// omission: the remedy an attached refusal may carry is
			// TestARefusalPrescribesOnlyAPlaceThatExists's attached arm,
			// which reaches every pseudo-element and every attached name
			// from one loop. Repeating it here would be a second copy of
			// one claim, and review of #486 found this test cited as
			// covering a tail it does not read — so the citation lives
			// here instead of the assertion.
			if !strings.Contains(err.Error(), "builds no component") {
				t.Errorf("<%s %s=\"1\"> is refused without the shared sentence:\n\t%v",
					sp.Name, name, err)
			}
		}

		// AND A NAMESPACE DECLARATION STAYS LEGAL, which is a claim
		// about the RULE and not about the predicate. cannotApplyTo asks
		// for a dot and xmlns:h carries a colon, so nothing here is what
		// keeps this loading — the first version of the guard carried an
		// xmlns exclusion on the theory that it was, and a mutation
		// showed removing it changed nothing at all. The pin is worth
		// having anyway: docs/markup-reference.md says a prefixed
		// declaration may sit on any element, and a future guard that
		// reached for the colon would break that silently. Raised in
		// review of #486.
		ns := fmt.Sprintf(tc.onPseudo, `xmlns:h="wonderforge.io/handlers/test"`)
		if _, err := Build([]byte(ns), &Context{}); err != nil {
			t.Errorf("<%s xmlns:h=…> is refused:\n\t%v\nA namespace declaration is "+
				"document structure rather than a property of anything, and "+
				"docs/markup-reference.md says one may sit on any element", sp.Name, err)
		}
	}
}

// attachedNames is every Owner.Property the vocabulary declares.
//
// AttachedParents/AttachedAttrs, not the catalog's per-element Attrs:
// the attached tables belong to NO element, which is the one-table blind
// spot bindsweep_test.go's sweepTargets was written for — a walk over
// spec.Attrs finds nothing with a dot in it and the loop above would
// range over an empty set, green. Reading the tables means an attached
// property added tomorrow joins on the commit that declares it.
func attachedNames(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, parent := range AttachedParents() {
		for _, a := range AttachedAttrs(parent) {
			out = append(out, a.Name)
		}
	}
	sort.Strings(out)
	return out
}

// TestAnUnknownAttributeReachesAKnownPseudoSurfaceAndNotAnOpaqueOne
// records the asymmetry the attached-property fix deliberately did not
// close, so that it is a measured state rather than an oversight.
//
// A pseudo-element declaring its attributes is covered by the ORDINARY
// unknown-attribute gate — <Menu Frobnicate="1"> is refused with "no
// such attribute; this element takes Title", and has been all along.
// That is why refuseComponentAttr does not widen to spec.Attrs: doing so
// would change no acceptance and would replace a better message with a
// worse one, since "no such attribute" is only a lie for an attribute
// that exists elsewhere.
//
// An OPAQUE one is not covered and cannot be. <Tab>'s AttrsKnown is
// false and its Attrs are empty because buildTabs reads "Header" off its
// children without declaring it, so nothing can tell Frobnicate from
// Header. #461 is where making that surface enumerable is tracked, and
// the day it lands this test's opaque arm is what goes red.
//
// The split is read from AttrsKnown rather than written down, so a <Tab>
// that gains a declared surface moves arms by itself. Raised in review
// of #486.
func TestAnUnknownAttributeReachesAKnownPseudoSurfaceAndNotAnOpaqueOne(t *testing.T) {
	var known, opaque int
	for _, sp := range pseudoSpecs(t) {
		tc := wholeLoadCases[sp.Name]
		if tc.onPseudo == "" {
			continue
		}
		doc := fmt.Sprintf(tc.onPseudo, fmt.Sprintf("%s=%q", nonUniversalProp, "1"))
		_, err := Build([]byte(doc), &Context{})
		if sp.AttrsKnown {
			known++
			if err == nil {
				t.Errorf("<%s> declares its attributes and %s is not among them, "+
					"yet it loaded — so the ordinary unknown-attribute gate is not "+
					"reaching a pseudo-element after all, and the attached-property "+
					"rule is covering less than this test assumes", sp.Name,
					nonUniversalProp)
			} else if !strings.Contains(err.Error(), "no such attribute") {
				t.Errorf("<%s %s=\"1\"> is refused by something other than the "+
					"unknown-attribute gate:\n\t%v\nIf refuseComponentAttr widened to "+
					"spec.Attrs, note that it changes no acceptance here and costs "+
					"the better message", sp.Name, nonUniversalProp, err)
			}
			continue
		}
		opaque++
		if err != nil {
			t.Errorf("<%s> is opaque — its surface is not enumerable, so %s cannot "+
				"be told from the attribute its parent's Build really reads — yet "+
				"it was refused:\n\t%v\nIf <%s> gained a declared surface (#461), "+
				"delete this arm rather than relaxing it", sp.Name, nonUniversalProp,
				err, sp.Name)
		}
	}
	// BOTH ARMS REACHED. One pseudo-element of each kind is what makes
	// this a discrimination rather than a restatement; with either at
	// zero the other arm is passing vacuously.
	if known == 0 || opaque == 0 {
		t.Errorf("the vocabulary holds %d pseudo-elements with a declared surface "+
			"and %d opaque ones; with either at zero this test asserts one rule "+
			"and reports the other as covered", known, opaque)
	}
}

// TestTheParsedByFallbackNamesAHostRegisteredReader is the arm that
// makes readsAsData's second branch live, and it is here because review
// of #486 read it as dead code.
//
// It is not dead — it is unreachable from the BUILTIN vocabulary, which
// is a different thing and is exactly why nothing covered it. Every
// builtin pseudo-element is named by some ModeRestricted container, so
// namingParent answers first and the ParsedBy clause never runs; a
// reader checking the branch against the builtins alone finds no input
// that reaches it and concludes the field has no live consumer.
//
// A HOST REGISTRATION IS THE INPUT. ctx.Elements takes an ElementDef
// carrying ParsedBy, elementdef.go derives Pseudo from it (Proto == nil
// && ParsedBy != ""), and nothing requires the named reader to declare
// the element as a child — buildMenuBar walks both levels of the builtin
// pair without any Children.Only saying so, which is the shape ParsedBy
// models. So the fallback answers for precisely the registration the
// catalog cannot describe from the other side, and deleting it would
// send such a host's users the generic "its parent reads it as data"
// instead of the name they registered. Raised in review of #486.
func TestTheParsedByFallbackNamesAHostRegisteredReader(t *testing.T) {
	ctx := &Context{Elements: map[string]*ElementDef{
		"Widget": {Name: "Widget", ParsedBy: "Host", Known: true},
		"Host":   {Name: "Host", Known: true},
	}}
	sp, ok := ctx.spec("Widget")
	if !ok {
		t.Fatal("the host's <Widget> does not resolve, so this test measures nothing")
	}
	// NON-VACUITY BOTH WAYS. The branch runs only for a pseudo-element
	// no container names; if either half stopped holding, the assertion
	// below would be about namingParent's answer instead.
	if !sp.Pseudo {
		t.Fatalf("<Widget> is not a pseudo-element (ParsedBy=%q, Proto nil), so the "+
			"refusal path this fallback serves is never entered", sp.ParsedBy)
	}
	if p := namingParent("Widget", ctx); p != "" {
		t.Fatalf("a container (<%s>) names <Widget>, so namingParent answers first "+
			"and the ParsedBy clause is not what produced the string below", p)
	}
	// AN ELEMENT WITH NO PARENT STAMPED, which is the third non-vacuity
	// half and the one the parent-first branch added: readsAsData now
	// answers from e.parent when the document has one, so a fixture
	// carrying a parent would be measuring that branch instead.
	got := readsAsData(Element{Name: "Widget"}, sp, ctx)
	if !strings.Contains(got, "<Host>") {
		t.Errorf("readsAsData says %q for a host-registered pseudo-element whose "+
			"ParsedBy is \"Host\". Without this branch it falls to the generic "+
			"\"its parent reads it as data\", and the host's users lose the one "+
			"name that says WHO consumed their element", got)
	}
}

// TestTheReaderIsTheDocumentsParentAndNotTheAlphabetsFirst is the
// discriminating case for which source readsAsData answers from. Raised
// in review of #486.
//
// namingParent returns the FIRST catalog element naming the child, and
// definedElements sorts by name, so "first" is alphabetical. Nothing in
// the builtin vocabulary makes that visible — <Menu> is the only
// container naming <MenuItem> — so the search agreed with the parent by
// luck, and every test over the builtins agreed with the search.
//
// A host registration supplies the second namer. <AContextMenu> sorts
// before <Menu> and names <MenuItem>; the document still puts the
// <MenuItem> inside a <Menu>. If the message came from the search it
// would now name a container this document does not contain, while
// defMenuItem.Build goes on saying "only valid directly inside <Menu>" —
// two containers in two errors about one element, which is the
// divergence round 2's finding 6 was filed to remove.
func TestTheReaderIsTheDocumentsParentAndNotTheAlphabetsFirst(t *testing.T) {
	ctx := &Context{Elements: map[string]*ElementDef{
		"AContextMenu": {
			Name:     "AContextMenu",
			Known:    true,
			Children: ChildSpec{Mode: ModeRestricted, Only: []string{"MenuItem"}},
		},
	}}
	// NON-VACUITY: the decoy has to be what the search would answer, or
	// this test passes without discriminating anything.
	if p := namingParent("MenuItem", ctx); p != "AContextMenu" {
		t.Fatalf("the catalog search answers <%s> for <MenuItem>, not the decoy — "+
			"either the sort is not by name or the registration did not take, "+
			"and either way this test is not measuring the two sources apart", p)
	}

	_, err := Build([]byte(wholeLoadCases["MenuItem"].refused), ctx)
	if err == nil {
		t.Fatal("the refusal fixture loaded, so there is no message to read")
	}
	if !strings.Contains(err.Error(), "<Menu> reads <MenuItem> as data") {
		t.Errorf("the refusal names a reader the document does not contain:\n\t%v\n"+
			"The <MenuItem> is inside a <Menu>; <AContextMenu> is merely the "+
			"alphabetically first element naming it. defMenuItem.Build says "+
			"\"only valid directly inside <Menu>\", so an author who reads both "+
			"is told about two containers for one element.", err)
	}
}

// TestTheParserStampsAParentOnEveryElement is the reachability claim
// readsAsData's comment makes, measured instead of asserted.
//
// That comment says everything after the parent branch is dead for any
// document a user can write, and the argument is that misplaced has
// already established the parent branch's own condition. The load
// bearing half is this: the parser stamps `parent` on every element it
// produces except the root, and the parser is the only constructor of an
// Element in this package. If a shape ever slipped through unstamped,
// the refusal would take the catalog search instead — which sorts by
// name, so it can name an element the document is not inside, which is
// the divergence round 2 removed.
//
// A DOCUMENT, NOT A CONSTRUCTED TREE, for the same reason: a fixture
// that builds Elements by hand would be asserting about its own
// construction. Raised in review of #486.
func TestTheParserStampsAParentOnEveryElement(t *testing.T) {
	const doc = `<Gooey>
	  <Stack>
	    <Tabs><Tab Header="A"><Text>x</Text></Tab></Tabs>
	    <MenuBar><Menu Title="F"><MenuItem Header="a"/></Menu></MenuBar>
	    <Grid><Grid.Resources/><Text Grid.Row="0">y</Text></Grid>
	  </Stack>
	</Gooey>`
	root, _, err := parse([]byte(doc))
	if err != nil {
		t.Fatalf("the fixture does not parse: %v", err)
	}
	var seen, unstamped int
	var walk func(e *Element, depth int)
	walk = func(e *Element, depth int) {
		seen++
		if depth > 0 && e.parent == "" {
			unstamped++
			t.Errorf("<%s> at depth %d carries no parent, so readsAsData would fall to "+
				"the catalog search — which sorts by NAME and can therefore name an "+
				"element this document is not inside", e.Name, depth)
		}
		for i := range e.Children {
			walk(&e.Children[i], depth+1)
		}
		// PROPERTY ELEMENTS ARE DELIBERATELY NOT WALKED, and the reason
		// is the one that makes this test about the right set.
		//
		// A Props entry carries NO parent — measured: adding it to this
		// walk reports <Grid.Resources> unstamped at depth 3. It is also
		// never what readsAsData is handed: refusePropElement
		// (attrcheck.go:290) takes the OWNER's Element and passes that,
		// naming the property only in the message. So an unstamped
		// Props entry cannot reach the fallbacks, and asserting over it
		// would fail this test for a shape the code under it never
		// sees. Recorded rather than dropped silently, because "the
		// parser stamps every element" is false as stated and true for
		// the set that matters.
	}
	walk(&root, 0)
	if seen < 10 {
		t.Fatalf("the walk saw %d elements, which is fewer than the fixture declares — "+
			"a walk that visits nothing reports no unstamped element either", seen)
	}
	t.Logf("%d elements, %d unstamped", seen, unstamped)
}

// TestNoRemedyPrescribesASpellingTheAttributeGateRefuses is the guard on
// the walk-from-one-refusal-to-another class, closed at the one gate
// attributeHere's own argument did not account for.
//
// attributeHere justifies prescribing `<X Name="…">` with "if the
// element does not take the name, the attribute gate answers with its
// own list, which is a better error". False for everything cannotApplyTo
// covers, because refuseComponentAttr intercepts those first and answers
// with no advice at all. Measured before the fix:
//
// (This comment used to say the claim was "true for a name the
// VOCABULARY gate sees — <Tab.Frobnicate> reaches suggest()". It does
// not: <Tab>'s AttrsKnown is false, so checkAttrs returns before the
// gate and <Tab Frobnicate="z"> loads and is dropped.
// TestARemedyOnAnUncheckedSurfaceSaysTheNameCanBeDropped is that case,
// and the remedy carries the caveat now. Corrected in review of #486.)
//
//	<Menu.Name>x</Menu.Name>  -> …; write it as an attribute on this
//	                             element instead, <Menu Name="…">, …
//	<Menu Name="x">           -> …builds no component for Name to apply to
//
// THE ASSERTION BUILDS THE PRESCRIBED DOCUMENT rather than reading the
// sentence, because the claim is behavioural: a remedy is a promise that
// following it gets somewhere. Raised in review of #486.
func TestNoRemedyPrescribesASpellingTheAttributeGateRefuses(t *testing.T) {
	ctx := &Context{}
	var checked int
	for _, sp := range ctx.Catalog() {
		if !sp.Pseudo {
			continue
		}
		names := []string{"Grid.Row", "Canvas.Left"}
		for _, u := range universalAttrs {
			names = append(names, u.Name)
		}
		for _, name := range names {
			checked++
			r := propRemedy(withContent(sp), sp, ctx, name)
			if !strings.Contains(r, "write it as an attribute on this element") {
				continue
			}
			t.Errorf("<%s.%s> is refused with advice to write <%s %s=\"…\">, and %q is "+
				"a name refuseComponentAttr refuses on any pseudo-element before the "+
				"vocabulary gate runs — so the author follows the advice into a second "+
				"refusal carrying no advice at all. Remedy: %q",
				sp.Name, name, sp.Name, name, name, r)
		}
	}
	if checked == 0 {
		t.Fatal("no pseudo-element in the catalog was checked, so this guard ran over " +
			"nothing — Catalog() or the Pseudo flag changed shape")
	}
	t.Logf("%d pseudo-element x refusable-name pairs checked", checked)
}

// TestAContentRemedySurvivesForThePropertyElements is finding 2 of round
// 5, and the half a green suite could not show.
//
// dbcbc7e added `if !isUniversalAttr(name) { return "" }` to
// pseudoRemedy to stop the content move being prescribed for ATTACHED
// properties, which have no destination. Behaviors and Resources are
// neither attributes nor universals, so the guard caught them too — and
// their destination IS derivable, because every element accepts them.
// <Tab.Behaviors> lost a remedy that was correct: the <Text.Behaviors>
// it pointed at loads. propRemedy's `case name == "Behaviors" || name ==
// "Resources": return r` could then return only "", against a comment
// saying the spelling carries over unchanged.
func TestAContentRemedySurvivesForThePropertyElements(t *testing.T) {
	ctx := &Context{}
	for _, name := range []string{"Behaviors", "Resources"} {
		sp, ok := ctx.spec("Tab")
		if !ok {
			t.Fatal("<Tab> is not in the catalog")
		}
		if got := propRemedy(withContent(sp), sp, ctx, name); got == "" {
			t.Errorf("<Tab.%s> is refused with no remedy. Every element accepts %s, so "+
				"the content inside a <Tab> is a destination this code can name — which "+
				"is the whole condition the content move needs", name, name)
		}
		// WITH CONTENT INSIDE, because that is what the remedy names.
		// A <Tab> holding only the property element has nothing to move
		// it onto, and pseudoRemedy now says so rather than prescribing
		// a destination that is not there — which is the round-7
		// finding one arm over. Raised in review of #486.
		doc := fmt.Sprintf(`<Gooey><Tabs><Tab Header="a"><Tab.%s/><Text>x</Text></Tab></Tabs></Gooey>`, name)
		_, err := Build([]byte(doc), ctx)
		if err == nil {
			t.Fatalf("%s is accepted, so there is no refusal to carry a remedy", doc)
		}
		if !strings.Contains(err.Error(), contentRemedy) {
			t.Errorf("%s is refused as:\n\t%v\nwant the content remedy", doc, err)
		}
		// AND THE DESTINATION TAKES IT, in the same spelling — the
		// property-element form, since that is what carries over
		// unchanged.
		dest := fmt.Sprintf(`<Gooey><Tabs><Tab Header="a"><Text><Text.%s/></Text></Tab></Tabs></Gooey>`, name)
		if _, err := Build([]byte(dest), ctx); err != nil {
			t.Errorf("the remedy for <Tab.%s> lands on <Text.%s>, and that is refused "+
				"too:\n\t%v", name, name, err)
		}
	}
}

// TestANamelessHostRegistrationStillNamesItself. checkElementNames
// explicitly permits a def with no Name — its loop reads `if d == nil ||
// d.Name == "" || d.Name == name` — and specAs copied the empty string
// through, so every message reading spec.Name rendered "<>" beside a
// clause reading e.Name. It also silently missed
// reservedOnContent[spec.Name] and namesChild(p, spec.Name), the second
// of which was the ONLY live route into readsAsData's fallbacks.
// Measured before the fix: "<Holder> reads <> as data" and
// "< Frob=…">, if <> takes one". Raised in review of #486.
func TestANamelessHostRegistrationStillNamesItself(t *testing.T) {
	ctx := &Context{Elements: map[string]*ElementDef{
		"Leafy":  {ParsedBy: "Holder", Known: true},
		"Holder": {Name: "Holder", Known: true, Children: ChildSpec{Mode: ModeRestricted, Only: []string{"Leafy"}}},
	}}
	sp, ok := ctx.spec("Leafy")
	if !ok {
		t.Fatal("the Name-less registration does not resolve at all")
	}
	if sp.Name != "Leafy" {
		t.Errorf("a def registered as Elements[%q] with no Name resolves to Name=%q; the "+
			"registry key is the element's name and every message here prints it",
			"Leafy", sp.Name)
	}
	if !sp.Pseudo {
		t.Fatal("<Leafy> is not pseudo, so the refusals this guards are never reached")
	}
	got := readsAsData(Element{Name: "Leafy", parent: "Holder"}, sp, ctx)
	if strings.Contains(got, "<>") {
		t.Errorf("readsAsData renders %q, which names the element as <>", got)
	}
	if !strings.Contains(got, "<Leafy>") {
		t.Errorf("readsAsData renders %q and does not name <Leafy>", got)
	}
}

// TestARemedyOnAnUncheckedSurfaceSaysTheNameCanBeDropped is the other
// half of the guard above, at the gate that does not exist.
//
// attributeHere's argument is that a wrong name lands on the attribute
// gate, which answers with its own list. checkAttrs returns BEFORE that
// gate when a spec's AttrsKnown is false, so the name is accepted and
// dropped instead — the #461 class, reached by following this PR's own
// advice. Measured on the head this was written against:
//
//	<Tab.Frobnicate>z</Tab.Frobnicate>  -> refused, advising <Tab Frobnicate="…">
//	<Tab Frobnicate="z">                -> <nil>
//
// The sibling case shows the premise holding where the surface IS
// declared, which is what makes this a shape rather than a wording
// problem:
//
//	<MenuItem Frobnicate="z">  -> no such attribute; this element takes
//	                              Checked, Command, Gesture, …
//
// So the remedy stays — <Tab.Header> is a real property with an obvious
// home, and nothing can tell Header from Frobnicate on a spec whose
// Attrs are not exhaustive — and it has to say what it cannot promise.
// Raised in review of #486.
func TestARemedyOnAnUncheckedSurfaceSaysTheNameCanBeDropped(t *testing.T) {
	ctx := &Context{}
	// OUTSIDE cannotApplyTo, which is what the guard above ranges over
	// and why it could not see this: every name it asks about is
	// intercepted by refuseComponentAttr before any vocabulary gate.
	const name = "Frobnicate"

	var unchecked, checked int
	for _, sp := range ctx.Catalog() {
		if !sp.Pseudo {
			continue
		}
		r := propRemedy(withContent(sp), sp, ctx, name)
		if !strings.Contains(r, "write it as an attribute on this element") {
			continue
		}
		if sp.AttrsKnown {
			checked++
			continue
		}
		unchecked++
		if !strings.Contains(r, "silently dropped") {
			t.Errorf("<%s.%s> is refused with advice to write <%s %s=\"…\">, and %s's "+
				"AttrsKnown is false — so checkAttrs returns before the vocabulary "+
				"gate and that attribute is accepted and dropped rather than "+
				"refused. The remedy must say so. Remedy: %q",
				sp.Name, name, sp.Name, name, sp.Name, r)
		}
	}
	if unchecked == 0 {
		t.Fatalf("no pseudo-element with AttrsKnown false was reached (%d with it "+
			"true), so this guard compared nothing. <Tab> is the one the finding "+
			"was written about; if it gained an exhaustive Attrs the #461 "+
			"residual hole is closed and this test should say that instead",
			checked)
	}

	// AND THE BEHAVIOUR, because the assertion above reads a sentence.
	// A remedy is a promise that following it gets somewhere, so the
	// document it prescribes is what settles whether the caveat is
	// needed.
	if _, err := Build([]byte(
		`<Gooey><Tabs><Tab Header="a" `+name+`="z"><Text>x</Text></Tab></Tabs></Gooey>`,
	), &Context{}); err != nil {
		t.Errorf("<Tab %s=\"z\"> is refused with %v, so the attribute gate DOES "+
			"answer for <Tab> and the caveat this test requires is now false. "+
			"Drop it from attributeHere and delete this arm", name, err)
	}
}

// hostTableCtx is a host's own pseudo-elements and the container that
// reads them, in the three shapes a host actually writes one.
//
// THE CONTAINER IS ModeMany, and that is half the fixture. A host
// registering a container declares what it BUILDS, and a container that
// hands its children to BuildChildren takes many of them — nothing
// obliges it to enumerate their names. So the ordinary host shape is a
// ModeMany parent holding a declared child, which is the one shape no
// builtin has: all three builtin pseudo-elements sit under a
// ModeRestricted container.
//
// THE OTHER HALF IS THAT Pseudo IS TRUE THREE WAYS, and the three
// children here are exactly those ways. <Row> states its reason with
// ParsedBy and carries a Build; <ORow> states it with Opaque; <Bare>
// states it with ParsedBy and has NO Build, which is a
// pseudo-element's natural host declaration — "declared here, read
// there" leaves nothing to put in the field. The first two were the two
// routes the previous round's repair covered and missed; the third is
// the only one of the three that genuinely builds no component.
//
// <Row>'s Build refuses its own placement the way defTab's does, so the
// misplaced arm has a real diagnosis to defer TO.
func hostTableCtx() *Context {
	build := func(e Element, ctx *Context) (gooey.Component, error) {
		if e.parent != "Table" {
			return nil, fmt.Errorf("markup: <%s> is only valid directly inside <Table>", e.Name)
		}
		return &components.Text{}, nil
	}
	return &Context{Elements: map[string]*ElementDef{
		"Table": {
			Name:     "Table",
			Known:    true,
			Attrs:    []AttrSpec{{Name: "Title"}},
			Children: ChildSpec{Mode: ModeMany},
			Build: func(e Element, ctx *Context) (gooey.Component, error) {
				kids, _, err := BuildChildren(e, ctx)
				if err != nil {
					return nil, err
				}
				return &components.VStack{Children: kids}, nil
			},
		},
		"Row": {
			Name:     "Row",
			Known:    true,
			ParsedBy: "Table",
			Attrs:    []AttrSpec{{Name: "Label"}},
			Children: ChildSpec{Mode: ModeNone},
			Build:    build,
		},
		"ORow": {
			Name:     "ORow",
			Known:    true,
			Opaque:   "a pseudo-element: <Table> parses an <ORow> itself",
			Attrs:    []AttrSpec{{Name: "Label"}},
			Children: ChildSpec{Mode: ModeNone},
			Build:    build,
		},
		"Bare": {
			Name:     "Bare",
			Known:    true,
			ParsedBy: "Table",
			Attrs:    []AttrSpec{{Name: "Label"}},
			Children: ChildSpec{Mode: ModeNone},
		},
	}}
}

// TestAHostsPseudoElementIsCheckedWhereItsReaderSaysItBelongs is #461's
// silent-drop class in the host-registration tier, and it took two
// rounds to state because the first repair was phrased over one of the
// three ways Pseudo can be true.
//
// The stand-down that defers to a misplaced element's own Build asked
// whether the parent is a ModeRestricted container naming this element.
// A host container that enumerates nothing is not, so every
// correctly-placed declared child under it read as MISPLACED and the
// exhaustive unknown-attribute gate went with the universal refusal.
// Measured against a clean origin/main worktree, which refuses all
// three:
//
//	<Table><Row  Label="a" Bogus="x"/></Table>  -> <nil>
//	<Table><ORow Label="a" Bogus="x"/></Table>  -> <nil>
//	<Table><Bare Label="a" Bogus="x"/></Table>  -> <nil>
//
// ALL THREE PSEUDO SPELLINGS, derived from the fixture rather than
// listed, because the first repair covered ParsedBy and left Opaque
// open — the identical defect one FIELD over, which the ParsedBy-only
// fixture could not see.
func TestAHostsPseudoElementIsCheckedWhereItsReaderSaysItBelongs(t *testing.T) {
	ctx := hostTableCtx()
	table, okT := ctx.spec("Table")
	if !okT || table.Children.Mode == ModeRestricted {
		t.Fatalf("the fixture's container is %v — a ModeRestricted one is "+
			"answered by namesChild and this test could not see the gap",
			table.Children.Mode)
	}
	var checked int
	for _, name := range []string{"Row", "ORow", "Bare"} {
		sp, ok := ctx.spec(name)
		if !ok || !sp.Pseudo || !sp.AttrsKnown {
			t.Errorf("<%s> is %v/%v, not the Pseudo+AttrsKnown shape this test "+
				"is about", name, sp.Pseudo, sp.AttrsKnown)
			continue
		}
		checked++
		doc := `<Gooey><Table><` + name + ` Label="a" Bogus="x"/></Table></Gooey>`
		_, err := Build([]byte(doc), hostTableCtx())
		if err == nil {
			t.Errorf("<%s> accepted an attribute it does not declare, inside the "+
				"very container the catalog says reads it — accepted, dropped, "+
				"and visible nowhere, which is the #461 class this branch closed "+
				"for the builtins", name)
			continue
		}
		if !strings.Contains(err.Error(), "Bogus") || !strings.Contains(err.Error(), "Label") {
			t.Errorf("<%s>'s refusal should name the attribute and the vocabulary "+
				"it is missing from: %v", name, err)
		}
	}
	if checked != 3 {
		t.Fatalf("checked %d of the three ways Pseudo can be true; the fixture "+
			"is what this test derives them from", checked)
	}

	// A NAME ON AN ELEMENT THAT BUILDS ONE IS APPLIED, NOT REFUSED, and
	// the previous round's version of this test asserted the opposite.
	// <Row> carries ParsedBy AND a Build; buildComponent calls named()
	// on what that Build returns, so the attribute addresses something
	// real. Pseudo says nothing about Build — that is round 7's finding
	// 1, and this arm is where the suite was pinning the false claim.
	if _, err := Build([]byte(`<Gooey><Table><Row Label="a" Name="n"/></Table></Gooey>`), ctx); err != nil {
		t.Errorf("a host element with a real Build was refused Name: %v", err)
	} else if ctx.Named["n"] == nil {
		t.Errorf("Name was accepted and dropped: ctx.Named holds %d entries",
			len(ctx.Named))
	}

	// AND THE ONE WITH NO Build IS A LOAD ERROR RATHER THAN A SEGV, in
	// the same position. <Bare> is the shape whose declaration really
	// does build nothing.
	//
	// THE TWO POSITIONS GET DIFFERENT SENTENCES, and this arm used to
	// check only that both names appeared — which passed on the arm
	// where the message was wrong. Round 8's finding 3: inside <Table>
	// the document has already done what the placement sentence asks,
	// so the author's next step is to make no change. The fault there is
	// the registration — <Table>'s Build routes children through
	// BuildChildren while the catalog says <Table> parses them — and the
	// message never said so.
	_, err := Build([]byte(`<Gooey><Table><Bare Label="a"/></Table></Gooey>`), hostTableCtx())
	if err == nil {
		t.Error("a registered element with no Build produced a component")
	} else {
		if !strings.Contains(err.Error(), "<Bare>") || !strings.Contains(err.Error(), "<Table>") {
			t.Errorf("the no-Build error should name the element and the reader the "+
				"catalog says consumes it: %v", err)
		}
		if strings.Contains(err.Error(), "only valid where") {
			t.Errorf("the <Bare> is inside the <Table> the catalog names, and the "+
				"refusal prescribes moving it there — a remedy whose next step is "+
				"to change nothing:\n\t%v", err)
		}
		if !strings.Contains(err.Error(), "BuildChildren") {
			t.Errorf("the in-place refusal does not name the registration that is "+
				"actually wrong: %v", err)
		}
	}

	// THE MISPLACED ONE KEEPS THE PLACEMENT SENTENCE, or the split above
	// would be a swap rather than a discrimination.
	_, err = Build([]byte(`<Gooey><VStack><Bare Label="a"/></VStack></Gooey>`), hostTableCtx())
	if err == nil {
		t.Error("a <Bare> outside its reader loaded clean")
	} else if !strings.Contains(err.Error(), "only valid where") {
		t.Errorf("a <Bare> in a <VStack> really is misplaced, and the refusal no "+
			"longer says where it belongs: %v", err)
	}

	// AND BOTH REMEDIES ARE RUN, because an error message's prescription
	// is a behavioural claim. The in-place sentence offers two edits;
	// each is applied to the fixture here and the document must load.
	withBuild := hostTableCtx()
	withBuild.Elements["Bare"].Build = func(e Element, ctx *Context) (gooey.Component, error) {
		return &components.Text{}, nil
	}
	if _, err := Build([]byte(`<Gooey><Table><Bare Label="a"/></Table></Gooey>`), withBuild); err != nil {
		t.Errorf(`the first remedy — "give <Bare> a Build" — does not load: %v`, err)
	}
	readsOwn := hostTableCtx()
	readsOwn.Elements["Table"].Build = func(e Element, ctx *Context) (gooey.Component, error) {
		// <Table> reading its <Bare> children itself, which is what
		// ParsedBy said all along.
		return &components.VStack{}, nil
	}
	if _, err := Build([]byte(`<Gooey><Table><Bare Label="a"/></Table></Gooey>`), readsOwn); err != nil {
		t.Errorf(`the second remedy — "have <Table>'s Build read its <Bare> `+
			`children itself" — does not load: %v`, err)
	}

	// THE DEFERRAL MUST SURVIVE. A fix that simply checked every
	// pseudo-element would satisfy every arm above and undo round 2's
	// finding: this document's fault is the PLACEMENT, and <Row>'s own
	// Build is the only thing that can say so.
	_, err = Build([]byte(`<Gooey><VStack><Row Label="a" Bogus="x"/></VStack></Gooey>`), hostTableCtx())
	if err == nil {
		t.Fatal("a <Row> in a <VStack> loaded clean")
	}
	if !strings.Contains(err.Error(), "only valid directly inside") {
		t.Errorf("a misplaced <Row>'s fault is where it is, and the refusal "+
			"talks about its attribute instead — the remedy leaves the document "+
			"just as broken and the diagnosis that would fix it never "+
			"prints:\n\t%v", err)
	}
}

// hostDeckCtx is a host pseudo-element that BUILDS a component and
// hosts attachments, which is the shape ElementSpec.Pseudo cannot
// describe.
func hostDeckCtx() *Context {
	return &Context{Elements: map[string]*ElementDef{
		"Deck": {
			Name:     "Deck",
			Known:    true,
			Attrs:    []AttrSpec{{Name: "Title"}},
			Children: ChildSpec{Mode: ModeRestricted, Only: []string{"Panel"}},
			Build: func(e Element, ctx *Context) (gooey.Component, error) {
				kids, _, err := BuildChildren(e, ctx)
				if err != nil {
					return nil, err
				}
				return &components.VStack{Children: kids}, nil
			},
		},
		"Panel": {
			Name:     "Panel",
			Known:    true,
			ParsedBy: "Deck",
			Attrs:    []AttrSpec{{Name: "Label"}},
			Children: ChildSpec{Mode: ModeMany},
			Build: func(e Element, ctx *Context) (gooey.Component, error) {
				kids, attach, err := BuildChildren(e, ctx)
				if err != nil {
					return nil, err
				}
				w := &components.VStack{Children: kids}
				return w, attachAll(e, w, attach)
			},
		},
	}}
}

// TestAHostPseudoElementThatBuildsKeepsWhatItBuildsWith is round 7's
// finding 1, and the defect is a refusal resting on a claim the
// derivation does not make.
//
// Pseudo is `Proto == nil && (Opaque != "" || ParsedBy != "")`
// (elementdef.go) and says nothing about Build. refuseComponentAttr's
// doc rests on the opposite — "it means 'this builds no component of its
// own'" — and refusePropElement refuses EVERY <X.Foo> on that basis. A
// host def carrying ParsedBy and a real Build is both at once, and the
// consequence was a page that stopped loading:
//
//	<Deck><Panel Label="a"><Panel.Behaviors><Tooltip Text="x"/>
//	  </Panel.Behaviors><Text>y</Text></Panel></Deck>
//	  origin/main: loads, Panel.Build runs, one attachment applied
//	  before:      markup: <Panel.Behaviors>: … builds no component for
//	               Behaviors to apply to
//
// with a reason that is false: Panel.Build runs and attachAll applied
// that behaviour.
//
// THE ATTACHMENT IS ASSERTED, NOT THE LOAD. "It loads again" passes for
// a build that accepted the property element and dropped it, which is
// the class this whole branch is about.
func TestAHostPseudoElementThatBuildsKeepsWhatItBuildsWith(t *testing.T) {
	ctx := hostDeckCtx()
	sp, ok := ctx.spec("Panel")
	if !ok || !sp.Pseudo {
		t.Fatalf("<Panel> is not Pseudo (%v/%v), so the refusal under test would "+
			"not fire on it either way", ok, sp.Pseudo)
	}
	root, err := Build([]byte(
		`<Gooey><Deck><Panel Label="a"><Panel.Behaviors><Tooltip Text="x"/>`+
			`</Panel.Behaviors><Text Name="y">y</Text></Panel></Deck></Gooey>`), ctx)
	if err != nil {
		t.Fatalf("a host element with a real Build was refused a property "+
			"element every element accepts: %v", err)
	}
	if root == nil {
		t.Fatal("Build returned no root")
	}
	// The behaviour reached the component Panel.Build returned.
	var found bool
	var walk func(c gooey.Component)
	walk = func(c gooey.Component) {
		if a, ok := c.(gooey.Attacher); ok {
			for _, x := range a.Attachments() {
				if _, isTip := x.(*components.Tooltip); isTip {
					found = true
				}
			}
		}
		if cont, ok := c.(gooey.Container); ok {
			for _, k := range cont.ChildComponents() {
				walk(k)
			}
		}
	}
	walk(root)
	if !found {
		t.Error("<Panel.Behaviors> loaded and its <Tooltip> attached to nothing — " +
			"accepted and dropped, which is what asserting the load alone would " +
			"have passed over")
	}
}

// TestTheContentRemedyIsDecidedPerAttribute is the fourth appearance of
// one class — a remedy that walks the author into a second load error —
// and the arm it had never reached is the one that matters most.
//
// pseudoRemedy asked the CATALOG on its ModeRestricted arm and answered
// unconditionally everywhere else. ModeUnknown is <Tab>'s, and <Tab> is
// the one pseudo-element whose content is actually present in the
// document, so the element with a real destination to name was the one
// whose destination was never consulted. Reachable on the builtins
// alone, which is why this test needs no host registration:
//
//	<Tabs><Tab Header="a" Margin="2"><Timer Interval="1s"/></Tab></Tabs>
//	  -> … no component for Margin to apply to; put it on the content
//	     inside instead
//	<VStack><Timer Interval="1s" Margin="2"/></VStack>
//	  -> no such attribute; this element takes Enabled, Interval, Name,
//	     Tick
//
// <Timer> is the destination and it is not an arbitrary pick: it builds
// a component and carries no Layout, so one content element accepts
// Name and refuses Margin. That is the axis an answer about the ELEMENT
// cannot represent.
//
// BOTH DIRECTIONS, because withholding always is the opposite defect,
// and the prescribing arm RUNS THE ADVICE — a remedy is a behavioural
// claim, and the only way to know it survives being followed is to
// follow it.
func TestTheContentRemedyIsDecidedPerAttribute(t *testing.T) {
	ctx := &Context{}
	timer, okT := ctx.spec("Timer")
	tab, okTab := ctx.spec("Tab")
	if !okT || !okTab || timer.Pseudo || TakesLayout(timer) || tab.Children.Mode == ModeRestricted {
		t.Fatalf("the fixture is not the discriminating shape — the content must "+
			"build a component (so an answer about the element says yes) and carry "+
			"no Layout (so the layout rows are refused there), and <Timer> is "+
			"%v/%v inside a %v <Tab>",
			timer.Pseudo, TakesLayout(timer), tab.Children.Mode)
	}

	withheld := `<Gooey><Tabs><Tab Header="a" Margin="2"><Timer Interval="1s"/></Tab></Tabs></Gooey>`
	_, err := Build([]byte(withheld), &Context{})
	if err == nil {
		t.Fatal("<Tab Margin=…> was not refused at all")
	}
	if strings.Contains(err.Error(), contentRemedy) {
		t.Errorf("nothing inside this <Tab> takes Margin, and its refusal tells "+
			"the author to put it on the content inside — a destination that "+
			"refuses it for the same reason:\n\t%v", err)
	}

	prescribed := `<Gooey><Tabs><Tab Header="a" Margin="2"><Text Name="t">x</Text></Tab></Tabs></Gooey>`
	_, err = Build([]byte(prescribed), &Context{})
	if err == nil {
		t.Fatal("<Tab Margin=…> was not refused at all with a <Text> inside")
	}
	if !strings.Contains(err.Error(), contentRemedy) {
		t.Errorf("this <Tab>'s content takes Margin, and its refusal prescribes "+
			"nowhere to put it — withholding the remedy is the same defect as "+
			"prescribing it, one direction over:\n\t%v", err)
	}

	// THE ADVICE, FOLLOWED.
	if _, err := Build([]byte(
		`<Gooey><Tabs><Tab Header="a"><Text Name="t" Margin="2">x</Text></Tab></Tabs></Gooey>`,
	), &Context{}); err != nil {
		t.Errorf("the prescribed document does not load, so the remedy walks the "+
			"author from one refusal to another: %v", err)
	}
}

// withContent is a pseudo-element as a document would spell it, with
// one ordinary component inside.
//
// pseudoRemedy asks e.Children on every arm the catalog cannot answer
// for — ModeUnknown is <Tab>'s, and what is inside a <Tab> is a fact of
// the document rather than of the vocabulary — so a hand-built Element
// with no children is a <Tab> with nothing inside, which correctly gets
// no content remedy at all. Every direct call below is about the
// ordinary case, so it passes the ordinary shape.
func withContent(sp ElementSpec) Element {
	return Element{
		Name:     sp.Name,
		parent:   namingParent(sp.Name, &Context{}),
		Children: []Element{{Name: "Text"}},
	}
}

// TestAShadowingHostDefStillRefusesNameWhereItsReaderTakesOver is round
// 8's finding 1, and it is the accepted-and-dropped shape #461 exists to
// close arriving from the side the fix for #461 left open.
//
// Name was gated on `builds || !spec.Pseudo`. `builds` is `!asData`, and
// the only caller that can reach vocabulary with builds == false is
// checkAttrs on a child its parent reads as DATA — buildTabs,
// buildMenuBar, or a host's own Build walking e.Children, none of which
// calls named(). So the `|| !spec.Pseudo` disjunct could only ever admit
// Name where it would be dropped, on an element that is read as data and
// is not Pseudo: a HOST def shadowing Tab, Menu or MenuItem with a real
// Proto.
//
// THE REVIEW'S PRESCRIBED FIX WAS NOT ENOUGH, and the measurement is why
// this test has two arms. Tightening the gate to `if builds {` left the
// fixture in the finding still silent:
//
//	Elements["Tab"] = {Proto: &components.Text{}, Attrs: [Header]}
//	<Tabs><Tab Header="a" Name="zonk"><Text>x</Text></Tab></Tabs>
//	  with `if builds {` alone: err = <nil>, len(ctx.Named) == 0
//
// because Name is ALSO in universalAttrs, and the loop that adds those
// runs below the guarded write for every element with a Layout. The
// guarded write was therefore reachable only for the layout-less
// minority. Both sites are now one decision — the universals loop skips
// Name — and the two arms below are the two sites:
//
//	*components.Text  TakesLayout == true   -> the universals loop
//	*components.Timer TakesLayout == false  -> the guarded write
//
// Drop either half and one arm goes green while the other stays red.
func TestAShadowingHostDefStillRefusesNameWhereItsReaderTakesOver(t *testing.T) {
	for _, tc := range []struct {
		name   string
		proto  gooey.Component
		layout bool
	}{
		{"a shadowing def that takes layout, where universalAttrs re-adds Name", &components.Text{}, true},
		{"a shadowing def with no Layout, where only the guarded write runs", &components.Timer{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := func() *Context {
				return &Context{Elements: map[string]*ElementDef{"Tab": {
					Name:     "Tab",
					Known:    true,
					Proto:    tc.proto,
					Attrs:    []AttrSpec{{Name: "Header"}},
					Children: ChildSpec{Mode: ModeOne},
				}}}
			}
			sp, ok := ctx().spec("Tab")
			if !ok || sp.Pseudo {
				t.Fatalf("the shadowing <Tab> is Pseudo (%v/%v), so the pseudo "+
					"refusals would answer first and this arm would be about "+
					"a different gate", ok, sp.Pseudo)
			}
			// The axis this arm exists for. Without it the two arms
			// could both be the same site and the table would prove
			// one thing twice.
			if got := TakesLayout(sp); got != tc.layout {
				t.Fatalf("TakesLayout is %v, want %v — this arm is not the gate "+
					"it is named for", got, tc.layout)
			}

			// The fixture loads without the attribute, so a refusal
			// below is about Name and not about the registration.
			base := ctx()
			if _, err := Build([]byte(
				`<Gooey><Tabs><Tab Header="a"><Text>x</Text></Tab></Tabs></Gooey>`), base); err != nil {
				t.Fatalf("the fixture itself does not load, so the arm below "+
					"proves nothing about Name: %v", err)
			}

			named := ctx()
			_, err := Build([]byte(
				`<Gooey><Tabs><Tab Header="a" Name="zonk"><Text>x</Text></Tab></Tabs></Gooey>`), named)
			if err == nil {
				t.Fatalf("<Tab Name=%q> loaded under <Tabs>, which reads it as data "+
					"and never calls named() — accepted, dropped, and reported "+
					"nowhere (ctx.Named holds %d)", "zonk", len(named.Named))
			}
			if !strings.Contains(err.Error(), "Name") {
				t.Errorf("the refusal does not name the attribute it refused: %v", err)
			}
			if strings.Contains(err.Error(), "takes") && strings.Contains(
				strings.SplitN(err.Error(), "takes", 2)[1], "Name") {
				t.Errorf("the near-miss advice still advertises Name on the element "+
					"that has just refused it: %v", err)
			}
		})
	}

	// THE COUNTERFACTUAL: an element that is BUILT keeps Name, or the
	// change above would be a repo-wide regression rather than a gate.
	ctx := &Context{}
	if _, err := Build([]byte(
		`<Gooey><VStack Name="outer"><Text Name="inner">x</Text></VStack></Gooey>`), ctx); err != nil {
		t.Fatalf("Name was refused on ordinary built elements: %v", err)
	}
	if len(ctx.Named) != 2 {
		t.Errorf("ctx.Named holds %d names, want 2 — Name reached the map for "+
			"neither <VStack> nor <Text>", len(ctx.Named))
	}
}

// TestTheDesignerOffersNoNameRowWhereTheLoaderHonoursOne is the identity
// row's version of the shape above, and it is round 8's finding 2.
//
// Grant.AttrsFor withholds Name on `!e.Pseudo`, and its comment asserts
// the agreement outright: "the loader refuses it for the same reason
// (Context.vocabulary), and these two must agree or the grid offers a
// row that fails to load." For a HOST def carrying ParsedBy AND a real
// Build — the shape TestAHostPseudoElementThatBuildsKeepsWhatItBuildsWith
// establishes is legal — that is measurably false, with the two sides
// swapped: the grid offers nothing and the loader HONOURS it.
//
//	AttrsFor(hostDeckCtx's <Panel>)                -> [Label]
//	Build(`<Deck><Panel Label="a" Name="p">…`)     -> err = <nil>,
//	                                                  ctx.Named["p"] set
//
// THE REVIEW'S PRESCRIBED FIX DOES NOT WORK, and this is why the gap is
// pinned rather than closed. It was to carry a has-a-Build bit from
// ElementDef onto ElementSpec, since specAs holds d.Build. Measured on
// the three builtin pseudo-elements:
//
//	Tab       Build != nil   Menu   Build != nil   MenuItem  Build != nil
//
// All three have one, and all three exist only to REFUSE — defMenu.Build
// returns "markup: <Menu> is only valid directly inside <MenuBar>". So
// `Builds: d.Build != nil` is true for exactly the elements the row must
// stay off, and gating on it reddens
// TestTheDesignerOffersNoUniversalRowOnAPseudoElement. The fact that
// separates hostDeck's Panel.Build from defMenu.Build is whether it
// RETURNS a component, which no bit on the spec can state.
//
// So this is stated and pinned rather than fixed, exactly as the layout
// row above is, and the pin is what keeps the comment honest.
func TestTheDesignerOffersNoNameRowWhereTheLoaderHonoursOne(t *testing.T) {
	offers := func(parent, child string, ctx *Context) bool {
		p, ok := ctx.spec(parent)
		if !ok {
			t.Fatalf("<%s> did not resolve", parent)
		}
		c, ok := ctx.spec(child)
		if !ok {
			t.Fatalf("<%s> did not resolve", child)
		}
		for _, a := range p.Grants.AttrsFor(c) {
			if a.Name == "Name" {
				return true
			}
		}
		return false
	}

	ctx := hostDeckCtx()
	sp, _ := ctx.spec("Panel")
	if !sp.Pseudo {
		t.Fatal("<Panel> is not Pseudo, so the gate under test would not fire " +
			"on it and this test would prove nothing")
	}
	if TakesLayout(sp) {
		t.Fatal("<Panel> takes layout, so AttrsFor's first arm answers and the " +
			"Name row comes from universalAttrs — a different gate")
	}

	offered := offers("Deck", "Panel", ctx)
	root, err := Build([]byte(
		`<Gooey><Deck><Panel Label="a" Name="p"><Text>x</Text></Panel></Deck></Gooey>`), ctx)
	if err != nil {
		t.Fatalf("the loader now refuses Name on a host ParsedBy def with a "+
			"real Build, which closes this gap from the other side — update "+
			"Grant.AttrsFor's comment with it: %v", err)
	}
	if root == nil {
		t.Fatal("Build returned no root")
	}
	honoured := ctx.Named["p"] != nil

	switch {
	case offered && honoured:
		t.Error("the grid now offers the Name row the loader honours, which " +
			"CLOSES this gap — delete this test and correct Grant.AttrsFor's " +
			"comment, which says the two gates agree")
	case !offered && !honoured:
		t.Error("Name was accepted and dropped on a host ParsedBy def with a " +
			"real Build, which is #461's own defect rather than this one — " +
			"the loader must either refuse it or honour it")
	case offered && !honoured:
		t.Error("the grid offers a Name row the loader drops, which is the " +
			"direction Grant.AttrsFor's comment says IS guarded")
	}
	// The surviving case is !offered && honoured, which is the asymmetry
	// this test exists to hold: an attribute that works with no designer
	// surface anywhere.
}
