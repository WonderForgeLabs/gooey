package main

// The effects. Each one is a pure function of (framebuffer, frame
// number) — no wall clock anywhere.
//
// That is not a style preference. A demo whose motion depends on
// time.Now cannot be recorded twice and cut together, and this one
// exists to be filmed: frame 900 looks the same on every run, on every
// machine, at every window size that has the same aspect. It also means
// a golden-frame test is possible, which for an effect is the only kind
// of test worth writing.

import (
	"image"
	"image/color"
	"math"
)

// Effect is one screen. w and h are PIXELS — twice the cell rows,
// because every cell carries a top and a bottom pixel.
type Effect struct {
	Name string
	Draw func(dst *image.RGBA, t int)
}

func effects() []Effect {
	return []Effect{
		{"plasma", plasma},
		{"starfield", starfield},
		{"tunnel", tunnel},
		{"metaballs", metaballs},
		{"rotozoom", rotozoom},
	}
}

// plasma is the oldest trick on the list: a sum of sines sampled per
// pixel and pushed through a rotating palette. It is here first because
// it is the one that makes people ask whether that is really a terminal.
func plasma(dst *image.RGBA, t int) {
	b := dst.Bounds()
	w, h := b.Dx(), b.Dy()
	ft := float64(t)
	for y := 0; y < h; y++ {
		fy := float64(y) / float64(h)
		for x := 0; x < w; x++ {
			fx := float64(x) / float64(w)
			v := math.Sin(fx*10 + ft*0.05)
			v += math.Sin((fy*10 + ft*0.03) / 2)
			v += math.Sin((fx*10 + fy*10 + ft*0.04) / 2)
			cx, cy := fx+0.5*math.Sin(ft*0.02), fy+0.5*math.Cos(ft*0.017)
			v += math.Sin(math.Sqrt(100*(cx*cx+cy*cy)+1) + ft*0.06)
			dst.Set(x, y, palette(v/4, t))
		}
	}
}

// palette maps a signed sine sum to colour. The phase rides on t, so the
// plasma cycles hue without the geometry moving — the two motions read
// as one because they are on different clocks.
func palette(v float64, t int) color.RGBA {
	p := v*math.Pi + float64(t)*0.02
	return color.RGBA{
		R: uint8(128 + 127*math.Sin(p)),
		G: uint8(128 + 127*math.Sin(p+2*math.Pi/3)),
		B: uint8(128 + 127*math.Sin(p+4*math.Pi/3)),
		A: 255,
	}
}

// starfield is perspective projection and nothing else: z decreases, x/z
// and y/z grow, brightness rises as z falls. The stars are a fixed set
// seeded once and recycled, so the field is identical run to run.
var stars = makeStars(420)

type star struct{ x, y, z float64 }

func makeStars(n int) []star {
	// A cheap deterministic hash rather than math/rand: no seed to carry,
	// no global state, same field on every machine.
	out := make([]star, n)
	for i := range out {
		out[i] = star{
			x: hash01(i*3+1)*2 - 1,
			y: hash01(i*3+2)*2 - 1,
			z: hash01(i*3+3)*0.98 + 0.02,
		}
	}
	return out
}

func hash01(n int) float64 {
	h := uint32(n)*2654435761 + 12345
	h ^= h >> 15
	h *= 2246822519
	h ^= h >> 13
	return float64(h%100000) / 100000
}

func starfield(dst *image.RGBA, t int) {
	b := dst.Bounds()
	w, h := b.Dx(), b.Dy()
	fill(dst, color.RGBA{4, 4, 12, 255})
	speed := 0.006
	for i, s := range stars {
		z := s.z - math.Mod(float64(t)*speed+float64(i)*0.0001, 1.0)
		if z <= 0.02 {
			z += 1
		}
		x := int(float64(w)/2 + s.x/z*float64(w)/3)
		y := int(float64(h)/2 + s.y/z*float64(h)/3)
		if x < 0 || y < 0 || x >= w || y >= h {
			continue
		}
		lum := uint8(clamp255(int(255 * (1 - z))))
		dst.Set(x, y, color.RGBA{lum, lum, uint8(clamp255(int(lum) + 40)), 255})
	}
}

// tunnel is the texture-mapped classic: for each pixel take the polar
// angle and the reciprocal of the radius, offset both by t, and look up
// a checker. The reciprocal is what makes it feel like depth.
func tunnel(dst *image.RGBA, t int) {
	b := dst.Bounds()
	w, h := b.Dx(), b.Dy()
	cx, cy := float64(w)/2, float64(h)/2
	ft := float64(t)
	for y := 0; y < h; y++ {
		dy := (float64(y) - cy) / cy
		for x := 0; x < w; x++ {
			dx := (float64(x) - cx) / cx
			r := math.Sqrt(dx*dx+dy*dy) + 0.0001
			a := math.Atan2(dy, dx)
			u := int(math.Floor(8/r + ft*0.08))
			v := int(math.Floor(a*8/math.Pi + ft*0.02))
			shade := 1.0 - math.Min(1, r*0.8)
			c := uint8(40 + 200*shade)
			if (u+v)%2 == 0 {
				dst.Set(x, y, color.RGBA{c, uint8(float64(c) * 0.4), uint8(float64(c) * 0.8), 255})
			} else {
				dst.Set(x, y, color.RGBA{uint8(float64(c) * 0.2), uint8(float64(c) * 0.7), c, 255})
			}
		}
	}
}

// metaballs: sum the inverse-square field of a few moving sources and
// colour by the total. Everything interesting happens at the threshold,
// which is why the palette has a hard edge in it.
func metaballs(dst *image.RGBA, t int) {
	b := dst.Bounds()
	w, h := b.Dx(), b.Dy()
	ft := float64(t)
	type ball struct{ x, y, r float64 }
	balls := []ball{
		{0.5 + 0.32*math.Sin(ft*0.021), 0.5 + 0.30*math.Cos(ft*0.017), 0.055},
		{0.5 + 0.28*math.Sin(ft*0.013+2), 0.5 + 0.34*math.Cos(ft*0.023+1), 0.045},
		{0.5 + 0.36*math.Sin(ft*0.011+4), 0.5 + 0.26*math.Cos(ft*0.019+3), 0.038},
		{0.5 + 0.22*math.Sin(ft*0.027+1), 0.5 + 0.22*math.Cos(ft*0.029+5), 0.030},
	}
	for y := 0; y < h; y++ {
		fy := float64(y) / float64(h)
		for x := 0; x < w; x++ {
			fx := float64(x) / float64(w)
			sum := 0.0
			for _, ba := range balls {
				dx, dy := fx-ba.x, fy-ba.y
				sum += ba.r * ba.r / (dx*dx + dy*dy + 0.0004)
			}
			switch {
			case sum > 1.5:
				g := uint8(clamp255(int(120 + 90*math.Sin(sum+ft*0.05))))
				dst.Set(x, y, color.RGBA{uint8(clamp255(int(sum * 40))), g, 255, 255})
			case sum > 0.9:
				dst.Set(x, y, color.RGBA{20, 40, uint8(clamp255(int(120 * sum))), 255})
			default:
				dst.Set(x, y, color.RGBA{6, 8, 18, 255})
			}
		}
	}
}

// rotozoom rotates and scales a procedural checker about the centre. The
// texture is generated rather than loaded so the whole demo stays one
// binary with no assets.
func rotozoom(dst *image.RGBA, t int) {
	b := dst.Bounds()
	w, h := b.Dx(), b.Dy()
	ft := float64(t)
	ang := ft * 0.012
	zoom := 1.6 + 1.2*math.Sin(ft*0.008)
	sn, cs := math.Sin(ang)*zoom, math.Cos(ang)*zoom
	cx, cy := float64(w)/2, float64(h)/2
	for y := 0; y < h; y++ {
		dy := float64(y) - cy
		for x := 0; x < w; x++ {
			dx := float64(x) - cx
			u := int(math.Floor((dx*cs - dy*sn) / 8))
			v := int(math.Floor((dx*sn + dy*cs) / 8))
			if (u+v)&1 == 0 {
				dst.Set(x, y, color.RGBA{
					uint8(clamp255(120 + u*7)),
					uint8(clamp255(60 + v*5)),
					uint8(clamp255(180 - u*3)), 255})
				continue
			}
			dst.Set(x, y, color.RGBA{
				uint8(clamp255(20 + v*3)),
				uint8(clamp255(140 + u*4)),
				uint8(clamp255(90 + v*6)), 255})
		}
	}
}

// scroller is the thing that makes a demo a demo: a message on a sine
// path, composited over whatever effect is running, in 5×7 letters drawn
// straight into the framebuffer.
func scroller(dst *image.RGBA, msg string, t int) {
	b := dst.Bounds()
	w, h := b.Dx(), b.Dy()
	const scale = 2
	glyphW := (glyphCols + 1) * scale
	x0 := w - (t*2)%(w+len(msg)*glyphW)
	for i, r := range msg {
		gx := x0 + i*glyphW
		if gx < -glyphW || gx > w {
			continue
		}
		g, ok := font[r]
		if !ok {
			continue
		}
		wobble := int(float64(h) / 4 * math.Sin(float64(gx)*0.03+float64(t)*0.06))
		gy := h/2 - glyphRows*scale/2 + wobble
		hue := color.RGBA{
			uint8(128 + 127*math.Sin(float64(t)*0.05+float64(i)*0.3)),
			uint8(128 + 127*math.Sin(float64(t)*0.05+float64(i)*0.3+2)),
			uint8(128 + 127*math.Sin(float64(t)*0.05+float64(i)*0.3+4)),
			255,
		}
		for ry := 0; ry < glyphRows; ry++ {
			for rx := 0; rx < glyphCols; rx++ {
				if g[ry]&(1<<(glyphCols-1-rx)) == 0 {
					continue
				}
				for sy := 0; sy < scale; sy++ {
					for sx := 0; sx < scale; sx++ {
						px, py := gx+rx*scale+sx, gy+ry*scale+sy
						if px >= 0 && py >= 0 && px < w && py < h {
							dst.Set(px, py, hue)
						}
					}
				}
			}
		}
	}
}

func fill(dst *image.RGBA, c color.RGBA) {
	b := dst.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			dst.SetRGBA(x, y, c)
		}
	}
}

func clamp255(n int) int {
	if n < 0 {
		return 0
	}
	if n > 255 {
		return 255
	}
	return n
}
