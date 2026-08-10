package svg

import (
	"image/color"
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey/imaging"
)

// fixture is a red square filling the left half of a 20×10 viewBox.
const fixture = `<?xml version="1.0" encoding="UTF-8"?>
<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 10">
  <rect x="0" y="0" width="10" height="10" fill="#ff0000"/>
</svg>`

func TestDecodeRegistersIntoTheImagingRegistry(t *testing.T) {
	img, err := imaging.Decode(strings.NewReader(fixture), "fixture.svg")
	if err != nil {
		t.Fatal(err)
	}
	b := img.Bounds()
	if b.Dx() != 20 || b.Dy() != 10 {
		t.Fatalf("rasterized to %v, want the 20×10 intrinsic size", b)
	}
	r, _, _, a := img.At(5, 5).RGBA()
	if a == 0 || r < 0x8000 {
		t.Fatalf("left half is %v, want red", color.RGBAModel.Convert(img.At(5, 5)))
	}
	_, _, _, a = img.At(15, 5).RGBA()
	if a != 0 {
		t.Fatalf("right half is painted (alpha %d), want transparent", a)
	}
}

func TestOversizedDocumentIsCappedPreservingAspect(t *testing.T) {
	huge := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 4096 2048"><rect width="4096" height="2048" fill="#00ff00"/></svg>`
	img, err := imaging.Decode(strings.NewReader(huge), "huge.svg")
	if err != nil {
		t.Fatal(err)
	}
	b := img.Bounds()
	if b.Dx() != 1024 || b.Dy() != 512 {
		t.Fatalf("rasterized to %v, want 1024×512 (capped, aspect kept)", b)
	}
}

func TestNoIntrinsicSizeIsAnError(t *testing.T) {
	bare := `<svg xmlns="http://www.w3.org/2000/svg"><rect width="10" height="10"/></svg>`
	if _, err := imaging.Decode(strings.NewReader(bare), "bare.svg"); err == nil {
		t.Fatal("an unsizable document decoded")
	}
}

func TestMatchDoesNotClaimOtherXML(t *testing.T) {
	if match([]byte(`<?xml version="1.0"?><Gooey><Text>hi</Text></Gooey>`)) {
		t.Fatal("match claimed a non-SVG XML document")
	}
	if !match([]byte("\xef\xbb\xbf  <!-- logo -->\n<svg viewBox=\"0 0 4 4\"/>")) {
		t.Fatal("match rejected an SVG behind a BOM and a comment")
	}
}
