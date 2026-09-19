package preview

import (
	"slices"
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/render"
)

// EVERY WIDTH IN THIS PACKAGE IS A COLUMN COUNT TOO, and that is what
// this file is for. columns_test.go one directory up makes the same
// check for the four helpers in package main; the sweep it records
// stopped at this package's boundary, and fit and drawText were the two
// it left — a fifth "fit into N cells" helper and the module's only
// per-rune write loop. Raised in review of #524.
//
// IT IS AN ABSOLUTE CLAIM AND IT HAD ONE UNRECORDED EXCEPTION.
// mirror.go sized its centred label with len() of a byte string, twice
// — once as a fit test and once as a centering offset — and it was
// correct only because the constant is ASCII in Go source, which is the
// same argument this PR already had to retract about surfaceSize. It
// measures with render.StringWidth now, so the claim needs no
// exception rather than carrying one. Raised in review of #524.
//
// WHAT IS NOT CLAIMED HERE IS END-TO-END REACHABILITY. A track spec is
// authored text, but a spec holding a wide glyph never reaches these
// two: components.ParseGridLens rejects it first, the build fails, and
// buildGuide answers nil, so there is no guide to draw. Measured with
// Cols="世世世,8" through the shipped designer:
//
//	status:  ✗ markup: <Grid Cols="世世世,8">: grid: bad length "世世世"
//	guide:   nil
//
// That makes these unit pins rather than fixture ones, and it is the
// reason to have them at all: the helpers are safe only because of a
// property of a DIFFERENT function, in a different module, which no
// reader of this file can see and no future spec grammar has to keep.
const (
	// wideCell is one rune and two columns.
	wideCell = "世"
	// wideWord is two runes and four columns.
	wideWord = "世界"
)

// TestFitAnswersInColumns is the rule fit exists for: a spelling that
// does not fit its track must come back shortened, and "fits" is a
// question about cells.
//
// fit was statusaddr.go's pre-sweep ellipsize body character for
// character, so fit(wideWord+wideCell, 4) — six columns — answered "it
// fits" and returned all of it, and the track's neighbour was painted
// over by a guide whose whole contract is never to overwrite anything.
func TestFitAnswersInColumns(t *testing.T) {
	for _, tc := range []struct {
		in string
		w  int
	}{
		{wideWord + wideCell, 4},
		{wideWord, 3},
		{strings.Repeat(wideCell, 5), 5},
		{"1*", 4},
	} {
		got := fit(tc.in, tc.w)
		if n := render.StringWidth(got); n > tc.w {
			t.Errorf("fit(%q, %d) = %q, %d columns, want at most %d. %q is %d runes "+
				"and %d columns, which is why a rune count lets it through",
				tc.in, tc.w, got, n, tc.w, tc.in,
				len([]rune(tc.in)), render.StringWidth(tc.in))
		}
	}
}

// TestAGutterLabelIsWrittenByCluster is the write half, and it is a
// different defect from the measure half above: drawText walked []rune
// and passed x+i as a column, so even a label that FITS was laid down
// one cell per rune.
//
// THE READBACK IS render.RowText, not a per-cell .Rune loop, for the
// reason CLAUDE.md gives about those helpers: a loop that renders the
// continuation marker as a literal rune cannot hold a wide glyph and be
// asserted on, which is how ~35 rune-counting sites stayed green.
func TestAGutterLabelIsWrittenByCluster(t *testing.T) {
	f := &gooey.Frame{Cells: render.NewBuffer(10, 1)}
	o := &Overlay{}
	o.drawText(f, 0, 0, wideWord+"x", render.Style{}, 10)

	if got, want := render.RowText(f.Cells, 0), wideWord+"x     "; got != want {
		t.Errorf("the gutter row reads %q, want %q: a rune index used as a column "+
			"puts the second glyph on the cell the first already covers",
			got, want)
	}
	// THE MODEL, NOT ONLY THE TEXT. A row that reads back correctly can
	// still hold a lead with no continuation beside it, which healSeam
	// blanks one frame later — the defect Buffer.Set in a per-rune loop
	// produces and the one a text comparison alone is blind to.
	if got := f.Cells.At(1, 0).Rune; got != render.Continuation {
		t.Errorf("column 1 holds %q, want the continuation of the wide glyph in "+
			"column 0; a lead without its tail is the broken pair healSeam "+
			"removes", got)
	}
}

// TestTheOverlayTakesBackAWideMark is the half the cluster write could
// have broken quietly. A guide that cannot lift its own marks leaves
// them on screen for the rest of the session — the symptom mark's own
// doc records — and restoreMarks only lifts a mark whose cell STILL
// HOLDS THE GLYPH THE OVERLAY PUT THERE.
//
// WHOLE CELLS, NOT RUNES, and that correction is the finding. This
// collected .Rune only, and the restore put the LEAD back while leaving
// healSeam's repair of the orphaned continuation carrying the style that
// cell held — the OVERLAY's. cursorStyle has a background, so what
// survived a lifted mark was a highlighted blank cell, on a clean node
// that will not repaint. The rune comparison could not see it, and
// setCluster's own doc cited this test for a claim it was true of less
// than. Both halves now: every cell the mark covers is recorded and put
// back, and this compares render.Cell values.
//
// NOT render.RowText either, for the reason that stood before: an
// unwritten cell and a space both read back as a space, so a text
// comparison cannot see a lead-and-tail pair replaced by two blanks —
// which is the outcome under test. Raised in review of #524.
func TestTheOverlayTakesBackAWideMark(t *testing.T) {
	f := &gooey.Frame{Cells: render.NewBuffer(10, 1)}
	row := func() []render.Cell {
		out := make([]render.Cell, 10)
		for x := range out {
			out[x] = f.Cells.At(x, 0)
		}
		return out
	}
	runes := func(cs []render.Cell) string {
		out := make([]rune, len(cs))
		for i, c := range cs {
			out[i] = c.Rune
		}
		return string(out)
	}
	before := row()

	o := &Overlay{}
	// A STYLE WITH A BACKGROUND, because that is what makes the
	// style half observable at all: restoring a cell to the wrong
	// style is invisible when the wrong style is the zero one.
	o.drawText(f, 0, 0, wideWord, render.Style{Bg: render.RGB(190, 180, 90)}, 10)
	if slices.Equal(row(), before) {
		t.Fatal("nothing was drawn, so the restore below would pass vacuously")
	}
	o.restoreMarks(f)

	// THE DIFFERING COLUMNS, not the whole row: a ten-cell %+v is four
	// screens of struct and the reader has to diff it by eye to find the
	// one column that moved.
	got := row()
	for x := range got {
		if got[x] == before[x] {
			continue
		}
		t.Errorf("after restoring, column %d holds %+v and held %+v. Row is now "+
			"%q against %q — either the overlay cannot take back what it wrote, "+
			"or it put the glyph back and left its own style on a column",
			x, got[x], before[x], runes(got), runes(before))
	}
}

// TestAMarkAtTheClipEdgeClaimsOnlyTheColumnItGot is the case where the
// caller's intended width and the buffer's answer come apart.
//
// setCluster asks for two columns; render.Buffer.SetCell refuses to lay
// a render.Continuation outside the clip and downgrades the write to a
// SPACE, which is one column. A mark built from the CALLER's width
// records two, and restoreMarks writes back every column a mark names.
//
// THE TWO BUFFER CALLS ARE NOT SCOPED ALIKE, which is the whole of why
// that matters. render.Buffer.At is bounded on the buffer; SetCell is
// bounded on the CLIP. So the prev[1] snapshot reaches a column the
// overlay could not have written and does not own, while the write back
// to it is dropped for as long as the clip still excludes it.
//
// THE CLIP IS NOT A CONSTANT, and the second half of this test is what
// turns a latent disagreement into a lost cell. Composer.build takes a
// clip from the component's BOUNDS every frame and Unclip widens back
// out at the end of it, so an overlay that grows — a wider pane, a
// changed grid extent — finds the column it once could not reach inside
// its clip on the next frame. Unclip-then-Clip below is that frame
// boundary, not test scaffolding: Clip INTERSECTS, so widening is only
// reachable through Unclip. 'Z' is what the document subtree composed
// into that column this time round, and the overlay paints after it.
//
// Raised in review of #524.
func TestAMarkAtTheClipEdgeClaimsOnlyTheColumnItGot(t *testing.T) {
	f := &gooey.Frame{Cells: render.NewBuffer(10, 1)}
	full := f.Cells.Clip(render.Rect{X: 0, Y: 0, W: 3, H: 1})

	o := &Overlay{}
	o.setCluster(f, 2, 0, wideCell, 2, render.Style{Bg: render.RGB(190, 180, 90)})
	if len(o.marks) != 1 {
		t.Fatalf("setCluster left %d marks, want 1 — the rest of this test "+
			"would pass vacuously", len(o.marks))
	}
	if got, want := o.marks[0].cols, f.Cells.At(2, 0).Width(); got != want {
		t.Errorf("the mark claims %d columns and the cell it wrote is %d wide. "+
			"SetCell downgraded the cluster to a space at the clip edge, so the "+
			"caller's w is not the width that landed", got, want)
	}

	// The next frame: the overlay's bounds grew, and the column it could
	// not reach before now holds content it did not write.
	f.Cells.Unclip(full)
	f.Cells.Clip(render.Rect{X: 0, Y: 0, W: 10, H: 1})
	f.Cells.SetCell(3, 0, render.Cell{Rune: 'Z'})
	o.restoreMarks(f)
	if got := f.Cells.At(3, 0).Rune; got != 'Z' {
		t.Errorf("restoring the mark put %q into column 3, which the overlay "+
			"never wrote — the clip it painted under stopped at column 3, so "+
			"prev[1] is a snapshot of somebody else's cell. A mark may only "+
			"name the columns that landed", got)
	}
}

// TestAWideMarkIsNotHalfLiftedWhenTheClipNARROWS is the other direction
// of the test above, and the file argued only one of them.
//
// setCluster reasons about the clip WIDENING — a column it could not
// reach at write time holding somebody else's content later. This is
// the clip NARROWING between the write and the lift, which is the
// overlay's bounds shrinking at a frame boundary, and a partial lift
// there is worse than none: Buffer.SetCell is clip-scoped, so prev[0]
// lands, healSeam repairs the now-orphaned continuation WITH THE STYLE
// THAT CELL HOLDS — the overlay's — and the follow-up SetCell of
// prev[1] is dropped. The result was a blank carrying the guide's
// background on a cell the guide no longer owns.
//
// ASSERTED ON Style, for the third time in this file and the same
// reason: the rune is a space either way. Raised in review of #524.
func TestAWideMarkIsNotHalfLiftedWhenTheClipNARROWS(t *testing.T) {
	f := &gooey.Frame{Cells: render.NewBuffer(10, 1)}
	pre := render.Style{Bg: render.RGB(1, 2, 3)}
	f.Cells.SetCell(0, 0, render.Cell{Rune: ' ', Style: pre})
	f.Cells.SetCell(1, 0, render.Cell{Rune: ' ', Style: pre})

	wide := f.Cells.Clip(render.Rect{X: 0, Y: 0, W: 10, H: 1})
	o := &Overlay{}
	o.setCluster(f, 0, 0, wideCell, 2, render.Style{Bg: render.RGB(9, 9, 9)})
	if len(o.marks) != 1 || o.marks[0].cols != 2 {
		t.Fatalf("the fixture is not a two-column mark (%d marks), so the "+
			"half-lift this test is about cannot arise", len(o.marks))
	}

	snap := o.marks[0].prev

	// The next frame: the overlay's bounds shrank to one column.
	f.Cells.Unclip(wide)
	f.Cells.Clip(render.Rect{X: 0, Y: 0, W: 1, H: 1})
	o.restoreMarks(f)

	// ALL OR NOTHING IS THE CLAIM, not a particular cell value, because
	// the skipped case leaves the overlay's own glyph in both columns —
	// which is in the overlay's style, exactly like the smear. What
	// separates the two worlds is whether the pair AGREES: a lead put
	// back while its tail could not be is the half-lift.
	if f.Cells.At(0, 0) == snap[0] && f.Cells.At(1, 0) != snap[1] {
		t.Errorf("the lead was restored to %+v and column 1 came back %+v "+
			"instead of %+v: the continuation is outside the clip, so "+
			"healSeam repaired it with the style that cell held — the "+
			"guide's — and the write that would have put the snapshot back "+
			"was dropped. A mark may only be lifted where every column of "+
			"it can be reached", snap[0], f.Cells.At(1, 0), snap[1])
	}
}

// TestAZeroWidthClusterDoesNotOverrunTheBudget is the disagreement
// between the two halves of this pair, measured.
//
// fit budgets with render.StringWidth, which charges a standalone
// combining mark, a tab and a NUL zero columns. drawText advances
// max(w, 1) for each, and that advance is right: advancing 0 leaves the
// next cluster landing where setCluster has already written, where it
// finds the cell non-blank, refuses, and truncates the rest of the
// label. So the walk has to stop at the budget rather than trust that
// the two counts agree.
//
// Measured: render.EachCluster("\u0301ab") yields w=0, 1, 1 while
// render.StringWidth answers 2 — so fit approves a two-column spec and
// the walk would write three cells. The overshoot is one column, into a
// blank cell inside the overlay's own bounds, which is why nothing
// looked wrong; it is still the rune-versus-column shape this pair was
// rewritten to retire. Raised in review of #524.
func TestAZeroWidthClusterDoesNotOverrunTheBudget(t *testing.T) {
	const s = "\u0301ab" // a LEADING combining mark is its own cluster
	if got := render.StringWidth(s); got != 2 {
		t.Fatalf("the fixture measures %d columns, not the 2 this test is "+
			"about — the disagreement it exercises is gone or moved", got)
	}

	f := &gooey.Frame{Cells: render.NewBuffer(10, 1)}
	o := &Overlay{}
	o.drawText(f, 0, 0, s, render.Style{}, render.StringWidth(s))

	if got := f.Cells.At(2, 0).Rune; got != 0 && got != ' ' {
		t.Errorf("column 2 holds %q after a two-column budget: the walk "+
			"charged the zero-width cluster one column and wrote past what "+
			"fit approved", got)
	}
}

// TestAFullyClippedWriteLeavesNoMark is the other half of the clip
// rule, and the arm the test above could not be: there, the LEAD column
// landed and only the tail was clipped away. Here nothing lands.
//
// Buffer.At is buffer-scoped and Buffer.SetCell is clip-scoped, so a
// write outside the clip reads back unchanged. A mark taken then names
// a cell this overlay never wrote, and a later frame with a wider clip
// puts that snapshot back over whatever the real owner has painted
// since. blank() does not catch it: a leaf pre-clear writes a STYLED
// SPACE, so prev[0].Rune is blank and the guard lets the write through.
//
// The assertion is on the STYLE, not the rune, because that is what
// survives: both cells hold a space, and only the background says who
// owns the cell. Raised in review of #524, which measured the overlay
// reverting a neighbour's background on a cell it never touched.
func TestAFullyClippedWriteLeavesNoMark(t *testing.T) {
	f := &gooey.Frame{Cells: render.NewBuffer(10, 1)}
	full := f.Cells.Clip(render.Rect{X: 0, Y: 0, W: 3, H: 1})

	o := &Overlay{}
	o.setCluster(f, 5, 0, " ", 1, render.Style{Bg: render.RGB(190, 180, 90)})
	if len(o.marks) != 0 {
		t.Fatalf("setCluster left %d marks for a write the clip dropped "+
			"whole: column 5 is outside the clip, so nothing landed and "+
			"there is nothing to put back. mark=%+v", len(o.marks), o.marks[0])
	}

	// The next frame: the overlay's bounds grew, and the neighbour who
	// does own that cell has painted it.
	f.Cells.Unclip(full)
	f.Cells.Clip(render.Rect{X: 0, Y: 0, W: 10, H: 1})
	owner := render.Style{Bg: render.RGB(9, 9, 9)}
	f.Cells.SetCell(5, 0, render.Cell{Rune: ' ', Style: owner})
	o.restoreMarks(f)
	if got := f.Cells.At(5, 0).Style; got != owner {
		t.Errorf("restoring put %+v into column 5, where the owner had "+
			"painted %+v. The overlay never wrote that cell — the clip "+
			"stopped at column 3 — so its snapshot is somebody else's", got, owner)
	}
}

// TestAForeignRepaintOfTheSameRuneKeepsItsCell is the OWNERSHIP half of
// restoreMarks, and the whole suite was green without it.
//
// The overlay draws box-drawing corners where a child Border's corner
// falls and ASCII in the gutters, so "the document repainted this cell
// with the same rune" is the ordinary case rather than a coincidence.
// Keyed on the rune alone, the mark was lifted and the overlay wrote
// its snapshot over content the document had just painted — on a node
// that is now clean, so nothing brought it back.
//
// ASSERTED ON Style, NOT ON Rune, for the reason
// TestAFullyClippedWriteLeavesNoMark already gives: the rune is
// identical in both the passing and the failing world, so a rune
// assertion agrees with the bug. Raised in review of #524.
func TestAForeignRepaintOfTheSameRuneKeepsItsCell(t *testing.T) {
	f := &gooey.Frame{Cells: render.NewBuffer(10, 1)}
	o := &Overlay{}
	o.setCluster(f, 0, 0, "┌", 1, render.Style{Fg: render.RGB(190, 180, 90)})
	if len(o.marks) != 1 {
		t.Fatalf("setCluster took %d marks for a write into a blank cell, "+
			"want 1 — this test is about LIFTING one", len(o.marks))
	}

	// The next frame: the document's own Border painted its corner
	// there, same glyph, its own style. The overlay paints after the
	// tree, so this is the state restoreMarks sees.
	owner := render.Style{Bg: render.RGB(4, 5, 6)}
	f.Cells.SetCell(0, 0, render.Cell{Rune: '┌', Style: owner})
	o.restoreMarks(f)
	if got := f.Cells.At(0, 0).Style; got != owner {
		t.Errorf("restoring put %+v into column 0, where the document had "+
			"just painted %+v with the same rune. A rune match is not "+
			"ownership: the overlay's snapshot is of a blank cell that "+
			"stopped existing a frame ago, and the node that painted "+
			"over it is clean, so it does not come back", got, owner)
	}
}

// TestSetClusterAllocatesNothing is the paint-path pin: the guide writes
// one of these per cell it draws, every frame it paints.
//
// It took `make([]render.Cell, cols)` for a cols that is 1 or 2 for
// everything this component draws. render.ClipCols one module down keeps
// its own loop for exactly this property and says so; this is the same
// claim one level up.
//
// THE MUTATION HAS TO USE A VARIABLE SIZE, which is worth recording
// because the obvious one does not fire: `make([]render.Cell, 2)` is a
// constant size the compiler can prove and keeps on the stack, so
// putting a two-element slice back here measures 0 allocations and this
// test stays green. It is `cols` being unprovable that puts the slice on
// the heap — measured, the variable-size form allocates 1 per run.
// Raised in review of #524.
//
// The fixture is wideCell, and the spelling it replaces is worth
// recording because it type-checked and measured the wrong thing:
// `wideWord[:len([]rune(wideWord))]` slices a STRING by a RUNE COUNT,
// so it took the first 2 BYTES of a 6-byte constant. That is half of
// 世 — invalid UTF-8, decoding to two U+FFFD — which is a narrow
// two-rune cluster, not the one-rune two-column glyph the w=2 beside it
// claims. Measured: `valid=false runes=[65533 65533]`. This is the
// column-count trap of CLAUDE.md's "every width is a COLUMN count"
// wearing the other hat — a rune count used where a byte offset goes.
// Raised in review of #524.
func TestSetClusterAllocatesNothing(t *testing.T) {
	f := &gooey.Frame{Cells: render.NewBuffer(10, 1)}
	o := &Overlay{}
	got := testing.AllocsPerRun(200, func() {
		o.setCluster(f, 0, 0, wideCell, 2, render.Style{})
		o.marks = o.marks[:0]
		f.Cells.SetCell(0, 0, render.Cell{Rune: ' '})
		f.Cells.SetCell(1, 0, render.Cell{Rune: ' '})
	})
	if got != 0 {
		t.Errorf("setCluster and the two resets around it allocate %v per run, "+
			"want 0 — the per-glyph slice is back on the paint path", got)
	}
}
