package main

import (
	"fmt"
	"image"
	"image/color"
	"math"
)

// Cover art is GENERATED, not loaded. Two reasons, both deliberate:
//
//   - the root module has exactly two direct requirements and no binary
//     assets belong in it, so forty JPEGs were never an option;
//   - a picture derived from the title is deterministic, which is what
//     lets a test say "this row is showing the art for `Halcyon`" without
//     comparing pixels to a golden file.
//
// The pictures are still real pixel content: 96×96 NRGBA, drawn per
// item, handed to components.Image, and emitted on the pixel plane by
// whichever protocol the terminal speaks (or turned into halfblock cells
// when it speaks none).
const coverPx = 96

// coverOf builds one item's art. The title seeds a hue and a pattern, so
// two titles that sort next to each other look nothing alike — which is
// the property this demo needs: if type-ahead lands you on the wrong row,
// the picture has to be able to tell you.
func coverOf(title string, year int) image.Image {
	h := fnv(title)
	img := image.NewNRGBA(image.Rect(0, 0, coverPx, coverPx))
	hue := float64(h%360) + float64(year%7)*3
	kind := int(h>>9) % 5
	base := hsv(hue, 0.55, 0.30)
	ink := hsv(math.Mod(hue+float64(40+int(h>>3)%120), 360), 0.85, 0.95)
	for y := range coverPx {
		for x := range coverPx {
			img.Set(x, y, paint(kind, x, y, h, base, ink))
		}
	}
	return img
}

// paint decides one pixel. Five patterns is enough that a screenful of
// rows reads as a shelf of different records rather than as a gradient
// swatch book.
func paint(kind, x, y int, h uint32, base, ink color.NRGBA) color.NRGBA {
	fx, fy := float64(x)-coverPx/2, float64(y)-coverPx/2
	switch kind {
	case 0: // concentric rings
		r := math.Hypot(fx, fy)
		if int(r/6)%2 == 0 {
			return mix(base, ink, 0.85-r/coverPx)
		}
	case 1: // diagonal stripes
		if ((x+y)/9)%2 == 0 {
			return ink
		}
	case 2: // checkerboard, coarse
		if (x/16+y/16)%2 == 0 {
			return ink
		}
	case 3: // a horizon with a disc over it
		if math.Hypot(fx, fy+12) < 26 {
			return ink
		}
		if y > coverPx*2/3 {
			return mix(base, ink, 0.35)
		}
	case 4: // vertical bars of varying width, seeded by the hash
		w := 4 + int((h>>uint(x/12))%9)
		if (x/w)%2 == 0 {
			return mix(base, ink, 0.7)
		}
	}
	return base
}

// palette turns a hue into a color without pulling in a color library.
func hsv(hdeg, s, v float64) color.NRGBA {
	hdeg = math.Mod(math.Mod(hdeg, 360)+360, 360)
	c := v * s
	x := c * (1 - math.Abs(math.Mod(hdeg/60, 2)-1))
	m := v - c
	var r, g, b float64
	switch int(hdeg / 60) {
	case 0:
		r, g, b = c, x, 0
	case 1:
		r, g, b = x, c, 0
	case 2:
		r, g, b = 0, c, x
	case 3:
		r, g, b = 0, x, c
	case 4:
		r, g, b = x, 0, c
	default:
		r, g, b = c, 0, x
	}
	return color.NRGBA{uint8((r + m) * 255), uint8((g + m) * 255), uint8((b + m) * 255), 255}
}

func mix(a, b color.NRGBA, t float64) color.NRGBA {
	t = math.Max(0, math.Min(1, t))
	return color.NRGBA{
		uint8(float64(a.R)*(1-t) + float64(b.R)*t),
		uint8(float64(a.G)*(1-t) + float64(b.G)*t),
		uint8(float64(a.B)*(1-t) + float64(b.B)*t),
		255,
	}
}

func fnv(s string) uint32 {
	h := uint32(2166136261)
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= 16777619
	}
	return h
}

func fourDigits(n int) string { return fmt.Sprintf("%d", n) }
