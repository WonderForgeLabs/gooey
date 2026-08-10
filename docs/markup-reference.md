# Markup reference

The `markup` package is gooey's XAML-analog authoring surface: XML elements map to components, attributes to properties, and `{{...}}` expressions to bindings resolved against a property registry — no reflection anywhere. This page is the complete reference for the `.gooey` file format as implemented today. For how the component/property machinery underneath works, see [architecture.md](architecture.md); for a first working app, see [getting-started.md](getting-started.md); the demo apps in `cmd/` are the living examples and are cataloged in [demos.md](demos.md).

A markup file is loaded against a `markup.Context` — the binding environment that supplies values, styles, custom component builders, event handlers, and the include filesystem:

```go
ctx := &markup.Context{
    Values:   map[string]any{...},           // {{.Name}} roots
    Styles:   map[string]render.Style{...},  // Style="name" lookup
    Components:  map[string]markup.Builder{...},// custom elements
    Handlers: map[string]gooey.Action{...},  // bare-name event handlers
    Includes: fsys,                          // convention-based controls
}
tree, err := markup.Load(fsys, "app.gooey", ctx)
```

`Load` reads from any `fs.FS` — `os.DirFS` in development, `embed.FS` in release; the loader cannot tell the difference. `markup.Watch` (single file) and `markup.WatchAll` (a set of files) poll ModTimes and rebuild on change, which is the hot-reload path: edit the file while the app runs and the tree rebuilds in place, with all state intact because the viewmodel properties are the durable thing and the tree is disposable (see `cmd/markuplog`, [../markuplog.gif](../markuplog.gif)). On an immutable FS this degrades to a natural no-op. Parse or build errors during a reload leave the current tree in place.

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

(from `cmd/statedemo/statedemo.gooey`)

### Text

A text block. The content is the element's text, with surrounding whitespace trimmed. Content may be a pure literal, a pure binding, or a mix (see the binding DSL below).

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

Attachment and scoping semantics: a KeyBinding is never laid out or painted. The builder hangs it off its parent element as an attachment (any element that embeds `gooey.Base` can host one — a Grid, Border, stack, or custom component). Key dispatch starts at the focused component and walks up its ancestor chain to the root; at each level the KeyBindings attached there are matched first, then that component's own key handler. So:

- A binding declared on the page root is effectively global — every focused component's chain passes through the root. The `q`/`esc`/`ctrl+c` bindings in `cmd/reader/reader.gooey` work this way.
- A binding declared inside a control fires only while focus is inside that control. `cmd/reader/storylist.gooey` attaches `<KeyBinding Gesture="enter" Command="{{.Open}}"/>` to the story pane's Border, so enter opens a story only while that pane has focus.
- The first consumer stops propagation, and unconsumed `tab`/`shift+tab` move focus — so either can be overridden by binding or handling it.

Before any of that, the event **tunnels**: every ancestor from the root down to the focused component that implements `gooey.PreviewKeyHandler` is offered the event first, and the first to take it ends the dispatch — no target handling, no bubbling, no bindings. `gooey.PreviewMouseHandler` does the same for pointer events. This is the parent-veto mechanism: a modal scrim swallows what is aimed at the layer underneath without any of those components being consulted. The full order is **tunnel down → target and bubble up (bindings then handler at each level) → app fallbacks**.

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

Literal `Src` decodes through the `imaging` registry: **png, jpeg, gif, bmp, ico** in core (GIF shows its first frame — animation is a player's job, see the browser demo's gifplay; ICO decodes its largest entry). **SVG** needs the nested module — blank-import `github.com/WonderForgeLabs/gooey/imagefmt/svg` and `.svg` paths rasterize at their intrinsic size (capped at 1024 px). Formats are sniffed by content, not extension.

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
| `Style` | Named style or a bound style. |

```xml
<Segmented Options="Idle | Fetch | Build | Deploy" Selected="{{.StageIndex}}" Changed="{{.StageChanged}}"/>
```

`←`/`→` step the selection and, as with `Toggle`, are consumed only while there is somewhere to move — so an arrow at either end leaves the control instead of dead-ending in it. `home`/`end` jump to the ends, space and enter cycle (wrapping), and a click selects the segment under the pointer. The same conditional-`Changed` disable rule applies.

### StatusBar

`components.StatusBar` — the bottom row every demo used to hand-roll as a dim `Text` with the spaces counted by hand: three sections, one against each edge and one in the middle.

Each section takes either form, and giving one section both is a load error:

| Form | Meaning |
|---|---|
| `Left` / `Center` / `Right` attribute | Shorthand for "a dim line of text". Bindable or literal. |
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

### MenuBar

`components.MenuBar` — the top menu row: titles across one line, and a dropdown overlay below the open title.

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

Cut and copy use a kill buffer shared by every TextBox in the process — `components.KillBuffer` / `components.SetKillBuffer`. It is deliberately not the system clipboard; reaching that means OSC 52, which is a decision to make on purpose rather than a side effect of adding cut and paste.

The field scrolls horizontally to keep the caret visible in either direction, and the caret and the selection anchor are source properties, so moving the caret repaints only this component.

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
| `Activate` | Command run on enter, on a double click, and on a second click of the already-selected row; resolved like `Click`. |

`<ItemsView.ItemTemplate>` is required and takes exactly one child element. The view is a focus stop with the house list keys — `↑`/`↓`/`j`/`k`, `PgUp`/`PgDn`, `Home`/`End`, `enter` — plus wheel, click to select, and a second click to activate. Keys it does not use bubble, so page-level `<KeyBinding>`s still work while the list has focus.

**The template is a factory, not a tree.** Its element subtree is captured at load and instantiated once per item, against a context whose values are *that item's* — dot is the ITEM. Page values are deliberately out of reach inside a template, the same isolation a UserControl gets; anything a row needs must come through the projection. Everything else the document carries — styles, registered components, handlers, includes, the `xmlns` table — is inherited, so a template may place a registered custom component exactly like any other markup.

**Items come from a projection.** Without reflection, gooey cannot walk a struct's fields, so the app says what a row is made of:

```go
"Rows": components.Items(stories, func(s Story) map[string]any {
    return map[string]any{"Title": s.Title, "Published": s.Published}
}),
```

The map's keys are what the template's bindings resolve against; its values become property handles the view Sets as the item changes. `string`, `bool`, `int`, `float64`, `render.Style` and `render.Color` become live handles; anything else crosses as a fixed literal for the life of the row (useful for a `gooey.Command`, not for anything that changes).

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

`cmd/cardsdemo` drives its whole data stream this way.

## Universal layout attributes

Every element whose component embeds `gooey.Base` (all built-ins and any well-behaved custom component) accepts the FrameworkElement attributes. They map onto the component's `Layout` and are honored by the shared measure/arrange sandwich, so they work identically inside any container.

| Attribute | Values | Meaning |
|---|---|---|
| `Width`, `Height` | integer cells | Explicit size; 0/absent = auto. |
| `Margin` | 1, 2, or 4 comma-separated integers | `"1"` = all four sides; `"2,0"` = horizontal, vertical; `"2,0,0,0"` = left, top, right, bottom. |
| `HAlign`, `VAlign` | `Stretch` (default), `Start`, `Center`, `End` | Alignment inside the layout slot. Stretch fills the slot; the others use the measured desired size. |
| `Visibility` | `Visible` (default), `Hidden`, `Collapsed`, or a `{{...}}` binding | Hidden occupies space but does not paint; Collapsed occupies nothing (and its subtree is skipped by focus traversal). The bound form accepts a `*prop.Property[gooey.Visibility]` or a `*prop.Property[bool]` (true→Visible, false→Collapsed); a `Set` repaints exactly what the literal flip repaints. |
| `Grid.Row`, `Grid.Col` | integer | Cell address when the parent is a Grid — the attached-property syntax. |
| `Grid.RowSpan`, `Grid.ColSpan` | integer | Cells spanned; 0/absent means 1. |
| `Canvas.Left`, `Canvas.Top` | integer cells | Offset from the parent Canvas's top-left corner — the attached-property syntax again. |

The `Grid.*` and `Canvas.*` attributes live on the child, XAML-style; they are stored in the element's own `Layout` (Go has no attached-property store, so the element itself is it) and are simply inert when the parent is not the matching panel. Both are also excluded from the attribute hand-off into an Include, since they position the instance rather than describing it.

## The binding DSL

A binding is `{{.Path}}` — Go-template spelling, where `Path` is a dot-separated lookup through `Context.Values` (nested `map[string]any` levels for the dots).

Text content and text-valued attributes (`Text` content, `Border Title`, `Button Content`) accept mixed literal and binding parts:

```xml
<Text>lines: {{.Count}} ({{.State}})</Text>
```

Each `{{.Path}}` must resolve to a `*prop.Property[string]` (a live handle) or a plain `string` (a static splice); anything else is a build error, as is a path that does not resolve.

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

On custom components, UserControls, and Includes, an attribute like `Stories="{{.Stories}}"` is resolved via `Context.BindingValue`, which returns the raw context value — typically a typed `*prop.Property[T]` handle of any `T`, not just string. The receiving code type-asserts it. This is how non-string data crosses element boundaries.

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

**Registration is the capability grant.** Markup can only invoke namespaces the host app registered; drop a registration and the same document stops loading, naming the URI it wanted. That is what makes markup loaded from an untrusted `fs.FS` safe to run: it reaches exactly the capabilities its host chose to hand it, and nothing else — no markup syntax registers a provider or widens a grant, so a document can never expand its own capabilities. The full doctrine (pack taxonomy, module boundaries, grant scopes) is [docs/specs/2026-08-10-pack-distribution.md](specs/2026-08-10-pack-distribution.md).

### Grammar

```
{{prefix:Func arg… | into .Target}}
```

- `prefix` resolves through the document's own xmlns table. Namespaces are **per document** — an Include or UserControl declares its own and cannot inherit the page's, so a control's capabilities never depend on who included it.
- Arguments are the DSL's usual atoms: `` `backtick literal` `` (a constant string) and `.Path` (a property handle). Bound arguments are read **at invoke time**, not at load — the same lvalue semantics as every other binding, so re-pointing `.Url` changes what the next press fetches.
- `| into .Target` names the `*prop.Property[string]` the result is written to. It is the only pipeline stage in v1.

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
| `gooey.dev/handlers/temporal` | `handlers/temporal` (separate module) | ` Activity `Name` .Arg` — a Temporal standalone activity |

Both deliver failures into the same target as an `"ERROR: …"` string in v1, so a page can show what went wrong without a second binding.

A provider is a typed factory — `NewCommand(*markup.Call) (gooey.Command, error)` — with no reflection: arguments arrive as resolved handles, and a provider needing a type other than string type-switches on `Arg.Raw`.

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

## Styles

`Style="name"` looks the name up in `Context.Styles` (`map[string]render.Style` — fg/bg color, bold, and so on), registered by the app:

```go
Styles: map[string]render.Style{
    "panel":  {Fg: render.RGB(120, 90, 220)},
    "accent": {Fg: render.RGB(255, 170, 60), Bold: true},
    "dim":    {Fg: render.RGB(140, 140, 150)},
}
```

Be honest about what this is: a named lookup, not a styling system. There is no cascading, no inheritance, no per-property overrides in markup (except `Text Bold`), no selectors, and an unknown style name silently yields the zero style. It exists so markup files do not embed raw colors.

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

Setting `accent` dirties `accentStyle`, which dirties exactly the components that read it while painting, and they repaint. No styling system is involved — it is the ordinary property graph, and it is as close to theming as gooey currently gets. `cmd/colordemo` styles its border, title, and swatches this way from the color being edited. `Text Bold="true"` composes over either form.

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

The universal layout attributes are applied by the framework after the builder returns, so a custom component that embeds `gooey.Base` gets `Margin`, `Grid.Row`, and the rest for free. A builder that wants typed data uses `ctx.BindingValue` — see the `Checkbox` builder in `cmd/statedemo/main.go`, which resolves `Checked="{{.Auto}}"` to a `*prop.Property[bool]` and binds it two-way (render reads it, toggling Sets it).

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

(from `cmd/cardsdemo`, the whole demo's Go file has no control code in it at all)

**A declared markup property is an ordinary dependency property, registered from markup.** Each declaration materializes exactly what a code-behind would have wired by hand — a `*prop.Property[T]` node in the same graph, read by the same `Get`, invalidated by the same `Set`. There is one property system; this is its markup tier, the way `DependencyProperty.Register` is its code tier.

### Declaration attributes

| Attribute | Meaning |
|---|---|
| `Name` | Required. The attribute callers set, and the path the control's own markup binds (`{{.Title}}`). Cannot be `Name` or a layout attribute — those belong to the element. |
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

Declaring anything at all makes the control strict: an attribute the control did not declare is a load error, because the declarations are now its public surface. Layout attributes and `Name` are the *element's*, never the control's, so they are always allowed.

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

A declared default materializes a *fresh* source each time the control is instantiated, and a hot reload re-instantiates every control. So state living in a defaulted property resets on reload, while state living in the app's viewmodel (the usual place) survives as it always has. The fix is `Name`-keyed state adoption across rebuilds, which is designed but not implemented; see the [decision record](specs/2026-08-10-markup-declared-properties.md).

### Explicitly out of scope

Markup-declarable **attached** properties — a markup-only panel defining its own attachment slots — would need a dynamic per-element property bag on `Base`, reintroducing stringly-typed storage. Attached properties stay host-type-defined (`Grid.Row` lives in `Layout`).

## Named elements and Find

`Name="..."` on any element registers the built component in `Context.Named`, the code-behind lookup surface. `markup.Find` retrieves it with its concrete type:

```xml
<Text Grid.Row="2" Name="stats" Style="dim"></Text>
```

```go
stats, _ := markup.Find[*components.Text](ctx, "stats")
stats.Content.Set(fmt.Sprintf("lines arrived=%d   frames=%d", lineCount, frames))
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
- The `+` key itself is the one case that needs spelling out: `ctrl++`.

Examples from the demos: `q`, `esc`, `ctrl+c`, `enter`, `s`, `a`.

Two normalizations reflect what the terminal actually sends: `shift` on a printable character folds into the rune (`shift+j` matches `J`, and the shift modifier is dropped), and `ctrl+<letter>` lowercases the letter (control bytes decode to the lowercase rune).

## Designed, not yet implemented

- `gooey gen` — compiled markup plus a typed per-control surface for compile-checked instantiation. `<x:Property>` declarations are the input it was waiting for: a declaration block is both a typed surface and, for the remote-behavior layer, a per-control wire schema.
- `Name`-keyed state adoption across hot reloads, so a declared default's per-instance source survives a rebuild (see [the wrinkle above](#known-wrinkle-hot-reload-resets-declared-defaults)).

For the project overview and demo GIFs, see [../README.md](../README.md).
