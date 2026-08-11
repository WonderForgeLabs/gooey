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
//
// Background declares a fill for the whole box. The fill itself is the
// framework's job (gooey.HasBackground): the Composer paints the bounds
// with the color before the chrome and the child go down, and its
// z-ordered pass repaints the subtree whenever the Border repaints over
// it. Chrome drawn with a Style whose Bg is unset sits ON the fill
// rather than punching through it.
type Border struct {
	gooey.Base
	Child      gooey.Component
	Title      *prop.Property[string]
	Style      *prop.Property[render.Style]
	Background *prop.Property[render.Color]
}

func (b *Border) ChildComponents() []gooey.Component { return []gooey.Component{b.Child} }

func (b *Border) BackgroundProperty() *prop.Property[render.Color] { return b.Background }

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
	// A degenerate rect is not a small box: with W or H at zero the
	// far-edge arithmetic (r.X+r.W-1) walks BACKWARDS, and the corners
	// land outside the node's own bounds — outside its damage rect, so
	// the composer never cleans them and the scar is permanent. Zero
	// size happens routinely: a Visible Border inside a Collapsed
	// ancestor (a hidden Tabs page) is arranged into nothing while
	// staying paintable. Painting only inside your own bounds is the
	// damage contract; this is where a Border keeps it.
	if r.W <= 0 || r.H <= 0 {
		return
	}
	style := getSty(b.Style)
	if !style.Bg.Set {
		if col := getColor(b.Background); col.Set {
			style.Bg = col
		}
	}
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
