// store: three parties, one screen.
//
//	cd examples/store && go run .
//
// Northwind Ops is an app somebody shipped. `elan` is the person using
// it. Chromatica, Vestibule and Ledgerline are companies being paid to
// change it. None of those three is the same entity, and no existing
// model for extending an app keeps them apart — a browser extension is
// authorised by the user over the app owner's head, an in-app purchase
// has no third party in it at all, an enterprise plugin is authorised by
// the app owner over the user's head.
//
// Here they are separate, and the seams say so. The app owner decides
// whether mcp.Serve is called and what goes in the binding context. The
// vendor gets thirteen operations and no way past them. The user gets —
// and this is the demo — no seam at all.
//
// The billing is mocked. Every gooey mechanism is real and local: the
// injection, the property registration, the markup patching, the control
// plane the vendors arrive through.
package main

import (
	"context"
	"flag"
	"os"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/control"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/mcp"

	// Registers the SVG decoder with imaging. The vendor logos are
	// vectors so they rasterize cleanly at whatever cell size the
	// terminal reports.
	_ "github.com/WonderForgeLabs/gooey/imagefmt/svg"
)

func main() {
	addr := flag.String("mcp", "127.0.0.1:7788", "loopback address for the OWNER's control plane; empty disables it")
	vendor := flag.String("vendor", "127.0.0.1:7789", "loopback address vendors are given; empty disables it")
	flag.Parse()

	dir := os.DirFS(".")
	store := NewStore(dir)

	// What Subscribe launches, and what teardown kills. It is handed the
	// VENDOR address and never the owner's — the address is the
	// capability, so passing the wrong one here would silently hand a
	// third party the unscoped port.
	store.vendors = newVendors(*vendor)
	defer store.vendors.stopAll()

	ctx := store.Context(store.logo)

	// <Modal> is registered from here rather than from Context because
	// its predicate is the app's, and a vendor must not be able to write
	// one. See modal.go: freezing is a Go interface on purpose.
	RegisterModal(ctx, store.Blocked)
	store.app = gooey.NewApp(markup.Page(dir, "store.gooey", ctx))
	store.svc = control.NewService(store.app, ctx)

	// TWO endpoints, and the difference between them is the demo.
	//
	// The owner's port is unscoped: Northwind can reach its own app,
	// which is what an app owner has always been able to do.
	if *addr != "" {
		srv, err := mcp.Serve(store.app, mcp.Options{
			Addr:    *addr,
			Context: ctx,
			Name:    "northwind-ops",
		})
		if err != nil {
			gooey.Exit(err)
		}
		defer srv.Close()
	}

	// The vendor's port carries a GRANT, and the grant is the whole of
	// what a vendor can do: the subtree under Name="Toolbar", and three
	// values. Not the services list, not the item list, not Subscribe,
	// not Quit, not the page.
	//
	// Three values, and the third one is the interesting one. Tint is
	// what Chromatica was actually sold — the handle its picker moves.
	// Wallet and OpenStore are Northwind's OWN toolbar content, and the
	// vendor needs them because patch_markup REPLACES a named element
	// rather than appending to it: to add one control beside Northwind's
	// button, Chromatica has to redraw Northwind's button, which means
	// binding Northwind's values.
	//
	// So the narrowest grant that lets this product work is strictly
	// wider than the product's own purpose, and that is a real property
	// of subtree replacement rather than an oversight here. Granting
	// only Tint is not a hypothetical: it was tried, and the host
	// refused the patch with "the markup binds one or more names outside
	// this session's granted values".
	//
	// This used to be a comment in store.gooey claiming those names were
	// "the app owner's whole grant". It was not: control.Service resolved
	// every name against the entire binding context, so the vendor could
	// patch any element, write any property, invoke any command and swap
	// the whole page. Client politeness is not a boundary. Now the grant
	// is a struct field on the server the host started, and there is no
	// request field to widen and no token to forge — a guest cannot name
	// a capability it was not handed.
	//
	// The address IS the capability, which is why this is a second Serve
	// on a second port rather than a flag on the first. A second vendor
	// with a disjoint island is a third one.
	if *vendor != "" {
		srv, err := mcp.Serve(store.app, mcp.Options{
			Addr:    *vendor,
			Context: ctx,
			Name:    "northwind-ops (vendor island)",
			Grant:   control.Island("Toolbar", "Tint", "Wallet", "OpenStore"),
		})
		if err != nil {
			gooey.Exit(err)
		}
		defer srv.Close()
	}

	if err := store.app.Run(context.Background()); err != nil {
		gooey.Exit(err)
	}
}
