# gooey

A XAML-like TUI framework for Go: retained visual tree, lazy
dependency-property graph, XML markup with Go-template bindings,
damage-tracked rendering.

`README.md` is the tour and `docs/architecture.md` is the grounded
walkthrough; `docs/specs/` holds the decision records, `docs/learn/` the
tutorials and how-tos. This file carries only what those do not: the
rules whose violation is *silent*, and the commands that catch it.

## Verify

```sh
go vet ./...     # whole-repo compile check — use this, not `go build`
go test ./...
```

`./...` stops at the module boundary, and there are **eight nested
modules** it skips. Run each one:

```sh
for m in handlers/temporal packs/temporal-visibility imagefmt/svg \
         examples/gitui examples/kanbandemo; do
  ( cd "$m" && go vet ./... && go test ./... ) || echo "FAIL $m"
done
for m in grpc mcp handlers/exec; do
  ( cd "$m" && go vet ./... && go test -race ./... ) || echo "FAIL $m"
done
```

The `-race` on those last three is not a nicety: what their tests prove is
that no RPC, tool body, or child-process callback touches the property
graph off the UI goroutine, and without the detector that assertion is only
half made. `.github/workflows/ci.yml` is the authority on what CI runs.

Two gaps CI leaves you to cover by hand: `examples/gitui` and
`examples/kanbandemo` are **not built by CI at all**, so a core API change
breaks them silently — build them yourself when you touch the root module.
And the root module's own tests do not run under `-race` in CI.

## Invariants

Each of these is checkable. Say so explicitly in your report when a change
touches one.

**No reflection in core.** Bindings resolve to typed `*prop.Property[T]`
handles at build time (lvalue semantics, not value), through registries and
type switches. The only `"reflect"` imports in the repo are generated
protobuf under `grpc/gen/` and one test in `packs/`. This is what keeps a
future `gooey gen` able to compile markup ahead of time, so "just use
reflection here" is a design change, not a shortcut.

**The `Get` call site decides subscribe-vs-read.** `prop.node.recordRead`
(`prop/prop.go:33`) records an edge only when a computed is on `evalStack`.
Inside an evaluating node — a paint node's `Render`, a validator, a style
computed — `Get` subscribes. Anywhere else — `Measure`/`Arrange`, an event
handler, a Composer sweep — the identical call is a plain read. Layout runs
deliberately outside any evaluation context (`composer.go:472`), which is
why `MeasureChild` can sync `Layout.Visibility` from a bound source without
creating a dependency; the Composer arms a separate observer for that
(`Composer.armVisibility`, `composer.go:344`).

**Every component's `Render` is its own paint node.** `Composer.build`
wraps each `Render` in a `prop.NewComputed` (`composer.go:260`), so reading
a property while painting *is* the damage declaration — there is no
`AffectsRender` and no `InvalidateVisual`. A change repaints exactly the
components that read it.

**Containers paint only their own chrome, through `MeasureChild` /
`ArrangeChild`.** The interface is `Container { ChildComponents() []Component }`
(`component.go:38`) — the framework walks children, never the container.
Parents never call `child.Measure`/`child.Arrange`; `MeasureChild`
(`layout.go:138`) and `ArrangeChild` (`layout.go:182`) apply the
margin/size/align/visibility sandwich, and skipping them silently drops all
four. A component calling `Base.Arrange(b)` on *itself* is fine and common.

Pre-clearing is the subtle half, and it is no longer a two-case rule
(`composer.go:263-299`):

- leaves pre-clear their bounds — to **the nearest ancestor's background**
  (`clearStyle`), not the terminal default, so a Text in a colored panel
  does not punch a hole;
- a **chrome-only container pre-clears nothing**, because its bounds
  enclose children whose own clean nodes will not repaint;
- a **hidden** container and a container with a declared `HasBackground`
  handle *do* fill their bounds, and are marked `covered` — which makes the
  z-ordered pass force their subtree to repaint above them in the same
  frame.

**Markup is two tiers behind one `fs.FS` seam.** `Include` = markup-only
control, no code-behind; without `<x:Property>` declarations its attributes
*become* the child context, with them they are type-checked against the
declared surface. `UserControl` = code-behind setup that **extends** a
declared surface; a setup defining a name the markup already declared is a
load error, because declarations own the public surface. Everything
resolvable — arity, argument types, unknown functions, undeclared xmlns
prefixes — must fail at **load time**, never as a surprise on click. The
`fs.FS` seam is what makes `os.DirFS` + watcher (dev) and `embed.FS`
(release) the same code path.

**One ordered `input.Event` stream.** Keys and SGR mouse reports arrive
interleaved on one wire and stay on one channel (`input/mouse.go:52`),
because two channels could reorder them. Dispatch is target-first then
bubble: `FocusManager.Dispatch` (`input.go:574`) walks focused →
ancestors, interleaving each node's scoped `KeyBinding`s with its own
`HandleKey`; `DispatchMouse` (`mouse.go:153`) does the same from the
captor-or-hit component. KeyBindings are scoped by their host component, so
one only fires while the focused chain passes through it. Unconsumed arrows
fall through to spatial focus navigation (`FocusDir`, `input.go:646`).
Focus and hover are ordinary source properties (`FocusState`,
`input.go:163`; `HoverState`, `mouse.go:49`) — that is the whole reason
moving focus repaints exactly two components.

**UI-goroutine confinement, via the Dispatcher.** Properties are unlocked
by design, so nothing off the main loop may `Get` or `Set`. Async work
posts a closure (`Dispatcher.Post`, safe anywhere) and the loop runs it
(`Drain`, UI goroutine only) — see `App.Run`'s select at `app.go:423`. A
`Startable` gets `post` as the *only* legal route to the graph, and nothing
in the framework will catch a violation.

**No `Screen` teardown may leave a goroutine reading the terminal.**
`Screen.Restore` (`term/term.go:215`) restores modes, closes the tty, then
**joins** the decoder while draining its channel, bounded by
`term.DecoderTimeout`, with `Screen.DecoderLeaked` as the tripwire.

**Heavy dependencies live in nested modules.** The root `go.mod` has
exactly two direct requirements (`golang.org/x/image`, `golang.org/x/term`).
Adding a third is a doctrine change, not a routine edit — see
`docs/specs/2026-08-10-pack-distribution.md`. The default answer to "this
needs an SDK" is a new nested module.

## Traps

**`go build ./...` writes 23 executables into the repo root.** Every main
package under `cmd/` and `docs/learn/examples/` lands as a file named after
its directory. `.gitignore` lists 14 of them — that list exists because
they have been committed before — and it does *not* cover `toolkitdemo` or
any `docs/learn/examples/*` binary, so `git add -A` will happily commit
those. The nested modules do the same thing in their own directories —
`examples/gitui/gitui` and `examples/kanbandemo/kanbandemo` are neither
tracked nor ignored. Use `go vet ./...` for a whole-repo check, or
`go build -o /tmp/gooey-bin/ ./...` when you actually want binaries; `-o`
accepts a directory for a multi-package build and leaves the tree clean.

**`prop.Set` does not compare values** (`prop/prop.go:101`). Setting a
property to what it already holds still invalidates every dependent and
still costs a repaint. Guard at the call site if you need idempotence.

**Dependencies are recorded by the `Get` that actually runs.** A `Get`
behind an early `return`, or on the short-circuit side of `&&`/`||`,
drops out of the dependency set on the frames where it does not execute,
and the component goes deaf to that property — no error, no panic, just a
stale cell. Hoist `Get`s above early returns and OR the results afterward.
Damage-count tests catch this; nothing else does.

**A damage-count assertion is the only pin for a repaint claim.**
`Composer.Frame()` returns `(*Frame, int)` where the int is how many
components repainted; `App.PaintedLastFrame()` is the same number on the
runtime, and `Composer.Damage()` the rects. A bounds assertion or a "the
cell says X" assertion passes just as well when the entire tree repainted,
so it proves nothing about damage. Roughly three dozen tests already assert
exact counts (`components/input_test.go`, `mouse_test.go`,
`background_test.go`, `tabs_test.go`, `itemsview_test.go`,
`validation_test.go`, `dynamic_test.go`, `format/graph_test.go`,
`grpc/e2e_linux_test.go`). They are contract tests: if your change moves a
number, that *is* the change, and it needs justifying rather than updating.

**Event fields are `gooey.Action`, an interface** (`input.go:36`), not
`func()`. A bare `func(){}` literal does not assign — wrap it in
`gooey.Command(...)` or `gooey.NewCommand(...).When(canProp)`. Never test
one with `!= nil`; use `gooey.CanExecute(a)` (`input.go:49`), which is
nil-tolerant and also consults `CanExecute()`. A disabled binding keeps
bubbling rather than being consumed (`input.go:587`).

**A `Startable`'s stop func must close AND join.** `close(done)` alone lets
a tick that already won its select post *after* `Close`, and lifetime tests
flake. The idiom is `func() { close(done); <-stopped }`
(`components/timer.go:71`; also `spinner.go:132`, `progressbar.go:112`;
Tooltip and Toast use the `wg.Wait()` variant). Joining is what makes stop
a barrier: Close ⇒ no further posts, ever.

**Never call `Fd()` on the tty.** `os.File.Fd()` puts the file in blocking
mode and removes it from Go's netpoller; after that a pending `Read` is an
uninterruptible syscall that `Close` cannot cancel, and `SetReadDeadline`
fails with `ErrNoDeadline`. `Screen.control` (`term/term.go:86`) routes
every ioctl through `SyscallConn().Control` for exactly this reason. The
bug record, including the A/B evidence and the ordering subtlety that made
it look intermittent, is `docs/specs/2026-08-10-tty-read-lifecycle.md`.
`term`'s pty-backed lifecycle tests only compile on Linux.

**Headless verification is already written down.** Driving an app under
`script -qec` with `stty`, octal `printf` escapes (`sh` has no `\x`),
replaying the log through `render.Screen` instead of hunting for the last
`\x1b[H`, and the asciinema/`agg` recipe are all in
`docs/learn/howto/howto-testing.md`. Two consequences worth internalizing:
mouse input cannot be injected through a recording pty, so every feature
must stay keyboard-operable; and `agg` renders the cell plane only, so
captures need halfblock, never sixel or kitty.

## Known-bad on main

Neither of these is your fault, and a red suite from either is pre-existing:

- `handlers/temporal` — `TestPagingKeysPageTheDashboardSelection`
  (`internal/ops/ops_test.go:614`) **fails under `-race`**: an activity
  goroutine started from a Command reaches the graph concurrently with the
  test. Issue **#182**. The module is green without `-race`, which is what
  CI runs for it.
- `TestSIGWINCHResizesTheComposition` (`signals_linux_test.go:104`, root
  module) is a timing **flake** — it waits on real deadlines and splits the
  transcript on sync markers. Issue **#183**.
