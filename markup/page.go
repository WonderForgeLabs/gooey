package markup

import (
	"io/fs"

	"github.com/WonderForgeLabs/gooey"
)

// Page is a markup file as an app's content: gooey.App builds its tree
// from it at startup and rebuilds it whenever the file changes.
//
// The extra names are the other files a rebuild depends on — the
// UserControls and markup-only controls the page instantiates. Loading
// resolves them, but watching cannot infer them (an <Include> is
// resolved during a build, and the build we are watching for has not
// happened yet), so they are named:
//
//	markup.Page(fsys, "dashboard.gooey", ctx, "card.gooey", "badge.gooey")
//
// Editing card.gooey then restyles every card on screen at once.
//
// fs.FS is the seam it is everywhere else: os.DirFS in dev gives hot
// reload, embed.FS in release reports constant ModTimes so watching is a
// natural no-op, and the same call compiles for both.
func Page(fsys fs.FS, name string, ctx *Context, also ...string) gooey.Content {
	return &page{fsys: fsys, name: name, ctx: ctx, also: also}
}

type page struct {
	fsys fs.FS
	name string
	ctx  *Context
	also []string
}

// Build loads the page. Named elements are cleared first: a rebuild
// produces new components, and leaving the old ones in the map would let
// markup.Find hand out a component belonging to a composition that is no
// longer on screen.
func (p *page) Build() (gooey.Component, error) {
	p.ctx.Named = map[string]gooey.Component{}
	p.ctx.Declared = nil
	return Load(p.fsys, p.name, p.ctx)
}

// Watch reports changes only — the App rebuilds on the UI goroutine.
// That is the difference between this and the older Watch/WatchAll used
// directly: those hand back a tree built on the watcher's goroutine,
// which means binding resolution touched the property graph from a
// goroutine that was never allowed near it.
func (p *page) Watch(changed func()) func() {
	names := append([]string{p.name}, p.also...)
	return WatchAll(p.fsys, names, changed)
}
