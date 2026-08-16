package main

// The two security claims this demo makes on stage and did not check.
//
// "Cannot invent a value" already had a test (loadcheck_test.go). These
// are the other two — a vendor cannot put a picture on your screen, and a
// vendor cannot freeze you — and both were established by reading
// framework source rather than by exercising anything. A claim made from
// a stage should be one a test can fail on.

import (
	"os"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/control"
	"github.com/WonderForgeLabs/gooey/markup"
)

// grantedValues is the value half of the vendor island, in one place so
// the tests below and main.go cannot drift apart silently.
var grantedValues = []string{"Tint", "Wallet", "OpenStore"}

// island builds the app for real and returns the vendor's scoped service —
// the same grant main.go hands the second port.
func island(t *testing.T) (*Store, *control.Service, *markup.Context) {
	t.Helper()
	s := NewStore(os.DirFS("."))
	ctx := s.Context(s.logo)
	RegisterModal(ctx, s.Blocked)
	root, err := markup.Load(os.DirFS("."), "store.gooey", ctx)
	if err != nil {
		t.Fatal(err)
	}
	c := gooey.NewComposer(root, 120, 40)
	c.Frame()
	return s, control.NewScopedService(composerHost{c}, ctx,
		control.Island("Toolbar", "Tint", "Wallet", "OpenStore")), ctx
}

const gooeyNS = `xmlns="wonderforge.io/gooey/2026"`

// A vendor cannot put a picture on your screen — and the reason is NOT
// that the framework forbids it.
//
// control.KindImage exists precisely so a client CAN: its own doc says
// "without it no client can put a picture on a page", because markup
// swapped over the wire has no filesystem for <Image Src="logo.png"> to
// resolve against. A guest that can register a KindImage property can
// send encoded bytes and bind them.
//
// What stops this vendor is the SHAPE of its grant. Registration is
// allowed to grow the namespace a guest was granted, and all three
// granted values are leaf properties — Tint is a Color, Wallet and
// OpenStore are strings — so there is no scope to grow into and nowhere
// to hang a new name. Grant a NAMESPACE instead of three leaves and the
// claim silently becomes false.
//
// So the test asserts that shape, not just today's refusals. This is the
// one that will fail on somebody widening the grant, which is the only
// way this claim can be lost.
//
// The message it prints is a MEASURED fact, not a worry. Granting a
// `Vendor` namespace and asking the scoped service to register
// `Vendor.Art` as KindImage succeeds — checked, then reverted. The four
// refusals above are refusals because Tint, Wallet and OpenStore are
// leaves and Toolbar is an ELEMENT grant, absent from the granted values
// entirely; none of them is a refusal on the grounds of being an image.
func TestAVendorCannotShipAnImage(t *testing.T) {
	_, vendor, ctx := island(t)

	// It cannot bind the host's handle: Logo is not in the grant.
	src := `<Gooey ` + gooeyNS + `><Border Name="Toolbar"><Image Src="{{.Logo}}" Cols="4" Rows="2"/></Border></Gooey>`
	if _, err := vendor.PatchMarkup("Toolbar", src); err == nil {
		t.Error("a vendor bound the host's image handle; it can put its own artwork on screen")
	}

	// It cannot register one to bind instead — not at the top level, and
	// not under any granted name.
	for _, name := range []string{"VendorArt", "Toolbar.Art", "Tint.Art", "Wallet.Art"} {
		if err := vendor.Register([]control.Registration{
			{Name: name, Kind: control.KindImage},
		}); err == nil {
			t.Errorf("a vendor registered %q as an image; it can now ship its own artwork", name)
		}
	}

	// THE LOAD-BEARING ASSERTION. Every granted value must be a leaf.
	// A map here is a namespace the vendor may grow, and a KindImage
	// registered inside it is a picture on the app owner's screen.
	for _, name := range grantedValues {
		v, ok := ctx.Values[name]
		if !ok {
			t.Errorf("the grant names %q but the context has no such value — "+
				"main.go and this test have drifted", name)
			continue
		}
		if _, isScope := v.(map[string]any); isScope {
			t.Errorf("granted value %q is a SCOPE, not a leaf property. A guest may "+
				"register inside a granted scope, so it can now add a KindImage "+
				"and put its own picture on screen. The demo's \"a vendor cannot "+
				"ship an image\" claim does not survive this grant.", name)
		}
	}
}

// A vendor cannot freeze you.
//
// <Modal> is registered from main.go rather than from Context precisely
// so its predicate is the app owner's Go func. A vendor can EMIT the
// element — that is not the boundary — but there is no attribute, no
// binding and no granted name that supplies the condition. Whatever it
// builds answers to Store.Blocked.
//
// So this does not check that the patch is refused. It checks that a
// Modal built from VENDOR markup, carrying attributes that look for all
// the world like a predicate, still tracks the host's state.
func TestAVendorCannotFreezeTheApp(t *testing.T) {
	s, _, ctx := island(t)

	build, ok := ctx.Components["Modal"]
	if !ok {
		t.Fatal("Modal is not registered — this test is checking nothing")
	}

	// The most a vendor could write.
	m, err := build(markup.Element{
		Name:  "Modal",
		Attrs: map[string]string{"Frozen": "true", "Blocked": "true"},
	}, ctx)
	if err != nil {
		t.Fatalf("building a vendor Modal: %v", err)
	}
	mod, isModal := m.(*Modal)
	if !isModal {
		t.Fatalf("Modal built a %T", m)
	}

	// The host is not blocked, so neither is this, whatever it asked for.
	s.pane.Set(paneStore)
	if mod.Frozen() {
		t.Error("a vendor's Modal froze the app while the host was not blocked")
	}
	// And it follows the HOST. Without this half the assertion above
	// passes for a Modal wired to nothing at all.
	s.pane.Set(panePurchase)
	if !mod.Frozen() {
		t.Error("the Modal ignored the host's own predicate — it is not wired to " +
			"Store.Blocked, so the check above passed for the wrong reason")
	}
}
