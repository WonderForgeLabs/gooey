package components

import (
	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// Border draws a rounded box with an optional title around a single
// child, which it arranges one cell inside. It is the one container
// here with chrome of its own — and it paints only that chrome, never
// clearing its interior, because the child's cells belong to the
// child's paint node.
type Border struct {
	gooey.Base
	Child gooey.Component
	Title *prop.Property[string]
	Style *prop.Property[render.Style]
}

func (b *Border) ChildComponents() []gooey.Component { return []gooey.Component{b.Child} }

func (b *Border) Measure(avail gooey.Size) gooey.Size {
	inner := gooey.MeasureChild(b.Child, gooey.Size{W: avail.W - 2, H: avail.H - 2})
	return gooey.Size{W: min(inner.W+2, avail.W), H: min(inner.H+2, avail.H)}
}

func (b *Border) Arrange(r gooey.Rect) {
	b.Base.Arrange(r)
	gooey.ArrangeChild(b.Child, gooey.Rect{X: r.X + 1, Y: r.Y + 1, W: r.W - 2, H: r.H - 2})
}

func (b *Border) Render(f *gooey.Frame) {
	r := b.Bounds()
	style := getSty(b.Style)
	for x := r.X + 1; x < r.X+r.W-1; x++ {
		f.Cells.Set(x, r.Y, '─', style)
		f.Cells.Set(x, r.Y+r.H-1, '─', style)
	}
	for y := r.Y + 1; y < r.Y+r.H-1; y++ {
		f.Cells.Set(r.X, y, '│', style)
		f.Cells.Set(r.X+r.W-1, y, '│', style)
	}
	f.Cells.Set(r.X, r.Y, '╭', style)
	f.Cells.Set(r.X+r.W-1, r.Y, '╮', style)
	f.Cells.Set(r.X, r.Y+r.H-1, '╰', style)
	f.Cells.Set(r.X+r.W-1, r.Y+r.H-1, '╯', style)
	if title := getStr(b.Title); title != "" {
		f.Cells.SetString(r.X+2, r.Y, " "+clipRunes(title, r.W-6)+" ", style)
	}
}
