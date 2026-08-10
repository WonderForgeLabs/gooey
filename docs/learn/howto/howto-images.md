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

`graphics.Scale(img, w, h)` resizes to a pixel size with
nearest-neighbour sampling if you need to prepare the source.

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

### There is no `<Image>` markup element

No built-in element builds one, because markup has no way to spell an
`image.Image`. Register it as a custom component and supply the picture
from Go:

```go
Components: map[string]markup.Builder{
	"Logo": func(e markup.Element, c *markup.Context) (gooey.Component, error) {
		cols, err := strconv.Atoi(e.Attrs["Cols"])
		if err != nil {
			return nil, fmt.Errorf("<Logo Cols=%q>: %w", e.Attrs["Cols"], err)
		}
		rows, err := strconv.Atoi(e.Attrs["Rows"])
		if err != nil {
			return nil, fmt.Errorf("<Logo Rows=%q>: %w", e.Attrs["Rows"], err)
		}
		return &components.Image{
			Src:  components.Img(logo()),
			Cols: components.Cells(cols),
			Rows: components.Cells(rows),
		}, nil
	},
}
```

```xml
<Logo Cols="24" Rows="12"/>
```

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

`cmd/demo` is the worked example — an image in an ordinary `gooey.App`,
with keys for each of those transitions and a footer counting the bytes
each one cost:

```sh
go run ./cmd/demo                 # detect, run interactively
go run ./cmd/demo --mode=sixel    # force a protocol
go run ./cmd/demo --dump          # one full frame to stdout, no tty
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

The repository's top-level [`demo.gif`](../../../demo.gif) shows the
capability detection and the pipeline; [`docs/demos.md`](../../demos.md)
catalogs what each demo exercises.

## See also

- [Tutorial 6: Write a custom component](../06-custom-components.md)
- [How to test a gooey app](howto-testing.md)
- Depth: [architecture.md — the two rendering planes](../../architecture.md#the-two-rendering-planes)
