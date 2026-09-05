package main

// Click-to-select: the designer's half of "pick the thing you can see".
//
// The framework already does the hard part, and the shape of this file is
// dictated by which part that is. In DESIGN mode the designer is a
// gooey.Frozen host, so a press anywhere inside the document is retargeted
// to the pane (mouse.go:176) and the document's own components never act —
// that is the mode. But hit-testing is deliberately NOT retargeted
// (component.go:86-93 says so in as many words: "Stopping the descent here
// would make click-to-select impossible"), so the deepest component under
// the pointer is still recoverable by asking for it.
//
// So there are exactly two things left to do, and both are here:
//
//  1. ask FocusManager.HitTest what the pointer is actually over, and
//  2. walk that answer UP to the top-level design node that owns it,
//     because root.Kids is flat and the hit is not.
//
// No reflection, no new framework seam, and no second copy of the routing
// rules — step 1 IS the framework's query rather than a re-derivation of it.

import "github.com/WonderForgeLabs/gooey"

// bindPicking wires the designer's selection gesture.
//
// hit is passed in rather than taken from ed.app because the composer is
// not a fixture: App.Swap builds a NEW one on every hot reload of the
// page, and a pinned *gooey.FocusManager would leave the designer
// selecting against a tree that is no longer on screen — a bug with no
// symptom except the wrong element appearing in the properties grid. A
// closure resolves it per press.
// invalidate is what asks for a frame after a write the property graph
// cannot see — see drag.go. Both are injected rather than reached through
// ed.app, because the composer is not a fixture.
func (ed *editor) bindPicking(hit func(x, y int) gooey.Component, invalidate func()) {
	ed.hitTest = hit
	ed.invalidateFn = invalidate
	// The editor IS the Designer: press, drag and release are one
	// interface so a host cannot bind selection and forget movement.
	ed.pv.BindDesigner(ed)
}

// nodeChain is the design nodes the pointer is inside, OUTERMOST FIRST:
// the surface root, then each nested node down to the deepest one under
// the cursor. Empty when the hit is not in the document at all.
//
// THIS IS THE WALK, AND IT IS DELIBERATELY NOT A POLICY. HitTest returns
// the deepest COMPONENT — the <Text> inside the <Border> inside the node
// — and the interesting question is which DESIGN NODE owns it. Answering
// that as a chain rather than as an index is what has let the policy
// invert twice without this function changing a line: it climbed to the
// top-level kid while the selection was a flat index, took the deepest
// node when the surface became chrome, and now indexes from the drill
// scope. Every one of those is an index into this chain.
//
// A press on a container's own chrome and a press on its child produce
// chains that agree on every element but the last, which is why
// nodeAtDepth's clamp is what makes chrome selectable at all.
func (ed *editor) nodeChain(hit gooey.Component) []*node {
	if hit == nil || ed.docRoot == nil {
		return nil
	}
	path, ok := componentPath(ed.docRoot, hit, nil)
	if !ok {
		return nil
	}
	var chain []*node
	for _, c := range path {
		n := ed.nodeOf[c]
		// Skip components that came from no node — a container's internal
		// furniture — and the repeats a multi-component node produces.
		if n == nil || (len(chain) > 0 && chain[len(chain)-1] == n) {
			continue
		}
		chain = append(chain, n)
	}
	return chain
}

// selectionScope is the DRILL SCOPE — Blend's "active container" — and
// the whole reason it needs no state of its own is that it is DERIVED:
//
//	scope == parentOf(selection), clamped to the user's root.
//
// That identity is what makes the three gestures compose without a
// separate variable that could go stale. A single click selects a child
// OF THE SCOPE, so after any click the scope is the parent of what is
// selected and the same click repeated is idempotent. A double-click
// selects one level deeper, which moves the scope down one. Escape
// selects the parent, which moves the scope up one — and because the
// scope followed, the next single click at the same cell re-selects the
// node Escape just landed on instead of drilling straight back in.
//
// A separate `ed.scope` field would have had to be reset by hand from
// four places (a click outside it, a delete, a retype, a document
// rebuild), and each one it was forgotten in is a designer that selects
// something the user cannot see a reason for.
func (ed *editor) selectionScope() *node {
	n := ed.sel
	if n == nil || n == ed.doc() || ed.isSurface(n) {
		return ed.doc()
	}
	p := ed.parentOf(n)
	if p == nil || ed.isSurface(p) {
		return ed.doc()
	}
	return p
}

// nodeAt is the POLICY: which node of the chain a single click selects.
//
// SHALLOW-FIRST, WITHIN THE SCOPE — the outermost node under the pointer
// that is a child of the current drill scope. This is the Blend / Figma /
// Illustrator model and it INVERTED the deepest-first policy that came
// before it, because deepest-first has a defect that gets worse as
// documents nest: a <Border> wrapping a <Text> covers itself entirely
// with its own child, so every press inside it selected the Text and the
// Border was effectively unselectable — and, once dragging arrived,
// unmovable. Its own one-cell chrome was the only way in.
//
// Shallow-first has no such hole. Every node is reachable: the outermost
// by one click, anything below it by a double-click per level, and the
// way back out is Escape.
//
// NIL IS A DECISION, NOT A FAILURE CODE. A press that resolves to the
// surface itself — bare canvas — selects NOTHING. The surface is the
// editor's own workspace rather than part of the document, so selecting
// it would point the properties grid at something the user cannot save
// and offer them attributes that never reach their file. An empty grid is
// the honest answer, and it is a state the selection can hold because it
// is a node pointer rather than an index.
func (ed *editor) nodeAt(hit gooey.Component) *node {
	return ed.nodeAtDepth(hit, 0)
}

// nodeAtDepth is nodeAt with `extra` levels of drill applied — 0 for a
// single click, 1 for a double-click.
//
// EXACTLY ONE LEVEL PER DOUBLE-CLICK, not all the way to the deepest hit.
// Drilling straight to the bottom would make the intermediate containers
// unreachable by pointer again, which is the defect this policy exists to
// remove; and it composes, because after the first double-click the scope
// has moved down one, so the second double-click at the same cell drills
// from there.
//
// Clamping at the end of the chain is what makes the gesture safe to
// repeat: a double-click on something with nothing below it selects that
// same thing rather than nothing.
func (ed *editor) nodeAtDepth(hit gooey.Component, extra int) *node {
	chain := ed.nodeChain(hit)
	if len(chain) < 2 {
		return nil
	}
	i := ed.scopeIndex(chain, ed.selectionScope()) + 1 + extra
	if i >= len(chain) {
		i = len(chain) - 1
	}
	return chain[i]
}

// scopeIndex locates the drill scope in a chain, POPPING OUTWARD until it
// finds one — which is what makes clicking away from the current scope
// leave it rather than select nothing.
//
// With a <Border> drilled into and a sibling <Border> clicked, the scope
// is in neither the chain nor below it; walking up its ancestors reaches
// the user's root, which is in every chain, and the click selects the
// sibling at the level the two share. That is the same "click outside the
// group to leave it" every direct-manipulation editor has, and it falls
// out of the walk rather than needing a rule.
//
// 1 is the floor rather than 0: chain[0] is the SURFACE, which is chrome,
// so the outermost thing a click may ever select is chain[1], the user's
// own root.
func (ed *editor) scopeIndex(chain []*node, scope *node) int {
	for s := scope; s != nil; s = ed.parentOf(s) {
		for i, n := range chain {
			if n == s {
				return i
			}
		}
	}
	return 1
}

// selectParent is Escape, and the spelling is Microsoft's rather than
// invented: "To select the parent of a current selection in the designer,
// press the ESC key" (the WPF designer's own documentation). Figma is the
// odd one out here — there Escape deselects entirely — and the difference
// matters, because a designer where Escape clears the selection gives the
// user no way back UP a tree they drilled into except by clicking outside
// and starting again.
//
// At the top it is a NO-OP rather than a clear. The user's root's parent
// is the surface, which is chrome and must never be selected, and
// clearing instead would make Escape mean two different things depending
// on how deep you happen to be.
func (ed *editor) selectParent() {
	n := ed.sel
	if n == nil || n == ed.doc() || ed.isSurface(n) {
		return
	}
	p := ed.parentOf(n)
	if p == nil || ed.isSurface(p) {
		return
	}
	ed.setSelection(p)
}

// selectChild is alt+enter, the inverse of Escape, and it is THE ONLY
// ROUTE to a node that builds no component of its own.
//
// Every other way into the tree goes through the pointer, and the
// pointer cannot reach these. HitTest answers with a COMPONENT and
// nodeChain maps that back to the node that owns it, so a node with no
// component is not in any chain the pointer can produce — a <Menu>
// inside a <MenuBar> is drawn by the bar itself, out of data, and there
// is nothing under the cursor that belongs to it. mapNodes stops at the
// bar for the same reason and says so.
//
// Which made the vocabulary work of #429 only half a fix: declaring
// <MenuItem>'s attributes gives the property grid something to show, and
// without a descent there was still no gesture that would select one. So
// the two halves ship together.
//
// FIRST CHILD, not the last selection remembered per node. A designer
// that restores where you were is nicer to use and needs state that must
// then be invalidated by every edit, retype, delete and undo — the
// selectionScope comment above is the same argument, and it went the
// same way. Escape climbs back out, so the round trip costs one keypress
// either way whatever this picks.
//
// The keyboard's whole share of the drill, which is not a nicety: mouse
// reports cannot be injected through a recording pty, so a gesture with
// no key has no capture and no headless test. Why the gesture is
// alt+enter rather than enter is in wysiwyg.gooey, beside the binding:
// bare enter never reaches a root KeyBinding in this app, and the
// design pane is not in the focus order to scope one to.
func (ed *editor) selectChild() {
	n := ed.sel
	// Nothing selected is the same start ctrl+n uses — the user's root,
	// not the surface — so the two descents agree about where the
	// document begins.
	if n == nil || ed.isSurface(n) {
		n = ed.doc()
	}
	if len(n.Kids) == 0 {
		return
	}
	ed.setSelection(n.Kids[0])
}

// componentPath is the components from root down to w inclusive, or false
// when w is not below root. It walks the same gooey.Container seam the
// framework's own hit test walks (component.go:85) — a type assertion,
// not reflection.
func componentPath(root, w gooey.Component, acc []gooey.Component) ([]gooey.Component, bool) {
	acc = append(acc, root)
	if root == w {
		return acc, true
	}
	for _, k := range childComponents(root) {
		if p, ok := componentPath(k, w, acc); ok {
			return p, true
		}
	}
	return nil, false
}

// mapNodes records which design node each built component came from, at
// every depth — the inverse of markup.Build over one document.
//
// The correspondence is POSITIONAL, and the guard is the honest part:
// where the built children do not line up one-for-one with the document's
// children, the descent STOPS rather than pairing them off by index. An
// element that builds several components, a slot that became a child, a
// container that wraps — any of those would otherwise map a node onto a
// component it has nothing to do with, and a press would select a
// neighbour of what was clicked. Stopping means a hit further down
// resolves to the last node that was certainly right, which is the
// closest true answer available.
//
// Keyed on the COMPONENT and valued with the *node pointer: neither is a
// Name and neither is a position, so a node's identity here survives
// being renamed and survives its geometry living only in the editor.
func (ed *editor) mapNodes(n *node, comp gooey.Component) {
	// BOTH DIRECTIONS AT ONE POINT, so they cannot drift: see compOf.
	//
	// Neither write is guarded, and the compOf one used to be. That
	// guard could not fire — the two maps are made on consecutive lines
	// (main.go) and cleared together, and the only caller is the line
	// right after they are made — but worse, it could not have HELPED if
	// it had: the nodeOf write above it dies on a nil map first. A guard
	// that is reached only after the panic it would prevent is not a
	// guard, it is a claim that the invariant is optional. Found in
	// review of #425.
	ed.nodeOf[comp] = n
	ed.compOf[n] = comp
	kids := childComponents(comp)
	if len(kids) != len(n.Kids) || !ed.pairsAgree(n, kids) {
		return
	}
	for i, k := range n.Kids {
		ed.mapNodes(k, kids[i])
	}
}

// pairsAgree is the second half of the correspondence check, and it exists
// because MATCHING COUNTS ARE NOT A PAIRING. Some document nodes build no
// component of their own — <Tab> is the one in the box today, and it is
// how this was found: <Tabs> hands back one child per <Tab>, but that
// child is the tab's CONTENT, so pairing by index maps the <Tab> node onto
// the component its <Text> built and leaves the <Text> mapped to nothing.
// Counts agree perfectly throughout.
//
// Context.Named is what can settle it, because it holds what THIS build
// made of each name. Two directions, and the second is the one that caught
// <Tab>:
//
//   - a child that declares a Name must BE the component the loader
//     recorded for that name;
//   - a child that declares none must not be handed a component the loader
//     recorded for something BELOW it — that is proof the node contributed
//     nothing and the component belongs to the descendant.
//
// A node with no Name and no named descendants cannot be checked either
// way, and the count alone is NOT ENOUGH — which was not a hypothetical
// limit, it was a live wrong pairing. A <MenuBar> holding one <Menu>
// hands back exactly one child, its dropdown surface, so the counts
// agreed and the <Menu> node was mapped onto the popup. Two menus and
// the counts disagree and the descent stops; one menu and it does not.
// A correctness guard that depends on how many children the user
// happened to write is not one.
//
// So the catalog answers it first now. markup.ElementSpec.Pseudo is the
// declared form of "builds no component of its own" — the exact fact
// this check was trying to infer from names — and an element carrying
// it can never be paired with anything. That covers <Tab>, <Menu> and
// <MenuItem> by declaration rather than by luck, and the Named checks
// below stay for what they were always for: a node that DOES build
// something, but not the component sitting at its index.
//
// The descent stays conservative rather than clever either way: the
// cost of stopping early is a press resolving to an ancestor, and the
// cost of pairing wrongly is a press resolving to the wrong element
// entirely.
func (ed *editor) pairsAgree(n *node, kids []gooey.Component) bool {
	for i, k := range n.Kids {
		if !ed.pairAgrees(k, kids[i]) {
			return false
		}
	}
	return true
}

func (ed *editor) pairAgrees(k *node, comp gooey.Component) bool {
	// A PSEUDO-ELEMENT PAIRS WITH NOTHING. There is no component it
	// could be, so whatever sits at its index belongs to something else
	// — its parent's chrome, or a sibling's content.
	if spec, ok := ed.specOf(k.Elem); ok && spec.Pseudo {
		return false
	}
	if name := k.Attrs["Name"]; name != "" {
		if c := ed.docCtx.Named[name]; c != nil {
			return c == comp
		}
		return true
	}
	return !ed.namedBelow(k, comp)
}

// namedBelow reports whether comp is what the loader built for some NAMED
// node strictly below k.
func (ed *editor) namedBelow(k *node, comp gooey.Component) bool {
	for _, d := range k.Kids {
		if name := d.Attrs["Name"]; name != "" && ed.docCtx.Named[name] == comp {
			return true
		}
		if ed.namedBelow(d, comp) {
			return true
		}
	}
	return false
}

func childComponents(c gooey.Component) []gooey.Component {
	if p, ok := c.(gooey.Container); ok {
		return p.ChildComponents()
	}
	return nil
}

// setSelection moves the selection, and it deliberately does NOT rebuild.
//
// Selection is EDITOR state. The document is unchanged, so building the
// markup again and swapping the previewed tree would discard and re-mount
// every component in the designer — a full repaint of the one region the
// user is looking at — in order to show a different set of rows in a pane
// beside it. What genuinely follows the selection is what is DERIVED from
// it: the outline's marker, and AttrsFor(selection) in the properties
// grid. rev is what makes those recompute, because the document is plain
// Go state and a computed over it records no dependency (see the field's
// comment on editor).
//
// This is also the path the KEYBOARD takes — selectNext calls it — so the
// two gestures cost the same and cannot drift apart.
func (ed *editor) setSelection(n *node) {
	ed.sel = n
	ed.treeText.Set(ed.outline())
	ed.rev.Set(ed.rev.Get() + 1)
}
