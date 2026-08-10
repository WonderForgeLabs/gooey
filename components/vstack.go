package components

import (
	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// VStack stacks its children top to bottom, each one measured against
// the height left over, with Gap blank rows between them. It paints
// nothing itself. Background, when set, is filled by the framework
// (gooey.HasBackground) — it covers the gap rows no child owns.
type VStack struct {
	gooey.Base
	Children   []gooey.Component
	Gap        int
	Background *prop.Property[render.Color]
	sizes      []gooey.Size
}

func (v *VStack) ChildComponents() []gooey.Component { return v.Children }

func (v *VStack) BackgroundProperty() *prop.Property[render.Color] { return v.Background }

func (v *VStack) Measure(avail gooey.Size) gooey.Size {
	v.sizes = v.sizes[:0]
	w, h := 0, 0
	placed := false
	for _, c := range v.Children {
		if gapBefore(c, placed) {
			h += v.Gap
		}
		s := gooey.MeasureChild(c, gooey.Size{W: avail.W, H: avail.H - h})
		v.sizes = append(v.sizes, s)
		h += s.H
		if !collapsed(c) {
			placed = true
		}
		if s.W > w {
			w = s.W
		}
	}
	return gooey.Size{W: min(w, avail.W), H: min(h, avail.H)}
}

func (v *VStack) Arrange(b gooey.Rect) {
	v.Base.Arrange(b)
	y := b.Y
	placed := false
	for i, c := range v.Children {
		s := v.sizes[i]
		if gapBefore(c, placed) {
			y += v.Gap
		}
		// The slot spans the stack's full width; alignment inside it
		// is the child's business (ArrangeChild).
		gooey.ArrangeChild(c, gooey.Rect{X: b.X, Y: y, W: b.W, H: s.H})
		y += s.H
		if !collapsed(c) {
			placed = true
		}
	}
}

func (v *VStack) Render(f *gooey.Frame) {} // containers paint nothing themselves
