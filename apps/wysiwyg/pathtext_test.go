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

	// PAINTED WIDE FIRST, which is the author's gesture: the row is on
	// screen at the declared width and then the splitter moves. The
	// refresh after the drag rests entirely on the Composer's bounds
	// sweep — pathText.Render reads Bounds(), a plain field, not a
	// property — so a row painted only once, already narrow, would not
	// exercise it. Raised in review of #569.
	c.Frame()
	settle(t, c)
	f, _ := c.Frame()
	wide := explorerRow(f.Cells, "final-file.gooey")
	if wide == "" {
		t.Fatalf("at the declared width no row shows the file name:\n%s", render.BufferText(f.Cells))
	}

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
	f, painted := c.Frame()
	settle(t, c)
	// THE DAMAGE PIN, which is the only thing that proves a repaint: the
	// narrowing frame repainted something, and the row it left reads
	// differently from the wide one.
	if painted == 0 {
		t.Error("narrowing the explorer repainted nothing")
	}
	f, _ = c.Frame()
	screen := render.BufferText(f.Cells)
	row := explorerRow(f.Cells, "final-file.gooey")
	if row == "" {
		t.Errorf("with the explorer narrowed to %d, the row for %q lost its file name:\n%s",
			ex.size.Get(), rel, screen)
	}
	if row == wide {
		t.Errorf("the row reads %q both wide and narrow: it was not repainted at the "+
			"new width", row)
	}
	if strings.Contains(screen, rel) {
		t.Errorf("the whole %d-column path fits a %d-wide pane; the test measures nothing",
			len(rel), ex.size.Get())
	}
}

// explorerRow is the painted PATH containing needle — the one
// whitespace-free token on the screen that holds it, which is exactly
// what <PathText> wrote, since the fixture's path has no spaces. The
// whole screen row would not do: it carries every other pane's cells,
// and those move with the splitter too.
func explorerRow(b *render.Buffer, needle string) string {
	for y := 0; y < b.H; y++ {
		for _, tok := range strings.Fields(render.RowText(b, y)) {
			if strings.Contains(tok, needle) {
				return tok
			}
		}
	}
	return ""
}
