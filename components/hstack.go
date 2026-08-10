package components

import "github.com/WonderForgeLabs/gooey"

// HStack is VStack's horizontal twin: children left to right, each
// measured against the width left over, Gap columns between them.
type HStack struct {
	gooey.Base
	Children []gooey.Component
	Gap      int
	sizes    []gooey.Size
}

func (h *HStack) ChildComponents() []gooey.Component { return h.Children }

func (h *HStack) Measure(avail gooey.Size) gooey.Size {
	h.sizes = h.sizes[:0]
	w, hh := 0, 0
	placed := false
	for _, c := range h.Children {
		if gapBefore(c, placed) {
			w += h.Gap
		}
		s := gooey.MeasureChild(c, gooey.Size{W: avail.W - w, H: avail.H})
		h.sizes = append(h.sizes, s)
		w += s.W
		if !collapsed(c) {
			placed = true
		}
		if s.H > hh {
			hh = s.H
		}
	}
	return gooey.Size{W: min(w, avail.W), H: min(hh, avail.H)}
}

func (h *HStack) Arrange(b gooey.Rect) {
	h.Base.Arrange(b)
	x := b.X
	placed := false
	for i, c := range h.Children {
		s := h.sizes[i]
		if gapBefore(c, placed) {
			x += h.Gap
		}
		gooey.ArrangeChild(c, gooey.Rect{X: x, Y: b.Y, W: s.W, H: b.H})
		x += s.W
		if !collapsed(c) {
			placed = true
		}
	}
}

func (h *HStack) Render(f *gooey.Frame) {}
