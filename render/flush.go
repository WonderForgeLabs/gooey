package render

import (
	"strconv"
	"unicode/utf8"
)

// Rect is a rectangle of cells, in cell coordinates.
type Rect struct{ X, Y, W, H int }

// gapMerge is how many unchanged cells are worth re-sending rather than
// jumping over. A cursor-position escape costs six to nine bytes; an
// unchanged cell costs one (plus, in the worst case, two style changes to
// cross it and come back). Four is the point where jumping stops paying.
const gapMerge = 4

// Flusher is the damage-rect flush: it remembers the buffer the terminal
// is currently showing and emits only the spans where the next buffer
// differs from it.
//
// This is the other half of the damage story. The Composer already
// repaints exactly the components that changed, but until now the whole
// buffer still went down the wire every frame — a keystroke in a text box
// cost the same bytes as a hot reload. The diff here is CELL-LEVEL truth
// rather than a replay of the paint-node damage: components overpaint
// each other, containers deliberately do not clear their bounds, and a
// leaf's pre-clear touches cells no damage counter knows about. Comparing
// buffers catches all of it, so correctness never depends on the damage
// count — the count only has to be right for the byte total to be small.
//
// Three things force a full frame, because in each of them the terminal
// is showing something this Flusher did not put there: the first frame,
// a resize (the previous buffer describes a screen that no longer
// exists), and an explicit Invalidate — which is what a host calls after
// handing the terminal to a child process and taking it back, since the
// alternate screen comes back blank.
//
// A Flusher belongs to one Screen and one buffer size, and is used from
// the UI goroutine only.
type Flusher struct {
	prev    *Buffer
	full    bool
	wasFull bool
	damage  []Rect
	touched []Rect
	bytes   int
}

// NewFlusher returns a Flusher whose first Encode is a full frame.
func NewFlusher() *Flusher { return &Flusher{full: true} }

// Invalidate makes the next Encode a full frame. Call it whenever
// something other than this Flusher may have written to the screen.
func (f *Flusher) Invalidate() { f.full = true }

// Damage forces the cells in r to be re-emitted on the next Encode even
// where the buffer says they did not change.
//
// It exists for the pixel plane. A sixel or iTerm2 image lives IN the
// terminal's cell grid, so removing one means writing the cells back over
// it — cells the buffer has held, unchanged, the whole time. The retained
// buffer is still the truth about what those cells should say; the only
// thing that changed is that the terminal stopped agreeing.
func (f *Flusher) Damage(r Rect) {
	if r.W > 0 && r.H > 0 {
		f.damage = append(f.damage, r)
	}
}

// Touched reports the rectangles the last Encode actually wrote, in the
// order it wrote them. The pixel plane needs this: a protocol whose
// images share the cell grid loses an image the moment a cell under it is
// re-sent, so a placement overlapping any of these must be re-emitted.
func (f *Flusher) Touched() []Rect { return f.touched }

// WasFull reports whether the last Encode was a full frame.
func (f *Flusher) WasFull() bool { return f.wasFull }

// Bytes is the size of the last Encode's output — the instrumentation the
// damage-flush tests assert on.
func (f *Flusher) Bytes() int { return f.bytes }

// Encode appends to dst the escape bytes that bring the terminal from the
// last flushed buffer to b, and remembers b as the new terminal state.
// Nothing is appended when nothing changed, not even a style reset: a
// clean frame is zero bytes.
//
// The synchronized-output bracket is NOT included. The caller brackets
// the whole frame, because a frame is cells plus the pixel placements
// that sit on top of them and the terminal must not present the gap.
func (f *Flusher) Encode(dst []byte, b *Buffer, depth ColorDepth) []byte {
	start := len(dst)
	f.touched = f.touched[:0]
	f.wasFull = f.full || f.prev == nil || f.prev.W != b.W || f.prev.H != b.H

	var e emitter
	if f.wasFull {
		dst = append(dst, "\x1b[H"...)
		for y := 0; y < b.H; y++ {
			if y > 0 {
				dst = append(dst, "\r\n"...)
			}
			dst = e.run(dst, b, 0, b.W, y, depth)
		}
		if b.W > 0 && b.H > 0 {
			f.touched = append(f.touched, Rect{0, 0, b.W, b.H})
		}
	} else {
		dst = f.diff(dst, b, depth, &e)
	}

	if e.wrote {
		dst = append(dst, "\x1b[0m"...)
	} else {
		dst = dst[:start]
	}
	f.remember(b)
	f.full = false
	f.damage = f.damage[:0]
	f.bytes = len(dst) - start
	return dst
}

// diff walks each row for spans of changed cells. A span is extended
// across runs of up to gapMerge unchanged cells, so a row where two words
// changed is one cursor move and one write rather than two of each.
func (f *Flusher) diff(dst []byte, b *Buffer, depth ColorDepth, e *emitter) []byte {
	for y := 0; y < b.H; y++ {
		for x := 0; x < b.W; {
			if !f.dirty(b, x, y) {
				x++
				continue
			}
			end := x + 1
			clean := 0
			for j := x + 1; j < b.W; j++ {
				if f.dirty(b, j, y) {
					end, clean = j+1, 0
					continue
				}
				if clean++; clean > gapMerge {
					break
				}
			}
			dst = append(dst, cup(x, y)...)
			dst = e.run(dst, b, x, end, y, depth)
			f.touched = append(f.touched, Rect{x, y, end - x, 1})
			x = end
		}
	}
	return dst
}

func (f *Flusher) dirty(b *Buffer, x, y int) bool {
	i := y*b.W + x
	if f.prev.Cells[i] != b.Cells[i] {
		return true
	}
	for _, r := range f.damage {
		if x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H {
			return true
		}
	}
	return false
}

func (f *Flusher) remember(b *Buffer) {
	// NewBuffer, not a literal: a hand-built Buffer has an empty clip and
	// would discard every Set. This one is only ever written by the copy
	// below, so it would have survived — but leaving the second
	// constructor in the tree is how the next hand-built buffer gets
	// written and silently paints nothing.
	if f.prev == nil || f.prev.W != b.W || f.prev.H != b.H {
		f.prev = NewBuffer(b.W, b.H)
	}
	copy(f.prev.Cells, b.Cells)
}

// emitter writes cell runs, carrying the terminal's SGR state across the
// whole flush. Style survives a cursor move, so a diff that jumps around
// the screen still pays for a style change only when the style actually
// changes.
type emitter struct {
	cur      Style
	styleSet bool
	wrote    bool
}

// run appends cells [x0,x1) of row y. The caller has already positioned
// the cursor.
func (e *emitter) run(dst []byte, b *Buffer, x0, x1, y int, depth ColorDepth) []byte {
	for x := x0; x < x1; x++ {
		c := b.Cells[y*b.W+x]
		if !e.styleSet || c.Style != e.cur {
			dst = append(dst, sgr(c.Style, depth)...)
			e.cur, e.styleSet = c.Style, true
		}
		dst = utf8.AppendRune(dst, c.Rune)
		e.wrote = true
	}
	return dst
}

// cup is CUP: move the cursor to a 1-based row;column.
func cup(x, y int) string {
	return "\x1b[" + strconv.Itoa(y+1) + ";" + strconv.Itoa(x+1) + "H"
}
