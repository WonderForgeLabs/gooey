package main

import (
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/markup"
)

// The element BODY — <Text>hello</Text> — which the editor could not
// express at all until node.Body existed.
//
// The bug this closes was found by click-to-select and is not a selection
// bug: node.markup serialized attributes, children and slots, so every
// <Text> the toolbox added came out as <Text Name="Text3"/>. That builds
// cleanly, measures ZERO, and a zero-size component is never returned by
// hitTest — so it was invisible on the canvas, unclickable, and therefore
// impossible to select in order to give it the content that would have
// made it appear. A user reaching for Text first would have reported
// click-to-select as broken.

// addFromPalette adds the named element the way the user does — by moving
// the palette selection and activating it — rather than by constructing a
// node, so the seeding path is the one under test.
func addFromPalette(t *testing.T, ed *editor, elem string) (*node, int) {
	t.Helper()
	for i, e := range ed.palette {
		if e.Name == elem {
			ed.paletteSel.Set(i)
			ed.addSelected()
			return ed.root.Kids[len(ed.root.Kids)-1], len(ed.root.Kids) - 1
		}
	}
	t.Fatalf("the palette does not offer <%s>", elem)
	return nil, -1
}

// TestAPaletteAddedTextIsVisibleAndSelectable is the user-facing claim,
// end to end: add a Text the way the toolbox does, and it must be on
// screen and pressable.
//
// Both halves matter and neither implies the other. A non-zero size with
// no hit would mean the walk is wrong; a hit with zero size cannot happen,
// which is exactly why the old behaviour was invisible AND unselectable
// from a single cause.
func TestAPaletteAddedTextIsVisibleAndSelectable(t *testing.T) {
	ed, c := designerPage(t)
	n, i := addFromPalette(t, ed, "Text")
	if !strings.HasPrefix(ed.status.Get(), "✓") {
		t.Fatalf("adding a <Text> from the palette does not build: %s", ed.status.Get())
	}
	c.Frame()

	comp := docKid(ed, i)
	b := comp.(interface{ Bounds() gooey.Rect }).Bounds()
	if b.W == 0 || b.H == 0 {
		t.Fatalf("a palette-added <Text> was arranged at %v: it is invisible on the canvas, and "+
			"a zero-size component is never returned by hitTest so it cannot be selected either", b)
	}

	ed.setSelection(nil)
	if !press(c, b.X, b.Y) {
		t.Fatal("a press on the new <Text> was not consumed")
	}
	if ed.sel != n {
		t.Errorf("a press on the new <Text> selected %v, want the node just added", ed.sel)
	}
}

// TestTheBodyReachesTheBuiltComponent is the round trip: node.Body ->
// markup text -> the Text component's Content property.
//
// Asserted on the PROPERTY rather than on the markup string, because a
// markup string containing the right characters proves only that the
// serializer ran — the parser has to agree with it.
func TestTheBodyReachesTheBuiltComponent(t *testing.T) {
	ed, c := designerPage(t)
	n, i := addFromPalette(t, ed, "Text")
	n.Body = "hello there"
	ed.rebuild()
	c.Frame()

	txt, ok := docKid(ed, i).(*components.Text)
	if !ok {
		t.Fatalf("the node built a %T, not a *components.Text", docKid(ed, i))
	}
	if got := txt.Content.Get(); got != "hello there" {
		t.Errorf("the built Text's content is %q, want %q: the body did not survive the "+
			"serialize-and-reparse round trip", got, "hello there")
	}
}

// TestABodyWithMarkupCharactersStillLoads is the escaping.
//
// The body is free text the user typed into the properties grid, so "<"
// and "&" arrive in it as a matter of course. Unescaped they do not
// produce a helpful message — a .gooey parse failure surfaces as "no root
// element", which points at nothing.
func TestABodyWithMarkupCharactersStillLoads(t *testing.T) {
	ed, c := designerPage(t)
	n, i := addFromPalette(t, ed, "Text")
	const raw = `a < b & "c" > d`
	n.Body = raw
	ed.rebuild()
	if !strings.HasPrefix(ed.status.Get(), "✓") {
		t.Fatalf("a body containing markup characters broke the document: %s", ed.status.Get())
	}
	c.Frame()

	txt, ok := docKid(ed, i).(*components.Text)
	if !ok {
		t.Fatalf("the node built a %T, not a *components.Text", docKid(ed, i))
	}
	if got := txt.Content.Get(); got != raw {
		t.Errorf("the body came back as %q, want %q", got, raw)
	}
	// And the emitted document must be parseable on its own, which is what
	// the CODE tab shows and what a save would write.
	if _, err := markup.Build([]byte(ed.source.Get()), ed.docCtx); err != nil {
		t.Errorf("the emitted markup does not reparse: %v\n%s", err, ed.source.Get())
	}
}

// TestTheBodyRowEditsTheBodyAndNeverAnAttribute.
//
// The grid's edit path is shared with every attribute row, so the risk is
// that "(text)" is written into node.Attrs and emitted as an attribute —
// which would be a load error carrying a name the editor itself supplied.
func TestTheBodyRowEditsTheBodyAndNeverAnAttribute(t *testing.T) {
	ed, _ := designerPage(t)
	n, _ := addFromPalette(t, ed, "Text")
	ed.setSelection(n)

	row, at := -1, ed.attrRows()
	for j, r := range at {
		if r.body {
			row = j
		}
	}
	if row < 0 {
		t.Fatal("the properties grid offers no body row for a <Text>")
	}
	if at[row].name != BodyRowName {
		t.Errorf("the body row is called %q, want %q", at[row].name, BodyRowName)
	}
	if at[row].value != n.Body {
		t.Errorf("the body row shows %q but the node holds %q", at[row].value, n.Body)
	}

	// Edit it the way the UI does: select the row, load it, type, commit.
	ed.attrSel.Set(row)
	ed.beginEdit()
	ed.editValue.Set("edited")
	ed.commitEdit()

	if got := n.Body; got != "edited" {
		t.Errorf("committing the body row left the body as %q, want %q", got, "edited")
	}
	if _, ok := n.Attrs[BodyRowName]; ok {
		t.Error("committing the body row wrote an ATTRIBUTE named " + BodyRowName)
	}
	if strings.Contains(ed.source.Get(), BodyRowName) {
		t.Errorf("%q was emitted into the markup:\n%s", BodyRowName, ed.source.Get())
	}
	if !strings.Contains(ed.source.Get(), ">edited</Text>") {
		t.Errorf("the edited body is not in the emitted markup:\n%s", ed.source.Get())
	}

	// Clearing it must clear the body, not leave the old text: the field
	// is assigned, where an attribute would be deleted.
	ed.beginEdit()
	ed.editValue.Set("")
	ed.commitEdit()
	if got := n.Body; got != "" {
		t.Errorf("clearing the body row left %q", got)
	}
}

// TestTheBodyRowIsOfferedOnlyWhereThereIsABody.
//
// <Button> carries its label in Content= and its catalog entry says
// nested text is IGNORED, so offering a body row there would invite the
// user to type into something that does nothing — the same class of
// dishonesty as offering Canvas.Left under a VStack.
func TestTheBodyRowIsOfferedOnlyWhereThereIsABody(t *testing.T) {
	ed, _ := designerPage(t)
	hasBodyRow := func() bool {
		for _, r := range ed.attrRows() {
			if r.body {
				return true
			}
		}
		return false
	}

	tx, _ := addFromPalette(t, ed, "Text")
	ed.setSelection(tx)
	if !hasBodyRow() {
		t.Error("a <Text> is offered no body row, so its content cannot be edited at all")
	}
	bn, _ := addFromPalette(t, ed, "Button")
	ed.setSelection(bn)
	if hasBodyRow() {
		t.Error("a <Button> is offered a body row: its label is Content= and nested text is " +
			"ignored, so anything typed there would silently do nothing")
	}
}

// TestTheBodyRowIsDescribedByTheCatalogAndNotByThisFile.
//
// takesBody used to be `elem == "Text"` and the row's kind/legal/doc were
// strings written here. Both are now reads of markup.BodySpec, so the row
// describes whatever the catalog says — including for an element that
// gains a body later, without this file changing.
//
// Asserted by COMPARING against the spec rather than against literals: a
// test that hardcoded "text" would pass just as well against a row that
// hardcoded it, which is the thing being removed.
func TestTheBodyRowIsDescribedByTheCatalogAndNotByThisFile(t *testing.T) {
	ed, _ := designerPage(t)
	n, _ := addFromPalette(t, ed, "Text")
	ed.setSelection(n)

	// Read from the CATALOG, not through ed.bodySpec. Asking the function
	// under test what the answer should be compares it to itself: a
	// bodySpec that returned a hand-built spec would agree with every
	// assertion below. A mutation returning a hardcoded BodySpec for
	// "Text" survived this test until this line changed.
	var bs *markup.BodySpec
	for _, e := range ed.docCtx.Catalog() {
		if e.Name == "Text" {
			bs = e.Body
		}
	}
	if bs == nil {
		t.Fatal("the catalog no longer reports <Text> as a body element")
	}
	var row attrRow
	for _, r := range ed.attrRows() {
		if r.body {
			row = r
		}
	}
	if row.name == "" {
		t.Fatal("no body row")
	}
	if row.kind != string(bs.Kind) {
		t.Errorf("the body row's kind is %q, the catalog says %q", row.kind, bs.Kind)
	}
	if row.doc != bs.Doc {
		t.Errorf("the body row's doc is %q, the catalog says %q", row.doc, bs.Doc)
	}
	if bs.Doc == "" {
		t.Error("the catalog's BodySpec carries no Doc, so the row has nothing to say")
	}
	// The Binds distinction has to REACH the row. A body that binds must
	// not be described as literal-only.
	if bs.Binds == markup.BindsEither && !strings.Contains(row.legal, "{{") {
		t.Errorf("the body binds either way but the row says %q: a user is not told they "+
			"can bind it", row.legal)
	}
}

// TestABodyBindingIsLiveAndNotLiteralText is why BodySpec carries Binds
// rather than being a bool.
//
// <Text>'s body goes through bindText, so {{.Serving}} typed into the
// body row is a LIVE BINDING. An editor that assumed literal-only would
// silently downgrade it to the eight characters the user typed — the same
// class of defect as the %q attribute bug: a valid input quietly becoming
// something else.
func TestABodyBindingIsLiveAndNotLiteralText(t *testing.T) {
	ed, c := designerPage(t)
	n, _ := addFromPalette(t, ed, "Text")
	n.Body = "{{.Serving}}"
	ed.rebuild()
	if !strings.HasPrefix(ed.status.Get(), "✓") {
		t.Fatalf("a bound body does not build: %s", ed.status.Get())
	}
	c.Frame()

	txt, ok := docKid(ed, len(ed.root.Kids)-1).(*components.Text)
	if !ok {
		t.Fatalf("the node built a %T, not a *components.Text", docKid(ed, len(ed.root.Kids)-1))
	}
	if got := txt.Content.Get(); got == "{{.Serving}}" {
		t.Fatal("the body was taken literally: the binding was downgraded to its own source text")
	}
	// The proof it is live: move the bound property and the component
	// follows, with no rebuild.
	ed.serveInfo.Set("bound-and-live")
	if got := txt.Content.Get(); got != "bound-and-live" {
		t.Errorf("the built Text reads %q after the bound property moved, want %q: the body "+
			"resolved to something that is not the live handle", got, "bound-and-live")
	}
}

// TestTheBodyRowNameCanNeverShadowARealAttribute is the guard on the
// spelling, checked against the whole catalog rather than against the one
// element that has a body today.
func TestTheBodyRowNameCanNeverShadowARealAttribute(t *testing.T) {
	ed := newEditor(editorFS())
	checked := 0
	for _, e := range ed.palette {
		for _, a := range markup.AttrsFor(e, "Canvas") {
			checked++
			if a.Name == BodyRowName {
				t.Errorf("<%s> has a real attribute called %q, which the body row would shadow",
					e.Name, BodyRowName)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no attributes were examined; this test asserts nothing")
	}
}

// TestOnlyBodyElementsAreSeededWithOne pins the seeding rule at the point
// where the palette meets the document, for EVERY element the toolbox
// offers rather than for the one being fixed.
func TestOnlyBodyElementsAreSeededWithOne(t *testing.T) {
	ed := newEditor(editorFS())
	ed.rebuild()
	for i := range ed.palette {
		ed.paletteSel.Set(i)
		ed.addSelected()
		n := ed.root.Kids[len(ed.root.Kids)-1]
		switch {
		case ed.takesBody(n.Elem) && n.Body == "":
			t.Errorf("<%s> takes a body and was added without one: it is invisible and "+
				"unselectable on the canvas", n.Elem)
		case !ed.takesBody(n.Elem) && n.Body != "":
			t.Errorf("<%s> was seeded with a body %q it does not read", n.Elem, n.Body)
		}
	}
}
