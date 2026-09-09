package main

import (
	"strings"
	"unicode"

	"github.com/WonderForgeLabs/gooey/components"
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
// non-visual elements and minus the NESTED ones, and a nested element is
// precisely what this file has to be able to reason about — <Tab> is the
// example, and asking the palette would make a tab's own child rule
// unknowable, which is how the hole above stayed open.
//
// This sentence used to name <Tab> as the filter rather than as the
// example, which was true until markup.ElementSpec.Nested replaced the
// hardcoded name. Deleting that hardcode is the point of the field, so a
// comment still quoting it is the same staleness in prose.
//
// A LOOKUP, not a Catalog() call. Context.Catalog is not a getter — it
// re-derives every builtin spec with fresh Attrs copies, re-runs
// markNested and sorts, and globs and parses every include file when a
// context has them. This is asked three times inside one add gesture
// (planAdd, canHold, wrapperNode) and once per property-grid row build
// from target(), which runs inside a paint node. ed.specs is that
// catalog by name, taken once in loadPalette — where the vocabulary
// actually changes — alongside the palette and the pseudo set, so all
// three are answers to one read rather than three reads that could
// disagree.
func (ed *editor) specOf(elem string) (markup.ElementSpec, bool) {
	e, ok := ed.specs[elem]
	return e, ok
}

// canHold reports whether an element named parent may take a child named
// elem, as far as the catalog knows.
func (ed *editor) canHold(parent, elem string) bool {
	spec, ok := ed.specOf(parent)
	if !ok {
		return false
	}
	// A NESTED ELEMENT HAS EXACTLY ONE LEGAL HOME, so the permissive
	// modes have to refuse it; ModeRestricted below already asks the
	// right question by name. Without this, canHold("Canvas",
	// "MenuItem") is true, paste lands the node at the root, the rebuild
	// fails and docRoot goes nil while the status line says it worked —
	// issue #403's failure mode, reached by the gesture selectChild
	// opens. Found in review of #454.
	if e, ok := ed.specOf(elem); ok && e.Nested && spec.Children.Mode != markup.ModeRestricted {
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
// would be a coin toss the user cannot see. Better to climb and let the
// user place it deliberately than to guess.
//
// THIS PARAGRAPH USED TO CITE <MenuBar> AS THAT CASE, "(Only: Menu,
// MenuItem)", and this PR is what made it false: the list is {"Menu"}
// now, because the two-entry version declared a child the builder refuses
// ("<MenuBar> children must be <Menu> elements"). So MenuBar moved from
// the example of the DECLINE path to a live user of the WRAP path, and
// the comment went on describing the old world. Reported in review of
// #454 — a doc citing a data structure it does not read is exactly the
// drift that stays resolvable while meaning something else.
//
// No restricted container names two children today, so the decline branch
// has no example in the shipped vocabulary. It is kept because the
// vocabulary is open — a host registers its own elements — not because
// something in this repo reaches it.
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
// The seed's CONTENT is dropped — both halves of it. Kids and Body are
// the same category of thing, the seed author's worked example, and the
// wrapper exists to hold what the user asked for; keeping either would
// silently add content nobody chose. Only the ATTRIBUTES carry over,
// because those are what make the wrapper legal rather than what fills
// it.
//
// Body was copied here until a review caught it. It was dormant — the one
// single-candidate restricted container today is <Tabs>, and its example
// <Tab> carries a <Text> CHILD rather than body text of its own — but
// nothing enforced that, so a future wrapper element seeded with body
// text would have reintroduced exactly the bug dropping Kids prevents.
//
// KNOWN LIMIT, stated rather than hidden: every wrapper built this way
// carries the same attribute values, so a second added tab repeats the
// first's header. That much is cosmetic — a header is a label, not an
// address, so nothing is shadowed and nothing fails to build — and
// fixing it needs a notion of "the attribute that labels this element"
// that the catalog does not have today.
//
// THE MENU CASE IS NOT COSMETIC, and reading it as one was wrong against
// a rule this same PR documents. A menu title with no `_` marker does
// not claim nothing — menus fall back to the FIRST LETTER
// (components/mnemonic.go, and the per-component doctrine at the top of
// that file: buttons take only an explicit marker, menus do not). So a
// second <Menu Title="File"> claims alt+f exactly as the first does, and
// MenuBar.titleWithAccel takes the first match: the new menu is
// unreachable from the keyboard, and which one loses is a fact about
// tree order no reader of the markup can see. The previous version of
// this paragraph said "the clone claims no accelerator", derived from
// the absence of an underscore — the same local re-derivation of the
// mnemonic rule that mnemonic.go records this module getting wrong in
// review of #428. Found in review of #454.
//
// unshadowMnemonic below is the fix, and it is deliberately narrow: it
// gives the clone an EXPLICIT marker on a letter no sibling claims,
// leaving the repeated text alone. The text repeat stays a documented
// cosmetic limit; the accelerator collision does not, because it makes a
// menu the user just created impossible to open.
func (ed *editor) wrapperNode(into *node, wrap string) *node {
	parent := into.Elem
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
		return &node{Elem: wrap, Attrs: attrs}
	}
	return bare
}

// unshadowMnemonic keeps a node about to be inserted from stealing a
// sibling's keyboard accelerator.
//
// AT THE INSERTION SEAM, called once beside each append, and that is the
// half review round 10 corrected. It used to live inside wrapperNode,
// which covered the ONE route to a <MenuBar> that cannot be reached from
// the palette: wrapperFor("MenuBar", elem) needs canHold("Menu", elem),
// and <Menu>'s Only is {"MenuItem"}, which loadPalette drops as Nested —
// so addSelected never produces wrap == "Menu". Both direct gestures were
// unguarded and both reproduce it: ctrl+d on a selected <Menu>
// (duplicateSelected, which <Menu> became selectable for IN THIS PR), and
// y-then-p (insertSubtree, where a <Menu> into a <MenuBar> needs no
// wrapper at all).
//
// It sits beside the renameInto/clone rename at each of those seams,
// which solves the same collision problem for Name. MOVES are not seams:
// promote and demote relocate a node that was already in the document,
// and a <Menu> can only live in a <MenuBar>, which cannot nest — so
// neither can produce a second claimant.
//
// It asks components.MenuMnemonic rather than looking for an underscore,
// which is the whole point: that function is EXPORTED FOR THE COLLISION
// GUARDS, and a local re-derivation of the rule is the defect
// mnemonic.go records this module shipping once already (review of
// #428 — it required an explicit marker, so every menu relying on the
// first-letter fallback was invisible to it, which is exactly the case
// here).
//
// "MenuBar" IS NAMED, and it is the only element name in this file. The
// rule being applied is menu-flavoured — mnemonic.go says so, and says a
// guard about buttons must not reach for this answer — so it may not be
// applied to whatever element happens to be a single-candidate wrapper.
// The PARENT is what the name tests, because the parent is what
// dispatches the alt gesture (MenuBar.HandleMnemonic); that is the rule
// itself rather than a proxy for it. markup.ElementSpec carries no "this
// attribute is an accelerator" fact — ElementDef.ParsedBy is not on the
// catalog's surface — so deriving it would only move the name.
func (ed *editor) unshadowMnemonic(into, n *node) {
	if into == nil || n == nil || n.Attrs == nil || into.Elem != "MenuBar" {
		return
	}
	spec, ok := ed.specOf(n.Elem)
	if !ok {
		return
	}
	for _, a := range spec.Attrs {
		if !a.Required || a.Kind != markup.KindString {
			continue
		}
		want, has := components.MenuMnemonic(n.Attrs[a.Name])
		if !has {
			continue
		}
		claimed := map[rune]bool{}
		for _, sib := range into.Kids {
			// sib != n because the seams differ: two call it before the
			// append and one after a wrapper is built, and a node that
			// claimed its own letter would always look shadowed.
			if sib == n || sib.Elem != n.Elem {
				continue
			}
			if r, ok := components.MenuMnemonic(sib.Attrs[a.Name]); ok {
				claimed[r] = true
			}
		}
		if !claimed[want] {
			continue
		}
		if marked, ok := markUnclaimed(n.Attrs[a.Name], claimed); ok {
			n.Attrs[a.Name] = marked
		}
	}
}

// markUnclaimed puts a `_` before the first letter or digit of title that
// no sibling has claimed, and reports whether it found one.
//
// It reports false rather than marking arbitrarily when every letter is
// taken: a wrapper carrying the seed's title unchanged is a repeat the
// user can see and fix, while one carrying a marker on a letter someone
// else already owns would be the same shadowing wearing a fix.
//
// THE STRING IT WALKS IS ENCODED, and that is the correction round 10
// asked for. The first version did strings.ReplaceAll(title, "_", "") and
// scanned the result, which deletes a LITERAL `__` as readily as a
// marker: markUnclaimed("Sa__ve", {'s'}) returned "S_ave", so a label the
// user wrote to read `Sa_ve` came back reading `Save`. `__` is a literal
// underscore in this convention — components/mnemonic.go's
// splitExplicitMnemonic, and the <Menu Title> row in
// docs/markup-reference.md.
//
// There is no exported encoder to borrow and this is not a re-derivation
// of the parse: mnemonic.go owns reading a marker, and nothing in the
// framework ever WRITES one, because a marker is authored. So the loop
// below normalises rather than interprets — it drops the first marker,
// leaves every literal escaped as `__`, and escapes a bare `_` the parser
// would have shown literally, which is what makes inserting one `_` in
// front of a chosen rune unambiguous. components.MenuMnemonic stays the
// authority on what a title CLAIMS; this only has to find a letter it
// does not.
func markUnclaimed(title string, claimed map[rune]bool) (string, bool) {
	in := []rune(title)
	out := make([]rune, 0, len(in)+2)
	// spots are the candidate accelerators: the rune, and where it sits
	// in out, so the marker goes in front of the letter and not in front
	// of an escape.
	type spot struct {
		at int
		r  rune
	}
	var spots []spot
	dropped := false
	for i := 0; i < len(in); i++ {
		r := in[i]
		if r == '_' && i+1 < len(in) && in[i+1] == '_' {
			out = append(out, '_', '_')
			i++
			continue
		}
		if r == '_' && i+1 < len(in) && !dropped {
			// The existing marker. It goes, so the one this adds is the
			// first — only the first counts — and the letter it named
			// stays a candidate.
			dropped = true
			continue
		}
		if r == '_' {
			// A second marker's underscore, or a trailing one: the parser
			// shows both literally, so they are escaped here and the
			// display text is unchanged.
			out = append(out, '_', '_')
			continue
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			spots = append(spots, spot{len(out), r})
		}
		out = append(out, r)
	}
	for _, s := range spots {
		if claimed[unicode.ToLower(s.r)] {
			continue
		}
		return string(out[:s.at]) + "_" + string(out[s.at:]), true
	}
	return title, false
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
	// THE FALLBACK ASKS TOO. Fixing canHold alone still lands the node
	// here: the loop finds nothing that can hold it and drops to the
	// root, which is the illegal parent by another route. A refusal is
	// the honest answer for an element with one legal home and no
	// instance of it on the page.
	if e, ok := ed.specOf(elem); ok && e.Nested && !ed.canHold(ed.doc().Elem, elem) {
		return addPlan{}
	}
	return addPlan{into: ed.doc()}
}

// addTarget is planAdd's landing node, kept for the callers that only ask
// "where would this go".
func (ed *editor) addTarget(elem string) *node { return ed.planAdd(elem).into }
