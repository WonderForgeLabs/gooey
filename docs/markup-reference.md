# Markup reference

The `markup` package is gooey's XAML-analog authoring surface: XML elements map to widgets, attributes to properties, and `{{...}}` expressions to bindings resolved against a property registry — no reflection anywhere. This page is the complete reference for the `.gooey` file format as implemented today. For how the widget/property machinery underneath works, see [architecture.md](architecture.md); for a first working app, see [getting-started.md](getting-started.md); the demo apps in `cmd/` are the living examples and are cataloged in [demos.md](demos.md).

A markup file is loaded against a `markup.Context` — the binding environment that supplies values, styles, custom widget builders, event handlers, and the include filesystem:

```go
ctx := &markup.Context{
    Values:   map[string]any{...},           // {{.Name}} roots
    Styles:   map[string]render.Style{...},  // Style="name" lookup
    Widgets:  map[string]markup.Builder{...},// custom elements
    Handlers: map[string]gooey.Command{...}, // bare-name event handlers
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

Both rules are enforced at build time. The default `xmlns` attribute is decorative versioning — the parser ignores its value. **Prefixed** namespaces are not decorative: they declare handler namespaces, and are captured per document into a prefix → URI table (see [handler namespaces](#handler-namespaces)).

## Built-in elements

### Border

Draws a rounded box around exactly one visual child (KeyBindings do not count against the one-child rule).

| Attribute | Meaning |
|---|---|
| `Title` | Text in the top edge. Bindable: `Title="{{.Title}}"` or a literal. |
| `Style` | Named style from `Context.Styles`, applied to the frame and title. |

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

```xml
<Button Content="serialize → json" Click="{{.Serialize}}"/>
```

### KeyBinding

A declared gesture — a non-visual element:

```xml
<KeyBinding Gesture="ctrl+c" Command="{{.Quit}}"/>
```

| Attribute | Meaning |
|---|---|
| `Gesture` | Key gesture, parsed by `input.ParseGesture` (syntax below). |
| `Command` | Binding or bare handler name, same resolution as `Click`. |

Attachment and scoping semantics: a KeyBinding is never laid out or painted. The builder hangs it off its parent element as an attachment (any element that embeds `gooey.Base` can host one — a Grid, Border, stack, or custom widget). Key dispatch starts at the focused widget and walks up its ancestor chain to the root; at each level the KeyBindings attached there are matched first, then that widget's own key handler. So:

- A binding declared on the page root is effectively global — every focused widget's chain passes through the root. The `q`/`esc`/`ctrl+c` bindings in `cmd/reader/reader.gooey` work this way.
- A binding declared inside a control fires only while focus is inside that control. `cmd/reader/storylist.gooey` attaches `<KeyBinding Gesture="enter" Command="{{.Open}}"/>` to the story pane's Border, so enter opens a story only while that pane has focus.
- The first consumer stops propagation, and unconsumed `tab`/`shift+tab` move focus — so either can be overridden by binding or handling it.

### Image

`gooey.Image` (a cell-region image drawn via the graphics planes, with halfblock fallback) exists as a widget but has no built-in markup element yet — the pixel pipeline predates the property model, so its fields are plain Go values. To use it from markup today, register it as a custom widget.

### Canvas

`gooey.Canvas` — absolute positioning. Children go wherever their `Canvas.Left`/`Canvas.Top` attached properties say, at their own desired size. It takes no attributes of its own.

```xml
<Canvas>
  <ColorPicker Value="{{.Accent}}" Canvas.Left="1" Canvas.Top="1"/>
  <Text Canvas.Left="46" Canvas.Top="0" Style="dim">a caption, placed exactly</Text>
</Canvas>
```

A child is measured against the space remaining from its offset, so one placed near the right edge clips its own content rather than overhanging. Children may overlap; paint order is tree order, so a later sibling paints over an earlier one.

One caveat worth knowing before you overlap things deliberately: damage tracking is per widget, and a leaf clears its own rect before repainting. If an *occluded* widget repaints on its own, it paints over the sibling that was covering it, and that sibling — being clean — does not repaint. Overlapping children are safe when they change together (as in `cmd/colordemo`, where every swatch derives from one property) or when the occluded one is static.

### Checkbox

`gooey.Checkbox` — a focus stop rendering `[x] label`, toggled by space, enter, or a click.

| Attribute | Meaning |
|---|---|
| `Checked` | **Required binding** to a `*prop.Property[bool]`. Shared with the viewmodel, not copied: Render reads it, the toggle Sets it. |
| `Label` | Text after the box. Bindable or literal. |
| `Style` | Named style or a bound style. |

### Gauge

`gooey.Gauge` — a labelled 0-100 meter, colored by a shared threshold ramp (green below 50, amber at 50, red at 80).

| Attribute | Meaning |
|---|---|
| `Value` | **Required binding** to a `*prop.Property[int]`, clamped to 0-100 on read. |
| `Label` | Text before the bar. Bindable or literal. |
| `BarWidth` | Preferred width in cells; absent = 34. |
| `Style` | Overrides the threshold ramp entirely when present. |

### Sparkline

`gooey.Sparkline` — a series of 0-100 values as stacked block rows, most recent on the right, colored per column by the same ramp.

| Attribute | Meaning |
|---|---|
| `Values` | **Required binding** to a `*prop.Property[[]float64]`. |
| `Height` | Rows tall; absent = 1. |
| `BarWidth` | Preferred width in cells; absent = 40. |
| `Style` | Overrides the threshold ramp. |

The series is tail-cropped to the arranged width, so a narrower window shows recent history rather than compressing all of it.

### ColorPicker

`gooey.ColorPicker` — an interactive RGB editor, and the worked example of a widget that adapts to the terminal it landed on.

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

`gooey.TextBox` — a single-line editor and a focus stop. It owns printable runes and the editing keys while focused; everything else bubbles, so page gestures still work from inside the field.

| Attribute | Meaning |
|---|---|
| `Text` | **Required binding** to a `*prop.Property[string]`, shared with the viewmodel. |
| `Prompt` | Optional prefix drawn before the text, e.g. `Prompt="&gt; "`. Bindable or literal. |
| `Style` | Style of the edited text. Named or bound. |
| `AccentStyle` | Named style for the prompt and caret. |
| `Changed` | Optional command run after every edit (not after caret moves) — for invalidating something derived. |

Keys: printable runes insert at the caret, `backspace`/`delete` remove either side of it, `←`/`→` move it, `home`/`end` jump. A click places the caret. The field scrolls horizontally to keep the caret visible, and the caret is a source property, so moving it repaints only this widget.

### Timer

`gooey.Timer` — a non-visual element that runs a command on an interval. Like `KeyBinding` it is hosted as an attachment on its parent, never laid out or painted.

```xml
<Timer Interval="600ms" Tick="{{.Advance}}" Enabled="{{.Running}}"/>
```

| Attribute | Meaning |
|---|---|
| `Interval` | **Required.** Any `time.ParseDuration` string (`"600ms"`, `"2s"`). Missing, unparseable, or non-positive is a load error. |
| `Tick` | The command, resolved like `Click` — a binding or a bare handler name. |
| `Enabled` | Optional binding to a `*prop.Property[bool]`. Absent means always enabled. |

Two things make it safe. The ticker goroutine never touches the property graph: it **posts** the tick to the `Dispatcher` and the app's loop runs it, so by the time `Tick` executes it is ordinary UI-goroutine code. And `Enabled` is read at fire time, on the loop, for the same reason — which is what lets the graph pause a timer, since binding it to the property a checkbox toggles stops the timer without tearing anything down.

Lifetime belongs to the `Composer`, not the widget. Timers do not run until `Composer.Start(dispatcher)`, and `Composer.Close()` stops them. Hot reload builds a new composition, so the outgoing one must be closed or its ticker keeps running against a viewmodel nobody is showing:

```go
disp := gooey.NewDispatcher()
attach := func(w gooey.Widget) {
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

Every element whose widget embeds `gooey.Base` (all built-ins and any well-behaved custom widget) accepts the FrameworkElement attributes. They map onto the widget's `Layout` and are honored by the shared measure/arrange sandwich, so they work identically inside any container.

| Attribute | Values | Meaning |
|---|---|---|
| `Width`, `Height` | integer cells | Explicit size; 0/absent = auto. |
| `Margin` | 1, 2, or 4 comma-separated integers | `"1"` = all four sides; `"2,0"` = horizontal, vertical; `"2,0,0,0"` = left, top, right, bottom. |
| `HAlign`, `VAlign` | `Stretch` (default), `Start`, `Center`, `End` | Alignment inside the layout slot. Stretch fills the slot; the others use the measured desired size. |
| `Visibility` | `Visible` (default), `Hidden`, `Collapsed` | Hidden occupies space but does not paint; Collapsed occupies nothing (and its subtree is skipped by focus traversal). |
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

Resolution happens once, at build time, to property handles — not values. This is the lvalue semantics of the design: the built widget holds the handles, and evaluation at render time does no lookups. Mixed content becomes a single computed string property over its parts, so setting any bound source property repaints exactly the widgets that read it — there is no refresh call anywhere.

### Event bindings

Event attributes (`Button Click`, `KeyBinding Command`) resolve in one of three ways — the event-binding split:

- Handler-expression form — `Click="{{net:Get .Url | into .Body}}"` names a function in a declared handler namespace, so the behavior itself is declared in markup with no delegate anywhere. See [handler namespaces](#handler-namespaces).

- Binding form — `Click="{{.Save}}"` resolves a value in `Context.Values`, which must be a `gooey.Command` or a `func()`. The delegate lives in the viewmodel, so markup-only controls can wire events with no code-behind at all. This is the form all the `cmd/` demos use:

  ```go
  Values: map[string]any{
      "Increment": gooey.Command(func() { count.Set(count.Get() + 1) }),
      "Quit":      gooey.Command(func() { app.Quit() }),
  }
  ```

- Bare-name form — `Click="OnSave"` resolves against `Context.Handlers`, the code-behind handler registry. An unregistered name is a build error.

An empty event attribute is not an error — the element simply has no command.

### Attribute bindings on custom elements

On custom widgets, UserControls, and Includes, an attribute like `Stories="{{.Stories}}"` is resolved via `Context.BindingValue`, which returns the raw context value — typically a typed `*prop.Property[T]` handle of any `T`, not just string. The receiving code type-asserts it. This is how non-string data crosses element boundaries.

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

**Registration is the capability grant.** Markup can only invoke namespaces the host app registered; drop a registration and the same document stops loading, naming the URI it wanted. That is what makes markup loaded from an untrusted `fs.FS` safe to run: it reaches exactly the capabilities its host chose to hand it, and nothing else.

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

Nothing in the provider knows which widgets display the result. The `Set` dirties whatever read the property, and the next frame repaints exactly those.

### Providers

| Namespace URI | Package | Functions |
|---|---|---|
| `gooey.dev/handlers/net` | `handlers/net` | `Get .Url` — HTTP GET, body as a string |
| `gooey.dev/handlers/temporal` | `handlers/temporal` (separate module) | ` Activity `Name` .Arg` — a Temporal standalone activity |

Both deliver failures into the same target as an `"ERROR: …"` string in v1, so a page can show what went wrong without a second binding.

A provider is a typed factory — `NewCommand(*markup.Call) (gooey.Command, error)` — with no reflection: arguments arrive as resolved handles, and a provider needing a type other than string type-switches on `Arg.Raw`.

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

Setting `accent` dirties `accentStyle`, which dirties exactly the widgets that read it while painting, and they repaint. No styling system is involved — it is the ordinary property graph, and it is as close to theming as gooey currently gets. `cmd/colordemo` styles its border, title, and swatches this way from the color being edited. `Text Bold="true"` composes over either form.

## Custom widgets

`Context.Widgets` maps an element name to a `Builder` — `func(e Element, ctx *Context) (gooey.Widget, error)`. A registered builder wins over everything, receives the raw element, and interprets attributes however it likes:

```go
Widgets: map[string]markup.Builder{
    "LogPane": func(e markup.Element, _ *markup.Context) (gooey.Widget, error) {
        return &logPane{src: visible}, nil
    },
}
```

```xml
<LogPane Grid.Row="3" Lines="{{.Visible}}"/>
```

(from `cmd/markuplog`)

The universal layout attributes are applied by the framework after the builder returns, so a custom widget that embeds `gooey.Base` gets `Margin`, `Grid.Row`, and the rest for free. A builder that wants typed data uses `ctx.BindingValue` — see the `Checkbox` builder in `cmd/statedemo/main.go`, which resolves `Checked="{{.Auto}}"` to a `*prop.Property[bool]` and binds it two-way (render reads it, toggling Sets it).

## UserControls

`markup.UserControl(fsys, "storylist.gooey", setup)` wraps a markup file plus a code-behind setup function as a Builder, so a control registers like any custom widget and instantiates as an element:

```xml
<StoryList Grid.Col="1" Stories="{{.Stories}}" Selected="{{.SelStory}}"
           Read="{{.Read}}" Open="{{.OpenStory}}" Title="stories"/>
```

(from `cmd/reader/reader.gooey`)

Context isolation is the contract: `setup(e, parent)` returns the instance's own `Context`, and bindings inside the control's markup resolve against it — never against the page. Data crosses the boundary through element attributes, resolved in the parent context:

- `parent.BindingValue(e.Attrs["Stories"])` returns the parent's property handle, which setup type-asserts and wires into its context or widgets. Bindings are live handles, not copied values.
- `parent.Command(e.Attrs["Open"])` resolves an event attribute the same way `Click` does; the control can then hand the command to a widget or expose it in its own context (storylist puts `Open` in its context so its markup can attach it to a `<KeyBinding>`).
- Literal attributes arrive as plain strings (`Title="stories"`).

`Styles`, `Widgets`, `Handlers`, and `Includes` inherit from the parent context when the child leaves them nil; `Named` is scoped per instance (like `x:Name` in templates). Layout attributes on the instance element apply to the instance and are not passed through.

`cmd/reader/controls.go` is the canonical example — three controls, each a `.gooey` file wrapping a per-instance rows widget, with a generic `attr[T]` helper for the typed hand-off. `markup.WatchAll` covers hot reload for the whole set: one page rebuild re-instantiates every control.

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

Element resolution order, in full: registered `Widgets` builder, then built-in element, then Includes convention, then error.

## Named elements and Find

`Name="..."` on any element registers the built widget in `Context.Named`, the code-behind lookup surface. `markup.Find` retrieves it with its concrete type:

```xml
<Text Grid.Row="2" Name="stats" Style="dim"></Text>
```

```go
stats, _ := markup.Find[*gooey.Text](ctx, "stats")
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

Two markup features have settled designs but no implementation yet — see [specs/](specs/) for the decision records:

- `x:Property` declarations — a `.gooey` file declaring its own typed, defaulted, bindable property surface, making declared markup properties ordinary dependency properties; [specs/2026-08-10-markup-declared-properties.md](specs/2026-08-10-markup-declared-properties.md).
- DataTemplates — declaring item visuals in markup for list-shaped data; today every list is a hand-rendered custom rows widget behind a UserControl, the pattern established in [specs/2026-08-10-reader-design.md](specs/2026-08-10-reader-design.md).

For the project overview and demo GIFs, see [../README.md](../README.md).
