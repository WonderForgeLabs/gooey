# Package reorganization + component naming (queued decision record)

Directives from Elan 2026-08-10. Execution is QUEUED behind the
runtime-chapter agent (it is actively writing root-package files);
the reorg agent gets the tree to itself afterward. Landed in
[PR #82](https://github.com/WonderForgeLabs/gooey/pull/82).

## Naming: components, not widgets

Epic [#2](https://github.com/WonderForgeLabs/gooey/issues/2). Elan,
verbatim: "Replace 'widget' with 'component' — we make components not
widgets." Sweep the whole surface:

- `Widget` interface → `Component`; `ChildWidgets()` →
  `ChildComponents()` (or `Children()` if free of collisions);
  `Container`, `Bounded`, `HasLayout` keep their names.
- File names (widgets.go → components split per the layout below),
  identifiers, doc comments, README/learn/demos.md prose, and
  user-visible strings ("widgets painted last frame" →
  "components painted last frame" — note demo GIFs showing the old
  string go stale; re-record only where a demo's story shows the
  stats line prominently, else let the next natural re-record fix it).
- specs/ history stays as written (decision records are records).

## Package layout

| Package | Contents |
|---|---|
| `gooey` (root) | Component/Container/Base/Layout contracts, Frame, Composer, Dispatcher, App + signals, focus/mouse routing, Command |
| `gooey/components` | Text, Button, Checkbox, TextBox, Gauge, Sparkline, ColorPicker, Image, Timer, and containers: VStack, HStack, Grid, Border, Canvas |
| unchanged | prop, input, render, graphics, term, markup, handlers/* |

Rules: `components` imports root, never the reverse; `markup`
builders import `components`; `Str`/`Sty` helpers move with the
components; tests move with their subjects; every demo/example
updates imports; docs get one path-and-naming pass at the end
(after the runtime chapter's docs/learn/examples move, so link
verification runs once). `handlers/temporal` submodule updates its
`replace`-based imports too.

Open question for the executing agent to surface, not decide:
whether Timer is a component or a root runtime citizen — Elan noted
it is genuinely arguable; default to components per the table unless
he rules otherwise.

## Executed — 2026-08-10

Done on branch `reorg/components`. All three modules build, vet, and
test green (`mcp` under `-race`); `gofmt` clean. The package table was
followed as written. Decisions and deviations, in the order they came
up:

**`ChildComponents()`, not `Children()`.** The alternative is not
merely a taste call — it is impossible. `VStack`, `HStack`, `Grid`, and
`Canvas` each carry a `Children []Component` *field*, and Go forbids a
type from having a field and a method of the same name. `Children()`
would have meant renaming that field on every container and rewriting
every composition literal in the demos and docs. `ChildComponents()`
costs nothing and keeps the `ChildWidgets` shape readers already know.

**`markup.Context.Widgets` → `Context.Components`,** as directed. It is
still `map[string]Builder`, and `Builder` still returns
`gooey.Component`.

**`layoutOf` was exported as `gooey.LayoutOf`.** This is new root API,
not in the spec. It was forced by the split: `VStack`, `HStack`,
`Grid`, and `Canvas` all read a child's `Layout` — visibility for the
gap and collapse rules, `Row`/`Col` for grid placement, `Left`/`Top`
for canvas offsets — and from outside the root package there was no
way to ask. `HasLayout` alone would have made every panel repeat the
same type assertion. Exporting it is also correct on its own terms:
anyone writing a container outside `gooey/components` needs exactly
this, so keeping it unexported would have made third-party panels
second-class. Same argument as `MeasureChild`/`ArrangeChild`.

**Component fields moved from `x.bounds` to `x.Bounds()`.** `Base`
stays in the root with its unexported `bounds` field, so components
now read their arranged rect through the accessor that already
existed. No behavior change; it is the same field.

**Root tests split by what they need, not by what they test.**
`app_test.go`, `companion_test.go`, `dispatch_test.go`,
`pty_linux_test.go`, and `signals_linux_test.go` already used
throwaway fakes (`label`, `eater`, `exploder`, `probe`), so they
stayed. The nine that instantiate real components —
`composer_test.go`, `input_test.go`, `layout_test.go`,
`mouse_test.go`, `navigation_test.go`, `canvas_test.go`,
`colorpicker_test.go`, `textbox_test.go`, `timer_test.go` — moved to
`components/` verbatim, so every damage-count assertion still counts
real paint nodes over a real tree. Nothing was weakened into a fake:
`TestComposerDamageIsPerComponent` still wants 3 then 1 then 0,
`TestFocusMoveDamageIsTwoComponents` still wants 2, hover still wants
leave+enter. The only edit was `c.frame.Cells` → `c.Cells()`, the
exported accessor for the same buffer.

**`Timer` went to `components`** (the table's default) and it holds up:
`Timer` is declared in the tree, embeds `Base`, and is bound with
ordinary property handles. What the root actually owns is the
`Startable` *contract* — the interface the Composer discovers and whose
lifetime it manages — and that stayed in the root, now in
`startable.go`. Contract in the root, implementation in `components`,
which is the same line drawn everywhere else.

**`KeyBinding` stayed in the root,** as the assignment proposed. It is
input plumbing, not a component: `FocusManager.walk` collects
`*KeyBinding` attachments by concrete type and `Dispatch` matches
gestures against them before any `HandleKey` runs. Moving it would
make the root import `components` to do its own event routing — the
one edge this reorg exists to remove.

**Composite literals are now keyed.** `gooey.Size{w, h}` and
`gooey.Rect{x, y, w, h}` are vet errors once written from another
package (`composites`), so every such literal in `components` became
`gooey.Size{W: w, H: h}` and so on. Mechanical, and the demos were
already written this way.

**One wire-visible change, in the MCP surface.** `tree_snapshot`
reports each node's `type` as `fmt.Sprintf("%T", …)`, so a `Text` now
serializes as `*components.Text` where it used to be `*gooey.Text`.
Scripts matching those strings must be updated; `mcp/mcp_test.go`
already asserts the new form. `cmd/statedemo` prints the same `%T` for
its focused/hovered nodes, so its JSON pane changed the same way.

**Renamed files:** `widget.go` → `component.go`; `widgets.go` and
`controls.go` dissolved into per-component files under `components/`
(`text.go`, `button.go`, `vstack.go`, …), with `Base`/`Attacher`
extracted to root `base.go` and the shared helpers (`Str`, `Sty`,
`clipRunes`, `clamp`, the collapse/gap predicates) in
`components/components.go`. (`clipRunes` is named as it stood on
2026-08-10 and no longer exists: clipping by RUNES was the defect
[#358](https://github.com/WonderForgeLabs/gooey/issues/358) fixed, and
it is `render.ClipCols` now — one implementation, counting columns, for
the ~25 paint sites that had shared the private one.) `docs/learn/06-custom-widgets.md`,
`docs/learn/examples/06-custom-widgets/`, and
`docs/learn/media/06-custom-widgets.png` all renamed to
`…custom-components…`.

**Docs.** `README.md` gained a "Packages" section and
`docs/architecture.md` a "Where the code lives" table stating the
one-way dependency rule; `docs/demos.md` propdemo numbers were retuned
to the new recording. Everything else was the naming sweep. The
`.github/workflows/claude*.yml` review prompts and
`.claude/workflows/docs-and-demos.js` were swept too — they named
`widget.go`/`widgets.go` and would have pointed at files that no longer
exist. This directory (`docs/specs/`) was left untouched apart from
this note, per "decision records are records".

**Stale GIFs** (not re-recorded; `propdemo.gif` was, per instruction):

| Asset | Why |
|---|---|
| `logview.gif` | stats line still reads "widgets painted last frame" |
| `statedemo.gif` | JSON pane shows `"*gooey.Button"` for the focused node |
| `mcpdemo.gif` | help pane says "the live widget tree" |
| `finder.gif` | indexes the repo — its results list `widgets.go`/`controls.go` and a root `composer_test.go`, none of which exist now, and its preview pane shows `.gooey` files whose comments changed |
| `cmd/browser/browser.gif` | demo list shows `06-custom-widgets` |
| `docs/learn/media/03-unwatched.png`, `03-watched.png` | stats line reads "widget(s)" |
| `docs/learn/media/06-custom-components.png`, `06-stepper.gif` | Border title reads "custom widgets" |

`cardsdemo`, `colordemo`, `demo`, `markuplog`, `reader`,
`temporaldemo`, `timerdemo`, `wizarddemo`, `01-hot-reload`,
`04-focus`, and the other learn media show no renamed string and are
unaffected.
