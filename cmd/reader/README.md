# reader

A three-pane feed reader: **feeds**, **stories**, and the story body. It is the
demo that exercises composition — three separate `.gooey` UserControls, each
with its own context, sharing one input system.

## Run

```sh
go run ./cmd/reader
```

## Keys

- `tab` — cycle focus between the three panes
- `j` / `k` (or the arrows) — move within the focused pane
- `PgUp` / `PgDn`, `Home` / `End` — move by page or to the ends, in the story list
- `enter` — open the selected story
- `a` — add a feed, then type a URL and press enter
- `q` — quit

## What it demonstrates

- **UserControls with their own contexts.** Each pane is a separate `.gooey`
  file. Data crosses a control boundary only through attribute bindings, never
  through a shared global.
- **Focus as a source property.** Only the focused pane shows the filled-dot
  indicator in its title, and only it receives `j` / `k` — moving focus repaints
  exactly the two panes involved.
- **A DataTemplate driving the story list.** `storylist.gooey` declares an
  `<ItemsView>` with an `<ItemsView.ItemTemplate>`; what used to be a Render
  loop is now a projection func handing each story's mark, styles, title and
  date to the template. Rows are windowed, so a 400-story feed builds only the
  rows on screen and changing one story repaints one row.
- **`<KeyBinding>` bound to viewmodel commands**, declared in markup rather than
  wired up in Go.
- **Async work marshalled back to the UI goroutine.** Feeds fetch over the
  network on their own goroutines and hand results back through a channel; the
  property graph is only ever touched on the main loop.

Adding a feed at runtime updates the list **and** writes `feeds.opml`, so the
change survives a restart.

See [the demo catalog](../../docs/demos.md#reader) for a full walkthrough.
