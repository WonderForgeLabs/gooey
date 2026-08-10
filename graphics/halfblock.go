package graphics

import (
	"image"

	"github.com/WonderForgeLabs/gooey/render"
)

// DrawHalfblock is the universal fallback: it degrades pixel content
// into the cell plane itself — each cell becomes '▀' with the top pixel
// as foreground and the bottom pixel as background (2 px per cell).
// This is why the fallback is not an Encoder: nothing is emitted beside
// the cells; the image simply becomes cells.
func DrawHalfblock(b *render.Buffer, img image.Image, col, row, cols, rows int) {
	px := Scale(img, cols, rows*2)
	for y := 0; y < rows; y++ {
		for x := 0; x < cols; x++ {
			tr, tg, tb, _ := px.At(x, y*2).RGBA()
			br, bg, bb, _ := px.At(x, y*2+1).RGBA()
			b.Set(col+x, row+y, '▀', render.Style{
				Fg: render.RGB(uint8(tr>>8), uint8(tg>>8), uint8(tb>>8)),
				Bg: render.RGB(uint8(br>>8), uint8(bg>>8), uint8(bb>>8)),
			})
		}
	}
}
