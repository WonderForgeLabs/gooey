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
	// DrawBoxRunes carries this guard too, and it stays here anyway —
	// not for the arithmetic but for the DEPENDENCY SET. Returning
	// before the property reads is what keeps a zero-sized Border
	// depending on nothing, so restyling a hidden Tabs page's Border
	// costs no repaint. Delete this and the reads move above the guard,
	// which is a damage change wearing a dead-code costume. Nothing
	// goes stale by it: bounds changes bump the node's own revision
	// (composer.go), so the Border repaints and picks up Style the
	// frame it gains real bounds.
	if r.W <= 0 || r.H <= 0 {
		return
	}
	// These reads happen inside the paint node, which is what makes a
	// color change repaint this Border and nothing else. The helpers
	// below read no property at all — they are arithmetic over the cell
	// buffer — so hoisting the loops out of here moved no dependency
	// edge with them.
	style := getSty(b.Style)
	if !style.Bg.Set {
		if col := getColor(b.Background); col.Set {
			style.Bg = col
		}
	}
	DrawBoxRunes(f.Cells, r, style)
	// The title's in-bounds budget lives in the helper now; see
	// components/box.go for why "clip, do not skip" and why a box with
	// no room for a title writes not even the pad spaces.
	DrawBoxTitle(f.Cells, r, getStr(b.Title), style)
}
