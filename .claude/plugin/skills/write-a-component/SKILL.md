---
name: write-a-component
description: Write or change a gooey component without silently breaking damage tracking, layout, input routing, or goroutine confinement. Use when adding a component to components/, editing Render/Measure/Arrange, adding a container, wiring an event or command, starting a goroutine from a component, or choosing between the cell plane and the pixel plane. Covers the Get-call-site rule, the three-case pre-clear, MeasureChild/ArrangeChild, Startable stop-must-join, gooey.Action, and the three-condition pixel-tier guard.
---

# Write or change a gooey component

Every rule here is checkable, and every one of them fails **silently** when
broken — no error, no panic, a stale cell or a blank rectangle. Read the
source, not this file, when they disagree: this file is the map.

**When a change touches one of these, say so explicitly in your report.**

## The one rule under all the others: `Get`'s call site decides subscribe-vs-read

`prop.node.recordRead` (`prop/prop.go`) records a dependency edge **only when
a computed is on the eval stack**.

- Inside an evaluating node — a paint node's `Render`, a validator, a style
  computed — `Get` **subscribes**.
- Anywhere else — `Measure`, `Arrange`, an event handler, a Composer sweep —
  the identical call is a **plain read**.

Layout runs deliberately outside any evaluation context, which is why
`MeasureChild` can sync `Layout.Visibility` from a bound source without
creating a dependency; the Composer arms a separate observer for that
(`Composer.armVisibility`).

So: reading a property while painting **is** the damage declaration. There is
no `AffectsRender` and no `InvalidateVisual`. `Composer.build` wraps each
`Render` in a `prop.NewComputed`, which is what makes every component's
`Render` its own paint node and makes "a change repaints exactly the
components that read it" true.

```sh
command grep -n 'recordRead\|evalStack' prop/prop.go
command grep -n 'NewComputed' composer.go
```

### The trap that follows directly from it

**Dependencies are recorded by the `Get` that actually runs.** A `Get` behind
an early `return`, or on the short-circuit side of `&&`/`||`, drops out of the
dependency set on the frames where it does not execute — and the component goes
deaf to that property. No error. A stale cell.

Hoist `Get`s above early returns and OR the results afterward:

```go
// wrong: on a frame where enabled is false, hover is never read,
// so this component stops repainting when hover changes
if !c.enabled.Get() { return }
if c.hover.Get() { ... }

// right
enabled, hover := c.enabled.Get(), c.hover.Get()
if !enabled { return }
if hover { ... }
```

Damage-count tests catch this; nothing else does.

**Also:** `prop.Set` does not compare values. Setting a property to what it
already holds still invalidates every dependent and still costs a repaint.
Guard at the call site if you need idempotence.

## Containers: paint only your own chrome

The interface is one method:

```go
type Container interface{ ChildComponents() []Component }   // component.go
```

The framework walks children. **The container never does.** Parents never call
`child.Measure` / `child.Arrange` — they call `MeasureChild` (`layout.go`) and
`ArrangeChild` (`layout.go`), which apply the margin / size / align /
visibility sandwich. Skipping them silently drops all four.

A component calling `Base.Arrange(b)` on *itself* is fine and common.

`ChildComponents()` is **four seams at once** — input routing, damage
granularity, adornments, and `Startable` lifetimes all walk it. Hiding a
subtree by returning fewer children to stop input from reaching it also kills
its damage granularity, its adornments, and its Startables. Stop the *routing*
instead.

### Pre-clearing is three cases, not two

In `composer.go` (search `clearStyle`):

1. **A leaf** pre-clears its bounds — to **the nearest ancestor's background**,
   not the terminal default. That is what stops a `Text` in a coloured panel
   from punching a hole.
2. **A chrome-only container pre-clears nothing.** Its bounds enclose children
   whose own clean nodes will not repaint. Getting this wrong is the bug that
   once wiped pane interiors.
3. **A hidden container, and a container with a declared `HasBackground`
   handle, *do* fill their bounds** and are marked `covered` — which makes the
   z-ordered pass force their subtree to repaint above them in the same frame.

If you are adding a container, decide which of the three you are and check it
with a damage count, not by looking at the screen.

## Events are `gooey.Action`, not `func()`

```go
type Action interface { ... }                                  // input.go
func CanExecute(a Action) bool { return a != nil && a.CanExecute() }
```

- A bare `func(){}` literal **does not assign**. Wrap it: `gooey.Command(...)`,
  or `gooey.NewCommand(...).When(canProp)`.
- **Never test one with `!= nil`.** Use `gooey.CanExecute(a)`, which is
  nil-tolerant *and* consults `CanExecute()`.
- A disabled binding keeps **bubbling** rather than being consumed.

## Input: one ordered stream, target-first then bubble

Keys and SGR mouse reports arrive interleaved on **one** wire and stay on one
channel (`input/mouse.go`) — two channels could reorder them.

`FocusManager.Dispatch` walks focused → ancestors, interleaving each node's
scoped `KeyBinding`s with its own `HandleKey`; `DispatchMouse` does the same
from the captor-or-hit component. KeyBindings are **scoped by their host
component**, so one only fires while the focused chain passes through it —
a binding on an inner Grid silently never fires on a page with no focus stop.
Put it at root scope there.

Unconsumed arrows fall through to spatial focus navigation (`FocusDir`).

Focus and hover are ordinary source properties (`FocusState`, `HoverState`) —
that is the entire reason moving focus repaints exactly two components, and
why that is a number a test can assert.

## Goroutines: the Dispatcher is the only route

Properties are unlocked by design. **Nothing off the main loop may `Get` or
`Set`.** Async work posts a closure (`Dispatcher.Post`, safe anywhere) and the
loop runs it (`Drain`, UI goroutine only). A `Startable` gets `post` as the
*only* legal route to the graph, and nothing in the framework will catch a
violation — the `-race` tier in CI is what catches it, which is why those
modules race.

### A `Startable`'s stop func must close **and join**

```go
// wrong: a tick that already won its select can post AFTER Close
func() { close(done) }

// right: joining is what makes stop a barrier — Close ⇒ no further posts, ever
func() { close(done); <-stopped }
```

`components/timer.go` is the canonical one; `spinner.go` and `progressbar.go`
do the same, and Tooltip/Toast use the `wg.Wait()` variant. `close(done)` alone
makes lifetime tests flake, and a goroutine-count diff will **not** catch it in
either direction — assert the effect with a chosen margin instead.

## The pixel tier needs all three conditions

The cell plane (`render`) and the pixel plane (`graphics`) are separate planes,
not alternative backends. A component choosing between them must check
**three** things:

```go
if f.Graphics != nil && f.CellW > 0 && f.CellH > 0 {   // components/image.go
```

An encoder scales its output to `cols*CellW x rows*CellH`, so a **zero in
either dimension asks it for an image of no pixels** — and taking the pixel
branch is also what stops halfblock from painting the cells underneath. The
result is a blank rectangle with no error on any surface. `CellW` and `CellH`
are **independently** fatal; a guard checking only width is half a guard.

Core's own `Image` had this wrong for its entire life (#251), and
`docs/learn/howto/howto-custom-draw.md` — the page you read while writing your
own component — taught the width-only form. Both are fixed now. `f.CellW` and
`f.Caps.CellW` are the same value; `SetCaps` writes both.

`App.caps` backfills `term.DefaultCellW/H` for a pinned protocol, so the App
path is closed and the bug is only reachable through a `Composer` driven
directly — which is exactly why a test that goes through `App` cannot see it.

## Markup: everything resolvable fails at load time

Two tiers behind one `fs.FS` seam:

- **`Include`** = markup-only control, no code-behind. Without `<x:Property>`
  declarations its attributes *become* the child context; with them they are
  type-checked against the declared surface.
- **`UserControl`** = code-behind setup that **extends** a declared surface. A
  setup defining a name the markup already declared is a **load error** —
  declarations own the public surface.

Arity, argument types, unknown functions, undeclared xmlns prefixes: all must
fail at **load**, never as a surprise on click. When you add a load-time error,
assert its *shape*, not its existence — nearly everything in this package fails
at load, so `err != nil` proves almost nothing about which mechanism caught it.

Two structural facts that constrain designs:

- **Nothing may sit between a container and a named element.** `childSlot` is a
  closed type switch; any interposed component makes the element unpatchable and
  breaks `PatchMarkup`. This rules out per-element decorators and middleware.
- **`PatchMarkup` redirects focus, it does not destroy it.** The caret resets to
  0, and a kept `Name` whose element type changed turns subsequent keystrokes
  into commands.

## No reflection in core

Bindings resolve to typed `*prop.Property[T]` handles at **build time** —
lvalue semantics, not values — through registries and type switches. This is
what keeps a future `gooey gen` able to compile markup ahead of time, so "just
use reflection here" is a **design change**, not a shortcut.

Check the current state rather than trusting a count in prose:

```sh
cd "$(git rev-parse --show-toplevel)" && git grep -l '"reflect"'
```

(Use `git grep` or `command grep` — plain `grep` in this shell honours
`.gitignore` and will not tell you the truth about the whole tree.)

Likewise the root `go.mod`: it has a deliberately tiny set of direct
requirements, and adding one is a doctrine change, not a routine edit — see
`docs/specs/2026-08-10-pack-distribution.md`. The default answer to "this needs
an SDK" is a new nested module. Read the current list:

```sh
go list -m -f '{{if not .Indirect}}{{.Path}}{{end}}' all 2>/dev/null | head
command grep -n -A10 '^require' go.mod
```

## Before you call it done

- [ ] Every `Get` a paint node needs is hoisted above every early return and
      out of every short-circuit
- [ ] Damage counts asserted — and if a change moved an existing count, that is
      *the* change and it is justified in the PR body
- [ ] Children routed through `MeasureChild` / `ArrangeChild`; the container
      paints only its own chrome
- [ ] Which of the three pre-clear cases this container is, decided on purpose
- [ ] Event fields are `gooey.Action`; nothing is tested with `!= nil`
- [ ] Any `Startable` stop func closes **and** joins
- [ ] Any pixel-tier branch checks encoder **and** `CellW` **and** `CellH`
- [ ] Nothing touches the property graph off the UI goroutine
- [ ] New markup surface fails at **load** time, with a test asserting the
      error's shape rather than its existence
- [ ] `docs/architecture.md` and `docs/markup-reference.md` still quote code
      that exists — the review gate checks this, and a quoted guard that has
      drifted teaches the bug
