# Concept: input routing

Keys and mouse reports arrive interleaved on the same wire, so gooey
keeps them on **one ordered stream** of `input.Event` rather than two
channels that could reorder relative to each other. `term.DecodeEvents`
pumps that stream from its own goroutine; `comp.Handle(ev)` routes each
event on the UI goroutine.

## Keys walk up from focus

A key event starts at the **focused widget** and walks its ancestors to
the root. At each level:

1. the `KeyBinding`s attached to that widget are matched, then
2. that widget's own `HandleKey`.

The first handler returning true stops the walk. This is what scopes a
binding: it fires only while the focused widget's path to the root passes
through the element it was declared on. A binding on the page root is
global; one inside a pane fires only while that pane has focus.

If nothing consumed the event, the **unconsumed tail** runs framework
navigation: `tab`/`shift+tab` move focus in tree order, and arrow keys
move focus spatially (nearest stop in that direction, preferring targets
in line; falling back to tree order so a direction is never a dead end).
Because navigation runs last, a widget that handles its own arrows simply
keeps them.

> A page with no focus stops starts dispatch at the root, so only
> root-attached bindings can fire.

## The pointer hit-tests instead

A mouse event finds its target by hit-testing the retained tree — deepest
widget first, later siblings before earlier ones — then bubbles up the
same ancestor chain. Three framework behaviors run before your code sees
anything:

- **Hover** moves to the nearest hover target at or above the hit.
- **A press focuses** the nearest focusable widget at or above the hit,
  and failing that the first focusable descendant — which is what makes
  clicking a pane's border focus the pane.
- **Implicit capture**: a release belongs to the widget the press went
  down on, and a `MouseClick` is synthesized when press and release land
  on the same widget.

Mouse reporting is opt-in (`screen.EnableMouse()`), and `Restore`
disables it unconditionally.

## Why focus damage is free

Focus and hover are ordinary **source properties** (`FocusState`,
`HoverState`). A widget that reads `IsFocused()` while painting picks up
focus changes as ordinary damage, so moving focus repaints exactly two
widgets. There is no focus-specific redraw path.

## Current limits

No tunneling (preview) phase — dispatch bubbles only. No `CanExecute`, so
no automatic disabled command state. No mouse capture API beyond the
implicit press-to-release capture. No built-in editable text widget.

Depth: [architecture.md — the input system](../../architecture.md#the-input-system).
