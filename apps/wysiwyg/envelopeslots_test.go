package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
)

// TestResourcesSurviveTheRoundTrip is #510's resources row.
//
// Two blocks, because they failed two different ways. The ENVELOPE's
// <Gooey.Resources> was parsed into the <Gooey> node's Slots and dropped
// at the unwrap, so the open blamed the first Style= that used it ("no
// style named panel is registered") and the save deleted the block. An
// ELEMENT's <Canvas.Resources> holding two declarations never opened at
// all: a slot held one child, and nodeOf refused the list.
//
// The Text uses the envelope's style, so a build that has lost the block
// cannot report ✓ — the open status is the assertion, not decoration.
func TestResourcesSurviveTheRoundTrip(t *testing.T) {
	root := workspaceFixture(t)
	const doc = `<Gooey Graphics="halfblock">
  <Gooey.Resources>
    <Style Key="panel" Fg="#7a7a8c"/>
    <Style Key="title" Bold="true"/>
  </Gooey.Resources>
  <Canvas Name="Root">
    <Canvas.Resources>
      <Style Key="local" Dim="true"/>
      <Style Key="other" Underline="true"/>
    </Canvas.Resources>
    <Text Name="T" Style="panel">hi</Text>
    <Text Name="U" Style="local">lo</Text>
  </Canvas>
</Gooey>
`
	path := filepath.Join(root, "res.gooey")
	if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}

	ed, _ := buildPage(t)
	ed.setDispatcher(gooey.NewDispatcher())
	ed.setWorkspace(root)
	ed.openWorkspaceFile("res.gooey")
	if got := ed.status.Get(); !strings.HasPrefix(got, "✓") {
		t.Fatalf("opening a document whose content uses its own resources "+
			"reports %q, want a build", got)
	}
	if err := ed.saveOpenFile(); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`<Gooey.Resources>`, `Key="panel"`, `Key="title"`,
		`<Canvas.Resources>`, `Key="local"`, `Key="other"`,
		`Graphics="halfblock"`,
	} {
		if !strings.Contains(string(first), want) {
			t.Errorf("the saved file lost %s:\n%s", want, first)
		}
	}

	// AND A SECOND OPEN IS STABLE: what the save wrote is something the
	// editor reads back to the same bytes, so the block cannot erode one
	// save at a time.
	ed.openWorkspaceFile("res.gooey")
	if got := ed.status.Get(); !strings.HasPrefix(got, "✓") {
		t.Fatalf("reopening the saved file reports %q", got)
	}
	if err := ed.saveOpenFile(); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Errorf("a second round trip changed the file:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

// TestTheNextFileDoesNotInheritTheEnvelopesResources is the assignment
// half: envSlots moves with the document, so opening a file with no
// resources after one with them must not carry the first file's block
// into the second's save.
func TestTheNextFileDoesNotInheritTheEnvelopesResources(t *testing.T) {
	root := workspaceFixture(t)
	withRes := "<Gooey>\n  <Gooey.Resources>\n    <Style Key=\"panel\" Bold=\"true\"/>\n  </Gooey.Resources>\n" +
		"  <Canvas Name=\"Root\"/>\n</Gooey>\n"
	plain := "<Gooey>\n  <Canvas Name=\"Root\"/>\n</Gooey>\n"
	for name, src := range map[string]string{"res.gooey": withRes, "plain.gooey": plain} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ed, _ := buildPage(t)
	ed.setDispatcher(gooey.NewDispatcher())
	ed.setWorkspace(root)
	ed.openWorkspaceFile("res.gooey")
	ed.openWorkspaceFile("plain.gooey")
	if err := ed.saveOpenFile(); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(root, "plain.gooey"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), "Resources") {
		t.Errorf("the previous file's <Gooey.Resources> was written into this one:\n%s", got)
	}
}
