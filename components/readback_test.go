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
// THE WINDOW COMES FROM WHAT WAS PAINTED — for frameText and screen,
// which is the pair this paragraph is about. Each took its extent as
// parameters, written down beside a composer that had already said the
// same numbers — and a read wider than the buffer is phantom blanks
// rather than an error (render.SpanText's own doc says so), so widening
// a composer in a later edit turns the tail of every read into blanks.
// Several call sites here compare two screens for equality, where blanks
// on both sides agree, and one is `!strings.Contains(got, "gone")`,
// which passes on blank input outright. Deriving the extent from the
// frame is what removes that class, and neither of those two now takes a
// width it was not given by the thing it is reading.
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
// consumer's comments and the copies went stale independently. FOUR
// readers, distinguished by what they read rather than by who asked: a
// region of a row, a whole row of a buffer, a whole frame, a whole
// composition. Plus reversedText, which reads a STYLE rather than text
// and so answers a different question, and the one row search every
// positional assertion in this package needs.
//
// `row` was the last one out, and it was the one with the most call
// sites — which is the shape to distrust: the helper everyone uses is
// the one nobody notices is somewhere odd.
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

// screen is the composition as a terminal would show it.
//
// A COMPOSER RATHER THAN A FRAME, which is the only reason it is not
// frameText: the two differ by where the buffer comes from, not by what
// is done with it.
func screen(c *gooey.Composer) string { return render.BufferText(c.Cells()) }

// reversedText is the text of the reversed cells of row 0, in order.
//
// Cell.Text() rather than Cell.Rune, because a reversed cell may hold a
// multi-rune cluster and a rune would report only its lead — which is
// the distinction three tests in textbox_test.go exist to make. A wide
// cluster's continuation cell carries no text, so a two-column glyph
// still contributes its cluster once.
//
// THE EXTENT COMES FROM THE FRAME, per this file's own header. It took
// a width, written at each call site beside the term.Caps that had
// already said the same number — `reversedText(f, 2)` under
// `Cols: 2` — which is the written-down-window class the header is
// about, and a read wider than the buffer is phantom blanks rather than
// an error. There are no reversed cells in phantom blanks, so the
// failure would have been a SILENTLY shorter answer.
func reversedText(f *gooey.Frame) string {
	var b strings.Builder
	for x := 0; x < f.Cells.W; x++ {
		if c := f.Cells.At(x, 0); c.Style.Reverse {
			b.WriteString(c.Text())
		}
	}
	return b.String()
}

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
