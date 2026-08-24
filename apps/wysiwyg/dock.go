package main

// The dock: what makes this an IDE shell rather than a fixed four-pane
// picture.
//
// # Why the <Grid> could not stay
//
// The shell used to be `<Grid Rows="1*,10,1" Cols="4,38,1*,46">` with one
// pane hardcoded into each cell. Every dock operation a user expects —
// move this pane to the other side, collapse it, get it out of the way —
// is a change to that attribute string, and an attribute string is not
// state. Grid track lists are plain `[]GridLen` fields read during
// Arrange, so nothing observes them and nothing can move a pane at
// runtime without rewriting the markup and rebuilding the page.
//
// So the dock is a container that owns the arrangement, and the
// arrangement is a MODEL. `<DockHost>` declares the panes; every dock
// gesture mutates the model; the model is read while painting, which is
// what schedules the frame that re-lays it out.
//
// # Four states per pane, and they are NOT four names for one thing
//
// This is the part that took the longest to get right, because "hidden",
// "collapsed" and "unpinned" all sound like "not showing".
//
//   - HIDDEN — the pane is not showing AND KEEPS ITS SLOT SPACE. This is
//     the user's explicit rule, and it maps exactly onto gooey.Hidden,
//     which the framework defines as "occupies space, does not paint".
//     The subtree stays alive: a TextBox in a hidden pane keeps its
//     caret, a Startable in one keeps running, and revealing it shows the
//     pane exactly as it was. That is the whole reason to prefer it over
//     Collapsed, which would drop the pane out of layout and take the
//     column width with it.
//
//   - COLLAPSED — the pane shows its HEADER ROW and nothing else, and its
//     extent along the slot's axis shrinks to that one row so its
//     neighbours get the space. This is the operation that reclaims room.
//
//   - UNPINNED — nothing on its own. Pin is a claim about what survives
//     `HideUnpinned` (View → Hide unpinned, the "get everything out of my
//     way" gesture): pinned panes stay, unpinned panes hide. Making pin
//     mean "hide me right now" would have made it a second spelling of
//     hidden, which is the collapse-of-two-meanings-into-one-cue this
//     repo keeps cataloguing.
//
//   - SLOT + ORDER — where the pane docks and where it sits among its
//     slot-mates. This is drag-to-position, and it is a model edit, so
//     the keyboard reaches it exactly as the mouse does.
//
// # The one sharp edge: gooey.Hidden on a CONTAINER hides only its chrome
//
// composer.go's build gives a hidden container the `covered` treatment —
// it fills its own bounds and the z-ordered pass repaints its subtree
// ABOVE it. That is correct for an overlay and wrong for a pane: a
// hidden pane whose children are still Visible paints its children over
// its own erasure, so the pane "vanishes" and its contents stay on
// screen.
//
// So hiding a pane is TWO facts, not one, and the pane owns both: its own
// Visibility goes Hidden (the framework's erase-and-restore sweep runs,
// and the slot space is kept), and its CONTENT goes Collapsed (out of
// layout, out of paint, but not out of existence — a Collapsed component
// keeps its state, it just measures zero).
//
// TestHidingAPaneKeepsItsWidthAndBlanksItsContent is the pin for that
// pair; TestHideIsNotCollapse is the pin for the difference from the
// operation next door.

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// dockSlot is where a pane docks. Center is the editor area and is the
// only slot with no cross-axis size of its own: it takes what the edge
// slots leave, which is what makes it the thing the others crowd.
type dockSlot int

const (
	dockLeft dockSlot = iota
	dockCenter
	dockRight
	dockBottom
)

// slotNames is the markup spelling, and the ONE table. parseSlot reads it
// and slotName writes it, so a slot cannot be spelled one way in a
// Slot="..." attribute and another way in the status line.
var slotNames = map[dockSlot]string{
	dockLeft:   "Left",
	dockCenter: "Center",
	dockRight:  "Right",
	dockBottom: "Bottom",
}

func slotName(s dockSlot) string { return slotNames[s] }

func parseSlot(s string) (dockSlot, error) {
	for k, v := range slotNames {
		if strings.EqualFold(v, s) {
			return k, nil
		}
	}
	return 0, fmt.Errorf("unknown Slot %q: want Left, Center, Right or Bottom", s)
}

// headerH is the pane header strip: one row, and it is CELLS rather than
// the pixel line art <Panel> uses. That is a testability decision with a
// cost. Pixel chrome is invisible to screen_text and to the pty harness,
// which read the cell plane — so a pin marker drawn in pixels could not
// be asserted by the only verification route this feature has. A pane's
// state has to be readable in the transcript that proves it.
const headerH = 1

// dockPane is one dockable pane: a header strip it paints itself, and one
// content component it does not.
type dockPane struct {
	gooey.Base

	ID    string
	Title string
	// Content is every view this pane can show, OVERLAID in the body
	// rect. Usually one; the editor pane holds two — the designer and
	// the code view — with opposite Visibility bindings, so exactly one
	// is Visible and the other Collapsed.
	//
	// Overlaying here rather than wrapping them in a <Grid Rows="1*"
	// Cols="1*"> is a DAMAGE decision measured, not assumed. A drag
	// motion's cost is the length of the ANCESTOR CHAIN above the moved
	// element — Composer.restoreUnder force-repaints everything beneath
	// the vacated rect — so every wrapper between the dock and the
	// design surface is one more component repainted on every pointer
	// report, forever. The wrapper Grid cost exactly one, which was the
	// difference between staying inside drag_test.go's ceiling of 8 and
	// needing it raised.
	Content []gooey.Component

	// The model. These are SOURCE PROPERTIES and not plain fields,
	// because the header reads them while painting: that read is the
	// damage declaration, so toggling pin repaints the one header that
	// shows it and nothing else.
	slot      *prop.Property[int]
	order     *prop.Property[int]
	size      *prop.Property[int]
	pinned    *prop.Property[bool]
	collapsed *prop.Property[bool]
	hidden    *prop.Property[bool]

	host   *dockHost
	attach []gooey.Component
}

func newDockPane(id, title string, slot dockSlot, size int, pinned bool) *dockPane {
	p := &dockPane{
		ID:        id,
		Title:     title,
		slot:      prop.NewSource(int(slot)),
		order:     prop.NewSource(0),
		size:      prop.NewSource(size),
		pinned:    prop.NewSource(pinned),
		collapsed: prop.NewSource(false),
		hidden:    prop.NewSource(false),
	}
	// The pane's own visibility IS the hidden bit, bound rather than
	// written: a Set on hidden schedules the frame through the Composer's
	// visibility observer, and Frame's sweep does the erase and the
	// restore-underneath. Writing the field by hand would schedule
	// nothing (Layout is outside the property graph) and the old pixels
	// would sit there until something else happened to repaint.
	p.LayoutProps().BindVisibilityFunc(func() gooey.Visibility {
		if p.hidden.Get() {
			return gooey.Hidden
		}
		return gooey.Visible
	})
	return p
}

func (p *dockPane) Attachments() []gooey.Component { return p.attach }

func (p *dockPane) ChildComponents() []gooey.Component { return p.Content }

// bodyHidden is the second half of the hide pair described in the file
// comment: the content is Collapsed whenever the pane is not showing its
// body, whether that is because the pane is hidden or because it is
// collapsed to its header.
//
// Both Gets are HOISTED above the `||`, because a dependency is recorded
// by the Get that actually RUNS: on the short-circuit side of `||` the
// second read does not happen, and the pane would go deaf to that
// property on exactly the frames where the first one was true.
func (p *dockPane) bodyHidden() bool {
	h, c := p.hidden.Get(), p.collapsed.Get()
	return h || c
}

// bindBody makes the pane's hidden/collapsed state part of the child's
// Visibility, COMPOSED with whatever the child already had rather than
// stamped over it.
//
// The first version of this stamped the field in Measure, and it was
// wrong in a way only a round trip catches: forcing Collapsed on the way
// down is easy, and there is then nothing to restore it, because the
// pane never knew what the value had been. The pane hid correctly and
// could never be revealed. TestHidingAPaneBlanksItsContentAndKeepsItsSubtree
// is the pin — its "revealing the pane left it blank" arm is the one that
// failed.
//
// Composing is also what keeps the region swap working. The editor pane's
// two panels carry their OWN Visibility bindings (the design/code
// computeds), and a pane that overwrote them would make the swap
// permanent in whichever direction it last stamped. `prev` is the child's
// own source, taken through Layout.VisibilitySource — which exists for
// exactly this: carrying a binding that is already there.
//
// A child with no binding at all contributes its LITERAL Visibility,
// captured once here, so <Panel Visibility="Hidden"> inside a dock pane
// still means what it says.
//
// The returned closure is called from two places with opposite meanings,
// which is the call-site rule doing its job: the Composer's visibility
// observer EVALUATES it (so p.hidden becomes a subscription and a Set
// schedules a frame), and MeasureChild calls it plain (so layout records
// nothing).
func (p *dockPane) bindBody(c gooey.Component) {
	l := gooey.LayoutOf(c)
	if l == nil {
		return
	}
	prev, lit := l.VisibilitySource(), l.Visibility
	l.BindVisibilityFunc(func() gooey.Visibility {
		// The pane's own state first, and BOTH reads happen before the
		// branch: a dependency is recorded by the Get that actually
		// runs, so a read behind an early return drops out of the set on
		// the frames that take it.
		if p.bodyHidden() {
			return gooey.Collapsed
		}
		if prev != nil {
			return prev()
		}
		return lit
	})
}

// extent is how much of its slot's axis this pane asks for, in the
// slot's units. A collapsed pane wants its header and nothing more; every
// other pane — INCLUDING A HIDDEN ONE — wants a full share. That is the
// "keeps its size" half of the hide rule, and it is why hiding a pane
// leaves a gap rather than reflowing its neighbours.
func (p *dockPane) collapsedNow() bool { return p.collapsed.Get() }

func (p *dockPane) Measure(avail gooey.Size) gooey.Size {
	body := gooey.Size{W: avail.W, H: max(0, avail.H-headerH)}
	for _, c := range p.Content {
		gooey.MeasureChild(c, body)
	}
	return avail
}

func (p *dockPane) Arrange(b gooey.Rect) {
	p.Base.Arrange(b)
	body := gooey.Rect{
		X: b.X, Y: b.Y + headerH,
		W: b.W, H: max(0, b.H-headerH),
	}
	for _, c := range p.Content {
		gooey.ArrangeChild(c, body)
	}
}

// Render paints the header strip only — the content is its own paint
// node. Every model read this makes is a subscription, which is the
// whole damage contract for the dock: toggling pin, collapsing, or
// moving the active pane repaints headers, not panes.
func (p *dockPane) Render(f *gooey.Frame) {
	b := p.Bounds()
	if b.W <= 0 || b.H <= 0 {
		return
	}
	st := p.host.headerStyle()
	if p.host.isActive(p) {
		st.Reverse = true
	}
	// The chevron is the collapse state and the collapse HIT TARGET: a
	// pane says whether it has a body, in the one row that is always
	// there to say it in.
	chev := "v"
	if p.collapsed.Get() {
		chev = ">"
	}
	pin := " "
	if p.pinned.Get() {
		pin = "*"
	}
	line := chev + " " + p.Title
	if n := b.W - len([]rune(line)) - 1; n > 0 {
		line += strings.Repeat(" ", n)
	}
	line += pin
	f.Cells.SetString(b.X, b.Y, clipTo(line, b.W), st)
}

// HandleMouse starts a drag from the header, and toggles collapse from
// the chevron. The press is forwarded to the HOST because the host is
// what takes the pointer capture: a drag that must be tracked across
// other panes cannot be routed by hit-testing, which is the same reason
// MenuBar's dropdown captures.
func (p *dockPane) HandleMouse(ev input.MouseEvent) bool {
	b := p.Bounds()
	if ev.Kind != input.MousePress || ev.Y != b.Y {
		return false
	}
	if ev.X == b.X {
		p.host.dock.ToggleCollapsed(p)
		return true
	}
	p.host.beginDrag(p)
	return true
}

func clipTo(s string, w int) string {
	r := []rune(s)
	if len(r) <= w {
		return s
	}
	return string(r[:max(0, w)])
}

// dockHost is the shell's client area: it owns the slot geometry and the
// drag in flight, and it paints the splitters between slots.
//
// It is NOT a Grid and deliberately does not become one. A Grid resolves
// tracks from a declared list; this resolves them from the model, which
// is the entire difference between a layout you can look at and a layout
// you can rearrange.
type dockHost struct {
	gooey.Base

	dock *dockModel

	mgr  *gooey.FocusManager
	drag *dockPane
	// dropSlot is what the pointer is over mid-drag, and it is what the
	// header line shows so a drag says where it will land BEFORE it
	// lands. Zero-valued when no drag is in flight, which is why `drag`
	// and not this is the "is a drag happening" test.
	dropSlot dockSlot

	style *prop.Property[render.Style]
}

func (h *dockHost) SetFocusManager(fm *gooey.FocusManager) { h.mgr = fm }

func (h *dockHost) ChildComponents() []gooey.Component {
	out := make([]gooey.Component, 0, len(h.dock.panes))
	for _, p := range h.dock.panes {
		out = append(out, p)
	}
	return out
}

func (h *dockHost) headerStyle() render.Style {
	if h.style == nil {
		return render.Style{}
	}
	return h.style.Get()
}

func (h *dockHost) isActive(p *dockPane) bool { return h.dock.Active() == p }

// slotPanes and slotExtent live on the MODEL, not on the host, and the
// host delegates. The fit check needs both to work out the shell's
// minimum size, and it has no business reaching through a component to
// ask a question that is purely about declared state.
func (h *dockHost) slotPanes(s dockSlot) []*dockPane { return h.dock.slotPanes(s) }
func (h *dockHost) slotExtent(s dockSlot) int        { return h.dock.slotExtent(s) }

// slotPanes is every pane docked in s, in order. Hidden panes are
// INCLUDED — they keep their space, so they keep their place in the
// stack — and the sort is by the order property so a reorder is a model
// edit like every other dock gesture.
func (d *dockModel) slotPanes(s dockSlot) []*dockPane {
	var out []*dockPane
	for _, p := range d.panes {
		if dockSlot(p.slot.Get()) == s {
			out = append(out, p)
		}
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].order.Get() < out[j-1].order.Get(); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// slotExtent is the cross-axis size of a slot: the largest size any of
// its panes asks for, and zero for an empty slot — which is what makes a
// slot that everything was dragged out of disappear rather than leave a
// blank stripe.
func (d *dockModel) slotExtent(s dockSlot) int {
	e := 0
	for _, p := range d.slotPanes(s) {
		if v := p.size.Get(); v > e {
			e = v
		}
	}
	return e
}

// Minimum is the smallest terminal the DOCK needs, in cells, derived
// entirely from what the panes DECLARE.
//
// # Why the fit check had to grow this, and what it replaces
//
// The old shell put every pane in a fixed Grid track, so the track list
// WAS the minimum: `Cols="4,38,1*,46"` said the side bar is 38 and the
// properties pane 46, and reading `Rows=`/`Cols=` off the shipped markup
// gave a number that could not drift from the layout, because it was the
// layout.
//
// A dock breaks that, and it is worth being exact about how. The shell's
// grid is now `Rows="1,1*,1" Cols="4,1*"` — a menu row, a status row, an
// activity rail, and ONE star track holding everything else. Its fixed
// tracks sum to 2 rows and 4 columns, which is a true statement about the
// grid and a useless one about the editor: it would report that the
// editor fits in a 4x2 terminal.
//
// So a minimum is no longer derivable from the grid's tracks ALONE. What
// replaces it is not a hardcoded number — that would be the second copy
// of a fact the markup states, which is the failure this whole mechanism
// exists to avoid. It is a SECOND SET OF DECLARED TRACKS: `Slot=` and
// `Size=` on each `<DockPane>` are as declared, as authoritative, and as
// impossible to drift from the layout as `Cols=` ever was, because the
// same numbers drive the arrangement. The composition is
//
//	usable = the grid's FIXED tracks + the dock's own minimum
//
// and the star track contributes the dock's requirement instead of the
// generic starMin allowance it would get if it held anything else.
//
// # The units
//
// COLUMNS: the left and right slots take their declared cross-axis
// extents; the centre gets starMin, because the centre is what everything
// else crowds and a designer with nothing in it is not a shell. The
// bottom strip spans the full width and stacks its panes horizontally, so
// it needs starMin per pane.
//
// ROWS: the bottom strip's declared extent, plus the tallest of the three
// upper slots. A slot stacking n panes needs n*(headerH+starMin): the
// header row each pane always draws, plus the same "enough for something
// bordered" allowance the rest of this file spends on a star track.
// Reusing starMin rather than inventing a second constant is deliberate —
// there is one judgement here about how small is too small, and it should
// have one name.
//
// # What this does NOT claim
//
// It is a USABLE minimum only. There is no dock equivalent of the hard
// minimum, and that is a real difference rather than an omission: the
// hard minimum names the size below which the shell is arranged OFF
// SCREEN, which happens because Grid.offsets accumulates fixed tracks
// unclamped. dockHost.place cannot do that — it splits whatever extent it
// is handed and its children's rects always sum to it — so a dock that is
// too small produces zero-height panes, not off-screen ones. The hard
// minimum therefore still comes from the grid alone, and still means
// exactly what it always meant.
func (d *dockModel) Minimum() fitSize {
	if len(d.panes) == 0 {
		return fitSize{}
	}
	cols := d.slotExtent(dockLeft) + d.slotExtent(dockRight) + starMin
	if n := len(d.slotPanes(dockBottom)); n > 0 {
		if w := n * starMin; w > cols {
			cols = w
		}
	}

	upper := 0
	for _, s := range []dockSlot{dockLeft, dockCenter, dockRight} {
		if n := len(d.slotPanes(s)); n*(headerH+starMin) > upper {
			upper = n * (headerH + starMin)
		}
	}
	rows := upper
	if n := len(d.slotPanes(dockBottom)); n > 0 {
		bottom := d.slotExtent(dockBottom)
		if min := n * (headerH + starMin); bottom < min {
			bottom = min
		}
		rows += bottom
	}
	return fitSize{Cols: cols, Rows: rows}
}

func (h *dockHost) Measure(avail gooey.Size) gooey.Size {
	h.layout(gooey.Rect{W: avail.W, H: avail.H}, false)
	return avail
}

func (h *dockHost) Arrange(b gooey.Rect) {
	h.Base.Arrange(b)
	h.layout(b, true)
}

// layout resolves the four slots and walks the panes. It runs for both
// passes off one function so Measure and Arrange cannot disagree about
// where a pane is — the failure mode where a pane measures against one
// width and paints at another.
//
// Reads here are PLAIN READS. Layout runs outside any evaluation context
// (Composer.Frame arranges before it paints and outside any computed),
// so none of this subscribes to anything; the subscriptions live in the
// headers' Render.
func (h *dockHost) layout(b gooey.Rect, arrange bool) {
	left := h.slotExtent(dockLeft)
	right := h.slotExtent(dockRight)
	bottom := h.slotExtent(dockBottom)

	// The edge slots are clamped so the centre never goes negative: a
	// dock whose panes together want more than the terminal has must
	// still put the editor somewhere.
	if left+right > b.W {
		left = min(left, b.W)
		right = max(0, b.W-left)
	}
	bottom = min(bottom, b.H)

	top := b.H - bottom
	h.place(dockLeft, gooey.Rect{X: b.X, Y: b.Y, W: left, H: top}, true, arrange)
	h.place(dockRight, gooey.Rect{X: b.X + b.W - right, Y: b.Y, W: right, H: top}, true, arrange)
	h.place(dockCenter, gooey.Rect{
		X: b.X + left, Y: b.Y,
		W: max(0, b.W-left-right), H: top,
	}, true, arrange)
	h.place(dockBottom, gooey.Rect{X: b.X, Y: b.Y + top, W: b.W, H: bottom}, false, arrange)
}

// place lays a slot's panes out along its axis. vertical says which axis
// stacks: left, right and centre stack top-to-bottom, the bottom strip
// stacks left-to-right, which is how a panel of tabs reads.
//
// The share rule: collapsed panes take their header row and no more,
// everything left over is split evenly between the rest — hidden panes
// INCLUDED, because a hidden pane keeps its size.
func (h *dockHost) place(s dockSlot, r gooey.Rect, vertical, arrange bool) {
	panes := h.slotPanes(s)
	if len(panes) == 0 {
		return
	}
	total := r.H
	if !vertical {
		total = r.W
	}
	fixed, flex := 0, 0
	for _, p := range panes {
		if p.collapsedNow() {
			fixed += headerH
		} else {
			flex++
		}
	}
	each, extra := 0, 0
	if flex > 0 {
		avail := max(0, total-fixed)
		each = avail / flex
		extra = avail % flex
	}
	at := r.Y
	if !vertical {
		at = r.X
	}
	for _, p := range panes {
		n := headerH
		if !p.collapsedNow() {
			n = each
			if extra > 0 {
				n++
				extra--
			}
		}
		var slot gooey.Rect
		if vertical {
			slot = gooey.Rect{X: r.X, Y: at, W: r.W, H: n}
		} else {
			slot = gooey.Rect{X: at, Y: r.Y, W: n, H: r.H}
		}
		if arrange {
			gooey.ArrangeChild(p, slot)
		} else {
			gooey.MeasureChild(p, gooey.Size{W: slot.W, H: slot.H})
		}
		at += n
	}
}

// Render paints the host's own chrome — the drag indicator, and nothing
// else. The rev read is what makes a model edit schedule a frame: layout
// itself is outside the property graph, so without a subscription here a
// dock move would change where panes belong and nothing would ask for the
// frame that puts them there.
func (h *dockHost) Render(f *gooey.Frame) {
	h.dock.rev.Get()
	b := h.Bounds()
	if h.drag == nil || b.W <= 0 || b.H <= 0 {
		return
	}
	st := h.headerStyle()
	st.Reverse = true
	msg := clipTo(" move "+h.drag.Title+" → "+slotName(h.dropSlot)+" ", b.W)
	f.Cells.SetString(b.X, b.Y+b.H-1, msg, st)
}

// beginDrag takes the pointer so the gesture can be tracked over panes
// that are not the one being dragged — hit-testing cannot do it, which is
// the same reason MenuBar's dropdown captures.
func (h *dockHost) beginDrag(p *dockPane) {
	h.drag, h.dropSlot = p, dockSlot(p.slot.Get())
	h.dock.SetActive(p)
	if h.mgr != nil {
		h.mgr.CaptureMouse(h)
	}
	h.dock.touch()
}

// slotAt maps a point to the slot that would receive a drop. It is the
// GEOMETRY and not the model: dropping is about where the pointer is, so
// the answer comes from the host's bounds and the resolved extents.
func (h *dockHost) slotAt(x, y int) dockSlot {
	b := h.Bounds()
	left := h.slotExtent(dockLeft)
	right := h.slotExtent(dockRight)
	bottom := h.slotExtent(dockBottom)
	if bottom > 0 && y >= b.Y+b.H-bottom {
		return dockBottom
	}
	if left > 0 && x < b.X+left {
		return dockLeft
	}
	if right > 0 && x >= b.X+b.W-right {
		return dockRight
	}
	return dockCenter
}

func (h *dockHost) HandleMouseMove(ev input.MouseEvent) bool {
	if h.drag == nil {
		return false
	}
	if s := h.slotAt(ev.X, ev.Y); s != h.dropSlot {
		h.dropSlot = s
		h.dock.touch()
	}
	return true
}

func (h *dockHost) HandleMouse(ev input.MouseEvent) bool {
	if h.drag == nil {
		return false
	}
	switch ev.Kind {
	case input.MouseMove:
		return h.HandleMouseMove(ev)
	case input.MouseRelease, input.MouseClick:
		p := h.drag
		h.drag = nil
		if h.mgr != nil && h.mgr.Captured() == gooey.Component(h) {
			h.mgr.ReleaseCapture()
		}
		h.dock.Move(p, h.slotAt(ev.X, ev.Y))
		return true
	}
	return true
}

// dockModel is the dock's state, and the only thing any gesture touches.
// Keyboard and mouse are two callers of the same six methods, which is
// what keeps the promise that every dock action has a key: there is no
// pointer-only path to reach.
type dockModel struct {
	panes  []*dockPane
	active *prop.Property[int]
	// rev is what the host reads while painting. Slot, order and size
	// changes move LAYOUT, and layout is outside the property graph — a
	// Grid track or an arranged rect subscribes to nothing — so a model
	// edit needs something inside a paint node to make it schedule a
	// frame. This is that something, and touch() is the only writer.
	rev *prop.Property[int]
}

func newDockModel() *dockModel {
	return &dockModel{active: prop.NewSource(0), rev: prop.NewSource(0)}
}

func (d *dockModel) touch() { d.rev.Set(d.rev.Get() + 1) }

func (d *dockModel) add(p *dockPane) {
	p.order.Set(len(d.panes))
	d.panes = append(d.panes, p)
}

func (d *dockModel) Active() *dockPane {
	if len(d.panes) == 0 {
		return nil
	}
	i := d.active.Get()
	if i < 0 || i >= len(d.panes) {
		return nil
	}
	return d.panes[i]
}

func (d *dockModel) SetActive(p *dockPane) {
	for i, q := range d.panes {
		if q == p {
			d.active.Set(i)
			return
		}
	}
}

// ByID is how a menu item names a pane. Names rather than indexes,
// because the index is an artefact of declaration order and a menu that
// stops working when a pane is added is a menu nobody can maintain.
func (d *dockModel) ByID(id string) *dockPane {
	for _, p := range d.panes {
		if p.ID == id {
			return p
		}
	}
	return nil
}

// Cycle moves the active pane marker. Only ever the marker: this is what
// picks the target for every other gesture, and it must not move a pane
// by accident.
func (d *dockModel) Cycle(delta int) {
	if len(d.panes) == 0 {
		return
	}
	n := len(d.panes)
	d.active.Set(((d.active.Get()+delta)%n + n) % n)
}

// Move docks p into s and puts it last among its new slot-mates. A move
// to the slot it is already in is not a no-op at the model level — it
// still re-orders — but Set does not compare values anyway, so guarding
// it here is what keeps a redundant drop from costing a repaint.
func (d *dockModel) Move(p *dockPane, s dockSlot) {
	if p == nil {
		return
	}
	if dockSlot(p.slot.Get()) == s {
		d.touch()
		return
	}
	last := -1
	for _, q := range d.panes {
		if q != p && dockSlot(q.slot.Get()) == s {
			if o := q.order.Get(); o > last {
				last = o
			}
		}
	}
	p.slot.Set(int(s))
	p.order.Set(last + 1)
	d.touch()
}

// MoveActive is the keyboard's half of drag-to-position.
func (d *dockModel) MoveActive(s dockSlot) { d.Move(d.Active(), s) }

// Reorder swaps the active pane with its neighbour inside its own slot.
// Swapping the ORDER VALUES rather than the slice positions is what keeps
// this independent of declaration order.
func (d *dockModel) Reorder(delta int) {
	p := d.Active()
	if p == nil {
		return
	}
	mates := []*dockPane{}
	for _, q := range d.panes {
		if dockSlot(q.slot.Get()) == dockSlot(p.slot.Get()) {
			mates = append(mates, q)
		}
	}
	for i := 1; i < len(mates); i++ {
		for j := i; j > 0 && mates[j].order.Get() < mates[j-1].order.Get(); j-- {
			mates[j], mates[j-1] = mates[j-1], mates[j]
		}
	}
	at := -1
	for i, q := range mates {
		if q == p {
			at = i
		}
	}
	to := at + delta
	if at < 0 || to < 0 || to >= len(mates) {
		return
	}
	a, b := mates[at].order.Get(), mates[to].order.Get()
	mates[at].order.Set(b)
	mates[to].order.Set(a)
	d.touch()
}

// ToggleCollapsed shrinks a pane to its header, or gives it its body
// back. Its slot-mates take the space, which is the difference from
// hiding.
func (d *dockModel) ToggleCollapsed(p *dockPane) {
	if p == nil {
		return
	}
	p.collapsed.Set(!p.collapsed.Get())
	d.touch()
}

// ToggleHidden stops the pane showing WITHOUT giving its space back —
// gooey.Hidden, so the subtree, its caret and its Startables survive and
// the column does not reflow under the user.
//
// No touch(): the pane's Visibility is BOUND, so the Set already
// schedules a frame through the Composer's visibility observer, and the
// erase-and-restore sweep is what makes the pane leave the screen.
// Ticking rev as well would repaint the host for nothing.
func (d *dockModel) ToggleHidden(p *dockPane) {
	if p == nil {
		return
	}
	p.hidden.Set(!p.hidden.Get())
}

// TogglePinned flips what HideUnpinned will spare.
func (d *dockModel) TogglePinned(p *dockPane) {
	if p == nil {
		return
	}
	p.pinned.Set(!p.pinned.Get())
}

// HideUnpinned is "get everything out of my way": every unpinned pane
// hides, the pinned ones stay. It is the only operation that gives pin a
// meaning, and it is why pin is not a second spelling of hidden.
//
// The Set is GUARDED. prop.Set does not compare values, so hiding a pane
// that is already hidden would invalidate its visibility observer and buy
// a frame for nothing.
func (d *dockModel) HideUnpinned() {
	for _, p := range d.panes {
		if !p.pinned.Get() && !p.hidden.Get() {
			p.hidden.Set(true)
		}
	}
}

// ShowAll is the way back, and a shell needs one: with every pane hidden
// there is nothing left on screen to click.
func (d *dockModel) ShowAll() {
	for _, p := range d.panes {
		if p.hidden.Get() {
			p.hidden.Set(false)
		}
	}
}

// Resize grows or shrinks the active pane's slot. A splitter drag needs
// this too; the keyboard gets it first because the keyboard is the half
// that can be verified.
func (d *dockModel) Resize(delta int) {
	p := d.Active()
	if p == nil {
		return
	}
	n := p.size.Get() + delta
	if n < headerH+1 {
		n = headerH + 1
	}
	p.size.Set(n)
	d.touch()
}

// dockDef registers <DockHost>. Its children are DATA — <DockPane> never
// enters the visual tree as itself and never reaches the general builder
// — which is the same shape <MenuBar> uses for <Menu>, and for the same
// reason: a pane declaration is a description of the dock, not a
// component to place.
func dockDef(d *dockModel) *markup.ElementDef {
	return &markup.ElementDef{
		Name:  "DockHost",
		Proto: &dockHost{},
		Known: true,
		Doc:   "The IDE shell's dockable client area: declares the panes and owns the slot geometry.",
		Attrs: []markup.AttrSpec{
			{Name: "Style", Kind: markup.KindStyle, Binds: markup.BindsEither, Origin: markup.OriginBuiltin},
		},
		Children: markup.ChildSpec{Mode: markup.ModeRestricted, Only: []string{"DockPane"}},
		Build: func(e markup.Element, ctx *markup.Context) (gooey.Component, error) {
			st, err := markup.BoundStyle(e, ctx)
			if err != nil {
				return nil, err
			}
			h := &dockHost{dock: d, style: st}
			for _, c := range e.Children {
				if c.Name != "DockPane" {
					return nil, fmt.Errorf("markup: <DockHost> children must be <DockPane> elements, got <%s>", c.Name)
				}
				p, err := buildDockPane(c, ctx, h)
				if err != nil {
					return nil, err
				}
				d.add(p)
			}
			return h, nil
		},
	}
}

// buildDockPane turns one <DockPane> declaration into a pane. Everything
// resolvable fails HERE, at load: an unknown slot, a non-numeric size, a
// pane with two children. A dock that mis-lays-itself out on the fourth
// gesture because Slot="Lft" silently meant Left is the class of bug the
// markup tier exists to make impossible.
func buildDockPane(e markup.Element, ctx *markup.Context, h *dockHost) (*dockPane, error) {
	id := strings.TrimSpace(e.Attrs["Id"])
	if id == "" {
		return nil, fmt.Errorf("markup: <DockPane> needs an Id")
	}
	slot, err := parseSlot(e.Attrs["Slot"])
	if err != nil {
		return nil, fmt.Errorf("markup: <DockPane Id=%q>: %w", id, err)
	}
	size := 0
	if raw := strings.TrimSpace(e.Attrs["Size"]); raw != "" {
		if size, err = strconv.Atoi(raw); err != nil {
			return nil, fmt.Errorf("markup: <DockPane Id=%q Size=%q>: want a number of cells", id, raw)
		}
	}
	title := e.Attrs["Title"]
	if title == "" {
		title = strings.ToUpper(id)
	}
	// Pinned DEFAULTS TRUE. A shell whose panes all vanish on the first
	// HideUnpinned is a shell that ate the user's workspace, so the
	// declaration has to opt IN to being disposable.
	pinned := e.Attrs["Pinned"] != "false"
	p := newDockPane(id, title, slot, size, pinned)
	p.host = h
	if e.Attrs["Collapsed"] == "true" {
		p.collapsed.Set(true)
	}
	if e.Attrs["Hidden"] == "true" {
		p.hidden.Set(true)
	}
	kids, attach, err := markup.BuildChildren(e, ctx)
	if err != nil {
		return nil, err
	}
	p.Content = kids
	for _, k := range kids {
		p.bindBody(k)
	}
	p.attach = attach
	return p, nil
}
