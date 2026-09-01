package sethandlers_test

import (
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	sethandlers "github.com/WonderForgeLabs/gooey/handlers/sets"
)

// TestTheAlgebraHoldsOverEveryGroup is the test whose absence let a
// permission fail open for a whole review round.
//
// sets_test.go covers sets:Group over "Mouse", which is a plain union of
// two primitives and therefore the case that cannot go wrong. The two
// that can are "All" and "None": neither is a union of primitives, and
// gooey.AllowGroups had to choose a spelling for each. When "All" was
// rendered as the single opaque token "All", this algebra — which is a
// set of NAMES and knows nothing about what they mean — could no longer
// take anything out of it. `Without(Group("All"), "Start")` removed
// nothing and evaluated back to "All", so a page asking for
// "everything except Start" GRANTED Start, the one category with a
// child-process argument behind it.
//
// The claim is per-group and mechanical: taking a primitive out of a
// group must leave a set that no longer has it — UNLESS one of the
// primitives left behind implies it. That exception is not a hedge, it
// is the vocabulary's own design: the implications live in the
// CONSTANTS (AllowAlpha = bitAlpha|bitFocus), so union is closed by
// construction and there is no path through this algebra that yields a
// key class without its focus bit. Removing "Focus" from "Keys" is
// therefore correctly a no-op, and a test demanding otherwise would be
// asserting against the design rather than for it.
//
// AllowStart is the category with no implier and no focus bit, which is
// why it is the one the fail-open was found on.
func TestTheAlgebraHoldsOverEveryGroup(t *testing.T) {
	groups := gooey.AllowGroups()
	if len(groups) == 0 {
		t.Fatal("AllowGroups is empty — this test would pass vacuously")
	}
	exercised := 0
	for name, names := range groups {
		group, err := gooey.ParseAllow(name)
		if err != nil {
			t.Fatalf("group %q does not parse: %v", name, err)
		}
		// The PRIMITIVES OF THE GROUP, not the names of its expansion,
		// and that distinction is the whole test. A markup author writes
		// `sets:Without (sets:Group "All") "Start"` — they name a
		// primitive the group CONTAINS, with no idea what the expansion
		// happens to be spelled as. Iterating the expansion instead
		// misses the defect entirely: removing "All" from ["All"] does
		// leave nothing, so the token looks fine right up until someone
		// removes something that was never in the list.
		for _, prim := range group.Names() {
			cat, err := gooey.ParseAllow(prim)
			if err != nil {
				t.Errorf("group %q contains %q, which does not parse: %v", name, prim, err)
				continue
			}
			// AllowNone is contained in EVERY set — Has is `a&cat == cat`
			// — so it can never be "removed" and proves nothing here.
			if cat == gooey.AllowNone {
				continue
			}

			// The difference, exactly as sets:Without computes it: a
			// name-for-name filter over the expansion.
			var rest []string
			implied := false
			for _, n := range names {
				if n == prim {
					continue
				}
				rest = append(rest, n)
				// ONLY A PRIMITIVE MAY EXCUSE A FAILED REMOVAL. A GROUP
				// name left standing in the result is not an implication,
				// it is the failure — "All" survives a difference over
				// "Start" precisely because the algebra could not see
				// into it, and counting that as "implied" is how this
				// test passed against the defect it was written for.
				if groups[n] != nil {
					continue
				}
				if other, err := gooey.ParseAllow(n); err == nil && other.Has(cat) {
					implied = true
				}
			}
			if implied {
				continue // by construction, not by accident — see the doc above
			}
			exercised++
			got, err := gooey.ParseAllow(sethandlers.Canonical(rest))
			if err != nil {
				t.Errorf("%q minus %q does not parse: %v", name, prim, err)
				continue
			}
			if got.Has(cat) {
				t.Errorf("sets:Without over group %q cannot remove %s: the result "+
					"%q still grants it. A page asking for everything except %s "+
					"gets %s — the expansion is opaque to the name algebra that "+
					"consumes it", name, prim, sethandlers.Canonical(rest), prim, prim)
			}

			// And sets:Has must agree the expansion holds it.
			if !strings.Contains(" "+sethandlers.Canonical(names)+" ", " "+prim+" ") {
				t.Errorf("group %q contains %s, but its expansion %q does not name "+
					"it, so sets:Has answers false for a primitive the group holds",
					name, prim, sethandlers.Canonical(names))
			}
		}
	}
	if exercised == 0 {
		t.Fatal("no group yielded a removable primitive — this test proves nothing")
	}
	t.Logf("exercised %d group/primitive pairs", exercised)
}

// TestNoGroupRendersAsNothing is the other half, and the failure it
// guards is the opposite direction.
//
// A group whose expansion is the empty string does not read as an empty
// set in markup — it reads as an attribute that was NOT WRITTEN, i.e. no
// <Frozen> at all. That is the widest reading of the narrowest set, and
// it is what "None" did before it was given its own token.
func TestNoGroupRendersAsNothing(t *testing.T) {
	for name, names := range gooey.AllowGroups() {
		if got := sethandlers.Canonical(names); got == "" {
			t.Errorf("sets:Group %q renders as the empty string, which markup "+
				"reads as an unwritten attribute — not as a seal", name)
		}
	}
}
