package main

import (
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey/markup"
)

// The WRAPPING MODEL: ed.root is the design SURFACE and is the editor's
// workspace; ed.doc() is the user's own root and is the whole of what a
// save writes.
//
// The surface exists so that everything dropped on it has free geometry to
// be dragged around in. It is never serialized, which is what makes "where
// do the positions live" a real question — under a save that writes doc()
// and nothing above it, a position ON the surface has no home in the file.
// Nothing here invents one.

// TestTheSurfaceNeverReachesTheSavedDocument is the load-bearing claim of
// the whole model, asserted on the string three different consumers read.
//
// source feeds the CODE tab, the OUTPUT tab and pushRemote. A remote app
// rendering the designer's own scaffolding would be showing the user
// something they cannot save, cannot edit and did not write — and it would
// make the wire contract depend on an editor implementation detail, so
// changing the chrome would change what every attached client sees.
func TestTheSurfaceNeverReachesTheSavedDocument(t *testing.T) {
	ed, _ := designerPage(t)
	src := ed.source.Get()

	if strings.Contains(src, `Name="Surface"`) {
		t.Errorf("the design surface was serialized into the saved document:\n%s", src)
	}
	// One <Canvas> in the emitted markup, not two. The surface and the
	// user's root are both Canvases by default, so counting is what
	// distinguishes "the surface is gone" from "they happen to look alike".
	if n := strings.Count(src, "<Canvas"); n != 1 {
		t.Errorf("the saved document contains %d <Canvas> elements, want 1 (the user's root "+
			"only):\n%s", n, src)
	}
	// The document must still be the user's ROOT at the top, not a
	// fragment: what a save writes has to load on its own.
	if _, err := markup.Build([]byte(src), ed.docCtx); err != nil {
		t.Errorf("the saved document does not build on its own: %v\n%s", err, src)
	}

	// And the PREVIEW is the other way round: it must contain the surface,
	// because the surface is what gives everything on it free geometry.
	if ed.docRoot == nil {
		t.Fatal("nothing was previewed")
	}
	if ed.nodeOf[ed.docRoot] != ed.root {
		t.Error("the previewed tree is not rooted at the surface: the document would be laid " +
			"out without the workspace it is positioned on")
	}
}

// TestRetypeChangesTheUsersRootAndNotTheSurface.
//
// Both are <Canvas> by default, so retyping the wrong one is invisible
// until you look at the saved file: it would alter how the workspace lays
// out and leave the document untouched, which is the exact opposite of
// what c and v are for.
func TestRetypeChangesTheUsersRootAndNotTheSurface(t *testing.T) {
	ed, _ := designerPage(t)
	if ed.root.Elem != "Canvas" || ed.doc().Elem != "Canvas" {
		t.Fatalf("fixture: surface <%s>, root <%s>; both should start as Canvas",
			ed.root.Elem, ed.doc().Elem)
	}

	ed.retype("VStack")
	if ed.doc().Elem != "VStack" {
		t.Errorf("retype left the user's root as <%s>, want <VStack>", ed.doc().Elem)
	}
	if ed.root.Elem != "Canvas" {
		t.Errorf("retype changed the SURFACE to <%s>: the workspace is not the user's to "+
			"retype, and nothing about the saved document would have changed", ed.root.Elem)
	}
	if !strings.Contains(ed.source.Get(), "<VStack") {
		t.Errorf("the saved document did not follow the retype:\n%s", ed.source.Get())
	}
	// The surface stays a Canvas, so the user's root still has free
	// geometry to be positioned in even when the root itself is a VStack.
	if strings.Count(ed.source.Get(), "<Canvas") != 0 {
		t.Errorf("a <Canvas> survived in the saved document after retyping to VStack:\n%s",
			ed.source.Get())
	}
}

// TestTheUsersRootCannotBeDeleted.
//
// A document has to have a root. Deleting it would leave the surface
// holding nothing while doc() still expected a child — a panic one
// keystroke later, from a gesture the user thinks is ordinary.
func TestTheUsersRootCannotBeDeleted(t *testing.T) {
	ed, _ := designerPage(t)
	ed.setSelection(ed.doc())
	ed.deleteSelected()
	if len(ed.root.Kids) == 0 {
		t.Fatal("deleting the selected root emptied the surface: doc() would panic on the " +
			"next edit")
	}
	if ed.doc().Elem == "" {
		t.Fatal("the user's root was replaced by something empty")
	}
	// Its children are still deletable, which is what makes the guard
	// specific rather than a blanket refusal.
	n := len(ed.doc().Kids)
	ed.setSelection(ed.doc().Kids[0])
	ed.deleteSelected()
	if len(ed.doc().Kids) != n-1 {
		t.Errorf("a child of the root was not deleted: %d kids, want %d", len(ed.doc().Kids), n-1)
	}
}

// TestAddingIntoALeafGoesToItsParentInstead is the guard on the silent
// drop, and it is the one that would ship green.
//
// A leaf element DISCARDS children — no error at load, nothing at runtime.
// So a Button added while a <Text> is selected would simply not exist, and
// the user's reading of that is "I clicked, nothing happened, the tool is
// broken". There is no diagnostic anywhere to contradict them.
func TestAddingIntoALeafGoesToItsParentInstead(t *testing.T) {
	ed, _ := designerPage(t)

	// <Text> is a leaf. Select it, then add.
	leaf := ed.doc().Kids[0]
	if ed.holdsChildren(leaf.Elem) {
		t.Fatalf("fixture: <%s> is not a leaf, so this test asserts nothing", leaf.Elem)
	}
	ed.setSelection(leaf)
	before := len(ed.doc().Kids)
	if got := ed.addTarget(); got != ed.doc() {
		t.Fatalf("with a leaf selected the add target is <%s>, want the user's root: a child "+
			"appended into a leaf is discarded with no error anywhere", got.Elem)
	}

	n, _ := addFromPalette(t, ed, "Button")
	if len(leaf.Kids) != 0 {
		t.Errorf("a <Button> was appended INTO the leaf <%s>: it will be discarded silently",
			leaf.Elem)
	}
	if len(ed.doc().Kids) != before+1 {
		t.Errorf("the user's root has %d kids, want %d: the add went somewhere unexpected",
			len(ed.doc().Kids), before+1)
	}
	if ed.parentOf(n) != ed.doc() {
		t.Errorf("the new node's parent is %s, want the user's root", nodeName(ed.parentOf(n)))
	}
	// It has to actually BUILD, which is the end of the silent-drop chain.
	if !strings.HasPrefix(ed.status.Get(), "✓") {
		t.Errorf("adding beside a leaf produced markup that does not build: %s", ed.status.Get())
	}
}

// TestAddingIntoAContainerGoesInside is the positive half — without it the
// leaf test above passes for an editor that always appends at the root.
func TestAddingIntoAContainerGoesInside(t *testing.T) {
	ed, _ := designerPage(t)

	ed.doc().Kids = append(ed.doc().Kids, &node{
		Elem:  "VStack",
		Attrs: map[string]string{"Name": "Box", "Canvas.Left": "0", "Canvas.Top": "8"},
	})
	box := ed.doc().Kids[len(ed.doc().Kids)-1]
	ed.rebuild()
	if !ed.holdsChildren(box.Elem) {
		t.Fatalf("fixture: <%s> does not hold children", box.Elem)
	}

	ed.setSelection(box)
	if got := ed.addTarget(); got != box {
		t.Fatalf("with a container selected the add target is <%s>, want the container", got.Elem)
	}
	n, _ := addFromPalette(t, ed, "Text")
	if ed.parentOf(n) != box {
		t.Errorf("the new node landed in %s, want inside the selected <VStack>",
			nodeName(ed.parentOf(n)))
	}
	if !strings.HasPrefix(ed.status.Get(), "✓") {
		t.Errorf("adding inside a container produced markup that does not build: %s", ed.status.Get())
	}
	// A child of a VStack has no free geometry, so it must not be seeded
	// with Canvas.Left — the parent decides, not the surface.
	if _, ok := n.Attrs["Canvas.Left"]; ok {
		t.Error("a child of a <VStack> was seeded with Canvas.Left, which applyLayout discards")
	}
}

// TestEveryPaletteElementIsClassifiedForContainment.
//
// holdsChildren treats an element the palette cannot describe as a LEAF,
// because the cost of being wrong that way is a rejected add and the cost
// of being wrong the other way is a silent drop. This asserts the
// classification is actually derived from the catalog rather than guessed,
// across every element the toolbox offers.
func TestEveryPaletteElementIsClassifiedForContainment(t *testing.T) {
	ed := newEditor(editorFS())
	checked := 0
	for _, e := range ed.palette {
		want := true
		switch e.Children.Mode {
		case markup.ModeLeaf, markup.ModeNone, markup.ModeAttachments:
			want = false
		}
		if got := ed.holdsChildren(e.Name); got != want {
			t.Errorf("<%s> has Children.Mode %q; holdsChildren says %v, want %v",
				e.Name, e.Children.Mode, got, want)
		}
		checked++
	}
	if checked == 0 {
		t.Fatal("no palette elements examined; this test asserts nothing")
	}
	// An element that is not in the palette at all is a leaf, which is the
	// safe answer rather than an optimistic one.
	if ed.holdsChildren("NoSuchElement") {
		t.Error("an unknown element is treated as a container: an add into it would be dropped")
	}
}
