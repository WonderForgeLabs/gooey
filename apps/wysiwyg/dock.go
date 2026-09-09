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
//     extent along the slot's stacking axis shrinks to that header so its
//     neighbours get the space. This is the operation that reclaims room.
//
//     THE HEADER IS NOT ALWAYS A ROW'S WORTH. Left, right and centre
//     stack top to bottom, so there the extent is headerH — one row. The
//     bottom strip stacks left to right, and there it is the header's
//     COLUMN WIDTH (dockPane.headerCols), which is the narrowest the
//     chevron, the title and the pin can all be drawn in. Spending
//     headerH on that axis is #431 — a one-column pane showing a bare
//     chevron; spending nothing is #441 — a full even share with a blank
//     body, and neighbours that got nothing at all.
//
//     What a collapsed strip gives back on the OTHER axis is
//     laidOutExtent's, and it is all-or-nothing: a horizontal strip
//     cannot be partly short, so its rows come back only once every pane
//     in it is collapsed.
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

	ID string
	// Title is a PLAIN FIELD and it feeds layout: headerCols measures
	// it, and in the bottom strip that measurement IS a collapsed pane's
	// width. So it is not the inert label the type suggests — mutating
	// it after the tree is live changes a pane's size, and nothing here
	// will notice.
	//
	// A source property is what the model's other fields use precisely
	// because the header reads them while painting. This one is not, and
	// the reason is that it is SET AT CONSTRUCTION: newPane's literal is
	// the only assignment in the package outside tests, and there is no
	// supported runtime rename.
	//
	// THE PREVIOUS VERSION OF THIS COMMENT SAID THE OPPOSITE, and said
	// it as a procedure: a caller assigning Title on a live pane "must
	// call dockModel.touch()", the alternative being "that the first
	// runtime rename is a silent no-op". Measured, touch() IS the silent
	// no-op. It bumps dock.rev, and the only node reading rev is
	// dockHost.Render; dockPane.Render subscribes to collapsed, pinned
	// and the active state and never to rev, so a header whose bounds
	// did not change is still clean — an OPEN pane repaints once and
	// keeps its old title for the life of the program. A collapsed one
	// in the bottom strip does update, and only by accident: headerCols
	// is its width, so the rename moved the pane's RECT and the repaint
	// followed the layout rather than the touch. A procedure documented
	// to prevent a defect, which produces it, is worse than no procedure
	// — it spends the attention that would have caught the bug.
	//
	// So: a test that renames must also change something the header
	// READS, which every test in dockcollapse_test.go already does —
	// `first.Title = …` is always followed by a Toggle*.
	// TestARenameNeedsMoreThanATouch pins both halves with damage
	// counts, so this paragraph is red rather than stale if the
	// subscription set changes.
	//
	// Making Title a property is the change that would buy a supported
	// rename, and it is not made here because nothing asks for one.
	// Teaching dockPane.Render to read rev is the other, and it would
	// repaint every header on every model touch — which per CLAUDE.md is
	// itself the change, not a side effect of one. Corrected in review
	// of #480.
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

// collapsedNow reads the pane's collapsed state.
//
// A method rather than a bare p.collapsed.Get() at each call site, so
// the read is greppable when a pane stops re-laying out. The prose that
// used to sit here described an `extent` function that does not exist;
// the sizing rule it explained lives inline in place(), and moved there.
//
// WHAT REPLACED THAT PROSE WAS A LIST OF THE CALL SITES, and it was
// already wrong when it was written: it named place() and laidOutExtent
// and missed slotMinimum and allCollapsed — and laidOutExtent does not
// call this at all, it calls allCollapsed. Three paragraphs in a row
// here have described a shape the code had left, which is enough to
// stop writing that kind of paragraph. `git grep collapsedNow` is the
// list, it is free, and it cannot go stale.
//
// THE RULE, which can: every caller is a layout pass or a minimum
// derived from one. Layout runs outside any evaluation context
// (`composer.go`, in `Composer.Frame`), so every one of these is a
// plain read that records nothing — exactly as place's own comment
// says. A caller from inside an evaluating node would be the
// interesting case and there is none; if one appears, it subscribes,
// and that is the thing to notice rather than the count.
//
// The reason to care at all is CLAUDE.md's "dependencies are recorded
// by the Get that actually runs" trap: a comment naming a mechanism
// that no longer runs is what makes that invisible. Corrected in review
// of #436, of #480, and of #480 again.
func (p *dockPane) collapsedNow() bool { return p.collapsed.Get() }

// trimHeaders caps the collapsed panes' extents — the non-zero entries
// of ext — so they sum to no more than budget, taking cells off the
// widest first.
//
// WHAT THIS REPLACED decremented the widest entry once per CELL of
// shortfall: O(shortfall x panes), run on both Measure and Arrange,
// with shortfall a column count. Small until a terminal is wide and a
// title is long, and the water-filling spelling is no harder to read.
// Raised in review of #480.
//
// IT IS EXACTLY EQUIVALENT, and that was measured rather than argued.
// Every (ext, budget) with up to four panes, extents 0..8 and budgets
// 0..19 — 63k cases — gives the same vector as the per-cell loop, which
// is why the remainder below is handed out from the RIGHT: the old loop
// picked the first STRICT maximum, so on a tie the later pane kept the
// extra column, and reproducing that is what makes this a refactor
// rather than a change nobody asked for.
//
// THE SAME SWEEP RETIRED A FINDING. Review of #480 reported that a
// collapsed pane could be trimmed to ZERO and lose the chevron that is
// the only way to re-open it, and that the trim therefore needed a
// floor of one. It cannot: to decrement a pane to zero the loop must
// find it the strict maximum, which means every other pane is already
// at zero, which means the budget could not have given one column to
// each. Zero of the 63k cases zeroed a pane while the budget had room
// for it. So the floor is a CONSEQUENCE of taking from the widest, not
// something to add on top — and TestTheTrimNeverZeroesAHeaderItCanAfford
// asserts the consequence, since nothing else in the suite did.
func trimHeaders(ext []int, budget int) {
	sum, n, hi := 0, 0, 0
	for _, e := range ext {
		sum += e
		if e > 0 {
			n++
		}
		if e > hi {
			hi = e
		}
	}
	if sum <= budget {
		return
	}
	// NOT EVEN A CHEVRON EACH, so somebody loses the only way to
	// re-open their pane, and position is all that is left to decide
	// who: every survivor gets exactly one column, so width has nothing
	// to say.
	//
	// IT WALKS FROM THE RIGHT, so the LAST-declared panes keep a chevron
	// and the first loses it. That is the direction the remainder loop
	// below uses and it is chosen for the same reason — it is what the
	// per-cell loop this replaced did, and the equivalence sweep above
	// covers these budgets too.
	//
	// It is also the OPPOSITE of place()'s `left` clamp and of
	// components.clampToExtent, which keep the first-declared and starve
	// the last. This comment used to cite the `left` clamp as "the same
	// rule", which borrowed authority from a rule the loop does not
	// follow — the third comment on this branch to do that, and the
	// reason the citation is now a contrast. Measured:
	// trimHeaders([8 3 3], 2), ([3 3 8], 2) and ([3 8 3], 2) all give
	// [0 1 1], indifferent to width. TestTheTrimTooTightForAChevronEach
	// pins it; the sweep above skips this branch by design (it `continue`s
	// once budget < n) because there is no floor to assert here.
	// Corrected in review of #480.
	if n > budget {
		for i := len(ext) - 1; i >= 0; i-- {
			switch {
			case ext[i] == 0:
			case budget > 0:
				ext[i], budget = 1, budget-1
			default:
				ext[i] = 0
			}
		}
		return
	}
	// The largest shared cap that still fits. Everything above it comes
	// down to it, which is "off the widest first" taken to its limit in
	// one step.
	lo := 1
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if cappedSum(ext, mid) <= budget {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	// The remainder after capping, handed back a cell at a time from the
	// RIGHT. Every pane above the cap is equal at it, so width has
	// nothing left to say and the direction is free — which makes it
	// worth spending on matching the loop this replaced exactly. See
	// the equivalence note above.
	left := budget - cappedSum(ext, lo)
	for i := len(ext) - 1; i >= 0; i-- {
		if ext[i] <= lo {
			continue
		}
		ext[i] = lo
		if left > 0 {
			ext[i]++
			left--
		}
	}
}

func cappedSum(ext []int, c int) int {
	s := 0
	for _, e := range ext {
		s += min(e, c)
	}
	return s
}

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
	pin := " "
	if p.pinned.Get() {
		pin = "*"
	}
	line := p.headerLead()
	// COLUMNS, not runes, on both halves of this. The pad used
	// len([]rune(line)) and the clip sliced runes, so a pane titled
	// "世界" asked for a header two cells narrower than its own text:
	// the pad overshot, the clip cut mid-glyph, and the pin landed on a
	// continuation cell. Nothing in this package could see it, because
	// every fixture title was ASCII — the CLAUDE.md trap verbatim.
	//
	// It stopped being cosmetic when a collapsed pane's WIDTH started
	// coming from this same string (#441): a rune count there sizes the
	// whole pane narrower than the header it exists to show.
	if n := b.W - render.StringWidth(line) - 1; n > 0 {
		line += strings.Repeat(" ", n)
	}
	line += pin
	// CLIPPED, THEN THE REMAINDER CLEARED, and the second half is not
	// belt-and-braces.
	//
	// render.ClipCols stops BEFORE a glyph that would overrun, so on a
	// pane whose last column would hold half of a wide glyph it comes
	// back a column SHORT of b.W. A dockPane is a chrome-only container
	// — its bounds enclose children whose own clean nodes will not
	// repaint — so the framework pre-clears nothing for it, and that
	// column keeps whatever the last frame left in it: the previous
	// title's glyph, on the header row, under the new one. Narrowing the
	// pane by a drag is the ordinary way to reach it.
	//
	// Written as a second SetString rather than by padding `line`,
	// because the pad above cannot know how many columns the clip will
	// actually return without doing the clip. Raised in review of #480.
	head := render.ClipCols(line, b.W)
	f.Cells.SetString(b.X, b.Y, head, st)
	if n := b.W - render.StringWidth(head); n > 0 {
		f.Cells.SetString(b.X+render.StringWidth(head), b.Y, strings.Repeat(" ", n), st)
	}
}

// headerLead is the header's TEXT — the chevron, a space, and the title —
// and it is one function because two callers must agree about it.
//
// Render pads from here out to the pin at the right edge, and place asks
// how wide a collapsed pane has to be in a slot that stacks in columns.
// Those are the same string, and writing it twice is how the pane comes
// to be laid out one width and painted at another.
//
// The Get is a subscription when Render calls it and a plain read when
// layout does, which is the framework's rule and not a special case
// here: the call site decides, per CLAUDE.md. Both chevrons are one
// column wide, so the width this feeds does not change when the pane
// opens and closes — a collapsed pane and the same pane open ask for the
// same header room.
func (p *dockPane) headerLead() string {
	chev := "v"
	if p.collapsed.Get() {
		chev = ">"
	}
	return chev + " " + p.Title
}

// headerCols is the narrowest the header can be drawn without losing any
// of it: the lead text plus the one column the pin always occupies.
//
// This is a COLUMN count from render.StringWidth, which is the whole
// constraint #441 named before the work started. A rune count here sizes
// a CJK-titled pane narrower than its own header, and the glyphs are
// then lost inside the pane's own rect — clipping stops the overflow
// reaching the neighbour, which is a different problem.
func (p *dockPane) headerCols() int {
	return render.StringWidth(p.headerLead()) + 1
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

// clipTo hard-truncates to w COLUMNS. It is render.ClipCols under a
// local name, kept because statusaddr.go's ellipsize documents itself
// against "dock.go has a clipTo of its own that HARD-TRUNCATES" and that
// sentence should keep naming something.
//
// It sliced RUNES until #441. The drag banner is the only caller left,
// and a pane title with one wide glyph in it made the banner one column
// too long — written into the cell past the host's right edge.
func clipTo(s string, w int) string { return render.ClipCols(s, w) }

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
func (h *dockHost) laidOutExtent(s dockSlot) int     { return h.dock.laidOutExtent(s) }

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
//
// COLLAPSE IS NOT ITS BUSINESS, and that division is the point. A pane
// collapses along the axis its slot STACKS on, which for left, right and
// centre is the axis this function does not measure — so a collapsed
// pane there rightly changes nothing here. The bottom strip stacks the
// other way, and there collapse lands on exactly this number; see
// laidOutExtent, which is what layout actually asks.
func (d *dockModel) slotExtent(s dockSlot) int {
	e := 0
	for _, p := range d.slotPanes(s) {
		if v := p.size.Get(); v > e {
			e = v
		}
	}
	return e
}

// laidOutExtent is slotExtent with collapse applied, and it exists for
// the one slot where collapse and the cross axis are the same axis.
//
// A pane collapses to its header. In a slot that stacks along its own
// length — left, right, centre — that is a share of the STACKING axis
// and place handles it; the slot's width is untouched, which is right.
// The bottom strip stacks the other way, so a pane there collapses along
// the axis this number IS, and the old code applied it in place instead:
// the pane became one COLUMN wide and full height, so its title vanished
// and only the chevron survived, while the strip kept every row of its
// declared Size. That is issue #431 as reported — "collapsing the bottom
// panel doesn't collapse everything, it just hides its contents".
//
// ALL, not any. A strip is as tall as its tallest pane needs, so one
// expanded pane keeps the strip open and the collapsed ones beside it
// simply have room to spare. That is a real limit rather than a
// simplification: a horizontal strip cannot be partly short. Making
// "all collapsed" the condition is what keeps the rule honest — the
// alternative, shrinking on any, would clip whatever stayed open.
// THE RESTRICTION IS STRUCTURAL, not a sentence asking the next caller
// to be careful. headerH is a ROW count, and returning it for a slot
// that measures columns is the exact unit confusion this function was
// written to remove: laidOutExtent(dockLeft) with both left panes
// collapsed answered 1, i.e. a one-column rail. The early return makes
// the wrong answer unreachable rather than merely documented. Found in
// review of #436.
func (d *dockModel) laidOutExtent(s dockSlot) int {
	if s != dockBottom || !d.allCollapsed(s) {
		// Collapse is on the STACKING axis in every other slot, and
		// place owns it there — the slot's extent does not change.
		return d.slotExtent(s)
	}
	return headerH
}

// allCollapsed reports whether s holds panes and every one of them is
// collapsed. An EMPTY slot is false, because "all of nothing" would make
// laidOutExtent answer headerH for a slot with no panes and leave a
// one-row stripe where the whole point is that the slot disappears.
//
// Extracted so laidOutExtent and Minimum ask the same question once
// rather than each spelling the loop. They disagreed before #441 — the
// fit check read slotExtent and could not see a collapse at all — and
// two hand-written copies of "is this strip shut" is how that comes
// back.
func (d *dockModel) allCollapsed(s dockSlot) bool {
	panes := d.slotPanes(s)
	if len(panes) == 0 {
		return false
	}
	for _, p := range panes {
		if !p.collapsedNow() {
			return false
		}
	}
	return true
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
// COLLAPSE REACHES THE COLUMN TERM, and this paragraph said the
// opposite for a review round after slotMinimum made it true. A
// collapsed strip pane is charged p.headerCols(), not starMin.
//
// The objection to that was real and is answered rather than dismissed:
// it measures LARGER, not smaller — headerCols for a pane titled "PANEL"
// is 8 against starMin's 3 — so collapsing a strip pane RAISES the
// usable minimum, which is not what the gesture looks like it should do.
// Measured on the bare two-pane strip dockcollapse_test.go builds
// ("世界世界" and "B"): 6x4 open, 14x4 with the wide one collapsed.
//
// It is charged anyway because Minimum answers a different question from
// the gesture. Minimum is "below this the shell is not usable", and
// place() genuinely needs those columns: a collapsed pane's header is
// drawn at its full title width and cannot be squeezed, so a Minimum
// that reported starMin for it would report less than the layout
// requires and hand the cram screen a size it cannot honour. The one
// invariant that has to hold is Minimum >= what place needs, and
// TestTheUsableMinimumCoversACollapsedHeaderBesideAnOpenPane is where it
// is pinned.
//
// What the gesture reclaims is ROWS, which is the axis it is about. That
// the column floor moves the other way is a consequence of the header
// being incompressible, not a policy about what "usable" means. Raised
// in review of #480, twice: once to make the term collapse-aware and
// once because this paragraph still denied it.
//
// ROWS: the bottom strip's laid-out extent, plus the tallest of the three
// upper slots. A slot stacking n panes VERTICALLY needs
// n*(headerH+starMin): the header row each pane always draws, plus the
// same "enough for something bordered" allowance the rest of this file
// spends on a star track. Reusing starMin rather than inventing a second
// constant is deliberate — there is one judgement here about how small is
// too small, and it should have one name.
//
// THE BOTTOM STRIP IS NOT ONE OF THOSE, and multiplying its row floor by
// its pane count was the same axis confusion #431 was about, one function
// over: its panes stack left to right and SHARE every row, so a third
// bottom pane asked for a minimum three header-plus-body strips tall. The
// n belongs in the column term, where it already is. The shipped page
// docks exactly one pane in Bottom, so n==1 and the whole suite agreed
// with the wrong rule. Found while fixing #441.
//
// COLLAPSE REACHES THIS NUMBER, through laidOutExtent rather than
// slotExtent. It did not before #441, and the consequence was the one
// the fit check exists to prevent: in a short terminal the user performs
// the gesture documented as "the operation that reclaims room", the rows
// genuinely come free, and the cram screen stays up because the minimum
// never moved.
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
	strip := d.slotPanes(dockBottom)
	if w := slotMinimum(strip, false); w > cols {
		cols = w
	}

	upper := 0
	for _, s := range []dockSlot{dockLeft, dockCenter, dockRight} {
		if n := slotMinimum(d.slotPanes(s), true); n > upper {
			upper = n
		}
	}
	rows := upper
	if len(strip) > 0 {
		bottom := d.laidOutExtent(dockBottom)
		// The floor is ONE pane's worth on this axis, whatever the pane
		// count — and it is headerH alone once the strip is shut, since
		// a row of headers is all it is going to draw.
		floor := headerH
		if !d.allCollapsed(dockBottom) {
			floor = headerH + starMin
		}
		if bottom < floor {
			bottom = floor
		}
		rows += bottom
	}
	return fitSize{Cols: cols, Rows: rows}
}

// slotMinimum is what a slot's panes need along the axis it stacks on,
// and it is ONE function because Minimum and place have to agree about
// it. They did not, in two directions at once, and both were the same
// mistake: this arithmetic was written out a second time in Minimum from
// the pane COUNT, which cannot see a pane.
//
// COLUMNS (`vertical` false, the bottom strip). place charges a
// collapsed pane p.headerCols() — the width of the header it still
// draws. Minimum charged starMin for every pane, collapsed or not, so a
// strip holding a collapsed 世界 (7 columns) beside an open pane
// reported 6 where place needs 10, and inside that gap the open pane is
// arranged at W=0. That is not a narrow pane, it is an absent one, and
// the fit screen — whose whole job is to say "this window is too small"
// — said the window was fine.
//
// ROWS (`vertical` true, the edge slots). place charges a collapsed pane
// headerH; Minimum charged n*(headerH+starMin) for the whole slot, so
// collapsing panes on the left LOWERED NOTHING. That is verbatim the
// failure Minimum's own doc comment describes four paragraphs up — the
// user performs "the operation that reclaims room", the rows genuinely
// come free, and the cram screen stays up because the minimum never
// moved. It was describing a bug it had.
//
// The open pane's ask is the usable share, not its declared size: a
// minimum is the point below which the dock stops being operable, and
// starMin is that number everywhere else in fit.go.
func slotMinimum(panes []*dockPane, vertical bool) int {
	n := 0
	for _, p := range panes {
		switch {
		case p.collapsedNow() && vertical:
			n += headerH
		case p.collapsedNow():
			n += p.headerCols()
		case vertical:
			n += headerH + starMin
		default:
			n += starMin
		}
	}
	return n
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
	bottom := h.laidOutExtent(dockBottom)

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
	// HOW MUCH OF THE SLOT'S AXIS EACH PANE ASKS FOR. A collapsed pane
	// wants its header and nothing more; every other pane — INCLUDING A
	// HIDDEN ONE — wants a full share. That is the "keeps its size" half
	// of the hide rule, and it is why hiding a pane leaves a gap rather
	// than reflowing its neighbours: hidden is not a third size, it is
	// the same size not drawn.
	//
	// A COLLAPSED PANE SHRINKS ON WHATEVER AXIS THIS SLOT STACKS ON, and
	// the units are the axis's own. `headerH` is a ROW count and is the
	// right answer only where the stacking axis runs in rows; in the
	// bottom strip it runs in COLUMNS, and the pane's natural extent
	// there is the width of the header it still draws.
	//
	// #431 spent headerH on the width, so the collapsed pane became ONE
	// COLUMN: its title disappeared and a bare chevron was left, while
	// the strip kept every row of its declared Size. #436 answered that
	// by moving the whole shrink to laidOutExtent and making place ignore
	// collapse in a cross-axis slot — which is right for a strip whose
	// panes are ALL collapsed and reclaims nothing at all when one of two
	// is, the state #441 reports. Both halves are needed: the rows come
	// from laidOutExtent when the strip is shut, and the columns come
	// from here whenever any single pane is.
	//
	// Said as `vertical` rather than through a `crossCollapse :=
	// !vertical` whose only use was `!crossCollapse`. A name asserting
	// the negation of how it is read costs a pass to undo. Simplified in
	// review of #436.
	collapsedExtent := func(p *dockPane) int {
		if vertical {
			return headerH
		}
		return p.headerCols()
	}

	// WHAT EACH COLLAPSED PANE TAKES, held per pane rather than summed,
	// because it has to be trimmable below.
	ext := make([]int, len(panes))
	fixed, flex := 0, 0
	for i, p := range panes {
		if p.collapsedNow() {
			ext[i] = collapsedExtent(p)
			fixed += ext[i]
		} else {
			flex++
		}
	}

	// A COLLAPSED PANE MAY NOT TAKE AN OPEN PANE'S LAST CELL.
	//
	// The `left` clamp further down is a document-order walk: it stops a
	// pane running past the slot's edge, and it decides who goes short
	// by declaration order alone. With `fixed` over budget that means
	// the panes declared FIRST take their full header and the ones after
	// get nothing — and nothing, for an OPEN pane, is a rect of zero
	// extent. A collapsed pane starving an open sibling is the wrong way
	// round in every reading of what collapse is for: the gesture gives
	// room back, and the pane that gave it up is the one that should go
	// short when there is not enough.
	//
	// So the collapsed panes are capped at total-flex — one cell each
	// for the open ones, which is the least that is still a pane — and
	// what they lose comes off the WIDEST header first. Evenly would
	// zero a one-column pane while an eight-column neighbour keeps
	// seven.
	//
	// THE FRAMEWORK'S OWN ANALOGUE IS NOT WIDEST-FIRST, and the
	// paragraph that used to sit here said it was: it cited
	// "ArrangeChild's own share loop", which does not exist —
	// ArrangeChild applies the margin/size/align/visibility sandwich to
	// ONE child and shares nothing. The real analogue is
	// components.clampToExtent, and it truncates in DOCUMENT ORDER: the
	// first tracks keep their stated size and the last ones lose,
	// because a fixed track means "this many cells". That is right for
	// a grid, where the sizes were declared, and wrong here, where they
	// are derived from title text nobody chose for its length. Naming
	// the difference is the point; borrowing authority from a rule that
	// was never read was the defect. Raised in review of #480.
	if budget := max(0, total-flex); fixed > budget {
		trimHeaders(ext, budget)
		fixed = 0
		for _, e := range ext {
			fixed += e
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
	// LEFT IS A BACKSTOP, NOT THE LIVE MECHANISM, and its comment
	// claimed otherwise for a round.
	//
	// What keeps the panes inside r is the widest-first trim above:
	// after it, fixed <= max(0, total-flex), so avail = total-fixed and
	// the shares sum to EXACTLY total — `n > left` cannot hold. Review
	// of #480 swept w = 0..60 over a two-collapsed-pane strip
	// (headerCols 20 and 19) and sum(n) never exceeded w at any width,
	// and the arm named for this clamp is green because of the trim.
	//
	// It stays because the two are guarding different things. The trim
	// is arithmetic over the pane list; this is a hard bound on the rect
	// actually handed to a child, and a future caller that reaches place
	// with an ext[] the trim did not produce would otherwise arrange a
	// header past the slot's right edge, where it paints into the
	// neighbouring slot's cells until something else repaints them. A
	// backstop is worth keeping and worth labelling as one, so the next
	// reader does not take it for the thing doing the work.
	left := total
	for i, p := range panes {
		n := ext[i]
		if !p.collapsedNow() {
			n = each
			if extra > 0 {
				n++
				extra--
			}
		}
		if n > left {
			n = left
		}
		left -= n
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
	bottom := h.laidOutExtent(dockBottom)
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

// Move docks p into s and puts it last among its new slot-mates.
//
// Dropping a pane into the slot it is already in returns early, and that
// guard is about BEHAVIOUR, not cost: without it the reorder below would
// run, find the highest order among its slot-mates and put the pane after
// them, so releasing a drag where it started would visibly send the pane
// to the bottom of its own slot.
//
// The early return still calls touch(), and that is load-bearing rather
// than an oversight. dockHost.Render reads d.rev and paints the drag
// indicator from h.drag; h.drag is cleared by a plain field write on the
// release path, which is outside the property graph and schedules
// nothing. So this touch is the only thing that asks for the frame that
// ERASES the indicator. Make this branch a true no-op and a same-slot
// drop leaves "move X → left" on screen until something unrelated
// repaints.
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
