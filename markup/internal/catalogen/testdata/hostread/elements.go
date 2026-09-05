// Package fake is a FIXTURE, not a build. catalogen reads .go files with
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
package hostread

var defHost = &ElementDef{
	Name:  "Host",
	Known: true,
	Attrs: []AttrSpec{
		{Name: "Style"},
	},
	Build: func(e Element, ctx *Context) (gooey.Component, error) {
		return buildHost(e, ctx)
	},
}

var defChild = &ElementDef{
	Name:     "Child",
	ParsedBy: "Host",
	Known:    true,
	Attrs: []AttrSpec{
		{Name: "Text"},
		{Name: "OwnRead"},
		// Read only through the helper idiom below, never as an index.
		{Name: "Tick"},
		{Name: "Style"}, // read off the HOST, not off a child — must be reported
	},
	Build: func(e Element, ctx *Context) (gooey.Component, error) {
		// A pseudo-element's OWN Build reading an attribute is a legal
		// shape, and Check's ParsedBy branch used to skip it entirely —
		// so the read was unattributed and the declaration serving it
		// was reported over-declared, both wrong at once.
		_ = e.Attrs["OwnRead"]
		return nil, errStandalone
	},
}

func buildHost(e Element, ctx *Context) (gooey.Component, error) {
	// Read off the HOST's own element. This must be attributed to
	// <Host>, never to <Child>.
	_ = e.Attrs["Style"]
	for _, c := range e.Children {
		// Read off a CHILD element. This is <Child>'s surface.
		_ = c.Attrs["Text"]
		_ = optDuration(c, "Tick")
		// A call into the generic builder machinery. scanChildAttrs must
		// not follow it, or the literals inside land in the child set
		// and get reported as attributes nobody declared.
		if err := checkAttrs(c, ctx); err != nil {
			return nil, err
		}
	}
	return nil, nil
}

// checkAttrs stands in for the general builder machinery. It reads off a
// variable that is NOT its own element parameter, which is what makes it
// discriminating: the receiver split alone would file such a read as the
// host's own and hide it, so only the deny-list keeps "Smuggled" out of
// the child set.
func optDuration(e Element, name string) any { return nil }

func checkAttrs(e Element, ctx *Context) error {
	for _, k := range e.Children {
		_ = k.Attrs["Smuggled"]
	}
	return nil
}
