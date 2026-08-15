package main

// MOVE: dragging an element around the design surface.
//
// The mechanism is two-speed on purpose, and the split is the whole
// design:
//
//   PER MOTION — write gooey.Layout.Left/Top on the live component and
//   ask for a frame. Nothing else. No markup, no rebuild, no re-mount.
//   Composer.Frame lays out unconditionally and its bounds sweep does the
//   rest: it clears the vacated rect to the ancestor background, repaints
//   the moved component, and force-repaints whatever the old rect
//   uncovered.
//
//   ON RELEASE — write Canvas.Left/Canvas.Top into the document and
//   rebuild ONCE.
//
// Writing markup per motion would work and is the trap. rebuild()
// discards and re-mounts the whole designer subtree, so a drag would cost
// a full re-mount per pointer report — and it would LOOK correct, which
// is why the per-motion damage count is pinned separately from the
// release count. A bounds assertion passes just as well when the entire
// tree repainted.
//
// Reconciling markup only on release is also what keeps the drag's own
// target stable: rebuild() is what repopulates docRoot/nodeOf, so a
// motion that wrote markup would invalidate the very map the drag is
// using to know what it is dragging.
//
// THE POSITIONS LIVE IN MEMORY, AND NOWHERE ELSE. Under the wrapping
// model the surface is never serialized, so a position on it has no home
// in the saved file. This file does not invent one — no attribute, no
// comment, no property element. `dragState` is the one place that holds
// in-flight geometry, so a future solution/project file has one struct to
// populate rather than a scattering.

import (
	"strconv"

	"github.com/WonderForgeLabs/gooey"
)

// dragState is the gesture in flight. Zero value means no drag.
//
// It holds the ORIGIN of the gesture rather than the last position, so
// every motion is computed from the press: accumulating deltas would
// drift by one cell per dropped or coalesced motion report, and a
// terminal coalesces freely under load.
type dragState struct {
	node *node
	comp gooey.Component
	// startX/startY is where the pointer went down; origL/origT is the
	// element's offset at that moment.
	startX, startY int
	origL, origT   int
	// moved is whether any motion actually changed the offset. A press
	// and release with no movement is a CLICK, and must not write markup
	// or cost a rebuild.
	moved bool
}

func (d *dragState) active() bool { return d != nil && d.node != nil }

// Press is preview.Designer. It selects what is under the pointer and,
// where that element has free geometry, begins a drag.
func (ed *editor) Press(x, y int) bool {
	ed.setSelection(ed.nodeAt(ed.hitTestOrNil(x, y)))
	ed.beginDrag(x, y)
	return true
}

// Drag is preview.Designer: one pointer report during a gesture.
func (ed *editor) Drag(x, y int) bool {
	if !ed.drag.active() {
		return false
	}
	l := gooey.LayoutOf(ed.drag.comp)
	if l == nil {
		return false
	}
	left, top := ed.drag.origL+(x-ed.drag.startX), ed.drag.origT+(y-ed.drag.startY)
	// Clamped at the surface's origin: a negative Canvas.Left is not
	// expressible in the document, so allowing the live offset to go
	// negative would let the element drift somewhere the release could
	// not record.
	if left < 0 {
		left = 0
	}
	if top < 0 {
		top = 0
	}
	if l.Left == left && l.Top == top {
		// Consume it anyway — the gesture owns the pointer — but do not
		// spend a frame on a motion that changed nothing. A terminal
		// reports motion per cell, and a drag along one axis produces a
		// stream of no-ops on the other.
		return true
	}
	l.Left, l.Top = left, top
	ed.drag.moved = true
	// THE FRAME DOES NOT HAPPEN BY ITSELF. Layout.Left/Top are plain int
	// fields with no property behind them (unlike Visibility, which has
	// BindVisibilityFunc), so this write is invisible to the property
	// graph — and App.handle does not schedule a frame either, because
	// frames are scheduled by the graph. Without this call the element
	// simply does not move, with no error anywhere.
	ed.invalidate()
	return true
}

// Release is preview.Designer: commit the gesture.
//
// This is the ONLY thing in the drag path that writes markup, which is
// what stops a save from racing a motion.
func (ed *editor) Release(x, y int) bool {
	if !ed.drag.active() {
		return false
	}
	d := ed.drag
	ed.drag = dragState{}
	if !d.moved {
		return true // a click, not a move
	}
	l := gooey.LayoutOf(d.comp)
	if l == nil {
		return true
	}
	d.node.Attrs["Canvas.Left"] = strconv.Itoa(l.Left)
	d.node.Attrs["Canvas.Top"] = strconv.Itoa(l.Top)
	ed.rebuild()
	return true
}

// beginDrag starts a gesture if the selected element can actually be
// moved, and silently does not if it cannot.
//
// FREE GEOMETRY IS A PROPERTY OF THE PARENT, not of the element. A child
// of a <Canvas> has Canvas.Left/Canvas.Top and can be put anywhere; a
// child of a <Grid> has Grid.Row/Grid.Col and a drag there would mean
// RE-CELLING, which is a different gesture with a different answer and is
// deliberately not implemented. A child of a <VStack> has no geometry at
// all — its position is its index, so a drag there means REORDER.
//
// Refusing rather than guessing is the point: writing Canvas.Left onto a
// child of a VStack would be silently discarded by applyLayout, which is
// the exact defect the catalog work exists to delete.
func (ed *editor) beginDrag(x, y int) {
	ed.drag = dragState{}
	n := ed.sel
	if n == nil || ed.isSurface(n) {
		return
	}
	p := ed.parentOf(n)
	if p == nil || p.Elem != "Canvas" {
		return
	}
	comp := ed.componentFor(n)
	if comp == nil {
		return
	}
	l := gooey.LayoutOf(comp)
	if l == nil {
		return
	}
	ed.drag = dragState{
		node: n, comp: comp,
		startX: x, startY: y,
		origL: l.Left, origT: l.Top,
	}
}

// componentFor is nodeOf inverted for one node. Linear, and deliberately
// so: it runs once per gesture rather than once per motion.
func (ed *editor) componentFor(n *node) gooey.Component {
	for c, m := range ed.nodeOf {
		if m == n {
			return c
		}
	}
	return nil
}

// hitTestOrNil is the framework query with the nil-binding case folded in.
func (ed *editor) hitTestOrNil(x, y int) gooey.Component {
	if ed.hitTest == nil {
		return nil
	}
	return ed.hitTest(x, y)
}

// invalidate asks for a frame. Injected like hitTest rather than reached
// through ed.app, because the tests drive Composer.Frame() directly and
// have no *gooey.App at all — and because App.Swap builds a new Composer
// on a hot reload.
func (ed *editor) invalidate() {
	if ed.invalidateFn != nil {
		ed.invalidateFn()
	}
}

// dragKind reports why an element can or cannot be dragged, in the terms
// the user would need to be told.
//
// A string rather than a bool because "you cannot move this" is not the
// interesting part — WHY is. The three reasons need three different
// answers and only one of them is decided, so the distinction is kept
// here rather than collapsed into a false.
func (ed *editor) dragKind(n *node) string {
	if n == nil || ed.isSurface(n) {
		return "nothing selected"
	}
	p := ed.parentOf(n)
	if p == nil {
		return "the document root"
	}
	switch p.Elem {
	case "Canvas":
		return "free"
	case "Grid":
		return "re-cell"
	}
	return "reorder"
}
