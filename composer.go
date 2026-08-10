package gooey

import (
	"io"

	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
	"github.com/WonderForgeLabs/gooey/term"
)

// Composer is the retained, damage-tracked render path. Each component's
// paint is its own graph node: evaluating it runs Component.Render, so the
// properties a component reads while painting become its dependencies
// automatically — "AffectsRender" metadata is discovered, not declared.
// A property change dirties exactly the components that read it; Frame()
// re-paints only those into the persistent buffer.
//
// Layout (Measure/Arrange) runs unconditionally every frame — cheap at
// terminal scale, and it runs outside any evaluation context so layout
// reads record nothing. A component whose bounds changed is force-dirtied
// via its rev source and its old region cleared.
//
// The tree is mostly static: a Composer is rebuilt when the whole tree is
// replaced (hot reload). A container that changes its own child set
// implements Dynamic (dynamic.go) and the composition re-syncs its paint
// nodes and input tree in place, keeping the nodes of everything that
// stayed. POC limit: cell-plane components only (no graphics placements).
type Composer struct {
	root       Component
	frame      *Frame
	cols, rows int
	nodes      []*paintNode
	nodeOf     map[Component]*paintNode
	focus      *FocusManager
	invalid    func()
	painted    int

	// Structural change (see dynamic.go): a Dynamic container raises this
	// flag through the hook it was given, and Frame re-syncs after layout
	// and before painting — layout being exactly when a list decides which
	// rows exist.
	structDirty bool

	startable []Startable          // discovered during the walk, started by Start
	disp      *Dispatcher          // remembered by Start, so a re-sync can start new arrivals
	stops     map[Startable]func() // one per started element, run by Close
}

type paintNode struct {
	w      Component
	node   *prop.Property[int]
	rev    *prop.Property[int] // bumped when bounds change → forces repaint
	bounds Rect
	vis    Visibility
}

// Bounded is implemented by components that expose their arranged bounds
// (embedding element provides it); the Composer uses it for damage
// clearing and bounds-change detection.
type Bounded interface{ Bounds() Rect }

func NewComposer(root Component, cols, rows int) *Composer {
	c := &Composer{root: root, cols: cols, rows: rows,
		frame: &Frame{Cells: render.NewBuffer(cols, rows)},
		stops: map[Startable]func(){}}
	c.walkNodes()
	c.focus = NewFocusManager(root)
	return c
}

// SetCaps hands the composition the terminal's capabilities, which land
// on the Frame for components to read at Render and set the color depth
// Flush encodes at. It is a setter rather than a constructor parameter
// because capabilities arrive from a probe that not every host runs —
// a composition without them keeps the truecolor, no-graphics defaults.
//
// Call it before the first Frame: components that adapt to capabilities
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

// Root is the component this composition was built over — the entry point
// for anything that needs to walk the live tree (serialization,
// inspection, an automation surface). Call it on the UI goroutine: the
// components it leads to hold properties, and properties are confined
// there.
func (c *Composer) Root() Component { return c.root }

// Cells is the retained cell plane as of the LAST Frame — the buffer
// Flush writes, not a fresh composition. Reading it is how a caller
// screenshots the app without disturbing damage state: composing here to
// "refresh" it would mark every dirty node clean and steal the repaint
// from the app's own next frame, which is the damage count the framework
// guarantees.
//
// UI goroutine only, and read-only in practice: the Composer paints into
// this buffer every frame.
func (c *Composer) Cells() *render.Buffer { return c.frame.Cells }

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

// walkNodes rebuilds the paint-node list from the current tree, REUSING
// the node of every component that was already there. Reuse is the whole
// point: a node carries the component's recorded dependencies and its
// clean/dirty flag, so a component that did not change does not repaint
// merely because something else in the tree appeared or vanished.
//
// Components that are gone have their last known rectangle cleared —
// nothing else will ever paint over those cells — and anything Startable
// among them is stopped. New arrivals are started if this composition was
// already started, so a row realized on frame 40 gets the same treatment
// as one that existed at frame 0.
func (c *Composer) walkNodes() {
	prev := c.nodeOf
	c.nodeOf = make(map[Component]*paintNode, len(prev))
	c.nodes = c.nodes[:0]
	c.startable = c.startable[:0]
	c.build(c.root, prev)

	for w, n := range prev {
		if _, kept := c.nodeOf[w]; !kept {
			clearRect(c.frame.Cells, n.bounds)
		}
	}
	if c.disp == nil {
		return
	}
	live := make(map[Startable]bool, len(c.startable))
	for _, s := range c.startable {
		live[s] = true
		if _, running := c.stops[s]; running {
			continue
		}
		if stop := s.Start(c.disp.Post); stop != nil {
			c.stops[s] = stop
		} else {
			c.stops[s] = func() {}
		}
	}
	for s, stop := range c.stops {
		if !live[s] {
			stop()
			delete(c.stops, s)
		}
	}
}

func (c *Composer) build(w Component, prev map[Component]*paintNode) {
	if n, ok := prev[w]; ok {
		c.nodes = append(c.nodes, n)
		c.nodeOf[w] = n
		c.collect(w, prev)
		return
	}
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
	c.nodeOf[w] = n
	if d, ok := w.(Dynamic); ok {
		d.SetStructureHook(c.structureChanged)
	}
	c.collect(w, prev)
}

// collect gathers the lifetime-bearing parts of one component and
// recurses. Non-visual attachments (KeyBinding, Timer) never get a paint
// node, but a Timer owns a goroutine, so the walk has to notice it. This
// is the same walk the FocusManager makes for bindings — collected here
// so the Composer, which owns the composition's lifetime, also owns the
// lifetime of anything running inside it.
func (c *Composer) collect(w Component, prev map[Component]*paintNode) {
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
		for _, ch := range ct.ChildComponents() {
			c.build(ch, prev)
		}
	}
}

// structureChanged is the hook handed to every Dynamic container. It only
// raises a flag: the sync itself has to happen at a defined point in the
// frame (after layout, before painting), and the caller is typically in
// the middle of arranging its children when it notices.
func (c *Composer) structureChanged() {
	if c.structDirty {
		return
	}
	c.structDirty = true
	if c.invalid != nil {
		c.invalid()
	}
}

// Start brings the composition's background elements to life, delivering
// their work onto the UI goroutine through d. Timers do not run until
// this is called, which is what makes "started" a property of the
// composition rather than of the component: a tree that was built but never
// composed never ticks.
//
// Calling Start twice stops the previous run first, so it is safe in an
// attach/swap helper.
func (c *Composer) Start(d *Dispatcher) {
	c.stopAll()
	c.disp = d
	if d == nil {
		return
	}
	for _, s := range c.startable {
		if stop := s.Start(d.Post); stop != nil {
			c.stops[s] = stop
		} else {
			c.stops[s] = func() {}
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
	c.stops = map[Startable]func(){}
}

// OnInvalidate registers the scheduler hook: fired when any component's
// paint node goes dirty.
func (c *Composer) OnInvalidate(fn func()) { c.invalid = fn }

// Frame lays out, repaints dirty components only, and reports how many
// components painted.
func (c *Composer) Frame() (*Frame, int) {
	c.painted = 0
	// Unconditional layout, outside any eval context: reads here are
	// not recorded as dependencies.
	c.root.Measure(Size{c.cols, c.rows})
	c.root.Arrange(Rect{0, 0, c.cols, c.rows})
	// A Dynamic container decides which children exist while it is being
	// arranged, so the sync lands here: after layout, before the bounds
	// sweep (which then gives new nodes their first remembered bounds) and
	// before painting (so a row realized this frame is painted this
	// frame, not next).
	if c.structDirty {
		c.structDirty = false
		c.walkNodes()
		c.focus.Resync()
	}
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

// Size reports the cell dimensions this composition lays out into.
func (c *Composer) Size() (cols, rows int) { return c.cols, c.rows }

// Resize re-targets the composition at a new terminal size: a new buffer
// of that size and a forced full repaint.
//
// Everything else follows from the machinery already here. Layout runs
// unconditionally every frame, so the tree re-measures against the new
// bounds by itself; what layout cannot do is repaint clean nodes, and
// after a resize EVERY node is stale because the buffer it painted into
// is gone. Dirtying all of them through their rev sources is the same
// force-repaint the bounds sweep uses, and clearing the remembered
// bounds keeps that sweep from clearing rectangles in the new buffer
// that describe the old one.
func (c *Composer) Resize(cols, rows int) {
	if cols <= 0 || rows <= 0 || (cols == c.cols && rows == c.rows) {
		return
	}
	c.cols, c.rows = cols, rows
	c.frame.Cells = render.NewBuffer(cols, rows)
	c.frame.Caps.Cols, c.frame.Caps.Rows = cols, rows
	for _, n := range c.nodes {
		n.bounds = Rect{}
		n.rev.Set(n.rev.Get() + 1)
	}
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

func visibilityOf(w Component) Visibility {
	if l := LayoutOf(w); l != nil {
		return l.Visibility
	}
	return Visible
}
