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
func paintedDropdown(t *testing.T, f *gooey.Frame, h int) gooey.Rect {
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

	want := paintedDropdown(t, f, 12)
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

// TestTheAccessorsSurviveAMenuListReplacedWhileOpen is finding 1 of the
// review of #400, and it was a PANIC rather than a wrong answer.
//
// Menus is an exported field, so replacing it while a dropdown is open
// is legal, and every other path in menu.go tolerates it — Arrange
// guards, drawDropdown guards, and the two new accessors guarded with
// neither. popupRect indexes m.Menus[m.curIdx()] and curIdx returns 0
// for an empty slice, so a public accessor crashed the process on a
// state the component itself handles.
func TestTheAccessorsSurviveAMenuListReplacedWhileOpen(t *testing.T) {
	bar := geomBar()
	c := gooey.NewComposer(bar, 40, 12)
	t.Cleanup(c.Close)
	c.Frame()
	bar.Open(1, nil)
	c.Frame()

	bar.Menus = nil
	// No recover(): a panic here fails the test by crashing it, which is
	// the report this test exists to make.
	if got := bar.OpenIndex(); got != -1 {
		t.Errorf("OpenIndex = %d after the menu list was emptied; there is no menu at that index", got)
	}
	if got := bar.DropdownBounds(); got != (gooey.Rect{}) {
		t.Errorf("DropdownBounds = %v after the menu list was emptied", got)
	}
	// And the frame after still composes — the accessors are not the
	// only thing that must survive it.
	c.Frame()
}

// TestAnOpenMenuWithNoItemsReportsNothing is finding 2, and it is the
// quiet half: no crash, just a plausible rect for a surface that was
// never arranged.
//
// Arrange refuses to show a dropdown whose menu has no items, so an
// accessor asking only IsOpen described a box that is not on screen —
// exactly what DropdownBounds' own doc comment says it must not do. An
// app placing a sixel into that rect draws it over the page.
func TestAnOpenMenuWithNoItemsReportsNothing(t *testing.T) {
	bar := &MenuBar{Menus: []Menu{{Title: "Empty"}}}
	c := gooey.NewComposer(bar, 40, 12)
	t.Cleanup(c.Close)
	c.Frame()
	bar.Open(0, nil)
	c.Frame()
	f, _ := c.Frame()

	// The premise: nothing was drawn below the bar row. Without this the
	// assertions below could pass for a fixture that did open properly.
	if got := paintedDropdown(t, f, 12); got.Y >= 0 {
		t.Fatalf("a menu with no items painted a dropdown at %v: this fixture cannot see the bug", got)
	}
	if got := bar.DropdownBounds(); got != (gooey.Rect{}) {
		t.Errorf("DropdownBounds = %v for a menu with no items, which Arrange declines to show", got)
	}
	if got := bar.OpenIndex(); got != -1 {
		t.Errorf("OpenIndex = %d while DropdownBounds reports nothing: the two accessors disagree "+
			"about one state", got)
	}
}

// menuWatcher reads OpenIndex while PAINTING, which is the only way to
// test the claim its doc makes.
type menuWatcher struct {
	gooey.Base
	bar *MenuBar
}

func (w *menuWatcher) Measure(gooey.Size) gooey.Size { return gooey.Size{W: 1, H: 1} }
func (w *menuWatcher) Render(f *gooey.Frame) {
	// The Get that makes this a dependency happens inside OpenIndex.
	b := w.Bounds()
	f.Cells.SetString(b.X, b.Y, string(rune('0'+w.bar.OpenIndex()+1)), render.Style{})
}

// TestReadingTheOpenIndexWhilePaintingIsADependency is finding 7.
//
// OpenIndex' doc says reading it from a Render makes it a paint
// dependency "exactly as IsOpen is", and CLAUDE.md is explicit that a
// DAMAGE COUNT is the only pin for a repaint claim — every other
// assertion here passes just as well when the whole tree repainted. The
// three tests above call the accessor from the test body, where nothing
// subscribes to anything.
func TestReadingTheOpenIndexWhilePaintingIsADependency(t *testing.T) {
	bar := geomBar()
	w := &menuWatcher{bar: bar}
	root := &rankRow{kids: []gooey.Component{bar, w}}

	c := gooey.NewComposer(root, 40, 12)
	t.Cleanup(c.Close)
	c.Frame()
	bar.Open(0, nil)
	c.Frame()
	c.Frame()

	// Switching menus must repaint the watcher.
	bar.Open(1, nil)
	_, painted := c.Frame()
	if painted == 0 {
		t.Fatal("switching menus repainted nothing at all")
	}
	var hit bool
	for _, d := range c.Damage() {
		if rectsOverlap(d, w.Bounds()) {
			hit = true
		}
	}
	if !hit {
		t.Errorf("switching menus did not damage the component that reads OpenIndex while painting: "+
			"the read is not a subscription. damage %v, watcher %v", c.Damage(), w.Bounds())
	}

	// And a frame with no change repaints nothing — otherwise the
	// assertion above is satisfied by a tree that repaints always.
	if _, painted := c.Frame(); painted != 0 {
		t.Errorf("an idle frame repainted %d components; the assertion above proves nothing", painted)
	}
}

// rankRow arranges its children side by side so the watcher has bounds
// of its own to be damaged.
type rankRow struct {
	gooey.Base
	kids []gooey.Component
}

func (r *rankRow) ChildComponents() []gooey.Component { return r.kids }
func (r *rankRow) Render(*gooey.Frame)                {}
func (r *rankRow) Measure(a gooey.Size) gooey.Size    { return a }
func (r *rankRow) Arrange(b gooey.Rect) {
	r.Base.Arrange(b)
	gooey.ArrangeChild(r.kids[0], b)
	gooey.ArrangeChild(r.kids[1], gooey.Rect{X: b.X + b.W - 1, Y: b.Y + b.H - 1, W: 1, H: 1})
}

// rectsOverlap is the damage check above; gooey.Rect carries no
// intersection predicate and Composer.Damage returns a slice.
func rectsOverlap(a, b gooey.Rect) bool {
	return a.X < b.X+b.W && b.X < a.X+a.W && a.Y < b.Y+b.H && b.Y < a.Y+a.H
}
