package markup

import (
	"sort"

	"github.com/WonderForgeLabs/gooey"
)

// ElementDef is one element of the markup vocabulary: what may be set on
// it, what may nest inside it, and how to build it — in ONE literal.
//
// # Why the vocabulary lives here rather than beside here
//
// The vocabulary used to be implicit in control flow: an arm read
// e.Attrs["Content"] and nothing anywhere said <Button> accepts Content.
// The first attempt to recover it derived the table from the switch with
// go/ast and committed the result. That worked, but it answered the
// wrong question — a build step producing an artifact, when what the
// system actually needs is for each element to STATE its own surface.
//
// Colocation is the mechanism, and it is not a matter of discipline.
// companionAttrs has been a parallel declaration that COULD drift for as
// long as it has existed, and it never has — because it is nine names
// sitting directly above the code that reads them. Put the same table in
// another file and it rots. That observation is why this is one literal
// per element rather than a table beside a switch.
//
// # What is declared and what is derived
//
// Attrs, Slots, Children and Open are DECLARED, because only the author
// of an arm knows them.
//
// The behavioural axes — NonVisual, Focusable, Attaches, HasLayout — are
// DERIVED from Proto, by type assertion against the framework's marker
// interfaces. They are deliberately not fields: a hand-written
// `NonVisual: true` is a second copy of a fact the type already carries,
// and the second copy is the one that goes stale. A type assertion is
// not reflection — it is the same mechanism Bound and every markup
// type-switch already use.
type ElementDef struct {
	// Name is the element name as written in markup.
	Name string

	// Proto is a zero-valued instance of the component this element
	// builds, used ONLY to derive the behavioural axes. It is never
	// rendered, never mutated, and never handed out.
	//
	// Nil is legal and means "this element builds no component of its
	// own" — <Tab> is a pseudo-element parsed by its parent — in which
	// case every derived axis is false and Known should be false too.
	Proto gooey.Component

	// Attrs is the element's own attribute vocabulary. It does NOT
	// include the universal surface (Name, Tooltip, layout) — that is
	// joined in by AttrsFor where TakesLayout holds.
	Attrs []AttrSpec

	// Slots are the property elements this element accepts.
	Slots []SlotSpec

	// Children is what may nest inside.
	Children ChildSpec

	// Grants is what this element confers ON ITS CHILDREN: the layout
	// model they are positioned by, and the attached attributes that
	// carry it. See Grant.
	//
	// DECLARED, not derived, and the reason is the same one that makes
	// Attrs declared: only the author of the arm knows. The Build
	// function above hands its children to a components.Grid, and
	// nothing about that type says the markup spelling of a cell is
	// "Grid.Row" — the spelling is a fact about this element's
	// vocabulary, which is what this file is for. A marker interface on
	// the component could carry the KIND, but not the names, and half a
	// contract in a second place is the drift this literal exists to
	// prevent.
	//
	// TestEveryMultiChildElementDeclaresAGrant makes the omission loud:
	// a new ModeMany container with no grant fails the suite rather than
	// silently telling every editor that its children may be reordered.
	Grants Grant

	// Body declares that this element's content is its XML BODY rather
	// than an attribute — see BodySpec. Nil for everything that reads
	// its content from e.Attrs, which is all but one builtin.
	Body *BodySpec

	// Open marks an element whose attribute set is EXTENDED at runtime
	// from the Context — <Validate>, whose vocabulary is its builtin
	// rules plus Context.Rules. An open element's Attrs is the builtin
	// half only; ctxAttrs supplies the rest.
	//
	// This is load-bearing rather than cosmetic. With unknown attributes
	// rejected, flattening an open element to its literal would make the
	// loader REFUSE VALID MARKUP — a host registering an Email rule
	// would find <Validate Email="true"/> rejected by the very mechanism
	// built to make attribute mistakes visible.
	Open bool

	// ParsedBy names the element whose Build actually consumes this
	// one's attributes, for a pseudo-element that builds no component of
	// its own — <Menu> and <MenuItem> are read by buildMenuBar.
	//
	// It exists because the vocabulary had no way to say "declared here,
	// read there", and the absence had a cost: the only shape available
	// was <Tab>'s, which pairs a nil Proto with Known false and an
	// Opaque reason. That is honest for <Tab>, whose attributes really
	// are whatever <Tabs> cares to read — but it is wrong for a
	// pseudo-element whose surface IS knowable, and paying it meant a
	// <MenuItem> could not tell a property grid it has a Text. Which is
	// what #429 reported, from the far end: no way to set a menu item's
	// label except by hand in $EDITOR.
	//
	// IT IS CHECKED, NOT TRUSTED, and that is what makes it a field
	// rather than a comment. catalogen resolves an element's Build
	// through this name, so a wrong one — or a right one that stops
	// reading an attribute — fails TestDeclaredVocabularyMatchesTheCode
	// exactly as an ordinary element's own drift does. The declaration
	// cannot quietly disagree with the code that reads it.
	//
	// The named element is the one that PARSES, not necessarily the
	// parent: <MenuItem> sits inside <Menu>, but <Menu> only refuses a
	// standalone use, and buildMenuBar walks both levels.
	ParsedBy string

	// Known reports whether Attrs is exhaustive. False for a
	// pseudo-element whose attributes are parsed by its parent.
	Known bool

	// Opaque is why Known is false.
	Opaque string

	// DynamicAttrs marks an element that consumes its attributes by
	// RANGING over e.Attrs against its own table, rather than reading
	// them by name. The cross-check in internal/catalogen cannot follow
	// that — a loop index is not a literal — so such an element is
	// skipped there and must validate its own vocabulary at load.
	//
	// Both elements that need this had a declared table before the
	// registry existed (companionAttrs, validateBuiltins), which is not
	// a coincidence: ranging is WHY they needed one. The value is the
	// reason, and TestDynamicAttrElementsAreExactlyTheseOnes pins the
	// set so it cannot quietly grow.
	DynamicAttrs string

	// Icon NAMES an icon for this element in the host's icon set. It is
	// a name — no directory, no extension, no colour — and that is the
	// whole reason the field can exist here at all.
	//
	// Rasterizing an SVG needs oksvg and rasterx, which live in
	// imagefmt/svg, which is a SEPARATE MODULE precisely so the core
	// graph stays free of them. A field holding an image.Image, a
	// decoder, or an fs.FS would drag that decision back into this
	// package and make every docs/learn example that imports markup
	// inherit a vector renderer. A string drags in nothing: core states
	// WHAT THE ELEMENT IS, and the host decides what it looks like.
	//
	// The indirection is the same one KindStyle already uses. A style
	// attribute carries a name the app's style table resolves; an app
	// that swaps its palette swaps the table, not the markup. An icon
	// name resolves against whatever set the host loaded — Codicons in
	// apps/wysiwyg, something else elsewhere — so an element is not
	// pinned to one icon vendor by a field in this file.
	//
	// Empty means the element declares no icon, which a consumer must
	// render as "no icon" rather than substituting one silently: the
	// same honesty rule AttrsKnown carries for attributes.
	Icon string
	// Seed is the markup a palette inserts for a NEW instance of this
	// element: the attributes, body, children and slots that make one
	// worth looking at the moment it appears.
	//
	// Every element a palette can OFFER needs one, and that includes an
	// element a host registers through Context.Elements rather than
	// building in. TestEveryElementDeclaresASeed walks the builtin
	// registry alone, so a registered element that declares no Seed goes
	// red nowhere: the requirement reaches it, the enforcement does not,
	// and holding up that half is the registering host's job.
	//
	// It is NOT AttrSpec.Default, and the difference is the whole reason
	// this field exists. Default is the value equivalent to OMITTING an
	// attribute — Width's is "0" — and TestDeclaredDefaultsRenderIdentically
	// ToOmission enforces exactly that. Seeding from it produces a
	// component of zero size, which is the bug: four elements were added
	// to a canvas measuring 0x0, invisible and therefore unselectable,
	// and two would not load at all.
	//
	// Markup rather than a struct because the answer has to cover more
	// than attributes. An empty <VStack> measures nothing no matter what
	// its attributes say — it needs CHILDREN — and <MenuBar> needs a
	// <Menu Title="…">. Those are expressible here in the one notation
	// this package already parses, validates and reports load errors for,
	// so a seed that cannot build is a test failure rather than a red
	// island in somebody's editor.
	//
	// Two rules a seed must follow, both checked:
	//
	//   - A container's seed names its children INLINE, and those are
	//     taken verbatim: the <Text> inside <VStack>'s seed is not
	//     re-seeded from <Text>'s own seed. The parent's seed wins at kid
	//     level, because a seed is one instance and not a recipe applied
	//     recursively — recursion here would make <Border>'s seed grow a
	//     Text that grew a body that grew, and there is no fixed point.
	//
	//   - It may NOT carry parent-dependent attributes. Canvas.Left and
	//     Canvas.Top are legal only under a <Canvas> and are silently
	//     discarded anywhere else, so they are the inserting editor's job
	//     — it knows the real target, and AttrsFor(spec, parent) is the
	//     function that answers it.
	//
	// A bind-only attribute cannot be seeded from here at all: markup
	// carries a {{.Path}}, never the live *prop.Property[T] behind it.
	// Those name a placeholder the inserter registers; see PlaceholderFor.
	Seed string

	// Doc is one line about the element.
	Doc string

	// Build constructs the component. This is the arm body.
	Build func(e Element, ctx *Context) (gooey.Component, error)
}

// axes derives the four behavioural facts from Proto.
//
// Type assertion, not reflection: each is a single-method interface the
// framework already defines, and asking whether a concrete type
// satisfies one is exactly what a type switch does everywhere else in
// this package.
func (d *ElementDef) axes() (nonVisual, focusable, attaches, hasLayout bool) {
	if d.Proto == nil {
		return false, false, false, false
	}
	if nv, ok := d.Proto.(gooey.NonVisual); ok {
		nonVisual = nv.NonVisual()
	}
	_, focusable = d.Proto.(gooey.Focusable)
	_, attaches = d.Proto.(gooey.Attacher)
	_, hasLayout = d.Proto.(gooey.HasLayout)
	return
}

// holdsSeveralChildren reports whether this element positions more than
// one visual child — the question both grant contracts actually ask, and
// which ModeMany answered for only two thirds of the elements that do
// it.
//
// ModeRestricted WAS READ AS "not a container", and it is not: <Tabs>
// and <MenuBar> restrict their children to a NAME rather than to a
// count, and each holds as many as the markup gives it. Gating on
// ModeMany alone made a <Tab> read as DragFixed — "placed by its parent,
// nothing to edit" — in an editor whose whole job includes reordering
// tabs. Found in review of #390 (issue #418).
//
// DERIVED FROM Proto, for the reason the axes above are. The fact that
// separates <Tabs> from <Companion> — also ModeRestricted, and whose
// <Arg>/<Var> children are process arguments with no geometry at all —
// is whether the framework walks the children AS COMPONENTS.
// gooey.Container is that question and the type already answers it.
// Naming the two containers here instead would put a third copy of the
// vocabulary beside a registry that has it, and the copy is the one that
// goes stale: the next restricted container would be silently
// undesignable with the suite still green.
//
// It cannot be answered from Children.Only either. Neither <Menu> nor
// <MenuItem> has an ElementDef — <MenuBar> parses them itself — so
// "every name is a defined element" would classify <MenuBar> as holding
// nothing visual, which is the same bug in a different place.
func (d *ElementDef) holdsSeveralChildren() bool {
	switch d.Children.Mode {
	case ModeMany:
		return true
	case ModeRestricted:
		_, ok := d.Proto.(gooey.Container)
		return ok
	}
	return false
}

// spec renders the declaration as the catalog entry consumers read.
func (d *ElementDef) spec() ElementSpec { return d.specAs(OriginBuiltin) }

// specAs is spec with the provenance supplied by the caller, because the
// SAME declaration means different things depending on who registered
// it. A definition in this package's registry is builtin; the identical
// struct handed to Context.Elements by a host app is registered, and a
// palette that showed it as builtin would be claiming this build of
// gooey compiled it in.
//
// Origin is provenance only — a consumer deciding whether Attrs is
// trustworthy must read AttrsKnown, which is d.Known either way. That is
// the whole point of the seam: a registered element with a declaration
// is exactly as knowable as a builtin one.
func (d *ElementDef) specAs(origin Origin) ElementSpec {
	nonVisual, focusable, attaches, hasLayout := d.axes()
	// Copied for the same reason Attrs and Slots are: a spec is handed
	// out, and a caller must not be able to reach back through it and
	// edit the registry's own definition.
	var body *BodySpec
	if d.Body != nil {
		b := *d.Body
		body = &b
	}
	return ElementSpec{
		Name:       d.Name,
		Origin:     origin,
		Go:         goTypeOf(d.Proto),
		AttrsKnown: d.Known,
		Opaque:     d.Opaque,
		Open:       d.Open,
		Attrs:      append([]AttrSpec(nil), d.Attrs...),
		Slots:      append([]SlotSpec(nil), d.Slots...),
		Body:       body,
		Children:   d.Children,
		Icon:       d.Icon,
		Grants: Grant{
			Kind: d.Grants.Kind,
			// Copied for the same reason Attrs and Slots are: a spec is
			// handed out, and a caller must not be able to reach back
			// through it and edit the registry's own definition.
			Attached: append([]AttrSpec(nil), d.Grants.Attached...),
		},
		Seed: d.Seed,
		// A NIL Proto IS NOT ENOUGH ON ITS OWN, and the difference only
		// shows for a HOST's def. TestDeclaredElementsCarryAProtoOrSay
		// WhyNot forces a Proto-less element to say why — Opaque, or a
		// ParsedBy naming its reader — but it ranges over the builtin
		// registry and never sees anything in Context.Elements. A host
		// def with a real Build and no Proto is legal and nothing
		// rejects it, so deriving Pseudo from the nil alone would make
		// it silently pseudo: dropped from every palette that filters
		// Nested, and unselectable-through in the designer, with no
		// error anywhere. Requiring the STATED reason means the field
		// is only true where something enforces it.
		Pseudo:    d.Proto == nil && (d.Opaque != "" || d.ParsedBy != ""),
		NonVisual: nonVisual,
		Focusable: focusable,
		Attaches:  attaches,
		HasLayout: hasLayout,
		Doc:       d.Doc,
	}
}

// elementDefs is the registry, keyed by element name. Registration
// happens in this package's init from the per-element literals.
var elementDefs = map[string]*ElementDef{}

// registerElements adds definitions to the registry. A duplicate name is
// a programming error and panics at init rather than silently shadowing:
// two definitions for one element means one of them is unreachable, and
// which one wins would depend on map iteration order.
func registerElements(defs ...*ElementDef) {
	for _, d := range defs {
		if _, dup := elementDefs[d.Name]; dup {
			panic("markup: duplicate element definition for <" + d.Name + ">")
		}
		elementDefs[d.Name] = d
	}
}

// definedElements returns every builtin definition, sorted by name.
func definedElements() []*ElementDef {
	out := make([]*ElementDef, 0, len(elementDefs))
	for _, d := range elementDefs {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
