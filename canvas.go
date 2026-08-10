package gooey

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
// children inside the canvas without a separate clipping pass: a widget
// that would overhang is instead told it has less room, and clips its own
// content the way it does anywhere else.
//
// Two things follow from absolute positioning that callers must know:
//
//   - Children may OVERLAP, and paint order is tree order — a later
//     sibling paints over an earlier one.
//   - Overlap and damage tracking interact. Each widget's paint is its
//     own node covering its own rect, and a leaf pre-clears that rect
//     before repainting (composer.go). So when an OCCLUDED widget
//     repaints alone, it clears its rect and paints itself — over the
//     part of the sibling that was covering it, and that sibling is
//     clean, so nothing restores it until something else dirties it.
//     Overlapping Canvas children are therefore only safe when the
//     occluded one is static. This is a real limit of paint-level
//     damage, not a Canvas bug; the fix is a z-ordered repaint of
//     intersecting nodes, which the POC does not do.
//     TestCanvasOverlapRepaintLeavesOccluderDamaged pins the behavior.
type Canvas struct {
	Base
	Children []Widget

	sizes []Size
}

func (c *Canvas) ChildWidgets() []Widget { return c.Children }

// Measure returns everything it is offered: a Canvas is a positioning
// surface, so it fills its slot rather than shrinking to its content
// (which, with absolute offsets, would be a meaningless bounding box).
func (c *Canvas) Measure(avail Size) Size {
	c.sizes = c.sizes[:0]
	for _, ch := range c.Children {
		l := layoutOf(ch)
		left, top := 0, 0
		if l != nil {
			left, top = l.Left, l.Top
		}
		c.sizes = append(c.sizes, MeasureChild(ch, Size{
			W: max(0, avail.W-left),
			H: max(0, avail.H-top),
		}))
	}
	return avail
}

func (c *Canvas) Arrange(b Rect) {
	c.Base.Arrange(b)
	for i, ch := range c.Children {
		l := layoutOf(ch)
		left, top := 0, 0
		if l != nil {
			left, top = l.Left, l.Top
		}
		s := c.sizes[i]
		ArrangeChild(ch, Rect{
			X: b.X + left,
			Y: b.Y + top,
			W: min(s.W, max(0, b.W-left)),
			H: min(s.H, max(0, b.H-top)),
		})
	}
}

func (c *Canvas) Render(f *Frame) {} // containers paint only their own chrome; a Canvas has none
