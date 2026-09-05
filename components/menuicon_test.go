package components

import (
	"image"
	"image/color"
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/graphics"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// Menu item icons (#400).
//
// The issue's own measurement is the design constraint and it is worth
// restating, because it rules out the obvious implementation: a dropdown
// row is ONE CELL TALL, and graphics.DrawHalfblock scales to cols×rows*2,
// so a one-row image is two vertical samples for the whole glyph. That
// app measured two clearly different icons coming back as the same
// uniform '▀' — 22/255 apart in one channel. So "degrade to halfblock"
// is not available at item size the way it is for a taller <Image>, and
// the cell-plane representation has to be a RUNE.
//
// Which makes the tier rule sharper than usual: the two tiers draw
// different THINGS, not the same thing at different fidelity, so the one
// property that must hold across them is that the dropdown MEASURES the
// same either way. buttonchrome.go reserves its pill rows the same way
// and for the same reason.

func iconImg(c color.Color) image.Image {
	im := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			im.Set(x, y, c)
		}
	}
	return im
}

// iconBar is a menu whose first item carries both tiers of icon and
// whose second carries neither — so every assertion below can tell "the
// gutter is reserved" from "this item drew something".
func iconBar() *MenuBar {
	return &MenuBar{Menus: []Menu{{
		Title: "_File",
		Items: []MenuItem{
			{Text: "_Open", Icon: iconImg(color.RGBA{200, 40, 40, 255}), IconRune: '○', Action: gooey.Command(func() {})},
			{Text: "_Quit", Action: gooey.Command(func() {})},
		},
	}}}
}

// plainBar is iconBar with the icons removed and nothing else changed.
// It is the width baseline: without it "the gutter is reserved" has
// nothing to be wider than.
func plainBar() *MenuBar {
	return &MenuBar{Menus: []Menu{{
		Title: "_File",
		Items: []MenuItem{
			{Text: "_Open", Action: gooey.Command(func() {})},
			{Text: "_Quit", Action: gooey.Command(func() {})},
		},
	}}}
}

// openWidth is the dropdown's arranged width, which is what the tier
// rule is about.
func openWidth(t *testing.T, bar *MenuBar, pixel bool) int {
	t.Helper()
	c := gooey.NewComposer(bar, 40, 16)
	t.Cleanup(c.Close)
	if pixel {
		c.SetCaps(term8x16(40, 16))
		c.SetGraphics(graphics.Kitty{})
	}
	c.Frame()
	bar.Open(0, nil)
	c.Frame()
	c.Frame()
	return bar.popupRect().W
}

// TestTheIconGutterIsReservedInBothTiers is the tier rule, and it is the
// assertion the whole design exists to satisfy.
//
// A dropdown that were one cell narrower without a graphics protocol
// would relayout when the capability probe's answer changed — and the
// probe runs after the first frame, so the menu would visibly reflow on
// a terminal that supports pixels. Reserving unconditionally is what
// buttonchrome.go does with its pill rows, for the same reason.
func TestTheIconGutterIsReservedInBothTiers(t *testing.T) {
	cells := openWidth(t, iconBar(), false)
	pixels := openWidth(t, iconBar(), true)
	if cells != pixels {
		t.Errorf("the dropdown measures %d cells wide without a graphics protocol and %d with one: "+
			"the gutter is being reserved conditionally, so the menu reflows when the capability "+
			"probe answers", cells, pixels)
	}
	// And it is actually reserving something, or the equality above holds
	// for the boring reason.
	plain := openWidth(t, plainBar(), false)
	if cells <= plain {
		t.Errorf("a menu with icons measures %d and the same menu without them measures %d: "+
			"no gutter is being reserved at all", cells, plain)
	}
}

// TestAnIconItemDrawsItsRuneOnTheCellPlane is the fallback tier, and the
// assertion is deliberately about the RUNE rather than about "something
// was drawn". A halfblock at one row is a uniform block whatever the
// image — the issue measured two different icons rendering identically —
// so a test satisfied by any non-space would pass against exactly the
// implementation this design rejects.
func TestAnIconItemDrawsItsRuneOnTheCellPlane(t *testing.T) {
	bar := iconBar()
	c := gooey.NewComposer(bar, 40, 16)
	t.Cleanup(c.Close)
	c.Frame()
	bar.Open(0, nil)
	c.Frame()
	f, _ := c.Frame()

	got := menuRows(f, bar.Bounds())
	if !strings.Contains(got, "○") {
		t.Errorf("an item with an IconRune did not draw it on the cell plane:\n%s", got)
	}
	if strings.ContainsRune(got, '▀') {
		t.Errorf("the fallback drew a halfblock: at one row that is two vertical samples for the "+
			"whole glyph, which is why this tier is a rune. See #400.\n%s", got)
	}
	// No placement, because there is no protocol to place into.
	if n := len(f.Placements()); n != 0 {
		t.Errorf("the cell tier emitted %d pixel placements", n)
	}
}

// TestAnIconItemPlacesItsImageWhenPixelsExist is the pixel tier: the
// image lands in the gutter, on the item's own row, one row tall.
func TestAnIconItemPlacesItsImageWhenPixelsExist(t *testing.T) {
	bar := iconBar()
	c := gooey.NewComposer(bar, 40, 16)
	t.Cleanup(c.Close)
	c.SetCaps(term8x16(40, 16))
	c.SetGraphics(graphics.Kitty{})
	c.Frame()
	bar.Open(0, nil)
	c.Frame()
	f, _ := c.Frame()

	ps := f.Placements()
	if len(ps) != 1 {
		t.Fatalf("want exactly one placement — one item carries an icon — got %d", len(ps))
	}
	b := bar.popupRect()
	p := ps[0]
	if p.Rows != 1 {
		t.Errorf("the icon is %d rows tall; a dropdown row is one cell", p.Rows)
	}
	if p.Row != b.Y+1 {
		t.Errorf("the icon is on row %d; the first item is on row %d", p.Row, b.Y+1)
	}
	if p.Col < b.X+1 || p.Col+p.Cols > b.X+b.W-1 {
		t.Errorf("the icon at cols %d..%d is outside the dropdown's interior %d..%d",
			p.Col, p.Col+p.Cols, b.X+1, b.X+b.W-1)
	}
	// THE PICTURE IS NARROWER THAN THE GUTTER IT SITS IN, and that is why
	// the reservation is two constants rather than one. iconWidth is what
	// Measure reserves; iconCols is how much of it the picture may take,
	// and the column between them is what keeps the image off the text.
	// Collapse them and the placement still lands inside the interior,
	// still sits on the right row, still measures the same — every other
	// assertion in this test passes — and the icon is drawn up against
	// the label.
	//
	// The gutter is MEASURED, not read off the constant: it is how much
	// further right the label sits than it does in plainBar, which is
	// iconBar with the icons removed and nothing else changed. Asserting
	// p.Cols == iconCols instead would be the code agreeing with itself.
	//
	// Both label columns come from StringWidth of the prefix rather than
	// from the strings.Index directly: Index returns a BYTE offset, which
	// agrees with the column only while the fixture stays ASCII.
	// Raised in review of #455.
	labelCol := func(f *gooey.Frame, r gooey.Rect) int {
		t.Helper()
		row := render.RowText(f.Cells, r.Y+1)
		k := strings.Index(row, "Open")
		if k < 0 {
			t.Fatalf("the first item's label is not on its row:\n\t%q", row)
		}
		return render.StringWidth(row[:k])
	}
	plain := plainBar()
	c2 := gooey.NewComposer(plain, 40, 16)
	t.Cleanup(c2.Close)
	c2.SetCaps(term8x16(40, 16))
	c2.SetGraphics(graphics.Kitty{})
	c2.Frame()
	plain.Open(0, nil)
	c2.Frame()
	f2, _ := c2.Frame()

	gutter := labelCol(f, b) - labelCol(f2, plain.popupRect())
	if gutter <= 0 {
		t.Fatalf("the icon menu reserves no more width than the plain one (%d cells); "+
			"nothing below was tested", gutter)
	}
	if p.Cols >= gutter {
		t.Errorf("the picture is %d cells wide in a %d-cell gutter: no column separates it "+
			"from the label, so the image is drawn up against the text", p.Cols, gutter)
	}
	// The SECOND item has no icon and must not have borrowed the first's.
	for _, q := range ps {
		if q.Row == b.Y+2 {
			t.Errorf("an item with no icon got a placement at row %d", q.Row)
		}
	}
}

// TestAnIconAndACheckAreDifferentColumns pins that the two leading
// columns compose rather than one overwriting the other. A menu with
// both is the case where an off-by-one lands the label on top of the
// icon, and neither feature's own tests would see it.
func TestAnIconAndACheckAreDifferentColumns(t *testing.T) {
	on := prop.NewSource(true)
	bar := &MenuBar{Menus: []Menu{{
		Title: "_View",
		Items: []MenuItem{
			{Text: "_Wrap", Checked: on, IconRune: '○', Action: gooey.Command(func() {})},
		},
	}}}
	c := gooey.NewComposer(bar, 40, 16)
	t.Cleanup(c.Close)
	c.Frame()
	bar.Open(0, nil)
	c.Frame()
	f, _ := c.Frame()

	got := menuRows(f, bar.Bounds())
	if !strings.Contains(got, "○") {
		t.Errorf("the icon is missing when the item also has a check:\n%s", got)
	}
	if !strings.Contains(got, "[x]") {
		t.Errorf("the check is missing when the item also has an icon:\n%s", got)
	}
	if !strings.Contains(got, "Wrap") {
		t.Errorf("the label is missing when the item has both:\n%s", got)
	}
}

// TestAWideIconRuneDoesNotOverrunItsGutter is the column-count trap
// CLAUDE.md names: a CJK or emoji rune is ONE rune and TWO cells, so a
// gutter measured in runes puts the label one cell into the icon. Read
// back with render.RowText, which resolves the continuation marker — a
// per-rune reader cannot see this at all.
func TestAWideIconRuneDoesNotOverrunItsGutter(t *testing.T) {
	bar := &MenuBar{Menus: []Menu{{
		Title: "_File",
		Items: []MenuItem{
			{Text: "Open", IconRune: '📁', Action: gooey.Command(func() {})},
		},
	}}}
	c := gooey.NewComposer(bar, 40, 16)
	t.Cleanup(c.Close)
	c.Frame()
	bar.Open(0, nil)
	c.Frame()
	f, _ := c.Frame()

	b := bar.popupRect()
	row := render.RowText(f.Cells, b.Y+1)
	if !strings.Contains(row, "Open") {
		t.Fatalf("the label is missing entirely: %q", row)
	}
	// The label must start at the same column it would with a
	// single-width rune — the gutter is a CELL count.
	narrow := &MenuBar{Menus: []Menu{{
		Title: "_File",
		Items: []MenuItem{{Text: "Open", IconRune: 'o', Action: gooey.Command(func() {})}},
	}}}
	c2 := gooey.NewComposer(narrow, 40, 16)
	t.Cleanup(c2.Close)
	c2.Frame()
	narrow.Open(0, nil)
	c2.Frame()
	f2, _ := c2.Frame()
	b2 := narrow.popupRect()
	row2 := render.RowText(f2.Cells, b2.Y+1)

	// COLUMNS, not strings.Index. That returns a BYTE offset, and an
	// emoji is four bytes to one cell's worth of arithmetic — comparing
	// byte offsets reports a difference for two rows that are identically
	// laid out, which is this file's own trap sprung on itself. Measure
	// the prefix with render.StringWidth, which is the same function the
	// gutter is padded with.
	wideAt := render.StringWidth(row[:strings.Index(row, "Open")])
	narrowAt := render.StringWidth(row2[:strings.Index(row2, "Open")])
	if wideAt != narrowAt {
		t.Errorf("a two-cell icon rune moves the label to column %d where a one-cell rune "+
			"puts it at %d: wide %q vs narrow %q — the gutter is being counted in runes, "+
			"not columns", wideAt, narrowAt, row, row2)
	}
}

// THE PLACEMENT MUST ALSO GO AWAY, and no cell assertion in this file
// can see whether it did.
//
// Under sixel or kitty a stale placement is pixels composited over the
// page — the cell plane is untouched and every RowText check stays
// green while an icon sits on screen after its dropdown closed. This is
// the first component in the tree to place images from a POPUP SURFACE
// THAT COMES AND GOES, a lifecycle Image and Button never exercise:
// placements.go names "painted fewer images" as its own diff case, and
// nothing else in the repo reaches that case from an overlay.
//
// Both arms passed when this was written — it is a pin, not a fix.
// Raised in review of #455.
func TestTheIconPlacementIsWithdrawn(t *testing.T) {
	t.Run("dismissing the dropdown", func(t *testing.T) {
		bar := iconBar()
		c := gooey.NewComposer(bar, 40, 16)
		t.Cleanup(c.Close)
		c.SetCaps(term8x16(40, 16))
		c.SetGraphics(graphics.Kitty{})
		c.Frame()
		bar.Open(0, nil)
		c.Frame()
		if f, _ := c.Frame(); len(f.Placements()) != 1 {
			t.Fatalf("the open dropdown published %d placements, want 1 — nothing below was tested",
				len(f.Placements()))
		}
		bar.Dismiss()
		f, _ := c.Frame()
		if n := len(f.Placements()); n != 0 {
			t.Errorf("a dismissed dropdown still publishes %d pixel placements: under sixel or kitty "+
				"that is an icon left composited over the page, and no cell assertion can see it", n)
		}
	})

	t.Run("switching to an icon-free menu", func(t *testing.T) {
		bar := &MenuBar{Menus: []Menu{
			{Title: "_File", Items: []MenuItem{
				{Text: "_Open", Icon: iconImg(color.RGBA{200, 40, 40, 255}), Action: gooey.Command(func() {})},
			}},
			{Title: "_Edit", Items: []MenuItem{
				{Text: "_Copy", Action: gooey.Command(func() {})},
			}},
		}}
		c := gooey.NewComposer(bar, 40, 16)
		t.Cleanup(c.Close)
		c.SetCaps(term8x16(40, 16))
		c.SetGraphics(graphics.Kitty{})
		c.Frame()
		bar.Open(0, nil)
		c.Frame()
		if f, _ := c.Frame(); len(f.Placements()) != 1 {
			t.Fatalf("the first menu published %d placements, want 1", len(f.Placements()))
		}
		bar.Open(1, nil)
		f, _ := c.Frame()
		if n := len(f.Placements()); n != 0 {
			t.Errorf("switching to a menu whose items carry no icons still publishes %d placements: "+
				"the previous menu's icon is stranded on screen", n)
		}
	})
}
