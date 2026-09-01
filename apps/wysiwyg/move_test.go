package main

import (
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/input"
)

// MOVE — reordering and reparenting a node that already exists.
//
// The editor could add a node and delete one, and nothing in between.
// Getting a child into a different position meant deleting it and adding
// it again, which loses every attribute on it and every descendant under
// it. That is not a shortcut a user can take: the attributes ARE the
// work.
//
// So the property under test is never "the tree changed shape" on its
// own. It is that the shape changed AND the node came through intact —
// same pointer, same attributes, same children, still selected. A move
// implemented as delete-then-add would satisfy a shape assertion
// perfectly and fail every one of those.

// moveFixture is a VStack root holding three named Texts, so there is an
// unambiguous order to permute and a middle element with a neighbour on
// each side. Attributes are deliberately present on the node that moves:
// they are what distinguishes a move from a re-add.
func moveFixture(t *testing.T) (*editor, *gooey.Composer) {
	t.Helper()
	ed, c, _ := designerPageCounting(t)
	ed.doc().Elem = "VStack"
	ed.doc().Attrs = map[string]string{"Name": "Root"}
	ed.doc().Kids = []*node{
		{Elem: "Text", Body: "aaa", Attrs: map[string]string{"Name": "A"}},
		{Elem: "Text", Body: "bbb", Attrs: map[string]string{"Name": "B", "Bold": "true"}},
		{Elem: "Text", Body: "ccc", Attrs: map[string]string{"Name": "C"}},
	}
	ed.rebuild()
	if !strings.HasPrefix(ed.status.Get(), "✓") {
		t.Fatalf("fixture does not build: %s", ed.status.Get())
	}
	c.Frame()
	return ed, c
}

func kidNames(n *node) string {
	var b strings.Builder
	for i, k := range n.Kids {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(k.Attrs["Name"])
	}
	return b.String()
}

// TestMovingASiblingReordersItAndKeepsItIntact is the feature, and the
// three assertions after the order check are what make it a move.
func TestMovingASiblingReordersItAndKeepsItIntact(t *testing.T) {
	ed, _ := moveFixture(t)
	b := ed.doc().Kids[1]
	ed.sel = b

	if !ed.moveSelected(-1) {
		t.Fatal("moving B up reported no change; the rest of this test would prove nothing")
	}
	if got := kidNames(ed.doc()); got != "B,A,C" {
		t.Fatalf("order is %q after moving B up, want \"B,A,C\"", got)
	}

	// The node came THROUGH the move rather than being rebuilt by it.
	if ed.doc().Kids[0] != b {
		t.Error("the node at the new position is not the node that moved: " +
			"a move that re-creates the node silently drops its identity, and " +
			"anything holding a pointer to it (a drag, the inspector) is now stale")
	}
	if ed.doc().Kids[0].Attrs["Bold"] != "true" {
		t.Errorf("Bold survived as %q, want \"true\": losing attributes is exactly "+
			"what made delete-and-re-add unusable", ed.doc().Kids[0].Attrs["Bold"])
	}
	if ed.sel != b {
		t.Errorf("selection is %s after the move, want the node that moved: "+
			"a user pressing the key twice must move the same node twice",
			nodeName(ed.sel))
	}
	if !strings.HasPrefix(ed.status.Get(), "✓") {
		t.Errorf("the document does not build after a move: %s", ed.status.Get())
	}
}

// TestMovingDownIsTheInverseOfMovingUp pins that the two directions are
// one implementation and not two, which is where an off-by-one lives.
func TestMovingDownIsTheInverseOfMovingUp(t *testing.T) {
	ed, _ := moveFixture(t)
	ed.sel = ed.doc().Kids[1]

	if !ed.moveSelected(1) {
		t.Fatal("moving B down reported no change")
	}
	if got := kidNames(ed.doc()); got != "A,C,B" {
		t.Fatalf("order is %q after moving B down, want \"A,C,B\"", got)
	}
	if !ed.moveSelected(-1) {
		t.Fatal("moving B back up reported no change")
	}
	if got := kidNames(ed.doc()); got != "A,B,C" {
		t.Fatalf("order is %q after moving B back up, want the original \"A,B,C\"", got)
	}
}

// TestMovingPastTheEndDoesNothingAndSaysSo is the guard, and the return
// value is the assertion.
//
// Reporting false rather than clamping silently is what lets the caller
// avoid a rebuild — and a move that rebuilt on every refused keypress
// would repaint the whole designer subtree for nothing.
func TestMovingPastTheEndDoesNothingAndSaysSo(t *testing.T) {
	ed, _ := moveFixture(t)

	for _, tc := range []struct {
		name string
		idx  int
		d    int
	}{
		{"first up", 0, -1},
		{"last down", 2, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ed.sel = ed.doc().Kids[tc.idx]
			before := kidNames(ed.doc())
			if ed.moveSelected(tc.d) {
				t.Error("a move past the end reported that it changed something")
			}
			if got := kidNames(ed.doc()); got != before {
				t.Fatalf("order is %q, want it unchanged at %q", got, before)
			}
		})
	}
}

// TestTheUsersRootCannotBeMoved pins the OUTCOME, and deliberately says
// so rather than pretending to pin the guard.
//
// A mutation deleting movable's isSurface check leaves this test green —
// I ran it. The surface holds exactly one child, so all three operations
// decline on arithmetic before the guard is consulted: the reorder's
// target index falls outside a one-element slice, promote finds no
// grandparent, demote finds no preceding sibling. The guard is real
// defence for the day the surface holds two children, but it is not the
// mechanism under test here and no test in this file can make it be.
//
// Left as an outcome assertion instead of being deleted or dressed up:
// "the root does not move and the surface keeps one child" is worth
// holding whichever line enforces it, and claiming more than that in the
// name would be the failure this repo keeps finding — a test name
// outrunning its assertion.
func TestTheUsersRootCannotBeMoved(t *testing.T) {
	ed, _ := moveFixture(t)
	ed.sel = ed.doc()

	for _, d := range []int{-1, 1} {
		if ed.moveSelected(d) {
			t.Errorf("moving the user's root by %d reported a change", d)
		}
	}
	if len(ed.root.Kids) != 1 {
		t.Fatalf("the surface holds %d children, want exactly 1", len(ed.root.Kids))
	}
}

// TestPromoteLiftsANodeToItsGrandparentAfterItsFormerParent.
//
// The INDEX is the whole assertion. Appending to the grandparent instead
// would also "work" — the node is out of its old parent and the document
// still builds — but it teleports to the bottom of a list the user was
// looking at, which reads as a bug in the move rather than a choice.
func TestPromoteLiftsANodeToItsGrandparentAfterItsFormerParent(t *testing.T) {
	ed, _ := moveFixture(t)
	inner := &node{Elem: "VStack", Attrs: map[string]string{"Name": "Inner"}, Kids: []*node{
		{Elem: "Text", Body: "ddd", Attrs: map[string]string{"Name": "D"}},
	}}
	ed.doc().Kids = []*node{ed.doc().Kids[0], inner, ed.doc().Kids[2]}
	ed.rebuild()

	d := inner.Kids[0]
	ed.sel = d
	if !ed.promoteSelected() {
		t.Fatal("promote reported no change")
	}
	if len(inner.Kids) != 0 {
		t.Fatalf("Inner still holds %d children, want 0", len(inner.Kids))
	}
	if got := kidNames(ed.doc()); got != "A,Inner,D,C" {
		t.Fatalf("order is %q after promoting D, want \"A,Inner,D,C\" — "+
			"immediately after its former parent, not appended to the end", got)
	}
	if ed.doc().Kids[2] != d {
		t.Error("the promoted node is not the node that was selected")
	}
	if ed.sel != d {
		t.Errorf("selection is %s after promote, want the promoted node", nodeName(ed.sel))
	}
}

// TestPromoteRefusesToCreateASecondRoot. Promoting a child of the user's
// root would make it a sibling of the root, i.e. a second child of the
// surface — the case TestTheUsersRootCannotBeMoved describes, reached
// from the other side.
func TestPromoteRefusesToCreateASecondRoot(t *testing.T) {
	ed, _ := moveFixture(t)
	ed.sel = ed.doc().Kids[1]

	if ed.promoteSelected() {
		t.Error("promoting a child of the user's root reported a change")
	}
	if len(ed.root.Kids) != 1 {
		t.Fatalf("the surface holds %d children, want exactly 1", len(ed.root.Kids))
	}
	if got := kidNames(ed.doc()); got != "A,B,C" {
		t.Fatalf("order is %q, want it unchanged", got)
	}
}

// TestDemoteNestsANodeIntoThePrecedingSibling.
func TestDemoteNestsANodeIntoThePrecedingSibling(t *testing.T) {
	ed, _ := moveFixture(t)
	holder := &node{Elem: "VStack", Attrs: map[string]string{"Name": "Holder"}}
	ed.doc().Kids = []*node{holder, ed.doc().Kids[1], ed.doc().Kids[2]}
	ed.rebuild()

	b := ed.doc().Kids[1]
	ed.sel = b
	if !ed.demoteSelected() {
		t.Fatal("demote reported no change")
	}
	if got := kidNames(ed.doc()); got != "Holder,C" {
		t.Fatalf("order is %q after demoting B, want \"Holder,C\"", got)
	}
	if len(holder.Kids) != 1 || holder.Kids[0] != b {
		t.Fatalf("Holder holds %s, want the demoted node", kidNames(holder))
	}
	if ed.sel != b {
		t.Errorf("selection is %s after demote, want the demoted node", nodeName(ed.sel))
	}
	if !strings.HasPrefix(ed.status.Get(), "✓") {
		t.Errorf("the document does not build after a demote: %s", ed.status.Get())
	}
}

// TestDemoteRefusesALeafAsTheNewParent is the guard that a shape
// assertion cannot make.
//
// Nesting into a <Text> produces markup that still SAVES and still
// builds — leaves silently discard children — so the child would simply
// vanish with no error anywhere. That silent-drop is a documented class
// in this framework, which is why the refusal has to happen here rather
// than being caught downstream.
func TestDemoteRefusesALeafAsTheNewParent(t *testing.T) {
	ed, _ := moveFixture(t)
	ed.sel = ed.doc().Kids[1] // B, preceded by A, a <Text>

	if ed.demoteSelected() {
		t.Error("demoting into a <Text> reported a change: a leaf discards children silently, " +
			"so the node would disappear from the document with nothing to report it")
	}
	if got := kidNames(ed.doc()); got != "A,B,C" {
		t.Fatalf("order is %q, want it unchanged", got)
	}
	if len(ed.doc().Kids[0].Kids) != 0 {
		t.Fatal("the leaf was given a child")
	}
}

// TestDemoteRefusesTheFirstChild — there is no preceding sibling to nest
// into. Distinct from the leaf case: this one is about position, and
// falling back to the FOLLOWING sibling would be a different feature
// that moves the node in the direction the user did not ask for.
func TestDemoteRefusesTheFirstChild(t *testing.T) {
	ed, _ := moveFixture(t)
	ed.sel = ed.doc().Kids[0]

	if ed.demoteSelected() {
		t.Error("demoting the first child reported a change")
	}
	if got := kidNames(ed.doc()); got != "A,B,C" {
		t.Fatalf("order is %q, want it unchanged", got)
	}
}

// TestTheMoveGesturesAreWiredAsShipped goes through the page's own
// KeyBindings rather than calling the methods, because everything above
// would pass with the four bindings absent from wysiwyg.gooey entirely.
//
// A method nothing can reach is the same defect as a test nothing can
// fail: the feature is present in the package and missing from the
// editor.
//
// THE EVENT COMES FROM ParseGesture, and it did not until #427 — the
// arms hand-built input.KeyEvent{KeyRune, 'j', ModCtrl}, which is a
// value NO TERMINAL CAN SEND. 0x0a is the byte for ctrl+j and the
// decoder reads it as enter, so the shipped binding had never fired
// once while this test proved it was wired. The harness was doing the
// thing under test: it manufactured the event the decoder refuses to
// manufacture, and then agreed with itself.
//
// Building through ParseGesture closes that, because ParseGesture now
// rejects an unproducible spelling — so an arm here cannot assert a
// dead gesture fires without failing at construction.
func TestTheMoveGesturesAreWiredAsShipped(t *testing.T) {
	gesture := func(t *testing.T, g string) input.Event {
		t.Helper()
		ev, err := input.ParseGesture(g)
		if err != nil {
			t.Fatalf("the page binds %q and ParseGesture refuses it: %v", g, err)
		}
		return input.KeyOf(ev)
	}
	for _, tc := range []struct {
		name string
		want string // "" — consumed is the whole claim; nesting is tested above
	}{
		{"alt+k", "B,A,C"},
		{"alt+j", "A,C,B"},
		{"alt+h", ""},
		{"alt+l", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ed, c := moveFixture(t)
			ed.sel = ed.doc().Kids[1]
			if !c.Handle(gesture(t, tc.name)) {
				t.Fatalf("%s was not consumed: the binding is not on the page", tc.name)
			}
			if tc.want == "" {
				return
			}
			if got := kidNames(ed.doc()); got != tc.want {
				t.Fatalf("order is %q after %s, want %q", got, tc.name, tc.want)
			}
		})
	}
}

// TestMoveWithNothingSelectedIsARefusal — every other editor command
// guards this, and a nil deref here would take the app down on a
// keypress that is legal at any time.
func TestMoveWithNothingSelectedIsARefusal(t *testing.T) {
	ed, _ := moveFixture(t)
	ed.sel = nil

	if ed.moveSelected(1) || ed.promoteSelected() || ed.demoteSelected() {
		t.Error("a move with nothing selected reported a change")
	}
}

// TestPromoteRefusesAGrandparentThatWillNotTakeTheChild is the gate arm of
// issue #403. <Tabs> declares Only:["Tab"], so a <Text> promoted out of its
// <Tab> would land directly in the <Tabs> — a child the loader refuses.
//
// The old code did it anyway AND returned true, so the caller was told the
// move succeeded while docRoot went nil and click-to-select died for the
// whole document.
func TestPromoteRefusesAGrandparentThatWillNotTakeTheChild(t *testing.T) {
	ed, tabs := tabsFixture(t)
	tab := tabs.Kids[0]
	text := tab.Kids[0]
	ed.sel = text

	if ed.promoteSelected() {
		t.Errorf("promoting a <Text> out of its <Tab> reported SUCCESS; <Tabs> declares " +
			"Only:[\"Tab\"] and cannot take it")
	}
	if got := kidElems(tabs); len(tabs.Kids) != 1 || tabs.Kids[0] != tab {
		t.Errorf("the <Tabs> now holds %v, want the single untouched <Tab>", got)
	}
	if len(tab.Kids) != 1 || tab.Kids[0] != text {
		t.Errorf("the <Tab> lost its child to a refused promote: %v", kidElems(tab))
	}
	if ed.docRoot == nil {
		t.Errorf("a REFUSED promote still broke the document: %s", ed.status.Get())
	}
	if s := ed.status.Get(); !strings.Contains(s, "Text") || !strings.Contains(s, "Tabs") {
		t.Errorf("status %q names neither the child nor the container it was refused by", s)
	}
}

// TestAPromoteThatEmptiesItsParentIsReverted is the arm a catalog gate
// cannot cover, and it is why the transactional half is not redundant with
// canHold.
//
// Here the grandparent DOES accept the child — <Tab> takes one content
// child, and canHold is deliberately permissive for ModeOne because it
// cannot know the slot is taken. The document still fails to build, for the
// opposite reason: the <Tab> ends up with two children. A gate reading only
// "may this container hold this element" answers yes and is still wrong.
func TestAPromoteThatEmptiesItsParentIsReverted(t *testing.T) {
	ed, tabs := tabsFixture(t)
	tab := tabs.Kids[0]
	inner := &node{
		Elem:  "VStack",
		Attrs: map[string]string{"Name": "Inner"},
		Kids:  []*node{{Elem: "Text", Body: "Deep", Attrs: map[string]string{"Name": "Deep"}}},
	}
	tab.Kids = []*node{inner}
	ed.rebuild()
	if ed.docRoot == nil {
		t.Fatalf("fixture does not build before the promote: %s", ed.status.Get())
	}
	deep := inner.Kids[0]
	ed.sel = deep

	if ed.promoteSelected() {
		t.Errorf("promoting into a <Tab> that already holds its one content child " +
			"reported success")
	}
	// REVERTED, not merely refused: the node is back where it started and
	// the document builds again. A refusal that left the tree mutated would
	// pass a `return false` assertion and still have destroyed the document.
	if len(inner.Kids) != 1 || inner.Kids[0] != deep {
		t.Errorf("the <VStack> holds %v after the revert, want its original <Text>",
			kidElems(inner))
	}
	if len(tab.Kids) != 1 || tab.Kids[0] != inner {
		t.Errorf("the <Tab> holds %v after the revert, want the single <VStack>",
			kidElems(tab))
	}
	if ed.docRoot == nil {
		t.Errorf("the document did not build again after the revert: %s", ed.status.Get())
	}
}
