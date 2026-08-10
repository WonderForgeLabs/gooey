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

## Executed (2026-08-10)

Implemented as designed, with the additions and deviations below. The
acceptance bar holds: a one-item change repaints one row, and a
selection move repaints two, both pinned by damage-count tests in
`components/itemsview_test.go`.

**A framework prerequisite the design did not name: structural change.**
The Composer walked the tree once and kept a paint node per component;
the FocusManager did the same for input. A windowed list violates that
outright — it does not know how many rows exist until it has been
arranged. So `gooey.Dynamic` (dynamic.go) hands such a container a hook,
and `Frame` re-syncs after layout and before painting. The sync is a
DIFF: a component still in the tree keeps its node, with its dependencies
and its clean/dirty flag, which is what preserves the damage guarantee
across a structural change. Removed components have their last rectangle
cleared and anything `Startable` among them stopped; new arrivals in a
started composition are started. `FocusManager.Resync` is the input half,
and it is what makes a realized row clickable at all. This generalizes:
it is the mechanism any future data-driven container will use.

**Reserved values are `_selected` and `_hovered`, both bool handles.**
The design named `_selected`; `_hovered` came free with hit-testing. What
a v1 template can DO with a bool is thin — there are no style setters or
triggers yet — so their real job is that mentioning `_selected` suppresses
the house highlight and takes the visual over. Composing selection into
per-cell styles is styles-with-setters work.

**The highlight is an overlay that re-styles, not a bar that paints.**
It is the row's last child, so its node runs after the template's, and
it flips `Reverse` on cells its siblings painted rather than filling the
row. Two consequences worth knowing: it reports itself as a `Container`
with no children so the Composer does not pre-clear its rect (which
would wipe the row it decorates), and while it is ON it reads every one
of the row's values so a re-projection dirties it too — otherwise
newly painted cells would lose the highlight. While OFF it reads only
`_selected`, so an unselected row costs nothing extra. That short-circuit
is load-bearing, not an optimization.

**`ItemsOf` joined `Items`.** A projection runs during layout, outside
any evaluation, so anything it reads there is invisible to the graph.
The moment a projection needs more than the item — reader's read-marks
map — the source has to be built inside the app's own computed.
`components.ItemsOf(slice, project)` is that inner half. Without it the
reader's read-dot would not repaint.

**Row height is discovered by measuring against the view's full height.**
Uniform rows, template-decided. A template rooted in something that
stretches asks for the whole view and yields a one-row list; the fix is
to say what the row wants (`<Grid Rows="1">`, or `Height="1"`), which is
the answer XAML gives too.

**Template bindings fail at LOAD when there is an item to check against.**
`ItemsView.Validate` builds one throwaway row against item 0 during the
markup build. An empty collection has nothing to typecheck, so those
errors surface at first realization and are painted into the view
(`ItemsView.Err`) rather than swallowed.

**Scrolling changed shape in the reader.** The old `storyRows` computed
`top = max(0, sel-H+1)`, which pins the selection to the bottom row
whenever the list is scrolled at all, including on the way back up. The
view keeps the selection visible from whichever edge it left, which is
what every other list does. Everything else about the pane is
behaviourally identical, verified over a pty across the full arc
(tab → move → enter → read-dot → focus to the reader pane) and again
against a 40-item feed to exercise the window.

**Not done here** (the remaining epic children): finder, logview and
browser still hand-render their lists. sysmon's DataGrid still waits on
its own epic. Grouping, headers, horizontal orientation, recycling pools,
`x:DataType` and multi-select remain out of scope as designed.
