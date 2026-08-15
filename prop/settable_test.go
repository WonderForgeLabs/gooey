package prop_test

import (
	"testing"

	"github.com/WonderForgeLabs/gooey/prop"
)

// Settable is the primitive a caller needs to reject a mutation against a
// derived property at LOAD time rather than panicking on the first click.
func TestSettableTellsASourceFromAComputed(t *testing.T) {
	src := prop.NewSource(1)
	if !src.Settable() {
		t.Fatal("a source reports itself unsettable — every mutation would be rejected")
	}
	derived := prop.NewComputed(func() int { return src.Get() * 2 })
	if derived.Settable() {
		t.Fatal("a computed reports itself settable — a Set on it panics, which is what this exists to prevent")
	}
}

// The near-miss that motivated the method. Evals() looks like it could
// answer the same question and cannot: a computed that has been read once
// is CLEAN and does not re-evaluate, so its eval count stops moving and
// becomes indistinguishable from a source's.
func TestEvalsCannotSubstituteForSettable(t *testing.T) {
	src := prop.NewSource(1)
	derived := prop.NewComputed(func() int { return src.Get() * 2 })

	derived.Get() // evaluate once; now clean
	before := derived.Evals()
	derived.Get()
	if derived.Evals() != before {
		t.Fatal("a clean computed re-evaluated; this test's premise is wrong, not the code")
	}
	// Both now sit still under repeated reads, which is exactly why the
	// eval count cannot discriminate and Settable has to exist.
	sBefore := src.Evals()
	src.Get()
	if src.Evals() != sBefore {
		t.Fatal("a source's eval count moved on Get")
	}
	if derived.Settable() == src.Settable() {
		t.Fatal("Settable failed to discriminate two properties their eval counts cannot tell apart")
	}
}

// Settable must agree with what Set actually does — the method is only
// worth anything if it predicts the panic rather than restating a guess.
func TestSettableAgreesWithSet(t *testing.T) {
	src := prop.NewSource(1)
	if !src.Settable() {
		t.Fatal("premise")
	}
	src.Set(2) // must not panic
	if src.Get() != 2 {
		t.Fatalf("source reads %d after Set(2)", src.Get())
	}

	derived := prop.NewComputed(func() int { return src.Get() * 2 })
	func() {
		defer func() {
			if recover() == nil {
				t.Error("Set on a computed did not panic, so Settable()==false is describing a rule that no longer holds")
			}
		}()
		derived.Set(9)
	}()
}
