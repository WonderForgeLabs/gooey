# Container backgrounds and paint-level damage

**Status:** deferred — analyzed, not implemented. Recorded so the next
attempt starts from the analysis rather than from the bug.

**Date:** 2026-08-10

## What was asked for

`Background` on `Border`: fill the interior with a color, so a panel can
be a colored surface rather than a frame drawn on the terminal's default
background.

## Why it was not shipped

A background looks like a one-line change to `Border.Render`. It is not.
It collides with the damage system in two separate places, and only the
first has a cheap fix.

### Problem 1: the leaf pre-clear punches holes (fixable)

`Composer.build` pre-clears a leaf's rect before repainting it:

```go
if _, isContainer := w.(Container); !isContainer {
    if b, ok := w.(Bounded); ok {
        clearRect(c.frame.Cells, b.Bounds())
    }
}
```

and `clearRect` writes `render.Style{}` — the terminal default. So a
`Text` inside a background-painted `Border`, repainting on its own,
clears its rect to *black* and leaves a rectangular hole in the
background.

The fix is real and not large: clear to the nearest ancestor's background
rather than to the zero style. The composer would walk the ancestor chain
(it already builds one for the focus tree) and ask for a clear style
through a small interface. Because that read happens **inside the leaf's
paint node**, reading the ancestor's `Background` property registers a
dependency automatically — so changing a panel's background dirties every
descendant that clears against it, which is exactly right, and falls out
of the existing graph with no new invalidation machinery.

### Problem 2: a filling container overpaints its clean children (the blocker)

Containers paint only their own chrome. That rule exists because a
container's bounds *enclose* its children's cells, and the children have
their own paint nodes: if a container paints over that area, it destroys
content whose nodes are clean and will not repaint.

A background fill is, by definition, painting over that area. So the
moment `Border` fills its interior, any repaint of the Border — a title
change, a style change, a bounds change — wipes every child inside it
until something independently dirties them.

The fix is not a clear-style tweak; it is z-ordered repaint: when a
widget that paints over its subtree repaints, its descendants must
repaint too, in order. That is implementable in the current design —
`c.nodes` is in depth-first pre-order, so bumping descendants' `rev`
during the ancestor's paint would still reach them in the same frame —
but it means **mutating the graph during an evaluation**, a `Set` inside
a computed's `compute()`. Everything about the property system's
correctness rests on the discipline that evaluation only reads. Breaking
that for one feature, at the end of an unrelated pass, is how a lazy
graph acquires an ordering bug that surfaces months later as one stale
frame under load.

The gap cells matter too, and rule out the obvious dodge: the interior of
a container is not fully covered by its children (a `VStack` with a `Gap`
leaves rows nobody owns). Those cells have no paint node, so *only* the
container can fill them — "let the leaves maintain the background" does
not work.

## Decision

Skip `Background` in the visual pass. Ship the color-depth work, the
Canvas, and the ColorPicker, none of which need it.

Do not ship the half version. A background that is correct until a child
repaints is worse than no background: it fails intermittently, in a way
that looks like a rendering glitch rather than a missing feature.

## What a real implementation needs

1. A `ClearStyle`-style interface and ancestor-aware `clearRect` (problem 1).
2. A z-order-aware repaint pass for widgets that paint over their subtree
   (problem 2), designed against the evaluation-only-reads discipline —
   most likely by collecting "also repaint these" targets during the frame
   and draining them in the composer's loop, *outside* any `compute()`,
   rather than by calling `Set` mid-evaluation.
3. Damage-count tests: a child repainting alone over a background must
   paint exactly 1 widget and leave no hole; a container repainting must
   repaint its subtree and leave no wiped children.

Item 2 is the interesting one and is the natural companion to
damage-rect flushing, which also wants to reason about overlapping
regions. They should probably be designed together.

## Related

`Canvas` (landed in the same pass) exposes the same underlying limit
without introducing it: overlapping children are legal there, and an
occluded child repainting alone paints over its occluder. That is pinned
by `TestCanvasOverlapRepaintLeavesOccluderDamaged` and documented in the
`Canvas` doc comment. Both are the same missing capability — the composer
has no notion of z-order — seen from two directions.
