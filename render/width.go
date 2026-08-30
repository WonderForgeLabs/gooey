package render

import (
	"strings"

	"github.com/rivo/uniseg"
)

// Display width, and the gap between what this buffer holds and what a
// terminal will draw from it. See issue #358.
//
// # The two models
//
// A Buffer is a grid of cells and every write advances one cell per rune.
// A terminal advances by each glyph's DISPLAY WIDTH, which for CJK and
// most emoji is two columns. Those models agree for ASCII and disagree
// the moment they do not, and the disagreement is invisible to every
// assertion this repo makes: a cell comparison confirms the buffer holds
// what we intended, and the corruption happens when the terminal renders
// it. That is why the functions below exist — a test cannot see this bug
// by looking at cells, because the cells are exactly right.
//
// # Why the width table is a dependency and not a hand-rolled map
//
// East Asian Width plus emoji presentation plus combining marks is not a
// table anybody should write by hand, and grapheme clusters make it
// worse: a flag emoji is two runes and two columns, and a ZWJ family
// sequence is many runes and two columns. Width and clustering have to be
// answered by the same code or they disagree at the joins.
//
// github.com/rivo/uniseg answers both. Judged the way CLAUDE.md asks —
// by what it compiles into rather than by the require count —
// `go list -deps github.com/rivo/uniseg` returns the package and nothing
// else outside the standard library, so this costs one pure-Go module
// with no transitive graph. It is NOT already a core dependency despite
// appearing in vendor/modules.txt: that entry belongs to tools/go.mod,
// which exists precisely to keep tool graphs off importable modules, and
// `go list -deps ./...` on the root module finds no uniseg at all.
//
// go-runewidth was the alternative and is width-only; it would have left
// clustering to a second decision, which the issue asks be avoided.

// RuneWidth reports how many terminal columns r occupies: 0 for a
// combining mark or other zero-width rune, 2 for a wide one, 1 otherwise.
//
// Per RUNE, which is the coarser of the two questions and the wrong one
// to ask about a grapheme cluster — a flag is two regional indicators of
// width 1 each by this function and two columns in total on screen. Use
// StringWidth for anything a user typed; this is for the cell layer,
// where one cell holds one rune by construction.
func RuneWidth(r rune) int {
	return uniseg.StringWidth(string(r))
}

// StringWidth reports how many terminal columns s occupies, counting by
// grapheme cluster so that multi-rune glyphs are measured as drawn.
func StringWidth(s string) int { return uniseg.StringWidth(s) }

// EachCluster walks s one grapheme cluster at a time, handing each its
// byte offset, the terminal COLUMN it starts at, and its own width in
// columns. Returning false stops the walk.
//
// It exists because the column arithmetic belongs here and was being
// re-derived elsewhere: three bytes, one rune and one column are four
// different numbers, and a caller that wants to place something
// alongside text — a highlight, a caret, an underline — needs the
// mapping between them. cmd/finder had its own copy of this loop, and
// the bug it was written to fix (#413: a two-byte character pushing a
// highlight two columns right) is exactly what a caller gets wrong when
// it uses the byte offset as a column.
//
// THE WIDTH IS HANDED OVER RATHER THAN RECOMPUTED, and the first version
// of this function withheld it. uniseg returns the cluster's width on
// the same call that finds its boundary, so keeping it private forced
// the one caller that needed it — matchLine.Render, deciding whether a
// glyph fits — to call StringWidth on every cluster and segment it a
// second time, on the paint path. A signature that hides a number it
// already has does not remove the work, it moves it somewhere slower.
// Found in review of #425.
//
// THE SEGMENTER STATE IS THREADED, which is the part a hand-rolled copy
// tends to drop: passing -1 on every call asks uniseg to re-decide a
// boundary it has just decided, and two walks that disagree about a
// boundary disagree about a width.
//
// ClipCols below deliberately KEEPS ITS OWN LOOP and is not a bug to be
// tidied away. It runs on every paint and is pinned at zero allocations
// by TestClipColsDoesNotAllocate; routing it through a callback puts a
// closure over its two accumulators on that path, which is the exact
// shape the comment inside it records getting wrong once already. It
// also needs to tell "walked off the end" from "stopped early" — the
// difference between returning s untouched and returning a prefix —
// which a bool-returning callback does not express. Two loops, one
// reason each.
func EachCluster(s string, fn func(cluster string, off, col, width int) bool) {
	off, col, state := 0, 0, -1
	rest := s
	for len(rest) > 0 {
		var cluster string
		var cw int
		cluster, rest, cw, state = uniseg.FirstGraphemeClusterInString(rest, state)
		if cluster == "" {
			return
		}
		if !fn(cluster, off, col, cw) {
			return
		}
		off += len(cluster)
		col += cw
	}
}

// ClipCols truncates s to w display COLUMNS, never splitting a wide
// glyph: if the next grapheme cluster would exceed the budget, clipping
// stops before it. That can leave one column unused, which is correct —
// half a glyph is not something a terminal can draw.
//
// Here rather than in each caller because it was about to be written
// twice. components had a private copy for its ~25 paint sites and
// cmd/browser needed the same rule for markdown; a second hand-rolled
// cluster loop is how the two quietly disagree at the joins. A duplicated
// local patch is the signal that an invariant belongs one level up.
//
// This doc comment used to sit ABOVE EachCluster's with no function
// between them, so godoc read the pair as one block belonging to
// EachCluster and ClipCols — the older and more used of the two — was
// documented nowhere. Found in review of #425.
func ClipCols(s string, w int) string {
	if w <= 0 {
		return ""
	}
	// ONE PASS, and the segmenter's state carried across it.
	//
	// This used to pre-scan with StringWidth and then walk the string
	// again, re-deriving the break state from scratch at every cluster
	// by passing -1. Both are the shape SetString was changed away from:
	// the pre-scan segments the whole string to answer a question the
	// walk answers on its way past, and a discarded state asks uniseg to
	// re-decide a boundary it had just decided. Threading it is one
	// variable and removes the question of whether the two agree.
	//
	// The early return for a string that fits survives as a cheap exit
	// from the loop rather than a second traversal in front of it: when
	// nothing was dropped, the original string is returned untouched.
	// Found in review of #413.
	//
	// A BYTE OFFSET, NOT A BUFFER, and the difference is the whole
	// reason this comment is trustworthy. The one-pass rewrite carried
	// the accepted clusters in a make([]byte, 0, len(s)) declared in
	// front of the loop, which ran on EVERY call — so the common case
	// this paragraph calls free allocated once per string per paint,
	// where the two-pass version it replaced allocated nothing. The
	// comment was written about the traversal and read as if it were
	// about the allocation.
	//
	// Nothing needed the buffer. Every accepted cluster is a contiguous
	// prefix of s, so the offset past the last one IS the answer, and
	// slicing is free in both branches. Measured with AllocsPerRun:
	// 1 and 2 allocations before, 0 and 0 after — pinned by
	// TestClipColsDoesNotAllocate, because a claim about allocation that
	// nothing measures is how this one came to be wrong. Found in the
	// review of PR #425.
	cut, used, rest := 0, 0, s
	state := -1
	for len(rest) > 0 {
		var cluster string
		var cw int
		cluster, rest, cw, state = uniseg.FirstGraphemeClusterInString(rest, state)
		if used+cw > w {
			// Something was dropped, so the prefix is the answer.
			return s[:cut]
		}
		cut += len(cluster)
		used += cw
	}
	return s
}

// RowText is what row y would READ AS on a terminal: the runes of the
// row with the continuation markers left out, since a marker is not a
// character the terminal ever received — it is this buffer's record that
// the cell before it covers two columns.
//
// Written for the test helpers, which is not a hedge about its place
// here. Six packages had their own `row(b, y)` building a string cell by
// cell, and every one of them rendered Continuation as a literal rune,
// so an assertion about a row holding a wide glyph read back
// "世\ufffd界\ufffd" and no fixture in the repo could contain one. A
// readback that cannot express what the writer produces makes the whole
// class of wide-glyph bugs unassertable.
func RowText(b *Buffer, y int) string {
	var sb strings.Builder
	for x := 0; x < b.W; x++ {
		sb.WriteString(b.At(x, y).Text())
	}
	return sb.String()
}

// TerminalColumns maps each cell of row y to the display column a
// terminal will start drawing it in. Result[i] is where cell i lands.
//
// THIS IS THE MODEL, and the invariant it exists to check is the simplest
// one available: a buffer column should BE a terminal column, so
// TerminalColumns(b, y)[i] should equal i for every i. Where it does not,
// everything from that cell rightward is displaced by the difference, and
// a component that was arranged at column i paints somewhere else.
//
// A row with no wide glyph satisfies it trivially, which is why the tests
// beside this one build rows that do.
func TerminalColumns(b *Buffer, y int) []int {
	if b == nil || y < 0 || y >= b.H {
		return nil
	}
	cols := make([]int, b.W)
	col := 0
	for x := range b.W {
		cols[x] = col
		if b.At(x, y).Rune == Continuation {
			// Draws nothing and advances nothing: the glyph in the cell
			// before already claimed this column. Flooring this at 1 the
			// way an ordinary cell is floored would double-count it.
			continue
		}
		w := b.At(x, y).Width()
		// A zero-width cell still occupies a cell, and advancing by 0
		// would map two cells to one column and report a displacement
		// that does not exist. The cell layer's floor is one column;
		// zero-width runes are a composition question one level up.
		if w < 1 {
			w = 1
		}
		col += w
	}
	return cols
}

// TerminalWidth reports how many columns row y actually occupies on a
// terminal. Anything past b.W has been pushed off the right edge.
func TerminalWidth(b *Buffer, y int) int {
	cols := TerminalColumns(b, y)
	if len(cols) == 0 {
		return 0
	}
	last := cols[len(cols)-1]
	// The last cell's own advance, through Cell.Width so a trailing
	// Continuation contributes 0. Flooring RuneWidth here counted one
	// column for it — RuneWidth(Continuation) is 1, since string(rune(-1))
	// is U+FFFD — and so a row ENDING in a wide glyph measured one column
	// over. That is invisible in a fixture ending in ASCII, which is
	// exactly what the acceptance test used.
	w := b.At(b.W-1, y).Width()
	if w < 1 && b.At(b.W-1, y).Rune != Continuation {
		w = 1
	}
	return last + w
}

// Displaced reports the first cell of row y whose terminal column is not
// its own index, and how far it has moved. ok is false when the row is
// faithful — every cell drawn where the buffer says it is.
//
// Returned rather than asserted so a caller can say WHICH cell and by how
// much: "the row is wrong" is not a useful failure message when the point
// is that everything after one glyph shifted.
func Displaced(b *Buffer, y int) (x, by int, ok bool) {
	// NIL-TOLERANT, like TerminalColumns and TerminalWidth above it. The
	// loop below is already safe — TerminalColumns answers nil, so it
	// does not run — but the last-column check dereferences b.W, and a
	// harness that renders without a frame would segfault on the one
	// instrument the #358 docs tell every custom-Render author to reach
	// for. Found in review of #425.
	if b == nil {
		return 0, 0, false
	}
	for i, c := range TerminalColumns(b, y) {
		// A continuation cell draws nothing, so it has no column of its
		// own to be displaced from. Its recorded column is where the
		// terminal's cursor sits mid-glyph, which is legitimately not i.
		if b.At(i, y).Rune == Continuation {
			continue
		}
		if c != i {
			return i, c - i, true
		}
	}
	// A WIDE LEAD IN THE LAST COLUMN IS A DISPLACEMENT NOTHING ABOVE CAN
	// SEE. Every cell is at its own index — there is no cell after it to
	// have been pushed — but the glyph needs a column the buffer does
	// not have, so the terminal either wraps it to column 0 of the next
	// row or drops it. Either way the row is not what the buffer says.
	//
	// The loop cannot catch it because displacement is defined against
	// the NEXT cell, and here the overflow runs off the end. Measuring
	// the row's own width is what turns "one cell short" into a number:
	// TerminalWidth already reports 4 for a 3-wide buffer holding a
	// wide lead at column 2.
	//
	// Only reachable by assigning Cells directly now — Set and SetString
	// both write a space rather than a lead they cannot complete — which
	// is exactly why the model has to be able to say so. A sharp edge the
	// instruments cannot see is not documented, it is hidden. Found in
	// review of #413.
	if w := TerminalWidth(b, y); w > b.W {
		return b.W - 1, w - b.W, true
	}
	return 0, 0, false
}
