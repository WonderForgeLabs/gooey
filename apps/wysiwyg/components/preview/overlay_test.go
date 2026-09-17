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
	o.drawText(f, 0, 0, wideWord+"x", render.Style{})

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
// ONE CELL RESTORED, TWO CELLS BACK, which is the claim setCluster's
// single mark rests on: healSeam repairs a broken pair on any write, so
// blanking the LEAD takes its continuation with it. A second mark on
// the continuation cell would be dead weight, and this is what says so
// rather than the comment there.
//
// RUNES, NOT render.RowText. An unwritten cell and a space both read
// back as a space, so a text comparison cannot see a lead-and-tail pair
// replaced by two blanks — which is the outcome under test. Raised in
// review of #524.
func TestTheOverlayTakesBackAWideMark(t *testing.T) {
	f := &gooey.Frame{Cells: render.NewBuffer(10, 1)}
	row := func() []rune {
		out := make([]rune, 10)
		for x := range out {
			out[x] = f.Cells.At(x, 0).Rune
		}
		return out
	}
	before := row()

	o := &Overlay{}
	o.drawText(f, 0, 0, wideWord, render.Style{})
	if slices.Equal(row(), before) {
		t.Fatal("nothing was drawn, so the restore below would pass vacuously")
	}
	o.restoreMarks(f)

	if got := row(); !slices.Equal(got, before) {
		t.Errorf("after restoring, the row holds %q, want %q — the overlay cannot "+
			"take back what it wrote, so the mark is on screen for good",
			string(got), string(before))
	}
}
