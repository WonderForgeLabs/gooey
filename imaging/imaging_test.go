package imaging

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"testing"
	"testing/fstest"

	"golang.org/x/image/bmp"
)

// fixture is a deterministic 2×2: red, green / blue, white.
func fixture() *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	img.SetNRGBA(0, 0, color.NRGBA{255, 0, 0, 255})
	img.SetNRGBA(1, 0, color.NRGBA{0, 255, 0, 255})
	img.SetNRGBA(0, 1, color.NRGBA{0, 0, 255, 255})
	img.SetNRGBA(1, 1, color.NRGBA{255, 255, 255, 255})
	return img
}

func wantPixels(t *testing.T, img image.Image) {
	t.Helper()
	if got := img.Bounds(); got.Dx() != 2 || got.Dy() != 2 {
		t.Fatalf("bounds %v, want 2×2", got)
	}
	want := fixture()
	for y := 0; y < 2; y++ {
		for x := 0; x < 2; x++ {
			gr, gg, gb, ga := img.At(x, y).RGBA()
			wr, wg, wb, wa := want.At(x, y).RGBA()
			if gr != wr || gg != wg || gb != wb || ga != wa {
				t.Fatalf("pixel (%d,%d) = %v, want %v", x, y, img.At(x, y), want.At(x, y))
			}
		}
	}
}

func TestDecodePNG(t *testing.T) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, fixture()); err != nil {
		t.Fatal(err)
	}
	img, err := Decode(&buf, "fixture.png")
	if err != nil {
		t.Fatal(err)
	}
	wantPixels(t, img)
}

func TestDecodeGIFFirstFrame(t *testing.T) {
	pal := color.Palette{
		color.NRGBA{255, 0, 0, 255}, color.NRGBA{0, 255, 0, 255},
		color.NRGBA{0, 0, 255, 255}, color.NRGBA{255, 255, 255, 255},
	}
	first := image.NewPaletted(image.Rect(0, 0, 2, 2), pal)
	first.SetColorIndex(0, 0, 0)
	first.SetColorIndex(1, 0, 1)
	first.SetColorIndex(0, 1, 2)
	first.SetColorIndex(1, 1, 3)
	second := image.NewPaletted(image.Rect(0, 0, 2, 2), pal) // all red
	var buf bytes.Buffer
	err := gif.EncodeAll(&buf, &gif.GIF{
		Image: []*image.Paletted{first, second}, Delay: []int{10, 10},
	})
	if err != nil {
		t.Fatal(err)
	}
	img, err := Decode(&buf, "anim.gif")
	if err != nil {
		t.Fatal(err)
	}
	wantPixels(t, img) // the FIRST frame, not the last
}

func TestDecodeJPEG(t *testing.T) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, fixture(), &jpeg.Options{Quality: 100}); err != nil {
		t.Fatal(err)
	}
	img, err := Decode(&buf, "fixture.jpg")
	if err != nil {
		t.Fatal(err)
	}
	// JPEG is lossy; the shape is the assertion.
	if got := img.Bounds(); got.Dx() != 2 || got.Dy() != 2 {
		t.Fatalf("bounds %v, want 2×2", got)
	}
}

func TestDecodeBMP(t *testing.T) {
	var buf bytes.Buffer
	if err := bmp.Encode(&buf, fixture()); err != nil {
		t.Fatal(err)
	}
	img, err := Decode(&buf, "fixture.bmp")
	if err != nil {
		t.Fatal(err)
	}
	wantPixels(t, img)
}

func TestLoadThroughFS(t *testing.T) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, fixture()); err != nil {
		t.Fatal(err)
	}
	fsys := fstest.MapFS{"assets/logo.png": {Data: buf.Bytes()}}
	img, err := Load(fsys, "assets/logo.png")
	if err != nil {
		t.Fatal(err)
	}
	wantPixels(t, img)
}

func TestLoadMissingFileIsTypedError(t *testing.T) {
	_, err := Load(fstest.MapFS{}, "nope.png")
	var le *Error
	if !errors.As(err, &le) {
		t.Fatalf("error is %T, want *imaging.Error", err)
	}
	if le.Path != "nope.png" || le.Format != "" {
		t.Fatalf("error = %+v, want Path nope.png and no format", le)
	}
}

func TestDecodeUnrecognizedFormat(t *testing.T) {
	_, err := Decode(bytes.NewReader([]byte("definitely not an image")), "junk.dat")
	var le *Error
	if !errors.As(err, &le) {
		t.Fatalf("error is %T, want *imaging.Error", err)
	}
	if le.Format != "" {
		t.Fatalf("sniff matched %q on junk", le.Format)
	}
	for _, name := range []string{"png", "jpeg", "gif", "bmp", "ico"} {
		if !bytes.Contains([]byte(le.Error()), []byte(name)) {
			t.Fatalf("error %q does not offer format %q", le, name)
		}
	}
}

func TestDecodeCorruptNamesTheFormat(t *testing.T) {
	// A valid PNG magic over garbage: the sniff commits to png, so the
	// error must say png and name the file.
	data := append([]byte("\x89PNG\r\n\x1a\n"), []byte("garbage")...)
	_, err := Decode(bytes.NewReader(data), "broken.png")
	var le *Error
	if !errors.As(err, &le) {
		t.Fatalf("error is %T, want *imaging.Error", err)
	}
	if le.Format != "png" || le.Path != "broken.png" {
		t.Fatalf("error = %+v, want png/broken.png", le)
	}
}
