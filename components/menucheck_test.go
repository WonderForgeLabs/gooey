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
//
// THE WINDOW IS THE FRAME, derived from f.Cells rather than written
// down. This read a fixed 40x14 from bar.Bounds().Y, which is the exact
// trap SpanText's own doc names — "every converted reader here reads a
// FIXED extent … a dropdown that moves down the screen, or a composer
// resized in a later edit, turns the tail of one of these reads into
// phantom blanks" — and this helper is the reader that paragraph is
// about. It also took a gooey.Rect it barely used: only b.Y, never b.X,
// so the window was never bounds-relative in the axis its callers'
// diagnostics claimed, and the 40 had to agree with a composer width
// written separately in each test. Both constants are gone rather than
// explained. Raised in review of #520.
func menuRows(f *gooey.Frame) string {
	var sb strings.Builder
	for y := 0; y < f.Cells.H; y++ {
		sb.WriteString(render.SpanText(f.Cells, 0, y, f.Cells.W))
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

	got := menuRows(f)
	if !strings.Contains(got, "[ ] Wrap") {
		t.Errorf("an unchecked item does not draw an empty box:\n%s", got)
	}

	on.Set(true)
	f, _ = c.Frame()
	got = menuRows(f)
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

	rows := strings.Split(menuRows(f), "\n")
	// EVERY MATCH, not the last one, for the reason
	// TestACheckItemDrawsAWideLabelInItsOwnColumns gives below: this was
	// an assignment inside the loop with no break, so a second row
	// holding "Wrap" was resolved by iteration order and the other became
	// invisible. menuRows just widened from a fixed 14 rows to the whole
	// frame, which is more rows for a second match to hide in. Raised in
	// review of #520.
	// PAIRED, because the two answers are about one match. This returned
	// the byte offset of the LAST match and the row indices of ALL of
	// them, then indexed a row taken from the second with an offset
	// taken from the first — correct only because the len == 1 guard
	// below happened to make them agree. Loosening that guard to "at
	// least one", which is the obvious next edit and the shape this
	// helper had one commit ago, slices one row at an offset measured in
	// another: a wrong column in a failure message at best, and an
	// out-of-range slice at worst. Safe by construction now rather than
	// by a neighbouring guard. Raised in review of #520.
	type match struct{ row, byteAt int }
	find := func(word string) (matches []match) {
		for y, r := range rows {
			if i := strings.Index(r, word); i >= 0 {
				matches = append(matches, match{y, i})
			}
		}
		return matches
	}
	wrap, plain := find("Wrap"), find("Plain")
	if len(wrap) != 1 || len(plain) != 1 {
		t.Fatalf("%q matched rows %v and %q matched rows %v, want one each: this "+
			"test compares ONE lead column against another, and two candidates "+
			"make the answer depend on iteration order:\n%s",
			"Wrap", wrap, "Plain", plain, strings.Join(rows, "\n"))
	}
	// MEASURED IN COLUMNS, AND COMPARED IN COLUMNS. strings.Index answers
	// in BYTES, and the dropdown's border is '│' — three bytes for one
	// column — so the offset printed 7 where the cell is 5, a diagnostic
	// sending the reader to the wrong cell of a row this file now puts
	// wide glyphs into. That much was already fixed; the COMPARISON was
	// left in bytes, on the argument that both rows carry the same prefix
	// so the offsets differ exactly when the columns do.
	//
	// That argument is true of today's fixture and is no longer
	// CONSTRAINED. menuRows reads the whole frame now, so `len(wrap) == 1
	// && len(plain) == 1` no longer implies both matches are dropdown
	// rows sharing a border prefix — only that each word appears once
	// anywhere on screen. A byte comparison over two rows with different
	// prefixes is then a column comparison only by coincidence, and
	// docs/specs/2026-09-05-menu-item-icons.md:118 records this exact
	// trap springing twice already in menu code. The two widths are
	// computed for the message regardless, so comparing them costs
	// nothing. Raised in review of #520.
	wrapCol := render.StringWidth(rows[wrap[0].row][:wrap[0].byteAt])
	plainCol := render.StringWidth(rows[plain[0].row][:plain[0].byteAt])
	if wrapCol != plainCol {
		t.Errorf("the checked item's text starts at column %d and the plain one's at %d; "+
			"a menu's lead column belongs to the menu", wrapCol, plainCol)
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

	got := menuRows(f)
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

	// menuRows, not a second hand-built window. Both extents were written
	// down here — 60 columns and 8 rows against a 60x16 composer, so this
	// read HALF the frame's height, on the axis a dropdown moves along and
	// the one SpanText's own doc calls out as turning a short read into
	// phantom blanks. menuRows derives both from f.Cells and is four lines
	// up. Raised in review of #520.
	if got := menuRows(f); !strings.Contains(got, "[x] Wrap long lines") {
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
	// COLUMNS ARE NOT RUNES AND NOT BYTES, and this comment used to say
	// the first of those. strings.Index returns BYTES — the dropdown's
	// border is '│', three bytes for one column, which is how the first
	// version of this assertion read column 7 of the row below and
	// reported a perfectly correct underline as misplaced — and the
	// conversion off a byte offset is render.StringWidth, not a rune
	// count. A rune count agrees with the column only while the prefix is
	// narrow, which it is in this fixture and is not in
	// TestACheckItemDrawsAWideLabelInItsOwnColumns thirty lines below:
	// exactly the accidental agreement the paragraph under this one names
	// for the row index. Raised in review of #520.
	// THE ROW INDEX IS THE ROW, now that menuRows reads the frame from
	// its top rather than from bar.Bounds().Y. This was `b.Y + dy` and
	// it was right for the old window; it is right for this one only
	// because a MenuBar sits at y=0 in this fixture, which is exactly
	// the kind of accidental agreement a moved fixture breaks silently.
	// Raised in review of #520.
	rows := strings.Split(menuRows(f), "\n")
	for y, r := range rows {
		bytesAt := strings.Index(r, "[x] Wrap")
		if bytesAt < 0 {
			continue
		}
		at := render.StringWidth(r[:bytesAt])
		// The check box must have SURVIVED, which is what discriminates
		// an underline placed in the label from one placed over the box:
		// Render SETS the rune as well as the style, so a wrong offset
		// does not leave an underlined '[' behind — it overwrites the '['
		// WITH the accelerator letter.
		want := at + render.StringWidth("[x] ")
		cell := f.Cells.At(want, y)
		if !cell.Style.Underline || cell.Rune != 'W' {
			t.Errorf("column %d of row %d is %q (underline=%v); the accelerator underline "+
				"is not on the label's first letter", want, y, cell.Rune, cell.Style.Underline)
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

	rows := strings.Split(menuRows(f), "\n")
	// EVERY MATCH, not the last one. This was `got, found = …` inside
	// the loop with no break, so a second row containing "Wrap" — a
	// second item, a status line echoing the label, a scrolled duplicate
	// — was resolved by iteration order and the other became invisible.
	// This test's whole claim is about a BORDER POSITION, so two
	// candidate rows is a fault to name rather than a choice to make.
	// Raised in review of #520.
	var at []int
	for y, r := range rows {
		if strings.Contains(r, "Wrap") {
			at = append(at, y)
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
	if len(at) == 0 {
		// len(rows)-1: menuRows TERMINATES each line with a newline, so
		// Split hands back a trailing empty element that is not a row of
		// the window. This message exists to name the window, so the
		// number in it has to BE the window. Raised in review of #520.
		//
		// AND THE WINDOW IS NOW THE FRAME. This said "a fixed 40 columns
		// from bar.Bounds", which named neither axis correctly: the read
		// started at column 0 and never touched b.X, and the likelier way
		// to miss a dropdown was the fixed 14-row HEIGHT, which is the
		// axis a menu moves along. Both constants are gone — menuRows
		// reads f.Cells.W by f.Cells.H — so a missing row now means the
		// dropdown is not on the frame at all. Raised in review of #520.
		t.Fatalf("none of the %d rows of the %dx%d frame holds %q, so the "+
			"dropdown did not paint — a different fault from the row being "+
			"mis-sized:\n%s",
			len(rows)-1, f.Cells.W, f.Cells.H, "Wrap", strings.Join(rows, "\n"))
	}
	if len(at) > 1 {
		t.Fatalf("%q appears on rows %v. This test asserts one row against an "+
			"exact border position, and picking one of two by iteration order "+
			"would hide whichever it did not pick:\n%s",
			"Wrap", at, strings.Join(rows, "\n"))
	}
	// TRIMMED AT BOTH ENDS, because the claim is RELATIVE: the right
	// border lands one column after the glyphs. TrimRight alone made the
	// assertion depend on the dropdown starting at column 0, which holds
	// only because _View is this fixture's one menu — add a menu before
	// it, the ordinary way a menu fixture grows, and the row reads
	// "        │[x] Wrap 世界 │", the comparison fails, and the message
	// diagnoses the menu's WIDTH when the difference is its X. That is
	// the fault-versus-fault distinction the missing-row diagnostic
	// above already draws, left undrawn one line below it. If the lead
	// column ever becomes part of the claim it gets its own assertion
	// and its own sentence. Raised in review of #520.
	got := strings.TrimSpace(rows[at[0]])
	if want := "│[x] Wrap 世界 │"; got != want {
		t.Errorf("the wide label's row reads %q, want %q. A box narrower than "+
			"its own text is the menu measuring runes where it owes columns; a "+
			"row holding U+FFFD is this test's reader doing it instead. The "+
			"comparison is trimmed at both ends, so the difference is the box "+
			"and not where it starts.\n%s",
			got, want, strings.Join(rows, "\n"))
	}
}
