package main

// Damage pins for the source picker. The picker is an overlay, and
// overlays live or die by the damage discipline: opening paints the
// popup (and the picker whose bounds went from nothing to the page),
// navigating repaints the popup alone, dismissing repaints exactly what
// the popup covered. If these counts climb, the picker has started
// taxing every frame it is visible for.

import (
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

func pickerSources() []source {
	return []source{
		{Name: "main", Branch: "main", Root: "/repo", Launch: true, Head: "tip of main"},
		{Name: "feat/wt", Branch: "feat/wt", Root: "/wt", Dirty: true, Head: "work in progress"},
		{Name: "old-branch", Branch: "old-branch", Head: "an older take"},
	}
}

// pickerPage is the standard test page: the demo list (the focus stop
// the browser really has) and the picker last, exactly as browser.gooey
// declares them — which no longer decides z-order (the popup surface
// lifts), only that this page matches the shipped one.
func pickerPage(chosen *source) (*sourcePicker, *demoList, gooey.Component) {
	demos := prop.NewComputed(func() []demo {
		return []demo{{name: "a", dir: "cmd/a"}, {name: "b", dir: "cmd/b"}}
	})
	list := &demoList{demos: demos, sel: prop.NewSource(0)}
	picker := newSourcePicker(func(s source) {
		if chosen != nil {
			*chosen = s
		}
	})
	grid := &components.Grid{Children: []gooey.Component{list, picker}}
	return picker, list, grid
}

// bufRow is the row as a terminal would read it. It goes through
// render.RowText so the continuation marker behind a wide glyph is left
// out — built cell by cell, a row holding one read back as "世�" and
// no assertion could name what it saw.
func bufRow(c *gooey.Composer, y int) string { return render.RowText(c.Cells(), y) }

func TestPickerOpenPaintsTheOverlay(t *testing.T) {
	picker, list, page := pickerPage(nil)
	c := gooey.NewComposer(page, 80, 24)
	c.Frame()
	if got := c.Focus().Focused(); got != gooey.Component(list) {
		t.Fatalf("initial focus = %T, want the demo list (the picker is collapsed)", got)
	}

	picker.Open(pickerSources(), "/repo")
	_, painted := c.Frame()
	// Two components: the popup leaf, and the picker container whose
	// bounds went from collapsed-zero to the page (it paints no cells,
	// but the bounds sweep dirties its node).
	if painted != 2 {
		t.Fatalf("opening painted %d components, want 2 (picker + popup)", painted)
	}
	if got := c.Focus().Focused(); got != gooey.Component(picker) {
		t.Fatalf("focus after open = %T, want the picker (keys are modal)", got)
	}
	found := false
	for y := 0; y < 24; y++ {
		if strings.Contains(bufRow(c, y), "sources") {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("the popup box did not paint")
	}
	if _, painted := c.Frame(); painted != 0 {
		t.Fatalf("settled frame painted %d, want 0", painted)
	}
}

func TestPickerNavigationRepaintsPopupOnly(t *testing.T) {
	picker, _, page := pickerPage(nil)
	c := gooey.NewComposer(page, 80, 24)
	c.Frame()
	picker.Open(pickerSources(), "/repo")
	c.Frame()

	if !c.HandleKey(input.Rune('j')) {
		t.Fatal("j was not consumed by the open picker")
	}
	_, painted := c.Frame()
	if painted != 1 {
		t.Fatalf("moving the selection painted %d components, want 1 (the popup)", painted)
	}
	if got := picker.selP.Get(); got != 1 {
		t.Fatalf("selection = %d, want 1", got)
	}
}

func TestPickerIsModalWhileOpen(t *testing.T) {
	picker, _, page := pickerPage(nil)
	c := gooey.NewComposer(page, 80, 24)
	c.Frame()
	picker.Open(pickerSources(), "/repo")
	c.Frame()

	// A key the picker has no use for is swallowed, not bubbled — the
	// page's own `q quits` must not fire under the popup.
	if !c.HandleKey(input.Rune('x')) {
		t.Fatal("an unhandled key escaped the open picker")
	}
	if _, painted := c.Frame(); painted != 0 {
		t.Fatalf("a swallowed key painted %d components", painted)
	}
}

func TestPickerDismissRestoresWhatItCovered(t *testing.T) {
	picker, list, page := pickerPage(nil)
	c := gooey.NewComposer(page, 80, 24)
	c.Frame()
	picker.Open(pickerSources(), "/repo")
	c.Frame()

	if !c.HandleKey(input.Named(input.KeyEsc)) {
		t.Fatal("esc was not consumed")
	}
	_, painted := c.Frame()
	// Four nodes: the picker and popup, whose bounds collapsed (their
	// evaluation counts even though a vanished node paints no cells —
	// erasure is the sweep's job), and the grid and list the restore
	// pass repaints under the vacated rectangle.
	if painted != 4 {
		t.Fatalf("dismissing painted %d components, want 4 (picker + popup gone, grid + list restored)", painted)
	}
	if picker.IsOpen() {
		t.Fatal("esc did not close the picker")
	}
	if got := c.Focus().Focused(); got != gooey.Component(list) {
		t.Fatalf("focus after dismiss = %T, want the demo list back", got)
	}
	found := false
	for y := 0; y < 24; y++ {
		if strings.Contains(bufRow(c, y), "sources") {
			found = true
			break
		}
	}
	if found {
		t.Fatal("popup cells survived the dismiss")
	}
}

func TestPickerEnterChoosesTheSelection(t *testing.T) {
	var chosen source
	picker, _, page := pickerPage(&chosen)
	c := gooey.NewComposer(page, 80, 24)
	c.Frame()
	picker.Open(pickerSources(), "/repo")
	c.Frame()

	c.HandleKey(input.Rune('j'))
	c.HandleKey(input.Rune('j'))
	c.HandleKey(input.Named(input.KeyEnter))
	if chosen.Name != "old-branch" {
		t.Fatalf("chose %q, want old-branch", chosen.Name)
	}
	if picker.IsOpen() {
		t.Fatal("choosing did not dismiss the picker")
	}
}

func TestPickerClickOutsideDismissesWithoutActivating(t *testing.T) {
	var chosen source
	chosen.Name = "-untouched-"
	picker, _, page := pickerPage(&chosen)
	c := gooey.NewComposer(page, 80, 24)
	c.Frame()
	picker.Open(pickerSources(), "/repo")
	c.Frame()

	// The picker holds the capture, so a click far from the popup routes
	// to it — and dismisses rather than reaching the list underneath.
	c.HandleMouse(input.MouseEvent{Kind: input.MouseClick, X: 1, Y: 23})
	if picker.IsOpen() {
		t.Fatal("a click outside the popup did not dismiss")
	}
	if chosen.Name != "-untouched-" {
		t.Fatalf("an outside click chose %q", chosen.Name)
	}
}

func TestPickerClickOnRowChooses(t *testing.T) {
	var chosen source
	picker, _, page := pickerPage(&chosen)
	c := gooey.NewComposer(page, 80, 24)
	c.Frame()
	picker.Open(pickerSources(), "/repo")
	c.Frame()

	b := picker.pop.SurfaceBounds()
	// Rows inside the box: header, main, feat/wt, header, old-branch.
	c.HandleMouse(input.MouseEvent{Kind: input.MouseClick, X: b.X + 2, Y: b.Y + 3})
	if chosen.Name != "feat/wt" {
		t.Fatalf("clicked row chose %q, want feat/wt", chosen.Name)
	}
}

func TestSourceRowsGroupAndMark(t *testing.T) {
	rows := sourceRows(pickerSources())
	want := []struct {
		header string
		src    int
	}{
		{"worktrees", -1}, {"", 0}, {"", 1}, {"branches", -1}, {"", 2},
	}
	if len(rows) != len(want) {
		t.Fatalf("got %d rows, want %d: %+v", len(rows), len(want), rows)
	}
	for i, w := range want {
		if w.src == -1 && rows[i].header != w.header {
			t.Fatalf("row %d header = %q, want %q", i, rows[i].header, w.header)
		}
		if w.src >= 0 && rows[i].src != w.src {
			t.Fatalf("row %d src = %d, want %d", i, rows[i].src, w.src)
		}
	}
	// The active source is marked (a real worktree's id is its root), a
	// dirty worktree is starred.
	if got := rows[1].text("/repo"); !strings.HasPrefix(got, "● main") {
		t.Fatalf("active row = %q, want the ● marker", got)
	}
	if got := rows[2].text("/repo"); !strings.Contains(got, "feat/wt *") {
		t.Fatalf("dirty row = %q, want the * marker", got)
	}
	if got := rows[4].text("/repo"); !strings.Contains(got, "old-branch — an older take") {
		t.Fatalf("branch row = %q, want name and subject", got)
	}
}

// A worktree row paints in two pieces — the name at full strength, then
// the dimmed subject after it — and the second piece starts where the
// first ended. Those were two different strings: clip(name, b.W-3) was
// written and the UNCLIPPED name's rune count was stepped over, so the
// separator landed inside the name it was meant to follow.
//
// The name here is 8 columns and 4 runes, so a rune advance puts " — "
// four columns early, on top of the last two glyphs.
func TestWorktreeSubjectStartsWhereTheNameEnds(t *testing.T) {
	const name = "世界世界"
	picker, _, page := pickerPage(nil)
	c := gooey.NewComposer(page, 80, 24)
	c.Frame()
	picker.Open([]source{
		{Name: "main", Branch: "main", Root: "/repo", Launch: true, Head: "tip of main"},
		{Name: name, Branch: "wide", Root: "/wt", Head: "an older take"},
	}, "/repo")
	c.Frame()

	want := name + " — an older take"
	for y := 0; y < 24; y++ {
		if strings.Contains(bufRow(c, y), want) {
			return
		}
	}
	// Report the rows that hold any of the name, which is where the
	// truncated version shows up.
	var saw []string
	for y := 0; y < 24; y++ {
		if r := bufRow(c, y); strings.Contains(r, "世") {
			saw = append(saw, strings.TrimRight(r, " "))
		}
	}
	t.Errorf("no row reads %q — the subject must begin one column past the "+
		"end of the name. Rows holding the name: %q", want, saw)
}
