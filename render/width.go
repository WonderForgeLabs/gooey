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
func ClipCols(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if StringWidth(s) <= w {
		return s
	}
	out, used, rest := make([]byte, 0, len(s)), 0, s
	for len(rest) > 0 {
		cluster, next, cw, _ := uniseg.FirstGraphemeClusterInString(rest, -1)
		if used+cw > w {
			break
		}
		out = append(out, cluster...)
		used += cw
		rest = next
	}
	return string(out)
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
		if r := b.At(x, y).Rune; r != Continuation {
			sb.WriteRune(r)
		}
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
		r := b.At(x, y).Rune
		if r == Continuation {
			// Draws nothing and advances nothing: the glyph in the cell
			// before already claimed this column. Flooring this at 1 the
			// way an ordinary cell is floored would double-count it.
			continue
		}
		w := RuneWidth(r)
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
	w := RuneWidth(b.At(b.W-1, y).Rune)
	if w < 1 {
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
	return 0, 0, false
}
