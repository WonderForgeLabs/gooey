# An element's icon is a NAME, not a picture

**Status:** accepted, implemented (`markup/elementdef.go`, `markup/catalog.go`,
`apps/wysiwyg/components/toolbox/`)
**Issue:** #287 (sub-issue of #240)
**Date:** 2026-08-23

## The gap

A palette needs an icon per element, and the catalog had nowhere to put
one. `ElementSpec` described what an element accepts, what may nest
inside it, and how it behaves — everything except what it looks like in
a picker. wysiwyg's toolbox was therefore a column of names, which is a
list rather than a toolbox.

The obvious field is the one that cannot be written.

## Why the obvious field cannot be written

`imagefmt/svg` is a nested module. That is deliberate: rasterizing vector
paths needs `oksvg` and `rasterx`, and a TUI framework's core graph does
not carry a renderer. The rule and its trail are
`docs/specs/2026-08-10-pack-distribution.md`.

So the field's type decides where the dependency lands:

| field type | what core would have to import | verdict |
|---|---|---|
| `image.Image` | a decoder — in practice the SVG stack | no |
| `func(px int, tint color.Color) (image.Image, error)` | nothing, but the definition now carries behaviour, and every declaration site has to know a renderer | no |
| `fs.FS` + path | nothing, but each element would ship an asset directory, and core would be choosing an icon vendor for every host | no |
| `string` | nothing at all | yes |

The last row is the whole design. `markup` gains one `string` per
element and no import. Every `docs/learn/examples/*` — root-module
consumers that cannot import a nested module without their own `go.mod`
— is unaffected, because there is nothing to import.

Checked rather than asserted:

```sh
go list -deps ./... | grep -E 'oksvg|rasterx'   # no output
```

## What the name means

`ElementDef.Icon` (and the `ElementSpec.Icon` it renders into) is a NAME
in the CONSUMER's icon set: no directory, no extension, no colour. Core
states **what the element is**; the host decides **what it looks like**.

This is not a new indirection. It is `KindStyle`'s, one layer up: a
`Style="dim"` attribute names a row in the app's style table, and an app
that swaps its palette swaps the table rather than the markup. An icon
name resolves against whatever set the host loaded — Codicons in
`apps/wysiwyg`, something else elsewhere — so nothing in core is pinned
to one icon vendor by a field in `elementdef.go`.

`TestDeclaredIconNamesAreBareNames` pins the "no directory, no
extension" half, because a name carrying `.svg` works in exactly one
consumer and silently produces `x.svg.svg` in the next.

### Empty is a value

An element may decline to declare an icon, and the absence must survive
to the row. A `Builder` registration — a func, not a schema — declares
nothing, including no icon, and a palette must render that as an absence
rather than substituting a default. It is the same honesty rule
`AttrsKnown` carries one field over: "no attributes" and "attributes
unknown" must not look alike, and neither must "no icon" and "some
icon".

`TestAnElementMayDeclineAnIcon` and
`TestAnElementWithNoDeclaredIconGetsNoPicture` are the two ends of that.

### A registered element carries one too

The `Context.Elements` seam means a host declaration is exactly as
describable as a builtin, and the icon travels with it — `specAs` copies
the field whichever provenance it stamps. `activitybar.Def` declares
`layout-activitybar-left` for exactly this reason: a palette showing
icons for gooey's own elements and blanks for the host's would be
describing the build rather than the app.

## The tint is a handle

The second half of the acceptance, and the one that is easy to satisfy
in appearance only. The host resolves a name to a
`*prop.Property[image.Image]` whose computed reads the tint **inside**
the closure:

```go
h := prop.NewComputed(func() image.Image {
    tint := ic.tint.Get()          // INSIDE: this is the subscription
    img, _ := ic.set.At(file, tint)
    return img
})
```

Reading the tint outside and closing over the value compiles, loads,
renders, and is silently wrong forever: the icon keeps its first colour
and nothing in the framework reports a picture that stopped changing.
That is the whole reason the colour is not baked into a raster at load.

Because the tint is read there and nowhere else, flipping the theme
dirties exactly the icon handles and through them exactly the `<Image>`
in each realized palette row. Pinned by a count, not by a screen
assertion — "the icon is the new colour" is just as true when the whole
tree repainted:

    TestThemeFlipRepaintsOnlyTheToolboxIcons

If a later change moves that number, the change IS that.

## Two things that bit, recorded so they do not bite again

**The house highlight and pixel content do not mix.** `rowHighlight`
re-styles the cells a row painted. Over an icon that is either invisible
(a graphics protocol paints above the cells) or a photo-negative
(halfblock, where the picture IS the cells) — and the `<Image>` is
*below* the decorator in z-order, so the composer never forces it to
repaint and re-record its placement. The row template therefore mentions
`_selected`, which stands the house highlight down and hands selection
to the template. `cmd/typeahead` reached the same conclusion for its
album covers; this is the second instance, which makes it a rule rather
than a quirk.

**A `<Grid>` with no `Rows` measures the whole list.** The row template
was `<Grid Cols="1,2,*">`, which took all 29 offered cells, so
`ItemsView` realized a SINGLE row — a toolbox showing one component,
with no error anywhere. The previous `<HStack><Text/></HStack>` measured
1 because a `Text` does; a `Grid` does not. `Rows="1"` is load-bearing
and `TestPaletteRowsCarryTheDeclaredIcon` fails without it.

## What is deliberately not here

- **A size, or a set of sizes.** `IconPx` is the host's, beside its
  assets. A pixel count in core would be a rendering decision made by
  the layer that was just kept out of rendering.
- **A second icon per state.** The rail draws active and inactive from
  one asset and two tints, which is what tinting-before-rasterizing buys;
  a second declared name would be a second thing to keep in sync for a
  colour.
- **Any check that the named asset exists.** Core cannot know the host's
  directory. The host's `Preload` is the gate, and it runs at startup
  because a `Render` has nowhere to put an error.
