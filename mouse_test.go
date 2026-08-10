package gooey

import (
	"testing"

	"github.com/WonderForgeLabs/gooey/input"
)

func press(x, y int) input.MouseEvent {
	return input.MouseEvent{Kind: input.MousePress, Button: input.ButtonLeft, X: x, Y: y}
}

func release(x, y int) input.MouseEvent {
	return input.MouseEvent{Kind: input.MouseRelease, Button: input.ButtonLeft, X: x, Y: y}
}

func move(x, y int) input.MouseEvent {
	return input.MouseEvent{Kind: input.MouseMove, Button: input.ButtonNone, X: x, Y: y}
}

// pane is an app-style widget: focusable, hoverable, and it records the
// pointer events it consumes.
type pane struct {
	Base
	FocusState
	HoverState
	got []input.MouseKind
}

func (p *pane) Measure(avail Size) Size { return Size{avail.W, min(1, avail.H)} }
func (p *pane) Render(f *Frame)         { p.IsFocused(); p.IsHovered() }

func (p *pane) HandleMouse(ev input.MouseEvent) bool {
	p.got = append(p.got, ev.Kind)
	return true
}

func TestHitTestDeepestWins(t *testing.T) {
	inner := &Text{Content: Str("x")}
	nested := &VStack{Children: []Widget{inner}}
	sibling := &Text{Content: Str("y")}
	root := &HStack{Children: []Widget{nested, sibling}}
	m := NewFocusManager(root)
	root.Measure(Size{20, 4})
	root.Arrange(Rect{0, 0, 20, 4})

	if got := m.HitTest(inner.Bounds().X, inner.Bounds().Y); got != Widget(inner) {
		t.Fatalf("hit inside nested = %T, want the innermost Text", got)
	}
	if got := m.HitTest(sibling.Bounds().X, sibling.Bounds().Y); got != Widget(sibling) {
		t.Fatalf("hit on sibling = %T, want the sibling Text", got)
	}
	if got := m.HitTest(500, 500); got != nil {
		t.Fatalf("hit outside the tree = %T, want nil", got)
	}
}

// Overlapping children resolve in paint order: the later sibling is on
// top, so it takes the hit.
func TestHitTestOverlapPrefersLastPainted(t *testing.T) {
	under, over := &pane{}, &pane{}
	root := &Grid{Children: []Widget{under, over}} // one cell, both fill it
	m := NewFocusManager(root)
	root.Measure(Size{10, 3})
	root.Arrange(Rect{0, 0, 10, 3})

	if got := m.HitTest(5, 1); got != Widget(over) {
		t.Fatalf("overlap hit = %p, want the last child %p", got, over)
	}
}

func TestHitTestSkipsCollapsed(t *testing.T) {
	hidden, shown := &pane{}, &pane{}
	hidden.LayoutProps().Visibility = Collapsed
	root := &Grid{Children: []Widget{shown, hidden}} // hidden would be on top
	m := NewFocusManager(root)
	root.Measure(Size{10, 3})
	root.Arrange(Rect{0, 0, 10, 3})

	if got := m.HitTest(5, 1); got != Widget(shown) {
		t.Fatalf("hit = %p, want the visible widget %p", got, shown)
	}
}

func TestPressMovesFocus(t *testing.T) {
	top, bottom := &pane{}, &pane{}
	c := NewComposer(&VStack{Children: []Widget{top, bottom}}, 20, 4)
	c.Frame()
	if c.Focus().Focused() != Widget(top) {
		t.Fatal("focus should start on the first pane")
	}
	c.HandleMouse(press(3, bottom.Bounds().Y))
	if c.Focus().Focused() != Widget(bottom) {
		t.Fatal("press did not move focus to the widget under the pointer")
	}
	// Focus damage is the same two-widget repaint as tab.
	if _, painted := c.Frame(); painted != 2 {
		t.Fatalf("focus-follows-click painted %d widgets, want 2", painted)
	}
}

func TestWheelGoesToPointerNotFocus(t *testing.T) {
	top, bottom := &pane{}, &pane{}
	c := NewComposer(&VStack{Children: []Widget{top, bottom}}, 20, 4)
	c.Frame() // focus is on top

	c.HandleMouse(input.MouseEvent{Kind: input.WheelDown, X: 3, Y: bottom.Bounds().Y})
	if len(bottom.got) != 1 || bottom.got[0] != input.WheelDown {
		t.Fatalf("wheel went to %v/%v — it must follow the pointer", top.got, bottom.got)
	}
	if len(top.got) != 0 {
		t.Fatal("wheel reached the focused widget instead of the hovered one")
	}
	if c.Focus().Focused() != Widget(top) {
		t.Fatal("wheel moved focus; it must not")
	}
}

func TestHoverTransitionFlipsExactlyTwo(t *testing.T) {
	top, bottom := &pane{}, &pane{}
	c := NewComposer(&VStack{Children: []Widget{top, bottom}}, 20, 4)
	c.Frame()

	c.HandleMouse(move(3, top.Bounds().Y))
	if !top.IsHovered() || bottom.IsHovered() {
		t.Fatalf("hover = top:%v bottom:%v, want top only", top.IsHovered(), bottom.IsHovered())
	}
	if _, painted := c.Frame(); painted != 1 {
		t.Fatalf("entering the first widget painted %d, want 1", painted)
	}
	c.HandleMouse(move(3, bottom.Bounds().Y))
	if top.IsHovered() || !bottom.IsHovered() {
		t.Fatalf("hover = top:%v bottom:%v, want bottom only", top.IsHovered(), bottom.IsHovered())
	}
	if _, painted := c.Frame(); painted != 2 {
		t.Fatalf("hover move painted %d widgets, want 2 (leave + enter)", painted)
	}
	// Moving within the same widget is not a transition: no damage.
	c.HandleMouse(move(5, bottom.Bounds().Y))
	if _, painted := c.Frame(); painted != 0 {
		t.Fatalf("moving inside one widget painted %d, want 0", painted)
	}
}

// Raw motion is not delivered to ordinary widgets — only enter/leave is.
func TestMoveNotDeliveredWithoutMoveHandler(t *testing.T) {
	p := &pane{}
	c := NewComposer(&VStack{Children: []Widget{p}}, 20, 4)
	c.Frame()
	c.HandleMouse(move(3, p.Bounds().Y))
	if len(p.got) != 0 {
		t.Fatalf("widget received raw motion %v; it did not ask for it", p.got)
	}
}

func TestClickSynthesis(t *testing.T) {
	top, bottom := &pane{}, &pane{}
	c := NewComposer(&VStack{Children: []Widget{top, bottom}}, 20, 4)
	c.Frame()

	ty, by := top.Bounds().Y, bottom.Bounds().Y
	c.HandleMouse(press(2, ty))
	c.HandleMouse(release(2, ty))
	want := []input.MouseKind{input.MousePress, input.MouseRelease, input.MouseClick}
	if len(top.got) != 3 || top.got[2] != input.MouseClick {
		t.Fatalf("press+release on one widget gave %v, want %v", top.got, want)
	}

	// Press on one widget, release on another: the release still goes to
	// the pressed widget (implicit capture) but no Click is synthesized.
	top.got, bottom.got = nil, nil
	c.HandleMouse(press(2, ty))
	c.HandleMouse(release(2, by))
	if len(top.got) != 2 || top.got[1] != input.MouseRelease {
		t.Fatalf("press target got %v, want press then release", top.got)
	}
	for _, k := range append(top.got, bottom.got...) {
		if k == input.MouseClick {
			t.Fatal("Click synthesized across two different widgets")
		}
	}
}

func TestButtonClickAndVisualStates(t *testing.T) {
	clicks := 0
	b := &Button{Content: Str("save"), Click: func() { clicks++ }}
	c := NewComposer(&VStack{Children: []Widget{b}}, 20, 3)
	c.Frame()

	x, y := b.Bounds().X, b.Bounds().Y
	c.HandleMouse(move(x, y))
	if !b.IsHovered() {
		t.Fatal("button did not take hover")
	}
	c.HandleMouse(press(x, y))
	if !b.pressed().Get() {
		t.Fatal("button did not enter its pressed state")
	}
	if _, painted := c.Frame(); painted != 1 {
		t.Fatalf("pressed state painted %d widgets, want 1", painted)
	}
	c.HandleMouse(release(x, y))
	if b.pressed().Get() {
		t.Fatal("button stayed pressed after release")
	}
	if clicks != 1 {
		t.Fatalf("click ran %d times, want 1", clicks)
	}
	// A release that wanders off still clears the pressed visual, and
	// does not fire the command.
	c.HandleMouse(press(x, y))
	c.HandleMouse(release(x, y+2))
	if b.pressed().Get() {
		t.Fatal("pressed visual stuck after releasing off the button")
	}
	if clicks != 1 {
		t.Fatalf("click ran %d times, want 1 (drag-off must not activate)", clicks)
	}
}

// Hover attaches to the nearest hoverable ancestor, so a container can
// highlight while the pointer is over a plain child.
func TestHoverUsesNearestHoverableAncestor(t *testing.T) {
	child := &Text{Content: Str("inside")}
	p := &pane{}
	hoverable := &hoverBox{Child: child}
	c := NewComposer(&VStack{Children: []Widget{hoverable, p}}, 20, 4)
	c.Frame()

	c.HandleMouse(move(1, child.Bounds().Y))
	if c.Focus().HitTest(1, child.Bounds().Y) != Widget(child) {
		t.Fatal("expected the hit to land on the plain child")
	}
	if !hoverable.IsHovered() {
		t.Fatal("hover did not attach to the hoverable ancestor")
	}
}

type hoverBox struct {
	Base
	HoverState
	Child Widget
}

func (h *hoverBox) ChildWidgets() []Widget  { return []Widget{h.Child} }
func (h *hoverBox) Measure(avail Size) Size { return MeasureChild(h.Child, avail) }
func (h *hoverBox) Arrange(r Rect)          { h.Base.Arrange(r); ArrangeChild(h.Child, r) }
func (h *hoverBox) Render(f *Frame)         { h.IsHovered() }
