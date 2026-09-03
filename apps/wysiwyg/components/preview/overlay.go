package preview

// DESIGN-TIME OVERLAY: drawing the layout model you are editing.
//
// A <Grid> renders as nothing at all. Its cells are an arithmetic fact
// about the container, not marks on the screen, so laying out inside one
// meant editing Rows="1,1" and Cols="1*,1*" against a preview that
// showed no rows and no columns. This paints them.
//
// # Why this is a component and not a few lines in Pane.Render
//
// Pane.Render would be the obvious seam and it is the wrong one, for a
// reason that is invisible until it goes wrong on screen.
//
// The composer paints in Z-ORDER, depth-first PRE-order (Composer.paint's z-order pass)
// — a container paints BEFORE its children. Anything Pane.Render drew
// would go down first and the previewed tree would paint over it; worse,
// every leaf in that tree PRE-CLEARS its bounds to the nearest ancestor's
// background (Composer.build's pre-clear, `clearStyle`), so the tree would not merely cover the
// guides, it would erase them. The result is guide lines that survive
// only in the gaps between elements, which reads as a rendering bug.
//
// A LATER SIBLING paints after. So the overlay is Pane's second child,
// and depth-first pre-order does the rest: Pane, the document subtree,
// then this.
//
// WITH ONE LIMIT SINCE #437: z-order is document order in TWO layers, and
// an ordinary later sibling only outranks the ordinary layer. Anything in
// the previewed subtree implementing gooey.Overlay — a popup surface, a
// ToastHost, an AdornmentLayer — is lifted above every ordinary node,
// including this one, wherever it sits. Harmless today for two reasons
// that do hold: this overlay paints nothing until a Guide is bound, and
// neither host paints while empty.
//
// NOT because design mode is Frozen, which an earlier version of this
// comment gave as the third reason. Frozen bounds DISPATCH and
// Startables, not evaluation or the input-tree walk — and a
// ValidationMarker places its popup from SetFocusManager through
// attachAdornment, with no gesture involved, showing from the first
// frame on an empty Required field. A previewed document containing
// <Validate Required="true"/> and <ValidationMarker/> therefore places
// a lifted adornment while Frozen, today. That is the case this stops
// being harmless for, and it is nearer than "the moment a previewed
// document grows a live overlay" suggested.
//
// Not asserted here: components.TestAValidationMarkerPlacesItsAdornmentWhileFrozen
// is the pin, and it fails the freeze itself if a future Frozen ever does
// gate the walk — at which point this paragraph may go. Corrected in
// review of #444. See gooey.Overlay and
// docs/specs/2026-08-30-overlay-layer.md.
//
// # Why it does not wipe what it sits on top of
//
// A component covering the previewed tree is exactly the thing that
// would blank it. The three-case pre-clear rule (in Composer.build, keyed on `covered`)
// turns on ONE question — is this a gooey.Container? — and a LEAF fills
// its whole rect before painting. This implements ChildComponents and
// returns nil, which makes it a chrome-only container: it pre-clears
// NOTHING and overpaints only the cells it actually draws.
//
// That is also why it must never declare a Background. A container with
// a background handle fills its bounds and is marked `covered`, which
// would both blank the tree and force the whole subtree to repaint above
// it every frame.
//
// # Why it does not eat every click
//
// It spans the preview, so hit-testing would find it first and the
// designer would select nothing, everywhere. gooey.HitTestTransparent is
// the framework's existing answer — AdornmentLayer had this exact
// problem (mouse.go:60) — and this returns true unconditionally: the
// overlay is a picture of the layout, never a target.

import (
	"strconv"
	"strings"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// Axis is which way a track runs.
type Axis int

const (
	AxisRow Axis = iota
	AxisCol
)

// Guide is everything the overlay draws: one grid's probed geometry and
// the track specs that produced it.
//
// The rectangles are PROBED — the editor walks a component through every
// cell and reads back what the real Grid.Arrange returned (see the
// editor's gridCells) — so this struct never recomputes track
// arithmetic and cannot drift from the layout. It is a picture of what
// happened, not a prediction.
type Guide struct {
	// Bounds is the grid's own rect, in terminal coordinates.
	Bounds gooey.Rect
	// Cells is [row][col] of probed rectangles.
	Cells [][]gooey.Rect
	// Rows and Cols are the track specs AS WRITTEN in the markup —
	// "Auto", "1*", "20". Shown in the gutters against the space they
	// produce, which is the whole point: the numbers you edit next to
	// their effect.
	Rows, Cols []string
	// Cursor is the track the keyboard verbs act on. Len < 0 means no
	// track is selected and the gutters are drawn without a highlight.
	Cursor Track
	// Selected is the cell of the currently selected element, or {-1,-1}
	// for none. Drawn as a filled corner marker so "which cell am I in"
	// is answerable without counting.
	SelRow, SelCol int
}

// Track identifies one row or column.
type Track struct {
	Axis Axis
	// Index is the track's position, or -1 for "no track selected".
	Index int
}

// None reports that no track is under the cursor.
func (t Track) None() bool { return t.Index < 0 }

// Overlay paints a Guide over the previewed tree.
type Overlay struct {
	gooey.Base

	// guide supplies the model, and nil-or-nil-result means DRAW
	// NOTHING. Injected as a function rather than a field the editor
	// writes, for the same reason the editor injects hitTest and
	// invalidate: this package has no document knowledge, and the
	// editor rebuilds its tree on hot reload.
	//
	// IT IS CALLED FROM Arrange, NEVER FROM Render, and that is a
	// correctness requirement rather than a preference. Producing a
	// Guide means PROBING the grid — walking a child through every cell
	// and re-running the real Grid.Arrange to read back the slots — and
	// that does two things a paint node must not do. It mutates the
	// tree, and it runs layout, whose Gets would be recorded as
	// dependencies of this overlay's paint node because a Get inside an
	// evaluating computed subscribes. Layout deliberately runs outside
	// any evaluation context (see Composer.Frame, and composer.go's package
	// comment) for exactly that reason,
	// and Arrange is on that side of the line.
	guide func() *Guide

	// cur is the last Guide produced, drawn by Render.
	cur *Guide

	// marks is every cell the last paint wrote, and what it covered. See
	// mark: without it the overlay cannot change its own output.
	marks []mark

	// rev is the overlay's own dependency, and the ONLY thing Render
	// subscribes to.
	//
	// It exists because everything the guide is built from — the
	// selection, the document, the track cursor — is PLAIN GO STATE the
	// property graph cannot see. A Render that only called guide() would
	// record no dependency at all and go permanently deaf: correct on
	// the frame it first painted, never updated again.
	//
	// Arrange bumps it ONLY WHEN THE GUIDE ACTUALLY CHANGED, and that
	// condition is not an optimisation — it is what makes the frame
	// terminate.
	//
	// Arrange runs on EVERY frame, and a bump dirties this paint node,
	// which schedules another frame, which arranges again. Bumping
	// unconditionally is therefore a permanent repaint loop: the
	// composition never settles, and the symptom is not a slow editor
	// but a test harness that spins until it gives up. Four tests catch
	// it, three of them belonging to other features.
	//
	// The weaker version of the same mistake is subscribing to the
	// editor's "something was edited or selected" revision instead.
	// That terminates, but it repaints this overlay on every click
	// anywhere in the document — a paint node that draws nothing, added
	// to every selection in the app, forever. Comparing first means the
	// overlay costs exactly one repaint when the picture changes and
	// zero when it does not.
	//
	// prop.Set does not compare values (prop/prop.go:101), so the
	// comparison has to happen here rather than being relied on there.
	rev *prop.Property[int]

	// design gates the whole overlay. Guides are a design-time artifact;
	// in LIVE mode the preview is the app and must look like the app.
	design *prop.Property[bool]

	style       render.Style
	gutterStyle render.Style
	cursorStyle render.Style
}

// NewOverlay builds the overlay.
func NewOverlay(guide func() *Guide, design *prop.Property[bool]) *Overlay {
	return &Overlay{
		guide:  guide,
		design: design,
		rev:    prop.NewSource(0),
		style: render.Style{
			Fg: render.Color{Set: true, R: 90, G: 100, B: 130},
		},
		gutterStyle: render.Style{
			Fg: render.Color{Set: true, R: 130, G: 140, B: 170},
		},
		cursorStyle: render.Style{
			Fg: render.Color{Set: true, R: 20, G: 20, B: 30},
			Bg: render.Color{Set: true, R: 190, G: 180, B: 90},
		},
	}
}

// ChildComponents makes this a CONTAINER holding nothing, which is what
// stops it pre-clearing. See the file comment — as a leaf it would blank
// the previewed tree every time it painted.
func (o *Overlay) ChildComponents() []gooey.Component { return nil }

// HitTestTransparent keeps the overlay out of the way of selection.
func (o *Overlay) HitTestTransparent() bool { return true }

// Measure takes nothing: the overlay is drawn over its siblings' space,
// so claiming any would push the previewed tree around.
func (o *Overlay) Measure(avail gooey.Size) gooey.Size { return gooey.Size{} }

// Arrange records the overlay's own rect and REFRESHES THE GUIDE.
//
// Pane arranges the document subtree first and this second, so by the
// time this runs the grid being described has its final bounds — which
// is what makes a probe of its cells accurate on the SAME frame the
// tracks changed, rather than one frame stale. See the guide field for
// why the refresh cannot live in Render.
// It also sizes the overlay to EXACTLY what it is about to draw, rather
// than to the pane it was handed, and that is the difference between a
// proportionate repaint and a catastrophic one.
//
// The bounds are what the composer uses for damage. When they change —
// and going from "describing a grid" to "describing nothing" is a
// change to zero — the sweep clears the old rect and force-repaints
// every node beneath it (Composer.restoreUnder). At pane size
// that is the entire document on every mode flip and every selection
// that leaves a grid. At guide size it is the grid and its gutters,
// which is precisely the region whose appearance actually changed.
//
// Zero size is also what makes the overlay cost NOTHING when there is
// no grid in scope: the paint loop skips a node with no area, so the
// common case is not a paint that draws nothing, it is not a paint.
func (o *Overlay) Arrange(b gooey.Rect) {
	var next *Guide
	if o.guide != nil {
		next = o.guide()
	}
	if !sameGuide(o.cur, next) {
		// Legal here for the same reason the composer's own bounds and
		// visibility sweeps may Set: layout runs OUTSIDE any evaluation
		// (Composer.Frame), so this is a plain write, not a write from
		// inside a computed. The paint loop later in the same frame
		// picks the node up, so the guide is drawn on the frame it
		// changed rather than one behind.
		o.rev.Set(o.rev.Get() + 1)
	}
	o.cur = next
	o.Base.Arrange(o.extent(b))
}

// sameGuide reports whether two guides would draw identically. Nil is a
// value here, not a missing one: "no grid in scope" is the state the
// overlay is in almost all the time, and nil == nil is what makes moving
// the selection around a document with no grid in it cost zero repaints.
func sameGuide(a, b *Guide) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.Bounds != b.Bounds || a.Cursor != b.Cursor ||
		a.SelRow != b.SelRow || a.SelCol != b.SelCol ||
		!sameStrings(a.Rows, b.Rows) || !sameStrings(a.Cols, b.Cols) ||
		len(a.Cells) != len(b.Cells) {
		return false
	}
	for r := range a.Cells {
		if len(a.Cells[r]) != len(b.Cells[r]) {
			return false
		}
		for c := range a.Cells[r] {
			if a.Cells[r][c] != b.Cells[r][c] {
				return false
			}
		}
	}
	return true
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// extent is the rect the overlay actually marks: the grid it describes,
// and nothing else — everything it draws is inside those bounds.
//
// Clamped to the pane it was handed, so a grid larger than the preview
// cannot claim damage outside it.
func (o *Overlay) extent(slot gooey.Rect) gooey.Rect {
	empty := gooey.Rect{X: slot.X, Y: slot.Y}
	if o.cur == nil {
		return empty
	}
	if o.design != nil && !o.design.Get() {
		return empty
	}
	b := o.cur.Bounds
	if b.W <= 0 || b.H <= 0 {
		return empty
	}
	x := max(slot.X, b.X)
	y := max(slot.Y, b.Y)
	right := min(slot.X+slot.W, b.X+b.W)
	bottom := min(slot.Y+slot.H, b.Y+b.H)
	if right <= x || bottom <= y {
		return empty
	}
	return gooey.Rect{X: x, Y: y, W: right - x, H: bottom - y}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// Render draws the guide.
//
// THE DEPENDENCY IS READ FIRST, above every early return, and that is
// not style. Dependencies are recorded by the Get that actually RUNS, so
// a Get behind `if o.cur == nil { return }` would drop out of the set on
// exactly the frames the overlay has nothing to draw — which are the
// frames it most needs to hear about the next change. It would go
// permanently silent the first time the selection left a grid, and
// nothing in the framework would report it.
//
// Design mode is deliberately NOT read here. It is already accounted
// for: the editor's guide function returns nil in LIVE mode, so the
// guide changes, Arrange bumps the revision, and this repaints once to
// erase. Reading the property here as well would make every mode flip
// repaint this overlay whether or not a grid was ever in scope.
func (o *Overlay) Render(f *gooey.Frame) {
	o.rev.Get()

	// Lift the previous frame's marks BEFORE the early return, not after
	// it. The frame where the guide disappears is exactly the frame that
	// has to take the old one off the screen, and it is the frame that
	// draws nothing.
	o.restoreMarks(f)

	g := o.cur
	if g == nil || g.Bounds.W <= 0 || g.Bounds.H <= 0 {
		return
	}
	// GUTTERS FIRST, THEN CELL MARKS, because setCell only writes into
	// blank cells and therefore whatever is drawn first wins. The track
	// specs are the information; the corner marks are the scaffolding
	// around it, and a short track can put a corner exactly where its
	// spec belongs (a two-row track's bottom-left corner is its spec's
	// line). Losing the corner there costs nothing; losing the spec
	// would hide the number being edited.
	o.drawGutters(f, g)
	o.drawCells(f, g)
}

// drawCells marks the cell boundaries.
//
// It draws the boundary INSIDE each cell's own rect rather than in the
// one-cell gap between cells, because there is no gap: Grid's tracks
// abut. So the top-left corner of every cell gets a mark, and the cell
// containing the selection gets a filled one.
func (o *Overlay) drawCells(f *gooey.Frame, g *Guide) {
	for r := range g.Cells {
		for c := range g.Cells[r] {
			q := g.Cells[r][c]
			if q.W <= 0 || q.H <= 0 {
				continue
			}
			mark, st := '┌', o.style
			if r == g.SelRow && c == g.SelCol {
				mark, st = '▟', o.cursorStyle
			}
			o.setCell(f, q.X, q.Y, mark, st)
			// The right and bottom edges of the LAST track have no
			// following cell to carry a corner, so the grid would read
			// as open on two sides without these.
			if c == len(g.Cells[r])-1 && q.W > 1 {
				o.setCell(f, q.X+q.W-1, q.Y, '┐', o.style)
			}
			if r == len(g.Cells)-1 && q.H > 1 {
				o.setCell(f, q.X, q.Y+q.H-1, '└', o.style)
			}
		}
	}
}

// drawGutters writes the track specs against the tracks they produce:
// the column specs along the grid's top edge, the row specs down its
// left edge.
//
// THIS IS THE STRUCTURE, NOT DECORATION. "1*" and "Auto" are the values
// the user edits, and showing them anywhere other than against the space
// they produced makes the reader hold the mapping in their head.
//
// THE GUTTERS ARE INSIDE THE GRID'S OWN BOUNDS, not in a margin around
// it, and that was a correction rather than a preference. Drawn outside,
// they land on whatever happens to be there — for a grid at the top-left
// of the preview that is the EDITOR'S OWN pane border, so the document's
// structure would be scribbled over the editor's furniture. A grid has
// no reserved margin and this component cannot create one (claiming
// space would push the previewed tree around and change the very layout
// it is describing), so the only space it may write in is the space
// being edited.
//
// Combined with the blank-cell rule in setCell, that makes the guide
// strictly additive: it fills empty room inside the grid and touches
// nothing else.
func (o *Overlay) drawGutters(f *gooey.Frame, g *Guide) {
	// Column specs on the grid's first row, one cell in from each
	// track's corner mark.
	if len(g.Cells) > 0 {
		for c, spec := range g.Cols {
			if c >= len(g.Cells[0]) {
				break
			}
			q := g.Cells[0][c]
			st := o.gutterStyle
			if g.Cursor.Axis == AxisCol && g.Cursor.Index == c {
				st = o.cursorStyle
			}
			o.drawText(f, q.X+1, q.Y, fit(spec, q.W-1), st)
		}
	}
	// Row specs on the grid's first column, one row BELOW each track's
	// corner — which is what keeps row 0's spec off the same cells the
	// column specs just took.
	for r, spec := range g.Rows {
		if r >= len(g.Cells) || len(g.Cells[r]) == 0 {
			break
		}
		q := g.Cells[r][0]
		if q.H < 2 {
			// A one-row-tall track has no second line, and writing the
			// spec on the shared first line would collide with the
			// column specs. Skipped rather than overlapped.
			continue
		}
		st := o.gutterStyle
		if g.Cursor.Axis == AxisRow && g.Cursor.Index == r {
			st = o.cursorStyle
		}
		o.drawText(f, q.X, q.Y+1, fit(spec, q.W), st)
	}
}

// fit truncates a spec to the space its track actually has, so a wide
// spelling in a narrow column cannot run into its neighbour.
func fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= w {
		return s
	}
	if w == 1 {
		return "…"
	}
	return string(r[:w-1]) + "…"
}

func (o *Overlay) drawText(f *gooey.Frame, x, y int, s string, st render.Style) {
	for i, r := range []rune(s) {
		o.setCell(f, x+i, y, r, st)
	}
}

// mark is one cell the overlay wrote, and what was under it.
//
// The overlay has to be able to take its own marks BACK, and that is not
// obvious until it breaks. Guides are drawn only into blank cells, and a
// chrome-only container pre-clears nothing — so once a mark is down, the
// cell is no longer blank and the overlay can never redraw it. The
// symptom was a track spec that appeared correctly and then would not
// change style when the cursor moved onto it: the glyph was already
// there, so the highlighted version was refused.
//
// Restoring first makes each frame's guide independent of the last.
type mark struct {
	x, y  int
	wrote rune
	prev  render.Cell
}

// restoreMarks puts back what the last frame's guide covered up.
//
// A mark is only lifted if the cell STILL HOLDS THE GLYPH THE OVERLAY
// PUT THERE. Anything else means the document repainted that cell in the
// meantime and now owns it — the overlay paints after the tree, so by
// the time this runs that repaint has already happened — and writing the
// saved content back would be restoring a stale copy over live content.
func (o *Overlay) restoreMarks(f *gooey.Frame) {
	for i := len(o.marks) - 1; i >= 0; i-- {
		m := o.marks[i]
		if f.Cells.At(m.x, m.y).Rune == m.wrote {
			f.Cells.SetCell(m.x, m.y, m.prev)
		}
	}
	o.marks = o.marks[:0]
}

// setCell writes one cell IF NOTHING IS ALREADY THERE.
//
// A GUIDE MAY NEVER DESTROY WHAT IT IS DESCRIBING, and that is not a
// nicety — it is what makes drawing over the document safe at all.
// Grid tracks ABUT: there is no gap between cells to draw a boundary in,
// so a cell's mark necessarily lands on the first cell of that track,
// which is exactly where a child's content starts. The first version of
// this overwrote the "a" of every element in the top-left of its cell,
// so turning the guide on silently corrupted the thing you were laying
// out.
//
// Reading the buffer back is legitimate here for a reason specific to
// this component's position: the overlay paints AFTER the document
// subtree (it is Pane's later sibling), so by the time this runs the
// cells hold the composed content of this frame. A component painting
// BEFORE its neighbours could not ask this question.
//
// The consequence is deliberate and worth stating: where a cell corner
// is occupied, no mark appears. The content wins, because the content is
// what the user is looking at.
//
// render.Buffer.Set is already bounds-checked; the nil guard is for
// tests that render without a frame.
func (o *Overlay) setCell(f *gooey.Frame, x, y int, r rune, st render.Style) {
	if f == nil || f.Cells == nil {
		return
	}
	prev := f.Cells.At(x, y)
	if !blank(prev.Rune) {
		return
	}
	f.Cells.Set(x, y, r, st)
	o.marks = append(o.marks, mark{x: x, y: y, wrote: r, prev: prev})
}

// blank is what counts as an empty cell. Both spellings occur: a cleared
// buffer holds the zero rune, and a pre-cleared rect holds spaces.
func blank(r rune) bool { return r == 0 || r == ' ' }

// FormatTracks renders a track list back to the markup spelling.
func FormatTracks(specs []string) string { return strings.Join(specs, ",") }

// ParseTracks splits a track attribute into its specs. An empty
// attribute is ONE implicit star track, which is what components.Grid
// does with no declared tracks — the editor must show the same thing the
// layout does, or the gutter would say "no tracks" over a grid that
// visibly has one.
func ParseTracks(attr string) []string {
	attr = strings.TrimSpace(attr)
	if attr == "" {
		return []string{"1*"}
	}
	parts := strings.Split(attr, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		out = append(out, strings.TrimSpace(p))
	}
	return out
}

// ResizeTrack returns spec grown or shrunk by one step, in its own
// units: a star track changes weight, a fixed track changes cells, and
// Auto becomes a fixed track at the size it currently occupies — because
// "make Auto bigger" has no meaning until it stops being Auto.
//
// size is the track's CURRENT measured extent, used only for that
// conversion.
func ResizeTrack(spec string, size, delta int) string {
	spec = strings.TrimSpace(spec)
	if strings.EqualFold(spec, "auto") {
		n := size + delta
		if n < 1 {
			n = 1
		}
		return strconv.Itoa(n)
	}
	if strings.HasSuffix(spec, "*") {
		n := 1
		if head := strings.TrimSuffix(spec, "*"); head != "" {
			if v, err := strconv.Atoi(head); err == nil {
				n = v
			}
		}
		n += delta
		if n < 1 {
			n = 1
		}
		return strconv.Itoa(n) + "*"
	}
	n, err := strconv.Atoi(spec)
	if err != nil {
		// Not a spelling this understands. Leaving it alone is the
		// honest answer: silently replacing it would discard something
		// the layout does understand.
		return spec
	}
	n += delta
	if n < 1 {
		n = 1
	}
	return strconv.Itoa(n)
}

// CycleTrack moves a spec through the three sizing modes, which is the
// edit that has no numeric form: 1* -> Auto -> fixed -> 1*.
func CycleTrack(spec string, size int) string {
	spec = strings.TrimSpace(spec)
	switch {
	case strings.HasSuffix(spec, "*"):
		return "Auto"
	case strings.EqualFold(spec, "auto"):
		if size < 1 {
			size = 1
		}
		return strconv.Itoa(size)
	default:
		return "1*"
	}
}
