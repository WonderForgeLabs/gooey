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
}

var wholeLoadCases = map[string]wholeLoad{
	"Tab": {
		loads:     `<Gooey><Tabs><Tab Header="a"><Text>x</Text></Tab></Tabs></Gooey>`,
		refused:   `<Gooey><Tabs><Tab Header="a" Name="Zonk"><Text>x</Text></Tab></Tabs></Gooey>`,
		attr:      "Name",
		misplaced: `<Gooey><VStack><Tab Header="a" Name="Zonk"><Text>x</Text></Tab></VStack></Gooey>`,
	},
	"Menu": {
		loads:     `<Gooey><MenuBar><Menu Title="F"><MenuItem Text="Open"/></Menu></MenuBar></Gooey>`,
		refused:   `<Gooey><MenuBar><Menu Title="F" Name="Zonk"><MenuItem Text="Open"/></Menu></MenuBar></Gooey>`,
		attr:      "Name",
		misplaced: `<Gooey><VStack><Menu Title="F" Name="Zonk"><MenuItem Text="Open"/></Menu></VStack></Gooey>`,
	},
	"MenuItem": {
		loads:     `<Gooey><MenuBar><Menu Title="F"><MenuItem Text="Open"/></Menu></MenuBar></Gooey>`,
		refused:   `<Gooey><MenuBar><Menu Title="F"><MenuItem Text="Open" Margin="2"/></Menu></MenuBar></Gooey>`,
		attr:      "Margin",
		misplaced: `<Gooey><VStack><MenuItem Text="Open" Margin="2"/></VStack></Gooey>`,
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
		checked++
		tc := wholeLoadCases[sp.Name]
		_, err := Build([]byte(tc.refused), &Context{})
		if err == nil {
			t.Errorf("<%s %s=…> was not refused at all", sp.Name, tc.attr)
			continue
		}
		if strings.Contains(err.Error(), "content inside") {
			t.Errorf("nothing <%s> may contain (%s) would accept %s, and its "+
				"refusal tells the author to put the attribute on the content "+
				"inside — a remedy whose destination refuses it too:\n\t%v",
				sp.Name, sp.Children.Mode, tc.attr, err)
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
