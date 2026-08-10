package gooey

import (
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/prop"
)

// The pointer half of the input chapter. Mouse events route the way keys
// do — one target, then its ancestors — but the target is found by
// hit-testing the retained tree instead of by focus, and two framework
// behaviors run first: hover tracking and focus-follows-click.

// MouseHandler is the optional interface for widgets that consume
// pointer events. Returning true stops propagation.
type MouseHandler interface{ HandleMouse(input.MouseEvent) bool }

// MouseMoveHandler opts a widget into raw motion events (drag, resize).
// Motion is high-frequency, so it is delivered only to widgets that ask
// for it — everything else sees hover enter/leave instead.
type MouseMoveHandler interface{ HandleMouseMove(input.MouseEvent) bool }

// HoverTarget is how the framework tells a widget the pointer entered or
// left it. HoverState implements it.
type HoverTarget interface{ SetHovered(bool) }

// HoverState is the pointer twin of FocusState, and works the same way:
// the flag is a source property, so a Render that reads IsHovered() gets
// enter/leave as damage and only the widget entered and the one left
// repaint. Widgets that do not embed it cost nothing — the dispatcher
// simply finds no hover target.
type HoverState struct{ hovered *prop.Property[bool] }

func (h *HoverState) SetHovered(v bool) { h.hover().Set(v) }
func (h *HoverState) IsHovered() bool   { return h.hover().Get() }

func (h *HoverState) hover() *prop.Property[bool] {
	if h.hovered == nil {
		h.hovered = prop.NewSource(false)
	}
	return h.hovered
}

// HitTest returns the deepest widget whose arranged bounds contain the
// cell, children before ancestors and later siblings before earlier ones
// (they paint on top). Collapsed subtrees and zero-size widgets are not
// hit. The walk allocates nothing — it runs on every motion event.
func (m *FocusManager) HitTest(x, y int) Widget { return hitTest(m.root, x, y) }

func hitTest(w Widget, x, y int) Widget {
	if l := layoutOf(w); l != nil && l.Visibility == Collapsed {
		return nil
	}
	b, ok := w.(Bounded)
	if !ok {
		return nil
	}
	r := b.Bounds()
	if x < r.X || y < r.Y || x >= r.X+r.W || y >= r.Y+r.H {
		return nil
	}
	if c, ok := w.(Container); ok {
		kids := c.ChildWidgets()
		for i := len(kids) - 1; i >= 0; i-- {
			if hit := hitTest(kids[i], x, y); hit != nil {
				return hit
			}
		}
	}
	return w
}

// DispatchMouse routes a pointer event to the widget under it, then up
// that widget's ancestors like a key. Two things happen before the app
// ever sees the event: the hover target is updated, and a press moves
// focus to the nearest focusable widget at or above the hit (the
// focus-follows-click convention). Wheel events go to the widget under
// the pointer, not the focused one — also the convention.
func (m *FocusManager) DispatchMouse(ev input.MouseEvent) bool {
	target := m.HitTest(ev.X, ev.Y)
	switch ev.Kind {
	case input.MouseMove:
		m.setHover(target)
		for n := target; n != nil; n = m.parent[n] {
			if h, ok := n.(MouseMoveHandler); ok && h.HandleMouseMove(ev) {
				return true
			}
		}
		return false
	case input.MousePress:
		m.setHover(target)
		if w := m.focusTargetFor(target); w != nil {
			m.SetFocus(w)
		}
		m.pressed = target
		return m.bubbleMouse(target, ev)
	case input.MouseRelease:
		// Implicit capture: the release belongs to the widget the press
		// went down on, so it can undo pressed-state visuals even if the
		// pointer wandered off before the button came up.
		dest := target
		if m.pressed != nil {
			dest = m.pressed
		}
		handled := m.bubbleMouse(dest, ev)
		if m.pressed != nil && m.pressed == target {
			click := ev
			click.Kind = input.MouseClick
			if m.bubbleMouse(target, click) {
				handled = true
			}
		}
		m.pressed = nil
		return handled
	default:
		return m.bubbleMouse(target, ev)
	}
}

func (m *FocusManager) bubbleMouse(target Widget, ev input.MouseEvent) bool {
	for n := target; n != nil; n = m.parent[n] {
		if h, ok := n.(MouseHandler); ok && h.HandleMouse(ev) {
			return true
		}
	}
	return false
}

// focusTargetFor picks what a press on w should focus: w itself or the
// nearest focusable ancestor, and failing that the first focusable
// DESCENDANT. The descent is what makes clicking a pane's border or
// title focus the pane — the hit there is the Border, whose focusable
// content is below it in the tree, not above.
func (m *FocusManager) focusTargetFor(w Widget) Widget {
	for n := w; n != nil; n = m.parent[n] {
		if f, ok := n.(Focusable); ok && f.AcceptsFocus() {
			return n
		}
	}
	return firstFocusable(w)
}

func firstFocusable(w Widget) Widget {
	if w == nil {
		return nil
	}
	if l := layoutOf(w); l != nil && l.Visibility == Collapsed {
		return nil
	}
	if f, ok := w.(Focusable); ok && f.AcceptsFocus() {
		return w
	}
	if c, ok := w.(Container); ok {
		for _, ch := range c.ChildWidgets() {
			if hit := firstFocusable(ch); hit != nil {
				return hit
			}
		}
	}
	return nil
}

// Hovered returns the widget the pointer is currently over.
func (m *FocusManager) Hovered() Widget { return m.hover }

// setHover moves the hover flag to the nearest HoverTarget at or above
// the hit widget, so hover composes: a Border can highlight while the
// pointer is over the Text inside it.
func (m *FocusManager) setHover(hit Widget) {
	var target Widget
	for n := hit; n != nil; n = m.parent[n] {
		if _, ok := n.(HoverTarget); ok {
			target = n
			break
		}
	}
	if target == m.hover {
		return
	}
	if h, ok := m.hover.(HoverTarget); ok {
		h.SetHovered(false)
	}
	m.hover = target
	if h, ok := m.hover.(HoverTarget); ok {
		h.SetHovered(true)
	}
}
