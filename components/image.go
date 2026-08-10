package components

import (
	"image"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/graphics"
)

// Image exercises the graphics planes (Compose path). Its fields stay
// plain in the POC — the pixel pipeline predates the property model.
type Image struct {
	gooey.Base
	Src        image.Image
	Cols, Rows int // requested size in cells
}

func (im *Image) Measure(avail gooey.Size) gooey.Size {
	return gooey.Size{W: min(im.Cols, avail.W), H: min(im.Rows, avail.H)}
}

func (im *Image) Render(f *gooey.Frame) {
	r := im.Bounds()
	if f.Graphics != nil {
		f.Placements = append(f.Placements, graphics.Placement{
			Img: im.Src, Col: r.X, Row: r.Y, Cols: r.W, Rows: r.H,
		})
		return
	}
	graphics.DrawHalfblock(f.Cells, im.Src, r.X, r.Y, r.W, r.H)
}
