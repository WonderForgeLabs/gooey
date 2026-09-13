package main

import (
	"os"
	"path/filepath"
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
	// saveOpenFile (browser.go:424) builds its OWN "<Gooey>\n" + doc +
	// "</Gooey>\n" string, independently of rebuild's (main.go:2307),
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
	if src := ed.source.Get(); strings.Contains(src, "xmlnsFoo") {
		t.Errorf("an ordinary attribute spelled xmlnsFoo was carried onto the "+
			"user's root as if it were a declaration. openWorkspaceFile must "+
			"accept the two shapes nodeOf writes, \"xmlns\" and \"xmlns:\"+local, "+
			"and nothing else:\n%s", src)
	}
}

// TestTheUnprefixedDeclarationIsCarriedToo covers the SECOND of the two
// shapes nodeOf writes, which every other test in this file misses.
//
// nodeOf (main.go:752, main.go:756) and the carry-down (browser.go:390) both accept
// exactly "xmlns" and "xmlns:"+local, and the decoy in
// TestOnlyARealDeclarationComesDownFromTheEnvelope pins what is
// REJECTED — leaving the plain form implemented twice and asserted
// nowhere. It is not cosmetic: Go's decoder applies a default namespace
// to ELEMENT names, so keeping one sets Element.Space for the whole
// subtree, and markup compares that against XNamespace
// (markup/markup.go:1073). Raised in review of #501.
func TestTheUnprefixedDeclarationIsCarriedToo(t *testing.T) {
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
	if src := ed.source.Get(); !strings.Contains(src, `xmlns="urn:x"`) {
		t.Errorf("the unprefixed declaration did not survive the round trip. It "+
			"is the other half of the set nodeOf writes, and the reader must "+
			"accept both:\n%s", src)
	}
}

// TestTheDesignerRefusesADocumentTheLoaderRefuses is the same shape
// pointed at the one namespace markup reserves.
//
// A default xmlns of wonderforge.io/gooey/x puts XNamespace on every
// element beneath the root, and markup.build refuses those by name
// (markup/markup.go:1073). Before the carry-down the editor dropped the
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
