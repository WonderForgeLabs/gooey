package main

// Modal is what makes the purchase sheet a dialog rather than a pane.
//
// # Why this is Go and not markup
//
// A modal needs two things: to be drawn on top, and to stop everything
// underneath from being usable. The first is layout. The second is
// `gooey.Frozen`, which is a Go INTERFACE and deliberately not an
// element in the markup vocabulary — the whole point of the store demo
// is that a vendor arriving over the control plane can inject markup
// that LOOKS modal and cannot BE modal, because nothing it can write
// freezes anything. You can always tab past a vendor's sheet.
//
// The app owner is on the other side of that line: Northwind ships Go,
// so Northwind can freeze its own app. That asymmetry is not a
// limitation being worked around here, it is the demo's thesis with a
// concrete edge on it.
//
// # Why the whole backdrop is wrapped, chrome included
//
// The sheet used to be shown by COLLAPSING the pane behind it, which
// made it modal by making everything else stop existing. That is why it
// filled the window: with nothing behind it there was nothing to float
// over, so it took the entire row and read as a screen rather than as a
// dialog.
//
// Now the app stays on screen behind the sheet, which means every focus
// stop in it is still there and still tabbable — the ItemsView, the
// three buttons on the store pane, and the toolbar down in the status
// bar. Freezing the panes and leaving the toolbar reachable would be a
// modal you can tab out of, so there are two of these: one around the
// panes, one around the status bar.
//
// # Why there is no plumbing here
//
// Frozen is OBSERVED. Composer.armFrozen wraps every implementer in a
// computed whose evaluation calls Frozen(), so whatever that method
// reads becomes a dependency by the ordinary call-site rule — no
// declaration, no second method, no handle to keep in step. Store.Blocked
// reads s.pane, so setting s.pane schedules a frame, the sweep sees the
// answer flip, and the re-sync runs in that same frame before anything
// paints. Nothing in this file has to ask for it.
//
// The limit is the same one every derived value here has: the observer
// subscribes to what Frozen() READS. A bare bool field written by a
// handler records no dependency and gets the old sampled behaviour. That
// is why blocked is a func rather than a bool this struct stores.

import (
	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/markup"
)

type Modal struct {
	gooey.Base

	child gooey.Component
	// blocked is asked, not stored: the answer has to be current at the
	// moment of the re-sync that reads it.
	blocked func() bool
}

func (m *Modal) Frozen() bool { return m.blocked() }

func (m *Modal) ChildComponents() []gooey.Component {
	if m.child == nil {
		return nil
	}
	return []gooey.Component{m.child}
}

// Measure and Arrange go through MeasureChild/ArrangeChild rather than
// calling the child directly, because those are what apply the
// margin/size/align/visibility sandwich. Skipping them drops all four
// silently — the panes inside carry Visibility bindings, and this is the
// code that honours them.
func (m *Modal) Measure(avail gooey.Size) gooey.Size {
	if m.child == nil {
		return gooey.Size{}
	}
	return gooey.MeasureChild(m.child, avail)
}

func (m *Modal) Arrange(b gooey.Rect) {
	m.Base.Arrange(b)
	if m.child != nil {
		gooey.ArrangeChild(m.child, b)
	}
}

// Render paints nothing. A Modal is a behaviour, not a picture: its
// bounds enclose its child, and a container that painted them would wipe
// the child it is wrapping.
func (m *Modal) Render(*gooey.Frame) {}

// RegisterModal adds <Modal> to the context.
//
// It takes no attributes. A third-party element cannot bind one anyway —
// the helpers that turn {{.X}} into a handle are unexported, so only
// built-ins get bound attributes — and inventing a literal for it would
// be worse: the predicate belongs to the app, and the app is what is
// holding it.
func RegisterModal(ctx *markup.Context, blocked func() bool) {
	if ctx.Components == nil {
		ctx.Components = map[string]markup.Builder{}
	}
	ctx.Components["Modal"] = func(e markup.Element, c *markup.Context) (gooey.Component, error) {
		kids, attach, err := markup.BuildChildren(e, c)
		if err != nil {
			return nil, err
		}
		m := &Modal{blocked: blocked}
		if len(kids) > 0 {
			m.child = kids[0]
		}
		for _, a := range attach {
			m.Attach(a)
		}
		return m, nil
	}
}
