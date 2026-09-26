package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
)

// TestASaveKeepsTheAuthorsAttributeOrder: node.Attrs is a map and
// node.markup wrote it sorted, so the first save of a hand-written file
// rewrote nearly every line of it. Measured in the real binary on a
// five-element form: every element line changed, and nothing in the
// document had.
//
// The fixture is deliberately in NO sorted order on any line, so a
// writer that sorts cannot pass it by accident, and it is written in
// the editor's own indentation so the comparison can be the whole file.
func TestASaveKeepsTheAuthorsAttributeOrder(t *testing.T) {
	const doc = `<Gooey>
  <Grid Name="Root" Rows="Auto,Auto,*" Cols="12,*">
    <Text Grid.Row="0" Grid.Col="0">Name</Text>
    <Button Name="Ok" Grid.Row="2" Grid.Col="1" Content="OK"/>
  </Grid>
</Gooey>
`
	root := t.TempDir()
	path := filepath.Join(root, "form.gooey")
	if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	ed, _ := buildPage(t)
	ed.setDispatcher(gooey.NewDispatcher())
	ed.setWorkspace(root)
	ed.openWorkspaceFile("form.gooey")
	if !strings.HasPrefix(ed.status.Get(), "✓") {
		t.Fatalf("the fixture does not build: %q", ed.status.Get())
	}
	save := func() string {
		t.Helper()
		if err := ed.saveOpenFile(); err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	if got := save(); got != doc {
		t.Errorf("an unedited save changed the file:\n--- wrote\n%s--- want\n%s", got, doc)
	}

	// An edit adds a name at the END, and leaves the author's order in
	// front of it alone.
	ok := ed.doc().Kids[1]
	ok.Attrs["Width"] = "6"
	ed.rebuild()
	const wantAdded = `<Button Name="Ok" Grid.Row="2" Grid.Col="1" Content="OK" Width="6"/>`
	if got := save(); !strings.Contains(got, wantAdded) {
		t.Errorf("an added attribute moved the author's:\n%s\nwant a line %s", got, wantAdded)
	}

	// A name deleted and set again comes back WHERE IT WAS, which is the
	// property that makes an undo-shaped round trip invisible in a diff.
	delete(ok.Attrs, "Name")
	ed.rebuild()
	ok.Attrs["Name"] = "Ok"
	ed.rebuild()
	if got := save(); !strings.Contains(got, wantAdded) {
		t.Errorf("a deleted and restored Name did not return to its place:\n%s\nwant a line %s",
			got, wantAdded)
	}

	// And an undo restores a CLONE of an earlier state: the copy has to
	// carry the order too, or the first ctrl+z re-sorts the file.
	ed.undo()
	if got := save(); !strings.Contains(got, `<Button Name="Ok" Grid.Row="2" Grid.Col="1" Content="OK"`) {
		t.Errorf("after an undo the author's order is gone:\n%s", got)
	}
}
