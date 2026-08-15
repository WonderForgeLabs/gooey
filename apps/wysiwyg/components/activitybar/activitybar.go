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
// fetched from microsoft/vscode-codicons. A codicon is a 16x16 monochrome
// document declaring fill="currentColor", so it has to be rasterized at
// the size it will be drawn and tinted before it is rasterized rather than
// after. Both live in svg.IconSet now, along with the cache that keeps
// either off the paint path; this package supplies the three things that
// are actually its own — Dir, iconPx, and the two state colours.
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
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"io/fs"
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
	// markerBlurred is the same hue at roughly a third of the distance
	// from the rail's own ground: still visibly the accent, no longer
	// competing with whatever does hold the keyboard. Mixed here rather
	// than emitted with alpha because sixel has no alpha channel at all —
	// a translucent marker is not expressible, so the blend is done in the
	// source pixels.
	markerBlurred = color.RGBA{0x36, 0x48, 0x74, 0xff}
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
// Def is Builder plus the declaration, for hosts that register through
// Context.Elements. It is what makes the rail DESCRIBABLE rather than
// merely nameable, and the bug it fixes was reported from the running
// editor: clicking ActivityBar in the toolbox emitted
//
//	<ActivityBar Name="ActivityBar1"/>
//
// which fails to load with "needs Sel=". The palette seeds an inserted
// element's required attributes from AttrSpec.Required + GoType, and a
// Builder registration has no Attrs to read — so it offered an element
// it could not produce valid markup for. Declaring Sel closes that: the
// palette now seeds a bound int handle and the insert loads.
//
// Registering through Elements also turns on the attribute check, so
// <ActivityBar Sell="{{.X}}"> becomes a load error with a near-miss
// suggestion instead of an attribute nothing reads. Builder is kept for
// hosts that predate the seam and for the tests that use it directly.
func Def(fsys fs.FS, icons []Icon) *markup.ElementDef {
	return &markup.ElementDef{
		Name: "ActivityBar",
		// The rail IS an Image — one sixel band, not one per icon — and
		// the proto is what the catalog derives the behavioural axes
		// from. Focusable is true through the Segmented inside it, but
		// that is the INSTANCE's business; the proto answers for the
		// type, which is what a catalog describes.
		Proto: &components.Image{},
		Known: true,
		Doc:   "A vertical icon rail. The picture is derived from the selection.",
		Attrs: []markup.AttrSpec{
			// Required AND binding-only, which is exactly what
			// selProperty enforces at load — a literal would be a rail
			// that can never change its selection. The two statements
			// have to agree: this one is what the palette reads, that one
			// is what actually rejects, and a declaration that promised a
			// literal would seed markup the builder refuses.
			{
				Name: "Sel", Kind: markup.KindBinding, Binds: markup.BindsBinding,
				GoType: "int", Required: true, Origin: markup.OriginRegistered,
				Doc: "The selected slot, as a live *prop.Property[int]. The rail is drawn from it.",
			},
		},
		Children: markup.ChildSpec{Mode: markup.ModeLeaf},
		Build:    Builder(fsys, icons),
	}
}

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
		// The strip is built BEFORE the picture, because the picture
		// depends on the strip's focus. Src is assigned after.
		seg := &components.Segmented{
			Selected: sel,
			Vertical: true,
			Count:    len(icons),
		}
		// Get is INSIDE the computed, which is what records the
		// dependency. Reading sel outside and closing over the value
		// would produce a rail that draws once and never changes again —
		// and nothing in the framework would report it.
		//
		// IsFocused() is read the same way and for the same reason: it is a
		// plain property read (input.go:159), so calling it here subscribes
		// the picture to focus, and moving focus onto or off the rail
		// redraws exactly this image. Reading it outside the closure would
		// bake in "unfocused" forever.
		img := &components.Image{Src: prop.NewComputed(func() image.Image {
			return r.Rail(sel.Get(), seg.IsFocused())
		})}
		seg.Child = img
		// The Image is a PICTURE — no focus, no keys, no hit-testing — so
		// on its own the rail was unselectable, which was the bug.
		//
		// The behaviour comes from components.Segmented, which already had
		// all of it: bound int selection, clamped, wrapping arrows, wheel
		// notches, click-to-segment, focus stop. It gained a Vertical axis
		// and a Child so the picture could be this rail instead of drawn
		// labels. Writing a second strip here would have been a third copy
		// of that behaviour in the repo — Tabs re-implements it too.
		return seg, nil
	}
}

// Renderer holds the rasterized icons so the rail is not re-rendered from
// SVG on every repaint.
//
// The tint-substitution, rasterize-at-size and per-(path, tint) cache that
// used to live here in private form are now svg.IconSet. They were never
// specific to this rail — a toolbox, a tab strip and a tree all want the
// same three — and keeping a second copy here would have meant the next
// consumer wrote a third. What stays is what IS specific: which directory,
// which pixel size, and the two state colours.
type Renderer struct {
	fsys  fs.FS
	icons []Icon

	mu      sync.Mutex
	iconSet *svg.IconSet
}

// Preload rasterizes every icon in both states, so a broken asset is
// found at load rather than at first paint.
func (r *Renderer) Preload() error {
	files := make([]string, len(r.icons))
	for i, ic := range r.icons {
		files[i] = ic.File
	}
	return wrap(r.set().Preload(files, inactive, active))
}

// set builds the icon set on first use rather than in a constructor,
// because a Renderer is also made as a plain struct literal — the tests
// do it, so Builder is not the only way in and a constructor-assigned
// field would be nil on those. Under the mutex, so two paints racing for
// the first icon share one set instead of one silently discarding the
// other's cache.
func (r *Renderer) set() *svg.IconSet {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.iconSet == nil {
		sub, err := fs.Sub(r.fsys, Dir)
		if err != nil {
			// fs.Sub rejects only an invalid path and Dir is a constant,
			// so this cannot fire without an edit to Dir itself. Falling
			// back to the unrooted FS keeps that edit a per-icon "file
			// does not exist" rather than a nil dereference here.
			sub = r.fsys
		}
		r.iconSet = svg.Icons(sub, iconPx)
	}
	return r.iconSet
}

func (r *Renderer) icon(file string, tint color.RGBA) (image.Image, error) {
	img, err := r.set().At(file, tint)
	return img, wrap(err)
}

// wrap names the directory the icon set was rooted at. The set reports the
// path it was given, which is relative to Dir — on its own that is a file
// name with no indication of where to go and look for it.
func wrap(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("activitybar %s: %w", Dir, err)
}

// Rail draws the whole strip: background, every icon in its state, and
// the selection marker beside the active one.
//
// focused changes the CUE, not the selection. Reported against the running
// editor as "no idea where focus is": the marker was the same bright blue
// whether the rail had the keyboard or not, so the one thing on screen that
// looked active was lying half the time. Focus is a property, so this is a
// picture derived from state like everything else — see Builder.
func (r *Renderer) Rail(sel int, focused bool) image.Image {
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
			//
			// DIMMER WHEN THE RAIL DOES NOT HOLD FOCUS. A colour change
			// rather than removing the bar: which view is showing is still
			// true when focus is elsewhere, so the cue must stay legible
			// and only stop claiming the keyboard.
			m := marker
			if !focused {
				m = markerBlurred
				tint = inactive
			}
			draw.Draw(img,
				image.Rect(0, top, markerW, top+slotH),
				&image.Uniform{m}, image.Point{}, draw.Src)
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
