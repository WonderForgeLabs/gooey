# Root package as an alias facade — analysis and recommendation

Elan, 2026-08-10: "we have lots of loose unorganized go files in repo
root. Move them to correct place also." The GIF half of that cleanup
shipped (#169). The ~18 root `.go` files did not, and this record decides
whether they should.

The mechanism on the table, proposed by the validation agent: move every
root file into one new package (`internal/core` or `core/`) and keep
`gooey.*` importable by re-exporting the surface from the root —
`type Component = core.Component`, `var NewApp = core.NewApp`, and so on
for the whole API.

## Recommendation: do not do this. Accept the root package.

**Not executed.** Nothing was moved on this branch; this file is the
whole deliverable.

Two findings kill it, both verified by experiment rather than reasoned
from memory:

1. **Type aliases are transparent at compile time and opaque at
   runtime.** `%T` of an aliased type prints the *underlying* name, so
   every root type would start reporting `core.X`. This framework uses
   `%T` strings as its type-identity mechanism *because* it bans
   reflection — so runtime type names are part of its public contract,
   including its gRPC/MCP wire contract. `mcp/tools.go:334` compares
   against the literal `"gooey.Command"` and decides whether a command is
   callable at all. The facade's entire premise — "`gooey.*` stays
   stable" — is false exactly where this codebase leans hardest.

2. **`go doc` on an alias facade is empty.** Not degraded: empty. The
   root package's documentation — which in this repo is essay-grade and
   is a deliberate asset — would vanish from the package users import.

The cosmetic win (23 root `.go` files → 1) is real and is the only win.
Everything else is neutral or worse.

## 1. What is actually in the root, and why #2 left it

16 non-test files, 4124 lines; 7 test files, 1640 lines.

| Files | Lines | What it is |
|---|---|---|
| `component.go`, `base.go`, `layout.go`, `startable.go`, `dynamic.go` | 1 013 | The contracts: `Component`, `Container`, `Base`, `Layout`/`Thickness`/`Align`/`Visibility`, `Frame`, `Size`/`Rect`, the measure/arrange sandwich helpers, `Startable`, `Dynamic` |
| `composer.go`, `placements.go` | 984 | The retained tree + damage tracking, and the pixel-plane placement diff (all `*Composer` methods; exports only `EncoderFor`) |
| `input.go`, `mouse.go` | 1 083 | The command model (`Action`/`Command`/`Cmd`), focus/hover interfaces, `FocusManager`, `KeyBinding`, mouse routing and capture |
| `app.go`, `signals_unix.go`, `signals_other.go` | 1 828 | The run loop, options, terminal lifecycle, suspend/resize/signal handling (signals are `*App` methods; they export nothing) |
| `companion.go`, `companion_unix.go`, `companion_other.go` | 1 036 | Lifecycle-bound background services |
| `dispatch.go` | 94 | The UI-goroutine marshaling seam |

The reorg epic (#2) moved `Text`/`Button`/`VStack`/… out to
`components/` and stopped there, and `docs/specs/2026-08-10-package-reorg.md`
says why in its own words: the split line is **contract in the root,
implementation in `components`**. `Startable` stayed while `Timer` left;
`KeyBinding` stayed because moving it would make the root import
`components` "the one edge this reorg exists to remove"; `LayoutOf` was
*exported* so out-of-tree containers could participate. Every one of
those calls is a statement that the root package is the framework, not a
junk drawer.

So the files are not "unorganized." They are one cohesive package whose
directory happens to be the module root — which is the dominant Go
convention for a library whose primary import path is the module path
(`net/http`, cobra, zap, lipgloss, bubbletea all look like this). The
thing that *looks* messy is `ls` interleaving 23 files with 20
directories. That is a listing problem, not a layering problem.

The one file that is genuinely arguable is `input.go` at 715 lines
holding three separate concerns; see §5.

## 2. The facade mechanism's exact limits in Go (go 1.25.6)

Verified in a scratch module, not recalled:

**Type aliases work, and better than expected.** All 49 exported root
types alias cleanly, and the 161 methods on them ride along for free —
methods cannot be aliased, but they do not need to be, because the alias
denotes the same type. Confirmed working through an alias:

- **Embedding.** `type Widget struct { gooey.Base }` keeps the field name
  `Base` (the *alias's* name is the field name), and both `w.Base.Bounds()`
  and the promoted `w.Bounds()` resolve. This matters — 56 external
  references embed `Base`.
- **Composite literals.** `gooey.Size{W: 1, H: 2}` works; so does
  `gooey.FocusState{}` and `gooey.HoverState{}`, whose fields are
  unexported (`hovered *prop.Property[bool]`) — users only ever need the
  zero value, and no exported root struct with unexported fields is
  constructed by literal anywhere in or out of the repo. Audited: clear.
- **Interface satisfaction across the boundary.** Identical types, so
  `components.Text` still implements `gooey.Component`. No root interface
  has unexported methods.
- **Typed constant enums.** `const AlignStart = core.AlignStart` keeps
  both the value and the type.

**Functions are the weak spot.** 32 exported funcs, none aliasable. Two
options, both lossy:

- `var NewApp = core.NewApp` — works for every call, including variadics.
  But it turns an immutable function into a **mutable package-level
  variable** any importer can reassign; it moves the doc comment onto a
  `var`; it forecloses inlining; and `go doc` files it under VARIABLES.
- `func NewApp(c Content, o ...Option) *App { return core.NewApp(c, o...) }`
  — keeps the declaration shape and immutability, at the cost of
  duplicating 32 signatures that must now drift in lockstep.

**Generics: not a problem here.** The root exports no generic functions
or types (`prop.Property[T]` lives in `prop`, which does not move), and
go 1.25.6 supports generic aliases anyway. Non-issue.

**Tests move with their package.** 5 of the 7 root test files are
`package gooey` and reach internals (`a.screen`, `a.suspend`, the signal
handles); they move to `core` verbatim. Checked for the cycle hazard that
would have blocked this: the internal tests import only
`input`/`prop`/`render`/`term`, never `components` or `markup`, so no
import cycle appears. `dispatch_test.go` is already `package gooey_test`
and could stay at the root as a facade acceptance test.

**The limit the facade cannot route around — runtime identity.**

```
%T of aliased func type: core.Command      (not aliasprobe.Command)
%T of aliased struct:    core.Size
error-style: need core.Command
```

The blast radius in this repo, all of it invisible to the compiler:

- **`mcp/tools.go:334` — a behavioral break, not a cosmetic one.**
  `func plainCommand(goType string) bool { return goType == "gooey.Command" || goType == "func()" }`
  gates the MCP `call_command` tool and the `kind: "command"` field of
  `list_values`. After the move, every plain `gooey.Command` reports
  `core.Command`, `plainCommand` returns false, and MCP answers
  `"Increment" is core.Command, not a command`. `mcp/` is a **separate
  module**, so the stated acceptance test ("external modules compile
  untouched") passes while the tool regresses. `mcp/mcp_test.go:702`
  would catch it — but the only fix is to edit `mcp/`, violating the
  acceptance criterion, and it bakes `core.Command` into the wire
  contract.
- `control/value.go:200` sets `ValueEntry.GoType` from `%T`; `grpc`
  forwards it as `DeclaredValue.GoType`; `control/snapshot.go:89` does
  the same for node `type`. All become `core.*` for root types.
- `markup/usercontrol.go:255` says `need gooey.Command, *gooey.Cmd or
  func()` — a hand-written string, so it survives the move and becomes a
  **lie** about what `%T` now prints. `mcp/mcp_test.go:908` asserts on it.
- `cmd/statedemo` prints `%T` of the focused/hovered component in its
  JSON pane (a demo whose GIF the last reorg already had to mark stale).

The package-reorg record had to warn "scripts matching those strings must
be updated" once, for component types. Doing it a second time — to the
framework's *identity* types, for zero functional gain — spends the same
compatibility budget on nothing.

## 3. Cycle analysis: the mechanism is sound, and that is not the problem

Moving all 23 files as **one** package is correct and sufficient:

- Root's only intra-module imports are `graphics`, `input`, `prop`,
  `render`, `term`. None of those imports the root, so `core` inherits a
  clean, acyclic position — it sits exactly where the root sits now.
- Today's mutual references (`App`↔`Composer`↔`Frame`↔`FocusManager`,
  `signals*.go` and `placements.go` as methods on `App`/`Composer`) stay
  intra-package and need no new exports. Splitting into *several*
  packages would not work: `signals*.go` and `placements.go` are
  method-only files over `App`/`Composer` internals, and separating them
  would force a pile of new exported accessors — strictly worse.
- Root becomes facade-only; `components`, `markup`, `control`,
  `handlers/*`, `cmd/*`, `docs/learn/examples/*` and the 7 satellite
  modules (`mcp`, `grpc`, `handlers/exec`, `handlers/temporal`,
  `imagefmt/svg`, `packs/temporal-visibility`, `examples/kanbandemo`)
  compile untouched. 26 in-module packages import the root today; the
  external surface they actually use is ~70 symbols of the 96 exported.
- `internal/core` vs `core/`: `internal/` would additionally hide the
  package from pkg.go.dev entirely, so the documentation would not merely
  move — it would become unpublishable. `core/` publishes, but then the
  package users import has no docs and the package with the docs is not
  the one they import.

**So the facade is technically feasible.** I am not recommending against
it because it cannot be built. I am recommending against it because
building it is a net loss.

## 4. Ledger

**Improves**

- Root `.go` file count 23 → 1 (or 2 with `doc.go`). `ls` reads as
  directories. This is the whole benefit and it is not nothing — first
  impressions of a repo matter.
- Marginally clearer *labelling*: a directory literally named `core`.

**Degrades**

- **`go doc` on the root becomes empty.** Measured on the probe:
  `go doc -all` over an alias facade prints the alias lines and nothing
  else — no methods, no fields, no doc comments. `go doc . Base` prints
  one line. This repo's root doc comments are unusually substantial
  (the `App` comment alone explains the run-loop design; `Action`
  explains why CanExecute-as-computed beats XAML's
  `CanExecuteChanged`). All of it would disappear from
  `go doc github.com/WonderForgeLabs/gooey`, the first thing a new user
  runs. The stated benefit "godoc grouping" is backwards: it regresses.
- **A 96-declaration hand-maintained file whose failure mode is
  silent.** A forgotten alias produces no error anywhere — the symbol
  simply stops existing for users. Guarding that needs a
  surface-diffing test (`go/packages` or a golden list), i.e. more
  machinery in service of no functional gain. The roadmap (xmlns handler
  namespaces, `x:Property`, styles/DataTemplates, mouse capture,
  spatial focus, damage-rect flushing, `gooey gen`) touches root API
  repeatedly; every one of those changes becomes a two-file edit
  forever.
- Runtime type names, `%T`-derived wire fields, and stack traces all say
  `core.*` — see §2. One of them is a live behavioral break in another
  module.
- `NewApp` and 31 friends become either mutable globals or duplicated
  wrappers.
- One more hop for contributors: "where is `Component` defined" gets a
  two-part answer for the rest of the project's life.

The trade is: a nicer `ls`, paid for with the package documentation, a
permanent maintenance tax with a silent-failure mode, and a wire-visible
runtime rename. That is not close.

## 5. Alternatives

**A. Accept the root package (recommended).** Ratify what #2 decided
implicitly. Two cheap, non-breaking follow-ups worth doing on their own
merits, neither of which needs a facade:

- Add a `doc.go` holding the package overview, and retire the word "POC"
  from it — the current package doc opens "Package gooey — POC of the
  retained visual tree / component model," which undersells a framework
  with a gRPC control plane and an MCP server.
- Split `input.go` (715 lines, three concerns) into `command.go`
  (`Action`/`Command`/`Cmd`), `focus.go` (`FocusManager` + the focus
  interfaces) and `keybinding.go`. Same package, zero API risk, real
  navigability win. Note this *raises* root file count — the honest
  version of "organize the root" and the cosmetic goal point in opposite
  directions, which is itself the argument that the cosmetic goal is the
  wrong target.

**B. Move core to `gooey/core` and delete the root package outright.**
The coherent version of "core does not belong at the root." It gets the
same cosmetic win with *no* permanent alias tax and *one* place for the
docs — and it is honest about being a breaking change instead of
pretending compatibility the `%T` layer does not deliver. The module is
untagged, so the cost is a one-time sweep of ~26 packages, 7 satellite
modules, and every doc/tutorial/example. Rejected anyway, for a reason
that is aesthetic but load-bearing: `gooey.Component`, `gooey.Command`,
`gooey.NewApp` read as a framework's vocabulary; `core.Component` reads
as somebody's internal package. But if the root-file count is ever
judged worth a break, this is the option to take — not the facade.

**C. Middle option: move only the peripheral files.** Audited each
candidate:

- `signals*.go` — methods on `*App` over `a.screen`/`a.suspend`/the
  signal handle; exports nothing. Moving means exporting App internals.
  **No.**
- `placements.go` — methods on `*Composer` plus `EncoderFor`; exports one
  function. Same coupling. **No.**
- `companion*.go` (1 036 lines + 465 test) — the only real candidate, and
  it is not a facade job. Public surface: `Companion`, `CompanionPhase`,
  `PhaseStart`/`PhaseRun`, `CompanionError`, `CompanionFunc`,
  `CompanionCmd`, `CompanionOutput`, `CompanionKillDelay`, `CmdOption`,
  the three `WithCompanion*` options and three `App` methods. The
  *interface* and error type must stay in the root (`App` drives the
  lifecycle and `WithCompanions` takes them), so what could move is the
  two implementations — `CompanionFunc` and the `os/exec` process-group
  machinery of `CompanionCmd`, ~500 lines. The root would shed its
  `os/exec` dependency and the stutter would improve
  (`companion.Func`, `companion.Cmd`, `companion.Output`).

  But this is a **breaking public rename**, not a file move: 15+ external
  references plus README, `docs/learn`, and
  `docs/specs/2026-08-10-companions.md`. And most of `companion_test.go`
  tests the *App lifecycle* (start-before-terminal, quit-on-exit,
  teardown-joins, survive-suspend), so the test file would have to be
  split rather than moved. Worth proposing; it needs Elan's decision,
  not an agent's, and it is orthogonal to the facade question. **Not
  executed here.**

## Verification behind this record

- Alias semantics probed in a scratch module on go 1.25.6: `%T` through
  an alias, embedded-alias field naming and method promotion, composite
  literals, `var`-facade calls, and `go doc -all` over a facade.
- Root API counted from source: 49 exported types, 32 exported funcs, 15
  exported consts, 161 methods on exported types.
- Import graph from `go list` (root's imports, TestImports, XTestImports;
  the 26 in-module importers of the root).
- External symbol usage counted across every module in the tree
  (~70 distinct root symbols referenced).
- `%T`-as-protocol sites found by grep across all 8 modules.
