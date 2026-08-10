package components

import (
	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/prop"
)

// Popup is the shared mechanics of an anchored, dismissable overlay —
// extracted once the framework had grown four hand-rolled copies (the
// MenuBar dropdown, the Tooltip popup, the demo browser's source
// picker, and the ToastHost's hosting shape). It is a Go-side
// primitive, not a markup element: a markup surface can come with a
// future customer that needs one.
//
// A popup has two halves and an OWNER:
//
//   - The owner is an ordinary component — a menu bar, a picker — that
//     stays in the tree, handles keys and mouse, and is the component
//     focus and pointer capture land on while the popup is open. The
//     owner keeps everything domain-shaped: what the popup shows, where
//     it goes, which gestures mean what.
//   - The SURFACE is the visible box: a leaf child the owner returns
//     from ChildComponents (LAST, because document order is z-order),
//     whose pre-clear paints exactly the popup rectangle — the overlay
//     contract. The primitive owns the surface so it can guarantee the
//     subscription rule below; the owner supplies only the draw func.
//   - The Popup itself is the lifecycle: an open property, focus
//     save/restore, pointer capture, and the dismissal grammar.
//
// THE SUBSCRIPTION CARRIER, solved here once: a paint node's
// dependencies are recorded by the reads its evaluation performs, and a
// Collapsed surface never evaluates its Render — so a closed popup's
// node has no edge from the open property, and the FIRST Open schedules
// no frame unless some always-painted node happens to read IsOpen (the
// bug the browser picker hit, worked around with an app-side hint
// computed). The primitive's surface therefore stays VISIBLE and is
// arranged to a ZERO RECT while closed: its node evaluates on the very
// first frame, Render reads the open property before the bounds check,
// and the subscription exists before the popup has ever opened. Opening
// dirties the surface itself; no carrier, no workaround.
//
// FOCUS RESTORE follows the wave-2 rules: capture-at-open,
// restore-on-dismiss. A MOUSE open passes MouseOpenRestore() — by the
// time a press bubbles to the owner, focus-follows-click has already
// moved focus there, so the component to give it back to is the one the
// manager remembers losing (FocusManager.PreviouslyFocused). A KEY open
// passes nil: the owner held focus legitimately, and esc leaves focus
// where it already is. Dismiss restores only while focus is still on
// the owner, so a popup dismissed after the user moved on does not yank
// focus back.
//
// POINTER: Open takes held capture for the owner, which is what makes
// an out-of-bounds surface clickable at all (it hangs where hit-testing
// cannot see it) and what routes an outside press here instead of to
// whatever is underneath. HandleMouse implements the dismissal half: a
// press anywhere the owner did not claim dismisses AND is consumed, and
// the residual release/click of a gesture that started while open is
// swallowed.
//
// KEYS: HandleKey is the owner's fall-through — esc dismisses and is
// consumed; with Modal set, everything else the owner declined is
// swallowed too, so page gestures cannot fire under an open popup.
type Popup struct {
	// Modal makes HandleKey swallow every key the owner declined while
	// the popup is open. Opt-in: a non-modal popup lets unhandled keys
	// keep bubbling (esc still dismisses either way).
	Modal bool

	owner   gooey.Component
	surf    *popupSurface
	mgr     *gooey.FocusManager
	openP   *prop.Property[bool]
	restore gooey.Component
}

// NewPopup builds the lifecycle for owner. draw paints the surface's
// cells given its arranged bounds; it runs inside the surface's paint
// node, so the properties it reads become the surface's dependencies —
// a selection highlight repaints the popup alone, like any component.
func NewPopup(owner gooey.Component, draw func(*gooey.Frame, gooey.Rect)) *Popup {
	p := &Popup{owner: owner, openP: prop.NewSource(false)}
	p.surf = &popupSurface{pop: p, draw: draw}
	return p
}

// Surface is the visible leaf. The owner returns it from
// ChildComponents — as the LAST child, so it paints above what it
// covers — and places it with ArrangeSurface from its own Arrange.
func (p *Popup) Surface() gooey.Component { return p.surf }

// SurfaceBounds is where the surface currently sits — the rectangle the
// owner's hit math (which item is under this cell?) works against.
func (p *Popup) SurfaceBounds() gooey.Rect { return p.surf.Bounds() }

// SetFocusManager receives the input tree — the owner forwards its
// gooey.FocusHost call here, and focus restore and pointer capture go
// through it.
func (p *Popup) SetFocusManager(fm *gooey.FocusManager) { p.mgr = fm }

// Manager is the input tree the popup was given, for owners that need
// it for their own purposes (a menu bar's mnemonic open).
func (p *Popup) Manager() *gooey.FocusManager { return p.mgr }

// IsOpen reports whether the popup is showing. Read from a Render it is
// a paint dependency like any other property; read from layout or an
// event handler it is just a question — call site decides, as always.
func (p *Popup) IsOpen() bool { return p.openP.Get() }

// Open shows the popup: focus and held pointer capture move to the
// owner, and restore is what gets focus back on dismiss. Pass
// MouseOpenRestore() for a mouse open, nil for a key open — the wave-2
// rules — or any explicit component (an accelerator open passes what
// held focus when the accelerator fired).
func (p *Popup) Open(restore gooey.Component) {
	p.openP.Set(true)
	p.restore = restore
	if p.mgr != nil {
		p.mgr.SetFocus(p.owner)
		p.mgr.CaptureMouse(p.owner)
	}
}

// Dismiss closes the popup, releases the pointer, and hands focus back
// to the restore component — provided nothing moved focus elsewhere in
// the meantime. Idempotent.
func (p *Popup) Dismiss() {
	if !p.IsOpen() {
		return
	}
	p.openP.Set(false)
	if p.mgr != nil {
		if p.mgr.Captured() == p.owner {
			p.mgr.ReleaseCapture()
		}
		if p.restore != nil && p.mgr.Focused() == p.owner {
			p.mgr.SetFocus(p.restore)
		}
	}
	p.restore = nil
}

// MouseOpenRestore is what should get focus back after a MOUSE-opened
// popup: focus-follows-click has already moved focus to the owner by
// the time the press bubbles there, so the component to give it back to
// is the one still focused (when the click did not move focus) or the
// one the manager remembers losing it.
func (p *Popup) MouseOpenRestore() gooey.Component {
	if p.mgr == nil {
		return nil
	}
	if f := p.mgr.Focused(); f != nil && f != p.owner {
		return f
	}
	return p.mgr.PreviouslyFocused()
}

// ArrangeSurface places the surface from the owner's Arrange: at r
// while showing, at a zero rect while not. The surface is never
// Collapsed — zero-size is what keeps the open-property subscription
// alive (see the type comment) while still occupying nothing, hitting
// nothing, and painting nothing. The Composer's bounds sweep turns both
// transitions into damage: appearing dirties the surface, vanishing
// clears its old cells and restores what they covered.
//
// show is the owner's decision, not just IsOpen — a menu that is "open"
// over zero items shows nothing. Reads here happen in layout, outside
// any evaluation, so they record no dependencies.
func (p *Popup) ArrangeSurface(show bool, r gooey.Rect) {
	if !show {
		gooey.ArrangeChild(p.surf, gooey.Rect{X: r.X, Y: r.Y})
		return
	}
	gooey.MeasureChild(p.surf, gooey.Size{W: r.W, H: r.H})
	gooey.ArrangeChild(p.surf, r)
}

// HandleKey is the owner's key fall-through while the popup may be
// open: route the keys your own handling declined here. Esc dismisses
// and is consumed; with Modal set every other declined key is swallowed
// — an open popup is modal, so the page's gestures cannot fire
// underneath it. Returns false while closed, so the owner can call it
// unconditionally.
func (p *Popup) HandleKey(ev input.KeyEvent) bool {
	if !p.IsOpen() {
		return false
	}
	if ev == input.Named(input.KeyEsc) {
		p.Dismiss()
		return true
	}
	return p.Modal
}

// HandleMouse is the pointer fall-through: route the events your own
// hit handling declined here. While open, a press anywhere the owner
// did not claim dismisses AND is consumed — the capture routed it here
// precisely so it never reaches, or activates, what is underneath — and
// everything else (the release/click residue of the dismissing gesture,
// wheel events) is swallowed for the same reason. Returns false while
// closed.
func (p *Popup) HandleMouse(ev input.MouseEvent) bool {
	if !p.IsOpen() {
		return false
	}
	if ev.Kind == input.MousePress {
		p.Dismiss()
	}
	return true
}

// popupSurface is the visible box: an ordinary leaf, so its paint node
// pre-clears and covers its rectangle — the overlay contract — and the
// draw func's reads are its dependencies. Its Render reads the open
// property FIRST, unconditionally: that read, recorded from the very
// first zero-size evaluation, is what makes the first Open schedule a
// frame with no external carrier.
type popupSurface struct {
	gooey.Base
	pop  *Popup
	draw func(*gooey.Frame, gooey.Rect)
}

func (s *popupSurface) Measure(avail gooey.Size) gooey.Size { return avail }

func (s *popupSurface) Render(f *gooey.Frame) {
	open := s.pop.IsOpen() // before ANY early return: the subscription carrier
	b := s.Bounds()
	if !open || b.W <= 0 || b.H <= 0 || s.draw == nil {
		return
	}
	s.draw(f, b)
}

// PopupSide is a popup's preferred side of its anchor.
type PopupSide uint8

const (
	PopupBelow PopupSide = iota
	PopupAbove
)

// PlacePopup is anchored placement, the Tooltip logic generalized: a
// popup of size sz goes on the preferred side of anchor, left-aligned
// with it, FLIPPED to the other side when the preferred one has no room
// inside bounds, and CLAMPED into bounds on both axes — a popup near an
// edge slides along it (possibly over its anchor) rather than falling
// off screen. Pure geometry: owners with deliberate other policies (the
// menu dropdown never flips — it clips) keep their own.
func PlacePopup(anchor gooey.Rect, sz gooey.Size, bounds gooey.Rect, side PopupSide) gooey.Rect {
	w := min(sz.W, bounds.W)
	h := min(sz.H, bounds.H)
	x := clamp(anchor.X, bounds.X, bounds.X+bounds.W-w)
	below := anchor.Y + anchor.H
	above := anchor.Y - h
	y := below
	if side == PopupAbove {
		y = above
	}
	if side == PopupBelow && y+h > bounds.Y+bounds.H {
		y = above
	}
	if side == PopupAbove && y < bounds.Y {
		y = below
	}
	y = clamp(y, bounds.Y, bounds.Y+bounds.H-h)
	return gooey.Rect{X: x, Y: y, W: w, H: h}
}
