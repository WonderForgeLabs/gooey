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

The first three are `graphics.Encoder`s: a widget records a
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

Force a protocol to compare them:

```sh
go run ./cmd/demo --mode=sixel      # kitty | sixel | iterm2 | halfblock
go run ./cmd/demo --dump            # one frame to stdout, no raw mode
```

## Put an image in a tree

`gooey.Image` takes a Go `image.Image` and a size in cells:

```go
&gooey.Image{Src: myImage, Cols: 24, Rows: 12}
```

`graphics.Scale(img, w, h)` resizes to a pixel size with
nearest-neighbour sampling if you need to prepare the source.

### There is no `<Image>` markup element

The pixel pipeline predates the property model, so `Image`'s fields are
plain Go values rather than properties, and no built-in element builds
one. Register it as a custom widget:

```go
Widgets: map[string]markup.Builder{
	"Logo": func(e markup.Element, c *markup.Context) (gooey.Widget, error) {
		cols, err := strconv.Atoi(e.Attrs["Cols"])
		if err != nil {
			return nil, fmt.Errorf("<Logo Cols=%q>: %w", e.Attrs["Cols"], err)
		}
		rows, err := strconv.Atoi(e.Attrs["Rows"])
		if err != nil {
			return nil, fmt.Errorf("<Logo Rows=%q>: %w", e.Attrs["Rows"], err)
		}
		return &gooey.Image{Src: logo(), Cols: cols, Rows: rows}, nil
	},
}
```

```xml
<Logo Cols="24" Rows="12"/>
```

Because the source is a plain field and not a property, **changing it
will not repaint anything**. An image that has to change needs a wrapper
widget that reads a property during `Render` — the pattern from
[tutorial 6](../06-custom-widgets.md).

## Know which render path you are on

This is the one that surprises people:

- **The damage-tracked path** (`gooey.NewComposer` + `comp.Flush`) writes
  the cell plane only. It carries no encoder, so an `Image` in a
  Composer-driven app always takes the **halfblock** branch and draws
  itself into cells. Which is fine — it works everywhere, and it damages
  and repaints like any other widget.
- **The one-shot path** (`gooey.Compose` + `frame.Flush`) carries the
  encoder and emits placements, so it is where the kitty, sixel, and
  iTerm2 protocols actually run. `cmd/demo` is that path; read
  `cmd/demo/main.go` for the current wiring.

So: interactive apps get halfblock today; protocol-quality images mean
the one-shot path.

## Capturing images

`agg` renders the **cell plane only**, so recorded GIFs and screenshots
show halfblock output faithfully and show nothing at all for sixel,
kitty, or iTerm2 — those need a real terminal in front of a real person.
The screenshot at the top of this page is halfblock for exactly that
reason.

The repository's top-level [`demo.gif`](../../../demo.gif) shows the
capability detection and the pipeline; [`docs/demos.md`](../../demos.md)
catalogs what each demo exercises.

## See also

- [Tutorial 6: Write a custom widget](../06-custom-widgets.md)
- [How to test a gooey app](howto-testing.md)
- Depth: [architecture.md — the two rendering planes](../../architecture.md#the-two-rendering-planes)
