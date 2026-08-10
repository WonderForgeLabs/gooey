package components

import (
	"sync"
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/render"
)

// DefaultToastDuration is how long a toast stays up when neither the
// host nor the Show call says otherwise.
const DefaultToastDuration = 3 * time.Second

// ToastHost is the notification layer: a transparent overlay the app
// places as the LAST child of its root, so document order — which is
// z-order — puts every toast above the page. Show stacks a transient
// message in the top-right corner; an auto-dismiss timer takes it down
// again, and the Composer's restore pass repaints whatever the toast
// was covering.
//
// The host paints nothing and declares no background, so a page that
// never shows a toast pays nothing for hosting the layer. Each toast is
// an ordinary leaf child, realized through the same Dynamic re-sync a
// list uses: showing one paints one, dismissing one repaints exactly
// the components its rectangle was covering.
//
// Auto-dismiss is the Timer discipline. The host is a Startable; its
// goroutines never touch the property graph — they post the dismissal
// to the UI loop and exit — and the stop func closes AND joins them, so
// after Composer.Close no post can ever arrive. A host that was never
// started (no dispatcher) still shows toasts; they simply do not expire
// on their own.
type ToastHost struct {
	gooey.Base
	// Duration is the default lifetime Show uses. Zero means
	// DefaultToastDuration; negative means sticky — toasts stay until
	// Dismiss.
	Duration time.Duration
	// Style is applied to toasts shown through this host. The zero value
	// paints reverse-video, which reads as a toast at any color depth.
	Style render.Style

	toasts    []*Toast
	structure func()
	post      func(func())
	done      chan struct{}
	wg        sync.WaitGroup
}

func (h *ToastHost) ChildComponents() []gooey.Component {
	kids := make([]gooey.Component, len(h.toasts))
	for i, t := range h.toasts {
		kids[i] = t
	}
	return kids
}

// SetStructureHook receives the composition's structural-change hook —
// showing and dismissing are child-set changes, the same seam a
// virtualized list uses (see gooey.Dynamic).
func (h *ToastHost) SetStructureHook(fn func()) { h.structure = fn }

// Start arms auto-dismissal: post is the only path back to the UI loop,
// and the returned stop closes the gate and joins every timer goroutine
// still in flight — once stop returns, no further dismissals arrive.
func (h *ToastHost) Start(post func(func())) func() {
	h.post = post
	done := make(chan struct{})
	h.done = done
	return func() {
		h.post = nil
		close(done)
		h.wg.Wait()
	}
}

// Show puts msg up for the host's default duration and returns the
// toast, which Dismiss takes down early.
func (h *ToastHost) Show(msg string) *Toast {
	d := h.Duration
	if d == 0 {
		d = DefaultToastDuration
	}
	return h.ShowFor(msg, d)
}

// ShowFor puts msg up for d. A non-positive d makes the toast sticky.
// UI goroutine only, like everything that reaches the tree.
func (h *ToastHost) ShowFor(msg string, d time.Duration) *Toast {
	t := &Toast{Text: msg, Style: h.Style}
	h.toasts = append(h.toasts, t)
	if h.structure != nil {
		h.structure()
	}
	if h.post != nil && d > 0 {
		post, done := h.post, h.done
		h.wg.Add(1)
		go func() {
			defer h.wg.Done()
			timer := time.NewTimer(d)
			defer timer.Stop()
			select {
			case <-done:
			case <-timer.C:
				// The join in stop makes this safe: a timer that already
				// fired posts before stop returns, and one that has not
				// fired is cancelled by done. Either way, stop ⇒ no
				// further posts, ever.
				select {
				case <-done:
				default:
					post(func() { h.Dismiss(t) })
				}
			}
		}()
	}
	return t
}

// Dismiss takes a toast down. It is idempotent — the auto-dismiss timer
// and a manual dismissal may race onto the UI loop, and the second one
// must find nothing to do.
func (h *ToastHost) Dismiss(t *Toast) {
	for i, x := range h.toasts {
		if x == t {
			h.toasts = append(h.toasts[:i], h.toasts[i+1:]...)
			if h.structure != nil {
				h.structure()
			}
			return
		}
	}
}

// Measure fills its slot — the host is a positioning surface over the
// whole page, like Canvas.
func (h *ToastHost) Measure(avail gooey.Size) gooey.Size {
	for _, t := range h.toasts {
		gooey.MeasureChild(t, avail)
	}
	return avail
}

// Arrange stacks the toasts down the top-right corner, oldest first.
func (h *ToastHost) Arrange(b gooey.Rect) {
	h.Base.Arrange(b)
	y := b.Y
	for _, t := range h.toasts {
		w := min(t.width(), b.W)
		gooey.ArrangeChild(t, gooey.Rect{X: b.X + b.W - w, Y: y, W: w, H: 1})
		y++
	}
}

// Render paints nothing: the host is a chrome-less container, and the
// toasts own their cells.
func (h *ToastHost) Render(*gooey.Frame) {}

// HitTestTransparent: the host spans the whole page invisibly (the same
// hosting shape as AdornmentLayer), so the pointer must pass through it
// — a page with a toast layer would otherwise never receive a click.
// The toasts themselves stay hittable; they own visible cells.
func (h *ToastHost) HitTestTransparent() bool { return true }

// Toast is one transient message — an ordinary leaf, so its paint node
// pre-clears and covers its rectangle, which is exactly what makes it
// an overlay under the z-ordered pass. Its content is fixed at Show
// time: a toast never updates, it is replaced.
type Toast struct {
	gooey.Base
	Text  string
	Style render.Style
}

func (t *Toast) width() int { return len([]rune(t.Text)) + 2 }

func (t *Toast) Measure(avail gooey.Size) gooey.Size {
	return gooey.Size{W: min(t.width(), avail.W), H: min(1, avail.H)}
}

func (t *Toast) Render(f *gooey.Frame) {
	b := t.Bounds()
	if b.W <= 0 || b.H <= 0 {
		return
	}
	st := t.Style
	if st == (render.Style{}) {
		st = render.Style{Reverse: true, Bold: true}
	}
	f.Cells.SetString(b.X, b.Y, clipRunes(" "+t.Text+" ", b.W), st)
	// Pad the remainder so the whole rectangle carries the toast style.
	for x := b.X + len([]rune(" "+t.Text+" ")); x < b.X+b.W; x++ {
		f.Cells.Set(x, b.Y, ' ', st)
	}
}
