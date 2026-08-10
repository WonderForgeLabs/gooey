# Architecture

This is the deep guide to how gooey works: the two rendering planes, the
lazy property graph, the retained component model, the damage-tracked
Composer, routed input, and the markup layer. It is grounded in the code
as it exists today — a proof of concept — so every section names the
real types and functions, quotes the load-bearing excerpts, and says
plainly where the POC stops. For a first walkthrough, start with
[getting-started.md](getting-started.md); for the markup syntax itself,
see [markup-reference.md](markup-reference.md); for what the demo apps
prove, see [demos.md](demos.md).

The one-paragraph shape: widgets are persistent objects in a retained
tree. Every visual property on them is a `*prop.Property[T]` in a lazy
dirty-tracking graph. The `Composer` gives each widget its own paint
node in that same graph, so a property change dirties exactly the
widgets that read it during their last paint. A frame is layout
(unconditional, cheap) plus repaint (dirty nodes only) into a persistent
cell buffer, flushed as ANSI. Input is a single ordered event stream
routed through the tree by focus (keys) or hit-testing (mouse). Markup
builds the same tree from XML, binding attributes to property handles.

## The two rendering planes

The founding question of the POC was "are there N rendering modes —
ansi, sixel, kitty, ...?" The answer, now baked into the package layout,
is no: **there is one cell renderer and N graphics protocols**, and they
are different planes, not alternative backends.

### The cell plane

Everything a widget tree normally is — text, borders, stacks, styling —
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
}
```

`render.Style` carries 24-bit `Fg`/`Bg` colors (zero value means
"terminal default") plus bold/underline/reverse. `render.Flush` walks
the buffer and emits ANSI: a cursor-home, then rows of runes with an SGR
sequence emitted only when the style changes between cells. This path is
universal — every terminal that can run a TUI can run it.

An honest POC note, straight from `render/ansi.go`:

```go
// POC note: full repaint every frame. The retained tree makes damage-rect
// diffing (compare against previous buffer, emit only changed spans) a
// drop-in replacement here — deliberately out of scope for the POC.
```

Damage tracking exists today at the *paint* level (the Composer repaints
only dirty widgets into the persistent buffer — see below), but the
*flush* still writes the whole buffer. The two optimizations are
independent, and the second is a local change to `Flush`.

### The pixel plane

Pixel content — the `Image` widget, a future canvas or chart — is the
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
- `graphics.Sixel` — DEC sixel (DCS `q`), colors quantized to a 6×6×6
  cube, 216 registers, under the 256 limit (xterm, foot, Windows
  Terminal ≥1.22, VTE ≥0.76, Konsole, mlterm).
- `graphics.ITerm2` — OSC 1337 inline images (iTerm2, WezTerm, mintty).

Each encoder is roughly fifty lines; adding a future protocol means
adding one more.

The fourth mode, halfblock, is deliberately *not* an `Encoder`. It is
the universal fallback that degrades pixel content back into the cell
plane: `graphics.DrawHalfblock` scales the image to `cols × rows*2`
pixels and writes each cell as `▀` with the top pixel as 24-bit
foreground and the bottom pixel as background — two pixels per cell,
works everywhere. Nothing is emitted beside the cells; the image simply
becomes cells. This asymmetry is why `Frame.Graphics == nil` means
"degrade during the render walk" rather than "skip images".

### How the planes meet: the Frame

The widget tree never knows which protocol is active. `gooey.Frame`
holds both planes:

```go
type Frame struct {
    Cells        *render.Buffer
    Graphics     graphics.Encoder
    Placements   []graphics.Placement
    CellW, CellH int
}
```

During the render walk, `Image.Render` does one of two things:

```go
func (im *Image) Render(f *Frame) {
    r := im.bounds
    if f.Graphics != nil {
        f.Placements = append(f.Placements, graphics.Placement{
            Img: im.Src, Col: r.X, Row: r.Y, Cols: r.W, Rows: r.H,
        })
        return
    }
    graphics.DrawHalfblock(f.Cells, im.Src, r.X, r.Y, r.W, r.H)
}
```

A `Placement` is a deferred draw — image, cell position, cell size.
`Frame.Flush` emits the cell plane first, then positions the cursor at
each placement and runs it through the active encoder, so pixel content
composites over the already-painted cells.

### Capability detection is a handshake, not config

`term.Screen.Detect` decides the protocol at startup by asking the
terminal, exploiting the one query every terminal answers:

```go
// Kitty query (tiny 1×1 RGB transmit, q=1 → responds if supported),
// then cell size, then DA1 terminator.
fmt.Fprint(s.tty, "\x1b_Gi=31,s=1,v=1,a=q,t=d,f=24;AAAA\x1b\\")
fmt.Fprint(s.tty, "\x1b[16t")
fmt.Fprint(s.tty, "\x1b[c")
```

Three queries go out in one burst: a Kitty graphics probe (a terminal
that supports the protocol echoes an APC response containing `i=31`),
XTWINOPS 16 (cell size in pixels, needed for sixel scaling), and DA1
(primary device attributes). DA1 is the terminator: terminals answer it
unconditionally, so whatever arrived before it is the answer set.
Sixel support is DA1 attribute `4`. iTerm2's protocol has no query, so
it is detected from `TERM_PROGRAM`/`LC_TERMINAL` environment variables.
The result lands in `term.Caps`, and `Caps.Best()` encodes the
preference order: `kitty > sixel > iterm2 > halfblock`. `cmd/probe`
prints exactly what this handshake found for your terminal.

One wrinkle worth knowing: file deadlines don't work on every tty, so
`readUntilDA1` runs the read in a goroutine and enforces the 500 ms
timeout with a `select`, abandoning the pending read on a terminal that
never answers — acceptable for a probe that runs once.

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
invalidation at all — `cmd/propdemo` demonstrates this live with a
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

### Widget, Container, Base

The tree is retained: widgets are persistent objects that survive from
frame to frame, so invalidation can be per-node instead of
rebuild-the-world. The core contract is three methods:

```go
type Widget interface {
    Measure(avail Size) Size   // desired size within avail (bottom-up)
    Arrange(bounds Rect)       // final bounds (top-down)
    Render(f *Frame)           // paint THIS widget only
}

type Container interface{ ChildWidgets() []Widget }
```

Two details matter more than they look:

- `Render` paints *this widget only*. Children are walked by the
  framework, not the widget — `Container` exists so `renderTree` (and,
  critically, the Composer) can enumerate them. This is what lets the
  Composer give every widget its own independent paint node.
- Containers paint nothing themselves unless they have chrome.
  `VStack.Render`, `HStack.Render`, and `Grid.Render` are empty;
  `Border.Render` draws only its box and title.

`Base` is the embeddable struct carrying the retained-tree bookkeeping:
arranged bounds (`Bounds() Rect`), the `Layout` properties
(`LayoutProps() *Layout`), and the attachment list (`Attach`/
`Attachments`, used by `KeyBinding` — see the input section).
Third-party widgets embed `Base` and get all of it.

Every visual property on the built-in widgets is a `*prop.Property[T]`:
`Text.Content`, `Text.Style`, `Border.Title`, `Border.Style`,
`Button.Content`. There is no second kind of property for literals —
`gooey.Str("hello")` and `gooey.Sty(style)` wrap literals as source
properties, so a widget field is the same thing whether it came from a
literal, a viewmodel source, or a markup binding. (The one confessed
exception: `Image.Src` and `Image.Cols/Rows` are plain fields — the
pixel pipeline predates the property model.)

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

`Compose` in `widget.go` is the one-shot path: fresh buffer, full
layout, full render walk. The interesting path is `Composer` in
`composer.go` — the retained, damage-tracked renderer, and the place
where the property graph and the component model fuse.

### Every widget's paint is a graph node

`NewComposer` walks the tree once and builds a `paintNode` per widget:

```go
n.node = prop.NewComputed(func() int {
    n.rev.Get()
    if _, isContainer := w.(Container); !isContainer {
        if b, ok := w.(Bounded); ok {
            clearRect(c.frame.Cells, b.Bounds())
        }
    }
    if paintable(w) {
        w.Render(c.frame)
    }
    c.painted++
    return c.painted
})
```

Evaluating the computed *is* painting the widget. Because `Render` runs
inside an evaluation context, every property the widget reads while
painting — `Content`, `Style`, `IsFocused()`, `IsHovered()`, a bound
viewmodel computed — is recorded as a dependency of that widget's paint
node, automatically. This is the payoff line from the package comment:
**"AffectsRender" metadata is discovered, not declared.** WPF makes you
annotate each dependency property with `FrameworkPropertyMetadata
(AffectsRender)`; gooey observes what the paint actually read, and
because computeds re-record on every evaluation, the metadata is always
exactly current — even through conditional reads.

The consequence is minimal damage with zero bookkeeping in widgets: a
`Set` on any property dirties precisely the paint nodes that read it,
and `Composer.Frame` re-evaluates only dirty nodes into the persistent
buffer. `composer_test.go` pins this down (change one of three texts,
exactly one node repaints), and `cmd/propdemo` shows it live (a tick
repainting 2 of 8 widgets).

### Damage semantics: pre-clear leaves, never containers

Before a dirty widget repaints, its bounds are cleared — but only if it
is *not* a container:

```go
// Pre-clear only leaves: a container's bounds enclose its
// children's cells, and wiping those would blank content whose
// own (clean) nodes won't repaint. Containers overpaint their
// own chrome in place instead.
```

This is the subtle center of the damage model. A `Border`'s bounds
enclose its child. If a title change cleared the whole border rect, the
child's cells would be blanked — and the child's paint node, being
clean, would never repaint them. So containers overpaint their chrome
(box characters, title) in place, and only leaves get the clear-then-
paint treatment.

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

Bounds changes are reconciled after layout: each `paintNode` remembers
its widget's last bounds, and if arrange moved the widget, the vacated
region is cleared and the node's `rev` source is bumped — `rev.Get()` is
the first line of every paint closure, so bumping it force-dirties the
paint node. That is how a moved-but-content-unchanged widget still
repaints at its new position.

### Stated POC limits

From the type comment, verbatim: static tree (rebuild the Composer on
structural change — this is what markup hot reload does via `swap`), and
cell-plane widgets only (the Composer path does not yet carry graphics
placements; `Compose` does).

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
through the widget tree; `input` is the one package both import, so the
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
unmapped, skip it". `term.DecodeEvents` adds the only two things that
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

A widget becomes a focus stop by embedding `FocusState`, which
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
so moving focus repaints exactly the widget that lost it and the one
that gained it — `input_test.go` asserts 2-of-4. No focus-changed
event, no invalidate call, no widget code beyond reading the flag while
painting.

The `FocusManager` (built by `NewFocusManager` from the same tree walk
the Composer does, owned via `Composer.Focus()`) holds the focus order
(tree order, filtered to focus stops), the parent map, and the
`KeyBinding` lists per widget. `FocusNext`/`FocusPrev` move in tree
order with wrapping, skipping anything inside a `Collapsed` subtree.

### Routed dispatch

`FocusManager.Dispatch` is WPF-style bubbling in thirty lines:

```go
for n := start; n != nil; n = m.parent[n] {
    for _, b := range m.bindings[n] {
        if b.Gesture == ev && b.Command != nil {
            b.Command()
            return true
        }
    }
    if h, ok := n.(KeyHandler); ok && h.HandleKey(ev) {
        return true
    }
}
```

The event starts at the focused widget and walks up the ancestor chain;
at each level, `KeyBinding`s attached there match first, then the
widget's own `HandleKey`. The first `true` stops propagation. Tab,
shift-tab, and the arrow keys navigate focus only in the *unconsumed
tail* of that walk — which means any of them is overridable by simply
handling it, and is what lets a list pane keep its own arrow handling
while buttons and checkboxes let arrows fall through to navigation.
Arrow navigation is spatial (XAML's XYFocus): the nearest focus stop
whose center lies in that direction, preferring stops in line with the
current one, falling back to tree order so a direction is never a dead
end.

`KeyBinding` is non-visual: it implements the `NonVisual` marker and
hangs off its parent as an *attachment* (`Base.Attach`), walked for
input but never measured, arranged, or painted. Attachment position is
what scopes it: because dispatch only visits bindings on the focused
widget's ancestor chain, a binding declared inside a control fires only
while that control's subtree has focus, and one on the page root is
global. `cmd/reader` uses this: its Enter binding lives in
`storylist.gooey`, so Enter opens a story only when the story list has
focus — the same key does nothing from the reader pane, with no `if`
anywhere.

`Command` is just `func()`. Markup event attributes resolve to one —
either a delegate from the binding context (`Click="{{.Save}}"`, the
func living in the viewmodel) or a code-behind handler by bare name
(`Click="OnSave"`, from `Context.Handlers`).

### The pointer: hit-testing, hover, capture, click synthesis

Mouse events route the same way keys do — one target, then its
ancestors — but the target comes from hit-testing instead of focus.
`FocusManager.HitTest` returns the deepest widget whose arranged
`Bounds()` contain the cell, children before ancestors and later
siblings before earlier ones (they paint on top); `Collapsed` subtrees
and zero-size widgets are not hit. The walk allocates nothing, because
it runs on every motion report.

`DispatchMouse` runs two framework behaviors before the app sees
anything:

- **Focus-follows-click**: a press moves focus to the nearest focusable
  widget at or above the hit — or, when there is none, the first
  focusable *below* it, so clicking a pane's border or title focuses
  the pane rather than doing nothing.
- **Hover tracking**: `setHover` moves the hover flag to the nearest
  `HoverTarget` at or above the hit — so hover composes upward; a
  `Border` can highlight while the pointer is over the `Text` inside it.
  `HoverState` is the exact twin of `FocusState`: the flag is a source
  property, `IsHovered()` read during `Render` is a paint dependency,
  and crossing between widgets repaints the one entered and the one
  left, nothing else.

Raw motion is deliberately not delivered to widgets — any-motion
tracking is high-frequency — except to widgets that opt in via
`MouseMoveHandler` (drag, resize). Everyone else sees enter/leave
through hover.

Press and release are delivered as they arrive, with **implicit
capture**: the release is routed to the widget the press went down on
(`m.pressed`), even if the pointer wandered off, so pressed-state
visuals can always be undone. When press and release land on the same
widget, the dispatcher synthesizes a `MouseClick` — `MouseClick` is not
a terminal report; it exists only as this synthesis. Wheel events go to
the widget under the pointer, not the focused one, per terminal
convention. `Button` exercises all of it: focused, hovered, and pressed
are three property reads in its `Render`, so each state change repaints
just the button.

## Markup

The `markup` package is the XAML-analog authoring surface: XML elements
map to widgets, attributes to properties, `{{...}}` expressions
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
  the page and the files it includes. It reports only THAT something
  changed; `gooey.App` does the rebuild on the UI goroutine, because
  resolving bindings touches the property graph. Parse errors leave the
  current tree in place, so a bad edit never blanks the running app.
  Rebuilding the tree means rebuilding the `Composer` (static-tree
  limit), while viewmodel properties live outside the tree and survive —
  that is why hot reload keeps your state.
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
resolve against), `Styles`, `Widgets` (custom builders), `Handlers`
(code-behind commands), `Named` (widgets collected by `Name="..."`, read
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

### UserControl: context isolation and the attribute hand-off

`markup.UserControl(fsys, "storylist.gooey", setup)` wraps a markup file
plus a code-behind setup as a `Builder`, registered like any custom
widget and instantiated as an element: `<StoryList
Stories="{{.Stories}}"/>`.

The contract is **context isolation**: `setup` returns the instance's
*own* `Context`, and bindings inside the control's markup resolve
against it — never against the page. Data crosses the boundary through
element attributes, resolved in the *parent* context via
`Context.BindingValue`, which returns the raw context value — typically
a `*prop.Property[T]` handle — that setup wires into its own context or
widgets. This is XAML's DataContext-plus-dependency-property hand-off,
done with explicit handles instead of an ambient inherited value.
`Styles`, `Widgets`, `Handlers`, and `Includes` inherit from the parent
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
component with a real property surface — implicit and unchecked in the
POC, which is precisely the gap the x:Property spec closes.

`cmd/reader` is the working proof of the whole markup layer: a
`Grid Cols="24,1*,2*"` shell in `reader.gooey`, three UserControls with
their own contexts, bindable focus-aware pane titles, and
`<KeyBinding>`s scoped by where they are declared. Its design record is
[specs/2026-08-10-reader-design.md](specs/2026-08-10-reader-design.md),
and the running app is in [demos.md](demos.md).

## Designed, not built

Two specs extend this architecture on paper. Neither is implemented;
both are worth reading because they show where the design pressure
points are.

**Markup-declared properties** —
[specs/2026-08-10-markup-declared-properties.md](specs/2026-08-10-markup-declared-properties.md).
A `.gooey` file will declare its property surface with
`<x:Property Name="Title" Type="string" Default="untitled"/>` under the
root. Each declaration materializes the same artifact code-behind wires
today — a `*prop.Property[T]`: a bound attribute passes the parent's
handle through (today's Include behavior, now type-checked), an absent
one materializes a per-instance source with the declared default, and
`Required` makes absence a load error. One property system throughout —
the markup tier of registration, exactly as `DependencyProperty
.Register` is WPF's code tier. The spec deliberately excludes
markup-declarable *attached* properties and records the merge semantics
with code-behind (declarations first, setup extends, collision is a
load error).

**Remote handlers and Temporal** —
[specs/2026-08-10-remote-handlers-design.md](specs/2026-08-10-remote-handlers-design.md).
xmlns returns as a capability system: prefixed namespaces map to
`HandlerProvider`s the host app registers, and the binding DSL grows an
extension form — `Click="{{net:Get .Url | into .Body}}"`, or
``Click="{{temporal:Activity `RebuildIndex` .Query | into .Results}}"``
— that produces a `Command`. Args keep lvalue semantics (`.Path` is a
handle read at invoke time); `| into` Sets a handle with the result,
marshaled back to the UI goroutine through a new `Dispatcher`, since
properties are goroutine-confined. The Temporal provider targets
standalone activities, which is the striking consequence: combined with
fs.FS-served markup, an entire app — layout, bindings, and behavior
wiring — ships as data, with workers anywhere doing the compute.

Neither spec changes the foundations above; both lean on them. The
declared-properties design works *because* every property is already a
graph node ("the graph is the callback system"); the remote-handler
design works *because* bindings are already handles, not values. That is
the architecture doing its job.

Back to the [README](../README.md) for the short version, or on to
[demos.md](demos.md) to see all of this moving.
