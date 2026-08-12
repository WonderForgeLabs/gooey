package main

import (
	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/render"
)

// mirror is what a <Preview> becomes when it appears inside the document
// being previewed.
//
// # Why this exists
//
// The editor's preview pane renders the document. Dropping <Preview>
// into the document therefore made the document contain the thing that
// renders the document: Measure recursed — Canvas.Measure and
// MeasureChild alternating — until the stack overflowed, killing the
// process and leaving the user's terminal unrestored.
//
// The obvious guard is "if my parent is my own type, stop". That is
// insufficient: it catches <Preview><Preview> and misses
// <Preview><Canvas><Preview>, which is the case a user actually reaches,
// because they drop it inside a container. Any same-type ANCESTOR
// recurses, not just the direct parent.
//
// This design makes the question moot rather than answering it. The
// editor keeps two vocabularies (see newEditor): the editor's own
// <Preview> builds the real pane, and the DOCUMENT's <Preview> builds
// this instead. A document's <Preview> is never the real one at any
// depth, so there is no recursion to detect, no ancestor walk, and no
// parent links needed at measure time.
//
// That is also why the palette can keep offering <Preview> honestly. It
// genuinely can be placed; it simply cannot recurse forever. Removing it
// from the palette would have been the editor lying by omission about
// what the document can contain — the failure this project spent its
// length cataloguing.
type mirror struct {
	gooey.Base
	style render.Style
}

// mirrorDepth is how many nested frames the mirror draws before it stops.
//
// THIS IS AN AESTHETIC CHOICE, NOT A SAFETY LIMIT. Nothing recurses here
// — the frames are drawn in one loop — so changing this number cannot
// make anything unsafe and cannot make anything overflow. Raise it for a
// deeper tunnel, lower it for a plainer box. Four reads as "infinity"
// without turning the pane into noise at small sizes.
const mirrorDepth = 4

func (m *mirror) Measure(avail gooey.Size) gooey.Size { return avail }

// Render draws concentric frames, each inset by one cell and dimmer than
// the last, so the pane recedes into itself.
//
// It composes no children on purpose. A version built from nested
// components would be a real tree and would reintroduce exactly the
// depth the crash came from; drawing the illusion in one pass cannot.
func (m *mirror) Render(f *gooey.Frame) {
	b := m.Bounds()
	for d := 0; d < mirrorDepth; d++ {
		inset := d * 2
		x, y := b.X+inset, b.Y+inset
		w, h := b.W-inset*2, b.H-inset*2
		if w < 2 || h < 2 {
			break
		}
		st := m.style
		// Each frame recedes: dim everything past the first, so the
		// nesting reads as depth rather than as stacked boxes.
		if d > 0 {
			st.Dim = true
		}
		top := "╭" + repeat("─", w-2) + "╮"
		bot := "╰" + repeat("─", w-2) + "╯"
		f.Cells.SetString(x, y, top, st)
		f.Cells.SetString(x, y+h-1, bot, st)
		for row := y + 1; row < y+h-1; row++ {
			f.Cells.SetString(x, row, "│", st)
			f.Cells.SetString(x+w-1, row, "│", st)
		}
	}
	// A label in the middle, so the joke lands rather than looking like
	// a rendering fault.
	const label = " preview of the preview "
	if b.W > len(label)+2 && b.H > 2 {
		f.Cells.SetString(b.X+(b.W-len(label))/2, b.Y+b.H/2, label, m.style)
	}
}

func repeat(s string, n int) string {
	if n <= 0 {
		return ""
	}
	out := make([]byte, 0, len(s)*n)
	for i := 0; i < n; i++ {
		out = append(out, s...)
	}
	return string(out)
}
