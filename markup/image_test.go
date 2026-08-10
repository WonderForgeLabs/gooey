package markup

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/imaging"
	"github.com/WonderForgeLabs/gooey/prop"
)

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(40 * x), G: uint8(40 * y), B: 200, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestImageLiteralSrcLoadsFromThePageFS(t *testing.T) {
	fsys := fstest.MapFS{
		"page.gooey": {Data: []byte(`<Gooey>
  <VStack>
    <Image Name="logo" Src="assets/logo.png" Cols="6" Rows="3"/>
  </VStack>
</Gooey>`)},
		"assets/logo.png": {Data: pngBytes(t, 4, 4)},
	}
	ctx := &Context{Values: map[string]any{}}
	if _, err := Load(fsys, "page.gooey", ctx); err != nil {
		t.Fatal(err)
	}
	im, err := Find[*components.Image](ctx, "logo")
	if err != nil {
		t.Fatal(err)
	}
	if got := im.Src.Get().Bounds(); got.Dx() != 4 || got.Dy() != 4 {
		t.Fatalf("decoded image is %v, want 4×4", got)
	}
	if im.Cols.Get() != 6 || im.Rows.Get() != 3 {
		t.Fatalf("size = %d×%d cells, want 6×3", im.Cols.Get(), im.Rows.Get())
	}
}

func TestImageBoundSrcSharesTheHandle(t *testing.T) {
	fsys := fstest.MapFS{
		"page.gooey": {Data: []byte(`<Gooey>
  <VStack>
    <Image Name="pic" Src="{{.Logo}}" Cols="{{.W}}" Rows="4"/>
  </VStack>
</Gooey>`)},
	}
	logo := prop.NewSource[image.Image](image.NewNRGBA(image.Rect(0, 0, 2, 2)))
	width := prop.NewSource(8)
	ctx := &Context{Values: map[string]any{"Logo": logo, "W": width}}
	if _, err := Load(fsys, "page.gooey", ctx); err != nil {
		t.Fatal(err)
	}
	im, err := Find[*components.Image](ctx, "pic")
	if err != nil {
		t.Fatal(err)
	}
	if im.Src != logo {
		t.Fatal("bound Src is not the viewmodel's own handle")
	}
	if im.Cols != width {
		t.Fatal("bound Cols is not the viewmodel's own handle")
	}
}

func TestImageMissingFileIsATypedLoadError(t *testing.T) {
	fsys := fstest.MapFS{
		"page.gooey": {Data: []byte(`<Gooey><Image Src="nope.png" Cols="4" Rows="2"/></Gooey>`)},
	}
	_, err := Load(fsys, "page.gooey", &Context{Values: map[string]any{}})
	if err == nil {
		t.Fatal("a missing image loaded")
	}
	var le *imaging.Error
	if !errors.As(err, &le) {
		t.Fatalf("error is %T (%v), want to unwrap *imaging.Error", err, err)
	}
	if le.Path != "nope.png" {
		t.Fatalf("error names %q, want nope.png", le.Path)
	}
	if !strings.Contains(err.Error(), `<Image Src="nope.png">`) {
		t.Fatalf("load error does not name the element: %v", err)
	}
}

func TestImageUndecodableFileNamesTheFormat(t *testing.T) {
	fsys := fstest.MapFS{
		"page.gooey": {Data: []byte(`<Gooey><Image Src="bad.png" Cols="4" Rows="2"/></Gooey>`)},
		"bad.png":    {Data: []byte("\x89PNG\r\n\x1a\ngarbage")},
	}
	_, err := Load(fsys, "page.gooey", &Context{Values: map[string]any{}})
	var le *imaging.Error
	if !errors.As(err, &le) {
		t.Fatalf("error is %T (%v), want to unwrap *imaging.Error", err, err)
	}
	if le.Format != "png" {
		t.Fatalf("error names format %q, want png", le.Format)
	}
}

func TestImageRequiresSrcAndSize(t *testing.T) {
	cases := map[string]string{
		"no Src":  `<Gooey><Image Cols="4" Rows="2"/></Gooey>`,
		"no Cols": `<Gooey><Image Src="{{.Logo}}" Rows="2"/></Gooey>`,
		"no Rows": `<Gooey><Image Src="{{.Logo}}" Cols="4"/></Gooey>`,
		"zero":    `<Gooey><Image Src="{{.Logo}}" Cols="0" Rows="2"/></Gooey>`,
	}
	logo := prop.NewSource[image.Image](image.NewNRGBA(image.Rect(0, 0, 1, 1)))
	for name, src := range cases {
		fsys := fstest.MapFS{"page.gooey": {Data: []byte(src)}}
		if _, err := Load(fsys, "page.gooey", &Context{Values: map[string]any{"Logo": logo}}); err == nil {
			t.Errorf("%s: loaded without complaint", name)
		}
	}
}

func TestImageLiteralSrcInsideAMarkupOnlyControl(t *testing.T) {
	// The control's own FS is where its literal assets resolve — the
	// same isolation its bindings get.
	fsys := fstest.MapFS{
		"page.gooey": {Data: []byte(`<Gooey><VStack><Badge/></VStack></Gooey>`)},
		"badge.gooey": {Data: []byte(`<Gooey>
  <Image Name="badge-img" Src="badge.png" Cols="4" Rows="2"/>
</Gooey>`)},
		"badge.png": {Data: pngBytes(t, 2, 2)},
	}
	ctx := &Context{Values: map[string]any{}, Includes: fsys}
	if _, err := Load(fsys, "page.gooey", ctx); err != nil {
		t.Fatal(err)
	}
}

func TestImageFromBytesWithoutAnyFSIsALoadError(t *testing.T) {
	src := []byte(`<Gooey><Image Src="logo.png" Cols="4" Rows="2"/></Gooey>`)
	_, err := Build(src, &Context{Values: map[string]any{}})
	if err == nil || !strings.Contains(err.Error(), "no file system") {
		t.Fatalf("err = %v, want the no-file-system load error", err)
	}
}
