package components

import "github.com/WonderForgeLabs/gooey"

// Adornment is a component positioned against a TARGET component's
// arranged bounds instead of by its own place in layout — WPF's adorner,
// absent from every TUI framework. A validation marker pinned to a
// TextBox, a focus ring, a badge on an icon, a tooltip: each is an
// ordinary component (its own paint node, its own damage) that answers
// two questions the layer asks — whose bounds am I anchored to, and
// where do I go relative to them.
type Adornment interface {
	gooey.Component
	// Anchor is the component this adornment is positioned against. It
	// must expose bounds (embed gooey.Base). An anchor that leaves the
	// tree, turns non-visible, or loses its bounds takes the adornment
	// down with it.
	//
	// A FREE adornment (one implementing gooey.PointerFollower) is never
	// asked: it is placed against the pointer, so returning nil is the
	// whole of its contribution here.
	Anchor() gooey.Component
	// Place returns the bounds the adornment wants, given the rect it is
	// pinned to and the layer's own (the screen, when the layer is hosted
	// at the root). Placement policy — flip-to-fit, clamping, which edge
	// to hug — belongs to the adornment; the layer only asks.
	//
	// `against` is the ANCHOR's arranged bounds for an ordinary
	// adornment, and the POINTER's 1x1 cell for one that implements
	// gooey.PointerFollower. One method rather than two, because the
	// policy is identical either way and PlacePopup already speaks rects:
	// a tooltip that flips above its anchor and a ghost that flips above
	// the pointer are the same three lines.
	Place(against, layer gooey.Rect) gooey.Rect
}

// orphanable is how the layer tells an adornment it was dropped because
// its anchor vanished — the one removal the adornment's owner did not
// ask for, so it is the one the owner has to hear about (a Tooltip must
// know its popup is gone or it would refuse to ever show again).
type orphanable interface{ orphaned() }

// PersistentAdornment is an optional refinement of Adornment, added for
// the layer's second customer. The drop-on-invisible policy was written
// for TRANSIENT adornments — a tooltip's owner re-raises it on the next
// hover, so dropping on a hidden anchor is free. A PERSISTENT adornment
// (a validation marker up for as long as its field is invalid) has no
// re-raising gesture: dropping it on a collapsed pane would lose it
// until the next structural walk, and re-adding it eagerly would fight
// the layer's own sweep, one add and one drop per frame, forever.
//
// So a persisting adornment whose anchor is merely INVISIBLE — still in
// the tree, but Hidden/Collapsed or arranged to nothing — is kept and
// arranged to a zero rect: present, subscribed, occupying nothing, back
// the moment its anchor is. Only an anchor that truly LEFT the tree
// still drops it (with the orphaned notification, as ever).
type PersistentAdornment interface {
	Adornment
	AdornmentPersists() bool
}

// AdornmentLayer hosts adornments above the whole page: the app declares
// it as the LAST child of its root — document order is z-order, the same
// hosting shape as ToastHost — and adorners are added and removed at
// runtime through the Dynamic re-sync a list uses. The layer paints
// nothing and declares no background, so a page that never shows an
// adornment pays nothing for hosting the layer.
//
// Anchoring is re-evaluated every frame, for free: layout runs
// unconditionally, so Arrange re-reads every anchor's bounds and
// re-places its adornments — a moved or resized anchor drags them along,
// and the Composer's bounds sweep turns the move into damage (repaint at
// the new rect, restore beneath the old). "Anchor bounds as a
// dependency" is realized by the layout pass, not the property graph:
// bounds are plain fields, and the per-frame Arrange is what watches
// them.
//
// An anchor that is no longer visibly reachable — it left the tree in a
// re-sync, or it (or an ancestor) went Hidden/Collapsed — has its
// adornments dropped in the same Arrange, and the restore pass repaints
// what they covered. The reachability walk runs only while adornments
// are up.
//
// FREE adornments skip all of that. A free adornment has no component
// anchor at all: it implements gooey.PointerFollower and is placed
// against the POINTER instead (issue #177). Drag ghosts, drop indicators, marquee rectangles and a
// crosshair inside a Canvas all want this — they exist only for the
// length of a gesture, and there is no component whose bounds describe
// where they go.
//
// Two consequences, and both are the point:
//
//   - A free adornment is exempt from the anchor sweep ENTIRELY. Anchor
//     is never called and never has to return anything real; nothing
//     can orphan it, because there is no anchor to be gone. Its owner
//     put it up and its owner takes it down — which is the same opt-out
//     PersistentAdornment makes, one step further.
//   - Its Place receives the pointer's 1x1 cell as `against`. That is
//     why the parameter is a rect and not a component: PlacePopup and
//     every hand-written Place already work on rects, so pointing at a
//     cell and pointing at a component share one geometry vocabulary.
//     A ghost offsets from it, a marquee spans from its own origin to
//     it, a crosshair centres on it.
//
// Whether it is following right now is FollowsPointer's answer, asked
// every frame. False — the gesture ended, the ghost is parked — arranges
// it to a zero rect: still there, still subscribed, occupying and
// painting nothing, and costing nothing per motion. See
// gooey.PointerFollower for why that split is what bounds the wakeup
// cost, and Composer.armPointer for what makes a motion schedule a frame
// at all.
type AdornmentLayer struct {
	gooey.Base
	adorns    []Adornment
	structure func()
	mgr       *gooey.FocusManager
}

// SetStructureHook receives the composition's structural-change hook —
// adding and removing adorners are child-set changes (gooey.Dynamic).
func (l *AdornmentLayer) SetStructureHook(fn func()) { l.structure = fn }

// SetFocusManager receives the input tree (gooey.FocusHost) — the seam
// the layer checks anchor reachability through.
func (l *AdornmentLayer) SetFocusManager(m *gooey.FocusManager) { l.mgr = m }

func (l *AdornmentLayer) ChildComponents() []gooey.Component {
	kids := make([]gooey.Component, len(l.adorns))
	for i, a := range l.adorns {
		kids[i] = a
	}
	return kids
}

// Adornments is what the layer is currently showing, in z-order.
func (l *AdornmentLayer) Adornments() []Adornment { return l.adorns }

// Add puts an adornment up. UI goroutine only, like everything that
// reaches the tree; it appears on the next frame, positioned by Place.
func (l *AdornmentLayer) Add(a Adornment) {
	l.adorns = append(l.adorns, a)
	if l.structure != nil {
		l.structure()
	}
}

// Remove takes an adornment down (idempotent — an owner's dismissal and
// the layer's own anchor sweep may both reach for the same adorner).
func (l *AdornmentLayer) Remove(a Adornment) {
	for i, x := range l.adorns {
		if x == a {
			l.adorns = append(l.adorns[:i], l.adorns[i+1:]...)
			if l.structure != nil {
				l.structure()
			}
			return
		}
	}
}

// Measure fills its slot — the layer is a positioning surface over the
// whole page, like Canvas and ToastHost.
func (l *AdornmentLayer) Measure(avail gooey.Size) gooey.Size {
	for _, a := range l.adorns {
		gooey.MeasureChild(a, avail)
	}
	return avail
}

// Arrange places every adornment where its Place asks, against its
// anchor's CURRENT bounds — and drops the ones whose anchor is gone.
// Dropping here raises the structure hook mid-layout, which is exactly
// the seam a Dynamic container uses: the re-sync runs after layout and
// clears the vacated cells this same frame.
func (l *AdornmentLayer) Arrange(b gooey.Rect) {
	l.Base.Arrange(b)
	var root gooey.Component
	if l.mgr != nil {
		root = l.mgr.Root()
	}
	pointer, seen := gooey.Rect{}, false
	if l.mgr != nil {
		// A plain read: Arrange runs outside any evaluation, so this
		// records no dependency and the layer's own node stays clean
		// through a whole drag. The wake comes from the follower's
		// observer, not from here.
		pointer, seen = l.mgr.Pointer()
	}
	live := l.adorns[:0]
	dropped := false
	for _, a := range l.adorns {
		// Free adornments first, and BEFORE Anchor is consulted: a
		// pointer-followed adornment has no anchor, so every question
		// below — bounds, reachability, orphaning — is one it cannot
		// answer and must not be asked.
		if p, free := a.(gooey.PointerFollower); free {
			live = append(live, a)
			if seen && p.FollowsPointer() {
				gooey.ArrangeChild(a, a.Place(pointer, b))
			} else {
				gooey.ArrangeChild(a, gooey.Rect{})
			}
			continue
		}
		anchor := a.Anchor()
		ab, ok := anchorBounds(anchor)
		if !ok || (root != nil && !visiblyReachable(root, anchor)) {
			// An invisible-but-present anchor HIDES a persistent
			// adornment instead of dropping it (see PersistentAdornment);
			// the zero rect vacates its cells through the bounds sweep
			// like any move.
			if p, can := a.(PersistentAdornment); can && p.AdornmentPersists() &&
				root != nil && inTree(root, anchor) {
				live = append(live, a)
				gooey.ArrangeChild(a, gooey.Rect{X: ab.X, Y: ab.Y})
				continue
			}
			if o, can := a.(orphanable); can {
				o.orphaned()
			}
			dropped = true
			continue
		}
		live = append(live, a)
		gooey.ArrangeChild(a, a.Place(ab, b))
	}
	l.adorns = live
	if dropped && l.structure != nil {
		l.structure()
	}
}

// Render paints nothing: the layer is chrome-less, and each adornment
// owns its cells.
func (l *AdornmentLayer) Render(*gooey.Frame) {}

// PassesCellsThrough exempts the layer from the z-ordered force-from-below:
// its Render owns no cells, so "repaint what sits above this painter"
// has nothing to restore here — the adornments themselves are separate
// nodes and are forced normally. Without this, the full-page layer was
// force-repainted (a counted no-op with a full-page damage rect) every
// time any covered leaf under it painted — every keystroke into a
// TextBox, on any page hosting the layer. Found by the validation
// marker's damage pins, the layer's second customer.
func (l *AdornmentLayer) PassesCellsThrough() {}

// HitTestTransparent: the layer spans the whole page invisibly; the
// pointer must pass through it to the content beneath, or hosting the
// layer would starve every click and hover on the page.
func (l *AdornmentLayer) HitTestTransparent() bool { return true }

// attachAdornment is the attach half both AdornmentLayer customers share:
// find the page's layer through the input tree and put pop in it,
// returning the layer it went into — or nil, meaning "not placed", for
// the three ways that can fail identically. No host yet and no input
// tree yet are both "too early, ask again on the next walk"; a page that
// declares no layer is "this app shows no adornments", which is a
// supported configuration and not an error (a tooltipless page, a form
// whose errors show only in the TextBox).
//
// The already-placed guard stays at the CALL SITE, because "already up"
// is not the same question for the two of them: a Tooltip is transient
// and asks it per hover, a ValidationMarker is persistent and asks it on
// every input-tree re-sync. Only the lookup-and-add is common.
func attachAdornment(host gooey.Component, mgr *gooey.FocusManager, pop Adornment) *AdornmentLayer {
	if host == nil || mgr == nil {
		return nil
	}
	layer := findAdornmentLayer(mgr.Root())
	if layer == nil {
		return nil
	}
	layer.Add(pop)
	return layer
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

// anchorBounds is an anchor's arranged rectangle, and whether it has one
// worth anchoring to — a nil or unbounded component, or one arranged to
// nothing (a collapsed subtree), has not.
func anchorBounds(w gooey.Component) (gooey.Rect, bool) {
	if w == nil {
		return gooey.Rect{}, false
	}
	b, ok := w.(gooey.Bounded)
	if !ok {
		return gooey.Rect{}, false
	}
	r := b.Bounds()
	return r, r.W > 0 && r.H > 0
}

// inTree reports whether target is reachable from root at all,
// visibility ignored — the "still exists" half of the persistent
// adornment's keep-or-drop decision.
func inTree(root, target gooey.Component) bool {
	if root == nil || target == nil {
		return false
	}
	if root == target {
		return true
	}
	if c, ok := root.(gooey.Container); ok {
		for _, ch := range c.ChildComponents() {
			if inTree(ch, target) {
				return true
			}
		}
	}
	return false
}

// visiblyReachable reports whether target is reachable from root through
// Visible elements only — the live tree, walked fresh, so a component a
// Dynamic container removed THIS frame is already unreachable even
// before the input tree re-syncs.
func visiblyReachable(root, target gooey.Component) bool {
	if root == nil || target == nil {
		return false
	}
	if l := gooey.LayoutOf(root); l != nil && l.Visibility != gooey.Visible {
		return false
	}
	if root == target {
		return true
	}
	if c, ok := root.(gooey.Container); ok {
		for _, ch := range c.ChildComponents() {
			if visiblyReachable(ch, target) {
				return true
			}
		}
	}
	return false
}
