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

---

## Addendum: runtime visibility (2026-08-10)

Two related gaps were found while writing the tutorials, and they split
the same way as everything above.

**Fixed now.** `Visibility` is a plain field on `Layout`, so flipping it
at runtime dirtied nothing and a widget turned `Hidden` stayed on screen
indefinitely. `Collapsed` was already safe by accident — it arranges to
zero size, so the Composer's existing bounds-change sweep caught it. The
Composer now also compares each node's visibility against the previous
frame and force-dirties on a delta, which makes `Hidden`↔`Visible`
correct for **leaves**: a leaf pre-clears its own rect, so hiding it
erases it. Pinned by `TestHidingALeafAtRuntimeErasesIt`, and
`TestUnchangedVisibilityDoesNotRepaint` guards against the sweep dirtying
unconditionally.

A **container's** own chrome still persists when it is hidden, because
containers must not clear their bounds — that would wipe children whose
paint nodes are clean. This is the same missing z-order notion as the
background problem, seen from a third direction, and it should be fixed
by the same work.

**Roadmap, not done: bindable `Visibility`.** *(Since executed — the
chosen design and the fate of the two options below are recorded in
`2026-08-10-bindable-visibility.md`.)* `Visibility="{{.ShowDetail}}"`
does not work, and making it work is not a layout change at all — it is a
*binding system* change. Every attribute binding today resolves to either
a `*prop.Property[string]` (via `bindText`) or a typed handle asserted at
a known call site (`boundProp[T]`). A bound `Visibility` needs the widget
to hold `*prop.Property[Visibility]` and read it during layout — but
layout deliberately runs OUTSIDE any evaluation context, so that read
would record no dependency and the change would not repaint. So it needs
either:

1. layout reads to become graph reads (a large, deliberate change to
   "layout runs outside the evaluation context", one of the load-bearing
   invariants), or
2. a visibility property read during *paint* with the layout pass
   consulting the last-evaluated value — cheaper, but it makes visibility
   lag layout by a frame.

Neither is a small change, and the honest workaround costs a line: bind a
computed and assign it in a command, or keep using the Composer's delta
detection, which now makes the plain-field mutation safe for leaves.
This belongs with `<x:Property>` and typed non-string attribute
bindings.

---

## Executed (2026-08-10)

Both problems, solved together as this record asked, plus the addendum's
hidden-container chrome — epic #26, landed as
[PR #88](https://github.com/WonderForgeLabs/gooey/pull/88).

**The surface.** `Background *prop.Property[render.Color]` on `Border`,
`VStack`, `HStack`, `Grid`, and `Canvas`, declared to the framework
through `gooey.HasBackground` (`BackgroundProperty()` returning the
handle — a method name, because `Background` is the field). A color, not
a `Style`: a fill is blank cells with a background; anything more is the
leaf's own styling. Markup: `Background="#rrggbb"` (the existing color
literal) or a binding to the viewmodel's `*prop.Property[render.Color]`
handle, via `bindColor`, on all five container elements. The fill itself
is the FRAMEWORK's paint, not the component's: the Composer (and the
one-shot `Compose`) fills the container's bounds before its chrome and
children go down, so "containers paint only their own chrome" survives
verbatim — `Border.Render`'s only change is that chrome drawn with an
unset style background sits ON the fill instead of punching through it.

**Problem 1** landed as analyzed: the leaf pre-clear goes to
`Composer.clearStyle` — walk the paint-node parent chain (a `parent`
pointer added to `paintNode`, maintained through `Dynamic` re-syncs),
first visible ancestor with a set background wins. The read happens
inside the leaf's paint node, so the dependency registration fell out of
the graph exactly as the record predicted: recoloring a panel repaints
the panel and every leaf that clears against it, with no new
invalidation machinery. The out-of-evaluation clears (vacated bounds,
departed nodes) use the same walk as plain reads.

**Problem 2** landed as the record's item 2 sketched — repaint targets
collected during the frame, drained outside any `compute()` — but
simpler than a queue: one forward pass over `c.nodes`, which is already
depth-first pre-order, i.e. z-order. As the loop paints, it remembers
what painted (`frameSeq` stamps); before evaluating each node it checks
whether anything already painted this frame intersects its bounds, and
if so bumps the node's `rev` — a `Set` between evaluations, never inside
one, the same legality as the bounds sweep. One pass suffices because
paint can only damage nodes LATER in z-order, and the forcing cascades
forward transitively. The record's fear — "mutating the graph during an
evaluation" — never materializes.

**Deviations and discoveries.**

1. Two exemptions were needed to keep damage counts at their old
   minima, and both are contracts, not heuristics. A chrome-only
   container never forces its own DESCENDANTS (its chrome never covers
   their cells — the same contract that lets containers skip
   pre-clearing), so a `Border` title change still paints exactly 1; it
   still forces overlapping non-descendants (a Canvas sibling occluding
   its ring). And `gooey.Decorator` (new, marker) exempts components
   that own no cells from being forced from below: ItemsView's
   `rowHighlight` re-styles cells it does not own, and without the
   exemption every content change in an unselected row would have paid
   an extra paint —
   `TestOneItemChangeRepaintsOneRow` is the pin that caught it.
2. A container whose `Background` handle is non-nil but whose color is
   UNSET still fills — with the ancestor's background — so clearing a
   background at runtime erases the old fill instead of stranding it.
   Only a nil handle keeps the container on the chrome-only path; the
   feature costs nothing where it is not used.
3. The addendum's hidden-container chrome came along for free: a
   container turning `Hidden` now clears its bounds (to the ancestor
   background) and the z-pass repaints its still-visible children above
   it. Visibility stays per-element — hiding a `Border` hides its
   chrome, not its subtree — which is what `paintable` always meant.
4. Glyph cells are not blended: a `Text` with an unset style background
   renders terminal-default cells ON a filled panel (cells have no
   alpha). The fill shows wherever the leaf's rect is blank; a leaf that
   wants to sit flush sets its own style background. Border chrome gets
   the merge because the border and the fill are the same component's
   declaration.

**Tests.** The three acceptance tests from this record, verbatim in
intent: `TestChildRepaintAloneLeavesNoHoleInBackground` (1 component, no
hole), `TestContainerRepaintOverBackgroundRepaintsSubtreeAndWipesNothing`
(border + forced child, nothing wiped, settles to 0),
`TestCanvasOverlapRepaintRepaintsTheOccluderAbove` (the inverted pin —
formerly `TestCanvasOverlapRepaintLeavesOccluderDamaged`, which
documented the artifact). Plus
`TestBackgroundChangeRepaintsTheLeavesThatClearAgainstIt` (the automatic
dependency), `TestStackBackgroundFillsTheGapCells` (the gap cells that
ruled out "let the leaves do it"),
`TestChromeOnlyContainerRepaintStaysOneComponent` and the untouched
`TestContainerRepaintPreservesChildCells` (the old minima hold), and
`TestHidingAContainerAtRuntimeErasesItsChrome` (the addendum). Markup:
`TestContainerBackgroundAttribute`,
`TestContainerBackgroundRejectsBadLiterals`.
