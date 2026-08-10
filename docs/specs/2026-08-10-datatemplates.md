# DataTemplates & ItemsView (design — epic #14)

The second of the two great unimplemented XAML pillars. Today every
list in the repo is a hand-written rows component behind a UserControl
(reader's feed/story rows, finder's results, logview's pane, browser's
demo list, sysmon's process table). ItemsView + ItemTemplate makes
list UI declarative:

```xml
<ItemsView Items="{{.Stories}}" Selected="{{.SelStory}}">
  <ItemsView.ItemTemplate>
    <HStack Gap="1">
      <Text Style="accent">●</Text>
      <Text>{{.Title}}</Text>
      <Text Style="dim">{{.Published}}</Text>
    </HStack>
  </ItemsView.ItemTemplate>
</ItemsView>
```

## Decisions

1. **Property-element parse path first.** `<Parent.Child>` dotted
   elements enter the parser as the general mechanism — it serves
   ItemTemplate now and `<x:Property>` (#7) next. A property element's
   children are handed to the parent element's builder as a named
   structured attribute, not built as tree children.
2. **The template is a factory, not a tree.** ItemTemplate captures
   its Element subtree at load; each item instantiates it against a
   PER-ITEM context (UserControl-style isolation): dot rebinds to the
   item — `{{.Title}}` resolves against the item's fields. With no
   reflection, items must expose their fields the way controls do:
   v1 items are `map[string]any` value-maps OR the app registers an
   item-projection func `func(item T) map[string]any` on the view —
   type-switch world, honest about the ceiling until x:Property/gen.
   (`x:DataType` typechecking is #7/gen territory; not v1.)
3. **Items binding is a property of slices.** `Items="{{.Stories}}"`
   accepts `*prop.Property[[]T]` via a small adapter interface
   (length + item(i) as the projected map) so typed viewmodel slices
   keep their types; the markup layer sees only the adapter. A
   `components.Items[T](p, project)` helper builds it.
4. **Realization and damage.** v1 realizes only visible rows
   (windowed by the view's height + scroll offset — virtualization
   from day one, since every consumer here scrolls) and keys realized
   rows by index. A change to the Items property re-projects the
   window; per-row contexts are rebuilt for rows whose projected map
   changed (shallow compare) — so a one-item change repaints one row.
   Damage-count tests are the acceptance bar, as everywhere.
5. **Selection, focus, input.** Selected is an optional
   `*prop.Property[int]` binding. ItemsView is a focus stop with the
   house list keys (arrows/j/k/pgup/pgdn/home/end), wheel + click +
   hover via hit-testing, and an `Activate="{{.Open}}"` command on
   enter/double-click. Row highlight = reverse on the realized row's
   context (a reserved `_selected` value the template may use, plus
   default whole-row reverse when it doesn't).
6. **Migrations prove it** (epic children): reader story list first
   (richest template), then finder results (match highlighting needs
   a custom cell — templates allow registered components inside, so
   the highlight component stays, now placed BY a template), logview
   pane, browser list. sysmon's DataGrid waits for #78.

## Out of scope v1

Grouping, headers/footers, horizontal orientation, item recycling
pools, `x:DataType`, multi-select, live re-sort animations.
