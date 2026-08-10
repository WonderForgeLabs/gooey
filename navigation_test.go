package gooey

import (
	"testing"

	"github.com/WonderForgeLabs/gooey/input"
)

// arrowEater stands in for a list pane: it consumes arrows for its own
// cursor, so directional navigation must never see them.
type arrowEater struct {
	Base
	FocusState
	moved int
}

func (a *arrowEater) Measure(avail Size) Size { return Size{avail.W, min(1, avail.H)} }
func (a *arrowEater) Render(f *Frame)         { a.IsFocused() }

func (a *arrowEater) HandleKey(ev input.KeyEvent) bool {
	if ev == input.Named(input.KeyUp) || ev == input.Named(input.KeyDown) {
		a.moved++
		return true
	}
	return false
}

func TestArrowsMoveFocusWhenUnconsumed(t *testing.T) {
	a, b := btn("a", nil), btn("b", nil)
	c := NewComposer(&VStack{Children: []Widget{a, b}}, 20, 4)
	c.Frame()

	if !c.HandleKey(input.Named(input.KeyDown)) {
		t.Fatal("down arrow was not handled")
	}
	if c.Focus().Focused() != Widget(b) {
		t.Fatal("down arrow did not move focus to the widget below")
	}
	c.HandleKey(input.Named(input.KeyUp))
	if c.Focus().Focused() != Widget(a) {
		t.Fatal("up arrow did not move focus back")
	}
}

// The ordering is the whole point: a widget that handles arrows keeps
// them, and focus does not move underneath it.
func TestArrowsDoNotMoveFocusWhenConsumed(t *testing.T) {
	eater := &arrowEater{}
	other := btn("b", nil)
	c := NewComposer(&VStack{Children: []Widget{eater, other}}, 20, 4)
	c.Frame()
	if c.Focus().Focused() != Widget(eater) {
		t.Fatal("expected the list pane to start focused")
	}
	for i := 0; i < 3; i++ {
		c.HandleKey(input.Named(input.KeyDown))
	}
	if eater.moved != 3 {
		t.Fatalf("consumer saw %d arrows, want 3", eater.moved)
	}
	if c.Focus().Focused() != Widget(eater) {
		t.Fatal("focus moved even though the focused widget consumed the arrow")
	}
	// A direction it does NOT consume still navigates.
	c.HandleKey(input.Named(input.KeyRight))
	if c.Focus().Focused() != Widget(other) {
		t.Fatal("an unconsumed arrow direction did not navigate")
	}
}

// Spatial navigation picks the neighbour in the arrow's direction, not
// merely the next one in tree order.
func TestArrowNavigationIsSpatial(t *testing.T) {
	left, mid, right := btn("l", nil), btn("m", nil), btn("r", nil)
	below := btn("b", nil)
	below.LayoutProps().Row = 1
	row := &HStack{Children: []Widget{left, mid, right}, Gap: 2}
	root := &Grid{Rows: []GridLen{Auto(), Auto()}, Children: []Widget{row, below}}
	c := NewComposer(root, 40, 4)
	c.Frame()

	c.Focus().SetFocus(right)
	c.HandleKey(input.Named(input.KeyLeft))
	if c.Focus().Focused() != Widget(mid) {
		t.Fatal("left arrow did not pick the horizontal neighbour")
	}
	c.HandleKey(input.Named(input.KeyLeft))
	if c.Focus().Focused() != Widget(left) {
		t.Fatal("left arrow did not continue along the row")
	}
	c.HandleKey(input.Named(input.KeyDown))
	if c.Focus().Focused() != Widget(below) {
		t.Fatal("down arrow did not reach the widget on the row below")
	}
}

// Nothing in that direction falls back to tree order rather than
// stranding focus.
func TestArrowFallsBackToTreeOrder(t *testing.T) {
	a, b := btn("a", nil), btn("b", nil)
	c := NewComposer(&HStack{Children: []Widget{a, b}, Gap: 1}, 20, 3)
	c.Frame()
	c.Focus().SetFocus(a)
	c.HandleKey(input.Named(input.KeyUp)) // nothing is above
	if c.Focus().Focused() == Widget(a) {
		t.Fatal("focus did not move at all; the fallback should cycle")
	}
}

// The reader's shape: three Border-wrapped focusable leaves in a Grid.
// Clicking anywhere in a pane — interior OR its title/border chrome —
// must focus that pane, and the wheel must follow the pointer.
func TestReaderShapeClickAndWheelRouting(t *testing.T) {
	a, b, cc := &pane{}, &pane{}, &pane{}
	wrap := func(w Widget, col int) Widget {
		bd := &Border{Child: w, Title: Str("t")}
		bd.LayoutProps().Col = col
		return bd
	}
	inner := &Grid{
		Cols:     []GridLen{Fixed(26), Star(1), Star(2)},
		Children: []Widget{wrap(a, 0), wrap(b, 1), wrap(cc, 2)},
	}
	status := &Text{Content: Str("status")}
	status.LayoutProps().Row = 1
	comp := NewComposer(&Grid{Rows: []GridLen{Star(1), Auto()}, Children: []Widget{inner, status}}, 110, 16)
	comp.Frame()

	mid := b.Bounds()
	comp.HandleMouse(press(mid.X+2, mid.Y))
	if comp.Focus().Focused() != Widget(b) {
		t.Fatalf("clicking the middle pane's interior focused %p, want %p", comp.Focus().Focused(), b)
	}

	// The title row is the Border's own chrome — the focusable content
	// is a descendant, not an ancestor.
	comp.Focus().SetFocus(a)
	comp.HandleMouse(press(mid.X+2, mid.Y-1))
	if comp.Focus().Focused() != Widget(b) {
		t.Fatalf("clicking the middle pane's title focused %p, want %p", comp.Focus().Focused(), b)
	}

	// Wheel follows the pointer, not focus: focus is on the middle pane.
	third := cc.Bounds()
	comp.HandleMouse(input.MouseEvent{Kind: input.WheelDown, Button: input.ButtonNone, X: third.X + 2, Y: third.Y})
	if len(cc.got) == 0 || cc.got[len(cc.got)-1] != input.WheelDown {
		t.Fatalf("wheel over the third pane went elsewhere (a=%v b=%v c=%v)", a.got, b.got, cc.got)
	}
	if comp.Focus().Focused() != Widget(b) {
		t.Fatal("wheel moved focus; it must not")
	}
}
