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

// TestClipColsDoesNotAllocate is the assertion that would have caught
// ClipCols' own comment being wrong, and it is a different instrument
// from every other test in this file.
//
// TestClipColsHoldsItsContract asserts prefix, width and maximality —
// all true of an implementation that allocates a scratch buffer on every
// call, which is what the one-pass rewrite did while its comment said
// the common case "allocates nothing". A behavioural test cannot see a
// cost, so a claim about cost needs a measurement or it is prose.
//
// BOTH BRANCHES, because they were not equally wrong: the clipping path
// has to build an answer at all, so a reader could reasonably expect an
// allocation there, while the fits path returns the argument. Pinning
// only the fits case would leave the clip case free to regress to a
// buffer, and the whole point of the byte-offset form is that neither
// needs one.
//
// AllocsPerRun rather than a benchmark: this is a contract, so it
// belongs where a failure is red rather than where someone has to read a
// number and decide.
func TestClipColsDoesNotAllocate(t *testing.T) {
	for _, tc := range []struct {
		name string
		s    string
		w    int
	}{
		{"fits", "the quick brown fox jumps over the lazy dog", 100},
		{"clips", "the quick brown fox jumps over the lazy dog", 20},
		{"clips at a wide glyph", "日本語テキストの見本です", 7},
		{"fits with combining marks", "e\u0301cole nave\u0308", 20},
		{"empty budget", "anything at all", 0},
	} {
		var got string
		n := testing.AllocsPerRun(100, func() { got = ClipCols(tc.s, tc.w) })
		if n != 0 {
			t.Errorf("%s: ClipCols allocated %v times per call, want 0. Every accepted "+
				"cluster is a contiguous prefix of the input, so the answer is a slice "+
				"of it — a scratch buffer here runs once per string per paint",
				tc.name, n)
		}
		_ = got
	}
}
