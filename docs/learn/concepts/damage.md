# Concept: damage tracking

The `Composer` gives **every component its own paint node** in the property
graph. Evaluating that node runs the component's `Render`, so the properties
a component reads while painting automatically become the set of things that
can dirty it.

That is the whole damage model. There is no `AffectsRender` metadata to
declare and no `InvalidateVisual()` to call: *reading a property during
Render is the declaration.* A component that never reads a property is never
repainted when it changes.

Two behaviors follow, and both are observable — and pinned by the
damage-count contract tests
([#30](https://github.com/WonderForgeLabs/gooey/issues/30)):

- Setting one bound property repaints exactly the components that read it.
  Tutorial 3's `m` key prints `last frame painted 1 component(s)`
  after a state change on a page holding eleven components.
- Focus and hover are ordinary source properties (`FocusState`,
  `HoverState`), so moving focus repaints exactly two components — the one
  losing focus and the one gaining it. Nothing special-cases focus.

Layout is deliberately *outside* this system. `Measure`/`Arrange` run
unconditionally every frame and outside any evaluation context, so reads
made during layout subscribe to nothing and layout can never pollute the
graph. When a component's bounds change, the Composer force-dirties it and
clears the region it vacated.

Pre-clearing is three cases, not one, and the framework decides them inside
each paint node — container authors do not opt in:

- a **leaf** pre-clears its bounds to the *nearest ancestor's background*,
  not the terminal default, so a Text in a colored panel does not punch a
  hole when it repaints alone;
- a **chrome-only container** pre-clears nothing — its bounds enclose its
  children's cells, and wiping those would blank content whose own (clean)
  paint nodes will not repaint, so it overpaints its chrome in place;
- a **hidden** container, and a container with a declared `HasBackground`
  handle, *do* fill their whole bounds, and are marked `covered` — which
  makes the z-ordered pass force their subtree to repaint above them in the
  same frame.

Damage carries all the way to the wire: `Flush` diffs the current
buffer against the previous one and emits only the changed spans, so a
settled frame writes zero bytes and a keystroke writes tens, not the
whole screen — damage-rect flushing,
[#23](https://github.com/WonderForgeLabs/gooey/issues/23), landed in
[PR #85](https://github.com/WonderForgeLabs/gooey/pull/85).

Depth: [architecture.md — the Composer](../../architecture.md#the-composer).
