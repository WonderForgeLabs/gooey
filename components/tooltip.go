package components

import (
	"sync"
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// DefaultTooltipDelay is how long the pointer rests on a component
// before its tooltip shows, when the Tooltip does not say otherwise.
const DefaultTooltipDelay = 600 * time.Millisecond

// Tooltip is a hover-triggered text adornment — the first customer of
// the AdornmentLayer. It is a NON-VISUAL attachment like KeyBinding: in
// markup it hangs off the component it describes, either as a child
//
//	<Button Content="save" Click="{{.Save}}">
//	  <Tooltip Text="write the file"/>
//	</Button>
//
// or through the shorthand any element accepts, Tooltip="write the
// file". The framework routes hover to it (gooey.HoverWatcher): the
// pointer resting on the host for Delay shows the tip adjacent to the
// host — below, flipping above when the screen runs out — and it
// dismisses on hover-out, on any key, and on any press.
//
// The tip itself is shown in the page's AdornmentLayer, found by
// walking the tree from the root at show time; a page without a layer
// shows no tooltips (declare one as the root's last child). If the host
// declares a KeyBinding, the tip renders the gesture as a dim hint in
// the canonical spelling — the MenuItem hint pattern; Gesture overrides
// it for hosts whose binding lives elsewhere (a page-level gesture).
//
// The delay is the Timer discipline: a goroutine per hover that posts
// the show back to the UI loop, and Start's stop func closes AND joins,
// so after Composer.Close no show can ever arrive. A composition that
// was never started has no way to marshal a delayed show, so it shows
// immediately on hover-in — degraded, not broken. A negative Delay asks
// for exactly that on purpose.
type Tooltip struct {
	gooey.Base
	// Text is what the tip says. Bound text stays live: a tip that is up
	// repaints when the property changes.
	Text *prop.Property[string]
	// Delay is the hover-rest time before showing. Zero means
	// DefaultTooltipDelay; negative shows immediately.
	Delay time.Duration
	// Gesture is a display hint in the canonical gesture spelling
	// ("ctrl+s"), overriding the host's own KeyBinding lookup. Display
	// only — wiring the key is still a KeyBinding's job.
	Gesture string
	// Style paints the tip. The zero value is reverse video, which reads
	// as an overlay at any color depth.
	Style render.Style

	host  gooey.Component     // via gooey.Hosted
	mgr   *gooey.FocusManager // via gooey.FocusHost
	layer *AdornmentLayer     // where the popup is showing, nil when hidden
	pop   *tipPopup

	post func(func())
	done chan struct{}
	wg   sync.WaitGroup
	gen  int // hover generation: bumped on every edge, stales pending shows
}

func (t *Tooltip) Measure(gooey.Size) gooey.Size { return gooey.Size{} }
func (t *Tooltip) Render(*gooey.Frame)           {}
func (t *Tooltip) NonVisual() bool               { return true }

// SetHost receives the component this tooltip is attached to
// (gooey.Hosted) — the anchor its popup is placed against.
func (t *Tooltip) SetHost(w gooey.Component) { t.host = w }

// SetFocusManager receives the input tree (gooey.FocusHost) — the seam
// the tooltip finds the page's AdornmentLayer through.
func (t *Tooltip) SetFocusManager(m *gooey.FocusManager) { t.mgr = m }

// IsShown reports whether the tip is currently up.
func (t *Tooltip) IsShown() bool { return t.pop != nil }

// Start arms the delay timer: post is the only path back to the UI
// loop, and the returned stop closes the gate and joins every delay
// goroutine still in flight — once stop returns, no show ever arrives.
func (t *Tooltip) Start(post func(func())) func() {
	t.post = post
	done := make(chan struct{})
	t.done = done
	return func() {
		t.post = nil
		close(done)
		t.wg.Wait()
	}
}

func (t *Tooltip) delay() time.Duration {
	if t.Delay == 0 {
		return DefaultTooltipDelay
	}
	return t.Delay
}

// PointerOver is the framework's hover notification (gooey.HoverWatcher).
func (t *Tooltip) PointerOver(over bool) {
	t.gen++
	if !over {
		t.hide()
		return
	}
	if t.pop != nil {
		return
	}
	d := t.delay()
	if d <= 0 || t.post == nil {
		t.show()
		return
	}
	gen, post, done := t.gen, t.post, t.done
	t.wg.Add(1)
	go func() {
		defer t.wg.Done()
		tm := time.NewTimer(d)
		defer tm.Stop()
		select {
		case <-done:
		case <-tm.C:
			// The join in stop makes this safe: a timer that already
			// fired posts before stop returns, and one that has not is
			// cancelled by done. Either way, stop ⇒ no shows, ever.
			select {
			case <-done:
			default:
				post(func() { t.showIf(gen) })
			}
		}
	}()
}

// Interrupted is the framework's activity notification: any key or
// press takes the tip down and cancels a pending show. It does not
// reset the hover itself, so the tip stays down until the pointer
// leaves the host and comes back — a keystroke is a person working, not
// a person asking again.
func (t *Tooltip) Interrupted() {
	t.gen++
	t.hide()
}

// showIf runs on the UI loop, posted by the delay goroutine: it shows
// only if no hover edge or interruption happened since the timer was
// armed.
func (t *Tooltip) showIf(gen int) {
	if gen == t.gen {
		t.show()
	}
}

func (t *Tooltip) show() {
	if t.pop != nil || t.host == nil || t.mgr == nil {
		return
	}
	layer := findAdornmentLayer(t.mgr.Root())
	if layer == nil {
		return // no AdornmentLayer on the page: nothing to show in
	}
	t.layer = layer
	t.pop = &tipPopup{tip: t}
	layer.Add(t.pop)
}

func (t *Tooltip) hide() {
	if t.pop == nil {
		return
	}
	t.layer.Remove(t.pop)
	t.pop, t.layer = nil, nil
}

// gestureHint is the key hint the tip renders: the explicit Gesture, or
// the first KeyBinding declared on the host. Display only.
func (t *Tooltip) gestureHint() string {
	if t.Gesture != "" {
		return t.Gesture
	}
	if a, ok := t.host.(gooey.Attacher); ok {
		for _, at := range a.Attachments() {
			if kb, ok := at.(*gooey.KeyBinding); ok {
				return kb.Gesture.String()
			}
		}
	}
	return ""
}

// tipPopup is the visible tip: an ordinary leaf hosted by the
// AdornmentLayer, so its paint node pre-clears and covers its rectangle
// — the overlay contract — and its Render reads the Tooltip's Text,
// which keeps a bound tip live while it is up.
type tipPopup struct {
	gooey.Base
	tip *Tooltip
}

func (p *tipPopup) Anchor() gooey.Component { return p.tip.host }

// HitTestTransparent: a tooltip is never interactive; the pointer sees
// through it, so a tip can never trap the hover that raised it.
func (p *tipPopup) HitTestTransparent() bool { return true }

// orphaned: the layer dropped this popup because the host vanished; the
// tooltip must forget it or it would never show again.
func (p *tipPopup) orphaned() { p.tip.pop, p.tip.layer = nil, nil }

func (p *tipPopup) text() string {
	if p.tip.Text == nil {
		return ""
	}
	return p.tip.Text.Get()
}

func (p *tipPopup) width() int {
	w := len([]rune(p.text())) + 2
	if g := p.tip.gestureHint(); g != "" {
		w += len([]rune(g)) + 2
	}
	return w
}

func (p *tipPopup) Measure(avail gooey.Size) gooey.Size {
	return gooey.Size{W: min(p.width(), avail.W), H: min(1, avail.H)}
}

// Place puts the tip adjacent to its anchor: left-aligned under it,
// clamped into the layer horizontally, flipped ABOVE when the row below
// falls off the screen — flip-to-fit.
func (p *tipPopup) Place(anchor, layer gooey.Rect) gooey.Rect {
	w := min(p.width(), layer.W)
	x := clamp(anchor.X, layer.X, layer.X+layer.W-w)
	y := anchor.Y + anchor.H
	if y >= layer.Y+layer.H {
		y = anchor.Y - 1
	}
	if y < layer.Y {
		y = layer.Y
	}
	return gooey.Rect{X: x, Y: y, W: w, H: 1}
}

func (p *tipPopup) Render(f *gooey.Frame) {
	b := p.Bounds()
	if b.W <= 0 || b.H <= 0 {
		return
	}
	st := p.tip.Style
	if st == (render.Style{}) {
		st = render.Style{Reverse: true}
	}
	for x := b.X; x < b.X+b.W; x++ {
		f.Cells.Set(x, b.Y, ' ', st)
	}
	f.Cells.SetString(b.X, b.Y, clipRunes(" "+p.text()+" ", b.W), st)
	if g := p.tip.gestureHint(); g != "" {
		hs := st
		hs.Dim = true
		hint := g + " "
		if hx := b.X + b.W - len([]rune(hint)); hx > b.X {
			f.Cells.SetString(hx, b.Y, hint, hs)
		}
	}
}

// findAdornmentLayer walks the live tree for the page's layer. Overlays
// are declared last, so the walk searches later siblings first.
func findAdornmentLayer(w gooey.Component) *AdornmentLayer {
	if l, ok := w.(*AdornmentLayer); ok {
		return l
	}
	if c, ok := w.(gooey.Container); ok {
		kids := c.ChildComponents()
		for i := len(kids) - 1; i >= 0; i-- {
			if l := findAdornmentLayer(kids[i]); l != nil {
				return l
			}
		}
	}
	return nil
}
