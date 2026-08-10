package gooey

import (
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/prop"
)

// The input chapter: commands, focus, and routed key events.
//
// Commands are plain funcs bound from the viewmodel, so an event stays
// declarative in markup and needs no code-behind. Focus is framework-
// owned: the FocusManager walks the same tree the Composer does, holds
// the focused component, and moves with tab/shift+tab. A component learns it
// is focused by reading IsFocused() while painting, which makes focus a
// paint dependency like any other property — moving focus repaints the
// two components involved, not the screen.

// Command is a bound action. Markup event attributes (Button's Click,
// KeyBinding's Command) resolve to one of these, either from a func in
// the binding context — Click="{{.Save}}", the delegate living in the
// viewmodel — or from the code-behind handler registry by bare name.
type Command func()

// KeyHandler is the optional interface for components that consume keys.
// Returning true stops propagation.
type KeyHandler interface{ HandleKey(input.KeyEvent) bool }

// Focusable marks a component as a focus stop. Embedding FocusState is the
// easy way to implement it.
type Focusable interface{ AcceptsFocus() bool }

// FocusTarget is how the framework tells a component it gained or lost
// focus. Implementations must make the flag observable to Render.
type FocusTarget interface{ SetFocused(bool) }

// FocusState is the mixin that makes a component focusable. It keeps the
// framework-set flag in a source property, so a Render that reads
// IsFocused() picks up focus changes as damage — exactly the component
// losing focus and the one gaining it repaint.
type FocusState struct{ focused *prop.Property[bool] }

func (f *FocusState) AcceptsFocus() bool { return true }
func (f *FocusState) SetFocused(v bool)  { f.state().Set(v) }
func (f *FocusState) IsFocused() bool    { return f.state().Get() }

func (f *FocusState) state() *prop.Property[bool] {
	if f.focused == nil {
		f.focused = prop.NewSource(false)
	}
	return f.focused
}

// NonVisual marks elements that live in the tree for behavior only.
// The framework attaches them to their parent component instead of laying
// them out or painting them (see Base.Attach).
type NonVisual interface{ NonVisual() bool }

// KeyBinding is a declared gesture: <KeyBinding Gesture="ctrl+s"
// Command="{{.Save}}"/>. It hangs off its parent component as an
// attachment, and the dispatcher only reaches it while the focused
// component's ancestor chain passes through that parent — so a binding
// declared inside a control fires only while that control has focus,
// and one declared on the page root is global.
type KeyBinding struct {
	Base
	Gesture input.KeyEvent
	Command Command
}

func (k *KeyBinding) Measure(Size) Size { return Size{} }
func (k *KeyBinding) Render(*Frame)     {}
func (k *KeyBinding) NonVisual() bool   { return true }

// FocusManager is the input tree: focus order, parent links, and the
// KeyBindings attached along the way. It is built by the same walk the
// Composer does and owned by it (Composer.Focus).
type FocusManager struct {
	root     Component
	order    []Component
	parent   map[Component]Component
	bindings map[Component][]*KeyBinding
	cur      int

	hover   Component // current hover target, nil when the pointer is nowhere
	pressed Component // component a button went down on, until it comes up
}

// NewFocusManager walks root and focuses the first focus stop, so a page
// always has somewhere for keys to land.
func NewFocusManager(root Component) *FocusManager {
	m := &FocusManager{
		root:     root,
		parent:   map[Component]Component{},
		bindings: map[Component][]*KeyBinding{},
		cur:      -1,
	}
	m.walk(root, nil)
	for _, w := range m.order {
		if t, ok := w.(FocusTarget); ok {
			t.SetFocused(false) // components outlive tree rebuilds
		}
	}
	if len(m.order) > 0 {
		m.SetFocus(m.order[0])
	}
	return m
}

func (m *FocusManager) walk(w, parent Component) {
	m.parent[w] = parent
	if f, ok := w.(Focusable); ok && f.AcceptsFocus() {
		m.order = append(m.order, w)
	}
	if a, ok := w.(Attacher); ok {
		for _, at := range a.Attachments() {
			m.parent[at] = w
			if kb, ok := at.(*KeyBinding); ok {
				m.bindings[w] = append(m.bindings[w], kb)
			}
		}
	}
	if c, ok := w.(Container); ok {
		for _, ch := range c.ChildComponents() {
			m.walk(ch, w)
		}
	}
}

// Focused returns the component holding focus, or nil.
func (m *FocusManager) Focused() Component {
	if m.cur < 0 || m.cur >= len(m.order) {
		return nil
	}
	return m.order[m.cur]
}

// Order is the focus traversal order — tree order, filtered to focus
// stops. Exposed for tests and for apps that want to restore focus.
func (m *FocusManager) Order() []Component { return m.order }

// SetFocus moves focus to w if it is a focus stop.
func (m *FocusManager) SetFocus(w Component) bool {
	for i, o := range m.order {
		if o == w {
			m.focusIndex(i)
			return true
		}
	}
	return false
}

func (m *FocusManager) focusIndex(i int) {
	if i == m.cur {
		return
	}
	if old, ok := m.Focused().(FocusTarget); ok {
		old.SetFocused(false)
	}
	m.cur = i
	if now, ok := m.Focused().(FocusTarget); ok {
		now.SetFocused(true)
	}
}

// FocusNext and FocusPrev move focus in tree order, wrapping, skipping
// anything currently inside a Collapsed subtree.
func (m *FocusManager) FocusNext() { m.move(1) }
func (m *FocusManager) FocusPrev() { m.move(-1) }

func (m *FocusManager) move(d int) {
	n := len(m.order)
	if n == 0 {
		return
	}
	for i := 1; i <= n; i++ {
		next := ((m.cur+d*i)%n + n) % n
		if m.reachable(m.order[next]) {
			m.focusIndex(next)
			return
		}
	}
}

func (m *FocusManager) reachable(w Component) bool {
	for n := w; n != nil; n = m.parent[n] {
		if l := LayoutOf(n); l != nil && l.Visibility == Collapsed {
			return false
		}
	}
	return true
}

// Dispatch routes a key event. It starts at the focused component and walks
// up its ancestors to the root; at each level the KeyBindings attached
// there are matched first, then that component's own HandleKey. The first
// true stops propagation. If nothing consumed the event, tab and
// shift+tab move focus — which means either can be overridden by binding
// or handling it.
func (m *FocusManager) Dispatch(ev input.KeyEvent) bool {
	start := m.Focused()
	if start == nil {
		start = m.root
	}
	for n := start; n != nil; n = m.parent[n] {
		for _, b := range m.bindings[n] {
			if b.Gesture == ev && b.Command != nil {
				b.Command()
				return true
			}
		}
		if h, ok := n.(KeyHandler); ok && h.HandleKey(ev) {
			return true
		}
	}
	switch ev {
	case input.Named(input.KeyTab):
		m.FocusNext()
		return true
	case input.KeyEvent{Key: input.KeyTab, Mods: input.ModShift}:
		m.FocusPrev()
		return true
	}
	if d, ok := arrowDir(ev); ok {
		return m.FocusDir(d)
	}
	return false
}

// Direction is an arrow's meaning for focus navigation.
type Direction uint8

const (
	DirUp Direction = iota
	DirDown
	DirLeft
	DirRight
)

func arrowDir(ev input.KeyEvent) (Direction, bool) {
	switch ev {
	case input.Named(input.KeyUp):
		return DirUp, true
	case input.Named(input.KeyDown):
		return DirDown, true
	case input.Named(input.KeyLeft):
		return DirLeft, true
	case input.Named(input.KeyRight):
		return DirRight, true
	}
	return 0, false
}

// FocusDir moves focus spatially — the nearest focus stop whose center
// lies in the given direction from the focused component, preferring ones
// roughly in line with it (XAML's XYFocus). It falls back to tree order
// when nothing lies that way, so a direction is never a dead end.
//
// Arrows reach this only when nothing in the tree consumed them, which
// is what lets a list pane keep its own arrow handling.
func (m *FocusManager) FocusDir(d Direction) bool {
	if len(m.order) == 0 {
		return false
	}
	cur, ok := focusBounds(m.Focused())
	if !ok {
		m.moveInDir(d)
		return true
	}
	cx, cy := cur.X+cur.W/2, cur.Y+cur.H/2
	best, bestScore := -1, 0
	for i, w := range m.order {
		if w == m.Focused() || !m.reachable(w) {
			continue
		}
		b, ok := focusBounds(w)
		if !ok {
			continue
		}
		x, y := b.X+b.W/2, b.Y+b.H/2
		var along, across int
		switch d {
		case DirRight:
			along, across = x-cx, abs(y-cy)
		case DirLeft:
			along, across = cx-x, abs(y-cy)
		case DirDown:
			along, across = y-cy, abs(x-cx)
		case DirUp:
			along, across = cy-y, abs(x-cx)
		}
		if along <= 0 {
			continue // not in this direction
		}
		score := along + 2*across // drifting off-axis costs double
		if best < 0 || score < bestScore {
			best, bestScore = i, score
		}
	}
	if best >= 0 {
		m.focusIndex(best)
		return true
	}
	m.moveInDir(d)
	return true
}

func (m *FocusManager) moveInDir(d Direction) {
	if d == DirRight || d == DirDown {
		m.FocusNext()
		return
	}
	m.FocusPrev()
}

func focusBounds(w Component) (Rect, bool) {
	b, ok := w.(Bounded)
	if !ok {
		return Rect{}, false
	}
	r := b.Bounds()
	return r, r.W > 0 && r.H > 0
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
