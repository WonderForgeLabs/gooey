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
	// TWO TITLES OF THE SAME RUNE COUNT AND DIFFERENT COLUMN COUNTS is
	// what makes the wide row an assertion rather than a restatement:
	// "世界" and "ab" are both two runes, and the answers differ by two.
	// A fixture that agreed with itself under either rule is the trap
	// CLAUDE.md names, and the ASCII row alone was one.
	for _, tc := range []struct {
		title string
		want  int // 1 chevron + 1 space + the title's COLUMNS + 1 pin
	}{
		{"PANEL", 8},
		{"ab", 5},
		{"世界", 7},
	} {
		p := newDockPane("x", tc.title, dockBottom, 4, false)
		if got := p.headerCols(); got != tc.want {
			t.Errorf("headerCols() for a %q header is %d, want %d = 1 chevron + 1 "+
				"space + %d columns of title + 1 pin. A collapsed pane laid out at "+
				"this width draws exactly its header and nothing is clipped",
				tc.title, got, tc.want, render.StringWidth(tc.title))
		}
	}

	p := newDockPane("x", "PANEL", dockBottom, 4, false)
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
// laid out at SEVEN columns — chevron 1, space 1, 世界 4, pin 1, which
// is headerCols' own arithmetic — and a rune-counted pad then adds two
// spaces too many, the clip cuts what it must, and the pin, being the
// last thing on the line, is what falls off. Both halves have to be
// columns or the two disagree at exactly the width the first half
// chose.
//
// The number said 6 for one review round, which is the rune count of
// the lead plus the pin: the wrong rule, written into the prose
// explaining why the wrong rule is wrong. Corrected in review of #480 —
// and it is a comment rather than an assertion, which is why nothing
// went red for it. TestAHeadersColumnBudgetIsItsTextPlusThePin asserts
// the 7 directly.
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
// is an ordinary size.
//
// WHAT HOLDS THE INVARIANT IS THE TRIM, not the `left` clamp this test
// was written for, and review of #480 measured the difference: after the
// widest-first trim the shares sum to exactly the slot width, so `left`
// never bites and this arm was green either way. The arm is kept and
// widened rather than deleted — it asserts the CONSEQUENCE (no pane
// outside the slot), which is what a reader wants pinned however it is
// achieved — and TestTheSharesSumToTheSlotAtEveryWidth below is the mechanism, so a
// change that breaks the trim names the trim.
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

// TestTheSharesSumToTheSlotAtEveryWidth is the mechanism finding 2 named,
// pinned as a mechanism.
//
// The `left` clamp reads as the thing keeping the panes inside the slot,
// and it is unreachable: the widest-first trim leaves
// fixed <= max(0, total-flex), so the per-pane shares sum to EXACTLY the
// slot width and `n > left` cannot hold. A review swept w = 0..60 over
// this fixture and the sum never exceeded w; this is that sweep, so the
// property is asserted rather than remembered.
//
// It is a SUM, not a containment check. The arm above already asserts
// that no pane escapes the slot, and it passes whether the trim or the
// clamp got it there — which is exactly why it could not see the clamp
// going dead. A total that falls short means somebody's columns went
// nowhere; a total that overshoots means the clamp is doing real work
// and the trim has a hole.
func TestTheSharesSumToTheSlotAtEveryWidth(t *testing.T) {
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
	wide := panes[0].headerCols() + panes[1].headerCols()
	if wide > 60 {
		t.Fatalf("the two headers need %d columns and the sweep stops at 60, so it "+
			"never reaches the width where they fit", wide)
	}
	matched := 0
	for w := 0; w <= 60; w++ {
		h.place(dockBottom, gooey.Rect{X: 3, Y: 7, W: w, H: headerH}, false, true)
		sum := 0
		for _, p := range panes {
			sum += p.Bounds().W
		}
		if sum > w {
			t.Errorf("at slot width %d the shares sum to %d. The clamp then has to "+
				"take the overflow off whichever pane is arranged last, which is a "+
				"pane silently narrower than the trim decided", w, sum)
		}
		if sum == w {
			matched++
		}
	}
	// NON-VACUITY: a place() that arranged nothing would satisfy the
	// loop above at every width.
	if matched == 0 {
		t.Errorf("the shares never summed to the slot width at any of 61 widths, " +
			"so the loop above passed on panes that were not laid out at all")
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
	// AND THE COLUMNS STILL COVER WHAT place NEEDS, which is the
	// invariant, and "it did not widen" is not.
	//
	// That arm stood here and was VACUOUS on this fixture: the shipped
	// page's rails dominate the column term, so a strip that widened by
	// 8 could not move the total. It was also FALSE as a claim — the
	// column term IS collapse-aware, deliberately, because a collapsed
	// header is drawn at its full title width and cannot be squeezed.
	// Minimum answers "below this the shell is not usable", so it may
	// not report less than the layout requires; that it rises on the
	// column axis while the gesture reclaims rows is a consequence of
	// the header being incompressible. Raised in review of #480.
	//
	// The check is the CONSEQUENCE rather than the arithmetic: lay the
	// dock out at exactly the width it reports, and the collapsed pane's
	// header must not be clipped.
	slot := dockSlot(panel.slot.Get())
	panel.host.place(slot, gooey.Rect{W: after.Cols,
		H: ed.dock.laidOutExtent(slot)}, false, true)
	if got, want := panel.Bounds().W, panel.headerCols(); got < want {
		t.Errorf("at the reported minimum of %d columns the collapsed pane is %d "+
			"wide and its header needs %d. Minimum reports less than place "+
			"requires, so checkFit says a window is big enough for a header it "+
			"then clips", after.Cols, got, want)
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

// stripOf is a bare host with a collapsed pane and an open one in the
// bottom strip, which is the arrangement all three findings of #480's
// review live in. Bare rather than the shipped page for the reason
// TestTheBottomStripsRowFloorDoesNotMultiplyByItsPaneCount gives: on the
// real dock the edge slots move at the same time and the term under test
// is never on its own.
//
// The collapsed pane is DECLARED FIRST and its title is WIDE. Both are
// load-bearing: document order is what the starvation finding is about,
// and a header narrower than starMin cannot outrun anything.
func stripOf(t *testing.T) (*dockHost, *dockPane, *dockPane) {
	t.Helper()
	h := &dockHost{dock: newDockModel()}
	shut := newDockPane("a", "世界世界", dockBottom, 4, false)
	open := newDockPane("b", "B", dockBottom, 4, false)
	for _, p := range []*dockPane{shut, open} {
		p.host = h
		h.dock.add(p)
	}
	shut.collapsed.Set(true)
	if shut.headerCols() <= starMin {
		t.Fatalf("the collapsed header is %d columns and starMin is %d, so it "+
			"cannot outweigh an open pane's share and nothing below discriminates",
			shut.headerCols(), starMin)
	}
	return h, shut, open
}

// TestTheUsableMinimumCoversACollapsedHeaderBesideAnOpenPane is finding 1
// of review #480, and the assertion is the CONSEQUENCE rather than the
// arithmetic: lay the strip out at exactly the width Minimum reports and
// the open pane must still be usable.
//
// Minimum charged len(strip)*starMin — one body allowance per pane,
// collapsed or not — while place charges a collapsed pane the full width
// of the header it draws. So the reported minimum was below the width
// place needs, and inside that gap the open pane is arranged at W=0.
// checkFit's whole job is to say "this window is too small", and it said
// the window was fine.
func TestTheUsableMinimumCoversACollapsedHeaderBesideAnOpenPane(t *testing.T) {
	h, shut, open := stripOf(t)

	m := h.dock.Minimum()
	// NON-VACUITY, against the formula this replaces. If the old count
	// happened to be large enough the test would pass on the bug.
	if old := 2 * starMin; m.Cols <= old {
		t.Fatalf("Minimum reports %d columns and the count-based formula gave %d; "+
			"the two agree here, so this test cannot see the difference",
			m.Cols, old)
	}

	h.place(dockBottom, gooey.Rect{W: m.Cols, H: headerH + starMin}, false, true)
	if got := open.Bounds().W; got < starMin {
		t.Errorf("at the reported minimum of %d columns the open pane is %d wide, "+
			"want at least starMin=%d. The collapsed pane beside it takes %d for "+
			"its header, so a minimum that charges both panes starMin is short by "+
			"%d — and the fit check reports the window is big enough",
			m.Cols, got, starMin, shut.headerCols(),
			shut.headerCols()-starMin)
	}
}

// TestACollapsedPaneCannotStarveAnOpenOne is finding 2, and it is about
// a width BELOW the minimum — which is an ordinary window, since Minimum
// is a usable minimum and not a floor the layout enforces.
//
// The clamp in place walks in document order, so with the collapsed
// panes over budget the ones declared FIRST take their full header and
// whatever is declared after gets nothing. Nothing, for an open pane, is
// a rect of zero extent. The pane that gave up its body is the one that
// should go short.
func TestACollapsedPaneCannotStarveAnOpenOne(t *testing.T) {
	h, shut, open := stripOf(t)

	// Narrower than the collapsed header alone, so the trim must engage.
	w := shut.headerCols() - 2
	slot := gooey.Rect{X: 3, Y: 7, W: w, H: headerH + starMin}
	h.place(dockBottom, slot, false, true)

	if got := open.Bounds().W; got < 1 {
		t.Errorf("in a %d-column strip the open pane is %d wide: the collapsed "+
			"pane declared before it took %d for its header and left nothing. A "+
			"pane of zero extent is not a narrow pane, it is an absent one — and "+
			"collapse is the gesture that gives room BACK",
			w, got, shut.Bounds().W)
	}
	// AND THE SLOT STILL HOLDS THEM. The trim must not be paid for by
	// running past the edge, which is the failure the clamp exists for.
	for _, p := range []*dockPane{shut, open} {
		b := p.Bounds()
		if b.X < slot.X || b.X+b.W > slot.X+slot.W {
			t.Errorf("pane %q was arranged at %+v, outside the slot %+v",
				p.Title, b, slot)
		}
	}
}

// TestTheUsableMinimumFallsWhenAnEdgeSlotCollapses is finding 3, and it
// is the row half of the same mistake: Minimum wrote out place's
// arithmetic a second time, from the pane COUNT, which cannot see a pane.
//
// n*(headerH+starMin) charges every pane a body allowance whether or not
// it has a body, so collapsing panes in the left, right or centre slot
// lowered nothing. That is verbatim the failure Minimum's own doc
// comment describes — the user performs the operation documented as
// reclaiming room, the rows come free, and the cram screen stays up.
func TestTheUsableMinimumFallsWhenAnEdgeSlotCollapses(t *testing.T) {
	m := newDockModel()
	first := newDockPane("a", "A", dockLeft, headerH+starMin, false)
	second := newDockPane("b", "B", dockLeft, headerH+starMin, false)
	m.add(first)
	m.add(second)

	before := m.Minimum()
	first.collapsed.Set(true)
	after := m.Minimum()

	// EXACTLY starMin, not merely "less". A collapsed pane keeps its
	// header row and gives up its body, which is starMin — an assertion
	// of "it fell" would pass for a fix that dropped the header too, and
	// then the pane could not be re-opened because its chevron is the
	// hit target.
	if got := before.Rows - after.Rows; got != starMin {
		t.Errorf("collapsing one of two panes in the LEFT slot moved the usable "+
			"minimum by %d rows (%s to %s), want %d — the body allowance it gave "+
			"up. The row term counts panes rather than asking each one, so the "+
			"rows come free and the fit check cannot see them",
			got, before, after, starMin)
	}
}

// TestANarrowedHeaderLeavesNoStaleGlyph is finding 4 of review #480, and
// it needs two paints into one buffer because a stale cell is by
// definition what the SECOND paint did not write.
//
// render.ClipCols stops BEFORE a glyph that would overrun, so where the
// pane's last column would hold half a wide glyph it comes back a column
// short. A dockPane is a chrome-only container — its bounds enclose
// children whose own clean nodes will not repaint — so the framework
// pre-clears nothing for it, and that column keeps whatever was in it.
func TestANarrowedHeaderLeavesNoStaleGlyph(t *testing.T) {
	h := &dockHost{dock: newDockModel()}
	p := newDockPane("a", "ABC", dockBottom, 4, false)
	p.host = h
	h.dock.add(p)

	buf := render.NewBuffer(10, 2)
	f := &gooey.Frame{Cells: buf}
	// THREE COLUMNS, which is the width at which the two titles clip to
	// different lengths: "> A" is exactly 3, and "> 世" would be 4, so
	// the wide one stops at 2 and leaves column 2 unwritten.
	p.Base.Arrange(gooey.Rect{X: 0, Y: 0, W: 3, H: headerH})
	p.Render(f)
	stale := buf.At(2, 0).Rune
	if stale == ' ' || stale == 0 {
		t.Fatalf("the ASCII header left column 2 as %q, so there is nothing for "+
			"the second paint to fail to overwrite", stale)
	}

	p.Title = "世界"
	if render.StringWidth(render.ClipCols("> "+p.Title, 3)) != 2 {
		t.Fatal("the wide title no longer clips a column short of the pane, so " +
			"this test cannot see the cell the clip leaves behind")
	}
	p.Render(f)

	if got := buf.At(2, 0).Rune; got == stale {
		t.Errorf("column 2 still holds %q from the previous title. ClipCols "+
			"returned 2 columns for a 3-column pane, and a chrome-only container "+
			"pre-clears nothing — so the old glyph sits on the header row under "+
			"the new one until something else happens to repaint that cell", got)
	}
}

// TestTheTrimComesOffTheWidestHeader is the other half of finding 2, and
// it needs the narrow pane declared FIRST — otherwise "off the widest"
// and "off the first" pick the same pane and agree.
//
// Both rules keep an open pane alive, so the starvation test above
// passes either way. What separates them is what the collapsed panes
// look like afterwards: taking the shortfall in document order empties
// the narrow pane completely while its wide neighbour keeps ten of its
// eleven columns, and a collapsed pane at zero has no chevron — which is
// the hit target that re-opens it. Taking it off the widest each pass
// leaves every header as readable as the room allows.
func TestTheTrimComesOffTheWidestHeader(t *testing.T) {
	h := &dockHost{dock: newDockModel()}
	narrow := newDockPane("a", "B", dockBottom, 4, false)
	wide := newDockPane("b", "\u4e16\u754c\u4e16\u754c", dockBottom, 4, false)
	for _, p := range []*dockPane{narrow, wide} {
		p.host = h
		h.dock.add(p)
		p.collapsed.Set(true)
	}
	if narrow.headerCols() >= wide.headerCols() {
		t.Fatalf("the two headers are %d and %d columns; with the first no "+
			"narrower than the second, document order and widest-first choose "+
			"the same pane", narrow.headerCols(), wide.headerCols())
	}

	// Over budget by enough that the wide pane alone can absorb it.
	total := narrow.headerCols() + wide.headerCols() - 5
	h.place(dockBottom, gooey.Rect{W: total, H: headerH}, false, true)

	if got := narrow.Bounds().W; got != narrow.headerCols() {
		t.Errorf("the narrow pane was cut to %d of its %d columns while the wide "+
			"one kept %d of %d. The shortfall is being taken in declaration "+
			"order, so the pane that could least afford it paid — and a collapsed "+
			"pane with no chevron cannot be re-opened",
			got, narrow.headerCols(), wide.Bounds().W, wide.headerCols())
	}
	if got := narrow.Bounds().W + wide.Bounds().W; got != total {
		t.Errorf("the two panes occupy %d columns of the %d-column slot", got, total)
	}
}

// TestTheTrimNeverZeroesAHeaderItCanAfford is the finding that turned
// out not to be one, kept as an assertion because nothing else in the
// suite made it.
//
// Review of #480 reported that the trim needed a floor of one column,
// on the ground that a collapsed pane walked to zero has no chevron and
// the chevron is the only way to re-open it. The premise is sound and
// the conclusion is not: taking from the widest each pass ALREADY
// implies the floor, because a pane can only be the strict maximum on
// its way to zero when every other pane is already there — which is
// exactly the case where the budget could not have given one column to
// each anyway.
//
// So the floor is a consequence, and a consequence nobody was holding.
// This sweeps the same space the equivalence check did — every pane
// count to four, extents to eight, budgets to nineteen — and asserts
// it directly, so a future trim that IS fair-by-halves or proportional
// cannot quietly drop it.
func TestTheTrimNeverZeroesAHeaderItCanAfford(t *testing.T) {
	var walk func(ext []int, k int)
	cases := 0
	walk = func(ext []int, k int) {
		if k == 0 {
			n := 0
			for _, e := range ext {
				if e > 0 {
					n++
				}
			}
			for budget := 0; budget < 20; budget++ {
				got := append([]int(nil), ext...)
				trimHeaders(got, budget)
				cases++
				sum := 0
				for i, e := range got {
					sum += e
					if e > ext[i] {
						t.Fatalf("trimHeaders(%v, %d) = %v: pane %d GREW", ext, budget, got, i)
					}
				}
				if sum > budget && sumOf(ext) > budget {
					t.Fatalf("trimHeaders(%v, %d) = %v sums to %d, over budget",
						ext, budget, got, sum)
				}
				if budget < n {
					continue
				}
				for i, e := range got {
					if ext[i] > 0 && e == 0 {
						t.Fatalf("trimHeaders(%v, %d) = %v: pane %d lost its chevron "+
							"in a budget with room for %d of them. A collapsed pane at "+
							"zero columns cannot be re-opened, because the chevron is "+
							"the hit target", ext, budget, got, i, n)
					}
				}
			}
			return
		}
		for e := 0; e <= 8; e++ {
			walk(append(ext, e), k-1)
		}
	}
	for k := 1; k <= 4; k++ {
		walk(nil, k)
	}
	// A FLOOR ON THE SWEEP ITSELF, because a walk that generated nothing
	// would pass every assertion above.
	if cases < 100000 {
		t.Fatalf("the sweep ran %d cases, which is fewer than the space it "+
			"claims to cover", cases)
	}
}

func sumOf(ns []int) int {
	n := 0
	for _, v := range ns {
		n += v
	}
	return n
}

// TestTheStripRowPastACollapsedHeaderIsBlank is finding 4, and it is
// about a row that now belongs to nobody.
//
// An all-collapsed one-pane strip used to span the full width, so the
// whole row carried the header style. It is headerCols wide now and the
// remainder is owned by no component — which is the shape a stale glyph
// survives in, because no node's Render covers those cells to overwrite
// them. The visibility sweep does clear them today; nothing said so, and
// nothing would go red if it stopped.
//
// It reads through render.RowText rather than the package's own
// rune-per-cell helper, for CLAUDE.md's reason: a helper that renders
// the continuation marker as a literal rune cannot hold a wide glyph.
func TestTheStripRowPastACollapsedHeaderIsBlank(t *testing.T) {
	ed, c := dockFixture(t)
	panel := pane(t, ed, "panel")
	ed.dock.Move(panel, dockBottom)
	// A WIDE GLYPH IN THE HEADER. CLAUDE.md's rule about column counts
	// is that an ASCII fixture agrees with itself under either the rune
	// rule or the column rule and so passes against the bug; this row
	// now holds a title whose rune count and column count differ, which
	// is the only state in which the read below is saying anything.
	// Raised in review of #480.
	panel.Title = "\u4e16panel"
	settle(t, c)

	// A BODY WORTH LOSING. The row is only interesting if the pane
	// spanned it before the collapse, so the arm can tell "cleared" from
	// "never painted".
	b := panel.Bounds()
	if b.W <= panel.headerCols() {
		t.Fatalf("the open pane is %d columns and its header needs %d, so no part "+
			"of the row is vacated by collapsing and this arm checks nothing",
			b.W, panel.headerCols())
	}
	f, _ := c.Frame()
	if b.Y < 0 || b.Y >= f.Cells.H {
		t.Fatalf("the pane is at row %d, outside the %d-row frame", b.Y, f.Cells.H)
	}
	if body := render.RowText(f.Cells, b.Y+1); strings.TrimSpace(body) == "" {
		t.Fatalf("the open pane's body row %d is already blank, so a blank row "+
			"after the collapse says nothing", b.Y+1)
	}

	ed.dock.ToggleCollapsed(panel)
	settle(t, c)
	f, _ = c.Frame()

	got := panel.Bounds()
	if got.W != panel.headerCols() {
		t.Fatalf("the collapsed pane is %d columns and its header is %d; the "+
			"remainder this arm is about is not where it thinks",
			got.W, panel.headerCols())
	}
	// READ BY CELL, NOT BY RUNE. `past` is a COLUMN, and
	// render.RowText's result is indexable by rune — the two agree only
	// while every glyph in the row is one column wide, which is true of
	// this fixture and of no promise anybody made about it. A title with
	// a CJK character or an emoji in it would slice at the wrong place
	// and the assertion would still pass, for the wrong row. Raised in
	// review of #480.
	if row := render.RowText(f.Cells, got.Y); len([]rune(row)) == f.Cells.W {
		t.Fatalf("the strip row is %d runes across %d columns, so a rune index "+
			"and a column index agree and this arm cannot tell them apart. The "+
			"wide title above is what is supposed to separate them",
			len([]rune(row)), f.Cells.W)
	}
	past := got.X + got.W
	if past >= f.Cells.W {
		t.Fatalf("the header reaches column %d of a %d-column row, so there is "+
			"nothing to its right to assert about", past, f.Cells.W)
	}
	var tail strings.Builder
	for x := past; x < f.Cells.W; x++ {
		tail.WriteString(f.Cells.At(x, got.Y).Text())
	}
	if rest := strings.TrimRight(tail.String(), " "); rest != "" {
		t.Errorf("the strip row past the collapsed header at column %d reads %q, "+
			"want blank. Those cells belong to no component now, so nothing "+
			"repaints them — a leftover there is the pane's old body, still on "+
			"screen after the gesture that removed it", past, rest)
	}
}
