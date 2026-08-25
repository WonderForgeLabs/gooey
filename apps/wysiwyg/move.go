package main

import "strings"

// Moving a node that already exists.
//
// The editor could add and delete, and nothing in between: repositioning
// a child meant deleting it and adding it again, which discards its
// attributes and its whole subtree. The attributes are the work, so that
// was not a workaround — it was a wall.
//
// All three operations RELINK the existing *node rather than copying it.
// That is what keeps the drag state, the inspector's selection and the
// outline's mark pointing at the same object across a move, and it is
// why every one of these returns the node to ed.sel unchanged.
//
// Each returns whether it changed anything, and a refusal must not
// rebuild: `rebuild` re-serialises and re-mounts the designer subtree, so
// a key held down at the top of a list would repaint the editor once per
// repeat for no change at all.

// indexIn returns n's position among p's children, or -1.
func indexIn(p, n *node) int {
	for i, k := range p.Kids {
		if k == n {
			return i
		}
	}
	return -1
}

// unlink removes n from p, leaving the node itself intact.
func unlink(p, n *node) int {
	i := indexIn(p, n)
	if i < 0 {
		return -1
	}
	p.Kids = append(p.Kids[:i], p.Kids[i+1:]...)
	return i
}

// insertAt splices n into p at i, clamped to the ends.
func insertAt(p *node, i int, n *node) {
	if i < 0 {
		i = 0
	}
	if i > len(p.Kids) {
		i = len(p.Kids)
	}
	p.Kids = append(p.Kids, nil)
	copy(p.Kids[i+1:], p.Kids[i:])
	p.Kids[i] = n
}

// movable reports the selected node's parent when the node is allowed to
// move at all.
//
// The user's ROOT is not: its only sibling would be the surface, and
// doc() returns root.Kids[0] unconditionally, so a second child there is
// not a misrender — it is a different document from the one that saves.
// This is the same rule deleteSelected enforces, for the same reason.
//
// The isSurface check is DEFENCE IN DEPTH and is not what refuses today.
// A mutation deleting it kills no test, because the surface holds exactly
// one child and each caller independently declines on that fact: the
// reorder's target index falls outside a one-element slice, promote finds
// no grandparent above the surface, and demote finds no preceding
// sibling at index 0. Kept because it states the invariant where a reader
// looks for it, and because all three of those accidents stop holding the
// day the surface gains a second child — but a test asserting "the root
// did not move" pins the OUTCOME, never this line.
func (ed *editor) movable() *node {
	if ed.sel == nil {
		return nil
	}
	p := ed.parentOf(ed.sel)
	if p == nil || ed.isSurface(p) {
		return nil
	}
	return p
}

// moveSelected reorders the selection among its siblings by d.
func (ed *editor) moveSelected(d int) bool {
	p := ed.movable()
	if p == nil {
		return false
	}
	i := indexIn(p, ed.sel)
	j := i + d
	if i < 0 || j < 0 || j >= len(p.Kids) {
		return false
	}
	p.Kids[i], p.Kids[j] = p.Kids[j], p.Kids[i]
	ed.rebuild()
	return true
}

// promoteSelected lifts the selection out to its grandparent, landing
// immediately AFTER its former parent.
//
// The index is a decision, not an implementation detail. Appending to
// the grandparent would also unnest the node and still build, but it
// teleports the element to the bottom of a list the user is looking at.
func (ed *editor) promoteSelected() bool {
	p := ed.movable()
	if p == nil {
		return false
	}
	g := ed.parentOf(p)
	// A grandparent that is the surface means p is the user's root, so
	// promoting would create a second root. Refused for the same reason
	// the root itself cannot move.
	if g == nil || ed.isSurface(g) {
		return false
	}
	n := ed.sel
	if unlink(p, n) < 0 {
		return false
	}
	insertAt(g, indexIn(g, p)+1, n)
	ed.rebuild()
	return true
}

// demoteSelected nests the selection into its PRECEDING sibling.
//
// The preceding one specifically: falling back to the following sibling
// when there is no preceding one would move the element in the direction
// the user did not ask for.
//
// The gate is load-bearing rather than defensive. A leaf silently
// DISCARDS children — the markup still saves and still builds — so
// nesting into a <Text> would make the node disappear from the document
// with nothing anywhere to report it.
//
// canHold, NOT holdsChildren, and the difference is a bug this used to
// have: holdsChildren never consults ChildSpec.Only, so it said yes to
// demoting anything into a <Tabs>, which takes only <Tab>.
func (ed *editor) demoteSelected() bool {
	p := ed.movable()
	if p == nil {
		return false
	}
	i := indexIn(p, ed.sel)
	if i <= 0 {
		return false
	}
	host := p.Kids[i-1]
	if !ed.canHold(host.Elem, ed.sel.Elem) {
		return false
	}
	n := ed.sel
	// TRANSACTIONAL, for the same reason addSelected is (addplan.go). The
	// gate above is necessary and not sufficient: canHold is deliberately
	// permissive where the catalog cannot answer — ModeOne cannot know its
	// slot is already taken, ModeUnknown knows nothing — so the build is
	// the real decision, and a demote that breaks the document has to put
	// it back.
	//
	// This used to gate on holdsChildren and then `return true`
	// unconditionally: no docRoot check, no revert, no message. Demoting
	// into a <Tabs> wrote a direct illegal child and left docRoot nil,
	// which kills click-to-select for the WHOLE document while the last
	// good tree stays on screen looking pressable — the exact failure
	// addSelected was made transactional to stop, reachable by a different
	// gesture. Found in review of the PR that fixed the other half.
	at := unlink(p, n)
	host.Kids = append(host.Kids, n)
	ed.rebuild()
	if ed.remote == nil && ed.docRoot == nil {
		refused := strings.TrimPrefix(ed.status.Get(), "✗ ")
		host.Kids = host.Kids[:len(host.Kids)-1]
		insertAt(p, at, n)
		ed.rebuild()
		ed.status.Set("✗ <" + n.Elem + "> does not go inside <" + host.Elem +
			">: " + refused)
		return false
	}
	return true
}
