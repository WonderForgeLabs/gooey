package main

import (
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
)

// The editor stranded by a document that stopped building.
//
// The whole failure was reported as "i can't select any components on
// canvas" and took an hour to find, because every one of the three
// mechanisms below hid the next. The document held a <Tabs> inside a
// <Tabs>, which markup.Build rejects; rebuild dropped docRoot and
// returned; the previewed tree stayed on screen looking pressable; every
// press resolved to no node and installed a drag hint; and the hint
// displaced the one line naming the actual error. The status bar had held
// `✗ markup: <Tabs> children must be <Tab> elements` the entire time.
//
// Each test below pins ONE of those, and each has the arm where the
// mechanism must say the other thing — a status bar that only ever shows
// errors, or an insert that only ever refuses, would pass half of this
// file and be useless.

// paletteIndex is the palette entry for elem, or a skip.
func paletteIndex(t *testing.T, ed *editor, elem string) int {
	t.Helper()
	for i, e := range ed.palette {
		if e.Name == elem {
			return i
		}
	}
	t.Skipf("no <%s> in the palette; this fixture cannot be built", elem)
	return -1
}

// breakTheDocument appends an element no vocabulary knows, which is the
// smallest edit that makes rebuild fail. The <Tabs>-in-<Tabs> that caused
// the original is no longer reachable through the palette — that is what
// the third test asserts — so the fixture uses a node built by hand.
func breakTheDocument(ed *editor) {
	ed.doc().Kids = append(ed.doc().Kids, &node{Elem: "Nonesuch"})
	ed.rebuild()
}

// TestAnErrorOutranksADragHint is the mechanism that hid the diagnostic.
//
// THE ORDERING IS THE BUG, not the presence of either string. The hint is
// installed BY THE PRESS, so a user whose document has stopped building
// destroys the explanation by doing the one thing that would prompt them
// to read it. Both arms are here because a status bar that always
// preferred the build status would hide every drag refusal instead, which
// is the same defect pointed the other way.
func TestAnErrorOutranksADragHint(t *testing.T) {
	ed, _ := buildPage(t)

	// The must-say-HINT arm: the document builds, so the hint is the news.
	ed.status.Set("✓ builds")
	ed.dragHint.Set("nothing is selected: press an element to move it")
	if got := ed.statusText.Get(); got != "nothing is selected: press an element to move it" {
		t.Errorf("with a healthy document the status bar shows %q; the drag hint is "+
			"the only thing that has anything to say", got)
	}

	// The must-say-ERROR arm: the same hint, now competing with a reason
	// the press could not have worked at all.
	ed.status.Set("✗ markup: <Tabs> children must be <Tab> elements; got <Tabs>")
	got := ed.statusText.Get()
	if !strings.HasPrefix(got, "✗") {
		t.Errorf("the status bar shows %q while the document does not build; the "+
			"error is what explains the failed press, and the failed press is what "+
			"set the hint that displaced it", got)
	}
}

// TestAStalePreviewSaysSoRatherThanBlamingTheSelection is the sentence the
// user was given while doing exactly what it told them to do.
//
// With docRoot nil every press resolves to no node, so dragKind reported
// DragNone and the editor answered "nothing is selected: press an element
// to move it" — to someone pressing an element, repeatedly. The selection
// is not the problem, and saying it is sends them to the wrong pane.
func TestAStalePreviewSaysSoRatherThanBlamingTheSelection(t *testing.T) {
	ed, root := buildPage(t)
	c := gooey.NewComposer(root, 270, 72)
	t.Cleanup(c.Close)
	c.Frame()
	ed.bindPicking(func(x, y int) gooey.Component { return c.Focus().HitTest(x, y) }, func() {})

	// The cell of an element that IS on screen, taken before the break so
	// it is the last good tree's geometry — which is precisely what the
	// user is still looking at afterwards.
	b := docKid(ed, 0).(gooey.Bounded).Bounds()

	breakTheDocument(ed)
	if ed.docRoot != nil {
		t.Fatal("the fixture did not break the document; the rest of this test asserts nothing")
	}

	press(c, b.X, b.Y)
	hint := ed.dragHint.Get()
	if strings.Contains(hint, "nothing is selected") {
		t.Errorf("pressing a visible element on a stale preview says %q; the "+
			"selection is not why the press did nothing", hint)
	}
	if !strings.Contains(hint, "does not build") {
		t.Errorf("the refusal says %q; it has to name the reason, which is that "+
			"the document stopped building and the designer is showing an older tree", hint)
	}
}

// TestThePaletteRefusesAnInsertTheDocumentCannotBuild is the transactional
// guard, and its subject MOVED when tabs became authorable.
//
// It used to use <Tabs> + <Text>, because that combination was
// unsatisfiable: <Tabs> declares Only:["Tab"] and <Tab> is not in the
// palette, so nothing could be added to a <Tabs> at all. addplan.go
// closed that — the insert is now wrapped in the one child the container
// declares — so <Tabs> is the wrong fixture for a refusal and the test
// would have kept passing for the wrong reason had it only been made to
// compile.
//
// <Border> is the case that remains: ModeOne, already holding its child.
// The catalog cannot say the slot is taken — the mode says "exactly one",
// not "one is left" — so canHold answers yes, the insert is TRIED, and
// the trial build is what refuses. That is the design: permissive where
// the catalog is silent, because a refusal that names both elements costs
// less than an insert silently relocated.
func TestThePaletteRefusesAnInsertTheDocumentCannotBuild(t *testing.T) {
	ed, _ := buildPage(t)
	ed.paletteSel.Set(paletteIndex(t, ed, "Text"))

	// The must-say-YES arm FIRST, so a refusal that refused everything
	// cannot pass this test. A <Canvas> holds anything.
	ed.sel = ed.doc()
	before := len(ed.doc().Kids)
	ed.addSelected()
	if len(ed.doc().Kids) != before+1 {
		t.Fatalf("adding a <Text> to a <Canvas> left %d children, was %d; the "+
			"refusal below proves nothing if the insert never works",
			len(ed.doc().Kids), before)
	}
	if ed.docRoot == nil {
		t.Fatal("a legal insert left the document not building")
	}

	// Now the arm that must refuse: a <Border> whose one slot is taken.
	border := &node{
		Elem:  "Border",
		Attrs: map[string]string{"Name": "B", "Canvas.Left": "0", "Canvas.Top": "12"},
		Kids:  []*node{{Elem: "Text", Body: "taken", Attrs: map[string]string{"Name": "Inner"}}},
	}
	ed.doc().Kids = append(ed.doc().Kids, border)
	ed.rebuild()
	if ed.docRoot == nil {
		t.Fatalf("the <Border> fixture does not build: %q", ed.status.Get())
	}

	ed.sel = border
	if got := ed.addTarget("Text"); got != border {
		t.Fatalf("with the <Border> selected an Add targets %s; this test is only "+
			"about what happens when it targets the <Border> itself", nodeLabel(got))
	}
	kids := len(border.Kids)
	ed.addSelected()

	if len(border.Kids) != kids {
		t.Errorf("the <Border> took %d children, was %d: an element it cannot hold "+
			"was written into the document anyway", len(border.Kids), kids)
	}
	if ed.docRoot == nil {
		t.Error("the refused insert left docRoot nil — which is the state that " +
			"kills click-to-select for the whole document while the previous " +
			"tree stays on screen looking pressable")
	}
	msg := ed.status.Get()
	if !strings.Contains(msg, "<Text>") || !strings.Contains(msg, "<Border>") {
		t.Errorf("the refusal says %q; it has to name BOTH the element and the "+
			"container, because \"that doesn't go there\" without saying what "+
			"decides it is the same dead end one step later", msg)
	}
}
