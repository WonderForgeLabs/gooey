package main

import (
	"strings"
	"testing"
)

// Round 8 of #454's review, on the transactional revert round 7 asked
// for. Both findings are about what the revert LEAVES BEHIND.

// TestARefusedCutLeavesNothingOnTheClipboard.
//
// Cut is copy-then-delete. deletable() answers the refusals deleteSelected
// can see before trying, but not the one only the loader can: removing a
// child can make its PARENT illegal, and that is discovered by building.
// So cutSelected wrote the clipboard, called deleteSelected, and then
// overwrote its refusal with "cut <Text>" — leaving the node on the page
// AND on the clipboard, so the next paste duplicates it under a colliding
// Name, which is the one thing markup.Find cannot resolve.
func TestARefusedCutLeavesNothingOnTheClipboard(t *testing.T) {
	ed, _ := moveFixture(t)
	only := &node{Elem: "Text", Body: "inside", Attrs: map[string]string{"Name": "Inside"}}
	tabs := &node{Elem: "Tabs", Attrs: map[string]string{"Name": "Tabs1"}, Kids: []*node{
		{Elem: "Tab", Attrs: map[string]string{"Name": "Tab1", "Header": "One"}, Kids: []*node{only}},
	}}
	ed.doc().Kids = []*node{tabs}
	ed.rebuild()
	if ed.docRoot == nil {
		t.Fatalf("fixture does not build: %s", ed.status.Get())
	}

	ed.sel = only
	ed.cutSelected()

	if ed.clip.node != nil {
		t.Error("a refused cut put the node on the clipboard; the next paste duplicates it")
	}
	if !strings.HasPrefix(ed.status.Get(), "✗") {
		t.Errorf("status is %q after a refused cut, want a refusal", ed.status.Get())
	}
	if len(tabs.Kids[0].Kids) != 1 {
		t.Errorf("the refused cut was not reverted: <Tab> holds %d children",
			len(tabs.Kids[0].Kids))
	}
	if ed.docRoot == nil {
		t.Error("docRoot is nil after a refused cut")
	}
}

// TestACutTheLoaderAcceptsStillCuts is the arm that stops the guard above
// being satisfied by refusing every cut.
func TestACutTheLoaderAcceptsStillCuts(t *testing.T) {
	ed, _ := moveFixture(t)
	b := ed.doc().Kids[1]

	ed.sel = b
	ed.cutSelected()

	if ed.clip.node == nil {
		t.Fatal("an accepted cut put nothing on the clipboard")
	}
	if got := kidNames(ed.doc()); got != "A,C" {
		t.Errorf("children are %q after cutting B, want \"A,C\"", got)
	}
	if !strings.HasPrefix(ed.status.Get(), "cut ") {
		t.Errorf("status is %q after a successful cut", ed.status.Get())
	}
}

// TestARefusedMutationLeavesNothingOnTheUndoStack.
//
// Every mutator ends in a rebuild, and rebuild is where history is
// recorded — so a revert recorded TWICE: once for the broken intermediate
// the loader rejected, once for the restore. The stack then held the
// broken state, and one ctrl+z after a refusal walked the user straight
// back into it: docRoot nil, click-to-select dead for the whole document.
// That is the crash the revert exists to prevent, reached through the
// undo key.
//
// The assertion is docRoot AFTER the undo, not the depth of the stack. A
// stack-depth check passes for a history that recorded the right number
// of wrong states, and the crash is what the user meets.
func TestARefusedMutationLeavesNothingOnTheUndoStack(t *testing.T) {
	ed, _ := moveFixture(t)
	only := &node{Elem: "Text", Body: "inside", Attrs: map[string]string{"Name": "Inside"}}
	tabs := &node{Elem: "Tabs", Attrs: map[string]string{"Name": "Tabs1"}, Kids: []*node{
		{Elem: "Tab", Attrs: map[string]string{"Name": "Tab1", "Header": "One"}, Kids: []*node{only}},
	}}
	// The state BEFORE the only real edit in this test, which is the
	// fixture's own. Exactly one undo step should exist and it should be
	// that one — so undo lands here, not on the broken intermediate.
	beforeFixture := ed.doc().markup("")

	ed.doc().Kids = []*node{tabs}
	ed.rebuild()
	if ed.docRoot == nil {
		t.Fatalf("fixture does not build: %s", ed.status.Get())
	}

	ed.sel = only
	ed.deleteSelected() // refused, and reverted

	ed.undo()

	if ed.docRoot == nil {
		t.Fatal("ctrl+z after a refused delete re-entered the broken state — " +
			"docRoot is nil and click-to-select is dead for the whole document")
	}
	// Landing on the fixture's predecessor proves the refused delete
	// consumed no step: had it recorded one, this undo would have spent
	// itself on the broken intermediate and left the <Tabs> standing here.
	if got := ed.doc().markup(""); got != beforeFixture {
		t.Errorf("ctrl+z after a refused delete did not undo the last REAL edit; "+
			"the refused one is still on the stack.\ngot:\n%s\nwant:\n%s",
			got, beforeFixture)
	}
}

// TestUndoStillUndoesTheEditBeforeARefusedOne. The abort must remove the
// REFUSED step and nothing else — a fix that cleared the stack, or that
// re-baselined over a real edit, would pass the test above and lose work.
func TestUndoStillUndoesTheEditBeforeARefusedOne(t *testing.T) {
	ed, _ := moveFixture(t)
	only := &node{Elem: "Text", Body: "inside", Attrs: map[string]string{"Name": "Inside"}}
	tabs := &node{Elem: "Tabs", Attrs: map[string]string{"Name": "Tabs1"}, Kids: []*node{
		{Elem: "Tab", Attrs: map[string]string{"Name": "Tab1", "Header": "One"}, Kids: []*node{only}},
	}}
	ed.doc().Kids = []*node{tabs}
	ed.rebuild()

	// A REAL edit first, which ctrl+z must still reach.
	beforeReal := ed.doc().markup("")
	tabs.Attrs["Name"] = "Renamed"
	ed.rebuild()
	if ed.doc().markup("") == beforeReal {
		t.Fatal("the real edit changed nothing; nothing below was tested")
	}

	// Then a refused one.
	ed.sel = only
	ed.deleteSelected()

	ed.undo()

	if ed.docRoot == nil {
		t.Fatal("docRoot is nil after undo")
	}
	if got := ed.doc().markup(""); got != beforeReal {
		t.Errorf("ctrl+z did not undo the real edit that preceded the refused one.\n"+
			"got:\n%s\nwant:\n%s", got, beforeReal)
	}
}
