# Learn gooey

Task-oriented guides for building terminal UIs with gooey: a retained
visual tree, dependency properties, XML markup with Go-template bindings,
and damage-tracked rendering.

Start with the tutorial series. Every tutorial is runnable code you can
copy, and every step you can verify on screen.

**New here?** [Tutorial 1](01-first-app.md) gets a working app on screen
in about fifteen minutes.

**Coming from XAML?** You will recognize `Grid` with star sizing,
attached properties, `Measure`/`Arrange`, `UserControl`, and
`KeyBinding`. `gooey.App` is `Application.Run()` and `app.Post` is
`Dispatcher.Invoke`. The real difference is that properties are **lazy**
rather than eager. Each tutorial flags the places where your WPF, WinUI,
or Avalonia instincts need adjusting.

## Tutorial series

Work through these in order — each builds on the last.

| # | Tutorial | Time | What you build |
|---|---|---|---|
| 1 | [Build your first gooey app](01-first-app.md) | 15 min | A markup file, a viewmodel, and a four-line `gooey.App` — then edit the UI while it runs |
| 2 | [Lay out a page with Grid](02-layout.md) | 20 min | A three-column page using Fixed, Auto, and Star tracks, margins, alignment, and visibility |
| 3 | [Bind data and drive state](03-binding-and-state.md) | 25 min | Sources, computeds, and an on-screen proof of the read-versus-subscribe rule |
| 4 | [Handle input with commands and key bindings](04-input-commands.md) | 25 min | Buttons, commands, focus navigation, and one key that means two things depending on focus |
| 5 | [Build reusable controls](05-usercontrols.md) | 30 min | An Include with no Go code, and a UserControl with a typed setup function |
| 6 | [Write a custom component](06-custom-components.md) | 30 min | A meter and a stepper, from `Measure` through focus and input |
| 7 | [Add app chrome — menu, status bar, toasts, and tips](07-app-chrome.md) | 30 min | A MenuBar with page-wide mnemonics, a bound StatusBar, toasts, and tooltips over a working page |
| 8 | [Drive your app from outside — MCP and gRPC](08-remote-control.md) | 30 min | A Kanban board driven from another shell: snapshot the tree, press buttons, swap the whole page over the wire |
| 9 | [Temporal end-to-end](09-temporal.md) | 40 min | A button that is a durable activity, an ops dashboard whose every call is markup, and a terminal a workflow draws |
| 10 | [Scope resources and theme with styles](10-resources-and-styles.md) | 20 min | A page-level `<Resource>` two sibling panes both read through a `<Style>`, one subtree that shadows it with its own, and a runtime `Set` that repaints only the panel still holding that handle |

Tutorials 1-6 are the core sequence; 7-10 are what you reach for when the
app grows chrome, a wire surface, a durable backend, or a theme. Tutorials
8 and 9 run demos that live in nested modules (`mcp/`, `apps/kanban`,
`handlers/temporal`), so they are run from those directories rather than
the repo root.

Finished code for each is under [`examples/`](examples), beside these
pages — one directory per tutorial. Run any of them from its own
directory, which is what makes `os.DirFS(".")` find the markup:

```sh
cd docs/learn/examples/01-first-app && go run .
```

Or launch any of them, and every demo, from the browser:

```sh
go run ./cmd/browser
```

## How-to guides

Single tasks, for when you know what you want.

| Guide | Use it when |
|---|---|
| [Hot-reload markup](howto/howto-hot-reload.md) | You want edits to a `.gooey` file to appear in the running app |
| [Embed markup for release](howto/howto-embed-release.md) | You want one self-contained binary, with the same code |
| [Show a list with a template](howto/howto-lists.md) | You have a collection to render and want the row declared in markup |
| [Declare key bindings](howto/howto-keybindings.md) | You need the gesture syntax, or a key that is scoped to one pane |
| [Handle mouse input](howto/howto-mouse.md) | You want clicks, hover, wheel, or drag |
| [Validate a form](howto/howto-forms.md) | You have inputs to validate, errors to show, and a submit to gate |
| [Draw images](howto/howto-images.md) | You have pixel content and need to know which protocol you get |
| [Draw anything with a custom Render](howto/howto-custom-draw.md) | Markup cannot express what you want, and you need to paint cells (or pixels) yourself |
| [Give a component a popup](howto/howto-popup.md) | You need an anchored dropdown or overlay that dismisses and restores focus properly |
| [Work off the UI goroutine](howto/howto-async.md) | You have a fetch, a timer, or any background work to apply |
| [Fetch, read files, and run commands from markup](howto/howto-handlers.md) | You want the behavior itself declared in the markup, with no delegate — and a capability grant deciding what it may reach |
| [Run services with your app's lifetime](howto/howto-companions.md) | You have a worker, dev server, or sidecar that should start and die with the app |
| [Format values for display](howto/howto-format.md) | You have byte counts, durations, or timestamps and a `Text` that wants a string |
| [Test a gooey app](howto/howto-testing.md) | You want assertions on rendered output, damage counts, or the real binary |

## Demo catalog

The tutorials are small enough to read in one sitting. The demos are the
same ideas at full size — each one exists to prove a specific claim, and
each is a working app you can run and read. Full walkthroughs are in
[demos.md](../demos.md); this table says which tutorial each one extends.

| Demo | Proves | Learn it first in |
|---|---|---|
| [`cmd/props`](../demos.md#props) | Unwatched sources render zero frames | [Tutorial 3](03-binding-and-state.md) |
| [`cmd/state`](../demos.md#state) | Markup with no code-behind; reactive serialization | [Tutorial 4](04-input-commands.md) |
| [`cmd/logview`](../demos.md#logview) | Conditional dependencies: pause drops a firehose out of the graph | [Tutorial 3](03-binding-and-state.md) |
| [`cmd/markuplog`](../demos.md#markuplog) | The same app in markup, hot-reloaded live | [Tutorial 1](01-first-app.md), [how-to: hot reload](howto/howto-hot-reload.md) |
| [`cmd/finder`](../demos.md#finder) | Input to derived view, with per-pane damage | [Tutorial 4](04-input-commands.md) + [Tutorial 6](06-custom-components.md) |
| [`cmd/reader`](../demos.md#reader) | Multi-UserControl composition, scoped input, live fetches | [Tutorial 5](05-usercontrols.md), [how-to: async](howto/howto-async.md) |
| [`cmd/cards`](../demos.md#cards) | `<x:Property>` end to end: one markup-only control declaring a typed, defaulted, partly-required surface, instantiated four times over four live streams | [Tutorial 5](05-usercontrols.md) |
| [`cmd/colors`](../demos.md#colors) | Canvas absolute layout and per-terminal color tiers | [how-to: images](howto/howto-images.md) |
| [`cmd/probe` + `cmd/pixels`](../demos.md#probe--pixels) | Capability detection and the graphics pipeline | [how-to: images](howto/howto-images.md) |
| [`cmd/toolkit`](../demos.md#toolkit) | The whole toolkit on one page, with MenuBar, ToastHost, and tooltips as overlays | [Tutorial 7](07-app-chrome.md), [concept: overlays](concepts/overlays.md) |
| [`cmd/sysmon`](../demos.md#sysmon) | A live dashboard over real system data | [Tutorial 2](02-layout.md) |
| [`cmd/prefs`](../demos.md#prefs) | Settings persist across runs as ordinary bound properties, with the disk-write count on screen | [Tutorial 3](03-binding-and-state.md) |
| [`cmd/browser`](../demos.md#browser) | Launching another program on your terminal and taking it back — and browsing any worktree or branch of the repo from one running instance | [concept: the App lifecycle](concepts/app-lifecycle.md) |
| [`mcp/cmd/server`](../demos.md#mcp-server) | An app that is also an MCP server: the tree, the state and the commands are the wire surface | [Tutorial 8](08-remote-control.md) |
| [`apps/kanban`](../demos.md#kanban) | The same surface on a real list app, plus a live log of every MCP message | [Tutorial 8](08-remote-control.md), [how-to: companions](howto/howto-companions.md) |
| [`handlers/temporal/cmd/temporaldemo`](../demos.md#temporaldemo) | A button whose behavior is a durable activity run by a worker elsewhere | [Tutorial 9](09-temporal.md), [how-to: async](howto/howto-async.md) |
| [`handlers/temporal/cmd/temporalops`](../demos.md#temporalops) | A real ops dashboard with every Temporal call declared in markup | [Tutorial 9](09-temporal.md), [how-to: lists](howto/howto-lists.md) |
| [`handlers/temporal/cmd/wizardui`](../demos.md#wizardui) | A terminal with no application in it: the workflow serves the markup | [Tutorial 9](09-temporal.md), [how-to: handlers](howto/howto-handlers.md) |

`cmd/browser` is the front door to most of it: it indexes exactly two
groups — the demos under `cmd/` and the tutorial examples under
`docs/learn/examples/` — shows each one's doc comment, and runs (or
records) the one you pick. The nested-module demos in the last five rows
are absent from it by construction, since each must run from inside its
own module's directory.

```sh
go run ./cmd/browser
```

## Concepts

Short framings, each linking into the deep guide.

- [The property graph](concepts/property-graph.md) — lazy sources and
  computeds, and why the call site decides what a read means.
- [Damage tracking](concepts/damage.md) — every component's paint is a graph
  node, so reading a property *is* declaring a repaint trigger.
- [Markup tiers and the loading seam](concepts/markup-tiers.md) —
  Include, UserControl, custom component; `os.DirFS` versus `embed.FS`.
- [Input routing](concepts/input-routing.md) — focus-to-root dispatch,
  hit-testing, and why focus movement is cheap.
- [Overlays and z-order](concepts/overlays.md) — there is no z-index and
  no overlay registry; document order is the whole mechanism, so an
  overlay is a declaration rather than machinery.
- [The App lifecycle](concepts/app-lifecycle.md) — what `gooey.App` owns:
  the terminal, the console signal story, suspend/resume, and dying with
  the terminal intact.

## Where else to look

- [getting-started.md](../getting-started.md) — the original five-step
  walkthrough, including building a tree in pure Go with no markup.
- [markup-reference.md](../markup-reference.md) — the complete catalog of
  elements, attributes, gestures, and binding rules.
- [architecture.md](../architecture.md) — the deep guide: rendering
  planes, the property system, the Composer, input, markup.
- [demos.md](../demos.md) — every demo, and what each one proves.
- [specs/](../specs/) — decision records. Most of them describe work that
  has **shipped**: each carries a status line, and the ones marked
  *Executed* record what was actually built and which decisions were
  argued with along the way. Read them as the ground truth behind a
  feature, not as a roadmap.

## What gooey is not, yet

These tutorials document what runs today. Things you may expect and will
not find:

- **Styles have no selectors or state yet.** `<Gooey.Resources>` and
  `<X.Resources>` give you scoped, lexically-shadowed `<Resource>`s and
  `<Style>`s with `<Setter>`s ([Tutorial 10](10-resources-and-styles.md)),
  but there is no `TargetType` implicit matching and no `:focus`/`:hover`/
  `:disabled` state sections yet — declaring one is a load error, not a
  silent no-op. A bound `Style="{{.Handle}}"` still gets you a reactive
  computed style outside the whole system, which remains the closest
  thing to per-state theming until selectors land.
- **No converters and no two-way binding syntax.** A binding resolves to
  a property handle and that is all; two-way is something a component
  does in code, and formatting a value for display is the `format`
  package's computed constructors, not a markup stage.
- **Lists are declarative, but items are projected by hand.** `<ItemsView>`
  with an `<ItemsView.ItemTemplate>` works; without reflection, an item
  reaches its template through a `func(T) map[string]any` you write.
  There is no grouping, no headers, no horizontal orientation, and no
  multi-select.
- **Attached properties cannot be declared in markup.** `<x:Property>`
  declares ordinary dependency properties on a control's root; the
  framework's own `Grid.Row`/`Canvas.Left` shape is not something your
  control can add to.
- **There is no scrolling container.** `ItemsView` windows its own rows
  (and, from Go, tail-anchors with `Scroll`), but nothing wraps arbitrary
  content in a viewport you can scroll.
- **`TextBox` is single-line** — mid-string editing, word-wise caret
  movement and selection all work (see `cmd/finder`), but there is no
  multi-line text area yet.

Each tutorial repeats the limits relevant to its topic in a "current
limitations" section, so you find them where they bite.
