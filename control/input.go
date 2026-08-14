package control

import (
	"strings"

	"github.com/WonderForgeLabs/gooey"
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
// A SCOPED session may only type into its own island, and the check has
// to be on FOCUS rather than on a name, because keys are not
// name-addressed: they go wherever focus is. So the rule is "the focused
// element must be inside your island" — which a guest satisfies by
// calling Focus on something it owns first.
//
// The honest consequence, worth stating rather than smoothing over:
// focus is singular and global — there is one keyboard. Two guests with
// disjoint islands cannot BOTH hold focus, so they cannot both type at
// once. Enforcement does not fix that; it makes it a visible refusal
// instead of silent cross-talk, which is the improvement actually on
// offer. Every other verb is genuinely concurrent.
func (s *Service) SendKeys(text string, gestures []string) ([]bool, error) {
	c, err := s.composer()
	if err != nil {
		return nil, err
	}
	if s.scoped() {
		if s.islandRoot() == nil {
			return nil, deniedf("this session is scoped to island %q, which names no element in the running tree", s.grant.Island)
		}
		f := c.Focus().Focused()
		if f == nil {
			return nil, deniedf("nothing is focused, and a scoped session may only send keys while focus is inside its island %q; call SetFocus on an element you own first", s.grant.Island)
		}
		if !s.islandSet()[f] {
			return nil, deniedf("focus is on a %T outside this session's island %q; keys go where focus is, so sending them would type into the host's tree", f, s.grant.Island)
		}
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
//
// A SCOPED session may only point at what its island would RECEIVE, and
// the distinction between that and "cells its island occupies" is the
// whole of the check. It asks FocusManager.MouseTarget, which is the
// routing decision itself rather than a paraphrase of it, because two
// framework behaviours move the target off the hit and getting either
// one wrong is silent:
//
//   - Frozen retargets to the frozen HOST. A check on the raw hit would
//     clear an event that dispatch then delivers to a component outside
//     the island — which is live since preview.Pane became a Frozen host.
//   - Capture overrides the hit entirely, so a check on the hit alone
//     would REFUSE a guest's own drag the moment the pointer left its
//     island's bounds, which is precisely when a drag matters.
//
// The event is built BEFORE the guard because the answer depends on the
// event kind: a fresh press discards an implicit capture and a held one
// survives, and MouseTarget models that.
//
// There is no goroutine window between this check and the dispatch below
// it. Every transport calls this inside one control.Bridge.Do closure —
// grpc/controlserver.go, grpc/session.go's act loop, and mcp's single
// dispatch site — so the UI goroutine runs check and delivery without
// yielding, and no other input can interleave between them.
func (s *Service) SendPointer(p Pointer) (bool, error) {
	c, err := s.composer()
	if err != nil {
		return false, err
	}
	ev := input.MouseEvent{X: p.X, Y: p.Y, Button: p.Button}
	click := false
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
		ev.Kind, click = input.MousePress, true
	default:
		return false, invalidf("unknown pointer kind")
	}

	if err := s.mayPoint(c, ev); err != nil {
		return false, err
	}

	if click {
		press, release := ev, ev
		release.Kind = input.MouseRelease
		h1 := c.HandleMouse(press)
		s.echo(input.MouseOf(press), h1)
		// The release is NOT re-checked: the press above set the implicit
		// captor to a component the check just cleared, and a release
		// routes to the captor. Checking it again would re-derive the same
		// answer from state this call itself created.
		h2 := c.HandleMouse(release)
		s.echo(input.MouseOf(release), h2)
		return h1 || h2, nil
	}
	consumed := c.HandleMouse(ev)
	s.echo(input.MouseOf(ev), consumed)
	return consumed, nil
}

// mayPoint guards one pointer event against the grant, by asking where
// the framework would route it.
func (s *Service) mayPoint(c *gooey.Composer, ev input.MouseEvent) error {
	if !s.scoped() {
		return nil
	}
	if s.islandRoot() == nil {
		return deniedf("this session is scoped to island %q, which names no element in the running tree", s.grant.Island)
	}
	target := c.Focus().MouseTarget(ev)
	if target == nil {
		return deniedf("no component would receive a pointer event at cell (%d,%d); it is outside this session's island %q",
			ev.X, ev.Y, s.grant.Island)
	}
	if !s.islandSet()[target] {
		return deniedf("a pointer event at cell (%d,%d) would be delivered to a %T outside this session's island %q",
			ev.X, ev.Y, target, s.grant.Island)
	}
	return nil
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
	if err := s.mayAddress(name); err != nil {
		return err
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
