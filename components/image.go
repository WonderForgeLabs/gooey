package components

import (
	"image"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/graphics"
	"github.com/WonderForgeLabs/gooey/prop"
)

// Image is pixel content in a cell rectangle.
//
// It renders one of two ways, and the choice is the terminal's, not the
// author's: with a graphics protocol it records a placement, which the
// flush emits on the pixel plane over the cells; without one it draws
// itself INTO the cell buffer as halfblock runes. Nothing else in the
// tree changes either way — the same bounds, the same damage node, the
// same repaint rules.
//
// Its Src, Cols and Rows are properties like every other visual property,
// so setting one dirties this component and nothing else. That was not
// true while they were plain fields: the pixel pipeline predated the
// property model, and an image that changed repainted nothing.
type Image struct {
	gooey.Base
	Src        *prop.Property[image.Image]
	Cols, Rows *prop.Property[int] // requested size in cells
}

// Img wraps an image as a source property, the way Str and Sty wrap a
// string and a style.
func Img(i image.Image) *prop.Property[image.Image] { return prop.NewSource(i) }

// Cells wraps a cell count as a source property — Image's size, and
// anything else measured in cells.
func Cells(n int) *prop.Property[int] { return prop.NewSource(n) }

func (im *Image) size() (cols, rows int) {
	return getInt(im.Cols), getInt(im.Rows)
}

func (im *Image) Measure(avail gooey.Size) gooey.Size {
	cols, rows := im.size()
	return gooey.Size{W: min(cols, avail.W), H: min(rows, avail.H)}
}

func (im *Image) Render(f *gooey.Frame) {
	if im.Src == nil {
		return
	}
	src := im.Src.Get()
	if src == nil {
		return
	}
	// Read the size properties even though Measure already used them:
	// Measure runs outside any evaluation context, so only a read HERE
	// records the dependency that makes a resize repaint this component.
	im.size()
	r := im.Bounds()
	if r.W <= 0 || r.H <= 0 {
		return
	}
	if f.Graphics != nil {
		f.Place(graphics.Placement{Img: src, Col: r.X, Row: r.Y, Cols: r.W, Rows: r.H})
		return
	}
	graphics.DrawHalfblock(f.Cells, src, r.X, r.Y, r.W, r.H)
}
