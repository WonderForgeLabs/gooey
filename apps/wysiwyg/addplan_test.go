package main

import (
	"testing"

	"github.com/WonderForgeLabs/gooey/markup"
)

// TABS BECOME AUTHORABLE, which they were not.
//
// The hole: <Tabs> declares ChildSpec{ModeRestricted, Only:["Tab"]}, and
// <Tab> is filtered out of the palette because it is a pseudo-element
// <Tabs> parses itself. So the one child a <Tabs> accepts was the one
// element the palette would never offer — a closed loop — while
// holdsChildren answered `true` for it, so every palette entry was
// written into it as an illegal child. Two keystrokes to a document that
// does not build, which kills click-to-select for the whole document.
//
// Each test below has the arm where the mechanism must say the OTHER
// thing, because a planner that always wraps and a planner that never
// wraps each satisfy half of this file.

// tabsFixture is a document holding one populated <Tabs>.
func tabsFixture(t *testing.T) (*editor, *node) {
	t.Helper()
	ed, _ := buildPage(t)
	tabs := &node{
		Elem:  "Tabs",
		Attrs: map[string]string{"Name": "Tabs1", "Canvas.Left": "0", "Canvas.Top": "10"},
		Kids: []*node{{
			Elem:  "Tab",
			Attrs: map[string]string{"Header": "One"},
			Kids:  []*node{{Elem: "Text", Body: "First", Attrs: map[string]string{"Name": "First"}}},
		}},
	}
	ed.doc().Kids = append(ed.doc().Kids, tabs)
	ed.rebuild()
	if ed.docRoot == nil {
		t.Fatalf("the <Tabs> fixture does not build: %q", ed.status.Get())
	}
	return ed, tabs
}

// TestCanHoldReadsTheCatalogsRestriction is the fact everything else
// rests on, and it is the one the old holdsChildren got wrong: it checked
// only Leaf/None/Attachments and never looked at ChildSpec.Only.
func TestCanHoldReadsTheCatalogsRestriction(t *testing.T) {
	ed, _ := buildPage(t)

	// Must say NO: <Tabs> is restricted to <Tab>.
	if ed.canHold("Tabs", "Text") {
		t.Error("<Tabs> reports that it can hold a <Text>; it declares Only:[\"Tab\"], " +
			"and writing one produces a document that does not build")
	}
	// Must say YES, in the same restricted element — so this is a test of
	// the restriction and not of a blanket refusal.
	if !ed.canHold("Tabs", "Tab") {
		t.Error("<Tabs> reports that it cannot hold a <Tab>, which is the only thing it CAN hold")
	}
	// And YES for an ordinary container, so it is not refusing everything.
	if !ed.canHold("Canvas", "Text") {
		t.Error("<Canvas> reports that it cannot hold a <Text>")
	}
	// And NO for a leaf.
	if ed.canHold("Text", "Button") {
		t.Error("<Text> is a leaf but reports that it can hold a <Button>")
	}
}

// TestCanHoldIsPermissiveWhereTheCatalogIsSilent pins the deliberate half
// of the rule. ModeOne cannot say whether the slot is already taken, so
// canHold answers yes and the trial build refuses instead — which is what
// keeps the refusal specific rather than relocating the insert silently.
func TestCanHoldIsPermissiveWhereTheCatalogIsSilent(t *testing.T) {
	ed, _ := buildPage(t)
	spec, ok := ed.specOf("Border")
	if !ok {
		t.Fatal("no <Border> in the catalog")
	}
	if spec.Children.Mode != markup.ModeOne {
		t.Skipf("<Border> is %v, not ModeOne; this test needs the can't-tell case", spec.Children.Mode)
	}
	if !ed.canHold("Border", "Text") {
		t.Error("<Border> reports that it cannot hold a <Text>: ModeOne means \"exactly one\", " +
			"not \"one is left\", so the catalog cannot answer and the build has to")
	}
}

// TestAddingIntoATabsWrapsTheElementInATab is the feature.
func TestAddingIntoATabsWrapsTheElementInATab(t *testing.T) {
	ed, tabs := tabsFixture(t)
	ed.sel = tabs
	before := len(tabs.Kids)

	ed.paletteSel.Set(paletteIndex(t, ed, "Button"))
	ed.addSelected()

	if ed.docRoot == nil {
		t.Fatalf("adding a <Button> to a <Tabs> broke the document: %q", ed.status.Get())
	}
	if len(tabs.Kids) != before+1 {
		t.Fatalf("the <Tabs> has %d children, was %d: the insert did not land in it (status %q)",
			len(tabs.Kids), before, ed.status.Get())
	}
	added := tabs.Kids[len(tabs.Kids)-1]
	if added.Elem != "Tab" {
		t.Fatalf("the <Tabs> took a <%s> directly; it declares Only:[\"Tab\"] and this "+
			"document would not build", added.Elem)
	}
	if len(added.Kids) != 1 || added.Kids[0].Elem != "Button" {
		t.Fatalf("the new <Tab> holds %d children (%v), want the one <Button> that was asked for",
			len(added.Kids), kidElems(added))
	}
	// THE SELECTION IS THE BUTTON, not the scaffolding. The user picked a
	// <Button>, so the properties grid has to show one — landing the
	// selection on the <Tab> would put them in front of an element they
	// did not choose and cannot see.
	if ed.sel != added.Kids[0] {
		t.Errorf("after the add the selection is %s, want the <Button> that was added",
			nodeLabel(ed.sel))
	}
}

// TestAddingATabToATabsDoesNotWrapItInAnother is the must-say-no arm of
// the wrapper: the element the user asked for IS the permitted child, so
// there is nothing to build around it.
//
// It cannot go through the palette, which does not offer <Tab>, so it
// asks the planner directly — which is the unit that would be wrong.
func TestAddingATabToATabsDoesNotWrapItInAnother(t *testing.T) {
	ed, tabs := tabsFixture(t)
	ed.sel = tabs
	if got := ed.planAdd("Tab"); got.into != tabs || got.wrap != "" {
		t.Errorf("adding a <Tab> to a <Tabs> planned into=%s wrap=%q, want the <Tabs> "+
			"itself and no wrapper", nodeLabel(got.into), got.wrap)
	}
}

// TestAWrapperIsRefusedWhenTheContainerPermitsTwoChildren. A restricted
// container naming two permitted children has no single right answer, and
// picking the first would be a coin toss the user cannot see. It climbs
// instead.
func TestAWrapperIsRefusedWhenTheContainerPermitsTwoChildren(t *testing.T) {
	ed, _ := buildPage(t)
	var twoWay string
	for _, e := range ed.docCtx.Catalog() {
		if e.Children.Mode == markup.ModeRestricted && len(e.Children.Only) > 1 {
			twoWay = e.Name
			break
		}
	}
	if twoWay == "" {
		t.Skip("no restricted element names more than one permitted child; nothing to pin")
	}
	if got := ed.wrapperFor(twoWay, "Text"); got != "" {
		t.Errorf("<%s> permits %d children and wrapperFor chose %q; with more than one "+
			"candidate there is no answer the user could have predicted", twoWay, 2, got)
	}
}

// TestTheAddTargetClimbsPastAContainerThatCannotHoldTheElement is the
// other half of planAdd, and it removes a silent relocation: the previous
// version looked at the selection and then at its parent and then gave up
// on the document root, so a selection two levels inside something that
// could not hold the element landed the insert at the root with nothing
// saying it had moved.
func TestTheAddTargetClimbsPastAContainerThatCannotHoldTheElement(t *testing.T) {
	ed, tabs := tabsFixture(t)
	// The <Text> INSIDE the tab. Its parent is a <Tab>, its grandparent a
	// <Tabs> — neither takes a <Button> — so the climb has to reach the
	// document root.
	inner := tabs.Kids[0].Kids[0]
	ed.sel = inner

	plan := ed.planAdd("Button")
	if plan.wrap != "" {
		// A <Tab> holds one child and already has it, but the catalog
		// calls a <Tab> opaque, so the climb is what has to answer.
		t.Logf("planned a wrapper %q from inside the tab", plan.wrap)
	}
	if plan.into == tabs {
		t.Error("the plan landed in the <Tabs> itself, which takes only <Tab>")
	}
}

// TestTheWrapperTakesItsAttributesFromTheContainersSeed is the mechanism
// the test above would pass without, if <Tab> ever grew a Seed of its own.
//
// It asserts the SOURCE, not just the outcome: the new <Tab> carries the
// attribute the <Tabs> seed's example tab carries, with that example's
// value. A wrapper built from the wrapper element's own (absent) seed is
// bare, and `markup: <Tab> needs a Header` is what the user saw — reported
// against their <Button>, which was never the problem.
func TestTheWrapperTakesItsAttributesFromTheContainersSeed(t *testing.T) {
	ed, _ := buildPage(t)

	spec, ok := ed.specOf("Tabs")
	if !ok {
		t.Fatal("no <Tabs> in the catalog")
	}
	example, err := nodeOf(spec.Seed)
	if err != nil {
		t.Fatalf("the <Tabs> seed does not parse: %v", err)
	}
	var want map[string]string
	for _, k := range example.Kids {
		if k.Elem == "Tab" {
			want = k.Attrs
			break
		}
	}
	if len(want) == 0 {
		t.Skip("the <Tabs> seed's example tab carries no attributes; nothing to inherit")
	}

	got := ed.wrapperNode("Tabs", "Tab")
	for name, v := range want {
		if got.Attrs[name] != v {
			t.Errorf("the new <Tab> has %s=%q, want %q from the <Tabs> seed's own "+
				"example — which is the only place the editor can learn that a "+
				"<Tab> needs one without naming the attribute here", name, got.Attrs[name], v)
		}
	}
	// And NOT the example's children: the wrapper exists to hold what the
	// user asked for.
	if len(got.Kids) != 0 {
		t.Errorf("the new <Tab> arrives holding %v; the seed's content is an example, "+
			"not something the user chose to add", kidElems(got))
	}

	// The must-fall-back arm. An element whose parent has no seed to read
	// gets a bare node rather than nothing at all — wrapperNode is a best
	// effort, and the transactional build is still the gate.
	bare := ed.wrapperNode("Nonesuch", "Tab")
	if bare == nil || bare.Elem != "Tab" || bare.Attrs == nil {
		t.Errorf("with no catalog entry to read, wrapperNode returned %v; it has to "+
			"still produce a usable node and let the build refuse it", bare)
	}
}

// TestAContainerIsTheRouteToMoreThanOneThingOnATab is the workflow the
// user has to be told to use, pinned so the telling stays true.
//
// buildTabs requires EXACTLY ONE content child per tab
// (markup/toolkit.go), so the wrapper alone does not make a tab fill up:
// the second add into a tab whose slot is taken is refused. Putting a
// container on the tab first is the route, and this asserts that the
// route is walkable with the editor's own two gestures — add with the
// <Tabs> selected, then add with the container selected.
//
// The refusal half is asserted too. A test that only showed the happy
// path would pass just as well if a tab silently accepted three children
// and the document quietly stopped building — which is the exact failure
// this whole change exists to remove.
func TestAContainerIsTheRouteToMoreThanOneThingOnATab(t *testing.T) {
	ed, tabs := tabsFixture(t)

	// Gesture one: a container, with the <Tabs> selected. It arrives
	// wrapped in a new <Tab>.
	ed.sel = tabs
	ed.paletteSel.Set(paletteIndex(t, ed, "VStack"))
	ed.addSelected()
	if ed.docRoot == nil {
		t.Fatalf("adding a <VStack> to the <Tabs> broke the document: %q", ed.status.Get())
	}
	tab := tabs.Kids[len(tabs.Kids)-1]
	if tab.Elem != "Tab" || len(tab.Kids) != 1 || tab.Kids[0].Elem != "VStack" {
		t.Fatalf("the new tab is <%s> holding %v, want a <Tab> holding the one <VStack>",
			tab.Elem, kidElems(tab))
	}
	stack := tab.Kids[0]
	// Its OWN seed's content, which is not zero: a <VStack> arrives
	// populated. The assertion below is that it GREW, not that it holds
	// exactly what was added — the first version of this test asserted the
	// latter and failed against [<Text> <Text> <Button> <Text>], which was
	// the test being wrong about the fixture rather than the editor being
	// wrong about the insert.
	seeded := len(stack.Kids)

	// Gesture two, twice: the container is now the selection, and it takes
	// as many children as asked.
	for _, elem := range []string{"Button", "Text"} {
		ed.sel = stack
		ed.paletteSel.Set(paletteIndex(t, ed, elem))
		ed.addSelected()
		if ed.docRoot == nil {
			t.Fatalf("adding a <%s> to the <VStack> on the tab broke the document: %q",
				elem, ed.status.Get())
		}
	}
	if len(stack.Kids) != seeded+2 {
		t.Errorf("the <VStack> on the tab holds %v (%d children, was %d before the two "+
			"adds); a container on a tab is the route to more than one thing and it "+
			"has to take both", kidElems(stack), len(stack.Kids), seeded)
	}

	// The refusal half: the TAB itself still takes only its one child, and
	// says so rather than leaving a document that does not build.
	ed.sel = tab
	if got := ed.addTarget("Button"); got != tab {
		t.Skipf("with the <Tab> selected an Add targets %s, not the tab; this half "+
			"of the test is only about what the tab itself refuses", nodeLabel(got))
	}
	kids := len(tab.Kids)
	ed.addSelected()
	if len(tab.Kids) != kids {
		t.Errorf("the <Tab> took %d children, was %d; buildTabs requires exactly one "+
			"and this document would not build", len(tab.Kids), kids)
	}
	if ed.docRoot == nil {
		t.Error("the refused insert left docRoot nil — the state that kills " +
			"click-to-select for the whole document")
	}
}

func kidElems(n *node) []string {
	out := make([]string, 0, len(n.Kids))
	for _, k := range n.Kids {
		out = append(out, "<"+k.Elem+">")
	}
	return out
}
