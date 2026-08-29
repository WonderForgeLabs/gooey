package main

import (
	"testing"
)

// colWidth clamps how far the log pane scrolls right, and the log holds
// whatever a worker printed — the one input in this app that is neither
// a fixed literal nor typed by the user into a known field.
//
// It counted runes while the scroll it bounds is measured in cells, so a
// line with any wide glyph in it stopped short of its own end: the last
// column per wide glyph was unreachable, with no error and nothing on
// screen to explain why the line would not scroll further.
func TestLogScrollClampCountsColumns(t *testing.T) {
	// The same row said two ways: four wide glyphs across two runs, and
	// the same eight columns of ASCII. The clamp must not care which.
	wide := []colorRun{{Text: "世界"}, {Text: "世界"}}
	narrow := []colorRun{{Text: "abcd"}, {Text: "efgh"}}
	if got, want := colWidth(wide), colWidth(narrow); got != want {
		t.Errorf("colWidth = %d for two 4-column runs but %d for two 4-column "+
			"ASCII runs — a rune count sees 4 where the terminal draws 8, so "+
			"the pane refuses to scroll to the end of the line", got, want)
	}
	if got := colWidth(wide); got != 8 {
		t.Errorf("colWidth(%v) = %d, want 8", wide, got)
	}
}
