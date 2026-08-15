package main

// The logo, generated rather than shipped.
//
// It exists to prove one thing on camera: an <Image> in a terminal app
// is not a metaphor. On a terminal with sixel or kitty graphics this is
// real pixels on a real graphics plane, composited over the cells; on
// one without, the framework degrades it to '▀' halfblocks and the same
// picture appears at half the vertical resolution. Same markup, same
// handle, no branch in this program.
//
// It is generated because an example that needs a PNG next to it is an
// example half the people who clone it cannot run — and because a handle
// is what <Image Src> wants anyway.

import (
	"image"
	"image/color"
	"math"
)

// makeLogo draws a waveform badge: a filled sine envelope over a dark
// panel, with the peaks tinted the same green the scope uses so the two
// read as the same instrument.
func makeLogo(w, h int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{14, 16, 24, 255})
		}
	}
	mid := float64(h) / 2
	for x := 0; x < w; x++ {
		u := float64(x) / float64(w)
		// Three partials and an envelope: enough structure that it reads
		// as a waveform rather than as a sine.
		v := math.Sin(u*math.Pi*7)*0.55 +
			math.Sin(u*math.Pi*17)*0.25 +
			math.Sin(u*math.Pi*3)*0.30
		v *= math.Sin(u * math.Pi) // fade in and out at the edges
		top := int(mid - v*mid*0.92)
		bot := int(mid + v*mid*0.92)
		if top > bot {
			top, bot = bot, top
		}
		for y := top; y <= bot; y++ {
			if y < 0 || y >= h {
				continue
			}
			d := math.Abs(float64(y)-mid) / mid
			img.SetRGBA(x, y, color.RGBA{
				R: uint8(60 + 190*d),
				G: uint8(230 - 60*d),
				B: uint8(170 - 60*d),
				A: 255,
			})
		}
	}
	return img
}
