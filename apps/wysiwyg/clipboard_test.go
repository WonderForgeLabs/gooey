package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
)

// clipEditor is an editor whose system-clipboard write is captured
// instead of going to a terminal. Every test here uses it, because the
// real writer needs an App and a Screen and `go test` has neither — and
// because the failure path is the one that has to be exercised.
func clipEditor(t *testing.T) (*editor, *fakeClip) {
	t.Helper()
	ed := newEditor(editorFS())
	f := &fakeClip{}
	prev := writeSystemClipboard
	writeSystemClipboard = func(_ *editor, text string) error {
		f.last = text
		f.calls++
		return f.err
	}
	t.Cleanup(func() { writeSystemClipboard = prev })
	ed.rebuild()
	return ed, f
}

type fakeClip struct {
	last  string
	calls int
	err   error
}

func findNode(ed *editor, name string) *node {
	var found *node
	walkNode(ed.root, func(n *node) {
		if n.Attrs != nil && n.Attrs["Name"] == name {
			found = n
		}
	})
	return found
}

func names(ed *editor) []string {
	var out []string
	walkNode(ed.doc(), func(n *node) {
		if n.Attrs != nil && n.Attrs["Name"] != "" {
			out = append(out, n.Attrs["Name"])
		}
	})
	return out
}

// ---- the deep copy ----

func TestDeepCopySharesNothing(t *testing.T) {
	src := &node{Elem: "VStack", Attrs: map[string]string{"Name": "V1"},
		Kids:  []*node{{Elem: "Text", Body: "hi", Attrs: map[string]string{"Name": "T1"}}},
		Slots: map[string]*node{"ItemTemplate": {Elem: "Text", Attrs: map[string]string{"Name": "Tpl"}}}}

	c := src.deepCopy()
	if c.Slots["ItemTemplate"] == nil {
		// Guarded rather than dereferenced: a copy that drops Slots is a
		// real regression, and it should fail with THIS sentence rather
		// than as a nil-pointer panic that aborts the rest of the file.
		t.Fatal("the copy has no ItemTemplate slot; a deep copy that walks Kids alone loses property elements")
	}
	c.Attrs["Name"] = "changed"
	c.Kids[0].Body = "changed"
	c.Slots["ItemTemplate"].Attrs["Name"] = "changed"

	if src.Attrs["Name"] != "V1" {
		t.Error("editing the copy's Attrs changed the original")
	}
	if src.Kids[0].Body != "hi" {
		t.Error("editing the copy's child changed the original")
	}
	if src.Slots["ItemTemplate"].Attrs["Name"] != "Tpl" {
		t.Error("editing the copy's SLOT changed the original")
	}
}

// THE SLOT CASE, on its own, because it is the one a copy silently
// loses: Slots holds property elements (<ItemsView.ItemTemplate>), which
// are structured attributes rather than children, so a walk over Kids
// alone drops an entire subtree with no error anywhere — the paste
// succeeds, the element builds, and its template is simply gone.
func TestDeepCopyCarriesSlots(t *testing.T) {
	src := &node{Elem: "ItemsView", Attrs: map[string]string{"Name": "I1"},
		Slots: map[string]*node{"ItemTemplate": {Elem: "Text", Body: "{{.Label}}"}}}
	c := src.deepCopy()
	if c.Slots == nil || c.Slots["ItemTemplate"] == nil {
		t.Fatal("the copy lost its ItemTemplate slot")
	}
	if c.Slots["ItemTemplate"].Body != "{{.Label}}" {
		t.Errorf("slot body = %q, want the original's", c.Slots["ItemTemplate"].Body)
	}
	// And the serialization agrees, which is what a save and the system
	// clipboard both go through.
	if !strings.Contains(c.markup(""), "ItemsView.ItemTemplate") {
		t.Errorf("copied subtree serializes without its slot:\n%s", c.markup(""))
	}
}

// ---- names ----

func TestPastingBesideTheOriginalRenamesRatherThanColliding(t *testing.T) {
	ed, _ := clipEditor(t)
	ed.sel = findNode(ed, "T1")
	ed.copySelected()
	ed.pasteClip()

	got := names(ed)
	seen := map[string]int{}
	for _, n := range got {
		seen[n]++
	}
	for n, c := range seen {
		if c > 1 {
			t.Fatalf("name %q appears %d times after a paste; markup.Build treats a duplicate as a LOAD ERROR, so the document no longer builds: %v", n, c, got)
		}
	}
	if ed.sel == nil || ed.sel.Attrs["Name"] != "T2" {
		t.Errorf("pasted node is named %q, want T2 (base T, lowest free suffix)", ed.sel.Attrs["Name"])
	}
	// The whole point of renaming: the document still builds.
	if s := ed.status.Get(); strings.HasPrefix(s, "✗") {
		t.Errorf("after a paste the build failed: %s", s)
	}
}

func TestASecondPasteTakesTheNextFreeName(t *testing.T) {
	ed, _ := clipEditor(t)
	ed.sel = findNode(ed, "T1")
	ed.copySelected()
	ed.pasteClip()
	ed.pasteClip()

	got := names(ed)
	want := map[string]bool{"T2": true, "T3": true}
	for _, n := range got {
		delete(want, n)
	}
	if len(want) != 0 {
		t.Errorf("after two pastes the names are %v; missing %v — the second paste reused a name or invented T1_copy_copy", got, want)
	}
}

// A name that is FREE in the destination is kept, which is what makes
// pasting into a different document preserve the names the user's
// bindings were written against.
func TestAFreeNameSurvivesThePaste(t *testing.T) {
	ed, _ := clipEditor(t)
	ed.sel = findNode(ed, "B1")
	ed.copySelected()
	// Remove the original, so its name is free when the copy lands.
	ed.deleteSelected()
	ed.pasteClip()

	if ed.sel.Attrs["Name"] != "B1" {
		t.Errorf("a non-colliding name was rewritten to %q; renaming must be a response to a COLLISION, not a reflex",
			ed.sel.Attrs["Name"])
	}
}

// Renaming descends. A subtree's INNER names collide just as its root's
// does, and an element nobody looks at is exactly where a duplicate goes
// unnoticed until the build breaks.
func TestRenamingReachesInsideTheSubtree(t *testing.T) {
	ed, _ := clipEditor(t)
	root := ed.doc()
	root.Kids = append(root.Kids, &node{Elem: "VStack", Attrs: map[string]string{"Name": "V1"},
		Kids: []*node{{Elem: "Text", Body: "inner", Attrs: map[string]string{"Name": "Inner1"}}}})
	ed.rebuild()

	ed.sel = findNode(ed, "V1")
	ed.copySelected()
	ed.pasteClip()

	seen := map[string]int{}
	for _, n := range names(ed) {
		seen[n]++
	}
	if seen["Inner1"] != 1 {
		t.Fatalf("the INNER name appears %d times; renaming stopped at the subtree root", seen["Inner1"])
	}
}

// A name freed by a delete is still not reusable, because its binding
// keys are still registered — nothing ever unregisters one, deliberately,
// so an undone paste can be redone onto the values the user had set.
// Handing that name to a new element would silently adopt a deleted
// element's state.
func TestANameWhoseBindingKeysSurviveIsNotReused(t *testing.T) {
	ed, _ := clipEditor(t)
	ed.ctx.Values["Ghost1_Content"] = prop.NewSource("left behind")
	ed.doc().Kids = append(ed.doc().Kids, &node{Elem: "Text", Body: "x",
		Attrs: map[string]string{"Name": "Ghost1"}})
	ed.rebuild()

	ed.sel = findNode(ed, "Ghost1")
	ed.copySelected()
	ed.pasteClip()
	if got := ed.sel.Attrs["Name"]; got == "Ghost1" {
		t.Fatal("the paste reused a name that still owns registered binding keys")
	}
}

// ---- bindings ----

// THE ALIASING BUG. markup.Seeded gives each instance its own binding
// keys precisely so a second Gauge does not move the first one's needle.
// A copy inherits the original's keys, so without re-keying both
// elements bind the same handle: both documents load, both paint, and
// moving one moves the other. Silent.
func TestPastedPerInstanceBindingsAreRekeyed(t *testing.T) {
	ed, _ := clipEditor(t)
	spec := ed.specFor("Gauge")
	if spec.Seed == "" {
		t.Skip("no <Gauge> in this build's palette")
	}
	src, values, err := markup.Seeded(spec, "G1")
	if err != nil {
		t.Fatalf("seed <Gauge>: %v", err)
	}
	for k, v := range values {
		ed.ctx.Values[k] = v
	}
	n, err := nodeOf(src)
	if err != nil {
		t.Fatal(err)
	}
	n.Attrs["Name"] = "G1"
	ed.doc().Kids = append(ed.doc().Kids, n)
	ed.rebuild()

	before := n.Attrs["Value"]
	if before == "" {
		t.Skip("<Gauge>'s seed does not bind Value in this build")
	}

	ed.sel = findNode(ed, "G1")
	ed.copySelected()
	ed.pasteClip()

	after := ed.sel.Attrs["Value"]
	if after == before {
		t.Fatalf("the pasted <Gauge> still binds %s — it SHARES state with the one it was copied from", after)
	}
	key := strings.TrimSuffix(strings.TrimPrefix(after, "{{."), "}}")
	if _, ok := ed.ctx.Values[key]; !ok {
		t.Fatalf("re-keyed to %q but registered nothing under it; the document will not build", key)
	}
	// The convention is markup's, not a second one invented here.
	if key != markup.SeedKey(ed.sel.Attrs["Name"], "Value") {
		t.Errorf("key = %q, want markup.SeedKey(%q, \"Value\") = %q — two conventions will drift",
			key, ed.sel.Attrs["Name"], markup.SeedKey(ed.sel.Attrs["Name"], "Value"))
	}
	if s := ed.status.Get(); strings.HasPrefix(s, "✗") {
		t.Errorf("the re-keyed paste does not build: %s", s)
	}
}

// A binding pointing OUTSIDE the subtree is carried across untouched. It
// names a viewmodel property the user wired up by hand, and rewriting or
// dropping it would be editing their intent; if the destination has no
// such name the build says so, which is a visible failure.
func TestAForeignBindingIsCarriedAcrossUnchanged(t *testing.T) {
	ed, _ := clipEditor(t)
	ed.ctx.Values["UserPicked"] = prop.NewSource("hand wired")
	ed.doc().Kids = append(ed.doc().Kids, &node{Elem: "Text",
		Attrs: map[string]string{"Name": "T9", "Content": "{{.UserPicked}}"}})
	ed.rebuild()

	ed.sel = findNode(ed, "T9")
	ed.copySelected()
	ed.pasteClip()

	if got := ed.sel.Attrs["Content"]; got != "{{.UserPicked}}" {
		t.Errorf("a foreign binding became %q; it must survive a paste verbatim", got)
	}
}

// The longest-match rule in splitSeedKey, which is not pedantry: "T1"
// and "T1_Extra" can both be element names, and a shortest-match split
// would re-key {{.T1_Extra_Content}} as T1's "Extra_Content" attribute —
// a key nothing registers and a binding that fails at load.
func TestSeedKeySplitTakesTheLongestName(t *testing.T) {
	renamed := map[string]string{"T1": "T5", "T1_Extra": "T1_Extra2"}
	old, suffix, ok := splitSeedKey("T1_Extra_Content", renamed)
	if !ok {
		t.Fatal("no split at all")
	}
	if old != "T1_Extra" || suffix != "Content" {
		t.Errorf("split as (%q, %q), want (T1_Extra, Content)", old, suffix)
	}
}

// ---- cut ----

func TestCutRemovesAndKeeps(t *testing.T) {
	ed, _ := clipEditor(t)
	ed.sel = findNode(ed, "B1")
	ed.cutSelected()

	if findNode(ed, "B1") != nil {
		t.Error("cut left the node in the document")
	}
	if ed.clip.node == nil || ed.clip.node.Attrs["Name"] != "B1" {
		t.Fatal("cut did not put the node on the clipboard")
	}
	ed.pasteClip()
	if findNode(ed, "B1") == nil {
		t.Error("pasting a cut node did not bring it back")
	}
}

// The root cannot be cut, and the refusal has to happen BEFORE the copy.
// deleteSelected refuses the user's root (a document must keep one), so
// a cut that copied first would leave the user believing the root was on
// the clipboard while it sat untouched — and the next paste would
// DUPLICATE it.
func TestCuttingTheRootIsRefusedBeforeItIsCopied(t *testing.T) {
	ed, _ := clipEditor(t)
	ed.sel = ed.doc()
	ed.cutSelected()

	if ed.clip.node != nil {
		t.Error("the root was put on the clipboard by a cut that could not remove it")
	}
	if ed.doc() == nil || len(ed.root.Kids) != 1 {
		t.Error("the document lost its root")
	}
	if s := ed.status.Get(); !strings.Contains(s, "cannot be cut") {
		t.Errorf("status = %q, want it to say the cut was refused", s)
	}
}

// A cut says so. deleteSelected rebuilds, and rebuild sets the build
// status, so a message written before the delete is overwritten in the
// same frame by "builds" — and the cut works while reporting nothing.
func TestCutReportsItselfAfterTheRebuild(t *testing.T) {
	ed, _ := clipEditor(t)
	ed.sel = findNode(ed, "B1")
	ed.cutSelected()
	if s := ed.status.Get(); !strings.HasPrefix(s, "cut ") {
		t.Errorf("status after a cut = %q, want it to start with \"cut \"", s)
	}
}

// ---- the system half ----

func TestCopyWritesTheSubtreeMarkupToTheSystemClipboard(t *testing.T) {
	ed, f := clipEditor(t)
	ed.sel = findNode(ed, "B1")
	ed.copySelected()

	if f.calls != 1 {
		t.Fatalf("the system clipboard was written %d times, want 1", f.calls)
	}
	if !strings.Contains(f.last, `<Button`) || !strings.Contains(f.last, `"B1"`) {
		t.Errorf("system clipboard got %q, want the subtree's markup", f.last)
	}
	if f.last != ed.clip.markup {
		t.Error("the two clipboards were given different text")
	}
	if s := ed.status.Get(); !strings.Contains(s, "system clipboard") {
		t.Errorf("status = %q, want it to mention the system clipboard", s)
	}
}

// THE ONE THAT MATTERS. A confirmation for a copy that did not happen is
// worse than no feature. When the write fails the status must say so and
// must NOT claim the copy reached the terminal.
func TestAFailedSystemCopyIsReportedNotConfirmed(t *testing.T) {
	ed, f := clipEditor(t)
	f.err = fmt.Errorf("terminal suspended")
	ed.sel = findNode(ed, "B1")
	ed.copySelected()

	s := ed.status.Get()
	if !strings.Contains(s, "terminal suspended") {
		t.Fatalf("status = %q, want it to carry the failure", s)
	}
	if strings.Contains(s, "→ system clipboard") {
		t.Fatalf("status = %q claims the copy reached the terminal after the write FAILED", s)
	}
	// The internal half still worked, and saying so is not a
	// contradiction: the subtree really is on the component clipboard.
	if ed.clip.node == nil {
		t.Error("a failed system write also lost the internal copy")
	}
}

// ---- pasting markup text in ----

func TestABracketedPasteOfMarkupInsertsIt(t *testing.T) {
	ed, _ := clipEditor(t)
	ed.sel = ed.doc()
	ed.pasteMarkup(`<Text Name="Pasted1">from outside</Text>`)

	n := findNode(ed, "Pasted1")
	if n == nil {
		t.Fatalf("pasted markup did not land: %v", names(ed))
	}
	if n.Body != "from outside" {
		t.Errorf("body = %q, want %q — the body did not survive the round trip", n.Body, "from outside")
	}
}

// A <Gooey> envelope is optional on the way IN: copying out of the CODE
// tab gives you one, copying an element out of a file does not, and
// refusing the second would refuse the common case.
func TestAPastedGooeyEnvelopeIsUnwrapped(t *testing.T) {
	ed, _ := clipEditor(t)
	ed.sel = ed.doc()
	ed.pasteMarkup("<Gooey>\n  <Text Name=\"Wrapped1\">body</Text>\n</Gooey>\n")

	if findNode(ed, "Wrapped1") == nil {
		t.Fatalf("an enveloped paste did not land: %v", names(ed))
	}
	if findNode(ed, "") != nil {
		// A <Gooey> node itself must never enter the document.
	}
	var sawGooey bool
	walkNode(ed.root, func(n *node) {
		if n.Elem == "Gooey" {
			sawGooey = true
		}
	})
	if sawGooey {
		t.Error("the <Gooey> envelope was inserted as an element")
	}
}

// Pasted text is arbitrary — prose as often as markup — and a designer
// that silently ignored the wrong thing would be indistinguishable from
// one where paste is broken.
func TestPastingNonMarkupSaysSoRatherThanDoingNothing(t *testing.T) {
	ed, _ := clipEditor(t)
	before := len(names(ed))
	ed.sel = ed.doc()
	ed.pasteMarkup("just some prose someone copied out of a chat window")

	if got := len(names(ed)); got != before {
		t.Errorf("non-markup text changed the document (%d names, was %d)", got, before)
	}
	s := ed.status.Get()
	if !strings.HasPrefix(s, "✗") || !strings.Contains(s, "not markup") {
		t.Fatalf("status = %q, want a message saying the text is not markup", s)
	}
	// And the message must not still call it a "seed" — nodeOf's errors
	// are written for this repo's own markup, and a user pasting from a
	// chat window has no idea what a seed is.
	if strings.Contains(s, "seed") {
		t.Errorf("status = %q leaks the word \"seed\" to a user who pasted text", s)
	}
}

func TestPastingAnEmptyClipboardSaysSo(t *testing.T) {
	ed, _ := clipEditor(t)
	ed.pasteClip()
	if s := ed.status.Get(); !strings.Contains(s, "clipboard is empty") {
		t.Errorf("status = %q, want it to say the clipboard is empty", s)
	}
}

// A paste of a subtree with a BODY must round-trip the body exactly.
// markup.BodyText owns the rule (a one-line body is verbatim, a
// multi-line one is trimmed), and a clipboard that restated it would
// quietly alter bodies with deliberate leading spaces.
func TestAPastedBodyKeepsItsLeadingSpaces(t *testing.T) {
	ed, _ := clipEditor(t)
	n := findNode(ed, "T1")
	n.Body = "  indented"
	ed.rebuild()

	ed.sel = n
	ed.copySelected()
	ed.pasteClip()

	if ed.sel.Body != "  indented" {
		t.Errorf("pasted body = %q, want %q — the clipboard trimmed a one-line body",
			ed.sel.Body, "  indented")
	}
}

// ---- routing ----

// A bracketed paste that the TREE consumed is not the editor's. The
// properties TextBox implements PasteHandler and takes a paste while
// focused, which is correct and must stay that way; the editor takes
// what bubbled out, the same rule App.handle uses for the quit key.
func TestTheEditorOnlyTakesAPasteTheTreeDeclined(t *testing.T) {
	ed, _ := clipEditor(t)
	var hooked func(input.Event, bool)
	ed.bindClipboardTo(func(fn func(input.Event, bool)) { hooked = fn }, func() {})
	if hooked == nil {
		t.Fatal("bindClipboard registered no AfterEvent hook")
	}

	before := len(names(ed))
	hooked(input.PasteOf(input.PasteEvent{Text: `<Text Name="Nope1">x</Text>`}), true)
	if got := len(names(ed)); got != before {
		t.Fatalf("the editor inserted a paste the tree had already consumed")
	}
	hooked(input.PasteOf(input.PasteEvent{Text: `<Text Name="Yes1">x</Text>`}), false)
	if findNode(ed, "Yes1") == nil {
		t.Fatal("the editor ignored a paste the tree declined")
	}
	// A key is not a paste.
	hooked(input.KeyOf(input.Rune('a')), false)
	if s := ed.status.Get(); strings.Contains(s, "not markup") {
		t.Error("a KEY event was routed into the markup paste path")
	}
}

// ---- damage ----

// A paste repaints what changed, not the tree. The number itself is not
// the claim — what is pinned is that a paste into a document costs a
// bounded, small repaint rather than a full one, so a regression that
// re-paints everything shows up here.
func TestPasteDoesNotRepaintTheWholeTree(t *testing.T) {
	ed, _ := clipEditor(t)
	full := gooey.NewComposer(mustBuildDoc(t, ed), 80, 24)
	_, all := full.Frame()
	if all < 3 {
		t.Fatalf("the baseline document paints %d components; this test cannot discriminate", all)
	}

	ed.sel = findNode(ed, "T1")
	ed.copySelected()
	ed.pasteClip()

	after := gooey.NewComposer(mustBuildDoc(t, ed), 80, 24)
	_, n := after.Frame()
	if n <= all {
		t.Fatalf("after a paste the document paints %d, was %d — the pasted element did not appear", n, all)
	}
	if n != all+1 {
		t.Errorf("a paste of ONE element changed the paint count by %d, want 1", n-all)
	}
}

func mustBuildDoc(t *testing.T, ed *editor) gooey.Component {
	t.Helper()
	w, err := markup.Build([]byte("<Gooey>\n"+ed.root.markup("  ")+"</Gooey>\n"), ed.docCtx)
	if err != nil {
		t.Fatalf("document does not build: %v", err)
	}
	return w
}

// ---- the Slots coupling ----

// parentIn (main.go) walks only Kids, never Slots, so parentOf returns
// nil for a node inside a property element. That is SAFE today and it is
// safe for exactly one reason: such a node cannot become the selection.
// mapNodes, outline and selectNext all walk Kids alone too, so nothing
// puts a slot interior in ed.nodeOf, in the outline, or under ctrl+n.
//
// This test pins that precondition rather than "fixing" parentIn,
// because teaching parentIn about Slots ON ITS OWN would make things
// WORSE, not better: deleteSelected searches p.Kids for the node and
// would still find nothing (silently doing nothing, exactly as now),
// and addTarget would start returning a slot owner as an append target,
// so a palette click would push a child into an element whose content
// is a slot. The three have to learn about slots together or not at all.
//
// So the day someone makes slot interiors selectable, THIS is what goes
// red, and its message says what else has to change.
func TestSlotInteriorsAreNotSelectableWhichIsWhatMakesParentInSafe(t *testing.T) {
	ed, _ := clipEditor(t)
	spec := ed.specFor("ItemsView")
	if spec.Seed == "" {
		t.Skip("no <ItemsView> in this build's palette")
	}
	n, err := ed.seed(spec, "IV1")
	if err != nil {
		t.Fatalf("seed <ItemsView>: %v", err)
	}
	n.Attrs["Name"] = "IV1"
	ed.doc().Kids = append(ed.doc().Kids, n)
	ed.rebuild()
	if len(n.Slots) == 0 {
		t.Fatal("<ItemsView> seeded no slot; this test cannot discriminate")
	}

	// Every node reachable ONLY through Slots.
	var inSlots []*node
	for _, s := range n.Slots {
		walkNode(s, func(k *node) { inSlots = append(inSlots, k) })
	}
	outline := ed.outline()
	for _, k := range inSlots {
		if p := ed.parentOf(k); p != nil {
			t.Fatalf("parentOf now answers for a slot interior (<%s>). If that is deliberate, "+
				"deleteSelected must learn to remove from Slots and addTarget must stop "+
				"returning a slot owner as an append target — see this test's comment", k.Elem)
		}
		for comp, mapped := range ed.nodeOf {
			if mapped == k {
				t.Fatalf("a slot interior (<%s>) is mapped to component %T, so a click can select it "+
					"— and parentOf returns nil for it, which makes delete and drag silent no-ops",
					k.Elem, comp)
			}
		}
	}
	// The outline is the other selection route. It shows Name, so a slot
	// child with a name would be visible and look selectable.
	if ed.takesBody("Text") && strings.Contains(outline, "IV1") {
		// IV1 itself is a Kid and SHOULD be listed; its slot must not be.
		for _, k := range inSlots {
			if nm := k.Attrs["Name"]; nm != "" && strings.Contains(outline, nm) {
				t.Fatalf("the outline lists slot interior %q, which invites a selection "+
					"parentOf cannot resolve", nm)
			}
		}
	}
}

// TestPastingIntoATabsWrapsTheNodeInATab is the paste half of the hole
// addplan.go closed for the palette.
//
// The two gestures reach the same illegal insert by different routes, and
// fixing one did not fix the other: addSelected consults planAdd and
// builds the wrapper, while insertSubtree called addTarget and appended
// straight into the landing node. Pasting anything but a <Tab> into a
// <Tabs> therefore wrote a child the container declares it does not take,
// the rebuild failed, docRoot went nil, and click-to-select died for the
// whole document.
//
// Neither branch could see this on its own — clipboard.go and addplan.go
// arrived from different PRs and never shared a tree until the merge.
func TestPastingIntoATabsWrapsTheNodeInATab(t *testing.T) {
	ed, tabs := tabsFixture(t)
	src := tabs.Kids[0].Kids[0]
	if src.Elem != "Text" {
		t.Fatalf("fixture changed: the tab holds a <%s>, expected the <Text>", src.Elem)
	}
	ed.sel = src
	ed.copySelected()

	ed.sel = tabs
	before := len(tabs.Kids)
	ed.pasteClip()

	if len(tabs.Kids) != before+1 {
		t.Fatalf("the <Tabs> has %d children, was %d: the paste did not land in it (status %q)",
			len(tabs.Kids), before, ed.status.Get())
	}
	added := tabs.Kids[len(tabs.Kids)-1]
	if added.Elem != "Tab" {
		t.Fatalf("the <Tabs> took a <%s> directly; it declares Only:[\"Tab\"] and this "+
			"document would not build", added.Elem)
	}
	if len(added.Kids) != 1 || added.Kids[0].Elem != "Text" {
		t.Fatalf("the new <Tab> holds %d children (%v), want the one <Text> that was pasted",
			len(added.Kids), kidElems(added))
	}
	// The selection is the PASTED node, not the scaffolding — the same rule
	// addSelected follows, for the same reason: the properties grid must
	// show what the user pasted.
	if ed.sel != added.Kids[0] {
		t.Errorf("after the paste the selection is %s, want the pasted <Text>", nodeLabel(ed.sel))
	}
	// The point of all of it: the document still builds.
	if ed.docRoot == nil {
		t.Errorf("the document stopped building after the paste: %s", ed.status.Get())
	}
	if s := ed.status.Get(); strings.HasPrefix(s, "\u2717") {
		t.Errorf("after a paste the build failed: %s", s)
	}
}

// TestPastingATabIntoATabsDoesNotWrapItInAnother is the must-say-NO arm.
// A wrapper that always wraps is as wrong as one that never does, and only
// this arm can tell the two apart: the pasted element IS the permitted
// child, so there is nothing to build around it.
func TestPastingATabIntoATabsDoesNotWrapItInAnother(t *testing.T) {
	ed, tabs := tabsFixture(t)
	ed.sel = tabs.Kids[0]
	ed.copySelected()

	ed.sel = tabs
	before := len(tabs.Kids)
	ed.pasteClip()

	if len(tabs.Kids) != before+1 {
		t.Fatalf("the <Tabs> has %d children, was %d (status %q)",
			len(tabs.Kids), before, ed.status.Get())
	}
	added := tabs.Kids[len(tabs.Kids)-1]
	if added.Elem != "Tab" {
		t.Fatalf("pasted a <Tab> and got a <%s>", added.Elem)
	}
	if len(added.Kids) == 1 && added.Kids[0].Elem == "Tab" {
		t.Fatalf("the pasted <Tab> was wrapped in another <Tab>: the wrapper fired where " +
			"the element was already the permitted child")
	}
	if ed.docRoot == nil {
		t.Errorf("the document stopped building after the paste: %s", ed.status.Get())
	}
}
