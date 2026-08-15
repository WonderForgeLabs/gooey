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
	addr := flag.String("mcp", "127.0.0.1:7788", "loopback address for the control plane; empty disables it")
	flag.Parse()

	dir := os.DirFS(".")
	store := NewStore(dir)

	ctx := store.Context(store.logo)

	// <Modal> is registered from here rather than from Context because
	// its predicate is the app's, and a vendor must not be able to write
	// one. See modal.go: freezing is a Go interface on purpose.
	RegisterModal(ctx, store.Blocked)
	store.app = gooey.NewApp(markup.Page(dir, "store.gooey", ctx))
	store.svc = control.NewService(store.app, ctx)

	// Frozen is sampled at a structural re-sync (component.go), so the
	// modal's backdrop only actually goes inert when one is forced. This
	// is that force, and store.go routes every pane change through it.
	store.resync = func() {
		if c := store.app.Composer(); c != nil {
			c.InvalidateStructure()
		}
	}

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

	if err := store.app.Run(context.Background()); err != nil {
		gooey.Exit(err)
	}
}
