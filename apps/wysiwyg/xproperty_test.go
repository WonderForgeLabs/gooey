package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
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

// TestTheRootCountRefusalSaysWhatItCounted covers the branch nothing
// asserted.
//
// The refusal's prefix derivation, its count and its pluralisation were
// all rewritten across two rounds of review with no test on any of them —
// `grep "not root elements" *_test.go` matched a comment. What shipped
// was "its 1 <p:Property> declaration is not root elements": the noun
// carried the verb and the trailing literal stayed plural.
//
// Three arms, because there are three things the message derives: the
// author's own prefix, the agreement of the whole tail, and the unbound
// case where no prefix exists to name. Raised in review of #522.
func TestTheRootCountRefusalSaysWhatItCounted(t *testing.T) {
	for _, tc := range []struct {
		name string
		doc  string
		want []string
		not  []string
	}{
		{
			name: "one declaration, no content root, the author's own prefix",
			doc: `<Gooey xmlns:p="` + markup.XNamespace + `">` + "\n" +
				`  <p:Property Name="Title" Type="string" Default="hi"/>` + "\n" +
				`</Gooey>` + "\n",
			want: []string{"found 0", "its 1 <p:Property> declaration is not a root element"},
			not:  []string{"<x:Property>", "are not root elements"},
		},
		{
			name: "two declarations and two content roots",
			doc: `<Gooey xmlns:x="` + markup.XNamespace + `">` + "\n" +
				`  <x:Property Name="A" Type="string" Default="a"/>` + "\n" +
				`  <x:Property Name="B" Type="string" Default="b"/>` + "\n" +
				`  <Canvas Name="One"/>` + "\n" +
				`  <Canvas Name="Two"/>` + "\n" +
				`</Gooey>` + "\n",
			want: []string{"found 2", "its 2 <x:Property> declarations are not root elements"},
			not:  []string{"declaration is"},
		},
		{
			// THE DECLARATION NAMED BY THE DEFAULT xmlns, which binds no
			// prefix at all. declBinding's fallback is "x", and writing
			// it here names an element this file does not contain.
			name: "no prefix bound",
			doc: `<Gooey xmlns="` + markup.XNamespace + `">` + "\n" +
				`  <Property Name="Title" Type="string" Default="hi"/>` + "\n" +
				`</Gooey>` + "\n",
			want: []string{"its 1 <Property> declaration is not a root element"},
			not:  []string{"<x:Property>"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := workspaceFixture(t)
			if err := os.WriteFile(filepath.Join(root, "r.gooey"), []byte(tc.doc), 0o644); err != nil {
				t.Fatal(err)
			}
			ed, _ := buildPage(t)
			ed.setDispatcher(gooey.NewDispatcher())
			ed.setWorkspace(root)
			ed.openWorkspaceFile("r.gooey")

			got := ed.status.Get()
			if !strings.HasPrefix(got, "✗") {
				t.Fatalf("the document has no single content root and was accepted: %q", got)
			}
			for _, w := range tc.want {
				if !strings.Contains(got, w) {
					t.Errorf("the refusal reads %q, want it to contain %q", got, w)
				}
			}
			for _, n := range tc.not {
				if strings.Contains(got, n) {
					t.Errorf("the refusal reads %q, which contains %q — that names "+
						"something the author's file does not", got, n)
				}
			}
		})
	}
}

// TestPastingAWholeDocumentSaysWhy is the case unwrapGooey's own comment
// is written about, and the one the first repair left reporting
// "markup: unknown element <Gooey>".
//
// That string is what this branch calls a defect when a pasted envelope
// carries declarations. A pasted envelope with two roots reached
// insertSubtree and produced it verbatim, because the explanation lived
// in pasteMarkup and tested only the declaration arm while unwrapGooey
// refused on three. Raised in review of #522.
func TestPastingAWholeDocumentSaysWhy(t *testing.T) {
	ed, _ := buildPage(t)
	ed.setDispatcher(gooey.NewDispatcher())
	ed.pasteMarkup("<Gooey>\n  <Text Text=\"a\"/>\n  <Text Text=\"b\"/>\n</Gooey>\n")

	got := ed.status.Get()
	if strings.Contains(got, "unknown element <Gooey>") {
		t.Fatalf("pasting a whole document reports %q — the loader's noun for a "+
			"thing the editor knows perfectly well, handed to somebody who has "+
			"just copied a valid file", got)
	}
	for _, w := range []string{"not pasted", "2 root elements"} {
		if !strings.Contains(got, w) {
			t.Errorf("the refusal reads %q, want it to contain %q", got, w)
		}
	}
}

// xPropertyUsedDoc is the shape xPropertyDoc deliberately is not: a
// control that BINDS the property it declares. Content="{{.Title}}" is
// the whole of #7's surface — a declaration nothing references buys the
// author nothing — and it is the shape the editor still refused after
// #517 made the file open.
const xPropertyUsedDoc = `<Gooey xmlns:x="` + markup.XNamespace + `">` + "\n" +
	`  <x:Property Name="Title" Type="string" Default="hi"/>` + "\n" +
	`  <x:Property Name="Who" Type="string" Required="true"/>` + "\n" +
	`  <Canvas Name="Root">` + "\n" +
	`    <Button Name="B" Content="{{.Title}}"/>` + "\n" +
	`    <Button Name="W" Content="{{.Who}}"/>` + "\n" +
	`  </Canvas>` + "\n" +
	`</Gooey>` + "\n"

// TestADocumentThatUsesWhatItDeclaresPreviews is the inverse of
// TestADocumentDeclaringAPropertyOpens' control arm, and the inversion
// is the point.
//
// There the loader took the document, so the refusal was the editor's.
// Here the loader REFUSES it — there is no instantiation site, so
// nothing fills Values and the control's own binding does not resolve —
// and the editor has to do something the loader does not: seed the
// declared defaults (seedDeclared). Asserting only "the editor opens
// it" would pass just as well if markup.Build had quietly started
// instantiating top-level declarations, which is a different fix in a
// different package.
//
// The Default is read off the BUILT component, not off the status line.
// "✓ builds" is satisfied by a handle holding the zero string, and a
// preview that silently shows an empty button for a declared
// Default="hi" is the failure this is really about. Raised in review of
// #522.
func TestADocumentThatUsesWhatItDeclaresPreviews(t *testing.T) {
	if _, err := markup.Build([]byte(xPropertyUsedDoc), &markup.Context{}); err == nil {
		t.Fatal("markup.Build now resolves a top-level declaration on its own; " +
			"seedDeclared exists because it did not, so both it and this test need revisiting")
	} else if !strings.Contains(err.Error(), `"Title" not found in context`) {
		t.Fatalf("the premise is the unresolved binding, and the loader refused "+
			"this document for another reason: %v", err)
	}

	root := workspaceFixture(t)
	if err := os.WriteFile(filepath.Join(root, "used.gooey"), []byte(xPropertyUsedDoc), 0o644); err != nil {
		t.Fatal(err)
	}

	ed, _ := buildPage(t)
	ed.setDispatcher(gooey.NewDispatcher())
	ed.setWorkspace(root)
	ed.openWorkspaceFile("used.gooey")

	if got := ed.status.Get(); !strings.HasPrefix(got, "✓") {
		t.Fatalf("a control that binds the property it declares reports %q, "+
			"want a build", got)
	}
	btn := map[string]*components.Button{}
	walkNode(ed.doc(), func(n *node) {
		if b, ok := ed.compOf[n].(*components.Button); ok {
			btn[n.Attrs["Name"]] = b
		}
	})
	if btn["B"] == nil || btn["W"] == nil {
		t.Fatalf("the document built but its buttons are not in compOf: %v", btn)
	}
	if got := btn["B"].Content.Get(); got != "hi" {
		t.Errorf("the previewed button reads %q, want the declared Default %q — "+
			"a zero handle builds too, and shows the author nothing", got, "hi")
	}
	// REQUIRED IS NOT A REFUSAL HERE, and the status check above is what
	// pins it: Required means an instantiation site must pass the
	// attribute, and a tool holding the definition alone is not one. The
	// zero string is the stand-in, which is also what the site that
	// forgot it would show.
	if got := btn["W"].Content.Get(); got != "" {
		t.Errorf("a Required property with no site previews as %q, want the "+
			"type's zero", got)
	}
}

// TestADeclaredNameDoesNotOutliveItsDocument pins the retirement half of
// seedDeclared. docCtx.Values is the editor's ONE binding map, so a
// name seeded for the file that was open a moment ago would otherwise
// still resolve in the next one — and the next document would build
// against a property it never declared, then break for whoever opened
// it anywhere else.
func TestADeclaredNameDoesNotOutliveItsDocument(t *testing.T) {
	root := workspaceFixture(t)
	for name, src := range map[string]string{
		"used.gooey":  xPropertyUsedDoc,
		"plain.gooey": "<Gooey>\n  <Canvas Name=\"Root\"/>\n</Gooey>\n",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	ed, _ := buildPage(t)
	ed.setDispatcher(gooey.NewDispatcher())
	ed.setWorkspace(root)

	ed.openWorkspaceFile("used.gooey")
	if _, ok := ed.docCtx.Values["Title"]; !ok {
		t.Fatal("the declaring document is open and Title is not bindable; " +
			"the rest of this test would pass vacuously")
	}
	ed.openWorkspaceFile("plain.gooey")
	if _, ok := ed.docCtx.Values["Title"]; ok {
		t.Error("Title still resolves with a document open that never declared it")
	}
	// And the binding really is gone, not merely absent from the map:
	// the previous document is the one thing that proves the name was
	// ever live.
	if _, err := markup.Build([]byte(xPropertyUsedDoc), ed.docCtx); err == nil {
		t.Error("the previous document still builds against the editor's context, " +
			"so its declaration outlived it")
	}
}

// TestADeclarationDoesNotCaptureAnEditorBinding is the other half of
// seedDeclared living in a SHARED map.
//
// menuValues registers the IDE shell's bindings under bare names —
// Region, CodeView, Save — in the same ed.ctx.Values that docCtx holds
// by reference, and a document is free to declare a property called any
// of them. Seeding over one would point the menus at a string source,
// and retiring it on the next open would then unbind them outright, in
// a session whose user had only opened a file.
func TestADeclarationDoesNotCaptureAnEditorBinding(t *testing.T) {
	const shadows = `<Gooey xmlns:x="` + markup.XNamespace + `">` + "\n" +
		`  <x:Property Name="Region" Type="string" Default="zzz"/>` + "\n" +
		`  <Canvas Name="Root"/>` + "\n" +
		`</Gooey>` + "\n"

	root := workspaceFixture(t)
	if err := os.WriteFile(filepath.Join(root, "shadow.gooey"), []byte(shadows), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "plain.gooey"), []byte("<Gooey>\n  <Canvas Name=\"Root\"/>\n</Gooey>\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ed, _ := buildPage(t)
	ed.setDispatcher(gooey.NewDispatcher())
	ed.setWorkspace(root)

	// The premise: Region is one of the shell's own, and this fixture is
	// only interesting while that is true.
	if ed.docCtx.Values["Region"] != any(ed.region) {
		t.Fatal("Region is no longer the editor's own binding; pick another " +
			"name from menuValues or this test shadows nothing")
	}
	ed.openWorkspaceFile("shadow.gooey")
	if got := ed.docCtx.Values["Region"]; got != any(ed.region) {
		t.Errorf("opening a document that declares Region replaced the editor's "+
			"binding with %T", got)
	}
	ed.openWorkspaceFile("plain.gooey")
	if got := ed.docCtx.Values["Region"]; got != any(ed.region) {
		t.Errorf("closing that document left Region as %v — a name the editor "+
			"owns was retired with the document that shadowed it", got)
	}
}

// TestTheEditorSaysWhatMarkupWouldAboutABareProperty is finding 2 of
// #522's round 1, and the finding is that the editor answered a
// DIFFERENT question from the one the document asks.
//
// markup's splitDeclarations has three arms and the editor kept two.
// The missing one is the likely typo — a <Property> with no namespace —
// and markup diagnoses it by name. The editor counted it as a root
// element instead:
//
//	<Gooey><Property …/><Canvas/></Gooey>
//	  -> ✗ a <Gooey> document needs exactly one root element, found 2
//
// which reads as "delete one of your roots" about a file whose second
// root is the declaration.
//
// THE ONE-KID SPELLING WAS NOT BETTER, and the review that raised this
// assumed it was — "it passes the count and fails at Build, and that
// asymmetry is the tell". Measured, it does not: the envelope is
// unwrapped first, so the declaration is built INSIDE the editor's
// surface and markup answers `unknown element <Property>`, which names
// no remedy at all. Both spellings are arms here for that reason.
//
// THE SENTENCE IS CHECKED AGAINST markup'S OWN, not against a copy of
// it. The editor cannot call splitDeclarations, so it restates the
// advice; what keeps the restatement honest is asking markup for its
// error on the same source and requiring the parts an author must act
// on to appear in both.
func TestTheEditorSaysWhatMarkupWouldAboutABareProperty(t *testing.T) {
	for _, tc := range []struct {
		name, doc string
		plural    bool
	}{
		{
			name: "a declaration beside a content root",
			doc: "<Gooey>\n" +
				`  <Property Name="Title" Type="string" Default="hi"/>` + "\n" +
				`  <Canvas Name="Root"/>` + "\n" +
				"</Gooey>\n",
		},
		{
			name: "a declaration alone",
			doc: "<Gooey>\n" +
				`  <Property Name="Title" Type="string" Default="hi"/>` + "\n" +
				"</Gooey>\n",
		},
		{
			// THE PLURAL BRANCH, because the last untested
			// pluralisation in this file shipped saying "its 1
			// <p:Property> declaration is not root elements".
			name:   "two declarations",
			plural: true,
			doc: "<Gooey>\n" +
				`  <Property Name="A" Type="string" Default="a"/>` + "\n" +
				`  <Property Name="B" Type="string" Default="b"/>` + "\n" +
				`  <Canvas Name="Root"/>` + "\n" +
				"</Gooey>\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := markup.Build([]byte(tc.doc), &markup.Context{})
			if err == nil {
				t.Fatal("markup now accepts an unprefixed <Property>, so there is " +
					"nothing for the editor to agree with")
			}
			// The parts of markup's advice an author has to act on. Read
			// off the loader's message rather than written here, so the
			// day markup changes its mind this test says so.
			for _, want := range []string{"<x:Property>", `xmlns:x="` + markup.XNamespace + `"`} {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("markup's own diagnosis no longer contains %q, so this "+
						"test is checking the editor against nothing: %v", want, err)
				}
			}

			root := workspaceFixture(t)
			if err := os.WriteFile(filepath.Join(root, "r.gooey"), []byte(tc.doc), 0o644); err != nil {
				t.Fatal(err)
			}
			ed, _ := buildPage(t)
			ed.setDispatcher(gooey.NewDispatcher())
			ed.setWorkspace(root)
			ed.openWorkspaceFile("r.gooey")

			got := ed.status.Get()
			if !strings.HasPrefix(got, "✗") {
				t.Fatalf("an unprefixed <Property> was accepted: %q", got)
			}
			for _, want := range []string{"<x:Property>", `xmlns:x="` + markup.XNamespace + `"`} {
				if !strings.Contains(got, want) {
					t.Errorf("the editor's refusal reads %q and does not carry %q — "+
						"the author is told something other than what would fix the "+
						"file", got, want)
				}
			}
			for _, not := range []string{"root element, found", "unknown element"} {
				if strings.Contains(got, not) {
					t.Errorf("the editor's refusal reads %q, which answers a "+
						"different question from the one the document asks", got)
				}
			}
			// THE WHOLE TAIL AGREES. The last untested pluralisation in
			// this file shipped reading "its 1 <p:Property> declaration
			// is not root elements", so the count and the verb are
			// asserted rather than assumed.
			want, wrong := "write it as", "write them as"
			if tc.plural {
				want, wrong = "write them as", "write it as"
			}
			if !strings.Contains(got, want) || strings.Contains(got, wrong) {
				t.Errorf("the refusal reads %q; for %d declarations it should say "+
					"%q and not %q", got, strings.Count(tc.doc, "<Property "), want, wrong)
			}
		})
	}
}

// TestASeededNameIsVisibleToTheControlPlaneAndIsTransient measures
// finding 4 of #522's round 1, which is a consequence of the shared map
// that seedDeclared's doc reasoned about only through menuValues.
//
// ed.docCtx.Values IS ed.ctx.Values and both servers are handed ed.ctx,
// so a seeded name is in the vocabulary the control plane validates and
// patches against. Both halves are asserted because the first is the
// reason the second is tolerated: the binding pickers read the same map,
// which is what puts {{.Title}} in front of the author while the
// declaring document is open.
//
// The transience is the half a client cannot see: a value set against a
// seeded name survives until the next rebuild and no longer, because the
// seed loop installs a fresh handle from Default every time.
func TestASeededNameIsVisibleToTheControlPlaneAndIsTransient(t *testing.T) {
	root := workspaceFixture(t)
	if err := os.WriteFile(filepath.Join(root, "used.gooey"), []byte(xPropertyUsedDoc), 0o644); err != nil {
		t.Fatal(err)
	}
	ed, _ := buildPage(t)
	ed.setDispatcher(gooey.NewDispatcher())
	ed.setWorkspace(root)
	ed.openWorkspaceFile("used.gooey")

	// THE SERVERS' CONTEXT, not docCtx — that is the whole finding.
	v, ok := ed.ctx.Values["Title"]
	if !ok {
		t.Fatal("a declared name is not in the context the servers are handed, " +
			"so this test measures nothing")
	}
	p, ok := v.(*prop.Property[string])
	if !ok {
		t.Fatalf("the seeded handle is %T, not a string property", v)
	}
	p.Set("set by a client")
	if got := p.Get(); got != "set by a client" {
		t.Fatalf("the seeded handle did not take a write: %q", got)
	}

	ed.rebuild()

	w, ok := ed.ctx.Values["Title"].(*prop.Property[string])
	if !ok {
		t.Fatal("the name left the context on a rebuild")
	}
	if w.Get() != "hi" {
		t.Errorf("the seeded handle still reads %q after a rebuild; this test "+
			"asserts the OPPOSITE — a client's write is discarded, and if that "+
			"has changed the doc on seedDeclared is now wrong", w.Get())
	}
}

// TestSeedingDoesNotRunOnTheRemotePath is finding 5 of #522's round 1:
// rebuild returns on the remote branch before it reaches seedDeclared,
// so #517's build half is local preview only.
//
// It is the right behaviour — the target's context is the authority and
// the editor cannot seed one it does not own — and it is asserted rather
// than left implied, because seedDeclared's doc reads as unconditional
// and nothing else says where it stops.
func TestSeedingDoesNotRunOnTheRemotePath(t *testing.T) {
	ed, _ := attachedEditor(t)
	ed.envAttrs = map[string]string{"xmlns:x": markup.XNamespace}
	ed.envDecls = []*node{{
		Elem:  "Property",
		Space: markup.XNamespace,
		Attrs: map[string]string{"Name": "Title", "Type": "string", "Default": "hi"},
	}}
	ed.root.Kids = []*node{{Elem: "Text", Attrs: map[string]string{"Name": "T"}}}
	ed.rebuild()

	if _, ok := ed.docCtx.Values["Title"]; ok {
		t.Error("the editor seeded a declared name while driving another app — " +
			"the preview would then resolve a binding the target cannot, and " +
			"agree with itself about a document that does not load there")
	}
	if len(ed.seededDecls) != 0 {
		t.Errorf("seededDecls is %v on the remote path, so a later local rebuild "+
			"would retire names that were never installed", ed.seededDecls)
	}
}
