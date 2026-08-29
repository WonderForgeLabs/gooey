package main

import (
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/apps/wysiwyg/components/preview"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/input"
)

// Every verb goes in through Composer.Handle, exactly as the terminal
// delivers it, rather than by calling the editor method — because a
// KeyBinding on the wrong element never fires, and a direct call passes
// just as well for a page that has no binding at all. That is the same
// reason pressEsc exists.
func pressRune(c *gooey.Composer, r rune) bool {
	return c.Handle(input.KeyOf(input.KeyEvent{Key: input.KeyRune, Rune: r}))
}

// gridPage is the shipped designer with a <Grid> as the user's root,
// with tracks and one child, laid out once.
func gridPage(t *testing.T) (*editor, *gooey.Composer, *int) {
	t.Helper()

	ed, c, frames := designerPageCounting(t)
	ed.doc().Elem = "Grid"
	ed.doc().Attrs = map[string]string{"Rows": "2,2,2", "Cols": "8,8"}
	ed.doc().Kids = []*node{
		{Elem: "Text", Body: "aa", Attrs: map[string]string{"Name": "A", "Grid.Row": "0", "Grid.Col": "0"}},
	}
	ed.rebuild()
	if !strings.HasPrefix(ed.status.Get(), "✓") {
		t.Fatalf("the grid fixture does not build: %s", ed.status.Get())
	}
	ed.setSelection(ed.doc())
	c.Frame()
	return ed, c, frames
}

// TestTheGridIsDrawn is the whole point of the feature: before this a
// <Grid> rendered as nothing at all, so you could not see what you were
// laying out.
//
// It asserts CELLS ON SCREEN at the probed rectangles, not merely that
// the overlay has a guide — an overlay that computed everything and drew
// nothing would satisfy any structural check.
func TestTheGridIsDrawn(t *testing.T) {
	ed, c, _ := gridPage(t)

	g := ed.buildGuide()
	if g == nil {
		t.Fatal("no guide was built for a selected <Grid>")
	}
	if len(g.Cells) != 3 || len(g.Cells[0]) != 2 {
		t.Fatalf("the probe found a %dx%d grid, want 3x2", len(g.Cells), len(g.Cells[0]))
	}

	f, _ := c.Frame()
	painted := 0
	for r := range g.Cells {
		for col := range g.Cells[r] {
			q := g.Cells[r][col]
			if ch := f.Cells.At(q.X, q.Y).Rune; ch == '┌' || ch == '▟' {
				painted++
			}
		}
	}
	// Five of six, not six: the child sits in cell (0,0) and a guide
	// never overwrites content, so that corner keeps its "a". The other
	// five cells are empty and get their mark.
	if painted != 5 {
		t.Errorf("%d of the 6 cell corners are marked on screen, want 5 (cell 0,0 is "+
			"occupied by the child). A grid that draws nothing is the defect this "+
			"overlay exists to fix", painted)
	}
}

// TestTheTrackSpecIsShownAgainstTheSpaceItProduces. The gutter is
// STRUCTURE, not decoration: "1*" and "Auto" are the values being
// edited, and showing them anywhere but beside the track they size makes
// the reader hold the mapping in their head.
func TestTheTrackSpecIsShownAgainstTheSpaceItProduces(t *testing.T) {
	ed, c, _ := gridPage(t)
	// An EMPTY grid, which is both the case the feature most serves and
	// the only one where every gutter position is free. A guide never
	// overwrites content, so a child sitting in cell (0,0) legitimately
	// hides that column's spec — asserting against a populated grid
	// would be asserting the guide beats the document, which is the
	// opposite of the rule.
	ed.doc().Kids = nil
	ed.doc().Attrs["Cols"] = "6,8"
	ed.rebuild()
	ed.setSelection(ed.doc())
	f, _ := c.Frame()

	g := ed.buildGuide()
	if g == nil {
		t.Fatal("no guide for the selected grid")
	}
	if len(g.Cols) != 2 || g.Cols[0] != "6" || g.Cols[1] != "8" {
		t.Fatalf("the guide's column specs are %v, want [6 8]", g.Cols)
	}
	// A column's spec sits on the grid's first row, one cell in from
	// that column's corner mark — INSIDE the grid, because a grid has no
	// margin and writing outside it lands on the editor's own chrome.
	for c := range g.Cols {
		q := g.Cells[0][c]
		got := readCells(f, q.X+1, q.Y, len([]rune(g.Cols[c])))
		if got != g.Cols[c] {
			t.Errorf("column %d is %q but the gutter at its top edge reads %q — the number "+
				"you edit is not shown against the space it produces", c, g.Cols[c], got)
		}
	}
	// A row's spec sits one row below its corner, on the first column.
	for r := range g.Rows {
		q := g.Cells[r][0]
		if q.H < 2 {
			continue
		}
		got := readCells(f, q.X, q.Y+1, len([]rune(g.Rows[r])))
		if got != g.Rows[r] {
			t.Errorf("row %d is %q but its gutter reads %q", r, g.Rows[r], got)
		}
	}
}

func readCells(f *gooey.Frame, x, y, n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteRune(f.Cells.At(x+i, y).Rune)
	}
	return b.String()
}

// TestTheKeyboardResizesATrack drives the SHIPPED bindings.
//
// Keyboard rather than the divider handle, and not as a courtesy: mouse
// reports cannot be injected through a recording pty, so a resize that
// existed only as a drag could not be verified in a capture at all.
func TestTheKeyboardResizesATrack(t *testing.T) {
	ed, c, _ := gridPage(t)

	if !pressRune(c, ']') {
		t.Fatal("] was not consumed; the NextTrack binding is not on the page")
	}
	c.Frame()
	if !ed.cursor.on || ed.cursor.axis != preview.AxisCol || ed.cursor.index != 0 {
		t.Fatalf("] left the cursor at %+v, want column 0", ed.cursor)
	}

	if !pressRune(c, '=') {
		t.Fatal("= was not consumed; the GrowTrack binding is not on the page")
	}
	c.Frame()
	if got := ed.doc().Attrs["Cols"]; got != "9,8" {
		t.Errorf("after ] = the columns are %q, want \"9,8\"", got)
	}

	if !pressRune(c, '-') {
		t.Fatal("- was not consumed")
	}
	c.Frame()
	if got := ed.doc().Attrs["Cols"]; got != "8,8" {
		t.Errorf("after - the columns are %q, want \"8,8\"", got)
	}
}

// TestTheKeyboardAddsAndRemovesATrack.
func TestTheKeyboardAddsAndRemovesATrack(t *testing.T) {
	ed, c, _ := gridPage(t)

	pressRune(c, ']') // column 0
	c.Frame()
	if !pressRune(c, 'a') {
		t.Fatal("a was not consumed; the AddTrack binding is not on the page")
	}
	c.Frame()
	if got := ed.doc().Attrs["Cols"]; got != "8,1*,8" {
		t.Errorf("after a the columns are %q, want \"8,1*,8\"", got)
	}
	// The cursor follows the track it just made — it is what you are
	// about to size.
	if ed.cursor.index != 1 {
		t.Errorf("after adding, the cursor is on track %d, want 1", ed.cursor.index)
	}

	if !pressRune(c, 'r') {
		t.Fatal("r was not consumed; the RemoveTrack binding is not on the page")
	}
	c.Frame()
	if got := ed.doc().Attrs["Cols"]; got != "8,8" {
		t.Errorf("after r the columns are %q, want \"8,8\"", got)
	}
}

// TestTheLastTrackCannotBeRemovedByTheGridVerbs. components.Grid treats no declared
// tracks as ONE implicit star track, so "zero tracks" is not a state the
// layout has — an editor that wrote Cols="" would show a grid with one
// column while claiming it had none.
func TestTheLastTrackCannotBeRemovedByTheGridVerbs(t *testing.T) {
	ed, c, _ := gridPage(t)
	ed.doc().Attrs["Cols"] = "8"
	ed.rebuild()
	c.Frame()

	pressRune(c, ']')
	c.Frame()
	pressRune(c, 'r')
	c.Frame()

	if got := ed.doc().Attrs["Cols"]; got != "8" {
		t.Errorf("removing the only column left Cols=%q", got)
	}
	if !strings.Contains(ed.dragHint.Get(), "at least one track") {
		t.Errorf("the refusal was silent; the hint says %q", ed.dragHint.Get())
	}
}

// TestCyclingATrackKindReachesEveryMode. star -> Auto -> fixed -> star
// is the edit that has no numeric form, so it cannot be spelled with -/=.
func TestCyclingATrackKindReachesEveryMode(t *testing.T) {
	ed, c, _ := gridPage(t)
	ed.doc().Attrs["Cols"] = "1*,8"
	ed.rebuild()
	c.Frame()
	pressRune(c, ']')
	c.Frame()

	var seen []string
	for i := 0; i < 3; i++ {
		if !pressRune(c, 'g') {
			t.Fatal("g was not consumed; the CycleTrack binding is not on the page")
		}
		c.Frame()
		seen = append(seen, strings.Split(ed.doc().Attrs["Cols"], ",")[0])
	}
	if seen[0] != "Auto" {
		t.Errorf("1* cycled to %q, want Auto", seen[0])
	}
	if seen[1] == "Auto" || strings.HasSuffix(seen[1], "*") {
		t.Errorf("Auto cycled to %q, want a fixed size", seen[1])
	}
	if seen[2] != "1*" {
		t.Errorf("a fixed track cycled to %q, want 1*", seen[2])
	}
}

// TestTheCursorWalksBothAxesAndOff. One pair of keys covers columns and
// rows and dismissing, so the keymap does not need three more bindings.
func TestTheCursorWalksBothAxesAndOff(t *testing.T) {
	ed, c, _ := gridPage(t)
	// 2 columns, 3 rows, then off = 6 steps back to the start.
	var axes []string
	for i := 0; i < 6; i++ {
		pressRune(c, ']')
		c.Frame()
		switch {
		case !ed.cursor.on:
			axes = append(axes, "off")
		case ed.cursor.axis == preview.AxisCol:
			axes = append(axes, "col")
		default:
			axes = append(axes, "row")
		}
	}
	want := []string{"col", "col", "row", "row", "row", "off"}
	for i := range want {
		if axes[i] != want[i] {
			t.Fatalf("] walked %v, want %v", axes, want)
		}
	}
}

// TestMovingTheTrackCursorAsksForAFrame is the Layout.Left/Top hazard in
// a different costume: the cursor is a plain Go field, so moving it
// changes what the overlay draws and schedules NOTHING on its own. In a
// test the frame is driven by hand, so without counting the request the
// assertion would pass for code that never asked.
func TestMovingTheTrackCursorAsksForAFrame(t *testing.T) {
	_, c, frames := gridPage(t)
	before := *frames
	pressRune(c, ']')
	if *frames == before {
		t.Error("moving the track cursor asked for no frame, so nothing would repaint " +
			"until an unrelated event happened to schedule one")
	}
}

// TestAnEmptyGridStillDrawsItsCells is the case the feature is most
// needed for and the one the probe cannot do unaided: gridCells walks a
// COMPONENT through every cell, and an empty grid has none to walk.
func TestAnEmptyGridStillDrawsItsCells(t *testing.T) {
	ed, c, _ := gridPage(t)
	ed.doc().Kids = nil
	ed.rebuild()
	ed.setSelection(ed.doc())
	c.Frame()

	g := ed.buildGuide()
	if g == nil {
		t.Fatal("an empty grid produced no guide, so a grid with nothing in it is still " +
			"invisible — which is the grid you most need to see")
	}
	if len(g.Cells) != 3 || len(g.Cells[0]) != 2 {
		t.Errorf("the probe found a %dx%d grid, want 3x2", len(g.Cells), len(g.Cells[0]))
	}
}

// TestTheProbeLeavesTheTreeAsItFoundIt. The probe mutates a live
// component's Layout and re-runs the real Grid.Arrange; a probe that
// leaves the tree wrong is one every later reader has to reason about.
func TestTheProbeLeavesTheTreeAsItFoundIt(t *testing.T) {
	ed, c, _ := gridPage(t)
	kid := docKid(ed, 0)
	// The fields the probe actually writes. gooey.Layout as a whole is
	// not comparable — it carries a func — so these are named rather
	// than compared wholesale.
	type geom struct {
		row, col, rowSpan, colSpan int
		hAlign, vAlign             gooey.Align
		w, h                       int
		margin                     gooey.Thickness
	}
	read := func() geom {
		l := gooey.LayoutOf(kid)
		return geom{l.Row, l.Col, l.RowSpan, l.ColSpan, l.HAlign, l.VAlign, l.Width, l.Height, l.Margin}
	}
	before := read()
	beforeBounds := kid.(interface{ Bounds() gooey.Rect }).Bounds()

	ed.buildGuide()
	c.Frame()

	if got := read(); got != before {
		t.Errorf("the probe left the child's layout as %+v, want %+v", got, before)
	}
	if got := kid.(interface{ Bounds() gooey.Rect }).Bounds(); got != beforeBounds {
		t.Errorf("the probe left the child arranged at %+v, want %+v", got, beforeBounds)
	}
}

// gridComponent is the built *components.Grid for the user's root.
func gridComponent(t *testing.T, ed *editor) *components.Grid {
	t.Helper()
	g, ok := ed.componentFor(ed.doc()).(*components.Grid)
	if !ok {
		t.Fatalf("the document root built a %T, not a *components.Grid", ed.componentFor(ed.doc()))
	}
	return g
}

// TestTheEmptyGridProbeRemovesItsScratchChild. The scratch component is
// appended to a LIVE grid's children for the duration of the probe; one
// left behind would be an extra child of the user's document that the
// next mapNodes walk would try to pair with a markup node and fail.
func TestTheEmptyGridProbeRemovesItsScratchChild(t *testing.T) {
	ed, c, _ := gridPage(t)
	ed.doc().Kids = nil
	ed.rebuild()
	ed.setSelection(ed.doc())
	c.Frame()

	g := gridComponent(t, ed)
	for i := 0; i < 3; i++ {
		ed.buildGuide()
		if n := len(g.Children); n != 0 {
			t.Fatalf("after probe %d the empty grid has %d children; the scratch subject "+
				"was not removed", i+1, n)
		}
	}
}

// TestTheOverlayIsDesignTimeOnly. Guides are an editing artifact; in
// LIVE mode the preview IS the app and must look like the app.
func TestTheOverlayIsDesignTimeOnly(t *testing.T) {
	ed, c, _ := gridPage(t)
	if ed.buildGuide() == nil {
		t.Fatal("no guide in DESIGN mode, so this test would pass vacuously")
	}
	pressD(c)
	c.Frame()
	if ed.design.Get() {
		t.Fatal("d did not leave DESIGN mode")
	}
	if g := ed.buildGuide(); g != nil {
		t.Errorf("the layout guide is still drawn in LIVE mode: %+v", g.Bounds)
	}
}

// TestTheOverlayCostsNothingWhenThereIsNoGridInScope is the damage pin
// that matters most, and the reason the overlay owns a revision of its
// own instead of subscribing to the editor's.
//
// The overlay spans the preview and sits ABOVE the whole document in
// z-order. Wired the obvious way — subscribing to "the document or the
// selection changed" — it would repaint on every click anywhere in the
// app, forever, drawing nothing. A bounds assertion would never notice.
// This counts.
func TestTheOverlayCostsNothingWhenThereIsNoGridInScope(t *testing.T) {
	ed, c, _ := designerPageCounting(t)
	// The shipped document is a <Canvas>: no cell structure anywhere, so
	// the overlay has nothing to draw and must cost nothing to say so.
	ed.rebuild()
	c.Frame()

	kids := ed.doc().Kids
	if len(kids) < 2 {
		t.Skipf("the shipped document has %d children; this test needs two to move "+
			"the selection between", len(kids))
	}
	ed.setSelection(kids[0])
	c.Frame()

	ed.setSelection(kids[1])
	_, painted := c.Frame()

	overlayPainted := false
	for _, r := range c.Damage() {
		if r.W == 0 && r.H == 0 {
			overlayPainted = true
		}
	}
	if overlayPainted {
		t.Errorf("moving the selection in a document with no grid repainted the layout "+
			"overlay (%d components, damage %v). It draws nothing here, so it must "+
			"cost nothing: the guide is unchanged, so its revision must not tick",
			painted, c.Damage())
	}
}

// TestChangingATrackMovesTheProbedCells. The converse pin: the overlay
// must not be so frugal that it stops updating.
//
// RENAMED from ...RepaintsTheOverlayExactlyOnce, which promised a damage
// count this body never takes — it discards both of Frame()'s returns and
// never reads Damage(). CLAUDE.md is explicit that a geometry assertion
// "passes just as well when the entire tree repainted", so the old name
// claimed the one thing the test could not see. Caught in review.
//
// The name was not fixed by ADDING the count, because "exactly once" is
// not true here and the test below says why: the composer's z-order pass
// force-repaints everything above a rect that just painted, and the
// overlay sits above the whole grid, so a track edit repaints it for free
// whether or not it is subscribed. The repaint claim is pinned by
// TestMovingTheTrackCursorRedrawsTheHighlight, whose gesture changes
// nothing beneath the overlay; this one pins the model.
func TestChangingATrackMovesTheProbedCells(t *testing.T) {
	ed, c, _ := gridPage(t)
	pressRune(c, ']')
	c.Frame()

	before := ed.buildGuide()
	if before == nil {
		t.Fatal("no guide for the selected grid")
	}
	pressRune(c, '=')
	c.Frame()

	after := ed.buildGuide()
	if after == nil {
		t.Fatal("the guide vanished after a resize")
	}
	if sameCellGeometry(before, after) {
		t.Error("growing a column changed none of the probed cell rectangles, so the " +
			"overlay would draw the same picture as before the edit")
	}
}

// TestMovingTheTrackCursorRedrawsTheHighlight is the test that catches
// the overlay going DEAF, and getting it to discriminate took two
// attempts worth recording.
//
// The obvious version — edit a track, check the screen followed — passes
// even with the subscription deleted, because a track edit rebuilds the
// document and the composer's z-order pass FORCE-repaints everything
// sitting above a rect that just painted. The overlay is above the whole
// grid, so it repaints for free on any edit beneath it, subscribed or
// not.
//
// So the discriminating gesture is the one that changes NOTHING BELOW:
// moving the track cursor repaints no document component at all, and the
// only thing that can bring the overlay back is its own dependency.
// Asserting on the highlight's STYLE rather than its glyph is what makes
// the cursor visible to a cell-plane test — the text is the same either
// way; the background is not.
func TestMovingTheTrackCursorRedrawsTheHighlight(t *testing.T) {
	ed, c, _ := gridPage(t)
	ed.doc().Kids = nil
	ed.rebuild()
	ed.setSelection(ed.doc())
	c.Frame()

	g := ed.buildGuide()
	if g == nil || len(g.Cols) < 2 {
		t.Fatal("need a grid with at least two columns")
	}
	// A column's spec is drawn one cell in from its corner.
	specAt := func(col int) (int, int) {
		q := g.Cells[0][col]
		return q.X + 1, q.Y
	}

	pressRune(c, ']') // cursor on column 0
	f, _ := c.Frame()
	x0, y0 := specAt(0)
	if !f.Cells.At(x0, y0).Style.Bg.Set {
		t.Fatalf("after ] the column-0 spec at (%d,%d) has no highlight background, so "+
			"this test cannot see the cursor at all", x0, y0)
	}

	pressRune(c, ']') // cursor on column 1
	f, _ = c.Frame()

	if f.Cells.At(x0, y0).Style.Bg.Set {
		t.Errorf("after moving the cursor to column 1, column 0's spec at (%d,%d) is "+
			"STILL highlighted. Moving the cursor repaints nothing in the document, so "+
			"the overlay only redraws if its own revision is a dependency of its Render "+
			"— drop that Get and the guide freezes with no error anywhere", x0, y0)
	}
	x1, y1 := specAt(1)
	if !f.Cells.At(x1, y1).Style.Bg.Set {
		t.Errorf("column 1's spec at (%d,%d) is not highlighted after the cursor moved "+
			"onto it", x1, y1)
	}
}

// TestTheDrawnGridFollowsATrackEdit is the coarser companion: the guide
// tracks the layout it describes. It does NOT discriminate the missing
// subscription (see above) and is kept for what it does pin — that the
// drawn cells follow a real edit rather than lagging a frame.
func TestTheDrawnGridFollowsATrackEdit(t *testing.T) {
	ed, c, _ := gridPage(t)
	ed.doc().Kids = nil // an empty grid: every corner is free to draw
	ed.rebuild()
	ed.setSelection(ed.doc())
	c.Frame()

	before := ed.buildGuide()
	if before == nil {
		t.Fatal("no guide for the selected grid")
	}
	oldX := before.Cells[0][1].X

	pressRune(c, ']') // column 0
	c.Frame()
	pressRune(c, '=') // grow it
	f, _ := c.Frame()

	after := ed.buildGuide()
	newX := after.Cells[0][1].X
	if newX == oldX {
		t.Fatalf("growing column 0 did not move column 1 (still at x=%d), so this test "+
			"cannot tell a repaint from a stale frame", oldX)
	}
	if got := f.Cells.At(newX, after.Cells[0][1].Y).Rune; got != '┌' {
		t.Errorf("after growing column 0, the cell at its new corner (%d,%d) reads %q, "+
			"want '┌'. The overlay's model moved but the screen did not: its paint node "+
			"is not subscribed to anything, so it never repainted",
			newX, after.Cells[0][1].Y, got)
	}
	if got := f.Cells.At(oldX, after.Cells[0][1].Y).Rune; got == '┌' {
		t.Errorf("the corner mark is still drawn at the OLD position (%d,%d): the "+
			"previous frame's guide was never erased", oldX, after.Cells[0][1].Y)
	}
}

// TestRestoringAMarkNeverClobbersNewContent pins the "still holds the
// glyph the overlay wrote" condition in restoreMarks, which nothing else
// exercises: delete it and restore unconditionally, and every other test
// in this package still passes.
//
// The condition exists because save/restore and z-order interact. The
// overlay saves what a cell held before it marked it and lifts the mark
// on the next paint — but the DOCUMENT paints first (the overlay is
// Pane's later sibling), so by the time restoreMarks runs, a cell marked
// last frame may already have been repainted this frame with real
// content. Writing the saved copy back would put a blank over the
// element that just arrived, and the victim cell is then CLEAN and will
// not repaint: the stale-cell class, permanent until something unrelated
// happens to paint over it.
//
// So: mark a corner while the grid is empty, then move an element into
// exactly that cell.
func TestRestoringAMarkNeverClobbersNewContent(t *testing.T) {
	ed, c, _ := gridPage(t)
	ed.doc().Kids = nil
	ed.rebuild()
	ed.setSelection(ed.doc())
	c.Frame()

	g := ed.buildGuide()
	if g == nil {
		t.Fatal("no guide for the selected grid")
	}
	q := g.Cells[0][0]

	f, _ := c.Frame()
	if got := f.Cells.At(q.X, q.Y).Rune; got != '┌' {
		t.Fatalf("cell (0,0)'s corner at (%d,%d) reads %q, not '┌' — the overlay never "+
			"marked it, so there is no mark to restore and this test is vacuous",
			q.X, q.Y, got)
	}

	// An element arrives in that exact cell. The tracks are fixed sizes,
	// so the geometry does not move underneath us.
	ed.doc().Kids = []*node{
		{Elem: "Text", Body: "Z", Attrs: map[string]string{"Name": "Z", "Grid.Row": "0", "Grid.Col": "0"}},
	}
	ed.rebuild()
	f, _ = c.Frame()

	if got := f.Cells.At(q.X, q.Y).Rune; got != 'Z' {
		t.Errorf("after an element moved into cell (0,0), the cell at (%d,%d) reads %q, "+
			"want 'Z'. The overlay restored what it had saved for that cell over content "+
			"the document painted this frame — a mark may only be lifted while the cell "+
			"still holds the glyph the overlay put there", q.X, q.Y, got)
	}
}

// sameCellGeometry is the tests' own comparison, deliberately NOT the
// overlay's sameGuide: a guard sharing the implementation it guards
// cannot catch that implementation being wrong.
func sameCellGeometry(a, b *preview.Guide) bool {
	if len(a.Cells) != len(b.Cells) {
		return false
	}
	for r := range a.Cells {
		if len(a.Cells[r]) != len(b.Cells[r]) {
			return false
		}
		for c := range a.Cells[r] {
			if a.Cells[r][c] != b.Cells[r][c] {
				return false
			}
		}
	}
	return true
}

// TestTheOverlayDoesNotEatClicks. It spans the grid and sits on top, so
// without HitTestTransparent the designer would select nothing,
// everywhere — the AdornmentLayer defect.
func TestTheOverlayDoesNotEatClicks(t *testing.T) {
	ed, c, _ := gridPage(t)
	kid := docKid(ed, 0)
	b := kid.(interface{ Bounds() gooey.Rect }).Bounds()
	if b.W == 0 {
		t.Fatal("the child was never arranged")
	}

	// The grid stays SELECTED, so the overlay is live and covering the
	// whole grid. Clearing the selection first would give the overlay
	// zero bounds and this would pass for an overlay that eats every
	// click — which is exactly what it did until this line was fixed.
	c.Frame()
	overlay := ed.pv.Overlay()
	if ob := overlay.Bounds(); ob.W == 0 || ob.H == 0 {
		t.Fatalf("the overlay is %+v, so it covers nothing and could not eat a click "+
			"even if it wanted to — this test would be vacuous", ob)
	} else if b.X < ob.X || b.X >= ob.X+ob.W || b.Y < ob.Y || b.Y >= ob.Y+ob.H {
		t.Fatalf("the press point %d,%d is outside the overlay %+v", b.X, b.Y, ob)
	}

	press(c, b.X, b.Y)
	if ed.sel == nil {
		t.Fatal("a press inside the grid selected nothing: the overlay is eating clicks")
	}
	if ed.sel.Elem != "Text" {
		t.Errorf("a press on the child selected <%s>, want <Text> — the overlay is "+
			"hit-testable and is intercepting the designer's own gesture", ed.sel.Elem)
	}
}

// TestTheOverlayNeverBlanksTheGrid. A component covering the previewed
// tree is exactly the thing that would erase it: the three-case
// pre-clear rule turns on whether the painter is a gooey.Container, and
// a LEAF fills its whole rect before painting.
func TestTheOverlayNeverBlanksTheGrid(t *testing.T) {
	ed, c, _ := gridPage(t)
	kid := docKid(ed, 0)
	b := kid.(interface{ Bounds() gooey.Rect }).Bounds()

	f, _ := c.Frame()
	if got := f.Cells.At(b.X, b.Y).Rune; got != 'a' {
		t.Errorf("the child's first cell reads %q, want 'a' — the overlay pre-cleared "+
			"over the previewed tree", got)
	}
}

// TestTheGuideProbeIsNotRepeatedOnAnIdleFrame pins the memo, and it needs
// its own counter because NO EFFECT-LEVEL INSTRUMENT CAN SEE THIS.
//
// A repeated probe produces identical cell rects, so the guide is
// unchanged, so nothing repaints — a damage count, a cell assertion and a
// FlushBytes diff are all blind to it by construction. Wasted work is
// effect-free; that is exactly why it survives review. The only witness
// is how often the measurement RAN.
//
// What it cost: probeUncached calls Grid.Arrange once per cell and each
// of those re-arranges every child, and Overlay.Arrange calls it from
// Arrange, which Composer.Frame runs unconditionally. So a selected grid
// paid rows×cols sub-tree layouts every frame, with no bound on the track
// count. Found in review of #390; the drag path it borrows from runs the
// same probe once per GESTURE and says so in its own comment.
func TestTheGuideProbeIsNotRepeatedOnAnIdleFrame(t *testing.T) {
	ed, c, _ := gridPage(t)

	// Baseline from a COLD memo. Clearing the cached cells as well as the
	// counters is what makes the next call a real measurement: the
	// fixture's own frame already warmed it, so resetting the counters
	// alone left the first assertion reading a cache hit and expecting a
	// probe. (That the fixture warms it at all is the mechanism working —
	// but a test has to start from a known state, not a lucky one.)
	ed.guideProbes, ed.guideHits = 0, 0
	ed.guideKey, ed.guideCells = "", nil
	if got := ed.buildGuide(); got == nil {
		t.Fatal("no guide for the selected grid; this test would pin nothing")
	}
	if ed.guideProbes != 1 {
		t.Fatalf("the first guide took %d probes, want exactly 1", ed.guideProbes)
	}

	// THE ASSERTION: nothing changed, so the next frames must reuse the
	// measurement rather than repeat it.
	for i := 0; i < 5; i++ {
		c.Frame()
		ed.buildGuide()
	}
	if ed.guideProbes != 1 {
		t.Errorf("five idle frames took %d probes, want the original 1: the "+
			"overlay is re-measuring a grid nothing touched, and no damage "+
			"count can see it because the rects come out identical",
			ed.guideProbes)
	}
	if ed.guideHits == 0 {
		t.Error("the memo recorded no hits, so the calls above may have been " +
			"skipped for some other reason — this test would pass with no " +
			"cache at all")
	}

	// The must-RE-PROBE arm. A memo that never invalidates is worse than
	// none: it pins a stale overlay over a grid that moved. Editing a
	// track changes the grid's track list, which is in the key.
	before := ed.guideProbes
	ed.doc().Attrs["Rows"] = "3,3,3"
	ed.rebuild()
	c.Frame()
	ed.buildGuide()
	if ed.guideProbes == before {
		t.Errorf("after a track edit the probe count is still %d: the guide "+
			"is cached against tracks that changed, so the overlay would be "+
			"drawn over the grid's old geometry", ed.guideProbes)
	}
}

// TestTheTrackLineSurvivesAnUnrelatedEdit separates the two kinds of
// thing the status bar's hint carries, which rebuild was treating as
// one.
//
// A refused drag is an EVENT: it happened, it is reported once, and the
// next edit rightly retires it — that is what the clear at the top of
// rebuild is for. The track line is a MODE: it says which track the
// cursor is on and what the keys do, and it is true for exactly as long
// as the cursor is on. Clearing it on the next rebuild leaves the
// gutter highlighting a track the bar no longer names, with the verbs
// that operate on it unlisted — the mode is still live and the only
// thing telling the user so is gone.
//
// The edit here is deliberately NOT a track verb. Those re-announce
// through sayTrack on their own way out, which is why the clear looked
// safe: every path anyone checked set the hint again after rebuilding.
// It is any OTHER edit that exposes it.
func TestTheTrackLineSurvivesAnUnrelatedEdit(t *testing.T) {
	ed, c, _ := gridPage(t)

	if !pressRune(c, ']') {
		t.Fatal("] was not consumed; the NextTrack binding is not on the page")
	}
	c.Frame()
	line := ed.dragHint.Get()
	if line == "" {
		t.Fatal("] left the status bar empty, so there is no mode line to lose")
	}

	// An ordinary document edit, through the labelled path any slice of
	// the editor uses.
	ed.applyEdit("add", func() {
		ed.doc().Kids = append(ed.doc().Kids, &node{
			Elem: "Text", Body: "bb",
			Attrs: map[string]string{"Name": "B", "Grid.Row": "1", "Grid.Col": "1"},
		})
	})
	c.Frame()

	if !ed.cursor.on {
		t.Fatal("the edit dismissed the track cursor; this test is about a " +
			"cursor that is still on")
	}
	if got := ed.dragHint.Get(); got != line {
		t.Errorf("after an unrelated edit the status bar reads %q, want the "+
			"track line %q — the cursor is still on column %d",
			got, line, ed.cursor.index)
	}
}
