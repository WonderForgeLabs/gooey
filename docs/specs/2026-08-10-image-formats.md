# Image file loading: the imaging registry, markup Src, and the SVG module (issue #90)

**Status:** executed ([PR #103](https://github.com/WonderForgeLabs/gooey/pull/103)). Requested by Elan ("i need an image
(png/jpg/gif/svg/ico/bmp) component to use").

**Date:** 2026-08-10

## Plan

`components.Image` already renders a decoded `image.Image` on either
plane; what it lacked was any way to get one out of a file, and markup
had no `<Image>` element at all. The plan:

1. A decoder registry in core — `imaging` — with `Load(fsys, path)` and
   `Decode(r, name)`. Table-driven content sniffing, no reflection.
   png/jpeg/gif from stdlib, bmp from `golang.org/x/image`, ico from a
   small in-repo parser. Typed errors naming path and format.
2. SVG rasterization in a nested module (`imagefmt/svg`, own go.mod,
   `replace ../..` like mcp/ and handlers/temporal/), registering into
   the core registry from its init — a blank import is the opt-in.
3. `<Image Src Cols Rows/>` in markup: a literal Src resolves through
   the same `fs.FS` the page was loaded from and decodes at build time;
   `Src="{{.Logo}}"` binds an `image.Image` property; Cols/Rows take
   literals or int bindings.
4. `components.LoadImg(fsys, path)` as the Go-side convenience.
5. No animation: GIF is its first frame; playing frames is a player's
   job (the browser demo's gifplay pattern), a future issue if wanted.

## Executed

As planned. The decisions worth arguing with:

### The registry is its own package, not more of graphics

`graphics` answers "how do pixels reach the terminal"; decoding answers
"how do pixels come out of a file". Image (in `components`) imports
`graphics`; nothing in the render path wants decoders, and the SVG
module wants to import the registry WITHOUT dragging in encoders.
`imaging` imports only stdlib + `x/image` — it sits below everything.

`x/image` in core is a recorded judgment call: it is the Go project's
own extension repo, versioned and dependency-free, which is the line —
official extensions may enter core, third-party SDKs may not.

### Formats sniff content, never extensions

`Format{Name, Match, Decode}`; Match sees the first 512 bytes (the
net/http.DetectContentType budget — SVG needs room for an XML prolog).
The ICO magic is weak (`\0\0\1\0`), so its Match also requires a
nonzero entry count; SVG's Match requires the data to *start* like an
XML document and contain `<svg`, so it does not claim `.gooey` files.
The registry is mutex-guarded but registration is expected at init —
core's own five formats register in `imaging`'s init, the SVG module's
init calls `imaging.Register`.

### The ICO parser decodes DIBs directly, mask and all

The alternative — synthesize a BMP file header and reuse the bmp
decoder — founders on the parts of an ICO DIB that are not BMP: the
doubled `biHeight` and the 1-bit AND mask that IS the transparency for
1/4/8/24-bit entries. So `decodeDIB` reads BITMAPINFOHEADER +
palette + bottom-up rows itself (~100 lines), applies the mask, and
uses the classic 32-bit heuristic: a live alpha plane wins; an all-zero
alpha plane defers to the mask (some writers mean the mask and leave
alpha dead — without the heuristic those icons decode invisible).
Scope: uncompressed 1/4/8/24/32-bit DIBs and PNG entries; BI_RGB only;
decoding picks the largest entry (then deepest) — an icon is one image
at many sizes and the cell pipeline rescales anyway. PNG entries cover
everything modern (Vista+ 256px).

### Markup threads the loading FS through Context

`Context` gains an unexported `fsys`, installed by `Load` with the same
document-scoped save/restore the xmlns table uses, and set on the child
context by control instantiation — so a literal Src inside a
UserControl resolves against the CONTROL's FS, the same isolation its
bindings get. A tree built from raw bytes (`markup.Build`) falls back
to `Context.Includes`, else fails the load with an error saying so.
Decoding happens in the builder, which is what makes hot reload
re-decode a literal Src for free: a page rebuild re-runs the builder
(verified — `Page.Build` → `Load` → builder). The watcher stats markup
files only; changing image bytes under an unchanged path does not
trigger a rebuild by itself.

Cols and Rows are **required** (positive literal or int binding). An
Image without a size measures 0×0 and silently vanishes; a component
that can disappear by omission is a debugging session, not a default.

### The SVG module rasterizes at intrinsic size, capped

`oksvg` + `rasterx` (the pure-Go pairing), viewBox/width/height as the
intrinsic size, scaled down to at most 1024 px on the long side —
memory bound, no visible cost, since the pixel pipeline rescales to a
cell rectangle regardless. No intrinsic size is a decode error. CI runs
the module as its own step, mirroring handlers/temporal and mcp.

### Damage

Setting `Src` repaints exactly the Image —
`TestSettingSrcRepaintsExactlyTheImage` pins the count at 1, making
explicit what the rendering-2 property conversion (deviation 2) built.
File-loaded images ride the same guarantee: the decode happened at
build time, so at runtime a markup image IS a source property.

## Deferrals

- GIF animation (gifplay-style player component) — tracked as
  [#105](https://github.com/WonderForgeLabs/gooey/issues/105).
- 16-bit (BI_BITFIELDS) and PNG-compressed DIB entries in ICO.
- `<Image>` alignment sugar: an Image in a stretching parent still
  wants `HAlign` from markup, which already exists as a layout
  attribute and is documented in the howto.
