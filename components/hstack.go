package components

import (
	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// HStack is VStack's horizontal twin: children left to right, each
// measured against the width left over, Gap columns between them.
// Background, when set, is filled by the framework (gooey.HasBackground).
type HStack struct {
	gooey.Base
	Children   []gooey.Component
	Gap        int
	Background *prop.Property[render.Color]
	sizes      []gooey.Size
}

func (h *HStack) ChildComponents() []gooey.Component { return h.Children }

func (h *HStack) BackgroundProperty() *prop.Property[render.Color] { return h.Background }

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
	// No room means no room for anybody — the same contract Grid keeps.
	// The cross axis comes straight from b, but the MAIN axis comes out
	// of the measure cache, which an Arrange into nothing does not
	// refresh: without this an HStack handed a zero-WIDTH slot gives
	// every child the width it had last time, at the stack's full
	// height, so the subtree keeps a rect with real area outside its
	// parent's bounds and paints there.
	if b.W <= 0 || b.H <= 0 {
		for _, c := range h.Children {
			gooey.ArrangeChild(c, gooey.Rect{X: b.X, Y: b.Y})
		}
		return
	}
	// The same clamp VStack applies on Y, for the same reason: the
	// measure cache records what each child wanted, Arrange may be
	// handed less, and an unclamped walk puts later children outside the
	// stack — where nothing will clip them, because the framework clips
	// to the buffer and not to the parent. See VStack.Arrange for the
	// full account.
	right := b.X + b.W
	x := b.X
	placed := false
	for i, c := range h.Children {
		s := measuredAt(h.sizes, i)
		if gapBefore(c, placed) {
			x += h.Gap
		}
		w := min(s.W, max(0, right-x))
		gooey.ArrangeChild(c, gooey.Rect{X: min(x, right), Y: b.Y, W: w, H: b.H})
		x += s.W
		if !collapsed(c) {
			placed = true
		}
	}
}

func (h *HStack) Render(f *gooey.Frame) {}
