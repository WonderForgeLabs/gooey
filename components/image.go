package components

import (
	"image"
	"io/fs"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/graphics"
	"github.com/WonderForgeLabs/gooey/imaging"
	"github.com/WonderForgeLabs/gooey/prop"
)

// Image is pixel content in a cell rectangle.
//
// It renders one of two ways, and the choice is the terminal's, not the
// author's: with a graphics protocol AND a known cell size it records a
// placement, which the flush emits on the pixel plane over the cells;
// without either it draws itself INTO the cell buffer as halfblock
// runes. Nothing else in the tree changes either way — the same bounds,
// the same damage node, the same repaint rules.
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

// LoadImg loads an image file through the imaging registry (png, jpeg,
// gif, bmp, ico in core; more via nested format modules) and wraps it
// as a source property — Img for pictures that live in files. The
// fs.FS is the same seam markup pages load through: os.DirFS in dev,
// embed.FS in release.
func LoadImg(fsys fs.FS, path string) (*prop.Property[image.Image], error) {
	img, err := imaging.Load(fsys, path)
	if err != nil {
		return nil, err
	}
	return Img(img), nil
}

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
	// The tier test is three conditions, not one. An encoder scales its
	// output by the cell size — Sixel.Encode to cols*CellW × rows*CellH —
	// so a protocol pinned with no capabilities behind it, leaving CellW
	// at zero, asks for an image of zero pixels. Taking this branch is
	// also what stops halfblock from running, so the cells underneath
	// stay unpainted: a blank rectangle with nothing on any surface to
	// see. App.caps backfills term.DefaultCellW/H for exactly this
	// (app.go:601), but a Composer driven directly does not —
	// SetGraphics takes an encoder and no metrics at all.
	//
	// Same guard as buttonchrome.go:297 and colorpicker.go:163, which is
	// what markup.go's Variant comment has always claimed this file asks.
	if f.Graphics != nil && f.CellW > 0 && f.CellH > 0 {
		f.Place(graphics.Placement{Img: src, Col: r.X, Row: r.Y, Cols: r.W, Rows: r.H})
		return
	}
	graphics.DrawHalfblock(f.Cells, src, r.X, r.Y, r.W, r.H)
}
