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
`KeyBinding`. The two real differences are that properties are **lazy**
rather than eager, and that you own the run loop. Each tutorial flags the
places where your WPF, WinUI, or Avalonia instincts need adjusting.

## Tutorial series

Work through these in order — each builds on the last.

| # | Tutorial | Time | What you build |
|---|---|---|---|
| 1 | [Build your first gooey app](01-first-app.md) | 15 min | A markup file, a viewmodel, and the host loop — then edit the UI while it runs |
| 2 | [Lay out a page with Grid](02-layout.md) | 20 min | A three-column page using Fixed, Auto, and Star tracks, margins, alignment, and visibility |
| 3 | [Bind data and drive state](03-binding-and-state.md) | 25 min | Sources, computeds, and an on-screen proof of the read-versus-subscribe rule |
| 4 | [Handle input with commands and key bindings](04-input-commands.md) | 25 min | Buttons, commands, focus navigation, and one key that means two things depending on focus |
| 5 | [Build reusable controls](05-usercontrols.md) | 30 min | An Include with no Go code, and a UserControl with a typed setup function |
| 6 | [Write a custom widget](06-custom-widgets.md) | 30 min | A meter and a stepper, from `Measure` through focus and input |

Finished code for each is under
[`examples/`](../../examples) — one directory per tutorial. Run any of
them from its own directory:

```sh
cd examples/01-first-app && go run .
```

## How-to guides

Single tasks, for when you know what you want.

| Guide | Use it when |
|---|---|
| [Hot-reload markup](howto/howto-hot-reload.md) | You want edits to a `.gooey` file to appear in the running app |
| [Embed markup for release](howto/howto-embed-release.md) | You want one self-contained binary, with the same code |
| [Declare key bindings](howto/howto-keybindings.md) | You need the gesture syntax, or a key that is scoped to one pane |
| [Handle mouse input](howto/howto-mouse.md) | You want clicks, hover, wheel, or drag |
| [Draw images](howto/howto-images.md) | You have pixel content and need to know which protocol you get |
| [Work off the UI goroutine](howto/howto-async.md) | You have a fetch, a timer, or any background work to apply |
| [Test a gooey app](howto/howto-testing.md) | You want assertions on rendered output, damage counts, or the real binary |

## Concepts

Short framings, each linking into the deep guide.

- [The property graph](concepts/property-graph.md) — lazy sources and
  computeds, and why the call site decides what a read means.
- [Damage tracking](concepts/damage.md) — every widget's paint is a graph
  node, so reading a property *is* declaring a repaint trigger.
- [Markup tiers and the loading seam](concepts/markup-tiers.md) —
  Include, UserControl, custom widget; `os.DirFS` versus `embed.FS`.
- [Input routing](concepts/input-routing.md) — focus-to-root dispatch,
  hit-testing, and why focus movement is cheap.

## Where else to look

- [getting-started.md](../getting-started.md) — the original five-step
  walkthrough, including building a tree in pure Go with no markup.
- [markup-reference.md](../markup-reference.md) — the complete catalog of
  elements, attributes, gestures, and binding rules.
- [architecture.md](../architecture.md) — the deep guide: rendering
  planes, the property system, the Composer, input, markup.
- [demos.md](../demos.md) — the apps in `cmd/`, and what each one proves.
- [specs/](../specs/) — decision records for work that is designed but
  not built.

## What gooey is not, yet

These tutorials document what runs today. Things you may expect and will
not find:

- **No resize handling.** The Composer takes the terminal size at
  construction.
- **No styling system.** `Style="name"` is a lookup — no cascading,
  selectors, or setters.
- **No DataTemplates.** Every list is a hand-written rows widget.
- **No editable text widget.** `cmd/finder` builds one by hand.
- **No `CanExecute`**, so no automatic disabled command state.
- **Visibility is not bindable** — it is set when markup is built.
- **Damage tracking stops at the paint level.** The flush still writes
  the whole buffer each frame.

Each tutorial repeats the limits relevant to its topic in a "current
limitations" section, so you find them where they bite.
