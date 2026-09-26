package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
)

// TestASaveKeepsBlankLinesAndLineBreaks: the first save of a
// hand-written file dropped every blank line the author left between
// sections, and wrote a multi-line <Text> as one line of &#xA;. Both
// read back as the same document, so nothing failed — the file simply
// changed under the author, and every line below the first blank one
// moved. Measured over the repo's own .gooey files, an unedited save
// removed 669 lines before this; 430 after.
//
// The fixture holds each shape once: a blank line between siblings, a
// blank line between a comment and the element it describes, a run of
// several blank lines (kept as one), and a body with a line break.
func TestASaveKeepsBlankLinesAndLineBreaks(t *testing.T) {
	const src = `<Gooey>
  <VStack Name="Root">
    <Text Name="A">one</Text>

    <!-- the second section -->

    <Text Name="B">two</Text>



    <Text Name="C">first line
second line</Text>
  </VStack>
</Gooey>
`
	// The same file with the run of three blank lines kept as one.
	const want = `<Gooey>
  <VStack Name="Root">
    <Text Name="A">one</Text>

    <!-- the second section -->

    <Text Name="B">two</Text>

    <Text Name="C">first line
second line</Text>
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
	if got := ed.doc().Kids[2].Body; got != "first line\nsecond line" {
		t.Fatalf("the multi-line body read as %q", got)
	}
	if err := ed.saveOpenFile(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != want {
		t.Errorf("the save changed the file:\n--- wrote\n%s--- want\n%s", b, want)
	}
}
