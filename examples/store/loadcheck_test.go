package main

import (
	"os"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/markup"
)

// store.gooey must LOAD. Everything resolvable — an unknown element, an
// unknown attribute, an undeclared binding, a bad enum — is a load-time
// error in this framework, and a deck or a demo that fails at load fails
// with a black screen and a line on stderr nobody is watching.
func TestMarkupLoads(t *testing.T) {
	s := NewStore(os.DirFS("."))
	ctx := s.Context(s.logo)
	RegisterModal(ctx, s.Blocked)
	if _, err := markup.Load(os.DirFS("."), "store.gooey", ctx); err != nil {
		t.Fatalf("store.gooey does not load: %v", err)
	}
}

// A modal that you can tab out of is not a modal.
//
// The sheet floats over the store list now instead of collapsing it,
// which means every focus stop behind it is still in the tree: the
// ItemsView, the store pane's three buttons, and the integrations
// button down in the status bar. <Modal> freezes them — but Frozen is
// SAMPLED, not observed, so the freeze only takes effect at a
// structural re-sync. Store.setPane forces one. Without that call this
// test walks straight out of the dialog and into the app behind it.
func TestFocusIsTrappedInsideTheDialog(t *testing.T) {
	s := NewStore(os.DirFS("."))
	ctx := s.Context(s.logo)
	RegisterModal(ctx, s.Blocked)
	root, err := markup.Load(os.DirFS("."), "store.gooey", ctx)
	if err != nil {
		t.Fatal(err)
	}

	c := gooey.NewComposer(root, 120, 40)
	fm := gooey.NewFocusManager(root)
	s.resync = func() {
		c.InvalidateStructure()
		fm.Resync()
	}
	c.Frame()

	s.itemSel.Set(0)
	s.Buy()
	c.Frame()

	if !s.Blocked() {
		t.Fatal("Buy did not put the app in the blocked state")
	}

	sheet, err := markup.Find[gooey.Component](ctx, "Sheet")
	if err != nil {
		t.Fatal(err)
	}
	// More tabs than there are focus stops on the whole page, so a wrap
	// that escaped the dialog cannot be missed.
	for i := 0; i < 12; i++ {
		fm.FocusNext()
		c.Frame()
		if !within(sheet, fm.Focused()) {
			t.Fatalf("tab %d left the dialog; the backdrop is still reachable", i+1)
		}
	}
}

// within reports whether c is at or below w.
func within(w, c gooey.Component) bool {
	if w == nil || c == nil {
		return false
	}
	if w == c {
		return true
	}
	cont, ok := w.(gooey.Container)
	if !ok {
		return false
	}
	for _, k := range cont.ChildComponents() {
		if k != nil && within(k, c) {
			return true
		}
	}
	return false
}
