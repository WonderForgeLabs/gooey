package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/markup"
)

// TestACopyCarriesTheNamespacesItsExpressionsUse is #525. The copy's
// text is what OSC 52 hands every OTHER document, and xmlns:t lived on
// the envelope, so the paste anywhere else was refused with
// "undeclared namespace prefix".
func TestACopyCarriesTheNamespacesItsExpressionsUse(t *testing.T) {
	const uri = "urn:gooey:test:525:t"
	handlerNS(t, uri)
	root := workspaceFixture(t)
	doc := `<Gooey xmlns:t="` + uri + `" xmlns:u="urn:gooey:test:525:u" xmlns:v="urn:gooey:test:525:v">` + "\n" +
		`  <Canvas Name="Root">` + "\n" +
		`    <Button Name="B" Content="go" Click="{{t:Fire}}"/>` + "\n" +
		`    <Button Name="Plain" Content="no"/>` + "\n" +
		`  </Canvas>` + "\n" +
		`</Gooey>` + "\n"
	other := "<Gooey>\n  <Canvas Name=\"Root\"/>\n</Gooey>\n"
	for name, src := range map[string]string{"src.gooey": doc, "dst.gooey": other} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ed, _ := buildPage(t)
	ed.setDispatcher(gooey.NewDispatcher())
	ed.setWorkspace(root)
	ed.openWorkspaceFile("src.gooey")
	if got := ed.status.Get(); !strings.HasPrefix(got, "✓") {
		t.Fatalf("opening the source document reports %q", got)
	}

	ed.sel = findNode(ed, "B")
	ed.copySelected()
	got := ed.clip.markup
	if !strings.Contains(got, `xmlns:t="`+uri+`"`) {
		t.Errorf("the copied text does not declare the prefix its Click uses:\n%s", got)
	}
	// FROM THE EXPRESSIONS, NOT THE DOCUMENT.
	for _, p := range []string{"xmlns:u", "xmlns:v"} {
		if strings.Contains(got, p) {
			t.Errorf("the copy carries %s, which nothing in it uses:\n%s", p, got)
		}
	}
	// The copy is decorated, not the document.
	if _, ok := findNode(ed, "B").Attrs["xmlns:t"]; ok {
		t.Error("copying wrote xmlns:t onto the document's own <Button>")
	}

	ed.sel = findNode(ed, "Plain")
	ed.copySelected()
	if strings.Contains(ed.clip.markup, "xmlns") {
		t.Errorf("a copy that uses no prefix carries a declaration:\n%s", ed.clip.markup)
	}

	// AND THE POINT OF IT: the text pastes into a document that has
	// never heard of t.
	ed.sel = findNode(ed, "B")
	ed.copySelected()
	text := ed.clip.markup
	ed.openWorkspaceFile("dst.gooey")
	ed.sel = ed.doc()
	ed.pasteMarkup(text)
	if ed.docRoot == nil || strings.HasPrefix(ed.status.Get(), "✗") {
		t.Fatalf("pasting the copied text into another document reports %q", ed.status.Get())
	}
	if !strings.Contains(ed.source.Get(), uri) {
		t.Errorf("the pasted document does not declare the namespace:\n%s", ed.source.Get())
	}
}

// TestTheCopiedPrefixIsTheOneMarkupResolves keeps handlerPrefixRe in step
// with markup's own handlerExprRe: for each spelling, the prefix the
// designer would carry is the one markup's loader asks for, and a value
// that is not a handler expression carries nothing.
func TestTheCopiedPrefixIsTheOneMarkupResolves(t *testing.T) {
	ed, _ := buildPage(t)
	ed.setDispatcher(gooey.NewDispatcher())
	for _, v := range []string{"{{t:Fire}}", "{{ t : Fire }}", "{{t-x:Fire .Name}}", "{{_p:Go `lit` | into .Name}}"} {
		src := `<Gooey><Canvas><Button Content="go" Click="` + v + `"/></Canvas></Gooey>`
		_, err := markup.Build([]byte(src), ed.docCtx)
		p, ok := handlerPrefix(v)
		if !ok {
			t.Errorf("handlerPrefix(%q) found no prefix; markup says %v", v, err)
			continue
		}
		if err == nil || !strings.Contains(err.Error(), `undeclared namespace prefix "`+p+`"`) {
			t.Errorf("for %q the designer carries %q, and markup's refusal is %v", v, p, err)
		}
	}
	for _, v := range []string{"{{.Name}}", "go", "{{not .Checked}}", ""} {
		if p, ok := handlerPrefix(v); ok {
			t.Errorf("handlerPrefix(%q) = %q; that is not a handler expression", v, p)
		}
	}
}

// TestACutCarriesThemToo: cut is a copy followed by a delete, and it
// built its clipboard text on its own.
func TestACutCarriesThemToo(t *testing.T) {
	const uri = "urn:gooey:test:525:cut"
	handlerNS(t, uri)
	root := workspaceFixture(t)
	doc := `<Gooey xmlns:t="` + uri + `">` + "\n" +
		`  <Canvas Name="Root">` + "\n" +
		`    <Button Name="B" Content="go" Click="{{t:Fire}}"/>` + "\n" +
		`  </Canvas>` + "\n" +
		`</Gooey>` + "\n"
	if err := os.WriteFile(filepath.Join(root, "src.gooey"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	ed, _ := buildPage(t)
	ed.setDispatcher(gooey.NewDispatcher())
	ed.setWorkspace(root)
	ed.openWorkspaceFile("src.gooey")
	ed.sel = findNode(ed, "B")
	ed.cutSelected()
	if !strings.Contains(ed.clip.markup, `xmlns:t="`+uri+`"`) {
		t.Errorf("the cut text does not declare the prefix its Click uses:\n%s", ed.clip.markup)
	}
}
