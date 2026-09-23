package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey/render"
)

// TestANarrowedExplorerKeepsTheFileName is #528's acceptance: resize the
// explorer pane narrower than its declared Size and an ASCII path must
// keep its TAIL. The row was shortened to a static 30 before layout, so
// a narrower pane clipped it from the right and the file name went.
func TestANarrowedExplorerKeepsTheFileName(t *testing.T) {
	ed, c := dockFixture(t)
	root := t.TempDir()
	const rel = "apps/somewhere/deeply/nested/directory/final-file.gooey"
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte("<Gooey><Canvas/></Gooey>\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ed.setWorkspace(root)
	ex := pane(t, ed, "explorer")
	declared := ex.size.Get()
	// EVERY PANE IN THE SLOT: a slot is as wide as its widest pane
	// (slotExtent), so narrowing the explorer alone leaves the column as
	// it was when a slot-mate is wider.
	const narrow = 20
	for _, p := range ed.dock.slotPanes(dockSlot(ex.slot.Get())) {
		ed.dock.SetActive(p)
		ed.dock.Resize(narrow - p.size.Get())
	}
	if ex.size.Get() >= declared {
		t.Fatalf("the explorer is %d wide after narrowing from %d; nothing to measure",
			ex.size.Get(), declared)
	}
	f, _ := c.Frame()
	settle(t, c)
	f, _ = c.Frame()
	screen := render.BufferText(f.Cells)
	if !strings.Contains(screen, "final-file.gooey") {
		t.Errorf("with the explorer narrowed to %d, the row for %q lost its file name:\n%s",
			ex.size.Get(), rel, screen)
	}
	if strings.Contains(screen, rel) {
		t.Errorf("the whole %d-column path fits a %d-wide pane; the test measures nothing",
			len(rel), ex.size.Get())
	}
}
