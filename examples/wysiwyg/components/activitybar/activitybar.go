// Package activitybar is the VS Code activity rail: a narrow vertical
// strip of icons down the left edge, with the active one marked.
//
// It is the first piece of chrome in this editor drawn in PIXELS rather
// than cells, and it is a deliberate choice of where to spend them. A
// terminal draws text well and shapes badly; an icon is a shape.
//
// # The icons are real assets, rasterized at the size they are drawn
//
// They are VS Code's own Codicons — the SVGs in icons/ next to this file,
// fetched from microsoft/vscode-codicons. Two consequences worth stating,
// because both were the point:
//
//   - VECTOR, RENDERED AT TARGET SIZE. A codicon is a 16x16 document.
//     Decoding it through the imaging registry rasterizes at that
//     intrinsic size, and scaling a 16px raster up to a 32px slot is
//     exactly the blur this was meant to avoid. svg.RasterizeAt renders
//     the paths at the destination size instead, so the curves are drawn
//     sharp rather than resampled.
//   - THE TINT IS PART OF THE SOURCE. Codicons declare
//     fill="currentColor", which is a CSS cascade the rasterizer has no
//     cascade for. Substituting the colour into the document before
//     rasterizing both resolves that and gives active/inactive states for
//     free — the icon is drawn in its colour rather than drawn grey and
//     recoloured after.
//
// Attribution: Codicons are copyright Microsoft Corporation, licensed
// CC-BY-4.0. See icons/LICENSE.
//
// # What pixels cost, stated once
//
// A sixel band is invisible to every screen-level check this repo has.
// screen_text reads the CELL plane, so it reports blanks where the rail
// is; agg records the cell plane, so a demo capture shows nothing there
// either. The rail is therefore verified by a human looking at a terminal
// and by unit tests over the image it generates — which is why RailImage
// is a plain function returning an image.Image.
//
// Where the terminal has no pixel protocol the framework's halfblock
// fallback draws the same image into the cell plane at half vertical
// resolution. The rail degrades; it does not disappear.
package activitybar

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"io/fs"
	"strings"
	"sync"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/imagefmt/svg"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
)

// Dir is where the icon assets live, relative to the editor root.
const Dir = "components/activitybar/icons"

// Icon is one entry on the rail: what it means, and the asset that draws
// it.
type Icon struct {
	// Name is what the icon stands for — the side bar it selects.
	Name string
	// File is the SVG's name inside Dir.
	File string
}

// DefaultIcons is the editor's rail, in VS Code's order: what you are
// building, what you can add to it, what it is made of, what is wrong
// with it.
var DefaultIcons = []Icon{
	{Name: "designer", File: "layout.svg"},
	{Name: "toolbox", File: "library.svg"},
	{Name: "markup", File: "code.svg"},
	{Name: "problems", File: "warning.svg"},
}

// Geometry of the rail, in PIXELS. A terminal cell is commonly 10x20, so
// a 40px rail is about four cells and a 40px slot about two rows.
//
// The icon is rendered at iconPx and NOT at some fraction of the slot
// resolved later: the rasterizer needs the number before it draws, which
// is the whole reason these are pixels rather than cells.
const (
	railW   = 40
	slotH   = 40
	iconPx  = 24
	markerW = 3
)

var (
	bg       = color.RGBA{0x1e, 0x1e, 0x24, 0xff} // the rail's own ground
	inactive = color.RGBA{0x86, 0x88, 0x99, 0xff}
	active   = color.RGBA{0xe8, 0xeb, 0xf7, 0xff}
	marker   = color.RGBA{0x6c, 0x9c, 0xff, 0xff}
)

// Builder registers the rail as <ActivityBar Sel="{{.Selected}}"/>.
//
// Built with the OBJECT MODEL, not a markup file: the rail is one
// <Image>, and a .gooey holding a single element is a parse step and a
// file to keep in sync in exchange for nothing. Markup is for layouts.
//
// ONE Image, not one per icon. A sixel band writes pixels into the cell
// grid and damages the cells it covers as a unit, so four stacked bands
// are four damage rects that can tear against each other when only the
// selection moved. One band repaints once.
//
// Src is a COMPUTED image — a picture derived from the selection, which
// redraws when the selection changes because a computed that reads a
// property subscribes to it. No invalidate call, no clock.
func Builder(fsys fs.FS, icons []Icon) markup.Builder {
	if len(icons) == 0 {
		icons = DefaultIcons
	}
	r := &Renderer{fsys: fsys, icons: icons}
	return func(e markup.Element, ctx *markup.Context) (gooey.Component, error) {
		sel, err := selProperty(e, ctx)
		if err != nil {
			return nil, err
		}
		// A load-time read, so a missing or malformed asset is a load
		// error naming the file rather than an empty strip at runtime.
		if err := r.Preload(); err != nil {
			return nil, err
		}
		// Get is INSIDE the computed, which is what records the
		// dependency. Reading sel outside and closing over the value
		// would produce a rail that draws once and never changes again —
		// and nothing in the framework would report it.
		return &components.Image{Src: prop.NewComputed(func() image.Image {
			return r.Rail(sel.Get())
		})}, nil
	}
}

// Renderer holds the rasterized icons so the rail is not re-rendered from
// SVG on every repaint.
//
// The cache is keyed by (file, tint) because tinting happens BEFORE
// rasterization — an icon in two colours is two rasters, not one raster
// recoloured. Four icons in two states is eight small images, built once.
type Renderer struct {
	fsys  fs.FS
	icons []Icon

	mu    sync.Mutex
	cache map[string]image.Image
	err   error
}

// Preload rasterizes every icon in both states, so a broken asset is
// found at load rather than at first paint.
func (r *Renderer) Preload() error {
	for _, ic := range r.icons {
		for _, c := range []color.RGBA{inactive, active} {
			if _, err := r.icon(ic.File, c); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *Renderer) icon(file string, tint color.RGBA) (image.Image, error) {
	key := fmt.Sprintf("%s#%02x%02x%02x", file, tint.R, tint.G, tint.B)
	r.mu.Lock()
	defer r.mu.Unlock()
	if img, ok := r.cache[key]; ok {
		return img, nil
	}
	path := Dir + "/" + file
	src, err := fs.ReadFile(r.fsys, path)
	if err != nil {
		return nil, fmt.Errorf("activitybar: %s: %w", path, err)
	}
	// currentColor is a CSS cascade with no cascade here. Substituting it
	// is both the fix and the tinting mechanism.
	hex := fmt.Sprintf("#%02x%02x%02x", tint.R, tint.G, tint.B)
	doc := strings.ReplaceAll(string(src), "currentColor", hex)
	img, err := svg.RasterizeAt(bytes.NewReader([]byte(doc)), iconPx, iconPx)
	if err != nil {
		return nil, fmt.Errorf("activitybar: %s: %w", path, err)
	}
	if r.cache == nil {
		r.cache = map[string]image.Image{}
	}
	r.cache[key] = img
	return img, nil
}

// Rail draws the whole strip: background, every icon in its state, and
// the selection marker beside the active one.
func (r *Renderer) Rail(sel int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, railW, slotH*len(r.icons)))
	draw.Draw(img, img.Bounds(), &image.Uniform{bg}, image.Point{}, draw.Src)

	for i, ic := range r.icons {
		top := i * slotH
		tint := inactive
		if i == sel {
			tint = active
			// VS Code's active cue: a bar on the leading edge, not a
			// filled slot. A filled slot at this size reads as a button
			// that has been pressed and stuck.
			draw.Draw(img,
				image.Rect(0, top, markerW, top+slotH),
				&image.Uniform{marker}, image.Point{}, draw.Src)
		}
		glyph, err := r.icon(ic.File, tint)
		if err != nil {
			continue // Preload already reported it; do not panic mid-paint
		}
		at := image.Rect(
			(railW-iconPx)/2, top+(slotH-iconPx)/2,
			(railW-iconPx)/2+iconPx, top+(slotH-iconPx)/2+iconPx)
		// Over, not Src: the icons are anti-aliased with a real alpha
		// edge, and Src would paste their transparent background as
		// black squares — which is precisely the "looks like a rendering
		// fault" outcome that made these pixels worth spending.
		draw.Draw(img, at, glyph, image.Point{}, draw.Over)
	}
	return img
}

// selProperty resolves Sel=, and REQUIRES it to be a binding.
//
// A literal would be a rail that can never change its selection, which is
// a mistake worth a load error rather than a puzzle at runtime — the same
// rule the catalog applies to every binding-only attribute.
func selProperty(e markup.Element, ctx *markup.Context) (*prop.Property[int], error) {
	raw, ok := e.Attrs["Sel"]
	if !ok {
		return nil, fmt.Errorf("markup: <ActivityBar> needs Sel=; the rail is drawn from the selection")
	}
	v, err := ctx.BindingValue(raw)
	if err != nil {
		return nil, fmt.Errorf("markup: <ActivityBar> Sel=%q: %w", raw, err)
	}
	p, ok := v.(*prop.Property[int])
	if !ok {
		return nil, fmt.Errorf("markup: <ActivityBar> Sel=%q is %T; the rail needs *prop.Property[int]", raw, v)
	}
	return p, nil
}
