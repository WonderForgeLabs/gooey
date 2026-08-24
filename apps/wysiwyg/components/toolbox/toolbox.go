// Package toolbox turns the icon NAMES the element catalog declares into
// tinted pictures the palette can bind.
//
// # Why a name on one side and a picture on the other
//
// markup.ElementDef.Icon is a string, and deliberately nothing more.
// Rasterizing an SVG needs oksvg and rasterx, which live in the nested
// imagefmt/svg module precisely so core's dependency graph stays free of
// them; a field holding an image.Image — or an fs.FS, or a decoder —
// would have pulled a vector renderer into every package that imports
// markup, including every docs/learn example. So core states WHAT AN
// ELEMENT IS and stops. This package is the other half: it owns the
// assets, the renderer, and the colour, and it is an app package because
// all three are the app's business.
//
// The indirection is KindStyle's, one layer up. A style attribute names
// a row in the app's style table and an app that swaps its palette swaps
// the table; an icon name resolves against whatever set the host loaded.
// Nothing in core is pinned to Codicons by this arrangement, and a host
// with a different icon vendor answers the same names its own way.
//
// # The tint is a handle, not a colour
//
// For returns a *prop.Property[image.Image] that reads the tint INSIDE
// its computed, which is what subscribes it. Flipping the tint therefore
// dirties exactly the icon handles, and through them exactly the <Image>
// components bound to them — no invalidate call, no cache to sweep, and
// no other component in the tree touched. That is the whole reason the
// tint is not baked into a raster at load: a baked icon needs somebody
// to remember to rebuild it, and nothing in the framework would report
// the frame where they forgot.
//
// # What pixels cost here
//
// Same bill the activity rail pays, stated again because it applies per
// row rather than per rail: a graphics protocol draws the icon on the
// pixel plane above the cells, and where there is none the framework's
// halfblock fallback draws it INTO the cells at half vertical
// resolution. A one-row icon is four halfblock pixels, which is a smudge
// rather than a glyph. The toolbox stays readable because the NAME is
// text either way; the icon is the thing that degrades.
//
// That is also why the palette's row template draws its own selection
// (it mentions _selected, which stands the house highlight down). The
// house highlight re-styles the cells a row painted, and over pixel
// content that is either invisible — a protocol paints above the cells —
// or a photo-negative of the icon, in halfblock where the picture IS the
// cells. cmd/typeahead reached the same conclusion for its album covers.
//
// Attribution: Codicons are copyright Microsoft Corporation, licensed
// CC-BY-4.0. See icons/LICENSE.
package toolbox

import (
	"image"
	"image/color"
	"io/fs"
	"sync"

	"github.com/WonderForgeLabs/gooey/imagefmt/svg"
	"github.com/WonderForgeLabs/gooey/prop"
)

// Dir is where the icon assets live, relative to the editor root.
const Dir = "components/toolbox/icons"

// IconPx is the raster's size in pixels, and it is a PIXEL count for the
// reason the rail's is: the rasterizer needs the number before it draws.
// A palette row is one cell tall and the icon two cells wide, so on a
// common 10x20 cell that is a 20x20 target.
const IconPx = 20

// Icons resolves declared icon names against one asset directory, at one
// size, in one bound tint.
//
// It is NOT a second svg.IconSet. That type already owns the three
// things a vector icon needs — rasterize at target size, substitute
// currentColor before rendering, cache per (path, tint) — and this wraps
// it with the two things it deliberately does not know: the extension
// convention that turns a catalog name into a file, and the property
// handle that makes the colour live.
type Icons struct {
	set  *svg.IconSet
	tint *prop.Property[color.Color]

	mu sync.Mutex
	// by is the per-NAME handle cache, and it exists for a damage reason
	// rather than a speed one. The palette re-projects whenever the
	// document revision changes, and a fresh computed per projection
	// would hand ItemsView a new property every time — which its
	// *prop.Property[image.Image] case answers by comparing the
	// PICTURES, so it would still be damage-free, but only by accident
	// of the raster cache underneath. Handing back the same handle makes
	// it true by pointer compare, which is the check that cannot be
	// broken by a change to the raster cache.
	by map[string]*prop.Property[image.Image]
}

// New reads icons from fsys, rooted at Dir, tinted from tint.
//
// tint is a handle rather than a colour: see the package comment. A nil
// handle is legal and means "as authored", which for a monochrome
// codicon is a load error rather than a black glyph — that is
// svg.IconSet.At's contract, and Preload is where it surfaces.
func New(fsys fs.FS, tint *prop.Property[color.Color]) *Icons {
	sub, err := fs.Sub(fsys, Dir)
	if err != nil {
		// fs.Sub rejects only an invalid path and Dir is a constant, so
		// this cannot fire without an edit to Dir itself. Falling back
		// to the unrooted FS keeps that edit a per-icon "file does not
		// exist" rather than a nil dereference here — the same choice
		// the activity rail makes.
		sub = fsys
	}
	return &Icons{
		set:  svg.Icons(sub, IconPx),
		tint: tint,
		by:   map[string]*prop.Property[image.Image]{},
	}
}

// File is the asset path a catalog name resolves to. The convention is
// the whole mapping: a name, plus this package's extension.
func File(name string) string { return name + ".svg" }

// Preload rasterizes every name in every tint the theme can produce, so
// a missing or malformed asset is a LOAD error naming the file rather
// than a blank column discovered mid-paint.
//
// Painting cannot report an error — Render returns nothing — so this is
// the only place an icon problem can be stated. Pass every tint the
// theme switches between, not just the current one: preloading one tint
// leaves the other's first raster to happen inside a paint, where a
// failure has nowhere to go.
//
// An empty name is skipped rather than rejected. An element that
// declares no icon is a fact about the catalog, not an error in it.
func (ic *Icons) Preload(names []string, tints ...color.Color) error {
	files := make([]string, 0, len(names))
	for _, n := range names {
		if n == "" {
			continue
		}
		files = append(files, File(n))
	}
	return ic.set.Preload(files, tints...)
}

// For is the handle a palette row binds to Image.Src.
//
// An empty name returns a nil handle, which is how a row says it has no
// picture: ItemsView's projection case tolerates one, and the <Image>
// renders nothing rather than substituting a placeholder. An element
// with no declared icon must look like an element with no declared icon.
//
// The tint is read INSIDE the computed. Reading it out here and closing
// over the value would produce an icon that rasterizes once and never
// changes again, and nothing in the framework would report it — the
// single failure this whole design is arranged to prevent.
func (ic *Icons) For(name string) *prop.Property[image.Image] {
	if name == "" {
		return nil
	}
	ic.mu.Lock()
	defer ic.mu.Unlock()
	if h, ok := ic.by[name]; ok {
		return h
	}
	file := File(name)
	h := prop.NewComputed(func() image.Image {
		var tint color.Color
		if ic.tint != nil {
			tint = ic.tint.Get()
		}
		// Preload has already rasterized this file in every tint the
		// theme produces, so At is a cache hit here. An error can only
		// mean a tint nobody preloaded, and a paint cannot report it —
		// so the icon goes missing rather than the terminal going with
		// it. Preload is the check; this is the seatbelt.
		img, err := ic.set.At(file, tint)
		if err != nil {
			return nil
		}
		return img
	})
	ic.by[name] = h
	return h
}
