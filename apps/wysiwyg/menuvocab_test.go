package main

import (
	"errors"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
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
	specs := map[string]markup.ElementSpec{}
	for _, e := range ed.docCtx.Catalog() {
		specs[e.Name] = e
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
	// THE RULE IS Nested, NOT "named in an Only list", and the
	// difference is the whole of markNested's doc comment. "Named in an
	// Only list" is the one-conjunct converse it spends two paragraphs
	// rejecting, and markup/menuvocab_test.go's
	// TestARestrictedContainerDoesNotHideARealElement builds a fixture
	// specifically to forbid it — a host with Only: ["Button"] must
	// leave <Button> on offer.
	//
	// Asserting the converse here made the two tests contradict each
	// other about one derivation. It passed only because every Only name
	// in the shipped catalog happens to be Pseudo or NonVisual, so
	// adding that host would turn this red while blaming a palette
	// filter that is right. Found in review of #454.
	for name, parent := range restricted {
		spec, ok := specs[name]
		if !ok {
			// UNDECLARED, which is #429's symptom one element over and
			// not something this test can assert about the palette: an
			// element with no catalog entry has no Nested flag to
			// compare against. <Companion> restricts to <Arg> and
			// <Var>, and buildCompanion validates both by hand
			// (companion.go:289, :322) — so nothing is silently
			// dropped, but no property grid can show them either.
			//
			// Declaring them is exactly what ParsedBy is for and is
			// deliberately NOT done here: it is a new public surface,
			// beyond the findings this commit answers, and it belongs
			// with the same tests and docs <Menu>/<MenuItem> got.
			// Logged rather than skipped silently, so the next reader
			// finds it.
			t.Logf("gap: <%s> is named in <%s>'s Only list but is not a declared element — "+
				"its properties are unreachable from any tool, the way <MenuItem>'s were", name, parent)
			continue
		}
		if offered[name] == spec.Nested {
			if spec.Nested {
				t.Errorf("the palette offers <%s>, which is Nested — legal only inside <%s>: adding it produces markup that will not load", name, parent)
			} else {
				t.Errorf("the palette withholds <%s>, which <%s> restricts to but which is NOT Nested: an element placeable on its own has been hidden", name, parent)
			}
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
	// DIFFERENTIAL, NOT AN ABSOLUTE CEILING, and the change is the point.
	//
	// This was `AllocsPerRun(...) > 8000` against a measured 6,630 — a
	// fifth of headroom over its own baseline. The number encoded today's
	// TOTAL rebuild cost, so any unrelated change adding 20% to rebuild()
	// turned it red, in a nested module CI only VETS: the failure would
	// surface first in the CLAUDE.md loop, in a file named for menu
	// vocabulary, where it is easy to misattribute.
	//
	// The claim is "the catalog is not read per node". Measuring the same
	// rebuild at N and 2N nodes and asserting the PER-NODE delta stays
	// small says exactly that, and is invariant to rebuild() getting
	// cheaper or dearer overall. A catalog read per node shows up as a
	// delta of ~87 allocations per node — the cost of one Catalog() —
	// against single digits when the snapshot is doing its job.
	// Raised in review of #454.
	measure := func(t *testing.T, nodes int) float64 {
		t.Helper()
		ed, _ := buildPage(t)
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
		return testing.AllocsPerRun(3, func() { ed.rebuild() })
	}

	const n = 60
	small := measure(t, n)
	large := measure(t, 2*n)
	// Each added node is a VStack AND a Text, so the document grows by
	// 2*n nodes between the two measurements.
	perNode := (large - small) / float64(2*n)

	// THE CEILING IS SET FROM THE MEASURED PAIR, not from a guess about
	// what the work "should" cost — which is the correction round 3 of
	// this review already forced on the two absolute pins, and which I
	// got wrong again here on the first attempt by assuming the healthy
	// per-node cost was single digits. Measured on this checkout:
	//
	//	healthy                       52.6 allocations per node
	//	catalog read per node        141.7 allocations per node
	//
	// (the regression arm produced by replacing ed.pseudo[k.Elem] in
	// select.go with a scan of ed.docCtx.Catalog()). 90 sits between
	// them with room for ordinary per-node work to grow, and still
	// catches a partial regression that reads the catalog for only some
	// nodes.
	const perNodeCeiling = 90
	if perNode > perNodeCeiling {
		t.Errorf("rebuild() allocates %.1f times per additional document node "+
			"(%.0f at %d nodes, %.0f at %d; ceiling %d): a Catalog() read is ~87 "+
			"allocations, so something on the per-node path is asking again — see loadPalette",
			perNode, small, 2*n, large, 4*n, perNodeCeiling)
	}
	t.Logf("rebuild(): %.0f allocs at %d nodes, %.0f at %d — %.1f per node",
		small, 2*n, large, 4*n, perNode)
}

// TestAttrRowsDoesNotRebuildTheCatalog is the SECOND allocation pin, and
// it is not a duplicate of the one above — the two watch different
// paths, and the first is blind to what this catches.
//
// attrRows() reaches the catalog through target(). That is O(1) per
// call, so a rebuild-ceiling test cannot see it; but attrRows is
// evaluated by ed.attrItems, a prop.NewComputed bound to
// <ItemsView Items="{{.AttrItems}}">, which means it runs INSIDE that
// ItemsView's paint node on every repaint after an ed.rev bump — every
// property edit, every selection change — and again per keypress from
// selectedRow() and per commit from valueEditor.Write.
//
// Context.Catalog() re-derives every builtin spec with fresh Attrs
// copies, re-runs markNested and sorts, at ~89 allocations. Worse, it
// globs *.gooey and runs Declarations on every include file when a
// context has them: filesystem I/O and XML parsing inside a Render,
// which has nowhere to put an error. docCtx sets no Includes today and
// this is a workspace editor.
//
// The ceiling comes from BOTH measurements rather than a guess:
// building the rows for a selected <MenuItem> costs 91 allocations, and
// restoring the Catalog() call in specOf takes it to 180. 135 sits
// between them with room either side — a guard against a regression of
// a known shape, not a budget attrRows is held to.
func TestAttrRowsDoesNotRebuildTheCatalog(t *testing.T) {
	ed, _, _, _, item := menuFixture(t)
	ed.setSelection(item)
	if len(ed.attrRows()) == 0 {
		t.Fatal("the selected <MenuItem> has no rows: this test would measure nothing")
	}
	got := testing.AllocsPerRun(5, func() { _ = ed.attrRows() })
	const ceiling = 135
	if got > ceiling {
		t.Errorf("attrRows() allocates %.0f times (ceiling %d): something on it is rebuilding "+
			"the catalog, and it runs inside the properties ItemsView's paint node — see target()",
			got, ceiling)
	}
	t.Logf("attrRows() allocates %.0f times", got)
}

// TestANestedParentsGrantStillReachesTheGrid is finding 3 of the review
// of #454, and it is the palette-vs-catalog mistake that target() had
// just been corrected for, one function away.
//
// grantOf scanned ed.palette, which is the catalog MINUS what may not be
// placed on its own — so a Nested parent was simply absent and the scan
// returned the empty Grant. Every attached row vanished from the
// inspector with no error: #418's defect returning through the fix for
// #429's.
//
// It is inert in the shipped catalog because defMenu grants nothing,
// which is why this registers its own pair. A nested container that
// grants an attached property is the whole case, and nothing in the
// vocabulary is one yet.
func TestANestedParentsGrantStillReachesTheGrid(t *testing.T) {
	ed, _ := buildPage(t)
	if ed.docCtx.Elements == nil {
		ed.docCtx.Elements = map[string]*markup.ElementDef{}
	}
	// THREE LEVELS, mirroring MenuBar -> Menu -> MenuItem, because
	// markNested marks an element Nested only when it is NAMED in some
	// other element's Only list. The granting container must be the
	// nested one — that is the whole case — so it needs an owner above
	// it that restricts to it.
	ed.docCtx.Elements["GrantOwner"] = &markup.ElementDef{
		Name: "GrantOwner", Icon: "list-unordered", Known: true,
		Children: markup.ChildSpec{Mode: markup.ModeRestricted, Only: []string{"GrantHost"}},
		Build: func(markup.Element, *markup.Context) (gooey.Component, error) {
			return nil, errNotStandalone
		},
	}
	ed.docCtx.Elements["GrantHost"] = &markup.ElementDef{
		Name: "GrantHost", Icon: "list-unordered", ParsedBy: "GrantOwner", Known: true,
		Grants:   markup.Grant{Attached: []markup.AttrSpec{{Name: "GrantHost.Slot", Kind: markup.KindInt, Binds: markup.BindsLiteral}}},
		Children: markup.ChildSpec{Mode: markup.ModeRestricted, Only: []string{"GrantKid"}},
		Build: func(markup.Element, *markup.Context) (gooey.Component, error) {
			return nil, errNotStandalone
		},
	}
	ed.docCtx.Elements["GrantKid"] = &markup.ElementDef{
		Name: "GrantKid", Icon: "list-selection", ParsedBy: "GrantOwner", Known: true,
		Children: markup.ChildSpec{Mode: markup.ModeLeaf},
		Build: func(markup.Element, *markup.Context) (gooey.Component, error) {
			return nil, errNotStandalone
		},
	}
	ed.loadPalette()

	// The premise: GrantHost really is Nested, so it really is absent
	// from the palette. Without this the assertion below passes for a
	// host the palette happened to contain.
	spec, ok := ed.specOf("GrantHost")
	if !ok {
		t.Fatal("the registered host is not in the catalog: nothing below was tested")
	}
	if !spec.Nested {
		t.Fatal("the registered host is not Nested, so it is in the palette and this test cannot see the bug")
	}
	for _, e := range ed.palette {
		if e.Name == "GrantHost" {
			t.Fatal("the palette offers the nested host: same")
		}
	}

	if got := ed.grantOf("GrantHost"); len(got.Attached) != 1 {
		t.Errorf("a nested parent's grant resolved to %d attached attributes, want 1: "+
			"the inspector drops every attached row for a nested container", len(got.Attached))
	}
}

var errNotStandalone = errors.New("markup: only valid inside its parent")

// TestTheCatalogSnapshotMatchesTheCatalog is the enforcement the snapshot
// shipped without.
//
// specOf, target(), grantOf and pairAgrees moved off a live
// docCtx.Catalog() read and onto ed.specs / ed.pseudo, which loadPalette
// fills. That is correct as shipped — loadPalette runs once at the end of
// newEditor and docCtx is never reassigned — and the paint-path reason
// for moving is sound: Catalog() re-derives every builtin spec with fresh
// Attrs copies, and mapNodes asks per document node on every rebuild.
//
// WHAT MADE IT WORTH PINNING is that both failure modes are SILENT, and
// both are the symptoms the change exists to remove: a missing ed.specs
// entry sends target() to the markup.ElementSpec{Name: n.Elem} fallback,
// which is #429's empty property grid, and grantOf returns
// markup.Grant{}, which is #418's missing attached rows. Neither errors,
// and a nil map reads exactly like an empty one.
//
// WHAT THIS CANNOT CATCH, stated so nobody reads more into it: it cannot
// see a future caller that changes the vocabulary and forgets to
// re-derive, because a test only exercises the paths it calls. It pins
// that loadPalette's derivation IS the catalog — so a divergence in the
// derivation itself is red rather than blank. The stronger fix is one
// seam owning both halves (mutate docCtx and re-derive in one step), and
// it is worth taking the day docCtx.Includes is wired, which target()'s
// own comment calls "one feature away". Raised in review of #454.
func TestTheCatalogSnapshotMatchesTheCatalog(t *testing.T) {
	ed, _ := buildPage(t)
	check := func(when string) {
		t.Helper()
		cat := ed.docCtx.Catalog()
		if len(ed.specs) != len(cat) {
			t.Errorf("%s: ed.specs has %d entries, the catalog has %d — a name missing here "+
				"is an empty property grid and an empty Grant, with no error on either path",
				when, len(ed.specs), len(cat))
		}
		for _, e := range cat {
			if _, ok := ed.specs[e.Name]; !ok {
				t.Errorf("%s: <%s> is in the catalog and not in ed.specs", when, e.Name)
			}
			if e.Pseudo && !ed.pseudo[e.Name] {
				t.Errorf("%s: <%s> is Pseudo in the catalog and not in ed.pseudo", when, e.Name)
			}
		}
	}
	check("after newEditor")

	// And after the vocabulary changes — the path
	// TestANestedParentsGrantStillReachesTheGrid exercises, where the
	// re-derive has to be remembered by hand.
	if ed.docCtx.Elements == nil {
		ed.docCtx.Elements = map[string]*markup.ElementDef{}
	}
	ed.docCtx.Elements["SnapshotProbe"] = &markup.ElementDef{
		Name:  "SnapshotProbe",
		Proto: &components.Border{},
		Known: true,
		Build: func(e markup.Element, ctx *markup.Context) (gooey.Component, error) {
			return &components.Border{}, nil
		},
	}
	ed.loadPalette()
	check("after a vocabulary change and loadPalette")
	if _, ok := ed.specs["SnapshotProbe"]; !ok {
		t.Error("the re-derive did not pick up the new element, so the check above is vacuous")
	}
}
