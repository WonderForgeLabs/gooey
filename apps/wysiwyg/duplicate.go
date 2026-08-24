package main

import "fmt"

// Naming, and duplicating a configured subtree.
//
// These are one file because they are one problem. `Name` is what
// markup.Find resolves against, so two siblings sharing one makes the
// second unaddressable — and the document still BUILDS, so nothing
// reports it. Any operation that mints a name has to consult what is
// already in use rather than count anything.
//
// The palette did count: it named a new element `<Elem><len(Kids)+1>`.
// Delete from the middle and the length drops back, so the next add
// re-issues a name still on a sibling. Three adds, one delete, one add
// produced two <Text Name="Text3"> and a green status line.

// namesInUse folds every Name in the document into a set. The whole
// document, not the target's children: a name only has to be unique
// within the page for Find to resolve it, and the editor moves nodes
// between parents, so a name that was free in one container must not
// become a collision by being promoted into another.
//
// SLOTS ARE PART OF THE DOCUMENT. <ItemsView.ItemTemplate> and its
// subtree serialize into the same page and are resolved by the same
// markup.Find, so a Name in there is as taken as one in Kids. Walking
// only Kids made "unique per document" mean "unique per document,
// excluding slots" — a silent collision waiting for slot content to
// become selectable, which node.Slots' own comment says is expected.
func namesInUse(root *node) map[string]bool {
	used := map[string]bool{}
	var walk func(*node)
	walk = func(n *node) {
		if n == nil {
			return
		}
		if s := n.Attrs["Name"]; s != "" {
			used[s] = true
		}
		for _, k := range n.Kids {
			walk(k)
		}
		for _, sl := range n.Slots {
			walk(sl)
		}
	}
	walk(root)
	return used
}

// uniqueName returns the first `<base><n>` not already taken, counting
// from 1. Deriving the suffix from the SET rather than from a length is
// the entire fix: a set does not shrink when something is deleted from
// the middle of it.
func uniqueName(root *node, base string) string {
	used := namesInUse(root)
	for i := 1; ; i++ {
		cand := fmt.Sprintf("%s%d", base, i)
		if !used[cand] {
			return cand
		}
	}
}

// clone deep-copies a subtree, renaming every node that carries a Name
// so the copy collides with nothing. `used` accumulates across the walk
// because the copy's own descendants are minted during it and are not
// yet in the document.
func clone(n *node, used map[string]bool) *node {
	c := &node{Elem: n.Elem, Body: n.Body, Attrs: map[string]string{}}
	for k, v := range n.Attrs {
		c.Attrs[k] = v
	}
	if base := n.Elem; n.Attrs["Name"] != "" {
		for i := 1; ; i++ {
			cand := fmt.Sprintf("%s%d", base, i)
			if !used[cand] {
				c.Attrs["Name"] = cand
				used[cand] = true
				break
			}
		}
	}
	for _, k := range n.Kids {
		c.Kids = append(c.Kids, clone(k, used))
	}
	// Slots come with it. They are structured attributes, and the
	// catalog can mark one REQUIRED — <ItemsView> seeds an ItemTemplate
	// on the way in — so a copy that dropped them serialized as
	// <ItemsView/> and produced a document that no longer satisfies the
	// element's own contract. That is a duplicate that silently deletes
	// half of what it copied.
	for k, sl := range n.Slots {
		if c.Slots == nil {
			c.Slots = map[string]*node{}
		}
		c.Slots[k] = clone(sl, used)
	}
	return c
}

// duplicateSelected inserts a deep copy immediately after the original
// and selects it.
//
// Immediately after, not appended: the user is looking at the thing they
// copied, and a copy that lands at the bottom of a long container reads
// as nothing having happened.
//
// Selecting the COPY is the other half. The point of duplicating is to
// then change the copy, and leaving the selection on the original means
// the next edit silently modifies the wrong one — which looks exactly
// like the duplicate having failed.
func (ed *editor) duplicateSelected() bool {
	p := ed.movable()
	if p == nil {
		return false
	}
	i := indexIn(p, ed.sel)
	if i < 0 {
		return false
	}
	c := clone(ed.sel, namesInUse(ed.root))
	insertAt(p, i+1, c)
	ed.sel = c
	ed.rebuild()
	return true
}
