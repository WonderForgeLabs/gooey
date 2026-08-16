package logdata

import "testing"

// The demos style a line by looking its Level up in a map and filter by
// comparing Level to a literal, so a level this set does not name renders
// unstyled and is invisible to the filter. Both demos would still exit 0.
var known = map[string]bool{"ERROR": true, "WARN": true, "INFO": true, "DEBUG": true}

func TestNextProducesStyleableLines(t *testing.T) {
	before := Count()
	seen := map[string]int{}
	const n = 2000
	for i := 0; i < n; i++ {
		l := Next()
		if !known[l.Level] {
			t.Fatalf("line %d: level %q is not one the demos style or filter on", i, l.Level)
		}
		if l.Text == "" {
			t.Fatalf("line %d: empty text — the pane would render a blank row", i)
		}
		seen[l.Level]++
	}
	// Every level has to actually turn up, or `f` cycles to a filter that
	// empties the pane and the demo looks broken.
	for level := range known {
		if seen[level] == 0 {
			t.Errorf("no %s lines in %d draws", level, n)
		}
	}
	if got, want := Count()-before, n; got != want {
		t.Errorf("Count advanced by %d, want %d", got, want)
	}
}
