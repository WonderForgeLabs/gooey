package input

import "testing"

// TestNamedKeyMatchingIsCaseInsensitiveIncludingSpace pins the claim
// docs/learn/howto/howto-keybindings.md makes — "modifier and named-key
// matching is case-insensitive" — against the one named key that was
// not.
//
// "space" is not in keyNames. It is rewritten to a literal " " before
// the EqualFold loop that handles every other name, and that rewrite was
// an exact ==, so "Enter", "ESC" and "Tab" all parsed while "Space"
// failed with `unknown key "Space"`. A markup author who capitalised a
// spelling the how-to lists got a LOAD ERROR.
//
// Worth a test rather than the one-line fix alone, because the asymmetry
// is invisible from the code: the arm that makes space special sits
// three lines above the loop that makes case not matter, and nothing
// connects them. Found in review of #428, which is the change that made
// ctrl+space a documented destination by normalising ctrl+@ onto it.
func TestNamedKeyMatchingIsCaseInsensitiveIncludingSpace(t *testing.T) {
	want, err := ParseGesture("space")
	if err != nil {
		t.Fatalf(`ParseGesture("space") = %v`, err)
	}
	for _, g := range []string{"Space", "SPACE", "sPaCe"} {
		got, err := ParseGesture(g)
		if err != nil {
			t.Errorf("ParseGesture(%q) = %v; every other named key folds case, "+
				"and the how-to says space does too", g, err)
			continue
		}
		if got != want {
			t.Errorf("ParseGesture(%q) = %s, want %s", g, got, want)
		}
	}
	// Under a modifier too.
	lower, err := ParseGesture("ctrl+space")
	if err != nil {
		t.Fatal(err)
	}
	upper, err := ParseGesture("ctrl+Space")
	if err != nil {
		t.Fatalf(`ParseGesture("ctrl+Space") = %v`, err)
	}
	if lower != upper {
		t.Errorf("ctrl+space parses to %s and ctrl+Space to %s", lower, upper)
	}
	// The discriminating half: folding case must not have made every
	// near-miss parse.
	if _, err := ParseGesture("spce"); err == nil {
		t.Error(`ParseGesture("spce") succeeded; a near-miss must still be an error`)
	}
}

// TestTheOtherNamedKeysStillFoldCase is the floor the test above needs:
// it asserts about space specifically, and would pass just as well if
// the EqualFold loop it is comparing against had stopped folding.
func TestTheOtherNamedKeysStillFoldCase(t *testing.T) {
	for _, pair := range [][2]string{
		{"enter", "Enter"}, {"esc", "ESC"}, {"tab", "Tab"}, {"up", "UP"},
	} {
		lo, err := ParseGesture(pair[0])
		if err != nil {
			t.Fatalf("ParseGesture(%q) = %v", pair[0], err)
		}
		hi, err := ParseGesture(pair[1])
		if err != nil {
			t.Errorf("ParseGesture(%q) = %v", pair[1], err)
			continue
		}
		if lo != hi {
			t.Errorf("%q and %q parse differently (%s vs %s)", pair[0], pair[1], lo, hi)
		}
	}
}
