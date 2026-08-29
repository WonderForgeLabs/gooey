package components

import (
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/render"
)

// toyOwner is the smallest possible Popup customer: a one-row focusable
// strip whose enter key-opens and whose press mouse-opens a box below
// it. Deliberately, NOTHING on the page reads IsOpen from a Render —
// its own Render paints static dashes — so these tests exercise the
// primitive's own subscription carrier, not an accidental one.
type toyOwner struct {
	gooey.Base
	gooey.FocusState
	pop *Popup
}

func (o *toyOwner) popup() *Popup {
	if o.pop == nil {
		o.pop = NewPopup(o, func(f *gooey.Frame, b gooey.Rect) {
			for y := b.Y; y < b.Y+b.H; y++ {
				f.Cells.SetString(b.X, y, clipCols("POPUP!", b.W), render.Style{Reverse: true})
			}
		})
		o.pop.Modal = true
	}
	return o.pop
}

// SetFocusManager forwards the gooey.FocusHost seam to the popup — the
// one line every owner writes to give the primitive focus and capture.
func (o *toyOwner) SetFocusManager(fm *gooey.FocusManager) { o.popup().SetFocusManager(fm) }

func (o *toyOwner) ChildComponents() []gooey.Component {
	o.popup()
	return []gooey.Component{o.pop.Surface()}
}

func (o *toyOwner) Measure(avail gooey.Size) gooey.Size {
	return gooey.Size{W: avail.W, H: min(1, avail.H)}
}

func (o *toyOwner) Arrange(r gooey.Rect) {
	o.Base.Arrange(r)
	p := o.popup()
	p.ArrangeSurface(p.IsOpen(), gooey.Rect{X: r.X, Y: r.Y + 1, W: 6, H: 2})
}

func (o *toyOwner) Render(f *gooey.Frame) {
	b := o.Bounds()
	for x := b.X; x < b.X+b.W; x++ {
		f.Cells.Set(x, b.Y, '-', render.Style{})
	}
}

func (o *toyOwner) HandleKey(ev input.KeyEvent) bool {
	if !o.popup().IsOpen() && ev == input.Named(input.KeyEnter) {
		o.popup().Open(nil) // key-open: nothing to restore
		return true
	}
	return o.popup().HandleKey(ev)
}

func (o *toyOwner) HandleMouse(ev input.MouseEvent) bool {
	if ev.Kind == input.MousePress && !o.popup().IsOpen() {
		b := o.Bounds()
		if ev.Y == b.Y {
			o.popup().Open(o.popup().MouseOpenRestore())
			return true
		}
	}
	return o.popup().HandleMouse(ev)
}

// toyPage: content under where the popup drops, two buttons elsewhere,
// the owner declared LAST (document order is z-order).
func toyPage() (*toyOwner, *Button, *Button, gooey.Component) {
	owner := &toyOwner{}
	under := gooey.L(&Text{Content: Str(strings.Repeat("#", 20))}, gooey.Layout{Top: 1})
	btn := &Button{Content: Str("btn"), Click: gooey.Command(func() {})}
	btn2 := &Button{Content: Str("two"), Click: gooey.Command(func() {})}
	page := &Canvas{Children: []gooey.Component{
		under,
		gooey.L(btn, gooey.Layout{Top: 3, Left: 12}),
		gooey.L(btn2, gooey.Layout{Top: 4, Left: 12}),
		owner,
	}}
	return owner, btn, btn2, page
}

// The collapsed-overlay hazard, pinned at the primitive: the FIRST open
// must schedule a frame and paint the surface even though no
// always-painted node on the page reads IsOpen — the surface's own
// zero-size first evaluation carries the subscription. Before the
// primitive, every customer needed an app-side carrier (the browser
// picker's hint computed) or an owner whose chrome happened to read the
// flag (the menu bar's title highlight).
func TestPopupFirstOpenSchedulesItsOwnFrame(t *testing.T) {
	owner, _, _, page := toyPage()
	c := gooey.NewComposer(page, 20, 5)
	c.Focus().SetFocus(owner)
	c.Frame()

	scheduled := 0
	c.OnInvalidate(func() { scheduled++ })
	if !c.HandleKey(input.Named(input.KeyEnter)) {
		t.Fatal("enter on the focused owner was not consumed")
	}
	if scheduled == 0 {
		t.Fatal("the first Open scheduled no frame — the surface's subscription carrier is broken")
	}
	_, painted := c.Frame()
	if painted != 1 {
		t.Fatalf("the first open painted %d components, want 1 (the surface alone)", painted)
	}
	if got := row(c.Cells(), 1); !strings.HasPrefix(got, "POPUP!") {
		t.Fatalf("row 1 = %q, want the popup over the content", got)
	}
	if _, painted := c.Frame(); painted != 0 {
		t.Fatalf("settled frame painted %d, want 0", painted)
	}
}

// Esc dismisses: consumed, capture released, the vacated cells restored
// from what was beneath, and the frame settles.
func TestPopupEscDismissesAndRestoresBeneath(t *testing.T) {
	owner, _, _, page := toyPage()
	c := gooey.NewComposer(page, 20, 5)
	c.Focus().SetFocus(owner)
	c.Frame()
	c.HandleKey(input.Named(input.KeyEnter))
	c.Frame()
	if c.Focus().Captured() != gooey.Component(owner) {
		t.Fatal("the open popup's owner does not hold the pointer capture")
	}

	if !c.HandleKey(input.Named(input.KeyEsc)) {
		t.Fatal("esc was not consumed by the open popup")
	}
	c.Frame()
	if owner.popup().IsOpen() {
		t.Fatal("esc did not close the popup")
	}
	if got := c.Focus().Captured(); got != nil {
		t.Fatalf("the pointer is still captured by %T after dismiss", got)
	}
	if got := row(c.Cells(), 1); got != strings.Repeat("#", 20) {
		t.Fatalf("row 1 after dismiss = %q — the content beneath did not restore", got)
	}
	if _, painted := c.Frame(); painted != 0 {
		t.Fatalf("settled frame painted %d, want 0", painted)
	}
}

// The wave-2 focus rules, pinned at the primitive: a MOUSE open (focus-
// follows-click already moved focus to the owner) restores the
// component the manager remembers losing; a KEY open (the owner held
// focus legitimately) restores nothing.
func TestPopupMouseOpenRestoresFocusKeyOpenDoesNot(t *testing.T) {
	owner, btn, _, page := toyPage()
	c := gooey.NewComposer(page, 20, 5)
	c.Focus().SetFocus(btn)
	c.Frame()

	// Mouse open: press on the owner's row.
	c.HandleMouse(input.MouseEvent{Kind: input.MousePress, X: 2, Y: 0})
	c.HandleMouse(input.MouseEvent{Kind: input.MouseRelease, X: 2, Y: 0})
	c.Frame()
	if c.Focus().Focused() != gooey.Component(owner) {
		t.Fatal("the open popup's owner does not hold focus")
	}
	c.HandleKey(input.Named(input.KeyEsc))
	if got := c.Focus().Focused(); got != gooey.Component(btn) {
		t.Fatalf("focus after a mouse-open dismiss is %T, want the button it was taken from", got)
	}

	// Key open: the owner has focus already; esc leaves it there.
	c.Focus().SetFocus(owner)
	c.HandleKey(input.Named(input.KeyEnter))
	c.HandleKey(input.Named(input.KeyEsc))
	if got := c.Focus().Focused(); got != gooey.Component(owner) {
		t.Fatalf("focus after a key-open dismiss is %T, want the owner (nothing to restore)", got)
	}
}

// A dismissal after focus already moved on must not yank focus back:
// the popup restores only while the owner still holds focus.
func TestPopupDismissAfterFocusMovedOnLeavesFocusAlone(t *testing.T) {
	owner, btn, btn2, page := toyPage()
	c := gooey.NewComposer(page, 20, 6)
	c.Focus().SetFocus(btn)
	c.Frame()
	c.HandleMouse(input.MouseEvent{Kind: input.MousePress, X: 2, Y: 0})
	c.HandleMouse(input.MouseEvent{Kind: input.MouseRelease, X: 2, Y: 0}) // restore = btn

	c.Focus().SetFocus(btn2) // the app moved focus while the popup was up
	owner.popup().Dismiss()
	if got := c.Focus().Focused(); got != gooey.Component(btn2) {
		t.Fatalf("dismiss moved focus to %T — it must restore only while the owner still holds it", got)
	}
}

// While open, the owner holds the capture: a press anywhere the owner
// did not claim dismisses, is consumed, and must NOT reach — or
// activate — what is underneath.
func TestPopupClickOutsideDismissesAndIsConsumed(t *testing.T) {
	clicked := 0
	owner, btn, _, page := toyPage()
	btn.Click = gooey.Command(func() { clicked++ })
	c := gooey.NewComposer(page, 20, 5)
	c.Focus().SetFocus(owner)
	c.Frame()
	c.HandleKey(input.Named(input.KeyEnter))
	c.Frame()

	bb := btn.Bounds()
	c.HandleMouse(input.MouseEvent{Kind: input.MousePress, X: bb.X + 1, Y: bb.Y})
	c.HandleMouse(input.MouseEvent{Kind: input.MouseRelease, X: bb.X + 1, Y: bb.Y})
	if owner.popup().IsOpen() {
		t.Fatal("a press elsewhere did not dismiss the popup")
	}
	if clicked != 0 {
		t.Fatal("the press that dismissed the popup leaked to the button underneath")
	}
}

// Modal: keys the owner declined are swallowed while open, so a page
// gesture cannot fire underneath — and a KeyBinding higher up the chain
// never sees them.
func TestPopupModalSwallowsUnhandledKeys(t *testing.T) {
	fired := 0
	owner, _, _, page := toyPage()
	page.(*Canvas).Attach(&gooey.KeyBinding{
		Gesture: input.Rune('q'),
		Command: gooey.Command(func() { fired++ }),
	})
	c := gooey.NewComposer(page, 20, 5)
	c.Focus().SetFocus(owner)
	c.Frame()
	c.HandleKey(input.Named(input.KeyEnter))
	c.Frame()

	if !c.HandleKey(input.Rune('q')) {
		t.Fatal("an unhandled key escaped the modal popup")
	}
	if fired != 0 {
		t.Fatal("the page's q binding fired under the open popup")
	}
	if _, painted := c.Frame(); painted != 0 {
		t.Fatalf("a swallowed key painted %d components", painted)
	}

	// Non-modal: the same key keeps bubbling.
	owner.popup().Modal = false
	if c.HandleKey(input.Rune('x')) {
		t.Fatal("a non-modal popup swallowed a key it does not own")
	}
	owner.popup().Modal = true
}

// PlacePopup: preferred side, flip-to-fit, and clamping on both axes —
// the tooltip logic, generalized to any size and either side.
func TestPlacePopupPlacement(t *testing.T) {
	bounds := gooey.Rect{X: 0, Y: 0, W: 40, H: 10}
	for _, tc := range []struct {
		name   string
		anchor gooey.Rect
		sz     gooey.Size
		side   PopupSide
		want   gooey.Rect
	}{
		{"below, fits", gooey.Rect{X: 5, Y: 2, W: 8, H: 1}, gooey.Size{W: 10, H: 3}, PopupBelow,
			gooey.Rect{X: 5, Y: 3, W: 10, H: 3}},
		{"below, flips above at the bottom", gooey.Rect{X: 5, Y: 8, W: 8, H: 1}, gooey.Size{W: 10, H: 3}, PopupBelow,
			gooey.Rect{X: 5, Y: 5, W: 10, H: 3}},
		{"above, fits", gooey.Rect{X: 5, Y: 6, W: 8, H: 1}, gooey.Size{W: 10, H: 3}, PopupAbove,
			gooey.Rect{X: 5, Y: 3, W: 10, H: 3}},
		{"above, flips below at the top", gooey.Rect{X: 5, Y: 1, W: 8, H: 1}, gooey.Size{W: 10, H: 3}, PopupAbove,
			gooey.Rect{X: 5, Y: 2, W: 10, H: 3}},
		{"clamped into the right edge", gooey.Rect{X: 36, Y: 2, W: 4, H: 1}, gooey.Size{W: 10, H: 1}, PopupBelow,
			gooey.Rect{X: 30, Y: 3, W: 10, H: 1}},
		{"wider than bounds: clipped and pinned left", gooey.Rect{X: 5, Y: 2, W: 8, H: 1}, gooey.Size{W: 60, H: 1}, PopupBelow,
			gooey.Rect{X: 0, Y: 3, W: 40, H: 1}},
		{"no room either side: clamped over the anchor", gooey.Rect{X: 5, Y: 0, W: 8, H: 10}, gooey.Size{W: 10, H: 4}, PopupBelow,
			gooey.Rect{X: 5, Y: 0, W: 10, H: 4}},
	} {
		if got := PlacePopup(tc.anchor, tc.sz, bounds, tc.side); got != tc.want {
			t.Errorf("%s: PlacePopup = %+v, want %+v", tc.name, got, tc.want)
		}
	}
}
