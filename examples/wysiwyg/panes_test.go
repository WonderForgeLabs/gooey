package main

import (
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/markup"
)

// The panes are controls with DECLARED surfaces, and this file is the
// evidence that the declarations do something.
//
// Extracting four panes into four files is not by itself an improvement:
// markup that binds whatever it likes against one shared struct is the
// same markup whichever file it sits in. What changes it into a contract
// is <x:Property> — the surface is checked when the page loads instead of
// being whatever the pane happened to read.
//
// So each case below is a pair. The good instantiation must BUILD and the
// broken one must FAIL, because a rejection test whose good half was
// already broken proves only that everything fails, and an acceptance
// test with no broken half proves only that nothing is checked. Both
// halves run against the same context the editor itself uses.

// paneCases is one entry per pane: a correct instantiation, then the
// three ways a caller can get it wrong. The attributes are written out
// rather than derived from the declarations on purpose — a test that
// generated them from the same file it is checking would agree with any
// surface, including an empty one.
var paneCases = []struct {
	name string
	good string
	// undeclared adds an attribute no pane declares.
	undeclared string
	// wrongType binds a string handle where the declaration says int.
	wrongType string
	// missing omits one required attribute.
	missing string
}{
	{
		name:       "Palette",
		good:       `<Palette Items="{{.PaletteItems}}" Sel="{{.PaletteSel}}" Activate="{{.Add}}"/>`,
		undeclared: `<Palette Items="{{.PaletteItems}}" Sel="{{.PaletteSel}}" Activate="{{.Add}}" Colour="red"/>`,
		wrongType:  `<Palette Items="{{.PaletteItems}}" Sel="{{.Source}}" Activate="{{.Add}}"/>`,
		missing:    `<Palette Items="{{.PaletteItems}}" Sel="{{.PaletteSel}}"/>`,
	},
	{
		name: "Inspector",
		good: `<Inspector Items="{{.AttrItems}}" Sel="{{.AttrSel}}" Activate="{{.BeginEdit}}"
		                  EditName="{{.EditName}}" EditValue="{{.EditValue}}"
		                  Commit="{{.CommitEdit}}" Doc="{{.EditDoc}}"/>`,
		undeclared: `<Inspector Items="{{.AttrItems}}" Sel="{{.AttrSel}}" Activate="{{.BeginEdit}}"
		                        EditName="{{.EditName}}" EditValue="{{.EditValue}}"
		                        Commit="{{.CommitEdit}}" Doc="{{.EditDoc}}" Rows="7"/>`,
		wrongType: `<Inspector Items="{{.AttrItems}}" Sel="{{.Source}}" Activate="{{.BeginEdit}}"
		                       EditName="{{.EditName}}" EditValue="{{.EditValue}}"
		                       Commit="{{.CommitEdit}}" Doc="{{.EditDoc}}"/>`,
		missing: `<Inspector Items="{{.AttrItems}}" Sel="{{.AttrSel}}" Activate="{{.BeginEdit}}"
		                     EditName="{{.EditName}}" EditValue="{{.EditValue}}"
		                     Commit="{{.CommitEdit}}"/>`,
	},
	{
		name:       "MarkupView",
		good:       `<MarkupView Tree="{{.TreeText}}" Source="{{.Source}}"/>`,
		undeclared: `<MarkupView Tree="{{.TreeText}}" Source="{{.Source}}" Wrap="true"/>`,
		// Not an int declaration to violate, so this one goes the other
		// way: a Type="string" declaration handed a handle that is not a
		// string property.
		wrongType: `<MarkupView Tree="{{.PaletteItems}}" Source="{{.Source}}"/>`,
		missing:   `<MarkupView Tree="{{.TreeText}}"/>`,
	},
}

func buildPane(t *testing.T, ed *editor, frag string) error {
	t.Helper()
	_, err := markup.Build([]byte("<Gooey>"+frag+"</Gooey>"), ed.ctx)
	return err
}

// TestEveryPaneAcceptsItsDeclaredSurface is the half that has to pass, and
// it is what makes the three rejection tests below mean anything.
func TestEveryPaneAcceptsItsDeclaredSurface(t *testing.T) {
	ed := newEditor(editorFS())
	for _, c := range paneCases {
		if err := buildPane(t, ed, c.good); err != nil {
			t.Errorf("<%s> does not build with the attributes the shell writes: %v", c.name, err)
		}
	}
}

// TestAPaneRejectsAnAttributeItDoesNotDeclare is strict mode: declaring a
// surface at all makes an undeclared attribute a load error.
//
// This is the case that matters most in an EDITOR, because the editor
// writes markup. A pane that silently ignored an attribute would let a
// typo — or a shell rewritten to pass something new — look like it worked
// while the pane never received it.
func TestAPaneRejectsAnAttributeItDoesNotDeclare(t *testing.T) {
	ed := newEditor(editorFS())
	for _, c := range paneCases {
		err := buildPane(t, ed, c.undeclared)
		if err == nil {
			t.Errorf("<%s> accepted an attribute it does not declare; the surface is not being checked", c.name)
			continue
		}
		if !strings.Contains(err.Error(), "dependency property") {
			t.Errorf("<%s> failed for the wrong reason — want the declared-surface error, got: %v", c.name, err)
		}
	}
}

// TestAPaneRejectsAWronglyTypedBinding — the declaration carries a Type,
// and a handle of the wrong one is caught at load rather than producing a
// pane that renders nothing.
//
// The error's SHAPE is asserted, not just its existence, and that is the
// whole value of this test. Both of these fragments fail without any
// declarations too — a string handle reaching ItemsView's Selected= is an
// error on its own — so "err != nil" would have passed against a control
// with no surface at all. Measured: deleting the <x:Property> block from
// palette.gooey leaves this test green if it only checks for non-nil.
func TestAPaneRejectsAWronglyTypedBinding(t *testing.T) {
	ed := newEditor(editorFS())
	for _, c := range paneCases {
		err := buildPane(t, ed, c.wrongType)
		if err == nil {
			t.Errorf("<%s> accepted a binding of the wrong type; Type= on the declaration is decorative", c.name)
			continue
		}
		if !strings.Contains(err.Error(), "dependency property") {
			t.Errorf("<%s> rejected the wrong type, but not AT THE SURFACE — the failure came from inside the pane, "+
				"so the declaration is not what caught it: %v", c.name, err)
		}
	}
}

// TestAPaneRejectsAMissingRequiredAttribute — every pane declaration is
// Required, because a pane with a defaulted input would come up empty and
// look like a data problem instead of a wiring one.
//
// Same reasoning as above for the error-shape check: an omitted attribute
// also breaks the binding inside the pane, so non-nil alone would not
// distinguish a checked surface from an unchecked one.
func TestAPaneRejectsAMissingRequiredAttribute(t *testing.T) {
	ed := newEditor(editorFS())
	for _, c := range paneCases {
		err := buildPane(t, ed, c.missing)
		if err == nil {
			t.Errorf("<%s> built without a required attribute", c.name)
			continue
		}
		if !strings.Contains(err.Error(), "required attribute missing") {
			t.Errorf("<%s> failed without its required attribute, but the message is not the surface's — "+
				"the omission was caught downstream, where it reads as a broken pane rather than a wrong call: %v", c.name, err)
		}
	}
}

// ---- the panes render, not just load ----
//
// Everything above this line checks a CONTRACT: that a pane accepts the
// attributes it declares and refuses the ones it does not. Everything
// elsewhere in this package checks the MODEL: attrRows, the palette, the
// per-Kind cycles. Between the two there was a hole exactly the width of
// the thing a user looks at — no test composed a pane and asked whether
// the data arrived on screen.
//
// That hole existed before the extraction too (the old shell's tests
// asserted layout and the cramped message, never a pane's contents), but
// it matters more now: each pane is a separately loadable unit whose
// bindings resolve inside its own context, and a declared surface that is
// accepted and then never read is a pane that loads perfectly and renders
// nothing.

// paneScreen composes the four-pane fixture and returns the text plane.
// 120x40 gives each pane 60x20 — enough rows that a list is not clipped
// down to its border.
func paneScreen(t *testing.T, ed *editor, root gooey.Component) string {
	t.Helper()
	c := gooey.NewComposer(root, 120, 40)
	t.Cleanup(c.Close)
	ed.rebuild()
	c.Frame()
	var b strings.Builder
	cells := c.Cells()
	cols, rows := c.Size()
	for y := 0; y < rows; y++ {
		for x := 0; x < cols; x++ {
			r := cells.At(x, y).Rune
			if r == 0 {
				r = ' '
			}
			b.WriteRune(r)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// TestEachPaneRendersTheEditorsData is the missing pin: every pane must
// put something on screen that could only have come from the editor's
// state.
//
// Each expectation is chosen to be traceable to one binding rather than
// to the pane's own chrome — a Border title would pass while the pane's
// entire contents were blank.
func TestEachPaneRendersTheEditorsData(t *testing.T) {
	ed, root := buildPanes(t)
	ed.selected = 0
	if r, ok := ed.selectedRow(); ok {
		ed.editAsText(r) // load a row into the inspector's input
	}
	out := paneScreen(t, ed, root)

	for _, c := range []struct{ pane, want, from string }{
		{"palette", "Button", "an element name out of (*markup.Context).Catalog()"},
		{"palette", "? unknown", "the honesty column — LogPane's unknowable attribute set"},
		{"inspector", "Canvas.Left", "an attribute row from AttrsFor(spec, parent)"},
		{"markup", "<Canvas", "the emitted document source"},
		{"markup", ">   <Text", "the outline, with its selection marker"},
		{"preview", "click", "the previewed document's <Button Content=\"click\">"},
	} {
		if !strings.Contains(out, c.want) {
			t.Errorf("the %s pane does not show %q (%s) — it loaded and rendered nothing from it:\n%s",
				c.pane, c.want, c.from, out)
		}
	}
}

// TestAPaneFollowsTheEditorLive is the discrimination half, and it is the
// one that could not pass by accident.
//
// The test above would still pass against a pane that rendered its
// bindings once and then went deaf — which is a real failure mode here,
// not a hypothetical: a computed that reads no property records no
// dependency and caches forever, and the derived lists only invalidate
// because they read ed.rev. So this changes editor state after the first
// frame and requires the screen to follow.
func TestAPaneFollowsTheEditorLive(t *testing.T) {
	ed, root := buildPanes(t)
	const sentinel = "ZZ-SENTINEL-ZZ"

	before := paneScreen(t, ed, root)
	if strings.Contains(before, sentinel) {
		t.Fatal("the sentinel is already on screen, so its later presence proves nothing")
	}

	// One value into the inspector's input, one element into the document
	// the preview and the markup pane both read.
	ed.editName.Set("Content")
	ed.editValue.Set(sentinel)
	ed.paletteSel.Set(indexOfPaletteEntry(t, ed, "Text"))
	ed.addSelected()

	after := paneScreen(t, ed, root)
	if !strings.Contains(after, sentinel) {
		t.Errorf("the inspector's input did not follow a Set after the first frame:\n%s", after)
	}
	// The document grew, so the markup pane's outline must have grown too.
	if strings.Count(before, "<Text") >= strings.Count(after, "<Text") {
		t.Errorf("the markup pane did not follow an edit to the document — before %d, after %d occurrences of \"<Text\"",
			strings.Count(before, "<Text"), strings.Count(after, "<Text"))
	}
}

func indexOfPaletteEntry(t *testing.T, ed *editor, name string) int {
	t.Helper()
	for i, e := range ed.palette {
		if e.Name == name {
			return i
		}
	}
	t.Fatalf("no <%s> in the palette", name)
	return -1
}

// TestThePreviewDeclaresNothingAndSaysSo pins the one asymmetry.
//
// The other three panes take their whole input as attributes; this one
// takes a live component tree, which no attribute can carry. So it has a
// code-behind and an implicit surface, and the test that would be wrong
// here is one asserting all four panes are alike.
func TestThePreviewDeclaresNothingAndSaysSo(t *testing.T) {
	ed := newEditor(editorFS())
	if err := buildPane(t, ed, `<Preview/>`); err != nil {
		t.Fatalf("<Preview/> takes no attributes and must build bare: %v", err)
	}
	root, err := markup.Build([]byte(`<Gooey><Preview Name="Island"/></Gooey>`), ed.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if findPreview(root) == nil {
		t.Error("the <Preview> control did not mount the editor's pane; its setup is the only thing carrying it across the markup boundary")
	}
}
