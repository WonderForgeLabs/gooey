package components

import (
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// Menu check items. The point of binding a handle rather than carrying a
// bool is that a menu item and whatever else displays the same state —
// an accelerator's own indicator, a status line — are ONE state rendered
// twice. These pin the two halves that makes true: the box is drawn from
// the handle, and the read happens inside the dropdown's paint node so a
// flip while the menu is open actually repaints it.

func checkBarFixture(checked *prop.Property[bool]) *MenuBar {
	return &MenuBar{Menus: []Menu{{
		Title: "_View",
		Items: []MenuItem{
			{Text: "_Wrap", Checked: checked, Action: gooey.Command(func() {})},
			{Text: "_Plain", Action: gooey.Command(func() {})},
		},
	}}}
}

// menuRows is the open dropdown as a terminal would show it. Through
// render.SpanText — a rune-per-cell read puts the continuation marker in
// the row, which is what kept a wide-glyph label out of this file's
// fixtures. See #516.
func menuRows(f *gooey.Frame, b gooey.Rect) string {
	var sb strings.Builder
	for y := b.Y; y < b.Y+14; y++ {
		sb.WriteString(render.SpanText(f.Cells, 0, y, 40))
		sb.WriteByte('\n')
	}
	return sb.String()
}

func TestACheckItemDrawsItsBox(t *testing.T) {
	on := prop.NewSource(false)
	bar := checkBarFixture(on)
	c := gooey.NewComposer(bar, 40, 16)
	c.Frame()
	bar.Open(0, nil)
	c.Frame()
	f, _ := c.Frame()

	got := menuRows(f, bar.Bounds())
	if !strings.Contains(got, "[ ] Wrap") {
		t.Errorf("an unchecked item does not draw an empty box:\n%s", got)
	}

	on.Set(true)
	f, _ = c.Frame()
	got = menuRows(f, bar.Bounds())
	if !strings.Contains(got, "[x] Wrap") {
		t.Errorf("after checking, the item does not draw a checked box:\n%s", got)
	}
}

// TestAPlainItemAlignsWithItsCheckedNeighbour — the lead column is a
// property of the MENU, not of the item. A menu with one check item that
// stepped its plain items three cells left would read as broken.
func TestAPlainItemAlignsWithItsCheckedNeighbour(t *testing.T) {
	bar := checkBarFixture(prop.NewSource(true))
	c := gooey.NewComposer(bar, 40, 16)
	c.Frame()
	bar.Open(0, nil)
	c.Frame()
	f, _ := c.Frame()

	rows := strings.Split(menuRows(f, bar.Bounds()), "\n")
	var wrapAt, plainAt int = -1, -1
	for _, r := range rows {
		if i := strings.Index(r, "Wrap"); i >= 0 {
			wrapAt = i
		}
		if i := strings.Index(r, "Plain"); i >= 0 {
			plainAt = i
		}
	}
	if wrapAt < 0 || plainAt < 0 {
		t.Fatalf("could not find both items:\n%s", strings.Join(rows, "\n"))
	}
	if wrapAt != plainAt {
		t.Errorf("the checked item's text starts at column %d and the plain one's at %d; "+
			"a menu's lead column belongs to the menu", wrapAt, plainAt)
	}
}

// TestAMenuWithNoCheckItemsKeepsItsOldSpacing — the change must be
// invisible to every menu that does not use it.
func TestAMenuWithNoCheckItemsKeepsItsOldSpacing(t *testing.T) {
	bar := &MenuBar{Menus: []Menu{{
		Title: "_View",
		Items: []MenuItem{{Text: "_Wrap", Action: gooey.Command(func() {})}},
	}}}
	c := gooey.NewComposer(bar, 40, 16)
	c.Frame()
	bar.Open(0, nil)
	c.Frame()
	f, _ := c.Frame()

	got := menuRows(f, bar.Bounds())
	if strings.Contains(got, "[ ]") || strings.Contains(got, "[x]") {
		t.Errorf("a menu with no check items drew a check column:\n%s", got)
	}
	// One cell of lead, exactly as before: "│ Wrap".
	if !strings.Contains(got, "│ Wrap") {
		t.Errorf("a plain menu's item lost its single-space lead:\n%s", got)
	}
}

// TestFlippingACheckRepaintsOnlyTheDropdown is the damage pin, and it is
// what makes the handle worth having over a bool. The box is read inside
// drawDropdown, which runs inside the popup surface's own paint node.
func TestFlippingACheckRepaintsOnlyTheDropdown(t *testing.T) {
	on := prop.NewSource(false)
	bar := checkBarFixture(on)
	c := gooey.NewComposer(bar, 40, 16)
	c.Frame()
	bar.Open(0, nil)
	for i := 0; i < 5; i++ {
		if _, n := c.Frame(); n == 0 {
			break
		}
	}

	on.Set(true)
	_, painted := c.Frame()
	if painted == 0 {
		t.Fatal("flipping a check with the menu OPEN repainted nothing: the box on screen " +
			"is stale, which means it is not read while painting")
	}
	if painted != 1 {
		t.Errorf("flipping a check repainted %d components, want 1 (the dropdown surface); "+
			"damage %v", painted, c.Damage())
	}

	// Closed, nothing on screen reads the box, so nothing repaints.
	bar.Dismiss()
	for i := 0; i < 5; i++ {
		if _, n := c.Frame(); n == 0 {
			break
		}
	}
	on.Set(false)
	if _, painted := c.Frame(); painted != 0 {
		t.Errorf("flipping a check with the menu CLOSED repainted %d; nothing is displaying "+
			"it", painted)
	}
}

// TestACheckedMenuIsWideEnoughForItsLabels — popupRect sizes the dropdown
// from the item widths, and forgetting the lead column clips every label
// by three cells. Silent: the text is simply short.
func TestACheckedMenuIsWideEnoughForItsLabels(t *testing.T) {
	bar := &MenuBar{Menus: []Menu{{
		Title: "_View",
		Items: []MenuItem{
			{Text: "_Wrap long lines", Checked: prop.NewSource(true), Action: gooey.Command(func() {})},
		},
	}}}
	c := gooey.NewComposer(bar, 60, 16)
	c.Frame()
	bar.Open(0, nil)
	c.Frame()
	f, _ := c.Frame()

	var sb strings.Builder
	for y := 0; y < 8; y++ {
		sb.WriteString(render.SpanText(f.Cells, 0, y, 60))
		sb.WriteByte('\n')
	}
	if got := sb.String(); !strings.Contains(got, "[x] Wrap long lines") {
		t.Errorf("the label is clipped; the dropdown was sized without the check column:\n%s", got)
	}
}

// TestTheAcceleratorUnderlineFollowsTheCheckColumn — the mnemonic rule is
// drawn at an offset, and an offset measured from the wrong place puts
// the underline under the check box.
func TestTheAcceleratorUnderlineFollowsTheCheckColumn(t *testing.T) {
	bar := checkBarFixture(prop.NewSource(true))
	c := gooey.NewComposer(bar, 40, 16)
	c.Frame()
	bar.Open(0, nil)
	c.Frame()
	f, _ := c.Frame()

	// The assertion has to be about the COLUMN, not about finding an
	// underlined 'W' somewhere. Render SETS the rune as well as the style
	// at the offset it computes, so a wrong offset does not leave an
	// underlined '[' to catch — it overwrites the '[' WITH a 'W'. A test
	// that only looked for an underlined 'W' passed against exactly that,
	// which a mutation run is how we found out.
	// COLUMNS ARE RUNES, and strings.Index returns BYTES. The dropdown's
	// border is '│', three bytes each, so a byte offset lands three cells
	// right of the cell it names — which is how the first version of this
	// assertion read column 7 of the row below and reported a perfectly
	// correct underline as misplaced.
	b := bar.Bounds()
	rows := strings.Split(menuRows(f, b), "\n")
	for dy, r := range rows {
		bytesAt := strings.Index(r, "[x] Wrap")
		if bytesAt < 0 {
			continue
		}
		at := len([]rune(r[:bytesAt]))
		// The check box must have SURVIVED, which is what discriminates
		// an underline placed in the label from one placed over the box:
		// Render SETS the rune as well as the style, so a wrong offset
		// does not leave an underlined '[' behind — it overwrites the '['
		// WITH the accelerator letter.
		want := at + len([]rune("[x] "))
		cell := f.Cells.At(want, b.Y+dy)
		if !cell.Style.Underline || cell.Rune != 'W' {
			t.Errorf("column %d of row %d is %q (underline=%v); the accelerator underline "+
				"is not on the label's first letter", want, b.Y+dy, cell.Rune, cell.Style.Underline)
		}
		return
	}
	t.Fatalf("no intact \"[x] Wrap\" row; the underline overwrote the check box:\n%s",
		strings.Join(rows, "\n"))
}

// TestACheckItemDrawsAWideLabelInItsOwnColumns is the fixture menuRows
// could not hold before it read through render.SpanText, and it is the
// half of #516 that matters: converting a reader proves nothing on its
// own — the fixture it UNBLOCKS is what pins a claim.
//
// Measured, both readers, on the same frame:
//
//	SpanText  "│[x] Wrap 世界 │"
//	Cell.Rune "│[x] Wrap 世�界� │"
//
// So an assertion written against the second failed for a reason that
// was not the bug, and nobody wrote one — which is how every fixture in
// this file came to be ASCII, and an ASCII fixture agrees with itself
// under the rune rule and the column rule alike.
//
// THE BORDER IS THE ASSERTION, not the label. That the glyphs read back
// is the reader working; that the right edge lands one column after them
// is the DROPDOWN having sized itself in columns rather than runes. A
// menu measuring "Wrap 世界" with len([]rune(...)) asks for a box two
// columns narrower than its own text and clips the label, which is the
// #357 failure this file's check column is otherwise full of ASCII
// proof against.
func TestACheckItemDrawsAWideLabelInItsOwnColumns(t *testing.T) {
	bar := &MenuBar{Menus: []Menu{{
		Title: "_View",
		Items: []MenuItem{
			{Text: "_Wrap 世界", Checked: prop.NewSource(true), Action: gooey.Command(func() {})},
			{Text: "_Plain", Action: gooey.Command(func() {})},
		},
	}}}
	c := gooey.NewComposer(bar, 40, 16)
	t.Cleanup(c.Close)
	c.Frame()
	bar.Open(0, nil)
	c.Frame()
	f, _ := c.Frame()

	rows := strings.Split(menuRows(f, bar.Bounds()), "\n")
	var got string
	var found bool
	for _, r := range rows {
		if strings.Contains(r, "Wrap") {
			got, found = strings.TrimRight(r, " "), true
		}
	}
	// "NO ROW MATCHED" AND "THE ROW IS WRONG" ARE DIFFERENT FAULTS, and
	// without this they arrive as one. got stays "" when nothing
	// matches, the comparison below still fails, and its message reads
	// `the wide label's row reads ""` — which points at the menu's width
	// when the real answer is that the dropdown moved outside the fixed
	// window menuRows reads. That is one step removed from the class
	// SpanText's own doc calls out: a read that has drifted off the
	// surface comes back as blanks rather than going short, so going
	// short is not the signal either. Raised in review of #520.
	if !found {
		t.Fatalf("none of the %d rows menuRows read holds %q. The dropdown is "+
			"outside the reader's window (a fixed 40 columns from bar.Bounds), "+
			"which is a different fault from the row being mis-sized:\n%s",
			len(rows), "Wrap", strings.Join(rows, "\n"))
	}
	if want := "│[x] Wrap 世界 │"; got != want {
		t.Errorf("the wide label's row reads %q, want %q. A box narrower than "+
			"its own text is the menu measuring runes where it owes columns; a "+
			"row holding U+FFFD is this test's reader doing it instead.\n%s",
			got, want, strings.Join(rows, "\n"))
	}
}
