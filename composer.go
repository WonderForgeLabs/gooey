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
//
// The forward pass has a reverse half, restoreUnder: when a rect LEAVES
// the screen (a component turned non-visible, departed in a re-sync, or
// moved), everything that sat beneath it is force-dirtied from the
// sweeps, before the paint loop — a dismissed overlay's vacated cells
// repaint from what was underneath in the same frame.
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

	// fault is the FIRST walk that refused this tree, and it is sticky on
	// purpose: the frame that hit it is over by the time anyone can ask,
	// and a cycle repeats every frame anyway.
	//
	// First rather than latest, for two reasons. It is deterministic — a
	// cyclic tree trips Compose, then Focus, then Measure, then Arrange
	// in that order every frame, so "latest" would report whichever walk
	// happened to run last and change if the frame's internals were
	// reordered. And the first walk to meet a cycle is the most
	// actionable: Compose runs before layout exists, so it is the closest
	// report to the structural mistake that caused it.
	fault *LayoutFault
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

	// visObs exists only for a component whose Layout binds Visibility:
	// a computed whose evaluation reads the bound source (recording the
	// dependency) and syncs the plain Layout field. It is not a paint
	// node — it never renders and never counts as damage. Its whole job
	// is to make a Set on the source schedule a frame; the per-frame
	// visibility sweep below then does what it always did.
	visObs *prop.Property[int]

	// frozenObs is the same two-part shape applied to Frozen, and it
	// exists only for a component that implements it: a computed whose
	// evaluation CALLS Frozen(), so whatever properties the host reads to
	// decide become this observer's dependencies by the ordinary call-site
	// rule. Like visObs it is not a paint node — it never renders and never
	// counts as damage — and its OnInvalidate only schedules a frame.
	// frozen is the answer as of the last sweep; the sweep comparing the
	// two is what raises structDirty.
	frozenObs *prop.Property[bool]
	frozen    bool

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
// as one that existed at frame 0. Departed startables stop BEFORE new
// ones start, so a replaced element never overlaps its replacement.
//
// A caveat worth stating plainly: this runs inside Frame(), so a stop
// that waits (components.Companion waits for its child, bounded by
// StopTimeout) blocks the UI goroutine mid-frame — no paint, no input,
// no signals until it returns. That is the price of "stopped means
// stopped" and it is paid on a structural re-sync, not only at teardown.
// A Startable whose stop cannot be made cheap should be removed by
// swapping the whole composition, where the wait happens between trees.
func (c *Composer) walkNodes() {
	prev := c.nodeOf
	c.nodeOf = make(map[Component]*paintNode, len(prev))
	c.nodes = c.nodes[:0]
	c.startable = c.startable[:0]
	c.build(c.root, prev, nil)
	// Taken here as well as in Frame, so a cycle in the tree a Composer is
	// CONSTRUCTED with is readable before the first frame is asked for.
	if f := TakeLayoutFault(); f != nil && c.fault == nil {
		c.fault = f
	}

	for w, n := range prev {
		if _, kept := c.nodeOf[w]; !kept {
			fillRect(c.frame.Cells, n.bounds, c.clearStyle(n))
			// A departed node may have been an overlay: whatever its rect
			// was covering has clean paint nodes that will never repaint
			// on their own, so they are force-dirtied here and the paint
			// loop lays them down again in z-order (a dismissed toast's
			// vacated cells repaint from what was beneath).
			c.restoreUnder(n.bounds)
			// Its cells will be overwritten by the clear above, but its
			// pixel placements are on a plane no clear reaches: the flush
			// has to be told to take them off the screen.
			c.gonePlacements = append(c.gonePlacements, n.shown...)
		}
	}
	if c.disp == nil {
		return
	}
	// STOP FIRST, THEN START. The order is load-bearing for anything whose
	// lifetime touches a resource outside the process: a <Companion>
	// replaced by another declaring the same service would otherwise have
	// two children alive at once for the whole outgoing StopTimeout — the
	// new one failing to bind a port the old one still holds, and
	// truncating the log the old one is still writing into.
	//
	// Nothing depends on the reverse order, and no Startable can be caught
	// out by it: live is computed from the COMPLETE new list before any
	// stop runs, and membership is pointer identity, so a component that
	// is present both before and after is neither stopped nor restarted —
	// there is no "departs and re-appears in the same pass" to lose.
	live := make(map[Startable]bool, len(c.startable))
	for _, s := range c.startable {
		live[s] = true
	}
	for s, stop := range c.stops {
		if !live[s] {
			stop()
			delete(c.stops, s)
		}
	}
	for _, s := range c.startable {
		if _, running := c.stops[s]; running {
			continue
		}
		if stop := s.Start(c.disp.Post); stop != nil {
			c.stops[s] = stop
		} else {
			c.stops[s] = func() {}
		}
	}
}

func (c *Composer) build(w Component, prev map[Component]*paintNode, parent *paintNode) {
	// A cycle reaches the STRUCTURAL walk before it ever reaches layout,
	// and it is worse here than a stack overflow: build/collect/build
	// allocates a paintNode and three property nodes per level, so a
	// cyclic tree does not die, it grows — the process wedges eating
	// memory, with no fatal error and no trace to read. MeasureChild's
	// depth cap cannot help, because NewComposer walks the tree before
	// Measure is called even once.
	//
	// The test here is identity, not depth, because identity is EXACT and
	// this map makes it free. nodeOf gives each component exactly one
	// paint node — that is the damage model's central assumption, not an
	// implementation detail — so a component reached twice in one walk is
	// either a cycle or the same instance placed in two parents, and
	// neither can be composed. Catching it on the second visit also means
	// a cycle costs two nodes rather than the 512 a depth bound would
	// allocate before noticing.
	if _, seen := c.nodeOf[w]; seen {
		noteIdentityFault("Compose", w)
		return
	}
	if n, ok := prev[w]; ok {
		n.parent = parent // a re-sync may have moved the subtree
		c.nodes = append(c.nodes, n)
		c.nodeOf[w] = n
		c.armVisibility(n)
		c.armFrozen(n)
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
	c.armVisibility(n)
	c.armFrozen(n)
	if d, ok := w.(Dynamic); ok {
		d.SetStructureHook(c.structureChanged)
	}
	c.collect(w, prev, n)
}

// armVisibility gives a component with bound Visibility its observer —
// the composition-side half of Layout.BindVisibility. The observer's
// evaluation is where the source read becomes a SUBSCRIPTION (the
// call-site rule, applied deliberately): a Set on the source dirties the
// observer, whose OnInvalidate schedules a frame, and Frame's sweep —
// which only ever needed to be woken — sees the synced field and runs
// the same erase/restore/relayout a literal flip takes. The observer is
// not a paint node: painted counts and damage rectangles are identical
// to the literal path's.
//
// Called from build (new and reused nodes both, so a Dynamic re-sync
// arms arrivals) and from Frame's re-arm pass, which also catches a
// binding made after the composition was built.
func (c *Composer) armVisibility(n *paintNode) {
	if n.visObs != nil {
		return
	}
	l := LayoutOf(n.w)
	if l == nil || l.visSrc == nil {
		return
	}
	n.visObs = prop.NewComputed(func() int {
		if src := l.visSrc; src != nil {
			l.Visibility = src()
		}
		return 0
	})
	n.visObs.OnInvalidate(func() {
		if c.invalid != nil {
			c.invalid()
		}
	})
	n.visObs.Get() // arm: record the dependency and sync the field
}

// armFrozen is armVisibility's twin, and it is what turns Frozen from a
// value sampled at re-sync into one that re-routes input in the frame it
// changes.
//
// THE OBSERVER TAKES NO NEW INTERFACE. Its evaluation calls Frozen()
// itself, so any property the host reads to decide becomes a dependency of
// this node by the ordinary call-site rule — the same discovery that makes
// a Render's reads its damage declaration. A host whose Frozen() returns a
// constant reads nothing, records nothing, is never invalidated, and costs
// one dead computed; a host whose answer is `p.Get()` or
// `mode.Get() && !sel.Get()` is observed with no declaration at all. The
// alternative considered was a second method returning a
// *prop.Property[bool] for the framework to watch, which would have been
// two statements of one fact that can disagree, and would have forbidden a
// host whose frozen-ness is DERIVED unless it maintained a mirror source —
// and prop.Set does not compare values, so that mirror would re-dirty the
// composition on every no-op write.
//
// The observer only ever SCHEDULES a frame. It deliberately does not raise
// structDirty itself: an invalidation says "something Frozen() read
// changed", not "the answer changed", and a re-sync that walks the tree,
// stops and restarts Startables and rebuilds the focus order is far too
// expensive to run for an answer that came back the same. Frame's sweep
// compares against n.frozen and raises the flag only on a real flip —
// exactly the division of labour between visObs and the visibility sweep.
//
// Arming lives here rather than in the FocusManager, which is the other
// consumer, because the FocusManager owns neither an invalidate hook nor
// per-component storage, and because freezing has TWO consumers: the input
// tree and the Startable set. One observer in the Composer wakes both; one
// in the FocusManager would have left Startables sampled.
//
// Called from build for new and reused nodes alike. There is no late-arm
// pass like Frame's for visObs: whether a component implements Frozen is
// fixed at compile time, so a node that did not arm here never will.
func (c *Composer) armFrozen(n *paintNode) {
	if n.frozenObs != nil {
		return
	}
	if _, ok := n.w.(Frozen); !ok {
		return
	}
	n.frozenObs = prop.NewComputed(func() bool { return isFrozen(n.w) })
	n.frozenObs.OnInvalidate(func() {
		if c.invalid != nil {
			c.invalid()
		}
	})
	n.frozen = n.frozenObs.Get() // arm: record the dependency and seed the sweep
}

// collect gathers the lifetime-bearing parts of one component and
// recurses. Non-visual attachments (KeyBinding, Timer) never get a paint
// node, but a Timer owns a goroutine, so the walk has to notice it. This
// is the same walk the FocusManager makes for bindings — collected here
// so the Composer, which owns the composition's lifetime, also owns the
// lifetime of anything running inside it.
// Nothing inside a Frozen subtree is started. That is the widest part of
// freezing and the one with a safety argument rather than a UX one:
// Companion.Start spawns a child process, so a frozen tree that still
// started its Startables would launch a subprocess the moment a design
// surface was handed one — an effect outside this process, from an
// editing gesture, outliving the editor that caused it. Declining to
// start a subtree is this walk's existing responsibility taking a new
// input, not a new mechanism.
func (c *Composer) collect(w Component, prev map[Component]*paintNode, n *paintNode) {
	// The frozen COMPONENT is not itself frozen — its subtree is — so the
	// test is on ancestors. A design surface's own gestures keep working
	// while nothing it contains does.
	frozen := frozenAncestor(n)
	if a, ok := w.(Attacher); ok {
		for _, at := range a.Attachments() {
			if s, ok := at.(Startable); ok && !frozen {
				c.startable = append(c.startable, s)
			}
		}
	}
	if s, ok := w.(Startable); ok && !frozen {
		c.startable = append(c.startable, s)
	}
	if ct, ok := w.(Container); ok {
		for _, ch := range ct.ChildComponents() {
			c.build(ch, prev, n)
		}
	}
}

// frozenAncestor reports whether any STRICT ancestor of n freezes its
// subtree. The paint-node chain is walked rather than a flag threaded
// through build/collect: Startables are rare and trees are a dozen levels
// deep, and a parameter would have to be kept correct through the reused
// node path as well, where n.parent is reassigned on every re-sync.
func frozenAncestor(n *paintNode) bool {
	if n == nil {
		return false
	}
	for p := n.parent; p != nil; p = p.parent {
		if isFrozen(p.w) {
			return true
		}
	}
	return false
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

// InvalidateStructure tells the composition that the tree's SHAPE changed
// under it from outside — a child replaced or inserted by something other
// than a Dynamic container raising its own hook (the markup patch path is
// the motivating caller). The next Frame re-syncs paint nodes and the
// input tree exactly as it does for a Dynamic container: components still
// present keep their nodes with clean/dirty state intact, departed nodes
// have their cells cleared and their startables stopped, and new arrivals
// get nodes, focus-order entries, and a Start if the composition is
// running. UI goroutine only, like every other structural operation.
func (c *Composer) InvalidateStructure() { c.structureChanged() }

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

// LayoutFault reports the deepest-tree breach seen by any layout pass so
// far, or nil. Non-nil means some subtree was too deep to walk and was
// left unlaid — the frame is still valid, just missing that part.
func (c *Composer) LayoutFault() *LayoutFault { return c.fault }

// Frame lays out, repaints dirty components only, and reports how many
// components painted.
func (c *Composer) Frame() (*Frame, int) {
	c.painted = 0
	// Bound visibility first: re-evaluate each dirty observer (syncing
	// the Layout field and re-recording its subscription) so the layout
	// pass and the sweeps below see the bound value. These Gets happen
	// outside any evaluation — plain re-arms, not reads that subscribe
	// this frame to anything. The nil check doubles as late arming for a
	// binding made after the composition was built.
	//
	// The frozen sweep rides in the same loop, and it has to be BEFORE the
	// structDirty block below rather than beside the bounds/visibility
	// sweeps that come after it: raising the flag is the whole point, and
	// the block that consumes it runs between the two. Re-Getting the
	// observer is not optional bookkeeping either — a dirty computed stays
	// dirty until read, so a node left unread would go deaf to the NEXT
	// flip.
	for _, n := range c.nodes {
		if n.visObs != nil {
			n.visObs.Get()
		} else {
			c.armVisibility(n)
		}
		if n.frozenObs != nil {
			if f := n.frozenObs.Get(); f != n.frozen {
				// A flip is a STRUCTURAL change, because what it changes is
				// what the tree's shape means: walkNodes re-derives the
				// Startable set through frozenAncestor, and Resync re-derives
				// the focus order, the scoped bindings, the mnemonics and the
				// hover watchers. Both already run off this flag for a
				// Dynamic container, and neither needed a new input.
				n.frozen = f
				c.structureChanged()
			}
		}
	}
	// Unconditional layout, outside any eval context: reads here are
	// not recorded as dependencies.
	//
	// There is deliberately no `layoutDepth = 0` reset here. It looks
	// like prudent belt-and-braces and is dead code: MeasureChild and
	// ArrangeChild decrement with a defer, and a deferred call runs
	// during panic unwinding too, so the counter is already balanced on
	// every path out — including the one a reset would supposedly cover.
	// No edit to the reset can fail a test, which is the definition of a
	// line that should not be here.
	c.root.Measure(Size{c.cols, c.rows})
	c.root.Arrange(Rect{0, 0, c.cols, c.rows})
	if f := TakeLayoutFault(); f != nil && c.fault == nil {
		c.fault = f
	}
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
		old := n.bounds
		if b, ok := n.w.(Bounded); ok {
			if nb := b.Bounds(); nb != n.bounds {
				// Vacated region, cleared to the ancestor background so a
				// component shrinking inside a colored panel does not
				// leave a default-colored scar. Outside any evaluation:
				// the property reads record nothing. Whatever sat BENEATH
				// the old rect is force-repainted — an overlay that moved
				// (a menu switching titles) must not leave a scar where
				// the content it covered used to show through.
				fillRect(c.frame.Cells, n.bounds, c.clearStyle(n))
				n.bounds = nb
				n.rev.Set(n.rev.Get() + 1) // bounds moved → must repaint
				c.restoreUnder(old)
			}
		}
		// The Visibility FIELD is plain, so flipping it from code dirties
		// nothing on its own (a bound Visibility reaches this same sweep
		// through its observer: the Set schedules the frame, the observer
		// pass above synced the field). Collapsed is covered by the bounds
		// check above (it arranges to zero size), but Hidden↔Visible
		// keeps its bounds and would otherwise leave the old pixels on
		// screen forever. Catching the delta here is the same trick the
		// bounds sweep uses: notice the change, act outside any evaluation.
		//
		// BECOMING visible dirties the node: it must paint again. LEAVING
		// the screen is handled here in the sweep, not by the node's own
		// paint: its rect is cleared (to the ancestor background) and
		// everything that sat beneath it is force-repainted — the z-order
		// pass below can only force nodes ABOVE a painter, so a vanished
		// overlay's vacated cells have to be restored from this side. The
		// vanished node itself paints nothing: erasure is a sweep, the
		// same as a vacated bounds, and costs zero paint nodes.
		if v := visibilityOf(n.w); v != n.vis {
			was := n.vis
			n.vis = v
			if was == Visible && v != Visible {
				fillRect(c.frame.Cells, old, c.clearStyle(n))
				// The cell clear cannot reach the pixel plane: dropping
				// the node's recorded placements is what makes the next
				// placement diff take its images off the screen.
				n.places = n.places[:0]
				c.restoreUnder(old)
			} else {
				n.rev.Set(n.rev.Get() + 1)
			}
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
		// A non-paintable node is never forced from below: it has nothing
		// on screen to restore, and forcing it would run its pre-clear
		// over cells the restore pass just repainted (a Hidden overlay
		// keeps its bounds but owns no pixels).
		if n.stamp != c.frameSeq && n.bounds.W > 0 && n.bounds.H > 0 && paintable(n.w) {
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

// Damage reports the arranged bounds of the components the most recent
// Frame repainted, in z-order — the damage-discipline number made
// visible as rectangles, one per repainted component. The slice is a
// fresh copy each call.
//
// UI goroutine only, and read it between frames (an AfterFrame hook is
// the natural place): the next Frame overwrites it. Reading it composes
// nothing and dirties nothing — it is plain bookkeeping about the frame
// that already happened.
func (c *Composer) Damage() []Rect {
	out := make([]Rect, 0, len(c.over))
	for _, n := range c.over {
		out = append(out, n.bounds)
	}
	return out
}

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

// restoreUnder is the reverse half of the z-ordered repaint. The forward
// pass in Frame restores what sits ABOVE a rect somebody painted; this
// restores what sat BENEATH a rect that just left the screen — a
// component turned Hidden/Collapsed, departed in a re-sync, or moved.
// Every still-visible node whose bounds intersect the vacated rect is
// force-dirtied, and the ordinary paint loop then lays them down again
// in z-order, with the forward pass keeping everything above them
// honest. Decorators are included: the cells they re-style are exactly
// the ones being restored.
//
// Runs outside any evaluation (from the sweeps), so the Sets here are
// the same legality as the bounds sweep's.
func (c *Composer) restoreUnder(r Rect) {
	if r.W <= 0 || r.H <= 0 {
		return
	}
	for _, n := range c.nodes {
		if n.bounds.W <= 0 || n.bounds.H <= 0 || !paintable(n.w) {
			continue
		}
		if intersects(r, n.bounds) {
			n.rev.Set(n.rev.Get() + 1)
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
