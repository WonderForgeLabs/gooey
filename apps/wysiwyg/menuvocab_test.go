package main

import (
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/markup"
)

// The designer's half of #429 — "why don't I see the child properties to
// set content for menu item?"
//
// The report reads like one missing panel and is two independent holes,
// which is why fixing either alone leaves the same screen. The
// vocabulary did not declare <Menu> or <MenuItem> at all, so there was
// nothing for a property grid to show; and NOTHING COULD SELECT ONE
// EITHER, because selection runs through the pointer and these elements
// put no component under it. A grid with rows nobody can reach and a
// selection that lands on an element with no rows look identical from
// the outside: an empty inspector.
//
// So this file pins both ends and the join. menuFixture is shared
// because the fixture is the point — a <MenuBar> is the only element in
// the box today whose children are DATA.

// menuFixture is a document holding one populated <MenuBar>.
//
// Canvas offsets because the design surface is a <Canvas>: without them
// every node stacks at the origin, which does not affect selection but
// makes any geometry assertion a coincidence.
func menuFixture(t *testing.T) (ed *editor, root gooey.Component, bar, menu, item *node) {
	t.Helper()
	ed, root = buildPage(t)
	item = &node{Elem: "MenuItem", Attrs: map[string]string{"Text": "Open"}}
	menu = &node{Elem: "Menu", Attrs: map[string]string{"Title": "File"}, Kids: []*node{item}}
	bar = &node{
		Elem:  "MenuBar",
		Attrs: map[string]string{"Name": "Bar1", "Canvas.Left": "0", "Canvas.Top": "0"},
		Kids:  []*node{menu},
	}
	ed.doc().Kids = append(ed.doc().Kids, bar)
	ed.rebuild()
	if ed.docRoot == nil {
		t.Fatalf("the <MenuBar> fixture does not build: %q", ed.status.Get())
	}
	return ed, root, bar, menu, item
}

// TestTheMenuVocabularyIsNotOfferedInThePalette is the half a name list
// could not keep.
//
// The filter was `e.NonVisual || e.Name == "Tab"` — correct when it was
// written, and wrong the moment a second nested element existed, in the
// way a hardcoded name always goes wrong: not with an error, but by
// quietly offering a <Menu> whose insertion produces markup the loader
// refuses. Nothing anywhere would have gone red.
//
// The assertion is therefore about the DERIVATION, not about three
// names: every element some other entry restricts itself to is absent,
// and the container that restricts them is present. A fourth nested
// element added tomorrow is covered by the same loop.
func TestTheMenuVocabularyIsNotOfferedInThePalette(t *testing.T) {
	ed, _ := buildPage(t)
	offered := map[string]bool{}
	for _, e := range ed.palette {
		offered[e.Name] = true
	}
	// Derived from the catalog rather than listed here, so this test
	// cannot be the second copy of the fact it is checking.
	restricted := map[string]string{}
	hosts := map[string]markup.ElementSpec{}
	for _, e := range ed.docCtx.Catalog() {
		if e.Children.Mode != markup.ModeRestricted {
			continue
		}
		hosts[e.Name] = e
		for _, only := range e.Children.Only {
			restricted[only] = e.Name
		}
	}
	if len(restricted) == 0 {
		t.Fatal("no element in the catalog restricts its children: the palette filter has nothing to exclude and this test proves nothing")
	}
	for name, parent := range restricted {
		if offered[name] {
			t.Errorf("the palette offers <%s>, which is legal only inside <%s>: adding it produces markup that will not load", name, parent)
		}
	}
	// The other direction, so the filter cannot pass by emptying the
	// palette: the containers themselves stay on offer.
	//
	// Unless the container is excluded on its OWN account, and both
	// exclusions are real rather than defensive. <Companion> restricts
	// its children to <Arg> and <Var> and is non-visual, so it belongs
	// to the attachment gesture rather than to "add a child". <Menu>
	// restricts its children to <MenuItem> and is itself restricted to
	// <MenuBar> — a container and a nested element at once, which is
	// exactly why "may this be placed on its own" had to become a
	// question about the element rather than about its role.
	checked := 0
	for _, parent := range restricted {
		h := hosts[parent]
		if h.NonVisual || h.Nested {
			continue
		}
		checked++
		if !offered[parent] {
			t.Errorf("the palette does not offer <%s>, the container the nested elements belong in", parent)
		}
	}
	if checked == 0 {
		t.Error("every restricted container is itself excluded: the second half of this test asserted nothing")
	}
}

// TestThePointerCannotReachAMenuItem is why the keyboard route exists,
// and it is stated as a test rather than as a comment because it is the
// premise the next two rest on. If a press ever COULD select a
// <MenuItem>, selectChild would be a convenience instead of the only
// way in, and this failing is how someone finds that out.
//
// The mechanism: buildMenuBar consumes <Menu> and <MenuItem> as data —
// they never enter the visual tree — so mapNodes finds the bar's built
// children do not correspond to its document children and stops. Every
// component inside the bar's rect maps to the bar.
func TestThePointerCannotReachAMenuItem(t *testing.T) {
	ed, _, bar, menu, item := menuFixture(t)
	if ed.compOf[menu] != nil {
		t.Error("a <Menu> now has a component of its own; selectChild is no longer the only route and its comment is stale")
	}
	if ed.compOf[item] != nil {
		t.Error("a <MenuItem> now has a component of its own; selectChild is no longer the only route and its comment is stale")
	}
	// The bar itself IS mapped — otherwise the two checks above would
	// pass for a fixture that simply failed to build.
	if ed.compOf[bar] == nil {
		t.Fatal("the <MenuBar> has no component either: the fixture did not build and nothing above was tested")
	}
}

// TestEnterDescendsIntoTheMenuVocabulary is the join.
//
// Driven through the page's own KeyBinding rather than by calling
// ed.selectChild, for the reason pressEsc gives: a binding declared on
// the wrong element never fires, and a direct call passes for a page
// that has no binding at all. That is not hypothetical here — this went
// red twice, once on bare enter (eaten by the toolbox) and once on a
// binding scoped to the design pane (which is not in the focus order at
// all), and both would have passed against ed.selectChild.
func TestEnterDescendsIntoTheMenuVocabulary(t *testing.T) {
	ed, root, bar, menu, item := menuFixture(t)
	c := gooey.NewComposer(root, 160, 48)
	t.Cleanup(c.Close)
	c.Frame()
	ed.setSelection(bar)
	if !pressEnter(c) {
		t.Fatal("enter was not handled: the page has no SelectChild binding")
	}
	if ed.sel != menu {
		t.Fatalf("enter on the <MenuBar> selected %v, want the <Menu>", elemOf(ed.sel))
	}
	pressEnter(c)
	if ed.sel != item {
		t.Fatalf("enter on the <Menu> selected %v, want the <MenuItem>", elemOf(ed.sel))
	}
	// A leaf is a no-op, not a clear: enter at the bottom must not
	// deselect, or the gesture means two things depending on depth.
	pressEnter(c)
	if ed.sel != item {
		t.Fatalf("enter on a leaf moved the selection to %v", elemOf(ed.sel))
	}
	// And the round trip, because a descent nobody can climb out of is
	// worse than none.
	pressEsc(c)
	if ed.sel != menu {
		t.Fatalf("esc from the <MenuItem> selected %v, want the <Menu>", elemOf(ed.sel))
	}
	pressEsc(c)
	if ed.sel != bar {
		t.Fatalf("esc from the <Menu> selected %v, want the <MenuBar>", elemOf(ed.sel))
	}
}

// TestASelectedMenuItemOffersItsAttributes is the reported symptom,
// asserted at the grid rather than at the catalog.
//
// Reading markup.AttrsFor directly would pass on the state this change
// found: the vocabulary declared the attributes and ed.target() resolved
// the selection in ed.palette, which no longer contains a <MenuItem> —
// so the catalog knew and the grid still showed nothing.
func TestASelectedMenuItemOffersItsAttributes(t *testing.T) {
	ed, _, _, _, item := menuFixture(t)
	ed.setSelection(item)
	got := map[string]bool{}
	for _, r := range ed.attrRows() {
		got[r.name] = true
	}
	// Every attribute buildMenuBar reads off a <MenuItem>. Text is the
	// one the issue asked for; the rest are here because a grid that
	// offers the label and hides the accelerator is the same defect one
	// row narrower.
	for _, want := range []string{"Text", "Gesture", "Checked", "Command", "Separator"} {
		if !got[want] {
			t.Errorf("a selected <MenuItem> has no %q row: %v", want, got)
		}
	}
}

// TestASelectedMenuOffersItsTitle is the sibling case, and it is not
// redundant: <Menu> and <MenuItem> are separate declarations read by one
// Build, which is exactly the arrangement catalogen cannot attribute
// reads across (see checkPseudo). Its guard catches an attribute NOBODY
// reads; this catches Title going to the wrong declaration of the two.
func TestASelectedMenuOffersItsTitle(t *testing.T) {
	ed, _, _, menu, _ := menuFixture(t)
	ed.setSelection(menu)
	rows := ed.attrRows()
	for _, r := range rows {
		if r.name == "Title" {
			if r.value != "File" {
				t.Errorf("the Title row shows %q, want the document's %q", r.value, "File")
			}
			return
		}
	}
	t.Errorf("a selected <Menu> has no Title row: %v", rows)
}

// pressEnter is the SelectChild gesture, through the page's binding.
//
// ALT is not decoration. Bare enter never reaches a root KeyBinding in
// this app — the toolbox, the inspector and every caret editor claim it
// first — so a test that pressed enter would be testing whichever of
// those happened to hold focus. The markup carries the full reasoning.
func pressEnter(c *gooey.Composer) bool {
	return c.Handle(input.KeyOf(input.KeyEvent{Key: input.KeyEnter, Mods: input.ModAlt}))
}

// elemOf names a node for a failure message, nil included.
func elemOf(n *node) string {
	if n == nil {
		return "nothing"
	}
	return "<" + n.Elem + ">"
}

// TestARebuildDoesNotRebuildTheCatalogPerNode is the pin on a cost that
// is invisible in every other assertion here.
//
// pairAgrees answers "is this element pseudo?" once per document node,
// and the obvious way to answer it — ed.specOf — ranges over
// ed.docCtx.Catalog(), which is not a getter: it re-derives every
// builtin spec with fresh Attrs copies, re-runs markNested and sorts.
// Measured at ~87 allocations per call. mapNodes runs from rebuild(),
// which fires on every drag frame, every move and every property edit,
// so a per-node catalog read is O(nodes) of that on the inner loop —
// and NOTHING ELSE WOULD NOTICE. Every correctness test here passes
// either way; a rebuild simply gets slower and allocates megabytes.
//
// So the assertion is allocations, and the ceiling is set where the two
// implementations cannot both fit: the document below has enough nodes
// that a per-node catalog read costs thousands of allocations more than
// the whole rest of the rebuild. It is deliberately loose — this is a
// guard against a regression of a known shape, not a budget for
// rebuild() to be held to.
func TestARebuildDoesNotRebuildTheCatalogPerNode(t *testing.T) {
	ed, _ := buildPage(t)
	const nodes = 60
	for i := 0; i < nodes; i++ {
		ed.doc().Kids = append(ed.doc().Kids, &node{
			Elem:  "VStack",
			Attrs: map[string]string{"Canvas.Left": "0", "Canvas.Top": "0"},
			Kids:  []*node{{Elem: "Text", Body: "x"}},
		})
	}
	ed.rebuild()
	if ed.docRoot == nil {
		t.Fatalf("the fixture does not build: %q", ed.status.Get())
	}
	// The set must actually be populated, or the cheap path is cheap
	// because it answers nothing.
	if !ed.pseudo["MenuItem"] || !ed.pseudo["Menu"] || !ed.pseudo["Tab"] {
		t.Fatalf("ed.pseudo does not hold the pseudo-elements: %v", ed.pseudo)
	}
	got := testing.AllocsPerRun(3, func() { ed.rebuild() })
	// 120 document nodes at ~87 allocations per catalog read is >10000
	// on its own; a rebuild that reads the catalog once lands far below.
	const ceiling = 8000
	if got > ceiling {
		t.Errorf("rebuild() allocates %.0f times for a %d-node document (ceiling %d): "+
			"something on the per-node path is asking the catalog again — see loadPalette",
			got, nodes*2, ceiling)
	}
	t.Logf("rebuild() allocates %.0f times for %d document nodes", got, nodes*2)
}
