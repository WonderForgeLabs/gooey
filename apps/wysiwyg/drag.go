package main

// MOVE: dragging an element around the design surface.
//
// TWO KINDS OF MOVE, ONE MECHANISM. Free geometry belongs to the PARENT,
// so what a drag means depends on what the element is inside:
//
//   FREE — a child of a <Canvas> has Canvas.Left/Canvas.Top and goes
//   wherever the pointer goes.
//
//   RE-CELL — a child of a <Grid> has Grid.Row/Grid.Col and no offset at
//   all, so it SNAPS to whichever cell the pointer is in. Two elements
//   may land in the same cell; Grid renders that as an overlap and
//   reports nothing, and that is accepted rather than papered over. See
//   dragKind.
//
// The mechanism is two-speed on purpose, and the split is the whole
// design:
//
//   PER MOTION — write the attached properties on the LIVE component's
//   gooey.Layout (Left/Top, or Row/Col) and ask for a frame. Nothing
//   else. No markup, no rebuild, no re-mount. Composer.Frame lays out
//   unconditionally and its bounds sweep does the rest: it clears the
//   vacated rect to the ancestor background, repaints the moved
//   component, and force-repaints whatever the old rect uncovered.
//
//   ON RELEASE — write Canvas.Left/Canvas.Top or Grid.Row/Grid.Col into
//   the document and rebuild ONCE.
//
// THE SNAP HAPPENS DURING THE DRAG, NOT ON RELEASE, and that is part of
// the decision rather than a nicety. An element that floated freely under
// the pointer and jumped into a cell when the button came up would be a
// preview that lies about what the release is going to do — which is
// exactly what a user reports as a bug. Writing Row/Col per motion is the
// same fast path the free drag uses, so the honest preview costs nothing.
//
// Writing markup per motion would work and is the trap. rebuild()
// discards and re-mounts the whole designer subtree, so a drag would cost
// a full re-mount per pointer report — and it would LOOK correct, which
// is why the per-motion damage count is pinned separately from the
// release count. A bounds assertion passes just as well when the entire
// tree repainted.
//
// Reconciling markup only on release is also what keeps the drag's own
// target stable: rebuild() is what repopulates docRoot/nodeOf, so a
// motion that wrote markup would invalidate the very map the drag is
// using to know what it is dragging.
//
// THE POSITIONS LIVE IN MEMORY, AND NOWHERE ELSE. Under the wrapping
// model the surface is never serialized, so a position on it has no home
// in the saved file. This file does not invent one — no attribute, no
// comment, no property element. `dragState` is the one place that holds
// in-flight geometry, so a future solution/project file has one struct to
// populate rather than a scattering.

import (
	"strconv"
	"strings"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/markup"
)

// dragState is the gesture in flight. Zero value means no drag.
//
// It holds the ORIGIN of the gesture rather than the last position, so
// every motion is computed from the press: accumulating deltas would
// drift by one cell per dropped or coalesced motion report, and a
// terminal coalesces freely under load.
type dragState struct {
	node *node
	comp gooey.Component
	// kind is dragKind's answer, and only DragFree and DragCell ever
	// reach here — a gesture that cannot proceed does not start.
	kind string
	// startX/startY is where the pointer went down; origL/origT is the
	// element's offset at that moment.
	startX, startY int
	origL, origT   int
	// origRow/origCol is the DragCell equivalent, and cells is the grid's
	// cell rectangles in terminal coordinates, probed once at press. See
	// gridCells for why they are probed rather than computed.
	origRow, origCol int
	cells            [][]gooey.Rect
	// moved is whether any motion actually changed the offset. A press
	// and release with no movement is a CLICK, and must not write markup
	// or cost a rebuild.
	moved bool
}

func (d *dragState) active() bool { return d != nil && d.node != nil }

// dragLive is active() plus the question active() cannot ask: is the
// node still IN the document?
//
// A gesture is not the only thing happening while the button is down.
// Keys and SGR mouse reports arrive interleaved on ONE ordered stream
// (input/mouse.go), so a Delete binding fires perfectly well between a
// press and its release — and deleteSelected unlinks the node without
// knowing a drag is holding it. What survives is a dragState pointing at
// a node the document no longer contains and a component no longer
// mounted, which does not crash: Drag writes Left/Top to a detached
// component and Release writes Canvas.Left onto an orphan that the next
// rebuild does not serialise. The write is simply lost, silently, which
// is the same silent-discard class the drag path exists to delete.
//
// Checked here rather than cleared in deleteSelected because the stale
// pointer is the invariant, not the delete: retype and any future
// mutator can unlink a node just as easily, and a rule enforced where it
// is READ cannot be forgotten by the next writer.
func (ed *editor) dragLive() bool {
	if !ed.drag.active() {
		return false
	}
	// parentOf is nil only for the surface, and the surface cannot be
	// selected — so for any node a gesture could have started on, nil
	// means it has left the tree.
	if ed.parentOf(ed.drag.node) == nil {
		ed.drag = dragState{}
		return false
	}
	return true
}

// Press is preview.Designer. It selects what is under the pointer and,
// where that element has free geometry, begins a drag.
func (ed *editor) Press(x, y int) bool {
	ed.setSelection(ed.nodeAt(ed.hitTestOrNil(x, y)))
	ed.beginDrag(x, y)
	return true
}

// Click is preview.Designer, and it is the DRILL: a double-click selects
// one level deeper than the press already selected.
//
// A single click is ignored here rather than duplicated — the press did
// it, and doing it twice would cost a second setSelection for every
// click, which is a repaint of the properties grid nobody asked for.
//
// It never begins a drag. A drag starts on the press that precedes this,
// and by the time a click is synthesized the button is already up.
func (ed *editor) Click(x, y, count int) bool {
	if count < 2 {
		return false
	}
	n := ed.nodeAtDepth(ed.hitTestOrNil(x, y), 1)
	if n == nil || n == ed.sel {
		// Nothing below what is already selected. Consumed anyway — the
		// gesture belongs to the designer — but not spent on a Set that
		// would repaint the properties grid to show the same rows.
		return true
	}
	ed.setSelection(n)
	return true
}

// Drag is preview.Designer: one pointer report during a gesture.
func (ed *editor) Drag(x, y int) bool {
	if !ed.dragLive() {
		return false
	}
	l := gooey.LayoutOf(ed.drag.comp)
	if l == nil {
		return false
	}
	if ed.drag.kind == DragCell {
		return ed.dragCell(l, x, y)
	}
	left, top := ed.drag.origL+(x-ed.drag.startX), ed.drag.origT+(y-ed.drag.startY)
	// Clamped at the surface's origin: a negative Canvas.Left is not
	// expressible in the document, so allowing the live offset to go
	// negative would let the element drift somewhere the release could
	// not record.
	if left < 0 {
		left = 0
	}
	if top < 0 {
		top = 0
	}
	if l.Left == left && l.Top == top {
		// Consume it anyway — the gesture owns the pointer — but do not
		// spend a frame on a motion that changed nothing. A terminal
		// reports motion per cell, and a drag along one axis produces a
		// stream of no-ops on the other.
		return true
	}
	l.Left, l.Top = left, top
	ed.drag.moved = true
	// THE FRAME DOES NOT HAPPEN BY ITSELF. Layout.Left/Top are plain int
	// fields with no property behind them (unlike Visibility, which has
	// BindVisibilityFunc), so this write is invisible to the property
	// graph — and App.handle does not schedule a frame either, because
	// frames are scheduled by the graph. Without this call the element
	// simply does not move, with no error anywhere.
	ed.invalidate()
	return true
}

// dragCell is the RE-CELL motion: snap to whichever cell the pointer is
// in, cell by cell, while the button is still down.
//
// The offset from the press is deliberately NOT used. A free drag carries
// the element by its grab point so it does not jump under the pointer;
// a cell has no sub-cell position to preserve, so the honest rule is the
// simplest one — the element goes where the pointer IS. Carrying an
// offset here would put the element in a different cell from the one the
// pointer is over, which is the same lie as snapping on release.
func (ed *editor) dragCell(l *gooey.Layout, x, y int) bool {
	row, col := ed.drag.cellAt(x, y)
	if l.Row == row && l.Col == col {
		// Consume it — the gesture owns the pointer — but do not spend a
		// frame. Most motion inside a cell changes nothing, which is
		// precisely what makes the snap cheap.
		return true
	}
	l.Row, l.Col = row, col
	ed.drag.moved = true
	// Layout.Row/Col are outside the property graph exactly as Left/Top
	// are: plain int fields, no notification, no frame without this.
	ed.invalidate()
	return true
}

// cellAt is the SNAP: the cell containing the pointer, or the nearest one
// when the pointer has left the grid.
//
// Nearest by squared edge distance rather than by centre: a pointer just
// past the right edge of the grid belongs to the last column whatever the
// column's width, and a centre metric gets that wrong on a wide track.
// Falling back to nearest rather than refusing is what keeps a drag that
// overshoots the grid recoverable without releasing.
func (d *dragState) cellAt(x, y int) (int, int) {
	best, bestRow, bestCol := -1, d.origRow, d.origCol
	for r := range d.cells {
		for c := range d.cells[r] {
			q := d.cells[r][c]
			if x >= q.X && x < q.X+q.W && y >= q.Y && y < q.Y+q.H {
				return r, c
			}
			dx, dy := edgeDist(x, q.X, q.W), edgeDist(y, q.Y, q.H)
			if d2 := dx*dx + dy*dy; best < 0 || d2 < best {
				best, bestRow, bestCol = d2, r, c
			}
		}
	}
	return bestRow, bestCol
}

// edgeDist is how far v is outside the span [at, at+size), and 0 inside.
func edgeDist(v, at, size int) int {
	if v < at {
		return at - v
	}
	if v >= at+size {
		return v - (at + size) + 1
	}
	return 0
}

// Release is preview.Designer: commit the gesture.
//
// This is the ONLY thing in the drag path that writes markup, which is
// what stops a save from racing a motion.
func (ed *editor) Release(x, y int) bool {
	if !ed.dragLive() {
		return false
	}
	d := ed.drag
	ed.drag = dragState{}
	if !d.moved {
		return true // a click, not a move
	}
	l := gooey.LayoutOf(d.comp)
	if l == nil {
		return true
	}
	// The attribute names are the ATTACHED properties of the parent, and
	// they are ASKED FOR BY ROLE rather than spelled here. Writing the
	// wrong pair is the silent-discard defect the catalog work exists to
	// delete, and "Grid.Row" written as a literal in an editor is that
	// defect one rename away: applyLayout's missing default arm discards
	// an unrecognised attached attribute without a word.
	//
	// This was the SECOND copy of the taxonomy — dragKind decided the
	// kind from the element name, and this decided the names from the
	// kind. Both now come from the one declaration.
	g := ed.grantOf(ed.parentOf(d.node).Elem)
	// AN ORDERED PAIR, NOT A MAP. Both roles are known and fixed at the
	// switch, so a map buys nothing and costs determinism twice: Go
	// randomises range order, so the two `d.node.Attrs` writes landed in
	// an arbitrary order, and a `lost` built by overwriting kept
	// whichever role the runtime happened to visit last. The same drag
	// on the same document could report either role, which makes the
	// sentence untestable and the diff noisy.
	type roleWrite struct {
		role markup.Role
		v    int
	}
	var writes [2]roleWrite
	switch d.kind {
	case DragCell:
		writes = [2]roleWrite{{markup.RoleRow, l.Row}, {markup.RoleCol, l.Col}}
	default:
		writes = [2]roleWrite{{markup.RoleX, l.Left}, {markup.RoleY, l.Top}}
	}
	var lost []string
	for _, w := range writes {
		name := g.Attr(w.role)
		if name == "" {
			// The gesture began because the grant said this role
			// existed, so an empty name here means the document changed
			// under the drag. Dropping the write is what already
			// happened silently; saying so is the change.
			//
			// EVERY missing role is named, not just one: losing both is
			// a different failure from losing one, and a message that
			// reports a single role makes them look identical.
			lost = append(lost, string(w.role))
			continue
		}
		d.node.Attrs[name] = strconv.Itoa(w.v)
	}
	ed.rebuild()
	// After the rebuild the status line says "✓ builds" again, which is
	// what clears any refusal a previous press left there — unless the
	// move could not be recorded, in which case the refusal is the news.
	if len(lost) > 0 {
		ed.sayDrag("<" + d.node.Elem + "> moved, but its parent grants no " +
			strings.Join(lost, " or ") +
			" to write it to — the move was not saved")
	} else {
		ed.sayDrag("")
	}
	return true
}

// beginDrag starts a gesture if the selected element can actually be
// moved, AND SAYS SO WHEN IT CANNOT.
//
// FREE GEOMETRY IS A PROPERTY OF THE PARENT, not of the element — see
// dragKind for the three answers. What changed here is that the refusal
// is no longer silent: a press that will not move anything writes a
// sentence saying which element and why, because "I dragged it and
// nothing happened" is indistinguishable from a broken editor and there
// was no diagnostic anywhere to tell the two apart.
//
// Refusing rather than guessing is still the point: writing Canvas.Left
// onto a child of a VStack would be silently discarded by applyLayout,
// which is the exact defect the catalog work exists to delete.
func (ed *editor) beginDrag(x, y int) {
	ed.drag = dragState{}
	n := ed.sel
	kind := ed.dragKind(n)
	if kind != DragFree && kind != DragCell {
		ed.sayDrag(ed.dragSummary(n, kind))
		return
	}
	comp := ed.componentFor(n)
	if comp == nil {
		// The document builds but this node has no component the walk
		// could verify — see mapNodes. Nothing to move, and the user is
		// entitled to know that is why.
		ed.sayDrag(ed.dragSummary(n, DragUnmapped))
		return
	}
	l := gooey.LayoutOf(comp)
	if l == nil {
		ed.sayDrag(ed.dragSummary(n, DragUnmapped))
		return
	}
	d := dragState{
		node: n, comp: comp, kind: kind,
		startX: x, startY: y,
		origL: l.Left, origT: l.Top,
		origRow: l.Row, origCol: l.Col,
	}
	if kind == DragCell {
		d.cells = ed.gridCells(ed.parentOf(n), comp)
		if len(d.cells) == 0 {
			ed.sayDrag(ed.dragSummary(n, DragUnmapped))
			return
		}
	}
	ed.drag = d
	ed.sayDrag("")
}

// gridCells is the grid's cell rectangles in terminal coordinates, and it
// is PROBED THROUGH THE REAL Grid.Arrange rather than computed.
//
// Recomputing them would mean a second copy of Grid's track arithmetic —
// star distribution, auto sizing, the offsets — living in an editor, and
// the day the two disagreed the ghost would snap to a cell the layout
// then put the element somewhere else in. So instead the dragged
// component is walked through every cell with its alignment temporarily
// set to Stretch (which is what makes ArrangeChild hand it the WHOLE slot
// rather than its desired size inside it), and the bounds that come back
// are the slots themselves.
//
// Once per gesture, not once per motion: it costs an Arrange of the grid
// per cell, which is nothing for a design-time grid and would still be
// nothing per motion — but the Layout it mutates is the live one, and
// doing that repeatedly while the user is dragging is a larger window for
// a frame to land mid-probe.
//
// ONE LIMIT, STATED RATHER THAN HIDDEN: Auto tracks are sized by the
// MEASURE pass, which this does not re-run, so a grid whose track widths
// depend on the dragged element sees the slots it had before the drag.
// The snap still lands the element in the right CELL — the real frame
// re-measures and re-arranges — but the probed rectangle it was chosen
// from can be a cell or two stale at the edges.
func (ed *editor) gridCells(parent *node, comp gooey.Component) [][]gooey.Rect {
	if parent == nil {
		return nil
	}
	g, _ := ed.componentFor(parent).(*components.Grid)
	if g == nil {
		return nil
	}
	return ed.cellsThrough(g, comp)
}

// cellsThrough is gridCells once the grid and the probe subject are
// known. Split out so the design-time overlay can probe with a subject
// of its own choosing — the grid's first child, or a scratch component
// when the grid is empty, which is the case a guide is most needed for.
func (ed *editor) cellsThrough(g *components.Grid, comp gooey.Component) [][]gooey.Rect {
	b := g.Bounds()
	if b.W <= 0 || b.H <= 0 {
		return nil
	}
	l := gooey.LayoutOf(comp)
	if l == nil {
		return nil
	}
	// A <Grid> with no declared tracks is one star row by one star column
	// — components.Grid.rows()/cols() say so — so the counts floor at 1
	// rather than at len(), which would be zero and probe nothing.
	rows, cols := max(1, len(g.Rows)), max(1, len(g.Cols))

	saved := *l
	defer func() {
		*l = saved
		// Put the tree back exactly as it was. Composer.Frame lays out
		// unconditionally so this would be corrected anyway, but a probe
		// that leaves the tree wrong between here and the next frame is a
		// probe that any reader has to reason about.
		g.Arrange(b)
	}()
	l.HAlign, l.VAlign = gooey.AlignStretch, gooey.AlignStretch
	l.Width, l.Height = 0, 0
	l.Margin = gooey.Thickness{}
	l.RowSpan, l.ColSpan = 1, 1

	out := make([][]gooey.Rect, rows)
	for r := range out {
		out[r] = make([]gooey.Rect, cols)
		for c := range out[r] {
			l.Row, l.Col = r, c
			g.Arrange(b)
			out[r][c] = boundsOf(comp)
		}
	}
	return out
}

// boundsOf is Bounds() through the interface every component embedding
// gooey.Base satisfies. It is an assertion, not reflection — the same
// test the framework's own hit walk makes.
func boundsOf(c gooey.Component) gooey.Rect {
	if b, ok := c.(interface{ Bounds() gooey.Rect }); ok {
		return b.Bounds()
	}
	return gooey.Rect{}
}

// componentFor is nodeOf inverted for one node. Linear, and deliberately
// so: it runs once per gesture rather than once per motion.
func (ed *editor) componentFor(n *node) gooey.Component {
	for c, m := range ed.nodeOf {
		if m == n {
			return c
		}
	}
	return nil
}

// hitTestOrNil is the framework query with the nil-binding case folded in.
func (ed *editor) hitTestOrNil(x, y int) gooey.Component {
	if ed.hitTest == nil {
		return nil
	}
	return ed.hitTest(x, y)
}

// invalidate asks for a frame. Injected like hitTest rather than reached
// through ed.app, because the tests drive Composer.Frame() directly and
// have no *gooey.App at all — and because App.Swap builds a new Composer
// on a hot reload.
func (ed *editor) invalidate() {
	if ed.invalidateFn != nil {
		ed.invalidateFn()
	}
}

// The drag kinds. A string rather than a bool because "you cannot move
// this" is not the interesting part — WHY is, and each reason needs a
// different sentence.
const (
	// DragNone is nothing selected, or the surface, which is chrome.
	DragNone = "nothing selected"
	// DragRoot is the user's own root. It is positioned by the surface,
	// and the surface is never saved, so there is nowhere to record a
	// move to.
	DragRoot = "the document root"
	// DragFree is a child of a <Canvas>: Canvas.Left/Canvas.Top.
	DragFree = "free"
	// DragCell is a child of a <Grid>: Grid.Row/Grid.Col, snapped.
	DragCell = "re-cell"
	// DragOrder is a child of a <VStack>, an <HStack> or any other
	// element granting markup.GrantOrder: no geometry at all, position
	// IS the index, so a drag means reorder.
	DragOrder = "reorder"
	// DragFixed is a child of a container that grants NOTHING — a
	// <Border>, a <ScrollView>, anything holding one child it places
	// itself.
	//
	// This used to be folded into DragOrder, because dragKind's default
	// arm returned reorder for everything that was not a Canvas or a
	// Grid. That was wrong in a way no test could see: it told the user
	// a border's child could be reordered among siblings it does not
	// have. The catalog distinguishes the two — GrantOrder is declared,
	// GrantNone is the zero value — so the editor can now say which.
	DragFixed = "placed by its parent"
	// DragUnmapped is a node the built tree could not be paired with —
	// see mapNodes. Not a property of the parent; a property of how far
	// the inversion could descend.
	DragUnmapped = "unmapped"
	// DragStale is the document not building AT ALL. The designer is
	// still showing the last version that did, so there are elements on
	// screen that look pressable and correspond to nothing the editor can
	// address — see rebuild, which drops docRoot up front and returns on
	// the error without restoring it.
	DragStale = "stale preview"
)

// dragKind reports why an element can or cannot be dragged.
//
// GEOMETRY IS A PROPERTY OF THE PARENT, not of the element, and the
// answer now comes from the parent's DECLARATION rather than from its
// name: a markup.GrantOffset parent gives its children an offset, a
// GrantCell parent gives them a cell, GrantOrder gives them nothing but
// their order, and GrantNone places them itself.
//
// The mapping below is this editor's whole knowledge of layout models.
// Adding a container to gooey does not touch it; adding a new
// markup.GrantKind does, and the compiler will not say so — which is
// what TestEveryGrantKindHasADragKind is for.
func (ed *editor) dragKind(n *node) string {
	// FIRST, because it explains the nil that would otherwise be reported
	// as DragNone. With docRoot nil every press resolves to no node at
	// all, so "nothing is selected: press an element to move it" is what
	// the user was told — while pressing an element, repeatedly. The
	// selection is not the problem and saying it is sends them looking in
	// the wrong place.
	if ed.docRoot == nil {
		return DragStale
	}
	if n == nil || ed.isSurface(n) {
		return DragNone
	}
	p := ed.parentOf(n)
	if p == nil || ed.isSurface(p) {
		return DragRoot
	}
	return dragKindFor(ed.grantOf(p.Elem).Kind)
}

// dragKindFor is the grant-to-gesture mapping on its own, so a test can
// walk every declared GrantKind without building a document for each.
func dragKindFor(g markup.GrantKind) string {
	switch g {
	case markup.GrantOffset:
		return DragFree
	case markup.GrantCell:
		return DragCell
	case markup.GrantOrder:
		return DragOrder
	}
	return DragFixed
}

// dragSummary is the sentence a refused drag puts on screen.
//
// THE SILENT REFUSAL IS THE DEFECT THIS DELETES. Before it, a press on a
// child of a <VStack> selected the element and then did nothing at all
// for the rest of the gesture — which is exactly what a broken editor
// looks like, and there was no diagnostic anywhere to tell the two apart.
// Every reason names the ELEMENT and the CONTAINER, because "you can't
// move this" without saying what decides it is the same dead end one step
// later.
//
// It returns "" for the kinds that proceed, so the caller never has to
// ask twice whether there is anything to say.
func (ed *editor) dragSummary(n *node, kind string) string {
	switch kind {
	case DragNone:
		return "nothing is selected: press an element to move it"
	case DragRoot:
		return nodeLabel(n) + " is the document root: it is positioned by " +
			"the design surface, which is never saved"
	case DragOrder:
		p := ed.parentOf(n)
		return nodeLabel(n) + " is inside a <" + p.Elem + ">, which positions its " +
			"children by ORDER: reordering by drag is not implemented yet"
	case DragUnmapped:
		return nodeLabel(n) + " has no component this editor can address: the built " +
			"tree stopped corresponding to the document above it"
	case DragStale:
		return "the document does not build, so the designer is showing the last " +
			"version that did: nothing on it can be selected until the error above is fixed"
	}
	return ""
}

// nodeLabel names an element the way the outline does.
func nodeLabel(n *node) string {
	if n == nil {
		return "nothing"
	}
	if name := n.Attrs["Name"]; name != "" {
		return "<" + n.Elem + " Name=\"" + name + "\">"
	}
	return "<" + n.Elem + ">"
}

// sayDrag posts (or clears) the drag hint, and it GUARDS AT THE CALL SITE
// because prop.Set does not compare: setting the hint to the empty string
// it already holds would invalidate the status bar's dependents and cost
// a repaint on every press of every draggable element, which would show
// up as the selection gesture getting more expensive for no reason.
func (ed *editor) sayDrag(msg string) {
	if ed.dragHint.Get() == msg {
		return
	}
	ed.dragHint.Set(msg)
}
