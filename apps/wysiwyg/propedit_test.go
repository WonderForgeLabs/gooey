package main

import (
	"testing"
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
)

// Double-clicking a property row is the gesture that hung a live editor:
// the app kept painting and answering its control plane, and never took
// another keystroke from the terminal again.
//
// Driving it here rather than through a pty is deliberate. The composer's
// HandleMouse runs on the test goroutine, which is the UI goroutine, so a
// lock taken twice or a wait on work only this goroutine can do stops the
// TEST — and `go test` prints every goroutine's stack on its timeout,
// which is the one instrument that shows a deadlock rather than implying
// one. A pty harness would reproduce the symptom and tell you nothing
// about the cause.

// attrRows finds the ItemsView the properties pane is built from, by the
// property it is bound to rather than by name or position: the pane is
// one of several ItemsViews on the page, and the binding is what makes
// this one the properties list.
func attrRows(t *testing.T, ed *editor, root gooey.Component) *components.ItemsView {
	t.Helper()
	var found *components.ItemsView
	var walk func(gooey.Component)
	walk = func(w gooey.Component) {
		if v, ok := w.(*components.ItemsView); ok && v.Items == ed.attrItems {
			found = v
		}
		for _, k := range children(w) {
			walk(k)
		}
	}
	walk(root)
	if found == nil {
		t.Fatal("the page does not mount an ItemsView bound to AttrItems")
	}
	return found
}

// buttonSelected is the state the report came from: a Button selected in
// the designer, so the properties pane lists its layout attributes and
// Width is one of them.
func buttonSelected(t *testing.T) (*editor, *gooey.Composer) {
	t.Helper()
	ed, c := designPage(t)
	ed.doc().Kids = []*node{{
		Elem:  "Button",
		Attrs: map[string]string{"Name": "B1", "Content": "click", "Canvas.Left": "1", "Canvas.Top": "1"},
	}}
	ed.rebuild()
	ed.sel = ed.doc().Kids[0]
	c.Frame()
	return ed, c
}

// rowY scans the pane for the row a given attribute is on. The row index
// is not written down anywhere — it comes from the catalog for whatever
// element is selected — so finding it by clicking and asking which row
// got selected is the only way that does not hard-code a number that the
// next catalog change invalidates.
func rowY(t *testing.T, ed *editor, c *gooey.Composer, v *components.ItemsView, name string) int {
	t.Helper()
	b := v.Bounds()
	for y := b.Y; y < b.Y+b.H; y++ {
		press(c, b.X+1, y)
		release(c, b.X+1, y)
		c.Frame()
		if r, ok := ed.selectedRow(); ok && r.name == name {
			return y
		}
	}
	t.Fatalf("no row named %q in the properties pane (bounds %v)", name, b)
	return 0
}

// TestDoubleClickingAPropertyRowReturns is the whole bug, stated as the
// weakest thing that has to be true: the gesture COMPLETES.
//
// It deliberately asserts nothing about the edit that follows. An
// assertion about editName would pass or fail on the same run, but it
// would describe the feature; what was actually broken is that control
// never came back, and a test that hangs is how that gets reported.
func TestDoubleClickingAPropertyRowReturns(t *testing.T) {
	ed, c := buttonSelected(t)
	v := attrRows(t, ed, c.Root())
	y := rowY(t, ed, c, v, "Width")
	x := v.Bounds().X + 1

	// The second click has to land inside DoubleClickInterval to arrive
	// as Count 2 — that is the gesture under test, and a slow first click
	// would silently downgrade it to two ordinary clicks that prove
	// nothing.
	done := make(chan struct{})
	go func() {
		defer close(done)
		press(c, x, y)
		release(c, x, y)
		press(c, x, y)
		release(c, x, y)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("double-clicking the Width row never returned: the gesture deadlocked, " +
			"which in the real app leaves it painting and permanently deaf")
	}
	c.Frame()

	// Not a feature assertion — a check that the test can fail at all.
	// "It returned" is vacuous if the double click never reached the
	// activation, and every way that could happen (the row scanned to the
	// wrong pane, Count never reaching 2, Activate unbound in this
	// fixture) leaves editName untouched and the test passing.
	if got := ed.editName.Get(); got != "Width" {
		t.Fatalf("the double click did not begin an edit (editName=%q): this test "+
			"never exercised the path it claims to cover", got)
	}
}
