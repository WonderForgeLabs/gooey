package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/markup"
)

// xPropertyDoc is a control's type definition: an <x:Property>
// declaration on the envelope and one content root beneath it. This is
// the shape #7 specified and PR #84 landed, and the shape the editor
// refused.
const xPropertyDoc = `<Gooey xmlns:x="` + markup.XNamespace + `">` + "\n" +
	`  <x:Property Name="Title" Type="string" Default="hi"/>` + "\n" +
	`  <Canvas Name="Root">` + "\n" +
	`    <Button Name="B" Content="go"/>` + "\n" +
	`  </Canvas>` + "\n" +
	`</Gooey>` + "\n"

// TestADocumentDeclaringAPropertyOpens is #517's reproduction, driven
// through the path a user takes.
//
// THE COUNT WAS THE BUG. <x:Property> is a child of the ENVELOPE, not of
// the content root — markup.parseDocument hands the whole <Gooey> to
// splitDeclarations, which partitions its children and only then
// requires one visual kid. The editor counted n.Kids and refused the
// file with "needs exactly one root element, found 2", which describes a
// well-formed document as malformed and prescribes deleting the
// declaration.
//
// THE CONTROL ARM IS markup ITSELF. Asserting only that the editor opens
// the file would leave "the document is actually invalid" as a live
// reading of the old refusal, so the same bytes are built through
// markup.Build first: the loader takes it, therefore the refusal was the
// editor's.
func TestADocumentDeclaringAPropertyOpens(t *testing.T) {
	if _, err := markup.Build([]byte(xPropertyDoc), &markup.Context{}); err != nil {
		t.Fatalf("markup itself refuses this document, so the editor refusing it "+
			"is not the defect under test: %v", err)
	}

	root := workspaceFixture(t)
	if err := os.WriteFile(filepath.Join(root, "prop.gooey"), []byte(xPropertyDoc), 0o644); err != nil {
		t.Fatal(err)
	}

	ed, _ := buildPage(t)
	ed.setDispatcher(gooey.NewDispatcher())
	ed.setWorkspace(root)
	ed.openWorkspaceFile("prop.gooey")

	if got := ed.status.Get(); !strings.HasPrefix(got, "✓") {
		t.Fatalf("opening a document with an <x:Property> declaration reports %q, "+
			"want a build", got)
	}
	if ed.docRoot == nil {
		t.Error("the status says it builds but no tree was swapped in")
	}
	// The DOCUMENT is the content root, not the declaration: a designer
	// whose canvas rooted itself on <x:Property> would let the user drag
	// a Button into a type declaration.
	if got := ed.doc().Elem; got != "Canvas" {
		t.Errorf("the editor's document root is <%s>, want <Canvas> — the "+
			"declaration is envelope furniture, not the tree", got)
	}
}

// TestASavedPropertyDocumentStillDeclaresItsProperty is the half opening
// cannot show. A declaration the editor drops on the way out is a
// control that silently loses its public surface on the first save, and
// #517's whole point is that the editor can open the documents that
// DEFINE a control rather than only those that use one.
//
// REBUILT THROUGH markup.Build, not merely grepped. The string could
// contain "x:Property" and still be a document the loader refuses — an
// unbound prefix, or a declaration moved under the content root where
// splitDeclarations never looks.
func TestASavedPropertyDocumentStillDeclaresItsProperty(t *testing.T) {
	root := workspaceFixture(t)
	if err := os.WriteFile(filepath.Join(root, "prop.gooey"), []byte(xPropertyDoc), 0o644); err != nil {
		t.Fatal(err)
	}

	ed, _ := buildPage(t)
	ed.setDispatcher(gooey.NewDispatcher())
	ed.setWorkspace(root)
	ed.openWorkspaceFile("prop.gooey")

	src := ed.source.Get()
	if !strings.Contains(src, "x:Property") {
		t.Fatalf("the rebuilt source dropped the declaration:\n%s", src)
	}
	if _, err := markup.Build([]byte(src), &markup.Context{}); err != nil {
		t.Errorf("the source the editor would save does not load:\n%s\n%v", src, err)
	}

	// AND THE DECLARATION IS STILL THE ENVELOPE'S. A copy nested inside
	// the content root round-trips through Build only because
	// splitDeclarations would then treat it as a component named
	// Property — which is a different document, and one whose x: prefix
	// happens to still resolve.
	head, _, ok := strings.Cut(src, "<Canvas")
	if !ok {
		t.Fatalf("the saved source has no content root:\n%s", src)
	}
	if !strings.Contains(head, "x:Property") {
		t.Errorf("the declaration is written below the content root, not on the "+
			"envelope:\n%s", src)
	}
}

// TestASavedDeclarationCarriesTheBindingThatNamesIt is the half
// TestASavedPropertyDocumentStillDeclaresItsProperty cannot show: its
// fixture binds x: on the envelope only, which is the one shape where
// the prefix and its binding cannot come apart.
//
// Both documents below are legal and both load. Both saved a file
// markup.Build REFUSES, because envelopeHead wrote an x:-prefixed
// declaration onto a <Gooey> that bound no prefix to the namespace — and
// saveOpenFile is not gated on the build, so ctrl+s reported "✓ saved"
// over it. Before #522 the "2 root elements" refusal stopped both
// documents at the door; this branch is what makes the path live, which
// is the same argument its own body makes about #501's carryDeclarations.
//
// THROUGH markup.Build ON BOTH ENDS. The saved text contains
// "x:Property" under the bug too — the string is not the claim, the
// loader is.
func TestASavedDeclarationCarriesTheBindingThatNamesIt(t *testing.T) {
	for _, tc := range []struct{ name, doc string }{
		{
			// envelopeAttrs drops the envelope's xmlns:x because the
			// content root repeats it. Redundant for MEANING, and the
			// one thing envelopeHead cannot do without.
			"bound on both the envelope and the content root",
			`<Gooey xmlns:x="` + markup.XNamespace + `">` + "\n" +
				`  <x:Property Name="Title" Type="string" Default="hi"/>` + "\n" +
				`  <Canvas Name="Root" xmlns:x="` + markup.XNamespace + `">` + "\n" +
				`    <Button Name="B" Content="go"/>` + "\n" +
				`  </Canvas>` + "\n</Gooey>\n",
		},
		{
			// No prefix anywhere: the declaration names itself with a
			// default xmlns, which the re-prefixed copy must not keep.
			"bound as the declaration's own default xmlns",
			`<Gooey>` + "\n" +
				`  <Property xmlns="` + markup.XNamespace + `" Name="Title" Type="string" Default="hi"/>` + "\n" +
				`  <Canvas Name="Root">` + "\n" +
				`    <Button Name="B" Content="go"/>` + "\n" +
				`  </Canvas>` + "\n</Gooey>\n",
		},
		{
			// The author's own prefix survives, which is what declBinding
			// was added for and must keep doing.
			"bound to a prefix that is not x",
			`<Gooey xmlns:p="` + markup.XNamespace + `">` + "\n" +
				`  <p:Property Name="Title" Type="string" Default="hi"/>` + "\n" +
				`  <Canvas Name="Root">` + "\n" +
				`    <Button Name="B" Content="go"/>` + "\n" +
				`  </Canvas>` + "\n</Gooey>\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := markup.Build([]byte(tc.doc), &markup.Context{}); err != nil {
				t.Fatalf("markup refuses the fixture itself, so the editor is not "+
					"what this measures: %v", err)
			}
			root := workspaceFixture(t)
			if err := os.WriteFile(filepath.Join(root, "prop.gooey"), []byte(tc.doc), 0o644); err != nil {
				t.Fatal(err)
			}
			ed, _ := buildPage(t)
			ed.setDispatcher(gooey.NewDispatcher())
			ed.setWorkspace(root)
			ed.openWorkspaceFile("prop.gooey")

			src := ed.source.Get()
			if _, err := markup.Build([]byte(src), &markup.Context{}); err != nil {
				t.Errorf("the source the editor would save does not load:\n%s\n%v", src, err)
			}
			// And the declaration is still on the envelope rather than
			// having been rescued by moving it under the content root,
			// which loads for a different reason.
			head, _, ok := strings.Cut(src, "<Canvas")
			if !ok {
				t.Fatalf("the saved source has no content root:\n%s", src)
			}
			if !strings.Contains(head, ":Property") {
				t.Errorf("the declaration is written below the content root, not on "+
					"the envelope:\n%s", src)
			}
			// AND NOTHING RE-BINDS THE DEFAULT NAMESPACE. A declaration
			// that named itself with a default xmlns is re-emitted
			// PREFIXED, so the old binding names nothing and re-points
			// the default namespace for whatever follows it — a
			// different document from the one that was opened. It loads
			// either way, which is why the loader cannot be the
			// assertion here.
			if strings.Contains(head, `xmlns="`+markup.XNamespace+`"`) {
				t.Errorf("the re-prefixed declaration kept the default xmlns that "+
					"used to name it:\n%s", src)
			}
		})
	}
}

// TestPastingADocumentThatDeclaresAPropertySaysWhy. unwrapGooey refuses
// an envelope carrying declarations, deliberately and for a reason its
// comment gives at length — and none of that reached the user. The
// envelope fell through to insertSubtree, which reported
// "markup: unknown element <Gooey>" to somebody who had just copied a
// valid file. Raised in review of #522.
func TestPastingADocumentThatDeclaresAPropertySaysWhy(t *testing.T) {
	ed, _ := buildPage(t)
	ed.setDispatcher(gooey.NewDispatcher())
	ed.pasteMarkup(xPropertyDoc)

	got := ed.status.Get()
	if strings.Contains(got, "unknown element") {
		t.Fatalf("the paste refusal still reports %q — <Gooey> is not an unknown "+
			"element, it is the envelope, and the reason it is refused is the "+
			"declaration it carries", got)
	}
	for _, want := range []string{"declares", "property", "envelope"} {
		if !strings.Contains(got, want) {
			t.Errorf("the paste refusal reads %q and does not mention %q", got, want)
		}
	}
}
