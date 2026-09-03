// Package preview is the editor's live island — and the one pane that is
// not a pure function of editor state.
//
// The other three panes display; this one HOSTS. It owns a swappable
// child component tree, which is why it needs a Go type at all: a
// markup-only control has no way to receive a *gooey.Component, and
// gooey.Dynamic is the framework mechanism for changing what
// ChildComponents returns without rebuilding everything around it.
//
// That asymmetry is worth stating rather than smoothing over, because it
// is also what the layout is arranged around: this subtree is discarded
// and rebuilt on every edit, so nothing the user types into may live
// inside it.
package preview

import (
	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// Pane is the one-child container whose child is REPLACED whenever the
// document changes.
//
// gooey.Dynamic is the framework's existing mechanism for exactly this:
// change what ChildComponents returns, call the hook, and the next frame
// re-syncs paint nodes and the input tree while KEEPING the node of every
// component that is still there. Swapping the preview therefore repaints
// the preview, not the editor.
type Pane struct {
	gooey.Base
	child gooey.Component
	hook  func()
	size  gooey.Size

	// design is the mode switch: true means the mounted tree is a PICTURE
	// and nothing in it acts. See Frozen.
	design *prop.Property[bool]

	// designer is the design-surface gesture set. See BindDesigner.
	designer Designer

	// overlay is the design-time layout guide, painted OVER the
	// previewed tree. Nil until BindOverlay.
	overlay *Overlay
}

// BindOverlay installs the design-time layout overlay.
//
// It becomes Pane's SECOND child, and the ordering is the entire reason
// it is a child rather than something Pane.Render draws: the composer
// paints depth-first pre-order, so a later sibling paints after the
// document subtree, and anything Pane drew itself would go underneath
// and be erased by the tree's own pre-clears. That ranks it above the
// ORDINARY layer only — a gooey.Overlay inside the previewed tree is
// lifted above this too (#437), which is inert while design mode is
// Frozen. See overlay.go's file
// comment.
func (p *Pane) BindOverlay(o *Overlay) { p.overlay = o }

// Overlay is the bound layout guide, or nil. Exported for the tests that
// need to assert against its arranged bounds — an overlay covering
// nothing cannot eat a click, so a hit-test assertion has to be able to
// prove it was covering something first.
func (p *Pane) Overlay() *Overlay { return p.overlay }

// Designer is the editor's half of the design-surface gestures: what a
// press, a drag and a release MEAN, which is document knowledge this
// component deliberately does not have.
//
// It replaced a single select-on-press closure when dragging arrived.
// Three related callbacks passed separately would have let a host bind
// one and forget another — a press that selects and a motion that does
// nothing is a designer where things cannot be moved, with no error
// anywhere — so they are one interface a host implements or does not.
//
// Every method reports whether it consumed the event.
type Designer interface {
	// Press selects what is under the cell, and may begin a drag.
	Press(x, y int) bool
	// Drag continues a drag in progress. Called for raw motion, which is
	// why the pane opts into MouseMoveHandler.
	Drag(x, y int) bool
	// Release commits a drag in progress.
	Release(x, y int) bool
	// Click is the SYNTHESIZED click, with count is 1 or 2. It is the
	// drill-in gesture: a double-click selects one level deeper than the
	// press already selected.
	//
	// The count is the framework's, not this pane's. DispatchMouse
	// synthesizes a click on release and counts repeats against the
	// captor inside FocusManager.DoubleClickInterval (mouse.go:203) — and
	// under a frozen host the captor IS this pane, so the count is
	// already measured against the right component. Timing a second
	// press here would be a second, worse copy of that.
	Click(x, y, count int) bool
}

// BindDesignMode makes the pane a gooey.Frozen host whose answer is a
// property rather than a constant, which is the whole point of a design
// surface: the same tree is a picture while you are building it and a
// working UI while you are trying it, and the switch between them is one
// Set.
//
// It is wired in Go rather than through a <Preview Design="{{...}}">
// attribute because this pane is already constructed in Go and handed to
// Builder — one instance per editor, deliberately — so an attribute would
// be a second route to a field the builder cannot honestly own.
func (p *Pane) BindDesignMode(design *prop.Property[bool]) { p.design = design }

// Frozen is gooey.Frozen, and the Get is what makes the flip observable:
// called from the Composer's frozen observer this read SUBSCRIBES, so
// setting the property re-routes input, stops the document's Startables
// and re-tabs the page in the same frame. Called from dispatch or from the
// re-sync walk the identical line is a plain read. The call site decides,
// as everywhere else.
//
// An unbound pane is frozen. That is the safe default for what this
// component IS — a preview you cannot accidentally click into — and it
// means forgetting to bind the mode leaves a picture rather than a live
// tree wired to nothing.
func (p *Pane) Frozen() bool {
	if p.design == nil {
		return true
	}
	return p.design.Get()
}

// BindSelect installs click-to-select, the design surface's own gesture.
//
// IT BELONGS ON THIS COMPONENT AND NOWHERE ELSE, and that is a framework
// fact rather than a filing decision. gooey.Frozen exempts the host from
// its own freeze precisely so a design surface has somewhere to put its
// gestures, and DispatchMouse retargets a press inside a frozen subtree
// to that host in ONE place at the top (mouse.go:176). So in DESIGN mode
// this pane is the only component a press inside the document can reach
// — the document's own Button never sees it, which is the whole point of
// the mode.
//
// sel is handed the pressed cell rather than a component because the
// retarget is lossy on purpose: by the time the event arrives here the
// deepest hit is gone, and recovering it is a call to
// FocusManager.HitTest, which is the editor's to make (it holds the
// composer) and not a paraphrase this file should keep its own copy of.
// Reporting true consumes the press.
func (p *Pane) BindDesigner(d Designer) { p.designer = d }

// HandleMouse is click-to-select, gated on the pane being FROZEN.
//
// THE MODE GUARD IS NOT BELT-AND-BRACES. In LIVE mode this pane is an
// ordinary ancestor of a live tree, and bubbling is what makes that
// matter: a press the document does not consume — on a Canvas's empty
// background, on a Text, on any of the many components that handle no
// pointer at all — walks up its ancestors and arrives here exactly as a
// DESIGN-mode press does. Selecting on it would take clicks that belong
// to the thing the user asked to try, which is the one thing LIVE mode
// exists for.
//
// The Frozen() call is a plain READ. Dispatch is not an evaluation
// context, so this Get records no dependency and needs none: the
// Composer's frozen observer already subscribes to the same property,
// and it is what re-routed the event to this method in the first place.
//
// SELECTION FOLLOWS THE PRESS, DRILLING FOLLOWS THE CLICK, and the split
// is not a detail. Selection should land on the button going down, the
// way focus-follows-click does — and a drag starts there, so waiting for
// a click would mean no drag at all. A click is what CARRIES THE COUNT,
// and a double-click is by definition not knowable until the second
// release, so drilling has to be the later of the two.
//
// Left only — nothing in the repo consumes input.ButtonRight yet, and
// quietly selecting on a right press would spend the gesture a context
// menu will want.
func (p *Pane) HandleMouse(ev input.MouseEvent) bool {
	if p.designer == nil || !p.Frozen() {
		return false
	}
	if ev.Button != input.ButtonLeft {
		return false
	}
	switch ev.Kind {
	case input.MousePress:
		return p.designer.Press(ev.X, ev.Y)
	case input.MouseRelease:
		return p.designer.Release(ev.X, ev.Y)
	case input.MouseClick:
		return p.designer.Click(ev.X, ev.Y, ev.Count)
	}
	return false
}

// HandleMouseMove is gooey.MouseMoveHandler, and the pane opts into raw
// motion for exactly one reason: a drag is motion.
//
// It costs nothing when nothing is being dragged — Drag returns false
// immediately — and motion is delivered only to components that ask for
// it, so the rest of the tree is unaffected.
//
// THE PRESS ALREADY CAPTURED THIS PANE. DispatchMouse sets the implicit
// captor from the (frozen-retargeted) hit before routing, so every motion
// event of the gesture arrives here even when the pointer leaves the
// designer entirely — which is what makes a drag that wanders over the
// properties grid and back still work. No CaptureMouse call is needed.
func (p *Pane) HandleMouseMove(ev input.MouseEvent) bool {
	if p.designer == nil || !p.Frozen() {
		return false
	}
	return p.designer.Drag(ev.X, ev.Y)
}

// Builder registers the pane as <Preview/>, with p as its host.
//
// IT CARRIES NO CHROME OF ITS OWN. It used to wrap itself in a
// components.Border, which made the designer the one region framed by a
// different mechanism than every other region — box-drawing runes beside
// pixel line art, in a shell whose whole point is that the frames match.
// Chrome belongs to <Panel>, so the shell writes:
//
//	<Panel Title="designer"><Preview/></Panel>
//
// and the designer is framed by the same component as the rest.
//
// One Pane per Builder, deliberately: the editor has exactly one preview,
// and a second instance of this element would mount the same tree twice.
func Builder(p *Pane) markup.Builder {
	return func(e markup.Element, ctx *markup.Context) (gooey.Component, error) {
		return p, nil
	}
}

// SetStructureHook is gooey.Dynamic.
func (p *Pane) SetStructureHook(h func()) { p.hook = h }

// Child is the tree currently mounted, or nil before the first Swap.
// Exported for the tests that assert the island was populated at all;
// the field stays unexported because writing it without the structure
// hook would leave the composer's paint nodes describing the old tree.
func (p *Pane) Child() gooey.Component { return p.child }

// ChildComponents is the previewed tree, then the overlay.
//
// THE ORDER IS THE Z-ORDER and may not be swapped: the composer walks
// depth-first pre-order, so the overlay paints last and therefore on
// top. Putting it first would paint the guides under the document, and
// the document's own pre-clears would erase them.
//
// ABOVE THE ORDINARY LAYER ONLY, since #437. A gooey.Overlay inside the
// previewed subtree is lifted above this one wherever it sits; see
// overlay.go's file comment for why that is inert while design mode is
// Frozen. Third copy of this rule in this package, and the one a grep
// for "document order" does not match — found by reading, in review of
// #444, after the other two were qualified.
func (p *Pane) ChildComponents() []gooey.Component {
	var out []gooey.Component
	if p.child != nil {
		out = append(out, p.child)
	}
	if p.overlay != nil {
		out = append(out, p.overlay)
	}
	return out
}

// Swap replaces the previewed tree. Called on the UI goroutine, from a
// property subscription — never from another goroutine.
func (p *Pane) Swap(c gooey.Component) {
	p.child = c
	if p.hook != nil {
		p.hook()
	}
}

func (p *Pane) Measure(avail gooey.Size) gooey.Size {
	if p.child != nil {
		p.size = gooey.MeasureChild(p.child, avail)
	}
	// THE OVERLAY IS A LISTED CHILD, SO IT GOES THROUGH THE SANDWICH
	// TOO. ChildComponents returns it, Arrange arranges it, and this
	// measured only the document — which quietly opted the overlay out
	// of the margin/size/align/visibility handling MeasureChild applies.
	// The one with teeth is visibility: MeasureChild is where a child's
	// Layout.Visibility is synced from a bound source, so a Collapsed
	// overlay would have gone on being arranged and painted.
	//
	// Nothing sets that today — the overlay is constructed in Go, not
	// declared in markup — which is exactly why it was easy to skip and
	// why nothing failed. The rule is that a container never calls
	// Measure/Arrange on a child itself and never skips them either; a
	// child listed in one walk and absent from another is the shape that
	// bites later.
	//
	// The result is discarded on purpose. Arrange hands the overlay the
	// pane's WHOLE rect as a ceiling and it narrows itself to the guide,
	// which is the reasoning written there; Overlay.Measure answers zero
	// and costs nothing, so this is the sandwich rather than a size
	// negotiation.
	if p.overlay != nil {
		gooey.MeasureChild(p.overlay, avail)
	}
	return avail
}

func (p *Pane) Arrange(b gooey.Rect) {
	p.Base.Arrange(b)
	if p.child != nil {
		gooey.ArrangeChild(p.child, gooey.Rect{
			X: b.X, Y: b.Y,
			W: min(p.size.W, b.W),
			H: min(p.size.H, b.H),
		})
	}
	if p.overlay != nil {
		// ARRANGED LAST, AFTER THE DOCUMENT, and the order is
		// load-bearing twice over.
		//
		// Overlay.Arrange re-probes the grid it describes, so the
		// subtree must already have its final bounds or the guide would
		// describe the PREVIOUS frame's layout — visible as a grid whose
		// drawn cells lag a keystroke behind the track spec that
		// produced them.
		//
		// It also gets the pane's WHOLE rect rather than the child's,
		// and this is a CEILING, not the overlay's final size: its
		// bounds are what the composer uses for damage bookkeeping and
		// for the z-ordered "who sits above whom" test, so it has to be
		// free to claim any region the guide might need. Overlay.Arrange
		// then narrows itself to the guide — grid plus gutters — or to
		// zero when there is no grid in scope, which is what makes the
		// common case cost nothing.
		//
		// This comment used to justify the whole rect by saying the
		// gutters are drawn OUTSIDE the grid. They are not: overlay.go's
		// drawGutters says they are INSIDE the grid's own bounds, and
		// docs/specs/2026-08-23-layout-grants.md records "outside" as the
		// framing that was tried and rejected — drawn outside, a grid at
		// the top-left of the preview scribbles its structure over the
		// editor's own pane border. The behaviour here was right and the
		// reason attached to it was the discarded one, which is worse
		// than no reason: it sends the next reader to reconcile two files
		// that do not disagree.
		gooey.ArrangeChild(p.overlay, b)
	}
}

// Render paints nothing: a container paints only its own chrome, and this
// one has none. Pre-clearing here would wipe the previewed tree.
func (p *Pane) Render(f *gooey.Frame) {}

// MirrorBuilder is what <Preview> becomes in the DOCUMENT's vocabulary —
// see mirror.go for why the document gets a different builder for the
// same element name rather than a recursion guard.
func MirrorBuilder(style render.Style) markup.Builder {
	return func(markup.Element, *markup.Context) (gooey.Component, error) {
		return &Mirror{style: style}, nil
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
