package catalogen

import (
	"strings"
	"testing"
)

// This package had no tests, and that is how two holes in it reached
// review. Both are about the SAME confusion — which element a read
// belongs to — and neither is reachable from the real vocabulary, so
// neither could be pinned against it. testdata/src is the fixture that
// makes them reachable.

func findingsFor(t *testing.T, dir string) []Finding {
	t.Helper()
	out, err := Check("testdata/" + dir)
	if err != nil {
		t.Fatalf("checking the %s fixture: %v", dir, err)
	}
	return out
}

// TestAHostsOwnReadIsNotAChildsAttribute is the first hole.
//
// checkPseudo asks "does the host's Build read this attribute?", and it
// used to ask a walk that recorded reads off EVERY identifier —
// including the host's own element. So a pseudo-element could declare
// an attribute only the host reads off itself and pass: <MenuItem>
// declaring Style, which buildMenuBar reads off the bar, was green while
// `<MenuItem Style="…">` loaded clean and was dropped on the floor.
//
// The fixture reproduces it exactly: <Host> reads Style off e and Text
// off each child. A <Child> declaring Style must be reported.
func TestAHostsOwnReadIsNotAChildsAttribute(t *testing.T) {
	// The baseline first — the clean fixture must be clean, or the
	// assertion below passes for the wrong reason. It differs from the
	// hostread one by exactly the offending line.
	for _, f := range findingsFor(t, "src") {
		t.Errorf("the clean fixture is not clean to begin with: %s", f)
	}

	// Text is read off the child and declared by the child: legal.
	// Style is read off the HOST and declared by the child: not.
	got := findingsFor(t, "hostread")
	var named bool
	for _, f := range got {
		if f.Element == "Child" && strings.Contains(f.String(), "Style") {
			named = true
		}
	}
	if !named {
		t.Errorf("a pseudo-element declaring an attribute the HOST reads off ITSELF was accepted: "+
			"markup setting it would load clean and be silently dropped. findings: %v", got)
	}
}

// TestTheDenyListAppliesToTheChildWalk is the second hole.
//
// The child walk followed any package-level callee by name, where scan
// applies a deny-list (build, buildChildren, checkAttrs, …) so it does
// not wander into the general builder machinery. checkPseudoPool then
// subtracted a set computed WITH the guards from one computed without,
// which is not a subtraction: anything reachable only through the loose
// walk was reported as an attribute no element declares — a finding
// naming something nobody wrote.
//
// The fixture's checkAttrs reads "Smuggled". A clean run is the whole
// assertion.
func TestTheDenyListAppliesToTheChildWalk(t *testing.T) {
	for _, f := range findingsFor(t, "src") {
		if strings.Contains(f.String(), "Smuggled") {
			t.Errorf("the child walk followed a generic builder and reported an attribute "+
				"belonging to no element: %s", f)
		}
	}
}

// TestTheHostsOwnAttributeIsNotReportedUndeclared is the other side of
// the split: <Host> reads Style off itself and declares it, so the pool
// check must not demand that some CHILD declare it.
func TestTheHostsOwnAttributeIsNotReportedUndeclared(t *testing.T) {
	for _, f := range findingsFor(t, "src") {
		if strings.Contains(f.String(), "Style") {
			t.Errorf("the host's own attribute was attributed to its children: %s", f)
		}
	}
}
