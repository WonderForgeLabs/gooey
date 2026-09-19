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

// nodeNamed finds a node by its Name attribute. The tests below cannot
// hold pointers across an undo: restore replaces ed.root wholesale with a
// fresh clone, so every pointer taken before it dangles.
func nodeNamed(root *node, name string) *node {
	var found *node
	walkNode(root, func(n *node) {
		if found == nil && n.Attrs["Name"] == name {
			found = n
		}
	})
	return found
}

// TestARefusedCutLeavesTheSYSTEMClipboardAlone is the half the first fix
// missed, and it is the half that routes back into the document.
//
// cutSelected built its status message above the delete guard, and
// sayCopiedOut is not a formatter — it calls copyToSystem, which writes
// the OSC 52. So a refused cut left the node on the page and its markup
// on the SYSTEM clipboard, which the terminal's own paste key feeds
// straight back through bindClipboardTo → pasteMarkup → insertSubtree.
// The quieter half: the user's clipboard was overwritten while the status
// line read "✗ … cannot be deleted", against sayCopiedOut's own "never
// silent in either direction".
//
// COUNTING CALLS, not comparing text. Asserting the captured text is not
// the cut markup passes when the writer was called with something else,
// and this fixture's document has only one thing worth copying. Raised in
// review of #454.
func TestARefusedCutLeavesTheSYSTEMClipboardAlone(t *testing.T) {
	ed, f := clipEditor(t)
	only := &node{Elem: "Text", Body: "inside", Attrs: map[string]string{"Name": "Inside"}}
	tabs := &node{Elem: "Tabs", Attrs: map[string]string{"Name": "Tabs1"}, Kids: []*node{
		{Elem: "Tab", Attrs: map[string]string{"Name": "Tab1", "Header": "One"}, Kids: []*node{only}},
	}}
	ed.doc().Kids = []*node{tabs}
	ed.rebuild()
	if ed.docRoot == nil {
		t.Fatalf("fixture does not build: %s", ed.status.Get())
	}
	before := f.calls

	ed.sel = only
	ed.cutSelected()

	if !strings.HasPrefix(ed.status.Get(), "✗") {
		t.Fatalf("status is %q after the cut, want a refusal — the loader accepted "+
			"this delete and the test is about nothing", ed.status.Get())
	}
	if f.calls != before {
		t.Errorf("a refused cut wrote the system clipboard %d time(s) with %q. The "+
			"node is still on the page, so the terminal's paste key duplicates it "+
			"under a colliding Name — and the user's clipboard was replaced while "+
			"the status line reported a refusal.", f.calls-before, f.last)
	}
}

// TestAnAcceptedCutStillWritesTheSystemClipboard stops the guard above
// being satisfied by never writing at all.
func TestAnAcceptedCutStillWritesTheSystemClipboard(t *testing.T) {
	ed, f := clipEditor(t)
	target := &node{Elem: "Text", Body: "b", Attrs: map[string]string{"Name": "B"}}
	ed.doc().Kids = []*node{
		{Elem: "Text", Body: "a", Attrs: map[string]string{"Name": "A"}},
		target,
	}
	ed.rebuild()
	before := f.calls

	ed.sel = target
	ed.cutSelected()

	if ed.clip.node == nil {
		t.Fatalf("an accepted cut put nothing on the internal clipboard: %s",
			ed.status.Get())
	}
	if f.calls != before+1 {
		t.Errorf("an accepted cut made %d system-clipboard write(s), want 1",
			f.calls-before)
	}
	if !strings.Contains(f.last, "Name=\"B\"") {
		t.Errorf("the system clipboard got %q, which is not the cut node's markup", f.last)
	}
}

// TestARefusedMutationLeavesTheRedoBranchAlone is round 8's "a revert
// must leave the history where it found it", applied to the OTHER stack.
//
// record clears h.redo in its push branch — the one place it is cleared,
// and correctly so — and the refused mutation's own rebuild goes through
// that branch, so the branch was already gone by the time the mutator
// reverted. abort popped the undo entry and never touched redo. So a
// gesture that was refused AND reported as refused still destroyed a
// state that was reachable a keystroke earlier: ctrl+y answered "nothing
// to redo".
//
// The existing revert tests assert through undo() only, so none of them
// can see this. Raised in review of #454.
func TestARefusedMutationLeavesTheRedoBranchAlone(t *testing.T) {
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

	// A real edit, then a real undo — which is what puts something on the
	// redo stack in the first place.
	ed.applyEdit("rename", func() { only.Attrs["Name"] = "Renamed" })
	ed.undo()
	if !ed.CanRedo() {
		t.Fatal("nothing on the redo stack after an undo; the fixture cannot show " +
			"a refusal destroying one")
	}
	depth := len(ed.history().redo)

	// The refusal: deleting the <Tab>'s only child leaves the parent
	// illegal, which only the loader can see.
	ed.sel = nodeNamed(ed.root, "Inside")
	if ed.sel == nil {
		t.Fatal("the undo did not bring the node back under a findable name")
	}
	if ed.deleteSelected() {
		t.Fatalf("the delete was accepted, so nothing here is a refusal: %s",
			ed.status.Get())
	}

	if !ed.CanRedo() {
		t.Error("a REFUSED mutation destroyed the redo branch: ctrl+y now answers " +
			"\"nothing to redo\" over a state that was reachable a keystroke ago, " +
			"and the gesture that took it was reported as refused")
	}
	if got := len(ed.history().redo); got != depth {
		t.Errorf("the redo stack is %d deep after a refused mutation, was %d", got, depth)
	}
}

// TestARealEditStillAbandonsTheRedoBranch is the invalidation half, and
// it is why record resets h.cleared on EVERY call rather than only where
// it clears redo. A stash that outlived the record that made it would let
// a later abort restore a branch the user had already edited past —
// which is the classic bug record's own comment describes from the other
// side, arriving through the fix for it. Raised in review of #454.
func TestARealEditStillAbandonsTheRedoBranch(t *testing.T) {
	ed, _ := moveFixture(t)
	only := &node{Elem: "Text", Body: "inside", Attrs: map[string]string{"Name": "Inside"}}
	tabs := &node{Elem: "Tabs", Attrs: map[string]string{"Name": "Tabs1"}, Kids: []*node{
		{Elem: "Tab", Attrs: map[string]string{"Name": "Tab1", "Header": "One"}, Kids: []*node{only}},
	}}
	ed.doc().Kids = []*node{tabs}
	ed.rebuild()

	ed.applyEdit("rename", func() { only.Attrs["Name"] = "Renamed" })
	ed.undo()
	if !ed.CanRedo() {
		t.Fatal("nothing on the redo stack after an undo")
	}

	// A real edit that STANDS. This is what abandons the branch.
	ed.applyEdit("retitle", func() {
		nodeNamed(ed.root, "Tabs1").Attrs["Name"] = "Tabs2"
	})
	if ed.CanRedo() {
		t.Fatal("a real edit did not abandon the redo branch; the assertion below " +
			"cannot distinguish a stale stash from a live one")
	}

	// Now a refusal. It must NOT resurrect the branch the edit abandoned.
	// "Inside", not "Renamed": the undo above put the rename back.
	ed.sel = nodeNamed(ed.root, "Inside")
	if ed.sel == nil {
		t.Fatal("the fixture lost the node the refusal is aimed at")
	}
	if ed.deleteSelected() {
		t.Fatalf("the delete was accepted: %s", ed.status.Get())
	}
	if ed.CanRedo() {
		t.Errorf("a refused mutation resurrected a redo branch the user had already "+
			"edited past: %d state(s) came back", len(ed.history().redo))
	}
}

// TestAnUnchangedRebuildDoesNotLeaveAStaleRedoStash is the arm for the
// reset at the top of record, and it is deliberately a unit test on
// history rather than a gesture.
//
// Measured first: driving this through the editor does NOT fire. Every
// abortHistory in the tree is preceded by the refused mutator's own
// rebuild, which changes the tree, so record reaches its push branch and
// re-stashes — cleared is freshly correct whether or not the reset is
// there, and the mutation removing it was SILENT against the whole
// wysiwyg suite. The reset is what makes that a property rather than a
// coincidence about today's call sites: a record that STANDS and is then
// followed by a rebuild changing nothing leaves an older branch in the
// stash, and the next abort restores it — the classic redo bug arriving
// through the fix for the other half of it.
//
// So the sequence below is stated directly: push, abandon the branch,
// rebuild-with-no-change, abort. Raised in review of #454.
func TestAnUnchangedRebuildDoesNotLeaveAStaleRedoStash(t *testing.T) {
	h := &history{limit: 10}
	one := &node{Elem: "VStack", Attrs: map[string]string{"Name": "A"}}
	two := &node{Elem: "VStack", Attrs: map[string]string{"Name": "B"}}
	three := &node{Elem: "VStack", Attrs: map[string]string{"Name": "C"}}

	h.record(one, nil, false) // the baseline
	h.record(two, nil, false) // an edit
	// The history half of ed.undo(), inline: the editor's version also
	// restores the tree, which is not what this is about.
	h.redo = append(h.redo, h.base)
	h.base = h.undo[len(h.undo)-1]
	h.undo = h.undo[:len(h.undo)-1]
	if len(h.redo) == 0 {
		t.Fatal("nothing on the redo stack; the fixture cannot show a stale stash")
	}
	h.record(three, nil, false) // a real edit: the branch is abandoned
	if len(h.redo) != 0 {
		t.Fatal("the real edit did not abandon the redo branch")
	}

	// A rebuild that changes nothing — record returns early, and must
	// still have cleared the stash on its way in.
	h.record(three, nil, false)
	h.abort(three)

	if len(h.redo) != 0 {
		t.Errorf("abort resurrected %d redo state(s) from a branch the user had "+
			"already edited past", len(h.redo))
	}
}

// TestAnAbortAfterAnUnchangedRecordKeepsTheRedoBranch is the OTHER
// direction of the same asymmetry, and it is the one round 10 found.
//
// The test above proves abort does not RESURRECT a branch the record
// before it abandoned. This proves it does not DESTROY one the record
// before it never touched. record takes an early return when the attempt
// changed no document state — one of the three cases abort's own fallback
// branch enumerates — and never reaches the line that nils redo. An
// unconditional `h.redo, h.cleared = h.cleared, nil` in abort therefore
// assigns nil over a live branch.
//
// A unit test on history, for the same reason as the one above: every
// abortHistory in the editor is preceded by the refused mutator's own
// rebuild, which changed the tree, so record reaches its push branch and
// there is nothing to be asymmetric about. The undo half of abort is
// already guarded; this is the redo half catching up.
func TestAnAbortAfterAnUnchangedRecordKeepsTheRedoBranch(t *testing.T) {
	h := &history{limit: 10}
	one := &node{Elem: "VStack", Attrs: map[string]string{"Name": "A"}}
	two := &node{Elem: "VStack", Attrs: map[string]string{"Name": "B"}}

	h.record(one, nil, false) // the baseline
	h.record(two, nil, false) // an edit
	// The history half of ed.undo(), inline — the user has stepped back,
	// so there is a live branch to lose.
	h.redo = append(h.redo, h.base)
	h.base = h.undo[len(h.undo)-1]
	h.undo = h.undo[:len(h.undo)-1]
	want := len(h.redo)
	if want == 0 {
		t.Fatal("nothing on the redo stack; there is no branch for abort to destroy " +
			"and this test asserts nothing")
	}

	// A rebuild that changes nothing, then a refusal's abort. Neither
	// touched the branch.
	h.record(one, nil, false)
	h.abort(one)

	if got := len(h.redo); got != want {
		t.Errorf("the redo stack holds %d state(s) after a no-change record and an "+
			"abort, want %d — one ctrl+y is now dead, and nothing in the sequence "+
			"abandoned the branch", got, want)
	}
}
