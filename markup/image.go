package markup

import (
	"fmt"
	"image"
	"strconv"
	"strings"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/imaging"
	"github.com/WonderForgeLabs/gooey/prop"
)

// buildImage is the <Image Src="logo.png" Cols="20" Rows="10"/> element.
//
// Src takes either form the rest of the vocabulary does: a binding
// ({{.Logo}}) shares the viewmodel's *prop.Property[image.Image]
// handle, and a literal is a file path resolved through the SAME fs.FS
// the page was loaded from — assets ship the way markup does, and a
// bad path or undecodable file is a load-time error naming both. The
// decode happens here, at build time, which is what makes hot reload
// re-read the file: a page rebuild re-runs this builder.
//
// Cols and Rows are required. An Image without a size measures 0×0 and
// silently vanishes, and a component that can disappear by omission is
// a debugging session, not a default.
func buildImage(e Element, ctx *Context) (gooey.Component, error) {
	raw := strings.TrimSpace(e.Attrs["Src"])
	if raw == "" {
		return nil, fmt.Errorf(`markup: <Image> needs Src — a file path in the page's FS, or a binding like {{.Logo}}`)
	}
	var src *prop.Property[image.Image]
	if bindRe.MatchString(raw) {
		var err error
		if src, err = Bound[image.Image](e, ctx, "Src"); err != nil {
			return nil, err
		}
	} else {
		fsys := ctx.assets()
		if fsys == nil {
			return nil, fmt.Errorf("markup: <Image Src=%q>: no file system to load from — this tree was built from bytes; use markup.Load, set Context.Includes, or bind Src", raw)
		}
		img, err := imaging.Load(fsys, raw)
		if err != nil {
			return nil, fmt.Errorf("markup: <Image Src=%q>: %w", raw, err)
		}
		src = components.Img(img)
	}
	cols, err := cellCount(e, ctx, "Cols")
	if err != nil {
		return nil, err
	}
	rows, err := cellCount(e, ctx, "Rows")
	if err != nil {
		return nil, err
	}
	return &components.Image{Src: src, Cols: cols, Rows: rows}, nil
}

// cellCount reads a required size-in-cells attribute: a positive int
// literal, or a binding to the viewmodel's own int handle.
func cellCount(e Element, ctx *Context, attr string) (*prop.Property[int], error) {
	raw := strings.TrimSpace(e.Attrs[attr])
	if raw == "" {
		return nil, fmt.Errorf("markup: <%s> needs %s — a cell count or a binding", e.Name, attr)
	}
	if bindRe.MatchString(raw) {
		return Bound[int](e, ctx, attr)
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return nil, fmt.Errorf("markup: <%s %s=%q>: %w", e.Name, attr, raw, err)
	}
	if n <= 0 {
		return nil, fmt.Errorf("markup: <%s %s=%q>: must be positive", e.Name, attr, raw)
	}
	return components.Cells(n), nil
}
