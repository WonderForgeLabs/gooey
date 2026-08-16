package main

import (
	"os"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/control"
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
// button down in the status bar. <Modal> freezes them, and Store.Blocked
// reads s.pane — which is what makes the freeze OBSERVED rather than
// sampled, so it lands in the frame the pane changes.
//
// The pin is the tab walk, not the mechanism: it fails identically if
// the freeze never arrives, if it arrives a frame late, or if a focus
// stop is added outside both Modal wrappers.
func TestFocusIsTrappedInsideTheDialog(t *testing.T) {
	s := NewStore(os.DirFS("."))
	ctx := s.Context(s.logo)
	RegisterModal(ctx, s.Blocked)
	root, err := markup.Load(os.DirFS("."), "store.gooey", ctx)
	if err != nil {
		t.Fatal(err)
	}

	// The Composer owns the FocusManager and re-syncs it itself when a
	// Frozen answer flips, so this is the real path rather than a
	// stand-in wired up by the test.
	c := gooey.NewComposer(root, 120, 40)
	fm := c.Focus()
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

// The vendor port's grant is the demo's central claim, so it gets a
// test rather than a comment — which is exactly the change #250 made to
// the framework, applied to the app that was making the claim.
func TestTheVendorIslandIsNarrowerThanTheApp(t *testing.T) {
	s := NewStore(os.DirFS("."))
	ctx := s.Context(s.logo)
	RegisterModal(ctx, s.Blocked)
	root, err := markup.Load(os.DirFS("."), "store.gooey", ctx)
	if err != nil {
		t.Fatal(err)
	}
	c := gooey.NewComposer(root, 120, 40)
	c.Frame()

	vendor := control.NewScopedService(composerHost{c}, ctx, control.Island("Toolbar", "Tint", "Wallet", "OpenStore"))

	// NARROWED, not refused: a listing shows the granted world, so a
	// guest cannot refuse-probe its way to a map of what it cannot
	// touch.
	vals, _, err := vendor.Values()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, v := range vals {
		got[v.Name] = true
	}
	for _, want := range []string{"Tint", "Wallet", "OpenStore"} {
		if !got[want] {
			t.Errorf("the vendor island cannot see %q, which its toolbar markup binds", want)
		}
	}
	for _, forbidden := range []string{"Subscribe", "Quit", "Items", "Services", "Receipt"} {
		if got[forbidden] {
			t.Errorf("the vendor island can see %q; the grant is wider than the product", forbidden)
		}
	}

	// REFUSED: an element outside the island.
	if _, err := vendor.PatchMarkup("Shell", `<Gooey xmlns="wonderforge.io/gooey/2026"><Border Name="Shell"><Text>owned</Text></Border></Gooey>`); err == nil {
		t.Error("the vendor patched Shell; the island is not enforced")
	}
}

// composerHost is a control.Host backed by a real Composer, which the
// grant needs: an island is resolved against the LIVE tree on every
// call, so a host that returns no composer cannot resolve one at all.
type composerHost struct{ c *gooey.Composer }

func (h composerHost) Post(fn func())            { fn() }
func (h composerHost) Composer() *gooey.Composer { return h.c }
func (h composerHost) Swap(gooey.Component)      {}
