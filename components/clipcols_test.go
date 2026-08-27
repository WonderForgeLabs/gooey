package components

import (
	"testing"

	"github.com/WonderForgeLabs/gooey/render"
)

// clipCols clips to COLUMNS, which is what all ~25 of its callers pass —
// b.W, b.X+b.W-x, a Border's inner width. Counting runes instead was the
// same number only for ASCII, and the overrun landed on whatever was
// painted next, since gooey clips nothing at the frame level (#357).
//
// Every case here uses a string whose rune count and column count DIFFER,
// except the two that exist to prove the function still does the ordinary
// thing. A table of ASCII would pass against the rune-counting version
// this replaced.
func TestClipColsClipsToColumns(t *testing.T) {
	for _, c := range []struct {
		in   string
		w    int
		want string
		why  string
	}{
		{"世界ab", 3, "世", "世 is 2 columns; 界 would make 4, past the budget of 3"},
		{"世界ab", 4, "世界", "exactly the budget"},
		{"世界ab", 5, "世界a", "one ASCII column fits after two wide glyphs"},
		{"世界ab", 99, "世界ab", "budget exceeds the string; returned whole"},
		{"abcd", 2, "ab", "ASCII still behaves"},
		{"世", 1, "", "a 2-column glyph cannot be drawn in 1 column, and half a " +
			"glyph is not a thing a terminal can render"},
		{"", 4, "", "empty in, empty out"},
		{"abc", 0, "", "no budget"},
		{"abc", -1, "", "negative budget"},
	} {
		if got := clipCols(c.in, c.w); got != c.want {
			t.Errorf("clipCols(%q, %d) = %q, want %q — %s", c.in, c.w, got, c.want, c.why)
		}
	}
}

// THE PROPERTY, rather than another example: whatever clipCols returns
// must fit. This is the assertion that would catch a case the table above
// did not think of, and it is the one the callers actually depend on.
func TestClipColsNeverExceedsItsBudget(t *testing.T) {
	inputs := []string{
		"世界ab", "a世b界c", "🇯🇵x", "👍👍👍", "héllo", "", "abc",
		"世", "a世", "世a", "🇯🇵🇯🇵",
	}
	for _, in := range inputs {
		for w := -1; w <= 10; w++ {
			got := clipCols(in, w)
			if n := render.StringWidth(got); n > w && w > 0 {
				t.Errorf("clipCols(%q, %d) = %q, which is %d columns — a clip that "+
					"overruns its budget is the whole defect", in, w, got, n)
			}
			// And it must not invent content.
			if len(got) > len(in) {
				t.Errorf("clipCols(%q, %d) = %q, longer than its input", in, w, got)
			}
		}
	}
}

// A clip is allowed to UNDER-fill, and that is not a bug to be tuned
// away. Pinned so nobody "fixes" it by padding: a budget of 3 holding
// "世界" fits only 世, leaving one column blank, because the alternative
// is drawing half of 界.
func TestClipColsMayLeaveAColumnUnused(t *testing.T) {
	got := clipCols("世界", 3)
	if got != "世" {
		t.Fatalf("clipCols(\"世界\", 3) = %q, want \"世\"", got)
	}
	if n := render.StringWidth(got); n != 2 {
		t.Errorf("result is %d columns in a 3-column budget; the test's premise "+
			"is that it under-fills, and it does not", n)
	}
}
