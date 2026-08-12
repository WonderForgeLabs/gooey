package graphics

import (
	"image"
	"image/color"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The sixel encoder's quality is decided entirely by which registers it
// declares, so that is what these tests read: the "#n;2;r;g;b" table at
// the head of the stream.
//
// They exist because the palette became adaptive. A fixed 6x6x6 cube
// needed no tests — it was the same 216 colors whatever it was handed. An
// adaptive palette has a claim to keep: that it is EXACT below the
// register limit and sensible above it.

var regDecl = regexp.MustCompile(`#(\d+);2;(\d+);(\d+);(\d+)`)

// palette parses the declared registers out of an encoded stream.
func paletteOf(t *testing.T, img image.Image, cols, rows, cellW, cellH int) map[int][3]int {
	t.Helper()
	var out []byte
	if err := (Sixel{}).Encode(&out, img, cols, rows, cellW, cellH); err != nil {
		t.Fatalf("encode: %v", err)
	}
	s := string(out)
	if !strings.HasPrefix(s, "\x1bP0;0;0q") {
		t.Fatalf("stream does not start with the sixel DCS introducer")
	}
	if !strings.HasSuffix(s, "\x1b\\") {
		t.Fatalf("stream is not ST-terminated")
	}
	// Only the declaration block, which precedes the first band.
	head := s
	if i := strings.IndexByte(s, '-'); i > 0 {
		head = s[:i]
	}
	got := map[int][3]int{}
	for _, m := range regDecl.FindAllStringSubmatch(head, -1) {
		n, _ := strconv.Atoi(m[1])
		r, _ := strconv.Atoi(m[2])
		g, _ := strconv.Atoi(m[3])
		b, _ := strconv.Atoi(m[4])
		got[n] = [3]int{r, g, b}
	}
	return got
}

// solid fills an image with n distinct colors, one per column band.
func nColors(w, h, n int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := (x * n) / w
			// Spread across the cube so the colors are genuinely distinct
			// in the 0..100 space, not just in 24-bit.
			img.Set(x, y, color.RGBA{
				R: uint8((i * 255) / max(1, n-1)),
				G: uint8((i * 137) % 256),
				B: uint8((i * 61) % 256),
				A: 255,
			})
		}
	}
	return img
}

// TestChromeIsEncodedLosslessly is the claim that matters for this
// framework: interface chrome has few colors, so every one of them must
// be declared exactly rather than snapped to a fixed cube.
//
// The old 6x6x6 encoder fails this — 0x1e,0x1e,0x24 quantizes to the same
// register as a range of nearby colors and comes back as (20,20,20) on
// the 0..100 scale rather than (12,12,14).
func TestChromeIsEncodedLosslessly(t *testing.T) {
	want := []color.RGBA{
		{0x1e, 0x1e, 0x24, 0xff}, // the activity rail's ground
		{0x6c, 0x9c, 0xff, 0xff}, // its selection marker
		{0xe8, 0xeb, 0xf7, 0xff}, // an active icon
		{0x86, 0x88, 0x99, 0xff}, // an inactive one
	}
	img := image.NewRGBA(image.Rect(0, 0, 4, 6))
	for x, c := range want {
		for y := 0; y < 6; y++ {
			img.Set(x, y, c)
		}
	}
	pal := paletteOf(t, img, 4, 1, 1, 6)
	if len(pal) != len(want) {
		t.Fatalf("declared %d registers for %d colors; a lossless encode declares exactly the colors present", len(pal), len(want))
	}
	declared := map[[3]int]bool{}
	for _, v := range pal {
		declared[v] = true
	}
	for _, c := range want {
		key := [3]int{int(to100(c.R)), int(to100(c.G)), int(to100(c.B))}
		if !declared[key] {
			t.Errorf("colour %v was not declared exactly; got registers %v", key, pal)
		}
	}
}

// TestThePaletteNeverExceedsTheRegisterLimit — 256 is what every sixel
// implementation supports, and exceeding it is not a quality question but
// a corruption one.
func TestThePaletteNeverExceedsTheRegisterLimit(t *testing.T) {
	for _, n := range []int{2, 200, 256, 257, 4000} {
		img := nColors(512, 6, n)
		pal := paletteOf(t, img, 512, 1, 1, 6)
		if len(pal) > maxRegisters {
			t.Errorf("%d colors: declared %d registers, limit is %d", n, len(pal), maxRegisters)
		}
		if len(pal) == 0 {
			t.Errorf("%d colors: declared no registers at all", n)
		}
	}
}

// TestAPaletteBiggerThanTheLimitIsCutRatherThanTruncated is the
// discrimination half. An encoder that simply kept the first 256 colors
// it saw would pass the limit test above; a median cut must instead
// SPREAD its registers over the range actually present.
//
// The image is a ramp from black to white, so the correct answer covers
// both ends. Keeping the first 256 would cover only the dark end.
func TestAPaletteBiggerThanTheLimitIsCutRatherThanTruncated(t *testing.T) {
	img := nColors(2048, 6, 1000)
	pal := paletteOf(t, img, 2048, 1, 1, 6)
	lo, hi := 100, 0
	for _, v := range pal {
		if v[0] < lo {
			lo = v[0]
		}
		if v[0] > hi {
			hi = v[0]
		}
	}
	if lo > 10 || hi < 90 {
		t.Errorf("registers span red %d..%d; a median cut covers the range, truncation covers one end", lo, hi)
	}
}

// TestEncodingIsDeterministic — the same picture must produce the same
// bytes. Damage-tracked flushing compares output, so a stream that varies
// with map iteration order is a repaint that never settles.
func TestEncodingIsDeterministic(t *testing.T) {
	img := nColors(256, 12, 500)
	var a, b []byte
	if err := (Sixel{}).Encode(&a, img, 256, 2, 1, 6); err != nil {
		t.Fatal(err)
	}
	if err := (Sixel{}).Encode(&b, img, 256, 2, 1, 6); err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) {
		t.Error("two encodes of one image differ; the output depends on map iteration order")
	}
}

// TestRoundingDoesNotDarkenFlatColour — to100 rounds rather than
// truncating. Truncation shifts every channel down by up to a full step,
// which on a flat panel background is a visible change of colour for no
// reason.
func TestRoundingDoesNotDarkenFlatColour(t *testing.T) {
	// 0xff must be 100, not 99; mid grey must land on the nearest step.
	cases := []struct {
		in   uint8
		want uint8
	}{{0, 0}, {0xff, 100}, {0x80, 50}, {0x1e, 12}}
	for _, c := range cases {
		if got := to100(c.in); got != c.want {
			t.Errorf("to100(%#x) = %d, want %d", c.in, got, c.want)
		}
	}
}

// TestTransparentPixelsEmitNothing is what makes line-art chrome possible
// at all.
//
// Sixel has no alpha: a register is opaque, and a pixel with no register
// is never written, leaving the cell as it was. So transparency must be
// carried as ABSENCE. Before this, alpha was discarded — every
// transparent pixel became black and got a register — so any image that
// was not a solid rectangle painted a black box over whatever it framed.
//
// The image here is the shape that matters: a stroked outline with a
// hollow middle. The assertion is that the hollow contributes NO colour,
// which is checked by declaring exactly one register for a two-"colour"
// picture.
func TestTransparentPixelsEmitNothing(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 8, 6))
	// A one-pixel border, fully transparent inside.
	for y := 0; y < 6; y++ {
		for x := 0; x < 8; x++ {
			if x == 0 || y == 0 || x == 7 || y == 5 {
				img.Set(x, y, color.RGBA{0xff, 0xff, 0xff, 0xff})
			}
		}
	}
	pal := paletteOf(t, img, 8, 1, 1, 6)
	if len(pal) != 1 {
		t.Fatalf("declared %d registers for an outline with a transparent middle; "+
			"the hole is being treated as a colour, which paints a box over the content: %v", len(pal), pal)
	}
	for _, v := range pal {
		if v != [3]int{100, 100, 100} {
			t.Errorf("the one register is %v, want the stroke's white", v)
		}
	}
}

// TestAFullyTransparentImageEmitsNoPicture — the degenerate case of the
// above. Nothing to declare, so nothing to paint; a stream that declared
// a register here would clear cells it was never asked to touch.
func TestAFullyTransparentImageEmitsNoPicture(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 8, 6)) // zero value: alpha 0 throughout
	var out []byte
	if err := (Sixel{}).Encode(&out, img, 8, 1, 1, 6); err != nil {
		t.Fatalf("a transparent image is not an error: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("a fully transparent image emitted %d bytes; it must emit none", len(out))
	}
}

// TestAntiAliasedEdgesResolveToOneStateOrTheOther — the format has two
// states per pixel, so a soft edge has to land on one. Half alpha is the
// threshold: the opaque core of a stroke survives, the outer fringe drops
// out. This pins the rule so a later change cannot quietly move it.
func TestAntiAliasedEdgesResolveToOneStateOrTheOther(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 4, 6))
	alphas := []uint8{0x00, 0x40, 0xc0, 0xff} // out, out, in, in
	for x, a := range alphas {
		for y := 0; y < 6; y++ {
			img.Set(x, y, color.RGBA{a, a, a, a}) // premultiplied
		}
	}
	var out []byte
	if err := (Sixel{}).Encode(&out, img, 4, 1, 1, 6); err != nil {
		t.Fatal(err)
	}
	pal := paletteOf(t, img, 4, 1, 1, 6)
	if len(pal) != 2 {
		t.Errorf("declared %d registers; the two columns at or above half alpha are in, the two below are out", len(pal))
	}
}

// TestAnEmptyTargetIsAnErrorNotAnEmptyImage — a zero cell size is what an
// unprobed terminal reports, and the old encoder answered it with an
// 18-byte image that painted nothing while reporting success. That is the
// black-screen-with-no-error failure this repo has already recorded once.
func TestAnEmptyTargetIsAnErrorNotAnEmptyImage(t *testing.T) {
	img := nColors(8, 8, 4)
	var out []byte
	if err := (Sixel{}).Encode(&out, img, 4, 4, 0, 0); err == nil {
		t.Fatal("a zero cell size must be an error; silently emitting an empty image is the black-screen failure")
	}
}
