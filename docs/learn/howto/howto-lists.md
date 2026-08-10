# How to show a list with a template

`<ItemsView>` renders a collection: you declare what ONE row looks like,
and the view builds one instance per item.

```xml
<ItemsView Items="{{.Rows}}" Selected="{{.Sel}}" Activate="{{.Open}}">
  <ItemsView.ItemTemplate>
    <HStack Gap="1">
      <Text>{{.Title}}</Text>
      <Text Style="dim">{{.Published}}</Text>
    </HStack>
  </ItemsView.ItemTemplate>
</ItemsView>
```

`<ItemsView.ItemTemplate>` is a **property element** — a child whose name
is `Parent.Property`. It is not a child of the list; it is an attribute
whose value happens to be markup. It is required, and it takes exactly
one child element.

## Project your items

gooey has no reflection, so it cannot look inside your struct. You say
what a row is made of:

```go
type Story struct{ Title, Published, Link string }

vals := map[string]any{
    "Rows": components.Items(stories, func(s Story) map[string]any {
        return map[string]any{"Title": s.Title, "Published": s.Published}
    }),
    "Sel":  selected,          // *prop.Property[int]
    "Open": gooey.Command(openStory),
}
```

`stories` stays a `*prop.Property[[]Story]` — typed, in your viewmodel.
`components.Items` adapts it to the one non-generic type markup can name
in a binding. The map's keys are what the template's bindings resolve
against.

Values become live handles when they are `string`, `bool`, `int`,
`float64`, `render.Style` or `render.Color`. Anything else crosses as a
fixed literal for the life of the row — fine for a `gooey.Command`, wrong
for anything that changes.

## Style from the projection

A template's bindings are the only thing that varies per row, so
per-row *styling* is a projected value too:

```go
return map[string]any{
    "Title":      s.Title,
    "TitleStyle": styleFor(s),   // render.Style
}
```

```xml
<Text Style="{{.TitleStyle}}">{{.Title}}</Text>
```

## When the projection needs more than the item

A projection runs during layout, where reads are **not** recorded as
dependencies. So if it reads anything else — a lookup table, a filter, a
mode — read that in your own computed and build the source there, or the
list will not repaint when it changes:

```go
rows := prop.NewComputed(func() components.ItemSource {
    marks := read.Get() // recorded HERE: the source depends on it
    return components.ItemsOf(stories.Get(), func(s Story) map[string]any {
        return map[string]any{"Title": s.Title, "Seen": marks[s.Link]}
    })
})
```

This is the read-versus-subscribe rule from
[Tutorial 3](../03-binding-and-state.md), in the one place it is easy to
trip over.

## Selection, keys and the mouse

Bind `Selected` to a `*prop.Property[int]` and it is shared: the view
Sets it as the user navigates, and your viewmodel can Set it too. Leave
it out and the list is display-only.

The view is a focus stop with the house keys — `↑`/`↓`/`j`/`k`,
`PgUp`/`PgDn`, `Home`/`End`, and `enter` for `Activate` — plus wheel,
click to select, and a second click on the selected row to activate.
Anything it does not use bubbles, so page-level `<KeyBinding>`s keep
working while the list has focus.

The selected row's cells are re-styled reverse for you. A template that
mentions `_selected` takes that over and gets no house highlight;
`_selected` and `_hovered` are both `*prop.Property[bool]` and are always
in a row's context.

## Watch out

- **Dot is the ITEM inside a template.** Page values are out of reach —
  the same isolation a UserControl gets. Anything a row needs comes
  through the projection. Referring to a page value is a load error.
- **A stretching template root gives you a one-row list.** Row height is
  whatever the template measures against the view's full height, so a
  `<Grid>` with default star rows asks for all of it. Say what the row
  wants: `<Grid Rows="1">`, or `Height="1"`.
- **Only visible rows exist.** Rows are built for the window and keyed by
  item index, so changing one item repaints one row. Do not expect to
  find a component for item 900 in the tree.
- **Registered components work inside templates.** A custom cell stays a
  custom component; the template just places it.
- **No grouping, headers, horizontal orientation, or multi-select** yet.

## See also

- [Markup reference: ItemsView](../../markup-reference.md#itemsview)
- [Concept: damage tracking](../concepts/damage.md)
- `cmd/reader`'s story pane is the worked example.
