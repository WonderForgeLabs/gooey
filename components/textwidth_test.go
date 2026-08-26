package components

import (
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/prop"
)

// Measure has to report DISPLAY COLUMNS, because that is the unit layout
// hands out. A rune count under-reports any line holding a CJK character
// or emoji, so the Text is arranged too narrow and overruns whatever sits
// to its right — with nothing to notice it, since gooey has no clipping
// (#357) and the cells it wrote were the ones it meant to write.
//
// Asserted against a string whose rune count and column count DIFFER,
// which is the only kind that can fail: for ASCII the two answers are
// identical, so an all-ASCII test would pass against the rune-counting
// version this replaced and prove nothing.
func TestTextMeasuresInColumnsNotRunes(t *testing.T) {
	for _, c := range []struct {
		content string
		runes   int
		want    int
		why     string
	}{
		{"世界", 2, 4, "two wide glyphs are four columns"},
		{"a世b", 3, 4, "one wide glyph among ASCII"},
		{"🇯🇵", 2, 2, "a flag is two runes and two columns — measured by cluster, " +
			"the rune count is an OVER-count here rather than an under-count"},
		{"abc", 3, 3, "ASCII, where the two answers agree; here so a regression " +
			"to rune counting cannot hide behind the wide cases alone"},
	} {
		txt := &Text{Content: prop.NewSource(c.content)}
		got := txt.Measure(gooey.Size{W: 100, H: 10})
		if got.W != c.want {
			t.Errorf("Measure(%q).W = %d, want %d (rune count is %d) — %s",
				c.content, got.W, c.want, c.runes, c.why)
		}
	}
}

// The multi-line case, because Measure takes the widest LINE and the loop
// that does so is the one that changed.
func TestTextMeasuresTheWidestLineInColumns(t *testing.T) {
	// Line two has fewer runes and more columns than line one, so a
	// rune-counting Measure picks the wrong line — not merely the wrong
	// number. That distinction is why this case exists separately.
	txt := &Text{Content: prop.NewSource("abcd\n世界")}
	got := txt.Measure(gooey.Size{W: 100, H: 10})
	if got.W != 4 {
		t.Errorf("Measure().W = %d, want 4 — 世界 is 2 runes and 4 columns, so it "+
			"is the widest line even though abcd has more runes", got.W)
	}
	if got.H != 2 {
		t.Errorf("Measure().H = %d, want 2", got.H)
	}
}
