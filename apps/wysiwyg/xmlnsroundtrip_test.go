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
// nodeOf's attribute loop and carryDeclarations both accept
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
	paste := `<Gooey xmlns="` + theirs + `" xmlns:t="` + prefixURI + `">` + "\n" +
		`  <Button Name="Pasted" Content="go" Click="{{t:Pasted}}"/>` + "\n" +
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
	if n.Attrs["Value"] == "" {
		t.Skip("<Gauge>'s seed does not bind Value in this build")
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
