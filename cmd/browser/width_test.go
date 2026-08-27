package main

import (
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/render"
)

// The browser renders README markdown and Go doc comments — text written
// by whoever wrote the demo, not by this repo. An emoji in a heading or a
// CJK identifier in a fence is an ordinary input here.
//
// Every one of these tests is built from a string whose RUNE count and
// COLUMN count differ, because that is the only shape that can tell the
// two apart. "世界" is 2 runes and 4 columns; an ASCII fixture agrees with
// itself under either rule and would pass against the bug.

// clip is handed a column budget by all nine of its call sites (b.W,
// b.W-3, b.X+b.W-1-x). Counting runes let it return up to twice its slot.
func TestClipCountsColumns(t *testing.T) {
	// 4 runes, 8 columns. A 5-column budget fits two glyphs and has one
	// column left over that no glyph can fill — clipping stops there
	// rather than splitting one in half.
	got := clip("世界世界", 5)
	if w := render.StringWidth(got); w > 5 {
		t.Errorf("clip(%q, 5) = %q, %d columns — over budget", "世界世界", got, w)
	}
	if got != "世界" {
		t.Errorf("clip(%q, 5) = %q, want %q — two whole glyphs, and the odd "+
			"column left empty because half a glyph is not drawable", "世界世界", got, "世界")
	}
}

// The doc-comment path. Nothing downstream clips it (#357), so an overrun
// lands on whatever is painted beside the pane.
func TestWrapLineCountsColumns(t *testing.T) {
	const word = "世界世界" // 4 runes, 8 columns
	got := wrapLine(word+" "+word, 12)
	if len(got) != 2 {
		t.Fatalf("wrapLine put %q on %d line(s) at width 12, want 2 — two "+
			"8-column words plus a space need 17. A rune count sees 9 and "+
			"wrongly fits them", word+" "+word, len(got))
	}
	for i, l := range got {
		if w := render.StringWidth(l); w > 12 {
			t.Errorf("line %d is %d columns in a 12-column pane: %q", i, w, l)
		}
	}
}

// The ASCII control, so a regression to rune counting cannot hide behind
// the wide cases — under the bug this test passes, which is the point of
// keeping it separate and labelled.
func TestWrapLineKeepsAsciiWithinItsWidth(t *testing.T) {
	for _, l := range wrapLine("the quick brown fox jumps over the lazy dog", 12) {
		if w := render.StringWidth(l); w > 12 {
			t.Errorf("line %q is %d columns, over the 12-column budget", l, w)
		}
	}
}

// wrapSpans measures through colWidth, and its doc comment has always
// said "lines of w columns" — it was the body that disagreed.
func TestMarkdownWrapsToColumns(t *testing.T) {
	st := markdownStyles()
	for i, ln := range text(renderMarkdown("世界世界 世界世界 世界世界\n", 12, st)) {
		if w := render.StringWidth(ln); w > 12 {
			t.Errorf("markdown line %d is %d columns in a 12-column pane: %q", i, w, ln)
		}
	}
}

// drawLines is the paint half, and it fails differently from the wrappers
// above: it used to advance one column per RUNE via Cells.Set, so a wide
// glyph was written into one cell and the next rune into the cell that
// glyph's second column covers. Both halves are asserted here — that the
// span does not overrun its rect, and that the second column of each
// glyph carries the continuation marker rather than the following text.
func TestDrawLinesAdvancesByColumnAndMarksContinuations(t *testing.T) {
	f := &gooey.Frame{Cells: render.NewBuffer(12, 1)}
	drawLines(f, 0, 0, 6, 1, []mdLine{{{text: "世界世界"}}})

	// The buffer is twice the rect so an overrun has somewhere to land
	// and be seen. Three of the four glyphs fill the 6-column rect
	// exactly; the fourth is clipped and nothing is written past column 5.
	for x := 6; x < 12; x++ {
		if r := f.Cells.At(x, 0).Rune; r != 0 && r != ' ' {
			t.Errorf("column %d holds %q — drawLines painted past its 6-column rect",
				x, string(r))
		}
	}
	// And the layout inside the rect is glyph, continuation, glyph,
	// continuation — not four glyphs in four columns, which is what
	// advancing one column per rune produced.
	want := []rune{'世', render.Continuation, '界', render.Continuation, '世', render.Continuation}
	for x, w := range want {
		if got := f.Cells.At(x, 0).Rune; got != w {
			t.Errorf("column %d = %q, want %q — the second column of a wide "+
				"glyph must be a continuation, not the next rune",
				x, string(got), string(w))
		}
	}
}
