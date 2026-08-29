package main

import (
	"strings"
	"testing"
)

// TestDuplicateCopiesTheSubtreeAndRenamesEveryNode.
//
// The rename is the feature, not tidiness. Name is what markup.Find
// resolves against, so a copy that kept its original's names would build
// green and leave half the document unaddressable — the exact failure
// the counting namer shipped.
func TestDuplicateCopiesTheSubtreeAndRenamesEveryNode(t *testing.T) {
	ed, _ := moveFixture(t)
	inner := &node{Elem: "VStack", Attrs: map[string]string{"Name": "Inner"}, Kids: []*node{
		{Elem: "Text", Body: "ddd", Attrs: map[string]string{"Name": "D", "Bold": "true"}},
	}}
	ed.doc().Kids = []*node{ed.doc().Kids[0], inner, ed.doc().Kids[2]}
	ed.rebuild()

	ed.sel = inner
	if !ed.duplicateSelected() {
		t.Fatal("duplicate reported no change")
	}

	// Immediately after the original, not appended.
	if len(ed.doc().Kids) != 4 {
		t.Fatalf("the root holds %d children after a duplicate, want 4", len(ed.doc().Kids))
	}
	c := ed.doc().Kids[2]
	if c == inner {
		t.Fatal("the node after the original IS the original: nothing was inserted")
	}

	// Deep, not shallow.
	if len(c.Kids) != 1 {
		t.Fatalf("the copy holds %d children, want 1", len(c.Kids))
	}
	if c.Kids[0] == inner.Kids[0] {
		t.Error("the copy's child is the SAME node as the original's: a shallow copy " +
			"means editing one edits both, and the document serialises the same " +
			"subtree twice")
	}

	// Attributes carried, names not.
	if c.Kids[0].Attrs["Bold"] != "true" {
		t.Errorf("the copy's child lost Bold: got %q", c.Kids[0].Attrs["Bold"])
	}
	if c.Kids[0].Body != "ddd" {
		t.Errorf("the copy's child lost its body: got %q", c.Kids[0].Body)
	}

	seen := map[string]int{}
	var walk func(*node)
	walk = func(n *node) {
		if s := n.Attrs["Name"]; s != "" {
			seen[s]++
		}
		for _, k := range n.Kids {
			walk(k)
		}
	}
	walk(ed.doc())
	for name, n := range seen {
		if n > 1 {
			t.Errorf("%d nodes are called %q after a duplicate:\n%s", n, name, ed.outline())
		}
	}

	if ed.sel != c {
		t.Errorf("selection is %s after duplicate, want the COPY: the point of "+
			"duplicating is to then change the copy, and leaving the selection on "+
			"the original makes the next edit silently hit the wrong node",
			nodeName(ed.sel))
	}
	if !strings.HasPrefix(ed.status.Get(), "✓") {
		t.Errorf("the document does not build after a duplicate: %s", ed.status.Get())
	}
}

// TestDuplicateRefusesTheUsersRoot — a second root is a different
// document, the same rule the move operations enforce.
func TestDuplicateRefusesTheUsersRoot(t *testing.T) {
	ed, _ := moveFixture(t)
	ed.sel = ed.doc()

	if ed.duplicateSelected() {
		t.Error("duplicating the user's root reported a change")
	}
	if len(ed.root.Kids) != 1 {
		t.Fatalf("the surface holds %d children, want exactly 1", len(ed.root.Kids))
	}
}

// TestUniqueNameSkipsWhatIsInUseRatherThanCounting is the unit that the
// palette bug was missing.
//
// The discriminating case is a GAP: names 1 and 3 taken, 2 free. A
// counting namer returns 3 (len+1) and collides; a set-based one returns
// the free 2. Asserting only "the result is unused" would pass for a
// namer that always jumped to a huge number, so the exact value matters.
func TestUniqueNameSkipsWhatIsInUseRatherThanCounting(t *testing.T) {
	root := &node{Elem: "Canvas", Kids: []*node{
		{Elem: "Text", Attrs: map[string]string{"Name": "Text1"}},
		{Elem: "Text", Attrs: map[string]string{"Name": "Text3"}},
	}}
	if got := uniqueName(root, "Text"); got != "Text2" {
		t.Errorf("uniqueName returned %q with Text1 and Text3 in use, want the free \"Text2\"", got)
	}

	// And it must see names at ANY depth, not just among one parent's
	// children — the editor promotes nodes between containers.
	deep := &node{Elem: "Canvas", Kids: []*node{
		{Elem: "VStack", Attrs: map[string]string{"Name": "V1"}, Kids: []*node{
			{Elem: "Text", Attrs: map[string]string{"Name": "Text1"}},
		}},
	}}
	if got := uniqueName(deep, "Text"); got != "Text2" {
		t.Errorf("uniqueName returned %q with a nested Text1 in use, want \"Text2\": "+
			"a namer that only looks at top-level children hands out a name that "+
			"collides the moment something is promoted", got)
	}
}

// TestNamesInUseSeesInsideSlots.
//
// A slot — <ItemsView.ItemTemplate> — serializes into the same document
// and is resolved by the same markup.Find, so a Name in there is taken.
// namesInUse walked only Kids, which made "unique per document" quietly
// mean "unique per document, excluding slots": uniqueName would hand out
// a name a slot already claims, and the collision builds green.
func TestNamesInUseSeesInsideSlots(t *testing.T) {
	root := &node{Elem: "Canvas", Kids: []*node{
		{Elem: "ItemsView", Attrs: map[string]string{"Name": "List"}, Slots: map[string]*node{
			"ItemTemplate": {Elem: "Text", Attrs: map[string]string{"Name": "Text1"}},
		}},
	}}
	if !namesInUse(root)["Text1"] {
		t.Error("namesInUse missed Text1 inside an ItemTemplate slot")
	}
	if got := uniqueName(root, "Text"); got != "Text2" {
		t.Errorf("uniqueName returned %q with Text1 living in a slot, want \"Text2\": "+
			"a namer blind to slots re-issues a name the document already resolves", got)
	}
}

// TestDuplicateCarriesSlotsRatherThanDroppingThem.
//
// The catalog can mark a slot REQUIRED, and the toolbox seeds one on the
// way in, so a copy that dropped Slots serialized as a bare <ItemsView/>
// — a duplicate that silently deleted half of what it copied, and left a
// document that no longer satisfies the element's own contract.
func TestDuplicateCarriesSlotsRatherThanDroppingThem(t *testing.T) {
	used := map[string]bool{"List": true, "Text1": true}
	orig := &node{Elem: "ItemsView", Attrs: map[string]string{"Name": "List"}, Slots: map[string]*node{
		"ItemTemplate": {Elem: "Text", Body: "row", Attrs: map[string]string{"Name": "Text1"}},
	}}

	c := clone(orig, used)

	sl, ok := c.Slots["ItemTemplate"]
	if !ok {
		t.Fatalf("the copy has no ItemTemplate slot: clone dropped it, so the copy "+
			"serialises as <%s/> and no longer satisfies the element's required slot", c.Elem)
	}
	if sl == orig.Slots["ItemTemplate"] {
		t.Error("the copy's slot is the SAME node as the original's: a shallow copy " +
			"means editing one edits both")
	}
	if sl.Body != "row" {
		t.Errorf("the copy's slot lost its body: got %q", sl.Body)
	}
	// Renamed, for the same reason every other copied node is.
	if sl.Attrs["Name"] == "Text1" {
		t.Error("the copy's slot kept Name=\"Text1\": two nodes answer to one address " +
			"and markup.Find resolves the wrong one")
	}
	if sl.Attrs["Name"] == "" {
		t.Error("the copy's slot lost its Name entirely; it should have been renamed, not dropped")
	}
}

// TestDuplicateIsRevertedWhenTheCopyOverfillsTheParent is the arm no catalog
// gate could ever cover.
//
// The copy lands beside the original, so the parent provably accepts this
// element and canHold can only say yes. What fails is the COUNT: a
// <Tab Header="One"> takes exactly one content child, and the duplicate makes
// two. Before the fix this left docRoot nil — click-to-select dead for the
// whole document — and returned true.
func TestDuplicateIsRevertedWhenTheCopyOverfillsTheParent(t *testing.T) {
	ed, tabs := tabsFixture(t)
	tab := tabs.Kids[0]
	text := tab.Kids[0]
	ed.sel = text

	if ed.duplicateSelected() {
		t.Errorf("duplicating the one content child of a <Tab> reported success")
	}
	if len(tab.Kids) != 1 || tab.Kids[0] != text {
		t.Errorf("the <Tab> holds %v after the revert, want its single original <Text>",
			kidElems(tab))
	}
	// The SELECTION comes back too. duplicateSelected moves it to the copy
	// as its whole point, so a revert that left it on a node no longer in
	// the document would leave every following edit addressing a ghost.
	if ed.sel != text {
		t.Errorf("after the revert the selection is %s, want the original <Text>",
			nodeLabel(ed.sel))
	}
	if ed.docRoot == nil {
		t.Errorf("the document did not build again after the revert: %s", ed.status.Get())
	}
	if s := ed.status.Get(); !strings.HasPrefix(s, "✗") {
		t.Errorf("a refused duplicate left status %q, want a ✗ message saying why", s)
	}
}

// TestDuplicateStillWorksWhereTheParentHasRoom is the must-say-YES arm. A
// revert that fired on every duplicate would pass the test above and break
// the feature.
func TestDuplicateStillWorksWhereTheParentHasRoom(t *testing.T) {
	ed, tabs := tabsFixture(t)
	ed.sel = tabs.Kids[0]
	before := len(tabs.Kids)

	if !ed.duplicateSelected() {
		t.Fatalf("duplicating a <Tab> inside a <Tabs> was refused: %s", ed.status.Get())
	}
	if len(tabs.Kids) != before+1 {
		t.Errorf("the <Tabs> holds %d children, want %d", len(tabs.Kids), before+1)
	}
	if ed.sel == nil || ed.sel == tabs.Kids[0] {
		t.Errorf("the selection did not move to the copy")
	}
	if ed.docRoot == nil {
		t.Errorf("a legal duplicate broke the document: %s", ed.status.Get())
	}
}
