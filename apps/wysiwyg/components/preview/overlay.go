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
// THIS TYPE IS NOT A gooey.Overlay, despite the name, and must not
// become one. That marker lifts a subtree out of document order into a
// second paint layer — which would put these guides above the whole
// PAGE rather than above the previewed document, and would break the
// one thing the paragraph above depends on. Ordinary document order is
// still the rule for everything that is not lifted, which is what makes
// "Pane's second child" the correct and sufficient answer here.
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
			o.drawText(f, q.X+1, q.Y, spec, st, q.W-1)
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
		o.drawText(f, q.X, q.Y+1, spec, st, q.W)
	}
}

// fit truncates a spec to the space its track actually has, so a wide
// spelling in a narrow column cannot run into its neighbour.
//
// COLUMNS, not runes. This was statusaddr.go's ellipsize body character
// for character, including the defect that one was rewritten for, and
// the sweep that fixed the four helpers in package main stopped at this
// package's boundary: fit("世世世", 4) answered "it fits" and returned
// six columns for a four-column track.
//
// IT IS NOT REACHABLE TODAY, and this paragraph claimed it was — off a
// "Tracks=" attribute that does not exist; ed.tracks reads Rows/Cols.
// components.ParseGridLens rejects a spec holding a wide glyph before
// a *Guide exists at all, so nothing in the shipped editor can hand
// this function one. The argument for pinning it anyway is in
// overlay_test.go's header: the safety is a property of a DIFFERENT
// function, in a different module, that no reader of this file can see
// and no future spec grammar has to keep. Raised in review of #524.
func fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if render.StringWidth(s) <= w {
		return s
	}
	if w == 1 {
		return "…"
	}
	return render.ClipCols(s, w-1) + "…"
}

// drawText writes a string across the guide's cells, ONE GRAPHEME
// CLUSTER AT A TIME AND BY ITS COLUMN WIDTH.
//
// It walked []rune and passed x+i as a column, which is the second half
// of the same trap fit held: a rune index is not a column, and
// Buffer.Set lays no render.Continuation — so a wide glyph left the
// buffer believing one column where the terminal draws two, and the next
// rune landed on a cell the previous one already covered. CLAUDE.md
// names this pair. Raised in review of #524.
//
// IT FITS THE STRING ITSELF, and then bounds the walk anyway. The call
// sites passed fit(spec, w) AND w, two arguments that have to agree
// with nothing making them — the only thing keeping them together was
// that both sites repeated the width expression. Now `cols` is said
// once per call.
//
// THE SECOND BOUND IS NOT REDUNDANT, because fit and this walk disagree
// about a zero-width cluster and `max(w, 1)` is the right advance. fit
// budgets
// with render.StringWidth, which charges a standalone combining mark, a
// tab or a NUL zero columns; this walk charges each of them one,
// because advancing 0 would leave the next cluster landing on a cell
// setCluster has already marked — it would find it non-blank, refuse,
// and truncate the rest of the label. So the walk stops at the budget
// instead of trusting that the two counts agree. The overshoot it
// prevents was one column (only a LEADING combining mark is its own
// cluster) into a blank cell inside the overlay's own bounds, which is
// why nothing was visibly wrong; it is the shape this pair was
// rewritten to retire. Measured in review of #524.
func (o *Overlay) drawText(f *gooey.Frame, x, y int, s string, st render.Style, cols int) {
	end := x + cols
	render.EachCluster(fit(s, cols), func(cluster string, _, _, w int) bool {
		adv := max(w, 1)
		if x+adv > end {
			return false
		}
		o.setCluster(f, x, y, cluster, w, st)
		x += adv
		return true
	})
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
	x, y int
	// THE WHOLE CELL, not its rune. Ownership is the question this
	// field answers, and a rune cannot answer it: the overlay draws
	// box-drawing corners exactly where a child Border's corner falls
	// and ASCII in the gutters, so the document repainting that cell
	// with the SAME rune in its own style is the ordinary case rather
	// than a coincidence. On a rune match the mark was lifted and the
	// stale snapshot written over content the document had just
	// painted — and that node is clean, so it never came back. The
	// cluster half is the same hole: `wrote` kept only the lead, so an
	// `e` under a written `é` passed too. render.Cell is comparable,
	// which setCluster already relies on at `got == prev[0]`. Raised
	// in review of #524.
	//
	// BOTH COLUMNS, not just the lead, and for a reason the lead
	// cannot cover: ownership is a per-column question once healSeam
	// is in the picture. A foreign NARROW write over a wide mark's
	// lead makes healSeam blank the continuation beside it, in the
	// style that cell holds — ours — so the tail is still the
	// overlay's while the lead is not. Deciding from the lead alone
	// skipped the whole mark and left the guide's background on a
	// column the document owns, with the mark then discarded so it
	// could never be repaired. Raised in review of #524.
	wrote [2]render.Cell
	// EVERY COLUMN THE CLUSTER COVERS, not just the lead. A fixed array
	// because nothing this component draws is wider than two columns and
	// the alternative allocates once per glyph per frame on the paint
	// path; cols says how many of it are live.
	//
	// TWO IS THE CELL PLANE'S OWN CEILING, not this component's taste in
	// glyphs. render.Cell.Width answers 1 or 2 and nothing else, and
	// Buffer.SetCell lays at most one render.Continuation beside a lead
	// — so a mark can never span a third column whatever it is handed.
	// An earlier version of this comment argued the cap from what the
	// track specs happen to contain, which is a fact about this file's
	// fixtures and would stop being true the day a guide drew something
	// else. It is the plane that makes the array safe. Raised in review
	// of #524.
	prev [2]render.Cell
	cols int
}

// ours reports whether column c of this mark still holds what the
// overlay put there.
//
// THE SEAM REPAIR'S OUTPUT IS STILL OURS, which is the arm a whole-cell
// comparison misses. healSeam's `cont && !lead` arm blanks an orphaned
// continuation using the style THAT CELL holds, and on a wide mark
// whose lead a narrow foreign write has taken, that style is the
// overlay's. The result is a blank carrying the guide's background on a
// column the document owns, and the node beneath it is clean — so it
// stays until something unrelated dirties the cell. Measured in review
// of #524.
//
// BOTH ARMS, AND THE GUARD IS ON cols RATHER THAN ON c. healSeam
// blanks the CONTINUATION when a foreign write takes the lead and the
// LEAD when one takes the continuation (render/cell.go), so the injury
// is symmetric. This said "only for c > 0: at c == 0 a blank in our own
// style is what the overlay would have found and declined to write
// over" — which reasons about the PRE-WRITE state. A blanked lead is
// not a cell the overlay declined; it is the lead of its own mark. What
// actually bounds the tolerance is that a seam repair can only blank a
// cell that was half of a PAIR, and a narrow mark's own blank is
// something the overlay wrote and owns outright. Raised in review of
// #524.
func (m mark) ours(got render.Cell, c int) bool {
	if got == m.wrote[c] {
		return true
	}
	return m.cols > 1 && got.Rune == ' ' && got.Cluster == "" &&
		got.Style == m.wrote[c].Style
}

// restoreMarks puts back what the last frame's guide covered up.
//
// A mark is only lifted if the cell STILL HOLDS THE CELL THE OVERLAY
// PUT THERE — rune, cluster and style. Anything else means the
// document repainted that cell in the meantime and now owns it — the
// overlay paints after the tree, so by the time this runs that repaint
// has already happened — and writing the saved content back would be
// restoring a stale copy over live content.
//
// AND ONLY IF EVERY COLUMN OF IT IS STILL REACHABLE, which is the same
// "only touch what you can reach" rule setCluster applies to the write
// and the direction this file did not argue. setCluster reasons about
// the clip WIDENING; a clip that NARROWS between the write and the lift
// — the overlay's bounds shrank at a frame boundary — leaves the tail
// of a wide mark outside it, and a partial lift is worse than none.
// Buffer.SetCell is clip-scoped, so writing prev[0] back lands, healSeam
// repairs the now-orphaned continuation WITH THE STYLE THAT CELL HOLDS
// (the overlay's), and the follow-up SetCell of prev[1] is dropped: a
// blank carrying the guide's background on a cell the guide no longer
// owns, on a node that will not repaint.
//
// Measured in review of #524 — `世` at column 0 under Clip{W:10}, then
// Clip{W:1}, then restoreMarks: column 1 came back Bg{9,9,9} where the
// pre-clear had left Bg{1,2,3}. Skipping leaves the previous frame's
// own writes in place, which is what the composer's bounds sweep
// repaints over (see Arrange's doc); the half-lift leaves a cell
// neither side agrees about.
func (o *Overlay) restoreMarks(f *gooey.Frame) {
	// THE SAME TOLERANCE setCluster CARRIES, and it has to be here
	// rather than only there: Render calls this as its second
	// statement, above every early return, so a frame without cells
	// reached ClipRect before setCluster's guard could run and
	// segfaulted — render.Buffer.ClipRect reads four fields off a nil
	// receiver. Raised in review of #524.
	if f == nil || f.Cells == nil {
		return
	}
	clip := f.Cells.ClipRect()
	for i := len(o.marks) - 1; i >= 0; i-- {
		m := o.marks[i]
		// THE CLIP IS A WHOLE-MARK GATE and ownership is per column.
		// The two questions are not the same one. A clip that no
		// longer holds every column of the mark means the tail write
		// would be DROPPED, so restoring the lead alone half-lifts a
		// pair — that is the case
		// TestAWideMarkIsNotHalfLiftedWhenTheClipNARROWS pins. A
		// column somebody else has taken is a different thing: the
		// columns we still own go back, and nothing is left half
		// anything.
		if m.y < clip.Y || m.y >= clip.Y+clip.H ||
			m.x < clip.X || m.x+m.cols > clip.X+clip.W {
			continue
		}
		// THE LEAD FIRST, THEN THE REST, and the order is the whole of
		// it. Writing the lead makes healSeam repair the orphaned
		// continuation beside it — with the style THAT CELL currently
		// holds, which is the overlay's, so the seam repair leaves a
		// blank carrying the guide's background. Restoring the
		// continuation afterwards is what puts the cell the overlay
		// found back.
		for c := 0; c < m.cols; c++ {
			if !m.ours(f.Cells.At(m.x+c, m.y), c) {
				continue
			}
			f.Cells.SetCell(m.x+c, m.y, m.prev[c])
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
// render.Buffer.SetCell is CLIP-scoped, which is the axis this file
// turns on everywhere else — see setCluster's own doc — so nothing
// here clips. The nil guard is for tests that render without a frame,
// and restoreMarks carries the same one for the same callers.
func (o *Overlay) setCell(f *gooey.Frame, x, y int, r rune, st render.Style) {
	o.setCluster(f, x, y, string(r), render.RuneWidth(r), st)
}

// setCluster is setCell over a whole grapheme cluster, and every write
// in this file goes through it.
//
// ALL OF THE CLUSTER'S COLUMNS MUST BE FREE, not just its first: the
// blank check is what keeps a guide from destroying the content it
// describes, and a wide glyph whose second column is occupied would
// overwrite that neighbour while its own cell looked empty.
//
// WHAT WAS WRITTEN IS READ BACK, rather than assumed, because
// Buffer.SetCell is allowed to write something else: it answers with a
// SPACE where a wide cluster's second column would fall outside the
// clip. Recording the intended rune there would leave restoreMarks
// refusing to lift its own mark.
//
// THE COLUMN COUNT COMES OFF THAT SAME READBACK, and taking it from the
// caller's `w` instead was a real defect rather than a spelling
// preference. At a clip edge the two disagree: SetCell downgrades the
// wide cluster to a space, so the cell is ONE column while `w` still
// says two. Measured on a 10-wide buffer clipped to W=3, drawing 世 at
// x=2 — At(2,0).Width()==1, and the mark claimed cols=2. Cell.Width is
// the same answer SetCell just reached, so the readback settles it.
//
// WHAT AN OVER-CLAIMING MARK COSTS turns on an asymmetry between the
// two buffer calls, and it is worth stating exactly because the obvious
// reading overstates it. render.Buffer.At is BUFFER-scoped
// (render/cell.go, bounded on W and H); render.Buffer.SetCell is
// CLIP-scoped (render/cell.go, bounded on the clip rect). So prev[1]
// snapshots a column this component could not have written and does not
// own — the read reaches where the write cannot — while restoreMarks'
// write back to it is DROPPED for as long as the clip still excludes
// it. That is not a guarantee, because the clip is not a constant: it
// is the component's bounds, re-taken every frame by Composer.build,
// and Unclip widens back out at the end of each. An overlay whose
// bounds grow — the pane widens, the grid extent changes — finds that
// column inside its clip on a later frame, and the stale blank lands on
// whatever the document subtree composed there this time. Recording
// what was actually written removes the question rather than relying on
// a clip staying put.
//
// ONE MARK FOR THE PAIR AND BOTH ITS CELLS IN IT — and the LIFT IS PER
// COLUMN TOO. This said the lift was one decision because "healSeam
// means a foreign write to the lead cannot leave our tail orphaned",
// and orphaned is not the injury: healSeam repairs the orphan with the
// style that cell currently holds, which is the OVERLAY's, so a
// foreign narrow write over the lead leaves the guide's background on
// a column the document owns. Deciding the whole mark from the lead
// skipped it, and restoreMarks clears o.marks whether or not anything
// was lifted, so the snapshot went with it. See mark.ours. Raised in
// review of #524, twice — the second time for the lift.
//
// Measured on this component before the first fix, drawing 世 in the
// guide's style and lifting it again:
//
//	after draw:    c0={世 fg=0,255,0 bg=255,0,0}  c1={Continuation, same}
//	after restore: c0={' ' style unset}           c1={' ' fg=0,255,0 bg=255,0,0}
//
// cursorStyle carries a background, so what survived a lifted mark was a
// highlighted blank cell on a clean node that will not repaint — which
// is the persistence this whole mark model exists for.
//
// The paragraph this replaces said a second cell would be "dead weight"
// and cited TestTheOverlayTakesBackAWideMark for it. That test collects
// .Rune only, so it could not see the style and the claim was true of
// less than it said. It compares whole render.Cell values now. Raised in
// review of #524.
func (o *Overlay) setCluster(f *gooey.Frame, x, y int, cluster string, w int, st render.Style) {
	if f == nil || f.Cells == nil {
		return
	}
	cols := min(max(w, 1), len(mark{}.prev))
	var prev [2]render.Cell
	for c := 0; c < cols; c++ {
		prev[c] = f.Cells.At(x+c, y)
		if !blank(prev[c].Rune) {
			return
		}
	}
	// ONE CONVERSION, because this runs per cell per glyph per frame on
	// the paint path and []rune(cluster) allocates. Raised in review of
	// #524.
	runes := []rune(cluster)
	cell := render.Cell{Rune: runes[0], Style: st}
	if len(runes) > 1 {
		cell.Cluster = cluster
	}
	f.Cells.SetCell(x, y, cell)
	got := f.Cells.At(x, y)
	// A WRITE THE CLIP DROPPED WHOLE LEAVES NO MARK, which is the other
	// half of "only name what landed". Buffer.At is buffer-scoped and
	// Buffer.SetCell is clip-scoped, so a lead column outside the clip
	// comes back unchanged: the mark would claim a cell this overlay
	// never wrote and does not own, and a later frame with a wider clip
	// restores that snapshot over live content. blank() does not close
	// it — a leaf pre-clear writes a STYLED SPACE, whose rune is blank,
	// so the mark is taken and restoreMarks' guard passes. Measured in review of #524: the overlay reverted a
	// neighbour's background on a cell it never touched.
	//
	// The comparison is exact rather than conservative because Cell is
	// comparable and a write that produced an identical cell has
	// nothing to put back either.
	if got == prev[0] {
		return
	}
	// READ BACK, not constructed: the continuation the buffer lays in
	// the second column is its spelling of the pair, and asking it is
	// the same reason the lead is read back rather than assumed.
	var wrote [2]render.Cell
	wrote[0] = got
	if cols > 1 {
		wrote[1] = f.Cells.At(x+1, y)
	}
	o.marks = append(o.marks, mark{
		// CLAMPED TO WHAT WAS SNAPSHOTTED, not only to the array.
		// cols above bounds the FILL; this bounds the COUNT
		// restoreMarks indexes prev by, and the two reach this struct
		// from different sources — cols from EachCluster, this from
		// Cell.Width(), which recomputes through StringWidth. A
		// divergence upward is an index-out-of-range on the paint path
		// at 3, and at 2-against-1 it writes a zero render.Cell the
		// snapshot loop never filled over a neighbour's live content.
		// Neither is reachable today, measured over CJK, ZWJ, both flag
		// forms, VS16, a combining pair, tab and NUL — the bound
		// belongs on the value the array is indexed by anyway. Raised
		// in review of #524.
		x: x, y: y, wrote: wrote, prev: prev, cols: min(max(got.Width(), 1), cols),
	})
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
