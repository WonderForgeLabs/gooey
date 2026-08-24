package markup

import (
	"testing"
)

// The placeholder table has two consumers that used to be written out
// separately — a switch and a hand-maintained list of the same type
// names. Nothing pinned them together, and the drift was silent: the
// only symptom would have been the error at the bottom of `placeholder`
// naming fewer types than exist, sending an author to write a literal
// for something already supported.
//
// These are the tests that make the single-table version stay single.

// TestPlaceholderTypesAndPlaceholderForAgreeBothWays.
//
// BOTH directions, because only one of them bites. "Every listed type
// resolves" catches a list entry with no implementation — the harmless
// direction. The one that matters is the reverse: an implementation
// missing from the list, which is what a stale mirror actually produces.
// A one-directional test would have passed against the exact bug this
// replaces.
func TestPlaceholderTypesAndPlaceholderForAgreeBothWays(t *testing.T) {
	listed := PlaceholderTypes()
	if len(listed) == 0 {
		t.Fatal("PlaceholderTypes is empty, so every assertion below is vacuous")
	}

	inList := map[string]bool{}
	for _, ty := range listed {
		inList[ty] = true
		if PlaceholderFor(ty) == nil {
			t.Errorf("PlaceholderTypes lists %q but PlaceholderFor returns nil for it: "+
				"the error message sends authors to a type nothing can seed", ty)
		}
	}
	for ty := range placeholders {
		if !inList[ty] {
			t.Errorf("PlaceholderFor answers for %q but PlaceholderTypes omits it: "+
				"the \"it knows …\" error under-reports the vocabulary, so an author "+
				"writes a literal for a type that is already supported", ty)
		}
	}
}

// TestPlaceholderTypesIsSorted — the error message interpolates it, and
// a set iterated in map order would reorder that message run to run.
func TestPlaceholderTypesIsSorted(t *testing.T) {
	got := PlaceholderTypes()
	for i := 1; i < len(got); i++ {
		if got[i-1] > got[i] {
			t.Fatalf("PlaceholderTypes is not sorted: %q precedes %q", got[i-1], got[i])
		}
	}
}

// TestEachPlaceholderCallIsAFreshProperty is the one a values-map would
// fail.
//
// Two seeded elements sharing one source property is the same defect the
// {{.Name_Attr}} rename exists to prevent, and it is silent in the same
// way: both documents load, and one checkbox ticks the other. Storing
// values in the table instead of factories would hand the same pointer
// to every caller.
func TestEachPlaceholderCallIsAFreshProperty(t *testing.T) {
	for _, ty := range PlaceholderTypes() {
		a, b := PlaceholderFor(ty), PlaceholderFor(ty)
		if a == nil || b == nil {
			t.Fatalf("PlaceholderFor(%q) returned nil", ty)
		}
		if a == b {
			t.Errorf("two PlaceholderFor(%q) calls returned the SAME property: "+
				"two seeded elements would share state, so editing one changes "+
				"the other and both documents still load", ty)
		}
	}
}

// TestPlaceholderForRejectsAnUnknownType — the guard has to say no as
// well as yes, and callers are documented to treat nil as an error
// rather than as an empty binding.
func TestPlaceholderForRejectsAnUnknownType(t *testing.T) {
	if h := PlaceholderFor("chan struct{}"); h != nil {
		t.Errorf("PlaceholderFor invented a %T for an unsupported type", h)
	}
}
