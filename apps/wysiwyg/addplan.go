package main

import (
	"strings"

	"github.com/WonderForgeLabs/gooey/markup"
)

// WHERE A PALETTE INSERT LANDS, and what has to be built around it to make
// it legal there.
//
// # The hole this closes
//
// <Tabs> declares ChildSpec{ModeRestricted, Only: ["Tab"]}, and <Tab> is
// filtered OUT of the palette — it is a pseudo-element that <Tabs> parses
// itself, so a palette entry for it would offer something the loader
// refuses everywhere except in the one place the palette cannot put it.
//
// The result was a closed loop. <Tabs> was addable, and NOTHING could
// ever be added to it: the one child it accepts was the one element the
// palette would not offer. Worse, holdsChildren answered `true` for it —
// it checked only Leaf/None/Attachments and never looked at Only — so
// addTarget cheerfully returned the <Tabs> and every palette entry wrote
// an illegal child. Two keystrokes from a working editor to a document
// that does not build, which killed click-to-select for the whole
// document (see rebuild and DragStale). It was reported twice.
//
// # The rule, and why it is not a table
//
// Everything here reads the CATALOG. Which children an element takes is
// already declared — ChildSpec.Mode and ChildSpec.Only — and a second
// copy in this file would be the drift markup/elementdef.go's one-literal
// design exists to prevent. Nothing below names <Tabs> or <Tab>.
//
// # Permissive where the catalog is silent, because the build is the gate
//
// canHold answers false only where the catalog KNOWS the child is
// refused. ModeUnknown — an opaque element whose child rule could not be
// enumerated — and ModeOne, which cannot say whether the slot is already
// taken, both answer true and let the insert be TRIED. That is safe
// because addSelected is transactional: it builds the candidate document
// and reverts on failure, naming both elements. Guessing "no" here would
// silently move the insert somewhere the user did not ask for; guessing
// "yes" costs a refusal message that says exactly what happened.

// specOf is the catalog entry for an element name.
//
// The CATALOG, not ed.palette. The palette is the catalog minus the
// non-visual elements and minus <Tab>, and <Tab> is precisely the entry
// this file has to be able to reason about — asking the palette would
// make a tab's own child rule unknowable, which is how the hole above
// stayed open.
func (ed *editor) specOf(elem string) (markup.ElementSpec, bool) {
	for _, e := range ed.docCtx.Catalog() {
		if e.Name == elem {
			return e, true
		}
	}
	return markup.ElementSpec{}, false
}

// canHold reports whether an element named parent may take a child named
// elem, as far as the catalog knows.
func (ed *editor) canHold(parent, elem string) bool {
	spec, ok := ed.specOf(parent)
	if !ok {
		return false
	}
	switch spec.Children.Mode {
	case markup.ModeLeaf, markup.ModeNone, markup.ModeAttachments:
		return false
	case markup.ModeRestricted:
		for _, only := range spec.Children.Only {
			if only == elem {
				return true
			}
		}
		return false
	}
	return true
}

// wrapperFor is the element that has to go BETWEEN a container and the
// child the user asked for, or "" when none will do.
//
// This is what makes "add a <Button> with the <Tabs> selected" mean the
// thing a designer means by it: a new tab, holding the button. The
// alternative — climbing past the <Tabs> and dropping the button beside
// it — is defensible and is what happens when no wrapper fits, but it
// silently ignores where the user was pointing.
//
// EXACTLY ONE CANDIDATE, or nothing. A restricted container naming two
// permitted children has no single right answer, and picking the first
// would be a coin toss the user cannot see; <MenuBar> is that case today
// (Only: Menu, MenuItem). Better to climb and let them place it
// deliberately than to guess.
func (ed *editor) wrapperFor(parent, elem string) string {
	spec, ok := ed.specOf(parent)
	if !ok || spec.Children.Mode != markup.ModeRestricted {
		return ""
	}
	if len(spec.Children.Only) != 1 {
		return ""
	}
	w := spec.Children.Only[0]
	if w == elem || !ed.canHold(w, elem) {
		return ""
	}
	return w
}

// wrapperNode builds the scaffolding element, with the attributes its
// PARENT's seed says a well-formed one carries.
//
// The parent's seed, not the wrapper's own, and that is forced rather
// than chosen: a pseudo-element declares no Seed of its own — <Tabs>
// parses <Tab>, so <Tab> has no builder and nothing to seed from — while
// the container's seed contains a worked example of exactly this child.
// <Tabs>'s is `<Tabs><Tab Header="One">…`, and Header is REQUIRED
// (`markup: <Tab> needs a Header`), so a bare node does not build. Reading
// the container's own example is the only source for that which is not a
// table in this file naming "Header".
//
// The seed's CHILDREN are dropped: the wrapper exists to hold what the
// user asked for, and keeping the example's content would silently add an
// element nobody chose.
//
// KNOWN LIMIT, stated rather than hidden: every wrapper built this way
// carries the same attribute values, so a second added tab repeats the
// first's header. That is cosmetic — a header is a label, not an address,
// so nothing is shadowed and nothing fails to build — but it is real, and
// fixing it needs a notion of "the attribute that labels this element"
// that the catalog does not have today.
func (ed *editor) wrapperNode(parent, wrap string) *node {
	bare := &node{Elem: wrap, Attrs: map[string]string{}}
	spec, ok := ed.specOf(parent)
	if !ok || strings.TrimSpace(spec.Seed) == "" {
		return bare
	}
	example, err := nodeOf(spec.Seed)
	if err != nil {
		return bare
	}
	for _, k := range example.Kids {
		if k.Elem != wrap {
			continue
		}
		attrs := map[string]string{}
		for name, v := range k.Attrs {
			attrs[name] = v
		}
		return &node{Elem: wrap, Attrs: attrs, Body: k.Body}
	}
	return bare
}

// addPlan is where the insert goes and what wraps it.
type addPlan struct {
	into *node
	// wrap is an element name to build around the new node, or "".
	wrap string
}

// planAdd resolves the selection into a landing site for elem.
//
// It CLIMBS, which is the half that removes the old silent failure. The
// previous version looked at the selection and then at its parent and
// then gave up on the document root, so a selection two levels inside
// something that cannot hold elem landed the insert at the root with no
// indication that it had moved. Walking up until something can hold it
// puts the element as close to where the user was pointing as the
// vocabulary allows.
//
// The wrap is tried BEFORE the climb, because a container that can take
// the element via its own declared child is a better answer than its
// grandparent.
func (ed *editor) planAdd(elem string) addPlan {
	for n := ed.sel; n != nil && !ed.isSurface(n); n = ed.parentOf(n) {
		if ed.canHold(n.Elem, elem) {
			return addPlan{into: n}
		}
		if w := ed.wrapperFor(n.Elem, elem); w != "" {
			return addPlan{into: n, wrap: w}
		}
	}
	return addPlan{into: ed.doc()}
}

// addTarget is planAdd's landing node, kept for the callers that only ask
// "where would this go".
func (ed *editor) addTarget(elem string) *node { return ed.planAdd(elem).into }
