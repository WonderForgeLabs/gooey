package gooey

import (
	"time"

	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/prop"
)

// The input chapter: commands, focus, and routed key events.
//
// Commands are plain funcs bound from the viewmodel, so an event stays
// declarative in markup and needs no code-behind. Focus is framework-
// owned: the FocusManager walks the same tree the Composer does, holds
// the focused component, and moves with tab/shift+tab. A component learns it
// is focused by reading IsFocused() while painting, which makes focus a
// paint dependency like any other property — moving focus repaints the
// two components involved, not the screen.

// Action is what an event attribute resolves to: something that can run,
// and that can say whether running it is legal right now. Component
// fields that used to be typed Command — Button's Click, KeyBinding's
// Command, ItemsView's Activate — are Actions, so either form fits.
//
// Two things implement it. A plain Command is always executable, which
// is the overwhelmingly common case and the reason the func type carries
// the methods itself rather than needing a wrapper. A *Cmd adds a
// CanExecute condition that is an ordinary bool property.
//
// That property IS XAML's CanExecuteChanged, and the improvement over
// XAML is that there is no event to raise: the call site decides what a
// CanExecute call means. Called from Render it records a dependency, so
// the component repaints when the condition flips; called from a key or
// mouse handler it records nothing and is just a question. Nobody
// invalidates anything by hand.
type Action interface {
	// Run performs the action. Implementations must be no-ops when
	// CanExecute is false, so a caller that forgets to ask cannot run a
	// disabled command.
	Run()
	// CanExecute reports whether running is legal right now.
	CanExecute() bool
}

// CanExecute is the nil-tolerant form of the interface method: it is
// false for an absent action and for one whose condition says no. Use it
// wherever an Action arrives from markup or an app, since a field left
// unset is a nil interface and calling a method on that panics.
func CanExecute(a Action) bool { return a != nil && a.CanExecute() }

// Command is a bound action with no condition. Markup event attributes
// (Button's Click, KeyBinding's Command) resolve to one of these, either
// from a func in the binding context — Click="{{.Save}}", the delegate
// living in the viewmodel — or from the code-behind handler registry by
// bare name.
type Command func()

// Run calls the func. A nil Command runs nothing, which is what lets an
// unset attribute cross as a typed zero rather than a panic.
func (c Command) Run() {
	if c != nil {
		c()
	}
}

// CanExecute is true for any non-nil Command: a plain delegate has no
// condition to consult, so it is always enabled. Reading it records no
// dependency, which is correct — nothing about it can change.
func (c Command) CanExecute() bool { return c != nil }

// Cmd is a command with a condition: run this, but only while that bool
// property is true.
//
//	save := gooey.NewCommand(vm.Save).When(vm.Dirty)
//
// A Button bound to it paints itself disabled while Dirty is false and
// refuses enter, space and clicks; a KeyBinding whose Command is this
// declines the gesture and lets it keep bubbling. Nothing subscribes to
// anything: the button read the condition while painting, so the flip
// dirties that one paint node.
//
// The condition may be any bool property, and a computed is the point —
// When(prop.NewComputed(func() bool { return len(sel.Get()) > 0 })) is a
// CanExecute derived from the rest of the viewmodel, recomputed lazily
// and only when something it read actually changed.
type Cmd struct {
	run func()
	can *prop.Property[bool]
}

// NewCommand builds a conditional command around run. Without a When it
// behaves exactly like a Command.
func NewCommand(run func()) *Cmd { return &Cmd{run: run} }

// When sets the command's CanExecute condition and returns the command,
// so it chains onto NewCommand. A nil property means unconditional.
func (c *Cmd) When(can *prop.Property[bool]) *Cmd {
	if c != nil {
		c.can = can
	}
	return c
}

// CanExecute reads the condition. Called during a paint this is the
// subscription that repaints the reader when the condition flips.
func (c *Cmd) CanExecute() bool {
	if c == nil || c.run == nil {
		return false
	}
	if c.can == nil {
		return true
	}
	return c.can.Get()
}

// Run performs the action, or does nothing while the condition is false.
// The guard is here rather than only in callers so that "disabled" is
// structural: an Action reached by some path that forgot to ask still
// cannot fire.
func (c *Cmd) Run() {
	if !c.CanExecute() {
		return
	}
	c.run()
}

// KeyHandler is the optional interface for components that consume keys.
// Returning true stops propagation.
type KeyHandler interface{ HandleKey(input.KeyEvent) bool }

// PreviewKeyHandler is the tunneling half of key routing: an ancestor
// that implements it is offered the event on the way DOWN, root first,
// before the focused component ever sees it. Returning true stops the
// descent and the event goes no further — no target handling, no
// bubbling, no bindings.
//
// This is the parent-veto mechanism. A modal scrim swallows everything
// aimed at the layer underneath, a masked input rewrites what its
// children may receive, and neither has to be consulted by the
// components it is overriding.
type PreviewKeyHandler interface{ PreviewKey(input.KeyEvent) bool }

// Focusable marks a component as a focus stop. Embedding FocusState is the
// easy way to implement it.
type Focusable interface{ AcceptsFocus() bool }

// FocusTarget is how the framework tells a component it gained or lost
// focus. Implementations must make the flag observable to Render.
type FocusTarget interface{ SetFocused(bool) }

// FocusState is the mixin that makes a component focusable. It keeps the
// framework-set flag in a source property, so a Render that reads
// IsFocused() picks up focus changes as damage — exactly the component
// losing focus and the one gaining it repaint.
type FocusState struct{ focused *prop.Property[bool] }

func (f *FocusState) AcceptsFocus() bool { return true }
func (f *FocusState) SetFocused(v bool)  { f.state().Set(v) }
func (f *FocusState) IsFocused() bool    { return f.state().Get() }

func (f *FocusState) state() *prop.Property[bool] {
	if f.focused == nil {
		f.focused = prop.NewSource(false)
	}
	return f.focused
}

// FocusHost is implemented by a component that moves focus among its own
// children — a toolbar whose arrows walk along it, a menu, a grid that
// wraps at a row end. The FocusManager hands itself to every FocusHost
// it walks past, on the first walk and on every Resync, so a host is
// always holding the manager for the tree it is actually in.
//
// It is a narrow, opt-in seam rather than a general "reach the
// framework" hook: nothing is handed to a component that did not ask,
// and the only thing a host can usefully do with it — SetFocus — checks
// that its argument is a live focus stop, so a stale pointer from a
// replaced tree fails safely instead of focusing something off screen.
//
// A focus host is NOT a focus trap. Tab still walks straight through it
// in tree order; the host only sees the keys its children declined, and
// declining an arrow itself hands the key back to the ordinary spatial
// navigation, which is how up and down leave a horizontal bar.
type FocusHost interface{ SetFocusManager(*FocusManager) }

// MnemonicHandler is the opt-in seam for PAGE-scoped accelerators — a
// menu bar's alt+letter. Key routing normally never leaves the focused
// component's ancestor chain, and a menu bar is a sibling of the content
// it overlays, so without this seam alt+f could only open a menu while
// the bar itself held focus.
//
// The dispatcher collects implementers on the same walk that finds focus
// stops and offers them the keys NOTHING else consumed: every
// PreviewKey, KeyBinding and HandleKey in the focused chain outranks a
// mnemonic — a KeyBinding on an alt gesture keeps winning, per dispatch
// order — and a mnemonic outranks the framework's own tab/arrow
// fallbacks. Implementers are offered every unconsumed key and must
// return false for the ones they do not own.
type MnemonicHandler interface{ HandleMnemonic(input.KeyEvent) bool }

// NonVisual marks elements that live in the tree for behavior only.
// The framework attaches them to their parent component instead of laying
// them out or painting them (see Base.Attach).
type NonVisual interface{ NonVisual() bool }

// Hosted is implemented by non-visual attachments that need to know the
// component they hang off — the input-tree walk tells them, on the first
// walk and on every Resync. A Tooltip uses it to anchor its popup to the
// component it decorates and to read that component's declared
// KeyBindings for its gesture hint.
type Hosted interface{ SetHost(Component) }

// HoverWatcher is the attachment seam behind hover-triggered transient
// UI (Tooltip is the first customer). An attachment implementing it is
// told when the pointer enters or leaves its HOST's subtree — the
// component it is attached to, everything under it included — and when
// any key or button press arrives, the conventional dismissals for a
// transient surface.
//
// Like hover itself, the pointer notification is exclusive: when
// watching hosts nest, only the innermost host containing the pointer
// counts as entered, so a tooltip on a button inside a tooltipped panel
// shows the button's tip, not both.
type HoverWatcher interface {
	// PointerOver reports the pointer entering (true) or leaving (false)
	// the host component's subtree. Called on the UI goroutine, from
	// event dispatch — property access is legal.
	PointerOver(over bool)
	// Interrupted fires on every key dispatch and every pointer press.
	// It is a notification, not a handler: the event still routes
	// normally, which is exactly what a tooltip wants — the keystroke
	// both dismisses the tip and does its job.
	Interrupted()
}

// hoverWatch pairs a watcher with the host it was attached to, plus the
// edge-detection state that turns per-event containment into
// enter/leave notifications.
type hoverWatch struct {
	host Component
	w    HoverWatcher
	over bool
}

// KeyBinding is a declared gesture: <KeyBinding Gesture="ctrl+s"
// Command="{{.Save}}"/>. It hangs off its parent component as an
// attachment, and the dispatcher only reaches it while the focused
// component's ancestor chain passes through that parent — so a binding
// declared inside a control fires only while that control has focus,
// and one declared on the page root is global.
// A binding whose Command is conditional (see Cmd.When) matches only
// while the condition holds: a disabled gesture is not consumed, so the
// key carries on bubbling and an outer binding or the app's own fallback
// can still have it.
type KeyBinding struct {
	Base
	Gesture input.KeyEvent
	Command Action
}

func (k *KeyBinding) Measure(Size) Size { return Size{} }
func (k *KeyBinding) Render(*Frame)     {}
func (k *KeyBinding) NonVisual() bool   { return true }

// FocusManager is the input tree: focus order, parent links, and the
// KeyBindings attached along the way. It is built by the same walk the
// Composer does and owned by it (Composer.Focus).
type FocusManager struct {
	root      Component
	order     []Component
	parent    map[Component]Component
	bindings  map[Component][]*KeyBinding
	watchers  []*hoverWatch
	mnemonics []Component // MnemonicHandlers in tree order
	cur       int
	prev      Component // what held focus before the last real move

	hover Component // current hover target, nil when the pointer is nowhere

	// Pointer capture. captor owns every pointer event while it is set;
	// held is true when it was taken by CaptureMouse rather than by a
	// press, which is what decides whether a release gives it back.
	captor Component
	held   bool

	// Click counting, keyed to the component a click was synthesized on.
	lastClick   Component
	lastClickAt time.Time
	clicks      int

	// DoubleClickInterval is how close two clicks on the same component
	// must be to count as a double click. Now is the clock, a field so
	// tests can drive click counting without sleeping.
	DoubleClickInterval time.Duration
	Now                 func() time.Time
}

// DefaultDoubleClickInterval is the conventional 400ms.
const DefaultDoubleClickInterval = 400 * time.Millisecond

// NewFocusManager walks root and focuses the first focus stop, so a page
// always has somewhere for keys to land.
func NewFocusManager(root Component) *FocusManager {
	m := &FocusManager{
		root:                root,
		parent:              map[Component]Component{},
		bindings:            map[Component][]*KeyBinding{},
		cur:                 -1,
		DoubleClickInterval: DefaultDoubleClickInterval,
		Now:                 time.Now,
	}
	m.walk(root, nil, false)
	for _, w := range m.order {
		if t, ok := w.(FocusTarget); ok {
			t.SetFocused(false) // components outlive tree rebuilds
		}
	}
	if len(m.order) > 0 {
		m.SetFocus(m.order[0])
	}
	return m
}

// Resync rebuilds the input tree after a structural change (see
// dynamic.go), keeping focus where it was. It is the FocusManager's half
// of what the Composer does to paint nodes: a list that realized three
// rows must be able to route a click to them, and the rows' ancestor
// chain is what mouse and key bubbling walk.
//
// Focus, hover and the capture target survive if they are still in the
// tree. A focused component that vanished hands focus to the first stop
// — a composition always has somewhere for keys to land, the same
// invariant NewFocusManager establishes — and a captor that vanished
// mid-drag drops the capture rather than routing to a component nobody
// can see.
func (m *FocusManager) Resync() {
	focused, hover, captor := m.Focused(), m.hover, m.captor
	// Watcher enter/leave state survives the rebuild: a re-sync in the
	// middle of a hover (showing a tooltip IS a re-sync) must not read
	// as the pointer leaving and re-entering, or every structural change
	// would restart every delay timer on the page.
	wasOver := make(map[HoverWatcher]bool, len(m.watchers))
	for _, hw := range m.watchers {
		wasOver[hw.w] = hw.over
	}
	m.order = m.order[:0]
	m.parent = map[Component]Component{}
	m.bindings = map[Component][]*KeyBinding{}
	m.watchers = m.watchers[:0]
	m.mnemonics = m.mnemonics[:0]
	m.walk(m.root, nil, false)
	for _, hw := range m.watchers {
		hw.over = wasOver[hw.w]
	}

	m.cur = -1
	for i, w := range m.order {
		if w == focused {
			m.cur = i
			break
		}
	}
	if m.cur < 0 {
		if t, ok := focused.(FocusTarget); ok {
			t.SetFocused(false)
		}
		if len(m.order) > 0 {
			m.focusIndex(0)
		}
	}
	if _, live := m.parent[hover]; !live {
		if h, ok := hover.(HoverTarget); ok {
			h.SetHovered(false)
		}
		m.hover = nil
	}
	if _, live := m.parent[captor]; !live {
		m.captor, m.held = nil, false
	}
	if _, live := m.parent[m.prev]; !live {
		m.prev = nil
	}
	if _, live := m.parent[m.lastClick]; !live {
		m.lastClick, m.clicks = nil, 0
	}
}

// walk builds the input tree. frozen is true inside a Frozen subtree, and
// what it turns off is exactly the registrations that make a component
// TARGETABLE — focus stops, scoped KeyBindings, mnemonics, hover
// watchers. Structure is still recorded: m.parent feeds capture and hover
// liveness, depth/ancestor, and the retarget below, and FocusHost wiring
// is a seam a component needs whether or not anything can reach it.
//
// The frozen component itself is not frozen; its subtree is. So w's own
// registrations use the INCOMING flag, and children get frozen || w.
func (m *FocusManager) walk(w, parent Component, frozen bool) {
	m.parent[w] = parent
	if f, ok := w.(Focusable); ok && f.AcceptsFocus() && !frozen {
		m.order = append(m.order, w)
	}
	if h, ok := w.(FocusHost); ok {
		h.SetFocusManager(m)
	}
	if _, ok := w.(MnemonicHandler); ok && !frozen {
		m.mnemonics = append(m.mnemonics, w)
	}
	if a, ok := w.(Attacher); ok {
		for _, at := range a.Attachments() {
			m.parent[at] = w
			if kb, ok := at.(*KeyBinding); ok && !frozen {
				// A scoped KeyBinding is a SECOND route to a component,
				// independent of HandleKey — Dispatch interleaves each
				// level's bindings with that level's handler.
				//
				// Skipping it here is DEFENCE IN DEPTH rather than the
				// guarantee, and the distinction is worth keeping honest:
				// a binding only fires while the focused chain passes
				// through its host, and nothing inside a frozen subtree
				// can be focused, so this registration was already
				// unreachable. TestFocusCannotBeSetIntoAFrozenSubtree is
				// the test that actually holds the door.
				m.bindings[w] = append(m.bindings[w], kb)
			}
			// Attachments get the same seams components do: an
			// attachment that needs the input tree (a Tooltip reaching
			// the adornment layer) asks the way a FocusHost does, and
			// one that needs its host is told — attachments have no
			// parent pointer, and the walk is the one place that knows.
			if h, ok := at.(Hosted); ok {
				h.SetHost(w)
			}
			if fh, ok := at.(FocusHost); ok {
				fh.SetFocusManager(m)
			}
			if hw, ok := at.(HoverWatcher); ok && !frozen {
				m.watchers = append(m.watchers, &hoverWatch{host: w, w: hw})
			}
		}
	}
	if c, ok := w.(Container); ok {
		kidsFrozen := frozen || isFrozen(w)
		for _, ch := range c.ChildComponents() {
			m.walk(ch, w, kidsFrozen)
		}
	}
}

// frozenHostFor is the component that owns events aimed at w: the
// OUTERMOST Frozen ancestor at or above it, or w itself when there is
// none.
//
// Outermost rather than nearest, because a frozen host nested inside
// another frozen subtree cannot act either — handing it the event would
// be routing to something that is itself supposed to be a picture.
func (m *FocusManager) frozenHostFor(w Component) Component {
	target := w
	for n := w; n != nil; n = m.parent[n] {
		if isFrozen(n) && n != w {
			target = n
		}
	}
	return target
}

// depth is how many ancestors w has; ancestor walks that many links back
// up. Together they enumerate the root→w chain without building one, and
// the tunnel phases want exactly that: allocating a path on every motion
// event would be the one allocation in the routing hot loop, and a
// shared scratch slice would be clobbered by a handler that dispatches
// again from inside a Preview. Trees are a dozen levels deep, so the
// quadratic walk is cheaper than either.
func (m *FocusManager) depth(w Component) int {
	d := 0
	for n := m.parent[w]; n != nil; n = m.parent[n] {
		d++
	}
	return d
}

// ancestor returns the component up links above w — ancestor(w, 0) is w
// itself and ancestor(w, depth(w)) is the root.
func (m *FocusManager) ancestor(w Component, up int) Component {
	for ; up > 0 && w != nil; up-- {
		w = m.parent[w]
	}
	return w
}

// within reports whether w is root or lives under it. Capture uses it to
// ask whether the pointer is still inside the component that captured it.
func (m *FocusManager) within(root, w Component) bool {
	if root == nil {
		return false
	}
	for n := w; n != nil; n = m.parent[n] {
		if n == root {
			return true
		}
	}
	return false
}

// Root is the component this input tree was built over. Exposed for the
// pieces of the framework that hang off the tree without being able to
// see it — an AdornmentLayer checking that an anchor is still reachable,
// a Tooltip finding the layer to show itself in.
func (m *FocusManager) Root() Component { return m.root }

// Focused returns the component holding focus, or nil.
func (m *FocusManager) Focused() Component {
	if m.cur < 0 || m.cur >= len(m.order) {
		return nil
	}
	return m.order[m.cur]
}

// Order is the focus traversal order — tree order, filtered to focus
// stops. Exposed for tests and for apps that want to restore focus.
func (m *FocusManager) Order() []Component { return m.order }

// PreviouslyFocused is the component that held focus before the last
// real focus move, or nil when there has been none (or it left the
// tree). It exists for overlays: a menu opened by a click has already
// had focus handed to it by focus-follows-click, so "what should get
// focus back on dismiss" is exactly this — the component the gesture
// took it from.
func (m *FocusManager) PreviouslyFocused() Component {
	if m.prev == nil {
		return nil
	}
	if _, live := m.parent[m.prev]; !live {
		return nil
	}
	return m.prev
}

// SetFocus moves focus to w if it is a focus stop.
func (m *FocusManager) SetFocus(w Component) bool {
	for i, o := range m.order {
		if o == w {
			m.focusIndex(i)
			return true
		}
	}
	return false
}

func (m *FocusManager) focusIndex(i int) {
	if i == m.cur {
		return
	}
	if old := m.Focused(); old != nil {
		m.prev = old
		if t, ok := old.(FocusTarget); ok {
			t.SetFocused(false)
		}
	}
	m.cur = i
	if now, ok := m.Focused().(FocusTarget); ok {
		now.SetFocused(true)
	}
}

// FocusNext and FocusPrev move focus in tree order, wrapping, skipping
// anything currently inside a Collapsed subtree.
func (m *FocusManager) FocusNext() { m.move(1) }
func (m *FocusManager) FocusPrev() { m.move(-1) }

func (m *FocusManager) move(d int) {
	n := len(m.order)
	if n == 0 {
		return
	}
	for i := 1; i <= n; i++ {
		next := ((m.cur+d*i)%n + n) % n
		if m.reachable(m.order[next]) {
			m.focusIndex(next)
			return
		}
	}
}

func (m *FocusManager) reachable(w Component) bool {
	for n := w; n != nil; n = m.parent[n] {
		if l := LayoutOf(n); l != nil && l.Visibility == Collapsed {
			return false
		}
	}
	return true
}

// Dispatch routes a key event in three phases.
//
// It TUNNELS first: every PreviewKeyHandler from the root down to the
// focused component is offered the event, and the first that takes it
// ends the dispatch. Then it BUBBLES: starting at the focused component
// and walking up its ancestors to the root, the KeyBindings attached at
// each level are matched first, then that component's own HandleKey. The
// first true stops propagation. Then the MNEMONICS get their turn: every
// MnemonicHandler in the tree, in tree order, is offered what the
// focused chain declined — accelerators are page-scoped, so they run
// outside the chain, but after it, which is what keeps a KeyBinding on
// the same gesture winning. Finally, if nothing consumed the event,
// tab and shift+tab move focus and an unclaimed arrow navigates — which
// means either can be overridden by binding or handling it.
//
// Bindings stay interleaved with handlers per level rather than running
// as one pass after the bubble: a binding declared inside a control has
// always beaten its own container's HandleKey and lost to a deeper
// component's, and that ordering is what scopes a control's gestures.
func (m *FocusManager) Dispatch(ev input.KeyEvent) bool {
	m.interrupt() // any key dismisses transient UI; the key still routes
	start := m.Focused()
	if start == nil {
		start = m.root
	}
	for d := m.depth(start); d >= 0; d-- {
		if h, ok := m.ancestor(start, d).(PreviewKeyHandler); ok && h.PreviewKey(ev) {
			return true
		}
	}
	for n := start; n != nil; n = m.parent[n] {
		for _, b := range m.bindings[n] {
			if b.Gesture == ev && CanExecute(b.Command) {
				b.Command.Run()
				return true
			}
		}
		if attachedKey(n, ev) {
			return true
		}
		if h, ok := n.(KeyHandler); ok && h.HandleKey(ev) {
			return true
		}
	}
	for _, w := range m.mnemonics {
		if h, ok := w.(MnemonicHandler); ok && m.reachable(w) && h.HandleMnemonic(ev) {
			return true
		}
	}
	switch ev {
	case input.Named(input.KeyTab):
		m.FocusNext()
		return true
	case input.KeyEvent{Key: input.KeyTab, Mods: input.ModShift}:
		m.FocusPrev()
		return true
	}
	if d, ok := arrowDir(ev); ok {
		return m.FocusDir(d)
	}
	return false
}

// attachedKey offers ev to w's attachments that handle keys, and reports
// that one consumed it. It is the seam a BEHAVIOUR attachment needs: a
// KeyBinding maps one gesture to one command, but a type-ahead search
// consumes a whole class of keys and keeps state between them, which no
// number of bindings expresses.
//
// Two things about where it sits in Dispatch are load-bearing.
//
// It runs AFTER the level's KeyBindings, so a gesture the page declared
// out loud still outranks a behaviour that would otherwise absorb it —
// <KeyBinding Gesture="/"> on a list keeps meaning what it says.
//
// It runs BEFORE the host's own HandleKey, and that ordering is the
// whole point: ItemsView already claims j and k for navigation
// (components/itemsview.go), so an attachment offered keys after its
// host could never see a letter the host wanted. Reversing these two
// lines does not fail to compile and does not fail most tests — it
// silently makes a type-ahead unable to search for anything starting
// with j. TestAttachmentKeysPrecedeHost pins it.
//
// The attachment list is read from the host rather than collected in
// walk: a cached copy would be a second statement of what Attachments()
// already says, and the second copy is the one that goes stale across a
// Resync.
func attachedKey(w Component, ev input.KeyEvent) bool {
	a, ok := w.(Attacher)
	if !ok {
		return false
	}
	for _, at := range a.Attachments() {
		if h, ok := at.(KeyHandler); ok && h.HandleKey(ev) {
			return true
		}
	}
	return false
}

// Direction is an arrow's meaning for focus navigation.
type Direction uint8

const (
	DirUp Direction = iota
	DirDown
	DirLeft
	DirRight
)

func arrowDir(ev input.KeyEvent) (Direction, bool) {
	switch ev {
	case input.Named(input.KeyUp):
		return DirUp, true
	case input.Named(input.KeyDown):
		return DirDown, true
	case input.Named(input.KeyLeft):
		return DirLeft, true
	case input.Named(input.KeyRight):
		return DirRight, true
	}
	return 0, false
}

// FocusDir moves focus spatially — the nearest focus stop whose center
// lies in the given direction from the focused component, preferring ones
// roughly in line with it (XAML's XYFocus). It falls back to tree order
// when nothing lies that way, so a direction is never a dead end.
//
// Arrows reach this only when nothing in the tree consumed them, which
// is what lets a list pane keep its own arrow handling.
func (m *FocusManager) FocusDir(d Direction) bool {
	if len(m.order) == 0 {
		return false
	}
	cur, ok := focusBounds(m.Focused())
	if !ok {
		m.moveInDir(d)
		return true
	}
	cx, cy := cur.X+cur.W/2, cur.Y+cur.H/2
	best, bestScore := -1, 0
	for i, w := range m.order {
		if w == m.Focused() || !m.reachable(w) {
			continue
		}
		b, ok := focusBounds(w)
		if !ok {
			continue
		}
		x, y := b.X+b.W/2, b.Y+b.H/2
		var along, across int
		switch d {
		case DirRight:
			along, across = x-cx, abs(y-cy)
		case DirLeft:
			along, across = cx-x, abs(y-cy)
		case DirDown:
			along, across = y-cy, abs(x-cx)
		case DirUp:
			along, across = cy-y, abs(x-cx)
		}
		if along <= 0 {
			continue // not in this direction
		}
		score := along + 2*across // drifting off-axis costs double
		if best < 0 || score < bestScore {
			best, bestScore = i, score
		}
	}
	if best >= 0 {
		m.focusIndex(best)
		return true
	}
	m.moveInDir(d)
	return true
}

func (m *FocusManager) moveInDir(d Direction) {
	if d == DirRight || d == DirDown {
		m.FocusNext()
		return
	}
	m.FocusPrev()
}

func focusBounds(w Component) (Rect, bool) {
	b, ok := w.(Bounded)
	if !ok {
		return Rect{}, false
	}
	r := b.Bounds()
	return r, r.W > 0 && r.H > 0
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
