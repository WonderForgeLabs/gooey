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
