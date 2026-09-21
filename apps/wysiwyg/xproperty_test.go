package main

import (
	"os"
	"path/filepath"
	"regexp"
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
	for _, tc := range []struct{ name, prefix, doc, keeps string }{
		{
			// A ROUND-TRIP PIN, AND NOT A PIN ON THE BINDING WRITE,
			// which is what this comment used to claim: "envelopeAttrs
			// drops the envelope's xmlns:x because the content root
			// repeats it". That was true when it was written and
			// stopped being true at a base merge — carryDeclarations
			// skips v == markup.XNamespace, so the binding is never in
			// the moved set and envelopeAttrs keeps it. This document
			// therefore takes bound == true and never reaches
			// withDeclBinding; disabling that write leaves this arm
			// green. WHICH ARMS DO REACH IT IS NOT LISTED HERE: it was,
			// and the decline-and-mint routes added later in this same
			// branch made the list short by two without anything going
			// red. Any arm whose declPrefix answers bound == false
			// reaches the write, so run the arms and ask. What this arm
			// is worth keeping for is the duplicate binding surviving
			// the round trip at all.
			"bound on both the envelope and the content root",
			"x",
			`<Gooey xmlns:x="` + markup.XNamespace + `">` + "\n" +
				`  <x:Property Name="Title" Type="string" Default="hi"/>` + "\n" +
				`  <Canvas Name="Root" xmlns:x="` + markup.XNamespace + `">` + "\n" +
				`    <Button Name="B" Content="go"/>` + "\n" +
				`  </Canvas>` + "\n</Gooey>\n",
			"",
		},
		{
			// No prefix anywhere: the declaration names itself with a
			// default xmlns, which the re-prefixed copy must not keep.
			"bound as the declaration's own default xmlns",
			"x", // nothing to keep, so the minted spelling is the answer
			`<Gooey>` + "\n" +
				`  <Property xmlns="` + markup.XNamespace + `" Name="Title" Type="string" Default="hi"/>` + "\n" +
				`  <Canvas Name="Root">` + "\n" +
				`    <Button Name="B" Content="go"/>` + "\n" +
				`  </Canvas>` + "\n</Gooey>\n",
			"",
		},
		{
			// The author's own prefix survives, which is what declBinding
			// was added for and must keep doing.
			"bound to a prefix that is not x",
			"p",
			`<Gooey xmlns:p="` + markup.XNamespace + `">` + "\n" +
				`  <p:Property Name="Title" Type="string" Default="hi"/>` + "\n" +
				`  <Canvas Name="Root">` + "\n" +
				`    <Button Name="B" Content="go"/>` + "\n" +
				`  </Canvas>` + "\n</Gooey>\n",
			"",
		},
		{
			// AND THE SECOND LEGAL PLACEMENT. XML scoping lets the
			// binding sit on the declaration ELEMENT rather than on
			// <Gooey>; markup/property.go records both and
			// TestTheXPropertyRefusalNamesTheRoot pins all three. Reading
			// only the envelope, this round-tripped lossily — p: became a
			// minted x:, a new xmlns:x appeared on <Gooey>, and the
			// author's xmlns:p was left on the declaration naming
			// nothing. It still LOADED, which is why the loader assertion
			// above cannot see it and the prefix assertion below can.
			// Raised in review of #522.
			"bound on the declaration element itself",
			"p",
			`<Gooey>` + "\n" +
				`  <p:Property xmlns:p="` + markup.XNamespace + `" Name="Title" Type="string" Default="hi"/>` + "\n" +
				`  <Canvas Name="Root">` + "\n" +
				`    <Button Name="B" Content="go"/>` + "\n" +
				`  </Canvas>` + "\n</Gooey>\n",
			"",
		},
		{
			// AND A BINDING OF THE SAVE PREFIX TO SOMETHING ELSE, which
			// is the shape that was a BUG rather than a residue. XML
			// scoping lets the declaration rebind p: for itself; the
			// envelope binds p: to the x namespace, so p: is what
			// envelopeHead writes the copy under — and the declaration's
			// own xmlns:p, left in place, made that p: resolve to
			// urn:other. markup.Build accepts this document and refused
			// the saved one, under "✓ saved". Raised in review of #522.
			//
			// THIS ARM EXPECTED "p" AND NO `keeps`, AND THAT WAS THE
			// REMAINING HALF OF THE SAME LOSS. Dropping the
			// declaration's xmlns:p is what keeps the emitted element a
			// declaration, so the first fix was right about the
			// element — and markup resolves value expressions through
			// one flat document-ORDER map, in which the declaration's
			// xmlns:p comes last. So {{p:Thing}} already means
			// urn:other in the OPENED file, and a save that keeps p
			// for the declaration re-points it to the declaration
			// namespace: the document changes meaning under "✓ saved",
			// exactly as the two arms below it do.
			//
			// declPrefix declines the envelope's prefix on the same
			// grounds it declines an adopted one now, mints a free
			// spelling, and the author's binding survives — so the
			// expectation moves to "x" and `keeps` gains the binding.
			// Raised in review of #522, twice: the first round fixed
			// the element and left the document. Changing a green
			// fixture is the right call only when the fixture is the
			// claim being corrected, and here it is.
			"bound on the declaration element to a DIFFERENT namespace",
			"x",
			`<Gooey xmlns:p="` + markup.XNamespace + `">` + "\n" +
				`  <Property xmlns="` + markup.XNamespace + `" xmlns:p="urn:other" Name="Title" Type="string" Default="hi"/>` + "\n" +
				`  <Canvas Name="Root">` + "\n" +
				`    <Button Name="B" Content="go"/>` + "\n" +
				`  </Canvas>` + "\n</Gooey>\n",
			`xmlns:p="urn:other"`,
		},
		{
			// THE SAME LOSS WITH NO ENVELOPE BINDING AT ALL, reached the
			// other way: declPrefix MINTS a prefix, and the collision
			// loop read only the envelope's attrs — so it minted x
			// while the declaration itself bound x to something else.
			//
			// THE MINT AVOIDS IT NOW, and this arm expected "x" until
			// review of #522 measured what that cost. markup's table
			// for value expressions is one flat document-wide map, so
			// declAttrs' third clause dropping the declaration's own
			// xmlns:x does not merely remove a residue — it removes a
			// binding the rest of the document may be USING, and the
			// saved file stops loading under "✓ saved". Minting x2
			// keeps the author's binding and names the declaration, so
			// both survive; `keeps` is what asserts the half the
			// generic assertions below cannot see.
			// AND THE SAME LOSS ON THE ADOPTED ROUTE, which the mint's
			// fix does not reach. declPrefix takes the first
			// declaration carrying a binding and never asks whether a
			// SIBLING declaration spends that prefix on something
			// else; declAttrs' first clause then drops the sibling's
			// xmlns:p, because on an emitted <p:Property> a xmlns:p
			// naming anything else would unname the element. The
			// document uses {{p:Thing}}, so the saved file stops
			// loading — the third shape reaching that clause, where
			// declAttrs' doc claimed there were two. Raised in review
			// of #522.
			"a prefix a SIBLING declaration binds elsewhere",
			"x",
			`<Gooey>` + "\n" +
				`  <p:Property xmlns:p="` + markup.XNamespace + `" Name="A" Type="string" Default="a"/>` + "\n" +
				`  <Property xmlns="` + markup.XNamespace + `" xmlns:p="urn:other" Name="B" Type="string" Default="b"/>` + "\n" +
				`  <Canvas Name="Root">` + "\n" +
				`    <Button Name="B2" Content="go"/>` + "\n" +
				`  </Canvas>` + "\n</Gooey>\n",
			`xmlns:p="urn:other"`,
		},
		{
			"a minted prefix the declaration itself binds elsewhere",
			"x2",
			`<Gooey>` + "\n" +
				`  <Property xmlns="` + markup.XNamespace + `" xmlns:x="urn:other" Name="Title" Type="string" Default="hi"/>` + "\n" +
				`  <Canvas Name="Root">` + "\n" +
				`    <Button Name="B" Content="go"/>` + "\n" +
				`  </Canvas>` + "\n</Gooey>\n",
			`xmlns:x="urn:other"`,
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
			// AND THE AUTHOR'S PREFIX IS THE ONE WRITTEN BACK. The
			// assertions above pass for any spelling that loads, which
			// is how the element-level placement round-tripped as a
			// minted x: with nothing going red. Raised in review of
			// #522.
			if !strings.Contains(head, "<"+tc.prefix+":Property") {
				t.Errorf("the declaration is written under a prefix other than the "+
					"%q this document binds:\n%s", tc.prefix, src)
			}
			// AND A BINDING THE DOCUMENT WAS USING IS STILL THERE. The
			// prefix the declaration spends on something else is not
			// this editor's to reclaim: markup resolves value
			// expressions through one flat document-wide table, so
			// dropping it changes what {{x:Fire}} means anywhere in the
			// file. Raised in review of #522.
			if tc.keeps != "" && !strings.Contains(src, tc.keeps) {
				t.Errorf("the save dropped %s, which the document binds and may "+
					"be using — the minted prefix took a name that was already "+
					"spent:\n%s", tc.keeps, src)
			}
			// AND NO BINDING IS LEFT NAMING NOTHING. The dead xmlns:p a
			// re-prefixing leaves behind is the same residue as the
			// default binding above, one spelling over.
			for _, m := range regexp.MustCompile(`xmlns:([A-Za-z0-9_.-]+)="`+
				regexp.QuoteMeta(markup.XNamespace)+`"`).FindAllStringSubmatch(head, -1) {
				if m[1] != tc.prefix {
					t.Errorf("the saved head binds %q to the declaration namespace, "+
						"which names nothing — the declaration is written as %q:\n%s",
						m[1], tc.prefix, src)
				}
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
			// prefix at all, so the refusal must spell it <Property> —
			// which is what the file contains. declElemName passes
			// declSpelling an empty fallback for exactly this row.
			name: "no prefix bound",
			doc: `<Gooey xmlns="` + markup.XNamespace + `">` + "\n" +
				`  <Property Name="Title" Type="string" Default="hi"/>` + "\n" +
				`</Gooey>` + "\n",
			want: []string{"its 1 <Property> declaration is not a root element"},
			not:  []string{"<x:Property>"},
		},
		{
			// THE BINDING ON THE DECLARATION ITSELF, which is the
			// placement declPrefix was written for and this branch's
			// save path already handles. declBinding reads the ENVELOPE
			// only, so it reported the file as containing <Property> —
			// and that spelling is not neutral, because bareDeclMsg in
			// this same editor tells an author that a bare <Property>
			// means they forgot the namespace. A correctly namespaced
			// document was described with the one spelling the editor
			// elsewhere calls a typo. Raised in review of #522.
			name: "the binding is on the declaration, not the envelope",
			doc: `<Gooey>` + "\n" +
				`  <p:Property xmlns:p="` + markup.XNamespace + `" Name="Title" Type="string" Default="hi"/>` + "\n" +
				`</Gooey>` + "\n",
			want: []string{"found 0", "its 1 <p:Property> declaration is not a root element"},
			not:  []string{"<x:Property>", "<Property>", "are not root elements"},
		},
		{
			// TWO DECLARATIONS UNDER TWO BINDINGS, which is the arm
			// declPrefix exists for and the one none of the four above
			// has. declPrefix answers a SAVE-path question — which
			// prefix will the save write, and does the document already
			// bind it — so its bound=false means "the envelope needs a
			// binding added at write time", not "the file writes
			// <Property> unprefixed". Reading it as the second handed a
			// correctly namespaced document the bare spelling
			// bareDeclMsg defines as the missing-namespace typo.
			// Raised in review of #522.
			name: "two declarations carrying two different bindings",
			doc: `<Gooey xmlns="wonderforge.io/gooey/2026">` + "\n" +
				`  <p:Property xmlns:p="` + markup.XNamespace + `" Name="A" Type="string"/>` + "\n" +
				`  <q:Property xmlns:q="` + markup.XNamespace + `" Name="B" Type="string"/>` + "\n" +
				`</Gooey>` + "\n",
			want: []string{"found 0", "<p:Property>", "<q:Property>", "are not root elements"},
			not:  []string{"<x:Property>", "2 <p:Property>", "2 <q:Property>"},
		},
		{
			// THE MIXED CASE ASSERTED AS A COUNT, which is the half the
			// arm above could not state while it expected decls[0]'s
			// spelling for both. The file holds one <p:Property> and
			// one <q:Property>; "2 <p:Property> declarations" is a
			// count of elements it does not contain, and it was the
			// expectation here for two rounds. Raised in review of
			// #522.
			name: "three declarations, two of them sharing a binding",
			doc: `<Gooey xmlns:p="` + markup.XNamespace + `">` + "\n" +
				`  <p:Property Name="A" Type="string"/>` + "\n" +
				`  <p:Property Name="B" Type="string"/>` + "\n" +
				`  <q:Property xmlns:q="` + markup.XNamespace + `" Name="C" Type="string"/>` + "\n" +
				`</Gooey>` + "\n",
			want: []string{"found 0", "its 3 declarations", "<p:Property>, <p:Property>, <q:Property>"},
			not:  []string{"3 <p:Property>", "<x:Property>"},
		},
		{
			// AND THE AGREEING CASE KEEPS THE OLD WORDING, so the list
			// form is reached only where a single spelling would lie.
			name: "two declarations sharing one binding on the elements",
			doc: `<Gooey>` + "\n" +
				`  <p:Property xmlns:p="` + markup.XNamespace + `" Name="A" Type="string"/>` + "\n" +
				`  <p:Property xmlns:p="` + markup.XNamespace + `" Name="B" Type="string"/>` + "\n" +
				`</Gooey>` + "\n",
			want: []string{"found 0", "its 2 <p:Property> declarations are not root elements"},
			not:  []string{"declarations —", "<x:Property>"},
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
//
// AND THE STATUS SAYS SO, which is the half this test was missing. The
// skip is right and it is INVISIBLE: the binding resolves, the tree
// builds, the status said "✓ builds" — and the handle is the IDE's
// *prop.Property[int] region enum, so Default="zzz" never appears, the
// declared Type="string" is not what the binding resolved to, and a
// <Text>{{.Region}}</Text> in the user's document renders "0". An
// editor implementation detail, shown inside the document, under a
// green status. The author's only other route to the diagnosis is to
// know menuValues' list, which is not in their file.
//
// THE SECOND HALF IS THE RETIREMENT, and it is a separate claim: the
// note is derived from a slice that outlives one rebuild, so a document
// that shadows nothing must clear it. Opening plain.gooey takes
// seedDeclared's early return, which is exactly where a note left over
// from the previous document would survive. Raised in review of #522.
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
	if got := ed.status.Get(); !strings.Contains(got, "Region") {
		t.Errorf("the status reads %q after opening a document whose declaration "+
			"the editor silently kept out of the vocabulary; the preview is "+
			"showing the editor's own value for Region and nothing says so", got)
	}
	ed.openWorkspaceFile("plain.gooey")
	if got := ed.docCtx.Values["Region"]; got != any(ed.region) {
		t.Errorf("closing that document left Region as %v — a name the editor "+
			"owns was retired with the document that shadowed it", got)
	}
	if got := ed.status.Get(); strings.Contains(got, "Region") {
		t.Errorf("the status still reads %q with a document open that declares "+
			"nothing; the note outlived the document it was about", got)
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

// TestADeclarationOutsideTheEnvelopeIsRefusedBeforeItCanBeSaved is the
// hole the x-namespace exemption opened, and the shape of it is why the
// comment defending the exemption was wrong.
//
// nodeOf refuses a prefixed element because the model cannot write the
// prefix back. The exemption for markup.XNamespace is answered by
// envelopeHead re-deriving the prefix from declPrefix — and that exists
// for the ENVELOPE's children and nothing else. Applied at every depth,
// it let a document in that the save path then rewrote. Measured on the
// branch before this fix:
//
//	status:      ✗ markup: unknown element <Property>
//	openPath:    nest.gooey                  ← the document IS open
//	after ^S:    <Property Name="T" …/>      ← the prefix is gone from the FILE
//
// saveOpenFile is not gated on the build and canSave gates on openPath,
// which the open had set — so ctrl+s on a file the editor was reporting
// an error for rewrote it to a different document. Before this branch
// nodeOf refused the open outright and nothing could rewrite anything,
// which is what makes this a data loss rather than a message defect.
//
// THE ROOT-POSITION ARM IS THE OTHER HALF, and it is the reason the
// exemption cannot simply be "depth 1". nodeOf still accepts a
// declaration as the PARSED ROOT, because that is paste's shape and
// bareDeclWhy owns the refusal there with a message that says what a
// declaration is. A FILE whose root element is one reaches the same
// acceptance — measured, it opened, was wrapped in a <Gooey> and saved
// as <Property> — so openWorkspaceFile refuses that directly.
//
// BOTH ARMS ASSERT THE FILE IS UNCHANGED, not just the status, because
// the status was already a refusal in the nested case and the file was
// rewritten anyway. Raised in review of #522.
func TestADeclarationOutsideTheEnvelopeIsRefusedBeforeItCanBeSaved(t *testing.T) {
	for _, tc := range []struct {
		name, doc string
		want      string
	}{
		{
			name: "under the content root",
			doc: `<Gooey xmlns="wonderforge.io/gooey/2026" xmlns:x="` + markup.XNamespace + `">` + "\n" +
				`  <Canvas Name="Root">` + "\n" +
				`    <x:Property Name="T" Type="string"/>` + "\n" +
				`  </Canvas>` + "\n</Gooey>\n",
			want: "is namespaced",
		},
		{
			name: "as the whole file",
			doc:  `<p:Property xmlns:p="` + markup.XNamespace + `" Name="T" Type="string"/>` + "\n",
			want: "is a dependency property declaration, not a document",
		},
		{
			// AN ALIEN ELEMENT IS NOT A DECLARATION, and the arm above
			// cannot see the difference: the root-position guard tested
			// only the namespace, so this file was called a declaration
			// too — and then told to move it under a <Gooey> root, which
			// is where the editor's own alien refusal is waiting for it
			// (TestAnXNamespacedElementThatIsNotPropertyGetsMarkupsOwnAnswer).
			// bareDeclWhy has asked the element name first since round
			// 5; this is the one site added after it. Raised in review
			// of #522.
			name: "an alien element as the whole file",
			doc:  `<p:Foo xmlns:p="` + markup.XNamespace + `" Name="T"/>` + "\n",
			// THE TAIL IS PART OF THE WANT, and it is the half that was
			// unpinned: matching only the "unknown language element"
			// clause let the file-level sentence be added or removed
			// with nothing red, which is how this arm came to differ
			// from its two whole-file siblings. Raised in review of
			// #522.
			want: "<p:Foo> is an unknown language element; the " +
				markup.XNamespace + " namespace declares <p:Property> only. " +
				"A file whose whole content is one has no document to show",
		},
		{
			// THE UNPREFIXED SPELLING, and it is the typo the whole
			// partition exists to diagnose rather than an edge. The
			// root-position guard asked only the namespace, so this
			// file fell through every arm: it OPENED, with markup's
			// "unknown element <Property>" — a sentence about the
			// document the editor synthesised, not about these bytes,
			// which markup.Build refuses with "root element must be
			// <Gooey>, got <Property>" — and ctrl+s then wrote that
			// synthesised document over the author's file. The disk
			// assertion below is the one that matters here: the status
			// was already a refusal and the rewrite happened anyway.
			// Raised in review of #522.
			//
			// IT DESCRIBES WHERE A DECLARATION BELONGS rather than
			// prescribing an edit, and this arm asserted the
			// prescription until round 17. bareDeclMsg's remedy is
			// "add xmlns:x to the <Gooey> root element", which this
			// file does not have — so an author following it in order
			// arrived at the prefixed arm's refusal instead. The
			// spelling is still named, because that is the diagnosis
			// and it was always true here.
			name: "an unprefixed declaration as the whole file",
			doc:  `<Property Name="T" Type="string"/>` + "\n",
			want: "the spelling is <x:Property>",
		},
		{
			// THE ENVELOPE ITSELF, PREFIXED. nodeOf exempted every
			// x-namespaced element in ROOT position, for the paste
			// path, and the open path's own guard excludes
			// n.Elem == "Gooey" — so this file had no arm at all: the
			// children are unprefixed, splitDecls files them as kids,
			// alienDecls never fires, and the editor reported
			// "✓ builds". ctrl+s then wrote it back as plain <Gooey>,
			// rewriting the root element's resolved namespace on disk
			// under a green status, which is the class the prefixed
			// refusal exists to delete. markup.Build accepts both
			// forms — its root check is on the LOCAL name — so nothing
			// downstream stops it either. Measured in review of #522.
			name: "the envelope itself, prefixed",
			doc: `<x:Gooey xmlns:x="` + markup.XNamespace + `" ` +
				`xmlns="wonderforge.io/gooey/2026">` + "\n" +
				`  <Canvas Name="Root"/>` + "\n</x:Gooey>\n",
			want: "is namespaced",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := workspaceFixture(t)
			path := filepath.Join(root, "d.gooey")
			if err := os.WriteFile(path, []byte(tc.doc), 0o644); err != nil {
				t.Fatal(err)
			}
			ed, _ := buildPage(t)
			ed.setDispatcher(gooey.NewDispatcher())
			ed.setWorkspace(root)
			ed.openWorkspaceFile("d.gooey")

			if got := ed.status.Get(); !strings.Contains(got, tc.want) {
				t.Errorf("the refusal reads %q, want it to contain %q", got, tc.want)
			}
			// openPath is what canSave gates on, so an empty one is the
			// mechanism rather than a corollary: it is why the save
			// below cannot reach this file.
			if got := ed.openPath.Get(); got != "" {
				t.Errorf("openPath is %q after a refused open; ctrl+s then writes "+
					"the editor's document over this file", got)
			}

			ed.saveOpenFile()
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != tc.doc {
				t.Errorf("the file on disk changed:\n--- before\n%s--- after\n%s"+
					"A refused document must not be written back: the prefix the "+
					"model cannot hold is exactly what a rewrite loses", tc.doc, after)
			}
		})
	}
}

// TestAnXNamespacedElementThatIsNotPropertyGetsMarkupsOwnAnswer covers
// the arm splitDecls' namespace key created and every message built from
// it then mis-described.
//
// splitDecls files a child by its NAMESPACE, exactly as markup's
// splitDeclarations does, so <x:Foo> lands in `decls`. Both readers then
// called it a declaration, and the open path spelled the element
// literally: a file holding <x:Foo/> was refused with "its 1
// <x:Property> declaration is not a root element", naming an element the
// file does not contain. markup's own answer is "unknown language
// element <x:Foo>", and the editor now says that — which is the whole
// premise of bareDeclMsg one arm over.
//
// THE LOADER IS THE REFERENCE, not a copy of its sentence: the arms read
// markup.Build's own error first and fail if it stops containing the
// words this checks the editor for. Raised in review of #522.
func TestAnXNamespacedElementThatIsNotPropertyGetsMarkupsOwnAnswer(t *testing.T) {
	// NO CONTENT ROOT BESIDE IT, and that is the discriminator rather
	// than a simpler fixture. splitDecls files <x:Foo> under decls, so a
	// document with one content root has kids == 1, the count branch is
	// never reached, and the open path falls through to markup.Build —
	// which answers correctly on its own. The editor's own message is
	// only reachable when the count branch fires, so a fixture that
	// carries a root measures the loader and calls it the editor.
	// Measured: with the alien arm disabled, the two-child fixture stays
	// GREEN and this one reports "found 0 root elements (its 1 <x:Foo>
	// declaration is not a root element)".
	for _, tc := range []struct {
		name, doc, elem, absent string
	}{
		{
			// The envelope binds it, so the editor can do better than
			// markup and use the author's own spelling.
			name: "bound on the envelope",
			doc: `<Gooey xmlns:x="` + markup.XNamespace + `">` + "\n" +
				`  <x:Foo Name="Title"/>` + "\n</Gooey>\n",
			elem: "<x:Foo>",
		},
		{
			// AND THE ONE ARRANGEMENT WHERE THERE IS NO PREFIX TO USE.
			// The envelope binds xmlns:x to something else and the alien
			// element names the namespace with its own default xmlns, so
			// declBinding finds nothing and MINTS x2 — a prefix that
			// appears nowhere in the file. The refusal used to write it,
			// sending the author to look for <x2:Foo> and to invent a
			// prefix to fix it with. Unbound, the editor says exactly
			// what markup says. Raised in review of #522.
			name: "bound by the element's own default xmlns, under an envelope that binds x elsewhere",
			doc: `<Gooey xmlns:x="urn:something-else">` + "\n" +
				`  <Foo Name="Title" xmlns="` + markup.XNamespace + `"/>` + "\n</Gooey>\n",
			elem:   "<x:Foo>",
			absent: "x2",
		},
		{
			// AND THE BINDING ON THE ELEMENT ITSELF, which is the
			// placement declPrefix exists for and the one this arm was
			// reading past. Both call sites hand alienDeclMsg the
			// ENVELOPE's binding, so a file that binds x: on the
			// envelope and writes <d:Foo> with its own binding was
			// refused as <x:Foo> — worse than the unbound case above,
			// because x: IS bound here, so the message reads as a quote
			// from the document and the author searches for an x:Foo
			// nobody wrote. The question is per-element, so the answer
			// has to be. Raised in review of #522.
			name: "bound on the element itself, under an envelope that also binds x",
			doc: `<Gooey xmlns:x="` + markup.XNamespace + `">` + "\n" +
				`  <d:Foo xmlns:d="` + markup.XNamespace + `" Name="Title"/>` + "\n</Gooey>\n",
			elem:   "<d:Foo>",
			absent: "<x:Foo>",
		},
		{
			// AND THE ELEMENT BINDING WITH NOTHING ON THE ENVELOPE,
			// which is the arm that reaches the message's TAIL. The
			// element half was made per-element above; the tail was
			// left normalising to declFallbackPrefix whenever the
			// ENVELOPE bound nothing, so a file whose only binding is
			// on the element was told the namespace "declares
			// <x:Property> only" — a prefix it would have to invent,
			// which is the defect the element half exists to prevent,
			// one step further out. No arm above mixes an unbound
			// envelope with an element-level binding, which is why
			// they stayed green over it. Raised in review of #522.
			name: "bound on the element, with the envelope binding nothing",
			doc: `<Gooey>` + "\n" +
				`  <d:Foo xmlns:d="` + markup.XNamespace + `" Name="Title"/>` + "\n</Gooey>\n",
			elem:   "<d:Foo>",
			absent: "<x:Property>",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := markupRefuses(t, tc.doc, "unknown language element", "<x:Foo>")

			t.Run("opening", func(t *testing.T) {
				root := workspaceFixture(t)
				if err := os.WriteFile(filepath.Join(root, "foo.gooey"), []byte(tc.doc), 0o644); err != nil {
					t.Fatal(err)
				}
				ed, _ := buildPage(t)
				ed.setDispatcher(gooey.NewDispatcher())
				ed.setWorkspace(root)
				ed.openWorkspaceFile("foo.gooey")

				got := ed.status.Get()
				// NOT "must not mention <x:Property>" — markup's own
				// sentence names it, as the thing the namespace declares
				// INSTEAD. The defect was calling it what the document
				// CONTAINS, which is the count refusal's tail and
				// nothing else.
				for _, bad := range []string{"is not a root element", "are not root elements"} {
					if strings.Contains(got, bad) {
						t.Errorf("the refusal reads %q and counts the element as a "+
							"declaration that is not a root element. markup rejects it "+
							"outright (%v), so there is nothing to count: before the "+
							"element name was read off decls this also spelled it "+
							"<x:Property>, an element the document does not contain",
							got, err)
					}
				}
				assertNames(t, "the refusal", got, tc.elem, tc.absent)
			})

			t.Run("pasting", func(t *testing.T) {
				ed, _ := buildPage(t)
				ed.setDispatcher(gooey.NewDispatcher())
				ed.pasteMarkup(tc.doc)

				got := ed.status.Get()
				if strings.Contains(got, "this document declares") {
					t.Errorf("the paste refusal reads %q and calls the element a "+
						"declaration; markup rejects it outright, so it has no public "+
						"surface to merge and the sentence about merging one is about "+
						"nothing", got)
				}
				assertNames(t, "the paste refusal", got, tc.elem, tc.absent)
			})
		})
	}
}

// TestAPastedAlienElementKeepsItsOwnPrefix is the same rule on the leg
// that has no envelope to read.
//
// bareDeclWhy spelled the prefix "x" outright, so a node pasted with its
// own xmlns:d — which is how one arrives here, since #472 carries a
// pasted node's namespace declarations as ordinary attributes — was
// reported as <x:Foo>, an element the clipboard does not hold. The
// binding is in n.Attrs, where the other two call sites read it from.
// Raised in review of #522.
func TestAPastedAlienElementKeepsItsOwnPrefix(t *testing.T) {
	ed, _ := buildPage(t)
	ed.setDispatcher(gooey.NewDispatcher())
	ed.pasteMarkup(`<d:Foo xmlns:d="` + markup.XNamespace + `" Name="Title"/>`)
	assertNames(t, "the paste refusal", ed.status.Get(), "<d:Foo>", "<x:Foo>")
}

// TestAnEnvelopeInTheXNamespaceGetsTheAlienRefusal pins the branch
// browser.go's `n.Elem != "Gooey"` conjunct sends a reader to.
//
// That comment said the answer for such a file is "the root-count
// refusal below". It is not, and has not been since the alien arm was
// added ahead of the count: with the default xmlns on <Gooey>, splitDecls
// files every child into decls, so there are no kids to count and
// alienDecls returns first. The behaviour is right — it is markup's own
// sentence for the same bytes, which is the standard this file holds its
// refusals to — and only the stated reason was stale. Pinned rather than
// only corrected, because the next person to move an arm ahead of another
// should find out from a test rather than from a comment. Raised in
// review of #522.
func TestAnEnvelopeInTheXNamespaceGetsTheAlienRefusal(t *testing.T) {
	doc := `<Gooey xmlns="` + markup.XNamespace + `">` + "\n" +
		`  <Canvas Name="Root"/>` + "\n</Gooey>\n"
	markupRefuses(t, doc, "unknown language element", "<x:Canvas>")

	root := workspaceFixture(t)
	if err := os.WriteFile(filepath.Join(root, "env.gooey"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	ed, _ := buildPage(t)
	ed.setDispatcher(gooey.NewDispatcher())
	ed.setWorkspace(root)
	ed.openWorkspaceFile("env.gooey")

	got := ed.status.Get()
	assertNames(t, "the refusal for an envelope in the x namespace", got, "<x:Canvas>", "")
	// AND IT IS NOT THE COUNT, which is the half the comment got wrong.
	// The count refusal would report zero root elements for a document
	// that plainly has one child, and send the author to look at the
	// shape of their file rather than at its namespace.
	if strings.Contains(got, "root element") {
		t.Errorf("the refusal reads %q and counts roots; every child of this "+
			"envelope is in the x namespace, so there is nothing to count and "+
			"the namespace is the whole answer", got)
	}
}

// markupRefuses is the "am I agreeing with anything" gate every arm of
// the alien tests opens with: markup's own diagnosis has to still exist
// and still say these words, or the editor is being checked against a
// sentence nobody writes.
func markupRefuses(t *testing.T, doc string, wants ...string) error {
	t.Helper()
	_, err := markup.Build([]byte(doc), &markup.Context{})
	if err == nil {
		t.Fatal("markup now accepts the fixture, so there is nothing for the " +
			"editor to agree with")
	}
	for _, want := range wants {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("markup's own diagnosis no longer contains %q, so this test "+
				"is checking the editor against nothing: %v", want, err)
		}
	}
	return err
}

// assertNames is the pair every refusal arm makes: it names the element
// the file actually holds, and it does not name a prefix the file does
// not contain. absent is "" where there is no minted spelling to avoid.
func assertNames(t *testing.T, what, got, elem, absent string) {
	t.Helper()
	for _, want := range []string{"unknown language element", elem} {
		if !strings.Contains(got, want) {
			t.Errorf("%s reads %q and does not mention %q", what, got, want)
		}
	}
	if absent != "" && strings.Contains(got, absent) {
		t.Errorf("%s reads %q and names %q, which this document does not bind — "+
			"either declBinding's MINTED spelling, which it returns when it "+
			"finds no binding, or a literal \"x\" written in place of reading "+
			"one. Both send the author looking for an element nobody wrote",
			what, got, absent)
	}
}

// TestPastingABareDeclarationSaysWhatItIs is the leg unwrapGooey could
// not reach.
//
// It returns the moment n.Elem != "Gooey", so a declaration copied on
// its OWN — which is what you get selecting one line in a file — was
// never an envelope refusal. It fell through to insertSubtree: planAdd
// finds no spec for "Property", node.markup writes it with no prefix,
// and the rebuild answers "markup: unknown element <Property>" — the
// exact string splitDecls' doc calls out as the one the author must not
// be shown. The document survives, so this is a message defect; the
// branch that made it DETECTABLE is this one, because n.Space now
// survives deepCopy. Raised in review of #522.
func TestPastingABareDeclarationSaysWhatItIs(t *testing.T) {
	for _, tc := range []struct {
		name, src string
		want      []string
		absent    []string
	}{
		{
			name: "prefixed, no envelope",
			src:  `<x:Property xmlns:x="` + markup.XNamespace + `" Name="Title" Type="string"/>`,
			want: []string{"<x:Property>", "declaration"},
		},
		{
			// THE AUTHOR'S OWN PREFIX, and the arm above cannot see the
			// difference because its fixture agrees with the bug: it
			// binds x:, so a hardcoded "x" and a read binding print the
			// same string. The alien arm one case up in bareDeclWhy was
			// moved onto declBinding in round 4 and this one was not, so
			// pasting a p:-bound declaration reported <x:Property> — a
			// prefix the clipboard does not hold, and one that is
			// actively wrong if the open document binds x: elsewhere.
			// Raised in review of #522.
			name:   "prefixed with something other than x, no envelope",
			src:    `<p:Property xmlns:p="` + markup.XNamespace + `" Name="Title" Type="string"/>`,
			want:   []string{"<p:Property>", "declaration"},
			absent: []string{"<x:Property>"},
		},
		{
			name: "unprefixed, no envelope",
			src:  `<Property Name="Title" Type="string"/>`,
			want: []string{"<x:Property>", `xmlns:x="` + markup.XNamespace + `"`},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ed, _ := buildPage(t)
			ed.setDispatcher(gooey.NewDispatcher())
			ed.pasteMarkup(tc.src)

			got := ed.status.Get()
			if !strings.HasPrefix(got, "✗") {
				t.Fatalf("a bare declaration was pasted into the document: %q", got)
			}
			if strings.Contains(got, "unknown element <Property>") {
				t.Fatalf("the paste still reports %q — the element is not unknown, "+
					"it is a declaration in the wrong place, and that message is "+
					"the one this branch exists to stop showing", got)
			}
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Errorf("the refusal reads %q and does not mention %q", got, want)
				}
			}
			for _, bad := range tc.absent {
				if strings.Contains(got, bad) {
					t.Errorf("the refusal reads %q and names %q, a prefix the "+
						"clipboard does not hold — and one that is actively wrong "+
						"if the open document binds it to something else", got, bad)
				}
			}
		})
	}
}

// TestDeclAttrsNeverHandsBackTheEditorsOwnMap pins the sentence
// declAttrs' doc makes, which nothing else can see.
//
// THE CALLERS ONLY READ IT, which is exactly why this exists. The map
// declAttrs is handed is ed.envDecls[i].Attrs — the editor's document
// state — and while it returned that map unchanged on the no-drop path,
// the doc's claim that the result is a copy was false and no test in
// the package could tell: envelopeHead assigns it to a throwaway and
// envelopeNamespaces ranges it. The cost is deferred rather than
// absent, and withDeclBinding's doc names it: a write through the
// shared map makes the next save look as though the file had always
// carried the attribute.
//
// SO THE ASSERTION IS IDENTITY, NOT CONTENT. Content is equal either
// way — that is the whole difficulty — so this mutates the returned map
// and asks whether the input moved. Measured: with the no-drop fast
// path restored this fails, and the whole apps/wysiwyg suite is
// otherwise green, which is what an unpinned invariant looks like.
// Raised in review of #522.
func TestDeclAttrsNeverHandsBackTheEditorsOwnMap(t *testing.T) {
	// NOTHING DEAD IN IT, which is the path that aliased: xmlns:x names
	// the emitted element and is bound to the x namespace, so no
	// attribute is dropped and the copy is the only difference.
	attrs := map[string]string{
		"xmlns:x": markup.XNamespace,
		"Name":    "T",
		"Type":    "string",
	}
	got := declAttrs(attrs, "x")
	if len(got) != len(attrs) {
		t.Fatalf("declAttrs dropped something from a declaration with nothing "+
			"dead in it: got %v, want %v — the arm below measures the COPY, so "+
			"it says nothing if the contents already differ", got, attrs)
	}
	got["Name"] = "mutated"
	if attrs["Name"] != "T" {
		t.Errorf("writing to declAttrs' result changed the caller's map: "+
			"Name is now %q. That map is the editor's own node attrs, so a "+
			"future caller that writes a binding into the result would make "+
			"the next save look as though the file had always carried one",
			attrs["Name"])
	}
}

// TestEnvelopePartsNeverHandsBackTheEditorsOwnMap is the same sentence
// one function over, and envelopeParts was the one place in this
// cluster still making it conditionally.
//
// Its no-change path — the envelope already binds the prefix, so no
// mint, and no second binding of the declaration namespace to strip —
// returned `attrs` exactly as received, and for both callers that is
// ed.envAttrs, the editor's live document state. Inert today because
// envelopeHead and envelopeNamespaces only read the result; that is
// the same "inert for the same reason" the declAttrs round above
// declined to rely on, and the fast path is what made the guarantee
// depend on which document the caller happened to hold. Raised in
// review of #522.
func TestEnvelopePartsNeverHandsBackTheEditorsOwnMap(t *testing.T) {
	// THE NO-CHANGE PATH, which is the one that aliased: the envelope
	// already binds the prefix, so declPrefix reports it bound and no
	// mint runs, and it binds the declaration namespace once, so the
	// strip loop rebuilds nothing.
	attrs := map[string]string{
		"xmlns:x": markup.XNamespace,
		"Title":   "t",
	}
	decls := []*node{{Elem: "Property", Attrs: map[string]string{"Name": "T"}}}
	got, prefix := envelopeParts(attrs, decls)
	if prefix != "x" || len(got) != len(attrs) {
		t.Fatalf("envelopeParts(%v) = %v, %q — want the envelope's own prefix "+
			"and nothing added or dropped; the arm below measures the COPY, so "+
			"it says nothing if this path already rebuilt the map", attrs, got, prefix)
	}
	got["Title"] = "mutated"
	if attrs["Title"] != "t" {
		t.Errorf("writing to envelopeParts' result changed the caller's map: "+
			"Title is now %q. That map is ed.envAttrs, so a future caller "+
			"writing a binding into the result would make the next save look "+
			"as though the file had always carried one", attrs["Title"])
	}
}

// TestAnAnyDeclarationSeedsAHandleItsConsumersRefuse is the scope
// boundary of the preview, stated as a test because the claim in three
// places said "every declaration".
//
// AbsentValue returns exactly what resolve returns for an absent
// optional: for Type="any" that is a *prop.Property[any], and every
// consumer one level down wants the concrete handle. So a defining
// document using the escape hatch OPENS and does not BUILD — which is
// the half "opening it is not enough" was about, one type over. Both
// markup-only controls this tree ships use Type="any", so this is not
// an obscure corner: isHandlerExpr requires it for behaviour crossing a
// control boundary, and propKinds has no row for a slice type.
//
// THIS PINS THE STATE OF PLAY RATHER THAN BLESSING IT. Giving these a
// preview cannot come from AbsentValue — a Declaration does not know
// its consumer — so it is a separate decision; when it is made, this
// test goes red and every page that describes the fourth case goes with
// it: docs/architecture.md, docs/markup-reference.md and seedDeclared's
// own doc, which this commit scoped, plus
// docs/specs/2026-08-10-markup-declared-properties.md, which it did
// not — that page's worked example is card.gooey, the one file in the
// tree that demonstrates this exception rather than the rule, and it is
// #556. A count in prose is a sample taken once, so this names them
// instead of counting them. Raised in review of #522.
func TestAnAnyDeclarationSeedsAHandleItsConsumersRefuse(t *testing.T) {
	root := workspaceFixture(t)
	const doc = `<Gooey xmlns="wonderforge.io/gooey/2026" xmlns:x="` +
		markup.XNamespace + `">` + "\n" +
		`  <x:Property Name="Tint" Type="any"/>` + "\n" +
		`  <Text Style="{{.Tint}}">hi</Text>` + "\n</Gooey>\n"
	if err := os.WriteFile(filepath.Join(root, "any.gooey"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	ed, _ := buildPage(t)
	ed.setDispatcher(gooey.NewDispatcher())
	ed.setWorkspace(root)
	ed.openWorkspaceFile("any.gooey")

	got := ed.status.Get()
	if !strings.Contains(got, "*prop.Property[interface {}]") {
		t.Errorf("the status reads %q. If this now BUILDS, the preview has "+
			"gained an answer for Type=\"any\" — update seedDeclared's doc, "+
			"docs/architecture.md and docs/markup-reference.md, all three of "+
			"which describe the fourth case, and then delete this test", got)
	}
	// OPENED, which is the half that makes the failure a preview gap
	// rather than a refusal: openPath is set, so ctrl+s still works and
	// the author can edit the file. A refusal would have cleared it.
	if ed.openPath.Get() == "" {
		t.Errorf("the file was refused rather than opened; this test is about a " +
			"document that opens and does not build")
	}
}
