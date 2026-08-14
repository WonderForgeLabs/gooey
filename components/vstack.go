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
	// No room means no room for anybody — the same contract Grid keeps.
	// The cross axis comes straight from b, but the MAIN axis comes out
	// of the measure cache, which an Arrange into nothing does not
	// refresh: without this a VStack handed a zero-HEIGHT slot gives
	// every child the height it had last time, at the stack's full
	// width, so the subtree keeps a rect with real area outside its
	// parent's bounds and paints there.
	if b.W <= 0 || b.H <= 0 {
		for _, c := range v.Children {
			gooey.ArrangeChild(c, gooey.Rect{X: b.X, Y: b.Y})
		}
		return
	}
	// The measure cache is not a budget. v.sizes holds what each child
	// WANTED against whatever avail the last Measure used, and Arrange
	// can be handed a smaller rect than that — a Grid measuring its
	// children against the screen and then arranging them into a fixed
	// track is the ordinary case, not an exotic one. Walking `y` by the
	// cached heights then marches straight past b's bottom edge, and
	// every child from that point on is arranged outside the stack.
	//
	// Nothing downstream catches it: no part of the framework clips a
	// component to its arranged rect (render.Cells.SetString clips to
	// the BUFFER, not the parent), so the child paints exactly where it
	// was told — over its neighbours, or over the chrome of the Border
	// that contains it. The reported symptom was a bottom border row
	// reading "╰  </Canvas>": the corner intact, the rule replaced by
	// text from a pane that had overrun it.
	//
	// The zero-size guard above is this same failure at b.H <= 0, and
	// its comment already names the class. This is the b.H > 0 half:
	// real room, just not enough of it.
	bottom := b.Y + b.H
	y := b.Y
	placed := false
	for i, c := range v.Children {
		s := measuredAt(v.sizes, i)
		if gapBefore(c, placed) {
			y += v.Gap
		}
		// Truncate the child that straddles the edge and give nothing to
		// those past it, rather than scaling every child down: a stack
		// out of room should show its first children at the size they
		// asked for and lose the last, the way clipped text keeps its
		// first lines.
		h := min(s.H, max(0, bottom-y))
		// The slot spans the stack's full width; alignment inside it
		// is the child's business (ArrangeChild).
		gooey.ArrangeChild(c, gooey.Rect{X: b.X, Y: min(y, bottom), W: b.W, H: h})
		y += s.H
		if !collapsed(c) {
			placed = true
		}
	}
}

func (v *VStack) Render(f *gooey.Frame) {} // containers paint nothing themselves
