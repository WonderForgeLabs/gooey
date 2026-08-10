# Concept: damage tracking

The `Composer` gives **every component its own paint node** in the property
graph. Evaluating that node runs the component's `Render`, so the properties
a component reads while painting automatically become the set of things that
can dirty it.

That is the whole damage model. There is no `AffectsRender` metadata to
declare and no `InvalidateVisual()` to call: *reading a property during
Render is the declaration.* A component that never reads a property is never
repainted when it changes.

Two behaviors follow, and both are observable:

- Setting one bound property repaints exactly the components that read it.
  Tutorial 3's `measure` button prints `last frame painted 1 component(s)`
  after a state change on a page holding eleven components.
- Focus and hover are ordinary source properties (`FocusState`,
  `HoverState`), so moving focus repaints exactly two components — the one
  losing focus and the one gaining it. Nothing special-cases focus.

Layout is deliberately *outside* this system. `Measure`/`Arrange` run
unconditionally every frame and outside any evaluation context, so reads
made during layout subscribe to nothing and layout can never pollute the
graph. When a component's bounds change, the Composer force-dirties it and
clears the region it vacated.

One rule constrains container authors: **containers must never pre-clear
their own bounds.** A container's bounds enclose its children's cells, so
wiping them blanks content whose own (clean) paint nodes will not
repaint. Only leaf components pre-clear; containers overpaint their chrome
in place.

Damage carries all the way to the wire: `Flush` diffs the current
buffer against the previous one and emits only the changed spans, so a
settled frame writes zero bytes and a keystroke writes tens, not the
whole screen.

Depth: [architecture.md — the Composer](../../architecture.md#the-composer).
re
