// Package render provides the cell plane: a buffer of styled character
// cells and the ANSI diff/flush path that puts them on screen.
package render

import "github.com/rivo/uniseg"

// Color is a 24-bit RGB color. Zero value means "terminal default".
type Color struct {
	R, G, B uint8
	Set     bool
}

func RGB(r, g, b uint8) Color { return Color{r, g, b, true} }

type Style struct {
	Fg, Bg    Color
	Bold      bool
	Dim       bool
	Underline bool
	Reverse   bool
}

type Cell struct {
	Rune rune
	// Cluster is the WHOLE grapheme cluster when it is more than one
	// rune — "e\u0301", "\u26a0\ufe0f", a ZWJ emoji sequence — and empty
	// otherwise, which is the overwhelmingly common case.
	//
	// It exists because the width and the content have to come from the
	// same thing. Storing only the cluster's first rune reserved cells by
	// the CLUSTER's width and drew a glyph of the FIRST RUNE's width:
	// "\u26a0\ufe0f" is two columns, U+26A0 alone is one, so the row was
	// displaced in the opposite direction from the bug #358 exists to
	// fix. The same truncation silently dropped every combining mark —
	// decomposed "e\u0301" painted as "e".
	//
	// A string keeps Cell comparable, so == still works and every
	// Cell{Rune: 'x'} literal still means what it did.
	Cluster string
	Style   Style
}

// Text is what the terminal receives for this cell: the full cluster when
// there is one, nothing at all for a continuation (the glyph before it
// already drew this column), and the plain rune otherwise.
func (c Cell) Text() string {
	if c.Rune == Continuation {
		return ""
	}
	if c.Cluster != "" {
		return c.Cluster
	}
	return string(c.Rune)
}

// WithStyle is this cell's CONTENT under a different style.
//
// It exists because restyling is a read-modify-write and the obvious
// spelling of it loses information. `c := buf.At(x, y); c.Style.Reverse =
// on; buf.Set(x, y, c.Rune, c.Style)` drops c.Cluster, so a cell holding
// "⚠️" comes back as bare U+26A0 — one column where the buffer
// had reserved two, which displaces the row in the direction #358 exists
// to prevent. Selection highlighting is exactly this shape, so the glyph
// narrows the moment a row is selected and repairs itself when it is not.
//
// Four call sites had written it the lossy way (ItemsView's highlight,
// introdeck's terminal twice, the wysiwyg overlay's mark restore). That
// they were four copies of one missing operation is why this is a method
// rather than a fix in each of them. Found in review of #413.
func (c Cell) WithStyle(s Style) Cell {
	c.Style = s
	return c
}

// Width is how many columns a terminal advances for this cell.
//
// Zero for a continuation, which is the rule TerminalColumns spells out
// and the one that is easy to get wrong from the outside: RuneWidth of
// the Continuation sentinel is 1, because string(rune(-1)) is U+FFFD.
// Asking the cell rather than its rune is what keeps that from being
// re-derived — and mis-derived — at every call site.
func (c Cell) Width() int {
	if c.Rune == Continuation {
		return 0
	}
	// ASCII FAST PATH. A printable ASCII rune is one column and cannot
	// carry a combining mark on its own, so the segmenter has nothing to
	// decide — and this is the overwhelmingly common cell. Worth having
	// because Set now asks TWICE per write, once for itself and once
	// through healSeam, on a path that runs for every character painted.
	//
	// Guarded on Cluster being empty as well as on the rune: a cell whose
	// cluster is "é" holds an ASCII lead and is not what Text()
	// returns. Control characters and DEL fall through rather than being
	// assumed, since their width is a terminal's business.
	if c.Cluster == "" && c.Rune >= 0x20 && c.Rune < 0x7f {
		return 1
	}
	return StringWidth(c.Text())
}

// Buffer is a W×H grid of cells — one frame of the cell plane.
type Buffer struct {
	W, H  int
	Cells []Cell

	// clip is the half-open region Set may write, ALWAYS inside the
	// buffer — Clip intersects, so a clip can only ever narrow. That
	// invariant is what lets Set test the clip INSTEAD of the buffer
	// rather than as well as it, which is why clipping costs nothing:
	// the same four comparisons that already bounded every write now
	// bound it to the painting component instead of to the screen.
	//
	// Use NewBuffer. A zero-value Buffer has an empty clip and silently
	// discards every write — which is why remember() below was changed
	// to stop building one by hand.
	cx0, cy0, cx1, cy1 int
}

func NewBuffer(w, h int) *Buffer {
	b := &Buffer{W: w, H: h, Cells: make([]Cell, w*h), cx1: w, cy1: h}
	b.Clear()
	return b
}

// Clip narrows the writable region to r and returns what it was, so the
// caller can put it back. The Composer brackets every component's Render
// with a pair of these.
//
// It INTERSECTS with the current clip rather than replacing it, so
// nesting can only ever narrow — a component cannot widen its way out to
// a neighbour's cells by clipping to something larger than it was given.
func (b *Buffer) Clip(r Rect) Rect {
	prev := b.ClipRect()
	b.cx0, b.cy0 = max(b.cx0, r.X), max(b.cy0, r.Y)
	b.cx1, b.cy1 = min(b.cx1, r.X+r.W), min(b.cy1, r.Y+r.H)
	// An empty intersection must stay empty rather than invert, or the
	// comparisons in Set would admit everything.
	b.cx1, b.cy1 = max(b.cx0, b.cx1), max(b.cy0, b.cy1)
	return prev
}

// Unclip restores a region taken from Clip. Separate from Clip because
// restoring must NOT intersect: the whole point is to widen back out.
func (b *Buffer) Unclip(r Rect) {
	b.cx0, b.cy0, b.cx1, b.cy1 = r.X, r.Y, r.X+r.W, r.Y+r.H
}

// ClipRect is the region Set will currently accept.
func (b *Buffer) ClipRect() Rect {
	return Rect{X: b.cx0, Y: b.cy0, W: b.cx1 - b.cx0, H: b.cy1 - b.cy0}
}

func (b *Buffer) Clear() {
	for i := range b.Cells {
		b.Cells[i] = Cell{Rune: ' '}
	}
}

// Set writes one cell, and DROPS the write when it falls outside the
// current clip.
//
// Dropping rather than growing the buffer is the whole fix for #357. The
// cells beyond a component's rect belong to its neighbours, and those
// neighbours are CLEAN — their paint nodes did not invalidate — so
// nothing ever repaints over a stray write. It survives until something
// unrelated dirties the victim, which is why the symptom shows up as
// "stray characters in a pane that never fixes itself".
//
// The clip is kept inside the buffer by Clip, so testing it also tests
// the buffer: four comparisons, exactly as many as bounding to the
// screen alone used to cost.
//
// IT IS SetCell, and that is the point rather than a shortcut. The two
// were the same 25 lines twice — same clip test, same wide-lead-at-the-
// edge space, same continuation write, same healSeam pair — differing
// only in SetCell refusing a Continuation. This PR exists because two
// writers disagreed about one rule, and the review of PR #425 pointed
// out that a third copy of that rule is the same trap set again: the
// copy a future clip or seam change has to remember to edit twice.
//
// Collapsing also gives Set SetCell's refusal, which it should always
// have had. Nothing in the repo passes render.Continuation to Set, so
// this closes a door rather than changing behaviour — but the door was
// open, and through it a caller could place the orphan healSeam exists
// to remove.
func (b *Buffer) Set(x, y int, r rune, s Style) {
	b.SetCell(x, y, Cell{Rune: r, Style: s})
}

// SetCell writes a whole cell — cluster and all — under exactly the clip
// and seam rules Set uses.
//
// Set takes a rune, which is the right argument for painting text and the
// wrong one for moving a cell that already exists: a cluster cannot be
// spelled as a rune, so every restyle through Set silently truncated one.
// See WithStyle for what that costs.
func (b *Buffer) SetCell(x, y int, c Cell) {
	if x < b.cx0 || y < b.cy0 || x >= b.cx1 || y >= b.cy1 {
		return
	}
	// A Continuation is written only as the tail of the pair its lead
	// writes; accepting one on its own would place the orphan healSeam
	// exists to remove.
	//
	// SO SetCell CANNOT ROUND-TRIP EVERY CELL At HANDS BACK, which is
	// the one thing to know before using it to restyle a SPAN. Over a
	// run of cells — `SetCell(x, y, At(x, y).WithStyle(st))` in a loop,
	// which is what a row highlight does — the second column of every
	// wide glyph is refused, so it keeps its old style.
	//
	// That is invisible in the output and real in the model. The flusher
	// skips continuation cells (emitter.run), so their style is never
	// emitted and the lead's covers both columns; what it costs is that
	// the buffer no longer says what the screen shows, and a caller that
	// un-highlights by comparing styles finds Reverse still set on cells
	// it thought it had cleared. Found in the review of PR #425.
	if c.Rune == Continuation {
		return
	}
	// A WIDE RUNE OWNS TWO COLUMNS, so a write has to place both of them.
	//
	// Writing only the lead left a wide cell with no Continuation beside
	// it, which is precisely the broken pair healSeam exists to repair —
	// so healSeam blanked the glyph one line after it was written, and a
	// caller could not put a CJK or emoji rune on the screen at all.
	// Every caller reaching for the single-rune form got a space: TextBox
	// drawing its own runes, cmd/finder, the mnemonic underline painters.
	// Found in review of #413, after the healSeam it collides with had
	// already landed.
	if c.Width() >= 2 {
		// The second column has to be inside the CLIP, not merely inside
		// the buffer. Outside it the cell belongs to a neighbour whose
		// paint node is clean — the stray-write defect every writer here
		// drops writes to avoid — and a lead without its tail displaces
		// the rest of the row either way. A space is the honest answer,
		// and the one SetString already gives at the right edge.
		if x+1 >= b.cx1 {
			b.Cells[y*b.W+x] = Cell{Rune: ' ', Style: c.Style}
			b.healSeam(x, y)
			b.healSeam(x+1, y)
			return
		}
		b.Cells[y*b.W+x] = c
		b.Cells[y*b.W+x+1] = Cell{Rune: Continuation, Style: c.Style}
		b.healSeam(x, y)
		b.healSeam(x+2, y)
		return
	}
	b.Cells[y*b.W+x] = c
	b.healSeam(x, y)
	b.healSeam(x+1, y)
}

// healSeam restores the one invariant that makes a wide glyph safe to
// overpaint: the cell at x is a Continuation EXACTLY WHEN the cell before
// it is a wide lead. Either half alone is a corrupt row.
//
// This matters because the framework's paint model is overpainting —
// flush.go:25 says it outright: "components overpaint each other,
// containers deliberately do not clear their bounds, and a leaf's
// pre-clear touches cells no damage counter knows about". With one rune
// per cell that model was closed under any single write. With wide glyphs
// it is not: writing over a lead leaves an orphan Continuation, which the
// flusher skips forever, so that column can never be repainted; writing
// over a Continuation leaves a lead whose glyph displaces everything
// after it on the row.
//
// The surviving half becomes a space rather than being left alone,
// because a space is the only value that both draws correctly and
// occupies exactly the one column the cell owns.
// # The seam is the ONE place a repair may write outside the clip
//
// PR #425 scoped this function to the clip, and its comment then claimed
// the unrepairable case could not arise because Set, SetCell and
// SetString all refuse to leave a lead in their own last column. The
// review of that PR showed the claim covers the wrong neighbour: it is
// true of a NARROWER writer beside us, and says nothing about an
// ENCLOSING paint — an ancestor container, an AdornmentLayer, a
// ToastHost — whose own clip legitimately spanned both columns of a wide
// glyph that a descendant's narrower clip then cuts through.
//
// Both halves were reproducible on that branch. Clip{0,10}, write 世 at
// column 4; then Clip{5,10}, write 'a' at column 5: column 4 is a lead
// with no tail, the row measures 11 columns on a 10-wide buffer, and
// every glyph after it is displaced. Mirror it — Clip{0,5}, write at
// column 4 over a lead — and column 5 keeps a Continuation the flusher
// skips forever.
//
// SO THE REPAIR REACHES ONE COLUMN PAST THE CLIP, deliberately, and it
// is the only write in this file that does. The reasoning is that the
// cell it touches is ALREADY corrupt, and corrupt because of the write
// we just made: a lead with no tail displaces the entire rest of the
// row, and an orphan Continuation is a column that can never be
// repainted by anything. A one-cell space is a smaller injury to a
// neighbour than either, and unlike either it is confined to the column
// the broken glyph already owned.
//
// The residual is real and worth stating: that neighbour's node is
// clean, so it will not repaint, and its glyph stays a space until
// something else dirties it. That is the honest outcome of two
// components disagreeing about who owns a column — the row can only be
// one thing, and it is not this function's job to decide the layout was
// wrong.
func (b *Buffer) healSeam(x, y int) {
	// THE CLIP, not the buffer, for the same reason every writer below
	// tests it: a repair outside this component's rect is still a write
	// to a neighbour's cells, and the neighbour's node is clean so
	// nothing repaints over it.
	//
	// The case that cannot then be repaired also cannot arise. It would
	// need a neighbour to have left a wide LEAD in its own last column
	// with the continuation past its clip — and Set, SetCell and
	// SetString all refuse exactly that, writing a space instead. So the
	// dangling half this declines to fix is unreachable rather than
	// tolerated — EXCEPT at the seam itself, which is the one column on
	// each side of the clip and the subject of the paragraph above.
	if y < b.cy0 || y >= b.cy1 || x < b.cx0 || x > b.cx1 || x >= b.W {
		return
	}
	i := y*b.W + x
	// x > 0, not x > b.cx0: the pair this inspects may STRADDLE the left
	// clip edge, and refusing to look at the cell before it is what left
	// the straddling case unrepaired.
	lead := x > 0 && b.Cells[i-1].Width() >= 2
	cont := b.Cells[i].Rune == Continuation
	switch {
	case lead && !cont:
		b.Cells[i-1] = Cell{Rune: ' ', Style: b.Cells[i-1].Style}
	case cont && !lead:
		b.Cells[i] = Cell{Rune: ' ', Style: b.Cells[i].Style}
	}
}

// put writes a cell without repairing seams — for SetString, which writes
// whole pairs itself and repairs only the two edges of its run. Going
// through Set would make the Continuation it writes at x+1 look like an
// overpaint of the lead it wrote at x one call earlier.
//
// IT TESTS THE CLIP, and until PR #425 it tested only the buffer — so
// SetString, the framework's primary text writer, wrote straight through
// the rect Composer.build brackets every Render with. Two writers then
// disagreed about one rule: Set and SetCell refused a wide glyph whose
// second column fell outside the clip while SetString wrote it, leaving
// a Continuation one column past the clip in a cell belonging to a clean
// node that never repaints over it. The clip-blindness predated the PR;
// what the PR added was the disagreement, and a new unclipped write in
// the left-straddle branch below.
//
// Clip narrows only (see Clip), so testing it also tests the buffer.
func (b *Buffer) put(x, y int, c Cell) {
	if x < b.cx0 || y < b.cy0 || x >= b.cx1 || y >= b.cy1 {
		return
	}
	b.Cells[y*b.W+x] = c
}

func (b *Buffer) At(x, y int) Cell {
	if x < 0 || y < 0 || x >= b.W || y >= b.H {
		return Cell{Rune: ' '}
	}
	return b.Cells[y*b.W+x]
}

// Continuation is the Rune held by the cell a wide glyph's SECOND column
// covers. It is not drawn — the terminal already advanced two columns
// when it drew the glyph in the cell before — and it exists so that a
// buffer column keeps meaning a terminal column.
//
// A negative rune because every value a caller could legitimately write
// is non-negative, so this cannot collide with content. It is emphatically
// not a space: a space would erase the right half of the glyph.
const Continuation rune = -1

// SetString writes a string starting at (x,y), clipped to the buffer, and
// advances by each glyph's DISPLAY WIDTH rather than one cell per rune.
//
// The invariant it maintains is that cell index equals terminal column
// (see width.go). A double-width glyph takes the cell it is written to
// AND marks the next one Continuation, so the text after it starts at the
// column layout actually allocated. Before this, a wide glyph consumed
// one cell and two columns, and everything to its right was displaced —
// invisibly, because the cells were exactly what we asked for (#358).
//
// By grapheme cluster, not by rune: a flag is two regional indicators and
// two columns, and a ZWJ sequence many runes and two columns. Iterating
// runes would write each piece to its own cell and measure the whole
// thing wrong.
func (b *Buffer) SetString(x, y int, str string, s Style) {
	if y < 0 || y >= b.H {
		return
	}
	start := x
	// uniseg's contract is -1 on the first call and the returned state
	// thereafter. Passing -1 every time re-derives the break state from
	// scratch at each cluster; probing flags, ZWJ families and the
	// rainbow flag found no divergence, but threading it is one line and
	// removes the question.
	state := -1
	for len(str) > 0 {
		var cluster string
		var w int
		cluster, str, w, state = uniseg.FirstGraphemeClusterInString(str, state)
		if cluster == "" {
			continue
		}
		if x >= b.cx1 {
			break
		}
		// Clip on the LEFT as well as the right. Set bounds-checks, so
		// before this the lead of a straddling glyph was swallowed and
		// its Continuation still landed in cell 0 — an orphan marking a
		// column the flusher then skips forever. Skipping by the
		// cluster's width keeps the columns of what follows correct.
		if x < b.cx0 {
			// A cluster STRADDLING the left edge still covers columns
			// this string is responsible for, and cannot draw in them —
			// half a glyph is not something a terminal renders. Skipping
			// them outright left whatever was underneath, so a line
			// scrolled one column right showed the previous frame's
			// character in the gap and nothing ever repainted it.
			//
			// The visible columns are those from the clip's left edge to
			// where the cluster ends — cx0 rather than 0, because the
			// left edge this string straddles is the component's, not
			// the buffer's. Written before PR #425 as a bare 0, which
			// blanked column 0 of the SCREEN whatever rect the caller
			// was clipped to. Found in review of #413, then in the
			// review of that fix.
			for c := b.cx0; c < x+max(w, 1); c++ {
				b.put(c, y, Cell{Rune: ' ', Style: s})
			}
			x += max(w, 1)
			continue
		}
		c := Cell{Rune: []rune(cluster)[0], Style: s}
		if len([]rune(cluster)) > 1 {
			c.Cluster = cluster
		}
		if w >= 2 {
			// No room for the second half: drawing it would overflow the
			// line and put the glyph's tail in column 0 of the next row
			// on a terminal with autowrap. A space is the honest answer.
			//
			// The CLIP, matching Set and SetCell. Against b.W the three
			// writers disagreed wherever a component's rect was narrower
			// than the screen, which is every component but the root.
			if x+1 >= b.cx1 {
				b.put(x, y, Cell{Rune: ' ', Style: s})
				x++
				break
			}
			b.put(x, y, c)
			b.put(x+1, y, Cell{Rune: Continuation, Style: s})
			x += 2
			continue
		}
		b.put(x, y, c)
		x++
	}
	// Only the two edges of the run can have broken a pair; everything
	// between them this call wrote itself.
	b.healSeam(start, y)
	b.healSeam(x, y)
}
