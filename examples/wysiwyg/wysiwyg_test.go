package main

import (
	"os"
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/examples/wysiwyg/components/preview"
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

// paneLayout mounts all four panes as siblings. The shipped page does not
// do this today — it is empty, pending the canvas-first rebuild — so the
// two structural tests below assert against this instead.
//
// BE CLEAR ABOUT WHAT THAT COSTS. While the page was a four-pane shell,
// those tests read the shipped markup, so a future edit that nested an
// input inside the preview would have failed them. Against a fixture they
// can only prove the PANES permit a correct arrangement, not that the
// shipped page uses one. The missing half comes back the moment the new
// shell exists, by pointing them at buildPage again.
//
// What survives the change intact is the stronger guarantee the
// extraction bought: <Preview> is a control whose markup is a fixed
// Border wrapping its host, with no slot for children at all. An input
// inside the rebuilt island is no longer a mistake a page can make.
const paneLayout = `<Gooey><Grid Rows="1*,1*" Cols="1*,1*">
  <Preview Grid.Row="0" Grid.Col="0" Name="Island"/>
  <Palette Grid.Row="0" Grid.Col="1" Items="{{.PaletteItems}}" Sel="{{.PaletteSel}}" Activate="{{.Add}}"/>
  <MarkupView Grid.Row="1" Grid.Col="0" Tree="{{.TreeText}}" Source="{{.Source}}"/>
  <Inspector Grid.Row="1" Grid.Col="1" Items="{{.AttrItems}}" Sel="{{.AttrSel}}" Activate="{{.BeginEdit}}"
             EditName="{{.EditName}}" EditValue="{{.EditValue}}" Commit="{{.CommitEdit}}" Doc="{{.EditDoc}}"/>
</Grid></Gooey>`

func buildPanes(t *testing.T) (*editor, gooey.Component) {
	t.Helper()
	ed := newEditor(editorFS())
	root, err := markup.Build([]byte(paneLayout), ed.ctx)
	if err != nil {
		t.Fatalf("the four panes do not compose: %v", err)
	}
	ed.rebuild()
	return ed, root
}

// TestTheShippedPageLoads is what buildPage still pins on its own, and it
// is not nothing: the page is what the app starts with, and a page that
// does not load is a black screen at launch.
func TestTheShippedPageLoads(t *testing.T) {
	_, root := buildPage(t)
	if root == nil {
		t.Fatal("no root")
	}
}

// TestEditorInputsAreSiblingsOfThePreview is the structural mitigation
// for the caret hazard, and the reason it is worth having rather than a
// convention: it is checkable at BUILD TIME.
//
// Patching or rebuilding a subtree preserves focus and the bound text
// but resets the caret to 0, because a caret is component-local state
// and the component was replaced. The editor rebuilds its preview on
// every keystroke, so an input inside that island would send the user's
// next character to the middle of their own text — a data-shaped bug
// that reads as "the app put my text in the wrong place" and trains
// people to blame themselves.
//
// Keeping inputs OUTSIDE the rebuilt island removes the question. This
// test fails if a later edit moves one inside, so the guarantee cannot
// rot silently.
func TestEditorInputsAreSiblingsOfThePreview(t *testing.T) {
	_, root := buildPanes(t)

	island := findPreview(root)
	if island == nil {
		t.Fatal("no <Preview> island in the editor's page")
	}
	inside := countInputs(island)
	if inside != 0 {
		t.Errorf("%d input(s) live inside the rebuilt island; a rebuild resets their caret to 0. Move them out — they must be SIBLINGS of <Preview>.", inside)
	}
	if total := countInputs(root); total == 0 {
		t.Fatal("the editor has no inputs at all, so this test is asserting nothing")
	}
}

// TestPreviewRebuildDoesNotDisturbTheEditorsOwnInput is the behavioural
// half: rebuilding the island repeatedly must leave the inspector's
// TextBox — and the text in it — exactly where it was.
func TestPreviewRebuildDoesNotDisturbTheEditorsOwnInput(t *testing.T) {
	ed, root := buildPanes(t)
	box := firstTextBox(root)
	if box == nil {
		t.Fatal("no TextBox in the editor")
	}
	ed.editValue.Set("hello")

	for i := 0; i < 5; i++ {
		ed.addSelected()
		ed.retype("VStack")
		ed.retype("Canvas")
	}
	if got := firstTextBox(root); got != box {
		t.Error("the inspector's TextBox was replaced by a preview rebuild")
	}
	if got := ed.editValue.Get(); got != "hello" {
		t.Errorf("the editor's own input lost its text across rebuilds: %q", got)
	}
}

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
	ed.selected = 0

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
	for _, k := range ed.root.Kids {
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
	ed.selected = 0

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
func TestPaletteNeverOffersTheEditorsOwnChrome(t *testing.T) {
	ed := newEditor(editorFS())
	if len(ed.ctx.Components) == 0 {
		t.Fatal("the editor registers no chrome, so this test asserts nothing")
	}
	offered := map[string]bool{}
	for _, e := range ed.palette {
		offered[e.Name] = true
	}
	for name := range ed.ctx.Components {
		if ed.docCtx.Components[name] != nil {
			continue // deliberately part of the document's vocabulary
		}
		if offered[name] {
			t.Errorf("the palette offers <%s>, which is the editor's own chrome: "+
				"a document containing it renders the document, and Measure never bottoms out", name)
		}
	}
}

// TestDocumentCannotBuildTheEditorsChrome is the same guarantee at the
// other end: even if something put <Preview> in a document, the document
// vocabulary cannot build it.
func TestDocumentCannotBuildTheEditorsChrome(t *testing.T) {
	ed := newEditor(editorFS())
	for name := range ed.ctx.Components {
		if ed.docCtx.Components[name] != nil {
			continue
		}
		src := "<Gooey><VStack Name=\"R\"><" + name + " Name=\"X\"/></VStack></Gooey>"
		if _, err := markup.Build([]byte(src), ed.docCtx); err == nil {
			t.Errorf("a document built <%s>; the editor's chrome must not be in the document vocabulary", name)
		}
	}
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
	ed.selected = 0

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

	target := ed.root.Kids[0]
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
	ed.selected = 1 // the Button: it has an event, a style and a layout surface

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
