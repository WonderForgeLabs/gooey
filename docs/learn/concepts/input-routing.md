# Concept: input routing

Keys and mouse reports arrive interleaved on the same wire, so gooey
keeps them on **one ordered stream** of `input.Event` rather than two
channels that could reorder relative to each other. `term.DecodeEvents`
pumps that stream from its own goroutine; `comp.Handle(ev)` routes each
event on the UI goroutine.

## Keys walk up from focus

A key event starts at the **focused component** and walks its ancestors to
the root. At each level:

1. the `KeyBinding`s attached to that component are matched, then
2. that component's own `HandleKey`.

The first handler returning true stops the walk. This is what scopes a
binding: it fires only while the focused component's path to the root passes
through the element it was declared on. A binding on the page root is
global; one inside a pane fires only while that pane has focus.

If nothing consumed the event, the **unconsumed tail** runs framework
navigation: `tab`/`shift+tab` move focus in tree order, and arrow keys
move focus spatially (nearest stop in that direction, preferring targets
in line; falling back to tree order so a direction is never a dead end).
Because navigation runs last, a component that handles its own arrows simply
keeps them.

> A page with no focus stops starts dispatch at the root, so only
> root-attached bindings can fire.

## The pointer hit-tests instead

A mouse event finds its target by hit-testing the retained tree — deepest
component first, later siblings before earlier ones — then bubbles up the
same ancestor chain. Three framework behaviors run before your code sees
anything:

- **Hover** moves to the nearest hover target at or above the hit.
- **A press focuses** the nearest focusable component at or above the hit,
  and failing that the first focusable descendant — which is what makes
  clicking a pane's border focus the pane.
- **Implicit capture**: the press captures the component it landed on, so
  every pointer event routes there until the release regardless of what
  the pointer is over — that is what makes drags work outside a
  component's own bounds. Hover transitions are suppressed meanwhile. A
  `MouseClick` is synthesized on release when the pointer is still inside
  the captor, carrying a click `Count` so a double click is `Count` 2.

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
bubble up → framework fallbacks**.

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
cut and copy use a process-local kill buffer.

Depth: [architecture.md — the input system](../../architecture.md#the-input-system).
