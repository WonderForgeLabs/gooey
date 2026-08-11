# gooey

A proof-of-concept XAML-like TUI framework for Go. Components live in a
retained visual tree with two-pass Measure/Arrange layout; every visual
property is a node in a lazy dependency-property graph, so a change
repaints exactly the components that read it. UIs are authored in XML
markup with Go-template-spelled bindings that resolve to property
handles at build time, hot-reload on save with state intact, and
compose into UserControls with isolated contexts. Input is routed:
commands, scoped KeyBindings, framework-owned focus with spatial arrow
navigation, and full mouse support via hit-testing. Rendering is
damage-tracked at the paint level, with pixel graphics (sixel, kitty,
iTerm2, halfblock fallback) riding a second plane over the cell buffer.

## Showcase

![reader](docs/media/demos/reader.gif)

Three UserControl panes, one input system: focus-scoped keys, live
network fetches, and a feed added at runtime persisting to OPML.

![statedemo](docs/media/demos/statedemo.gif)

Pure markup, no code-behind: buttons and a checkbox drive manual vs
reactive JSON serialization through the property graph.

![finder](docs/media/demos/finder.gif)

An fzf-style fuzzy finder as a dependency graph: typing re-scores the
index live and damage tracking repaints only the affected panes.

![temporaldemo](docs/media/demos/temporaldemo.gif)

Behavior declared in markup: one button is an HTTP GET, the other is a
Temporal activity run by a worker in another process — the terminal
names *what* runs, the app grants the capability, and the result lands
in a property.

## Quick start

```sh
go run ./cmd/statedemo
```

A UI is a `.gooey` file — elements map to components, attributes to
properties, `{{.Name}}` to bindings against your viewmodel:

```xml
<Gooey xmlns="wonderforge.io/gooey/2026">
  <Border Title="counter" Style="panel">
    <VStack Gap="1">
      <Text Style="accent">{{.Label}}</Text>
      <Button Content="+1" Click="{{.Increment}}"/>
      <KeyBinding Gesture="q" Command="{{.Quit}}"/>
    </VStack>
  </Border>
</Gooey>
```

Edit the file while the app runs and it reloads in place — state
survives, because it lives in the properties, not the components.
[docs/getting-started.md](docs/getting-started.md) builds this up in
five steps from a pure-Go tree to multi-control pages.

The program around it is `gooey.App`, which owns the terminal, the
input decoder, frame scheduling, hot-reload swaps and the whole console
signal story:

```go
var app *gooey.App
ctx := &markup.Context{Values: map[string]any{
    "Label": label, "Increment": increment,
    "Quit": gooey.Command(func() { app.Quit() }),
}}
app = gooey.NewApp(markup.Page(os.DirFS("."), "counter.gooey", ctx))
if err := app.Run(context.Background()); err != nil {
    gooey.Exit(err)
}
```

That is the whole main function. ctrl+c quits (only if no component
claimed it); `SIGINT`/`SIGTERM` restore the terminal and exit 128+n
after a bounded shutdown hook; `SIGWINCH` resizes and repaints; ctrl+z
restores, stops and comes back intact; a panic restores the terminal
BEFORE printing its stack, so a crash is readable. `app.Suspend(fn)`
hands the terminal to a child process and takes it back — the demo
browser launches every demo that way. The signal story is spelled out
in [docs/specs/2026-08-10-runtime-signals.md](docs/specs/2026-08-10-runtime-signals.md).

## Packages

The root package `gooey` is the framework: the `Component` contract,
`Base`, the layout sandwich, `Frame`, `Composer`, `Dispatcher`, `App`,
and input routing. The built-in components — `Text`, `Button`,
`Checkbox`, `TextBox`, `Gauge`, `Sparkline`, `ProgressBar`, `Spinner`,
`Toggle`, `Segmented`, `ColorPicker`, `Image`, `ItemsView`, `Timer`, the
containers `VStack`, `HStack`, `Grid`, `Border`, `Canvas`,
`StatusBar`, `ButtonBar`, and the overlays `MenuBar`, `ToastHost`,
`Tooltip`, and `AdornmentLayer` (their shared anchored-overlay mechanics
are the Go-side `Popup` primitive) — live in `gooey/components`, which imports the root and is
never imported by it. Writing your own component means embedding
`gooey.Base` and implementing `gooey.Component`; the built-ins have no
privileges you do not. Under both sit `prop` (the property graph),
`input`, `render`, `graphics`, and `term`; beside them, `markup`,
`format` (computed-property constructors for display strings — byte
sizes, counts, durations, relative times), and the opt-in `handlers/*`
and `mcp` modules.

## Distribution: packs

Reusable capability ships from this repo in three shapes, per the
[pack-distribution doctrine](docs/specs/2026-08-10-pack-distribution.md):
**handler packs** (`handlers/<name>` — gooey-coupled `HandlerProvider`s
behind an xmlns URI: `net`, `fs`, `temporal`, `exec`), **activity packs**
(`packs/<runtime>-<domain>` — gooey-free standalone modules anyone in
that runtime's ecosystem can import: `packs/temporal-visibility`), and
plain **component/format libraries** (root packages). The module
boundary follows dependency hygiene: a zero-third-party-dep handler pack
lives in the root module, any third-party dependency forces a nested
module, and activity packs are always standalone — so importing gooey
never drags SDKs, and importing a pack never drags gooey unless the pack
is gooey-coupled by nature. Every pack is inert until the host registers
it, and **registration names the capability's scope** (a client, an
`fs.FS` root, an allowlist, a task queue); each exposes its registered
names as data (`AllNames()`) and carries its own README. Releases are Go
nested-module tags (`packs/temporal-visibility/v0.1.0`).

## The control plane

A running gooey app can be a server as well as a screen. The root-module
`control` package is the one implementation of that surface — read the
tree and the screen, invoke commands, set values, drive keys and mouse,
swap the page's markup — and every transport is a thin adapter over it:
`mcp.Serve(app, …)` exposes it as MCP tools (the nested `mcp/` module),
and the gRPC server (the nested `grpc/` module,
[contract](docs/specs/2026-08-10-grpc-contract.md)) fronts the same
service. The demos run this in both directions. `wizardui` inverts the
usual arrangement: its markup is *served by* a Temporal workflow — the
workflow is the application, versioned and replayed like any workflow
state, and the terminal contributes only the capability grant that lets
served markup signal that one workflow. `examples/kanbandemo` is driven
from the outside instead: `examples/temporal-worker`, a Python Temporal
worker, has Claude generate a page and pushes it into the running board
through the MCP `swap_markup` tool. The sidecars these arrangements need
— a worker, a dev server — ride `gooey.Companion` (goroutines) and
`gooey.CompanionCmd` (child processes), started before the first frame
and stopped with the app
([companions spec](docs/specs/2026-08-10-companions.md)).

## Where it stands vs modern XAML

| Capability | Status | Notes |
|---|---|---|
| Retained tree + Measure/Arrange | done | Persistent components, measure/arrange sandwich via `MeasureChild`/`ArrangeChild`; `SIGWINCH` resizes the composition and repaints |
| Dependency properties | done | Lazy dirty-tracking graph (Slint lineage), not eager WPF-style notification; UI-goroutine-confined |
| Bindings | done | `{{.Path}}` resolves once at build time to property handles (lvalue semantics); mixed text content; typed handles across element boundaries. No converters or two-way markup syntax — two-way is component code, and value formatting is the `format` package's computed-property constructors (`format.Bytes(p)` → `Property[string]`; markup converter stages over the same functions are #99) |
| Markup + hot reload | done | XML over any `fs.FS`; `markup.Page` polls ModTimes and the App rebuilds on the UI goroutine, viewmodel state survives |
| UserControls | done | Context isolation, data crosses only via attribute hand-off; the property surface is implicit unless the control declares it with `<x:Property>` |
| Grid / star sizing | done | `Auto`/`Fixed`/`Star` tracks with spans, XAML `GridLength` semantics |
| Canvas / absolute layout | done | `Canvas.Left`/`Canvas.Top` attached properties; children may overlap, paint order is tree order |
| Timers | done | `<Timer Interval="600ms" Tick="{{.Fn}}"/>` — non-visual attachment; the goroutine posts through the Dispatcher and the Composer owns its lifetime, so a hot reload cannot leak one |
| Commands + KeyBindings | done | `Command` is `func()`; bindings are non-visual attachments scoped by where they are declared; dispatch bubbles, navigation runs in the unconsumed tail |
| Focus + mouse | done | Framework-owned focus (`FocusState`), spatial arrow navigation (XYFocus), hit-testing, hover, implicit capture, click synthesis, SGR and legacy X10 decoding; focus/hover damage is just property damage |
| Styles | partial | `Style="name"` is a named lookup; `Style="{{.Handle}}"` binds a live `render.Style` property, so a computed style is reactive. No cascading, selectors, setters, or overrides |
| DataTemplates / ItemsView | done | `<ItemsView.ItemTemplate>` via the XAML property-element syntax; the template is a factory instantiated per item against an isolated context. Items arrive through a projection func (`map[string]any`) — the no-reflection stand-in for `x:DataType`. Rows are windowed and index-keyed, so a one-item change repaints one row. No grouping, headers, horizontal orientation, or multi-select |
| Adornments + Tooltip | done | WPF's adorner plane: an `AdornmentLayer` (last child of the root) hosts components positioned against a *target's* arranged bounds — re-anchored every frame, dropped when the target vanishes. `Tooltip` is the first customer: `<Tooltip Text="…"/>` as a KeyBinding-style attachment or `Tooltip="…"` on any element, delay timer, flip-to-fit, gesture hints, pure markup ([spec](docs/specs/2026-08-10-adornments.md)) |
| Validation | done | Validators are computeds — `validate.Field(src, rules…)` yields an error property (empty = valid, first failure wins), `validate.All` a value-stabilized is-valid for submit gating via `.When()`. The built-in vocabulary is .NET's DataAnnotations set (Required, MinLen/MaxLen, Pattern, EmailAddress, Url, Phone, CreditCard, Digits, Integer, MinValue/MaxValue, Compare, Range) with stock messages, no third-party deps, and stricter-than-.NET semantics where it matters. Markup: a `<Validate Required="true" EmailAddress="true"/>` behavior on the input (bare child or `<X.Behaviors>` slot) builds the same computed, publishes it (`Into`) for inline error `<Text>`s, and wires the TextBox's invalid visual; `ctx.Rules` registers domain rules beyond the annotations; `<ValidationMarker/>` floats the message via the AdornmentLayer for dense layouts ([spec](docs/specs/2026-08-10-validation-core.md)) |
| Menus, toasts, popups | done | `MenuBar` (modal dropdowns, page-wide `alt+letter` mnemonics, focus restored on dismiss) and `ToastHost` (auto-dismissed notifications), both declared as the *last* children of the root because document order is z-order. Their shared lifecycle — anchored surface, focus save/restore, pointer capture, dismissal grammar — is extracted as the Go-side `Popup` primitive ([spec](docs/specs/2026-08-10-popup.md)); there is no markup `<Popup>` element yet |
| Color depth adaptation | done | Truecolor / 256 / 16 detected per session; the buffer stays 24-bit and downsampling happens at the wire. Components read `Frame.Caps` to adapt |
| x:Property (markup-declared properties) | done | `<x:Property Name="Title" Type="string" Default="untitled"/>` on a control's root — declared markup properties are ordinary dependency properties, registered from markup. Bound attributes pass the parent's handle through type-checked, absent ones materialize a per-instance source with the default, `Required` is a load error. Declaring a surface makes the control strict. Types are a type-switch table (`string`/`int`/`bool`/`float`/`duration`/`color`/`any`), no reflection. No markup-declared *attached* properties; declared defaults reset on hot reload ([spec](docs/specs/2026-08-10-markup-declared-properties.md)) |
| Handler namespaces (xmlns, Temporal) | done | `{{net:Get .Url \| into .Body}}` — events bound to framework handlers declared in markup; registration is the capability grant. One pipeline stage (`into`), one result, no retry surface yet ([spec](docs/specs/2026-08-10-remote-handlers-design.md)) |
| MCP server (live tree control) | done | `mcp.Serve(app, …)` makes a running app an MCP host: read the tree and the screen, invoke commands, set values, drive keys and mouse, replace the page's markup. Protocol is the official `modelcontextprotocol/go-sdk`, isolated in the nested `mcp/` module so core's graph is unchanged. Loopback-only, opt-in, no auth; every tool marshals through the Dispatcher onto the UI loop ([spec](docs/specs/2026-08-10-mcp-server.md)) |

## Demos

Each has a walkthrough in [docs/demos.md](docs/demos.md). Most live
under `cmd/`; demos whose dependencies are quarantined in nested modules
live with their module (`handlers/temporal/cmd/`, `mcp/cmd/`,
`examples/`) and run from that module's directory.

| Demo | GIF | Proves |
|---|---|---|
| `cmd/probe` + `cmd/demo` | [demo.gif](docs/media/demos/demo.gif) | Capability detection and the graphics pipeline (`--mode` forces a protocol) |
| `cmd/propdemo` | [propdemo.gif](docs/media/demos/propdemo.gif) | Lazy property graph: unwatched sources render zero frames |
| `cmd/logview` | [logview.gif](docs/media/demos/logview.gif) | Conditional dependency recording: pause drops the firehose out of the graph |
| `cmd/markuplog` | [markuplog.gif](docs/media/demos/markuplog.gif) | Markup hot reload: live edits rebuild the tree, buffer intact |
| `cmd/finder` | [finder.gif](docs/media/demos/finder.gif) | Input-to-derived-view pipeline with per-pane damage |
| `cmd/reader` | [reader.gif](docs/media/demos/reader.gif) | Multi-UserControl composition, scoped input, live fetches |
| `cmd/statedemo` | [statedemo.gif](docs/media/demos/statedemo.gif) | No-code-behind markup and reactive serialization |
| `cmd/cardsdemo` | [cardsdemo.gif](docs/media/demos/cardsdemo.gif) | Markup-declared `<x:Property>` surfaces: one markup-only control, four instances over four live data streams ([timerdemo.gif](docs/media/demos/timerdemo.gif) isolates its `<Timer>` element) |
| `handlers/temporal/cmd/temporaldemo` | [temporaldemo.gif](docs/media/demos/temporaldemo.gif) | Handler namespaces: a button whose behavior is a remote Temporal activity |
| `handlers/temporal/cmd/temporalops` | [temporalops.gif](docs/media/demos/temporalops.gif) | A live Temporal visibility dashboard: every Temporal call declared in markup, an `ItemsView` over real API responses, paging on real page tokens |
| `handlers/temporal/cmd/wizardui` | [wizarddemo.gif](handlers/temporal/wizarddemo.gif) (earlier cut; final GIF: docs-and-demos workflow) | Workflow-served markup: every screen arrives as a workflow query payload, and the terminal contributes only the capability grant |
| `cmd/colordemo` | [colordemo.gif](docs/media/demos/colordemo.gif) | Canvas absolute layout, per-terminal color tiers, and a page styled live by the color being picked |
| `mcp/cmd/mcpdemo` | [mcpdemo.gif](docs/media/demos/mcpdemo.gif) | The app as an MCP server: every change in the GIF is a tool call from a script, including the page swapping itself out from under a surviving viewmodel |
| `examples/kanbandemo` | — | A real Kanban board that is also an MCP server, with a live traffic log; `examples/temporal-worker` pushes generated markup into it over `swap_markup` |
| `cmd/toolkitdemo` | [toolkitdemo.gif](docs/media/demos/toolkitdemo.gif) | The whole component kit alive at once, organized under a `<Tabs>` into six pages — panels and inputs, meters and an ItemsView template, Canvas/ColorPicker/Image, Validate behaviors, and the overlay plane (MenuBar, ToastHost, AdornmentLayer, Popup) |
| `cmd/sysmon` | — | A live `/proc` system monitor: the promoted Gauge/Sparkline components, threshold styling, and Set-only-on-change dedup keeping an idle system near zero repaints |

The tutorial examples under [`docs/learn/examples/`](docs/learn/examples)
are runnable too, and `cmd/browser` lists both groups:

```sh
go run ./cmd/browser
```

## Documentation

- [docs/getting-started.md](docs/getting-started.md) — hands-on tutorial, five steps to a componentized app
- [docs/architecture.md](docs/architecture.md) — the deep guide: rendering planes, property graph, Composer, input, markup
- [docs/markup-reference.md](docs/markup-reference.md) — every element, attribute, gesture, and binding rule
- [docs/learn/](docs/learn/index.md) — the tutorial series, how-to guides, and concepts, with runnable code under [docs/learn/examples/](docs/learn/examples)
- [docs/demos.md](docs/demos.md) — what each demo exercises, with walkthroughs
- [docs/specs/](docs/specs/) — decision records: [markup-declared properties](docs/specs/2026-08-10-markup-declared-properties.md), [reader design](docs/specs/2026-08-10-reader-design.md), [remote handlers](docs/specs/2026-08-10-remote-handlers-design.md), [container backgrounds](docs/specs/2026-08-10-container-backgrounds.md), [MCP server](docs/specs/2026-08-10-mcp-server.md)

## POC limits, honestly

There is no styling system (named style lookup only). The file watcher is
300 ms ModTime polling. Properties are confined to the UI goroutine;
background work crosses in over a channel.

## Architecture decisions, one line each

- **Not N renderers — one cell renderer plus N graphics protocols**, on separate planes the terminal composites; halfblock is the universal cell-plane fallback: [the two rendering planes](docs/architecture.md#the-two-rendering-planes)
- **Capability detection is a handshake, not config** — Kitty query + XTWINOPS + DA1, preference kitty > sixel > iterm2 > halfblock: [detection](docs/architecture.md#capability-detection-is-a-handshake-not-config)
- **Properties are lazy, not eager** — a set marks dirty and computes nothing; evaluation records its own dependencies, so conditional reads watch only the taken branch: [the property system](docs/architecture.md#the-property-system)
- **"AffectsRender" is discovered, not declared** — each component's paint is a computed node, so whatever it reads is its damage set: [the Composer](docs/architecture.md#the-composer)
- **The flush diffs cells, not paint nodes** — components overpaint each other and containers never clear their bounds, so only a buffer comparison is trustworthy; damage counts decide the byte total, never the correctness. An idle frame writes zero bytes, a keystroke writes about thirty: [damage reaches the wire](docs/architecture.md#damage-reaches-the-wire-renderflusher)
- **Pixel placements are owned by the paint node that recorded them** — only dirty components re-render, so a rebuilt-from-scratch placement list would lose every image that did not repaint; and protocols without placement identity erase a vanished image by repainting the cells under it: [damage on the pixel plane](docs/architecture.md#damage-on-the-pixel-plane)
- **Layout runs outside the evaluation context** — reads during Measure subscribe to nothing, keeping layout out of the graph by construction: [layout vs the graph](docs/architecture.md#layout-runs-outside-the-evaluation-context)
- **Framework state in source properties makes focus and hover damage free** — moving focus repaints exactly two components: [the input system](docs/architecture.md#the-input-system)
- **KeyBindings scope by attachment position, and navigation runs in the unconsumed tail** — a binding fires only while its subtree has focus; arrows are spatial (XYFocus) with a tree-order fallback: [routed dispatch](docs/architecture.md#routed-dispatch)
- **Both mouse encodings are decoded** — an undecoded legacy X10 report would inject phantom keystrokes, not just drop the event: [one ordered stream](docs/architecture.md#one-ordered-stream)
- **Bindings are handles, not values** — resolved once at build time, zero lookups at render: [markup](docs/architecture.md#markup)
- **Registering a handler namespace IS the capability grant** — markup reaches only the URIs its host registered, so an untrusted document is sandboxed by construction, and async results marshal back through a Dispatcher: [markup reference](docs/markup-reference.md#handler-namespaces)
- **The run loop is the framework's, and nothing extends it with another select case** — a dynamic select needs reflection, and it is not needed: every asynchronous source reaches the UI through the Dispatcher, which is the confinement rule anyway: [the runtime](docs/specs/2026-08-10-runtime-signals.md)
- **No Screen teardown may leave a goroutine reading the terminal** — the ioctls go through `SyscallConn` so the tty stays pollable and Close really cancels a pending read, which is what makes handing the terminal to a child safe: [tty read lifecycle](docs/specs/2026-08-10-tty-read-lifecycle.md)
- **The `fs.FS` seam is the deployment story** — `os.DirFS` in dev hot-reloads, `embed.FS` in release is a natural no-op, same code: [loading tiers](docs/architecture.md#three-loading-tiers-one-seam)
