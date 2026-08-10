package gooey

import (
	"io"

	"github.com/WonderForgeLabs/gooey/graphics"
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
// stayed.
//
// Both planes are damage-tracked. Cells go out through a render.Flusher,
// which sends only the spans where this frame's buffer differs from the
// one the terminal is showing; pixel placements are stored per paint node
// and diffed the same way (placements.go).
//
// Z-order is document order: c.nodes is the tree in depth-first pre-order,
// so children paint after (above) their parents and later siblings after
// earlier ones. The paint loop keeps that order honest under partial
// repaints: when a node paints, every LATER node whose bounds intersect
// the painted rect is forced to repaint in the same frame — it was (or may
// have been) painted over, and it is above, so it must go down again on
// top. Two exemptions keep the damage counts tight: a chrome-only
// container never forces its own descendants (its chrome never covers
// their cells — that contract is why containers may skip pre-clearing),
// and a Decorator is never forced from below (it owns no cells to
// restore). This is what makes container backgrounds, hidden containers,
// and overlapping Canvas children all repaint correctly.
type Composer struct {
	root       Component
	frame      *Frame
	cols, rows int
	nodes      []*paintNode
	nodeOf     map[Component]*paintNode
	focus      *FocusManager
	invalid    func()
	painted    int
	frameSeq   int          // stamps which frame a node last painted in
	over       []*paintNode // nodes painted this frame, in z-order (reused)

	// The wire. flusher owns the previous cell buffer; the placement
	// fields own what the terminal is showing on the pixel plane.
	flusher        *render.Flusher
	gonePlacements []shownPlacement // owed removals, from nodes that left the tree
	nextPlaceID    int
	lastBytes      int
	gfxForced      bool

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
	w       Component
	node    *prop.Property[int]
	rev     *prop.Property[int] // bumped when bounds change → forces repaint
	bounds  Rect
	vis     Visibility
	parent  *paintNode // ancestor chain: background lookup and z-order exemptions
	stamp   int        // frameSeq of the last frame this node painted in
	covered bool       // last paint overwrote the node's whole rect (pre-clear or fill)

	// The pixel plane, per node: what this component recorded the last
	// time it painted, and what the terminal is currently showing for it.
	places []graphics.Placement
	shown  []shownPlacement
}

// Bounded is implemented by components that expose their arranged bounds
// (embedding element provides it); the Composer uses it for damage
// clearing and bounds-change detection.
type Bounded interface{ Bounds() Rect }

func NewComposer(root Component, cols, rows int) *Composer {
	c := &Composer{root: root, cols: cols, rows: rows,
		frame:   &Frame{Cells: render.NewBuffer(cols, rows)},
		flusher: render.NewFlusher(),
		stops:   map[Startable]func(){}}
	c.walkNodes()
	c.focus = NewFocusManager(root)
	return c
}

// SetCaps hands the composition the terminal's capabilities, which land
// on the Frame for components to read at Render, set the color depth
// Flush encodes at, and select the graphics protocol pixel content is
// emitted with. It is a setter rather than a constructor parameter
// because capabilities arrive from a probe that not every host runs —
// a composition without them keeps the truecolor, no-graphics defaults.
//
// Call it before the first Frame: components that adapt to capabilities
// read them while painting, so changing caps after a frame would leave
// already-clean paint nodes showing the old tier.
func (c *Composer) SetCaps(caps term.Caps) {
	c.frame.Caps = caps
	c.frame.CellW, c.frame.CellH = caps.CellW, caps.CellH
	if !c.gfxForced {
		c.frame.Graphics = EncoderFor(caps)
	}
}

// SetGraphics pins the pixel protocol, overriding what SetCaps would pick
// from capabilities — a nil encoder forces the halfblock fallback, where
// pixel content degrades into cells. For a host that knows better than
// the probe, and for the demos that show the protocols side by side.
func (c *Composer) SetGraphics(enc graphics.Encoder) {
	c.frame.Graphics = enc
	c.gfxForced = true
}

// Caps reports the capabilities this composition was given.
func (c *Composer) Caps() term.Caps { return c.frame.Caps }

// Graphics is the pixel protocol this composition emits with, or nil when
// pixel content degrades into cells.
func (c *Composer) Graphics() graphics.Encoder { return c.frame.Graphics }

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
	c.build(c.root, prev, nil)

	for w, n := range prev {
		if _, kept := c.nodeOf[w]; !kept {
			fillRect(c.frame.Cells, n.bounds, c.clearStyle(n))
			// Its cells will be overwritten by the clear above, but its
			// pixel placements are on a plane no clear reaches: the flush
			// has to be told to take them off the screen.
			c.gonePlacements = append(c.gonePlacements, n.shown...)
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

func (c *Composer) build(w Component, prev map[Component]*paintNode, parent *paintNode) {
	if n, ok := prev[w]; ok {
		n.parent = parent // a re-sync may have moved the subtree
		c.nodes = append(c.nodes, n)
		c.nodeOf[w] = n
		c.collect(w, prev, n)
		return
	}
	n := &paintNode{w: w, rev: prop.NewSource(0), parent: parent}
	n.node = prop.NewComputed(func() int {
		n.rev.Get()
		n.covered = false
		if b, ok := w.(Bounded); ok {
			r := b.Bounds()
			if _, isContainer := w.(Container); !isContainer {
				// Leaves pre-clear to the nearest ancestor's background,
				// not to the terminal default — a Text inside a colored
				// panel must not punch a default-colored hole when it
				// repaints alone. The read happens INSIDE this paint
				// node, so the ancestor's Background property becomes a
				// dependency: recoloring a panel repaints every leaf
				// that clears against it, automatically.
				fillRect(c.frame.Cells, r, c.clearStyle(n))
				n.covered = true
			} else if !paintable(w) {
				// A hidden container's chrome must leave the screen the
				// same way a hidden leaf's content does. Clearing its
				// bounds wipes its (still visible) children too — the
				// z-ordered pass in Frame repaints them above it.
				fillRect(c.frame.Cells, r, c.clearStyle(n))
				n.covered = true
			} else if bp := backgroundProp(w); bp != nil {
				// A container with a declared background fills its whole
				// bounds — including the gap cells no child owns — and
				// the z-ordered pass repaints its subtree on top. An
				// unset color still fills, with the ancestor's
				// background, so clearing a background at runtime erases
				// the old fill rather than stranding it.
				if col := bp.Get(); col.Set {
					fillRect(c.frame.Cells, r, render.Style{Bg: col})
				} else {
					fillRect(c.frame.Cells, r, c.clearStyle(n))
				}
				n.covered = true
			}
			// A chrome-only container pre-clears nothing: its bounds
			// enclose its children's cells, and wiping those would blank
			// content whose own (clean) nodes won't repaint. It
			// overpaints its own chrome in place instead.
		}
		// A repaint re-records this node's pixel placements from nothing,
		// which is what makes "painted no images this time" mean the
		// images are gone. The sink is saved and restored rather than
		// cleared: a Render that evaluates another node would otherwise
		// hand its placements to the wrong owner.
		outer := c.frame.sink
		n.places = n.places[:0]
		c.frame.sink = func(p graphics.Placement) { n.places = append(n.places, p) }
		if paintable(w) {
			w.Render(c.frame)
		}
		c.frame.sink = outer
		c.painted++
		n.stamp = c.frameSeq
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
	c.collect(w, prev, n)
}

// collect gathers the lifetime-bearing parts of one component and
// recurses. Non-visual attachments (KeyBinding, Timer) never get a paint
// node, but a Timer owns a goroutine, so the walk has to notice it. This
// is the same walk the FocusManager makes for bindings — collected here
// so the Composer, which owns the composition's lifetime, also owns the
// lifetime of anything running inside it.
func (c *Composer) collect(w Component, prev map[Component]*paintNode, n *paintNode) {
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
			c.build(ch, prev, n)
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
				// Vacated region, cleared to the ancestor background so a
				// component shrinking inside a colored panel does not
				// leave a default-colored scar. Outside any evaluation:
				// the property reads record nothing.
				fillRect(c.frame.Cells, n.bounds, c.clearStyle(n))
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
		// A hidden LEAF erases itself through its pre-clear; a hidden
		// CONTAINER clears its whole bounds (see build) and the z-ordered
		// pass below repaints its still-visible children on top.
		if v := visibilityOf(n.w); v != n.vis {
			n.vis = v
			n.rev.Set(n.rev.Get() + 1)
		}
	}
	// Paint in z-order (depth-first pre-order), forcing the repaint of
	// anything that sits ABOVE a rect somebody below just painted. The
	// forcing happens here in the loop — a Set between evaluations, never
	// inside one — so the evaluation-only-reads discipline holds. One
	// forward pass is enough: paint can only damage nodes later in
	// z-order, and by the time the loop reaches them every painter below
	// is already in c.over.
	c.frameSeq++
	c.over = c.over[:0]
	for _, n := range c.nodes {
		if n.stamp != c.frameSeq && n.bounds.W > 0 && n.bounds.H > 0 {
			if _, isDecorator := n.w.(Decorator); !isDecorator {
				for _, p := range c.over {
					if !intersects(p.bounds, n.bounds) {
						continue
					}
					// A chrome-only container's paint never touches its
					// descendants' cells — that contract is why it may
					// skip pre-clearing — so it does not force them.
					// Covered painters (leaves, filled or hidden
					// containers) force everything above them.
					if !p.covered && isAncestorOf(p, n) {
						continue
					}
					n.rev.Set(n.rev.Get() + 1)
					break
				}
			}
		}
		n.node.Get() // only dirty (or just-forced) nodes execute
		if n.stamp == c.frameSeq {
			c.over = append(c.over, n)
		}
	}
	// Republish the pixel plane in paint order, so the Frame handed back
	// describes the whole composition and not just what repainted. The
	// incremental emission works off the per-node lists; this is for
	// anyone holding the Frame — Frame.Flush, a test, a screenshot.
	c.frame.placements = c.frame.placements[:0]
	for _, n := range c.nodes {
		c.frame.placements = append(c.frame.placements, n.places...)
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

// Flush sends this frame to w: the cell spans that changed since the last
// flush, then the pixel placements that changed, all inside one
// synchronized-output bracket so the terminal presents cells and the
// images over them as a single update.
//
// A frame where nothing changed on either plane writes NOTHING — not even
// the bracket. That is the point of the whole path: an idle app costs
// zero bytes, and a keystroke costs a row.
//
// Color is encoded at the depth from SetCaps.
func (c *Composer) Flush(w io.Writer) error {
	ops, kept := c.placementOps() // before the cell flush: removals damage cells
	cells := c.flusher.Encode(nil, c.frame.Cells, c.frame.Caps.Color)
	pix, err := c.encodePlacements(c.refresh(ops, kept))

	c.lastBytes = 0
	if len(cells) == 0 && len(pix) == 0 {
		return err
	}
	out := make([]byte, 0, len(render.BeginSync)+len(cells)+len(pix)+len(render.EndSync))
	out = append(out, render.BeginSync...)
	out = append(out, cells...)
	out = append(out, pix...)
	out = append(out, render.EndSync...)
	c.lastBytes = len(out)
	if _, werr := w.Write(out); werr != nil {
		return werr
	}
	return err
}

// FlushBytes is how many bytes the last Flush wrote. It is the damage
// guarantee made countable on the wire, the way PaintedLastFrame makes it
// countable in components, and the number the byte-budget tests assert.
func (c *Composer) FlushBytes() int { return c.lastBytes }

// Invalidate forces the next Flush to repaint the whole screen and
// re-emit every placement.
//
// Call it whenever something other than this Composer may have written to
// the terminal: the alternate screen comes back blank after a child
// process has had it, so the retained buffer is right and the screen is
// wrong, and only the flush needs redoing — no component repaints.
func (c *Composer) Invalidate() { c.flusher.Invalidate() }

// Snapshot writes the ENTIRE retained buffer to w — every cell, in one
// synchronized update — without touching the incremental flush state.
//
// Flush sends differences, which is right for a terminal and useless for
// anyone asking "what does the screen look like": an automation client
// taking a styled screenshot wants the picture, not the delta since the
// last one. Pixel placements are not included; they are not cells.
func (c *Composer) Snapshot(w io.Writer) error {
	return render.Flush(w, c.frame.Cells, c.frame.Caps.Color)
}

func fillRect(b *render.Buffer, r Rect, s render.Style) {
	for y := r.Y; y < r.Y+r.H; y++ {
		for x := r.X; x < r.X+r.W; x++ {
			b.Set(x, y, ' ', s)
		}
	}
}

// clearStyle is what a node's rect clears TO: blank cells styled with the
// nearest visible ancestor's set background, or the terminal default when
// no ancestor declares one. Call site decides what the reads mean, as
// always: from inside a paint node's evaluation the ancestor Background
// properties become dependencies of that node; from the sweeps (vacated
// bounds, departed nodes) they are plain reads.
func (c *Composer) clearStyle(n *paintNode) render.Style {
	for p := n.parent; p != nil; p = p.parent {
		if !paintable(p.w) {
			continue // a hidden panel's background is not on screen
		}
		bp := backgroundProp(p.w)
		if bp == nil {
			continue
		}
		if col := bp.Get(); col.Set {
			return render.Style{Bg: col}
		}
	}
	return render.Style{}
}

func intersects(a, b Rect) bool {
	return a.X < b.X+b.W && b.X < a.X+a.W && a.Y < b.Y+b.H && b.Y < a.Y+a.H
}

func isAncestorOf(p, n *paintNode) bool {
	for a := n.parent; a != nil; a = a.parent {
		if a == p {
			return true
		}
	}
	return false
}

func visibilityOf(w Component) Visibility {
	if l := LayoutOf(w); l != nil {
		return l.Visibility
	}
	return Visible
}
