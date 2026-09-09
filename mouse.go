package gooey

import (
	"time"

	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/prop"
)

// The pointer half of the input chapter. Mouse events route the way keys
// do — tunnel down to the target, then bubble back up its ancestors —
// but the target is found by hit-testing the retained tree instead of by
// focus, and three framework behaviors wrap the routing: hover tracking,
// focus-follows-click, and pointer capture, which suspends the first two
// for the length of a drag.

// MouseHandler is the optional interface for components that consume
// pointer events. Returning true stops propagation.
type MouseHandler interface{ HandleMouse(input.MouseEvent) bool }

// MouseMoveHandler opts a component into raw motion events (drag, resize).
// Motion is high-frequency, so it is delivered only to components that ask
// for it — everything else sees hover enter/leave instead.
type MouseMoveHandler interface{ HandleMouseMove(input.MouseEvent) bool }

// PreviewMouseHandler is the pointer twin of PreviewKeyHandler: an
// ancestor of the routing target is offered the event on the way down,
// root first, and returning true ends the dispatch before the target
// sees it. It covers every kind, motion included, which is what lets an
// overlay swallow the pointer for the layer underneath.
type PreviewMouseHandler interface{ PreviewMouse(input.MouseEvent) bool }

// HoverTarget is how the framework tells a component the pointer entered or
// left it. HoverState implements it.
type HoverTarget interface{ SetHovered(bool) }

// HoverState is the pointer twin of FocusState, and works the same way:
// the flag is a source property, so a Render that reads IsHovered() gets
// enter/leave as damage and only the component entered and the one left
// repaint. Components that do not embed it cost nothing — the dispatcher
// simply finds no hover target.
type HoverState struct{ hovered *prop.Property[bool] }

func (h *HoverState) SetHovered(v bool) { h.hover().Set(v) }
func (h *HoverState) IsHovered() bool   { return h.hover().Get() }

func (h *HoverState) hover() *prop.Property[bool] {
	if h.hovered == nil {
		h.hovered = prop.NewSource(false)
	}
	return h.hovered
}

// HitTestTransparent marks components the pointer passes THROUGH:
// hit-testing never returns one, so events land on whatever sits
// beneath. Its children remain hittable — transparency is about the
// component's own (often invisible) surface, not its subtree.
//
// The overlay hosts need this to exist at all: a ToastHost or an
// AdornmentLayer spans the whole page, so wherever the walk reaches it
// it would be an invisible layer eating every click and starving every
// hover beneath it. Non-interactive adornments (a tooltip's popup) are
// transparent for the same reason.
//
// It also used to be what kept those two HOSTS safe under a
// paint/input divergence, and that divergence is gone: this walk asks
// overlayOf now (#465), so an overlay is hit where it paints rather than
// where it was declared. Transparency is still the right answer for a
// full-page host — it should not eat clicks it has no use for — but it
// is no longer load-bearing for anyone else's correctness.
//
// Their CHILDREN were the case that actually bit: a Toast is hittable,
// so a host declared first painted its toasts above a button and left
// the click to the button. TestARankOrdersHitTestingAsWellAsPaint now
// pins the agreement, and components/toast.go carries the author-facing
// version.
type HitTestTransparent interface{ HitTestTransparent() bool }

// PointerFollower is implemented by a component whose arranged position
// comes from the POINTER rather than from its place in the tree: a drag
// ghost, a drop indicator, a marquee rectangle, a crosshair. It is the
// free-position half of the adornment layer (issue #177) — an adornment
// that implements it is exempt from the layer's anchor sweep entirely
// (there is no anchor to be gone) and has Place called with the
// pointer's cell instead of an anchor's bounds.
//
// The METHOD is what decides whether the component is following RIGHT
// NOW; implementing the interface at all is what makes it free. The two
// are deliberately different questions, because the wakeup cost lives
// on the first one and the lifetime on the second: ?1003h reports a
// motion event per cell crossed, so a component that follows for the
// length of a gesture must stop costing anything the moment the gesture
// ends, without having to leave the layer to do it. A parked follower —
// in the layer, FollowsPointer() false — is arranged to a zero rect and
// costs exactly nothing per motion.
//
// The Composer arms an observer for every component that implements
// this (Composer.armPointer, the armFrozen shape): the observer CALLS
// FollowsPointer, so whatever the implementation reads to decide becomes
// its dependency by the ordinary call-site rule, and only while the
// answer is true does it also read the pointer. That is what makes the
// wake self-scoping, and it is why nothing here asks the author to
// remember to read the pointer from Render. The observer is not a paint
// node: it schedules a frame and counts as no damage.
type PointerFollower interface{ FollowsPointer() bool }

// HitTest returns the component the pointer is over: THE ONE THAT PAINTS
// LAST among those whose arranged bounds — AND EVERY ANCESTOR'S BOUNDS —
// contain the cell. Collapsed subtrees, zero-size components, and
// HitTestTransparent components are not hit. The walk allocates nothing
// — it runs on every motion event.
//
// THE ANCESTOR CLAUSE IS THE ONE PLACE THE TWO PLANES STILL DIVERGE, and
// it is stated rather than fixed. This walk prunes on bounds at every
// node; paint walks c.paint flat and clips each node to ITS OWN rect, so
// a child arranged outside its parent's rectangle paints and can never
// be hit. Measured, with an overlay child arranged one row below its
// owner:
//
//	row 1 painted = the overlay's cells
//	HitTest(0, 1) = the page underneath
//
// That is the dropdown shape — Popup.ArrangeSurface exists to place a
// surface at a rect the owner chooses, and MenuBar.Arrange hands it a
// popupRect() below the bar row. No live bug follows, because
// popupSurface is the only non-transparent shipped Overlay whose bounds
// can escape its parent and Popup.Open takes capture; the sentence had
// to change anyway, because four files were claiming more than the code
// does. Descending into a lifted subtree regardless of the ancestor
// prune is the other resolution, and it is a behaviour change that wants
// its own PR — filed as #482, with both candidate resolutions and what
// each costs, because a deferral with no number cannot be checked for
// having happened. Pinned by TestAnOverlayOutsideItsParentPaintsAndIsNotHit
// so the divergence cannot quietly become something else.
// Raised in review of #478.
//
// "Paints last" is one sentence and it is deliberately the SAME sentence
// the Composer's paint order is derived from, because the two used to be
// different rules that happened to agree. Document order gives you
// children over ancestors and later siblings over earlier ones, which is
// what this walk used to say directly; the overlay layer (#437) and its
// ranks (#439) then made paint answer something else, and #465 was the
// two planes disagreeing:
//
//	over  := &rankedStripe{stripe{ch: 'O', rank: OverlayRankToast}}
//	under := &overlayStripe{stripe{ch: 'U'}}   // rank 0, declared later
//
// `over` painted over `under` and `under` took the click. Under the
// retired "declare it last is z-order" rule that could not happen — the
// thing on top was also the thing this walk found first — so the
// divergence arrived with the freedom, not with the layer.
//
// The fix is not a second ordering; it is asking the ONE ordering.
// overlayOf is the shared membership-and-rank rule (component.go),
// orderPaint and gooey.Compose's collectPaint already call it, and a hit
// candidate is now compared on exactly what appendByRank orders by:
// the overlay layer above the ordinary one, then a higher rank within
// it, and only then document order.
// Nothing here consults the Composer, because it does not need to
// — the rule is a function of the tree, which is the whole reason it was
// extracted in #438.
//
// WHAT THIS COST. The walk no longer returns on the first hit: an
// earlier sibling can out-rank a later one, so every subtree whose
// bounds contain the point is visited. It is still pruned by bounds at
// every node, which is where the work actually was — a sibling that does
// not contain the point costs the same rectangle test it always did, and
// siblings that do overlap were already both visited by the reverse
// walk on a miss. What is gone is the early exit on a HIT, and that is
// one extra rectangle test per remaining sibling on the path.
//
// Popup never depended on any of this: it holds pointer capture while
// open (Popup.Open), so presses never reach the walk. That is Popup's
// mechanism, not something the Overlay marker provides — an overlay that
// takes no capture is now routed correctly instead of being documented
// as an exception in four files.
func (m *FocusManager) HitTest(x, y int) Component {
	var best hitCandidate
	order := 0
	hitTest(m.root, x, y, 0, false, 0, &order, &best)
	return best.w
}

// hitCandidate is the running best of the walk: the component that paints
// last among those found so far. Held by pointer through the recursion
// rather than returned, so the walk still allocates nothing.
//
// order is the node's index in DEPTH-FIRST PRE-ORDER, and it is the LAST
// question asked rather than the rule: the overlay layer decides first,
// then the rank within it, and position separates only two components
// that tie on both. Same numbering c.nodes carries, which is why this
// walk can run forward where the old one had to run in reverse.
type hitCandidate struct {
	w       Component
	rank    int
	overlay bool
	order   int
}

// beatenBy reports whether a candidate with these coordinates paints
// above the one already held. It is appendByRank's ordering, asked one
// pair at a time: the lifted layer is above the ordinary one, a higher
// rank is above a lower one WITHIN that layer, and equal ranks fall back
// to position — which is where document order still decides something,
// and the only place it does.
//
// rank is not consulted for two ordinary components, because it is not
// meaningful there — overlayOf returns 0 for them, so comparing it would
// be reading a field that means nothing rather than a tie.
func (h *hitCandidate) beatenBy(overlay bool, rank, order int) bool {
	if h.w == nil {
		return true
	}
	if h.overlay != overlay {
		return overlay
	}
	if overlay && h.rank != rank {
		return rank > h.rank
	}
	return order > h.order
}

// depth is threaded rather than counted in a package variable because
// this walk returns from the middle of a loop on every miss — the
// deferred-decrement trick MeasureChild uses would cost a defer per
// component per mouse move, and a parameter costs an increment.
//
// parentOverlay and parentRank are threaded for the same reason
// orderPaint inherits them down c.nodes: membership moves a whole
// SUBTREE, so a node's answer is its lifting ancestor's, and asking each
// node on its own would let a nested Overlay sort out of its parent's
// run.
func hitTest(w Component, x, y, depth int, parentOverlay bool, parentRank int, order *int, best *hitCandidate) {
	if depth > MaxLayoutDepth {
		noteLayoutFaultAt("HitTest", w, depth)
		return
	}
	if l := LayoutOf(w); l != nil && l.Visibility == Collapsed {
		return
	}
	b, ok := w.(Bounded)
	if !ok {
		return
	}
	r := b.Bounds()
	if x < r.X || y < r.Y || x >= r.X+r.W || y >= r.Y+r.H {
		return
	}
	// WHICH SIDE OF THE BOUNDS TEST THIS SITS ON DOES NOT MATTER, and
	// that is worth writing down because it looks like it should. An
	// earlier spelling numbered every visited node and carried a comment
	// arguing the number had to be a document index rather than a count
	// of hits; moving it here was measured and is SILENT across the
	// suite, because it is an equivalent change. Nodes are numbered in
	// visit order either way, a miss is never a candidate, and only
	// candidates are ever compared — so the two spellings differ in the
	// values and agree on every ordering. The comment was the defect,
	// not the placement.
	mine := *order
	*order++
	overlay, rank := overlayOf(w, parentOverlay, parentRank)
	if c, ok := w.(Container); ok {
		// DELIBERATELY no Frozen check here, and it is not an oversight.
		// Freezing constrains DISPATCH, not this query: hit-testing must
		// keep returning the deepest component so a design surface can
		// call HitTest, find the actual <Button> under the pointer and
		// select it, while DispatchMouse hands the press to the frozen
		// host (see FocusManager.target). Stopping the descent here would
		// make click-to-select impossible, and every freeze test would
		// stay green while it broke.
		for _, kid := range c.ChildComponents() {
			hitTest(kid, x, y, depth+1, overlay, rank, order, best)
		}
	}
	if t, ok := w.(HitTestTransparent); ok && t.HitTestTransparent() {
		return
	}
	if best.beatenBy(overlay, rank, mine) {
		*best = hitCandidate{w: w, rank: rank, overlay: overlay, order: mine}
	}
}

// CaptureMouse routes every pointer event to w until ReleaseCapture,
// regardless of what the pointer is actually over. It reports false for a
// component that is not in the tree.
//
// A press already captures implicitly (see DispatchMouse), so this is for
// the cases a press cannot express: a component that must keep the
// pointer past the release, or one that takes it without a press at all.
// Called from inside a press handler it upgrades that press's implicit
// capture into a held one, which is how a drag survives the button
// coming up.
func (m *FocusManager) CaptureMouse(w Component) bool {
	if w == nil {
		return false
	}
	if _, live := m.parent[w]; !live {
		return false
	}
	m.captor, m.held = w, true
	return true
}

// ReleaseCapture gives the pointer back to hit-testing. Hover is left
// wherever it was frozen; the next motion event re-establishes it.
func (m *FocusManager) ReleaseCapture() { m.captor, m.held = nil, false }

// Captured returns the component holding the pointer, or nil.
func (m *FocusManager) Captured() Component { return m.captor }

// DispatchMouse routes a pointer event. The target is the component the
// pointer is over — or, while the pointer is CAPTURED, the captor
// regardless of where the pointer is. From there the event tunnels down
// the target's ancestor chain (PreviewMouseHandler, root first) and then
// bubbles up from the target like a key.
//
// Two framework behaviors run before the app sees anything, and both are
// skipped while captured: the hover target is updated, and a press moves
// focus to the nearest focusable component at or above the hit (the
// focus-follows-click convention). Wheel events follow the same rule as
// everything else — the captor while captured, the component under the
// pointer otherwise, never the focused one.
//
// A press CAPTURES the component it landed on until the release. That is
// what makes drags work: motion outside the component still reaches it,
// so a scrollbar thumb, a splitter or a text selection keeps tracking a
// pointer that has left its bounds, and the release always comes back to
// the component that started the gesture. The capture is given up on
// release unless the captor took it explicitly.
//
// A click is synthesized on release when the pointer is still inside the
// captor — the captor itself or anything under it — which is what makes
// a button pressed, dragged off and dragged back still fire, and a button
// released elsewhere not fire. It carries a click count, so a second
// click on the same component inside DoubleClickInterval arrives as
// Count 2.
func (m *FocusManager) DispatchMouse(ev input.MouseEvent) bool {
	// One retarget, here, for everything downstream. A frozen subtree does
	// not act, so for every routing purpose the effective hit IS the
	// frozen host: it takes the event, it takes the implicit capture, it
	// takes the focus a press moves, and the click synthesized on release
	// is measured against it.
	//
	// Retargeting only in target() was not enough and the press proved it:
	// a press sets the implicit captor from the hit BEFORE routing, and
	// target() returns the captor first, so the raw descendant got the
	// event back through the capture. Doing it once at the top is also why
	// setHover does not repeat the check.
	//
	// HitTest itself still returns the deepest component — see the comment
	// there. This is dispatch; that is a query.
	//
	// TWO retargets now, because AllowPointer and AllowHover are separate
	// categories and a design surface wants exactly that split: the
	// element under the pointer lights up, and clicking it does nothing.
	// hit is what ROUTES, hov is what HOVERS, and they are equal for every
	// host that answers the bool — AllowNone withholds both, so both walks
	// stop at the same ancestor.
	deepest := m.HitTest(ev.X, ev.Y)
	hit := m.frozenHostFor(deepest, AllowPointer)
	hov := m.frozenHostFor(deepest, AllowHover)
	// Every kind carries a position, so every kind updates it — a drag
	// ghost raised inside a press handler must find the pointer already
	// where the press was, not one motion event later. MouseTarget
	// deliberately does not do this: it is a query, and a query moves
	// nothing.
	m.notePointer(ev.X, ev.Y)

	switch ev.Kind {
	case input.MousePress:
		// A press dismisses transient UI (tooltips) before it routes —
		// even while captured, and without consuming anything.
		m.interrupt()
		// Only a HELD capture survives a new press. An implicit one is
		// scoped to a single press-release gesture, so a fresh press ends
		// it and begins another — which is also the recovery path when a
		// release never arrives (a terminal that dropped the report, a
		// suspend mid-drag) rather than the pointer being stuck forever.
		if !m.held {
			m.captor = nil
			m.setHover(hov)
			if w := m.focusTargetFor(hit); w != nil {
				m.SetFocus(w)
			}
			m.captor = hit
		}
		return m.routeMouse(m.target(hit), ev)

	case input.MouseRelease:
		captor := m.captor
		handled := m.routeMouse(m.target(hit), ev)
		if m.within(captor, hit) {
			click := ev
			click.Kind, click.Count = input.MouseClick, m.clickCount(captor)
			if m.routeMouse(captor, click) {
				handled = true
			}
		}
		// A held capture outlives its release; an implicit one ends here,
		// and hover — frozen for the length of the gesture — catches up
		// with where the pointer actually is.
		if !m.held {
			m.captor = nil
			m.setHover(hov)
		}
		return handled

	case input.MouseMove:
		if m.captor == nil {
			m.setHover(hov)
		}
		target := m.target(hit)
		if m.tunnelMouse(target, ev) {
			return true
		}
		for n := target; n != nil; n = m.parent[n] {
			if h, ok := n.(MouseMoveHandler); ok && h.HandleMouseMove(ev) {
				return true
			}
		}
		return false

	default:
		return m.routeMouse(m.target(hit), ev)
	}
}

// MouseTarget reports the component DispatchMouse WOULD route ev to,
// without dispatching it and without touching hover, focus or capture.
//
// It exists because a caller that needs to decide something about an
// event before delivering it — a control-plane session checking that a
// guest's pointer stays inside its island — must ask about the
// EFFECTIVE target, and the effective target is not the hit. Two
// framework behaviours move it, and paraphrasing either one at the call
// site is how a check drifts from the routing it claims to model:
//
//   - Frozen retargets. `HitTest` returns the deepest component on
//     purpose (see the comment there), but a frozen subtree does not
//     act, so dispatch routes to the frozen HOST. A check on the raw hit
//     would clear an event whose delivery lands somewhere else entirely.
//   - Capture overrides. While the pointer is captured every event goes
//     to the captor regardless of where it points — which is what makes
//     a drag work outside the captor's bounds, and a check on the hit
//     alone would refuse exactly those events.
//
// The press asymmetry is the third thing, and it is the one worth
// spelling out: a fresh press DISCARDS an implicit capture (only a HELD
// one survives), so a press routes to the hit even when a stale implicit
// captor is still recorded. DispatchMouse gets that for free by setting
// m.captor before it calls target; a query made BEFORE dispatch has to
// say it.
//
// UI-goroutine only, like every other query on this type.
func (m *FocusManager) MouseTarget(ev input.MouseEvent) Component {
	hit := m.frozenHostFor(m.HitTest(ev.X, ev.Y), AllowPointer)
	if ev.Kind == input.MousePress && !m.held {
		return hit
	}
	return m.target(hit)
}

// target is where an event routes: the captor while the pointer is
// captured, the hit otherwise.
//
// No Frozen check here. DispatchMouse retargets the hit ONCE, at the top,
// and everything downstream — including this — is already holding the
// effective hit. Doing it in both places is how the freeze tests came to
// pass with one of the two deleted.
func (m *FocusManager) target(hit Component) Component {
	if m.captor != nil {
		return m.captor
	}
	return hit
}

// MaxClickCount is the highest count a click sequence reports. Three:
// single, double, triple. A FOURTH rapid click starts a new sequence at
// 1 rather than reporting 4, for the reason the original two-click
// version gave for stopping at 2 — reporting a number nothing
// understands is worse than starting fresh — and because "select
// progressively more" has no fourth step to offer.
const MaxClickCount = 3

// clickCount advances the click sequence for w. A click elsewhere, or
// one that came too late, starts over; so does the click after the last
// one the sequence can express.
//
// Each click must land on the SAME component and inside
// DoubleClickInterval of the one before it — the interval is measured
// click-to-click, not from the start of the run, so a slow third click
// begins a new sequence rather than completing a triple. That is what
// makes the gesture a rhythm rather than a race against a deadline that
// started two clicks ago.
func (m *FocusManager) clickCount(w Component) int {
	now := m.now()
	if w == m.lastClick && m.clicks < MaxClickCount && now.Sub(m.lastClickAt) <= m.DoubleClickInterval {
		m.clicks++
	} else {
		m.clicks = 1
	}
	m.lastClick, m.lastClickAt = w, now
	return m.clicks
}

func (m *FocusManager) now() time.Time {
	if m.Now == nil {
		return time.Now()
	}
	return m.Now()
}

// routeMouse is the tunnel-then-bubble pair every kind but motion uses.
func (m *FocusManager) routeMouse(target Component, ev input.MouseEvent) bool {
	return m.tunnelMouse(target, ev) || m.bubbleMouse(target, ev)
}

func (m *FocusManager) tunnelMouse(target Component, ev input.MouseEvent) bool {
	for d := m.depth(target); d >= 0; d-- {
		if h, ok := m.ancestor(target, d).(PreviewMouseHandler); ok && h.PreviewMouse(ev) {
			return true
		}
	}
	return false
}

func (m *FocusManager) bubbleMouse(target Component, ev input.MouseEvent) bool {
	for n := target; n != nil; n = m.parent[n] {
		if h, ok := n.(MouseHandler); ok && h.HandleMouse(ev) {
			return true
		}
	}
	return false
}

// focusTargetFor picks what a press on w should focus: w itself or the
// nearest focusable ancestor, and failing that the first focusable
// DESCENDANT. The descent is what makes clicking a pane's border or
// title focus the pane — the hit there is the Border, whose focusable
// content is below it in the tree, not above.
func (m *FocusManager) focusTargetFor(w Component) Component {
	for n := w; n != nil; n = m.parent[n] {
		if f, ok := n.(Focusable); ok && f.AcceptsFocus() {
			return n
		}
	}
	return firstFocusable(w, 0)
}

func firstFocusable(w Component, depth int) Component {
	if depth > MaxLayoutDepth {
		noteLayoutFaultAt("Focusable", w, depth)
		return nil
	}
	if w == nil {
		return nil
	}
	if l := LayoutOf(w); l != nil && l.Visibility == Collapsed {
		return nil
	}
	if f, ok := w.(Focusable); ok && f.AcceptsFocus() {
		return w
	}
	if c, ok := w.(Container); ok {
		for _, ch := range c.ChildComponents() {
			if hit := firstFocusable(ch, depth+1); hit != nil {
				return hit
			}
		}
	}
	return nil
}

// Hovered returns the component the pointer is currently over.
func (m *FocusManager) Hovered() Component { return m.hover }

// Pointer is the cell the pointer was last seen at, as a 1x1 rect, and
// whether one has ever been reported (false before the first mouse
// event, and forever in an app that never enabled the mouse).
//
// The CALL SITE decides what asking means, as everywhere else: read from
// inside an evaluation — a Render, a PointerFollower observer — it is a
// subscription, and the next motion that changes the cell schedules a
// frame. Read from Measure, Arrange or an event handler it is a plain
// question and records nothing, which is what lets AdornmentLayer.Arrange
// place a ghost against it every frame without ever creating a
// dependency on it.
//
// The rect is 1x1 rather than a bare X/Y so that placement policy is
// shared with anchored adornments: PlacePopup and every hand-written
// Place take a rect, so pointing at a cell and pointing at a component
// go through the same geometry.
func (m *FocusManager) Pointer() (Rect, bool) {
	m.pointerRev().Get()
	return Rect{X: m.ptrX, Y: m.ptrY, W: 1, H: 1}, m.ptrSeen
}

// pointerRev is the revision property, created on first use. A source
// property with no dependents is free to Set — invalidate() walks an
// empty dependent set — which is the whole zero-cost story: with nothing
// following the pointer, a motion event allocates nothing, dirties
// nothing, and schedules no frame.
func (m *FocusManager) pointerRev() *prop.Property[int] {
	if m.ptrRev == nil {
		m.ptrRev = prop.NewSource(0)
	}
	return m.ptrRev
}

// notePointer records where a pointer event landed. Guarded on the CELL
// because prop.Set does not compare values: an emulator may re-report
// the same cell (a press and its release, sub-cell motion under a pixel
// -reporting mode), and each of those would otherwise be a frame.
func (m *FocusManager) notePointer(x, y int) {
	if m.ptrSeen && m.ptrX == x && m.ptrY == y {
		return
	}
	m.ptrX, m.ptrY, m.ptrSeen = x, y, true
	m.pointerRev().Set(m.pointerRev().Get() + 1)
}

// setHover moves the hover flag to the nearest HoverTarget at or above
// the hit component, so hover composes: a Border can highlight while the
// pointer is over the Text inside it.
// A frozen subtree does not react to the pointer: the hover lands on the
// frozen host rather than the button underneath it. The retarget happens
// once, in DispatchMouse, so every caller here is already holding the
// effective hit.
//
// That is a decision with a named cost. Hover STYLING on a descendant is
// given up, and hover styling is the one animation-shaped thing this
// framework can do for free, since HoverState is an ordinary property and
// motion over time would need a clock (which is a Startable, which
// freezing stops). Wanting it back inside a design surface is an opt-in
// for whoever turns up with the case, not a default.
func (m *FocusManager) setHover(hit Component) {
	// HoverWatcher attachments update on the raw hit, before the
	// HoverTarget dedup below: the hit can cross from one watched host
	// to another without the nearest HoverTarget changing at all.
	m.updateWatchers(hit)
	var target Component
	for n := hit; n != nil; n = m.parent[n] {
		if _, ok := n.(HoverTarget); ok {
			target = n
			break
		}
	}
	if target == m.hover {
		return
	}
	if h, ok := m.hover.(HoverTarget); ok {
		h.SetHovered(false)
	}
	m.hover = target
	if h, ok := m.hover.(HoverTarget); ok {
		h.SetHovered(true)
	}
}

// updateWatchers turns per-event containment into enter/leave edges for
// HoverWatcher attachments. Exclusive like hover: among the watching
// hosts whose subtree contains the hit, only the INNERMOST is "entered",
// so nested tooltipped components never show two tips at once. A page
// with no watchers pays one length check.
func (m *FocusManager) updateWatchers(hit Component) {
	if len(m.watchers) == 0 {
		return
	}
	var in Component
	depth := -1
	if hit != nil {
		for _, hw := range m.watchers {
			if m.within(hw.host, hit) {
				if d := m.depth(hw.host); d > depth {
					in, depth = hw.host, d
				}
			}
		}
	}
	for _, hw := range m.watchers {
		over := in != nil && hw.host == in
		if over != hw.over {
			hw.over = over
			hw.w.PointerOver(over)
		}
	}
}

// interrupt notifies every HoverWatcher of input activity — a key or a
// press. Notification only: nothing is consumed, nothing re-routes.
func (m *FocusManager) interrupt() {
	for _, hw := range m.watchers {
		hw.w.Interrupted()
	}
}
