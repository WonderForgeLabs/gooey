// Package gooey — POC of the retained visual tree / component model.
//
// The tree is retained: widgets are persistent objects with parents,
// children, and computed bounds. A frame is produced by the classic
// two-pass layout (Measure bottom-up, Arrange top-down) followed by a
// Render walk. Pixel content never enters the cell buffer — widgets
// record graphics.Placements on the Frame, and the flush composites the
// two planes (cells first, then pixel placements over them).
package gooey

import (
	"io"

	"github.com/WonderForgeLabs/gooey/graphics"
	"github.com/WonderForgeLabs/gooey/render"
)

type Size struct{ W, H int }

type Rect struct{ X, Y, W, H int }

// Widget is the component model. Everything in the tree implements it.
type Widget interface {
	// Measure returns the size the widget wants within avail.
	Measure(avail Size) Size
	// Arrange assigns final bounds (and arranges children).
	Arrange(bounds Rect)
	// Render paints THIS widget only into the frame using the bounds
	// from Arrange; children are walked by the framework via Container.
	Render(f *Frame)
}

// Container is implemented by widgets with children. The framework —
// not the widget — walks them, so the Composer can give every widget
// its own paint node.
type Container interface{ ChildWidgets() []Widget }

// Frame is one composed frame: the cell plane plus deferred pixel
// placements. Graphics is nil when the terminal has no pixel protocol —
// widgets with pixel content must then degrade into cells (halfblock).
type Frame struct {
	Cells        *render.Buffer
	Graphics     graphics.Encoder
	Placements   []graphics.Placement
	CellW, CellH int
}

// Compose lays out root into a fresh frame of cols×rows — the one-shot
// path (full repaint). The damage-tracked path is Composer.
func Compose(root Widget, cols, rows int, enc graphics.Encoder, cellW, cellH int) *Frame {
	f := &Frame{Cells: render.NewBuffer(cols, rows), Graphics: enc, CellW: cellW, CellH: cellH}
	root.Measure(Size{cols, rows})
	root.Arrange(Rect{0, 0, cols, rows})
	renderTree(root, f)
	return f
}

func renderTree(w Widget, f *Frame) {
	if l := layoutOf(w); l != nil && l.Visibility == Collapsed {
		return // collapsed subtrees paint nothing at all
	}
	if paintable(w) {
		w.Render(f)
	}
	if c, ok := w.(Container); ok {
		for _, ch := range c.ChildWidgets() {
			renderTree(ch, f)
		}
	}
}

// Flush writes the frame: cell plane first, then pixel placements.
func (f *Frame) Flush(w io.Writer) error {
	if err := render.Flush(w, f.Cells); err != nil {
		return err
	}
	for _, p := range f.Placements {
		// Position cursor at the placement cell (1-based), emit protocol bytes.
		var out []byte
		out = append(out, []byte(cursorTo(p.Col, p.Row))...)
		if err := f.Graphics.Encode(&out, p.Img, p.Cols, p.Rows, f.CellW, f.CellH); err != nil {
			return err
		}
		if _, err := w.Write(out); err != nil {
			return err
		}
	}
	return nil
}

func cursorTo(col, row int) string {
	return "\x1b[" + itoa(row+1) + ";" + itoa(col+1) + "H"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [8]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
