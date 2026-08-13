// Package svg registers an SVG rasterizer with gooey's imaging
// registry. Importing it is the whole opt-in:
//
//	import _ "github.com/WonderForgeLabs/gooey/imagefmt/svg"
//
// After that, imaging.Load / Decode — and therefore markup's
// <Image Src="logo.svg"> — accept SVG files. Rasterization happens at
// the document's intrinsic size (its viewBox, or width/height),
// scaled down to at most 1024 px on the long side: the pixel pipeline
// rescales to a cell rectangle anyway, so the cap bounds memory
// without costing visible resolution.
//
// The renderer is oksvg + rasterx — the reason this lives in a nested
// module rather than core (see go.mod).
package svg

import (
	"bytes"
	"fmt"
	"image"
	"io"

	"github.com/srwiley/oksvg"
	"github.com/srwiley/rasterx"

	"github.com/WonderForgeLabs/gooey/imaging"
)

// maxDim caps the raster's long side, in pixels.
const maxDim = 1024

func init() {
	imaging.Register(imaging.Format{Name: "svg", Match: match, Decode: decode})
}

// match sniffs for an XML document whose root element is <svg. The
// registry hands it the first 512 bytes, which is room enough for a
// BOM, an XML declaration, and comments before the root.
func match(h []byte) bool {
	h = bytes.TrimPrefix(h, []byte("\xef\xbb\xbf")) // UTF-8 BOM
	trimmed := bytes.TrimLeft(h, " \t\r\n")
	if len(trimmed) == 0 || trimmed[0] != '<' {
		return false
	}
	return bytes.Contains(h, []byte("<svg"))
}

// RasterizeAt renders an SVG at a size the CALLER chooses, rather than at
// the document's intrinsic size.
//
// The registry path deliberately rasterizes at intrinsic size, which is
// right when the pixel pipeline is going to rescale to a cell rectangle
// anyway. It is wrong for the case this exists for: a 16x16 icon that
// needs to land at 32x32 on the pixel plane. Rasterizing at 16 and
// scaling up throws away the one advantage a vector source has — the
// renderer can draw it sharp at any size, and only if it is asked to.
//
// Aspect is preserved: the icon is fitted into w x h and centred, so a
// non-square target letterboxes rather than distorting.
func RasterizeAt(r io.Reader, w, h int) (image.Image, error) {
	if w < 1 || h < 1 {
		return nil, fmt.Errorf("svg: target size %dx%d", w, h)
	}
	icon, err := oksvg.ReadIconStream(r)
	if err != nil {
		return nil, err
	}
	vw, vh := icon.ViewBox.W, icon.ViewBox.H
	if vw <= 0 || vh <= 0 {
		return nil, fmt.Errorf("svg: no intrinsic size (needs a viewBox or width/height)")
	}
	// Fit, then centre. SetTarget takes the destination rectangle, so the
	// offsets are where the fitted box starts.
	s := min(float64(w)/vw, float64(h)/vh)
	dw, dh := vw*s, vh*s
	icon.SetTarget((float64(w)-dw)/2, (float64(h)-dh)/2, dw, dh)
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	scanner := rasterx.NewScannerGV(w, h, img, img.Bounds())
	icon.Draw(rasterx.NewDasher(w, h, scanner), 1)
	return img, nil
}

func decode(r io.Reader) (image.Image, error) {
	icon, err := oksvg.ReadIconStream(r)
	if err != nil {
		return nil, err
	}
	w, h := icon.ViewBox.W, icon.ViewBox.H
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("svg: no intrinsic size (needs a viewBox or width/height)")
	}
	// Scale the intrinsic size down to the cap, preserving aspect.
	scale := 1.0
	if long := max(w, h); long > maxDim {
		scale = maxDim / long
	}
	rw, rh := int(w*scale+0.5), int(h*scale+0.5)
	if rw < 1 {
		rw = 1
	}
	if rh < 1 {
		rh = 1
	}
	icon.SetTarget(0, 0, float64(rw), float64(rh))
	img := image.NewRGBA(image.Rect(0, 0, rw, rh))
	scanner := rasterx.NewScannerGV(rw, rh, img, img.Bounds())
	icon.Draw(rasterx.NewDasher(rw, rh, scanner), 1)
	return img, nil
}
