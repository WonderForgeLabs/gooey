// Package gooey — POC of the retained visual tree / component model.
//
// The tree is retained: components are persistent objects with parents,
// children, and computed bounds. A frame is produced by the classic
// two-pass layout (Measure bottom-up, Arrange top-down) followed by a
// Render walk. Pixel content never enters the cell buffer — components
// record graphics.Placements on the Frame, and the flush composites the
// two planes (cells first, then pixel placements over them).
package gooey

import (
	"io"

	"github.com/WonderForgeLabs/gooey/graphics"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
	"github.com/WonderForgeLabs/gooey/term"
)

type Size struct{ W, H int }

type Rect struct{ X, Y, W, H int }

// Component is the component model. Everything in the tree implements it.
type Component interface {
	// Measure returns the size the component wants within avail.
	Measure(avail Size) Size
	// Arrange assigns final bounds (and arranges children).
	Arrange(bounds Rect)
	// Render paints THIS component only into the frame using the bounds
	// from Arrange; children are walked by the framework via Container.
	Render(f *Frame)
}

// Container is implemented by components with children. The framework —
// not the component — walks them, so the Composer can give every component
// its own paint node.
type Container interface{ ChildComponents() []Component }

// HasBackground is implemented by containers that declare a background
// fill. The fill itself is the framework's job — the Composer (and the
// one-shot Compose) paints the container's bounds with the color before
// the container's chrome and its children go down — so a container
// declares the surface and still paints only its own chrome.
//
// A nil handle means the container has no background and stays on the
// cheap chrome-only damage path. A non-nil handle whose color is unset
// still fills — with the nearest ancestor's background — so clearing a
// background at runtime erases the old fill instead of leaving it
// stranded. Either way, a container with a background handle overpaints
// its subtree when it repaints, and the Composer's z-ordered pass
// repaints the subtree above it in the same frame.
type HasBackground interface {
	BackgroundProperty() *prop.Property[render.Color]
}

// Frozen is implemented by a component whose subtree renders but does not
// act. The picture is live; the behaviour is not.
//
// Descendants lay out, paint, and keep their own paint nodes — damage
// granularity is untouched — and they are simply never the target of
// anything. Keys, scoped KeyBindings, mnemonics, clicks, drags, the
// wheel, hover watchers and focus all stop at the frozen component, and
// nothing Startable below it is started.
//
// The motivating case is a UI builder's design surface: click a button
// and it sits there like a picture. A read-only preview and a disabled
// subtree are the same shape.
//
// Two things are deliberately NOT frozen, because freezing them would
// freeze the picture rather than the behaviour. Validators are computeds
// that evaluate during paint, and their evaluation is what puts a
// validation marker on screen — so a validator with a side effect gets
// that side effect anyway, and nothing here can prevent it. And a style
// that reads HoverState still repaints, because hover is an ordinary
// property; what is lost is only motion over time, which in this
// framework needs a clock, and a clock is a Startable.
//
// The component itself is not frozen — its subtree is. A frozen host is
// still focusable, still receives the events its subtree would have, and
// its own attachments still run. That is what makes it the place a design
// surface puts its own gestures.
//
// FLIPPING IT IS A STRUCTURAL CHANGE, and the framework makes that happen
// rather than asking the host to. Composer.armFrozen gives every
// implementer an observer whose evaluation CALLS this method, so any
// property read inside it is subscribed by the ordinary call-site rule; a
// Set on that property schedules a frame, Frame's sweep compares the new
// answer against the last one, and a real flip raises the same structDirty
// a Dynamic container raises. The re-sync that follows runs in the SAME
// frame, before anything paints: walkNodes re-derives the Startable set
// through frozenAncestor, FocusManager.Resync re-derives the focus order,
// the scoped bindings, the mnemonics and the hover watchers, and
// FocusManager.evictFrozen retargets a hover and drops a capture that is
// now inside the picture. So pressing the key that turns design mode on
// leaves nothing in the subtree reachable by the very next event.
//
// Freezing costs no repaint of its own. It changes what the tree MEANS,
// not what it looks like, so the only components that repaint for a flip
// are the ones that read the same property while painting (a mode
// indicator) plus the two involved if focus had to leave the subtree.
//
// THE LIMIT, and it is the same limit every derived value in this
// framework has: the observer subscribes to what Frozen() READS. An
// implementation over plain Go state — a bare bool field written by a
// handler — records no dependency, so nothing notices when it changes and
// the old sampled behaviour is what you get. Read a property, or call
// Composer.InvalidateStructure by hand.
type Frozen interface{ Frozen() bool }

// isFrozen reports whether w freezes its subtree.
func isFrozen(w Component) bool {
	f, ok := w.(Frozen)
	return ok && f.Frozen()
}

// Decorator is implemented by components whose Render owns no cells of
// its own but re-styles cells that earlier siblings painted (ItemsView's
// row highlight). It must be re-applied after an underlying repaint.
type Decorator interface{ DecoratesCells() }

// CellPassthrough is implemented by transparent containers whose Render
// neither owns nor re-styles cells. Their paint nodes never need to be
// restored when an overlay leaves a region.
//
// The claim is about the TYPE's Render; the Composer still checks the
// INSTANCE, because a container that declares a Background is filled by
// the framework and owns its bounds however empty its Render is. A
// Canvas implements this and is a passthrough only while it has no
// colour — see cellPassthrough in composer.go.
type CellPassthrough interface{ PassesCellsThrough() }

// backgroundProp returns w's declared background handle, or nil when w
// declares none.
func backgroundProp(w Component) *prop.Property[render.Color] {
	if hb, ok := w.(HasBackground); ok {
		return hb.BackgroundProperty()
	}
	return nil
}

// Frame is one composed frame: the cell plane plus deferred pixel
// placements. Graphics is nil when the terminal has no pixel protocol —
// components with pixel content must then degrade into cells (halfblock).
//
// Caps is the terminal's detected capability set, carried on the frame
// so a component can adapt AT RENDER TIME: the color depth it will
// actually be shown in, which graphics protocol (if any) is available,
// and the pixel size of a cell. This is the mechanism behind
// "a different experience per rendering engine" — the component asks the
// frame what it is painting onto. It is a plain field, not a property:
// capabilities are fixed for the life of a session, so making them
// observable would buy nothing and cost every component a dependency edge.
type Frame struct {
	Cells        *render.Buffer
	Graphics     graphics.Encoder
	CellW, CellH int
	Caps         term.Caps

	placements []graphics.Placement
	// sink is installed by the Composer around each paint node, so a
	// placement recorded during Render is filed under the component that
	// recorded it. See Place.
	sink func(graphics.Placement)
}

// Depth is the color depth this frame will be flushed at.
func (f *Frame) Depth() render.ColorDepth { return f.Caps.Color }

// Place records pixel content to be composited over the cells. It is the
// pixel-plane counterpart of writing to f.Cells, and a component with an
// image calls it from Render exactly where a text component would write
// runes.
//
// It is a method rather than an appendable field because a placement has
// an OWNER. Under the Composer only dirty components re-render, so a
// placement list rebuilt from scratch each frame would lose the images of
// every component that did not repaint. Routing through here files each
// placement under the paint node that was executing, which is what lets
// the flush say "this component's images changed" and leave the rest
// alone — the same per-component damage rule the cell plane follows.
func (f *Frame) Place(p graphics.Placement) {
	if f.sink != nil {
		f.sink(p)
		return
	}
	f.placements = append(f.placements, p)
}

// Placements is this frame's pixel plane in paint order.
func (f *Frame) Placements() []graphics.Placement { return f.placements }

// Compose lays out root into a fresh frame sized to caps — the one-shot
// path (full repaint). The damage-tracked path is Composer.
func Compose(root Component, caps term.Caps, enc graphics.Encoder) *Frame {
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

func renderTree(w Component, f *Frame) {
	if l := LayoutOf(w); l != nil && l.Visibility == Collapsed {
		return // collapsed subtrees paint nothing at all
	}
	if paintable(w) {
		if bp := backgroundProp(w); bp != nil {
			if col := bp.Get(); col.Set {
				if b, ok := w.(Bounded); ok {
					fillRect(f.Cells, b.Bounds(), render.Style{Bg: col})
				}
			}
		}
		w.Render(f)
	}
	if c, ok := w.(Container); ok {
		for _, ch := range c.ChildComponents() {
			renderTree(ch, f)
		}
	}
}

// Flush writes the frame: cell plane first, then pixel placements. The
// whole sequence is one synchronized update — cells and the images that
// sit on top of them are a single frame, so the terminal must not
// present the gap between them.
func (f *Frame) Flush(w io.Writer) error {
	if _, err := io.WriteString(w, render.BeginSync); err != nil {
		return err
	}
	defer io.WriteString(w, render.EndSync)
	if err := render.FlushCells(w, f.Cells, f.Caps.Color, false); err != nil {
		return err
	}
	for _, p := range f.placements {
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
