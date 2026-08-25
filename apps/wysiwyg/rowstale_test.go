package main

import (
	"testing"

	"github.com/WonderForgeLabs/gooey/input"
)

// The staleness guard one level down: the ROW can move out from under an
// open editor without the ELEMENT changing, and the element-identity
// check cannot see it.
//
// Why this is reachable with the mouse and not only in principle: the
// caret modes return from Open before pop.Open(nil), so unlike every
// floated mode they never capture the pointer. A press on another
// attribute row is therefore consumed by the ItemsView, which moves
// attrSel — and the outside-press dismissal that closes a dropdown never
// runs. Arrange then reads attrSel and relocates the floating TextBox
// onto the newly selected row, while Write still targets the name
// captured at open. The editor shows one row and writes another.
func TestPressingAnotherRowRetiresACaretEditorInsteadOfWritingTheOldOne(t *testing.T) {
	ed, c, p := propsPane(t)
	ed.sel = ed.doc().Kids[1] // the Button — several editable rows
	ed.rebuild()
	c.Frame()

	from := rowAt(t, ed, c, "Content")
	p.OpenAsText()
	c.Frame()
	if p.Mode() != editCaret {
		t.Fatalf("the caret editor did not open on Content (mode %v); this test cannot "+
			"exercise the guard it is named for", p.Mode())
	}
	before := ed.doc().Kids[1].Attrs["Content"]

	// Press on a DIFFERENT row of the same element. Not a helper that
	// sets attrSel: the bug is that the ItemsView consumes this press and
	// moves the selection itself, so driving the press is the whole
	// point — assigning attrSel would test the guard against a state the
	// mouse is not what produced.
	to := rowIndexOf(t, ed, "Chrome")
	if to == from {
		t.Fatal("Content and Chrome resolved to the same row")
	}
	r, ok := p.list.RowBounds(to)
	if !ok {
		t.Fatalf("row %d (Chrome) has no bounds; the pane is not laid out", to)
	}
	c.HandleMouse(input.MouseEvent{
		Kind: input.MousePress, Button: input.ButtonLeft, X: r.X + 1, Y: r.Y,
	})
	c.Frame()

	if ed.attrSel.Get() == from {
		t.Fatalf("the press did not move the selection off row %d, so the divergence "+
			"this test exists to catch never happened", from)
	}
	// THE ASSERTION, and it is about the WRITE rather than about the mode.
	//
	// Retiring the moment the row moves would mean Setting properties
	// from Arrange, i.e. mutating the graph during layout — a worse
	// invariant to break than the one being fixed. So the guard lives at
	// the write seam, exactly where the element-level guard already did,
	// and the editor stays open-but-doomed until something tries to use
	// it. What must never happen is the write landing.
	//
	// This is the reported gesture finished: open on Content, click
	// Chrome, type.
	p.Write("TYPED")

	if got := ed.doc().Kids[1].Attrs["Content"]; got != before {
		t.Errorf("Content = %q, want %q — the editor floated over Chrome and wrote to "+
			"Content. That is the divergence the row guard exists to stop.", got, before)
	}
	if got := ed.doc().Kids[1].Attrs["Chrome"]; got == "TYPED" {
		t.Error("the write landed on Chrome instead: a stale editor must commit to " +
			"NEITHER row, because the value it holds was typed for the old one")
	}
	// And it is retired on the way out, so the next keystroke has no
	// editor to write through at all.
	if p.Mode() != editNone {
		t.Errorf("the caret editor is still open (mode %v) after a refused write; a "+
			"stale editor must retire rather than stay armed", p.Mode())
	}
}

// rowIndexOf is rowAt without the selection side effect — this test needs
// to know where a row IS before pressing on it, and selecting it first
// would move attrSel by hand and hide the behaviour under test.
func rowIndexOf(t *testing.T, ed *editor, name string) int {
	t.Helper()
	for i, r := range ed.attrRows() {
		if r.name == name {
			return i
		}
	}
	t.Fatalf("no row named %q", name)
	return 0
}
