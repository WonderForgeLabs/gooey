package components

import (
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/prop"
)

// Segmented gained a vertical axis and a Child, so that a strip of
// pixel-drawn icons could BE a Segmented rather than a second copy of its
// behaviour. These are the pins for the parts that were added; the
// horizontal label tier's own tests are elsewhere and unchanged, which is
// the point — the addition had to leave them alone.
//
// The bug that prompted it: an activity rail built as a bare Image could
// not be selected at all. A picture has no focus, no keys and no
// hit-testing. Each test below would have failed against that.

// railSeg is the activity rail's shape: four slots, vertical, a child
// picture standing in for the icon strip, arranged 4 cells by 8 rows.
func railSeg(t *testing.T) (*Segmented, *prop.Property[int]) {
	t.Helper()
	sel := prop.NewSource(0)
	s := &Segmented{
		Selected: sel,
		Vertical: true,
		Child:    &Text{Content: Str("")},
		Count:    4,
	}
	s.Arrange(gooey.Rect{X: 0, Y: 0, W: 4, H: 8})
	return s, sel
}

// TestAChildStripIsClickedByDividingTheBounds — with a Child there are no
// labels to measure, so the slot geometry is the bounds over Count. Two
// rows per slot here.
func TestAChildStripIsClickedByDividingTheBounds(t *testing.T) {
	s, sel := railSeg(t)
	for _, c := range []struct{ y, want int }{{0, 0}, {1, 0}, {2, 1}, {5, 2}, {7, 3}} {
		if !s.HandleMouse(input.MouseEvent{Kind: input.MouseClick, X: 1, Y: c.y}) {
			t.Errorf("a click at y=%d was not consumed", c.y)
		}
		if got := sel.Get(); got != c.want {
			t.Errorf("click at y=%d selected %d, want %d", c.y, got, c.want)
		}
	}
}

// TestAClickOutsideTheStripIsNotConsumed — the strip must not swallow
// events belonging to whatever sits beside it.
func TestAClickOutsideTheStripIsNotConsumed(t *testing.T) {
	s, sel := railSeg(t)
	for _, c := range []struct{ x, y int }{{9, 1}, {1, 9}, {-1, 1}} {
		if s.HandleMouse(input.MouseEvent{Kind: input.MouseClick, X: c.x, Y: c.y}) {
			t.Errorf("a click at (%d,%d) is outside and must not be consumed", c.x, c.y)
		}
	}
	if sel.Get() != 0 {
		t.Error("an outside click moved the selection")
	}
}

// TestTheVerticalAxisUsesUpAndDown — and the cross axis is deliberately
// left alone, so left/right reach spatial focus navigation and move OUT
// of a rail down the left edge. Without that it cannot be escaped
// sideways.
func TestTheVerticalAxisUsesUpAndDown(t *testing.T) {
	s, sel := railSeg(t)
	if !s.HandleKey(input.Named(input.KeyDown)) || sel.Get() != 1 {
		t.Errorf("down did not move the selection: sel=%d", sel.Get())
	}
	if !s.HandleKey(input.Named(input.KeyUp)) || sel.Get() != 0 {
		t.Errorf("up did not move it back: sel=%d", sel.Get())
	}
	for _, k := range []input.Key{input.KeyLeft, input.KeyRight} {
		if s.HandleKey(input.Named(k)) {
			t.Errorf("a vertical strip consumed %v; the cross axis belongs to focus navigation", k)
		}
	}
}

// TestAnEndOfTravelArrowIsNotConsumedVertically is the rocker rule on the
// new axis, which Wrap="false" now selects rather than it being universal.
func TestAnEndOfTravelArrowIsNotConsumedVertically(t *testing.T) {
	s, sel := railSeg(t)
	s.Wrap = NoWrapping
	if s.HandleKey(input.Named(input.KeyUp)) {
		t.Error("up at the first slot was consumed; the keyboard is trapped")
	}
	sel.Set(3)
	if s.HandleKey(input.Named(input.KeyDown)) {
		t.Error("down at the last slot was consumed; the keyboard is trapped")
	}
}

// TestTheRailWrapsAtBothEndsByDefault — reported against the running
// editor: going all the way down should return to the top.
//
// railSeg sets no Wrap, so this is the DEFAULT being asserted, not a
// configured mode. Both directions, because a modulo that forgets Go's %
// keeps the sign of its dividend wraps correctly downward and clamps
// upward — one direction passing proves nothing about the other.
func TestTheRailWrapsAtBothEndsByDefault(t *testing.T) {
	s, sel := railSeg(t)
	sel.Set(3)
	if !s.HandleKey(input.Named(input.KeyDown)) || sel.Get() != 0 {
		t.Errorf("down at the last slot left sel=%d; it must return to the first", sel.Get())
	}
	if !s.HandleKey(input.Named(input.KeyUp)) || sel.Get() != 3 {
		t.Errorf("up at the first slot left sel=%d; it must return to the last", sel.Get())
	}
}

// TestWrappingStillLeavesTheCrossAxisAlone is the load-bearing half of
// making wrapping the default: with the strip's own axis always consumed,
// the cross axis is the only arrow that can still move focus out. If this
// ever fails, a rail down the left edge has trapped the keyboard.
func TestWrappingStillLeavesTheCrossAxisAlone(t *testing.T) {
	s, _ := railSeg(t)
	for _, k := range []input.Key{input.KeyLeft, input.KeyRight} {
		if s.HandleKey(input.Named(k)) {
			t.Errorf("a wrapping vertical strip consumed %v; nothing can leave it sideways", k)
		}
	}
}

// TestTheWheelStepsTheSelection — reported against the running editor: the
// strip did not move with the scroll wheel. Tabs and ItemsView both handled
// the wheel; Segmented did not, so the rail was the one strip in the repo
// that ignored it.
func TestTheWheelStepsTheSelection(t *testing.T) {
	s, sel := railSeg(t)
	if !s.HandleMouse(input.MouseEvent{Kind: input.WheelDown, X: 1, Y: 1}) || sel.Get() != 1 {
		t.Errorf("a wheel-down notch left sel=%d, want 1", sel.Get())
	}
	if !s.HandleMouse(input.MouseEvent{Kind: input.WheelUp, X: 1, Y: 1}) || sel.Get() != 0 {
		t.Errorf("a wheel-up notch left sel=%d, want 0", sel.Get())
	}
	// Anywhere in the strip, not over a particular segment: the wheel is a
	// next/previous gesture. y=7 is the LAST slot, and a notch there must
	// still step by one rather than select the slot under the pointer.
	sel.Set(0)
	if !s.HandleMouse(input.MouseEvent{Kind: input.WheelDown, X: 1, Y: 7}) || sel.Get() != 1 {
		t.Errorf("a notch over the last slot selected %d; the wheel steps, it does not point", sel.Get())
	}
}

// TestTheWheelOutsideTheStripIsNotConsumed — a rail beside a scrollable
// pane must not eat that pane's scrolling.
func TestTheWheelOutsideTheStripIsNotConsumed(t *testing.T) {
	s, sel := railSeg(t)
	for _, c := range []struct{ x, y int }{{9, 1}, {1, 9}, {-1, 1}} {
		if s.HandleMouse(input.MouseEvent{Kind: input.WheelDown, X: c.x, Y: c.y}) {
			t.Errorf("a wheel notch at (%d,%d) is outside the strip and must not be consumed", c.x, c.y)
		}
	}
	if sel.Get() != 0 {
		t.Error("an outside wheel notch moved the selection")
	}
}

// TestAChildStripIsAContainerAndPaintsNothing — the child is the picture,
// so the strip must expose it to the tree and must not pre-clear over it.
func TestAChildStripIsAContainerAndPaintsNothing(t *testing.T) {
	s, _ := railSeg(t)
	ct, ok := gooey.Component(s).(gooey.Container)
	if !ok {
		t.Fatal("a Child-bearing Segmented must be a Container, or its picture is never composed")
	}
	if n := len(ct.ChildComponents()); n != 1 {
		t.Errorf("ChildComponents returned %d, want the one child", n)
	}
	// A leaf Segmented is still a leaf: adding the seam must not have made
	// every existing Segmented a container with a nil child.
	plain := &Segmented{Options: prop.NewSource([]string{"a", "b"}), Selected: prop.NewSource(0)}
	if kids := plain.ChildComponents(); len(kids) != 0 {
		t.Errorf("a label Segmented reports %d children; it is a leaf", len(kids))
	}
}

// TestCountComesFromTheChildOrTheLabels — Index clamps against whichever
// source is in use, and getting that wrong is a selection that silently
// sticks at 0.
func TestCountComesFromTheChildOrTheLabels(t *testing.T) {
	s, sel := railSeg(t)
	sel.Set(99)
	if got := s.Index(); got != 3 {
		t.Errorf("Index()=%d for a 4-slot child strip, want it clamped to 3", got)
	}
	plain := &Segmented{Options: prop.NewSource([]string{"a", "b"}), Selected: prop.NewSource(9)}
	if got := plain.Index(); got != 1 {
		t.Errorf("Index()=%d for two labels, want 1", got)
	}
}

// TestAVerticalLabelStripIsOnePerRow — the stacked tier without a Child,
// which is the shape a text-only side rail would use.
func TestAVerticalLabelStripIsOnePerRow(t *testing.T) {
	sel := prop.NewSource(0)
	s := &Segmented{
		Options:  prop.NewSource([]string{"one", "two", "three"}),
		Selected: sel,
		Vertical: true,
	}
	s.Arrange(gooey.Rect{X: 0, Y: 0, W: 9, H: 3})
	if !s.HandleMouse(input.MouseEvent{Kind: input.MouseClick, X: 2, Y: 2}) || sel.Get() != 2 {
		t.Errorf("a click on the third row selected %d, want 2", sel.Get())
	}
	if got := s.Measure(gooey.Size{W: 20, H: 20}); got.H != 3 {
		t.Errorf("a stacked strip of three measured %v high, want 3 rows", got.H)
	}
}
