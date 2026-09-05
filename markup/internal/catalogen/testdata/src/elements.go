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
package fake

var defHost = &ElementDef{
	Name:  "Host",
	Known: true,
	Attrs: []AttrSpec{
		{Name: "Style"},
		// Read through a ctx-FIRST helper, which is the idiom attr.go
		// documents: markup.Attr[T](ctx, e, "Value"). The walk used to
		// pick the first bare identifier argument, answer "ctx", decide
		// the helper had been handed something that is not the host, and
		// file this read against a CHILD — reporting <Host> as
		// over-declaring an attribute it plainly reads.
		{Name: "Ctxed"},
		// bothRoles reads this off whatever element it is handed, as a
		// HARDCODED index rather than a string argument — which is what
		// lets it see a SKIPPED DESCENT. An attribute named as a call
		// argument is picked up by the literal branch whether or not the
		// walk descends, so it cannot. buildHost calls that helper on
		// its own element AND on each child, so both declare this.
		{Name: "Roled"},
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
		// scan recognises that form; the child walk did not, and the
		// consequence was a FALSE over-declaration — a red test
		// asserting the opposite of what the code does.
		{Name: "Tick"},
		// Read inside a helper the host hands the CHILD to. The helper
		// names its own parameter, so the receiver split would call
		// this a read of the HOST's attribute unless being handed a
		// child resets it — and the walk would not descend at all
		// unless the gate asks the callee rather than testing for the
		// identifier "e". Both were wrong; <MenuItem Icon> is the real
		// case that found them (#400).
		{Name: "Deep"},
		// Read through bothRoles, which buildHost also calls on its OWN
		// element — the case a name-keyed `seen` set cannot see.
		{Name: "Roled"},
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
	// The same, through a helper whose ELEMENT PARAMETER IS SECOND.
	// Resolving the element by argument order gets this wrong; resolving
	// it from the callee's signature gets it right.
	_, _ = ctxFirst[int](ctx, e, "Ctxed")
	// The SAME helper the loop below calls with a child, called here
	// with the host's own element. A `seen` set keyed on the function
	// NAME records this call and skips that one, so the child's read
	// never lands and <Child> is reported as over-declaring it. Keyed on
	// (name, role), both are scanned.
	bothRoles(e)
	for _, c := range e.Children {
		// Read off a CHILD element. This is <Child>'s surface.
		_ = c.Attrs["Text"]
		// The same, through a helper: the attribute name is a string
		// ARGUMENT, and the element it belongs to is the first bare
		// identifier — c here, e would make it the host's own.
		_ = optDuration(c, "Tick")
		childExtras(c)
		bothRoles(c)
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
// optDuration stands in for Bound / BoundColor / optDuration — the
// helper idiom where the attribute name is an argument.
func optDuration(e Element, name string) any { return nil }

// childExtras stands in for menuItemIcon (#400's helper, which lands one
// PR above this one — not greppable from this branch): a helper the host hands a
// CHILD element to, which then reads off its own parameter. Its
// parameter is deliberately named c, matching the caller's variable, so
// a walk that inherited the caller's `self` would file the read as the
// host's own and report <Child> over-declaring an attribute the code
// plainly reads.
func childExtras(c Element) {
	_ = c.Attrs["Deep"]
}

// bothRoles is called on the host's own element AND on each child. Its
// parameter is named `e` on purpose: scan — the walk for ordinary
// elements — resolves a read only off a receiver literally named "e"
// (attrOf), so a helper named otherwise is invisible to it and the HOST
// half of this fixture could not be expressed at all. That is a real
// limit of the ordinary walk, orthogonal to what this fixture is for,
// and widening scan is exactly the repair that broke three elements of
// the shipped vocabulary once already.
func bothRoles(e Element) {
	_ = e.Attrs["Roled"]
}

// ctxFirst stands in for markup.Attr, whose signature puts ctx before
// the element. Its own reads are off its element parameter, so they
// belong to whoever was passed in — the HOST here.
func ctxFirst[T any](ctx *Context, e Element, name string) (T, error) {
	var zero T
	_ = e.Attrs[name]
	return zero, nil
}

func checkAttrs(e Element, ctx *Context) error {
	for _, k := range e.Children {
		_ = k.Attrs["Smuggled"]
	}
	return nil
}
