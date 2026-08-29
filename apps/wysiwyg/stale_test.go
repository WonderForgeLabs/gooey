package main

import (
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/input"
)

// Three findings from #292's review, each dormant on this HEAD and each
// one feature away from being live. They are pinned here rather than
// left as prose, because "unreachable today" is a fact about the current
// call sites and nothing was enforcing it.

// pressDelete is the delete gesture through the page's own KeyBinding.
// The gesture is `x`, not the Delete key — going through Handle rather
// than calling deleteSelected is what makes this a test of the binding
// as shipped.
func pressDelete(c *gooey.Composer) bool {
	return c.Handle(input.KeyOf(input.Rune('x')))
}

// FINDING 2: a delete mid-gesture orphans the drag.
//
// Keys and mouse reports share ONE ordered stream, so `x` lands between
// a press and its release perfectly well. Before the fix, deleteSelected
// unlinked the node while dragState went on holding it: Drag then wrote
// Left/Top to a component no longer mounted and Release wrote
// Canvas.Left onto a node the next rebuild did not serialise. Nothing
// crashed and nothing appeared — the write was simply lost.
func TestADeleteMidDragDoesNotLeaveTheGestureHoldingAnOrphan(t *testing.T) {
	ed, c, _ := dragFixture(t)
	a := docKid(ed, 0)
	b0 := a.(interface{ Bounds() gooey.Rect }).Bounds()

	if !press(c, b0.X, b0.Y) {
		t.Fatal("the press was not consumed")
	}
	if !ed.drag.active() {
		t.Fatal("the press did not begin a drag; the rest of this test would prove nothing")
	}
	victim := ed.drag.node
	if !pressDelete(c) {
		t.Fatal("the delete binding did not fire")
	}
	if ed.parentOf(victim) != nil {
		t.Fatal("the delete did not actually unlink the node")
	}

	// The gesture continues, because the pointer does not know.
	//
	// CONSUMPTION is the assertion, and it is the only one that
	// discriminates. Asserting "ed.drag is empty afterwards" passes
	// either way — Release zeroes the state on its way out regardless —
	// and asserting "nothing was written" passes too, because the write
	// lands on an orphan nobody serialises. What actually changes is
	// whether a dead gesture still CLAIMS the pointer: a drag holding a
	// node that left the document must decline the event and let it fall
	// through, not swallow it and report success.
	if motion(c, b0.X+4, b0.Y+4) {
		t.Error("a motion was consumed by a drag whose node is no longer in the document")
	}
	if release(c, b0.X+4, b0.Y+4) {
		t.Error("a release was consumed by a drag whose node is no longer in the document")
	}
	if ed.drag.active() {
		t.Error("the drag is still holding a node that is no longer in the document")
	}
	// The survivor is the load-bearing assertion: an orphaned commit
	// would have written Canvas.Left onto the deleted node, but a commit
	// that went to the WRONG live node would show up here.
	if got := ed.doc().Kids[0].Attrs["Canvas.Left"]; got != "1" {
		t.Errorf("the surviving element moved to Canvas.Left=%q; the orphaned gesture committed onto it", got)
	}
	if !strings.HasPrefix(ed.status.Get(), "✓") {
		t.Errorf("the document no longer builds: %s", ed.status.Get())
	}
}

// The other half: the guard must not break an ORDINARY drag. A liveness
// check that answered "not live" for everything would pass the test
// above and silently disable dragging.
func TestTheLivenessGuardLeavesAnOrdinaryDragAlone(t *testing.T) {
	ed, c, _ := dragFixture(t)
	a := docKid(ed, 0)
	b0 := a.(interface{ Bounds() gooey.Rect }).Bounds()

	press(c, b0.X, b0.Y)
	if !motion(c, b0.X+3, b0.Y+2) {
		t.Fatal("a motion during a live drag was not consumed")
	}
	if !release(c, b0.X+3, b0.Y+2) {
		t.Fatal("the release of a live drag was not consumed")
	}
	if got := ed.doc().Kids[0].Attrs["Canvas.Left"]; got == "1" {
		t.Errorf("Canvas.Left is still %q; the guard cancelled a legitimate drag", got)
	}
}

// FINDING 1: the body route through the pane's mutation seam.
//
// The body is a FIELD on the node, not a map entry. commitEdit has
// always known that; the cycling editor that preceded the float-over one
// wrote target.Attrs unconditionally. It could not fire only because the
// body row was built without values — an accident of construction that
// nothing declared, one BodySpec value set away from writing a "(text)"
// attribute into the markup that no element declares.
//
// The pane now has ONE seam (valueEditor.Write) rather than two writers,
// so the claim is asserted against it: whatever opens an editor, a row
// carrying the body flag lands on the field.
func TestCyclingTheBodyRowWritesTheBodyNotAnAttribute(t *testing.T) {
	ed, _ := designPage(t)
	// Attrs is populated, not nil, so the guard is what fails the test
	// rather than a nil-map panic: a node the inspector could actually be
	// pointed at always has the map, and a mutant that writes into it
	// must be caught by the assertion, not by a crash that would also
	// "fail" for the wrong reason.
	ed.doc().Kids = []*node{{Elem: "Text", Body: "one", Attrs: map[string]string{"Name": "t"}}}
	ed.rebuild()
	target := ed.doc().Kids[0]
	ed.sel = target

	// The seam pointed at a body row, which is what any of the per-Kind
	// editors does the moment one is opened on the body.
	ed.props.name, ed.props.body = BodyRowName, true
	ed.props.Write("two")

	if got := target.Attrs[BodyRowName]; got != "" {
		t.Errorf("%s was written into Attrs as %q; it is not an attribute and no element declares it", BodyRowName, got)
	}
	if target.Body != "two" {
		t.Errorf("Body = %q, want %q: the cycle did not reach the field it names", target.Body, "two")
	}
}

// FINDING 3: the property that motivated the index→*node redesign, which
// had no direct pin.
//
// Selection used to be an INDEX into a parent's Kids, so deleting an
// earlier sibling shifted every later index and the selection silently
// became a different element. A *node cannot shift. The test deletes a
// node UNRELATED to the selection — a different subtree entirely — which
// is the case an index gets wrong and identity gets right.
func TestDeletingAnUnrelatedSiblingLeavesTheSelectionOnTheSameNode(t *testing.T) {
	ed, _ := designPage(t)
	ed.doc().Kids = []*node{
		{Elem: "Text", Body: "first", Attrs: map[string]string{"Name": "first"}},
		{Elem: "VStack", Attrs: map[string]string{"Name": "box"}, Kids: []*node{
			{Elem: "Text", Body: "nested", Attrs: map[string]string{"Name": "nested"}},
		}},
	}
	ed.rebuild()

	box := ed.doc().Kids[1]
	nested := box.Kids[0]
	ed.sel = nested

	// The path that would have located the selection under the old
	// representation, captured BEFORE the edit: "root's child 1, then its
	// child 0". Recorded rather than asserted, so the contrast below is
	// between two real answers rather than an assumed one.
	oldPath := []int{1, 0}

	// Remove the unrelated first sibling. Written as a tree edit rather
	// than through deleteSelected because deleteSelected removes the
	// SELECTION, and the property under test is what happens to a
	// selection that is not the thing being removed — which is the shape
	// every reorder, reload and remote patch has.
	ed.doc().Kids = ed.doc().Kids[1:]
	ed.rebuild()

	if ed.sel != nested {
		t.Fatalf("selection moved to %s", nodeName(ed.sel))
	}
	if ed.parentOf(nested) == nil {
		t.Fatal("the selected node left the document when an unrelated sibling was removed")
	}
	if got := nested.Attrs["Name"]; got != "nested" {
		t.Errorf("the selection points at %q, want %q", got, "nested")
	}

	// THE DISCRIMINATION: the same path now resolves somewhere else, so
	// the test above is pinning identity rather than restating an index
	// that happened not to move.
	if len(ed.doc().Kids) > oldPath[0] {
		if stale := ed.doc().Kids[oldPath[0]]; stale == box {
			t.Error("the old index path still resolves to the same parent; " +
				"this document does not discriminate index from identity")
		}
	}
}
