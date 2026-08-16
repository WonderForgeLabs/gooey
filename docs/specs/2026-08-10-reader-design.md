# reader — RSS/Atom three-pane reader (design)

The first complex gooey app, and the vehicle for the UserControl
mechanism. Decisions locked with Elan 2026-08-10.

## Decisions

- Feeds from **OPML** (`feeds.opml` in cwd). An embedded default set
  (HN frontpage, Lobsters, Go Blog, Cloudflare Blog) is written out on
  first run as the base file. `a` adds a feed URL in-app and writes
  back to the OPML.
- **Ephemeral session**: all feeds fetched once at startup (and a new
  feed on add). Read/unread state is in-memory only. No auto-refresh,
  no persistence of read state in v1.
- Both **RSS 2.0 and Atom** parse; format sniffed by root element.

## UserControl mechanism (framework)

A UserControl is a `.gooey` file + a code-behind setup + a
registration. Core rule: **context isolation** — each instance loads
its markup against its own `markup.Context`, so bindings inside the
control resolve against the control's values, never the page's.

- `markup.UserControl(fsys, "storylist.gooey", setup)` returns a
  `Builder` registered like any custom widget.
- `setup(e Element, parent *Context) (*Context, error)` builds the
  instance context. Data crosses the boundary through **attributes**,
  resolved in the parent's context to property handles:
  `<StoryList Stories="{{.Stories}}"/>` → setup calls
  `parent.BindingValue(e.Attrs["Stories"])` and receives the
  `*prop.Property[...]` handle. This is XAML's DataContext +
  dependency-property hand-off.
- Styles/Widgets inherit from the parent context when the child sets
  none. `Named` is scoped per control (like x:Name in templates).
- `markup.WatchAll` watches every loaded markup file; any change
  rebuilds the page tree. Control state survives because viewmodel
  properties live outside the tree.

## App structure (cmd/reader)

- `reader.gooey` — shell: `Grid Cols="24,1*,2*"`, one UserControl per
  column: FeedList, StoryList, ReaderPane. Status/help line row.
- `feedlist.gooey` / `storylist.gooey` / `readerpane.gooey` — each a
  Border (bindable focus-aware title) around a per-instance custom
  rows/content widget.
- Viewmodel (page-level, in main): sources `feeds`, `selFeed`,
  `selStory`, `focus`, `read` (session read-set), `input` (add-feed
  buffer, empty = inactive); computeds `stories`, `current` (selected
  story → HTML-stripped wrapped text), per-pane titles (● on focused),
  `status` (help or add-feed prompt).
- Fetch: one goroutine per feed at startup → results channel → main
  loop `Set`s. Properties stay UI-goroutine-confined; panes fill as
  feeds arrive.
- Keys: `tab` cycle focus; `j/k`/arrows in focused pane; `enter` open
  story (marks read); `a` add-feed input mode (enter=confirm+append
  OPML, esc=cancel); `q` quit.

## Out of scope v1

Read-state persistence, auto-refresh, nested OPML categories,
article scrolling (truncate at pane height), opening links in browser —
collected under epic [#64](https://github.com/WonderForgeLabs/gooey/issues/64)
and tracked as [#65](https://github.com/WonderForgeLabs/gooey/issues/65)
(persistence), [#66](https://github.com/WonderForgeLabs/gooey/issues/66)
(auto-refresh), [#67](https://github.com/WonderForgeLabs/gooey/issues/67)
(scrolling), [#68](https://github.com/WonderForgeLabs/gooey/issues/68)
(open in browser), [#69](https://github.com/WonderForgeLabs/gooey/issues/69)
(nested OPML).

Note: repo is not yet a git repository; spec uncommitted by design.
