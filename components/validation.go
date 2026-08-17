package components

import (
	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// errorRed is the shared error hue: the TextBox's invalid text and the
// ValidationMarker's message both derive from it, so a form's error
// state reads as one visual voice at any color depth (the buffer stays
// 24-bit; downsampling happens at the wire).
var errorRed = render.RGB(235, 90, 85)

// ValidationMarker is the FLOATING error display — the compact-layout
// variant, not the default. Gooey's primary error display is inline,
// the MAUI arrangement: an ordinary bound Text in the form layout
// (<Text Content="{{.NameErr}}"/>, optionally with Visibility bound to
// a has-error bool so the row collapses while valid). Reach for the
// marker when the layout has no room for an error row — dense grids,
// single-line toolbars — and the message must float above the page
// instead.
//
// It is also the AdornmentLayer's second customer, after Tooltip — the
// one issue #91 named from the start. It is a NON-VISUAL attachment
// like Tooltip and KeyBinding: in markup it hangs off the input it
// marks,
//
//	<TextBox Text="{{.Name}}" Error="{{.NameErr}}">
//	  <ValidationMarker/>
//	</TextBox>
//
// and in code it is attached with host.Attach(&ValidationMarker{...}).
// An omitted Error adopts the host TextBox's own handle, so the common
// form names the error property once.
//
// Unlike the Tooltip — transient, raised and dismissed by hover — the
// marker is PERSISTENT: its floating message lives in the layer for as
// long as the marker is attached, and whether it is VISIBLE is decided
// by the error property alone. The popup's Render reads the error
// before anything else (the Popup primitive's subscription-carrier
// rule), so the very first failing edit schedules a frame with no
// external help; while the error is empty the popup is arranged to a
// zero rect and paints, occupies, and hits nothing. Empty-to-message is
// paint damage on the popup; message-to-empty vacates its cells and the
// composer restores what was beneath.
//
// Placement is the shared popup policy (PlacePopup): the message hugs
// the host's left edge on the row below, flipping above when the screen
// runs out. The popup is HitTestTransparent — an error message must
// never trap the pointer on its way to the field being fixed.
type ValidationMarker struct {
	gooey.Base
	// Error is what the marker shows: empty = valid = nothing. Left nil,
	// it adopts the host TextBox's Error at attach time.
	Error *prop.Property[string]
	// Style paints the message. The zero value is white on the shared
	// error red.
	Style render.Style

	host  gooey.Component     // via gooey.Hosted
	mgr   *gooey.FocusManager // via gooey.FocusHost
	layer *AdornmentLayer     // where the popup lives, nil until placed
	pop   *markerPopup
}

func (m *ValidationMarker) Measure(gooey.Size) gooey.Size { return gooey.Size{} }
func (m *ValidationMarker) Render(*gooey.Frame)           {}
func (m *ValidationMarker) NonVisual() bool               { return true }

// SetHost receives the component this marker is attached to
// (gooey.Hosted) — the anchor its message is placed against. A marker
// without its own Error handle adopts the host TextBox's here: one
// property named once, read by both.
func (m *ValidationMarker) SetHost(w gooey.Component) {
	m.host = w
	if m.Error == nil {
		if tb, ok := w.(*TextBox); ok {
			m.Error = tb.Error
		}
	}
}

// SetFocusManager receives the input tree (gooey.FocusHost). The walk
// runs on the first build and on every re-sync, which makes it the
// attach seam: the marker places its popup in the page's AdornmentLayer
// the first time the walk reaches it, and a marker whose popup was
// dropped (its host left the tree and returned, an ItemsView row
// re-realized) re-places it on the next walk. Idempotent — a popup
// already up is left alone.
func (m *ValidationMarker) SetFocusManager(fm *gooey.FocusManager) {
	m.mgr = fm
	m.ensurePlaced()
}

// IsShown reports whether the marker's message is currently visible —
// placed in a layer AND showing a non-empty error.
func (m *ValidationMarker) IsShown() bool {
	return m.pop != nil && getStr(m.Error) != ""
}

func (m *ValidationMarker) ensurePlaced() {
	if m.pop != nil {
		return
	}
	// Not placed — no host, no input tree, or no AdornmentLayer on the
	// page — means errors show only in the TextBox, and the next re-sync
	// asks again.
	pop := &markerPopup{m: m}
	if layer := attachAdornment(m.host, m.mgr, pop); layer != nil {
		m.layer, m.pop = layer, pop
	}
}

// markerPopup is the floating message: an ordinary leaf hosted by the
// AdornmentLayer. It is a PERSISTENT adornment — an anchor that goes
// invisible hides it (zero rect) rather than dropping it, so the
// message survives a collapsed-and-reopened pane without anyone
// re-adding it.
type markerPopup struct {
	gooey.Base
	m *ValidationMarker
}

func (p *markerPopup) Anchor() gooey.Component { return p.m.host }

// AdornmentPersists opts out of the layer's drop-on-invisible policy:
// the marker's lifetime belongs to its attachment, not to a gesture.
func (p *markerPopup) AdornmentPersists() bool { return true }

// HitTestTransparent: the pointer must reach the field being fixed, not
// the message explaining why.
func (p *markerPopup) HitTestTransparent() bool { return true }

// orphaned: the layer dropped the popup because the host truly left the
// tree. Forget it, so the next input-tree walk can place a fresh one if
// the host comes back.
func (p *markerPopup) orphaned() { p.m.pop, p.m.layer = nil, nil }

// text reads the error. From Render the read is the popup's paint
// dependency; from Measure/Place it is layout's per-frame re-ask and
// records nothing — call site decides, as always.
func (p *markerPopup) text() string { return getStr(p.m.Error) }

func (p *markerPopup) size() gooey.Size {
	msg := p.text()
	if msg == "" {
		return gooey.Size{}
	}
	return gooey.Size{W: len([]rune(msg)) + 2, H: 1}
}

func (p *markerPopup) Measure(avail gooey.Size) gooey.Size {
	sz := p.size()
	return gooey.Size{W: min(sz.W, avail.W), H: min(sz.H, avail.H)}
}

// Place puts the message under the host's left edge, flipped above and
// clamped by the shared popup placement. With no error it collapses to
// a zero rect at the anchor — present, subscribed, occupying nothing —
// the same posture Popup's closed surface keeps.
func (p *markerPopup) Place(anchor, layer gooey.Rect) gooey.Rect {
	sz := p.size()
	if sz.W == 0 {
		return gooey.Rect{X: anchor.X, Y: anchor.Y}
	}
	return PlacePopup(anchor, sz, layer, PopupBelow)
}

func (p *markerPopup) Render(f *gooey.Frame) {
	msg := p.text() // before ANY early return: the subscription carrier
	b := p.Bounds()
	if msg == "" || b.W <= 0 || b.H <= 0 {
		return
	}
	paintBanner(f, b, msg, p.m.Style, render.Style{Fg: render.RGB(255, 255, 255), Bg: errorRed})
}
