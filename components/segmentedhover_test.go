package components

import (
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/prop"
)

// Segmented reports WHICH segment the pointer is over (#398).
//
// gooey.HoverState answers "is the pointer on me" — one bool for the
// whole strip, which is the right answer for a Button and useless for a
// rail of wordless icons, where a tooltip is the only affordance that can
// say what a slot does and it needs to know which slot.

// vrail is the shape the issue is about: a vertical Child-bearing strip,
// four slots, two cells each.
func vrail(t *testing.T) *Segmented {
	t.Helper()
	s := &Segmented{
		Selected: prop.NewSource(0),
		Hovered:  prop.NewSource(-1),
		Vertical: true,
		Child:    &Text{Content: Str("")},
		Count:    4,
	}
	s.Arrange(gooey.Rect{X: 0, Y: 0, W: 4, H: 8})
	return s
}

func moveOver(s *Segmented, x, y int) {
	s.HandleMouseMove(input.MouseEvent{Kind: input.MouseMove, X: x, Y: y})
}

func TestSegmentedReportsWhichSegmentIsHovered(t *testing.T) {
	s := vrail(t)
	// Four slots over eight rows: rows 0-1, 2-3, 4-5, 6-7.
	for _, c := range []struct {
		y, want int
	}{{0, 0}, {1, 0}, {2, 1}, {3, 1}, {4, 2}, {6, 3}, {7, 3}} {
		moveOver(s, 1, c.y)
		if got := s.Hovered.Get(); got != c.want {
			t.Errorf("pointer at row %d reports segment %d, want %d", c.y, got, c.want)
		}
	}
	// Outside the strip is -1, not a clamped edge segment: "nowhere" and
	// "the last one" are different answers and a tooltip needs to tell
	// them apart.
	moveOver(s, 9, 3)
	if got := s.Hovered.Get(); got != -1 {
		t.Errorf("pointer outside the strip reports segment %d, want -1", got)
	}
}

// THE LEAVE EDGE IS THE HALF MOTION CANNOT REACH, and it is the one a
// naive implementation gets wrong.
//
// FocusManager.setHover (mouse.go:473) drives hover on the hit, so when
// the pointer moves from this strip onto a SIBLING, the motion event
// routes to the sibling and this control never hears another one. Without
// the SetHovered(false) hook the index stays pointing at whichever
// segment the pointer left by — forever, and a tooltip keyed on it
// describes a slot the pointer is nowhere near.
func TestLeavingTheStripClearsTheHoveredSegment(t *testing.T) {
	s := vrail(t)
	moveOver(s, 1, 5)
	if got := s.Hovered.Get(); got != 2 {
		t.Fatalf("setup: hovered = %d, want 2", got)
	}
	// The router's leave edge, with NO further motion — which is exactly
	// what happens when the pointer crosses onto a sibling.
	s.SetHovered(false)
	if got := s.Hovered.Get(); got != -1 {
		t.Errorf("after the pointer left, hovered is still %d — motion never "+
			"comes back to clear it, so the leave edge is the only chance", got)
	}
	// And the bool half still works: this extends HoverState, it does not
	// replace it.
	if s.IsHovered() {
		t.Error("IsHovered is still true after SetHovered(false)")
	}
	s.SetHovered(true)
	if !s.IsHovered() {
		t.Error("IsHovered did not come back on")
	}
}

// Motion is OBSERVED, NEVER CONSUMED. Returning true would stop the event
// bubbling to ancestors (mouse.go:267) — which is what a drag, a marquee
// or an outer hover watcher is listening for. A control that reports its
// own hover has no business ending someone else's gesture.
func TestHoverTrackingDoesNotConsumeMotion(t *testing.T) {
	s := vrail(t)
	for _, y := range []int{0, 3, 7, 99} {
		if s.HandleMouseMove(input.MouseEvent{Kind: input.MouseMove, X: 1, Y: y}) {
			t.Errorf("motion at row %d was consumed", y)
		}
	}
}

// THE DAMAGE CONTRACT, and motion is where it matters most: it is the
// highest-frequency event there is, so an unguarded Set here invalidates
// every dependent on every pointer twitch.
//
// The instrument is a COMPUTED DEPENDENT, not the source. prop.Set
// invalidates a source's dependents (prop/prop.go) and never marks the
// source's own node dirty, so OnInvalidate hung on s.Hovered would never
// fire and the test would read 0 whether the guard existed or not.
// node.invalidate() also latches on dirty, so the watcher is re-read
// between moves.
func TestMovingWithinOneSegmentCostsNothing(t *testing.T) {
	s := vrail(t)
	// SETTLE INTO A SEGMENT FIRST. Starting at -1 makes the first move a
	// real change, so a counter registered before it measures the setup
	// rather than the property under test.
	moveOver(s, 1, 4)

	seen := prop.NewComputed(func() int { return s.Hovered.Get() })
	seen.Get() // record the dependency AND go clean

	writes := 0
	seen.OnInvalidate(func() { writes++ })

	// Rows 4 and 5 are the SAME segment, so crossing between them must
	// publish nothing.
	for range 5 {
		moveOver(s, 1, 4)
		seen.Get()
		moveOver(s, 1, 5)
		seen.Get()
	}
	if writes != 0 {
		t.Errorf("ten moves inside one segment invalidated %d times, want 0 — "+
			"prop.Set does not compare values, and motion is the highest-"+
			"frequency event there is", writes)
	}

	// And the guard is not "never write": crossing into another segment
	// still publishes. A guard that only says no is untested.
	moveOver(s, 1, 0)
	seen.Get()
	if writes != 1 {
		t.Errorf("crossing into another segment invalidated %d times, want "+
			"exactly 1", writes)
	}
}

// A strip whose Hovered was never supplied still works — the control
// makes its own, so a caller that does not care pays nothing and one that
// asks later finds a real handle.
func TestHoveredIsOptional(t *testing.T) {
	s := &Segmented{
		Selected: prop.NewSource(0),
		Vertical: true,
		Child:    &Text{Content: Str("")},
		Count:    2,
	}
	s.Arrange(gooey.Rect{X: 0, Y: 0, W: 4, H: 4})
	moveOver(s, 1, 3)
	if s.Hovered == nil {
		t.Fatal("the control did not create its own Hovered handle")
	}
	if got := s.Hovered.Get(); got != 1 {
		t.Errorf("hovered = %d, want 1", got)
	}
}
