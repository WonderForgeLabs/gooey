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

	// Write what a save would write, and open THAT.
	if err := os.WriteFile(filepath.Join(root, "again.gooey"), []byte(first), 0o644); err != nil {
		t.Fatal(err)
	}
	ed.openWorkspaceFile("again.gooey")
	if got := ed.status.Get(); !strings.HasPrefix(got, "✓") {
		t.Fatalf("reopening the document the editor itself wrote reports %q:\n%s", got, first)
	}
	if second := ed.source.Get(); second != first {
		t.Errorf("the document moved on its second pass.\nfirst:\n%s\nsecond:\n%s", first, second)
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
