package components

import (
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/render"
)

// Readback helpers: how a test in this package reads what was painted.
//
// THE CONTINUATION MARKER IS WHY THEY EXIST, and it is one fact rather
// than one per helper. A wide glyph occupies its first column and leaves
// render.Continuation — rune(-1), U+FFFD when stringified — in the
// second, so a rune-per-cell read writes that marker into the middle of
// any row holding one. Six packages' row helpers had exactly that bug at
// once ([#358]), which is why no fixture in this package could contain a
// wide glyph and be asserted on until [#516]. Everything below goes
// through render.SpanText, render.RowText or render.BufferText, which do
// not write it.
//
// THE WINDOW COMES FROM WHAT WAS PAINTED — for every reader below whose
// extent is the FRAME's rather than a caller's, which is what this
// paragraph is about. The shape that class replaces is an extent passed
// in as parameters, written down beside a composer that had already
// said the same numbers — and a read wider than the buffer is phantom
// blanks rather than an error (render.SpanText's own doc says so), so
// widening a composer in a later edit turns the tail of every read into
// blanks. Several call sites here compare two screens for equality,
// where blanks on both sides agree, and one is
// `!strings.Contains(got, "gone")`, which passes on blank input
// outright. Deriving the extent from the frame is what removes that
// class: none of these readers takes a width it was not given by the
// thing it is reading.
//
// "NEITHER OF THOSE TWO" is what that last clause said until review of
// #520, and the opening clause had been widened from "frameText and
// screen, which is the pair" in the same commit — so a paragraph whose
// point is that counts decay closed on a count, two sentences later,
// already wrong by one. "Each took its extent as parameters" was wrong
// in the other direction: frameRows is one of the readers the widened
// clause covers and arrived in 4f0b381 with the signature it has, never
// having taken an extent at all.
//
// rowText IS THE EXCEPTION, AND IT IS THE RULE ITSELF THAT MOVES. It
// exists to read a caller-chosen REGION, so its window cannot come from
// the frame; what it owes instead is that the region be a real one. A
// span is the caller's business — a menu's lead column, a picker's bar
// — and a WHOLE ROW is render.RowText, never rowText(f, 0, y, <the
// composer's width>), because a literal repeating a width declared
// somewhere above it is the same phantom-blanks decay one level down.
// The account of how this rule came to have counterexamples inside its
// own package is in the spec's readback section, which is where this
// branch decided such accounts live.
//
// ONE OF EACH, IN ONE FILE. These are package-scope names called from
// many test files, and each lived in the first file that happened to
// want it — under a doc block about dropdowns, or about a colour picker,
// or about the Composer — so the explanation was retold in each
// consumer's comments and the copies went stale independently. The
// readers below are distinguished by what they read rather than by who
// asked — a region of a row, a whole row of a buffer, a whole frame as
// text, a whole frame as rows, a whole composition — plus the one row
// search every positional assertion in this package needs.
//
// NO COUNT HERE, and that is the point rather than terseness: the
// sentence said FOUR and listed four, and the first reader added after
// it made both wrong at once with nothing to go red. The declarations
// are the inventory.
//
// [#358]: https://github.com/WonderForgeLabs/gooey/issues/358
// [#516]: https://github.com/WonderForgeLabs/gooey/issues/516

// rowText is w columns of row y, from column x, as a terminal would show
// them — render.SpanText's own signature, with its arguments passed
// straight through.
//
// SpanText's ORDER, not a convenient one. Every parameter is an int, so
// a wrapper taking y before w reads as x=0 to anyone who has just
// internalised SpanText(b, x, y, w), compiles either way, and returns
// blanks rather than failing.
func rowText(f *gooey.Frame, x, y, w int) string {
	return render.SpanText(f.Cells, x, y, w)
}

// row is the whole of row y of a BUFFER, trailing blanks trimmed.
//
// TRIMMED, AND THAT IS THE WHOLE DIFFERENCE FROM rowText. The two names
// are one letter apart in one package and agree about nothing else: this
// takes a *render.Buffer and reads the whole row; rowText takes a
// *gooey.Frame, reads a caller-chosen span, and pads rather than trims.
// A reader who has internalised one mis-reads the other, which is why
// this is stated here rather than at either call site.
func row(b *render.Buffer, y int) string {
	return strings.TrimRight(render.RowText(b, y), " ")
}

// frameText is the frame as a terminal would show it, one row per line.
//
// THE FOURTH COPY OF THIS LOOP IS WHAT IT IS NAMED AFTER.
// `menugeom_test.go` had its own `frameText(f, w, h)` — the same walk,
// taking the extent as two ints and IGNORING w entirely, so the 40
// written at its one call site meant nothing and the 12 restated the
// composer's height. No grep for the cell-reader bug could find it: it
// already went through render.RowText, and what made it a duplicate was
// the loop around it. The three it joins were `dump`, `menuRows` and
// `screen` — the last of which is still a declaration of its own below,
// because a composer is not a frame; only its loop went to
// render.BufferText.
func frameText(f *gooey.Frame) string { return render.BufferText(f.Cells) }

// frameRows is frameText split into rows, WITHOUT the phantom last one.
//
// render.BufferText terminates EVERY row including the last — a stated
// contract with its own pin (TestBufferTextIsEveryRowNewlineTerminated),
// there so that a dump missing its final row is a different string
// rather than a prefix of a longer one. Split on "\n" and that property
// hands back one more element than the frame has rows, the last of them
// empty.
//
// Nothing is wrong with the empty element on its own: a non-empty needle
// cannot match it, and row indices are unaffected. What it costs is
// len(): a caller reaching for len(rows) as the frame HEIGHT is off by
// one, over a row that the padding contract makes indistinguishable from
// a real blank one. Every failure dump also ends in a blank line.
//
// It is here rather than at the three call sites that had the split
// written out because this file exists for exactly that — one of each
// reader, in one file — and because the remaining directories of #516
// will reach for this shape rather than re-derive the TrimSuffix.
//
// [#516]: https://github.com/WonderForgeLabs/gooey/issues/516
func frameRows(f *gooey.Frame) []string {
	// ZERO ROWS IS NIL, NOT ONE EMPTY ROW, and without this line the
	// helper returned the very off-by-one its doc above says it removes:
	// frameText is "" for a frame of no rows — and for one whose Cells
	// is nil, by render.BufferText's stated contract — TrimSuffix leaves
	// "", and strings.Split("", "\n") is ONE element. len(rows) then
	// answers 1 for a height of 0.
	text := frameText(f)
	if text == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(text, "\n"), "\n")
}

// screen is the composition as a terminal would show it.
//
// A COMPOSER RATHER THAN A FRAME, which is the only reason it is not
// frameText: the two differ by where the buffer comes from, not by what
// is done with it.
func screen(c *gooey.Composer) string { return render.BufferText(c.Cells()) }

// rowMatch is one row that held a needle and the byte offset it held it
// at.
//
// PAIRED, because the two answers are about one match. Three tests grew
// their own version of this search, and two of them had already taken
// the row from one pass and the offset from another — correct only while
// a neighbouring `len == 1` guard happened to make them agree. Loosening
// such a guard to "at least one" slices one row at an offset measured in
// another: a wrong column in a failure message at best, an out-of-range
// slice at worst.
type rowMatch struct{ row, byteAt int }

// matchRows is every row of rows holding needle, with where.
//
// EVERY CANDIDATE, NOT THE FIRST. Two of the three copies stopped at the
// first hit, so a second matching row — a second item, a status line
// echoing the label, a scrolled duplicate — was resolved by iteration
// order and the other became invisible. Every claim these tests make is
// POSITIONAL, about a specific column of a specific row, so two
// candidates is a fault to name rather than a choice to make.
func matchRows(rows []string, needle string) []rowMatch {
	var out []rowMatch
	for y, r := range rows {
		if i := strings.Index(r, needle); i >= 0 {
			out = append(out, rowMatch{y, i})
		}
	}
	return out
}

// onlyMatch is matchRows with "and exactly one", which is what every
// caller wants.
//
// ifNone is the caller's: "no row matched" and "the row is wrong" are
// different faults, and which one zero means is test-specific — for one
// test it is the regression under test, for another it is the dropdown
// not painting at all. Folding a generic sentence in here would lose
// that.
func onlyMatch(t *testing.T, rows []string, needle, ifNone string) rowMatch {
	t.Helper()
	got := matchRows(rows, needle)
	if len(got) == 1 {
		return got[0]
	}
	if len(got) == 0 {
		t.Fatalf("no row holds %q. %s\n%s", needle, ifNone, strings.Join(rows, "\n"))
	}
	var at []int
	for _, m := range got {
		at = append(at, m.row)
	}
	t.Fatalf("%q appears on rows %v, want exactly one. The assertion below is "+
		"positional — a column of a specific row — so picking one of two by "+
		"iteration order would hide whichever it did not pick:\n%s",
		needle, at, strings.Join(rows, "\n"))
	return rowMatch{}
}

// TestFrameRowsIsTheFrameHeight is the pin for the off-by-one frameRows
// exists to remove, and it is written as an A/B against the spelling it
// replaced because the defect is invisible any other way.
//
// The phantom element breaks nothing a matcher does — a non-empty needle
// cannot match "", and indices are unaffected — so no existing assertion
// moves when it is there. What it breaks is len(), and only a test that
// asks for len() can see it. Both halves are measured here so the
// paragraph on frameRows is a statement about this package rather than
// about render.BufferText's contract in the abstract.
func TestFrameRowsIsTheFrameHeight(t *testing.T) {
	// A TABLE OF SHAPES, because one fixture pins the contract at the
	// one shape where it was never in doubt. The claim is len(rows) ==
	// H for every W and H, and it was false at H == 0 — where TrimSuffix
	// has nothing to trim and Split answers one element for no rows —
	// while a single 6x4 fixture reported green. Zero height is the row
	// that needed the guard; one is where the trailing newline is the
	// whole of the string; four is the ordinary case.
	//
	// AND W IS A SEPARATE AXIS, which three heights at one width do not
	// reach. frameRows' doc is about the TrimSuffix, and the obvious
	// mutation of it is silent at every non-zero width:
	//
	//	helper body                     W=6,H=4    W=0,H=4
	//	TrimSuffix(text, "\n")           4 rows     4 rows
	//	TrimRight(text, "\n")            4 rows     1 row
	//
	// At W == 0 every row is the empty string, so frameText is "\n\n\n\n"
	// and TrimRight eats all four — the same phantom-element class the
	// helper exists to remove, reached from the other axis. Measured in
	// review of #520; W == 0 is not live in components today, which is
	// the argument this test already makes for H == 0.
	for _, tc := range []struct{ w, h int }{{6, 0}, {6, 1}, {6, 4}, {0, 4}} {
		w, h := tc.w, tc.h
		f := &gooey.Frame{Cells: render.NewBuffer(w, h)}
		if got := len(frameRows(f)); got != h {
			t.Errorf("frameRows returned %d rows for a %dx%d frame, want %d — a "+
				"caller reading len() as the frame height is off by the phantom "+
				"element BufferText's trailing newline leaves", got, w, h, h)
		}
		if h == 0 {
			// THE A/B BELOW DOES NOT APPLY AT ZERO. frameText is "" for
			// a frame of no rows, so Split gives one element and h+1 is
			// also one — the two agree for the wrong reason, and
			// asserting it would pin the bug rather than the contract.
			continue
		}
		if got, want := len(strings.Split(frameText(f), "\n")), h+1; got != want {
			t.Fatalf("splitting frameText gave %d elements for a %dx%d frame, want "+
				"%d — if this is no longer height+1 then BufferText stopped "+
				"terminating the last row and frameRows' TrimSuffix is now wrong",
				got, w, h, want)
		}
	}
	// A NIL Cells IS THE SAME ROW OF THE TABLE reached another way:
	// render.BufferText answers nil with "" by its stated contract, so
	// this is the H == 0 case without a buffer to declare it.
	if got := len(frameRows(&gooey.Frame{})); got != 0 {
		t.Errorf("frameRows returned %d rows for a frame with no Cells, want 0 — "+
			"BufferText answers a nil buffer with the empty string, which is "+
			"the same shape as a zero-row frame", got)
	}
}
