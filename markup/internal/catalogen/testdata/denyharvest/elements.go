// Package denyharvest is a FIXTURE, not a build. catalogen reads .go files with
// go/ast and never type-checks them, so this file only has to parse and
// to have the shapes the scanner looks for: ElementDef literals with
// Attrs and Build, and a host builder that reads attributes off both its
// own element and its children.
//
// It exists because catalogen had no tests at all, which is how two
// holes in it reached review — one where a pseudo-element could declare
// an attribute only the HOST reads off itself, and one where two walks
// with different guards were subtracted from each other. Neither is
// reachable from the real vocabulary today, so neither could be pinned
// against it.
package denyharvest

// defPhantom is the deny-list hole in SCAN, the ordinary-element walk —
// the sibling of what src's defHost/checkAttrs pins for the child walk.
//
// Its Build hands a capitalised literal to checkProps, which is on the
// deny-list because it is builder machinery rather than an attribute
// reader. "Ghost" is therefore NOT a read. It is declared and nothing
// reads it, so Check must report it over-declared.
//
// The DIRECTION is what earns a fixture of its own. If the harvest runs
// before the deny-list, the literal is filed as a read of Ghost, the
// declaration looks served, and the finding DISAPPEARS — an
// over-declared attribute stays settable in markup and silently
// ignored, which this package's doc comment says nothing else catches.
// The noisy failure (an attribute nobody declared) announces itself;
// this one is a check that quietly stops checking.
//
// It lives HERE rather than in src because src's contract is that it
// produces NO findings — TestAHostsOwnReadIsNotAChildsAttribute asserts
// that as its baseline — and this fixture exists to produce one.
// Raised in review of #454.
var defPhantom = &ElementDef{
	Name:  "Phantom",
	Known: true,
	Attrs: []AttrSpec{
		{Name: "Ghost"},
	},
	Build: func(e Element, ctx *Context) (gooey.Component, error) {
		return nil, checkProps(e, ctx, "Ghost")
	},
}

// checkProps stands in for the general builder machinery that takes the
// element but reads no attribute off it by that name. It is on the
// deny-list for the same reason checkAttrs is.
func checkProps(e Element, ctx *Context, kind string) error { return nil }

// defDeepPhantom is the same hole ONE LEVEL DOWN, and it is a separate
// element because a separate FUNCTION runs there.
//
// scan handles a Build's own body; the moment it follows a helper it
// hands off to scanWith, which recurses into itself. The two are near
// duplicates and each carried its own copy of the ordering, so a fixture
// that only reaches scan leaves scanWith's copy unpinned — which is how
// the hole survived in TWO walks after being closed in the third. Found
// by mutating each hoist separately: the src-level fixture reddened for
// scan and stayed green for scanWith.
var defDeepPhantom = &ElementDef{
	Name:  "DeepPhantom",
	Known: true,
	Attrs: []AttrSpec{
		{Name: "DeepGhost"},
	},
	Build: func(e Element, ctx *Context) (gooey.Component, error) {
		return nil, viaHelper(e, ctx)
	},
}

// viaHelper is followed (it takes the element and is not on the
// deny-list), so its body is walked by scanWith rather than by scan. The
// denied call inside it is what only scanWith can see.
func viaHelper(e Element, ctx *Context) error {
	return checkProps(e, ctx, "DeepGhost")
}
