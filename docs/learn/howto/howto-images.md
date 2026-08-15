# How to draw images

gooey draws pixel content on a **second plane** that the terminal
composites over the cell buffer. Text, borders, and styling always go
through the cell renderer; only images need a protocol.

![The demo drawing its logo with the halfblock fallback beside the detected capabilities](../media/howto-images-halfblock.png)

## The four modes

| Mode | Protocol | Terminals |
|---|---|---|
| `kitty` | Kitty graphics protocol | kitty, Ghostty, WezTerm |
| `sixel` | DEC sixel | xterm, foot, Windows Terminal ≥ 1.22, VTE ≥ 0.76, mlterm, Konsole |
| `iterm2` | OSC 1337 inline images | iTerm2, WezTerm, mintty |
| `halfblock` | none — pixels become cells | everywhere |

The first three are `graphics.Encoder`s: a component records a
`graphics.Placement` during painting and the flush emits the protocol
bytes after the cell plane, so pixels land over the already-painted
cells. **Halfblock is not an encoder** — it renders the image *into* the
cell buffer as `▀` runes with 24-bit foreground and background, two
pixels per cell. That is why "no encoder" means "draw into the buffer
instead" rather than "draw nothing".

## Detect what the terminal supports

Capability detection is a handshake, not configuration. `term.Screen.Detect()`
sends a Kitty graphics query, a cell-size query (XTWINOPS 16), and DA1 —
terminals answer DA1 unconditionally, so it acts as the terminator and
anything that arrived before it is parsed.

```go
caps, err := screen.Detect()
// caps.Kitty, caps.Sixel, caps.ITerm2, caps.CellW, caps.CellH
mode := caps.Best() // kitty > sixel > iterm2 > halfblock
```

`Detect` must run on a real tty. Under a headless pty nothing answers and
it falls back after a 500 ms timeout.

Check your own terminal with the probe:

```sh
go run ./cmd/probe
```

```
terminal: xterm-256color /
size:     80×24 cells, cell 10×20 px
kitty:    false
sixel:    false
iterm2:   false
selected: halfblock
```

## Put an image in a tree

`components.Image` takes an `image.Image` and a size in cells, all three
as properties — so setting any of them repaints the image and nothing
else:

```go
&components.Image{
	Src:  components.Img(myImage),
	Cols: components.Cells(24),
	Rows: components.Cells(12),
}
```

`graphics.Scale(img, w, h)` resizes to a pixel size if you need to
prepare the source. It **resamples** with a triangle (bilinear) kernel:
every destination pixel is a weighted average of the source pixels it
covers, so a reduction keeps thin features instead of hitting or missing
them on a grid, and an enlargement is smooth rather than blocky. The
triangle kernel has no negative lobes, so its output is always a convex
combination of its inputs and it cannot overshoot the source's range and
ring a hard edge — which is why it is bilinear rather than a cubic like
CatmullRom. `Scale`'s doc comment carries the measurements behind that
choice.

Reducing costs accordingly: it reads every source pixel now, where
subsampling read only as many as it wrote. A 1920×1080 photo into a
200×120 halfblock rectangle is tens of milliseconds. That runs on the
Image's paint node, so it re-runs only when the source, the size, or the
damage changes — not once a frame — but resizing the terminal changes the
size, so a large source scaled straight into a pane will stutter on
resize. Scale once and bind the result if the size is not really varying.

An image asks for a cell rectangle and means it, so give it an alignment
if its parent would otherwise stretch it:

```go
im.LayoutProps().HAlign = gooey.AlignStart
```

To make it change, bind `Src` to a computed:

```go
phase := prop.NewSource(0)
src := prop.NewComputed(func() image.Image { return render(phase.Get()) })
im := &components.Image{Src: src, Cols: components.Cells(24), Rows: components.Cells(12)}
```

## Load an image from a file

The `imaging` package decodes **png, jpeg, gif, bmp, and ico** through
a content-sniffing registry, loading through the same `fs.FS` seam
markup uses — `os.DirFS` in dev, `embed.FS` in release:

```go
img, err := imaging.Load(assets, "logo.png")          // an image.Image
src, err := components.LoadImg(assets, "logo.png")    // …wrapped as a property
```

GIF decodes to its **first frame** — animation is a player's job (the
browser demo's gifplay pattern). ICO picks its largest entry, PNG or
DIB. A file that is missing or will not decode returns an
`*imaging.Error` naming the path and the sniffed format.

**SVG** costs a real rasterizer, so it lives in a nested module and a
blank import is the opt-in:

```go
import _ "github.com/WonderForgeLabs/gooey/imagefmt/svg"
```

After that, `.svg` files decode like any other format, rasterized at
their intrinsic size (viewBox or width/height, capped at 1024 px on the
long side — the pixel pipeline rescales to cells anyway).

### The `<Image>` markup element

```xml
<Image Src="assets/logo.png" Cols="24" Rows="12"/>
<Image Src="{{.Chart}}" Cols="{{.ChartCols}}" Rows="12"/>
```

A literal `Src` is a path **in the FS the page was loaded from**,
decoded at page load — a bad path or corrupt file fails the load with
an error naming both. A binding shares the viewmodel's
`*prop.Property[image.Image]` handle, exactly like every other bound
attribute. `Cols` and `Rows` are required (literal or bound): an image
with no size would measure 0×0 and silently vanish.

Hot reload re-decodes a literal `Src` naturally — a page rebuild
re-runs the builder. The watcher stats markup files, not image files,
so to see new pixels under an unchanged path, touch the page.

## Turn the protocol on

An app gets a pixel protocol only when its capabilities say so, and the
environment can report color depth but never graphics support. So either
probe, or say outright:

```go
app := gooey.NewApp(content, gooey.WithCapabilityProbe())        // ask the terminal
app := gooey.NewApp(content, gooey.WithGraphics(graphics.Kitty{})) // or pin it
```

`WithGraphics(nil)` forces halfblock. The default — no probe, no pinned
encoder — is halfblock too, deliberately: emitting an image protocol at a
terminal that does not speak it puts garbage on a user's screen, and only
a probe can tell.

Pixel content is damage-tracked like everything else. Change nothing and
no image bytes are written; move an image and a kitty terminal re-places
the picture it already has instead of receiving it again; hide the
component and the placement is deleted (kitty) or erased by repainting
the cells under it (sixel, iTerm2).

`cmd/pixels` is the worked example — an image in an ordinary `gooey.App`,
with keys for each of those transitions and a footer counting the bytes
each one cost:

```sh
go run ./cmd/pixels                 # detect, run interactively
go run ./cmd/pixels --mode=sixel    # force a protocol
go run ./cmd/pixels --dump          # one full frame to stdout, no tty
```

## Capturing images

`agg` renders the **cell plane only**, so recorded GIFs and screenshots
show halfblock output faithfully and show nothing at all for sixel,
kitty, or iTerm2 — those need a real terminal in front of a real person.
The screenshot at the top of this page is halfblock for exactly that
reason.

What a headless capture *can* verify is that the right protocol bytes
went out: run the app under a pty and count the signatures in the log —
`\x1b_Ga=T` and `\x1b_Ga=p` for kitty transmissions and re-placements,
`\x1bP0;0;0q` for sixel, `\x1b]1337;File=` for iTerm2.

One thing changed here with the incremental flush: **you can no longer
find the final frame by looking for the last `\x1b[H` in a log.** Only
full frames start with a cursor-home, and after the first one the log
holds differences. Replay the whole log through `render.Screen` instead —
it is an `io.Writer`, so it takes the bytes as they come and `Text()`
gives you the screen.

The repository's [`pixels.gif`](../../media/demos/pixels.gif) shows the
capability detection and the pipeline; [`docs/demos.md`](../../demos.md)
catalogs what each demo exercises.

## See also

- [Tutorial 6: Write a custom component](../06-custom-components.md)
- [How to test a gooey app](howto-testing.md)
- Depth: [architecture.md — the two rendering planes](../../architecture.md#the-two-rendering-planes)
