package components

import (
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/prop"
)

func btn(label string, click gooey.Command) *Button {
	return &Button{Content: Str(label), Click: click}
}

func TestFocusOrderIsTreeOrder(t *testing.T) {
	a, b, c := btn("a", nil), btn("b", nil), btn("c", nil)
	root := &VStack{Children: []gooey.Component{
		a,
		&HStack{Children: []gooey.Component{&Text{Content: Str("label")}, b}},
		c,
	}}
	m := gooey.NewFocusManager(root)

	if got := len(m.Order()); got != 3 {
		t.Fatalf("focus order has %d stops, want 3 (Text is not focusable)", got)
	}
	if m.Focused() != gooey.Component(a) {
		t.Fatalf("initial focus = %v, want the first stop", m.Focused())
	}
	want := []gooey.Component{b, c, a} // wraps
	for i, w := range want {
		m.FocusNext()
		if m.Focused() != w {
			t.Fatalf("FocusNext #%d = %v, want %v", i+1, m.Focused(), w)
		}
	}
	m.FocusPrev()
	if m.Focused() != gooey.Component(c) {
		t.Fatalf("FocusPrev = %v, want c", m.Focused())
	}
}

func TestFocusSkipsCollapsedSubtrees(t *testing.T) {
	a, b, c := btn("a", nil), btn("b", nil), btn("c", nil)
	hidden := &VStack{Children: []gooey.Component{b}}
	hidden.LayoutProps().Visibility = gooey.Collapsed
	root := &VStack{Children: []gooey.Component{a, hidden, c}}
	m := gooey.NewFocusManager(root)

	m.FocusNext()
	if m.Focused() != gooey.Component(c) {
		t.Fatalf("focus = %v, want c (b is inside a collapsed subtree)", m.Focused())
	}
}

// Focus is a paint dependency, not a global invalidation: moving it must
// repaint the component losing focus and the one gaining it, nothing else.
func TestFocusMoveDamageIsTwoComponents(t *testing.T) {
	root := &VStack{Children: []gooey.Component{btn("a", nil), btn("b", nil), btn("c", nil)}}
	c := gooey.NewComposer(root, 20, 5)
	if _, painted := c.Frame(); painted != 4 {
		t.Fatalf("first frame painted %d components, want 4", painted)
	}
	c.Focus().FocusNext()
	if _, painted := c.Frame(); painted != 2 {
		t.Fatalf("focus move painted %d components, want 2", painted)
	}
	if _, painted := c.Frame(); painted != 0 {
		t.Fatalf("clean frame painted %d components, want 0", painted)
	}
}

func TestButtonActivatesOnEnterAndSpace(t *testing.T) {
	clicks := 0
	b := btn("save", func() { clicks++ })
	c := gooey.NewComposer(&VStack{Children: []gooey.Component{b}}, 20, 3)

	if !c.HandleKey(input.Named(input.KeyEnter)) {
		t.Fatal("enter was not handled by the focused button")
	}
	if !c.HandleKey(input.Rune(' ')) {
		t.Fatal("space was not handled by the focused button")
	}
	if clicks != 2 {
		t.Fatalf("click ran %d times, want 2", clicks)
	}
}

func TestTabMovesFocusByDefault(t *testing.T) {
	a, b := btn("a", nil), btn("b", nil)
	c := gooey.NewComposer(&VStack{Children: []gooey.Component{a, b}}, 20, 3)

	if !c.HandleKey(input.Named(input.KeyTab)) {
		t.Fatal("tab was not handled")
	}
	if c.Focus().Focused() != gooey.Component(b) {
		t.Fatal("tab did not advance focus")
	}
	c.HandleKey(input.KeyEvent{Key: input.KeyTab, Mods: input.ModShift})
	if c.Focus().Focused() != gooey.Component(a) {
		t.Fatal("shift+tab did not move focus back")
	}
}

// keyPane is a stand-in for an app component with view-local key handling.
type keyPane struct {
	gooey.Base
	gooey.FocusState
	moved int
}

func (p *keyPane) Measure(avail gooey.Size) gooey.Size { return avail }
func (p *keyPane) Render(f *gooey.Frame)               { p.IsFocused() }

func (p *keyPane) HandleKey(ev input.KeyEvent) bool {
	if ev == input.Rune('j') {
		p.moved++
		return true
	}
	return false
}

func TestKeyRoutingFocusedThenBindings(t *testing.T) {
	pane := &keyPane{}
	fired := 0
	root := &VStack{Children: []gooey.Component{pane}}
	root.Attach(&gooey.KeyBinding{Gesture: input.Rune('j'), Command: func() { fired++ }})
	root.Attach(&gooey.KeyBinding{Gesture: input.Rune('q'), Command: func() { fired++ }})
	c := gooey.NewComposer(root, 20, 5)

	// The focused component consumes j, so the page binding never sees it.
	if !c.HandleKey(input.Rune('j')) {
		t.Fatal("j was not handled")
	}
	if pane.moved != 1 || fired != 0 {
		t.Fatalf("pane.moved=%d fired=%d — j should stop at the focused component", pane.moved, fired)
	}
	// q is not handled locally, so it bubbles to the page binding.
	if !c.HandleKey(input.Rune('q')) {
		t.Fatal("q was not handled")
	}
	if fired != 1 {
		t.Fatalf("page binding fired %d times, want 1", fired)
	}
	if c.HandleKey(input.Rune('z')) {
		t.Fatal("unbound key reported as handled")
	}
}

// A binding attached inside a subtree only fires while that subtree
// holds focus — the scoping that lets a control own its own gestures.
func TestBindingScopeFollowsAncestorChain(t *testing.T) {
	inner, outer := &keyPane{}, &keyPane{}
	scoped := &VStack{Children: []gooey.Component{inner}}
	fired := 0
	scoped.Attach(&gooey.KeyBinding{Gesture: input.Named(input.KeyEnter), Command: func() { fired++ }})
	c := gooey.NewComposer(&VStack{Children: []gooey.Component{scoped, outer}}, 20, 6)

	c.HandleKey(input.Named(input.KeyEnter))
	if fired != 1 {
		t.Fatalf("scoped binding fired %d times with focus inside, want 1", fired)
	}
	c.Focus().SetFocus(outer)
	if c.HandleKey(input.Named(input.KeyEnter)) {
		t.Fatal("scoped binding fired with focus outside its subtree")
	}
	if fired != 1 {
		t.Fatalf("fired = %d, want 1", fired)
	}
}

func TestFocusStateDamagesOnlyItsReader(t *testing.T) {
	// A component that never reads IsFocused must not repaint on focus moves.
	quiet := &Text{Content: prop.NewSource("static")}
	a, b := btn("a", nil), btn("b", nil)
	c := gooey.NewComposer(&VStack{Children: []gooey.Component{quiet, a, b}}, 20, 5)
	c.Frame()
	c.Focus().FocusNext()
	if _, painted := c.Frame(); painted != 2 {
		t.Fatalf("painted %d components, want 2 (the Text must not repaint)", painted)
	}
}
