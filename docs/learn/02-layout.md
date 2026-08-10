# Tutorial 2: Lay out a page with Grid

In this tutorial you build a three-column page with a `Grid`, size the
columns with Fixed, Auto, and Star tracks, control individual elements
with the layout attributes every gooey element accepts, and give each
track its own colors so the structure is visible at a glance.

**Time:** about 20 minutes.
**Prerequisites:** [Tutorial 1](01-first-app.md).

When you finish, you will have this:

![A three-column grid: a blue-filled fixed 24-cell column, a green-filled 1-star column, and a magenta-framed 2-star column twice its width](media/02-layout.png)

The finished code is in
[`docs/learn/examples/02-layout`](examples/02-layout). Everything in this
tutorial happens in `app.gooey`; the Go file is tutorial 1's, plus the
style map entries that step 6 explains.

## Step 1: Declare the grid

Replace the body of `app.gooey` with a `Grid`. Rows and columns are
declared on the grid; children say which cell they go in.

```xml
<Gooey xmlns="wonderforge.io/gooey/2026">
  <Grid Rows="Auto,*,Auto" Cols="24,1*,2*">
    <Text Grid.Row="0" Grid.ColSpan="3" Style="accent">a header spanning all three columns</Text>
    <Text Grid.Row="2" Grid.ColSpan="3" Style="dim">press q to quit</Text>

    <KeyBinding Gesture="q" Command="{{.Quit}}"/>
    <KeyBinding Gesture="ctrl+c" Command="{{.Quit}}"/>
  </Grid>
</Gooey>
```

Each entry in `Rows` and `Cols` is one of three kinds:

| Definition | Kind | Sizing |
|---|---|---|
| `Auto` | Auto | Sizes to the largest desired size among its span-1 children. Case-insensitive. |
| `24` | Fixed | Exactly that many cells. |
| `2*`, `*` | Star | A weighted share of what is left after Fixed and Auto tracks. Bare `*` means `1*`. |

Omitting `Rows` or `Cols` entirely gives you a single star track.

> **If you know XAML:** this is `RowDefinitions`/`ColumnDefinitions` with
> `GridLength` semantics, compressed into one attribute. `Auto` is
> `GridLength.Auto`, `24` is an absolute length, `2*` is
> `new GridLength(2, GridUnitType.Star)`. There is no `<Grid.RowDefinitions>`
> element — the string *is* the collection.

## Step 2: Place children with the attached properties

`Grid.Row`, `Grid.Col`, `Grid.RowSpan`, and `Grid.ColSpan` go on the
**child**, exactly as in XAML. Add three panels to the middle row:

```xml
<Border Grid.Row="1" Grid.Col="0" Title="24" Style="fixed" Background="#1c2b4a">
  <VStack Gap="1">
    <Text Style="fixed">a Fixed track:</Text>
    <Text Style="fixed">always 24 cells,</Text>
    <Text Style="fixed">whatever the width</Text>
  </VStack>
</Border>

<Border Grid.Row="1" Grid.Col="1" Title="1*" Style="one" Background="#1d3a2a">
  <VStack Gap="0">
    <Text Style="one">one share of</Text>
    <Text Style="one">what is left</Text>
  </VStack>
</Border>

<Border Grid.Row="1" Grid.Col="2" Title="2*" Style="two">
  <VStack Gap="0">
    <Text Style="two">two shares — twice as wide as 1*</Text>
  </VStack>
</Border>
```

Each panel gets a differently colored frame (`Style`), and the first two
get their own fill (`Background`), so the three tracks read apart
without counting cells — where the names come from, what else they can
say, and why the `2*` panel deliberately has no fill is
[step 6](#step-6-style-what-you-see). Structure first.

Defaults and clamping, so you are not surprised:

- A missing `Grid.Row`/`Grid.Col` means row 0, column 0.
- A missing or zero span means 1.
- Indexes and spans past the declared tracks are **clamped**, not an
  error. `Grid.Col="9"` in a three-column grid lands in column 2.
- `Grid.*` on a child whose parent is not a `Grid` is simply inert.

> **If you know XAML:** Go has no attached-property store, so
> `Grid.Row` is stored in the child's own `Layout` struct. The authoring
> experience matches XAML; the storage does not. It is why the attributes
> are inert rather than an error outside a grid.

## Step 3: Read the star-sizing story

Run it and count cells. Star tracks are resolved during Arrange, against
the width the grid actually got:

1. Fixed and Auto tracks are subtracted first.
2. What remains is divided by total star weight.
3. Each star track takes its weight's share, truncated to whole cells.
4. Rounding leftovers go to the **last** star track.

At the 84 columns of the screenshot, `Cols="24,1*,2*"` resolves to
84 − 24 = 60 cells to share, so `1*` gets 20 and `2*` gets 40. At 80
columns it is 56 to share: 18 and 38, the extra cell from truncation
landing on the last star track.

The practical rule: **a star track never sizes to its content.** It takes
what is offered. A grid with any star column asks for the full width it
was offered, so it fills its parent.

## Step 4: Control single elements with layout attributes

Every element whose component embeds `gooey.Base` — all built-ins, and any
well-behaved custom component — accepts the same layout attributes. They
work identically inside any container.

| Attribute | Values | Meaning |
|---|---|---|
| `Width`, `Height` | integer cells | Explicit size. 0 or absent means auto. |
| `Margin` | 1, 2, or 4 integers | `"1"` = all sides. `"2,0"` = horizontal, vertical. `"2,0,0,0"` = left, top, right, bottom. |
| `HAlign`, `VAlign` | `Stretch` (default), `Start`, `Center`, `End` | Position inside the layout slot. |
| `Visibility` | `Visible` (default), `Hidden`, `Collapsed` | See step 5. |

Add these to the `2*` panel to see each one:

```xml
<Text Margin="4,0,0,0" Style="accent">Margin="4,0,0,0"</Text>
<Text HAlign="Center" Style="accent">HAlign="Center"</Text>
<Text HAlign="End" Style="accent">HAlign="End"</Text>
```

`Stretch` fills the slot; every other alignment uses the size the element
asked for during Measure. That distinction is why `HAlign="Center"` moves
a `Text` but has no visible effect on something that already fills its
slot.

> **If you know XAML:** this is the FrameworkElement layer, and it
> behaves the way you expect — margin is subtracted before Measure and
> reapplied during Arrange, the classic measure/arrange sandwich.
> `Margin="2,0"` matches WPF's two-value form (horizontal, vertical).
> There is no `Padding`: a container that wants inner space gives its
> child a `Margin`.

## Step 5: Choose between Hidden and Collapsed

The two non-visible states differ in whether the element still occupies
space:

```xml
<VStack Gap="0">
  <Text>1 Visible</Text>
  <Text Visibility="Hidden">2 Hidden — line stays, blank</Text>
  <Text Visibility="Collapsed">3 Collapsed — no line at all</Text>
  <Text>4 sits right under the blank</Text>
</VStack>
```

Rendered, that is three lines: `1 Visible`, a blank line where the Hidden
text would be, then `4 sits right under the blank`. The Collapsed element
contributes nothing.

- **Hidden** measures and arranges normally but paints nothing. It keeps
  its full space — including its gap inside a stack.
- **Collapsed** measures to zero, arranges to zero, paints nothing, and
  costs **no gap either**: it neither brings a gap with it nor leaves one
  behind, so collapsing the first child does not strand a gap at the top.
  Its subtree is also skipped by focus traversal, so a collapsed panel
  cannot be tabbed into.

That is the whole distinction, and it is the reason to pick one over the
other: **Hidden reserves the space, Collapsed reclaims all of it.**

The `Gap="0"` above is for legibility, not necessity — it keeps the
three-line result easy to count. The same markup with `Gap="1"` behaves
the same way, just with blank rows between the entries.

### Binding visibility

`Visibility` binds like any other attribute — two handle types are
accepted:

```xml
<Border Visibility="{{.PanelState}}">…</Border>   <!-- *prop.Property[gooey.Visibility] -->
<Text   Visibility="{{.ShowDetails}}">…</Text>    <!-- *prop.Property[bool] -->
```

A `bool` maps true→`Visible`, false→`Collapsed` — show/hide state in a
viewmodel is almost always a bool, so the common case needs no
conversion (this is XAML's `BooleanToVisibilityConverter`, built in).
Bind a `*prop.Property[gooey.Visibility]` when you need `Hidden` — the
reserve-the-space state. A `Set` on the bound property repaints exactly
what a literal flip repaints: hiding erases the element, collapsing
relayouts its siblings, and nothing else redraws. The design is written
up in
[specs/2026-08-10-bindable-visibility.md](../specs/2026-08-10-bindable-visibility.md).

From Go you can also flip the plain field at runtime, and the Composer
notices:

```go
leaf, _ := markup.Find[*components.Text](ctx, "detail")
leaf.LayoutProps().Visibility = gooey.Hidden // repaints on the next frame
```

The Composer catches the change in its per-frame sweep and force-repaints
the component, the same way it handles a bounds change. This works for
containers too: a `Border` turned `Hidden` clears its whole bounds, and
the Composer's z-ordered repaint puts its still-visible children back
down on top in the same frame — visibility is per-element, so hiding a
container hides its chrome, not its subtree. Use `Collapsed` on the
container (or `Hidden` on the children) to take the content away.

## Step 6: Style what you see

The panels have been using `Style="fixed"` and `Background="#1c2b4a"`
since step 2. Time to say what those actually are.

### The style map

`Style="name"` is a lookup. The names live in Go, on the
`markup.Context` the page is built against:

```go
ctx := &markup.Context{
	Values: map[string]any{ /* … */ },
	Styles: map[string]render.Style{
		"fixed":  {Fg: render.RGB(110, 170, 255), Bg: render.RGB(0x1c, 0x2b, 0x4a), Bold: true},
		"one":    {Fg: render.RGB(120, 220, 150), Bg: render.RGB(0x1d, 0x3a, 0x2a), Bold: true},
		"two":    {Fg: render.RGB(230, 130, 220), Bold: true},
		"accent": {Fg: render.RGB(255, 170, 60), Bold: true},
		"dim":    {Fg: render.RGB(150, 150, 165)},
	},
}
```

A `render.Style` is the whole per-cell styling surface:

```go
type Style struct {
	Fg, Bg    Color // render.RGB(r, g, b); zero value = terminal default
	Bold      bool
	Dim       bool
	Underline bool
	Reverse   bool
}
```

Any element that draws styled text or chrome accepts `Style` — on a
`Text` it styles the runes, on a `Border` it colors the frame and title.
An unknown name is not an error; it resolves to the zero style, which is
the terminal default. That is worth knowing when a style silently fails
to appear: check the spelling against the map.

### Background is a container attribute, not a style

The panel fills come from `Background`, which `Border`, `Grid`,
`VStack`/`HStack`, and `Canvas` accept directly — a `#rgb`/`#rrggbb`
literal or a binding to a `*prop.Property[render.Color]`:

```xml
<Border Title="24" Style="fixed" Background="#1c2b4a">
```

The fill covers the container's **whole** bounds, including cells no
child owns. That is what the first panel's `VStack Gap="1"` shows: the
gap rows belong to no child, and they come out blue, not black holes.
Border chrome drawn with a style whose `Bg` is unset sits on the fill,
so a colored frame and a colored fill compose.

Text needs one more thing, and it is the reason the panel styles above
carry a `Bg`: **cells have no alpha.** A `Text` whose style leaves `Bg`
unset paints terminal-default cells even inside a filled panel — the
fill shows wherever the text's rectangle is blank, but the glyph cells
themselves would sit in a default-colored stripe. Text that wants to sit
flush on a fill says so in its own style, which is why `fixed` and `one`
set `Bg` to exactly their panel's fill color.

A container with no `Background` — like the `2*` panel — paints only
its own chrome and is cheaper to repaint: you pay for a fill only where
you declare one, and its text needs no `Bg` either. That is the whole
reason the third panel goes without.

### Bind a style to make it live

The second form of `Style` is a binding. Instead of a name, pass a
`*prop.Property[render.Style]` handle from the viewmodel:

```go
alert := prop.NewSource(false)
panelStyle := prop.NewComputed(func() render.Style {
	if alert.Get() {
		return render.Style{Fg: render.RGB(255, 90, 90), Bold: true}
	}
	return render.Style{Fg: render.RGB(110, 170, 255)}
})
ctx.Values["PanelStyle"] = panelStyle
```

```xml
<Border Title="status" Style="{{.PanelStyle}}">
```

Now `alert.Set(true)` recolors the border on the next frame — no
styling system involved, just the property graph from
[Tutorial 3](03-binding-and-state.md): the border reads the style while
painting, so the style is part of its damage set.

### Current limitation: a lookup, not a styling system

`Style="name"` and `Background` are the whole story today. There is no
cascading, no selectors, no setters, no theme resources — a style
applies to exactly the element that names it. The styling system —
setters, scoped resources, state-aware styles — is designed and tracked
in [the styles epic (#54)](https://github.com/WonderForgeLabs/gooey/issues/54).

### When no style is enough: draw anything

Styles color what the built-in components draw. When you need something
they cannot draw at all, gooey's floor is always available: implement
`Render` yourself and paint any cell inside your bounds — the same
escape hatch every built-in stands on. The how-to
[Draw anything with a custom Render](howto/howto-custom-draw.md) is the
complete tour; [Tutorial 6](06-custom-components.md) builds up to it.

## What you learned

- `Grid` declares tracks with `Rows`/`Cols`; children place themselves
  with the `Grid.*` attached properties.
- Fixed and Auto tracks are sized first; star tracks split the remainder
  by weight, with the rounding leftover going to the last star track.
- A star track never sizes to content — it takes what it is offered.
- `Width`, `Height`, `Margin`, `HAlign`, `VAlign`, and `Visibility` work
  on every element, in every container.
- `Hidden` reserves its space including its stack gap; `Collapsed`
  reclaims all of it, gap included.
- `Style="name"` looks up a `render.Style` in the context's style map;
  `Style="{{.Handle}}"` binds a live style property; `Background` fills
  a container's whole bounds, gaps included.

## Next steps

- **[Tutorial 3: Bind data and drive state](03-binding-and-state.md)** —
  properties, computeds, and the rule that decides whether a read is a
  subscription.
- Reference: [markup-reference.md](../markup-reference.md) has the full
  element and attribute catalog.
- Depth: [architecture.md — the component model](../architecture.md#the-component-model).
