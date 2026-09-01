package gooey

import (
	"github.com/WonderForgeLabs/gooey/graphics"
	"github.com/WonderForgeLabs/gooey/render"
	"github.com/WonderForgeLabs/gooey/term"
)

// The pixel plane under damage tracking.
//
// Cells have it easy: the buffer is retained, so a component that did not
// repaint still has its cells sitting there and the flush diff compares
// what is against what was. Pixel placements have no such buffer. They are
// recorded during Render, and only dirty components render — so the
// Composer has to be the retained store for them, keyed by the paint node
// that recorded each one.
//
// From that, everything else is a diff between two lists per node:
//
//   - a component repaints and records the same image at the same place:
//     nothing goes on the wire
//   - same image, new rectangle: a MOVE, which a protocol with placement
//     identity does with one control sequence
//   - different image, or a slot that did not exist before: a transmission
//   - a slot that no longer exists — the component turned Hidden, painted
//     fewer images, or vanished from the tree in a Dynamic re-sync: a
//     REMOVAL
//
// Removal is where the protocols stop agreeing. Kitty keeps images on its
// own plane and can delete one by id. Sixel and iTerm2 write pixels into
// the cell grid, and the only way to erase them is to write cells over
// them — which is exactly what render.Flusher.Damage forces, using the
// retained cell buffer that has held the correct content all along. The
// same asymmetry runs the other way: for those protocols any cell the
// flush re-sends erases whatever image was sitting on it, so a surviving
// placement whose rectangle intersects the flush's touched spans has to be
// re-emitted.

// shownPlacement is one placement the terminal is currently showing, with
// the protocol-level id we gave it.
type shownPlacement struct {
	id int
	p  graphics.Placement
}

type placeKind int

const (
	placeAdd     placeKind = iota // new slot: transmit and display
	placeReplace                  // same slot, different image
	placeMove                     // same image, different rectangle
	placeRemove                   // slot is gone
	placeRefresh                  // unchanged, but the terminal lost it
)

type placeOp struct {
	kind placeKind
	id   int
	p    graphics.Placement // the new rectangle (the old one for placeRemove)
}

// placementOps diffs every node's recorded placements against what the
// terminal is showing, updates the shown state to match, and returns the
// work. Slots that did not change are returned separately as `kept`,
// because whether they need re-emitting is not knowable until the cell
// flush has run.
//
// Removals and moves under a protocol without placement identity damage
// the cells they vacate, so this must run BEFORE the cell flush encodes.
func (c *Composer) placementOps() (ops []placeOp, kept []shownPlacement) {
	if c.frame.Graphics == nil {
		return nil, nil // halfblock: pixel content became cells, and cells diff themselves
	}
	_, byID := c.frame.Graphics.(graphics.IDEncoder)

	// Nodes that left the tree in a re-sync took their images with them.
	for _, s := range c.gonePlacements {
		ops = append(ops, placeOp{kind: placeRemove, id: s.id, p: s.p})
		if !byID {
			c.flusher.Damage(cellRect(s.p))
		}
	}
	c.gonePlacements = c.gonePlacements[:0]

	// PAINT ORDER, not document order, and the two stopped being the same
	// thing when Overlay arrived.
	//
	// For sixel and iterm2 — and for kitty placements of equal z — the
	// order these ops are EMITTED is the order the terminal stacks them.
	// Iterating c.nodes therefore left the pixel plane stacking in
	// document order while the cell plane stacked in overlay order, and
	// Frame's own republish (composer.go, "Republish the pixel plane in
	// paint order") already used c.paint. So an overlay whose draw func
	// calls Frame.Place — a popup surface's is app-supplied and may place
	// an image — landed UNDER a later ordinary sibling's image on the
	// live path and OVER it in the *Frame handed to Frame.Flush, a test
	// or a screenshot. Frame.Flush's doc calls the two planes "a single
	// frame"; this is what made them two.
	//
	// c.paint is a permutation of c.nodes — orderPaint partitions every
	// node into it — so this visits each node exactly once, as before.
	// Found in review of #437.
	for _, n := range c.paint {
		was := n.shown
		now := make([]shownPlacement, 0, len(n.places))
		for i, p := range n.places {
			if i >= len(was) {
				c.nextPlaceID++
				now = append(now, shownPlacement{id: c.nextPlaceID, p: p})
				ops = append(ops, placeOp{kind: placeAdd, id: c.nextPlaceID, p: p})
				continue
			}
			old := was[i]
			switch {
			case old.p.SameImage(p) && old.p.SameSpot(p):
				kept = append(kept, old)
			case old.p.SameImage(p):
				ops = append(ops, placeOp{kind: placeMove, id: old.id, p: p})
				if !byID {
					c.flusher.Damage(cellRect(old.p))
				}
			default:
				ops = append(ops, placeOp{kind: placeReplace, id: old.id, p: p})
				if !byID && !old.p.SameSpot(p) {
					c.flusher.Damage(cellRect(old.p))
				}
			}
			now = append(now, shownPlacement{id: old.id, p: p})
		}
		for _, old := range was[min(len(n.places), len(was)):] {
			ops = append(ops, placeOp{kind: placeRemove, id: old.id, p: old.p})
			if !byID {
				c.flusher.Damage(cellRect(old.p))
			}
		}
		n.shown = now
	}
	return ops, kept
}

// refresh appends the re-emissions the cell flush made necessary.
//
// Two reasons a placement the component did not change still has to go
// back on the wire. A full frame means the terminal is not showing what we
// think it is at all — it was just resized, or handed back after a child
// process had it — so nothing survives, images included. And for a
// protocol whose pixels live in the cell grid, any span the flush wrote
// erased the part of an image underneath it.
func (c *Composer) refresh(ops []placeOp, kept []shownPlacement) []placeOp {
	if len(kept) == 0 {
		return ops
	}
	_, byID := c.frame.Graphics.(graphics.IDEncoder)
	full := c.flusher.WasFull()
	for _, s := range kept {
		if !full && byID {
			continue // kitty images survive any amount of text under them
		}
		if !full && !touches(c.flusher.Touched(), cellRect(s.p)) {
			continue
		}
		ops = append(ops, placeOp{kind: placeRefresh, id: s.id, p: s.p})
	}
	return ops
}

// encodePlacements turns the diff into protocol bytes. The cursor is
// positioned per placement because every protocol draws at the cursor.
func (c *Composer) encodePlacements(ops []placeOp) ([]byte, error) {
	enc := c.frame.Graphics
	if enc == nil || len(ops) == 0 {
		return nil, nil
	}
	id, byID := enc.(graphics.IDEncoder)
	var out []byte
	var firstErr error
	transmit := func(p graphics.Placement, slot int) {
		out = append(out, cursorTo(p.Col, p.Row)...)
		var err error
		if byID {
			err = id.Transmit(&out, slot, p.Img, p.Cols, p.Rows, c.frame.CellW, c.frame.CellH)
		} else {
			err = enc.Encode(&out, p.Img, p.Cols, p.Rows, c.frame.CellW, c.frame.CellH)
		}
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	for _, op := range ops {
		switch op.kind {
		case placeRemove:
			if byID {
				id.Delete(&out, op.id, true)
			}
			// Without ids, the vacated cells were damaged instead: the
			// cell flush has already written over the image.
		case placeMove:
			if byID {
				id.Delete(&out, op.id, false) // placements only; the pixels stay put
				out = append(out, cursorTo(op.p.Col, op.p.Row)...)
				id.Place(&out, op.id, op.p.Cols, op.p.Rows)
				continue
			}
			transmit(op.p, op.id)
		case placeReplace:
			if byID {
				id.Delete(&out, op.id, true)
			}
			transmit(op.p, op.id)
		case placeAdd, placeRefresh:
			transmit(op.p, op.id)
		}
	}
	return out, firstErr
}

func cellRect(p graphics.Placement) render.Rect {
	return render.Rect{X: p.Col, Y: p.Row, W: p.Cols, H: p.Rows}
}

func touches(rects []render.Rect, r render.Rect) bool {
	for _, o := range rects {
		if o.X < r.X+r.W && r.X < o.X+o.W && o.Y < r.Y+r.H && r.Y < o.Y+o.H {
			return true
		}
	}
	return false
}

// EncoderFor is the protocol a capability set calls for, most capable
// first, or nil for the halfblock fallback. It is the same ladder
// term.Caps.Best names, resolved to the encoder itself.
func EncoderFor(c term.Caps) graphics.Encoder {
	switch {
	case c.Kitty:
		return graphics.Kitty{}
	case c.Sixel:
		return graphics.Sixel{}
	case c.ITerm2:
		return graphics.ITerm2{}
	default:
		return nil
	}
}
