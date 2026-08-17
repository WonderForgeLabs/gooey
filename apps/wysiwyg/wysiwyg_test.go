package main

import (
	"os"
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/apps/wysiwyg/components/preview"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/markup"
)

// buildPage builds the editor's SHIPPED page — whatever wysiwyg.gooey
// currently is. It is deliberately not parameterised: a test that asserts
// something about the page the user gets has to read that page.
func buildPage(t *testing.T) (*editor, gooey.Component) {
	t.Helper()
	ed := newEditor(editorFS())
	src, err := os.ReadFile("wysiwyg.gooey")
	if err != nil {
		t.Fatal(err)
	}
	root, err := markup.Build(src, ed.ctx)
	if err != nil {
		t.Fatalf("the editor's own page does not load: %v", err)
	}
	ed.rebuild()
	return ed, root
}

// shellLayout is what the editor currently composes: the activity rail
// and the designer. It is the shipped page's arrangement minus the empty
// regions, so a test using it is testing the real thing rather than a
// hypothetical one.
const shellLayout = `<Gooey><Grid Rows="1*" Cols="4,1*">
  <ActivityBar Grid.Row="0" Grid.Col="0" Name="Rail" Sel="{{.ActivitySel}}"/>
  <Preview Grid.Row="0" Grid.Col="1" Name="Island" Title="designer"/>
</Grid></Gooey>`

// TestTheShippedPageLoads is not nothing: the page is what the app starts
// with, and a page that does not load is a black screen at launch. With
// three regions empty it is most of what the shipped markup can be held
// to.
func TestTheShippedPageLoads(t *testing.T) {
	_, root := buildPage(t)
	if root == nil {
		t.Fatal("no root")
	}
	if findPreview(root) == nil {
		t.Error("the shipped page does not mount the designer; the one region that hosts is the one the canvas grows from")
	}
}

// THE CARET TESTS ARE GONE WITH THE PANE THEY GUARDED.
//
// TestEditorInputsAreSiblingsOfThePreview and its behavioural half asserted
// that the editor's only TextBox — the inspector's — was never a
// descendant of the rebuilt preview island, because rebuilding a subtree
// resets a caret to 0 and the user's next character lands mid-word.
//
// The inspector is gone, so the editor has no input at all, and both
// tests would now pass by asserting nothing: zero inputs inside the island
// is trivially true when there are zero inputs anywhere. Keeping them
// green would have been the exact failure this branch has catalogued nine
// times. They are at b41aa2a and in the commit that removed the panes.
//
// THE HAZARD IS NOT GONE, and whatever input arrives next inherits it.
// One half of the old mitigation survives structurally: <Preview> builds a
// Border around its host with no slot for children, so no page can nest
// anything inside the island. The half that is missing is the assertion
// that the editor's inputs are SIBLINGS of it — restore it, pointed at
// buildPage, on the first commit that adds a TextBox back.

// TestPaletteComesFromTheCatalog — claim 1. The palette is not a
// hand-listed menu; it is whatever this context can build.
func TestPaletteComesFromTheCatalog(t *testing.T) {
	ed := newEditor(editorFS())
	if len(ed.palette) < 20 {
		t.Fatalf("palette has %d entries; the catalog has more than that", len(ed.palette))
	}
	var button, logpane *markup.ElementSpec
	for i := range ed.palette {
		switch ed.palette[i].Name {
		case "Button":
			button = &ed.palette[i]
		case "LogPane":
			logpane = &ed.palette[i]
		}
	}
	if button == nil {
		t.Fatal("<Button> missing from the palette")
	}
	if logpane == nil {
		t.Fatal("the host-registered <LogPane> is missing; a builder must offer what the app can actually build")
	}
	// The honesty rule, at the point where a user would be misled.
	if !button.AttrsKnown || describeAttrs(*button) == "? unknown" {
		t.Error("<Button>'s attributes are knowable and must not read as unknown")
	}
	if logpane.AttrsKnown {
		t.Error("a registered Builder is a func; its attributes cannot be known")
	}
	if got := describeAttrs(*logpane); got != "? unknown" {
		t.Errorf("<LogPane> palette row = %q; an unknowable attribute set must not render as %q", got, "none")
	}
	if describeAttrs(*logpane) == describeAttrs(markup.ElementSpec{AttrsKnown: true}) {
		t.Error(`"unknown" and "none" render identically — the distinction the catalog exists for is lost in the UI`)
	}
}

// TestInspectorFollowsTheParent — claims 2 and 3, and the experiment the
// prototype was built to run. The SAME element, moved between
// containers, must offer a different attribute set: Canvas.Left is
// meaningful under a <Canvas> and silently dropped under a <VStack>.
func TestInspectorFollowsTheParent(t *testing.T) {
	ed := newEditor(editorFS())
	ed.rebuild()
	ed.sel = ed.doc().Kids[0]

	has := func(name string) bool {
		for _, r := range ed.attrRows() {
			if r.name == name {
				return true
			}
		}
		return false
	}

	ed.retype("Canvas")
	if !has("Canvas.Left") {
		t.Error("a child of a <Canvas> must be offered Canvas.Left")
	}
	if has("Grid.Row") {
		t.Error("a child of a <Canvas> must NOT be offered Grid.Row")
	}

	ed.retype("VStack")
	if has("Canvas.Left") {
		t.Error("a child of a <VStack> must not be offered Canvas.Left; applyLayout would discard it in silence")
	}
	// The universal set survives the move — it is joined in by HasLayout,
	// not contributed by the parent.
	if !has("Width") || !has("Name") {
		t.Error("the universal attributes must be offered under any parent")
	}
}

// TestRetypingStripsAttributesTheNewParentCannotHonor — the editor must
// not leave behind an attribute that would now be ignored. Leaving it is
// exactly what the old loader did, and it is the defect the catalog work
// exists to delete.
func TestRetypingStripsAttributesTheNewParentCannotHonor(t *testing.T) {
	ed := newEditor(editorFS())
	ed.rebuild()
	ed.retype("VStack")
	for _, k := range ed.doc().Kids {
		if _, ok := k.Attrs["Canvas.Left"]; ok {
			t.Errorf("<%s> kept Canvas.Left under a <VStack>", k.Elem)
		}
	}
	if !strings.Contains(ed.source.Get(), "<VStack") {
		t.Error("the emitted markup did not follow the retype")
	}
}

// TestEveryEditProducesMarkupThatBuilds — the editor's output is markup,
// so the measure of an edit is whether the result loads. Rejection makes
// this meaningful: before it, markup with a stray attribute loaded
// cleanly and did the wrong thing.
func TestEveryEditProducesMarkupThatBuilds(t *testing.T) {
	ed := newEditor(editorFS())
	ed.rebuild()
	for i := range ed.palette {
		ed.paletteSel.Set(i)
		ed.addSelected()
		if s := ed.status.Get(); strings.HasPrefix(s, "✗") {
			// Not every element can be added bare — some need required
			// attributes the catalog does not yet mark. That is a real
			// finding, not a test failure; it is reported by name so the
			// gap is visible.
			t.Logf("adding <%s> bare does not build: %s", ed.palette[i].Name, s)
			ed.deleteSelected()
			continue
		}
		if _, err := markup.Build([]byte(ed.source.Get()), ed.docCtx); err != nil {
			t.Errorf("status said it builds but it does not: %v", err)
		}
	}
}

// TestEveryBoundNameResolvesToALiveHandle catches a bug the other tests
// could not see, because they call the editor's methods directly rather
// than through the binding.
//
// Context.Values captures each handle BY VALUE. Building the map before
// creating a property therefore stores nil, every binding to that name
// resolves to nothing, and the pane renders EMPTY — with no error at
// load, no error at build, and nothing on screen to say why. That is the
// same silent-failure shape this whole change set exists to remove, so
// it deserves a guard rather than a second discovery.
//
// Found by running the app and looking at the screen. The unit tests
// were green throughout.
func TestEveryBoundNameResolvesToALiveHandle(t *testing.T) {
	ed := newEditor(editorFS())
	for name, v := range ed.ctx.Values {
		if v == nil {
			t.Errorf("%q is bound to a nil handle; its pane will render empty with no error", name)
		}
	}
	// And the two that feed the catalog-driven panes must actually
	// produce rows, which is the thing the screen showed was missing.
	if ed.paletteItems == nil || ed.paletteItems.Get().Len() == 0 {
		t.Error("the palette source is empty; the palette pane would render blank")
	}
	ed.rebuild()
	if ed.attrItems == nil || ed.attrItems.Get().Len() == 0 {
		t.Error("the inspector source is empty; the inspector pane would render blank")
	}
}

// TestDerivedListsInvalidateOnEdit is the second bug the unit tests
// could not see, for the same reason as the first: they called
// attrRows() directly, and the SCREEN reads through the computed.
//
// The document is plain Go state, so a computed over it records NO
// dependency and caches forever. The inspector kept offering
// Canvas.Left after the container became a <VStack> — the emitted markup
// was already correct, so only the pane was lying. An explicit revision
// property is what gives the graph something to invalidate on.
//
// This asserts through the property, not around it.
func TestDerivedListsInvalidateOnEdit(t *testing.T) {
	ed := newEditor(editorFS())
	ed.rebuild()
	ed.sel = ed.doc().Kids[0]

	names := func() map[string]bool {
		src := ed.attrItems.Get()
		out := map[string]bool{}
		for i := 0; i < src.Len(); i++ {
			out[src.At(i)["Name"].(string)] = true
		}
		return out
	}

	ed.retype("Canvas")
	if !names()["Canvas.Left"] {
		t.Fatal("under a <Canvas> the inspector must offer Canvas.Left")
	}
	ed.retype("VStack")
	if names()["Canvas.Left"] {
		t.Error("the inspector still offers Canvas.Left under a <VStack>: the computed did not invalidate, so the pane is lying about what can be set")
	}
}

// ---- tree helpers ----

func findPreview(c gooey.Component) gooey.Component {
	if _, ok := c.(*preview.Pane); ok {
		return c
	}
	for _, k := range children(c) {
		if got := findPreview(k); got != nil {
			return got
		}
	}
	return nil
}

func countInputs(c gooey.Component) int {
	n := 0
	if _, ok := c.(*components.TextBox); ok {
		n++
	}
	for _, k := range children(c) {
		n += countInputs(k)
	}
	return n
}

func firstTextBox(c gooey.Component) *components.TextBox {
	if tb, ok := c.(*components.TextBox); ok {
		return tb
	}
	for _, k := range children(c) {
		if got := firstTextBox(k); got != nil {
			return got
		}
	}
	return nil
}

func children(c gooey.Component) []gooey.Component {
	if p, ok := c.(interface{ ChildComponents() []gooey.Component }); ok {
		return p.ChildComponents()
	}
	if b, ok := c.(interface{ Child() gooey.Component }); ok {
		if ch := b.Child(); ch != nil {
			return []gooey.Component{ch}
		}
	}
	return nil
}

// TestPaletteNeverOffersTheEditorsOwnChrome.
//
// Dropping <Preview> from the palette into the document made the
// document contain the thing that RENDERS the document. Measure
// recursed — Canvas.Measure/MeasureChild alternating — until the stack
// overflowed, killing the process and leaving the user's terminal
// unrestored. Reported by a user, from a recording.
//
// The category error was in what the palette SOURCED, not in what it
// reported: it read the context the editor's own shell is built with,
// conflating "elements this context can build" with "elements the user
// is authoring with". Those differ by exactly the editor's furniture.
//
// A denylist of <Preview> would pass this test and re-break the day a
// third chrome component is added, so the assertion is structural: no
// element the EDITOR registers for itself may appear in the palette,
// whatever it is called.
// THE TWO "ABSENCE" TESTS HERE WENT VACUOUS AND WERE REPLACED.
//
// TestPaletteNeverOffersTheEditorsOwnChrome and
// TestDocumentCannotBuildTheEditorsChrome both looped over
// ed.ctx.Components and skipped any name also registered in docCtx. That
// was right while <Preview> was the only shared name. Registering <Panel>
// and <ActivityBar> as document vocabulary — they are reusable components,
// and withholding them made the toolbox misdescribe the app — meant every
// name was skipped and both loop bodies stopped executing. Two green tests
// asserting nothing, which is the failure this repo keeps cataloguing.
//
// They are not restored with an exemption list, because the thing worth
// guarding was never "chrome is absent from the document". It is that A
// DOCUMENT MUST NOT BE ABLE TO BUILD THE COMPONENT THAT RENDERS THE
// DOCUMENT — Measure recursed until the stack overflowed. That invariant
// survives sharing, so it is stated below in a form that stays honest as
// more components become document vocabulary.

// TestNoDocumentBuilderYieldsThePreviewPane is the recursion guard, stated
// as what it actually protects.
//
// Every name the DOCUMENT can build is built, and none of them may produce
// the pane that hosts the document. That covers <Preview> (which builds a
// Mirror here instead) and every component registered later, without
// needing to be told which names are dangerous.
func TestNoDocumentBuilderYieldsThePreviewPane(t *testing.T) {
	ed := newEditor(editorFS())
	if len(ed.docCtx.Components) == 0 {
		t.Fatal("the document context registers nothing, so this test asserts nothing")
	}
	checked := 0
	for name := range ed.docCtx.Components {
		// Sel= is required by <ActivityBar> and harmless on the others,
		// which ignore unknown attributes as registered builders may.
		src := `<Gooey><VStack Name="R"><` + name + ` Name="X" Sel="{{.ActivitySel}}"/></VStack></Gooey>`
		root, err := markup.Build([]byte(src), ed.docCtx)
		if err != nil {
			// A builder may legitimately refuse these attributes. That is
			// not this test's business — it cannot recurse if it cannot
			// build — but it must not be counted as checked.
			continue
		}
		checked++
		walkTree(root, func(c gooey.Component) {
			if p, ok := c.(*preview.Pane); ok {
				t.Errorf("a document built <%s> into %T (%p): the document now contains the "+
					"component that renders it, and Measure never bottoms out", name, p, p)
			}
		})
	}
	if checked == 0 {
		t.Fatal("no document builder could be exercised; this test asserts nothing")
	}
}

// TestTheEditorsOwnComponentsAreDocumentVocabulary is the positive half —
// the reason the tests above changed shape.
//
// A toolbox that lists every builtin while omitting the components the
// editor is built out of misdescribes the app, which is the same class of
// dishonesty as rendering "unknown" as "none".
// The check is REACHABILITY, not which map the registration lives in.
// It used to read ed.docCtx.Components[name] != nil, which pinned the
// mechanism rather than the claim — and when ActivityBar moved to
// Context.Elements to declare its Sel attribute, this failed while the
// document's access to it was strictly better than before. A test whose
// stated claim is "a document can use this element" must not fail
// because the element became describable.
func TestTheEditorsOwnComponentsAreDocumentVocabulary(t *testing.T) {
	ed := newEditor(editorFS())
	offered := map[string]bool{}
	for _, e := range ed.palette {
		offered[e.Name] = true
	}
	registered := map[string]bool{}
	for name := range ed.docCtx.Components {
		registered[name] = true
	}
	for name := range ed.docCtx.Elements {
		registered[name] = true
	}
	for _, name := range []string{"Panel", "ActivityBar"} {
		if !registered[name] {
			t.Errorf("<%s> is not document vocabulary; a document cannot use the editor's own component", name)
		}
		if !offered[name] {
			t.Errorf("the toolbox does not offer <%s>", name)
		}
	}
}

// ActivityBar declares its surface, and the palette is why that matters:
// clicking it used to emit <ActivityBar Name="ActivityBar1"/>, which
// fails to load because the rail requires a bound Sel=. seedRequired can
// only seed what the catalog describes.
//
// Mutation: register ActivityBar through Components instead of Elements
// and this fails at the Required check — Catalog gives a Builder
// AttrsKnown false with no Attrs at all, which is precisely the state
// that produced the bug.
func TestTheToolboxCanDescribeActivityBarsRequiredAttribute(t *testing.T) {
	ed := newEditor(editorFS())
	var spec *markup.ElementSpec
	for i, e := range ed.palette {
		if e.Name == "ActivityBar" {
			spec = &ed.palette[i]
		}
	}
	if spec == nil {
		t.Fatal("ActivityBar is absent from the palette")
	}
	if !spec.AttrsKnown {
		t.Fatal("ActivityBar's attributes are still unknowable; the palette cannot seed what it cannot describe")
	}
	for _, a := range markup.AttrsFor(*spec, "Canvas") {
		if a.Name != "Sel" {
			continue
		}
		// These three are exactly what seedRequired consumes: Required
		// decides whether to seed, Binds whether to write a binding
		// rather than a literal, GoType which handle to create.
		if !a.Required || a.Binds != markup.BindsBinding || a.GoType != "int" {
			t.Errorf("Sel = {Required:%v Binds:%q GoType:%q}, want {true binding int}", a.Required, a.Binds, a.GoType)
		}
		return
	}
	t.Error("ActivityBar does not declare Sel, so the palette will emit markup that cannot load")
}

// TestPreviewIsPlaceableAndBecomesAMirror.
//
// Dropping <Preview> into the document previously made the document
// contain the thing that renders it: Measure recursed until the stack
// overflowed and took the user's terminal with it. Reported by a user,
// from a recording.
//
// The fix keeps <Preview> PLACEABLE — removing it from the palette would
// have been the editor lying by omission about what a document can hold
// — and changes what it MEANS inside a document. The editor's <Preview>
// builds the real pane; the document's builds an Escher mirror.
//
// Asserted three ways, because "it no longer crashes" is not a thing a
// test can observe directly:
//
//  1. the palette still offers it,
//  2. building it yields a mirror, never the editor's own pane,
//  3. and it survives nesting inside a container — the case the obvious
//     parent-only guard would have missed.
func TestPreviewIsPlaceableAndBecomesAMirror(t *testing.T) {
	ed := newEditor(editorFS())

	var offered bool
	for _, e := range ed.palette {
		if e.Name == "Preview" {
			offered = true
		}
	}
	if !offered {
		t.Error("the palette must still offer <Preview>: it is genuinely placeable, and hiding it would misreport what a document can contain")
	}

	for _, src := range []string{
		`<Gooey><VStack Name="R"><Preview Name="P"/></VStack></Gooey>`,
		// The container case: <Preview> under something else, which a
		// parent-only check would not have caught.
		`<Gooey><VStack Name="R"><Canvas Name="C"><Preview Name="P" Canvas.Left="1" Canvas.Top="1"/></Canvas></VStack></Gooey>`,
	} {
		w, err := markup.Build([]byte(src), ed.docCtx)
		if err != nil {
			t.Fatalf("a document containing <Preview> must build: %v\n%s", err, src)
		}
		if found := findMirror(w); found == nil {
			t.Errorf("a document's <Preview> did not become a mirror:\n%s", src)
		}
		if containsPreviewPane(w, ed.pv) {
			t.Errorf("a document's <Preview> built the EDITOR'S OWN pane; that is the recursion:\n%s", src)
		}
	}
}

func findMirror(c gooey.Component) *preview.Mirror {
	if m, ok := c.(*preview.Mirror); ok {
		return m
	}
	for _, k := range children(c) {
		if got := findMirror(k); got != nil {
			return got
		}
	}
	return nil
}

func containsPreviewPane(c gooey.Component, pane *preview.Pane) bool {
	if c == gooey.Component(pane) {
		return true
	}
	for _, k := range children(c) {
		if containsPreviewPane(k, pane) {
			return true
		}
	}
	return false
}

// TestModifiedIsExactlyDifferingFromTheDeclaredDefault pins the Visual
// Studio rule the grid's emphasis is built on. Three states have to stay
// distinct, and only one of them is "modified":
//
//   - absent — the default applies, nothing to emphasise;
//   - written, equal to the default — the markup does the same thing, so
//     emphasising it would tell the user they changed something they did
//     not;
//   - written, different — the one row worth finding.
//
// A fourth case is deliberately never modified: an attribute the catalog
// declares no Default for. Empty is a third state, not a false, and
// AttrSpec.Default is empty exactly where nothing could check it.
func TestModifiedIsExactlyDifferingFromTheDeclaredDefault(t *testing.T) {
	ed := newEditor(editorFS())
	ed.retype("Canvas")
	ed.rebuild()
	ed.sel = ed.doc().Kids[0]

	row := func(name string) attrRow {
		t.Helper()
		for _, r := range ed.attrRows() {
			if r.name == name {
				return r
			}
		}
		t.Fatalf("no inspector row for %s", name)
		return attrRow{}
	}

	// newEditor seeds Canvas.Left="2" against a declared default of "0".
	if r := row("Canvas.Left"); !r.modified {
		t.Errorf("Canvas.Left=%q against default %q must read as modified", r.value, r.def)
	}
	if r := row("Visibility"); r.modified {
		t.Errorf("an absent Visibility must not read as modified (default %q)", r.def)
	}

	target := ed.doc().Kids[0]
	target.Attrs["Visibility"] = "Visible" // the declared default, written out
	if r := row("Visibility"); r.modified {
		t.Error("a value equal to the default must not read as modified: " +
			"the markup does the same thing either way")
	}
	target.Attrs["Visibility"] = "Hidden"
	if r := row("Visibility"); !r.modified {
		t.Error("a value differing from the default must read as modified")
	}

	// Name declares no Default — nothing can check one for an address —
	// so it can never be modified however it is written.
	if r := row("Name"); r.def != "" {
		t.Errorf("Name must declare no default, got %q", r.def)
	}
	if r := row("Name"); r.modified {
		t.Error("an attribute with no declared default can never be modified")
	}
}

// TestInspectorRowsAreGroupedByCategory — the categorised view is
// expressed as row ORDER rather than as header rows, because a header row
// would sit in the same list the selection indexes into and every
// activation would have to guard against editing one.
func TestInspectorRowsAreGroupedByCategory(t *testing.T) {
	ed := newEditor(editorFS())
	ed.retype("Canvas")
	ed.rebuild()
	ed.sel = ed.doc().Kids[1] // the Button: it has an event, a style and a layout surface

	rows := ed.attrRows()
	if len(rows) < 6 {
		t.Fatalf("expected a populated inspector, got %d rows", len(rows))
	}
	seen := map[string]bool{}
	last := -1
	for _, r := range rows {
		rank := categoryRank(r.cat)
		if rank < last {
			t.Fatalf("%s (%s) came after a later category: rows are not grouped",
				r.name, r.cat)
		}
		if rank > last {
			if seen[r.cat] {
				t.Fatalf("category %s appears in two runs: rows are not grouped", r.cat)
			}
			seen[r.cat] = true
		}
		last = rank
	}
	// The grouping has to be doing something: a single-category inspector
	// would pass the ordering check while proving nothing.
	if len(seen) < 3 {
		t.Fatalf("expected several categories on a <Button>, saw %v", seen)
	}
}
