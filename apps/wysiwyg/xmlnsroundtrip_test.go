package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/markup"
)

// handlerNS registers an inert provider for uri and returns it. INERT is
// deliberate: every assertion below is about whether the document LOADS,
// and a provider that did something would make a green result mean two
// things. Registration is by URI, so this also removes "no handler is
// registered" as an alternative reading of a refusal — what is left is
// the prefix, which is what #472 is about.
func handlerNS(t *testing.T, uri string) {
	t.Helper()
	markup.RegisterHandlers(uri, markup.HandlerFunc(
		func(c *markup.Call) (gooey.Command, error) {
			return gooey.Command(func() {}), nil
		}))
	t.Cleanup(func() { markup.RegisterHandlers(uri, nil) })
}

// TestAHandlerNamespaceSurvivesOpeningADocument is #472's own
// reproduction, driven through the path a user takes: a document on disk,
// opened through the file browser.
//
// The declaration is on the <Gooey> ENVELOPE, which is where a saved
// document carries it and where openWorkspaceFile discards it — the
// envelope is unwrapped and the user's root becomes the document, so
// anything the envelope held is gone before rebuild ever runs.
func TestAHandlerNamespaceSurvivesOpeningADocument(t *testing.T) {
	const uri = "urn:gooey:test:472:open"
	handlerNS(t, uri)

	root := workspaceFixture(t)
	doc := `<Gooey xmlns:t="` + uri + `">` + "\n" +
		`  <Canvas Name="Root">` + "\n" +
		`    <Button Name="B" Content="go" Click="{{t:Fire}}"/>` + "\n" +
		`  </Canvas>` + "\n" +
		`</Gooey>` + "\n"
	if err := os.WriteFile(filepath.Join(root, "handler.gooey"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}

	ed, _ := buildPage(t)
	// WIRED, as main() wires the running editor. Without this the load
	// fails on the Dispatcher (#462) instead of on the namespace, which
	// would make a red result here mean the wrong thing.
	ed.setDispatcher(gooey.NewDispatcher())
	ed.setWorkspace(root)
	ed.openWorkspaceFile("handler.gooey")

	if got := ed.status.Get(); !strings.HasPrefix(got, "✓") {
		t.Fatalf("opening a document with a handler namespace reports %q, want a build", got)
	}
	if ed.docRoot == nil {
		t.Error("the status says it builds but no tree was swapped in")
	}
	// THE SOURCE, not just the screen: a canvas that built from a
	// declaration the save would drop is a document that dies on reopen.
	if src := ed.source.Get(); !strings.Contains(src, uri) {
		t.Errorf("the rebuilt source no longer declares the namespace:\n%s", src)
	}
}

// TestReopeningTheRebuiltSourceIsStable is the half a single open cannot
// see. Carrying a declaration through ONE rebuild is not a round trip if
// the spelling it comes out in is one the reader then refuses, or one
// that moves again on the next pass — a document that oscillates would
// pass the test above on every individual open and still corrupt a file
// edited twice.
func TestReopeningTheRebuiltSourceIsStable(t *testing.T) {
	const uri = "urn:gooey:test:472:stable"
	handlerNS(t, uri)

	root := workspaceFixture(t)
	doc := `<Gooey xmlns:t="` + uri + `">` + "\n" +
		`  <Canvas Name="Root">` + "\n" +
		`    <Button Name="B" Content="go" Click="{{t:Fire}}"/>` + "\n" +
		`  </Canvas>` + "\n" +
		`</Gooey>` + "\n"
	if err := os.WriteFile(filepath.Join(root, "handler.gooey"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}

	ed, _ := buildPage(t)
	// WIRED, as main() wires the running editor. Without this the load
	// fails on the Dispatcher (#462) instead of on the namespace, which
	// would make a red result here mean the wrong thing.
	ed.setDispatcher(gooey.NewDispatcher())
	ed.setWorkspace(root)
	ed.openWorkspaceFile("handler.gooey")
	first := ed.source.Get()

	// THROUGH THE SAVE PATH, not through ed.source. This wrote
	// ed.source.Get() to a second file with os.WriteFile until review of
	// #501 pointed out that a save does not write ed.source at all:
	// saveOpenFile builds its OWN "<Gooey>\n" + doc + "</Gooey>\n"
	// string, independently of the one in rebuild,
	// and nothing crossed the two. So of the four legs this PR claims —
	// read, write, save, reopen — the save was the one no test touched,
	// and an edit to either literal that dropped the declaration on the
	// way to disk would have left the suite green and the user's file
	// without its xmlns.
	if err := ed.saveOpenFile(); err != nil {
		t.Fatalf("saving the open document: %v", err)
	}
	onDisk, err := os.ReadFile(filepath.Join(root, "handler.gooey"))
	if err != nil {
		t.Fatal(err)
	}
	if string(onDisk) != first {
		t.Errorf("the bytes a save wrote are not the source the editor is "+
			"showing, so the two envelope literals have drifted.\nrebuild:\n%s\n"+
			"saveOpenFile:\n%s", first, onDisk)
	}

	ed.openWorkspaceFile("handler.gooey")
	if got := ed.status.Get(); !strings.HasPrefix(got, "✓") {
		t.Fatalf("reopening the document the editor itself SAVED reports %q:\n%s",
			got, first)
	}
	second := ed.source.Get()
	if second != first {
		t.Errorf("the document moved on its second pass.\nfirst:\n%s\nsecond:\n%s",
			first, second)
	}
	// AND WHERE IT LANDS, not only that it survived. The declaration
	// MOVES on the first save: it is read off the <Gooey> envelope and
	// re-emitted on the user's root, because the envelope is not a node.
	// That is stable and consistent with an editor that regenerates the
	// whole file anyway, but nothing said so — asserting it makes a
	// future change back to envelope-emission a decision somebody made
	// rather than a diff nobody read. Raised in review of #501.
	if !strings.Contains(second, `<Canvas Name="Root" xmlns:t="`+uri+`">`) {
		t.Errorf("the declaration is not on the user's root element, where the "+
			"carry-down puts it. If it moved back onto <Gooey>, that is a "+
			"deliberate change and this assertion is the place to record "+
			"it:\n%s", second)
	}
}

// TestAnUndeclaredPrefixIsNotReportedAsAParentingFault is the message
// half of the namespace work, and the case it covers is the ordinary
// one: copying a single element out of a document leaves its
// declaration behind on the root.
//
// reconcileNamespaces has nothing to compare — the pasted node declares
// nothing — so the paste reaches insertSubtree's rebuild backstop, which
// prefixed every refusal with a parenting claim it cannot have
// established. The backstop knows a rebuild failed and nothing about
// whose fault it is; here the fault is the pasted node's own content.
// Measured before the fix:
//
//	✗ <Button> does not go inside <Canvas>: markup: <Button
//	  Click="{{t:Fire}}">: markup: undeclared namespace prefix "t"
//
// BOTH DIRECTIONS, because dropping the clause entirely would also pass
// an assertion that only forbids the wrong noun: the real cause has to
// survive into the message, and the two elements have to still be named
// so the author knows which paste failed. Raised in review of #501.
func TestAnUndeclaredPrefixIsNotReportedAsAParentingFault(t *testing.T) {
	const uri = "urn:gooey:test:501:undeclared"
	handlerNS(t, uri)

	root := workspaceFixture(t)
	doc := `<Gooey>` + "\n" +
		`  <Canvas Name="Root"/>` + "\n" +
		`</Gooey>` + "\n"
	if err := os.WriteFile(filepath.Join(root, "bare.gooey"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}

	ed, _ := buildPage(t)
	ed.setDispatcher(gooey.NewDispatcher())
	ed.setWorkspace(root)
	ed.openWorkspaceFile("bare.gooey")
	if got := ed.status.Get(); !strings.HasPrefix(got, "✓") {
		t.Fatalf("opening the fixture reports %q, want a build", got)
	}

	// NO DECLARATION, which is what a copy of one element out of a
	// document looks like.
	ed.pasteMarkup(`<Button Name="Pasted" Content="go" Click="{{t:Fire}}"/>`)

	got := ed.status.Get()
	if !strings.HasPrefix(got, "✗") {
		t.Fatalf("a paste using an undeclared prefix reported %q; it cannot "+
			"build, so this test is not looking at the refusal it is about", got)
	}
	if strings.Contains(got, "does not go inside") {
		t.Errorf("a namespace failure is reported as a parenting failure. "+
			"<Button> goes inside <Canvas> perfectly well; the backstop that "+
			"wrote this knows only that a rebuild failed, and the cause was "+
			"after the colon all along: %s", got)
	}
	if !strings.Contains(got, `"t"`) {
		t.Errorf("the refusal does not name the prefix that is missing, which "+
			"is the only thing the author can act on: %s", got)
	}
	if !strings.Contains(got, "<Button>") || !strings.Contains(got, "<Canvas>") {
		t.Errorf("the refusal names neither the pasted element nor where it "+
			"was going, so an author with several panes open cannot tell "+
			"which paste failed: %s", got)
	}
}

// TestTheEnvelopesOwnAttributesSurviveASave is the leg the round trip
// above does not cover, and the one that was losing user data.
//
// Only namespace declarations came off the <Gooey> envelope; everything
// else on it went with it, because the envelope is not a node and the
// three places that re-emit it wrote a bare "<Gooey>" literal. Graphics
// is not decorative — it forces the image protocol (markup.Graphics) —
// and apps/dynamic-activities/zoom.gooey carries Graphics="halfblock"
// with that app's own README calling the declaration load-bearing. So
// opening that file in the designer, changing one attribute and saving
// took the graphics mode away under a "✓ saved". Measured before the
// fix:
//
//	input:  <Gooey xmlns="wonderforge.io/gooey/2026" Graphics="sixel">
//	saved:  <Gooey>
//	          <Canvas Name="Root" xmlns="wonderforge.io/gooey/2026">
//
// BOTH HALVES, because either alone passes against the defect. Graphics
// must survive, AND the default xmlns must stay on the envelope rather
// than being relocated onto the user's root — a rewrite no documented
// example shows and that markup.parse ignores anyway, since it skips a
// plain xmlns without recording it. Raised in review of #501.
func TestTheEnvelopesOwnAttributesSurviveASave(t *testing.T) {
	root := workspaceFixture(t)
	doc := `<Gooey xmlns="wonderforge.io/gooey/2026" Graphics="halfblock">` + "\n" +
		`  <Canvas Name="Root">` + "\n" +
		`    <Button Name="B" Content="go"/>` + "\n" +
		`  </Canvas>` + "\n" +
		`</Gooey>` + "\n"
	if err := os.WriteFile(filepath.Join(root, "gfx.gooey"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}

	ed, _ := buildPage(t)
	ed.setDispatcher(gooey.NewDispatcher())
	ed.setWorkspace(root)
	ed.openWorkspaceFile("gfx.gooey")
	if got := ed.status.Get(); !strings.HasPrefix(got, "✓") {
		t.Fatalf("opening the fixture reports %q, want a build", got)
	}
	if err := ed.saveOpenFile(); err != nil {
		t.Fatalf("saving the open document: %v", err)
	}
	onDisk, err := os.ReadFile(filepath.Join(root, "gfx.gooey"))
	if err != nil {
		t.Fatal(err)
	}
	saved := string(onDisk)
	if !strings.Contains(saved, `Graphics="halfblock"`) {
		t.Errorf("a save dropped the envelope's Graphics attribute, which forces "+
			"the image protocol — the file now renders in whatever the terminal "+
			"defaults to:\n%s", saved)
	}
	envelope, below, _ := strings.Cut(saved, "\n")
	if !strings.Contains(envelope, `xmlns="wonderforge.io/gooey/2026"`) {
		t.Errorf("the default declaration is no longer on <Gooey>, where every "+
			"documented example and every .gooey in this tree puts it:\n%s", saved)
	}
	if strings.Contains(below, `xmlns="`) {
		t.Errorf("the default declaration was relocated onto the user's root. "+
			"markup.parse skips a plain xmlns outright, so the move has no "+
			"effect on load and produces a diff nobody asked for:\n%s", saved)
	}

	// AND IT IS STABLE, which is what makes the first save's output the
	// file rather than one step of an oscillation.
	first := ed.source.Get()
	ed.openWorkspaceFile("gfx.gooey")
	if second := ed.source.Get(); second != first {
		t.Errorf("the document moved on its second pass.\nfirst:\n%s\nsecond:\n%s",
			first, second)
	}
}

// TestARebuildCarriesAHandlerNamespaceAndPinsTheDispatcher replaces
// TestARebuildCannotCarryAHandlerNamespaceYet, which was written as a
// latch precisely so that closing #472 would turn it red and hand its
// author this instruction: with the namespace surviving a rebuild, a
// dispatcher assertion through the path the running editor takes has
// become possible, and should replace the latch.
//
// TWO ARMS, and they must fail for DIFFERENT reasons or this proves
// nothing. Without a Dispatcher the load is refused — that is #462's
// wiring, asserted here through ed.rebuild rather than through a direct
// markup.Build, which is what no test could do while the namespace was
// dropped first. With one, the same document builds. The refusal is
// matched on its own text so that an arm failing on the NAMESPACE again
// cannot be read as the dispatcher being pinned.
func TestARebuildCarriesAHandlerNamespaceAndPinsTheDispatcher(t *testing.T) {
	const uri = "urn:gooey:test:472:rebuild"
	handlerNS(t, uri)

	seed := func(ed *editor) {
		ed.doc().Attrs["xmlns:t"] = uri
		ed.doc().Kids = append(ed.doc().Kids, &node{
			Elem: "Button",
			Attrs: map[string]string{
				"Name": "B", "Content": "go", "Click": "{{t:Fire}}",
			},
		})
		ed.rebuild()
	}

	unwired := newEditor(editorFS())
	seed(unwired)
	got := unwired.status.Get()
	if strings.Contains(got, "undeclared namespace prefix") {
		t.Fatalf("the rebuild still drops the declaration: %q — #472 is not closed, "+
			"and neither arm of this test is measuring the Dispatcher", got)
	}
	if !strings.Contains(got, "Dispatcher") {
		t.Errorf("an editor with no Dispatcher rebuilt a handler document and reported %q.\n"+
			"This arm exists to fail on the MISSING DISPATCHER (#462); if it now builds, "+
			"docCtx is being wired from somewhere this test does not see", got)
	}
	if unwired.docRoot != nil {
		t.Error("the canvas built a document whose load it reported as failed")
	}

	wired := newEditor(editorFS())
	wired.setDispatcher(gooey.NewDispatcher())
	seed(wired)
	if got := wired.status.Get(); !strings.HasPrefix(got, "✓") {
		t.Fatalf("a wired editor rebuilding a handler document reports %q", got)
	}
	if wired.docRoot == nil {
		t.Error("the status says it builds but no tree was swapped in")
	}
}

// TestOnlyARealDeclarationComesDownFromTheEnvelope pins the carry-down's
// SET, which is the half a round-trip test cannot see.
//
// nodeOf writes exactly two shapes — "xmlns" and "xmlns:"+local — so the
// reader in openWorkspaceFile has exactly two to accept. It tested
// HasPrefix(k, "xmlns"), which also matches an ordinary attribute
// spelled xmlnsFoo: that would be copied onto the user's root, where the
// canvas reports it as an unknown attribute of the CHILD element rather
// than as the envelope-level mistake it is. Nothing in the suite was red
// for the loose form, which is why this exists. Raised in review of #501.
func TestOnlyARealDeclarationComesDownFromTheEnvelope(t *testing.T) {
	const uri = "urn:gooey:test:501:set"
	handlerNS(t, uri)

	root := workspaceFixture(t)
	doc := `<Gooey xmlns:t="` + uri + `" xmlnsFoo="not a declaration">` + "\n" +
		`  <Canvas Name="Root">` + "\n" +
		`    <Button Name="B" Content="go" Click="{{t:Fire}}"/>` + "\n" +
		`  </Canvas>` + "\n" +
		`</Gooey>` + "\n"
	if err := os.WriteFile(filepath.Join(root, "set.gooey"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}

	ed, _ := buildPage(t)
	ed.setDispatcher(gooey.NewDispatcher())
	ed.setWorkspace(root)
	ed.openWorkspaceFile("set.gooey")

	// NON-VACUITY: the real declaration beside it must have come down,
	// or this test passes for a carry-down that copies nothing at all.
	if src := ed.source.Get(); !strings.Contains(src, uri) {
		t.Fatalf("the declaration did not come down, so nothing here is "+
			"measuring which keys do:\n%s", src)
	}
	// ON THE ENVELOPE, WHERE THE AUTHOR WROTE IT, and nowhere else. The
	// envelope's own attributes are preserved since review of #501's
	// round 7, so the claim is no longer "xmlnsFoo appears nowhere" —
	// that would now be a demand to delete somebody's attribute. It is
	// that the carry-down did not treat it as a declaration: moved onto
	// the user's root, the canvas reports it as an unknown attribute of
	// the CHILD rather than as the envelope-level mistake it is.
	src := ed.source.Get()
	_, below, _ := strings.Cut(src, "\n")
	if strings.Contains(below, "xmlnsFoo") {
		t.Errorf("an ordinary attribute spelled xmlnsFoo was carried onto the "+
			"user's root as if it were a declaration. carryDeclarations must "+
			"accept the one shape that moves, \"xmlns:\"+local, and nothing "+
			"else:\n%s", src)
	}
	if !strings.Contains(src, "xmlnsFoo") {
		t.Errorf("xmlnsFoo was dropped from the document entirely; an attribute "+
			"the author wrote on the envelope belongs on the envelope, right "+
			"or wrong:\n%s", src)
	}
}

// TestTheUnprefixedDeclarationStaysOnTheEnvelope covers the SECOND of
// the two shapes nodeOf writes, which every other test in this file
// misses.
//
// nodeOf's attribute loop accepts exactly "xmlns" and "xmlns:"+local,
// and the decoy in TestOnlyARealDeclarationComesDownFromTheEnvelope pins
// what is REJECTED — leaving the plain form implemented twice and
// asserted nowhere. It is not cosmetic: Go's decoder applies a default
// namespace to ELEMENT names, so keeping one sets Element.Space for the
// whole subtree, and markup compares that against XNamespace (build's
// `e.Space == markup.XNamespace` arm, markup/markup.go).
//
// WHICH ELEMENT CARRIES IT is the assertion, not merely that the URI
// appears. This was named "…IsCarriedToo" and checked only
// Contains(src, `xmlns="urn:x"`) — which passes under either rule, so it
// could not see the narrowing that made carryDeclarations prefixed-only
// and left the plain declaration where the author wrote it. A test whose
// name describes removed behaviour and whose assertion cannot tell the
// two apart is two claims rotting at once. Raised in review of #501.
func TestTheUnprefixedDeclarationStaysOnTheEnvelope(t *testing.T) {
	root := workspaceFixture(t)
	doc := `<Gooey xmlns="urn:x">` + "\n" +
		`  <Canvas Name="Root">` + "\n" +
		`    <Button Name="B" Content="go"/>` + "\n" +
		`  </Canvas>` + "\n" +
		`</Gooey>` + "\n"
	if err := os.WriteFile(filepath.Join(root, "plain.gooey"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}

	ed, _ := buildPage(t)
	ed.setDispatcher(gooey.NewDispatcher())
	ed.setWorkspace(root)
	ed.openWorkspaceFile("plain.gooey")

	if got := ed.status.Get(); !strings.HasPrefix(got, "✓") {
		t.Fatalf("a document with a default namespace does not open: %q", got)
	}
	src := ed.source.Get()
	if !strings.Contains(src, `xmlns="urn:x"`) {
		t.Errorf("the unprefixed declaration did not survive the round trip. It "+
			"is the other half of the set nodeOf writes, and the reader must "+
			"accept both:\n%s", src)
	}
	envelope, below, _ := strings.Cut(src, "\n")
	if !strings.Contains(envelope, `xmlns="urn:x"`) {
		t.Errorf("the default declaration is not on <Gooey>, where the author "+
			"wrote it:\n%s", src)
	}
	if strings.Contains(below, `xmlns="urn:x"`) {
		t.Errorf("the default declaration was carried down onto the user's root "+
			"as well. Only prefixed declarations move; markup.parse skips a plain "+
			"xmlns outright, so the copy buys nothing but a diff:\n%s", src)
	}
}

// TestARefusedOpenLeavesTheOpenDocumentAlone is the second path to the
// envelope-stripping fault TestTheEnvelopesOwnAttributesSurviveASave
// closed on the first.
//
// That test drives open → save over ONE file, so it cannot see a field
// cleared by an open that never completed. ed.envAttrs was set to nil at
// the top of openWorkspaceFile and the two-root refusal returns without
// replacing ed.root.Kids, ed.openPath or re-running ed.rebuild — so
// clicking the wrong file in the browser stripped the Graphics off the
// document that was still open and still displayed, and the next save
// wrote a bare <Gooey>. The trigger is not an edit the user made.
// Raised in review of #501.
func TestARefusedOpenLeavesTheOpenDocumentAlone(t *testing.T) {
	root := workspaceFixture(t)
	good := `<Gooey xmlns="wonderforge.io/gooey/2026" Graphics="halfblock">` + "\n" +
		`  <Canvas Name="Root">` + "\n" +
		`    <Button Name="B" Content="go"/>` + "\n" +
		`  </Canvas>` + "\n" +
		`</Gooey>` + "\n"
	if err := os.WriteFile(filepath.Join(root, "gfx.gooey"), []byte(good), 0o644); err != nil {
		t.Fatal(err)
	}
	twoRoots := `<Gooey>` + "\n" +
		`  <Canvas Name="A"/>` + "\n" +
		`  <Canvas Name="B"/>` + "\n" +
		`</Gooey>` + "\n"
	if err := os.WriteFile(filepath.Join(root, "two.gooey"), []byte(twoRoots), 0o644); err != nil {
		t.Fatal(err)
	}

	ed, _ := buildPage(t)
	ed.setDispatcher(gooey.NewDispatcher())
	ed.setWorkspace(root)
	ed.openWorkspaceFile("gfx.gooey")
	if got := ed.status.Get(); !strings.HasPrefix(got, "✓") {
		t.Fatalf("opening the fixture reports %q, want a build", got)
	}

	ed.openWorkspaceFile("two.gooey")
	if got := ed.status.Get(); !strings.Contains(got, "exactly one root element") {
		t.Fatalf("the two-root file reports %q, so this test is no longer "+
			"exercising a refused open", got)
	}
	if got := ed.openPath.Get(); got != "gfx.gooey" {
		t.Fatalf("a refused open moved openPath to %q; the rest of this test "+
			"assumes the first document is still the open one", got)
	}

	if err := ed.saveOpenFile(); err != nil {
		t.Fatalf("saving the still-open document: %v", err)
	}
	onDisk, err := os.ReadFile(filepath.Join(root, "gfx.gooey"))
	if err != nil {
		t.Fatal(err)
	}
	if saved := string(onDisk); !strings.Contains(saved, `Graphics="halfblock"`) {
		t.Errorf("a REFUSED open of another file stripped this document's "+
			"envelope. The CODE tab still shows it:\n%s\nand the save wrote:\n%s",
			ed.source.Get(), saved)
	}
}

// TestAFolderChangeLeavesTheOpenDocumentsEnvelopeAlone is the third
// route to the same envelope loss, and the one the A1 fix's own stated
// invariant forbade.
//
// That fix says the field moves with ed.root.Kids now, and no partial
// path can separate the two. setWorkspace and closeWorkspace each zeroed
// ed.envAttrs and touched neither ed.root.Kids nor the canvas, so the
// document stayed on screen with its envelope gone: open a file with
// Graphics="halfblock", change or close the folder, make any edit, and
// the CODE tab shows a bare <Gooey> for a document nobody changed.
// Nothing reaches disk wrong, because a folder change clears openPath
// and canSave gates on it — which is the only reason this was a display
// fault rather than a data one. Raised in review of #501.
//
// BOTH arms are needed and neither substitutes for the other: the two
// clears were separate lines in separate files, so a test that only
// closed the folder passed with setWorkspace's copy still in place.
func TestAFolderChangeLeavesTheOpenDocumentsEnvelopeAlone(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(ed *editor, elsewhere string)
	}{
		{"close", func(ed *editor, _ string) { ed.closeWorkspace() }},
		{"switch", func(ed *editor, elsewhere string) { ed.setWorkspace(elsewhere) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := workspaceFixture(t)
			doc := `<Gooey xmlns="wonderforge.io/gooey/2026" Graphics="halfblock">` + "\n" +
				`  <Canvas Name="Root">` + "\n" +
				`    <Button Name="B" Content="go"/>` + "\n" +
				`  </Canvas>` + "\n" +
				`</Gooey>` + "\n"
			if err := os.WriteFile(filepath.Join(root, "gfx.gooey"), []byte(doc), 0o644); err != nil {
				t.Fatal(err)
			}

			ed, _ := buildPage(t)
			ed.setDispatcher(gooey.NewDispatcher())
			ed.setWorkspace(root)
			ed.openWorkspaceFile("gfx.gooey")
			if got := ed.status.Get(); !strings.HasPrefix(got, "✓") {
				t.Fatalf("opening the fixture reports %q, want a build", got)
			}

			tc.change(ed, workspaceFixture(t))
			// THE REBUILD IS THE OBSERVABLE. The source property only
			// moves when something rebuilds, so a test that read it
			// straight after the folder change would pass against the
			// defect on a stale string.
			ed.rebuild()
			if src := ed.source.Get(); !strings.Contains(src, `Graphics="halfblock"`) {
				t.Errorf("the folder change stripped the still-open document's "+
					"envelope; the next rebuild writes a bare <Gooey> for a "+
					"document nobody edited:\n%s", src)
			}
		})
	}
}

// TestAnAmpersandInAnAttributeSurvivesASave is the round trip through
// the character the emitter was not escaping.
//
// Attribute values went out through %q, which is GO quoting: a value the
// loader accepts came back as raw `&`, the canvas refused its own
// rebuild with "markup: no root element", and the save wrote a file
// whose reopen fails on "invalid character entity". `"` is the same
// class. n.Body was already escaped through xml.EscapeText with a
// comment explaining why; the attributes beside it were not. Raised in
// review of #501.
func TestAnAmpersandInAnAttributeSurvivesASave(t *testing.T) {
	root := workspaceFixture(t)
	doc := `<Gooey xmlns="wonderforge.io/gooey/2026">` + "\n" +
		`  <Canvas Name="Root">` + "\n" +
		`    <Button Name="B" Content="Save &amp; Exit &quot;now&quot;"/>` + "\n" +
		`  </Canvas>` + "\n" +
		`</Gooey>` + "\n"
	if err := os.WriteFile(filepath.Join(root, "amp.gooey"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}

	ed, _ := buildPage(t)
	ed.setDispatcher(gooey.NewDispatcher())
	ed.setWorkspace(root)
	ed.openWorkspaceFile("amp.gooey")
	if got := ed.status.Get(); !strings.HasPrefix(got, "✓") {
		t.Fatalf("a document with an escaped ampersand does not open: %q", got)
	}
	if err := ed.saveOpenFile(); err != nil {
		t.Fatalf("saving the open document: %v", err)
	}

	// REOPENED FROM DISK, so what is pinned is the FILE and not the
	// in-memory source. The open above already catches the emitter —
	// ed.rebuild serialises the model and builds THAT, so an unescaped
	// `&` reports "markup: no root element" before any save — and this
	// arm is what says the bytes on disk are loadable by anything else.
	ed.openWorkspaceFile("amp.gooey")
	if got := ed.status.Get(); !strings.HasPrefix(got, "✓") {
		onDisk, _ := os.ReadFile(filepath.Join(root, "amp.gooey"))
		t.Fatalf("the designer cannot reopen what it just wrote: %q\nfile:\n%s",
			got, onDisk)
	}
	if src := ed.source.Get(); !strings.Contains(src, `Save &amp; Exit &#34;now&#34;`) {
		t.Errorf("the attribute value did not round trip as XML:\n%s", src)
	}
}

// TestANewlineInAnAttributeSurvivesASaveAsACharacterReference is the
// OTHER half of #509, and it is the half the ampersand arm above cannot
// see. `&` fails LOUDLY — the document stops parsing — so any emitter
// that escapes it at all is caught by the sibling. A newline fails
// QUIETLY: under the `%q` this replaced, "line1\nline2" went to disk as
// a literal backslash-n, which is valid XML, reopens without complaint,
// and comes back as a four-character string that is not what the author
// wrote.
//
// THE ON-DISK SPELLING IS ASSERTED, NOT JUST THE ROUND TRIP, and that
// is not belt-and-braces — it is the only thing here that can fail.
// Go's encoding/xml does NOT do XML attribute-value normalisation:
// measured, a LITERAL newline inside an attribute comes back from
// xml.Unmarshal as a newline, where a conforming parser must hand back
// a space (XML 1.0 §3.3.3). So an emitter that escaped `&<>"` by hand
// and left whitespace alone would round trip perfectly through this
// editor and write a file that every other reader mis-reads. A test
// that only opened what it saved would certify that emitter. The
// character reference is what makes the bytes portable, so the bytes
// are what this reads.
//
// TAB TOO, in the same value rather than a second arm: xml.EscapeText
// covers \n, \r and \t together, and a fixture holding one of the
// three pins the family the same way two strings of equal COLUMN width
// pin a rune-vs-cell count. Raised in review of #501 and filed as #509.
func TestANewlineInAnAttributeSurvivesASaveAsACharacterReference(t *testing.T) {
	const want = "line1\nline2\tx"
	const onDiskWant = `Content="line1&#xA;line2&#x9;x"`

	root := workspaceFixture(t)
	doc := `<Gooey xmlns="wonderforge.io/gooey/2026">` + "\n" +
		`  <Canvas Name="Root">` + "\n" +
		`    <Button Name="B" ` + onDiskWant + `/>` + "\n" +
		`  </Canvas>` + "\n" +
		`</Gooey>` + "\n"
	path := filepath.Join(root, "nl.gooey")
	if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}

	ed, _ := buildPage(t)
	ed.setDispatcher(gooey.NewDispatcher())
	ed.setWorkspace(root)
	ed.openWorkspaceFile("nl.gooey")
	if got := ed.status.Get(); !strings.HasPrefix(got, "✓") {
		t.Fatalf("a document with an escaped newline does not open: %q", got)
	}
	if n := findNode(ed, "B"); n == nil || n.Attrs["Content"] != want {
		t.Fatalf("the loader handed the model %q, want %q — the rest of this "+
			"test is about what the EMITTER does with that value, so it has to "+
			"start from the right one", nodeAttr(n, "Content"), want)
	}
	if err := ed.saveOpenFile(); err != nil {
		t.Fatalf("saving the open document: %v", err)
	}

	onDisk, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(onDisk), onDiskWant) {
		t.Errorf("the saved file does not carry the newline as a character "+
			"reference, so a conforming parser will not hand back what the "+
			"author wrote:\nwant a value spelled %s\nfile:\n%s", onDiskWant, onDisk)
	}

	ed.openWorkspaceFile("nl.gooey")
	if got := ed.status.Get(); !strings.HasPrefix(got, "✓") {
		t.Fatalf("the designer cannot reopen what it just wrote: %q\nfile:\n%s",
			got, onDisk)
	}
	if n := findNode(ed, "B"); n == nil || n.Attrs["Content"] != want {
		t.Errorf("after a save and a reopen the value is %q, want %q",
			nodeAttr(n, "Content"), want)
	}
}

// nodeAttr is nil-tolerant so the failure messages above can name what
// they got without a second nil test at every site.
func nodeAttr(n *node, k string) string {
	if n == nil {
		return "<no such node>"
	}
	return n.Attrs[k]
}

// TestABothLevelsDeclarationKeepsTheEnvelopesCopy pins the complement
// envelopeAttrs claims against carryDeclarations.
//
// carryDeclarations skips a prefix the root already declares;
// envelopeAttrs dropped every xmlns: unconditionally. A file declaring
// one prefix at both levels therefore lost the envelope's copy outright.
// The resolved binding is unchanged under the loader's flat last-wins
// map, so this is fidelity rather than meaning — and it is exactly the
// class envelopeAttrs' comment claimed immunity from. Raised in review
// of #501.
//
// BOTH URI ARRANGEMENTS, and the second is the one three rounds of this
// finding kept missing. carryDeclarations decides by KEY PRESENCE and
// envelopeAttrs decided by VALUE EQUALITY, so they agreed everywhere
// except a prefix declared at both levels with the SAME URI — skipped by
// the first, dropped by the second, envelope declaration gone. The
// coverage bracketed it without touching it: this test used two URIs,
// and TestAnElementPrefixSurvivesBeingDeclaredAtBothLevels uses one URI
// but only markup.XNamespace, which the old exception short-circuited
// before the comparison. envelopeAttrs now takes the SET
// carryDeclarations moved, so there is no second predicate to disagree
// with. Raised in review of #501.
func TestABothLevelsDeclarationKeepsTheEnvelopesCopy(t *testing.T) {
	for _, tc := range []struct{ name, envURI, rootURI string }{
		{"different URIs at the two levels", "urn:A", "urn:B"},
		{"the SAME URI at the two levels", "urn:a", "urn:a"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handlerNS(t, tc.rootURI)
			root := workspaceFixture(t)
			doc := `<Gooey xmlns:t="` + tc.envURI + `">` + "\n" +
				`  <Canvas Name="Root" xmlns:t="` + tc.rootURI + `">` + "\n" +
				`    <Button Name="B" Content="go"/>` + "\n" +
				`  </Canvas>` + "\n" +
				`</Gooey>` + "\n"
			if err := os.WriteFile(filepath.Join(root, "both.gooey"), []byte(doc), 0o644); err != nil {
				t.Fatal(err)
			}

			ed, _ := buildPage(t)
			ed.setDispatcher(gooey.NewDispatcher())
			ed.setWorkspace(root)
			ed.openWorkspaceFile("both.gooey")
			if got := ed.status.Get(); !strings.HasPrefix(got, "✓") {
				t.Fatalf("opening the fixture reports %q, want a build", got)
			}

			src := ed.source.Get()
			envelope, below, _ := strings.Cut(src, "\n")
			if !strings.Contains(envelope, `xmlns:t="`+tc.envURI+`"`) {
				t.Errorf("the envelope's own declaration is gone from the file. "+
					"carryDeclarations declined to move it because the root already "+
					"declares the prefix, and envelopeAttrs dropped it anyway:\n%s", src)
			}
			if !strings.Contains(below, `xmlns:t="`+tc.rootURI+`"`) {
				t.Errorf("the root's own declaration did not survive:\n%s", src)
			}

			// AND IT IS STABLE, so keeping the envelope's copy is a fixed
			// point rather than a second document that reopens differently
			// again.
			ed.openWorkspaceFile("both.gooey")
			if second := ed.source.Get(); second != src {
				t.Errorf("the document moved on its second pass.\nfirst:\n%s\nsecond:\n%s",
					src, second)
			}
		})
	}
}

// TestTheDesignerRefusesADocumentTheLoaderRefuses is the same shape
// pointed at the one namespace markup reserves.
//
// A default xmlns of wonderforge.io/gooey/x puts XNamespace on every
// element beneath the root, and markup.build refuses those by name (its
// `e.Space == markup.XNamespace` arm). Before the carry-down the editor
// dropped the
// declaration and so opened a document markup.Build itself will not
// load; now the two agree. That is an improvement rather than a
// regression, which is exactly why it is worth an assertion — a later
// change that silently went back to dropping the declaration would make
// the editor generous again and nothing would say so. Raised in review
// of #501.
func TestTheDesignerRefusesADocumentTheLoaderRefuses(t *testing.T) {
	root := workspaceFixture(t)
	doc := `<Gooey xmlns="` + markup.XNamespace + `">` + "\n" +
		`  <Canvas Name="Root"/>` + "\n" +
		`</Gooey>` + "\n"
	if err := os.WriteFile(filepath.Join(root, "reserved.gooey"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	// THE LOADER'S OWN ANSWER FIRST, so a refusal below cannot be read as
	// the editor being stricter than the framework.
	if _, err := markup.Build([]byte(doc), &markup.Context{}); err == nil {
		t.Fatal("markup.Build accepts a document whose default namespace is the " +
			"reserved x namespace, so the editor refusing it below would be the " +
			"editor disagreeing with the loader")
	}

	ed, _ := buildPage(t)
	ed.setDispatcher(gooey.NewDispatcher())
	ed.setWorkspace(root)
	ed.openWorkspaceFile("reserved.gooey")

	if got := ed.status.Get(); !strings.HasPrefix(got, "✗") {
		t.Errorf("the designer opened a document markup.Build refuses: %q. The "+
			"carry-down is what makes the two agree — dropping the declaration "+
			"made an unloadable document loadable in the editor only", got)
	}
}

// TestAPastedEnvelopeCarriesItsDeclarations is the same bug through the
// other unwrap, and it is the one this PR left open until review of
// #501.
//
// The CODE tab hands you a whole document, envelope and all — copying
// out of this editor and pasting back in is the round trip a user takes
// without thinking about it. unwrapGooey threw the envelope away and
// with it every xmlns on it, so the pasted subtree reached the canvas
// with an undeclared prefix and was refused: #472's own symptom,
// reported against markup the editor had just written.
//
// The document opened first declares NOTHING, so the declaration under
// test can only have come down from the pasted envelope. Pasting into a
// document that already declared the prefix would pass whether or not
// the carry works.
func TestAPastedEnvelopeCarriesItsDeclarations(t *testing.T) {
	const uri = "urn:gooey:test:472:paste"
	handlerNS(t, uri)

	root := workspaceFixture(t)
	ed, _ := buildPage(t)
	ed.setDispatcher(gooey.NewDispatcher())
	ed.setWorkspace(root)
	ed.openWorkspaceFile("main.gooey")
	if got := ed.status.Get(); !strings.HasPrefix(got, "✓") {
		t.Fatalf("opening the plain fixture reports %q, want a build", got)
	}
	if src := ed.source.Get(); strings.Contains(src, uri) {
		t.Fatalf("the opened document already declares the namespace, so the "+
			"paste below would prove nothing:\n%s", src)
	}

	ed.pasteMarkup(`<Gooey xmlns:t="` + uri + `">` + "\n" +
		`  <Button Name="Pasted" Content="go" Click="{{t:Fire}}"/>` + "\n" +
		`</Gooey>` + "\n")

	if got := ed.status.Get(); strings.HasPrefix(got, "✗") {
		t.Fatalf("pasting a document that declares its own handler namespace "+
			"reports %q — the envelope's declaration was dropped with the "+
			"envelope, so the canvas refused the expression it came with", got)
	}
	if ed.docRoot == nil {
		t.Error("the status is not a refusal but no tree was swapped in")
	}
	// THE SOURCE, not just the screen: a paste that builds from a
	// declaration the save would drop is a document that dies on reopen.
	if src := ed.source.Get(); !strings.Contains(src, uri) {
		t.Errorf("the rebuilt source does not declare the pasted namespace:\n%s", src)
	}
}

// TestAPastedConflictingDeclarationIsRefused is the hazard the carry
// created, and it is newly reachable: before this branch the editor
// could not hold two declarations of one prefix at all.
//
// markup.parse keeps ONE FLAT, document-wide prefix map and takes the
// LAST declaration in document order. The envelope is parsed before its
// child, so on the OPEN path carrying its declaration down is a no-op
// either way — but a paste lands the envelope's declaration on a node
// INSIDE the open document, later than the root's own. A pasted
// `xmlns:t` whose URI differs therefore rebinds t for every expression
// in the document, including ones the user never touched, and
// saveOpenFile writes that to disk. Raised in review of #501.
func TestAPastedConflictingDeclarationIsRefused(t *testing.T) {
	const old, fresh = "urn:gooey:test:472:paste-old", "urn:gooey:test:472:paste-new"
	handlerNS(t, old)
	handlerNS(t, fresh)

	root := workspaceFixture(t)
	doc := `<Gooey xmlns:t="` + old + `">` + "\n" +
		`  <Canvas Name="Root">` + "\n" +
		`    <Button Name="Existing" Content="go" Click="{{t:Existing}}"/>` + "\n" +
		`  </Canvas>` + "\n" +
		`</Gooey>` + "\n"
	if err := os.WriteFile(filepath.Join(root, "conflict.gooey"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}

	ed, _ := buildPage(t)
	ed.setDispatcher(gooey.NewDispatcher())
	ed.setWorkspace(root)
	ed.openWorkspaceFile("conflict.gooey")
	if got := ed.status.Get(); !strings.HasPrefix(got, "✓") {
		t.Fatalf("opening the fixture reports %q, want a build", got)
	}

	ed.pasteMarkup(`<Gooey xmlns:t="` + fresh + `">` + "\n" +
		`  <Button Name="Pasted" Content="go" Click="{{t:Pasted}}"/>` + "\n" +
		`</Gooey>` + "\n")

	if got := ed.status.Get(); !strings.HasPrefix(got, "✗") {
		t.Errorf("pasting a document that binds t to a DIFFERENT uri reports "+
			"%q — the paste was accepted, so every {{t:…}} already in the "+
			"document now resolves through the pasted provider", got)
	}
	if got := ed.status.Get(); !strings.Contains(got, "t") || !strings.Contains(got, fresh) {
		t.Errorf("the refusal is %q, which does not name the prefix and the uri "+
			"it would have been rebound to — the one thing the author needs to "+
			"act on it", got)
	}
	// AND THE OTHER SIDE OF THE MECHANISM BRANCH. t: names EXPRESSIONS —
	// it is a handler namespace, resolved through markup.parse's one
	// flat ns table — so the flat last-wins sentence is the true one
	// here, and it is the x: arm in
	// TestAPasteCannotRebindAPrefixTheEnvelopeHolds that must not carry
	// it. Asserting only one side would let the branch collapse back to
	// a single sentence in either direction with one test still green.
	// Raised in review of #501.
	if got := ed.status.Get(); !strings.Contains(got, "the last declaration parsed wins") {
		t.Errorf("the refusal reads\n\t%q\nand does not carry the flat "+
			"last-wins mechanism, which is the true one for a prefix that "+
			"names expressions", got)
	}
	if got := ed.status.Get(); strings.Contains(got, "names ELEMENTS") {
		t.Errorf("the refusal explains an EXPRESSION prefix with the element "+
			"mechanism:\n\t%q", got)
	}
	// THE DOCUMENT, not just the status: a refusal that reported itself
	// and mutated anyway is the failure mode insertSubtree's revert-on-a
	// -failed-rebuild exists for.
	src := ed.source.Get()
	if strings.Contains(src, fresh) {
		t.Errorf("the refused declaration is in the document anyway:\n%s", src)
	}
	if !strings.Contains(src, old) {
		t.Errorf("the document lost its OWN declaration to a refused paste:\n%s", src)
	}
}

// TestAPastedDefaultNamespaceIsNotAConflict is the arm the refusal above
// must NOT cover, and it was covering it.
//
// isNamespaceAttr matches the plain "xmlns" as well as "xmlns:"+local,
// so a default declaration went through the conflict arm and a paste
// between two documents carrying different version strings was refused
// — with a message built by TrimPrefix(k, "xmlns:"), which returns
// "xmlns" unchanged for this key and so asked the author to rename a
// prefix that does not exist.
//
// There is nothing to rebind. markup.parse skips a plain xmlns outright
// ("the default namespace is decorative versioning"), so it never
// reaches the flat prefix map that makes a PREFIX conflict dangerous,
// and XML scoping confines it to the subtree that declares it.
//
// THE PREFIX ARM IS ASSERTED IN THE SAME BREATH, because the fix is an
// exemption and an exemption that swallowed the neighbouring case would
// pass every assertion above it. Raised in review of #501.
func TestAPastedDefaultNamespaceIsNotAConflict(t *testing.T) {
	const ours, theirs = "wonderforge.io/gooey/2026", "wonderforge.io/gooey/2027"
	const prefixURI = "urn:gooey:test:472:default-arm"
	handlerNS(t, prefixURI)

	root := workspaceFixture(t)
	doc := `<Gooey xmlns="` + ours + `" xmlns:t="` + prefixURI + `">` + "\n" +
		`  <Canvas Name="Root">` + "\n" +
		`    <Button Name="Existing" Content="go" Click="{{t:Existing}}"/>` + "\n" +
		`  </Canvas>` + "\n" +
		`</Gooey>` + "\n"
	if err := os.WriteFile(filepath.Join(root, "default.gooey"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}

	ed, _ := buildPage(t)
	ed.setDispatcher(gooey.NewDispatcher())
	ed.setWorkspace(root)
	ed.openWorkspaceFile("default.gooey")
	if got := ed.status.Get(); !strings.HasPrefix(got, "✓") {
		t.Fatalf("opening the fixture reports %q, want a build", got)
	}

	// A DIFFERENT default, and the same prefix binding the document
	// already has — so the only thing that differs is the one key the
	// exemption is about.
	// ON THE PASTED ELEMENT, NOT ON ITS ENVELOPE, and that moved in
	// review of #501's round 7. A pasted envelope is thrown away and
	// only xmlns:-prefixed declarations come down from it now — the
	// default one is the target document's business, not the fragment's
	// — so an envelope-level default would simply vanish and this
	// fixture would stop reaching the exemption it exists to exercise.
	// Written where a copy out of the CODE tab puts it when the element
	// itself carried the declaration.
	paste := `<Gooey xmlns:t="` + prefixURI + `">` + "\n" +
		`  <Button Name="Pasted" Content="go" Click="{{t:Pasted}}" xmlns="` + theirs + `"/>` + "\n" +
		`</Gooey>` + "\n"
	ed.pasteMarkup(paste)

	// NOT a "✓" test: a successful paste reports "pasted markup: <…>".
	// The refusal is the one with a mark on it.
	if got := ed.status.Get(); strings.HasPrefix(got, "✗") {
		t.Fatalf("pasting a subtree whose DEFAULT namespace differs reports %q. "+
			"markup.parse discards a plain xmlns without recording it, so there "+
			"is no prefix for it to rebind and nothing for this to refuse", got)
	}
	src := ed.source.Get()
	if !strings.Contains(src, `Name="Pasted"`) {
		t.Errorf("the paste reported success and the node is not in the document:\n%s", src)
	}
	if n := strings.Count(src, theirs); n != 1 {
		t.Fatalf("the pasted default namespace appears %d times after one paste, "+
			"want 1 — the count below is a comparison against this one", n)
	}

	// TWICE, FROM BYTE-IDENTICAL INPUT, which is the arm that was
	// missing: the exemption sat INSIDE the inequality until review of
	// #501, so the FIRST paste differs from the document's own default
	// and takes the skip, while the second matches the declaration the
	// first just left behind and falls through to the delete. Measured
	// before the fix: the declaration appears once after both pastes,
	// and this test passed because it asserted the first only. Two
	// identical pastes have to produce two identical subtrees.
	ed.pasteMarkup(paste)
	if got := ed.status.Get(); strings.HasPrefix(got, "✗") {
		t.Fatalf("pasting the same markup a second time reports %q", got)
	}
	if n := strings.Count(ed.source.Get(), theirs); n != 2 {
		t.Errorf("the pasted default namespace appears %d times after the SAME "+
			"markup was pasted twice, want 2. The second paste was stripped of a "+
			"declaration the first was allowed to keep, so the two subtrees differ "+
			"— from identical input:\n%s", n, ed.source.Get())
	}

	// AND THE NEIGHBOUR STILL REFUSES. A fix that exempted every
	// namespace attribute would pass everything above.
	ed.pasteMarkup(`<Gooey xmlns:t="urn:gooey:test:472:default-arm-other">` + "\n" +
		`  <Button Name="Rebinder" Content="go" Click="{{t:Rebinder}}"/>` + "\n" +
		`</Gooey>` + "\n")
	if got := ed.status.Get(); !strings.HasPrefix(got, "✗") {
		t.Errorf("a pasted PREFIX conflict now reports %q — the default-namespace "+
			"exemption swallowed the case this whole step exists for", got)
	}
}

// TestARedundantPastedDeclarationIsDropped is the benign half of the
// same missing step. Copying a button out of the CODE tab and pasting it
// ten times left ten redundant xmlns:t attributes in the user's file —
// every one of them agreeing with the root's, and every one of them
// noise the author did not write.
func TestARedundantPastedDeclarationIsDropped(t *testing.T) {
	const uri = "urn:gooey:test:472:paste-same"
	handlerNS(t, uri)

	root := workspaceFixture(t)
	doc := `<Gooey xmlns:t="` + uri + `">` + "\n" +
		`  <Canvas Name="Root">` + "\n" +
		`    <Button Name="Existing" Content="go" Click="{{t:Existing}}"/>` + "\n" +
		`  </Canvas>` + "\n" +
		`</Gooey>` + "\n"
	if err := os.WriteFile(filepath.Join(root, "same.gooey"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}

	ed, _ := buildPage(t)
	ed.setDispatcher(gooey.NewDispatcher())
	ed.setWorkspace(root)
	ed.openWorkspaceFile("same.gooey")
	before := strings.Count(ed.source.Get(), uri)
	if before != 1 {
		t.Fatalf("the opened document declares the uri %d times, want 1:\n%s",
			before, ed.source.Get())
	}

	ed.pasteMarkup(`<Gooey xmlns:t="` + uri + `">` + "\n" +
		`  <Button Name="Pasted" Content="go" Click="{{t:Pasted}}"/>` + "\n" +
		`</Gooey>` + "\n")

	if got := ed.status.Get(); strings.HasPrefix(got, "✗") {
		t.Fatalf("pasting a document that binds t to the SAME uri reports %q", got)
	}
	if got := strings.Count(ed.source.Get(), uri); got != 1 {
		t.Errorf("the document declares the uri %d times after a paste that "+
			"added nothing new, want 1 — a redundant declaration is noise in "+
			"a file the author writes:\n%s", got, ed.source.Get())
	}
}

// TestARefusedPasteBurnsNoName is the cost side of a corrigible error.
//
// A namespace conflict is something the author fixes and retries: the
// message names the prefix and asks them to rename it. That only holds
// if retrying lands where the first attempt would have. It did not.
// renameInto and rebindInto ran FIRST, and rebindInto writes
// ed.ctx.Values[key] for every binding it re-keys — registrations
// nothing ever removes, deliberately, so an undone paste can be redone
// onto the values the user had set. renameInto then counts a name that
// owns live handles as taken. So the refused attempt consumed G2 and
// the retry produced G3, with G2's handles registered to nothing.
//
// A SEEDED BINDING IS WHAT MAKES THE BURN REACHABLE, and this is the
// arm the first version of this test got wrong: rebindInto registers a
// handle only for a binding it RE-KEYS, so a node whose attributes are
// all literals costs nothing whatever the order is, and the test passed
// against the bug. <Gauge> is the element the clipboard tests already
// use for this, because its seed binds Value per instance.
//
// THE NAME IS THE OBSERVABLE, not the registry, because the name is what
// the user sees and what their bindings are written against. Raised in
// review of #501.
func TestARefusedPasteBurnsNoName(t *testing.T) {
	const uri = "urn:gooey:test:472:burn"
	handlerNS(t, uri+":other")

	ed, _ := clipEditor(t)
	spec := ed.specOrBare("Gauge")
	// FATAL, NOT SKIPPED. Both of these were t.Skip, and a skip here
	// disarms the only guard proving reconcileNamespaces runs BEFORE
	// renameInto and rebindInto — silently, and in the one direction
	// that matters: the palette and the seed are in this repository, so
	// a <Gauge> that stops binding Value is a change somebody made here,
	// not an environment this test has to tolerate. CLAUDE.md's skip
	// doctrine is for a claim that dies with its issue; this is a
	// FIXTURE PRECONDITION, and a precondition that stops holding means
	// the test covers nothing rather than that it does not apply.
	// Raised in review of #501.
	if spec.Seed == "" {
		t.Fatalf("<Gauge> has no seed in this build's palette, so this test has no "+
			"element whose seed binds a value and cannot exercise the ordering it "+
			"is about. Pick another seeded element rather than skipping: %+v", spec)
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
	if n.Attrs["Value"] == "" {
		t.Fatalf("<Gauge>'s seed does not bind Value, so every attribute on this "+
			"node is a literal — and rebindInto registers a handle only for a "+
			"binding it RE-KEYS, which is the arm the first version of this test "+
			"got wrong. Without a bound attribute the assertions below pass "+
			"against the bug, so this is a broken fixture rather than an "+
			"inapplicable test. Seed: %q", n.Attrs)
	}
	ed.doc().Kids = append(ed.doc().Kids, n)
	ed.rebuild()
	// The document declares the prefix, so a paste carrying the same
	// prefix bound elsewhere is the conflict this refuses.
	ed.doc().Attrs["xmlns:t"] = uri

	clashing := n.markup("  ")

	// REFUSED: the same prefix, a different URI.
	ed.pasteMarkup(`<Gooey xmlns:t="` + uri + `:other">` + "\n" + clashing + "\n" + `</Gooey>` + "\n")
	if got := ed.status.Get(); !strings.HasPrefix(got, "\u2717") {
		t.Fatalf("the clashing paste reports %q; this test measures what a REFUSAL "+
			"costs and nothing was refused", got)
	}

	// ACCEPTED: the author did what the message asked and dropped the
	// declaration. The name they get must be the one they would have got
	// had the first attempt never happened.
	ed.pasteMarkup(`<Gooey>` + "\n" + clashing + "\n" + `</Gooey>` + "\n")
	if got := ed.status.Get(); strings.HasPrefix(got, "\u2717") {
		t.Fatalf("the corrected paste reports %q", got)
	}
	if got := ed.sel.Attrs["Name"]; got != "G2" {
		t.Errorf("after a refused paste and a corrected one the pasted node is named "+
			"%q, want \"G2\" — the refusal consumed the name, so the author fixed "+
			"what the message asked them to fix and got a different answer than if "+
			"they had written it correctly the first time", got)
	}
}

// TestTheDesignerNamesANamespacedAttributeLikeMarkupDoes holds the two
// refusals to one spelling.
//
// nodeOf's comment claims its answer is "the same answer markup's own
// parser gives", and a claim like that is the reason the two can drift
// without anyone noticing: both refuse, both say something sensible, and
// only a user comparing them finds one naming an attribute the other
// does not. markup.namespacedAttrError writes {uri}local for an ordinary
// prefix and `xml:local` for the XML namespace — that one is bound by
// the spec rather than declared, so an author who wrote xml:space would
// not recognise it back as {http://www.w3.org/XML/1998/namespace}space.
// nodeOf wrote {uri}local for both.
//
// ASKED OF markup ITSELF rather than of a copy of its rule here: the
// package does not export the formatter and this is a nested module, so
// the only honest check is to make markup refuse the same attribute and
// require its message to CONTAIN what the designer said. Raised in
// review of #501.
//
// AND THIS TEST DOES NOT RUN IN CI, which is the half it cannot fix for
// itself: CI vets the app modules without running their suites (CLAUDE.md,
// Verify), so markup — the upstream copy — could change its spelling
// with every check green and only the loop somebody runs by hand would
// notice. markup.TestNamespacedAttributesAreLoadErrors spells both arms
// out in the module CI does run, and markup.namespacedAttrError's
// comment names namespacedAttrName as what moves with it. This test is
// the agreement; that one is the tripwire. Raised in review of #501 as
// well.
func TestTheDesignerNamesANamespacedAttributeLikeMarkupDoes(t *testing.T) {
	for _, tc := range []struct{ name, doc string }{
		{"a declared prefix", `<Gooey xmlns:p="urn:gooey:test:501:attr">` +
			`<Text p:Thing="x">hi</Text></Gooey>`},
		{"the xml namespace", `<Gooey><Text xml:space="preserve">hi</Text></Gooey>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, designer := nodeOf(tc.doc)
			if designer == nil {
				t.Fatal("the designer accepted a namespaced attribute; its model holds " +
					"only plain ones, so this would be dropped on the next write")
			}
			_, loader := markup.Build([]byte(tc.doc), &markup.Context{})
			if loader == nil {
				t.Fatal("markup accepted a namespaced attribute, so there is no second " +
					"spelling to agree with and this guard is checking nothing")
			}
			// The attribute's NAME as each side spells it. The messages
			// differ in every other word, deliberately — one is a user
			// opening a file and the other is a load error — and the
			// name is the part a user carries from one to the other.
			name := between(t, designer.Error(), `attribute "`, `"`)
			if !strings.Contains(loader.Error(), name) {
				t.Errorf("the designer calls the attribute %q and markup's own refusal "+
					"does not contain that:\n\tdesigner: %v\n\tmarkup:   %v",
					name, designer, loader)
			}
		})
	}
}

// between returns the text of s between the first after and the next
// before it, failing if either is missing — so a reworded message fails
// loudly here instead of comparing an empty string, which every message
// contains.
func between(t *testing.T, s, after, before string) string {
	t.Helper()
	i := strings.Index(s, after)
	if i < 0 {
		t.Fatalf("%q does not contain %q, so nothing can be extracted from it", s, after)
	}
	rest := s[i+len(after):]
	j := strings.Index(rest, before)
	if j < 0 {
		t.Fatalf("%q has no closing %q after %q", s, before, after)
	}
	if j == 0 {
		t.Fatalf("%q holds nothing between %q and %q", s, after, before)
	}
	return rest[:j]
}

// TestAPrefixedElementIsRefusedLikeAPrefixedAttribute closes the one
// shape nodeOf accepted, and the FOUR negative arms are the point of the
// table rather than padding.
//
// The defect: nodeOf built `&node{Elem: t.Name.Local}`, so
// `<t:Thing Name="x"/>` became a node called `Thing` with nothing said,
// while a prefixed ATTRIBUTE was refused by name two lines further on.
// The model cannot write either back out. carryDeclarations makes it
// consequential — the envelope keeps `xmlns:t` and a paste produces
// `<Thing xmlns:t="urn:a"/>`, the declaration preserved and the prefix
// it scoped discarded — and once the two-roots refusal is lifted,
// `<x:Property>` round-trips to `<Property>`, which markup refuses.
//
// THE OBVIOUS FIX IS WRONG, and the negatives are what say so.
// encoding/xml resolves a name to its URI and hands back no prefix, so
// under `xmlns="wonderforge.io/gooey/2026"` — which every document this
// editor writes carries — Gooey, Canvas and Button ALL have a non-empty
// Name.Space. `t.Name.Space != ""` would refuse every real document.
// The discriminator is a difference from the default in scope, which is
// why nodeOf carries a stack of defaults; arms 2-5 are each a state that
// rule has to get right and the naive one does not.
//
// Arm 6 is the residue, asserted as a PASS rather than left unstated: a
// prefix bound to the same URI as the default is the same element name
// by XML namespaces, so rewriting `<g:Canvas/>` to `<Canvas/>` preserves
// meaning. Raised in review of #501.
func TestAPrefixedElementIsRefusedLikeAPrefixedAttribute(t *testing.T) {
	const gooeyNS = `wonderforge.io/gooey/2026`
	for _, tc := range []struct {
		name, doc string
		refuse    string // the element spelling the refusal must name, or "" to accept
	}{
		{"a declared prefix on a child", `<Gooey xmlns:t="urn:a"><t:Thing Name="x"/></Gooey>`, `{urn:a}Thing`},
		{"an UNdeclared prefix", `<Gooey xmlns="` + gooeyNS + `"><x:Property Name="P"/></Gooey>`, `{x}Property`},
		{"a prefix on the ROOT", `<t:Gooey xmlns:t="urn:a"><Canvas Name="R"/></t:Gooey>`, `{urn:a}Gooey`},

		{"the default namespace every document carries", `<Gooey xmlns="` + gooeyNS + `"><Canvas Name="R"><Button Name="B"/></Canvas></Gooey>`, ""},
		{"a default alongside an unused prefix declaration", `<Gooey xmlns="` + gooeyNS + `" xmlns:x="urn:x"><Canvas Name="R"/></Gooey>`, ""},
		{"no namespace at all", `<Gooey><Canvas Name="R"/></Gooey>`, ""},
		{"a child REDECLARING the default", `<Gooey xmlns="urn:u"><Canvas xmlns="urn:v" Name="R"/></Gooey>`, ""},
		{"a prefix bound to the default's own URI", `<Gooey xmlns="urn:u" xmlns:g="urn:u"><g:Canvas Name="R"/></Gooey>`, ""},

		// THE EXEMPTION, and the reason this branch has one. The
		// refusal's premise is that the model cannot hold the namespace,
		// so the element is renamed on write. node.Space holds
		// markup.XNamespace and the envelope re-derives the prefix from
		// the document's own xmlns, so a declaration makes the round
		// trip under the author's prefix — which is the whole of #517.
		// Under the envelope's binding and under the element's own,
		// because those are the two placements declPrefix exists for.
		{"an x-namespaced declaration, bound on the envelope",
			`<Gooey xmlns:x="` + markup.XNamespace + `"><x:Property Name="T" Type="string"/></Gooey>`, ""},
		{"an x-namespaced declaration binding its own prefix",
			`<Gooey><p:Property xmlns:p="` + markup.XNamespace + `" Name="T" Type="string"/></Gooey>`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n, err := nodeOf(tc.doc)
			if tc.refuse == "" {
				if err != nil {
					t.Fatalf("nodeOf refused a document with no prefixed element: %v\n%s", err, tc.doc)
				}
				// AND THE EXEMPT ONE IS ACTUALLY CARRIED, not merely
				// tolerated: an accept that dropped the namespace would
				// pass the arm above while leaving exactly the rename
				// the refusal exists to prevent.
				for _, k := range n.Kids {
					if k.Elem != "Property" {
						continue
					}
					if k.Space != markup.XNamespace {
						t.Errorf("the declaration was accepted with Space %q; "+
							"the exemption is that the model HOLDS this "+
							"namespace, and a node without it is written back "+
							"out as a plain <Property>", k.Space)
					}
				}
				return
			}
			if err == nil {
				got := "<" + n.Elem + ">"
				for _, k := range n.Kids {
					got += " <" + k.Elem + ">"
				}
				t.Fatalf("nodeOf accepted a prefixed element and built %s — the "+
					"prefix is gone and the model cannot put it back:\n%s", got, tc.doc)
			}
			if !strings.Contains(err.Error(), tc.refuse) {
				t.Errorf("the refusal does not name the element as %q, so the "+
					"author cannot tell WHICH prefix is the problem: %v",
					tc.refuse, err)
			}
		})
	}
}

// TestCollectNamespacesReadsEveryWalkInOneOrder is the slot half of a
// finding whose attribute half was fixed a round earlier, and the two
// walks are the same claim: "last wins" is a statement about ORDER, and
// ranging a map has none.
//
// collectNamespaces feeds doc[k], which is exactly what accept-vs-refuse
// is decided against in reconcileNamespacesInto, so a prefix declared
// twice in one document under different slots resolved to whichever of
// Go's randomized iterations came second — the same document accepting a
// paste on one run and refusing it on the next. Measured on this
// fixture before the fix: four slots declaring xmlns:t to four URIs
// produced all four over 500 runs.
//
// THE ANSWER IS NAMED, not merely required to be stable: a walk that
// ranged a map and happened to agree with itself for 500 runs would pass
// a stability check, and a walk that sorted DESCENDING would too. The
// URI asserted is the one on the last slot name in sorted order, which
// is what node.markup writes last when it serialises the same node
// (main.go sorts slot names as well as attribute keys). Raised in
// review of #501.
func TestCollectNamespacesReadsEveryWalkInOneOrder(t *testing.T) {
	const prefix = "xmlns:t"
	slotted := func(uri string) *node {
		return &node{Elem: "Text", Attrs: map[string]string{prefix: uri}}
	}
	n := &node{
		Elem: "ItemsView",
		Slots: map[string]*node{
			"ItemsView.ItemTemplate":     slotted("urn:a"),
			"ItemsView.EmptyTemplate":    slotted("urn:b"),
			"ItemsView.HeaderTemplate":   slotted("urn:c"),
			"ItemsView.SelectedTemplate": slotted("urn:d"),
		},
	}

	seen := map[string]int{}
	for i := 0; i < 500; i++ {
		into := map[string]string{}
		collectNamespaces(n, into)
		seen[into[prefix]]++
	}
	if len(seen) != 1 {
		t.Fatalf("collectNamespaces resolved %s to %d different URIs over 500 runs: "+
			"%v. doc[k] is what a paste is accepted or refused against, so this is "+
			"the same document answering differently run to run", prefix, len(seen), seen)
	}
	// Sorted: EmptyTemplate, HeaderTemplate, ItemTemplate, SelectedTemplate.
	if _, ok := seen["urn:d"]; !ok {
		t.Errorf("the surviving binding is %v, not urn:d — the last slot name in "+
			"sorted order, which is the one node.markup writes last when it "+
			"serialises this node. A stable answer in the wrong order is still a "+
			"walk that disagrees with the file it produces", seen)
	}
}

// TestUndoDoesNotReachBackPastAnOpen is the fourth route to a document
// wearing the wrong envelope, and the only one where the envelope is the
// smaller half of the problem.
//
// Opening a file left the previous file's snapshots in the undo stack,
// so ctrl+z put the FIRST document's tree back while ed.envAttrs and
// ed.openPath still belonged to the SECOND. Measured before the fix:
// open one carrying Graphics="halfblock", open a plain one, undo, and
// the CODE tab reads a bare <Gooey> over Content="first". Nothing warns,
// and a save at that point writes one document's content into the
// other's path — which is why the fix is to end the history at the open
// rather than to carry envAttrs through it. Carrying the envelope would
// have made the screen self-consistent and the file mismatch total.
// Raised in review of #501.
func TestUndoDoesNotReachBackPastAnOpen(t *testing.T) {
	root := workspaceFixture(t)
	write := func(name, envelope string) {
		doc := `<Gooey xmlns="wonderforge.io/gooey/2026"` + envelope + `>` + "\n" +
			`  <Canvas Name="Root"><Button Name="B" Content="` + name + `"/></Canvas>` + "\n" +
			`</Gooey>` + "\n"
		if err := os.WriteFile(filepath.Join(root, name+".gooey"), []byte(doc), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("first", ` Graphics="halfblock"`)
	write("second", "")

	ed, _ := buildPage(t)
	ed.setDispatcher(gooey.NewDispatcher())
	ed.setWorkspace(root)
	ed.openWorkspaceFile("first.gooey")
	ed.openWorkspaceFile("second.gooey")
	// NON-VACUITY: the second file must actually be on screen, or the
	// undo below has nothing to reach back past.
	if src := ed.source.Get(); !strings.Contains(src, `Content="second"`) {
		t.Fatalf("the second file is not the open document:\n%s", src)
	}

	ed.undo()
	ed.rebuild()
	src := ed.source.Get()
	if strings.Contains(src, `Content="first"`) {
		t.Errorf("undo restored the PREVIOUS file's tree into the open document. "+
			"openPath still says %q, so this content would be saved over that "+
			"file:\n%s", ed.openPath.Get(), src)
	}
	// AND THE ENVELOPE IS STILL THE SECOND FILE'S — the symptom that
	// surfaced this, asserted separately because a fix that carried
	// envAttrs through the undo would clear the arm above and leave a
	// document whose envelope and openPath disagree.
	if strings.Contains(src, `Graphics="halfblock"`) {
		t.Errorf("undo brought the FIRST file's envelope onto the second "+
			"document:\n%s", src)
	}
	if got := ed.openPath.Get(); got != "second.gooey" {
		t.Errorf("undo moved openPath to %q; it names the file a save writes to "+
			"and no undo should change it", got)
	}
}

// TestAnElementPrefixSurvivesBeingDeclaredAtBothLevels is the sibling
// guard's exception, asserted on the complement.
//
// carryDeclarations refuses to move markup.XNamespace down;
// envelopeAttrs — documented as "everything on a <Gooey> that did NOT
// move down", with a paragraph on the complement being OBSERVED rather
// than assumed — compared values and fell into its skip whenever the
// content root happened to declare the same URI. A document declaring
// xmlns:x at BOTH levels therefore lost the envelope's copy by the other
// route. Measured before the fix, through the file browser:
//
//	envAttrs = map[]
//	rebuilt  = "<Gooey>"   over  <Canvas … xmlns:x="…">
//
// That saves as a bare <Gooey> with the declaration on the content root,
// which is exactly the relocation the sibling guard exists to prevent.
//
// ONE DOCUMENT, BOTH ENDS. The envelope's map is what gooeyOpen writes,
// so asserting on ed.envAttrs alone would pass over a rebuild that
// dropped it afterwards; asserting on the rebuilt source alone would
// pass over an envAttrs that happened to be repaired downstream. Raised
// in review of #501.
func TestAnElementPrefixSurvivesBeingDeclaredAtBothLevels(t *testing.T) {
	root := workspaceFixture(t)
	doc := `<Gooey xmlns:x="` + markup.XNamespace + `">` + "\n" +
		`  <Canvas Name="Root" xmlns:x="` + markup.XNamespace + `">` + "\n" +
		`    <Button Name="B" Content="go"/>` + "\n" +
		`  </Canvas>` + "\n" +
		`</Gooey>` + "\n"
	if err := os.WriteFile(filepath.Join(root, "both.gooey"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}

	ed, _ := buildPage(t)
	ed.setDispatcher(gooey.NewDispatcher())
	ed.setWorkspace(root)
	ed.openWorkspaceFile("both.gooey")
	if got := ed.status.Get(); !strings.HasPrefix(got, "✓") {
		t.Fatalf("the document did not open (%q), so nothing below is about "+
			"where its declaration went", got)
	}

	// THE PREMISE: the content root really does still declare it, or the
	// value-equality branch this test is about was never taken and the
	// assertions below pass for the wrong reason.
	if got := ed.doc().Attrs["xmlns:x"]; got != markup.XNamespace {
		t.Fatalf("the content root declares xmlns:x as %q; this test is about "+
			"the case where BOTH levels declare the same URI", got)
	}

	if got := ed.envAttrs["xmlns:x"]; got != markup.XNamespace {
		t.Errorf("the envelope's xmlns:x is %q after the open. carryDeclarations "+
			"never moves this namespace down, so envelopeAttrs — its complement — "+
			"has no business dropping it: value-equality is not the question, "+
			"\"did carryDeclarations put it there\" is, and for this URI the answer "+
			"is always no", got)
	}
	if src := ed.source.Get(); !strings.Contains(src, `<Gooey xmlns:x="`+markup.XNamespace+`"`) {
		t.Errorf("the rebuilt document opens without the declaration:\n%s\n"+
			"Saved, this is a bare <Gooey> over a content root holding the "+
			"prefix — the element prefix relocated, which is the loss "+
			"carryDeclarations' own guard exists to prevent", src)
	}
}

// TestUndoAfterAnOpenKeepsTheSelectionTheOpenMade is the other half of
// the re-baseline above, and it broke in the commit that added it.
//
// history.reset took root alone, so the baseline it wrote carried no
// selection. record's sel-refresh does not repair that: it is guarded on
// the path still RESOLVING in the state being left, and after an ADD the
// new node's path does not exist there, so the snapshot pushed for the
// paste keeps the base's hasSel — false. restore then runs
// `ed.sel = nil` unconditionally. Measured before the fix, through the
// file browser:
//
//	after open:    ed.sel != nil, base.hasSel = false, base.sel = []
//	after paste:   undo depth 1, top hasSel = false
//	after undo:    ed.sel == nil
//
// The document is intact and nothing is selected — the properties pane
// empties and ctrl+n is the only way back. Raised in review of #501.
//
// THE ASSERTION IS ON WHAT ed.sel NAMES, not on the pointer, because
// restore clones: the selection is "the node the open selected" only in
// the sense that it resolves to the same element of an equivalent tree.
func TestUndoAfterAnOpenKeepsTheSelectionTheOpenMade(t *testing.T) {
	root := workspaceFixture(t)
	doc := `<Gooey xmlns="wonderforge.io/gooey/2026">` + "\n" +
		`  <Canvas Name="Root"><Button Name="B" Content="x"/></Canvas>` + "\n" +
		`</Gooey>` + "\n"
	if err := os.WriteFile(filepath.Join(root, "sel.gooey"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}

	ed, _ := buildPage(t)
	ed.setDispatcher(gooey.NewDispatcher())
	ed.setWorkspace(root)
	ed.openWorkspaceFile("sel.gooey")
	opened := ed.sel
	if opened == nil {
		t.Fatal("the open selected nothing, so there is no selection for the undo to lose")
	}

	ed.insertSubtree(&node{Elem: "Button", Attrs: map[string]string{"Content": "hi"}}, "pasted")
	// NON-VACUITY, both halves. A refused paste (a <Text Text="…"> was
	// the first attempt here, and <Text> takes no Text attribute) pushes
	// nothing, and then the undo below is a no-op that passes for the
	// wrong reason.
	if got := ed.status.Get(); !strings.HasPrefix(got, "pasted") {
		t.Fatalf("the paste did not happen, so the undo has nothing to undo: %q", got)
	}
	if n := len(ed.history().undo); n != 1 {
		t.Fatalf("undo depth %d after the paste, want 1", n)
	}

	ed.undo()
	if ed.sel == nil {
		t.Fatalf("undo after an open deselected everything. The document is "+
			"still there (%s) — it is the SELECTION that was dropped, because "+
			"history.reset baselined without one and restore clears ed.sel "+
			"whenever the snapshot says hasSel is false", ed.source.Get())
	}
	if got, want := ed.sel.Elem, opened.Elem; got != want {
		t.Errorf("undo left <%s> selected; the open selected <%s>", got, want)
	}
	if got, want := ed.sel.Attrs["Name"], opened.Attrs["Name"]; got != want {
		t.Errorf("undo left Name=%q selected; the open selected Name=%q", got, want)
	}
	// AND IT NAMES A NODE OF THE RESTORED TREE, not a stranded pointer
	// into the one restore replaced — which is the shape a fix that
	// carried ed.sel across instead of re-resolving the path would leave.
	if _, ok := pathTo(ed.root, ed.sel); !ok {
		t.Error("ed.sel is not reachable from ed.root: the selection points " +
			"into the tree restore threw away")
	}
}

// TestAnElementPrefixStaysOnTheEnvelopeThroughAnOpen is the end-to-end
// half the round before this one said was unreachable.
//
// The argument for unreachability was that #517 refuses a document
// declaring xmlns:x as having two roots. It refuses one CONTAINING an
// <x:Property>: that element is a second kid of <Gooey>, and the
// len(n.Kids) != 1 arm in openWorkspaceFile is what turns it away. A
// document that only DECLARES the prefix has one kid and opens like any
// other, which is what this drives — through the file browser, on a file
// on disk, so the arm runs where a user reaches it rather than where a
// unit call does.
//
// The assertion is on ed.source and ed.doc().Attrs rather than on the
// screen: the defect a carried element prefix causes is not visible in
// the running canvas at all. It appears on the next OPEN, when the
// declaration sits on the content root and <x:Property> — its sibling —
// is out of scope.
func TestAnElementPrefixStaysOnTheEnvelopeThroughAnOpen(t *testing.T) {
	root := workspaceFixture(t)
	doc := `<Gooey xmlns:x="` + markup.XNamespace + `">` + "\n" +
		`  <Canvas Name="Root">` + "\n" +
		`    <Button Name="B" Content="go"/>` + "\n" +
		`  </Canvas>` + "\n" +
		`</Gooey>` + "\n"
	if err := os.WriteFile(filepath.Join(root, "decl.gooey"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}

	ed, _ := buildPage(t)
	ed.setDispatcher(gooey.NewDispatcher())
	ed.setWorkspace(root)
	ed.openWorkspaceFile("decl.gooey")

	// THE OPEN ITSELF IS HALF THE FINDING. If this refuses, the skip in
	// carryDeclarations really is unreachable through this path and the
	// comment that said so was right.
	if got := ed.status.Get(); !strings.HasPrefix(got, "✓") {
		t.Fatalf("opening a document that DECLARES xmlns:x reports %q. Only a "+
			"document that CONTAINS an <x:Property> has two kids of <Gooey>; "+
			"this one has a single Canvas and must open", got)
	}
	if got, ok := ed.doc().Attrs["xmlns:x"]; ok {
		t.Errorf("the element prefix came down onto the content root as %q. "+
			"<x:Property> is a SIBLING of this root, so a declaration here is "+
			"out of scope at the element it exists for and the saved document "+
			"stops loading", got)
	}
	if src := ed.source.Get(); !strings.Contains(src, `<Gooey xmlns:x=`) {
		t.Errorf("the rebuilt source does not carry the declaration on its "+
			"envelope:\n%s", src)
	}
}

// TestCarryDeclarationsLeavesTheElementPrefixOnTheEnvelope calls the
// function directly, which pins the rule at the seam both unwraps share.
// The end-to-end half is
// TestAnElementPrefixStaysOnTheEnvelopeThroughAnOpen, and it is reachable
// today: the comment here used to say the open path could not get here,
// on the grounds that #517 refuses such a document. What #517 refuses is
// a document CONTAINING an <x:Property>, which gives <Gooey> two kids.
// One that merely DECLARES xmlns:x has one kid and opens normally.
// Corrected in review of #501.
//
// The rule: a prefix used inside an attribute VALUE — a handler or value
// expression — may come down onto the content root, because markup.parse
// resolves one through a flat document-wide table and any element's
// declaration reaches any expression. (A prefix on an attribute NAME is
// never looked up in that table — parse refuses it outright — except for
// the reserved xmlns: declarations, which are what build it.) An ELEMENT prefix
// may not, because encoding/xml resolved it with real subtree scoping and
// <x:Property> is a SIBLING of the content root, not a descendant — a
// declaration moved onto the root is out of scope at the very element it
// was for.
func TestCarryDeclarationsLeavesTheElementPrefixOnTheEnvelope(t *testing.T) {
	env := &node{Elem: "Gooey", Attrs: map[string]string{
		"xmlns:t": "urn:handlers",
		"xmlns:x": markup.XNamespace,
		"xmlns":   "wonderforge.io/gooey/2026",
	}}
	root := &node{Elem: "Canvas", Attrs: map[string]string{}}
	carryDeclarations(env, root)

	if got, ok := root.Attrs["xmlns:t"]; !ok || got != "urn:handlers" {
		t.Errorf("the attribute prefix reads %q (present=%v) on the root, want it "+
			"carried down: the envelope is dropped by the unwrap, so a declaration "+
			"left only there is lost — which is #472", got, ok)
	}
	if got, ok := root.Attrs["xmlns:x"]; ok {
		t.Errorf("the element prefix was carried down as %q. <x:Property> is a "+
			"sibling of this root, not a descendant, so a declaration here is out "+
			"of scope at the element it exists for and the saved document stops "+
			"loading — the same scoping #501 corrected the docs about", got)
	}
	if _, ok := root.Attrs["xmlns"]; ok {
		t.Errorf("the default declaration came down; it is decorative versioning " +
			"that markup.parse skips, and the root has no use for it")
	}
}

// TestOnlyOneFunctionWritesADocumentEnvelope replaces the count that used
// to sit in gooeyOpen's doc comment.
//
// A hand-maintained list of call sites inside a comment is the
// enumeration CLAUDE.md's Verify section is about, and this one had
// already gone stale: it named three literals in two functions while a
// fourth sat in fragmentFor. The set is derived here, so adding a bare
// envelope anywhere reddens this rather than quietly making a sentence
// wrong. Raised in review of #501.
func TestOnlyOneFunctionWritesADocumentEnvelope(t *testing.T) {
	// EXEMPT, WITH THE REASON, not dropped: a patch fragment addresses an
	// island inside another document, so the envelope attributes gooeyOpen
	// writes are the ones it must NOT carry. See gooeyOpen's doc.
	const exempt = "fragmentFor"
	// PACKAGE SCOPE IS A SITE TOO, and it was invisible. The walk took
	// d.(*ast.FuncDecl) and inspected fn.Body, so a *ast.GenDecl was
	// skipped whole: `const envelope = "<Gooey>\n"` at package scope
	// would have been attributed to no function at all and every user of
	// it recorded under no name, leaving this guard green over the exact
	// thing it exists to find. Hoisting a repeated literal to a package
	// const is the ordinary refactor, not an exotic one — and this test
	// replaced a hand-maintained list precisely because that list failed
	// SILENTLY. A name nothing can exempt is the right attribution: there
	// is no function to route through gooeyOpen, so the answer is always
	// to move the literal. Raised in review of #501.
	const pkgScope = "package scope"

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parsing this package: %v", err)
	}
	seen := map[string]bool{}
	for _, pkg := range pkgs {
		for _, f := range pkg.Files {
			for _, d := range f.Decls {
				// THE WHOLE DECLARATION, whatever kind it is. A
				// FuncDecl's literals are attributed to it; anything
				// else — a const block, a var, an interface's default —
				// is package scope, which no exemption names.
				owner := pkgScope
				if fn, ok := d.(*ast.FuncDecl); ok {
					if fn.Body == nil {
						continue
					}
					owner = fn.Name.Name
				}
				// OPENS WITH IT, rather than contains it:
				// openWorkspaceFile's refusal message says "a <Gooey>
				// document needs exactly one root element", which is
				// prose about an envelope and not one. lit.Value keeps
				// the quote, so [1:] drops either kind of it.
				//
				// AND "IT" IS THE WHOLE MESSAGE, NOT A LINE OF ONE.
				// Go concatenation makes one string out of several
				// BasicLits, so a prose message wrapped across lines
				// can put "<Gooey>" at the start of a CONTINUATION —
				// and testing each literal on its own then reports the
				// function as writing an envelope. unwrapGooey's
				// declaration refusal does exactly that ("… declares %d
				// property %s on its " + "<Gooey>, and a paste lands
				// inside a document …"), and the guard flagged it the
				// moment #501 and #522 met in a merge: a false positive
				// whose only remedy would have been to reflow a
				// sentence, which the next gofmt could undo. Every
				// literal reachable to the RIGHT of a `+` is a
				// continuation, so only the leftmost one answers.
				cont := map[*ast.BasicLit]bool{}
				ast.Inspect(d, func(n ast.Node) bool {
					be, ok := n.(*ast.BinaryExpr)
					if !ok || be.Op != token.ADD {
						return true
					}
					ast.Inspect(be.Y, func(m ast.Node) bool {
						if l, isLit := m.(*ast.BasicLit); isLit && l.Kind == token.STRING {
							cont[l] = true
						}
						return true
					})
					return true
				})
				ast.Inspect(d, func(n ast.Node) bool {
					if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING &&
						!cont[lit] && strings.HasPrefix(lit.Value[1:], "<Gooey") {
						seen[owner] = true
					}
					return true
				})
			}
		}
	}
	if !seen["gooeyOpen"] {
		t.Fatalf("no <Gooey literal found in gooeyOpen; the walk found %v. An "+
			"empty or wrong side makes every assertion below vacuous",
			sortedNames(seen))
	}
	var extra []string
	for name := range seen {
		if name != "gooeyOpen" && name != exempt {
			extra = append(extra, name)
		}
	}
	sort.Strings(extra)
	if len(extra) != 0 {
		t.Errorf("%v spell a <Gooey envelope by hand. gooeyOpen is the one "+
			"function that writes a document's envelope, because the attributes "+
			"that belong on it — Graphics, a default xmlns, an xmlns:x — are "+
			"carried by ed.envAttrs and a hand-written literal drops all of "+
			"them silently. Route it through gooeyOpen, or exempt it here with "+
			"the reason it describes something other than a document, as "+
			"%s is", extra, exempt)
	}
	if !seen[exempt] {
		t.Errorf("%s no longer writes its own envelope; the exemption above is "+
			"stale and should go with whatever replaced it", exempt)
	}
}

// sortedNames is the keys of set, sorted, for a message.
func sortedNames(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// assignedIn returns the names of the functions in this package's
// non-test sources that WRITE the field the matcher picks out, sorted.
// It reads the AST rather than grepping because a grep cannot tell a
// write from a read, and every one of these fields is read in many more
// places than it is written.
//
// THREE SPELLINGS, not one, and the two that were missing are the ones
// the invariant is most likely to be broken with. This matched only
// `*ast.AssignStmt` with the field itself on the left, so an in-place
// empty was invisible:
//
//	ed.envAttrs = nil        // caught
//	clear(ed.envAttrs)       // NOT caught — a call, not an assignment
//	delete(ed.envAttrs, k)   // NOT caught, same reason
//	ed.envAttrs[k] = v       // NOT caught — the LHS is an IndexExpr
//
// Measured with a probe method rather than read off the types: with
// `func (ed *editor) probeClearA() { clear(ed.envAttrs) }` in the
// package, the guard stayed GREEN, while the `= nil` spelling beside it
// reddened. `clear` is the idiomatic Go spelling of exactly the clear
// the three comments this guard replaces forbid, so the blind spot was
// over the fourth site's most likely form. Same class as the
// FuncDecl-body hole closed in TestOnlyOneFunctionWritesADocumentEnvelope
// one commit earlier. Raised in review of #501.
//
// AND THE WHOLE DECLARATION, NOT fn.Body — the same hole, one round
// later, in the guard that was widened as the example. This took
// `d.(*ast.FuncDecl)` and skipped everything else, so
//
//	var reopen = func(ed *editor) { ed.envAttrs = nil }
//
// at package scope was invisible. Invisible on BOTH sides, which is why
// it is silent rather than loud: the two sets stay equal and the guard
// passes over exactly the fourth site it exists to catch. Measured — a
// probe file carrying that literal left this green. Package scope is
// attributed to a name no caller can produce, the way the sibling guard
// does it.
//
// `field` is what makes the receiver question answerable. See onEd.
func assignedIn(t *testing.T, field string, writes func(ast.Expr, map[string]bool) bool) []string {
	t.Helper()
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parsing this package: %v", err)
	}
	seen := map[string]bool{}
	for _, pkg := range pkgs {
		for _, f := range pkg.Files {
			for _, d := range f.Decls {
				owner := assignPkgScope
				if fn, ok := d.(*ast.FuncDecl); ok {
					if fn.Body == nil {
						continue
					}
					owner = fn.Name.Name
				}
				eds := editorIdents(d)
				// LOUD WHERE IT USED TO BE SILENT. A write to the
				// watched field through a base this walk cannot show is
				// an editor is reported rather than skipped, because
				// skipping it removes the site from BOTH sets at once
				// and leaves them equal. That is the whole failure mode
				// of a receiver-name narrowing, and it is why onEd's
				// "a spurious site fails loudly" argument does not cover
				// this direction on its own.
				suspect := func(e ast.Expr) {
					ast.Inspect(e, func(n ast.Node) bool {
						se, ok := n.(*ast.SelectorExpr)
						if !ok || se.Sel.Name != field {
							return true
						}
						if id, ok := se.X.(*ast.Ident); ok && eds[id.Name] {
							return true
						}
						t.Errorf("%s writes .%s through a base this walk cannot "+
							"identify as an *editor (%s). Either it IS one and "+
							"editorIdents has to learn the binding, or it is not "+
							"and the field belongs to something else — but it "+
							"must not be dropped: a dropped site leaves both "+
							"sets equal and this guard green",
							owner, field, types.ExprString(se.X))
						return true
					})
				}
				ast.Inspect(d, func(n ast.Node) bool {
					switch n := n.(type) {
					case *ast.AssignStmt:
						for _, lhs := range n.Lhs {
							// The field itself, or a slot in it:
							// `m[k] = v` writes m without naming it on
							// the left.
							if writes(lhs, eds) {
								seen[owner] = true
							}
							if ix, ok := lhs.(*ast.IndexExpr); ok && writes(ix.X, eds) {
								seen[owner] = true
							}
							suspect(lhs)
						}
					case *ast.CallExpr:
						// clear and delete are the in-place empties, and
						// they are calls rather than assignments. append
						// is deliberately NOT here: it returns, and the
						// assignment that stores the result is already
						// matched above.
						id, ok := n.Fun.(*ast.Ident)
						if !ok || len(n.Args) == 0 {
							return true
						}
						if id.Name != "clear" && id.Name != "delete" {
							return true
						}
						if writes(n.Args[0], eds) {
							seen[owner] = true
						}
						suspect(n.Args[0])
					}
					return true
				})
			}
		}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func selects(e ast.Expr, name string) (*ast.SelectorExpr, bool) {
	se, ok := e.(*ast.SelectorExpr)
	if !ok || se.Sel.Name != name {
		return nil, false
	}
	return se, true
}

// assignPkgScope is what a write outside any function is attributed to.
// It cannot collide with a function name, so nothing can exempt it: the
// answer to a document field written at package scope is always to move
// the write into a function, not to name it here.
const assignPkgScope = "package scope"

// onEd is selects with the base checked against the identifiers this
// declaration BINDS to *editor. The guard below is about the editor's
// two fields specifically — a bare `selects(e, "root")` counts any
// assignment whose final field is `root`, so `s.root = …` or
// `h.base.root = …` inside undo.go would be read as a
// document-replacement site.
//
// Latent rather than live: `grep '\.root = '` over the package's
// non-test sources returns only undo.go's `ed.root = s.root.clone()`.
//
// IT USED TO HARDCODE THE IDENTIFIER `ed`, and that is a false-NEGATIVE
// narrowing rather than a false-positive one — the opposite direction
// from the "a spurious site fails loudly" argument that justified adding
// it. A method spelled `func (e *editor) …` writing `e.envAttrs`
// disappeared from BOTH sets, so the sets stayed equal and the guard
// stayed green; measured with a probe file carrying one. The receiver
// name is derived per declaration now, and — the half a bare fn.Recv
// would still miss — so are parameters and locals, because two of the
// package's *editor values are neither receivers nor parameters
// (`ed := newEditor(root)` and `ed := &editor{…}`, both in main.go).
//
// The residue is handled rather than narrowed away: a base this cannot
// identify is REPORTED by assignedIn instead of skipped. See there.
// Raised in review of #501.
func onEd(e ast.Expr, name string, eds map[string]bool) (*ast.SelectorExpr, bool) {
	se, ok := selects(e, name)
	if !ok {
		return nil, false
	}
	id, ok := se.X.(*ast.Ident)
	if !ok || !eds[id.Name] {
		return nil, false
	}
	return se, true
}

// editorIdents is every identifier the declaration d binds to *editor:
// a method receiver, a parameter of any func type inside it (including
// a package-level func literal's, which is the shape that made the
// hardcoded `ed` a silent hole), and a short variable declaration
// initialised from `&editor{…}` or newEditor.
//
// DERIVED, NOT LISTED, for the reason CLAUDE.md's Verify section gives
// about counts and names in prose: `ed` is the spelling everywhere in
// this package today, and a guard that depends on that fact goes quietly
// blind the first time somebody writes `e`.
//
// It is a syntactic answer to a question types would answer exactly, and
// it is allowed to be incomplete only because incompleteness is loud
// here: assignedIn reports a write through a base this does not
// recognise. A new way of getting hold of an editor therefore fails the
// guard with the expression printed, rather than vanishing from it.
func editorIdents(d ast.Node) map[string]bool {
	out := map[string]bool{}
	isEditorPtr := func(e ast.Expr) bool {
		st, ok := e.(*ast.StarExpr)
		if !ok {
			return false
		}
		id, ok := st.X.(*ast.Ident)
		return ok && id.Name == "editor"
	}
	fromNew := func(e ast.Expr) bool {
		if u, ok := e.(*ast.UnaryExpr); ok && u.Op == token.AND {
			if cl, ok := u.X.(*ast.CompositeLit); ok {
				id, ok := cl.Type.(*ast.Ident)
				return ok && id.Name == "editor"
			}
			return false
		}
		call, ok := e.(*ast.CallExpr)
		if !ok {
			return false
		}
		id, ok := call.Fun.(*ast.Ident)
		return ok && id.Name == "newEditor"
	}
	add := func(fl *ast.FieldList) {
		if fl == nil {
			return
		}
		for _, f := range fl.List {
			if !isEditorPtr(f.Type) {
				continue
			}
			for _, n := range f.Names {
				out[n.Name] = true
			}
		}
	}
	if fn, ok := d.(*ast.FuncDecl); ok {
		add(fn.Recv)
	}
	ast.Inspect(d, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncType:
			add(n.Params)
		case *ast.AssignStmt:
			if n.Tok != token.DEFINE {
				return true
			}
			for i, lhs := range n.Lhs {
				id, ok := lhs.(*ast.Ident)
				if !ok || i >= len(n.Rhs) {
					continue
				}
				if fromNew(n.Rhs[i]) {
					out[id.Name] = true
				}
			}
		}
		return true
	})
	return out
}

// TestTheEditorIdentWalkFindsEveryBindingThisPackageUses is the guard on
// the guard, and it exists because editorIdents is a SYNTACTIC answer to
// a question only types answer exactly.
//
// The three forms are not hypothetical — each is in this package's
// non-test sources today, and the fourth arm is the one the hardcoded
// `ed` could not see at all:
//
//	func (ed *editor) …            receiver
//	func writeX(ed *editor, …)     parameter, incl. a package-level literal
//	ed := newEditor(root)          newEditor's caller, main.go
//	ed := &editor{…}               the &editor literal in main.go
//
// The NEGATIVE arm is the one that keeps this from being a tautology: a
// binding of some other type must NOT be collected, or onEd stops
// excluding `s.root` and `h.base.root` and the narrowing it exists for
// is gone. Raised in review of #501.
func TestTheEditorIdentWalkFindsEveryBindingThisPackageUses(t *testing.T) {
	for _, tc := range []struct {
		name, src string
		want      []string
	}{
		{"a receiver", `func (e *editor) m() {}`, []string{"e"}},
		{"a parameter", `func f(target *editor, s string) {}`, []string{"target"}},
		{"a package-level func literal's parameter",
			`var reopen = func(who *editor) { who.envAttrs = nil }`, []string{"who"}},
		{"a local from the constructor", `func f() { x := newEditor(nil) ; _ = x }`, []string{"x"}},
		{"a local from a composite literal", `func f() { y := &editor{} ; _ = y }`, []string{"y"}},
		{"two at once", `func (ed *editor) m(other *editor) {}`, []string{"ed", "other"}},
		{"nothing of the type", `func f(h *history, s snapshot) { _ = h ; _ = s }`, nil},
		{"a VALUE, not a pointer", `func f(ed editor) {}`, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, err := parser.ParseFile(token.NewFileSet(), "x.go",
				"package main\n"+tc.src+"\n", 0)
			if err != nil {
				t.Fatalf("parsing the fixture: %v", err)
			}
			if len(f.Decls) != 1 {
				t.Fatalf("the fixture has %d declarations, want 1", len(f.Decls))
			}
			got := sortedNames(editorIdents(f.Decls[0]))
			if len(got) == 0 {
				got = nil
			}
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Errorf("editorIdents found %v, want %v", got, tc.want)
			}
		})
	}
}

// TestEnvAttrsIsAssignedWhereTheDocumentIs turns an invariant three
// comments assert into one the suite checks.
//
// "ed.envAttrs may only move with ed.root.Kids" is stated in
// openWorkspaceFile, in setWorkspace (browser.go) and in closeWorkspace
// (menus.go) — three places that tell a reader not to clear the field,
// and nothing that notices when someone does. The defect it came from
// was exactly that: a clear on a path that left the document on the
// canvas, so the next rebuild wrote a bare <Gooey> for a file nobody had
// edited. A prose invariant repeated three times is three copies of a
// claim, not a guard, and the fourth site is the one that breaks it.
//
// The check is deliberately about ASSIGNMENT SITES rather than about any
// particular wrong value: it is the separation of the two fields that is
// the bug, whatever either is set to.
func TestEnvAttrsIsAssignedWhereTheDocumentIs(t *testing.T) {
	envAttrs := assignedIn(t, "envAttrs", func(e ast.Expr, eds map[string]bool) bool {
		_, ok := onEd(e, "envAttrs", eds)
		return ok
	})
	// TWO SPELLINGS OF REPLACING THE DOCUMENT, because matching one of
	// them is the hole this test was written to close, one level up.
	// `ed.root.Kids = …` swaps the content under a root the editor keeps;
	// `ed.root = …` swaps the root itself. history.restore (undo.go)
	// spells it the second way, does not touch envAttrs, and was invisible
	// to a matcher that only knew the first — so the guard reported the
	// package clean while holding a live example of the arrangement its
	// own message describes. Raised in review of #501.
	kids := assignedIn(t, "root", func(e ast.Expr, eds map[string]bool) bool {
		if se, ok := selects(e, "Kids"); ok {
			_, ok = onEd(se.X, "root", eds)
			return ok
		}
		_, ok := onEd(e, "root", eds)
		return ok
	})
	// AND ONE OF THEM IS ALLOWED TO, with the reason stated rather than
	// the site quietly dropped. history.reset (undo.go) clears the stack
	// on every open, so every snapshot restore can reach belongs to the
	// file that is open — the same document ed.envAttrs already describes,
	// which is why restoring one without re-assigning the other cannot
	// separate them. TestUndoDoesNotReachBackPastAnOpen is
	// what holds that premise; if reset ever stops clearing, this
	// exemption is what has to go with it.
	if !slices.Contains(kids, "restore") {
		t.Fatalf("the document-replacement walk found %v, with no restore in it "+
			"— the exemption below would then remove nothing and the matcher has "+
			"stopped seeing history.restore's `ed.root = s.root.clone()`, which "+
			"is the site this widening was for", kids)
	}
	kids = slices.DeleteFunc(kids, func(fn string) bool { return fn == "restore" })

	// ENVDECLS IS THE THIRD, and it was leaning on this test without
	// being in it. Its own comment cites this guard as the reason it is
	// "assigned at the one site that assigns those" — but the two
	// matchers above never looked at it, so a future site replacing the
	// document and the envelope attrs while leaving envDecls behind
	// would carry the PREVIOUS file's property declarations onto the new
	// one, rebuild would write them into the CODE tab, and save would
	// write them to somebody else's file, with nothing red. That is the
	// same "prose invariant repeated, not measured" shape this test was
	// written against, one field over. Raised in review of #522.
	//
	// ON THE EDITOR, like the two above. It read a bare
	// `selects(e, "envDecls")` when it was written, which counted any
	// assignment whose final field is envDecls whatever it belongs to —
	// harmless while the editor is the only holder, and the same
	// false-positive shape onEd exists to remove. The base derivation
	// makes that free now, and a base the walk cannot identify is
	// reported rather than dropped.
	envDecls := assignedIn(t, "envDecls", func(e ast.Expr, eds map[string]bool) bool {
		_, ok := onEd(e, "envDecls", eds)
		return ok
	})
	// ENVSLOTS IS THE FOURTH, added with it for #510: the envelope's
	// <Gooey.Resources>. Left behind by a site that replaced the rest,
	// the last file's styles would be written into the next one's save.
	envSlots := assignedIn(t, "envSlots", func(e ast.Expr, eds map[string]bool) bool {
		_, ok := onEd(e, "envSlots", eds)
		return ok
	})

	if len(kids) == 0 || len(envAttrs) == 0 || len(envDecls) == 0 || len(envSlots) == 0 {
		t.Fatalf("the walk found envAttrs assigned in %v, ed.root.Kids in %v and "+
			"envDecls in %v; an empty side means the matcher stopped matching and "+
			"every assertion below would pass vacuously (envSlots in %v)", envAttrs, kids, envDecls, envSlots)
	}
	if strings.Join(envSlots, ",") != strings.Join(kids, ",") {
		t.Errorf("ed.envSlots is assigned in %v and ed.root.Kids in %v. These must "+
			"be the same set: a <Gooey.Resources> left over from the last file is "+
			"written into the next one's save", envSlots, kids)
	}
	if strings.Join(envAttrs, ",") != strings.Join(kids, ",") {
		t.Errorf("ed.envAttrs is assigned in %v and the document in %v. These must "+
			"be the same set: the envelope belongs to the document on the canvas, "+
			"so a site that replaces one and not the other leaves the editor "+
			"describing a file it is no longer showing — which is the defect three "+
			"comments in this package warn about and nothing measured", envAttrs, kids)
	}
	if strings.Join(envDecls, ",") != strings.Join(kids, ",") {
		t.Errorf("ed.envDecls is assigned in %v and ed.root.Kids in %v. These must "+
			"be the same set for the same reason, and the cost of their parting is "+
			"worse than the envelope attrs': a declaration left over from the last "+
			"file becomes part of the next one's public surface, written back on "+
			"the first save", envDecls, kids)
	}
}

// The THIRD scope reconcileNamespaces has to collect, and the one its
// doc comment left out.
//
// ed.root is excluded because it is not in the save. ed.envAttrs is the
// mirror image: it IS what the saved <Gooey> carries, and it is not
// reachable from ed.doc(). An element prefix stays there through an open
// — TestAnElementPrefixStaysOnTheEnvelopeThroughAnOpen is that half — so
// with only ed.doc() collected, a paste rebinding it finds no conflict to
// report, and the second binding lands inside the document and is written
// to disk.
//
// THE PREMISE IS ASSERTED FIRST. If the declaration were on the content
// root this test would pass through the ordinary doc scope and prove
// nothing about the envelope. Raised in review of #501.
func TestAPasteCannotRebindAPrefixTheEnvelopeHolds(t *testing.T) {
	root := workspaceFixture(t)
	doc := `<Gooey xmlns:x="` + markup.XNamespace + `">` + "\n" +
		`  <Canvas Name="Root">` + "\n" +
		`    <Button Name="Existing" Content="go"/>` + "\n" +
		`  </Canvas>` + "\n" +
		`</Gooey>` + "\n"
	if err := os.WriteFile(filepath.Join(root, "env.gooey"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}

	ed, _ := buildPage(t)
	ed.setDispatcher(gooey.NewDispatcher())
	ed.setWorkspace(root)
	ed.openWorkspaceFile("env.gooey")
	if got := ed.status.Get(); !strings.HasPrefix(got, "✓") {
		t.Fatalf("opening the fixture reports %q, want a build", got)
	}
	if _, onRoot := ed.doc().Attrs["xmlns:x"]; onRoot {
		t.Fatal("the declaration came down onto the content root, so this test " +
			"is measuring the ordinary document scope and not the envelope")
	}
	if got := ed.envAttrs["xmlns:x"]; got != markup.XNamespace {
		t.Fatalf("the envelope holds xmlns:x = %q, want %q — the scope this "+
			"test is about is empty", got, markup.XNamespace)
	}

	const other = "urn:gooey:test:501:not-x"
	ed.pasteMarkup(`<Gooey xmlns:x="` + other + `">` + "\n" +
		`  <Button Name="Pasted" Content="go"/>` + "\n" +
		`</Gooey>` + "\n")

	got := ed.status.Get()
	if !strings.HasPrefix(got, "✗") {
		t.Errorf("pasting a fragment that binds x to a DIFFERENT uri reports "+
			"%q. The envelope's declaration is not reachable from ed.doc(), so "+
			"nothing compared the two: the second binding is now inside the "+
			"document, and nothing refused it", got)
	}
	// AND THE EXPLANATION, not only the ✗. This is the one end-to-end
	// exercise of reconcileNamespacesInto's refusal, and it rebinds x:,
	// so the single path under test is the one whose stated reason has
	// to be right. A message that tells the author why is a claim, and
	// this repo holds a claim under test.
	for _, want := range []string{
		"one flat document-wide table",
		"names ELEMENTS",
		"XML scopes to the subtree that declares them",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the refusal reads\n\t%q\nand does not carry\n\t%q", got, want)
		}
	}
	// AND IN THAT ORDER, which is the whole finding. BOTH mechanisms are
	// real for x: — markup.parse's ns map takes every xmlns attribute at
	// any depth (markup.parse's `a.Name.Space == "xmlns"` arm),
	// x: included, so the flat table
	// re-points x: expressions exactly as it does any other prefix; and
	// encoding/xml really has scoped the ELEMENT names before markup
	// sees a token. What separates them is REACHABILITY FROM HERE.
	// nodeOf's prefixed-element refusal turns back every namespaced
	// element at any depth on both the open path and the paste path, so
	// no document this editor can hold contains an x:-prefixed element
	// and the element half cannot describe anything the author is
	// looking at. It describes the saved file, later. The expression
	// half is what happens in the document on screen, so it leads.
	//
	// The previous version led with the elements and asserted the flat
	// table was NOT how x: resolves — the same premise carryDeclarations'
	// doc had already retracted one commit earlier, back in a
	// user-facing string rather than a comment, and pinned here in the
	// wrong direction.
	if i, j := strings.Index(got, "one flat document-wide table"),
		strings.Index(got, "names ELEMENTS"); i > j {
		t.Errorf("the refusal leads with the element scoping, which no document "+
			"this editor can hold can show the author, and leaves the live "+
			"consequence second:\n\t%q", got)
	}
	if src := ed.source.Get(); strings.Contains(src, other) {
		t.Errorf("the refused declaration is in the document anyway:\n%s", src)
	}
	if got := ed.envAttrs["xmlns:x"]; got != markup.XNamespace {
		t.Errorf("the envelope's own declaration became %q after a refused "+
			"paste, want %q", got, markup.XNamespace)
	}
}

// TestAPasteCannotRebindAPrefixTHEDECLARATIONSHold is the sibling above
// one scope out: the saved envelope binds prefixes ed.envAttrs has
// never held, and a paste may not re-point those either.
//
// TWO SHAPES, AND THEY REACH THE FILE BY DIFFERENT ROUTES — which is
// the point, because the editor's answer must not depend on the route:
//
//   - carried: one declaration holds its own xmlns:p, declPrefix reports
//     the document binds the namespace already, and declAttrs re-emits
//     that binding on the declaration element in the saved file;
//   - minted: the declarations disagree about how they bind it (one
//     prefixed, one as its own default xmlns), so declPrefix reports
//     bound == false and withDeclBinding puts a fresh xmlns:p on
//     <Gooey> that no opened byte ever contained.
//
// Both were ACCEPTED before #522's reconcileNamespaces widening, with
// the byte-identical paste into the envelope-bound document refused by
// the test above. The fixtures are the discriminator rather than the
// assertion text: each was run against the narrow scope set and each
// wrote the rebinding to disk.
//
// The refusal MESSAGE is not asserted here beyond its ✗ — the sibling
// above owns that text, and repeating it would make a reworded message
// three failures instead of one.
func TestAPasteCannotRebindAPrefixTHEDECLARATIONSHold(t *testing.T) {
	const other = "urn:gooey:test:522:not-x"
	decl := func(prefixed bool, name, def string) string {
		if prefixed {
			return `  <p:Property xmlns:p="` + markup.XNamespace +
				`" Name="` + name + `" Type="string" Default="` + def + `"/>`
		}
		return `  <Property xmlns="` + markup.XNamespace +
			`" Name="` + name + `" Type="string" Default="` + def + `"/>`
	}
	for _, tc := range []struct {
		name, why string
		decls     []string
	}{
		{
			name: "carried",
			why: "the declaration carries its own xmlns:p and declAttrs " +
				"re-emits it, so the saved file binds p: from a place " +
				"ed.envAttrs never sees",
			decls: []string{decl(true, "A", "a")},
		},
		{
			name: "minted",
			why: "the two declarations bind the namespace differently, so " +
				"declPrefix reports it unbound and withDeclBinding mints " +
				"xmlns:p onto <Gooey> — a binding no opened byte held",
			decls: []string{decl(true, "A", "a"), decl(false, "B", "b")},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := workspaceFixture(t)
			doc := "<Gooey>\n" + strings.Join(tc.decls, "\n") + "\n" +
				`  <Canvas Name="Root">` + "\n" +
				`    <Button Name="Existing" Content="go"/>` + "\n" +
				`  </Canvas>` + "\n" +
				`</Gooey>` + "\n"
			if err := os.WriteFile(filepath.Join(root, "decl.gooey"), []byte(doc), 0o644); err != nil {
				t.Fatal(err)
			}

			ed, _ := buildPage(t)
			ed.setDispatcher(gooey.NewDispatcher())
			ed.setWorkspace(root)
			ed.openWorkspaceFile("decl.gooey")
			if got := ed.status.Get(); !strings.HasPrefix(got, "✓") {
				t.Fatalf("opening the fixture reports %q, want a build", got)
			}
			// THE PREMISE, and it is the half that makes this test
			// different from its sibling: the binding must NOT be in
			// either scope the narrow reconcileNamespaces collected, or
			// the arm passes for the sibling's reason.
			if got, ok := ed.envAttrs["xmlns:p"]; ok {
				t.Fatalf("ed.envAttrs already binds p to %q, so this arm is "+
					"measuring the envelope scope the sibling test owns", got)
			}
			if _, onRoot := ed.doc().Attrs["xmlns:p"]; onRoot {
				t.Fatal("the binding came down onto the content root, so this " +
					"arm is measuring the ordinary document scope")
			}
			if len(ed.envDecls) != len(tc.decls) {
				t.Fatalf("ed.envDecls holds %d declarations, want %d — the "+
					"fixture did not reach the scope this arm is about",
					len(ed.envDecls), len(tc.decls))
			}
			// And the saved envelope must actually bind it, or there is
			// nothing for the paste to conflict with.
			if head := envelopeHead(ed.envAttrs, ed.envDecls, ed.envSlots); !strings.Contains(
				head, `xmlns:p="`+markup.XNamespace+`"`) {
				t.Fatalf("the saved envelope binds p: nowhere:\n%s", head)
			}

			ed.pasteMarkup(`<Gooey>` + "\n" +
				`  <Text Name="Pasted" xmlns:p="` + other + `">hi</Text>` + "\n" +
				`</Gooey>` + "\n")

			if got := ed.status.Get(); !strings.HasPrefix(got, "✗") {
				t.Errorf("pasting a fragment that binds p to a DIFFERENT uri "+
					"reports %q, want a refusal: %s, and markup.parse's one "+
					"flat last-wins table then hands every p: element in the "+
					"saved file to the pasted uri", got, tc.why)
			}
			if src := ed.source.Get(); strings.Contains(src, other) {
				t.Errorf("the refused declaration is in the document anyway:\n%s", src)
			}
		})
	}
}

// TestAPasteCannotRebindAPrefixAgainstITSELF closes the half of the
// rebind rule the comparison could not see.
//
// reconcileNamespacesInto compared every declaration against the
// DOCUMENT's and never against one it had just walked past, so a
// fragment carrying two bindings of one prefix — nested, or two
// siblings — was accepted whole and both landed in the file. Measured
// before the fix, both spellings returned nil.
//
// It is the same defect as the document-vs-paste case, not a smaller
// one: markup.parse keeps ONE FLAT ns map for the whole document
// (markup.parse's `a.Name.Space == "xmlns"` arm) with no scoping and
// last-wins, so two bindings
// inside the paste collide in exactly the table two bindings across the
// paste boundary collide in. Which is why the fix records into ONE map
// as the walk goes rather than copying per subtree — a copy would catch
// the nested spelling and leave the sibling one, which the loader does
// not distinguish.
//
// THE THIRD CASE IS THE COUNTERFACTUAL, and it is the one that would go
// red if the recording were made to refuse everything it records: an
// inner declaration REPEATING the outer one is redundant, not
// conflicting, and is dropped the way a redundant declaration is
// dropped everywhere else in this function.
//
// EVERY REFUSING ARM PASSES AN EMPTY DOCUMENT, which is what makes this
// test the place the message's OTHER half is pinned. Both colliding
// bindings are in the fragment, so any sentence naming the open document
// as the other party is false here — and the assertion that used to
// stand, `Contains(err, "xmlns:t")`, was true under that wording too. It
// watched the misattribution land and stayed green. The arms now assert
// the party and the remedy, and restoring the single old message turns
// all three red (mutation-checked).
func TestAPasteCannotRebindAPrefixAgainstITSELF(t *testing.T) {
	for _, tc := range []struct {
		name   string
		src    string
		refuse bool
	}{
		{
			"nested",
			`<Canvas xmlns:t="urn:A"><Button Name="B" xmlns:t="urn:B"/></Canvas>`,
			true,
		},
		{
			"siblings",
			`<Canvas><Button Name="B" xmlns:t="urn:A"/><Label Name="L" xmlns:t="urn:B"/></Canvas>`,
			true,
		},
		{
			// COUSINS, which is the fixture that separates one map from
			// a copy taken at each descent: the first binding is a level
			// deeper than the second, so any per-subtree copy has gone
			// out of scope by the time the second is read. markup.parse
			// has no scope to go out of.
			"cousins",
			`<Canvas><VStack><Button Name="B" xmlns:t="urn:A"/></VStack><Label Name="L" xmlns:t="urn:B"/></Canvas>`,
			true,
		},
		{
			"redundant",
			`<Canvas xmlns:t="urn:A"><Button Name="B" xmlns:t="urn:A"/></Canvas>`,
			false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n, err := nodeOf(tc.src)
			if err != nil {
				t.Fatalf("the fixture does not parse: %v", err)
			}
			err = reconcileNamespacesInto(n, map[string]string{}, map[string]string{})
			if tc.refuse {
				if err == nil {
					t.Fatalf("a fragment binding t twice was accepted whole, so "+
						"both declarations reach the file and markup.parse's one "+
						"flat table hands every t: expression to whichever it "+
						"parses last:\n%s", n.markup(""))
				}
				if !strings.Contains(err.Error(), "xmlns:t") {
					t.Errorf("the refusal does not name the colliding prefix: %v", err)
				}
				// THE PARTY, NOT JUST THE PREFIX. Every arm here has
				// BOTH bindings in the clipboard against an empty
				// document, so a sentence naming the document is false
				// on all three — and `Contains(err, "xmlns:t")` held
				// under the wording that did, which is why this arm
				// watched the misattribution go in and said nothing.
				// Asserting the remedy too, because that was the half
				// with a cost: it sent the author to change a
				// declaration their file does not contain.
				if strings.Contains(err.Error(), "this document") ||
					strings.Contains(err.Error(), "the document's own declaration") {
					t.Errorf("the refusal blames the open document for a conflict "+
						"whose two bindings are both in the paste — the document "+
						"passed here declares nothing, and an author following "+
						"the remedy goes looking for a declaration that is not "+
						"in their file: %v", err)
				}
				if !strings.Contains(err.Error(), "declares xmlns:t twice") {
					t.Errorf("the refusal does not say the paste declares the "+
						"prefix twice, which is the one fact that locates the "+
						"conflict for the author: %v", err)
				}
				// AND THE MECHANISM, which nothing asserted at all.
				// `mech` was built once above the fromDoc split and
				// interpolated into both refusals, so the
				// fragment-internal message carried the document-vs-
				// paste clause "which one depends on where this lands"
				// — false on every arm here, where both bindings are in
				// the clipboard and their order is fixed by the
				// fragment, and contradicted by the very next clause of
				// the same sentence. An unasserted string is one nobody
				// is stopping from saying that. Raised in review of
				// #501.
				if strings.Contains(err.Error(), "depends on where this lands") {
					t.Errorf("the refusal tells the author the winner is "+
						"position-dependent, but both bindings are in the paste "+
						"and their relative order is fixed by the fragment — the "+
						"later one wins wherever it lands, which the rest of the "+
						"same message already says: %v", err)
				}
				if !strings.Contains(err.Error(), "wherever it lands") {
					t.Errorf("the refusal does not say which of the two wins, "+
						"which is the fact an author needs to pick one to "+
						"rename: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("a repeated identical declaration was refused as a "+
					"conflict: %v", err)
			}
			// DROPPED, not kept: the same-URI rule everywhere else in
			// this function deletes a declaration the document already
			// makes, and a declaration the FRAGMENT already makes is the
			// same statement.
			if _, still := n.Kids[0].Attrs["xmlns:t"]; still {
				t.Errorf("the redundant inner declaration survived:\n%s", n.markup(""))
			}
		})
	}
}

// TestAModeOneParentingFaultReachesTheBackstop falsifies the premise the
// three reworded seams used to carry.
//
// The claim was that canHold refuses every parenting fault before the
// insert, so nothing reaching the rebuild backstop is about parenting.
// canHold's "Permissive where the catalog is silent, because the build
// is the gate" states the opposite in its own words and argues FOR it:
// canHold answers false only where the catalog KNOWS the child is
// refused, and ModeOne cannot know whether the slot is already taken, so
// the insert is tried and the revert names both elements. This is that
// path — a real parenting fault arriving at the line that said parenting
// faults cannot arrive.
//
// It is the structural half of the same argument
// TestCanHoldIsPermissiveWhereTheCatalogIsSilent makes about canHold
// alone: that one stops at the predicate, this one follows it to the
// message. Raised in review of #501.
func TestAModeOneParentingFaultReachesTheBackstop(t *testing.T) {
	ed, _ := buildPage(t)
	spec, ok := ed.specOf("Border")
	if !ok {
		t.Fatal("no <Border> in the catalog")
	}
	if spec.Children.Mode != markup.ModeOne {
		t.Skipf("<Border> is %v, not ModeOne; this test needs the can't-tell case", spec.Children.Mode)
	}

	border := &node{
		Elem:  "Border",
		Attrs: map[string]string{"Name": "Full", "Canvas.Left": "0", "Canvas.Top": "0"},
		Kids:  []*node{{Elem: "Text", Body: "taken", Attrs: map[string]string{"Name": "Taken"}}},
	}
	ed.doc().Kids = append(ed.doc().Kids, border)
	ed.rebuild()
	if ed.docRoot == nil {
		t.Fatalf("the full <Border> fixture does not build: %q", ed.status.Get())
	}

	// THE GATE LETS IT THROUGH, which is the premise being falsified.
	if !ed.canHold("Border", "Text") {
		t.Fatal("canHold refused a <Text> for a <Border>, so the insert never " +
			"reaches the backstop and this test cannot say anything about it")
	}

	ed.sel = border
	ed.pasteMarkup(`<Text Name="Second">second</Text>`)

	got := ed.status.Get()
	if !strings.HasPrefix(got, "✗") {
		t.Fatalf("a second child pasted into a ModeOne <Border> reported %q; "+
			"the loader refuses that document, so this test is not looking at "+
			"the refusal it is about", got)
	}
	if !strings.Contains(got, "exactly one child") {
		t.Errorf("the refusal does not carry the loader's own reason, so the "+
			"backstop is not the one under test here: %s", got)
	}
	if !strings.Contains(got, "<Text>") || !strings.Contains(got, "<Border>") {
		t.Errorf("the refusal names neither the pasted element nor where it "+
			"was going — which is the concession addplan.go makes canHold's "+
			"permissiveness on: %s", got)
	}
}

// TestTheOtherTwoSeamsDoNotClaimAParentingCause is the second and third
// of the three backstops that asserted one.
//
// A fix applied at one of several identical seams is the shape that
// leaves the others open, and carryDeclarations' own comment says so
// three files over. The paste seam was reworded first; the palette add
// and the demote emitted "<X> does not go inside <Y>" on the same
// evidence.
//
// THE DOCUMENT IS BROKEN BEFORE EITHER GESTURE, and by a third party:
// commitEdit has no docRoot == nil revert of its own (the six that do are
// insertSubtree, addSelected, deleteSelected, promoteSelected,
// demoteSelected and duplicateSelected), so a value the loader refuses
// leaves the build failed. The next insert is then reverted and blamed
// for an attribute on a node the user did not touch. That makes this the
// sharpest form of the finding: the cause is not merely "not the
// parenting", it is not the inserted element at all.
//
// THE BROKEN NODE IS A THIRD ONE, and it has to be. The first draft broke
// the very node the demote then moved, and the loader answered with
// "Canvas.Left is contributed by a <Canvas> parent, but this element's
// parent is <Border>" — a fault the move DID cause, which would have made
// the demote arm agree with the assertion for the wrong reason. The node
// carrying the refused value is now one neither gesture touches.
//
// BOTH DIRECTIONS, as the paste seam's test does: dropping the clause
// entirely would pass an assertion that only forbids the wrong noun, so
// the real cause and both element names have to survive. Raised in
// review of #501.
func TestTheOtherTwoSeamsDoNotClaimAParentingCause(t *testing.T) {
	// EACH SEAM PICKS A TARGET THE CATALOG IS HAPPY WITH, because a
	// container that also refuses the insert masks the fault under test:
	// a palette add into the full <Border> reports "needs exactly one
	// child" and the pane's value is never reached. What is wanted here
	// is the case where the parenting is fine and the document is not.
	for _, seam := range []struct {
		name string
		// gesture runs the insert on a document that is already
		// failing, and answers the element it inserted and the one it
		// was going into.
		gesture func(t *testing.T, ed *editor, host, after *node) (string, string)
	}{
		{"palette add", func(t *testing.T, ed *editor, host, after *node) (string, string) {
			ed.sel = ed.doc()
			ed.paletteSel.Set(paletteIndex(t, ed, "Text"))
			ed.addSelected()
			return "Text", ed.doc().Elem
		}},
		{"demote", func(t *testing.T, ed *editor, host, after *node) (string, string) {
			ed.sel = after // nests into the preceding sibling
			ed.demoteSelected()
			return after.Elem, host.Elem
		}},
	} {
		t.Run(seam.name, func(t *testing.T) {
			ed, _ := buildPage(t)
			host := &node{
				Elem:  "Canvas",
				Attrs: map[string]string{"Name": "Host", "Canvas.Left": "0", "Canvas.Top": "0"},
			}
			after := &node{
				Elem:  "Text",
				Body:  "sibling",
				Attrs: map[string]string{"Name": "Sibling"},
			}
			broken := &node{
				Elem:  "Text",
				Body:  "elsewhere",
				Attrs: map[string]string{"Name": "Elsewhere", "Canvas.Left": "0", "Canvas.Top": "8"},
			}
			ed.doc().Kids = append(ed.doc().Kids, host, after, broken)
			ed.rebuild()
			if ed.docRoot == nil {
				t.Fatalf("the fixture does not build: %q", ed.status.Get())
			}

			// THE THIRD PARTY. A value the loader refuses, committed
			// through the properties pane, which does not revert — on a
			// node neither gesture below goes near. That missing revert
			// is #531; commitEdit is the one mutator of seven without
			// it, and the skip below is what retires this arm when it
			// gains one.
			ed.sel = broken
			editAttr(t, ed, "Canvas.Left", "not-a-number")
			if ed.docRoot != nil {
				t.Skipf("the properties pane now reverts its own refusals "+
					"(status %q), so this seam can no longer be reached with a "+
					"fault the insert did not cause — #531 is the issue that "+
					"asked for that revert, and closing it is what retires "+
					"this arm", ed.status.Get())
			}

			elem, into := seam.gesture(t, ed, host, after)

			got := ed.status.Get()
			if !strings.HasPrefix(got, "✗") {
				t.Fatalf("the %s reported %q on a document that does not "+
					"build; this test is not looking at the refusal it is "+
					"about", seam.name, got)
			}
			if strings.Contains(got, "does not go inside") {
				t.Errorf("the %s blames the parenting for a fault on a "+
					"different node entirely — the attribute the properties "+
					"pane refused: %s", seam.name, got)
			}
			if !strings.Contains(got, "not-a-number") {
				t.Errorf("the %s drops the loader's own reason, so the author "+
					"is told the insert failed and not what to fix: %s",
					seam.name, got)
			}
			if !strings.Contains(got, "<"+elem+">") || !strings.Contains(got, "<"+into+">") {
				t.Errorf("the %s names neither the element nor where it was "+
					"going: %s", seam.name, got)
			}
		})
	}
}

// TestAPastedEnvelopesXDeclarationIsDropped pins a silent drop as
// intended rather than leaving it to be read as an oversight.
//
// carryDeclarations skips markup.XNamespace so the declaration "stays on
// the envelope". On the OPEN path that means kept — openWorkspaceFile
// holds the envelope in ed.envAttrs. On the PASTE path unwrapGooey
// discards the envelope, so the same skip means discarded, with no
// message. The two paths share a function whose comment argues the open
// path's case, which is why this needed a test rather than a sentence.
//
// Dropping is right: x: names ELEMENTS, its <x:Property> elements are
// siblings of the content root, and a pasted fragment is a content
// subtree — a carried declaration would scope nothing. The asymmetry
// against xmlns:t in the same envelope is the assertion, because that is
// what looks like a bug and is not.
//
// IT IS SAFE ONLY BECAUSE nodeOf REFUSES A PREFIXED ELEMENT, so nothing
// the model can hold uses x: and no drop can strand a live prefix. That
// is a second function holding this one up, and #522 proposes to relax
// exactly it. When it does, this test is the thing that goes red.
func TestAPastedEnvelopesXDeclarationIsDropped(t *testing.T) {
	src := `<Gooey xmlns:x="` + markup.XNamespace + `" xmlns:t="urn:t" ` +
		`Graphics="halfblock"><Canvas Name="P" Canvas.Left="0" Canvas.Top="0">` +
		`<Button Name="B"/></Canvas></Gooey>`
	n, err := nodeOf(src)
	if err != nil {
		t.Fatalf("the fixture does not parse: %v", err)
	}
	root, ok, why := unwrapGooey(n)
	if !ok {
		t.Fatalf("a <Gooey> over one root did not unwrap: %s", why)
	}
	if got, ok := root.Attrs["xmlns:t"]; !ok || got != "urn:t" {
		t.Errorf("the envelope's xmlns:t did not reach the content root (got %q, "+
			"present=%v). carryDeclarations moves a prefix the root does not "+
			"already declare, and without it a pasted document's expression "+
			"prefixes are lost — which is #472's own bug surviving through "+
			"paste:\n%s", got, ok, root.markup(""))
	}
	if got, ok := root.Attrs["xmlns:x"]; ok {
		t.Errorf("the envelope's xmlns:x was carried onto the content root as "+
			"%q. x: names ELEMENTS and XML scopes those to the subtree that "+
			"declares them, so moving it down changes what it covers — the "+
			"scope change carryDeclarations' markup.XNamespace skip exists to "+
			"prevent:\n%s", got, root.markup(""))
	}
	if got, ok := root.Attrs["Graphics"]; ok {
		t.Errorf("the envelope's Graphics=%q reached the content root. A "+
			"fragment must not carry the source document's envelope", got)
	}
}
