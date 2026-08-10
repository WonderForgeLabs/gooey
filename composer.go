package gooey

import (
	"io"

	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
	"github.com/WonderForgeLabs/gooey/term"
)

// Composer is the retained, damage-tracked render path. Each widget's
// paint is its own graph node: evaluating it runs Widget.Render, so the
// properties a widget reads while painting become its dependencies
// automatically — "AffectsRender" metadata is discovered, not declared.
// A property change dirties exactly the widgets that read it; Frame()
// re-paints only those into the persistent buffer.
//
// Layout (Measure/Arrange) runs unconditionally every frame — cheap at
// terminal scale, and it runs outside any evaluation context so layout
// reads record nothing. A widget whose bounds changed is force-dirtied
// via its rev source and its old region cleared.
//
// POC limits: static tree (rebuild the Composer on structural change)
// and cell-plane widgets only (no graphics placements).
type Composer struct {
	root       Widget
	frame      *Frame
	cols, rows int
	nodes      []*paintNode
	focus      *FocusManager
	invalid    func()
	painted    int

	startable []Startable // discovered during the walk, started by Start
	stops     []func()    // one per started element, run by Close
}

type paintNode struct {
	w      Widget
	node   *prop.Property[int]
	rev    *prop.Property[int] // bumped when bounds change → forces repaint
	bounds Rect
	vis    Visibility
}

// Bounded is implemented by widgets that expose their arranged bounds
// (embedding element provides it); the Composer uses it for damage
// clearing and bounds-change detection.
type Bounded interface{ Bounds() Rect }

func NewComposer(root Widget, cols, rows int) *Composer {
	c := &Composer{root: root, cols: cols, rows: rows,
		frame: &Frame{Cells: render.NewBuffer(cols, rows)}}
	c.build(root)
	c.focus = NewFocusManager(root)
	return c
}

// SetCaps hands the composition the terminal's capabilities, which land
// on the Frame for widgets to read at Render and set the color depth
// Flush encodes at. It is a setter rather than a constructor parameter
// because capabilities arrive from a probe that not every host runs —
// a composition without them keeps the truecolor, no-graphics defaults.
//
// Call it before the first Frame: widgets that adapt to capabilities
// read them while painting, so changing caps after a frame would leave
// already-clean paint nodes showing the old tier.
func (c *Composer) SetCaps(caps term.Caps) {
	c.frame.Caps = caps
	c.frame.CellW, c.frame.CellH = caps.CellW, caps.CellH
}

// Caps reports the capabilities this composition was given.
func (c *Composer) Caps() term.Caps { return c.frame.Caps }

// Focus is the input tree built from this composition: focus order,
// ancestor links, and declared key bindings.
func (c *Composer) Focus() *FocusManager { return c.focus }

// Handle routes one input event — key or mouse — through the tree.
func (c *Composer) Handle(ev input.Event) bool {
	if ev.IsMouse() {
		return c.focus.DispatchMouse(ev.Mouse)
	}
	return c.focus.Dispatch(ev.Key)
}

// HandleKey routes a key event through the tree. See FocusManager.Dispatch.
func (c *Composer) HandleKey(ev input.KeyEvent) bool { return c.focus.Dispatch(ev) }

// HandleMouse routes a pointer event. See FocusManager.DispatchMouse.
func (c *Composer) HandleMouse(ev input.MouseEvent) bool { return c.focus.DispatchMouse(ev) }

func (c *Composer) build(w Widget) {
	n := &paintNode{w: w, rev: prop.NewSource(0)}
	n.node = prop.NewComputed(func() int {
		n.rev.Get()
		// Pre-clear only leaves: a container's bounds enclose its
		// children's cells, and wiping those would blank content whose
		// own (clean) nodes won't repaint. Containers overpaint their
		// own chrome in place instead.
		if _, isContainer := w.(Container); !isContainer {
			if b, ok := w.(Bounded); ok {
				clearRect(c.frame.Cells, b.Bounds())
			}
		}
		if paintable(w) {
			w.Render(c.frame)
		}
		c.painted++
		return c.painted
	})
	n.node.OnInvalidate(func() {
		if c.invalid != nil {
			c.invalid()
		}
	})
	c.nodes = append(c.nodes, n)
	// Non-visual attachments (KeyBinding, Timer) never get a paint node,
	// but a Timer owns a goroutine, so the walk has to notice it. This is
	// the same walk the FocusManager makes for bindings — collected here
	// so the Composer, which owns the composition's lifetime, also owns
	// the lifetime of anything running inside it.
	if a, ok := w.(Attacher); ok {
		for _, at := range a.Attachments() {
			if s, ok := at.(Startable); ok {
				c.startable = append(c.startable, s)
			}
		}
	}
	if s, ok := w.(Startable); ok {
		c.startable = append(c.startable, s)
	}
	if ct, ok := w.(Container); ok {
		for _, ch := range ct.ChildWidgets() {
			c.build(ch)
		}
	}
}

// Start brings the composition's background elements to life, delivering
// their work onto the UI goroutine through d. Timers do not run until
// this is called, which is what makes "started" a property of the
// composition rather than of the widget: a tree that was built but never
// composed never ticks.
//
// Calling Start twice stops the previous run first, so it is safe in an
// attach/swap helper.
func (c *Composer) Start(d *Dispatcher) {
	c.stopAll()
	if d == nil {
		return
	}
	for _, s := range c.startable {
		if stop := s.Start(d.Post); stop != nil {
			c.stops = append(c.stops, stop)
		}
	}
}

// Close stops everything Start started. Hot reload replaces a whole
// composition, so the OLD Composer must be closed before the new one
// takes over — otherwise the dead tree's timers keep ticking against a
// viewmodel nobody is showing. Close is idempotent.
func (c *Composer) Close() { c.stopAll() }

func (c *Composer) stopAll() {
	for _, stop := range c.stops {
		stop()
	}
	c.stops = nil
}

// OnInvalidate registers the scheduler hook: fired when any widget's
// paint node goes dirty.
func (c *Composer) OnInvalidate(fn func()) { c.invalid = fn }

// Frame lays out, repaints dirty widgets only, and reports how many
// widgets painted.
func (c *Composer) Frame() (*Frame, int) {
	c.painted = 0
	// Unconditional layout, outside any eval context: reads here are
	// not recorded as dependencies.
	c.root.Measure(Size{c.cols, c.rows})
	c.root.Arrange(Rect{0, 0, c.cols, c.rows})
	for _, n := range c.nodes {
		if b, ok := n.w.(Bounded); ok {
			if nb := b.Bounds(); nb != n.bounds {
				clearRect(c.frame.Cells, n.bounds) // vacated region
				n.bounds = nb
				n.rev.Set(n.rev.Get() + 1) // bounds moved → must repaint
			}
		}
		// Visibility is a plain field, not a property, so flipping it
		// dirties nothing on its own. Collapsed is covered by the bounds
		// check above (it arranges to zero size), but Hidden↔Visible
		// keeps its bounds and would otherwise leave the old pixels on
		// screen forever. Catching the delta here is the same trick the
		// bounds sweep uses: notice the change, force the repaint.
		//
		// This makes LEAVES correct — a leaf pre-clears its rect, so
		// turning Hidden erases it. A CONTAINER's own chrome persists
		// until something else repaints it, because containers must not
		// clear their bounds (that would wipe children whose nodes are
		// clean). Same missing z-order notion as everything else in
		// docs/specs/2026-08-10-container-backgrounds.md.
		if v := visibilityOf(n.w); v != n.vis {
			n.vis = v
			n.rev.Set(n.rev.Get() + 1)
		}
	}
	for _, n := range c.nodes {
		n.node.Get() // only dirty nodes execute
	}
	return c.frame, c.painted
}

// Flush writes the current buffer to w, encoding color at the depth
// from SetCaps.
func (c *Composer) Flush(w io.Writer) error {
	return render.Flush(w, c.frame.Cells, c.frame.Caps.Color)
}

func clearRect(b *render.Buffer, r Rect) {
	for y := r.Y; y < r.Y+r.H; y++ {
		for x := r.X; x < r.X+r.W; x++ {
			b.Set(x, y, ' ', render.Style{})
		}
	}
}

func visibilityOf(w Widget) Visibility {
	if l := layoutOf(w); l != nil {
		return l.Visibility
	}
	return Visible
}
