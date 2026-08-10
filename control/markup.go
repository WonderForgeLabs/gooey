package control

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"sort"
	"strings"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/render"
)

// StyleEntry is one named entry in the markup context's style table —
// what a Style="name" attribute can resolve. Colors carry render.Color's
// Set flag, so an unset attribute stays distinguishable from black.
type StyleEntry struct {
	Name  string
	Style render.Style
}

// Styles reports the style table, sorted by name. An unknown style name
// in markup silently renders unstyled, so a markup generator must draw
// from this list.
func (s *Service) Styles() ([]StyleEntry, error) {
	if s.bind == nil {
		return nil, errNoContext
	}
	names := make([]string, 0, len(s.bind.Styles))
	for n := range s.bind.Styles {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]StyleEntry, 0, len(names))
	for _, n := range names {
		out = append(out, StyleEntry{Name: n, Style: s.bind.Styles[n]})
	}
	return out, nil
}

// ---- schema (issue #62) ----

// PropertyDecl is one <x:Property> as declared: the wire form carries
// the declared DEFAULT, where a snapshot's DeclaredValue carries an
// instance's current value.
type PropertyDecl struct {
	Name string
	Type Kind
	// DefaultLiteral is the default as the markup literal it was
	// declared with; empty means the type's zero value.
	DefaultLiteral string
	Required       bool
}

// Schema is a document's declared surface: what a values payload must
// satisfy before the document can build.
type Schema struct {
	// Control is the document's identity where known — empty for an
	// anonymous source document.
	Control string
	Props   []PropertyDecl
}

// DeclaredSchema returns a document's <x:Property> declaration block as
// a schema. Empty source asks about the running page's document, which
// works only when the host supplied one (Service.Doc).
func (s *Service) DeclaredSchema(source string) (*Schema, error) {
	sc := &Schema{}
	src := []byte(source)
	if strings.TrimSpace(source) == "" {
		if s.Doc == nil {
			return nil, preconditionf("the server does not know the running page's source; pass the markup to inspect explicitly")
		}
		src = s.Doc()
	}
	decls, err := markup.Declarations(src)
	if err != nil {
		return nil, invalidf("%v", err)
	}
	for _, d := range decls {
		sc.Props = append(sc.Props, PropertyDecl{
			Name:           d.Name,
			Type:           KindOf(d.Type),
			DefaultLiteral: d.Default,
			Required:       d.Required,
		})
	}
	return sc, nil
}

// ---- whole-page swap ----

// SwapMarkup replaces the page with new markup built against the app's
// existing binding context — so the viewmodel, and therefore the app's
// state, survives the swap. regs, when present, grow the viewmodel
// FIRST (issue #89), so the new page may bind names the app never
// pre-registered. Atomic: a failed build leaves the running tree, the
// name table AND the registrations untouched. Returns the new tree's
// Name= identities.
func (s *Service) SwapMarkup(source string, regs []Registration) ([]string, error) {
	if s.bind == nil {
		return nil, errNoContext
	}
	unregister, err := s.register(regs)
	if err != nil {
		unregister()
		return nil, err
	}
	// The name table and the declared-surface registry are rebuilt by the
	// load, so a FAILED load must not be allowed to leave them
	// half-written: the running tree is still on screen and SetFocus and
	// SnapshotTree still name its elements. Build into fresh maps and
	// commit only on success.
	root, restore, err := s.scratchBuild(source)
	if err != nil {
		restore()
		unregister()
		return nil, invalidf("%v", err)
	}
	s.host.Swap(root)
	return namesOf(s.bind.Named), nil
}

// Validate checks markup without touching the app: the exact
// parse-and-bind path SwapMarkup runs — against the live context,
// including declared properties — but nothing is attached and no frame
// is composed, so the check is invisible to the running app.
//
// An INVALID document is a normal result, not an error: the caller
// asked whether the markup is valid and this is the answer. The
// returned text is the same typed load error SwapMarkup would report.
func (s *Service) Validate(source string) (valid bool, loadErr string, named []string, err error) {
	if s.bind == nil {
		return false, "", nil, errNoContext
	}
	_, restore, berr := s.scratchBuild(source)
	named = namesOf(s.bind.Named)
	restore()
	if berr != nil {
		return false, berr.Error(), nil, nil
	}
	return true, "", named, nil
}

// scratchBuild builds markup source against the binding context with
// the Named table and Declared registry swapped for fresh ones. restore
// puts the previous maps back — call it on failure (or, for a
// validation, on every path); on success the fresh maps stay committed
// and restore must not be called.
func (s *Service) scratchBuild(src string) (root gooey.Component, restore func(), err error) {
	prevNamed, prevDecl := s.bind.Named, s.bind.Declared
	s.bind.Named = map[string]gooey.Component{}
	s.bind.Declared = nil
	restore = func() { s.bind.Named, s.bind.Declared = prevNamed, prevDecl }
	root, err = markup.Build([]byte(src), s.bind)
	return root, restore, err
}

// ---- targeted subtree replacement (issue #117) ----

// PatchMarkup replaces ONE named element's subtree with new markup,
// leaving the rest of the page — and every sibling's state — untouched.
//
// The addressing rule is that THE NAME IS THE ADDRESS, and the address
// must survive the patch: the fragment's root element is required to
// carry the same Name= as the element it replaces, so an agent
// iterating on one panel patches the same name every round.
//
// The layout rule: attached and framework layout attributes (Grid.Row,
// Width, Margin, ...) the fragment does NOT restate are preserved from
// the old element — a fragment describes a panel's content; its
// position in the parent's grid is the parent's business. Restating any
// layout attribute takes it over, per attribute.
//
// The ceiling: the target must be a direct child of a container this
// package can rewrite — the builtin containers whose child sets are
// public slices (VStack, HStack, Grid, Canvas, ButtonBar) or slots
// (Border) — or the composition root itself, which degrades to a whole
// swap. The same deliberate type-switch ceiling the snapshot has.
//
// Atomicity: the fragment is built into scratch tables and every rule
// is checked BEFORE the first mutation; any failure restores the tables
// and returns with the running tree never touched. The commit is one
// slot write plus Composer.InvalidateStructure, which re-syncs paint
// nodes and the input tree at the next frame while keeping every
// surviving component's node — clean/dirty state, focus and all.
func (s *Service) PatchMarkup(name, source string) ([]string, error) {
	if s.bind == nil {
		return nil, errNoContext
	}
	c, err := s.composer()
	if err != nil {
		return nil, err
	}
	old, ok := s.bind.Named[name]
	if !ok {
		return nil, notFoundf("no element named %q; SnapshotTree lists the named elements", name)
	}

	// Locate the target's slot before building anything: an unpatchable
	// address should not cost a build.
	root := c.Root()
	var put func(gooey.Component)
	if old != root {
		parent, index := findParent(root, old)
		if parent == nil {
			return nil, invalidf("element %q is not a visual child in the live tree; PatchMarkup replaces elements a container lays out", name)
		}
		put = childSlot(parent, index)
		if put == nil {
			return nil, invalidf("element %q sits inside a %T, which PatchMarkup cannot rewrite; supported parents are VStack, HStack, Grid, Canvas, ButtonBar and Border", name, parent)
		}
	}

	fresh, restore, err := s.scratchBuild(source)
	if err != nil {
		restore()
		return nil, invalidf("%v", err)
	}
	newNamed, newDecl := s.bind.Named, s.bind.Declared

	frag, err := fragmentRoot([]byte(source))
	if err != nil {
		restore()
		return nil, invalidf("%v", err)
	}
	if frag.name != name {
		restore()
		if frag.name == "" {
			return nil, invalidf("the fragment's root element must carry Name=%q — the name is the patch address, and it has to survive the patch", name)
		}
		return nil, invalidf("the fragment's root element is Name=%q, but the patch address is %q; the name is the address and must survive the patch", frag.name, name)
	}

	// Merge the name tables: everything outside the departing subtree
	// keeps its name, the fragment's names join, and a fragment name
	// that collides with a surviving element is refused before anything
	// moves.
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
			return nil, invalidf("the fragment names %q, which already names an element outside the patched subtree", n)
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
	return namesOf(s.bind.Named), nil
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
// these containers' child sets are public fields, so writing the slot
// is ordinary Go — no reflection. For each of them ChildComponents
// returns the field itself, so the walk index and the slice index agree
// by construction.
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
		// A bound Visibility survives the patch the way the value does:
		// the fresh element adopts the old element's source (which also
		// syncs the field); the Composer's re-sync arms its observer.
		if src := ol.VisibilitySource(); src != nil {
			nl.BindVisibilityFunc(src)
		} else {
			nl.Visibility = ol.Visibility
		}
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
// the same bytes, so the document shape is already established; this is
// presence extraction, not a second validator.
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
