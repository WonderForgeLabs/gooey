package mcp

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"strings"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/markup"
)

// patch_markup: targeted subtree replacement, addressed by Name.
//
// The addressing rule is that THE NAME IS THE ADDRESS, and the address
// must survive the patch: the fragment's root element is required to
// carry the same Name= as the element it replaces, so an agent iterating
// on one panel patches the same name every round instead of chasing
// whatever it renamed the panel to last time.
//
// The layout rule: attached and framework layout attributes (Grid.Row,
// Width, Margin, ...) the fragment does NOT restate are preserved from
// the old element. A fragment describes a panel's content; its position
// in the parent's grid is the parent's business, and forcing every
// fragment to re-declare its cell would make "regenerate this panel" a
// trap. Restating any layout attribute takes it over, per attribute.
//
// The ceiling: the target must be a direct child of a container this
// package can rewrite — the builtin containers whose child sets are
// public slices (VStack, HStack, Grid, Canvas, ButtonBar) or slots
// (Border) — or the composition root itself, which degrades to a whole
// swap. That is the same deliberate type-switch ceiling tree_snapshot
// has: a third-party container's children cannot be rewritten without
// reflection.
//
// Atomicity: the fragment is built into scratch name/declared tables and
// every rule is checked BEFORE the first mutation; any failure restores
// the tables and returns, with the running tree never touched. The
// commit itself is one slot write plus Composer.InvalidateStructure,
// which re-syncs paint nodes and the input tree at the next frame while
// keeping every surviving component's node — clean/dirty state, focus
// and all — which is what makes a patch cost the patched subtree and not
// the page.
func (s *Server) patchMarkup(a args) (any, error) {
	if s.bind == nil {
		return nil, errNoContext
	}
	c, err := s.composer()
	if err != nil {
		return nil, err
	}
	name, err := a.str("name")
	if err != nil {
		return nil, err
	}
	src, err := a.str("source")
	if err != nil {
		return nil, err
	}
	old, ok := s.bind.Named[name]
	if !ok {
		return nil, fmt.Errorf("no element named %q; tree_snapshot lists the named elements", name)
	}

	// Locate the target's slot before building anything: an unpatchable
	// address should not cost a build.
	root := c.Root()
	var put func(gooey.Component)
	if old != root {
		parent, index := findParent(root, old)
		if parent == nil {
			return nil, fmt.Errorf("element %q is not a visual child in the live tree; patch_markup replaces elements a container lays out", name)
		}
		put = childSlot(parent, index)
		if put == nil {
			return nil, fmt.Errorf("element %q sits inside a %T, which patch_markup cannot rewrite; supported parents are VStack, HStack, Grid, Canvas, ButtonBar and Border", name, parent)
		}
	}

	fresh, restore, err := s.scratchBuild(src)
	if err != nil {
		restore()
		return nil, err
	}
	newNamed, newDecl := s.bind.Named, s.bind.Declared

	frag, err := fragmentRoot([]byte(src))
	if err != nil {
		restore()
		return nil, err
	}
	if frag.name != name {
		restore()
		if frag.name == "" {
			return nil, fmt.Errorf("the fragment's root element must carry Name=%q — the name is the patch address, and it has to survive the patch", name)
		}
		return nil, fmt.Errorf("the fragment's root element is Name=%q, but the patch address is %q; the name is the address and must survive the patch", frag.name, name)
	}

	// Merge the name tables: everything outside the departing subtree
	// keeps its name, the fragment's names join, and a fragment name that
	// collides with a surviving element is refused before anything moves.
	departed := map[gooey.Component]bool{}
	collectSubtree(old, departed)
	prevNamed := map[string]gooey.Component{}
	restore() // context back to the running page while we merge
	for n, w := range s.bind.Named {
		if !departed[w] {
			prevNamed[n] = w
		}
	}
	for n, w := range newNamed {
		if ex, clash := prevNamed[n]; clash && ex != w {
			return nil, fmt.Errorf("the fragment names %q, which already names an element outside the patched subtree", n)
		}
		prevNamed[n] = w
	}

	preserveLayout(old, fresh, frag.attrs)

	// Commit. No failure paths below this line.
	s.bind.Named = prevNamed
	if len(newDecl) > 0 && s.bind.Declared == nil {
		s.bind.Declared = map[gooey.Component]markup.DeclaredSurface{}
	}
	for w := range departed {
		delete(s.bind.Declared, w)
	}
	for w, ds := range newDecl {
		s.bind.Declared[w] = ds
	}
	if old == root {
		s.host.Swap(fresh)
	} else {
		put(fresh)
		c.InvalidateStructure()
	}
	return map[string]any{"patched": name, "named": namesOf(s.bind.Named)}, nil
}

// findParent walks the visual tree for the container that lays out
// target, returning it and target's index in its child list. Components
// are pointers, so identity comparison is the search.
func findParent(w gooey.Component, target gooey.Component) (gooey.Component, int) {
	ct, ok := w.(gooey.Container)
	if !ok {
		return nil, 0
	}
	for i, ch := range ct.ChildComponents() {
		if ch == nil {
			continue
		}
		if ch == target {
			return w, i
		}
		if p, idx := findParent(ch, target); p != nil {
			return p, idx
		}
	}
	return nil, 0
}

// childSlot returns a setter for the i-th child of a container this
// package knows how to rewrite, or nil. The type switch is the point:
// these containers' child sets are public fields, so writing the slot is
// ordinary Go — no reflection, and the same ceiling as componentProps.
// For each of them ChildComponents returns the field itself, so the walk
// index and the slice index agree by construction.
func childSlot(parent gooey.Component, i int) func(gooey.Component) {
	switch p := parent.(type) {
	case *components.VStack:
		return func(w gooey.Component) { p.Children[i] = w }
	case *components.HStack:
		return func(w gooey.Component) { p.Children[i] = w }
	case *components.Grid:
		return func(w gooey.Component) { p.Children[i] = w }
	case *components.Canvas:
		return func(w gooey.Component) { p.Children[i] = w }
	case *components.ButtonBar:
		return func(w gooey.Component) { p.Children[i] = w }
	case *components.Border:
		return func(w gooey.Component) { p.Child = w }
	}
	return nil
}

// collectSubtree marks target and everything reachable from it —
// children and non-visual attachments both, because a departing
// subtree's names and declared surfaces all leave together.
func collectSubtree(w gooey.Component, out map[gooey.Component]bool) {
	if w == nil || out[w] {
		return
	}
	out[w] = true
	if a, ok := w.(gooey.Attacher); ok {
		for _, at := range a.Attachments() {
			collectSubtree(at, out)
		}
	}
	if ct, ok := w.(gooey.Container); ok {
		for _, ch := range ct.ChildComponents() {
			collectSubtree(ch, out)
		}
	}
}

// layoutAttrs is every attribute applyLayout reads — the framework
// element surface plus the attached properties.
var layoutAttrs = []string{
	"Width", "Height", "Margin", "HAlign", "VAlign", "Visibility",
	"Grid.Row", "Grid.Col", "Grid.RowSpan", "Grid.ColSpan",
	"Canvas.Left", "Canvas.Top",
}

// preserveLayout carries the old element's layout onto the replacement
// for every layout attribute the fragment did not restate. Restated
// attributes were already applied by the build and are left alone.
func preserveLayout(old, fresh gooey.Component, restated map[string]bool) {
	ol, nl := gooey.LayoutOf(old), gooey.LayoutOf(fresh)
	if ol == nil || nl == nil {
		return
	}
	if !restated["Width"] {
		nl.Width = ol.Width
	}
	if !restated["Height"] {
		nl.Height = ol.Height
	}
	if !restated["Margin"] {
		nl.Margin = ol.Margin
	}
	if !restated["HAlign"] {
		nl.HAlign = ol.HAlign
	}
	if !restated["VAlign"] {
		nl.VAlign = ol.VAlign
	}
	if !restated["Visibility"] {
		nl.Visibility = ol.Visibility
	}
	if !restated["Grid.Row"] {
		nl.Row = ol.Row
	}
	if !restated["Grid.Col"] {
		nl.Col = ol.Col
	}
	if !restated["Grid.RowSpan"] {
		nl.RowSpan = ol.RowSpan
	}
	if !restated["Grid.ColSpan"] {
		nl.ColSpan = ol.ColSpan
	}
	if !restated["Canvas.Left"] {
		nl.Left = ol.Left
	}
	if !restated["Canvas.Top"] {
		nl.Top = ol.Top
	}
}

// fragInfo is what the patch needs to know about the fragment's root
// element beyond what the build already checked: its Name= and which
// attributes it spelled out (presence, not values — "restated" is a
// syntactic fact).
type fragInfo struct {
	name  string
	attrs map[string]bool
}

// fragmentRoot scans the source for the single child element of <Gooey>
// and reports its attributes. It runs after markup.Build has accepted
// the same bytes, so the document shape (one root, one child) is already
// established; this is presence extraction, not a second validator.
func fragmentRoot(src []byte) (fragInfo, error) {
	dec := xml.NewDecoder(bytes.NewReader(src))
	depth := 0
	for {
		tok, err := dec.Token()
		if err != nil {
			return fragInfo{}, fmt.Errorf("markup: %v", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			if depth < 2 {
				continue // the <Gooey> envelope
			}
			info := fragInfo{attrs: map[string]bool{}}
			for _, at := range t.Attr {
				if at.Name.Space == "xmlns" || (at.Name.Space == "" && at.Name.Local == "xmlns") {
					continue
				}
				key := at.Name.Local
				if at.Name.Space != "" && !strings.Contains(key, ".") {
					// encoding/xml resolves a dotted attribute prefix
					// (Grid.Row) as a namespace; put the spelling back.
					key = at.Name.Space + "." + at.Name.Local
				}
				info.attrs[key] = true
				if key == "Name" {
					info.name = at.Value
				}
			}
			return info, nil
		}
	}
}
