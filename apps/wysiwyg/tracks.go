package main

// GRID STRUCTURE: seeing it, and editing it from the keyboard.
//
// A <Grid> renders as nothing. Its cells are arithmetic, not marks, so
// before this the only way to lay out inside one was to edit Rows="1,1"
// and Cols="1*,1*" in the properties grid and guess. This builds the
// design-time guide the overlay draws, and the verbs that change it.
//
// EVERY GESTURE HAS A KEY, and that is a testability requirement rather
// than politeness: mouse input cannot be injected through a recording
// pty, so a pointer-only feature cannot be verified in a capture at all
// (docs/learn/howto/howto-testing.md). The divider you can drag is the
// same edit `-`/`=` make.
//
// NOTHING HERE NAMES <Grid>. Which element grants cells, what its cell
// attributes are called, and which of its own attributes hold the track
// lists all come from markup.Grant and markup.AttrByRole — so a host
// registering a <Table> that grants cells gets this editor for free.

import (
	"strconv"
	"strings"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/apps/wysiwyg/components/preview"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/markup"
)

// trackCursor is which track the keyboard verbs act on.
type trackCursor struct {
	axis  preview.Axis
	index int
	// on is whether a track is selected at all. A separate flag rather
	// than index<0 because the axis has to survive the cursor being
	// dismissed and re-summoned.
	on bool
}

// gridNode is the node the guide describes: the selection if it grants
// cells, otherwise the selection's parent if IT does.
//
// Both cases matter. Selecting the grid itself is how you go and edit
// its tracks; selecting a child is how you see which cell that child is
// in while you move it. Anything else has no cell structure and the
// overlay draws nothing.
func (ed *editor) gridNode() *node {
	if ed.sel == nil {
		return nil
	}
	if ed.grantOf(ed.sel.Elem).Kind == markup.GrantCell {
		return ed.sel
	}
	if p := ed.parentOf(ed.sel); p != nil && ed.grantOf(p.Elem).Kind == markup.GrantCell {
		return p
	}
	return nil
}

// trackAttr is the name of the attribute holding one axis's track list,
// asked for BY ROLE. Empty when the element declares none.
func (ed *editor) trackAttr(elem string, axis preview.Axis) string {
	role := markup.RoleRowTracks
	if axis == preview.AxisCol {
		role = markup.RoleColTracks
	}
	for _, e := range ed.palette {
		if e.Name == elem {
			return markup.AttrByRole(e, role)
		}
	}
	return ""
}

// tracks is one axis's specs as written, with the implicit single star
// track filled in for an absent attribute.
func (ed *editor) tracks(n *node, axis preview.Axis) []string {
	attr := ed.trackAttr(n.Elem, axis)
	if attr == "" {
		return nil
	}
	return preview.ParseTracks(n.Attrs[attr])
}

// buildGuide is what the overlay calls from Arrange.
//
// It returns nil for "draw nothing", which is the common case: no
// selection, a selection with no cell structure anywhere near it, or a
// grid the layout has not given bounds yet.
func (ed *editor) buildGuide() *preview.Guide {
	if ed.design != nil && !ed.design.Get() {
		return nil
	}
	n := ed.gridNode()
	if n == nil {
		return nil
	}
	g, _ := ed.componentFor(n).(*components.Grid)
	if g == nil {
		return nil
	}
	b := g.Bounds()
	if b.W <= 0 || b.H <= 0 {
		return nil
	}
	cells := ed.probeCells(g)
	if len(cells) == 0 {
		return nil
	}

	guide := &preview.Guide{
		Bounds: b,
		Cells:  cells,
		Rows:   ed.tracks(n, preview.AxisRow),
		Cols:   ed.tracks(n, preview.AxisCol),
		SelRow: -1,
		SelCol: -1,
		Cursor: preview.Track{Index: -1},
	}
	if ed.cursor.on {
		guide.Cursor = preview.Track{Axis: ed.cursor.axis, Index: ed.cursor.index}
	}
	// Which cell the selection occupies, so "where am I" is answerable
	// without counting. Only when the SELECTION is a child of this grid
	// — when the grid itself is selected there is no one cell to mark.
	if ed.sel != n {
		if l := gooey.LayoutOf(ed.componentFor(ed.sel)); l != nil {
			guide.SelRow, guide.SelCol = l.Row, l.Col
		}
	}
	return guide
}

// probeCells is gridCells with a probe subject chosen for it.
//
// gridCells walks a COMPONENT through every cell and reads back what the
// real Grid.Arrange returned, which is what makes the rectangles a
// record of the layout rather than a second implementation of it. It
// needs something to walk, and the grid's own first child is the honest
// choice — it is already subject to that layout.
//
// An EMPTY grid has nothing to walk, and that is the case that made the
// feature worth having: a grid with no children is exactly the one you
// cannot see. So a scratch component is added for the duration and
// removed again. It is never rendered — no paint node is built for it,
// because the composer's tree walk happened before this — and the
// restore is unconditional.
func (ed *editor) probeCells(g *components.Grid) [][]gooey.Rect {
	if len(g.Children) > 0 {
		return ed.cellsThrough(g, g.Children[0])
	}
	scratch := &components.Text{}
	g.Children = append(g.Children, scratch)
	defer func() { g.Children = g.Children[:len(g.Children)-1] }()
	return ed.cellsThrough(g, scratch)
}

// setCursor moves the track cursor and asks for a frame.
//
// THE INVALIDATE IS NOT OPTIONAL. The cursor is a plain Go field,
// invisible to the property graph, so moving it changes what the overlay
// would draw and schedules exactly nothing — the documented
// Layout.Left/Top hazard in a different costume, and the same fix the
// drag path already threads an invalidate func for.
//
// The overlay learns about it during the frame this schedules: Arrange
// rebuilds the guide, sees it differ, and dirties its own paint node.
// Nothing subscribes to this field, because a subscription would have to
// fire on every selection in the app rather than only when the picture
// changes.
func (ed *editor) setCursor(c trackCursor) {
	ed.cursor = c
	ed.invalidate()
}

// cycleTrackCursor is the `[` / `]` verb: step through every track of
// the grid in scope, columns first, then rows, then off again.
//
// One walk over both axes rather than a key per axis, because the thing
// being chosen is "a track" and the axis is a property of which one —
// and because two more keybindings for a second axis is how a keymap
// stops being learnable.
func (ed *editor) cycleTrackCursor(step int) {
	n := ed.gridNode()
	if n == nil {
		ed.sayDrag("select a grid, or something inside one, to edit its tracks")
		return
	}
	cols, rows := len(ed.tracks(n, preview.AxisCol)), len(ed.tracks(n, preview.AxisRow))
	total := cols + rows
	if total == 0 {
		return
	}
	// A flat index over [cols..., rows...], with total meaning "off".
	// The +1 slot is what lets the cursor be dismissed by walking off
	// the end rather than needing its own key.
	cur := total
	if ed.cursor.on {
		cur = ed.cursor.index
		if ed.cursor.axis == preview.AxisRow {
			cur += cols
		}
	}
	next := ((cur+step)%(total+1) + (total + 1)) % (total + 1)
	if next == total {
		ed.setCursor(trackCursor{})
		ed.sayDrag("")
		return
	}
	c := trackCursor{on: true}
	if next < cols {
		c.axis, c.index = preview.AxisCol, next
	} else {
		c.axis, c.index = preview.AxisRow, next-cols
	}
	ed.setCursor(c)
	ed.sayTrack(n)
}

// trackExtent is the current measured size of the track under the
// cursor, which ResizeTrack and CycleTrack need to turn an Auto track
// into a fixed one at the size it already occupies.
func (ed *editor) trackExtent(n *node) int {
	g, _ := ed.componentFor(n).(*components.Grid)
	if g == nil {
		return 0
	}
	cells := ed.probeCells(g)
	if ed.cursor.axis == preview.AxisRow {
		if ed.cursor.index < len(cells) && len(cells[ed.cursor.index]) > 0 {
			return cells[ed.cursor.index][0].H
		}
		return 0
	}
	if len(cells) > 0 && ed.cursor.index < len(cells[0]) {
		return cells[0][ed.cursor.index].W
	}
	return 0
}

// writeTracks puts an axis's specs back into the document and rebuilds.
func (ed *editor) writeTracks(n *node, axis preview.Axis, specs []string) {
	attr := ed.trackAttr(n.Elem, axis)
	if attr == "" {
		return
	}
	if n.Attrs == nil {
		n.Attrs = map[string]string{}
	}
	n.Attrs[attr] = preview.FormatTracks(specs)
	ed.rebuild()
}

// resizeTrack is the `-` / `=` verb.
func (ed *editor) resizeTrack(delta int) {
	n, specs, ok := ed.cursorTracks()
	if !ok {
		return
	}
	specs[ed.cursor.index] = preview.ResizeTrack(specs[ed.cursor.index], ed.trackExtent(n), delta)
	ed.writeTracks(n, ed.cursor.axis, specs)
	ed.sayTrack(n)
}

// cycleTrackKind is the `g` verb: star -> Auto -> fixed -> star. The
// edit that has no numeric form, so it cannot be spelled with -/=.
func (ed *editor) cycleTrackKind() {
	n, specs, ok := ed.cursorTracks()
	if !ok {
		return
	}
	specs[ed.cursor.index] = preview.CycleTrack(specs[ed.cursor.index], ed.trackExtent(n))
	ed.writeTracks(n, ed.cursor.axis, specs)
	ed.sayTrack(n)
}

// addTrack is the `a` verb: a new star track after the cursor.
func (ed *editor) addTrack() {
	n, specs, ok := ed.cursorTracks()
	if !ok {
		return
	}
	at := ed.cursor.index + 1
	specs = append(specs, "")
	copy(specs[at+1:], specs[at:])
	specs[at] = "1*"
	ed.writeTracks(n, ed.cursor.axis, specs)
	// Follow the new track rather than staying on the old one: the user
	// asked for it, so it is what they are about to size.
	ed.setCursor(trackCursor{axis: ed.cursor.axis, index: at, on: true})
	ed.sayTrack(n)
}

// removeTrack is the `r` verb.
//
// It refuses to remove the LAST track rather than allowing a grid with
// none: components.Grid treats no declared tracks as one implicit star
// track, so "zero tracks" is not a state the layout has — an editor that
// wrote Rows="" would be showing a grid with one row while claiming it
// had none.
//
// Children left pointing past the end are NOT renumbered, and that is
// deliberate rather than unfinished: Grid clamps an out-of-range cell,
// so they stay visible, and silently rewriting cells the user did not
// touch is a bigger surprise than a child that needs re-placing.
func (ed *editor) removeTrack() {
	n, specs, ok := ed.cursorTracks()
	if !ok {
		return
	}
	if len(specs) <= 1 {
		ed.sayDrag("a grid always has at least one track on each axis, so this one cannot be removed")
		return
	}
	at := ed.cursor.index
	specs = append(specs[:at], specs[at+1:]...)
	ed.writeTracks(n, ed.cursor.axis, specs)
	if at >= len(specs) {
		at = len(specs) - 1
	}
	ed.setCursor(trackCursor{axis: ed.cursor.axis, index: at, on: true})
	ed.sayTrack(n)
}

// cursorTracks is the guard every track verb starts with: there is a
// grid in scope, the cursor is on one of its tracks, and the axis has
// specs to edit.
//
// It returns a COPY of the specs, so a verb that bails out part-way
// cannot leave the document half-edited.
func (ed *editor) cursorTracks() (*node, []string, bool) {
	n := ed.gridNode()
	if n == nil {
		ed.sayDrag("select a grid, or something inside one, to edit its tracks")
		return nil, nil, false
	}
	if !ed.cursor.on {
		ed.sayDrag("press ] to pick a track first")
		return nil, nil, false
	}
	specs := ed.tracks(n, ed.cursor.axis)
	if ed.cursor.index >= len(specs) {
		// The document changed under the cursor — a track it pointed at
		// is gone. Dismiss rather than edit the wrong one.
		ed.setCursor(trackCursor{})
		return nil, nil, false
	}
	return n, append([]string(nil), specs...), true
}

// sayTrack reports the track under the cursor and what it is set to, so
// the keyboard path has the same feedback the gutter gives the eye.
func (ed *editor) sayTrack(n *node) {
	if !ed.cursor.on {
		ed.sayDrag("")
		return
	}
	specs := ed.tracks(n, ed.cursor.axis)
	if ed.cursor.index >= len(specs) {
		return
	}
	axis := "row"
	if ed.cursor.axis == preview.AxisCol {
		axis = "col"
	}
	ed.sayDrag(axis + " " + strconv.Itoa(ed.cursor.index) + " of " +
		strconv.Itoa(len(specs)) + ": " + specs[ed.cursor.index] +
		"  [ ] move   - = size   g kind   a add   r remove")
}

// trackSummary is the whole track spec of the grid in scope, for tests
// and for anything that wants the structure as one string.
func trackSummary(specs []string) string { return strings.Join(specs, ",") }
