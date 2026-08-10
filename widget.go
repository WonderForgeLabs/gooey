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
	"github.com/WonderForgeLabs/gooey/term"
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
//
// Caps is the terminal's detected capability set, carried on the frame
// so a widget can adapt AT RENDER TIME: the color depth it will
// actually be shown in, which graphics protocol (if any) is available,
// and the pixel size of a cell. This is the mechanism behind
// "a different experience per rendering engine" — the widget asks the
// frame what it is painting onto. It is a plain field, not a property:
// capabilities are fixed for the life of a session, so making them
// observable would buy nothing and cost every widget a dependency edge.
type Frame struct {
	Cells        *render.Buffer
	Graphics     graphics.Encoder
	Placements   []graphics.Placement
	CellW, CellH int
	Caps         term.Caps
}

// Depth is the color depth this frame will be flushed at.
func (f *Frame) Depth() render.ColorDepth { return f.Caps.Color }

// Compose lays out root into a fresh frame sized to caps — the one-shot
// path (full repaint). The damage-tracked path is Composer.
func Compose(root Widget, caps term.Caps, enc graphics.Encoder) *Frame {
	f := &Frame{
		Cells:    render.NewBuffer(caps.Cols, caps.Rows),
		Graphics: enc,
		CellW:    caps.CellW,
		CellH:    caps.CellH,
		Caps:     caps,
	}
	root.Measure(Size{caps.Cols, caps.Rows})
	root.Arrange(Rect{0, 0, caps.Cols, caps.Rows})
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
	if err := render.Flush(w, f.Cells, f.Caps.Color); err != nil {
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
