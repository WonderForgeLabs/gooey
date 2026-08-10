package control

import (
	"strings"

	"github.com/WonderForgeLabs/gooey/input"
)

// PointerKind is what a pointer did. KindClick is synthesized as
// press+release on the same cell, exactly as the framework's dispatcher
// does — a terminal never sends a click.
type PointerKind int

const (
	PointerClick PointerKind = iota
	PointerPress
	PointerRelease
	PointerMove
	PointerWheelUp
	PointerWheelDown
)

// Pointer is one pointer action at a cell coordinate.
type Pointer struct {
	Kind PointerKind
	X, Y int
	// Button is the framework's own button type; input.ButtonLeft for
	// the common case.
	Button input.MouseButton
}

// EchoEvent is one injected input event after dispatch, for a transport
// that echoes the input stream to subscribers.
type EchoEvent struct {
	Event    input.Event
	Consumed bool
}

func (s *Service) echo(ev input.Event, consumed bool) {
	if s.Echo != nil {
		s.Echo(EchoEvent{Event: ev, Consumed: consumed})
	}
}

// SendKeys injects key events into the app's single ordered input
// stream: one event per rune of text first, then one per gesture, in
// markup gesture syntax ("tab", "enter", "ctrl+s"). Routed through the
// composition, not the app-level handler — the quit key is out of a
// client's reach. Returns per-event consumption, in send order.
func (s *Service) SendKeys(text string, gestures []string) ([]bool, error) {
	c, err := s.composer()
	if err != nil {
		return nil, err
	}
	var events []input.Event
	for _, r := range text {
		events = append(events, input.KeyOf(input.Rune(r)))
	}
	for _, g := range gestures {
		ev, err := input.ParseGesture(g)
		if err != nil {
			return nil, invalidf("%v", err)
		}
		events = append(events, input.KeyOf(ev))
	}
	if len(events) == 0 {
		return nil, invalidf("SendKeys needs text or gestures")
	}
	consumed := make([]bool, len(events))
	for i, ev := range events {
		consumed[i] = c.Handle(ev)
		s.echo(ev, consumed[i])
	}
	return consumed, nil
}

// SendPointer injects one pointer event. Hit-testing, hover and
// focus-follows-click all happen as they would from a real terminal.
func (s *Service) SendPointer(p Pointer) (bool, error) {
	c, err := s.composer()
	if err != nil {
		return false, err
	}
	ev := input.MouseEvent{X: p.X, Y: p.Y, Button: p.Button}
	switch p.Kind {
	case PointerPress:
		ev.Kind = input.MousePress
	case PointerRelease:
		ev.Kind = input.MouseRelease
	case PointerMove:
		ev.Kind, ev.Button = input.MouseMove, input.ButtonNone
	case PointerWheelUp:
		ev.Kind, ev.Button = input.WheelUp, input.ButtonNone
	case PointerWheelDown:
		ev.Kind, ev.Button = input.WheelDown, input.ButtonNone
	case PointerClick:
		// A terminal never sends a click: the dispatcher synthesizes one
		// from a press and a release on the same component. Sending the
		// pair is therefore what "click" has to mean here, and it also
		// gets the press-state visual and focus-follows-click for free.
		press, release := ev, ev
		press.Kind, release.Kind = input.MousePress, input.MouseRelease
		h1 := c.HandleMouse(press)
		s.echo(input.MouseOf(press), h1)
		h2 := c.HandleMouse(release)
		s.echo(input.MouseOf(release), h2)
		return h1 || h2, nil
	default:
		return false, invalidf("unknown pointer kind")
	}
	consumed := c.HandleMouse(ev)
	s.echo(input.MouseOf(ev), consumed)
	return consumed, nil
}

// Focus moves keyboard focus to the element with the given Name. The
// element must be a focus stop.
func (s *Service) Focus(name string) error {
	c, err := s.composer()
	if err != nil {
		return err
	}
	if s.bind == nil {
		return errNoContext
	}
	w, ok := s.bind.Named[strings.TrimSpace(name)]
	if !ok {
		return notFoundf("no element named %q; SnapshotTree lists the named elements", name)
	}
	if !c.Focus().SetFocus(w) {
		return invalidf("element %q (%T) is not a focus stop", name, w)
	}
	return nil
}
