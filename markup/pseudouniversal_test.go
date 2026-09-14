package markup

import (
	"fmt"
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
	good := `markup: <MenuItem Name="Zork">: <MenuBar> reads <MenuItem> as ` +
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
// refuseUniversal began inside checkAttrs' !AttrsKnown early return, so
// only <Tab> — the one pseudo-element whose attributes are not
// enumerable — reached it. <Menu> and <MenuItem> fell through to the
// exhaustive check and refused a universal with "no such attribute; this
// element takes Title", the wording refuseUniversal's own comment calls a
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
func TestARefusalPrescribesOnlyAPlaceThatExists(t *testing.T) {
	var checked int
	for _, sp := range pseudoSpecs(t) {
		if acceptsAUniversal(t, sp) {
			continue
		}
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
		for _, u := range universalAttrs {
			checked++
			attr := fmt.Sprintf("%s=%q", u.Name, validLiteralFor(t, "Border", u))
			_, err := Build([]byte(fmt.Sprintf(tc.onPseudo, attr)), &Context{})
			if err == nil {
				t.Errorf("<%s %s> was not refused at all", sp.Name, attr)
				continue
			}
			if strings.Contains(err.Error(), contentRemedy) {
				t.Errorf("nothing <%s> may contain (%s) would accept %s, and its "+
					"refusal tells the author to put the attribute on the content "+
					"inside — a remedy whose destination refuses it too:\n\t%v",
					sp.Name, sp.Children.Mode, u.Name, err)
			}
		}
	}
	if checked == 0 {
		t.Fatal("every pseudo-element in the catalog can host a universal " +
			"somewhere inside, so this test ranged over nothing. It is not a " +
			"pass — either the modes moved or the filter is wrong")
	}
}

// acceptsAUniversal reports whether the content inside sp could hold a
// universal attribute, which is what makes "put it on the content
// inside" a real instruction.
//
// Three answers, and the middle one is the finding. ModeLeaf/ModeNone
// hold nothing. An unrestricted container holds anything, so it holds
// something addressable. ModeRestricted holds a named set — and if every
// name in it is a pseudo-element, the content inside refuses the
// attribute for exactly the reason the outer element did.
func acceptsAUniversal(t *testing.T, sp ElementSpec) bool {
	t.Helper()
	switch sp.Children.Mode {
	case ModeLeaf, ModeNone:
		return false
	case ModeRestricted:
		for _, n := range sp.Children.Only {
			s, ok := (&Context{}).spec(n)
			if !ok || !s.Pseudo {
				return true
			}
		}
		return false
	}
	return true
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
// legalParent is the second route, and it is the SAME derivation
// readsAsData falls back to: the ModeRestricted element whose
// Children.Only names this one. Using the test's own helper rather than
// calling namingParent keeps this an independent statement of the
// answer instead of an echo of the implementation.
func TestARefusalNamesTheReaderWhenTheCatalogKnowsIt(t *testing.T) {
	var checked int
	for _, sp := range pseudoSpecs(t) {
		want := sp.ParsedBy
		if want == "" {
			want = legalParent(t, sp)
		}
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
		for _, u := range universalAttrs {
			// ASKED OF <Border>, not of the pseudo-element. Kind alone
			// answers "x" for Margin, whose Kind is KindString and whose
			// grammar is one, two or four whole numbers — so the generic
			// value made the destination refuse for a reason that has
			// nothing to do with the remedy. narrowerThanItsKind already
			// records that fact under "Border.Margin"; borrowing it is
			// cheaper and better guarded than a second table here, and a
			// pseudo-element declares nothing for such a row to key on.
			attr := fmt.Sprintf("%s=%q", u.Name, validLiteralFor(t, "Border", u))
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
					"destination accepts it:\n\t%v", sp.Name, u.Name, err)
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
// the same rule is how the wording drifts, and refuseUniversal's comment
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
		names := []string{"Behaviors", "Resources"}
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
