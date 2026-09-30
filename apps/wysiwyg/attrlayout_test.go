package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
)

// TestASaveKeepsTheAuthorsAttributeLayout: the first save of a
// hand-written file joined every wrapped tag onto one line and dropped
// the padding that lined up a column of KeyBindings. Both read back as
// the same document, so nothing failed — the file changed under the
// author anyway. Measured over the repo's own .gooey files it was the
// largest class of what an unedited save still rewrote.
//
// The fixture holds each shape once: attributes wrapped and aligned
// under the first, attributes wrapped at a fixed indent, and padding on
// one line.
func TestASaveKeepsTheAuthorsAttributeLayout(t *testing.T) {
	const src = `<Gooey>
  <VStack Name="Root">
    <Button Name="A" Width="10"
            Height="3" Content="t"/>
    <Button Name="B"
      Content="go"
      Height="1"/>
    <KeyBinding Gesture="q"    Command="{{.Quit}}"/>
  </VStack>
</Gooey>
`
	root := t.TempDir()
	path := filepath.Join(root, "d.gooey")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	ed, _ := buildPage(t)
	ed.setDispatcher(gooey.NewDispatcher())
	ed.setWorkspace(root)
	ed.openWorkspaceFile("d.gooey")
	if !strings.HasPrefix(ed.status.Get(), "✓") {
		t.Fatalf("the fixture does not build: %q", ed.status.Get())
	}
	if err := ed.saveOpenFile(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != src {
		t.Errorf("the save changed the file:\n--- wrote\n%s--- want\n%s", b, src)
	}
}

// TestAWrappedTagKeepsItsShapeAtANewDepth: the wrap's indent is kept
// RELATIVE to the element, so a subtree moved one level deeper keeps
// its continuation lines under its first attribute rather than at the
// column they had in the old position.
func TestAWrappedTagKeepsItsShapeAtANewDepth(t *testing.T) {
	n, err := nodeOf("<Border Name=\"A\" Width=\"10\"\n        Height=\"3\"/>")
	if err != nil {
		t.Fatal(err)
	}
	const want = "    <Border Name=\"A\" Width=\"10\"\n            Height=\"3\"/>\n"
	if got := n.markup("    "); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// TestASaveKeepsTheEnvelopesAttributeLayout: the <Gooey> envelope is
// not a node, so the two fixes above passed it by — its attributes
// still came back sorted and on one line, which rewrote the first
// line of nearly every file in the repo that set Graphics.
func TestASaveKeepsTheEnvelopesAttributeLayout(t *testing.T) {
	const src = `<Gooey xmlns="wonderforge.io/gooey/2026"
       Graphics="halfblock">
  <VStack Name="Root">
    <Text Name="A">one</Text>
  </VStack>
</Gooey>
`
	root := t.TempDir()
	path := filepath.Join(root, "d.gooey")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	ed, _ := buildPage(t)
	ed.setDispatcher(gooey.NewDispatcher())
	ed.setWorkspace(root)
	ed.openWorkspaceFile("d.gooey")
	if !strings.HasPrefix(ed.status.Get(), "✓") {
		t.Fatalf("the fixture does not build: %q", ed.status.Get())
	}
	if err := ed.saveOpenFile(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != src {
		t.Errorf("the save changed the file:\n--- wrote\n%s--- want\n%s", b, src)
	}
}
