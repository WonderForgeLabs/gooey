package render

import (
	"strings"
	"testing"

	"github.com/rivo/uniseg"
)

// TestTheASCIIFastPathAgreesWithTheSegmenter checks the shortcut against
// the thing it is shortcutting, for every rune it claims.
//
// A fast path is a second implementation of an answer, and the way it
// fails is by being right for the cases someone thought of. So this
// asks the segmenter for all of them rather than a sample, and also
// walks the runes just OUTSIDE the range to confirm they still take the
// slow path rather than being quietly excluded from both.
func TestTheASCIIFastPathAgreesWithTheSegmenter(t *testing.T) {
	for r := rune(0); r < 0x100; r++ {
		if r == Continuation {
			continue
		}
		c := Cell{Rune: r}
		if got, want := c.Width(), StringWidth(string(r)); got != want {
			t.Errorf("Cell{Rune: %q}.Width() = %d, segmenter says %d",
				r, got, want)
		}
	}
	// A cell whose CLUSTER is set holds an ASCII lead and is still not
	// one column — the guard the fast path needs and could plausibly
	// have been written without.
	c := Cell{Rune: 'e', Cluster: "é"}
	if got, want := c.Width(), StringWidth("é"); got != want {
		t.Errorf("a clustered cell with an ASCII lead measured %d, want %d",
			got, want)
	}
}

// TestClipColsHoldsItsContract pins the one-pass rewrite against the
// properties the two-pass version had, rather than against its output.
//
// Three of them, and each fails differently: a result wider than the
// budget overflows the line; a result that is not a prefix has dropped
// something from the middle; and a result that could have taken one
// more cluster clips more than it needed to.
func TestClipColsHoldsItsContract(t *testing.T) {
	inputs := []string{
		"", "a", "abcdef",
		"世界", "a世b", "世a界b",
		"éxyz",          // decomposed: two runes, one column
		"⚠️!", "🏳️‍🌈ab", // variation selector, ZWJ sequence
		strings.Repeat("界", 8),
		"1️⃣2️⃣3️⃣",
	}
	for _, s := range inputs {
		for w := -1; w <= StringWidth(s)+2; w++ {
			got := ClipCols(s, w)

			if w <= 0 {
				if got != "" {
					t.Errorf("ClipCols(%q, %d) = %q, want empty", s, w, got)
				}
				continue
			}
			if gw := StringWidth(got); gw > w {
				t.Errorf("ClipCols(%q, %d) = %q, which is %d columns",
					s, w, got, gw)
			}
			if !strings.HasPrefix(s, got) {
				t.Errorf("ClipCols(%q, %d) = %q, which is not a prefix",
					s, w, got)
			}
			// Maximal: whatever was dropped could not have fit.
			if rest := s[len(got):]; rest != "" {
				next, _, cw, _ := uniseg.FirstGraphemeClusterInString(rest, -1)
				if StringWidth(got)+cw <= w {
					t.Errorf("ClipCols(%q, %d) = %q but %q still fits",
						s, w, got, next)
				}
			}
		}
	}
}
