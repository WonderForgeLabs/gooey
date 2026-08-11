package components

import (
	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// Canvas is the absolute-positioning panel: every child sits at the
// offset its Canvas.Left/Canvas.Top attached properties name, at its own
// desired size. It is the escape hatch from the measure/arrange
// negotiation the other panels run — the one place where the author,
// not the layout system, decides where something goes.
//
// The attached properties live in Layout beside Grid.Row/Grid.Col, for
// the same reason: Go has no attached-property store, so the element
// itself is it (see layout.go).
//
// Measure gives a child the space remaining from its offset — a child at
// Left=70 on an 80-wide canvas measures against 10 columns. That keeps
// children inside the canvas without a separate clipping pass: a component
// that would overhang is instead told it has less room, and clips its own
// content the way it does anywhere else.
//
// Two things follow from absolute positioning that callers must know:
//
//   - Children may OVERLAP, and paint order is tree order — a later
//     sibling paints over an earlier one.
//   - Overlap and damage tracking interact through the Composer's
//     z-ordered repaint (composer.go): when an OCCLUDED component
//     repaints alone, every later sibling whose bounds intersect it is
//     forced to repaint in the same frame, so the occluder lands back on
//     top. The cost is honest — overlapping children repaint together —
//     and the pixels are always in tree order.
//     TestCanvasOverlapRepaintRepaintsTheOccluderAbove pins the behavior.
//
// Background, when set, is filled by the framework (gooey.HasBackground).
type Canvas struct {
	gooey.Base
	Children   []gooey.Component
	Background *prop.Property[render.Color]

	sizes []gooey.Size
}

func (c *Canvas) ChildComponents() []gooey.Component { return c.Children }

func (c *Canvas) BackgroundProperty() *prop.Property[render.Color] { return c.Background }

// Measure returns everything it is offered: a Canvas is a positioning
// surface, so it fills its slot rather than shrinking to its content
// (which, with absolute offsets, would be a meaningless bounding box).
func (c *Canvas) Measure(avail gooey.Size) gooey.Size {
	c.sizes = c.sizes[:0]
	for _, ch := range c.Children {
		l := gooey.LayoutOf(ch)
		left, top := 0, 0
		if l != nil {
			left, top = l.Left, l.Top
		}
		c.sizes = append(c.sizes, gooey.MeasureChild(ch, gooey.Size{
			W: max(0, avail.W-left),
			H: max(0, avail.H-top),
		}))
	}
	return avail
}

func (c *Canvas) Arrange(b gooey.Rect) {
	c.Base.Arrange(b)
	for i, ch := range c.Children {
		l := gooey.LayoutOf(ch)
		left, top := 0, 0
		if l != nil {
			left, top = l.Left, l.Top
		}
		s := measuredAt(c.sizes, i)
		gooey.ArrangeChild(ch, gooey.Rect{
			X: b.X + left,
			Y: b.Y + top,
			W: min(s.W, max(0, b.W-left)),
			H: min(s.H, max(0, b.H-top)),
		})
	}
}

func (c *Canvas) Render(f *gooey.Frame) {} // containers paint only their own chrome; a Canvas has none
