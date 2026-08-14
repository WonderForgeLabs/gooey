package graphics

import (
	"image"
	"image/color"
	"testing"
)

// Scale had no tests at all while it was nearest-neighbour, and the
// reason is worth stating because it is a trap: every existing test in
// this package encodes at its source's exact pixel size, so Scale runs at
// 1:1 and any filter whatsoever passes them. The suite could not tell
// nearest-neighbour from a cubic. These are the claims that separate
// them.

// solidBlock builds a w×h image split down the middle into two flat
// colours, both well inside 0..255 so that ANY out-of-range output is
// provably the filter's invention rather than the source's content.
func solidBlock(w, h int) *image.RGBA {
	m := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := color.RGBA{40, 60, 90, 255}
			if x >= w/2 {
				c = color.RGBA{200, 190, 170, 255}
			}
			m.Set(x, y, c)
		}
	}
	return m
}

// TestScaleIsExactAtOneToOne is the claim the REST of this package's
// tests quietly depend on: they all encode an image at its own pixel
// size, so if Scale perturbed a 1:1 copy every palette assertion here
// would be testing the filter instead of the encoder.
//
// It is not a tautology — a resampling kernel evaluated at unit scale
// only reduces to a copy if its taps land exactly on pixel centres, and a
// half-pixel phase error (an easy thing to introduce) would blur the
// whole image by one tap while still looking approximately right.
func TestScaleIsExactAtOneToOne(t *testing.T) {
	src := solidBlock(16, 12)
	got := Scale(src, 16, 12)
	for i := range src.Pix {
		if got.Pix[i] != src.Pix[i] {
			t.Fatalf("byte %d: got %d, want %d — a 1:1 scale is not a copy, so every "+
				"encoder test in this package is now measuring the filter", i, got.Pix[i], src.Pix[i])
		}
	}
}

// TestScaleNeverOvershootsTheSourceRange is why this is a triangle kernel
// and not draw.CatmullRom.
//
// A cubic has negative lobes, so at a hard edge it undershoots on the
// dark side and overshoots on the light one — values outside the range
// anything in the source ever had, which clip to black or white at high
// contrast. Terminal UI art is nearly all hard edges. Measured on the
// upscale below, CatmullRom put 2800 samples outside the source's range,
// worst case 12/255; a triangle kernel produces convex combinations of
// its inputs, so the count is not small but exactly zero.
func TestScaleNeverOvershootsTheSourceRange(t *testing.T) {
	src := solidBlock(64, 64)
	lo, hi := [3]uint8{40, 60, 90}, [3]uint8{200, 190, 170}

	for _, tc := range []struct {
		name string
		w, h int
	}{
		{"upscale 3.1x", 200, 200},
		{"downscale 5.1x", 100, 100},
		{"mixed: wider and shorter", 200, 20},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Scale(src, tc.w, tc.h)
			for i := 0; i+3 < len(got.Pix); i += 4 {
				for c := 0; c < 3; c++ {
					v := got.Pix[i+c]
					if v < lo[c] || v > hi[c] {
						t.Fatalf("pixel %d channel %d = %d, outside the source's own %d..%d — "+
							"the filter is ringing, which shows as a bright or dark seam at every edge",
							i/4, c, v, lo[c], hi[c])
					}
				}
			}
		})
	}
}

// checkerboard is 1px cells of two colours whose mean is exactly 120, so
// any correct reduction of it by a large factor is a FLAT field at 120
// and every deviation is the filter's error, per pixel, with no
// tolerance argument to have.
func checkerboard(n int) *image.RGBA {
	src := image.NewRGBA(image.Rect(0, 0, n, n))
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			c := color.RGBA{40, 40, 40, 255}
			if (x+y)%2 == 1 {
				c = color.RGBA{200, 200, 200, 255}
			}
			src.Set(x, y, c)
		}
	}
	return src
}

// TestDownscalingAveragesRatherThanSubsampling is the whole point of the
// change, and it is the test nearest-neighbour cannot pass.
//
// A 1px checkerboard reduced 4:1 covers sixteen source pixels per
// destination pixel, eight of each colour, so the answer is the midpoint.
// Nearest-neighbour reads ONE of the sixteen and returns whichever colour
// that grid position happened to land on — here, uniformly the dark one.
func TestDownscalingAveragesRatherThanSubsampling(t *testing.T) {
	got := Scale(checkerboard(64), 16, 16)
	for i := 0; i+3 < len(got.Pix); i += 4 {
		if v := got.Pix[i]; v < 118 || v > 122 {
			t.Fatalf("pixel %d red = %d, want the mean of the two source colours (120); "+
				"a subsampling scaler returns one of 40 or 200 and loses the other entirely", i/4, v)
		}
	}
}

// TestTheFilterWidensWithTheReductionRatio is why this is draw.BiLinear
// and not draw.ApproxBiLinear, and it is the one claim the rest of this
// file could not make: ApproxBiLinear passes every other test here.
//
// The difference is that a true kernel scales its support by the
// reduction ratio — reducing 6.9:1 reads about fourteen source pixels per
// axis — while ApproxBiLinear is a fixed 2x2 tap no matter the ratio.
// Four samples out of forty-eight is subsampling with a blur on top, and
// on a periodic source it beats against the sampling grid: the same
// checkerboard that must flatten to 120 comes back varying with the
// subpixel phase of each tap.
//
// The ratio is deliberately NOT an integer. At 4:1 a 2x2 tap happens to
// straddle two light and two dark pixels and averages correctly by luck,
// which is exactly why the test above cannot tell the two filters apart.
func TestTheFilterWidensWithTheReductionRatio(t *testing.T) {
	got := Scale(checkerboard(256), 37, 37)
	worst, at := 0, 0
	for i := 0; i+3 < len(got.Pix); i += 4 {
		if d := int(got.Pix[i]) - 120; d > worst || -d > worst {
			if d < 0 {
				d = -d
			}
			worst, at = d, i/4
		}
	}
	if worst > 3 {
		t.Fatalf("pixel %d is %d away from the flat 120 this reduction must produce; "+
			"a fixed-tap filter aliases against a periodic source instead of averaging it", at, worst)
	}
}

// TestAOnePixelFeatureSurvivesADownscale is the same failure in the shape
// it actually takes on screen.
//
// One bright row on a dark field, reduced 4:1 vertically. Averaging dilutes
// the row but keeps it: the band it lands in is measurably lighter than the
// rest. Nearest-neighbour samples source rows 0 and 4 for a two-row target,
// so a feature on row 1 is not dimmed — it is GONE, with nothing anywhere in
// the pipeline to report that a rule, an underline or a focus ring vanished.
func TestAOnePixelFeatureSurvivesADownscale(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			c := color.RGBA{0, 0, 0, 255}
			if y == 1 {
				c = color.RGBA{255, 255, 255, 255}
			}
			src.Set(x, y, c)
		}
	}
	got := Scale(src, 8, 2)
	top, bottom := got.RGBAAt(4, 0).R, got.RGBAAt(4, 1).R
	if top == 0 {
		t.Fatalf("the bright row vanished: top band = %d — a subsampling scaler drops "+
			"any feature its grid does not happen to land on", top)
	}
	if top <= bottom {
		t.Fatalf("top band %d is not lighter than bottom band %d; the feature did not "+
			"land where it came from", top, bottom)
	}
}

// TestUpscalingInterpolates pins the decision NOT to special-case
// enlargement back to nearest-neighbour.
//
// The argument for special-casing is real — enlargement recovers no
// information, and interpolation invents colours the sixel encoder then
// has to find registers for. It was rejected on measurement: at the
// ratios a terminal asks for, real anti-aliased icons stay far below the
// register limit, and a blocky enlargement beside the anti-aliased source
// it came from reads as a fault. If that is ever revisited it must be a
// deliberate change with this test in front of it, not a quiet one.
func TestUpscalingInterpolates(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 2, 1))
	src.Set(0, 0, color.RGBA{0, 0, 0, 255})
	src.Set(1, 0, color.RGBA{240, 240, 240, 255})

	got := Scale(src, 16, 1)
	mid := 0
	for x := 0; x < 16; x++ {
		if v := got.RGBAAt(x, 0).R; v > 8 && v < 232 {
			mid++
		}
	}
	if mid == 0 {
		t.Fatal("no intermediate values across a two-colour enlargement; the scaler is " +
			"replicating pixels rather than interpolating between them")
	}
}

// TestScaleAnswersForDegenerateSizes — halfblock asks for a zero-column
// rectangle whenever its component is collapsed, and an image with empty
// bounds reaches here from any decoder handed a truncated file. Neither is
// an error and neither may panic; the answer is a correctly sized,
// fully transparent image.
//
// The bounds assertion is the load-bearing half, and it was missing.
// image.Rect canonicalises by swapping corners, so image.Rect(0, 0, -4,
// -4) is a 4x4 rectangle at negative coordinates rather than an empty
// one — a real 16-pixel image where the caller asked for nothing. Every
// byte of it is zero, so the pixel loop below passes on it happily. A
// transparent image and no image are indistinguishable by pixels; only
// the rectangle tells them apart, which is why the "negative" case here
// was green against a Scale that allocated before it validated.
func TestScaleAnswersForDegenerateSizes(t *testing.T) {
	src := solidBlock(8, 8)
	for _, tc := range []struct {
		name string
		src  image.Image
		w, h int
	}{
		{"zero width", src, 0, 10},
		{"zero height", src, 10, 0},
		{"negative", src, -4, -4},
		{"one negative, one positive", src, -4, 10},
		{"empty source", image.NewRGBA(image.Rect(0, 0, 0, 0)), 10, 10},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Scale(tc.src, tc.w, tc.h)
			if got == nil {
				t.Fatal("nil image")
			}
			// The empty-source case is the one that legitimately keeps its
			// requested size: the TARGET was well formed, there was just
			// nothing to put in it.
			if tc.w > 0 && tc.h > 0 {
				if b := got.Bounds(); b.Dx() != tc.w || b.Dy() != tc.h {
					t.Fatalf("bounds %v for a %dx%d target, want exactly that size", b, tc.w, tc.h)
				}
			} else if b := got.Bounds(); !b.Empty() {
				t.Fatalf("bounds %v for a %dx%d target, want an EMPTY rectangle: "+
					"image.Rect swaps corners, so a negative size allocates a real image "+
					"at negative coordinates whose pixels are all zero and therefore look transparent",
					b, tc.w, tc.h)
			}
			for i, b := range got.Pix {
				if b != 0 {
					t.Fatalf("byte %d = %d, want a transparent image", i, b)
				}
			}
		})
	}
}

// TestScaleIsDeterministic — the sixel flush compares encoded bytes, so a
// scaler that varied between two calls would be a repaint that never
// settles. The encoder has its own version of this claim; the scaler
// upstream of it needs one too, because a difference here reaches the
// wire the same way.
func TestScaleIsDeterministic(t *testing.T) {
	src := solidBlock(101, 73)
	a, b := Scale(src, 37, 29), Scale(src, 37, 29)
	for i := range a.Pix {
		if a.Pix[i] != b.Pix[i] {
			t.Fatalf("byte %d differs between two scales of one image: %d vs %d", i, a.Pix[i], b.Pix[i])
		}
	}
}

// gifplay rescales every frame of an animation on decode, so this is the
// cost that changed. Sizes are a real clip from cmd/browser: 860x608
// reduced to a 240px long side.
func BenchmarkScaleGIFFrame(b *testing.B) {
	src := solidBlock(860, 608)
	b.ReportAllocs()
	for b.Loop() {
		Scale(src, 240, 169)
	}
}

// The sixel path's own shape: an icon enlarged into a small cell
// rectangle, which happens once per placement per frame.
func BenchmarkScaleIcon(b *testing.B) {
	src := solidBlock(16, 16)
	b.ReportAllocs()
	for b.Loop() {
		Scale(src, 40, 40)
	}
}

// The halfblock path: a full-screen image degraded into cells.
func BenchmarkScaleHalfblock(b *testing.B) {
	src := solidBlock(860, 608)
	b.ReportAllocs()
	for b.Loop() {
		Scale(src, 100, 60)
	}
}
