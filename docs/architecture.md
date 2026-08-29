# Architecture

This is the deep guide to how gooey works: the two rendering planes, the
lazy property graph, the retained component model, the damage-tracked
Composer, routed input, the `App` runtime, the markup layer, and the
control plane a running app exposes. It is grounded in the code as it
exists today, so every section names the real types and functions,
quotes the load-bearing excerpts, and says plainly where the
implementation stops. (The root packages still call themselves a POC;
this page is the measure of how much of the design that POC has since
grown into.) For a first walkthrough, start with
[getting-started.md](getting-started.md); for the markup syntax itself,
see [markup-reference.md](markup-reference.md); for what the demo apps
prove, see [demos.md](demos.md).

The one-paragraph shape: components are persistent objects in a retained
tree. Every visual property on them is a `*prop.Property[T]` in a lazy
dirty-tracking graph. The `Composer` gives each component its own paint
node in that same graph, so a property change dirties exactly the
components that read it during their last paint. A frame is layout
(unconditional, cheap) plus repaint (dirty nodes only) into a persistent
cell buffer, flushed as ANSI. Input is a single ordered event stream
routed through the tree by focus (keys) or hit-testing (mouse). Markup
builds the same tree from XML, binding attributes to property handles.
`App` owns the run loop — terminal lifetime, signals, dispatcher,
companions — and `control` exposes the running app to out-of-process
clients through one settle-barriered door.

## Where the code lives

| Package | Holds |
|---|---|
| `gooey` (root) | The contracts and the runtime: `Component`, `Container`, `Base`, `Layout` and the measure/arrange sandwich, `Frame`, `Compose`, `Composer`, `Dynamic`, `Startable`, `Dispatcher`, `App` and its signal/companion machinery, focus and mouse routing, `Command`, `KeyBinding` |
| `gooey/components` | The built-in components: `Text`, `Button`, `Checkbox`, `TextBox`, `Gauge`, `Sparkline`, `ProgressBar`, `Spinner`, `Toggle`, `Segmented`, `ColorPicker`, `Image`, `ItemsView`, `Timer`, `TypeAhead`, `ValidationMarker`, `Companion`; the overlays `MenuBar`, `ToastHost`, `Tooltip`, `AdornmentLayer`, `DragGhost` and the `Popup` primitive under them; and the containers `VStack`, `HStack`, `Grid`, `Border`, `Canvas`, `Tabs`, `StatusBar`, `ButtonBar` — plus the `Str`/`Sty`/`Strs` literal wrappers. `markup/elements.go` is the authority on what markup can name |
| `prop`, `input`, `render`, `graphics`, `term` | The layers underneath: property graph, decoded event stream, cell buffer and ANSI, pixel protocols, terminal capabilities |
| `markup` | XML → tree, bindings, `UserControl`, `Include`, `<x:Property>`, handler and value namespaces |
| `control` | The in-process control-plane service every remote transport fronts (see [the control plane](#the-control-plane)) |
| `format`, `imaging` | Display formatting (plain functions plus computed-property constructors, so a formatted string repaints itself); the image-decode registry (png/jpeg/gif/bmp/ico in core) |
| `validate`, `settings` | The forms-validation vocabulary over the property graph, and external state — one flat JSON document of dotted keys — as ordinary bindable properties |
| `handlers/net`, `handlers/fs`, `handlers/env`, `handlers/str`, `handlers/sets` | The in-tree capability packs behind markup's namespaces: `net`/`fs` behind handler namespaces (push), `env`/`str`/`sets` behind value namespaces (pull) |
| `handlers/exec`, `handlers/temporal`, `mcp`, `grpc`, `imagefmt/svg`, `paint`, `packs/*`, `apps/*` | Nested Go modules — heavy dependencies quarantined so `go build ./...` at the root never sees them: the exec and Temporal packs, the MCP and gRPC control-plane transports, SVG rasterization, `paint`'s 2D vector drawing (a graphics library rather than an SDK, but the same doctrine), the `packs/temporal-*` activity packs (one module per Temporal API domain), and the example apps that carry deps of their own. CLAUDE.md's discovery `find` is the authority on the set — the list here is a sample and will go stale |
| `proto`, `clients` | The `gooey.control.v1` proto contract and the committed generated clients — Go under `grpc/gen`, Python and TypeScript under `clients/` |

The dependency runs one way: **`components` imports the root, and the
root never imports `components`.** That is what makes the component set
replaceable — an app can write its own `Component` implementations
against the same contracts and owe nothing to the built-ins. The root
package's own tests use throwaway fakes for exactly this reason; the
tests that need real components (damage counts, layout, input routing)
live in `components/` and exercise the root machinery through them.

## The two rendering planes

The founding question of the POC was "are there N rendering modes —
ansi, sixel, kitty, ...?" The answer, now baked into the package layout,
is no: **there is one cell renderer and N graphics protocols**, and they
are different planes, not alternative backends.

### The cell plane

Everything a component tree normally is — text, borders, stacks, styling —
renders into the cell plane: `render.Buffer`, a W×H grid of styled
character cells.

```go
type Cell struct {
    Rune  rune
    Style Style
}

// Buffer is a W×H grid of cells — one frame of the cell plane.
type Buffer struct {
    W, H  int
    Cells []Cell

    cx0, cy0, cx1, cy1 int // the clip: what Set will accept
}
```

**Writes are clipped to the painting component.** The Composer brackets
every `Render` with `Buffer.Clip`/`Unclip` — the same place it installs
the graphics sink, and for the same reason: both answer "which component
is painting right now". A write outside that rect is dropped.

This is not a rendering nicety, it is a correctness rule, because of how
damage tracking works. Cells beyond a component's rect belong to
neighbours whose paint nodes did not invalidate, so those cells are
*clean* and nothing will ever repaint over a stray write. It survives
until something unrelated dirties the victim — seen from the far end as
"stray characters in a pane that never fixes itself".

It is framework-wide rather than opt-in because it is free: the clip is
kept inside the buffer by `Clip`, so `Set` tests the clip *instead of*
the buffer rather than as well as it — the same four comparisons that
already bounded every write. `Clip` intersects rather than replaces, so
nesting can only narrow and a component cannot widen its way out.

`Frame.Place` clips the pixel plane against the same rect, cropping a
partly-visible image rather than dropping it. Clipping text but not
pictures would be worse than no clipping at all: a sixel or kitty image
is composited by the terminal, so no cell-plane check can catch one that
overhangs, and the gap would only show up once a component with an image
overflowed. See `docs/specs/2026-08-25-clipping.md`.

`render.Style` carries 24-bit `Fg`/`Bg` colors (zero value means
"terminal default") plus bold/underline/reverse. `render.Flush` walks
the buffer and emits ANSI: a cursor-home, then rows of runes with an SGR
sequence emitted only when the style changes between cells. This path is
universal — every terminal that can run a TUI can run it.

Color *depth* is a property of the wire, not of the buffer. The buffer
always holds 24-bit colors; quantization happens once, on the way out,
so every flush entry point takes a `render.ColorDepth` — `Flush(w, b,
depth)`, `FlushCells`, `Flusher.Encode`, and `Frame.Depth()` for the
rare component that wants to preview the answer itself (ColorPicker).
`TrueColor` emits `38;2;r;g;b`, `Color256` maps to the xterm-256 palette,
`Color16` to the eight ANSI colors and their bright variants. `TrueColor`
is the zero value, so an undetected terminal behaves exactly as it did
before depth existed.

### Damage reaches the wire: `render.Flusher`

`render.Flush` writes every cell, every time. That is right for a
screenshot and wrong for an interactive app, so the run loop uses
`render.Flusher` instead: it remembers the buffer the terminal is
currently showing and emits only the spans where the next buffer differs.

Per row it finds runs of changed cells, extends a run across gaps of up
to four unchanged cells (jumping the cursor costs six to nine bytes;
crossing a cell costs one), emits a cursor-position escape and the run,
and carries SGR state across the whole flush — style survives a cursor
move, so a diff that jumps around the screen pays for a style change only
when the style actually changes. A frame where nothing changed emits
**nothing at all**, not even the synchronized-output bracket.

The diff is *cell-level truth*, not a replay of the paint-node damage.
Components overpaint each other, containers deliberately do not clear
their bounds, and a leaf's pre-clear touches cells no damage counter
knows about — comparing buffers catches all of it. Correctness therefore
never depends on the damage count; the count only has to be right for the
byte total to be small.

Three things force a full frame, because in each the terminal is showing
something the Flusher did not put there: the first frame, a resize (the
previous buffer describes a screen that no longer exists), and an
explicit `Invalidate` — which `App` calls after re-acquiring the terminal,
since the alternate screen comes back blank.

What it buys, on an 80×24 screen (`Composer.FlushBytes`):

| frame | components repainted | bytes written | full repaint would be |
|---|---|---|---|
| first | 19 | 2784 | 2784 |
| idle | 0 | **0** | 2784 |
| a gauge ticks | 1 | 49 | 2784 |
| a keystroke in a text box | 1 | 34 | 2784 |
| focus moves | 2 | 53 | 2792 |

Because the wire now holds *differences*, it no longer holds the screen:
changing `n=2` to `n=3` puts a single `3` on it. Anything that used to
grep the byte stream for what the app is showing has to reconstruct the
screen first, which is what `render.Screen` is for — a terminal model you
can hand to `Flush` or feed from a pty, and the audit that replaying the
emitted bytes reproduces the buffer. The flush and the per-node
placement damage below landed as one change
([PR #85](https://github.com/WonderForgeLabs/gooey/pull/85), epic
[#21](https://github.com/WonderForgeLabs/gooey/issues/21)), which is
where the argument for cell-level truth over replayed damage lives.

### The pixel plane

Pixel content — the `Image` component, a future canvas or chart — is the
only thing that varies by terminal, and it does not go through the cell
buffer at all. It rides a second plane that the terminal itself
composites over the cells, spoken through one of three protocols, each a
small implementation of `graphics.Encoder`:

```go
// Encoder emits a pixel image at the current cursor position, sized to
// cols×rows terminal cells (cellW×cellH is the cell size in pixels).
type Encoder interface {
    Name() string
    Encode(out *[]byte, img image.Image, cols, rows, cellW, cellH int) error
}
```

- `graphics.Kitty` — Kitty graphics protocol: PNG, base64, chunked APC
  `_G` sequences (kitty, Ghostty, WezTerm).
- `graphics.Sixel` — DEC sixel (DCS `q`), 256 color registers chosen
  from the image rather than from a fixed grid: lossless for anything
  with ≤256 distinct sixel-space colors (which is every piece of
  interface chrome ever drawn), a median cut above that. "Distinct" is
  counted in sixel's own 101-level space, not in 24-bit RGB (xterm,
  foot, Windows Terminal ≥1.22, VTE ≥0.76, Konsole, mlterm).
- `graphics.ITerm2` — OSC 1337 inline images (iTerm2, WezTerm, mintty).

An encoder that names a cell rectangle and lets the terminal scale is a
few dozen lines — kitty is 53, iTerm2 21 — and adding a future protocol
of that shape means adding one more. Sixel is the exception at 260,
because carrying actual pixels means owning quantization too. Kitty
additionally implements `graphics.IDEncoder`,
which is how the incremental flush asks "can this protocol address a
placement after the fact?" — a type assertion, like every other
capability question here.

The fourth mode, halfblock, is deliberately *not* an `Encoder`. It is
the universal fallback that degrades pixel content back into the cell
plane: `graphics.DrawHalfblock` scales the image to `cols × rows*2`
pixels and writes each cell as `▀` with the top pixel as 24-bit
foreground and the bottom pixel as background — two pixels per cell,
works everywhere. Nothing is emitted beside the cells; the image simply
becomes cells. This asymmetry is why `Frame.Graphics == nil` means
"degrade during the render walk" rather than "skip images".

### How the planes meet: the Frame

The component tree never knows which protocol is active. `gooey.Frame`
holds both planes, and a component records pixel content the same way it
writes runes — during `Render`:

```go
func (im *Image) Render(f *gooey.Frame) {
    r := im.Bounds()
    if f.Graphics != nil && f.CellW > 0 && f.CellH > 0 {
        f.Place(graphics.Placement{Img: src, Col: r.X, Row: r.Y, Cols: r.W, Rows: r.H})
        return
    }
    graphics.DrawHalfblock(f.Cells, src, r.X, r.Y, r.W, r.H)
}
```

All three conditions are load-bearing, and the third is the one that
looks redundant. An encoder scales to `cols*CellW × rows*CellH`, so a
zero in either metric asks it for an image of zero pixels — over cells
that halfblock never got to paint, because the placement branch already
returned. The region goes blank with no error on any surface. `CellW`
and `CellH` are independently fatal for the same reason, which is why
the guard names both rather than standing in one for the other. This is
also the rule `buttonchrome.go`, `colorpicker.go` and the wysiwyg
`panel` follow ([#251](https://github.com/WonderForgeLabs/gooey/issues/251),
applied in [PR #257](https://github.com/WonderForgeLabs/gooey/pull/257)).

`Place` is a method rather than an appendable field because a placement
has an **owner**. Under the Composer only dirty components re-render, so
a list rebuilt from scratch each frame would lose the images of every
component that did not repaint. The Composer installs a sink around each
paint node, so each placement is filed under the component that recorded
it — and the pixel plane gets the same per-component damage rule as the
cell plane.

### Damage on the pixel plane

Cells have it easy: the buffer is retained, so the flush compares what is
against what was. Placements have no such buffer, so the Composer *is*
the retained store for them, keyed by paint node. Everything else is a
diff between two lists per node:

| what changed | the frame does |
|---|---|
| same image, same rectangle | nothing goes on the wire |
| same image, new rectangle | a **move** |
| different image, or a new slot | a **transmission** |
| the slot is gone — turned `Hidden`, painted fewer images, or left the tree in a `Dynamic` re-sync | a **removal** |

Removal is where the protocols stop agreeing, and the split is the
`graphics.IDEncoder` interface:

- **Kitty** has placement identity. An image transmitted with `i=ID`
  stays in the terminal's store, so a move costs one control sequence
  (`a=d,d=i` then `a=p`) instead of a PNG, and a removal is `a=d,d=I`.
  The two delete forms are a case distinction, not a spelling: lowercase
  `d=i` drops the placements and keeps the pixels (right for a move),
  uppercase `d=I` frees the data too (right for something that vanished,
  or the terminal accumulates every picture the session ever showed).
- **Sixel and iTerm2** write pixels into the cell grid and then forget
  them. There is no delete, so a vanished image is erased by repainting
  the cells it covered — `render.Flusher.Damage` forces exactly those
  cells back onto the wire from the retained buffer, which has held the
  correct content all along. The rule runs the other way too: any cell
  the flush re-sends erases part of an image sitting on it, so a
  surviving placement intersecting the flush's touched spans is re-sent.

A full frame re-sends every placement for every protocol: after a resize
or a terminal hand-off, nothing on screen survived, images included.

### Capability detection is a handshake, not config

`term.Screen.Detect` decides the protocol at startup by asking the
terminal, exploiting the one query every terminal answers:

```go
// Kitty query (tiny 1×1 RGB transmit, q=1 → responds if supported),
// then cell size, then the color capability, then DA1 terminator.
fmt.Fprint(s.tty, "\x1b_Gi=31,s=1,v=1,a=q,t=d,f=24;AAAA\x1b\\")
fmt.Fprint(s.tty, "\x1b[16t")
fmt.Fprint(s.tty, xtgettcapQuery)
fmt.Fprint(s.tty, "\x1b[c")
```

Four queries go out in one burst: a Kitty graphics probe (a terminal
that supports the protocol echoes an APC response containing `i=31`),
XTWINOPS 16 (cell size in pixels, needed for sixel scaling), XTGETTCAP
for the `RGB`/`Tc` terminfo capability, and DA1 (primary device
attributes). DA1 is the terminator: terminals answer it
unconditionally, so whatever arrived before it is the answer set.
Sixel support is DA1 attribute `4`. iTerm2's protocol has no query, so
it is detected from `TERM_PROGRAM`/`LC_TERMINAL` environment variables.
Color depth is a ladder rather than a single answer —
`colorDepthFrom(osEnv, parseXTGETTCAP(resp))` takes `COLORTERM`, then a
`direct` entry in `TERM`, then the terminal's own XTGETTCAP reply, then
a list of terminals known to be 24-bit, and leaves `TrueColor` standing
when nothing decides. The result lands in `term.Caps`, and `Caps.Best()`
encodes the protocol preference order: `kitty > sixel > iterm2 >
halfblock`. `cmd/probe` prints exactly what this handshake found for
your terminal.

One wrinkle worth knowing: the probe sets a 500 ms read deadline on the
tty and reads *synchronously* under it — through `SyscallConn().Control`
rather than `Fd()`, which is the whole reason the deadline works at all.
A goroutine plus a `select` is the obvious alternative and is precisely
the shape this package refuses: the abandoned read stays parked on the
tty and swallows the bytes the app's own decoder was waiting for
([specs/2026-08-10-tty-read-lifecycle.md](specs/2026-08-10-tty-read-lifecycle.md)).
File deadlines don't work on every tty, so an `ErrNoDeadline` degrades
to `readOnceBounded` — one blocking read, on the fact that terminals
answer DA1 unconditionally.

## The property system

`prop` is gooey's answer to WPF's `DependencyProperty`, and the
difference in philosophy is the most important design decision in the
codebase.

WPF is **eager**: `SetValue` immediately runs coercion, property-changed
callbacks, binding updates, and invalidation metadata
(`AffectsRender`, `AffectsMeasure`) through the tree, at set time.
gooey is **lazy**, in the Slint lineage: a set marks things dirty and
computes nothing; values are pulled at frame time.

There is one generic type, `prop.Property[T]`, in two flavors:

- `prop.NewSource[T](v)` — a settable value. `Set` assigns and walks
  the dependent set calling `invalidate()`; that is all it does.
- `prop.NewComputed[T](f)` — a derived value. It starts dirty. `Get`
  evaluates only if dirty, and **evaluation itself records
  dependencies**.

The recording mechanism is a package-level evaluation stack:

```go
// evalStack is the active-evaluation stack: reads record an edge to the
// computed property currently on top.
var evalStack []*node
```

When a computed evaluates, `Get` pushes its node, runs the compute
function, and pops. Any `Property.Get` called during that window runs
`recordRead`, which adds an edge from the read property to the computed
on top of the stack. Before re-evaluating, a computed detaches from all
its previous dependencies (`Get` deletes itself from each dep's
`dependents` set) and re-records from scratch. That re-recording is what
makes conditional reads precise: if the compute function is
`if mode { a.Get() } else { b.Get() }`, only the branch actually taken
this time is watched. Changing the untaken branch's source produces no
invalidation at all — `cmd/props` demonstrates this live with a
watched/unwatched source pair you can toggle.

The read-vs-subscription distinction falls out of the same mechanism:
**a read inside an evaluation is a subscription; a read outside is just
a read.** When `evalStack` is empty, `recordRead` returns immediately.
This is not an edge case — the Composer relies on it to keep layout out
of the graph (see below).

Invalidation propagates dirty flags up the dependent graph, and
`OnInvalidate` hooks fire on the clean→dirty transition only:

```go
func (n *node) invalidate() {
    if n.dirty {
        return
    }
    n.dirty = true
    if n.onInvalid != nil {
        n.onInvalid()
    }
    for d := range n.dependents {
        d.invalidate()
    }
}
```

`OnInvalidate` is the render-scheduler hook: it says "something you will
eventually want to repaint has changed", once, no matter how many sets
happen before the next frame. Because values only recompute on demand at
frame time, updates batch per frame for free — ten `Set`s between
frames cost ten dirty-flag walks and one recomputation, where WPF's
model costs ten full notification cascades.

The POC constraints are stated in the package comment and are real:
properties are confined to the UI goroutine (no locking), and
`Property.Evals()` exists only so tests and demos can prove laziness.
Anything arriving from another goroutine — fetch results in
`cmd/reader`, for instance — crosses onto the UI loop via a channel
before it may `Set`.

## The component model

### Component, Container, Base

The tree is retained: components are persistent objects that survive from
frame to frame, so invalidation can be per-node instead of
rebuild-the-world. The core contract is three methods:

```go
type Component interface {
    Measure(avail Size) Size   // desired size within avail (bottom-up)
    Arrange(bounds Rect)       // final bounds (top-down)
    Render(f *Frame)           // paint THIS component only
}

type Container interface{ ChildComponents() []Component }
```

Two details matter more than they look:

- `Render` paints *this component only*. Children are walked by the
  framework, not the component — `Container` exists so `renderTree` (and,
  critically, the Composer) can enumerate them. This is what lets the
  Composer give every component its own independent paint node.
- Containers paint nothing themselves unless they have chrome.
  `VStack.Render`, `HStack.Render`, and `Grid.Render` are empty;
  `Border.Render` draws only its box and title.

`Base` is the embeddable struct carrying the retained-tree bookkeeping:
arranged bounds (`Bounds() Rect`), the `Layout` properties
(`LayoutProps() *Layout`), and the attachment list (`Attach`/
`Attachments`, used by `KeyBinding` — see the input section).
Third-party components embed `Base` and get all of it.

Every visual property on the built-in components is a `*prop.Property[T]`:
`Text.Content`, `Text.Style`, `Border.Title`, `Border.Style`,
`Button.Content`. There is no second kind of property for literals —
`components.Str("hello")` and `components.Sty(style)` wrap literals as source
properties, so a component field is the same thing whether it came from a
literal, a viewmodel source, or a markup binding — `Image.Src` and
`Image.Cols/Rows` included, since the pixel plane became damage-tracked.

### The measure-arrange sandwich

Layout is the classic XAML two-pass, with one rule: **parents never call
`child.Measure` or `child.Arrange` directly**. They go through
`MeasureChild` and `ArrangeChild` in `layout.go`, which wrap the child's
own methods in the FrameworkElement behavior — the "sandwich":

- `MeasureChild(w, avail)` subtracts `Margin` from the available size,
  clamps to explicit `Width`/`Height` if set, calls `w.Measure` on the
  inner size, overrides the result with the explicit size, adds margin
  back, and caches the result in `Layout.desired`. A `Collapsed` child
  measures as zero.
- `ArrangeChild(w, slot)` carves margin out of the slot, then applies
  alignment: `AlignStretch` (the default) fills the content rect;
  `Start`/`Center`/`End` use the cached desired size and position it
  inside the slot. Only then does `w.Arrange` run with the final rect.

`MeasureChild` and `ArrangeChild` stop at `MaxLayoutDepth` (512) and
record a `LayoutFault` instead of recursing further, so a constructible
markup cycle costs one frame rather than the process
([#216](https://github.com/WonderForgeLabs/gooey/issues/216)). Layout is
only two of the seven walks over `ChildComponents` that a cycle used to
kill, and the first to die was `Composer.build`, which runs *before*
layout exists and wedges the heap rather than the stack — so capping
here alone would have turned the original crash into a hang. Compose and
Focus detect the repeat by identity (they already key a map by
component); Measure, Arrange, HitTest, Focusable and Render count depth.
A control that includes itself is caught earlier still, as a load error
naming the loop. Nothing panics: read the report with
`Composer.LayoutFault()`, `App.LayoutFault()`, or `Frame.LayoutFault()`
on the one-shot path. The record is
[`docs/specs/2026-08-23-layout-cycle-bounds.md`](specs/2026-08-23-layout-cycle-bounds.md),
and the four walks outside the root package that remain unbounded are
[#375](https://github.com/WonderForgeLabs/gooey/issues/375).

`Layout` itself is the FrameworkElement property set — margin
(`Thickness`, in cells), explicit size, `HAlign`/`VAlign`, and
`Visibility` (`Visible`, `Hidden` = occupies space but does not paint,
`Collapsed` = occupies nothing, subtree skipped entirely). In Go
composition it applies via `gooey.L`:

```go
gooey.L(&Text{...}, gooey.Layout{Margin: gooey.M(1), HAlign: gooey.AlignCenter})
```

In markup the same fields are the attributes `Width`, `Height`,
`Margin`, `HAlign`, `VAlign`, `Visibility` (see `applyLayout` in
`markup/markup.go`).

### Grid and star sizing

`Grid` is the workhorse panel: `Rows` and `Cols` are lists of `GridLen`
— `Fixed(n)` cells, `Auto()` (size to content), or `Star(w)` (weighted
share of the remainder), i.e. XAML's `GridLength`. The markup form
parses via `ParseGridLens("Auto,2*,10,*")`.

Measure and arrange split the work: `Grid.Measure` measures every child
once (caching desired sizes), sizes `Auto` tracks to the max desired of
their span-1 children, and leaves star tracks at zero — a starred grid
simply asks for everything it is offered. `Grid.Arrange` then resolves
stars against the final extent with `distributeStars`: fixed and auto
tracks are subtracted, the remainder is split by weight, and integer
rounding leftovers are handed to the last star track so the tracks
always sum exactly to the extent. Star sizing is what replaces the
"greedy child must come last" ordering hack that stack-based TUI layouts
force on you.

### Attached properties, Go-style

Grid placement uses the attached properties `Grid.Row` / `Grid.Col` /
`Grid.RowSpan` / `Grid.ColSpan`. Go has no attached-property store, so
the design takes the honest shortcut, documented on `Layout` itself:

```go
// Layout is the per-element layout state — the XAML FrameworkElement
// properties plus grid attached properties (Grid.Row etc. live here
// because Go has no attached-property store; the element itself is it).
```

The fields live directly on `Layout`; markup maps the dotted attribute
syntax (`<Text Grid.Row="2"/>`) onto them. This means the set of
attached properties is fixed by the framework rather than open-ended —
a limitation the [x:Property spec](specs/2026-08-10-markup-declared-properties.md)
explicitly decides to keep (markup-declarable *attached* properties are
ruled out there, because they would reintroduce a stringly-typed
per-element bag).

## The Composer

`Compose` in `component.go` is the one-shot path: fresh buffer, full
layout, full render walk. The interesting path is `Composer` in
`composer.go` — the retained, damage-tracked renderer, and the place
where the property graph and the component model fuse.

### Every component's paint is a graph node

`NewComposer` walks the tree once and builds a `paintNode` per component:

```go
n.node = prop.NewComputed(func() int {
    n.rev.Get()
    n.covered = false
    if b, ok := w.(Bounded); ok {
        r := b.Bounds()
        if _, isContainer := w.(Container); !isContainer {
            fillRect(c.frame.Cells, r, c.clearStyle(n)) // a leaf
            n.covered = true
        } else if !paintable(w) {
            ... // a hidden container
        } else if bp := backgroundProp(w); bp != nil {
            ... // a container with a declared background
        }
        // a chrome-only container pre-clears nothing
    }
    outer := c.frame.sink // placements are filed under this node
    n.places = n.places[:0]
    c.frame.sink = func(p graphics.Placement) { n.places = append(n.places, p) }
    if paintable(w) {
        w.Render(c.frame)
    }
    c.frame.sink = outer
    c.painted++
    n.stamp = c.frameSeq
    return c.painted
})
```

The pre-clear is three cases and a fall-through, spelled out under
[damage semantics](#damage-semantics-pre-clear-leaves-fill-backgrounds-repaint-in-z-order)
below; `c.clearStyle(n)` is what makes it the nearest ancestor's
background rather than the terminal default.

Evaluating the computed *is* painting the component. Because `Render` runs
inside an evaluation context, every property the component reads while
painting — `Content`, `Style`, `IsFocused()`, `IsHovered()`, a bound
viewmodel computed — is recorded as a dependency of that component's paint
node, automatically. This is the payoff line from the package comment:
**"AffectsRender" metadata is discovered, not declared.** WPF makes you
annotate each dependency property with `FrameworkPropertyMetadata
(AffectsRender)`; gooey observes what the paint actually read, and
because computeds re-record on every evaluation, the metadata is always
exactly current — even through conditional reads.

The consequence is minimal damage with zero bookkeeping in components: a
`Set` on any property dirties precisely the paint nodes that read it,
and `Composer.Frame` re-evaluates only dirty nodes into the persistent
buffer. `components/composer_test.go` pins this down (change one of three texts,
exactly one node repaints), and `cmd/props` shows it live (a tick
repainting 2 of 8 components).

### Damage semantics: pre-clear leaves, fill backgrounds, repaint in z-order

Before a dirty leaf repaints, its rect is cleared — to the nearest
ancestor's `Background`, not to the terminal default, so a `Text` inside
a colored panel repaints without punching a default-colored hole. The
read happens inside the leaf's paint node, so the panel's `Background`
property becomes a dependency of every leaf that clears against it:
recolor the panel and they all repaint, automatically.

A chrome-only container still clears nothing. A `Border`'s bounds
enclose its child; if a title change cleared the whole border rect, the
child's cells would be blanked — and the child's paint node, being
clean, would never repaint them. So containers overpaint their chrome
(box characters, title) in place, and a title change costs exactly one
component.

A container *with* a `Background` is different by declaration: its fill
covers its children, so the Composer's z-ordered repaint puts them back.
Z-order is document order (children above parents, later siblings above
earlier), and the paint loop forces a repaint of every node above a rect
somebody below just painted — the forcing is a `Set` between
evaluations, never inside one, so the evaluation-only-reads discipline
holds. The same pass makes overlapping `Canvas` children and
runtime-hidden containers correct, and two exemptions keep the counts
tight: a chrome-only container never forces its own descendants, and a
`Decorator` (a component that owns no cells, like the ItemsView row
highlight) is never forced from below. All of it landed as
[PR #88](https://github.com/WonderForgeLabs/gooey/pull/88) (epic
[#26](https://github.com/WonderForgeLabs/gooey/issues/26)), whose three
children are the three cases:
[#27](https://github.com/WonderForgeLabs/gooey/issues/27) the
ancestor-aware leaf pre-clear,
[#28](https://github.com/WonderForgeLabs/gooey/issues/28) the z-ordered
repaint, [#29](https://github.com/WonderForgeLabs/gooey/issues/29) the
hidden container.

### Layout runs outside the evaluation context

`Composer.Frame` runs layout unconditionally, every frame, before
touching the graph:

```go
c.root.Measure(Size{c.cols, c.rows})
c.root.Arrange(Rect{0, 0, c.cols, c.rows})
```

Two reasons, both deliberate. First, layout at terminal scale is cheap —
the POC does not need `AffectsMeasure`-style layout invalidation, and
skipping that machinery keeps the model small. Second, and more
structurally: layout runs *outside any evaluation context*, so property
reads during `Measure` (e.g. `Text.Measure` reading `Content` to count
lines) record nothing. If they did, every content property would become
a dependency of nothing-in-particular, or worse, of whatever computed
happened to be evaluating. The read-vs-subscription distinction in
`prop` is exactly what makes this a one-line non-event.

Bounds changes are reconciled after layout in a per-frame sweep: each
`paintNode` remembers its component's last bounds, and if arrange moved
the component, the vacated region is cleared (to the ancestor
background, so a component shrinking inside a colored panel leaves no
default-colored scar) and the node's `rev` source is bumped —
`rev.Get()` is the first line of every paint closure, so bumping it
force-dirties the paint node. That is how a moved-but-content-unchanged
component still repaints at its new position. The same sweep catches
`Hidden`↔`Visible` flips: the `Visibility` field is plain, so nothing
dirties on its own; the sweep notices the delta, clears the vacated
rect, and drops the node's recorded placements so the pixel plane
follows the cells off screen. Every `Set` in the sweep happens outside
any evaluation — the sweep is the legal place to force-dirty, the same
call-site rule as everywhere else.

### restoreUnder: the reverse half of the z-order pass

The forward paint loop restores what sits *above* a rect somebody
painted. `restoreUnder` is its mirror: when a rect *leaves* the screen —
a component turned `Hidden` or `Collapsed`, departed in a `Dynamic`
re-sync, or moved away — every still-visible node whose bounds
intersect the vacated rect is force-dirtied, and the ordinary paint
loop lays them down again in z-order, in the same frame. The forward
pass can only force nodes *above* a painter, so a vanished overlay's
vacated cells have to be restored from this side. This is what overlays
required to exist: a dismissed menu dropdown or an expired toast covers
cells whose owners are clean paint nodes that would never repaint on
their own. The vanished component itself paints nothing — erasure is a
sweep, and costs zero paint nodes.

### Dynamic: structural change without a rebuild

The tree is *mostly* static: a Composer is rebuilt when the whole tree
is replaced, which is what markup hot reload does. A container that
changes its own child set while the composition is live — `ItemsView`
realizing the rows that actually fit — implements `Dynamic`
(`dynamic.go`):

```go
type Dynamic interface{ SetStructureHook(func()) }
```

The Composer hands every `Dynamic` a hook at build time; the container
calls it after changing what `ChildComponents()` returns — legally from
inside `Measure`/`Arrange`, since layout is exactly when a list learns
its size. The hook only raises a flag. The next `Frame` re-syncs paint
nodes and the input tree after layout and before painting, so a row
realized this frame is painted this frame, and the sync **keeps the
node of every component that is still there**, clean/dirty state and
recorded dependencies intact — realizing one new row paints one new
row, not the tree. Departed components get their last rectangle
cleared, `restoreUnder` run beneath it, and their pixel placements
queued for removal. `Composer.InvalidateStructure` triggers the same
re-sync for a shape change made from outside the tree — the control
plane's markup patch path is the motivating caller. The node-preserving
diff is specified in
[specs/2026-08-10-datatemplates.md](specs/2026-08-10-datatemplates.md)
and shipped, with ItemsView virtualization, in
[PR #83](https://github.com/WonderForgeLabs/gooey/pull/83) (epic
[#14](https://github.com/WonderForgeLabs/gooey/issues/14)).

`Frozen` (`component.go`) is the adjacent seam, and it reaches the same
flag from the other side: a component whose `Frozen() bool` reports true
takes its *subtree* out of play — no focus stops, no Startables, every
pointer event retargeted to the host — while staying live itself, which
is what lets a design surface keep its own gestures over a picture of a
UI. The Composer arms an observer for it, `armFrozen`, the same shape as
`armVisibility`: a computed that calls `Frozen()`, so any property read
inside subscribes by the ordinary call-site rule, and a real flip raises
the same structural-change flag a `Dynamic` container raises. The
re-sync runs in that frame before anything paints, so the key that turns
design mode on leaves nothing in the subtree reachable by the very next
event. Freezing costs no repaint of its own. The limit is the usual one:
the observer subscribes to what `Frozen()` *reads*, so an implementation
over a plain bool field records no dependency and stays sampled. Design
record:
[specs/2026-08-14-frozen-observed.md](specs/2026-08-14-frozen-observed.md).

The bool is now a **projection** of a wider answer. "Renders but does not
act" is all-or-nothing, and a design surface needs *frozen except X* — so
the framework asks `FrozenAllows` for a `gooey.Allow`, a comparable
bitmask of interaction categories (focus, each class of key, scoped
bindings, mnemonics, pointer, hover, Startables). `AllowAll` is a member
of that lattice and means "not frozen", so `isFrozen` is exactly
`allow != AllowAll` and there is still one observed value per component
rather than two that can disagree. Nothing about the observer changed:
`armFrozen`'s computed calls both methods, so a host whose permissions
are derived from properties is subscribed by the same call-site rule, and
the per-frame sweep compares `Allow` values — raising the structural flag
on **any** change to the set, because a subtree that starts allowing
hover really does need its watcher registrations rebuilt. Markup reaches
it as `<Frozen Allow="…">` (`components/frozen.go`), and the set is
composed with `handlers/sets`; the vocabulary and its two closure rules
are in
[markup-reference.md](markup-reference.md#the-allow-vocabulary).

### Start and Close: the composition owns its goroutines

Some elements own a background goroutine — `Timer` (a non-visual
attachment), `Spinner` and `ProgressBar` when animating, `Tooltip`'s
show delay, `ToastHost`'s expiry clock. They implement `Startable`:

```go
type Startable interface {
    Start(post func(func())) (stop func())
}
```

The Composer discovers them on the same walk that builds paint nodes
(attachments included), and `Composer.Start(disp)` brings them to
life; `Composer.Close` stops them. `post` is the *only* way a started
goroutine may reach the property graph: it queues work onto the UI
goroutine via `Dispatcher.Post`, per the confinement rule. Nothing runs
until `Start`, which makes "started" a property of the composition
rather than of the component — a tree that was built but never composed
never ticks. A `Dynamic` re-sync gets the same treatment: arrivals are
started if the composition is running, departures are stopped, so a row
realized on frame 40 is treated exactly like one that existed at frame
0.

The stop function must **join**, not just signal — `close(done);
<-stopped` — because a tick that already won its `select` still posts
before stop returns, so `Close` guarantees no further posts, ever. A
stop that only closes a channel lets one last tick land after teardown,
which is precisely the kind of lifetime bug that flakes in CI and
nowhere else. That is one line of difference and it was written out by
hand in seven controls, so it now lives once, in `startable.go`:
`gooey.Every(post, d, fn)` for a ticker and `gooey.Delays` for any
number of one-shots that stop together. `Timer`, `Spinner` and
`ProgressBar` return `Every`; `Tooltip` and `ToastHost` return
`Delays.Start`. A `Startable` rolling its own done/stopped pair is now a
claim that neither shape fits, and nothing will catch it if the claim is
wrong. Hot reload leans on the whole
contract: `App.attach` closes the outgoing Composer before the new one
starts, so a replaced tree's timers cannot keep ticking against a
viewmodel nobody is showing.

## The input system

### One ordered stream

Keys and mouse reports arrive interleaved on the same tty, so gooey
keeps them on one ordered stream rather than two channels that could
reorder: `input.Event` is a tagged union of `KeyEvent` and `MouseEvent`
(`EventKey`/`EventMouse`, with `KeyOf`/`MouseOf` constructors and
`IsKey`/`IsMouse`/`IsMove` predicates).

The `input` package is the terminal-independent vocabulary, and it
exists for an import-graph reason spelled out in its doc comment: `term`
reads bytes and produces `input.Event`; `gooey` routes `input.Event`
through the component tree; `input` is the one package both import, so the
graph stays a line rather than a cycle.

`input.Decode` is a pure function over raw bytes — CSI and SS3
sequences, xterm modifier params, shift-tab (`ESC [ Z`), control bytes
normalized to `ctrl+letter`, UTF-8 runes, and SGR mouse reports (which
share the CSI shape but carry a `<` parameter prefix, so they never
reach the key mapping). Both mouse encodings are decoded: SGR
(`CSI < … M`) and the legacy X10 form (`CSI M` plus three bytes) that
terminals fall back to when they ignore the SGR request. The legacy one
is not optional to support — its trailing bytes are printable ASCII, so
an *undecoded* report does not merely lose the event, it injects
phantom keystrokes (a wheel notch arrives as `a`, a click as a space)
that reach the app as real commands. Its tri-state return (`ok`, consumed count)
distinguishes "incomplete, feed me more bytes" from "complete but
unmapped, skip it". The third state is the one whose violation is
silent: under `idle` there are no more bytes to feed, so `Decode`
guarantees it never answers "incomplete" then — it always consumes a
byte or produces an event. A decoder that broke that guarantee would
strand its buffer and go permanently deaf while still painting, which
is the failure `App.Run`'s decoder-death watch cannot see, because the
goroutine never returns. `term.DecodeEvents` adds the only two things that
are genuinely I/O: reading the tty in a goroutine, and the 40 ms
`EscTimeout` that settles the classic ambiguity — a lone ESC and the
first byte of an escape sequence are the same byte, and only the absence
of a follow-up within the timeout proves the user meant the Esc key.
`input.ParseGesture` is the third leg: it parses the markup gesture
syntax (`"ctrl+s"`, `"shift+tab"`, `"j"`, `"esc"`) into a `KeyEvent`,
and because `KeyEvent` is comparable, gesture matching is `==`.

Mouse reporting is opt-in (`Screen.EnableMouse`, SGR mode 1006 plus
button and any-motion tracking), not part of `Raw` — motion reports are
just bytes on the tty, and an app that treats any byte as a keypress
would exit when the pointer moves. `Restore` disables it
unconditionally; leaving a terminal in tracking mode after exit is the
one unrecoverable mistake.

### Focus is framework-owned, and focus damage is just property damage

A component becomes a focus stop by embedding `FocusState`, which
implements `Focusable` and `FocusTarget` and keeps the framework-set
flag in a source property:

```go
type FocusState struct{ focused *prop.Property[bool] }

func (f *FocusState) SetFocused(v bool)  { f.state().Set(v) }
func (f *FocusState) IsFocused() bool    { return f.state().Get() }
```

This is the pattern to internalize, because it recurs: **framework state
stored in a source property becomes paint damage for free.** A `Render`
that reads `IsFocused()` picks focus up as an ordinary paint dependency,
so moving focus repaints exactly the component that lost it and the one
that gained it — `components/input_test.go` asserts 2-of-4. No focus-changed
event, no invalidate call, no component code beyond reading the flag while
painting.

The `FocusManager` (built by `NewFocusManager` from the same tree walk
the Composer does, owned via `Composer.Focus()`) holds the focus order
(tree order, filtered to focus stops), the parent map, and the
`KeyBinding` lists per component. `FocusNext`/`FocusPrev` move in tree
order with wrapping, skipping anything inside a `Collapsed` subtree.

### Routed dispatch

`FocusManager.Dispatch` is WPF-style routing in three phases — tunnel
down, bubble up, then the page-scoped accelerators on whatever the
focused chain declined. Tunnelling, explicit capture and
`CanExecute` are one design and landed together in
[PR #86](https://github.com/WonderForgeLabs/gooey/pull/86) (epic
[#31](https://github.com/WonderForgeLabs/gooey/issues/31)), which is
where the argument for why they could not be separated lives:

```go
// tunnel: root -> focused, first consumer ends the dispatch
for d := m.depth(start); d >= 0; d-- {
    if h, ok := m.ancestor(start, d).(PreviewKeyHandler); ok && h.PreviewKey(ev) {
        return true
    }
}
// then bubble: focused -> root. Three things at each level, in this order:
// the bindings declared there, then the attachments, then the component.
for n := start; n != nil; n = m.parent[n] {
    for _, b := range m.bindings[n] {
        if b.Gesture == ev && CanExecute(b.Command) {
            b.Command.Run()
            return true
        }
    }
    if attachedKey(n, ev) {
        return true
    }
    if h, ok := n.(KeyHandler); ok && h.HandleKey(ev) {
        return true
    }
}
// then the page-scoped accelerators, on what the chain declined
for _, w := range m.mnemonics {
    if h, ok := w.(MnemonicHandler); ok && m.reachable(w) && h.HandleMnemonic(ev) {
        return true
    }
}
```

The event **tunnels** first: every ancestor from the root down to the
focused component implementing `PreviewKeyHandler` is offered it, and the
first to take it ends the dispatch — no target handling, no bubbling, no
bindings. That is the parent-veto mechanism (modal scrims, masked
inputs, an overlay layer), and it is deliberately a separate interface
rather than a flag on `HandleKey`, so a component opts into the
tunnelling phase explicitly. `PreviewMouseHandler` is the same for
pointer events, motion included.

Then the event bubbles: it starts at the focused component and walks up the ancestor chain;
at each level, `KeyBinding`s attached there match first, then the level's
*key-handling attachments* (`attachedKey`), then the component's own
`HandleKey`. The first `true` stops propagation.

Both halves of that middle slot are load-bearing, and neither survives
being swapped. Attachments run **after** the bindings, so a gesture the
page declared out loud outranks a behaviour that would otherwise absorb
it — a `<KeyBinding Gesture="/">` on a list keeps meaning what it says.
They run **before** the host, because a host has usually claimed the
letters already: `ItemsView` takes `j` and `k` for navigation, so an
attachment offered keys after it could never search for a word beginning
with either. Reversing the two lines compiles, and passes almost
everything; `TestAttachmentKeysPrecedeHost` is what fails. This is the
seam a *behaviour* needs and a binding cannot express — one gesture to
one command versus a whole class of keys with state carried between them,
which is how type-ahead search is an attachment rather than a component.

Tab, shift-tab, and the arrow keys navigate focus only in the *unconsumed
tail* of that walk — which means any of them is overridable by simply
handling it, and is what lets a list pane keep its own arrow handling
while buttons and checkboxes let arrows fall through to navigation.
Arrow navigation is spatial (XAML's XYFocus): the nearest focus stop
whose center lies in that direction, preferring stops in line with the
current one, falling back to tree order so a direction is never a dead
end.

Two narrow, opt-in seams extend that routing. `FocusHost` is for a
component that moves focus among its own children — a toolbar whose
arrows walk along it, a menu bar: the `FocusManager` hands itself to
every host it walks past (on the first walk and on every re-sync), and
the only useful thing a host can do with it, `SetFocus`, checks its
argument is a live focus stop, so a stale pointer from a replaced tree
fails safely. A focus host is not a focus trap — tab walks straight
through it in tree order, and declining an arrow hands the key back to
spatial navigation, which is how up and down leave a horizontal bar.
`MnemonicHandler` is the seam for *page-scoped* accelerators — a menu
bar's alt+letter. Key routing never leaves the focused component's
ancestor chain, and a menu bar is a sibling of the content it overlays,
so the dispatcher collects implementers on the same walk that finds
focus stops and offers them only the keys nothing else consumed: every
`PreviewKey`, `KeyBinding`, and `HandleKey` in the focused chain
outranks a mnemonic, and a mnemonic outranks the framework's own
tab/arrow fallbacks.

`KeyBinding` is non-visual: it implements the `NonVisual` marker and
hangs off its parent as an *attachment* (`Base.Attach`), walked for
input but never measured, arranged, or painted. Attachment position is
what scopes it: because dispatch only visits bindings on the focused
component's ancestor chain, a binding declared inside a control fires only
while that control's subtree has focus, and one on the page root is
global. `cmd/reader` shows both halves of that: its four
`<KeyBinding>`s (q, esc, ctrl+c, a) sit on the root `<Grid>` in
`reader.gooey` and are deliberately global, while Enter is scoped
without a binding at all — `storylist.gooey` passes
`Activate="{{.Open}}"` to its `ItemsView`, whose own `HandleKey` takes
Enter, so Enter opens a story only when the list has focus and does
nothing from the reader pane, with no `if` anywhere.

Event fields are typed `gooey.Action`: something that can `Run()` and can
say whether running is legal (`CanExecute()`). `Command` is still just
`func()` and implements it trivially — always executable — so every
existing delegate keeps working. Markup event attributes resolve to an
Action either from the binding context (`Click="{{.Save}}"`, the func
living in the viewmodel) or from the code-behind registry by bare name
(`Click="OnSave"`, from `Context.Handlers`).

The second implementation is `gooey.NewCommand(run).When(cond)`, where
`cond` is an ordinary `*prop.Property[bool]`. **That property is
CanExecuteChanged**, and there is no event to raise, because the call
site decides what a `CanExecute()` call means: asked from `Render` it
records a dependency, so the condition flipping repaints exactly the
components that asked; asked from a key or mouse handler it records
nothing and is only a question. `Button` does both — dim while disabled
(the paint read) and refusing enter, space and clicks (the handler
reads) — and a `KeyBinding` whose command is disabled does not match at
all, so the gesture is not consumed and the key keeps bubbling. `Run()`
is itself a no-op while the condition is false, so "disabled" is
structural rather than a rule every caller has to remember.

### The pointer: hit-testing, hover, capture, click synthesis

Mouse events route the same way keys do — one target, then its
ancestors — but the target comes from hit-testing instead of focus.
`FocusManager.HitTest` returns the deepest component whose arranged
`Bounds()` contain the cell, children before ancestors and later
siblings before earlier ones (they paint on top); `Collapsed` subtrees,
zero-size components, and `HitTestTransparent` components are not hit.
The transparency marker is what lets a page-spanning overlay host exist
at all: a `ToastHost` or an `AdornmentLayer` sits above everything as
the root's last child, which makes it the *first* thing hit-testing
finds — an invisible layer that would eat every click and starve every
hover beneath it. Transparency is about the component's own surface,
not its subtree, so the toasts and adornments inside stay hittable. The
walk allocates nothing, because it runs on every motion report.

`DispatchMouse` runs three framework behaviors before the app sees
anything:

- **The frozen retarget**, once, at the top: a frozen subtree does not
  act, so for every routing purpose the effective hit is the frozen host
  — it takes the event, the implicit capture, the focus a press moves,
  and the click synthesized on release. `HitTest` still returns the
  deepest component (it is a query, not dispatch); `MouseTarget` is the
  query that models where an event would actually route.
- **Focus-follows-click**: a press moves focus to the nearest focusable
  component at or above the hit — or, when there is none, the first
  focusable *below* it, so clicking a pane's border or title focuses
  the pane rather than doing nothing.
- **Hover tracking**: `setHover` moves the hover flag to the nearest
  `HoverTarget` at or above the hit — so hover composes upward; a
  `Border` can highlight while the pointer is over the `Text` inside it.
  `HoverState` is the exact twin of `FocusState`: the flag is a source
  property, `IsHovered()` read during `Render` is a paint dependency,
  and crossing between components repaints the one entered and the one
  left, nothing else.

Raw motion is deliberately not delivered to components — any-motion
tracking is high-frequency — except to components that opt in via
`MouseMoveHandler` (drag, resize). Everyone else sees enter/leave
through hover.

Focus-follows-click and hover tracking are both skipped while the
pointer is **captured**. The frozen retarget is not — it decides what
"the hit" even means, so it runs first, every time.

A press captures the component it landed on, and until the release every
pointer event routes to that captor regardless of what the pointer is
actually over. That is what makes a drag work: motion outside the
component still arrives, so a scrollbar thumb, a splitter or a text
selection keeps tracking a pointer that has left its bounds. Hover
transitions are suppressed for the length of the gesture — otherwise
dragging across the tree would repaint every component the pointer
crossed — and catch up with reality when the capture ends.
`CaptureMouse`/`ReleaseCapture` take it explicitly, for a gesture that
must outlive one press; `Captured()` reports the holder. An implicit
capture is scoped to a single press-release pair, and a fresh press ends
it, which is also the recovery path when a terminal drops a release.

A `MouseClick` is synthesized on release when the pointer is still
inside the captor — the captor itself or anything under it. `MouseClick`
is not a terminal report; it exists only as this synthesis. Keying it to
the captor is what makes a button pressed, dragged off, and dragged back
still fire, while one released elsewhere does not. The click carries a
**count**: a second click on the same component within
`DoubleClickInterval` (400ms) arrives as `Count: 2`, which is what
`ItemsView` activates on and what `TextBox` selects a word on. There is
no triple click — a third click restarts the sequence at 1; that and
OSC 52 clipboard are deferred deliberately, tracked in
[#106](https://github.com/WonderForgeLabs/gooey/issues/106).

Wheel events, like everything else, go to the captor while captured and
to the component under the pointer otherwise — never to the focused one,
per terminal convention. `Button` exercises the rest: focused, hovered,
pressed and enabled are four property reads in its `Render`, so each
state change repaints just the button.

### Overlays are built from these pieces

`Popup` (`components/popup.go`) is the shared Go-side primitive under
the `MenuBar` dropdown and the `Tooltip` — extracted once the framework
had grown four hand-rolled copies. An *owner* component stays in the
tree, keeps focus, and decides what the popup shows; the *surface* is a
leaf child returned last from `ChildComponents` (document order is
z-order, so last paints on top) whose pre-clear paints exactly the
popup rectangle; the `Popup` itself is the lifecycle — an open
property, focus save/restore per the capture-at-open rules, held
pointer capture so a press anywhere outside dismisses, and esc as the
key fall-through. It solves one subscription subtlety once, and it is
worth knowing because it recurs: a `Collapsed` surface never evaluates
its `Render`, so a closed popup would have no dependency edge from its
open property and the first Open would schedule no frame. The surface
therefore stays `Visible` at a zero rect while closed — its node
evaluates on the very first frame, reads the open property, and the
subscription exists before the popup has ever opened. Around it,
`Tooltip` rides the `HoverWatcher` attachment seam, and `ToastHost` and
`AdornmentLayer` are the page-spanning hosts that lean on
`HitTestTransparent` here and on the Composer's `restoreUnder` sweep to
vacate cleanly. Design record:
[specs/2026-08-10-popup.md](specs/2026-08-10-popup.md).

## The runtime: gooey.App

`App` (`app.go`) is the framework-owned run loop: the terminal's
lifetime, the input decoder, the Dispatcher, frame scheduling,
hot-reload swaps, and the console signal story in one object. It exists
because every demo had been hand-writing the same sixty lines — open
the screen, raw mode, mouse, decoder goroutine, `select` loop, deferred
restore — each copy with its own subtly different bugs. The loop is
deliberately not extensible by adding select cases: everything
asynchronous — timers, signals, watchers, network handlers — reaches
the UI through `App.Post` (the Dispatcher), which is the confinement
rule anyway, and every hook — `BeforeFrame`, `AfterFrame`, `OnEvent`,
`AfterEvent`, `OnSwap` — runs on the UI goroutine, where touching
properties is legal. The tree comes from a `Content` (`Build` +
`Watch`): `markup.Page` for a markup app, `gooey.Tree` for one built in
Go, and `App.Swap` is the same replace-the-composition seam for a tree
that arrives from anywhere else — a control plane pushing markup, most
notably.

### Signals are UI-loop events

The full story is
[specs/2026-08-10-runtime-signals.md](specs/2026-08-10-runtime-signals.md);
the shape of it: every signal is delivered onto the UI goroutine
through the Dispatcher rather than handled where it lands, because the
terminal work has to be ordered against frames. ctrl+c and SIGINT are
*two different events*: in raw mode `ISIG` is off, so ctrl+c arrives as
byte `0x03`, is decoded into an ordinary key event, and routes through
the tree — the App's quit key matches it only on what the tree
declines, so a component that wants ctrl+c keeps it. An external
SIGINT or SIGTERM is a real signal: it runs the `WithShutdown` hook
(bounded by its timeout, terminal still up), quits the loop, and `Run`
returns a `*SignalError` that `gooey.Exit` turns into exit code 128+n.
SIGWINCH re-queries the size and calls `Composer.Resize` (new buffer,
whole tree dirtied — layout already runs every frame, so the tree
re-measures itself). SIGTSTP does the classic restore/reset/re-raise
dance and declines the stop where POSIX will not honor it (an orphaned
process group). A panic under `Run` restores the terminal *first* and
then re-panics, so the stack trace prints onto a cooked screen.

### Suspend, and why re-acquire invalidates the flush

`App.Suspend(fn)` hands the terminal to fn — an editor, a shell — and
takes it back afterwards. It is only correct because teardown joins the
input decoder (a reader left parked on the tty would eat the child's
keystrokes), and interrupts are shielded while fn runs: the tty driver
signals the whole foreground process group, so the ctrl+c a user aims
at the child arrives here too. On the way back in, the alternate screen
comes back *blank* — the retained buffer is right, the screen is wrong,
and no component needs to repaint. That is exactly what
`Composer.Invalidate` corrects: it forces the next flush to re-send the
whole buffer and every placement, without touching a single paint node.
The same invalidation runs after every acquire, which is why resume
from ctrl+z and return from Suspend repaint correctly with nothing
dirty.

### Companions

Companions ([specs/2026-08-10-companions.md](specs/2026-08-10-companions.md),
`companion.go`) are the app's background services: work that must be
running for the UI to mean anything and must not outlive it — the
motivating case was a Temporal worker that is not a separate program in
any interesting sense. A `Companion` is `Name`/`Start`/`Wait`, with
`CompanionFunc` for Go code and `CompanionCmd` for a child process, and
its lifetime is made explicit: started *before* the tree is built and
before raw mode, so a service that cannot start prints its error onto a
cooked terminal and a `Build` that talks to it finds it up; supervised
while the app runs, so a companion that dies quits the app and `Run`
reports which one; stopped and waited for on every exit path — quit,
signal, cancellation, panic — *after* the terminal has been handed
back, so a slow shutdown happens on a cooked screen. Cancelling the
app's context is the only stop mechanism, because two stop mechanisms
is one too many. Companions keep running across `Suspend`: they are
background services, and the child owns only the terminal.

## Markup

The `markup` package is the XAML-analog authoring surface: XML elements
map to components, attributes to properties, `{{...}}` expressions
(Go-template spelling, but not Go templates) to bindings resolved
against a property registry. No reflection anywhere — resolution is
maps and type switches.

### Three loading tiers, one seam

`markup.Load` takes an `fs.FS`, and that seam is the whole deployment
story:

```go
// Load reads and builds a markup file from any fs.FS — os.DirFS in
// dev, embed.FS in release; the loader cannot tell the difference.
```

- **Dev**: `os.DirFS` behind `markup.Page`, a 300 ms polling watcher over
  the page and the files it includes — a placeholder for filesystem
  notifications ([#53](https://github.com/WonderForgeLabs/gooey/issues/53)).
  It reports only THAT something
  changed; `gooey.App` does the rebuild on the UI goroutine, because
  resolving bindings touches the property graph. Parse errors leave the
  current tree in place, so a bad edit never blanks the running app.
  A reload rebuilds the tree and the `Composer` with it — the same
  whole-tree swap seam as `App.Swap`, distinct from the *within*-a-live-
  composition structural changes `Dynamic` handles — while viewmodel
  properties live outside the tree and survive; that is why hot reload
  keeps your state. Focus does not: it is derived from the tree that was
  replaced ([#52](https://github.com/WonderForgeLabs/gooey/issues/52)).
- **Release**: `embed.FS`. The same call is a natural no-op — embed.FS
  reports constant zero ModTimes — so dev and release run identical
  code.
- **Future**: `gooey gen`, compiling markup to Go at build time. This
  tier is designed, not built; it appears as a consequence in the
  [x:Property spec](specs/2026-08-10-markup-declared-properties.md)
  (declared property surfaces would give it a typed per-control API for
  compile-checked instantiation).

### The binding DSL and lvalue semantics

`Context` is the binding environment: `Values` (what `{{.Name}}` roots
resolve against), `Styles`, `Components` (custom builders), `Handlers`
(code-behind commands), `Named` (components collected by `Name="..."`, read
back via the generic `markup.Find[T]`), and `Includes` (see below).

`bindText` turns mixed content like `count: {{.Count}}` into a
`prop.NewComputed[string]` that concatenates literal parts and property
reads. The crucial property is in its comment:

```go
// Resolution happens once at build time — handles, not values — so
// evaluation does no lookups; this is the "lvalue semantics" of the
// design.
```

`{{.Count}}` resolves at build time to the `*prop.Property[string]`
*handle* in `Values`; the computed closes over the handle and calls
`Get` on it at evaluation. So evaluation does zero map lookups, and —
because the computed's reads are recorded like any other — a bound
`Text` repaints exactly when the bound source changes. A binding is not
string interpolation over a snapshot; it is an edge in the graph.

Event attributes go through `Context.Command`, the two-halves split
described in the input section: `{{.Save}}` resolves a func out of
`Values` (works in markup-only controls, no code-behind), a bare name
resolves through `Handlers` (requires code-behind).

### Handler namespaces: behavior as a capability grant

The DSL has a third, prefixed form for event attributes — shipped, not
sketched:

```xml
<Gooey xmlns="wonderforge.io/gooey/2026"
       xmlns:net="gooey.dev/handlers/net">
  <Button Content="fetch" Click="{{net:Get .Url | into .Body}}"/>
</Gooey>
```

A prefixed expression produces a `gooey.Command` from a
*framework-provided* handler instead of a viewmodel func. Four handler
packs ship — `net` (HTTP GET), `fs` (rooted file access, read-only
unless the writable grant is registered), `temporal` (standalone
activities), and `exec` (allowlisted local commands, conventional
prefix `sys:`) — plus the workflow namespace (conventional prefix
`wf:`) that signals exactly one Temporal workflow, which is what lets a
workflow serve its own approval UI. The heavy packs are nested modules,
so the root module stays dependency-free. The design holds three lines:
**registration is the capability grant**
(`markup.RegisterHandlers(URI, provider)` — a document reaches exactly
the capabilities its host registered and can never widen its own grant,
which is what makes served, untrusted markup safe to run); arguments
keep lvalue semantics (`.Url` is a handle read at invoke time, not at
load); and results return through the optional `| into` stage,
delivered onto the UI goroutine via the context's `Dispatcher`
(`markup.Target.Deliver` — delivering to an absent target is a no-op)
because properties are goroutine-confined. `| into` is the only stage
that ships; `| err`, `| progress`, multiple targets and bounded
retry/timeout are designed in the v2 grammar and tracked as epic
[#38](https://github.com/WonderForgeLabs/gooey/issues/38). Grammar and
provider tables:
[markup-reference.md](markup-reference.md#handler-namespaces); the pack
taxonomy and grant doctrine:
[specs/2026-08-10-pack-distribution.md](specs/2026-08-10-pack-distribution.md);
the original design record, now history rather than plan:
[specs/2026-08-10-remote-handlers-design.md](specs/2026-08-10-remote-handlers-design.md).

### Value namespaces: the pull half of the same mechanism

A handler namespace answers "what happens when the user does this". It
is reachable only from an event attribute, and its result is *pushed*
into a property by `| into`. An ambient reading like the environment, or
a pure transform like uppercasing a name, is not an event — it **is**
the value of a binding, and wants to go where a binding goes:

```xml
<Gooey xmlns="wonderforge.io/gooey/2026"
       xmlns:env="gooey.dev/handlers/env"
       xmlns:str="gooey.dev/handlers/str">
  <Text>{{str:Upper .User}} @ {{env:Get `HOSTNAME`}}</Text>
</Gooey>
```

Same grammar, opposite direction. A `{{ns:Func …}}` expression in a
value position resolves at build time to a `*prop.Property[string]`
handle, exactly as `{{.Path}}` does, and composes with literals and
paths in the same interpolation. The damage guarantee comes for free:
the provider builds its handle with `prop.NewComputed`, so every
argument read runs *inside* an evaluation and is therefore a
subscription — `{{str:Upper .Name}}` repaints exactly the components
that display it, when and only when `.Name` changes. The registry is
separate on purpose: `markup.RegisterValues` grants a document the
capability to **read** a namespace, `markup.RegisterHandlers` the
capability to **write** one, so a namespace offering both is registered
twice and a host can grant either half. Three packs ship in-tree:
`handlers/env`, `handlers/str` and `handlers/sets` — the last one set
algebra over name sets, which is how a page composes `<Frozen Allow>`.
Decision record:
[specs/2026-08-12-value-namespaces.md](specs/2026-08-12-value-namespaces.md).

### UserControl: context isolation and the attribute hand-off

`markup.UserControl(fsys, "storylist.gooey", setup)` wraps a markup file
plus a code-behind setup as a `Builder`, registered like any custom
component and instantiated as an element: `<StoryList
Stories="{{.Stories}}"/>`.

The contract is **context isolation**: `setup` returns the instance's
*own* `Context`, and bindings inside the control's markup resolve
against it — never against the page. Data crosses the boundary through
element attributes, resolved in the *parent* context via
`Context.BindingValue`, which returns the raw context value — typically
a `*prop.Property[T]` handle — that setup wires into its own context or
components. This is XAML's DataContext-plus-dependency-property hand-off,
done with explicit handles instead of an ambient inherited value.
`Styles`, `Components`, `Handlers`, and `Includes` inherit from the parent
when the child leaves them nil; `Named` is scoped per instance, like
`x:Name` inside a template.

### Include: markup-only controls

`markup.Include` is the zero-code tier. If `Context.Includes` is set,
an unknown element resolves by convention: `<Card/>` with no registered
builder loads `card.gooey` from that FS. The instance's attributes
*become* the control's context — each resolves in the parent context
(binding → property handle, literal → string) and is exposed under its
attribute name:

```go
// So <Card Title="{{.Header}}" Sub="details"/> gives card.gooey a
// context where {{.Title}} is the parent's Header handle and {{.Sub}}
// is a literal.
```

Layout attributes (`Width`, `Margin`, `Grid.Row`, ...) still apply to
the instance itself and are not passed through. The result is a
component with a real property surface — implicit unless the control
declares it (below).

### Markup-declared dependency properties

A `.gooey` file declares its property surface with `<x:Property>` as
direct children of the root, under gooey's language-services namespace
(`xmlns:x="wonderforge.io/gooey/x"` — XAML 2009's `x:Property`, which
WPF specified and never shipped):

```xml
<x:Property Name="Title"   Type="string" Required="true"/>
<x:Property Name="Caption" Type="string" Default="no caption"/>
```

**Declared markup properties are ordinary dependency properties,
registered from markup — one property system throughout.** Each
declaration materializes the same artifact a code-behind wires by hand,
a `*prop.Property[T]`: a bound attribute passes the parent's handle
through, now type-checked; an absent one materializes a per-instance
source carrying the declared default; `Required` makes absence a load
error. This is the markup tier of registration, exactly as
`DependencyProperty.Register` is WPF's code tier.

The mechanics that keep it inside the framework's constraints:

- **Types are a type-switch table** (`markup.propKinds`), one row per
  type, each row's closures carrying their own `T`. `any` is the escape
  hatch for app types with no markup literal. No reflection, as
  everywhere.
- **`Element.Space`** — the parser keeps each element name's resolved
  namespace URI, which is how a declaration is told apart from a
  component without reserving the name `Property`. The xmlns table the
  handler-namespace work introduced now carries element dispatch too.
- **Declaring anything makes the control strict**: an undeclared
  attribute at an instantiation site is a load error, so a typo is
  caught rather than silently doing nothing. No declarations keeps the
  implicit pass-through, unchanged.
- **Merge with code-behind**: declarations resolve first into a
  pre-populated context (readable from setup via
  `Context.DeclaredProperties`), setup runs second and extends it, and a
  setup value colliding with a declared name is a load error — one
  source of truth, the same reason a property system rejects double
  registration.

Markup-declarable *attached* properties stay out (they would need a
dynamic per-element bag on `Base`), and a declared default materializes
a fresh source per instantiation, so hot reload resets it — the concrete
customer for `Name`-keyed state adoption. Reference:
[markup-reference.md](markup-reference.md#declared-properties-xproperty);
decision record:
[specs/2026-08-10-markup-declared-properties.md](specs/2026-08-10-markup-declared-properties.md);
shipped in [PR #84](https://github.com/WonderForgeLabs/gooey/pull/84)
(epic [#7](https://github.com/WonderForgeLabs/gooey/issues/7)).

`cmd/reader` is the working proof of the whole markup layer: a
`Grid Cols="26,1*,2*"` shell in `reader.gooey`, three UserControls with
their own contexts, bindable focus-aware pane titles, and
`<KeyBinding>`s scoped by where they are declared. Its design record is
[specs/2026-08-10-reader-design.md](specs/2026-08-10-reader-design.md),
and the running app is in [demos.md](demos.md).

## The control plane

A running gooey app can expose itself to out-of-process clients — an
agent over MCP, a test driver or a dashboard over gRPC — and the
layering rule is stated in `control/control.go`'s package comment:
**one path, one model** — the thesis of epic
[#108](https://github.com/WonderForgeLabs/gooey/issues/108). `control`,
in the root module, is the
in-process control-plane service; the gRPC server (`grpc/`, a nested
module) is a proto adapter over it and the MCP server (`mcp/`, another
nested module) is a tool adapter over it. A tool or an RPC does what
`control.Service` does, or it does not exist. The shared logic lives in
the root on purpose: the transports' SDKs are heavy, but what they
share is plain Go over the framework's own interfaces — the
alternative, one transport calling the other over loopback, would put a
network hop inside what is semantically a function call.

### Host, Service, and the snapshot ceiling

`Service` reaches the app through the three-method `Host` seam:
`Post` (onto the UI goroutine), `Composer()` (the live composition —
read per call, never cached, because every swap replaces it), and
`Swap`. `*gooey.App` implements it; it is an interface so the service
can be tested against a hand-run loop, the only honest way to test the
confinement rule. The methods are the whole remote surface: the tree
snapshot (`Tree`), screen text from the retained cell plane (`Screen` —
it never composes a frame, which would steal the app's damage counts),
the bindable values (`Values`/`Value`/`Set`), commands (`Invoke`),
input injection (`SendKeys`/`SendPointer` — into the one ordered
stream, routed via the composition so the app's quit key is out of a
remote client's reach), focus (`Focus`), the viewport (`Resize` —
advisory on a tty, where the next SIGWINCH overrides it), and the markup
operations (`SwapMarkup`, `PatchMarkup`, `Validate`, `Styles`,
`DeclaredSchema`, `Register`/`Unregister`). Failures are classified
(`KindInvalidArgument`, `KindNotFound`, `KindFailedPrecondition`,
`KindPermissionDenied`) so a transport maps them without parsing text —
gRPC into status codes, MCP into tool errors.

The snapshot serializes the tree without reflection, from the same
interfaces the Composer and the FocusManager already walk —
`Container`, `Bounded`, `Focusable`, the attachments — plus a type
switch over the built-ins for per-component props. An unknown Go
component still yields a useful node (type, bounds, layout, children);
its fields stay undiscovered, and that is the deliberate ceiling — a
semantic tree of roles, names and states is what would raise it for
unknown Go components, tracked in
[#101](https://github.com/WonderForgeLabs/gooey/issues/101).
Markup-built controls declare their way past it: an `<x:Property>`
surface is retained on the context and serializes with current values —
the declaration block finally read as the per-control wire schema the
x:Property spec said it was.

### Islands: registration is the grant, on the control plane too

A `Service` carries an optional `*Grant`, and a nil one is the host's
own service — unscoped, the only way to reach the whole app.
`control.Island("EditorPane", "Doc")` narrows an endpoint to the subtree
rooted at `Name="EditorPane"` plus the value namespace `Doc`, and
nothing else. This is the framework's existing model rather than a new
one: `markup.Context.Components`, `.Handlers` and `.Rules` all work this
way — the host registers, and what it registered is exactly what the
guest can reach. There is no token and no negotiation, because the grant
is a field on the server the host started, not a parameter on the call.
**The address is the capability**: one endpoint carries one grant, and
two guests with disjoint islands are two servers on two loopback ports.

Both halves of a grant are used. Some verbs are *refused* outside the
island, which is what `KindPermissionDenied` exists for — deliberately
distinct from `KindNotFound`, since a guest asking for something outside
its island must not be told the app has no such name, because it usually
does. `SendKeys` is the awkward one and the check has to be on focus
rather than on a name, since keys go wherever focus is: the focused
element must be inside your island, which a guest satisfies by calling
`Focus` on something it owns first. Other verbs are *filtered*:
`Service.VisibleDamage` clips damage rects to the island. Stated
plainly, because the distinction matters: a grant is **scoping, not
authentication**. It stops an attached guest from exceeding its brief;
it does nothing about something that can reach the host's own unscoped
endpoint, which is why v1 refuses a non-loopback bind outright. Decision
record:
[specs/2026-08-14-island-grants.md](specs/2026-08-14-island-grants.md).

### Bridge.Do and the settle barrier

The confinement rule takes its sharpest form here. Requests arrive on
transport goroutines; the property graph is unlocked and
UI-goroutine-only; so every `Service` method is UI-goroutine-only, and
transports marshal each call through `control.Bridge.Do`. `Do` waits
twice, and the second wait is the point: first fn itself, then a bare
barrier closure that does nothing but come back. It works because
`Dispatcher.Drain` snapshots its queue — a closure posted while a
drain runs lands in the *next* drain, and the run loop composes a
frame between two drains — so waiting for the barrier waits for the
repaint fn's Sets asked for. A screen read immediately after a command
invocation sees the new pixels, and the end-to-end proofs need no
sleeps. A panic inside fn is recovered into a `*PanicError` (a remote
client must not be able to kill the app), and a blocked run loop is a
`*TimeoutError`, reported rather than hung on.

Two more disciplines complete the rule. Results are plain copied data —
never a component or a property handle, which read later from a
transport goroutine would be exactly the bug the Dispatcher exists to
prevent. And every property Get the service performs happens outside
any computed evaluation, so it reads a value and records nothing — a
snapshot that subscribed would wire the control plane into the damage
graph and repaint the app every time a client looked at it. The
call-site rule from the property section, doing remote duty.

### The wire contract

The proto contract is a decision record,
[specs/2026-08-10-grpc-contract.md](specs/2026-08-10-grpc-contract.md):
package `gooey.control.v1`, additive-only evolution, and `TypedValue`
mirroring markup's `propKinds` table case for case — adding a wire type
means adding a propKinds row first, so the two type systems grow in
lockstep or not at all. There is exactly one documented exception, and
it names the axis the rule is really about: `image_bytes` /
`control.KindImage` has no propKinds row, because a propKinds row is a
parser for a markup *literal* and there is no way to spell a picture
inline — markup can bind one (`<Image Src="{{.Logo}}">`) without ever
being able to write one down. `ControlService` is the unary surface,
argument-for-argument the MCP tool inventory, which is what made
rerouting MCP over `control`
([PR #137](https://github.com/WonderForgeLabs/gooey/pull/137))
mechanical. `SessionService.Attach` is one
bidi stream: client acts apply in stream order (the remote mirror of
the one ordered input stream), and `FrameDelta` carries everything one
composed frame changed — property deltas, damage rects, the repaint
count — in a single message keyed by frame sequence, so the torn read
(values arriving without the frame they belong to) is impossible by
construction. `FrameDelta.repainted` is the same damage number the
contract tests assert on, put on the wire — on an *unscoped* session. On
one carrying an island grant both it and `damage` mean something
narrower, counting only the repaints touching that island, so two
sessions watching the same frame report different numbers. That is
correct rather than incidental: a guest's damage budget is its own
subtree, and the app's total is a measurement of something it does not
own. The committed generated
clients live in `grpc/gen` (Go) and `clients/` (Python, TypeScript).

## Designed, not built

`gooey gen` — compiling markup to Go at build time, with a typed
per-control surface for compile-checked instantiation. `<x:Property>`
declarations are its input: a declaration block is both a typed surface
and a per-control wire schema — the control plane now reads it the
second way; the code-generation reading is the one that remains. Epic
[#59](https://github.com/WonderForgeLabs/gooey/issues/59) breaks it into
codegen, typed surfaces, wire schemas and the CLI.

`Name`-keyed state adoption across hot reloads, so a declared default's
per-instance source survives a rebuild — epic
[#50](https://github.com/WonderForgeLabs/gooey/issues/50).

Styles with setters, so a control can be restyled from outside beyond
passing a style name in — epic
[#54](https://github.com/WonderForgeLabs/gooey/issues/54), whose
selector matching covers `:focus` and `:hover`.

This list used to be longer. The remote-handler design
([specs/2026-08-10-remote-handlers-design.md](specs/2026-08-10-remote-handlers-design.md))
shipped as the handler namespaces above; the control plane it gestured
at shipped as `control/` and its two transports — and neither changed
the foundations. The declared-properties design works *because* every
property is already a graph node ("the graph is the callback system");
the handler design works *because* bindings are already handles, not
values; the settle barrier works *because* a frame already sits between
two drains. That is the architecture doing its job.

Back to the [README](../README.md) for the short version, or on to
[demos.md](demos.md) to see all of this moving.
