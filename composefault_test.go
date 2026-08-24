package gooey

import (
	"testing"

	"github.com/WonderForgeLabs/gooey/term"
)

// layoutFault is package-level state, so the question "whose fault is
// this?" has to be answered by whoever drains it. Composer does, twice
// — at construction and in Frame. The one-shot Compose did not drain at
// all, and these are the two halves of that.

func testCaps() term.Caps { return term.Caps{Cols: 40, Rows: 10} }

// TestComposeReportsItsOwnFault is the must-say-yes half, and it is not
// decoration: before this, Compose had NOWHERE to put a fault. Draining
// without exposing would have fixed the leak below and silently made a
// one-shot compose of a cyclic tree indistinguishable from a clean one.
func TestComposeReportsItsOwnFault(t *testing.T) {
	TakeLayoutFault()

	f := Compose(selfCycle(), testCaps(), nil)
	if f.LayoutFault() == nil {
		t.Fatal("a one-shot Compose of a self-cycling tree reported no fault, " +
			"so the caller of the one-shot path has no way to learn its tree " +
			"was too deep to walk")
	}
}

// TestAOneShotComposeDoesNotLeakItsFaultIntoTheNextComposer is the
// finding.
//
// Compose's Measure, Arrange and renderTree can each record a fault.
// With nothing draining them, the fault stayed in the package global
// after Compose returned — and the very next Composer picked it up at
// CONSTRUCTION, where the same Take runs so that a cycle in the tree a
// Composer is built with is readable before the first frame.
//
// The two trees here share nothing. A fault reported for the second is
// necessarily the first one's.
func TestAOneShotComposeDoesNotLeakItsFaultIntoTheNextComposer(t *testing.T) {
	TakeLayoutFault()

	Compose(selfCycle(), testCaps(), nil)

	clean := chain(3)
	c := NewComposer(clean, 40, 10)
	if f := c.LayoutFault(); f != nil {
		t.Fatalf("a Composer built over an acyclic 3-deep tree already carried "+
			"a fault at construction: %v — inherited from the unrelated cyclic "+
			"tree a previous Compose left in the package global", f)
	}
	c.Frame()
	if f := c.LayoutFault(); f != nil {
		t.Errorf("an acyclic 3-deep tree reported %v after a frame", f)
	}
}

// TestComposeDoesNotInheritAFaultLeftByAnEarlierPass is the other
// direction, and it is why Compose takes on the way IN as well as out.
// A fault left by something else must not be attributed to this frame's
// tree.
func TestComposeDoesNotInheritAFaultLeftByAnEarlierPass(t *testing.T) {
	TakeLayoutFault()

	// Strand a fault without draining it, exactly as an earlier bad
	// compose would have.
	selfCycle().Measure(Size{80, 24})

	f := Compose(chain(3), testCaps(), nil)
	if got := f.LayoutFault(); got != nil {
		t.Errorf("Compose of an acyclic 3-deep tree reported %v, which belongs "+
			"to a tree it never saw", got)
	}
}
