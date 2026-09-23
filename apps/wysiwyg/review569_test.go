package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
)

// openFixture writes doc as f.gooey in a fresh workspace and opens it.
func openFixture(t *testing.T, doc string) (*editor, string) {
	t.Helper()
	root := workspaceFixture(t)
	path := filepath.Join(root, "f.gooey")
	if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	ed, _ := buildPage(t)
	ed.setDispatcher(gooey.NewDispatcher())
	ed.setWorkspace(root)
	ed.openWorkspaceFile("f.gooey")
	if got := ed.status.Get(); !strings.HasPrefix(got, "✓") {
		t.Fatalf("opening the fixture reports %q", got)
	}
	return ed, path
}

// TestARetypedOwnerRenamesItsPropertyElements. A slot's tag is derived
// from its owner: node.Slots holds the property element, and its stored
// Elem is frozen at parse time, so writing that stored name put
// <Canvas.Resources> inside the <VStack> a retype had just produced —
// refused by the build, written to disk by the save, refused again on
// reopen. Raised in review of #569.
func TestARetypedOwnerRenamesItsPropertyElements(t *testing.T) {
	ed, path := openFixture(t, `<Gooey>
  <Canvas Name="Root">
    <Canvas.Resources>
      <Style Key="local" Bold="true"/>
    </Canvas.Resources>
    <Text Name="T" Style="local">hi</Text>
  </Canvas>
</Gooey>
`)
	ed.retype("VStack")
	if got := ed.status.Get(); !strings.HasPrefix(got, "✓") {
		t.Fatalf("retyping the root to <VStack> reports %q", got)
	}
	if err := ed.saveOpenFile(); err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(saved), "<VStack.Resources>") ||
		strings.Contains(string(saved), "Canvas.Resources") {
		t.Errorf("the saved file does not name the slot after its new owner:\n%s", saved)
	}
	ed.openWorkspaceFile("f.gooey")
	if got := ed.status.Get(); !strings.HasPrefix(got, "✓") {
		t.Errorf("reopening the retyped file reports %q", got)
	}
}

// TestPastingAWholeDocumentKeepsItsComments is the paste half of #529:
// unwrapGooey unwraps an envelope just as the open does, and the open
// was the only one of the two carrying the envelope's comments down.
// Raised in review of #569.
func TestPastingAWholeDocumentKeepsItsComments(t *testing.T) {
	ed, _ := openFixture(t, "<Gooey>\n  <Canvas Name=\"Root\"/>\n</Gooey>\n")
	ed.sel = ed.doc()
	ed.pasteMarkup("<!-- above -->\n<Gooey>\n  <Button Name=\"P\" Content=\"go\"/>\n  <!-- after -->\n</Gooey>\n<!-- epilog -->\n")
	if got := ed.status.Get(); strings.HasPrefix(got, "✗") {
		t.Fatalf("the paste was refused: %q", got)
	}
	src := ed.source.Get()
	for _, c := range []string{"above", "after", "epilog"} {
		if !strings.Contains(src, "<!-- "+c+" -->") {
			t.Errorf("pasting a whole document dropped <!-- %s -->:\n%s", c, src)
		}
	}
}

// TestADuplicateDoesNotCopyTheOriginalsComment. The comment above an
// element is the author's note about THAT element; the copy gets a new
// Name precisely so the two can be told apart. A comment inside the
// copied subtree still sits above what it describes, so it comes along.
// Raised in review of #569.
func TestADuplicateDoesNotCopyTheOriginalsComment(t *testing.T) {
	ed, _ := openFixture(t, `<Gooey>
  <Canvas Name="Root">
    <!-- the one that saves -->
    <VStack Name="V">
      <!-- inner -->
      <Button Name="B" Content="go"/>
    </VStack>
  </Canvas>
</Gooey>
`)
	ed.sel = findNode(ed, "V")
	if !ed.duplicateSelected() {
		t.Fatalf("duplicate refused: %q", ed.status.Get())
	}
	src := ed.source.Get()
	if n := strings.Count(src, "<!-- the one that saves -->"); n != 1 {
		t.Errorf("the original's leading comment appears %d times after a duplicate:\n%s", n, src)
	}
	if n := strings.Count(src, "<!-- inner -->"); n != 2 {
		t.Errorf("a comment inside the copied subtree appears %d times, want 2:\n%s", n, src)
	}
}
