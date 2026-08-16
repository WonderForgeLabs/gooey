# Concept: input routing

Keys and mouse reports arrive interleaved on the same wire, so gooey
keeps them on **one ordered stream** of `input.Event` rather than two
channels that could reorder relative to each other. `term.DecodeEvents`
pumps that stream from its own goroutine; `comp.Handle(ev)` routes each
event on the UI goroutine.

## Keys walk up from focus

A key event starts at the **focused component** and walks its ancestors to
the root. At each level:

1. the `KeyBinding`s declared on that component are matched, then
2. that component's key-handling **attachments** — how a `TypeAhead` sees
   letters its host would otherwise claim — then
3. that component's own `HandleKey`.

That middle step is load-bearing in both directions — an attachment must
see letters before its host claims them, but after any binding the host
declared — and `TestAttachmentKeysPrecedeHost` pins it.

The first handler returning true stops the walk. This is what scopes a
binding: it fires only while the focused component's path to the root passes
through the element it was declared on. A binding on the page root is
global; one inside a pane fires only while that pane has focus.

What the focused chain declines goes to **page-scoped mnemonics**: every
`MnemonicHandler` in the tree, in tree order, is offered the event
regardless of what holds focus, which is how a `MenuBar`'s alt+letter opens
a menu without the bar being focused.

Only then does the **unconsumed tail** run framework navigation:
`tab`/`shift+tab` move focus in tree order, and arrow keys
move focus spatially (nearest stop in that direction, preferring targets
in line; falling back to tree order so a direction is never a dead end).
Because navigation runs last, a component that handles its own arrows simply
keeps them.

> A page with no focus stops starts dispatch at the root, so only
> root-attached bindings can fire — mnemonics are unaffected, since they
> are matched against the tree rather than the focused chain.

## The pointer hit-tests instead

A mouse event finds its target by hit-testing the retained tree — deepest
component first, later siblings before earlier ones — then bubbles up the
same ancestor chain. Four framework behaviors run before your code sees
anything:

- **The frozen retarget**, first, because everything below is measured
  against its result. A component may declare its subtree frozen (a design
  surface). `HitTest` still returns the deepest component — that is a
  query — but dispatch routes to the outermost frozen ancestor, which takes
  the event, the capture, the focus a press moves, and the click
  synthesized on release. A frozen subtree is also out of focus order,
  scoped bindings, mnemonics and hover watchers. See the
  [design-surface spec](../../specs/2026-08-11-design-surface.md).
- **Hover** moves to the nearest hover target at or above the hit.
- **A press focuses** the nearest focusable component at or above the hit,
  and failing that the first focusable descendant — which is what makes
  clicking a pane's border focus the pane.
- **Implicit capture**: the press captures the component it landed on, so
  every pointer event routes there until the release regardless of what
  the pointer is over — that is what makes drags work outside a
  component's own bounds. Hover transitions are suppressed meanwhile. A
  `MouseClick` is synthesized on release when the pointer is still inside
  the captor, carrying a click `Count` so a double click is `Count` 2 —
  click synthesis and counting,
  [#36](https://github.com/WonderForgeLabs/gooey/issues/36).

Mouse reporting is on unless you decline it (`gooey.WithoutMouse()`), and teardown
disables it unconditionally.

## Why focus damage is free

Focus and hover are ordinary **source properties** (`FocusState`,
`HoverState`). A component that reads `IsFocused()` while painting picks up
focus changes as ordinary damage, so moving focus repaints exactly two
components. There is no focus-specific redraw path.

## Tunnelling comes first

Before the bubble, the event **tunnels**: every ancestor from the root
down to the target implementing `PreviewKeyHandler` (or
`PreviewMouseHandler`) is offered it, and the first to take it ends the
dispatch. That is the parent-veto phase — modal scrims, masked inputs, an
overlay layer — and it is opt-in, so nothing routes differently until a
component asks for it. The full order is **tunnel down → target and
bubble up → mnemonics → framework fallbacks**.

The preview phase, explicit capture and conditional commands landed
together in [PR #86](https://github.com/WonderForgeLabs/gooey/pull/86).

## Commands can say when they are runnable

Event fields are `gooey.Action`: `Run()` plus `CanExecute()`. A plain
`gooey.Command` (still just `func()`) is always runnable;
`gooey.NewCommand(run).When(cond)` adds a bool property as the condition.
Because the graph decides subscriptions by call site, a Button asking
`CanExecute()` *while painting* has subscribed to the condition — the flip
repaints that one button — while the same call from a key handler is only
a question. There is no `CanExecuteChanged` event because the property
graph already is one.

## Current limits

No triple click, no drag threshold, and no system clipboard — `TextBox`
cut and copy use a process-local kill buffer. Triple-click and OSC 52 are
tracked in [#106](https://github.com/WonderForgeLabs/gooey/issues/106).

Depth: [architecture.md — the input system](../../architecture.md#the-input-system).
