package components

import (
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/render"
)

// The dropdown's geometry, exposed (#400, gap 2).
//
// The bar already keeps everything an app-side decorator needs — which
// menu is open and where its dropdown landed — and kept all of it
// private. `popup()`, `curP`, `popupRect` and `titleSpan` are every one
// unexported, so the only seam was that ChildComponents() happens to
// return the popup surface as its single element, which is an
// implementation ordering rather than an API.
//
// #400's reporter recovered the open index by walking the title widths
// and matching the dropdown's left edge, duplicating titleSpan AND
// splitMnemonic's marker stripping in application code — arithmetic
// upstream is free to change without notice. Two accessors over state
// that already exists retire that.
//
// THE TESTS BELOW ASSERT AGAINST THE PAINTED CELLS, not against the
// bar's other getters. An accessor checked against the field behind it
// agrees with itself no matter what either one means; the question an
// app is actually asking is "where did the dropdown go", and only the
// frame can answer it.

// geomBar is a bar whose titles have DIFFERENT widths, so an off-by-one
// in the index cannot be hidden by two menus landing in the same place,
// and whose second title carries a mnemonic marker — the marker is
// syntax, not cells, and the reporter's hand-rolled version had to strip
// it to get the arithmetic right.
func geomBar() *MenuBar {
	return &MenuBar{Menus: []Menu{
		{Title: "A", Items: []MenuItem{{Text: "one", Action: gooey.Command(func() {})}}},
		{Title: "Lo_nger", Items: []MenuItem{{Text: "two", Action: gooey.Command(func() {})}}},
	}}
}

// paintedDropdown finds the dropdown's border box on the cell plane: the
// row and column range of the box-drawing frame below the bar row. It is
// deliberately independent of popupRect — it reads back what was
// actually drawn.
//
// WALKED IN COLUMNS, one rune at a time. The obvious spelling —
// strings.IndexAny / LastIndexAny over render.RowText — returns BYTE
// offsets, and every box-drawing glyph is three bytes in UTF-8. It gets
// the left edge right by luck (the padding before it is ASCII) and the
// width wrong by twice the border count, which is exactly how this
// helper first reported a 9-wide box for a 7-wide dropdown and blamed
// the accessor. Same trap as CLAUDE.md's rune-vs-column rule, one level
// up: the reader has to count cells too.
func paintedDropdown(t *testing.T, f *gooey.Frame, w, h int) gooey.Rect {
	t.Helper()
	const border = "┌┐└┘╭╮╰╯├┤│─"
	got := gooey.Rect{X: -1, Y: -1}
	for y := 1; y < h; y++ {
		first, last := -1, -1
		col := 0
		for _, r := range render.RowText(f.Cells, y) {
			if strings.ContainsRune(border, r) {
				if first < 0 {
					first = col
				}
				last = col
			}
			col += render.StringWidth(string(r))
		}
		if first < 0 {
			continue
		}
		if got.Y < 0 {
			got.X, got.Y = first, y
		}
		if right := last + 1; right-got.X > got.W {
			got.W = right - got.X
		}
		got.H = y - got.Y + 1
	}
	return got
}

// TestTheOpenIndexIsReadableFromOutside — -1 closed, the opened index
// while open, and it TRACKS rather than latching at the first open.
func TestTheOpenIndexIsReadableFromOutside(t *testing.T) {
	bar := geomBar()
	c := gooey.NewComposer(bar, 40, 12)
	t.Cleanup(c.Close)
	c.Frame()

	if got := bar.OpenIndex(); got != -1 {
		t.Errorf("a closed bar reports OpenIndex %d, want -1 — an app cannot tell closed from menu 0", got)
	}
	bar.Open(1, nil)
	c.Frame()
	if got := bar.OpenIndex(); got != 1 {
		t.Errorf("OpenIndex = %d after opening menu 1", got)
	}
	bar.Open(0, nil)
	c.Frame()
	if got := bar.OpenIndex(); got != 0 {
		t.Errorf("OpenIndex = %d after switching to menu 0: it latched at the first open", got)
	}
	bar.Dismiss()
	c.Frame()
	if got := bar.OpenIndex(); got != -1 {
		t.Errorf("OpenIndex = %d after closing, want -1", got)
	}
}

// TestTheDropdownBoundsAreWhereItPainted is the assertion that matters,
// and it is against the frame rather than against popupRect. An
// accessor returning the same private arithmetic the painter uses is
// correct by construction and proves nothing about what an app placing
// pixels into that rect would hit.
func TestTheDropdownBoundsAreWhereItPainted(t *testing.T) {
	bar := geomBar()
	c := gooey.NewComposer(bar, 40, 12)
	t.Cleanup(c.Close)
	c.Frame()

	if got := bar.DropdownBounds(); got != (gooey.Rect{}) {
		t.Errorf("a closed bar reports bounds %v, want the zero Rect", got)
	}

	// The SECOND menu, so a bar reporting menu 0's rect for every open
	// menu fails here. Its title is the wider one and carries a
	// mnemonic marker, which is exactly the arithmetic an app doing
	// this by hand has to reproduce.
	bar.Open(1, nil)
	c.Frame()
	f, _ := c.Frame()

	want := paintedDropdown(t, f, 40, 12)
	if want.Y < 0 {
		t.Fatal("no dropdown was painted: the fixture never opened and nothing below was tested")
	}
	if got := bar.DropdownBounds(); got != want {
		t.Errorf("DropdownBounds reports %v; the dropdown painted at %v.\n%s",
			got, want, frameText(f, 40, 12))
	}
}

// TestTheReportedBoundsMoveWithTheOpenMenu. Two menus of different
// title widths, so the rects differ in X — a bar that returned a
// constant, or one that measured from menu 0 always, passes every
// assertion above and fails this one.
func TestTheReportedBoundsMoveWithTheOpenMenu(t *testing.T) {
	bar := geomBar()
	c := gooey.NewComposer(bar, 40, 12)
	t.Cleanup(c.Close)
	c.Frame()

	bar.Open(0, nil)
	c.Frame()
	c.Frame()
	first := bar.DropdownBounds()

	bar.Open(1, nil)
	c.Frame()
	c.Frame()
	second := bar.DropdownBounds()

	if first.X == second.X {
		t.Errorf("both menus report the dropdown at column %d: the bounds do not follow the open title", first.X)
	}
	if first == (gooey.Rect{}) || second == (gooey.Rect{}) {
		t.Errorf("an open menu reported the zero Rect: %v then %v", first, second)
	}
}

func frameText(f *gooey.Frame, w, h int) string {
	var b strings.Builder
	for y := 0; y < h; y++ {
		b.WriteString(render.RowText(f.Cells, y))
		b.WriteByte('\n')
	}
	return b.String()
}
