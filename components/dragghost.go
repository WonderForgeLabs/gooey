package components

import (
	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// DragGhost is the AdornmentLayer's first FREE customer — the label that
// travels with the pointer for the length of a drag ("3 files", "Deploy
// task"), and the headline unlock issue #177 was filed for. It is an
// ordinary leaf component with an ordinary paint node; the only thing
// unusual about it is that its position comes from the pointer rather
// than from the tree, which it declares by implementing
// gooey.PointerFollower.
//
// It is emphatically NOT a cursor. The real pointer cannot be hidden by
// any portable escape sequence, so a ghost drawn UNDER it would
// double-image and quantize to cells; the default Offset puts the label
// down and to the right of the pointer cell instead, out from under the
// glyph the emulator is drawing, which is also where every desktop
// toolkit puts one.
//
// LIFETIME IS THE COST MODEL. A ghost exists only while a gesture is
// running: Show puts it in the page's layer and starts it following,
// Hide stops it following and takes it out again. That is what bounds
// the ?1003h wakeup — a motion report per cell crossed schedules a frame
// only while a ghost is up, and nothing at all the rest of the time.
// The two halves are both real and neither is redundant: leaving the
// layer removes the paint node (and with it the pointer observer), while
// clearing the follow flag parks a ghost that is still in the layer at a
// zero rect. An owner that recycles one ghost across many drags wants
// the second; most owners want Show/Hide and never think about it.
//
//	// in a press handler, having decided this is a drag
//	ghost.Label.Set("3 files")
//	ghost.Show(mgr)
//	// in the release handler
//	ghost.Hide()
//
// A ghost is HitTestTransparent: it hangs beside the pointer by
// definition, so a ghost that could be hit would swallow the events of
// the gesture that raised it.
type DragGhost struct {
	gooey.Base
	// Label is what the ghost says. Bound, so an owner that updates the
	// count mid-drag repaints the ghost alone.
	Label *prop.Property[string]
	// Style paints it. The zero value is reverse video, the same
	// fallback the tooltip and toast banners take.
	Style render.Style
	// Offset is where the label sits relative to the pointer's cell, in
	// cells. The zero value means the default {1, 1} — down and right,
	// clear of the emulator's own pointer glyph. Use SetOffset if you
	// really do want a zero offset, i.e. the ghost under the pointer.
	Offset gooey.Size

	offsetSet bool // Offset was chosen deliberately, zero value included
	active    *prop.Property[bool]
	layer     *AdornmentLayer
}

// SetOffset places the label relative to the pointer cell and records
// that the choice was deliberate, so an explicit zero offset is honored
// rather than read as "unset" and replaced by the default.
func (g *DragGhost) SetOffset(dx, dy int) {
	g.Offset, g.offsetSet = gooey.Size{W: dx, H: dy}, true
}

func (g *DragGhost) offset() gooey.Size {
	if !g.offsetSet && g.Offset == (gooey.Size{}) {
		return gooey.Size{W: 1, H: 1}
	}
	return g.Offset
}

// Anchor is nil: a ghost is free, so the layer never asks (see
// AdornmentLayer). It is here to satisfy Adornment, nothing more.
func (g *DragGhost) Anchor() gooey.Component { return nil }

// FollowsPointer is the gesture flag, and reading it is what subscribes
// the Composer's observer: flipping it schedules the frame that starts
// or stops the per-motion wake.
func (g *DragGhost) FollowsPointer() bool { return g.state().Get() }

// HitTestTransparent: the ghost sits beside the pointer for the whole
// gesture, so hit-testing must see straight through it.
func (g *DragGhost) HitTestTransparent() bool { return true }

func (g *DragGhost) state() *prop.Property[bool] {
	if g.active == nil {
		g.active = prop.NewSource(false)
	}
	return g.active
}

func (g *DragGhost) text() string {
	if g.Label == nil {
		return ""
	}
	return g.Label.Get()
}

func (g *DragGhost) width() int { return len([]rune(g.text())) + 2 }

func (g *DragGhost) Measure(avail gooey.Size) gooey.Size {
	return gooey.Size{W: min(g.width(), avail.W), H: min(1, avail.H)}
}

// Place offsets the label from the pointer's cell and clamps it into the
// layer, so a drag into a screen corner slides the ghost along the edge
// instead of pushing it off. No flipping: a ghost that jumped to the
// other side of the pointer near an edge would read as a second, rival
// cursor, which is exactly the impression this component must not give.
func (g *DragGhost) Place(against, layer gooey.Rect) gooey.Rect {
	off := g.offset()
	w := min(g.width(), layer.W)
	h := min(1, layer.H)
	if w <= 0 || h <= 0 {
		return gooey.Rect{X: against.X, Y: against.Y}
	}
	return gooey.Rect{
		X: clamp(against.X+off.W, layer.X, layer.X+layer.W-w),
		Y: clamp(against.Y+off.H, layer.Y, layer.Y+layer.H-h),
		W: w,
		H: h,
	}
}

// Render paints the label. It reads Label BEFORE the bounds early-return
// — the Popup primitive's subscription-carrier rule — so an owner that
// retitles a parked ghost still schedules the frame that will show it.
func (g *DragGhost) Render(f *gooey.Frame) {
	msg := g.text()
	b := g.Bounds()
	if b.W <= 0 || b.H <= 0 {
		return
	}
	paintBanner(f, b, msg, g.Style, render.Style{Reverse: true})
}

// Show puts the ghost in the page's adornment layer and starts it
// following the pointer. It reports false when the page declares no
// layer — a supported configuration meaning "this app shows no
// adornments", the same answer Tooltip and ValidationMarker give — and
// when there is no input tree to find one through.
//
// Idempotent: a second Show during the same gesture re-asserts the
// follow flag and does not add the ghost twice.
func (g *DragGhost) Show(mgr *gooey.FocusManager) bool {
	if g.layer == nil {
		if mgr == nil {
			return false
		}
		layer := findAdornmentLayer(mgr.Root())
		if layer == nil {
			return false
		}
		g.layer = layer
		layer.Add(g)
	}
	if !g.state().Get() { // prop.Set does not compare; a re-Show is not damage
		g.state().Set(true)
	}
	return true
}

// Hide ends the gesture: the ghost stops following and leaves the layer,
// which is what takes its paint node — and the pointer observer with it
// — back out of the composition. The vacated cells are restored by the
// layer's own structural sweep, like any departing adornment.
// Idempotent.
func (g *DragGhost) Hide() {
	if g.state().Get() {
		g.state().Set(false)
	}
	if g.layer != nil {
		g.layer.Remove(g)
		g.layer = nil
	}
}
