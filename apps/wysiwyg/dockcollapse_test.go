package main

// The cross-axis collapse contract: what a collapsed pane gives back when
// its slot stacks in COLUMNS, and what the fit check can see of it.
//
// #436 fixed the one-pane bottom strip by moving the whole shrink to
// laidOutExtent — the slot's rows — and making place ignore collapse
// there entirely. With two panes that leaves the reported symptom intact
// AND loses the reclamation that existed before it: the strip stays as
// tall as the open pane needs (correctly — a horizontal strip cannot be
// partly short), and the collapsed pane sits at a full even share with
// its body blank. Nobody gets the room. #441.

import (
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/render"
)

// bottomPair docks a second pane in Bottom through the same model call
// the drop path uses, and returns the two panes with the strip settled.
func bottomPair(t *testing.T, ed *editor, c *gooey.Composer) (*dockPane, *dockPane) {
	t.Helper()
	first := pane(t, ed, "panel")
	second := pane(t, ed, "explorer")
	ed.dock.Move(second, dockBottom)
	settle(t, c)
	if first.Bounds().W <= 0 || second.Bounds().W <= 0 {
		t.Fatalf("the bottom strip laid out as %+v / %+v; nothing below "+
			"discriminates", first.Bounds(), second.Bounds())
	}
	return first, second
}

// TestACollapsedPaneInAColumnStripGivesItsColumnsBack is the reported
// symptom of #441, and the assertion that fails on the branch that
// closed #431.
//
// Collapse is documented as "the operation that reclaims room". In a
// strip that stacks left to right the room is COLUMNS, and a collapsed
// pane's natural extent along that axis is the width of the header it
// still draws — not headerH, which is a row count, and not a full even
// share, which reclaims nothing.
func TestACollapsedPaneInAColumnStripGivesItsColumnsBack(t *testing.T) {
	ed, c := dockFixture(t)
	first, second := bottomPair(t, ed, c)

	wasFirst, wasSecond := first.Bounds(), second.Bounds()

	ed.dock.ToggleCollapsed(first)
	settle(t, c)

	got, gotSecond := first.Bounds(), second.Bounds()

	want := first.headerCols()
	if got.W != want {
		t.Errorf("the collapsed pane is %d columns wide, want %d — its header's "+
			"own width. It was %d before the collapse, so it gave back %d of the "+
			"%d columns it can no longer use", got.W, want, wasFirst.W,
			wasFirst.W-got.W, wasFirst.W-want)
	}
	if grew := gotSecond.W - wasSecond.W; grew != wasFirst.W-want {
		t.Errorf("the neighbour grew by %d columns and the collapsed pane gave up "+
			"%d. The space a collapse reclaims has to go to the neighbours — that "+
			"is what makes it the operation that reclaims room rather than one "+
			"that hides a body", grew, wasFirst.W-want)
	}
	// The strip keeps its rows, because the open pane still needs them.
	// Without this the test above passes against a fix that shortened the
	// strip on `any collapsed` and clipped the pane beside it.
	if gotSecond.H != wasSecond.H {
		t.Errorf("the still-open pane went from %d rows to %d when its neighbour "+
			"collapsed; a horizontal strip cannot be partly short", wasSecond.H,
			gotSecond.H)
	}
}

// TestACollapsedHeaderStillReadsItsTitle is what separates this fix from
// the behaviour it restores reclamation from.
//
// Before #436 a collapsed bottom pane was given headerH — ONE COLUMN —
// so it did reclaim, and what survived was a lone chevron. A width
// assertion alone cannot tell that apart from a correct one: both are
// "narrower than before". The pane has to still say which pane it is.
func TestACollapsedHeaderStillReadsItsTitle(t *testing.T) {
	ed, c := dockFixture(t)
	first, _ := bottomPair(t, ed, c)

	ed.dock.ToggleCollapsed(first)
	settle(t, c)
	f, _ := c.Frame()

	// render.RowText, not this package's rowText: the latter builds the
	// string cell by cell and renders a continuation marker as a literal
	// rune, so a header holding a wide glyph reads back as mojibake.
	//
	// The WHOLE row is read and the assertion is containment of the
	// chevron AND the title together, because everything else in the
	// shell shares this row — the activity rail to the left, the open
	// pane to the right. Asserting a prefix would be asserting about the
	// rail.
	b := first.Bounds()
	row := render.RowText(f.Cells, b.Y)
	if want := "> " + first.Title; !strings.Contains(row, want) {
		t.Errorf("row %d reads %q and does not contain %q. A collapse that "+
			"narrows the pane past its own header has reclaimed the room by "+
			"making the pane unidentifiable and un-openable — the chevron is the "+
			"hit target — which is the state #431 was filed against", b.Y, row, want)
	}
}

// TestACollapsedPanesWidthIsColumnsNotRunes is the CLAUDE.md wide-glyph
// pin, and it is written the way that file prescribes: two titles of the
// same COLUMN width and different rune counts must produce the same
// collapsed extent.
//
// dockPane.Render sized its header with len([]rune(line)) and clipTo
// sliced runes, so every fixture in this package agreed with either rule.
// Any header-column arithmetic added for a horizontal collapse inherits
// that bug unless it comes from render.StringWidth — named as a
// constraint in #441 before the work started.
func TestACollapsedPanesWidthIsColumnsNotRunes(t *testing.T) {
	ed, c := dockFixture(t)
	first, _ := bottomPair(t, ed, c)

	// "世界" is 2 runes and 4 columns; "abcd" is 4 runes and 4 columns.
	// A rune count says they differ; a column count says they do not.
	first.Title = "世界"
	ed.dock.ToggleCollapsed(first)
	settle(t, c)
	wide := first.Bounds().W

	first.Title = "abcd"
	ed.dock.ToggleCollapsed(first) // open
	settle(t, c)
	ed.dock.ToggleCollapsed(first) // and closed again, re-measuring
	settle(t, c)
	ascii := first.Bounds().W

	if wide != ascii {
		t.Errorf("a %q title collapses to %d columns and %q to %d. They are the "+
			"same width on a terminal and differ only in rune count, so this is "+
			"len([]rune(s)) where render.StringWidth belongs — the pane is sized "+
			"narrower than its own text and the glyphs are lost inside its rect",
			"世界", wide, "abcd", ascii)
	}
}

// TestAHeadersColumnBudgetIsItsTextPlusThePin pins the QUANTITY, with a
// literal, and that is the only way it can be pinned.
//
// Every other test in this file compares a laid-out width against
// headerCols() — which is right for the mechanism, and blind to the
// number: drop the pin column and the expectation drops with it, so the
// arm is silent. A policy value has to be asserted as a value at least
// once. The mechanism (both sides of the boundary, wide glyphs, the
// reclamation arithmetic) is derived everywhere else.
func TestAHeadersColumnBudgetIsItsTextPlusThePin(t *testing.T) {
	p := newDockPane("x", "PANEL", dockBottom, 4, false)
	// 1 chevron + 1 space + 5 for "PANEL" + 1 for the pin the header
	// always draws at its right edge.
	const want = 8
	if got := p.headerCols(); got != want {
		t.Errorf("headerCols() for a %q header is %d, want %d = 1 chevron + 1 "+
			"space + %d title + 1 pin. A collapsed pane laid out at this width "+
			"draws exactly its header and nothing is clipped", p.Title, got, want,
			len(p.Title))
	}
	// AND THE PIN IS WHY IT IS NOT 7. Without the extra column the
	// header's last cell is the title's last character and the pin — the
	// only thing that says a pane survives HideUnpinned — is clipped off
	// a collapsed pane forever.
	if got := p.headerCols() - render.StringWidth(p.headerLead()); got != 1 {
		t.Errorf("headerCols() reserves %d columns beyond the header text, want 1 "+
			"for the pin", got)
	}
}

// TestACollapsedHeaderStillShowsThePin is the same claim from the paint
// side, so the budget above is not just arithmetic that agrees with
// itself.
//
// The WIDE case is the one that fails if only headerCols was converted
// and Render was left counting runes. A collapsed pane titled "世界" is
// laid out at 6 columns; a rune-counted pad then adds two spaces too
// many, the clip cuts what it must, and the pin — the last thing on the
// line — is what falls off. Both halves have to be columns or the two
// disagree at exactly the width the first half chose.
func TestACollapsedHeaderStillShowsThePin(t *testing.T) {
	for _, title := range []string{"PANEL", "世界"} {
		t.Run(title, func(t *testing.T) {
			ed, c := dockFixture(t)
			first, _ := bottomPair(t, ed, c)
			first.Title = title

			ed.dock.TogglePinned(first)
			ed.dock.ToggleCollapsed(first)
			settle(t, c)
			f, _ := c.Frame()

			b := first.Bounds()
			row := render.RowText(f.Cells, b.Y)
			if want := "> " + title + "*"; !strings.Contains(row, want) {
				t.Errorf("row %d reads %q and does not contain %q. The pin is the "+
					"only mark that says this pane survives HideUnpinned, and a "+
					"collapsed pane whose header is measured in the wrong units "+
					"clips it off permanently", b.Y, row, want)
			}
		})
	}
}

// TestAStripNarrowerThanItsHeadersStaysInsideItsSlot is the bound on the
// new arithmetic, and it is not a degenerate case.
//
// `fixed` used to be a count of header ROWS — n*headerH, which only
// exceeds the slot in a terminal too short to run in. It is now a sum of
// TITLE WIDTHS, and a strip of panes with long titles in a narrow window
// is an ordinary size. Without the clamp the last pane is arranged past
// the slot's right edge and its header paints into the next slot's cells.
func TestAStripNarrowerThanItsHeadersStaysInsideItsSlot(t *testing.T) {
	h := &dockHost{dock: newDockModel()}
	panes := []*dockPane{
		newDockPane("a", "A LONG PANE TITLE", dockBottom, 4, false),
		newDockPane("b", "ANOTHER LONG ONE", dockBottom, 4, false),
	}
	for _, p := range panes {
		p.host = h
		h.dock.add(p)
		p.collapsed.Set(true)
	}

	const w = 12
	slot := gooey.Rect{X: 3, Y: 7, W: w, H: headerH}
	if panes[0].headerCols()+panes[1].headerCols() <= w {
		t.Fatalf("the two headers fit in %d columns, so this test cannot see an "+
			"overrun", w)
	}
	h.place(dockBottom, slot, false, true)

	for _, p := range panes {
		b := p.Bounds()
		if b.X < slot.X || b.X+b.W > slot.X+slot.W {
			t.Errorf("pane %q was arranged at %+v, outside the slot %+v. Its header "+
				"paints into cells the strip does not own, and nothing clips at the "+
				"slot boundary — the neighbour repaints over it whenever it happens "+
				"to repaint, and not before", p.Title, b, slot)
		}
	}
}

// TestAnEmptyStripIsNotAStripOfNoHeaders is the boundary allCollapsed
// has to get right, and it is the one place "all of them are collapsed"
// and "there are none" answer differently.
//
// slotExtent's promise is that an empty slot is ZERO — "which is what
// makes a slot that everything was dragged out of disappear rather than
// leave a blank stripe". Vacuous truth would make allCollapsed say yes to
// a slot with no panes, laidOutExtent hand back headerH, and the strip
// keep a one-row stripe across the bottom of the editor for as long as
// nothing is docked there.
func TestAnEmptyStripIsNotAStripOfNoHeaders(t *testing.T) {
	ed, c := dockFixture(t)
	panel := pane(t, ed, "panel")

	// Collapse it FIRST, so the pane leaves the slot in the state that
	// makes the vacuous answer look right on the way out.
	ed.dock.ToggleCollapsed(panel)
	settle(t, c)
	ed.dock.Move(panel, dockCenter)
	settle(t, c)

	if got := ed.dock.laidOutExtent(dockBottom); got != 0 {
		t.Errorf("the empty bottom strip lays out at %d rows, want 0. A slot "+
			"everything was dragged out of has to disappear rather than leave a "+
			"blank stripe, and `all of them are collapsed` is vacuously true of no "+
			"panes at all", got)
	}
	if ed.dock.allCollapsed(dockBottom) {
		t.Error("allCollapsed says an empty slot is fully collapsed")
	}
}

// TestTheUsableMinimumFallsWhenTheStripCollapses is the second half of
// #441: dockModel.Minimum reads slotExtent, so checkFit reports the same
// usable minimum whether the strip is collapsed or not.
//
// The consequence is the one the fit check exists to prevent: in a short
// terminal the user performs the gesture documented as reclaiming room,
// the rows genuinely come free, and the cram screen stays up.
func TestTheUsableMinimumFallsWhenTheStripCollapses(t *testing.T) {
	ed, c := dockFixture(t)
	panel := pane(t, ed, "panel")

	before := ed.dock.Minimum()
	if before.Rows <= 0 || before.Cols <= 0 {
		t.Fatalf("the shipped dock's minimum is %s; nothing below discriminates",
			before)
	}

	ed.dock.ToggleCollapsed(panel)
	settle(t, c)
	after := ed.dock.Minimum()

	if after.Rows >= before.Rows {
		t.Errorf("the usable minimum is %s collapsed and %s open — the bottom "+
			"strip gave up %d rows and the fit check cannot see them. Minimum "+
			"reads slotExtent, which is collapse-blind by design; the number it "+
			"needs is laidOutExtent", after, before,
			ed.dock.slotExtent(dockBottom)-ed.dock.laidOutExtent(dockBottom))
	}
	// AND IT DID NOT WIDEN. The columns are not expected to fall — the
	// strip's column term is a body allowance per pane and says nothing
	// about collapse, for the reason Minimum records — but a "reclaim"
	// that raised the minimum on the other axis would be worse than the
	// blindness it replaced. The first attempt at this fix did exactly
	// that, charging a collapsed pane its header's 8 columns where an
	// open one is charged starMin's 3.
	if after.Cols > before.Cols {
		t.Errorf("collapsing WIDENED the usable minimum, %s to %s", before, after)
	}
}

// TestTheBottomStripsRowFloorDoesNotMultiplyByItsPaneCount is a defect
// found while fixing the two above, and it is the same axis confusion one
// function over.
//
// Minimum's own doc comment says "a slot stacking n panes needs
// n*(headerH+starMin)" — true for the three slots that stack vertically,
// and false for the bottom strip, which stacks HORIZONTALLY and whose
// panes therefore share every row. The n belongs in the column term,
// where it already is. The shipped page docks exactly one pane in Bottom,
// so n==1 and the whole suite agreed with the wrong rule.
//
// A BARE MODEL, not the shipped page, and that is the whole reason this
// test can see anything. Moving a pane out of Bottom on the real dock
// changes an upper slot's requirement at the same time, and the two
// terms move together — the first version of this test compared 8 rows
// against 12 and the difference was mostly the centre slot gaining a
// pane. Two docks differing in exactly one bottom pane is the only
// arrangement where the row term is on its own.
func TestTheBottomStripsRowFloorDoesNotMultiplyByItsPaneCount(t *testing.T) {
	one := newDockModel()
	one.add(newDockPane("a", "A", dockBottom, headerH+starMin, false))

	two := newDockModel()
	two.add(newDockPane("a", "A", dockBottom, headerH+starMin, false))
	two.add(newDockPane("b", "B", dockBottom, headerH+starMin, false))

	got, want := two.Minimum(), one.Minimum()
	if got.Rows != want.Rows {
		t.Errorf("the bottom strip needs %d rows with two panes and %d with one. "+
			"They stack left to right and SHARE every row, so the pane count "+
			"belongs in the column term and not this one — on this arithmetic a "+
			"third bottom pane reports a minimum three header-plus-body strips "+
			"tall", got.Rows, want.Rows)
	}
	if got.Cols <= want.Cols {
		t.Errorf("two bottom panes need %d columns and one needs %d; the pane "+
			"count has to reach the axis they actually stack on", got.Cols, want.Cols)
	}
}
