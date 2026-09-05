# Markup reference

The `markup` package is gooey's XAML-analog authoring surface: XML elements map to components, attributes to properties, and `{{...}}` expressions to bindings resolved against a property registry — no reflection anywhere. This page is the complete reference for the `.gooey` file format as implemented today. For how the component/property machinery underneath works, see [architecture.md](architecture.md); for a first working app, see [getting-started.md](getting-started.md); the demo apps in `cmd/` are the living examples and are cataloged in [demos.md](demos.md).

A markup file is loaded against a `markup.Context` — the binding environment that supplies values, styles, custom component builders, event handlers, and the include filesystem:

```go
ctx := &markup.Context{
    Values:     map[string]any{...},              // {{.Name}} roots
    Styles:     map[string]render.Style{...},     // Style="name" lookup (outermost scope)
    Components: map[string]markup.Builder{...},   // custom elements, opaque
    Elements:   map[string]*markup.ElementDef{...}, // custom elements that DECLARE their surface
    Handlers:   map[string]gooey.Action{...},     // bare-name event handlers
    Includes:   fsys,                             // convention-based controls
}
tree, err := markup.Load(fsys, "app.gooey", ctx)
```

`Load` reads from any `fs.FS` — `os.DirFS` in development, `embed.FS` in release; the loader cannot tell the difference. `markup.Watch` (single file) and `markup.WatchAll` (a set of files) poll ModTimes ([#53](https://github.com/WonderForgeLabs/gooey/issues/53) replaces the polling with filesystem notifications) and rebuild on change, which is the hot-reload path: edit the file while the app runs and the tree rebuilds in place, with all state intact because the viewmodel properties are the durable thing and the tree is disposable (see `cmd/markuplog`, [media/demos/markuplog.gif](media/demos/markuplog.gif)) — focus is the one thing a reload still drops, which is [#52](https://github.com/WonderForgeLabs/gooey/issues/52). On an immutable FS this degrades to a natural no-op. Parse or build errors during a reload leave the current tree in place.

Apps built on `gooey.App` do not call `Load` and `Watch` themselves — they hand the whole arrangement to `markup.Page`:

```go
app := gooey.NewApp(markup.Page(fsys, "dashboard.gooey", ctx, "card.gooey", "badge.gooey"))
```

`Page` (in `markup/page.go`) packages Load plus WatchAll as a `gooey.Content`: the App builds the tree from it at startup and rebuilds **on the UI goroutine** whenever a named file changes — the watcher only reports; it never builds, so binding resolution never touches the property graph from a foreign goroutine, which is the hazard the raw `Watch` callback leaves to you. The extra names are the control files a rebuild depends on, since watching cannot infer what an `<Include>` will resolve to. The direct `Load`/`Watch` calls remain the right surface for hand-rolled loops and tests.

## The root element

Every file has exactly one `<Gooey>` root with exactly one child:

```xml
<Gooey xmlns="wonderforge.io/gooey/2026">
  <Border Title="finder" Style="panel">
    ...
  </Border>
</Gooey>
```

Both rules are enforced at build time. The default `xmlns` attribute is decorative versioning — the parser ignores its value. **Prefixed** namespaces are not decorative: they declare handler namespaces and gooey's language-services namespace, and are captured per document into a prefix → URI table (see [handler namespaces](#handler-namespaces) and [declared properties](#declared-properties-xproperty)).

The "exactly one child" rule counts *visual* children. `<x:Property>` declarations are also direct children of the root, and are not content.

### `<Gooey>` attributes

`<Gooey>` accepts one attribute of its own, and anything else on the root is a **load error** rather than a silent no-op — the same rule every other element follows.

| attribute | values | meaning |
|---|---|---|
| `Graphics` | `kitty`, `sixel`, `iterm2`, `halfblock` | Force the pixel protocol. Omit it — the default — to let the terminal's capabilities decide. |

```xml
<Gooey xmlns="wonderforge.io/gooey/2026" Graphics="sixel">
```

An unrecognised value fails at load rather than falling back, because falling back quietly is how a page ends up rendering as coloured blocks with no explanation of why.

The setting lives in the **document** because it is a property of the artwork the page carries, not of the machine it runs on. A page built around a detailed SVG wants real pixels wherever it goes, while capability detection answers for whoever launched the process — which is the wrong terminal whenever the app was started from a script, a recording pty, or a supervisor.

Hosts read it with `markup.ReadPageSettings`, which parses the root and no further: it builds nothing and binds nothing, so the answer is available before there is a component tree to ask.

### Per-protocol files: `Context.Variant`

The same axis from the other side. `Graphics` forces a protocol from inside one document; `Variant` picks a *different document* per protocol. Set `ctx.Variant` to the resolved encoder name — `kitty`, `sixel`, `iterm2`, or `cells` where there is no pixel plane — and `Load(fsys, "page.gooey", ctx)` resolves to `page.sixel.gooey` when that sibling exists and to `page.gooey` when it does not. The suffix goes before the extension so the files sort together. UserControls and Includes specialize the same way: ship `card.sixel.gooey` beside `card.gooey` and the instantiation site is unchanged.

Set it **after** capability detection (`App.Graphics().Name()`) — before the probe answers, the honest name is unknown and the base document is right. Empty, the default, disables the lookup entirely. A missing variant is the ordinary case, not an error, which is what makes the axis cheap to adopt one page at a time. `markup.Page` watches every variant name including ones not yet written, so *creating* `page.kitty.gooey` hot-reloads. (The `Variant` doc comment's own list of compliant files keeps going stale — [#260](https://github.com/WonderForgeLabs/gooey/issues/260).)

## Built-in elements

### Border

Draws a rounded box around exactly one visual child (KeyBindings do not count against the one-child rule).

| Attribute | Meaning |
|---|---|
| `Title` | Text in the top edge. Bindable: `Title="{{.Title}}"` or a literal. |
| `Style` | Named style from `Context.Styles`, applied to the frame and title. |
| `Background` | Fill color for the whole box: a `#rgb`/`#rrggbb` literal or a binding to a `*prop.Property[render.Color]`. Chrome drawn with a style whose background is unset sits on the fill. |

```xml
<Border Title="{{.Title}}" Style="panel">
  <ArticleBody Margin="1,0"/>
</Border>
```

(from `cmd/reader/readerpane.gooey`)

### Frozen

Wraps exactly one visual child in a region that **renders but does not act**. The picture is live; the behaviour is not. Descendants lay out, paint and keep their own paint nodes — damage granularity is untouched — and they are simply never the target of anything.

| Attribute | Meaning |
|---|---|
| `Active` | **Bind-only**: `Active="{{.DesignMode}}"`. Omitted means always frozen. A literal is a load error — a constant `false` is a `<Frozen>` that should be deleted rather than written. |
| `Allow` | The interaction categories that still act inside, as names separated by spaces or commas. Omitted means `None`. Literal or bound. |
| `AllowError` | **Bind-only, and a WRITE target**: `AllowError="{{.FreezeErr}}"` names a `*prop.Property[string]` the framework Sets with a bound `Allow`'s parse failure, or `""` when it parses. Requires a **bound** `Allow` — with an absent or literal one the parse either does not happen or already happened at load, so the channel could never carry anything — and requires `Context.Dispatcher`. Both are load errors. |

```xml
<Frozen Active="{{.DesignMode}}" Allow="Hover Mnemonics">
  <VStack>…the document being edited…</VStack>
</Frozen>
```

#### The `Allow` vocabulary

Categories compose by **union**: naming more permits strictly more. Nesting **intersects** — a `<Frozen>` inside a stricter one cannot hand out permission its container withheld.

| Category | What still acts |
|---|---|
| `Focus` | Descendants are focus stops again: tab, shift+tab, arrow navigation and `SetFocus` reach them |
| `Alpha` `Numeric` `Punct` `Space` | Which unmodified printable keys reach a focused descendant |
| `Nav` | tab, arrows, home/end, page up/down |
| `Edit` | enter, backspace, delete |
| `Escape` | esc |
| `Chords` | anything held with ctrl or alt |
| `Bindings` | scoped `<KeyBinding>`s attached inside are REGISTERED — **combine with the class of the key they bind**, see below |
| `Mnemonics` | mnemonics declared inside fire |
| `Pointer` | press, release, click, motion, capture and the wheel reach descendants |
| `Hover` | hover state and `HoverWatcher`s track descendants |
| `Start` | `Startable`s inside are started |

Groups, spelled the same way: `Text` (= `Alpha Numeric Punct Space`), `Keys` (every key class), `Mouse` (= `Pointer Hover`), `All`, `None`.

Two rules are built into the **constants** rather than applied by a pass, so no order of composition can defeat them:

- **every key class, and `Bindings`, carries `Focus`.** A key that reaches nothing is not an allowance: with no focus stops, nothing inside can be focused and no key routes there. Writing `Allow="Alpha"` therefore grants focus too.
- **`Mnemonics`, `Pointer`, `Hover` and `Start` do not.** None of the four is routed through focus — a mnemonic is offered to every handler in the tree regardless of what holds focus — so each is reachable inside a subtree that is otherwise completely sealed.

**`Bindings` on its own admits nothing, and this is the one composition that catches people.** It decides whether a scoped `<KeyBinding>` inside is registered; it does not decide whether a keystroke can travel to it. Dispatch begins at the outermost ancestor that withholds the pressed key's class, so a `<Frozen Allow="Bindings">` withholds every class, the walk starts *at* the `<Frozen>`, and a binding attached below it is never visited — registered, correct, and unreachable. Grant the class too:

```xml
<!-- ctrl+s inside a read-only preview -->
<Frozen Allow="Bindings Chords">
  <KeyBinding Gesture="ctrl+s" Command="{{.Save}}"/>
  ...
</Frozen>
```

`TestAllowBindingsAloneFiresNothing` in `markup/frozenallow_test.go` pins it.

**A bound `Allow` that fails to parse fails CLOSED, and `AllowError=` is how a page hears about it.** A literal `Allow` is checked at load time, so a typo in the markup is a load error naming the attribute. An interpolated one cannot be — its value does not exist yet — so `components.Frozen` parses it at runtime, answers `None` on failure, and records why. Without a channel the only symptom was a subtree that had silently stopped responding, which is [#424](https://github.com/WonderForgeLabs/gooey/issues/424). Bind a property and render it:

```xml
<Frozen Allow="{{.Categories}}" AllowError="{{.FreezeErr}}">
  <VStack> … the document being edited … </VStack>
</Frozen>
<Text Text="{{.FreezeErr}}"/>
```

It is published from an observer rather than read per frame, which is why it needs `Context.Dispatcher`: the parse failure surfaces during an invalidation, and Setting from inside one would mutate the graph mid-invalidation. The Set is posted and lands on the next drain.

`Start` is the one category nothing implies. `Companion.Start` spawns a child process, so a grant that turned starting on as a side effect of wanting hover would launch a subprocess from an editing gesture; it must always be asked for by name.

`Allow="All"` and no `<Frozen>` at all are the same thing, deliberately: "not frozen" is a member of the same lattice (`gooey.AllowAll`), which is what keeps one observed value per component instead of two that can disagree.

#### Errors

A **literal** `Allow` is checked at load time — `<Frozen Allow="Clicks">` fails to load, naming the vocabulary. A **bound** one cannot be, so it fails *closed*: an unparseable value becomes `None`, the strictest answer, and `components.Frozen.AllowError()` reports why.

#### Changing the set at runtime

`Composer.armFrozen` wraps the answer in a computed whose evaluation **calls** `Frozen()` and `FrozenAllow()`, so whatever those read becomes a dependency by the ordinary call-site rule. A `Set` schedules a frame, the per-frame sweep compares this frame's `gooey.Allow` against last frame's, and **any** change — not only the frozen/not flip — re-syncs the composition in the same frame, before anything paints. Freezing costs no repaint of its own.

Compose the set in markup with [`handlers/sets`](../handlers/sets/README.md):

```xml
<Frozen Allow="{{sets:Concat `Hover` .Selected}}">
```

### Grid

The workhorse layout panel: children go into cells addressed by the attached `Grid.Row` / `Grid.Col` / `Grid.RowSpan` / `Grid.ColSpan` attributes on the children themselves.

| Attribute | Meaning |
|---|---|
| `Rows` | Comma-separated row definitions (see below). |
| `Cols` | Comma-separated column definitions. |
| `Background` | Fill color: `#rgb`/`#rrggbb` literal or a color-property binding. |

Each definition is one of:

- `Auto` — size to content (case-insensitive). An Auto track sizes to the max desired size of its span-1 children.
- `N` — a fixed integer number of cells, e.g. `26`.
- `w*` — a star track taking a weighted share of the space left after fixed and Auto tracks, e.g. `2*`. Bare `*` means `1*`.

A missing `Rows` or `Cols` attribute defaults to a single star track. Star space is distributed by weight in the Arrange pass; rounding leftovers go to the last star track.

```xml
<Grid Rows="Auto,Auto,*" Cols="3*,2*">
  <Input   Grid.Row="0" Grid.ColSpan="2"/>
  <Text    Grid.Row="1" Grid.ColSpan="2" Style="dim">{{.Status}}</Text>
  <Results Grid.Row="2" Grid.Col="0"/>
  <Preview Grid.Row="2" Grid.Col="1" Margin="2,0,0,0"/>
</Grid>
```

(from `cmd/finder/finder.gooey`)

Grids nest — `cmd/reader/reader.gooey` puts a `Cols="26,1*,2*"` grid inside row 0 of a `Rows="*,Auto"` grid. Row/col indexes and spans are clamped to the defined tracks; a span of 0 means 1.

### VStack and HStack

Sequential stacks: VStack lays children top to bottom at their desired heights, HStack left to right at their desired widths.

| Attribute | Meaning |
|---|---|
| `Gap` | Cells of space between consecutive children. Defaults to 0. |
| `Background` | Fill color: `#rgb`/`#rrggbb` literal or a color-property binding. The fill covers the gap cells no child owns. |

```xml
<HStack Grid.Row="1" Gap="2">
  <Button Content="count +1" Click="{{.Increment}}"/>
  <Button Content="cycle message" Click="{{.Cycle}}"/>
  <Button Content="serialize → json" Click="{{.Serialize}}"/>
</HStack>
```

(from `cmd/state/state.gooey`)

### Text

A text block. The content is the element's text — written between the tags, not in an attribute. Content may be a pure literal, a pure binding, or a mix (see the binding DSL below).

**Whitespace in a body is significant on one line and trimmed across lines.** Written on a single line the body is taken exactly as typed, so `<Text>    Hello</Text>` renders four leading spaces and `<Text> </Text>` is a one-cell spacer. Wrapped across lines, the surrounding whitespace is source formatting and is stripped:

```xml
<Text>    indented four</Text>   <!-- renders "    indented four" -->
<Text>
  wrapped for readability        <!-- renders "wrapped for readability" -->
</Text>
```

Indenting the document does *not* indent a body — the file's indentation lands before the start tag, so a `<Text>` nested ten levels deep still renders `Hello` for `<Text>Hello</Text>`. That is what lets the one-line form be verbatim without an opt-in attribute. The one thing you cannot express is leading whitespace on a body wrapped across *several* lines; write one `<Text>` per line, or use `Canvas.Left`.

| Attribute | Meaning |
|---|---|
| `Style` | Named style from `Context.Styles`. |
| `Bold` | `"true"` sets bold on top of whatever the named style says. |

```xml
<Text Grid.Row="1" Style="dim">space: pause/follow   f: filter   q: quit</Text>
<Text Grid.Row="0" Style="accent">{{.Header}}</Text>
```

(from `cmd/markuplog/logview.gooey`)

Multi-line output comes from newlines in the bound value, not from markup structure.

### Button

The interactive focus stop: renders as `[ label ]` and runs its command on enter, space, or a mouse click. Focus, hover, and pressed states each restyle it.

| Attribute | Meaning |
|---|---|
| `Content` | The label. Bindable or literal. |
| `Click` | The command — a binding (`Click="{{.Save}}"`) or a bare handler name (`Click="OnSave"`); see event bindings below. Empty means no command. |
| `Style` | Named style from `Context.Styles`. |
| `Chrome` | `"cell"` (default) or `"pixel"`. Anything else is a load error. See [pixel chrome](#pixel-chrome) below. |

```xml
<Button Content="serialize → json" Click="{{.Serialize}}"/>
```

`Content` takes the same **mnemonic** marker as a `<Menu Title>`: `Content="_Save"` underlines the S and makes `alt+s` press the button from anywhere on the page, whatever holds focus. The marker is syntax, not text — it is stripped from the label and from every measure and render tier, so `Content="snake_case"` renders `snakecase` and quietly claims `alt+c`; `__` is the literal underscore. Unlike a menu there is **no** first-letter fallback: a button with no marker registers no accelerator. A disabled command declines the accelerator and the key keeps going. See [the MenuBar mnemonic rules](#menubar) for the shared convention.

A command with a `CanExecute` condition (`gooey.NewCommand(save).When(dirty)`) needs nothing extra in markup — the binding resolves it like any delegate. The button then asks the condition **while painting**, so it paints dim and refuses enter, space and clicks while the condition is false, and a flip repaints exactly that one button. See [conditional commands](#conditional-commands).

#### Pixel chrome

`Chrome="pixel"` makes the button a three-row rounded pill instead of a one-row `[ label ]`. Everything else about it is unchanged: same `Click`, same conditional-command rule, same focus, hover and press states, same keys.

```xml
<Button Content="Deploy" Chrome="pixel" Click="{{.Deploy}}"/>
```

What draws the pill depends on the terminal, the way `ColorPicker`'s bars do — and, as there, the tiers are not a quality ladder with a "real" version and a degraded one:

- **With a graphics protocol and a known cell size** (kitty, sixel, iTerm2 — so, under `WithCapabilityProbe`, `WithCaps`, or `WithGraphics`), the pill is generated in code at the terminal's exact pixel-per-cell resolution: a rounded rectangle with a vertical gradient, an outline, and a brighter ring while focused. No image files are involved.
- **Everywhere else** — no protocol, or a probe that never answered and so left the cell size at zero — the same pill is drawn in box-drawing runes.

The **footprint is identical on both**, which is the point: a page does not re-flow because the probe happened to find a protocol.

The label always stays on the cell plane. Pixel placements composite *over* the cells, so an image spanning the button would bury its own text; instead the generated pill is sliced into the four rectangles that are not the label — the top edge, the bottom edge, and the two end caps of the middle row — and the label is painted in the window between the caps over a background matching the pill's interior.

Damage works out of the existing rules and needs nothing new. The placements are recorded from `Render`, so the composition files them under this button's paint node: a hover replaces exactly those four images, a neighbour repainting sends none of them, and a button that turns `Hidden` has its images deleted by id (kitty) or the cells it vacated repainted (sixel, iTerm2, which cannot address a placement).

### KeyBinding

A declared gesture — a non-visual element:

```xml
<KeyBinding Gesture="ctrl+c" Command="{{.Quit}}"/>
```

| Attribute | Meaning |
|---|---|
| `Gesture` | Key gesture, parsed by `input.ParseGesture` (syntax below). |
| `Command` | Binding or bare handler name, same resolution as `Click`. A command whose `When` condition is false does not match: the gesture is not consumed and the key keeps bubbling, so an outer binding can still have it. |

Attachment and scoping semantics: a KeyBinding is never laid out or painted. The builder hangs it off its parent element as an attachment (any element that embeds `gooey.Base` can host one — a Grid, Border, stack, or custom component). Key dispatch starts at the focused component and walks up its ancestor chain to the root; at each level the KeyBindings attached there are matched first, then any **behaviour attachments** that handle keys (`<TypeAhead>`), then that component's own key handler. So:

- A binding declared on the page root is effectively global — every focused component's chain passes through the root. The `q`/`esc`/`ctrl+c` bindings in `cmd/reader/reader.gooey` work this way.
- A binding declared inside a control fires only while focus is inside that control. `cmd/reader/storylist.gooey` attaches `<KeyBinding Gesture="enter" Command="{{.Open}}"/>` to the story pane's Border, so enter opens a story only while that pane has focus.
- The first consumer stops propagation, and unconsumed `tab`/`shift+tab` move focus — so either can be overridden by binding or handling it.

Before any of that, the event **tunnels**: every ancestor from the root down to the focused component that implements `gooey.PreviewKeyHandler` is offered the event first, and the first to take it ends the dispatch — no target handling, no bubbling, no bindings. `gooey.PreviewMouseHandler` does the same for pointer events. This is the parent-veto mechanism: a modal scrim swallows what is aimed at the layer underneath without any of those components being consulted. The full order is **tunnel down → target and bubble up (bindings, then attachment handlers, then the component's handler at each level) → app fallbacks**.

Why attachment handlers sit between the two: a KeyBinding maps one gesture to one command, which is the wrong shape for a behaviour that consumes a whole class of keys and keeps state between them. Such an attachment must be offered keys **before** its host — `ItemsView` claims `j` and `k` as movement, so a type-ahead consulted after it could never search for a word beginning with `j` — but **after** the level's bindings, so a gesture the page declared out loud still wins.

### Conditional commands

`gooey.Command` is a plain `func()` and always runs. `gooey.NewCommand(run).When(cond)` adds a `CanExecute` condition that is an ordinary `*prop.Property[bool]`:

```go
canSave := prop.NewComputed(func() bool { return dirty.Get() && name.Get() != "" })
ctx.Values["Save"] = gooey.NewCommand(vm.Save).When(canSave)
```

Nothing subscribes to anything and nothing is invalidated by hand — the graph IS `CanExecuteChanged`. A component that asks `CanExecute()` **while painting** has subscribed to the condition, so a flip repaints exactly that component; one that asks while handling an event has only read it. `Run()` is itself a no-op while the condition is false, so a path that forgets to ask still cannot fire a disabled command.

Both forms satisfy `gooey.Action`, which is what every event field is typed as, so markup and existing `gooey.Command` delegates are unaffected.

### Image

`components.Image` — a cell-region image drawn on the pixel plane, with halfblock fallback. `Src`, `Cols` and `Rows` are ordinary properties, so it damages and repaints like anything else (setting `Src` repaints exactly the Image — pinned by test).

```xml
<Image Src="assets/logo.png" Cols="20" Rows="10"/>
<Image Src="{{.Chart}}" Cols="{{.ChartCols}}" Rows="12"/>
```

| Attribute | Meaning |
|---|---|
| `Src` | Required. A literal is a **file path resolved in the same `fs.FS` the page was loaded from** (`markup.Load`'s FS; inside a UserControl or markup-only control, the control's own FS) and decoded at build time — a missing or undecodable file is a load error naming the path and format, wrapping `*imaging.Error`. A binding shares the viewmodel's `*prop.Property[image.Image]` handle. |
| `Cols`, `Rows` | Required size in cells: a positive int literal, or a binding to an int property. |

Literal `Src` decodes through the `imaging` registry: **png, jpeg, gif, bmp, ico** in core (GIF shows its first frame — animation is a player's job, see the browser demo's gifplay, and an animated player is [#105](https://github.com/WonderForgeLabs/gooey/issues/105); ICO decodes its largest entry). **SVG** needs the nested module — blank-import `github.com/WonderForgeLabs/gooey/imagefmt/svg` and `.svg` paths rasterize at their intrinsic size (capped at 1024 px). Formats are sniffed by content, not extension.

Because the decode happens in the builder, hot reload re-reads the file: editing the page (or the control file naming the image) rebuilds and re-decodes. The watcher stats markup files only, so swapping the image bytes alone does not trigger a rebuild — touch the page.

From Go, `components.LoadImg(fsys, path)` is the same load returning a ready `*prop.Property[image.Image]`. See [how to draw images](learn/howto/howto-images.md).

### Canvas

`components.Canvas` — absolute positioning. Children go wherever their `Canvas.Left`/`Canvas.Top` attached properties say, at their own desired size. Its one attribute of its own is `Background`, a fill color (`#rgb`/`#rrggbb` literal or a color-property binding).

```xml
<Canvas>
  <ColorPicker Value="{{.Accent}}" Canvas.Left="1" Canvas.Top="1"/>
  <Text Canvas.Left="46" Canvas.Top="0" Style="dim">a caption, placed exactly</Text>
</Canvas>
```

A child is measured against the space remaining from its offset, so one placed near the right edge clips its own content rather than overhanging. Children may overlap; paint order is tree order, so a later sibling paints over an earlier one.

Overlap is safe under damage tracking: the Composer's z-ordered repaint means that when an *occluded* component repaints on its own, every later (higher) sibling whose bounds intersect it repaints in the same frame, so the stack always ends up back in tree order. The honest cost is that overlapping children repaint together — deliberate overlap trades damage-count minimality for compositing.

### Checkbox

`components.Checkbox` — a focus stop rendering `[x] label`, toggled by space, enter, or a click.

| Attribute | Meaning |
|---|---|
| `Checked` | **Required binding** to a `*prop.Property[bool]`. Shared with the viewmodel, not copied: Render reads it, the toggle Sets it. |
| `Label` | Text after the box. Bindable or literal. |
| `Style` | Named style or a bound style. |

### Gauge

`components.Gauge` — a labelled 0-100 meter, colored by a shared threshold ramp (green below 50, amber at 50, red at 80).

| Attribute | Meaning |
|---|---|
| `Value` | **Required binding** to a `*prop.Property[int]`, clamped to 0-100 on read. |
| `Label` | Text before the bar. Bindable or literal. |
| `BarWidth` | Preferred width in cells; absent = 34. |
| `Style` | Overrides the threshold ramp entirely when present. |

### Sparkline

`components.Sparkline` — a series of 0-100 values as stacked block rows, most recent on the right, colored per column by the same ramp.

| Attribute | Meaning |
|---|---|
| `Values` | **Required binding** to a `*prop.Property[[]float64]`. |
| `Height` | Rows tall; absent = 1. |
| `BarWidth` | Preferred width in cells; absent = 40. |
| `Style` | Overrides the threshold ramp. |

The series is tail-cropped to the arranged width, so a narrower window shows recent history rather than compressing all of it.

### ProgressBar

`components.ProgressBar` — how far along a task is: a 0-100 meter when that is known, a marching band when it is not.

| Attribute | Meaning |
|---|---|
| `Value` | **Required binding** to a `*prop.Property[int]`, clamped to 0-100 on read. |
| `Indeterminate` | Optional binding to a `*prop.Property[bool]`. While true the bar animates a band instead of showing a number. Absent means the bar can never be indeterminate — and then it starts no goroutine at all. |
| `Label` | Text before the bar. Bindable or literal. |
| `BarWidth` | Preferred width in cells; absent = 34. |
| `Tick` | Animation step, any `time.ParseDuration` string; absent = 80ms. Unparseable or non-positive is a load error. |
| `Thresholds` | `"true"` colors the bar with the shared good/warn/crit ramp. |
| `Style` | Overrides the coloring entirely when present. |

```xml
<ProgressBar Value="{{.Pct}}" Indeterminate="{{.Busy}}" Label="build " BarWidth="34"/>
```

`Thresholds` is off by default, which is the one place this differs from `Gauge`. A gauge shows utilization, where a high number is a warning and the ramp *is* the meaning; progress is the opposite, and painting a 96%-finished job crit-red says the reverse of what happened. Turn it on for the bars where the value really is a fill approaching a limit — a disk, a quota.

The animation follows the [`Timer`](#timer) discipline exactly: the ticker goroutine never touches the graph, it posts a step onto the UI goroutine, and the step reads `Indeterminate` there. So a bar that is currently determinate advances nothing and repaints nothing while its ticker runs. Lifetime belongs to the `Composer`, which starts every animated component it walks and stops them on `Close`.

### Spinner

`components.Spinner` — an activity indicator: one glyph from a cycling set, plus an optional label.

| Attribute | Meaning |
|---|---|
| `Frames` | Frame set by name: `braille` (default), `line`, `arc`, `dot`. An unknown name is a load error. |
| `Interval` | Frame interval, any `time.ParseDuration` string; absent = 100ms. |
| `Label` | Text after the glyph. Bindable or literal. |
| `Enabled` | Optional binding to a `*prop.Property[bool]`. Absent means always spinning. |
| `Style` | Named style or a bound style. |

```xml
<Spinner Frames="braille" Interval="90ms" Label="{{.Stage}}" Enabled="{{.Running}}" Style="accent"/>
```

`Enabled` is read **while painting** as well as at fire time, which is the one place `Spinner` differs from `Timer`: a paused spinner should look paused, so it parks at its first frame, and that read is what makes the flip repaint it. Pausing then costs nothing per tick — the posted step returns without setting anything.

From Go, `Frames` is an ordinary `[]string`, so an app can hand over any cycle it likes; `components.SpinnerBraille`, `SpinnerLine`, `SpinnerArc` and `SpinnerDot` are the built-in sets.

### Toggle

`components.Toggle` — a rocker switch. `Checkbox`'s sibling with switch rendering, bound the same way: `Render` reads the handle and the switch `Set`s it.

| Attribute | Meaning |
|---|---|
| `Checked` | **Required binding** to a `*prop.Property[bool]`. |
| `Label` | Text after the track. Bindable or literal. |
| `Changed` | Optional command, resolved like `Click`. Runs after the position changes. |
| `Style` | Named style or a bound style. |

```xml
<Toggle Checked="{{.Running}}" Label="job running"/>
```

Space, enter and a click on the label flip it. What makes it a *rocker* rather than a checkbox is the arrows: `←` means off and `→` means on, and an arrow that would not change anything **is not consumed**, so it keeps bubbling and moves focus instead — the same rule the framework applies to unclaimed arrows, one level down. A click on the track picks the side it landed on.

`Changed` may carry a `CanExecute` condition, and then it is also the disable switch: a `Toggle` whose condition says no paints dim and refuses every gesture, exactly like a `Button`. A `Toggle` with no `Changed` at all is inert, not disabled — it toggles freely.

### Segmented

`components.Segmented` — the rocker past two positions: a row of mutually exclusive options with one selected.

| Attribute | Meaning |
|---|---|
| `Options` | **Required.** Either a literal pipe-separated list (`Options="Day \| Week \| Month"`, whitespace trimmed) or a binding to a `*prop.Property[[]string]`. |
| `Selected` | **Required binding** to a `*prop.Property[int]`, clamped into range on read. |
| `Changed` | Optional command, run after the selection moves. |
| `Wrap` | `"false"` stops the selection at the ends, restoring `Toggle`'s rocker rule. Absent or `"true"` cycles. Any other spelling is a load error, not a silent false. |
| `Style` | Named style or a bound style. |

```xml
<Segmented Options="Idle | Fetch | Build | Deploy" Selected="{{.StageIndex}}" Changed="{{.StageChanged}}"/>
```

`←`/`→` step the selection and, unlike `Toggle`, **cycle** at the ends by default — `→` at the last segment returns to the first — so an arrow along the strip's own axis is always consumed. The keyboard is not trapped by that: the **cross** axis is never handled (`↑`/`↓` on a horizontal strip, `←`/`→` on a vertical one) and neither is `tab`, so there is always a way out. `Wrap="false"` selects the rocker tier, where an end-of-travel arrow bubbles instead. `home`/`end` jump to the ends, space and enter cycle regardless, and a click selects the segment under the pointer. The same conditional-`Changed` disable rule applies.

### StatusBar

`components.StatusBar` — the bottom row every demo used to hand-roll as a dim `Text` with the spaces counted by hand: three sections, one against each edge and one in the middle.

Each section takes either form, and giving one section both is a load error:

| Form | Meaning |
|---|---|
| `Left` / `Center` / `Right` attribute | Shorthand for "a dim line of text". Bindable or literal — though the exported element catalog still declares all three literal-only, so a property palette built on it will say otherwise ([#314](https://github.com/WonderForgeLabs/gooey/issues/314)). |
| `<StatusBar.Left>` / `.Center` / `.Right` | A property element holding exactly one component — anything at all. |

```xml
<StatusBar Left="{{.Status}}" Center="{{.Clock}}">
  <StatusBar.Right>
    <Text Style="dim">tab: focus   ←/→: move   q: quit</Text>
  </StatusBar.Right>
</StatusBar>
```

The sections being components is the whole promotion: a bar whose right section is a `Spinner` while something loads, or whose centre is a `ProgressBar`, is the same component as one showing three pieces of text. Each section keeps its own paint node, so a clock ticking on the right repaints the right section and leaves the key hints alone.

Layout gives the edges priority — `Left` takes what it asked for, `Right` takes what is left of what it asked for, and `Center` gets the gap between them — so a long status message shortens the middle rather than pushing a key hint off the screen.

It paints nothing of its own, and has no `Background`. A container's bounds enclose its children's cells, so filling the row would wipe sections whose nodes are clean and will not repaint; a bar that should look like a bar gets there by styling its sections. See [container backgrounds](specs/2026-08-10-container-backgrounds.md).

### ButtonBar

`components.ButtonBar` — a toolbar: buttons left to right, optionally all one width, optionally separated by a rule, clipped with an indicator when the bar is narrower than its members.

| Attribute | Meaning |
|---|---|
| `Gap` | Cells between members. A bar with a `Separator` forces at least 3, since the rule needs air either side. |
| `Uniform` | `"true"` gives every member the width of the widest one. |
| `Separator` | The rune drawn between members; absent draws none. |

```xml
<ButtonBar Gap="3" Uniform="true" Separator="│">
  <Button Content="start" Click="{{.Start}}"/>
  <Button Content="abort" Click="{{.Abort}}"/>
  <Button Content="reset all" Click="{{.Reset}}"/>
</ButtonBar>
```

Two things make it more than an `HStack`. Uniform sizing is a measure-pass decision rather than a styling one — the bar measures every member, takes the widest, and hands that width to all of them.

And the bar is a focus **scope**: `←`/`→` move between its members and wrap at the ends instead of walking out into the rest of the page. It reaches focus through `gooey.FocusHost`, an opt-in interface the `FocusManager` hands itself to while walking; arrows arrive by bubbling, because a `Button` does not consume them, so a member that wants an arrow for itself simply takes it first. `↑`/`↓` are left alone and still leave the bar by the ordinary spatial route, and `tab` walks straight through — a focus scope is not a focus trap.

Members that do not fit are **collapsed**, not clipped, and an indicator (`›`) is drawn in the last column. Collapsing is what keeps the keyboard honest: focus traversal skips a collapsed member, so `tab` never lands on a button nobody can see. Widening the bar brings them back.

### Tabs

`components.Tabs` — a header strip over exactly one visible page. The strip is `Segmented` grown a body: same segment geometry, same click targets, with the selection deciding which page's content is on screen. Its arrows follow the **rocker** rule rather than `Segmented`'s default cycling — consumed only when the selection moves.

| Attribute | Meaning |
|---|---|
| `Selected` | Optional binding to a `*prop.Property[int]`, clamped into range on read. Absent, the control keeps its own selection starting at 0. |
| `Changed` | Optional command, run after the selection moves (the property is already `Set`). |
| `Style` | Named style or a bound style for the strip. |

Children are `<Tab>` elements (plus non-visual attachments like `<KeyBinding>`); anything else is a load error. Each `<Tab>` takes a **required** `Header` (literal or bound) and **exactly one** content child:

```xml
<Tabs Selected="{{.Tab}}">
  <Tab Header="mcp">
    <Border Title="mcp" Style="panel"><Text>{{.Help}}</Text></Border>
  </Tab>
  <Tab Header="log">
    <LogPanel Height="12"/>
  </Tab>
</Tabs>
```

Switching is the bindable-Visibility machinery, not a structural rebuild: every page is a permanent child whose `Visibility` the Tabs binds to "selected == me", so a `Set` on `Selected` erases the outgoing page through the composer's sweep, repaints the incoming page and the strip, and touches nothing else. Because the Tabs owns that binding, a `Visibility` attribute on a page root is a load error. Hidden pages are `Collapsed`: out of layout, out of focus order, out of hit-testing.

`Selected` is an **int**, not a header key — the `Segmented`/`ItemsView` precedent, and headers are themselves bindable, so a header string is not a stable identity to key on.

Keyboard: the strip is one focus stop. `←`/`→` move the selection while it is focused and follow the rocker rule (consumed only when the selection moves); `home`/`end` jump. `ctrl+pgup`/`ctrl+pgdn` cycle with wrap from **anywhere inside the Tabs subtree** — they arrive by bubbling, which scopes them like a `KeyBinding` declared on the container. Clicking a header selects it, and the wheel over the strip steps without wrapping. Switching away from a page whose descendant holds focus moves focus to the strip, so the keyboard is never left on something collapsed. The conditional-`Changed` disable rule applies: a false `CanExecute` paints the strip dim and refuses every gesture.

A Tabs sizes to its **active** page (plus one strip row). Pages of different heights make the control grow and shrink on switch; give the pages equal explicit `Height`s when the strip should stay put.

### MenuBar

`components.MenuBar` — the top menu row: titles across one line, and a dropdown overlay below the open title. One flat tier; context menus and submenus are [#104](https://github.com/WonderForgeLabs/gooey/issues/104).

```xml
<MenuBar Grid.Row="0" Style="accent">
  <Menu Title="Job">
    <MenuItem Text="Start" Gesture="ctrl+s" Command="{{.Start}}"/>
    <MenuItem Text="Abort" Gesture="ctrl+x" Command="{{.Abort}}"/>
    <MenuItem Separator="true"/>
    <MenuItem Text="Quit" Gesture="q" Command="{{.Quit}}"/>
  </Menu>
  <Menu Title="Notify">
    <MenuItem Text="Toast the status" Command="{{.Notify}}"/>
  </Menu>
</MenuBar>
```

`<Menu>` and `<MenuItem>` are **data, not components** — like Grid track lists, they declare the bar's contents and never enter the visual tree.

| Element / attribute | Meaning |
|---|---|
| `<Menu Title="…">` | One titled dropdown. A missing `Title` is a load error. The title carries the menu's **mnemonic**: an underscore marks the accelerator letter (`Title="_File"`, `Title="E_xit"`), and without a marker the first letter is it. `__` renders a literal underscore. Underscore rather than `&` because these strings live in XML attributes. |
| `<MenuItem Text="…">` | One entry. Required unless `Separator="true"`. Takes the same mnemonic marker as `Title`; while the menu is open, typing the letter activates the item. |
| `MenuItem Command` | Resolved like `Click` — a binding or a bare handler name. Absent is inert: activating just closes the menu. A conditional command (`Cmd.When`) paints the item `Dim` and refuses activation while its condition says no; the condition is read while painting, so the flip repaints the open dropdown by itself. |
| `MenuItem Gesture` | A **display hint** in the gesture syntax, validated by `input.ParseGesture` at load (a typo is a load error) and shown right-aligned in the canonical spelling. It does not bind the key — declare a `KeyBinding` for that. |
| `MenuItem Separator` | `"true"` draws a rule. |
| `MenuItem Checked` | A bound **read** of a bool handle — source or computed — and bind-only: a literal `Checked="true"` is a load error, because a check that cannot change is a rule, not state. Not two-way, and the difference matters: unlike `Checkbox`/`Toggle`, nothing writes back through this binding. The write path is the item's own `Command`, which is why pointing `Checked` at a `prop.NewComputed` derived from that state is the intended shape — a check nothing can set by accident. Point it at the same state the item's `Command` (and any `KeyBinding` for the same action) writes, so the box and the key have one source between them rather than two that drift. It is read while the dropdown paints, so a flip from anywhere repaints the open dropdown and nothing else. A menu holding **any** checkable item gives *every* item a leading column, so plain items stay aligned instead of stepping one cell left. |
| `Style` | Bar and dropdown style. Named or bound. |

**Declare the `MenuBar` as the LAST child of its container.** Document order is z-order, so being last is what paints the dropdown above the content it drops over; in a `Grid`, `Grid.Row="0"` still places the bar on the top row — element order and layout position are independent, which is exactly what an overlay needs.

The bar is a focus stop. `enter`/`↓`/`space` open the highlighted menu; while open, `←`/`→` switch menus, `↑`/`↓` move the highlight (separators are skipped), `enter` activates, a plain **letter** activates the item wearing it as its accelerator (a disabled match moves the highlight and refuses), `alt+letter` switches to the matching menu, `esc` dismisses, and everything else is swallowed — an open menu is modal, so page gestures cannot fire underneath it. Dismissing **restores focus** to whatever had it when the menu opened: for a mouse-open that is the component focus-follows-click took it from, so clicking a menu while typing in a `TextBox` and pressing `esc` puts the caret back.

**Mnemonics work page-wide.** `alt+letter` opens the matching menu no matter what holds focus — the bar registers itself through the `gooey.MnemonicHandler` seam, which the dispatcher offers only the keys nothing in the focused chain consumed, so a `KeyBinding` on the same `alt+…` gesture still wins. Accelerator letters render **underlined, always** — on the bar and in the open dropdown. Always rather than "while ALT is held" because a terminal cannot see a held modifier (there are no key-up events); the underline is static chrome and costs no repaints. And dismissing after an accelerator open restores focus to whatever had it when the accelerator fired.

While open the bar holds the pointer capture: clicks on items activate, motion tracks the highlight and slides between titles, and a press anywhere else dismisses the menu **without reaching what is underneath**.

### ToastHost

`components.ToastHost` — the notification layer: transient messages stacked in the top-right corner, auto-dismissed by a timer, painted above everything.

```xml
<ToastHost Name="Toasts" Grid.Row="0" Grid.RowSpan="12" Duration="2500ms"/>
```

| Attribute | Meaning |
|---|---|
| `Duration` | Default lifetime for `Show`. Any `time.ParseDuration` string; absent means 3s, negative means sticky. |
| `Style` | Named style applied to the toasts; absent paints reverse-video. |

The host takes no children — toasts are shown from code, through the named element:

```go
toasts, _ := markup.Find[*components.ToastHost](ctx, "Toasts")
toasts.Show("job deployed")          // up for the host's Duration
toasts.ShowFor("stuck?", -1)         // sticky until Dismiss
```

Place it as the **last child of the root**, spanning the page (in a `Grid`, `Grid.RowSpan` across every row) — last in document order is what puts the toasts on top of the z-order. The host itself paints nothing and costs nothing while no toast is up; each toast is an ordinary leaf realized through the same structural re-sync a list uses, so showing one paints one component and dismissing one repaints exactly what it was covering.

Auto-dismiss follows the Timer discipline: the goroutine posts the dismissal to the `Dispatcher` and the UI loop runs it, and `Composer.Close` stops-and-joins so no dismissal can arrive after teardown. A host composed without a dispatcher still shows toasts; they just do not expire on their own.

### AdornmentLayer

`components.AdornmentLayer` — the adorner plane: components positioned against a **target** component's arranged bounds rather than by their own place in layout, painted above the whole page. Tooltips are the first customer; validation markers, focus rings and badges are the same shape.

```xml
<AdornmentLayer Grid.Row="0" Grid.RowSpan="12"/>
```

Declare it as the **last child of the root**, spanning the page — the same hosting rule as `ToastHost` (declare it after the ToastHost and tooltips paint above toasts too). It takes no children and no attributes beyond `Name`: adornments attach themselves at runtime (a `Tooltip` finds the layer on its own), and code adds custom adorners through `Add`/`Remove`. The layer paints nothing, is transparent to the pointer, and re-anchors every adornment each frame, so a moved or resized target drags its adornments along and a target that leaves the tree or turns non-visible takes them down.

**Free adornments are the exception to that last clause.** An adornment implementing `gooey.PointerFollower` is positioned against the **pointer** rather than a component: it has no anchor, so nothing can take it down but its owner, and its `Place` receives the pointer's 1x1 cell. That is what makes drag ghosts, drop indicators, marquee rectangles and crosshairs expressible ([#177](https://github.com/WonderForgeLabs/gooey/issues/177), [spec](specs/2026-08-23-free-adornments.md)). There is no markup element for one — a free adornment is raised and dismissed inside a gesture, from code — and `components.DragGhost` is the shipped example. Pointer motion costs nothing while nothing is following; see [concepts/overlays.md](learn/concepts/overlays.md).

### Tooltip

`components.Tooltip` — hover help on any element, from pure markup. Two forms:

```xml
<Button Content="toast" Click="{{.Notify}}">
  <Tooltip Text="pop the status as a toast" Gesture="ctrl+t"/>
</Button>

<Text Tooltip="just a label">plain</Text>
```

The child form is a **non-visual attachment** like `KeyBinding` — it hangs off the element it describes, never laid out or painted. The `Tooltip="…"` attribute is shorthand for the same thing and works on **any** element (it belongs to the element like the layout attributes, so on a user-control instance it decorates the instance rather than crossing into the control's context). Both need an `AdornmentLayer` on the page to show in.

| Attribute | Meaning |
|---|---|
| `Text` | What the tip says. Literal or bound; bound text stays live while the tip is up. |
| `Delay` | Hover-rest time before showing. Any `time.ParseDuration` string; absent means 600ms. |
| `Gesture` | A **display hint** in the gesture syntax, validated at load and shown dim in the canonical spelling — the `MenuItem` rule. Absent, the tip renders the host's own `KeyBinding` gesture automatically. Display only; wiring the key stays a `KeyBinding`'s job. |
| `Style` | Named style; absent paints reverse-video. |

Resting the pointer on the element for `Delay` shows the tip adjacent to it — below, flipping above when the screen runs out — and it dismisses on hover-out, on any key, and on any press (the key and the press still do their normal job). Only one tip is ever up: crossing to another tooltipped element swaps them, and with nested tooltipped elements the innermost wins. The delay follows the Timer discipline (`Composer.Close` stops-and-joins); a composition without a dispatcher shows tips immediately instead.

### ColorPicker

`components.ColorPicker` — an interactive RGB editor, and the worked example of a component that adapts to the terminal it landed on.

| Attribute | Meaning |
|---|---|
| `Value` | **Required binding** to a `*prop.Property[render.Color]`. |

Keys while focused: `↑`/`↓` (or `k`/`j`) pick a channel, `←`/`→` (or `h`/`l`) adjust it, shift makes the step 16, `home`/`end` saturate. Clicking a bar sets that channel from the click position; the wheel over a bar nudges it. Keys it does not use bubble on, so page gestures still work while it has focus.

What it shows depends on `Frame.Caps.Color`:

| Depth | Bars | Readout |
|---|---|---|
| truecolor | smooth gradients, each cell the color that position would give | `#FFAA3C`, wide swatch |
| 256 | the same gradients, banded by quantization at the flush | `#FFAA3C → xterm 215` |
| 16 | a plain fill meter — a gradient across 16 buckets would be a lie | `#FFAA3C ≈ yellow` |

### TextBox

`components.TextBox` — a single-line editor and a focus stop. It owns printable runes and the editing keys while focused; everything else bubbles, so page gestures still work from inside the field.

| Attribute | Meaning |
|---|---|
| `Text` | **Required binding** to a `*prop.Property[string]`, shared with the viewmodel. |
| `Prompt` | Optional prefix drawn before the text, e.g. `Prompt="&gt; "`. Bindable or literal. |
| `Style` | Style of the edited text. Named or bound. |
| `AccentStyle` | Named style for the prompt and caret. |
| `Changed` | Optional command run after every edit (not after caret moves) — for invalidating something derived. |
| `Error` | Optional **binding** to a `*prop.Property[string]` — the field's validation state, empty meaning valid. Non-empty flips the text into the invalid visual. Owned by a `<Validate>` behavior when one is attached (declaring both is a load error). |
| `InvalidStyle` | Named style replacing the default invalid visual (red + underline). |

Keys:

| Key | Effect |
|---|---|
| printable rune | insert at the caret, replacing the selection if there is one |
| `backspace` / `delete` | remove the selection, or the character on either side of the caret |
| `←` / `→` | move the caret one character, or collapse a selection to that edge |
| `ctrl+←` / `ctrl+→` | move by word — words, punctuation runs and whitespace runs are separate |
| `home` / `end` | jump to either end |
| `shift+` any of the above | extend the selection from its anchor instead of moving |
| `ctrl+x` / `ctrl+c` | cut / copy the selection to the process-local kill buffer |
| `ctrl+v` | paste the kill buffer at the caret |

`ctrl+c` is only consumed when there IS a selection, so the framework quit key still bubbles out of a focused field with nothing selected.

Mouse: a click places the caret, dragging selects (the drag survives leaving the field, because the press captures the pointer), and a double click selects the word under it.

Cut and copy use a kill buffer shared by every TextBox in the process — `components.KillBuffer` / `components.SetKillBuffer`. It is deliberately not the system clipboard. Reaching that means OSC 52, which the framework does have — `Screen.SetClipboard`, used by the wysiwyg editor — but wiring a text field to it is a decision on purpose rather than a side effect of adding cut and paste, and OSC 52 is write-only for reasons `term.ClipboardCaveat` sets out.

The field scrolls horizontally to keep the caret visible in either direction, and the caret and the selection anchor are source properties, so moving the caret repaints only this component.

### Validate

`markup.Validate` — the validation behavior: a non-visual attachment (MAUI's `ValidationBehavior` in the slot `KeyBinding` and `Tooltip` already occupy) that watches its host's bound `Text` source and materializes the same `validate.Field` computed the Go API builds.

```xml
<TextBox Prompt="name: " Text="{{.Name}}">
  <TextBox.Behaviors>
    <Validate Required="true" MinLen="3" Into=".NameErr"/>
  </TextBox.Behaviors>
</TextBox>
<Text Style="err">{{.NameErr}}</Text>
```

The vocabulary is .NET's `DataAnnotations` set. Every rule passes empty input except `Required`, so "optional but well-formed when present" is the default reading; every default message is a lowercase fragment meant to sit under a field.

| Attribute | Type | Answers (annotation) | Default message |
|---|---|---|---|
| `Required` | bool | `[Required]` | `required` |
| `MinLen` / `MaxLen` | int | `[StringLength]`, `[MinLength]`, `[MaxLength]` | `at least N characters` / `at most N characters` / `must be N–M characters` |
| `Pattern` | regex | `[RegularExpression]` | `invalid format` |
| `EmailAddress` | bool | `[EmailAddress]` | `not a valid email address` |
| `Url` | bool | `[Url]` | `not a valid URL` |
| `Phone` | bool | `[Phone]` | `not a valid phone number` |
| `CreditCard` | bool | `[CreditCard]` | `not a valid card number` |
| `Digits` | bool | — (numeric-string guard) | `digits only` |
| `Integer` | bool | — (numeric-string guard) | `must be a whole number` |
| `MinValue` / `MaxValue` | number | `[Range]` over a text field | `must be at least N` / `must be at most N` / `must be between N and M` |
| `Compare` | field path | `[Compare]` | `does not match` |
| `Message` | string | `ErrorMessage` | — (overrides every rule on this behavior) |
| `Into` | name | — | — |

`Into` is the context name the error property publishes under, so later bindings — the inline error `<Text>`, a gate — reach it. The leading dot is optional. Omitted, it derives from the Text binding: `Text="{{.Name}}"` publishes `NameErr`. Publication overwrites an existing key (a hot reload re-registers on every rebuild).

`Compare` names the *other* field — `Compare=".Password"` or `Compare="{{.Password}}"`, both accepted since the attribute names a property rather than carrying a value. The rule reads that property, and the read is what subscribes this field to it: editing the original re-validates the confirmation with no extra wiring.

`Message` is a **field-level** override: every rule on the behavior reports it instead of its own default, which is how a form says "e-mail address, please" once rather than leaking which check tripped. Per-rule wording is a `validate.Field` in Go.

Rules run in a fixed order regardless of attribute order — presence, then length, then shape (`Pattern`, then the annotation formats in the table's order), then value, then agreement, then registered rules in name order — and the first failure is the message shown, so a person fixing the field hears about the most fundamental problem first.

**Where gooey deliberately differs from .NET's implementations.** Its validators are famously permissive; a terminal form gets more from a rule that rejects nonsense than from bug compatibility:

- **`EmailAddress`** requires one `@` *and a dotted domain*. .NET accepts `a@b`; we do not. We are still far looser than RFC 5322 — quoted local parts, comments and address literals are out of scope — and unicode passes, so IDN domains and non-ASCII local parts are accepted rather than silently rejected.
- **`Url`** accepts `http`, `https`, `ftp` like .NET, and additionally **requires a non-empty host**: `url.Parse` will happily hand back an empty hostname for `http://`, and an allow-anything URL rule is worse than none.
- **`Phone`** requires **7–15 digits** in the number (E.164's maximum), excluding any extension. .NET has no digit-count rule at all.
- **`CreditCard`** is Luhn plus a **12–19 digit window**. .NET checks only Luhn, which accepts a bare `0`. Neither is an authorization: no issuer prefixes, no network rules — it is a typo catcher.
- **`Digits`** is ASCII-only on purpose: a field that accepts Devanagari digits and then hands them to `strconv` is a bug waiting to happen.

Markup spells the regex rule **`Pattern`**, not `RegularExpression`: gooey keeps one canonical spelling per concept (one gesture syntax, one `Style` attribute), and an alias would double the vocabulary a reader must recognize to buy nothing. The table above is how you find it from the annotation's name.

The behavior wires the host's `Error` handle automatically (the invalid visual comes for free); one `<Validate>` per element, and a host whose builder does not speak validation (anything but a `TextBox` today) refuses it at load.

**Extending the vocabulary** is a registration, exactly like `Components` and `Handlers` — rule bodies stay in code, pages keep the validation story in markup:

```go
ctx.Rules = map[string]markup.RuleFunc{
    "Email": func(arg string) (validate.Rule[string], error) {
        return validate.Pattern(`^[^@\s]+@[^@\s]+$`, "not an email"), nil
    },
}
```

```xml
<Validate Required="true" Email="true"/>
```

The constructor receives the attribute's literal and may reject it — a typed load error. An attribute that is neither a built-in nor a registered rule is a load error naming both sets. The built-ins cover the DataAnnotations vocabulary; `ctx.Rules` is for **domain** rules beyond it (an internal account-number format, a reserved-name list, a check against a lookup table).

The registration is not *quite* like `Components` and `Handlers` in one respect: `Rules` does not yet cross the control boundary (tracked in [#314](https://github.com/WonderForgeLabs/gooey/issues/314)). A page registering `ctx.Rules["Email"]` and then writing `<Validate Email="true"/>` inside an Include or UserControl gets "unknown rule", listing only the built-ins. Keep custom rules in the page, or have the control's setup copy `Rules` into the context it returns.

### ValidationMarker

`components.ValidationMarker` — the **floating** error display, for layouts with no room for an inline error row (the primary pattern is an ordinary bound `<Text>` under the field). A non-visual attachment whose message shows in the page's `AdornmentLayer`, anchored below its host, flipping above when the screen runs out.

```xml
<TextBox Text="{{.Tag}}" Error="{{.TagErr}}">
  <ValidationMarker/>
</TextBox>
```

| Attribute | Meaning |
|---|---|
| `Error` | Optional binding to the error property. Omitted, the marker adopts its host `TextBox`'s own `Error` handle — the property is named once. |
| `Style` | Named style for the message. Default is white on the error red. |

The marker is persistent: it lives in the layer for as long as it is attached, shows only while the error is non-empty, hides (rather than dropping) when its host goes invisible, and never intercepts the pointer. A page without an `<AdornmentLayer/>` degrades to inline-only display.

### ItemsView

`components.ItemsView` — the data-driven list: an item source, a template, and one realized row per item that fits.

```xml
<ItemsView Items="{{.Rows}}" Selected="{{.Sel}}" Activate="{{.Open}}">
  <ItemsView.ItemTemplate>
    <Grid Rows="1" Cols="1,*,12">
      <Text Grid.Col="0" Style="{{.MarkStyle}}">{{.Mark}}</Text>
      <Text Grid.Col="1">{{.Title}}</Text>
      <Text Grid.Col="2" Style="dim">{{.Published}}</Text>
    </Grid>
  </ItemsView.ItemTemplate>
</ItemsView>
```

| Attribute | Meaning |
|---|---|
| `Items` | **Required.** Binding to a `*prop.Property[components.ItemSource]` — build one with `components.Items` (below). |
| `Selected` | Optional binding to a `*prop.Property[int]`, shared with the viewmodel: the view Sets it on navigation and reads it to scroll and highlight. Absent means the list is not selectable. |
| `SelectionChanged` | Optional command, resolved like `Click`. Runs after the **view** moves the selection — a key, a click, the wheel — not when the viewmodel Sets `Selected` itself, and not when a gesture clamps to the row already selected: it reports change, not intent. `handlers/temporal/cmd/temporalops` binds it to a describe call so the detail pane follows the selection. |
| `Focusable` | `"false"` takes the view out of the tab order. For lists that are display surfaces for a selection some *other* component drives — finder's results pane, whose query line owns the keyboard fzf-style. A click still selects by hit-test. Anything other than `"true"`/`"false"` is a load error. |
| `Activate` | Command run on enter, on a double click, and on a second click of the already-selected row; resolved like `Click`. |

`<ItemsView.ItemTemplate>` is required and takes exactly one child element. The view is a focus stop with the house list keys — `↑`/`↓`/`j`/`k`, `PgUp`/`PgDn`, `Home`/`End`, `enter` — plus wheel, click to select, and a second click to activate. Keys it does not use bubble, so page-level `<KeyBinding>`s still work while the list has focus.

One field has no markup attribute yet: `Scroll` (Go only) turns a list with **no** `Selected` binding into a tail-anchored scroll view — the log-pane shape, where 0 pins the window to the end and scrolling up moves into history that stays put while new items arrive. A Go-composed view sets the field directly; `apps/kanban` registers such a view as a custom element for exactly this reason.

**The template is a factory, not a tree.** Its element subtree is captured at load and instantiated once per item, against a context whose values are *that item's* — dot is the ITEM. Page values are deliberately out of reach inside a template, the same isolation a UserControl gets; anything a row needs must come through the projection. Everything else the document carries — styles, registered components, handlers, includes, the `xmlns` table — is inherited, so a template may place a registered custom component exactly like any other markup.

**Items come from a projection.** Without reflection, gooey cannot walk a struct's fields, so the app says what a row is made of:

```go
"Rows": components.Items(stories, func(s Story) map[string]any {
    return map[string]any{"Title": s.Title, "Published": s.Published}
}),
```

The map's keys are what the template's bindings resolve against; its values become property handles the view Sets as the item changes. `string`, `bool`, `int`, `float64`, `render.Style`, `render.Color`, `[]int` and `image.Image` become live handles; anything else crosses as a fixed literal for the life of the row (useful for a `gooey.Command`, not for anything that changes).

A picture may be projected either way, and both follow the record when rows are reused — the reuse bug behind that rule is [#217](https://github.com/WonderForgeLabs/gooey/issues/217), fixed in [PR #274](https://github.com/WonderForgeLabs/gooey/pull/274). Project the `image.Image` itself for a picture the record already holds; project a `*prop.Property[image.Image]` — `components.Img(...)`, or a handle the app keeps — when the app wants to fill it in **after** the row exists, as an async thumbnail does. A handle the app owns and Sets later reaches the row without the collection re-projecting at all.

Use `components.ItemsOf` instead when the projection reads more than the item — a lookup table, a filter, a formatting mode. A projection runs during layout, where reads record nothing, so those reads have to happen in your own computed to become dependencies:

```go
rows := prop.NewComputed(func() components.ItemSource {
    marks := read.Get() // recorded: this source depends on it
    return components.ItemsOf(stories.Get(), func(s Story) map[string]any {
        return map[string]any{"Title": s.Title, "Seen": marks[s.Link]}
    })
})
```

**Selection visual.** The selected row's cells are re-styled `Reverse` by the view. A template that mentions the reserved value `_selected` takes that over and gets no house highlight. Two reserved row values are always in the context: `_selected` and `_hovered`, both `*prop.Property[bool]`.

**Rows are windowed and reused.** Only the rows that fit are built, keyed by item index; a change re-projects the window and Sets only the values that differ, so changing one item repaints that row and nothing else. Row height is discovered by measuring the template against the view's full height — a template rooted in something that stretches (a `Grid` with default star rows) will ask for the whole view and give you a one-row list. Say what the row wants: `<Grid Rows="1">`, or `Height="1"`.

### TypeAhead

`components.TypeAhead` — Windows Explorer's type-ahead find, attached to an `ItemsView`. You type; the selection jumps to the first item whose `Key` value has that prefix, in the collection's current order, wrapping at the end.

```xml
<ItemsView Items="{{.Rows}}" Selected="{{.Sel}}">
  <TypeAhead Key="Title" Search="{{.Typed}}" NoMatch="{{.Missed}}"/>
  <ItemsView.ItemTemplate>…</ItemsView.ItemTemplate>
</ItemsView>
```

| Attribute | Meaning |
| --- | --- |
| `Key` | **Required.** Which projected item value to match. A projection is a `map[string]any` and, with no reflection anywhere, nothing else can say which entry is the label. |
| `Search` | Binding to a `string`: the live buffer. |
| `NoMatch` | Binding to a `bool`: the last keystroke matched nothing. |
| `Timeout` | Idle reset, default `1s`. |

It **selects; it does not filter** — no row is ever hidden. That is what makes "any movement resets the search" coherent: a filter would make rows reappear underneath the user mid-gesture. Matching is case-insensitive **prefix**, not fuzzy (`dc` finds `dcache`, not `DocumentCache`), because "the first match in the current sort order" is not something subsequence matching has.

**The mode is entered implicitly** — there is no arming key, as in Explorer. Two things follow:

- The idle `Timeout` is load-bearing, not a nicety. `a`, pause, `b` lands on the first `b`, not on `ab`. Without it the buffer grows forever.
- **A list with a `<TypeAhead>` loses `j`/`k` navigation**, because `j` now types `j`. The trade is opt-in per list and visible in the markup; every list without the element keeps them.

Repeating one letter **cycles**: `aaa` steps through successive items beginning with `a` rather than searching for the repetition. Refining does not: the second character of `ap` searches from the current selection, so you keep the item you just landed on when it still matches.

**Any movement resets the buffer** — arrows, Home/End, PgUp/PgDn, enter, tab, esc, and equally a click, the wheel, or a viewmodel write. Navigation keys are declined rather than consumed, so the list still performs the movement.

**A miss is state, not sound.** The selection stays put and `NoMatch` goes true; the character stays in the buffer, so continuing to type keeps missing, and a typo is escaped by pausing. There is no terminal bell: input dispatch has no route to the output stream, and `render.Screen` treats `0x07` as an OSC terminator, so a bell would be invisible to gooey's own tests. Bind `NoMatch` to whatever your page should show.

Binding `Search` is optional but recommended. Explorer displays nothing and survives on muscle memory; an implicitly-armed TUI mode that shows nothing is misrepresenting what the next keystroke will do, and a status-bar `<Text>` costs one property.

`<TypeAhead>` on anything but an `<ItemsView>` is a load error.

### Timer

`components.Timer` — a non-visual element that runs a command on an interval. Like `KeyBinding` it is hosted as an attachment on its parent, never laid out or painted.

```xml
<Timer Interval="600ms" Tick="{{.Advance}}" Enabled="{{.Running}}"/>
```

| Attribute | Meaning |
|---|---|
| `Interval` | **Required.** Any `time.ParseDuration` string (`"600ms"`, `"2s"`). Missing, unparseable, or non-positive is a load error. |
| `Tick` | The command, resolved like `Click` — a binding or a bare handler name. |
| `Enabled` | Optional binding to a `*prop.Property[bool]`. Absent means always enabled. |

Two things make it safe. The ticker goroutine never touches the property graph: it **posts** the tick to the `Dispatcher` and the app's loop runs it, so by the time `Tick` executes it is ordinary UI-goroutine code. And `Enabled` is read at fire time, on the loop, for the same reason — which is what lets the graph pause a timer, since binding it to the property a checkbox toggles stops the timer without tearing anything down.

Lifetime belongs to the `Composer`, not the component. Timers do not run until `Composer.Start(dispatcher)`, and `Composer.Close()` stops them. Hot reload builds a new composition, so the outgoing one must be closed or its ticker keeps running against a viewmodel nobody is showing:

```go
disp := gooey.NewDispatcher()
attach := func(w gooey.Component) {
    if comp != nil {
        comp.Close()
    }
    comp = gooey.NewComposer(w, cols, rows)
    comp.Start(disp)
}
```

and the loop drains it:

```go
select {
case <-disp.Wake():
    disp.Drain()
case ev := <-events:
    comp.Handle(ev)
}
```

`cmd/cards` drives its whole data stream this way.

### FileWatcher

`components.FileWatcher` — `Timer`'s other half: a non-visual element that runs a command when a watched file or directory **changes**. Hosted as an attachment on its parent, never laid out or painted. Design record: [`docs/specs/2026-08-23-filewatcher.md`](specs/2026-08-23-filewatcher.md).

```xml
<FileWatcher Paths="{{.Sources}}" Changed="{{.Reload}}" Path="{{.Hit}}" Enabled="{{.Live}}"/>
```

| Attribute | Meaning |
|---|---|
| `Paths` | **Required.** A binding to a `*prop.Property[[]string]`, or a literal pipe-separated list (`Paths="notes.md \| assets"`). Paths resolve against the page's `fs.FS`, so they are slash-separated and unrooted; a literal that is not is a load error. Written empty is a load error, but a **bound** list that resolves empty is legal and inert. |
| `Changed` | The command, resolved like `Click` — a binding or a bare handler name. |
| `Path` | Optional binding to a `*prop.Property[string]`, `Set` to the path that caused this hit immediately before `Changed` runs. |
| `Interval` | Optional poll period. Absent means `components.DefaultWatchInterval` (300 ms — the framework's own hot-reload rate). Unparseable or non-positive is a load error. |
| `Enabled` | Optional binding to a `*prop.Property[bool]`, or a conditional (`Enabled="{{not .Paused}}"`). Absent means always enabled. |

Three things about it are decisions rather than details.

**A directory is not watched by its `ModTime`.** A directory's own mtime moves when an entry is added or removed and *not* when one is edited, so a `ModTime` comparison silently misses every edit — the exact failure a watcher exists to prevent. A path that resolves to a directory is fingerprinted from its entries (each name, size and mtime); a path that resolves to a file, from its own size and mtime; a path that resolves to nothing, as a distinct **absent** state, which is what makes "watch a file that does not exist yet" and "deleted and recreated" ordinary changes rather than special cases. The walk is one level deep — to follow a subtree, name its directories.

**One hit per poll.** Coalescing is by state comparison over the poll window: however many watched paths changed however many times between two polls, `Changed` runs once, and `Path` names the first of them in `Paths` order. That is the structural advantage of polling — an editor's rename-and-replace save is two states inside one window and only the endpoints are compared, so it is one hit rather than three.

**The baseline is taken inside `Start`, on the UI goroutine.** Everything true of the filesystem when the composition starts is the baseline and everything after it is a change, so there is no window in which a write races the launch and is swallowed. The same rule covers a path that joins a bound `Paths` list later: its current state is recorded silently rather than reported.

Confinement follows `Timer` exactly. The poll goroutine never touches the property graph — it cannot even read `Paths`, which it asks the loop for — and `Enabled` is read at fire time, on the loop. Because `Enabled` gates the hit and not the poll, a change made while disabled advances the baseline and is dropped; re-enabling resumes and does not replay it. Lifetime belongs to the `Composer`: `Composer.Close()` stops **and joins** the goroutine, so a replaced tree cannot keep polling on behalf of a viewmodel nobody is showing.

An `embed.FS` reports a constant zero `ModTime` for every file, so a watcher over one is a natural no-op — the same page works in the dev tier (`os.DirFS` + watcher) and the release tier without changing.

### Companion

`components.Companion` — a **child process** that runs for as long as the tree that declares it. Non-visual like `Timer` and `KeyBinding`: hosted as an attachment on its parent, never laid out or painted. It is the markup tier of `gooey.Companion`; the process machinery underneath is `gooey.CompanionCmd`, unchanged. Design record: [`docs/specs/2026-08-10-markup-companions.md`](specs/2026-08-10-markup-companions.md).

```xml
<Companion Name="temporal-worker"
           Path="python3"
           Dir="worker"
           Log="worker.log"
           Error="{{.WorkerError}}"
           Exited="{{.Quit}}">
  <Companion.Args>
    <Arg>worker.py</Arg>
    <Arg>--queue</Arg>
    <Arg>{{.TaskQueue}}</Arg>
  </Companion.Args>
  <Companion.Env>
    <Var Name="GOOEY_MCP_URL" Value="{{.McpURL}}"/>
    <Var Name="PYTHONUNBUFFERED" Value="1"/>
  </Companion.Env>
</Companion>
```

> **Security: this element spawns processes, and markup arrives over MCP.**
> Any MCP client can call `swap_markup` or `patch_markup`, and those build markup through the same path a page on disk takes. Because markup can now name a binary, **an app that serves MCP and allows companions gives its clients arbitrary command execution** — an escalation past the posture recorded in [`docs/specs/2026-08-10-mcp-server.md`](specs/2026-08-10-mcp-server.md), *"an MCP client can do anything the keyboard can"*. The keyboard cannot spawn `rm -rf`.
>
> This is deliberate: a capability honored on one build path and refused on another is two languages sharing a syntax. The perimeter is elsewhere — the MCP server is **opt-in**, unauthenticated, and bound wherever the host asks (loopback by default, not restricted to it). There is a default-deny `Origin` check, but read what it actually covers: a request carrying **no** `Origin` is allowed through, because that is what a non-browser client looks like. It narrows the *browser* attack surface and does nothing about who can reach the address. The off switch is the environment variable **`GOOEY_MARKUP_COMPANIONS`**. Unset or empty means enabled; set it to `0`/`false` (or to anything unparseable — it fails closed) and every `<Companion>` becomes a load error naming the switch. Do not hand untrusted markup to an app whose environment allows companions.

| Attribute | Meaning |
|---|---|
| `Name` | **Required.** The companion's label in errors, and the element's `Name=` identity for `markup.Find` and tree snapshots. |
| `Path` | **Required.** The executable. A bare name (`python3`) is resolved on `PATH` **at load time**; a path containing a separator resolves against the document's directory. Either way the result is made **absolute**, because `exec.Cmd` resolves a relative `Path` against `Dir` — so a relative one would silently mean two different files depending on whether `Dir` was also set. A binary that is not installed is a load error, not a start failure behind a screen that is already up. |
| `Dir` | Working directory, resolved against the document's directory. Must exist at load time. |
| `Log` | Output file, resolved against the document's directory. Truncated and opened when the child starts, closed after it stops. **Absent means `os.DevNull`.** The file need not exist at load time, but its directory must — and the path itself must not already *be* a directory. |
| `KillDelay` | `time.ParseDuration`; the grace between the stop signal and `SIGKILL`. Default 5s. |
| `StopTimeout` | `time.ParseDuration`; how long stopping waits for the child after cancelling it. Default 10s; past it `Leaked()` reports that the wait gave up. |
| `CleanEnv` | Starts the child from an **empty** environment. Any `strconv.ParseBool` spelling works (`true`, `1`, `TRUE`, `T`); anything else is a **load error**, because a value that quietly fell back to "inherit" would hand the child every secret in the launching shell. Default is inherit-and-override. |
| `Error` | Optional binding to a `*prop.Property[string]`. Receives a `*gooey.CompanionError`'s message when the child fails to start or exits unbidden, and `""` on a successful start. |
| `Exited` | Optional command, run on the UI goroutine when the child is gone for a reason nobody asked for — including never having started. `Exited="{{.Quit}}"` reproduces the app tier's "a dead service takes the app with it". |

Unknown attributes are a **load error**, as they now are on every element: a misspelled `Dir=` that silently ran the child somewhere else, or a misspelled `Log=` that silently sent its output to the null device, are both worse than a startup failure. Layout attributes are rejected for the same reason — a non-visual element has no bounds to place.

**Output never reaches the terminal.** A child writing to the inherited stdout paints over the UI's bottom rows in raw mode with bytes the framework cannot repair, so the default is the null device and `Log` takes a *path*. There is no `Log="stdout"` and no way to spell one.

**Args and env are property elements.** `Args="worker.py --queue my-queue"` is lossy the moment an argument contains a space, and XML attributes have already spent both quote characters. One `<Arg>` per argument, document order preserved, no escaping; `<Var>` names are literal and `<Var>` values bind like any other text attribute. Both are consumed as data, so they never enter the visual tree. There is no shell anywhere — `Path` plus the `<Arg>` list is an argv, and nothing re-parses it.

**An `<Arg>` body is whitespace-significant.** `<Arg>` takes its argument from its body, so it follows the same rule as `<Text>` (see [Text](#text)): a one-line body is kept verbatim, leading and trailing spaces included. `<Arg>  --lead</Arg>` passes `"  --lead"`, not `"--lead"`. That is deliberate — an argv token is exactly the kind of literal a loader must not quietly rewrite — but it means a stray space inside the tags reaches the child process. A body that wraps across lines is unindented and joined, as everywhere else.

**Bindings in `<Arg>` and `<Var Value>` are snapshots**, read once when the child starts. Changing the property afterwards does not restart the child — an argv is a value a process was launched with, not one it observes. This is what lets a declaration depend on something only Go knows (an MCP endpoint that is not knowable until the listener is bound): the app puts it in a property, the document binds it.

**Paths are document-relative.** `Dir` and `Log` resolve against `Context.Dir`, which an app sets to the same directory it rooted the page's `fs.FS` at:

```go
app = gooey.NewApp(markup.Page(os.DirFS(dir), "page.gooey", ctx))
ctx.Dir = dir
```

`fs.FS` cannot answer this — `os.DirFS(dir)` offers no way back to `dir`, and `chdir`/`open` do not take an `fs.FS`. An empty `Context.Dir` falls back to the process's working directory.

**Today that only holds for a companion declared in the page.** `Context.Dir` is not among the fields a UserControl or Include inherits from its parent, so a `<Companion>` inside a control file sees an empty `Dir` and resolves `Dir=`/`Log=` against the process's working directory even when the app set `ctx.Dir` — tracked in [#314](https://github.com/WonderForgeLabs/gooey/issues/314). Declare companions in the page, or pass absolute paths, until it is fixed.

**Lifetime is the composition's, not the app's.** The Composer starts the child when the tree goes live and stops it — cancelling, then waiting, bounded by `StopTimeout` — on `Composer.Close`. That covers every teardown path (quit, signal, context cancellation, panic). A requested stop does **not** run `Exited`.

**A replaced companion never overlaps its replacement, but the two replacement paths pay for it differently.** Both stop the outgoing child before the incoming one starts; where the wait happens is what differs, and it matters because these services are not idempotent — two children of the same service fight over a port, and the second truncates the log the first is still writing.

| Path | What happens | Cost |
|---|---|---|
| **Full swap** — hot reload, `swap_markup`, anything going through `App.attach` | The outgoing `Composer` is **closed** before the incoming one is built and started. | The wait happens *between trees*, with no tree on screen to freeze. |
| **Structural re-sync** — `patch_markup`, a `Dynamic` container dropping the row | The same `Composer` re-walks its tree inside `Frame()`. Departed startables are stopped, **then** arrivals are started. | The wait happens **on the UI goroutine, mid-frame**. Removing a `<Companion>` this way paints nothing, reads no input and handles no signals until the child is gone — up to `StopTimeout` (10s by default). |

That second row is a real freeze, not a theoretical one, and it is the price of "stopped means stopped": a patch cannot both return promptly and guarantee the child is dead. Keep `StopTimeout` short on a companion you expect to be patched in and out of a live page, or move the element out of the patched subtree so it survives the re-sync and is only torn down on a full swap.

That is one tier down from `gooey.WithCompanions` / `App.AddCompanion`, which start *before* the tree is built and stop *after* the terminal is restored. The rule of thumb: **if the tree's construction depends on the service, declare it in Go**; if the running UI merely uses it, declare it here. Both tiers can be used at once and do not interact. `WithCompanionGrace` has no markup spelling (it names a moment a markup declaration is discovered after, and a swapped-in page must not be able to reconfigure the app's startup); `WithCompanionStopTimeout` does, per element, which is the per-companion shape the [companions spec](specs/2026-08-10-companions.md) called the right one.

## Universal layout attributes

Every **visual** element (all built-ins whose component embeds `gooey.Base`, and any well-behaved custom component) accepts the FrameworkElement attributes. They map onto the component's `Layout` and are honored by the shared measure/arrange sandwich, so they work identically inside any container. The non-visual elements — `<KeyBinding>`, `<Timer>`, `<FileWatcher>`, `<Tooltip>`, `<Validate>`, `<TypeAhead>`, `<ValidationMarker>`, `<Companion>` — **reject** them at load: they have no bounds to place, so `<Timer Width="4"/>` is an error rather than a silent no-op.

| Attribute | Values | Meaning |
|---|---|---|
| `Width`, `Height` | integer cells | Explicit size; 0/absent = auto. |
| `Margin` | 1, 2, or 4 comma-separated integers | `"1"` = all four sides; `"2,0"` = horizontal, vertical; `"2,0,0,0"` = left, top, right, bottom. |
| `HAlign`, `VAlign` | `Stretch` (default), `Start`, `Center`, `End` | Alignment inside the layout slot. Stretch fills the slot; the others use the measured desired size. |
| `Visibility` | `Visible` (default), `Hidden`, `Collapsed`, or a `{{...}}` binding | Hidden occupies space but does not paint; Collapsed occupies nothing (and its subtree is skipped by focus traversal). The bound form accepts a `*prop.Property[gooey.Visibility]` or a `*prop.Property[bool]` (true→Visible, false→Collapsed); a `Set` repaints exactly what the literal flip repaints. |
| `Grid.Row`, `Grid.Col` | integer | Cell address when the parent is a Grid — the attached-property syntax. |
| `Grid.RowSpan`, `Grid.ColSpan` | integer | Cells spanned; 0/absent means 1. |
| `Canvas.Left`, `Canvas.Top` | integer cells | Offset from the parent Canvas's top-left corner — the attached-property syntax again. |

The `Grid.*` and `Canvas.*` attributes live on the child, XAML-style; they are stored in the element's own `Layout` (Go has no attached-property store, so the element itself is it). A **misplaced** one is a load error naming the parent that would have contributed it — `Canvas.Left` under a `<VStack>`, or `Grid.Row` under a `<Canvas>`, does not load, rather than sitting there inert. The one position where all of them are accepted is a document or patch-fragment **root**, which has no layout parent to scope against. Both families are also excluded from the attribute hand-off into an Include, since they position the instance rather than describing it.

## The binding DSL

A binding is `{{.Path}}` — Go-template spelling, where `Path` is a dot-separated lookup through `Context.Values` (nested `map[string]any` levels for the dots).

Text content and text-valued attributes (`Text` content, `Border Title`, `Button Content`) accept mixed literal and binding parts:

```xml
<Text>lines: {{.Count}} ({{.State}})</Text>
```

Each `{{.Path}}` must resolve to a live handle or a plain value of a **formattable type**, and anything else is a build error, as is a path that does not resolve. The accepted set (`textSource`, `markup/markup.go:1158`) is:

| Handle | Plain value | Rendered as |
|---|---|---|
| `*prop.Property[string]` | `string` | verbatim |
| `*prop.Property[int]`, `[int64]` | `int`, `int64` | `strconv.Itoa` / `FormatInt` |
| `*prop.Property[float64]` | `float64` | `strconv.FormatFloat(…, 'f', -1, 64)` |
| `*prop.Property[bool]` | `bool` | `"true"` / `"false"` |
| `*prop.Property[time.Duration]` | — | `Duration.String()` |
| `*prop.Property[render.Color]` | — | `#rrggbb` |

A handle stays live; a plain value is a static splice. There is exactly **one** float spelling — `'f'`, shortest round-trip — and it is deliberately not configurable here: a format string in an attribute is a second templating language. The same table backs handler arguments (`Arg.String`), so a value formats identically wherever it appears.

Three forms are legal inside the braces, and **nothing else is**: `{{.Path}}`, `{{ns:Func args…}}` (a [value namespace](#value-namespaces) call), and `{{op operands…}}` (a [conditional expression](#conditional-expressions)). A brace expression that is none of them — an undeclared prefix, a malformed path, an unterminated `{{` — is a **load error** naming what could not be resolved. It is not literal text. (It used to be: before the scanner in `markup/scan.go`, `<Text>{{env:Get `HOME`}}</Text>` loaded clean and painted its own source on the terminal. See issue #221.)

The three are disjoint by their first token: a binding starts with `.`, a namespace call has a colon after its prefix, and a conditional starts with a bare operator word **followed by operands**. That last clause is what keeps `{{ nonsense }}` — a lone bare word — reporting "neither a binding nor a value-namespace call" instead of a confusing complaint about conditional functions.

### Conditional expressions

A predicate grammar whose result is **always `bool`**, so a page can say "hidden while there is nothing to show" without a computed in the viewmodel:

```xml
<Text Visibility="{{not .Empty}}">rows</Text>
<Button IsEnabled="{{and (eq .Name ``) (eq .Email ``)}}" Content="Save"/>
```

| Form | Arity |
|---|---|
| `{{not X}}` | 1 |
| `{{and X Y …}}` | ≥ 2 |
| `{{or X Y …}}` | ≥ 2 |
| `{{eq A B}}`, `{{ne A B}}` | 2 |

where `X` is `.Path` (a `*prop.Property[bool]`) or a parenthesized subexpression, and `A` is `.Path` or a backtick literal. Nesting is allowed **only** through parentheses — `{{and .A (or .B .C)}}` — and a missing paren is a load error naming the trailing token, not a silent reinterpretation.

Everything resolvable fails at **load**: arity, operand types, an unknown operator, a path that does not resolve or resolves to the wrong type. A backtick literal may contain `}`, `{{` or `}}`; an unterminated one names itself.

The handle is a `prop.NewComputed`, so operand reads happen inside its evaluation and become dependencies — a conditional costs exactly what the equivalent hand-written computed costs, and repaints exactly its readers.

**A conditional is one-way.** `Set` on a computed panics, so `Checked="{{not .X}}"` on a Checkbox loads, paints, and panics on the first click. This is inherited, not introduced — `Checked="{{.AnyComputedBool}}"` has always done the same — and the fix belongs in the two-way binders. Use conditionals on one-way attributes: `Visibility`, `IsEnabled`, and the like.

Deliberately **excluded**: ordering (`lt`/`gt`/…), which is meaningless over bool and whose operand types stop being obvious from the name; `float64` in `eq`/`ne`, because exact float equality is a bug in almost every document that would write it; text output (`{{if}}`/`{{else}}` around markup), which would be a build-time tree transformation and a second answer to "what is my element vocabulary"; and bare-word operands, so that "read this property" has one spelling across all three grammars.

There is consequently no escape for a literal `{{` in content; route it through a property, whose value is never re-parsed. Issue #227 tracks whether that needs a mechanism.

Resolution happens once, at build time, to property handles — not values. This is the lvalue semantics of the design: the built component holds the handles, and evaluation at render time does no lookups. Mixed content becomes a single computed string property over its parts, so setting any bound source property repaints exactly the components that read it — there is no refresh call anywhere.

### Event bindings

Event attributes (`Button Click`, `KeyBinding Command`) resolve in one of three ways — the event-binding split:

- Handler-expression form — `Click="{{net:Get .Url | into .Body}}"` names a function in a declared handler namespace, so the behavior itself is declared in markup with no delegate anywhere. See [handler namespaces](#handler-namespaces).

- Binding form — `Click="{{.Save}}"` resolves a value in `Context.Values`, which must be a `gooey.Action` (a `gooey.Command`, or a `*gooey.Cmd` from `gooey.NewCommand`) or a plain `func()`. The delegate lives in the viewmodel, so markup-only controls can wire events with no code-behind at all. This is the form all the `cmd/` demos use:

  ```go
  Values: map[string]any{
      "Increment": gooey.Command(func() { count.Set(count.Get() + 1) }),
      "Quit":      gooey.Command(func() { app.Quit() }),
  }
  ```

- Bare-name form — `Click="OnSave"` resolves against `Context.Handlers`, the code-behind handler registry. An unregistered name is a build error.

An empty event attribute is not an error — the element simply has no command.

### Attribute bindings on custom elements

On custom components, UserControls, and Includes, an attribute like `Stories="{{.Stories}}"` resolves to the raw context value — typically a typed `*prop.Property[T]` handle of any `T`, not just string. This is how non-string data crosses element boundaries. A custom component's builder should reach it through `markup.Bound[T]` (see [Custom components](#custom-components)), which does the type assertion and produces the same load error a built-in would; `Context.BindingValue` is the lower level underneath it, and what a UserControl `setup` uses against the *parent* context.

## Handler namespaces

A prefixed namespace binds events to *framework-provided* handlers, so behavior can be declared in the markup itself:

```xml
<Gooey xmlns="wonderforge.io/gooey/2026"
       xmlns:net="gooey.dev/handlers/net"
       xmlns:temporal="gooey.dev/handlers/temporal">
  <Button Content="fetch"   Click="{{net:Get .Url | into .Body}}"/>
  <Button Content="slugify" Click="{{temporal:Activity `Slugify` .Input | into .Output}}"/>
</Gooey>
```

Neither button has a delegate. What the app supplies is the *capability*:

```go
markup.RegisterHandlers(nethandlers.URI, nethandlers.New())
markup.RegisterHandlers(temporalhandlers.URI, temporalhandlers.New(client, "gooey-demo"))
```

**Registration is the capability grant.** Markup can only invoke namespaces the host app registered; drop a registration and the same document stops loading, naming the URI it wanted. That is what makes markup loaded from an untrusted `fs.FS` safe to run *for handlers*: a handler URI reaches exactly the capabilities its host chose to hand it, and nothing else — no markup syntax registers a provider or widens a handler grant.

> **`<Companion>` is the exception, and it is not covered by this grant.** It names a binary directly rather than a registered namespace, so a document that declares one expands what the process does without any Go-side registration. It is enabled by default; `GOOEY_MARKUP_COMPANIONS=0` is the only thing that takes it away, process-wide. "Untrusted `fs.FS` is safe to run" holds for handlers, not for a build that allows companions — see [Companion](#companion). The full doctrine (pack taxonomy, module boundaries, grant scopes) is [docs/specs/2026-08-10-pack-distribution.md](specs/2026-08-10-pack-distribution.md).

### Grammar

```
{{prefix:Func arg… | into .Target}}
```

- `prefix` resolves through the document's own xmlns table. Namespaces are **per document** — an Include or UserControl declares its own and cannot inherit the page's, so a control's capabilities never depend on who included it.
- Arguments are the DSL's usual atoms: `` `backtick literal` `` (a constant string) and `.Path` (a property handle). Bound arguments are read **at invoke time**, not at load — the same lvalue semantics as every other binding, so re-pointing `.Url` changes what the next press fetches.
- `| into .Target` names the `*prop.Property[string]` the result is written to. It is the only pipeline stage v1 defines — grammar v2 (`| err`, `| progress`, multiple targets, retry and timeout) is epic [#38](https://github.com/WonderForgeLabs/gooey/issues/38) — and it is **optional**: a function with no result to deliver is simply written without one — ``{{wf:Signal `approve`}}`` sends the signal and drops the receipt, because delivering to an absent target is a no-op (`markup.Target.Deliver`). Functions that do produce a result still work without the stage; the result is discarded.

The expression produces a `gooey.Command`, so it works anywhere a command does — including `<KeyBinding Command="…">`. A handler expression on an Include's attribute is resolved in the *parent* (the document that declared the prefix) and crosses the boundary as an ordinary command value.

Everything resolvable is resolved when the document loads: unknown prefix, unregistered URI, unknown function, wrong arity, missing target, unbindable argument, and provider-specific complaints are all load errors, never surprises on click.

### Async results and the Dispatcher

Handlers run off the UI goroutine, and properties are UI-goroutine-confined. A document using handler namespaces therefore needs a dispatcher, and says so at load time if it is missing:

```go
disp := gooey.NewDispatcher()
ctx := &markup.Context{ /* … */ Dispatcher: disp }
```

The app's loop drains it, which is where the result properties are actually `Set`:

```go
select {
case <-disp.Wake():
    disp.Drain()
case ev := <-events:
    comp.Handle(ev)
}
```

Nothing in the provider knows which components display the result. The `Set` dirties whatever read the property, and the next frame repaints exactly those.

### Providers

| Namespace URI | Package | Functions |
|---|---|---|
| `gooey.dev/handlers/net` | `handlers/net` | `Get .Url` — HTTP GET, body as a string |
| `gooey.dev/handlers/fs` | `handlers/fs` | `Read .Path` — file contents (capped, 1 MiB default); `List .Dir` / `Stat .Path` — JSON entries; `Glob .Pattern` — JSON array of paths |
| `gooey.dev/handlers/fs` (writable grant) | `handlers/fs` | `Write .Path .Content` / `Append .Path .Content` — the target is a status slot, `""` on success |
| `gooey.dev/handlers/temporal` | `handlers/temporal` (separate module) | ` Activity `Name` .Arg` — a Temporal standalone activity |
| `gooey.dev/handlers/temporal/workflow` | `handlers/temporal` (separate module) | `` Signal `name` [args…] `` — signal ONE workflow: ``{{wf:Signal `approve` \| into .Notice}}``; conventional prefix `wf:`. The registration names the workflow ID, so served markup can signal that workflow and nothing else; the optional `into` receives a delivery receipt (`"ERROR: …"` on failure). Ground truth: `handlers/temporal/workflowui.go` |
| `gooey.dev/handlers/exec` | `handlers/exec` (separate module) | `` Run `name` [options] [args…] `` — an allowlisted local command; conventional prefix `sys:` |

All of them deliver failures into the same target as an `"ERROR: …"` string in v1, so a page can show what went wrong without a second binding.

The fs registration names a **root**: `fshandlers.New(fsys)` grants exactly the `fs.FS` it is handed, and every path a page names resolves inside it — absolute paths and `..` are rejected per `fs.ValidPath` (a literal path fails at load; a bound one delivers an ERROR). Read-only is the default posture; writes exist only through the separate constructor `fshandlers.NewWritable(dir)`, backed by `os.Root`, so a symlink inside the granted directory cannot lead out of it either. `Write` and `Append` on a read-only grant are load errors naming the missing writable grant.

The exec provider's registration is an **allowlist**: markup names a registered `Command` (a backtick literal, checked at load), never a binary, and nothing is ever shell-interpreted. Option literals between the name and the arguments select the capture stream (`` `capture=stdout|stderr|combined|both|exit-code` ``) and a gojq extraction (`` `jq=.items[].name` ``), both validated at load time; `` `--` `` ends option parsing. See `handlers/exec/README.md` and `docs/specs/2026-08-10-exec-pack.md`.

A provider is a typed factory — `NewCommand(*markup.Call) (gooey.Command, error)` — with no reflection: arguments arrive as resolved handles, and a provider needing a type other than string type-switches on `Arg.Raw`.

## Value namespaces

The pull half of the same mechanism (landed in [PR #231](https://github.com/WonderForgeLabs/gooey/pull/231); the wider surface is epic [#228](https://github.com/WonderForgeLabs/gooey/issues/228)). A handler namespace answers *what happens when the user does this*; a value namespace answers *what is this worth right now*:

```xml
<Gooey xmlns="wonderforge.io/gooey/2026"
       xmlns:env="gooey.dev/handlers/env"
       xmlns:str="gooey.dev/handlers/str">
  <Text>hi {{str:Upper .User}}, on {{env:Get `TERM` `(unknown)`}}</Text>
</Gooey>
```

```go
markup.RegisterValues(envhandlers.URI, envhandlers.New("USER", "TERM"))
markup.RegisterValues(strhandlers.URI, strhandlers.New())
```

Same grammar, same backtick literals, same `.Path` arguments — a different **position**. A value expression goes wherever a binding goes and resolves at build time to a `*prop.Property[string]`, composing with literals and paths in one run of interpolated content.

### Push and pull are a property of the capability

An **effect** — fetch a URL, run a workflow, spawn a process, play a sound — is an event: it happens at a moment, it can fail, it wants a target. `Click="{{net:Get .Url | into .Body}}"` is right.

A **value** — the environment, an uppercased name — has no moment and nothing to deliver into; it *is* the binding. Writing one on a `Click` would mean declaring a property, declaring a button, and pressing it before the page is correct.

The two registries are separate, so a namespace can grant its read half without its write half — which is exactly what `handlers/env` does. Both crossovers are load errors with specific messages:

```
markup: {{net:Get …}} is in a value position, but "gooey.dev/handlers/net"
is registered as a HANDLER namespace (event-only): invoke it from an event
attribute, as Click="{{net:Get … | into .Target}}"
```

`| into` in a value position is likewise a load error: a value expression delivers its result by *being* the binding.

### Damage tracking is inherited, not implemented

A provider builds its handle with `prop.NewComputed`, so every `Arg.String()` it calls runs **inside an evaluation** — which is what makes that `Get` a subscription rather than a read. `{{str:Upper .User}}` repaints exactly the components that display it, only when `.User` changes.

The corollary is the usual trap, and it bites providers harder than pages: an argument read behind an early return or a short-circuit drops out of the dependency set on the frames where it does not run. `env:Get`'s fallback and `str:Default` both hoist *both* reads above the branch, and both have a test that fails if someone un-hoists them.

### Providers

| Namespace URI | Package | Functions |
|---|---|---|
| `gooey.dev/handlers/env` | `handlers/env` | `` Get `NAME` [`fallback`] `` — an allowlisted environment variable; `Names` — the sorted grant |
| `gooey.dev/handlers/env` (writable grant) | `handlers/env` | `` Set `NAME` .Value `` / `` Unset `NAME` `` — handler side; writes the process environment **and** the source property, so readers repaint |
| `gooey.dev/handlers/str` | `handlers/str` | `Upper`, `Lower`, `Trim` (1 arg); `` Replace .S `old` `new` ``; `` Join `sep` a b… ``; `` Default .S `fb` ``; `` Pad .S `n` ``, `` Truncate .S `n` `` (width is a load-time literal, counted in COLUMNS — a CJK or emoji character costs the two cells it occupies, not one) |
| `gooey.dev/handlers/sets` | `handlers/sets` | Set algebra over name sets, for attributes like `<Frozen Allow>`: `` Concat a b… `` (union); `` Without .Base `X` `` (difference); `` When .Cond `X` `` (conditional); `` Group `Text` `` (expands a `gooey.Allow` group); `` Has .Set `X` `` |

The env registration is an **itemized allowlist**, `handlers/exec`'s posture rather than `handlers/fs`'s: the environment is where a process keeps its credentials next to its terminal type, so `envhandlers.New("USER", "HOME")` grants exactly those and an ungranted name is a load error naming the grant. There is deliberately no grant-everything constructor. The variable name is always a backtick literal — an allowlist checked at load time is the point.

A provider is a typed factory — `NewValue(*markup.Call) (*prop.Property[string], error)` — with no reflection. The `Call` is the same struct handler providers receive, with `Target` left invalid, so a provider serving both sides can tell the positions apart.

**Limits today:** value expressions only work in string positions (element content, `Content`, `Title`, `Label`, `Prompt`, `Frozen`'s `Allow`) — typed attributes such as `Visibility`, `Style` and `Background` resolve through a different path (issue #222). `<Frozen Allow>` is a string position *on purpose*: carrying the category set as text is what lets `sets:Concat` compose it out of literals and bound paths with no new binding machinery, at the cost of an unknown name in a bound value being a fail-closed runtime answer rather than a load error. There is no nesting: ``{{str:Upper env:Get `USER`}}`` does not parse, and composition is issue #223's question, likely answered by #99's converter stages.

Design record: [docs/specs/2026-08-12-value-namespaces.md](specs/2026-08-12-value-namespaces.md).

## Property elements

Most attributes are strings. Some are markup — a template, and later a declared property. Those use XAML's property-element syntax: a child whose name is `<Parent.Name>`, filed on the parent as a named structured attribute rather than built as a tree child.

```xml
<ItemsView Items="{{.Rows}}">
  <ItemsView.ItemTemplate>
    <Text>{{.Title}}</Text>
  </ItemsView.ItemTemplate>
</ItemsView>
```

The rules are load-time errors, all of them:

- the prefix must name the element it sits inside — `<Grid.ItemTemplate>` inside an `<ItemsView>` is a typo, not a child;
- the element must accept that property — `<VStack.ItemTemplate>` is rejected, so a misspelling cannot silently vanish;
- a property element takes no attributes of its own, and may be given only once.

A registered custom component is exempt from the second rule: its builder receives the raw `Element`, `Props` and all, and decides for itself — the same latitude it has with attributes.

### The Behaviors slot

`<X.Behaviors>` is the one property element **every** element accepts: MAUI's explicit spelling of the attachment slot. Its children must be non-visual attachments — `<Validate>`, `<Tooltip>`, `<KeyBinding>`, `<Timer>`, `<FileWatcher>` — and they land in exactly the list bare non-visual children feed, so the two spellings are equivalent and may be mixed:

```xml
<TextBox Text="{{.Name}}">
  <TextBox.Behaviors>
    <Validate Required="true"/>
    <KeyBinding Gesture="ctrl+k" Command="{{.Clear}}"/>
  </TextBox.Behaviors>
</TextBox>
```

A visual child inside the slot is a load error naming it; an element that cannot host attachments rejects the slot's contents the way it rejects bare ones. The set is the framework's own — making a behavior something an author can define in markup is [#100](https://github.com/WonderForgeLabs/gooey/issues/100).

## Styles

`Style="name"` looks the name up in `Context.Styles` (`map[string]render.Style` — fg/bg color, bold, and so on), registered by the app:

```go
Styles: map[string]render.Style{
    "panel":  {Fg: render.RGB(120, 90, 220)},
    "accent": {Fg: render.RGB(255, 170, 60), Bold: true},
    "dim":    {Fg: render.RGB(140, 140, 150)},
}
```

Be honest about what this is: a named lookup, not a styling system. There is no cascading, no inheritance, no per-property overrides in markup (except `Text Bold`), and no selectors.

An unknown style name is a **load error** — `no style named "typo" is registered` — not a silent zero style. If you are calling `styleValue`'s logic from your own `Builder`, use the exported `markup.ResolveStyle(e, ctx, "Style", name)` rather than indexing `ctx.Styles` directly; a bare map index yields the zero `Style` on a misspelled name, so the element loads, paints unstyled, and reports nothing. Going through `ResolveStyle` also gets your builder the markup-declared scopes below, which a document author already expects from every built-in.

`Style` also accepts a **binding**, which is a different thing entirely:

```xml
<Border Style="{{.AccentStyle}}">
```

That resolves to a `*prop.Property[render.Style]` handle from the viewmodel. Because it is a live handle, a *computed* style is reactive — this makes the whole page follow one color:

```go
accent      := prop.NewSource(render.RGB(255, 170, 60))
accentStyle := prop.NewComputed(func() render.Style {
    return render.Style{Fg: accent.Get(), Bold: true}
})
```

Setting `accent` dirties `accentStyle`, which dirties exactly the components that read it while painting, and they repaint. No styling system is involved — it is the ordinary property graph. `cmd/colors` styles its border, title, and swatches this way from the color being edited. `Text Bold="true"` composes over either form.

## Resources: a palette a page can declare

A palette was the one thing a designer edits and the one thing markup could not express. Every demo in this repo carried its colors as a Go `map[string]render.Style`, so changing a shade meant a rebuild, and `cmd/colors` carried `#12121e` in two languages with nothing checking they agreed.

```xml
<Gooey xmlns="wonderforge.io/gooey/2026">
  <Gooey.Resources>
    <Resource Key="ground" Type="color" Value="#12121e"/>
    <Style Key="accent" Fg="#ffaa3c" Bold="true"/>
    <Style Key="panel">
      <Setter Property="Fg" Resource="ground"/>
    </Style>
  </Gooey.Resources>
  <Border Style="panel">…</Border>
</Gooey>
```

`<Gooey.Resources>` is the root's only property element. `<Resource>` declares a typed value; `<Style>` declares a `render.Style` recipe, either as attributes or as `<Setter>` children that can themselves reference a `Resource` by key.

Three properties, all inherited from how the rest of this page already works (see `docs/specs/2026-08-10-styles-and-resources.md`):

- **A resource reference is an lvalue.** The key resolves once, at build, to a `*prop.Property[T]`. The read happens inside the style computed, which is read inside a paint node — so `Set` on a resource repaints exactly the components whose appearance depends on it. No dictionary walk at paint time, no invalidation pass, no styling machinery left running after build. `Context.Resource` hands the handle to Go code, which is what makes a theme swappable at runtime.

- **A `<Style>` is a reactive recipe, not a property bag.** It materializes as one `prop.NewComputed[render.Style]` per instance, fed into the `Style` slot every component already has. Zero component changes.

- **Scoping is lexical and resolved at build.** Entering an element with a `<X.Resources>` slot pushes a scope and leaving pops it, so siblings can never see a scope they are not inside, and an inner definition shadows an outer one by producing a different handle for whoever referenced it there. There are no priority numbers.

### What wins

`Context.Styles` — the host's Go map — is the **outermost** scope, below every markup-declared one, so a page-declared style beats a host-granted style of the same name. That is the same "nearest declaration wins" rule as everywhere else, with `ctx.Styles` simply furthest away.

Two consequences make it the right way round. Migration works one key at a time: a page can move `accent` out of Go into its own `<Gooey.Resources>` and see it take effect without first deleting the Go entry. And the surprising direction is the other one — a host grant silently overriding a style declared three lines above the element using it would make the visible declaration the lie.

A name found in **neither** the lexical chain nor `ctx.Styles` fails the load.

### Control boundaries

Values isolate; **resources are ambient**. A control's markup binds only what crossed its declared surface, but it inherits the theme of the site that instantiated it. A control file may declare its own `<Gooey.Resources>` to shadow that ambient chain for its subtree — with fresh handles per instantiation, so two instances of a control do not share resource state, exactly as two instances do not share declared-property defaults.

## Custom components

`Context.Components` maps an element name to a `Builder` — `func(e Element, ctx *Context) (gooey.Component, error)`. A registered builder wins over everything, receives the raw element, and interprets attributes however it likes:

```go
Components: map[string]markup.Builder{
    "LogPane": func(e markup.Element, _ *markup.Context) (gooey.Component, error) {
        return &logPane{src: visible}, nil
    },
}
```

```xml
<LogPane Grid.Row="3" Lines="{{.Visible}}"/>
```

(from `cmd/markuplog`)

The universal layout attributes are applied by the framework after the builder returns, so a custom component that embeds `gooey.Base` gets `Margin`, `Grid.Row`, and the rest for free. Letting a registered component *declare* that surface, and have its attributes checked like a built-in's, is proposed in [PR #290](https://github.com/WonderForgeLabs/gooey/pull/290) (open).

A builder resolves its own attributes through the same four functions the built-in elements use, so a registered component speaks the whole dialect rather than a subset of it ([#266](https://github.com/WonderForgeLabs/gooey/issues/266)):

| resolver | attribute shape | absent |
|---|---|---|
| `markup.Bound[T](e, ctx, attr)` | `"{{.Path}}"` → the viewmodel's own `*prop.Property[T]`, shared two-way | load error |
| `markup.BoundText(e, ctx, attr)` | a literal, an interpolation like `"Hi {{.Who}}!"`, or a `{{ns:Func …}}` call — always a handle, never nil | empty literal |
| `markup.BoundColor(e, ctx, attr)` | `"#rgb"`/`"#rrggbb"`, or a bound `render.Color` | `nil` |
| `markup.BoundStyle(e, ctx)` | `Style="name"` from `Context.Styles`, or a bound `render.Style` | the zero style |

`markup.Bound[bool](e, ctx, "Checked")` is what makes `Checked="{{.Auto}}"` two-way: the builder gets the viewmodel's handle, so `Render` reading it is the repaint dependency and toggling `Set`s the same node.

`Context.BindingValue` is the lower level underneath `Bound` and is still there for a builder that wants the raw `any`. Do not use it for text: it matches a `{{.Path}}` anywhere in the value and returns that handle, so `Label="Hi {{.Who}}!"` resolves to `Who` and silently drops the literal parts. `BoundText` is the rule that keeps them.

### `Context.Elements`: declaring the surface

A `Builder` is a func, and a func is opaque. An element registered through `Context.Components` therefore contributes **a name and nothing else**: `Catalog()` reports it as `AttrsKnown: false`, and — the half that costs you — attribute checking declines on it entirely, so a typo is accepted, ignored, and reported nowhere.

`Context.Elements` takes the same `*markup.ElementDef` the built-ins use, so a host component gets the built-in experience: catalog description, a `Seed` a palette inserts, and unknown-attribute errors with near-miss suggestions.

```go
Elements: map[string]*markup.ElementDef{
    "ActivityBar": {
        Name: "ActivityBar", Proto: &components.Image{}, Known: true,
        Attrs: []markup.AttrSpec{{
            Name: "Sel", Kind: markup.KindBinding, Binds: markup.BindsBinding,
            GoType: "int", Required: true, Origin: markup.OriginRegistered,
        }},
        Children: markup.ChildSpec{Mode: markup.ModeLeaf},
        Seed:     `<ActivityBar Sel="{{.Sel}}"/>`,
        Build:    myBuilder,
    },
}
```

**`Seed` is what a palette inserts**, and a registered element owes one just as a built-in does. It is markup, not an inference: what a new instance should look like has to cover more than attributes, because an empty `<VStack>` measures nothing whatever its attributes say — and an element that measures 0×0 is invisible on a canvas and unselectable, so a user cannot fix it by hand.

Refer to a bind-only attribute by its **bare** name — `{{.Sel}}`. `markup.Seeded` rewrites that to `{{.<instance>_Sel}}` and returns a placeholder value to register under the same key, which is what stops two instances sharing one property. `markup.PlaceholderFor` supplies the value per declared `GoType`; a type it cannot answer for is a gap you close by writing a literal in the seed or by teaching that table.

Two rules a seed must follow, both enforced by tests: a container's seed names its children **inline** and they are taken verbatim rather than re-seeded, and a seed may not carry parent-dependent attributes such as `Canvas.Left` or `Grid.Row` — those are the inserter's job and are silently discarded under any other parent.

One gap worth knowing: `TestEveryElementDeclaresASeed` walks the **built-in** registry alone, so a registered element with no `Seed` is not caught by it. The failure is a palette entry a user can add and then not see.

Resolution order is `Elements`, then `Components`, then the built-in registry, then the `Includes` convention. A name present in **both** maps is a load error rather than a silent winner — which one won would otherwise depend on the order two `if`s happen to be written in, and the loser's registration would be dead code nobody can see.

A `Builder` registration is unchanged by any of this. The asymmetry is the point: declare, and you are checked.

`ElementDef.Body` declares an element whose content is its XML **body** rather than an attribute, as `<Text>hello</Text>` is. Do not derive this from `Children.Mode` — "takes no children" and "takes body content" are different statements that merely coincide on `<Text>`, and fourteen built-ins are `ModeLeaf` while exactly one reads a body.

## UserControls

`markup.UserControl(fsys, "storylist.gooey", setup)` wraps a markup file plus a code-behind setup function as a Builder, so a control registers like any custom component and instantiates as an element:

```xml
<StoryList Grid.Col="1" Stories="{{.Stories}}" Selected="{{.SelStory}}"
           Read="{{.Read}}" Open="{{.OpenStory}}" Title="stories"/>
```

(from `cmd/reader/reader.gooey`)

Context isolation is the contract: `setup(e, parent)` returns the instance's own `Context`, and bindings inside the control's markup resolve against it — never against the page. Data crosses the boundary through element attributes, resolved in the parent context:

- `parent.BindingValue(e.Attrs["Stories"])` returns the parent's property handle, which setup type-asserts and wires into its context or components. Bindings are live handles, not copied values.
- `parent.Command(e.Attrs["Open"])` resolves an event attribute the same way `Click` does; the control can then hand the command to a component or expose it in its own context (storylist puts `Open` in its context so its markup can attach it to a `<KeyBinding>`).
- Literal attributes arrive as plain strings (`Title="stories"`).

`Styles`, `Components`, `Handlers`, and `Includes` inherit from the parent context when the child leaves them nil; `Named` is scoped per instance (like `x:Name` in templates). Layout attributes on the instance element apply to the instance and are not passed through.

A control that also [declares properties](#declared-properties-xproperty) gets them resolved *before* setup runs and installed into the context setup returns; setup reads them through `parent.DeclaredProperties()` and extends the context with private members.

`cmd/reader/controls.go` is the canonical example — three controls, each a `.gooey` file wrapping a per-instance rows component, with a generic `attr[T]` helper for the typed hand-off. `markup.WatchAll` covers hot reload for the whole set: one page rebuild re-instantiates every control.

## Includes: markup-only controls

`Include(fsys, name)` is a UserControl with no code-behind: the instance's attributes become the control's context. Each non-layout attribute resolves in the parent context — binding to a property handle, literal to a string — and is exposed under its attribute name.

The usual way in is `Context.Includes`: when set, an unknown element `<Card/>` resolves by convention to `card.gooey` (lowercased element name) in that FS. Zero registration, zero code-behind:

```xml
<!-- page.gooey -->
<Gooey>
  <VStack>
    <Card Title="{{.Header}}" Sub="static subtitle"/>
  </VStack>
</Gooey>

<!-- card.gooey -->
<Gooey>
  <Border Title="{{.Title}}">
    <Text>{{.Sub}}</Text>
  </Border>
</Gooey>
```

(from `markup/usercontrol_test.go`)

Inside `card.gooey`, `{{.Title}}` is the parent's `Header` handle (live — setting `Header` repaints the card) and `{{.Sub}}` is a literal. `Width`, `Margin`, `Grid.*`, and `Name` on the instance stay on the instance. An unresolvable attribute binding is a load-time error.

This is the *implicit* surface: whatever the instance writes, the control receives, and an attribute the control never reads simply does nothing. Declare the surface with [`<x:Property>`](#declared-properties-xproperty) to have it checked instead.

Element resolution order, in full: registered `Components` builder, then built-in element, then Includes convention, then error.

## Declared properties: `<x:Property>`

A control file can declare its own property surface. Declarations are direct children of the root, under gooey's language-services namespace — `xmlns:x="wonderforge.io/gooey/x"`, the XAML `x:` analog:

```xml
<!-- card.gooey -->
<Gooey xmlns="wonderforge.io/gooey/2026"
       xmlns:x="wonderforge.io/gooey/x">
  <x:Property Name="Title"   Type="string" Required="true"/>
  <x:Property Name="Value"   Type="string" Default="—"/>
  <x:Property Name="Trend"   Type="string" Default="…"/>
  <x:Property Name="Caption" Type="string" Default="no caption"/>

  <Border Title="{{.Title}}" Style="panel">
    <VStack Margin="1,0">
      <Text Style="big">{{.Value}}</Text>
      <Text Style="trend">{{.Trend}}</Text>
      <Badge Text="{{.Caption}}"/>
    </VStack>
  </Border>
</Gooey>
```

(from `cmd/cards`, the whole demo's Go file has no control code in it at all)

**A declared markup property is an ordinary dependency property, registered from markup.** Each declaration materializes exactly what a code-behind would have wired by hand — a `*prop.Property[T]` node in the same graph, read by the same `Get`, invalidated by the same `Set`. There is one property system; this is its markup tier, the way `DependencyProperty.Register` is its code tier.

### Declaration attributes

| Attribute | Meaning |
|---|---|
| `Name` | Required. The attribute callers set, and the path the control's own markup binds (`{{.Title}}`). Cannot be `Name`, `Tooltip`, or a layout attribute — those belong to the element. |
| `Type` | Required. One of `string`, `int`, `bool`, `float`, `duration`, `color`, `any`. |
| `Default` | The literal used when the attribute is absent, coerced by `Type`. A bad default fails the load of the *control*, not of the page. |
| `Required` | `true` makes an absent attribute a load error. Exclusive with `Default` — a default is what makes an attribute optional. |

Literal syntax per type is the obvious one: `strconv` for `int`/`bool`/`float`, `time.ParseDuration` for `duration` (`600ms`), `#rgb`/`#rrggbb` for `color`. `any` is the escape hatch for app types that have no markup literal; it accepts whatever handle the parent holds, unchecked, and takes no `Default`.

### What happens at the instantiation site

Each declaration resolves one of three ways:

- **Attribute bound** (`Value="{{.Reqs}}"`) — the parent's existing handle passes straight through, type-checked against `Type`. Nothing is copied, so the control and the page share one node.
- **Attribute literal** (`Title="requests"`) — coerced by `Type` and wrapped as a fresh source.
- **Attribute absent** — a fresh **per-instance** source carrying the declared `Default`: markup-defined, typed, bindable local state. Two instances of the control do not share it. Absent plus `Required` is a load error.

### Strict mode

Declaring anything at all makes the control strict: an attribute the control did not declare is a load error, because the declarations are now its public surface. Layout attributes, `Name` and `Tooltip` are the *element's*, never the control's, so they are always allowed.

```
markup: <Card Captoin="per second">: card.gooey declares no dependency property
"Captoin" (declared: Caption, Title, Trend, Value)
```

A file with no declarations keeps the implicit pass-through described above, unchanged. Error messages use the phrase **dependency property** throughout, because that is what these are:

```
markup: card.gooey: dependency property "Title" — required attribute missing on <Card>
markup: card.gooey: dependency property "Value" — Value="{{.Reqs}}" is *prop.Property[string]; Type="int" needs *prop.Property[int]
```

### With a code-behind

Declarations own the public surface; a setup func owns private members and behavior, and runs *second*. Inside setup, `parent.DeclaredProperties()` returns the already-resolved handles, so a control can build private computeds over its own declared properties:

```go
setup := func(e markup.Element, parent *markup.Context) (*markup.Context, error) {
    title := parent.DeclaredProperties()["Title"].(*prop.Property[string])
    return &markup.Context{Values: map[string]any{
        "Shout": prop.NewComputed(func() string { return strings.ToUpper(title.Get()) }),
    }}, nil
}
```

The framework installs the declared properties into the control's context afterwards, so setup must not copy them in itself: a `Values` entry colliding with a declared name is a load error, for the same reason a property system rejects double registration. Change callbacks need no mechanism — a computed reading a declared handle, or `OnInvalidate` on it, *is* the callback.

This makes three control tiers, each adding exactly one thing: Include (implicit surface, no behavior) → declarations (checked surface, no behavior) → declarations + code-behind (checked surface + private behavior).

### Known wrinkle: hot reload resets declared defaults

A declared default materializes a *fresh* source each time the control is instantiated, and a hot reload re-instantiates every control. So state living in a defaulted property resets on reload, while state living in the app's viewmodel (the usual place) survives as it always has. The fix is `Name`-keyed state adoption across rebuilds, which is designed but not implemented; see the [decision record](specs/2026-08-10-markup-declared-properties.md), and [#51](https://github.com/WonderForgeLabs/gooey/issues/51) under epic [#50](https://github.com/WonderForgeLabs/gooey/issues/50) for whether that is still true.

### Explicitly out of scope

Markup-declarable **attached** properties — a markup-only panel defining its own attachment slots — would need a dynamic per-element property bag on `Base`, reintroducing stringly-typed storage. Attached properties stay host-type-defined (`Grid.Row` lives in `Layout`).

## Named elements and Find

`Name="..."` on any element registers the built component in `Context.Named`, the code-behind lookup surface. `markup.Find` retrieves it with its concrete type:

```xml
<Text Grid.Row="2" Name="stats" Style="dim"></Text>
```

```go
stats, _ := markup.Find[*components.Text](ctx, "stats")
stats.Content.Set(fmt.Sprintf("lines arrived=%d   frames=%d", logdata.Count(), frames))
```

(from `cmd/markuplog`)

A wrong name or wrong type is an error, not a nil. `Watch` resets the `Named` map on each rebuild, so re-Find after a hot reload (the markuplog main loop does exactly this on swap). Inside a UserControl, names are per-instance and invisible to the page.

## Gesture syntax

`KeyBinding Gesture` values are parsed by `input.ParseGesture`. The shape is zero or more `+`-separated modifiers followed by a key; the key is whatever follows the last `+`. Modifier order does not matter; modifier and named-key matching is case-insensitive.

Modifiers:

| Spelling | Modifier |
|---|---|
| `ctrl`, `control`, `c` | Ctrl |
| `alt`, `meta`, `option` | Alt |
| `shift` | Shift |

Keys:

- Named keys: `enter`, `tab`, `esc`, `backspace`, `delete`, `up`, `down`, `left`, `right`, `home`, `end`, `pageup`, `pagedown`, and `space`.
- Any single rune: `j`, `q`, `/`, ...
- The `+` key itself is the one case that needs spelling out: `alt++`.

Examples from the demos: `q`, `esc`, `ctrl+c`, `enter`, `s`, `a`.

Three normalizations reflect what the terminal actually sends: `shift` on a printable character folds into the rune (`shift+j` matches `J`, and the shift modifier is dropped), `ctrl+<letter>` lowercases the letter (control bytes decode to the lowercase rune), and `ctrl+@` becomes `ctrl+space` (`0x00` is the byte for both, and space is what people press).

### A ctrl gesture the decoder cannot produce is a load error

`ctrl` does not reach every printable character, and the ones it cannot reach used to parse cleanly and then never fire — no error, no warning, nothing at runtime to tell the binding apart from a key you never pressed. They are refused at load time now ([#427](https://github.com/WonderForgeLabs/gooey/issues/427)), with a message naming the cause:

```
input: gesture "ctrl+j" never fires: a terminal sends 0x0a for it, which decodes as enter
```

Two rules produce the whole refused set, and both come from the decoder rather than from a list:

- **`ctrl` reaches only `@` through `_` and `a` through `z`.** A control byte decodes as `byte|0x40`, so every digit, most punctuation, and the braces are unreachable by construction — `ctrl+1`, `ctrl+,`, `` ctrl+` `` and `ctrl+~` among them.
- **Five spellings inside that range are claimed by a named key first.** `ctrl+h` is backspace, `ctrl+i` is tab, `ctrl+j` and `ctrl+m` are enter, and `ctrl+[` is esc — four letters and a bracket, not five letters. Those are the right calls, since people pressing backspace mean backspace, so bind the named key instead.

`alt+` on its own is unaffected: it is an ESC prefix rather than part of the byte, so `alt+j` and `alt+1` are fine. This is why the wysiwyg editor's move cluster is `alt+h/j/k/l` rather than the vim-shaped `ctrl+` spelling.

It does not rescue a ctrl gesture, though. A `ctrl+alt+…` spelling is judged on its **ctrl half**, so `ctrl+alt+j` and `ctrl+alt+1` are refused exactly as `ctrl+j` and `ctrl+1` are, while `ctrl+alt+s` is accepted because `ctrl+s` is. Adding the prefix changes how the byte is delivered, not whether there is a byte to deliver.

## Designed, not yet implemented

- `gooey gen` — compiled markup plus a typed per-control surface for compile-checked instantiation. `<x:Property>` declarations are the input it was waiting for: a declaration block is both a typed surface and, for the remote-behavior layer, a per-control wire schema. Tracked as epic [#59](https://github.com/WonderForgeLabs/gooey/issues/59).
- `Name`-keyed state adoption across hot reloads, so a declared default's per-instance source survives a rebuild (see [the wrinkle above](#known-wrinkle-hot-reload-resets-declared-defaults)).

For the project overview and demo GIFs, see [../README.md](../README.md).
