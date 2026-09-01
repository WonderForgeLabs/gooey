package render

import (
	"testing"
)

// EachCluster's own tests, which it shipped without — it was exported
// with one caller and covered only through that caller's behaviour, and
// "the finder highlights the right column" is a claim about the finder.
//
// The three numbers it hands over are the whole reason it exists: bytes,
// columns and a width are different questions, and a fixture where they
// agree cannot tell which one a caller is reading. Every case below is
// chosen so at least two of them DISAGREE.

// TestEachClusterHandsOverThreeDistinctNumbers is the discrimination
// test. "世" is 3 bytes and 2 columns; "é" is 3 bytes, one cluster
// and 1 column; "a" agrees with itself under every rule and is here only
// as the baseline the others are read against.
func TestEachClusterHandsOverThreeDistinctNumbers(t *testing.T) {
	const s = "a世éb"
	type visit struct {
		cluster        string
		off, col, wide int
	}
	want := []visit{
		{"a", 0, 0, 1},
		{"世", 1, 1, 2},
		{"é", 4, 3, 1},
		{"b", 7, 4, 1},
	}
	var got []visit
	EachCluster(s, func(cluster string, off, col, w int) bool {
		got = append(got, visit{cluster, off, col, w})
		return true
	})
	if len(got) != len(want) {
		t.Fatalf("walked %d clusters, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("cluster %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	// And the two cursors really did diverge, or the case above proves
	// nothing about which number a caller is reading.
	last := got[len(got)-1]
	if last.off == last.col {
		t.Errorf("the byte offset and the column agree (%d) at the last cluster; "+
			"this fixture cannot tell them apart and would pass against a walk "+
			"that returned one for both", last.off)
	}
}

// TestEachClusterWidthMatchesStringWidth pins the number the signature
// exists to hand over: the width the walk reports must be the width the
// package's own measurement function reports, or a caller that trusts it
// lays out differently from one that measures.
func TestEachClusterWidthMatchesStringWidth(t *testing.T) {
	for _, s := range []string{"", "a", "世界", "éx", "🇬🇧", "ab世cd"} {
		total := 0
		EachCluster(s, func(cluster string, _, _, w int) bool {
			if got := StringWidth(cluster); got != w {
				t.Errorf("EachCluster(%q) reports width %d for cluster %q; "+
					"StringWidth says %d", s, w, cluster, got)
			}
			total += w
			return true
		})
		if got := StringWidth(s); total != got {
			t.Errorf("the widths of %q's clusters sum to %d; StringWidth(%q) is %d",
				s, total, s, got)
		}
	}
}

// TestEachClusterStopsWhenTheCallbackSaysSo — the bool is the whole
// control surface, and a walk that ignores it hands the caller clusters
// it has already decided not to draw.
func TestEachClusterStopsWhenTheCallbackSaysSo(t *testing.T) {
	n := 0
	EachCluster("abcdef", func(string, int, int, int) bool {
		n++
		return n < 3
	})
	if n != 3 {
		t.Errorf("the callback returned false on visit 3 and the walk made %d "+
			"visits, want 3", n)
	}
}

// TestEachClusterOnAnEmptyStringVisitsNothing is the vacuity guard the
// two tests above need: a walk that never calls its callback satisfies
// every per-cluster assertion in this file.
func TestEachClusterOnAnEmptyStringVisitsNothing(t *testing.T) {
	called := false
	EachCluster("", func(string, int, int, int) bool {
		called = true
		return true
	})
	if called {
		t.Error("the empty string produced a cluster")
	}
}
