# Package reorganization + component naming (queued decision record)

Directives from Elan 2026-08-10. Execution is QUEUED behind the
runtime-chapter agent (it is actively writing root-package files);
the reorg agent gets the tree to itself afterward.

## Naming: components, not widgets

Elan, verbatim: "Replace 'widget' with 'component' — we make
components not widgets." Sweep the whole surface:

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
