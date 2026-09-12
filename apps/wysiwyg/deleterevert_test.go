package main

import (
	"strings"
	"testing"
)

// TestDeletingTheLastChildOfATabIsRefusedRatherThanKillingTheDocument.
//
// deleteSelected was the one mutator with no transactional revert. Its five
// siblings — promote, demote, move, paste, add — all rebuild and then check
// `ed.docRoot == nil`, putting the tree back when the loader refuses it
// (#403, and again for paste in this PR). Delete unlinked, rebuilt, and
// returned, so a delete the loader refuses reported success and left docRoot
// nil, which kills click-to-select for the WHOLE document while the last
// good tree stays on screen looking pressable.
//
// The reachable case is not exotic: `<Tab Header=… needs exactly one content
// child, got 0` (markup/toolkit.go:190) fires the moment you delete a tab's
// only child, which is one ctrl+x on a node the outline offers you.
//
// The assertion that discriminates is docRoot, NOT the status line and NOT
// the tree shape. A revert that puts the node back but never rebuilds leaves
// the tree correct and docRoot still nil, and only this check sees it.
func TestDeletingTheLastChildOfATabIsRefusedRatherThanKillingTheDocument(t *testing.T) {
	ed, _ := moveFixture(t)
	only := &node{Elem: "Text", Body: "inside", Attrs: map[string]string{"Name": "Inside"}}
	tabs := &node{Elem: "Tabs", Attrs: map[string]string{"Name": "Tabs1"}, Kids: []*node{
		// NO Name ON THE <Tab>: it is a pseudo-element, so the universal
		// set is a load error on it (#461). It carried one until that
		// landed, accepted and dropped, and nothing here ever read it —
		// these tests select by node pointer.
		{Elem: "Tab", Attrs: map[string]string{"Header": "One"}, Kids: []*node{only}},
	}}
	ed.doc().Kids = []*node{tabs}
	ed.rebuild()
	if !strings.HasPrefix(ed.status.Get(), "✓") {
		t.Fatalf("fixture does not build: %s", ed.status.Get())
	}
	if ed.docRoot == nil {
		t.Fatal("fixture built no docRoot, so the assertion below could not fail")
	}

	ed.sel = only
	ed.deleteSelected()

	if ed.docRoot == nil {
		t.Fatal("docRoot is nil after a refused delete — click-to-select is dead " +
			"for the whole document while the last good tree is still on screen")
	}
	if len(tabs.Kids) != 1 || len(tabs.Kids[0].Kids) != 1 || tabs.Kids[0].Kids[0] != only {
		t.Errorf("the refused delete was not reverted: <Tab> holds %d children",
			len(tabs.Kids[0].Kids))
	}
	if !strings.HasPrefix(ed.status.Get(), "✗") {
		t.Errorf("status is %q after a refused delete, want a refusal", ed.status.Get())
	}
	if ed.sel != only {
		t.Errorf("selection is %s after a refused delete, want the node that was not deleted",
			nodeName(ed.sel))
	}
}

// TestDeletingAChildTheLoaderAcceptsStillDeletesIt is the other arm, and it
// is what stops the guard above being satisfied by refusing everything.
func TestDeletingAChildTheLoaderAcceptsStillDeletesIt(t *testing.T) {
	ed, _ := moveFixture(t)
	b := ed.doc().Kids[1]

	ed.sel = b
	ed.deleteSelected()

	if got := kidNames(ed.doc()); got != "A,C" {
		t.Fatalf("children are %q after deleting B, want \"A,C\"", got)
	}
	if ed.docRoot == nil {
		t.Fatal("docRoot is nil after a delete the loader accepts")
	}
	if ed.sel != ed.doc().Kids[1] {
		t.Errorf("selection is %s, want the node that took B's place", nodeName(ed.sel))
	}
}
