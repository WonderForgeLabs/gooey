package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/apps/wysiwyg/components/preview"
	"github.com/WonderForgeLabs/gooey/input"
)

// UNDO AND REDO, and the two things that silently pass are the BOUND and
// the REDO INVALIDATION. Both have their own tests below and both are
// mutation-checked, because a history that never evicts and a redo stack
// that is never cleared both behave perfectly right up until the session
// that matters.

// ctrlZ and ctrlY go through the page's own KeyBindings rather than
// calling ed.undo, for the reason pressEsc gives in select_test.go: a
// binding on the wrong element never fires, and a direct call would pass
// for a page that has none.
func ctrlZ(c *gooey.Composer) bool {
	return c.Handle(input.KeyOf(input.KeyEvent{Key: input.KeyRune, Rune: 'z', Mods: input.ModCtrl}))
}

func ctrlY(c *gooey.Composer) bool {
	return c.Handle(input.KeyOf(input.KeyEvent{Key: input.KeyRune, Rune: 'y', Mods: input.ModCtrl}))
}

// docNames is the user's document as a flat list of element names, which
// is the cheapest thing that changes when an edit lands and changes back
// when it is undone.
func docNames(ed *editor) string {
	var b strings.Builder
	var walk func(n *node, depth int)
	walk = func(n *node, depth int) {
		if depth > 0 {
			if b.Len() > 0 {
				b.WriteString(",")
			}
			b.WriteString(n.Elem + ":" + n.Attrs["Name"])
		}
		for _, k := range n.Kids {
			walk(k, depth+1)
		}
	}
	walk(ed.doc(), 0)
	return b.String()
}

// nthEdit makes the i'th of a run of edits that must each be their own
// history entry.
//
// It ALTERNATES THE ELEMENT, and that is load-bearing rather than
// incidental: consecutive edits to the same attribute of the same node
// COALESCE into one undo step by design (see editKey), so a run written
// the obvious way — six edits to the same Name — records ONE entry, and a
// bound test built on it passes while measuring nothing. That is exactly
// how these tests went red when coalescing landed, which is the evidence
// that they count real entries now.
func nthEdit(t *testing.T, ed *editor, i int) {
	t.Helper()
	ed.sel = ed.doc().Kids[i%2]
	editAttr(t, ed, "Canvas.Top", strconv.Itoa(10+i))
}

// docState is the whole user document, so an assertion sees attribute
// edits and not only which elements exist.
func docState(ed *editor) string { return ed.doc().markup("") }

// undoFixture is the shipped page with a two-element document, already
// settled, with the history baseline established.
func undoFixture(t *testing.T) (*editor, *gooey.Composer) {
	t.Helper()
	ed, c, _ := designerPageCounting(t)
	ed.doc().Kids = []*node{
		{Elem: "Text", Body: "aaaa", Attrs: map[string]string{"Name": "A", "Canvas.Left": "1", "Canvas.Top": "1"}},
		{Elem: "Text", Body: "bbbb", Attrs: map[string]string{"Name": "B", "Canvas.Left": "1", "Canvas.Top": "6"}},
	}
	// SELECTED, not left dangling. Replacing Kids wholesale strands the
	// selection newEditor made, and a fixture that starts with ed.sel
	// pointing outside the document would make every selection assertion
	// below test the stranding rather than the undo.
	ed.sel = ed.doc().Kids[0]
	ed.rebuild()
	if !strings.HasPrefix(ed.status.Get(), "✓") {
		t.Fatalf("fixture does not build: %s", ed.status.Get())
	}
	// THE FIXTURE'S OWN SETUP IS AN EDIT, and a real one — it replaced the
	// document. Dropping the history and re-establishing the baseline from
	// here is what makes a step count below this test's own doing. Done
	// through the lazy constructor rather than by reaching into the
	// stacks, so it takes the same path a fresh editor does.
	ed.hist = nil
	ed.rebuild()
	settle(t, c)
	if ed.CanUndo() {
		t.Fatalf("the fixture starts with %d undo steps", len(ed.history().undo))
	}
	return ed, c
}

// ---- the feature ----

// TestCtrlZUndoesAnEditAndCtrlYRedoesIt is the whole gesture, end to end
// through the shipped page's bindings.
func TestCtrlZUndoesAnEditAndCtrlYRedoesIt(t *testing.T) {
	ed, c := undoFixture(t)
	before := docNames(ed)

	ed.sel = ed.doc().Kids[1]
	ed.deleteSelected()
	after := docNames(ed)
	if after == before {
		t.Fatalf("the delete changed nothing; the test proves nothing (%s)", before)
	}

	if !ctrlZ(c) {
		t.Fatal("ctrl+z was not consumed: the page has no Undo binding, or it is not on the root")
	}
	if got := docNames(ed); got != before {
		t.Errorf("after ctrl+z the document is %q, want the pre-delete %q", got, before)
	}
	if !ctrlY(c) {
		t.Fatal("ctrl+y was not consumed: the page has no Redo binding")
	}
	if got := docNames(ed); got != after {
		t.Errorf("after ctrl+y the document is %q, want the post-delete %q", got, after)
	}
}

// TestUndoCoversEveryMutatorTheEditorHas is the claim that matters most
// for the seam: undo is not wired per mutator, so this is a table over
// ALL of them and it is what would go red if one of them stopped going
// through rebuild.
//
// It is deliberately written as "do the edit, assert the document
// changed, undo, assert it came back" rather than as per-mutator expected
// values: the point is coverage of the seam, and a table of expected
// markup would be a table of things to update whenever a seed changes.
// undoCase is one row of that table.
//
// mutator names the *editor method the row drives, and it is not
// decoration: TestTheUndoTableNamesEveryMutatorInTheSource reads these
// names back and fails when the package grows a mutator no row covers.
// The test's name is a claim about ALL of them, and a hand-written
// table cannot keep that claim true on its own — this is what makes the
// next mutator someone adds go red here rather than quietly widening
// the gap. It went five mutators wide before anyone noticed, which is
// how the derivation came to be written.
//
// setup reshapes the document before the baseline is taken, for the
// rows whose mutator refuses on the flat two-Text fixture: promote
// needs a grandparent, demote a container to nest into, the track verbs
// a <Grid>. It runs BEFORE `before` is captured and the history is
// re-established after it, so a row's scaffolding is never part of the
// edit it is testing.
type undoCase struct {
	name    string
	mutator string
	setup   func(ed *editor)
	edit    func(ed *editor)
}

func undoCases(t *testing.T) []undoCase {
	return []undoCase{
		{name: "add", mutator: "addSelected", edit: func(ed *editor) {
			// The palette's first entry, whatever it is.
			ed.paletteSel.Set(0)
			ed.sel = ed.doc()
			ed.addSelected()
		}},
		{name: "delete", mutator: "deleteSelected", edit: func(ed *editor) {
			ed.sel = ed.doc().Kids[0]
			ed.deleteSelected()
		}},
		{name: "retype", mutator: "retype", edit: func(ed *editor) {
			ed.retype("VStack")
		}},
		{name: "attribute edit", mutator: "Write", edit: func(ed *editor) {
			ed.sel = ed.doc().Kids[0]
			editAttr(t, ed, "Canvas.Left", "17")
		}},
		{name: "body edit", mutator: "commitEdit", edit: func(ed *editor) {
			ed.sel = ed.doc().Kids[0]
			ed.attrSel.Set(bodyRowIndex(t, ed))
			ed.beginEdit()
			ed.editValue.Set("edited")
			ed.commitEdit()
		}},
		// THE THREE CLIPBOARD MUTATORS. The PR description called them
		// undoable and nothing pinned it — clipboard_test.go exercises
		// what they DO, this table is the only place that asserts the
		// document comes back. Added on review of #392.
		//
		// They are worth having here rather than trusting the choke
		// point: "every mutator ends in rebuild()" is the claim undo
		// derives from, and the way it fails is a mutator that quietly
		// does not. A table entry is what turns that from a convention
		// into a check.
		{name: "cut", mutator: "cutSelected", edit: func(ed *editor) {
			ed.sel = ed.doc().Kids[0]
			ed.cutSelected()
		}},
		{name: "paste", mutator: "insertSubtree", edit: func(ed *editor) {
			ed.sel = ed.doc().Kids[0]
			ed.copySelected() // seeds the clipboard; copy alone is not an edit
			ed.sel = ed.doc()
			ed.pasteClip()
		}},
		{name: "paste markup (the terminal's own paste key)",
			mutator: "pasteMarkup", edit: func(ed *editor) {
				ed.sel = ed.doc()
				ed.pasteMarkup(`<Text Name="P">pasted</Text>`)
			}},
		{name: "duplicate", mutator: "duplicateSelected", edit: func(ed *editor) {
			ed.sel = ed.doc().Kids[0]
			ed.duplicateSelected()
		}},
		// THE THREE STRUCTURAL MOVES, and the reason they need setup.
		// Each refuses on a document it cannot move within, and a row
		// that silently refused would pass every assertion below except
		// "the document changed" — which is exactly the guard that
		// caught them being written wrong.
		{name: "reorder", mutator: "moveSelected", edit: func(ed *editor) {
			ed.sel = ed.doc().Kids[0]
			ed.moveSelected(1)
		}},
		{name: "promote", mutator: "promoteSelected", setup: nestOneText,
			edit: func(ed *editor) {
				ed.sel = ed.doc().Kids[0].Kids[0]
				ed.promoteSelected()
			}},
		{name: "demote", mutator: "demoteSelected", setup: containerThenText,
			edit: func(ed *editor) {
				ed.sel = ed.doc().Kids[1]
				ed.demoteSelected()
			}},
		// The track verbs all write through writeTracks, so one row
		// covers the seam for resize, cycle-kind, add and remove. The
		// derivation names writeTracks for that reason: it is the
		// method that reaches rebuild.
		{name: "track resize", mutator: "writeTracks", setup: gridWithTracks,
			edit: func(ed *editor) {
				ed.setCursor(trackCursor{on: true, axis: preview.AxisCol})
				ed.resizeTrack(1)
			}},
		{name: "applyEdit (the labelled form other slices use)",
			mutator: "applyEdit", edit: func(ed *editor) {
				ed.applyEdit("paste", func() {
					ed.doc().Kids = append(ed.doc().Kids, &node{
						Elem: "Text", Body: "pasted",
						Attrs: map[string]string{"Name": "P"},
					})
				})
			}},
	}
}

// nestOneText gives promoteSelected a grandparent to lift out to.
func nestOneText(ed *editor) {
	ed.doc().Kids = []*node{{
		Elem:  "VStack",
		Attrs: map[string]string{"Name": "V"},
		Kids: []*node{{Elem: "Text", Body: "cccc",
			Attrs: map[string]string{"Name": "C"}}},
	}}
}

// containerThenText gives demoteSelected a preceding sibling that can
// actually hold the selection — a <Text> cannot, and demote refuses.
func containerThenText(ed *editor) {
	ed.doc().Kids = []*node{
		{Elem: "VStack", Attrs: map[string]string{"Name": "V"},
			Kids: []*node{{Elem: "Text", Body: "cccc",
				Attrs: map[string]string{"Name": "C"}}}},
		{Elem: "Text", Body: "dddd", Attrs: map[string]string{"Name": "D"}},
	}
}

// gridWithTracks makes the user's root a <Grid>, which is what the
// track verbs require in scope.
func gridWithTracks(ed *editor) {
	ed.doc().Elem = "Grid"
	ed.doc().Attrs = map[string]string{"Rows": "2,2", "Cols": "8,8"}
	ed.doc().Kids = []*node{{Elem: "Text", Body: "aa",
		Attrs: map[string]string{"Name": "A", "Grid.Row": "0", "Grid.Col": "0"}}}
	ed.setSelection(ed.doc())
}

func TestUndoCoversEveryMutatorTheEditorHas(t *testing.T) {
	for _, tc := range undoCases(t) {
		t.Run(tc.name, func(t *testing.T) {
			ed, c := undoFixture(t)
			if tc.setup != nil {
				tc.setup(ed)
				// The scaffolding is not the edit under test, so the
				// history is re-established over it exactly the way the
				// fixture does over its own seed.
				ed.hist = nil
				ed.rebuild()
				settle(t, c)
				if !strings.HasPrefix(ed.status.Get(), "✓") {
					t.Fatalf("the %s scaffolding does not build: %s",
						tc.name, ed.status.Get())
				}
				if ed.CanUndo() {
					t.Fatalf("the %s scaffolding left %d undo steps behind",
						tc.name, len(ed.history().undo))
				}
			}
			before := ed.doc().markup("")

			tc.edit(ed)
			after := ed.doc().markup("")
			if after == before {
				t.Fatalf("%s changed no document state, so undoing it proves nothing:\n%s", tc.name, before)
			}
			if !ed.CanUndo() {
				t.Fatalf("%s changed the document but recorded no history: it did not go through rebuild()", tc.name)
			}

			ed.undo()
			if got := ed.doc().markup(""); got != before {
				t.Errorf("undoing %s gave\n%s\nwant\n%s", tc.name, got, before)
			}
			ed.redo()
			if got := ed.doc().markup(""); got != after {
				t.Errorf("redoing %s gave\n%s\nwant\n%s", tc.name, got, after)
			}
		})
	}
}

// bodyRowIndex finds the (text) row, which commitEdit needs selected to
// take the body route rather than writing an attribute named "(text)".
func bodyRowIndex(t *testing.T, ed *editor) int {
	t.Helper()
	for i, r := range ed.attrRows() {
		if r.body {
			return i
		}
	}
	t.Fatalf("the selected element has no body row; the body case cannot be exercised")
	return -1
}

// ---- the redo invalidation: the classic bug ----

// TestANewEditAfterAnUndoInvalidatesRedo is the bug this test exists for
// and the one that silently passes without it.
//
// Undo to a fork point, then edit. The redone future belonged to a branch
// that no longer exists, and a ctrl+y that replayed it would silently
// throw away the edit the user just made — replacing their document with
// a state they explicitly walked away from.
func TestANewEditAfterAnUndoInvalidatesRedo(t *testing.T) {
	ed, _ := undoFixture(t)

	ed.sel = ed.doc().Kids[1]
	ed.deleteSelected() // edit 1: B is gone
	ed.undo()           // back to A,B — a redo of edit 1 is now available

	if !ed.CanRedo() {
		t.Fatal("nothing to redo after an undo; the rest of this test is vacuous")
	}
	forked := docNames(ed)

	// The new edit. It abandons the branch edit 1 was on.
	ed.sel = ed.doc().Kids[0]
	ed.deleteSelected() // edit 2: A is gone
	fresh := docNames(ed)
	if fresh == forked {
		t.Fatal("the second edit changed nothing; the fork never happened")
	}

	if ed.CanRedo() {
		t.Fatal("the redo stack survived a new edit: ctrl+y would replay a state from an " +
			"abandoned branch and silently discard the edit just made")
	}
	ed.redo()
	if got := docNames(ed); got != fresh {
		t.Errorf("ctrl+y after a new edit changed the document to %q, want it left at %q", got, fresh)
	}
}

// TestUndoAndRedoAlternateWithoutLosingTheStack is the OTHER half of the
// same rule, and the two constrain each other.
//
// Clearing redo too eagerly — inside undo, or on any keystroke — would
// make the test above pass and this one fail: the user could undo but
// never redo, which is the "fix" someone reaches for when redo
// invalidation is the thing they are debugging.
func TestUndoAndRedoAlternateWithoutLosingTheStack(t *testing.T) {
	ed, _ := undoFixture(t)
	s0 := docNames(ed)

	ed.sel = ed.doc().Kids[1]
	ed.deleteSelected()
	s1 := docNames(ed)

	for i := 0; i < 3; i++ {
		ed.undo()
		if got := docNames(ed); got != s0 {
			t.Fatalf("round %d: after undo the document is %q, want %q", i, got, s0)
		}
		ed.redo()
		if got := docNames(ed); got != s1 {
			t.Fatalf("round %d: after redo the document is %q, want %q", i, got, s1)
		}
	}
}

// TestUndoingSeveralStepsThenRedoingThemAllReturnsToWhereYouStarted is
// the DEPTH version of the test above, and it exists because the
// one-deep version cannot see the bug it was written for.
//
// A redo clear placed inside undo() — the obvious "fix" when redo
// invalidation is what you are debugging — still leaves the entry undo
// itself just pushed, so a single undo/redo pair works perfectly and the
// alternation test above stays green. Only the SECOND consecutive undo
// exposes it: it wipes the entry the first one left, and the redoes then
// run out one step in.
//
// Found by mutating exactly that and watching the alternation test pass.
// Three deep, so there is a middle to lose.
func TestUndoingSeveralStepsThenRedoingThemAllReturnsToWhereYouStarted(t *testing.T) {
	ed, _ := undoFixture(t)

	states := []string{docState(ed)}
	for i := 0; i < 3; i++ {
		nthEdit(t, ed, i)
		states = append(states, docState(ed))
	}

	for i := 0; i < 3; i++ {
		ed.undo()
		want := states[2-i]
		if got := docState(ed); got != want {
			t.Fatalf("undo %d gave %q, want %q", i+1, got, want)
		}
	}
	for i := 0; i < 3; i++ {
		if !ed.CanRedo() {
			t.Fatalf("redo %d has nothing to replay: %d undos left only %d redoable steps, "+
				"so an undo is discarding the redo stack it should be building",
				i+1, 3, i)
		}
		ed.redo()
		want := states[i+1]
		if got := docState(ed); got != want {
			t.Fatalf("redo %d gave %q, want %q", i+1, got, want)
		}
	}
}

// ---- the bound ----

// TestHistoryEvictsTheOldestBeyondTheLimit is the eviction path, and it
// is exercised rather than asserted: the stack is DRIVEN past the bound
// and then undone all the way down.
//
// A bound that is never crossed in a test is a claim, not a behaviour.
func TestHistoryEvictsTheOldestBeyondTheLimit(t *testing.T) {
	ed, _ := undoFixture(t)
	ed.setHistoryLimit(3)

	states := []string{docState(ed)}
	// Six edits, twice the bound. nthEdit alternates the element so no two
	// consecutive edits coalesce — six entries, not one.
	for i := 0; i < 6; i++ {
		nthEdit(t, ed, i)
		states = append(states, docState(ed))
	}

	if n := len(ed.history().undo); n != 3 {
		t.Fatalf("after 6 edits with a limit of 3 the stack holds %d; the bound did not evict", n)
	}

	// Undo as far as it goes. Three steps must land on states[6-1-i],
	// and the fourth must find nothing — the older states are gone.
	for i := 0; i < 3; i++ {
		ed.undo()
		want := states[len(states)-2-i]
		if got := docState(ed); got != want {
			t.Fatalf("undo %d gave %q, want %q", i+1, got, want)
		}
	}
	if ed.CanUndo() {
		t.Errorf("a 4th undo is available under a limit of 3: the stack holds %d entries",
			len(ed.history().undo))
	}
	stuck := docState(ed)
	ed.undo()
	if got := docState(ed); got != stuck {
		t.Errorf("undoing past the bound changed the document to %q, want it left at %q", got, stuck)
	}
	// AND THE EVICTED ENTRIES ARE NOT REACHABLE THROUGH THE BACKING
	// ARRAY. Reslicing without zeroing would leave them alive, which is
	// a bound on how far you can undo and not on what is held.
	for i, s := range ed.history().undo[len(ed.history().undo):cap(ed.history().undo)] {
		if s.root != nil {
			t.Errorf("evicted entry %d is still held past len(): the bound frees nothing", i)
		}
	}
}

// TestShrinkingTheLimitEvictsImmediately is what -history would do if it
// were changeable at runtime, and what setHistoryLimit must do
// regardless: a bound that only applies to future pushes leaves whatever
// is already held.
func TestShrinkingTheLimitEvictsImmediately(t *testing.T) {
	ed, _ := undoFixture(t)
	for i := 0; i < 5; i++ {
		nthEdit(t, ed, i)
	}
	if n := len(ed.history().undo); n != 5 {
		t.Fatalf("5 edits recorded %d entries", n)
	}
	ed.setHistoryLimit(2)
	if n := len(ed.history().undo); n != 2 {
		t.Errorf("after shrinking the limit to 2 the stack still holds %d", n)
	}
}

// TestAZeroLimitDisablesUndo pins the documented meaning of
// -history 0. It is the boundary the eviction arithmetic is most likely
// to get wrong, because it is the one where "drop the oldest" has to drop
// everything.
func TestAZeroLimitDisablesUndo(t *testing.T) {
	ed, _ := undoFixture(t)
	ed.setHistoryLimit(0)

	ed.sel = ed.doc().Kids[1]
	ed.deleteSelected()
	after := docNames(ed)

	if ed.CanUndo() {
		t.Error("-history 0 still recorded an undo step")
	}
	ed.undo()
	if got := docNames(ed); got != after {
		t.Errorf("ctrl+z under -history 0 changed the document to %q, want %q", got, after)
	}
}

// TestTheRedoStackIsBoundedByTheSameLimit is the claim undo.go makes
// about redo needing no bound of its own: it grows only when undo pops,
// so it cannot exceed the undo depth, which is limit.
func TestTheRedoStackIsBoundedByTheSameLimit(t *testing.T) {
	ed, _ := undoFixture(t)
	ed.setHistoryLimit(2)
	for i := 0; i < 6; i++ {
		nthEdit(t, ed, i)
	}
	for i := 0; i < 6; i++ {
		ed.undo()
	}
	if n := len(ed.history().redo); n > 2 {
		t.Errorf("the redo stack holds %d under a limit of 2: it needs a bound of its own after all", n)
	}
}

// ---- what does and does not record ----

// TestARebuildThatChangedNothingRecordsNothing is the property that makes
// the hook safe to put in rebuild at all. Without it every failed build,
// every selection change that happens to rebuild, and every remote
// re-push would cost an undo step, and ctrl+z would appear to do nothing
// several times before it did something.
func TestARebuildThatChangedNothingRecordsNothing(t *testing.T) {
	ed, _ := undoFixture(t)
	if ed.CanUndo() {
		t.Fatalf("the fixture already has %d entries; the count below would not mean anything",
			len(ed.history().undo))
	}
	for i := 0; i < 5; i++ {
		ed.rebuild()
	}
	if n := len(ed.history().undo); n != 0 {
		t.Errorf("5 rebuilds with no edit recorded %d undo steps", n)
	}
}

// TestMovingTheSelectionIsNotUndoable is the line this slice draws,
// asserted rather than only written down. The selection does not reach
// the saved file, so it is not an edit.
func TestMovingTheSelectionIsNotUndoable(t *testing.T) {
	ed, _ := undoFixture(t)
	ed.selectNext(1)
	ed.selectNext(1)
	ed.selectParent()
	if ed.CanUndo() {
		t.Errorf("moving the selection recorded %d undo steps; ctrl+z would walk the "+
			"selection backwards instead of undoing an edit", len(ed.history().undo))
	}
}

// TestToggleDesignModeIsNotUndoable, same line, other side. A view mode
// is not content.
func TestToggleDesignModeIsNotUndoable(t *testing.T) {
	ed, _ := undoFixture(t)
	ed.toggleMode()
	ed.toggleMode()
	if ed.CanUndo() {
		t.Error("toggling DESIGN/LIVE recorded an undo step")
	}
}

// TestUndoRestoresTheSelectionRatherThanDanglingIt is the hazard the
// clipboard and property slices both asked about: undo replaces the tree
// wholesale, so a selection held as a *node would name a node in a tree
// nobody holds. deleteSelected would then silently do nothing, because
// parentOf returns nil for a node outside the document.
func TestUndoRestoresTheSelectionRatherThanDanglingIt(t *testing.T) {
	ed, _ := undoFixture(t)
	ed.sel = ed.doc().Kids[1]
	editAttr(t, ed, "Name", "renamed")

	ed.undo()

	if ed.sel == nil {
		t.Fatal("undo left nothing selected")
	}
	if ed.parentOf(ed.sel) == nil {
		t.Fatal("the selection after undo is not in the document: it is a pointer into the " +
			"tree undo replaced, and every gesture that resolves through parentOf silently " +
			"does nothing")
	}
	// And it names the same ELEMENT it named before, not merely something.
	if got := ed.sel.Attrs["Name"]; got != "B" {
		t.Errorf("after undo the selection is %q, want the element that was selected, \"B\"", got)
	}
}

// ---- the drag ----

// TestOneDragRebuildsExactlyOnceAndRecordsOneUndoStep is the coalescing
// question, answered by COUNTING REBUILDS rather than by counting undo
// steps or by trusting the design.
//
// The failure it guards is specific to a diff-on-rebuild history: if the
// drag rebuilt per pointer motion, each intermediate position would be a
// recorded state and ctrl+z would walk the element back one cell at a
// time instead of undoing "the move". Every naturally-written test still
// passes in that world — the element ends up in the right place, the
// markup is right, the damage counts are right — and the user finds it by
// dragging something.
//
// COUNTING UNDO STEPS IS NOT ENOUGH, and that is why this test is shaped
// the way it is. A step count of 1 is also what you get if rebuild ran
// eight times and only one of them changed the document, because the
// history's own diff collapses the rest. That would still be a real
// defect — eight full designer re-mounts per gesture — and it would be
// invisible. So the phases are counted separately.
//
// ed.rev IS THE COUNTER, with no instrumentation added: it is ticked in
// exactly two places, rebuild (main.go) and setSelection (select.go).
// That makes a delta of 0 across the motions the strong claim — neither
// a rebuild NOR a re-selection happened — and a delta of 1 on the release
// exactly one rebuild with no re-selection.
//
// Measured with a temporary counter inside rebuild while writing this:
// press 0 rebuilds, seven motions 0, release 1. The gesture is already
// one-rebuild by construction (drag.go writes the live component's
// gooey.Layout per motion and markup only on release), so nothing had to
// be coalesced — this is the pin that it stays that way.
func TestOneDragRebuildsExactlyOnceAndRecordsOneUndoStep(t *testing.T) {
	ed, c := undoFixture(t)
	a := docKid(ed, 0)
	b0 := a.(interface{ Bounds() gooey.Rect }).Bounds()
	before := ed.doc().Kids[0].Attrs["Canvas.Left"]

	press(c, b0.X, b0.Y)
	atPress := ed.rev.Get()

	// SEVEN motions, so a per-motion rebuild would be unmistakable.
	for i := 1; i <= 7; i++ {
		motion(c, b0.X+i, b0.Y+i)
		c.Frame()
	}
	atMotions := ed.rev.Get()
	if n := atMotions - atPress; n != 0 {
		t.Errorf("7 motions ticked rev %d times, want 0: a motion is rebuilding the "+
			"designer (or re-selecting), so every intermediate position becomes its own "+
			"undo step and ctrl+z walks the element back a cell at a time", n)
	}

	release(c, b0.X+7, b0.Y+7)
	if n := ed.rev.Get() - atMotions; n != 1 {
		t.Errorf("the release ticked rev %d times, want exactly 1 (one rebuild, no "+
			"re-selection)", n)
	}

	if got := ed.doc().Kids[0].Attrs["Canvas.Left"]; got == before {
		t.Fatalf("the drag committed nothing (Canvas.Left still %q); the counts above are "+
			"vacuous", got)
	}
	if n := len(ed.history().undo); n != 1 {
		t.Fatalf("one press-drag-release over 7 motions recorded %d undo steps, want 1", n)
	}
	ed.undo()
	if got := ed.doc().Kids[0].Attrs["Canvas.Left"]; got != before {
		t.Errorf("one ctrl+z after a drag left Canvas.Left at %q, want the pre-drag %q", got, before)
	}
}

// TestAClickIsNotAnEdit. A press and release with no motion between them
// is a selection, not a move — drag.go declines to write markup for it
// (dragState.moved). So it must cost no rebuild and no undo step, or
// every click on the canvas would fill the history with entries that
// change nothing and ctrl+z would appear to do nothing several times
// before it did something.
func TestAClickIsNotAnEdit(t *testing.T) {
	ed, c := undoFixture(t)
	a := docKid(ed, 0)
	b0 := a.(interface{ Bounds() gooey.Rect }).Bounds()

	before := ed.rev.Get()
	press(c, b0.X, b0.Y)
	release(c, b0.X, b0.Y)

	// One tick at most, and it is setSelection's — never a rebuild's.
	if n := ed.rev.Get() - before; n > 1 {
		t.Errorf("a click ticked rev %d times, want at most 1 (the selection): it is "+
			"rebuilding the designer for a gesture that changed no document state", n)
	}
	if n := len(ed.history().undo); n != 0 {
		t.Errorf("a click with no motion recorded %d undo steps, want 0", n)
	}
}

// TestUndoDuringADragDropsTheGesture. Keys and mouse reports are one
// ordered stream, so a ctrl+z can land between a press and its release.
// After it, the dragged node is from a tree nobody holds. Nothing new was
// written for this — dragLive already drops a gesture whose node has left
// the document — and this is the pin that it really covers the case.
func TestUndoDuringADragDropsTheGesture(t *testing.T) {
	ed, c := undoFixture(t)
	// An edit to have something to undo mid-gesture.
	ed.sel = ed.doc().Kids[1]
	editAttr(t, ed, "Name", "renamed")
	settle(t, c)

	a := docKid(ed, 0)
	b0 := a.(interface{ Bounds() gooey.Rect }).Bounds()
	press(c, b0.X, b0.Y)
	if !ed.drag.active() {
		t.Fatal("the press started no drag; the test cannot reach the case")
	}

	ctrlZ(c)

	// The gesture must not write anything on release.
	left := ed.doc().Kids[0].Attrs["Canvas.Left"]
	release(c, b0.X+9, b0.Y+9)
	if got := ed.doc().Kids[0].Attrs["Canvas.Left"]; got != left {
		t.Errorf("a release after an undo wrote Canvas.Left=%q onto a node from the replaced "+
			"tree (was %q)", got, left)
	}
}

// ---- damage ----

// TestRedoCostsExactlyWhatTheEditCost is the damage pin, and it is a
// COMPARISON rather than a constant for the reason drag_test.go gives: an
// absolute number moves whenever the fixture's layout changes, and what
// must hold is the relationship.
//
// The relationship is exact. Redoing an edit is the SAME transition as
// making it — the same two document states, in the same direction — so it
// must cost the same repaints, to the component. If restoring ever
// invalidates anything the forward edit does not, this is where it shows
// up, immediately and by name.
//
// Undo is the reverse transition and is deliberately NOT compared to the
// edit: undoing a delete puts a component back, which legitimately costs
// more than taking one away. What it is held to is the thing that would
// make undo a defect — repainting the composition. See below.
func TestRedoCostsExactlyWhatTheEditCost(t *testing.T) {
	ed, c := undoFixture(t)
	total := componentCount(c.Root())

	ed.sel = ed.doc().Kids[1]
	ed.deleteSelected()
	_, edited := c.Frame()
	settle(t, c)
	if edited == 0 {
		t.Fatal("the edit repainted nothing; every comparison below is vacuous")
	}

	ed.undo()
	_, undone := c.Frame()
	settle(t, c)
	if undone == 0 {
		t.Fatal("the undo repainted nothing: the document changed and the screen did not")
	}

	ed.redo()
	_, redone := c.Frame()
	settle(t, c)

	t.Logf("edit=%d undo=%d redo=%d of %d components", edited, undone, redone, total)

	if redone != edited {
		t.Errorf("redo repainted %d components and the edit it replays repainted %d: "+
			"restoring a state invalidates something making it does not", redone, edited)
	}
	// AN UNDO THAT REPAINTS THE TREE IS A DEFECT. Against the size of the
	// whole composition rather than a constant, so the pin survives the
	// shell growing panes back.
	if undone*2 >= total {
		t.Errorf("undo repainted %d of the composition's %d components: it is repainting "+
			"the editor's shell, not the document", undone, total)
	}
}

// componentCount is the size of the whole composition, which is what an
// "it did not repaint the tree" claim has to be measured against.
func componentCount(c gooey.Component) int {
	n := 1
	for _, k := range childComponents(c) {
		n += componentCount(k)
	}
	return n
}

// TestAnUndoWithNothingToUndoRepaintsAtMostTheMessage.
//
// prop.Set does not compare values, so an undo at the bottom of the stack
// that Set anything unconditionally would cost a repaint for a keystroke
// that did nothing — and ctrl+z held down would repaint forever. sayDrag
// guards its own Set; this is the pin that the guard is actually on the
// path undo takes.
func TestAnUndoWithNothingToUndoRepaintsAtMostTheMessage(t *testing.T) {
	ed, c := undoFixture(t)
	if ed.CanUndo() {
		t.Fatal("the fixture has history; this test needs the empty stack")
	}
	ctrlZ(c) // the first one may legitimately paint the message
	settle(t, c)

	ctrlZ(c)
	if _, painted := c.Frame(); painted != 0 {
		t.Errorf("a second ctrl+z with nothing to undo repainted %d components; "+
			"holding the key down repaints forever", painted)
	}
}

// ---- clone and equal, which everything above rests on ----

// TestCloneSharesNothingWithItsOriginal, including SLOTS — the half that
// is easy to miss, because nothing but node.markup walks them. A clone
// that aliased a property element would let an edit inside an
// <ItemsView.ItemTemplate> silently rewrite every snapshot of it.
func TestCloneSharesNothingWithItsOriginal(t *testing.T) {
	orig := &node{
		Elem:  "ItemsView",
		Attrs: map[string]string{"Name": "IV"},
		Slots: map[string]*node{
			"ItemTemplate": {Elem: "Text", Body: "row", Attrs: map[string]string{"Name": "R"}},
		},
		Kids: []*node{{Elem: "Text", Body: "kid", Attrs: map[string]string{"Name": "K"}}},
	}
	c := orig.clone()
	if !c.equal(orig) {
		t.Fatalf("a clone does not equal its original:\n%s\n%s", c.markup(""), orig.markup(""))
	}

	c.Attrs["Name"] = "changed"
	c.Kids[0].Body = "changed"
	c.Slots["ItemTemplate"].Body = "changed"
	c.Slots["ItemTemplate"].Attrs["Name"] = "changed"

	if orig.Attrs["Name"] != "IV" {
		t.Error("writing the clone's Attrs reached the original")
	}
	if orig.Kids[0].Body != "kid" {
		t.Error("writing the clone's Kids reached the original")
	}
	if orig.Slots["ItemTemplate"].Body != "row" {
		t.Error("writing the clone's SLOT body reached the original: Slots are aliased, so " +
			"editing a property element rewrites every snapshot of it")
	}
	if orig.Slots["ItemTemplate"].Attrs["Name"] != "R" {
		t.Error("writing the clone's SLOT attrs reached the original")
	}
}

// TestEqualSeesEveryFieldThatReachesTheFile. equal is what decides
// whether an edit is recorded, so a field it cannot see is a field whose
// edits are silently not undoable. One case per field of node.
func TestEqualSeesEveryFieldThatReachesTheFile(t *testing.T) {
	base := func() *node {
		return &node{
			Elem:  "ItemsView",
			Body:  "",
			Attrs: map[string]string{"Name": "IV"},
			Kids:  []*node{{Elem: "Text", Body: "kid", Attrs: map[string]string{"Name": "K"}}},
			Slots: map[string]*node{"ItemTemplate": {Elem: "Text", Body: "row"}},
		}
	}
	cases := []struct {
		field string
		mut   func(n *node)
	}{
		{"Elem", func(n *node) { n.Elem = "VStack" }},
		{"Body", func(n *node) { n.Body = "now has one" }},
		{"Attrs value", func(n *node) { n.Attrs["Name"] = "other" }},
		{"Attrs added", func(n *node) { n.Attrs["Canvas.Left"] = "3" }},
		{"Attrs removed", func(n *node) { delete(n.Attrs, "Name") }},
		{"Kids count", func(n *node) { n.Kids = append(n.Kids, &node{Elem: "Text"}) }},
		{"Kids deep", func(n *node) { n.Kids[0].Body = "other" }},
		{"Slots added", func(n *node) { n.Slots["Other"] = &node{Elem: "Text"} }},
		{"Slots removed", func(n *node) { delete(n.Slots, "ItemTemplate") }},
		{"Slots deep", func(n *node) { n.Slots["ItemTemplate"].Body = "other" }},
	}
	for _, tc := range cases {
		a, b := base(), base()
		if !a.equal(b) {
			t.Fatalf("%s: two identical trees do not compare equal", tc.field)
		}
		tc.mut(b)
		if a.equal(b) {
			t.Errorf("equal is blind to %s: an edit to it records no history and is "+
				"silently not undoable", tc.field)
		}
	}
}

// TestEqualTreatsNilAndEmptyAsTheSameDocument. clone preserves nil-ness
// and other code builds nodes with empty maps, so a comparison that told
// them apart would record a phantom edit on the first rebuild after one.
func TestEqualTreatsNilAndEmptyAsTheSameDocument(t *testing.T) {
	a := &node{Elem: "Text"}
	b := &node{Elem: "Text", Attrs: map[string]string{}, Kids: []*node{}, Slots: map[string]*node{}}
	if !a.equal(b) {
		t.Error("a node with nil maps does not equal the same node with empty ones")
	}
}

// ---- ctrl+z vs SIGTSTP, and the unfireable sibling ----

// TestCtrlZArrivesAsAKeyNotASignal is the fact the whole binding decision
// rests on, pinned at the decoder.
//
// In a terminal ctrl+z is normally SUSP and the tty driver turns it into
// SIGTSTP. term.MakeRaw clears ISIG, so while a gooey app holds the
// terminal it does not: the byte lands on the tty and this is what the
// decoder makes of it. If that ever changed, this binding would be
// competing with job control rather than picking up something nobody
// used.
func TestCtrlZArrivesAsAKeyNotASignal(t *testing.T) {
	ev, n, ok := input.Decode([]byte{0x1a}, false)
	if !ok || n != 1 {
		t.Fatalf("Decode(0x1a) = %v, %d, %v; the SUSP byte is not decoded at all", ev, n, ok)
	}
	want := input.KeyOf(input.KeyEvent{Key: input.KeyRune, Rune: 'z', Mods: input.ModCtrl})
	if ev != want {
		t.Errorf("Decode(0x1a) = %v, want the ctrl+z key event %v", ev, want)
	}
}

// TestCtrlShiftZIsAnALIASForCtrlZAndSoIsNotBoundToRedo.
//
// ctrl+shift+z is the other common redo gesture, and it is deliberately
// NOT bound — not out of taste, but because in this framework it is not a
// distinct gesture at all.
//
// Two independent reasons, and either alone is decisive. At the WIRE: a
// terminal cannot report shift on a printable character, so ctrl+shift+z
// is the same byte 0x1a as ctrl+z and the decoder lower-cases it on
// purpose. At the GESTURE parser: ParseGesture folds a shift modifier
// into the rune and then lower-cases it again under ctrl
// (input/gesture.go), so "ctrl+shift+z" and "ctrl+z" parse to the
// IDENTICAL KeyEvent.
//
// So a page binding ctrl+shift+z to Redo and ctrl+z to Undo would have
// TWO bindings on ONE event, and one physical keystroke would fire
// whichever the scoped-binding walk reached first. Undo and redo — the
// two commands that must never be confused — would be a coin flip on
// every press. It would also pass every "the binding exists" assertion.
//
// This test is what stops someone adding it back as a kindness.
func TestCtrlShiftZIsAnALIASForCtrlZAndSoIsNotBoundToRedo(t *testing.T) {
	shifted, err := input.ParseGesture("ctrl+shift+z")
	if err != nil {
		t.Skipf("ctrl+shift+z is not a parseable gesture (%v); the trap does not exist", err)
	}
	plain, err := input.ParseGesture("ctrl+z")
	if err != nil {
		t.Fatal(err)
	}
	if shifted != plain {
		t.Fatalf("ctrl+shift+z now parses to %v and ctrl+z to %v: they have become distinct "+
			"gestures and this test's premise is stale — reconsider binding it to redo",
			shifted, plain)
	}
	// And the wire agrees: the byte a user's ctrl+shift+z actually sends
	// decodes to that same one event.
	decoded, _, ok := input.Decode([]byte{0x1a}, false)
	if !ok || decoded.Kind != input.EventKey || decoded.Key != plain {
		t.Fatalf("0x1a decodes to %v, want the single ctrl+z event %v", decoded, plain)
	}

	b, err := os.ReadFile(PageFile)
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	// The ATTRIBUTE, not the bare string: the page explains in a comment
	// why the gesture is not bound, and a substring match would call that
	// explanation the defect it prevents.
	if strings.Contains(src, `Gesture="ctrl+shift+z"`) {
		t.Error("the page binds ctrl+shift+z, which is the SAME event as ctrl+z: one " +
			"keystroke now has two commands and undo vs redo is decided by binding order")
	}
	// And the gestures the user CAN press are bound.
	if !strings.Contains(src, `Gesture="ctrl+z"`) || !strings.Contains(src, `Gesture="ctrl+y"`) {
		t.Error("the page does not bind both ctrl+z and ctrl+y")
	}
}

// ---- coalescing a run of keystrokes ----

// typeInto is what a LIVE editor does: it commits on every keystroke, so
// setting a field to "hello!" is one rebuild per rune. The properties
// pane's caret editor does exactly this, and its stepper and colour
// editors do the arrow-key equivalent.
func typeInto(t *testing.T, ed *editor, n *node, attr, final string) {
	t.Helper()
	for i := 1; i <= len(final); i++ {
		ed.sel = n
		editAttr(t, ed, attr, final[:i])
	}
}

// TestTypingIntoAFieldIsOneUndoStepNotOnePerKeystroke.
//
// This is the drag defect arriving through a different door. A live
// editor writes per rune, so recorded one-per-rebuild a six-character
// value costs six undo steps and ctrl+z walks the text back a character
// at a time. Reported by the properties-pane slice, whose editors commit
// on every keystroke by design.
func TestTypingIntoAFieldIsOneUndoStepNotOnePerKeystroke(t *testing.T) {
	ed, _ := undoFixture(t)
	before := docState(ed)

	typeInto(t, ed, ed.doc().Kids[0], "Tooltip", "hello!")

	if n := len(ed.history().undo); n != 1 {
		t.Fatalf("typing a 6-character value recorded %d undo steps, want 1: ctrl+z walks "+
			"the value back one character at a time", n)
	}
	if got := ed.doc().Kids[0].Attrs["Tooltip"]; got != "hello!" {
		t.Fatalf("the run left Content=%q, want the whole value", got)
	}
	ed.undo()
	if got := docState(ed); got != before {
		t.Errorf("one ctrl+z after typing gave\n%s\nwant the pre-typing\n%s", got, before)
	}
}

// TestACancelledEditLeavesNoUndoStep.
//
// Esc in the properties pane RESTORES by writing the original value back,
// so a cancelled edit is a second document change rather than a rollback.
// Recorded naively that leaves an entry whose undo changes nothing
// visible: the user presses ctrl+z, watches nothing happen, presses it
// again, and undoes an edit they did want.
//
// The run came back to where it started, so there is nothing to undo.
func TestACancelledEditLeavesNoUndoStep(t *testing.T) {
	ed, _ := undoFixture(t)
	target := ed.doc().Kids[0]
	original := target.Attrs["Canvas.Left"]
	before := docState(ed)

	typeInto(t, ed, target, "Canvas.Left", "999")
	// Esc: the editor writes back what the row held when it opened.
	ed.sel = target
	editAttr(t, ed, "Canvas.Left", original)

	if got := docState(ed); got != before {
		t.Fatalf("the cancel did not restore the document; the test is not exercising the "+
			"round trip:\n%s\nwant\n%s", got, before)
	}
	if n := len(ed.history().undo); n != 0 {
		t.Errorf("a typed-then-cancelled edit left %d undo steps, want 0: ctrl+z appears to "+
			"do nothing, and the next one undoes an edit the user wanted", n)
	}
}

// TestARunBreaksOnADifferentField. Coalescing must not swallow a second,
// separate edit — that would make it unreachable by ctrl+z entirely,
// which is far worse than an extra step.
func TestARunBreaksOnADifferentField(t *testing.T) {
	ed, _ := undoFixture(t)
	target := ed.doc().Kids[0]

	typeInto(t, ed, target, "Tooltip", "abc")
	afterFirst := docState(ed)
	typeInto(t, ed, target, "Canvas.Left", "42")

	if n := len(ed.history().undo); n != 2 {
		t.Fatalf("two runs into DIFFERENT fields recorded %d undo steps, want 2", n)
	}
	ed.undo()
	if got := docState(ed); got != afterFirst {
		t.Errorf("undo merged two separate fields into one step:\n%s\nwant\n%s", got, afterFirst)
	}
}

// TestARunBreaksOnADifferentElement, same rule across nodes: the key is
// the node AND the attribute, so the same attribute on a sibling is a
// separate edit.
func TestARunBreaksOnADifferentElement(t *testing.T) {
	ed, _ := undoFixture(t)

	typeInto(t, ed, ed.doc().Kids[0], "Tooltip", "abc")
	afterFirst := docState(ed)
	typeInto(t, ed, ed.doc().Kids[1], "Tooltip", "xyz")

	if n := len(ed.history().undo); n != 2 {
		t.Fatalf("the same attribute on two ELEMENTS recorded %d undo steps, want 2", n)
	}
	ed.undo()
	if got := docState(ed); got != afterFirst {
		t.Errorf("undo merged edits to two different elements:\n%s\nwant\n%s", got, afterFirst)
	}
}

// TestStructuralEditsNeverCoalesce. Add, delete and retype must always be
// their own step whatever surrounds them — merging a delete into a
// neighbouring edit would make the delete unreachable.
func TestStructuralEditsNeverCoalesce(t *testing.T) {
	ed, _ := undoFixture(t)
	ed.paletteSel.Set(0)
	ed.sel = ed.doc()

	ed.addSelected()
	ed.addSelected()
	if n := len(ed.history().undo); n != 2 {
		t.Errorf("two adds recorded %d undo steps, want 2", n)
	}
	before := len(ed.history().undo)
	ed.sel = ed.doc().Kids[0]
	ed.deleteSelected()
	ed.sel = ed.doc().Kids[0]
	ed.deleteSelected()
	if n := len(ed.history().undo) - before; n != 2 {
		t.Errorf("two deletes recorded %d undo steps, want 2", n)
	}
}

// TestARestoredStateStartsAFreshRun is the trap inside the coalescing
// rule.
//
// If a restored state kept the key of the edit that produced it, the next
// keystroke into that same field would MERGE into the state undo just
// restored — no entry pushed — and ctrl+z could not get back out again.
// The edit would be permanently unreachable.
func TestARestoredStateStartsAFreshRun(t *testing.T) {
	ed, _ := undoFixture(t)
	target := ed.doc().Kids[0]

	// TWO runs, and the second one is what makes this test able to fail.
	// Undoing back to the OPENING state proves nothing: the baseline
	// carries no key, so a restore that wrongly inherited one would
	// inherit "" and behave correctly by accident. The state restored
	// below was produced by a run and therefore carries Content's key —
	// which is the state a buggy restore merges the next keystroke into.
	// Found by mutation: with only the first run, carrying the key
	// through restore left the suite green.
	typeInto(t, ed, target, "Tooltip", "abc")
	typeInto(t, ed, target, "Canvas.Left", "42")
	ed.undo()
	restored := docState(ed)

	// Straight back into the SAME field the restored state was made by.
	typeInto(t, ed, ed.doc().Kids[0], "Tooltip", "xyz")
	if !ed.CanUndo() {
		t.Fatal("typing after an undo recorded nothing: it merged into the restored state, " +
			"and that edit can never be undone")
	}
	ed.undo()
	if got := docState(ed); got != restored {
		t.Errorf("ctrl+z after typing-after-an-undo gave\n%s\nwant\n%s", got, restored)
	}
}

// TestEditKeyIsConservative. A key wrongly SHARED merges edits the user
// thinks are separate and makes one unreachable; a key wrongly EMPTY only
// costs an extra step. So the two-attribute case — which is what a drag
// writes — must not coalesce.
func TestEditKeyIsConservative(t *testing.T) {
	base := func() *node {
		return &node{Elem: "Canvas", Kids: []*node{
			{Elem: "Text", Body: "t", Attrs: map[string]string{
				"Name": "A", "Canvas.Left": "1", "Canvas.Top": "1"}},
		}}
	}
	cases := []struct {
		name  string
		mut   func(n *node)
		wantK bool // true: coalescable (exactly one attribute moved)
	}{
		{"one attribute changed", func(n *node) { n.Kids[0].Attrs["Name"] = "B" }, true},
		{"one attribute added", func(n *node) { n.Kids[0].Attrs["Tooltip"] = "x" }, true},
		{"one attribute removed", func(n *node) { delete(n.Kids[0].Attrs, "Name") }, true},
		{"body changed", func(n *node) { n.Kids[0].Body = "u" }, true},
		{"TWO attributes (a drag)", func(n *node) {
			n.Kids[0].Attrs["Canvas.Left"] = "9"
			n.Kids[0].Attrs["Canvas.Top"] = "9"
		}, false},
		{"a child added", func(n *node) { n.Kids = append(n.Kids, &node{Elem: "Text"}) }, false},
		{"a child removed", func(n *node) { n.Kids = nil }, false},
		{"retyped", func(n *node) { n.Elem = "VStack" }, false},
		{"nothing changed", func(n *node) {}, false},
	}
	for _, tc := range cases {
		old, cur := base(), base()
		tc.mut(cur)
		got := editKey(old, cur) != ""
		if got != tc.wantK {
			t.Errorf("%s: coalescable=%v, want %v", tc.name, got, tc.wantK)
		}
	}
}

// ---- the pointer-identity contract other slices depend on ----

// TestUndoGivesTheSelectionAFreshPointer is a CROSS-SLICE CONTRACT, and
// it is pinned here because another slice now relies on it.
//
// The properties pane's editors are non-modal by necessity — the caret
// editor must let runes through to its TextBox — so the page-root
// KeyBindings fire underneath them, ctrl+n among them. It found a real
// bug that way: a *correct* re-resolve through ed.target() writes to
// whatever is selected NOW, so moving the selection with an editor open
// put the write on the wrong element. Its fix records the node the editor
// OPENED on, for identity only, and retires the editor when
// `ed.sel != p.on`.
//
// That makes "undo hands back a DIFFERENT *node" load-bearing outside
// this file. It is true by construction — restore installs a fresh deep
// copy and re-resolves the selection by index path, so every pointer in
// the tree is new — but "true by construction" is what an optimisation
// quietly breaks. A restore that patched the existing tree in place would
// leave ed.sel identical, their editor would not retire, and it would go
// on showing a value from a document that no longer exists.
func TestUndoGivesTheSelectionAFreshPointer(t *testing.T) {
	ed, _ := undoFixture(t)
	ed.sel = ed.doc().Kids[0]

	typeInto(t, ed, ed.sel, "Tooltip", "abc")
	beforeSel, beforeRoot := ed.sel, ed.root

	ed.undo()
	if ed.sel == beforeSel {
		t.Error("undo left ed.sel as the SAME *node: a slice that retires an open editor on " +
			"`ed.sel != opened` cannot tell the document was replaced, and goes on editing a " +
			"node the document no longer holds")
	}
	if ed.root == beforeRoot {
		t.Error("undo left ed.root as the same *node")
	}
	if ed.sel == nil || ed.parentOf(ed.sel) == nil {
		t.Fatal("undo left the selection outside the document")
	}
	// A fresh pointer to the SAME logical element, not a jump.
	if got := ed.sel.Attrs["Name"]; got != "A" {
		t.Errorf("after undo the selection names %q, want the element that was selected, \"A\"", got)
	}

	// Redo replaces the tree too, so the same holds.
	afterUndo := ed.sel
	ed.redo()
	if ed.sel == afterUndo {
		t.Error("redo left ed.sel as the SAME *node")
	}
}

// TestAnUndoThatDoesNothingLeavesTheSelectionPointerALONE is the inverse
// half of the same contract, and it is the one that would break the other
// slice by firing too OFTEN rather than too seldom.
//
// A retire keyed on pointer inequality closes the editor whenever the
// pointer moves. So every path that does NOT replace the document must
// leave ed.sel strictly alone — otherwise an open caret editor is retired
// out from under the user for no reason.
//
// Two such paths, and the second is the one this slice could easily have
// got wrong: ctrl+z at the bottom of the stack, and a COALESCED
// keystroke. Coalescing merges a run into one history entry; implemented
// as "restore to the run's start and re-apply" it would replace the tree
// on every rune, and the properties pane's editor would retire on every
// keystroke of the value it is editing. It is implemented as a forward
// update of the baseline instead, which touches neither ed.root nor
// ed.sel.
func TestAnUndoThatDoesNothingLeavesTheSelectionPointerALONE(t *testing.T) {
	ed, _ := undoFixture(t)
	ed.sel = ed.doc().Kids[0]

	// (1) ctrl+z with an empty stack.
	sel, root := ed.sel, ed.root
	if ed.CanUndo() {
		t.Fatal("the fixture has history; this arm needs the empty stack")
	}
	ed.undo()
	if ed.sel != sel || ed.root != root {
		t.Error("an undo with nothing to undo replaced the tree: an open editor keyed on " +
			"pointer identity is retired for a keystroke that did nothing")
	}

	// (2) a coalesced run — six keystrokes into one field.
	sel, root = ed.sel, ed.root
	typeInto(t, ed, ed.sel, "Tooltip", "hello!")
	if ed.root != root {
		t.Error("a coalesced run replaced ed.root: an editor keyed on pointer identity " +
			"retires on every keystroke of the value it is editing")
	}
	if ed.sel != sel {
		t.Error("a coalesced run replaced ed.sel")
	}
	if n := len(ed.history().undo); n != 1 {
		t.Fatalf("the run recorded %d steps, want 1; the arm above proved nothing", n)
	}
}

// ---- rebuild's OTHER TWO EXITS ----

// TestAnEditThatBreaksTheBuildIsStillUndoable.
//
// rebuild has THREE exits — a remote push, a failed build, and the
// success path — and every test above this point walks only the third.
// That gap came from the properties-pane slice, which found the same hole
// on its own side: a behavioural test survives any mechanism ON THE PATH
// IT WALKS, which is not the same as surviving any mechanism.
//
// This is the failed-build exit, and it is not an edge case — it is the
// single most important thing undo does. You type a value the document
// cannot load, the preview stops updating and the status bar goes "✗",
// and ctrl+z is the ONLY way back. An editor whose undo stops recording
// exactly when the document stops compiling has abandoned the user in the
// one state they cannot get out of by hand.
//
// It works because recordHistory runs at the TOP of rebuild, before the
// early return. A hook at the bottom would record nothing here.
func TestAnEditThatBreaksTheBuildIsStillUndoable(t *testing.T) {
	ed, _ := undoFixture(t)
	before := docState(ed)

	ed.sel = ed.doc().Kids[0]
	// "abc" is not an int, so the document no longer loads.
	editAttr(t, ed, "Canvas.Left", "abc")

	if !strings.HasPrefix(ed.status.Get(), "✗") {
		t.Fatalf("the document still builds (%q); this test is not on the failed-build exit",
			ed.status.Get())
	}
	if !ed.CanUndo() {
		t.Fatal("an edit that broke the build recorded no history: undo stops working exactly " +
			"when it is the only way back")
	}
	ed.undo()
	if got := docState(ed); got != before {
		t.Errorf("ctrl+z out of a broken document gave\n%s\nwant\n%s", got, before)
	}
	if !strings.HasPrefix(ed.status.Get(), "✓") {
		t.Errorf("after undoing back to a good document the status is %q, want it building",
			ed.status.Get())
	}
}

// TestTypingThroughABrokenStateIsStillOneUndoStep. The failed-build exit
// again, crossed with coalescing — because a half-typed value is USUALLY
// invalid, so a run of keystrokes passes through broken states on its way
// to a good one. Those intermediate failures must not each become their
// own undo step.
func TestTypingThroughABrokenStateIsStillOneUndoStep(t *testing.T) {
	ed, _ := undoFixture(t)
	before := docState(ed)
	target := ed.doc().Kids[0]

	// "1", "12", "12x", "12" — the third does not load.
	for _, v := range []string{"1", "12", "12x", "12"} {
		ed.sel = target
		editAttr(t, ed, "Canvas.Left", v)
	}
	if !strings.HasPrefix(ed.status.Get(), "✓") {
		t.Fatalf("the run did not end on a loadable document: %q", ed.status.Get())
	}
	if n := len(ed.history().undo); n != 1 {
		t.Errorf("a run that passed through a broken state recorded %d undo steps, want 1", n)
	}
	ed.undo()
	if got := docState(ed); got != before {
		t.Errorf("one ctrl+z did not undo the whole run:\n%s\nwant\n%s", got, before)
	}
}

// TestRemoteModeRecordsHistoryAndNotTheIslandRename is rebuild's THIRD
// exit, and it is the one where the placement of recordHistory makes two
// separate claims that were both written down and neither verified.
//
// Claim one: a hook at the BOTTOM of rebuild would never run in remote
// mode at all, because the remote branch returns first. Editing another
// app's island would silently have no undo — the mode where a mistake is
// least recoverable, since the document is being pushed into a process
// this editor does not own.
//
// Claim two: fragmentFor temporarily renames the surface to the island's
// name and restores it with a defer (remotemode.go). A hook that ran
// while that rename was live would record a document the user never
// wrote, and undoing to it would put the island's name into their file.
// The rename happens INSIDE pushRemote, which the top-placed hook runs
// before — so the recorded state must carry the editor's own "Surface",
// never the island name.
//
// Both are asserted against a real target over a real loopback listener,
// because remote mode's whole point is that the other process decides.
func TestRemoteModeRecordsHistoryAndNotTheIslandRename(t *testing.T) {
	ed, _ := attachedEditor(t)
	if ed.remote == nil {
		t.Fatal("the fixture is not attached; this test is not on the remote exit")
	}
	// attachedEditor wires the remote but never MOUNTS the page, and the
	// property browser made that matter: the editing surface is a
	// <ValueEditor> in the markup, so on an unmounted editor ed.props is
	// nil and beginEdit/commitEdit both return without doing anything.
	// The edit below would land nowhere and this test would report "no
	// history in remote mode" — blaming rebuild for a mutation that never
	// reached it. Mounted here rather than in the shared fixture so the
	// other remote tests keep the shape they were written against.
	mountPage(t, ed)
	if ed.props == nil {
		t.Fatal("the page mounted no <ValueEditor>; commitEdit cannot write and this test would blame rebuild")
	}
	ed.hist = nil
	ed.rebuild() // baseline, in remote mode
	before := docState(ed)

	ed.sel = ed.doc().Kids[0]
	// Through attrSel + beginEdit rather than by setting editName.
	//
	// This test used to write ed.editName directly, which worked while
	// commitEdit read that property. The property browser made the
	// editing surface an overlay that resolves its own subject on Open —
	// valueEditor.Write uses p.name, set by beginEdit — so a bare
	// editName.Set now writes a name nothing reads, Write returns early,
	// no rebuild happens and the history this test is about is never
	// recorded. It failed with "an edit in REMOTE mode recorded no
	// history", which names the wrong cause: the hook is still at the top
	// of rebuild, and it was the EDIT that never reached rebuild at all.
	i, _ := rowIndex(t, ed, "Canvas.Left")
	ed.attrSel.Set(i)
	ed.beginEdit()
	ed.editValue.Set("7")
	ed.commitEdit()

	if !ed.CanUndo() {
		t.Fatal("an edit in REMOTE mode recorded no history: rebuild returns early for the " +
			"remote push, so a hook below that point never runs and driving another app has " +
			"no undo at all")
	}
	// The recorded state is the EDITOR's document, not the wire fragment.
	rec := ed.history().undo[len(ed.history().undo)-1].root
	if got := rec.Attrs["Name"]; got != "Surface" {
		t.Errorf("the recorded surface is named %q, want \"Surface\": history captured "+
			"fragmentFor's temporary island rename, and undoing would write the island's "+
			"name into the user's document", got)
	}
	if got := rec.markup(""); strings.Contains(got, "Name=\"Island\"") {
		t.Errorf("the recorded document mentions the island:\n%s", got)
	}

	ed.undo()
	if got := docState(ed); got != before {
		t.Errorf("ctrl+z in remote mode gave\n%s\nwant\n%s", got, before)
	}
}

// TestUndoDoesNotBuryTheBuildStatusForever is a bug the status bar hides
// BY WORKING AS DESIGNED.
//
// statusText prefers a non-empty drag hint over a healthy build status —
// only a "✗" outranks it — which is right, because a hint is news about
// the gesture you just made. It goes wrong the moment nothing retires
// the hint: undo and redo end in sayDrag("undone: …") with no drag ever
// involved, so the bar kept reading "undone: …" through every later
// edit, reporting a gesture two edits ago as the current state.
//
// All three arms, because the fix is a CLEAR and a clear is easy to
// over-apply: the hint the current gesture just set must survive (or
// undo is silent), the stale one must not, and an error must still
// outrank both.
func TestUndoDoesNotBuryTheBuildStatusForever(t *testing.T) {
	ed, c := undoFixture(t)

	// A mutation to undo.
	ed.doc().Kids = append(ed.doc().Kids, &node{
		Elem: "Text", Body: "cccc",
		Attrs: map[string]string{"Name": "C", "Canvas.Left": "1", "Canvas.Top": "11"},
	})
	ed.rebuild()
	settle(t, c)
	if !ed.CanUndo() {
		t.Fatal("the edit recorded no history, so there is nothing to undo")
	}

	// ARM ONE — the hint undo itself sets MUST survive its own rebuild.
	// A gesture that changes the screen without saying so is
	// indistinguishable from one that did nothing.
	ed.undo()
	if got := ed.dragHint.Get(); got == "" {
		t.Fatal("undo said nothing: the hint it sets was cleared by its own " +
			"rebuild, so the gesture is silent")
	}
	if got := ed.statusText.Get(); !strings.Contains(got, "undone") {
		t.Errorf("the status bar shows %q right after an undo; the undo is the news", got)
	}

	// ARM TWO — the next edit retires it. This is the bug: before the fix
	// the bar still read "undone: …" here, for the rest of the session.
	ed.doc().Kids = append(ed.doc().Kids, &node{
		Elem: "Text", Body: "dddd",
		Attrs: map[string]string{"Name": "D", "Canvas.Left": "1", "Canvas.Top": "16"},
	})
	ed.rebuild()
	settle(t, c)
	if !strings.HasPrefix(ed.status.Get(), "✓") {
		t.Fatalf("the second edit does not build: %s", ed.status.Get())
	}
	if got := ed.dragHint.Get(); got != "" {
		t.Errorf("after a later edit the drag hint is still %q: nothing retires "+
			"it, so it covers the build status from here on", got)
	}
	if got := ed.statusText.Get(); strings.Contains(got, "undone") {
		t.Errorf("the status bar still shows %q after a later edit; it is "+
			"reporting a gesture two edits ago as the current state", got)
	}

	// ARM THREE — an ERROR still outranks a hint. The clear must not have
	// disturbed the ordering statusText exists for.
	ed.sayDrag("a hint")
	ed.status.Set("✗ markup: something is wrong")
	if got := ed.statusText.Get(); !strings.HasPrefix(got, "✗") {
		t.Errorf("the status bar shows %q while the document does not build; the "+
			"error is what explains everything else", got)
	}
}

// TestARunThatReturnsToItsStartDoesNotSwallowTheNextEdit pins the one
// way a coalescing run can lose an edit outright.
//
// A run's exit condition and its entry condition are two different
// pieces of state, and only one of them was being unwound. Coming back
// to the value the run started from pops the step it pushed — right,
// because there is nothing left to undo — but the baseline went on
// carrying the run's KEY. The next edit to the same field then matched
// that key, coalesced into a run whose step was no longer on the stack,
// and became unreachable: ctrl+z answered "nothing to undo" over a
// document the user had just changed.
//
// It takes three edits to reproduce and the middle one has to land
// exactly on the starting value, which is why the table above cannot
// see it — every case there is a single edit, and a single edit can
// only be the step that gets pushed.
func TestARunThatReturnsToItsStartDoesNotSwallowTheNextEdit(t *testing.T) {
	ed, c := undoFixture(t)
	ed.sel = ed.doc().Kids[0]
	start := ed.doc().Kids[0].Attrs["Canvas.Left"]

	editAttr(t, ed, "Canvas.Left", "17")
	editAttr(t, ed, "Canvas.Left", start) // back where it began: no edit at all
	if ed.CanUndo() {
		t.Fatalf("a run that returned to its start left %d steps behind",
			len(ed.history().undo))
	}

	editAttr(t, ed, "Canvas.Left", "42")
	if got := ed.doc().Kids[0].Attrs["Canvas.Left"]; got != "42" {
		t.Fatalf("the third edit did not land: Canvas.Left is %q", got)
	}
	if !ed.CanUndo() {
		t.Fatal("the edit after a returned run recorded no history: " +
			"ctrl+z cannot reach it")
	}

	settle(t, c)
	if !ctrlZ(c) {
		t.Fatal("ctrl+z was not consumed")
	}
	if got := ed.doc().Kids[0].Attrs["Canvas.Left"]; got != start {
		t.Errorf("after ctrl+z Canvas.Left is %q, want the pre-edit %q", got, start)
	}
}

// notATableMutator is every rebuild-reaching function that is
// deliberately NOT a row of the undo table, with the reason it is not.
//
// It is an enumeration, and that is the point: it is the only kind that
// cannot go stale silently, because the derivation below fails on any
// name that is in neither this map nor the table. A new mutator is red
// until somebody either covers it or writes down why it needs no cover.
// "Why" is a sentence here rather than a bare name, because the failure
// this whole file guards against is a mutator that looks covered.
var notATableMutator = map[string]string{
	"main": "the program's first build, before any document exists to edit",
	"restore": "undo's own replay — recording it would make ctrl+z " +
		"an undo step of its own",
	"Release": "the drag, covered by TestOneDragRebuildsExactlyOnceAnd" +
		"RecordsOneUndoStep, which pins the STEP COUNT over seven " +
		"motions — an assertion a one-shot table row cannot make",
	"openWorkspaceFile": "loads a different document, so there is no " +
		"outgoing state in this one for undo to bring back",
}

// TestTheUndoTableNamesEveryMutatorInTheSource is what makes the table
// above deserve its name.
//
// The table is hand-written and its name is a claim about ALL of them,
// which is a claim a hand-written list cannot keep. It did not: review
// of #392 found it silently five mutators short — moveSelected,
// promoteSelected, demoteSelected, the track verbs and pasteMarkup had
// no row, and nothing anywhere went red about it. Adding the rows fixes
// today; deriving the set is what stops tomorrow.
//
// The seam it derives from is the same one undo derives from: a
// document mutator is a function that reaches ed.rebuild(), because
// that is where recordHistory runs. So this reads the package's own
// source for the functions that do, rather than trusting a second list
// to agree with the first.
//
// It also checks the other direction — that every name the table gives
// is a function that EXISTS. That is not hypothetical either: undo.go's
// own header named cycleValue, a symbol this package has never had, and
// a comment cannot be compiled.
func TestTheUndoTableNamesEveryMutatorInTheSource(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}

	reaches := map[string]bool{} // func name -> reaches rebuild()
	exists := map[string]bool{}
	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", name, err)
		}
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			exists[fn.Name.Name] = true
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if sel.Sel.Name == "rebuild" || sel.Sel.Name == "applyEdit" {
					reaches[fn.Name.Name] = true
				}
				return true
			})
		}
	}
	if len(reaches) == 0 {
		t.Fatal("the scan found no function reaching rebuild(), so it " +
			"would pass for an editor with no undo at all")
	}

	covered := map[string]bool{}
	for _, tc := range undoCases(t) {
		if tc.mutator == "" {
			t.Errorf("the %q row names no mutator, so nothing can check "+
				"that it covers one", tc.name)
			continue
		}
		if !exists[tc.mutator] {
			t.Errorf("the %q row names mutator %q, which is not a function "+
				"in this package", tc.name, tc.mutator)
		}
		covered[tc.mutator] = true
	}

	var missing []string
	for name := range reaches {
		if covered[name] || notATableMutator[name] != "" {
			continue
		}
		missing = append(missing, name)
	}
	sort.Strings(missing)
	for _, name := range missing {
		t.Errorf("%s reaches rebuild() — so it records history and ctrl+z "+
			"is expected to undo it — but no row of undoCases covers it "+
			"and notATableMutator gives no reason it needs none", name)
	}

	// A reason left behind for a function that no longer exists is the
	// same defect pointed the other way: it reads as a considered
	// exemption and covers nothing.
	for name := range notATableMutator {
		if !exists[name] {
			t.Errorf("notATableMutator excuses %q, which is not a function "+
				"in this package", name)
		}
	}
}
