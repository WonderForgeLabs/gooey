# Free-position (pointer-anchored) adornments (issue #177)

**Status:** executed.

**Date:** 2026-08-23

## What was asked for

[#177](https://github.com/WonderForgeLabs/gooey/issues/177) — the gap the
adornment layer shipped with. `Adornment` is `Anchor() Component` +
`Place(anchor, layer Rect) Rect`, and `AdornmentLayer.Arrange` drops any
adornment whose anchor is not in the live tree. Cursor- and
gesture-attached UI has no component anchor, so drag ghosts, drop
indicators, marquee rectangles and a crosshair inside a Canvas were
inexpressible. `PersistentAdornment` was named as the precedent: an
interface addition, not a redesign.

Explicitly **not** a cursor replacement, and the sibling issue
[#178](https://github.com/WonderForgeLabs/gooey/issues/178) is where
cursor *shape* lives. No portable sequence hides the emulator's pointer,
so a permanently-painted cursor glyph would double-image under the real
one, quantize to cells, and cost a frame per cell crossed forever. What
is in scope is UI that exists only *during a gesture*, which is what
bounds the any-motion wakeup.

## The two decisions

### 1. Interface shape: a rect-valued `Place`, opted into by a marker

The fork was a pointer *pseudo-anchor* — a sentinel `Component` that
`Anchor()` returns — versus a `Place` that receives the *pointer rect*.
Chosen: the second, gated by a marker interface in core.

```go
// gooey (mouse.go)
type PointerFollower interface{ FollowsPointer() bool }
```

An `Adornment` that also implements it is **free**: the layer never calls
`Anchor`, and calls `Place` with the pointer's 1x1 cell.

Why not the sentinel. It would have to be a real `Component` that is in
no tree, has no bounds and is never painted — so `anchorBounds`,
`inTree` and `visiblyReachable` would each need a special case for it,
which is the same three special cases the marker needs, plus a fake
component in the vocabulary. It buys nothing.

Why one `Place` and not a second `PlaceFree`. Placement policy is
identical either way and the existing vocabulary is already rectangular:
`PlacePopup(anchor Rect, sz Size, bounds Rect, side)` is the shared
policy, and every hand-written `Place` in the tree works on rects. A
tooltip that flips above its anchor and a ghost that flips above the
pointer would be the same three lines. A second method would fork that
surface for no gain.

**The trade, stated:** `Place`'s first parameter is now "the rect you are
pinned to" rather than strictly "the anchor's bounds", so `Place` read on
its own no longer says which. The parameter is renamed `against` and the
interface method is the discriminator. This is the cost of one placement
vocabulary and it was accepted deliberately.

### 2. Free-ness and following are different questions

Implementing `PointerFollower` makes an adornment free — a
compile-time, structural fact. `FollowsPointer()` says whether it is
tracking *right now* — a per-frame, usually property-backed answer.

They are split because the **lifetime** hangs on the first and the
**wakeup cost** on the second. `?1003h` delivers a motion report per cell
crossed; a ghost must stop costing anything the moment its gesture ends
without having to leave the layer to do it. A parked follower —
in the layer, `FollowsPointer()` false — is arranged to a zero rect:
present, subscribed, occupying and painting nothing.

Collapsing the two (drop a free adornment when it stops following) was
rejected: it would orphan a recycled ghost the instant its drag ended,
mid-gesture, which is a race for no benefit.

A free adornment is exempt from the anchor sweep **entirely**. Nothing
can orphan it, because there is no anchor to be gone; its owner puts it
up and its owner takes it down. That is `PersistentAdornment`'s opt-out
one step further.

## The wakeup, which is the whole engineering problem

The pointer is not a paint dependency of anything by default, and it must
not become one. Three mechanisms were considered:

1. **The layer reads the pointer in its `Render`.** Rejected: the layer
   spans the page, so a repaint is a full-page damage rect *per motion* —
   the exact defect `DecoratesCells` was added to remove.
2. **The follower reads the pointer in its own `Render`.** Works, costs
   nothing extra — but it is a rule the author has to remember, and a
   forgotten read is a component that goes silently deaf. That is the
   failure mode this framework has the most scar tissue about, and the
   `Popup` primitive already answered it by owning the subscription
   itself rather than asking owners to.
3. **The Composer arms an observer.** Chosen.

`Composer.armPointer` is `armVisibility`/`armFrozen`'s third sibling, and
`armFrozen`'s reasoning applies unchanged: the observer **calls**
`FollowsPointer`, so whatever the component reads to decide becomes its
dependency by the ordinary call-site rule. It is not a paint node — it
schedules a frame and counts as no damage.

The evaluation is two reads and **the order is the design**:

```go
if p, ok := n.w.(PointerFollower); ok && p.FollowsPointer() {
    c.focus.Pointer()
}
```

`FollowsPointer` runs unconditionally, so *starting* to follow schedules
the frame that re-evaluates this and subscribes to the rest. The pointer
is read only while the answer is true, so a parked follower has no edge
from it and a motion report invalidates nothing. Hoisting that read above
the branch still compiles and still passes every placement test — it just
silently buys a frame per cell for the life of the app. `TestParked
GhostCostsNothingPerMotion` is the only thing that notices.

### Where the pointer lives

`FocusManager` gains plain `ptrX/ptrY/ptrSeen` fields plus a lazily
created revision property, and `Pointer() (Rect, bool)` reads the
revision before returning them — so **the call site decides** what asking
means, exactly as everywhere else. `AdornmentLayer.Arrange` asks from
layout and records nothing; the observer asks from an evaluation and
subscribes. It is the same split the framework already uses for bounds.

`notePointer` guards on the **cell**, because `prop.Set` does not compare
values and an emulator may re-report one. It is called from
`DispatchMouse` for every kind (a ghost raised in a press handler must
find the pointer where the press landed) and deliberately **not** from
`MouseTarget`, which is a query.

`Pointer()` is a 1x1 rect rather than an x/y pair so that the placement
vocabulary stays rectangular — see decision 1.

## The customer

`components.DragGhost` — a label that travels with the pointer, the
headline unlock. `Show(mgr)` finds the page's layer, adds the ghost and
starts it following; `Hide()` stops it and takes it out again, which is
what removes the paint node and its observer. The default offset is
`{1,1}`, down and right, deliberately clear of the glyph the emulator is
drawing, and placement **clamps** at the screen edge rather than flipping
— a ghost that jumped sides near an edge would read as a rival cursor,
which is the impression this component must not give.

## Damage pins

`components/dragghost_test.go`, on an all-`Text` page (no `HoverState`
anywhere, so the counts are the ghost's alone — `tipPage`'s reason):

| what | cost |
|---|---|
| motion, nothing following | **0 frames scheduled**, 0 painted |
| motion past a *parked* ghost | **0 frames scheduled**, 0 painted |
| the same cell re-reported during a drag | **0 frames scheduled** |
| motion after `Hide` | **0 frames scheduled** |
| ghost appears | 1 painted (the ghost) |
| one cell of drag motion | 4 painted, ~46 bytes flushed |
| retitling a ghost mid-drag | 1 painted |

The zero rows assert the **invalidation** count, not the painted count,
and that is load-bearing: driving `Frame()` by hand is the harness doing
the thing under test, so `painted == 0` would also hold if the frame were
scheduled and merely found nothing dirty — a completely different and
much worse cost. `counter()` wires `Composer.OnInvalidate` to ask the
real question.

### Measured: effect and the painted count are BOTH blind here

Asked whether these pins observe the effect or the mechanism, the honest
answer needed measuring, so it was measured. Under a mutation that arms
the observer for **every** node and reads the pointer unconditionally —
i.e. the whole tree subscribes and every cell the pointer crosses wakes
the app — 12 cells of motion with nothing following:

| | shipped | mutant |
|---|---|---|
| invalidations (frames scheduled) | 0 | **84** |
| components painted | 0 | 0 |
| bytes to the terminal | 0 | 0 |
| screen byte-identical | true | true |

**Only the invalidation count moves.** The screen is identical and not one
byte reaches the wire, because the flusher is a cell diff and the wasted
frames paint nothing; the painted count is blind too, because an observer
is not a paint node — it dirties, schedules a frame, and that frame finds
no paint node dirty. What those 84 invalidations buy in production is 84
full `Composer.Frame` passes (layout over the whole tree, every sweep) for
no output at all: precisely the "frame per cell crossed forever" #177
exists to avoid.

The general lesson, which is not specific to adornments: **for a COST
claim, effect is the weaker signal, because wasted work is by definition
effect-free.** Assert `Composer.OnInvalidate` for "this costs nothing",
`Damage()`/painted for "this repaints exactly these", and cells/bytes for
"this looks right". The tests here do all three (`quiet` is the
cells-and-bytes half) and none of them substitutes for another.

### The 4, named

One cell of drag motion repaints four components, and the test names
every rect because a bare number is what lets a regression hide:

```
{6 3 9 1}   the ghost at its new rect — the only real paint
{0 3 30 1}  the filler row it just uncovered, restored
{0 0 30 5}  the Canvas
{0 0 30 5}  the AdornmentLayer
```

The last two are full-page rects that paint **no cells**.
`Composer.restoreUnder` force-dirties every paintable node intersecting
the vacated rect with no exemption for a chrome-only container or a
`Decorator`, so both ancestors are swept. This is pre-existing composer
behaviour — the same shape as the tooltip's pinned dismissal (restored
leaf + 2 swept containers) — and it is free on the wire, which
`TestDragMotionStaysCheapOnTheWire` exists to keep true.

**Follow-up worth filing:** the forward pass exempts a `Decorator` from
being forced from below, on the grounds that it owns no cells to restore.
`restoreUnder` applies no such exemption, and by exactly the same
contract it could: an `AdornmentLayer` has no cells to restore under a
vacated rect either. That would take this 4 to 3 and the tooltip's
dismissal 3 to 2. It is deliberately **not** done here, because silently
lowering damage counts pinned by `tooltip_test.go`, `toast_test.go` and
`validation_test.go` in a feature PR is exactly as much a hidden change
as silently raising them.

## Invariants touched

- **Damage discipline (invariant 3):** extended, not weakened. The wake
  is an observer, not a paint node, so no free-adornment machinery
  appears in any damage count. No existing pinned count moved.
- **The `Get` call site decides (invariant 2):** `FocusManager.Pointer`
  is built on it — one method that subscribes from an evaluation and
  merely reads from layout.
- **Input routing (invariant 6):** no dispatch-order change.
  `DispatchMouse` records the cell before routing; `MouseTarget` still
  moves nothing.
- **No reflection (invariant 1):** a marker interface and two type
  switches.

## Not in this wave

A markup surface (a free adornment is created from code during a
gesture, not declared, so `<AdornmentLayer>` still takes no children);
drop indicators, marquee/lasso rectangles and the Canvas crosshair, all
of which the interface now supports and none of which has shipped a
customer; a demo, because mouse input cannot be injected through a
recording pty and a drag ghost has no keyboard equivalent to record; and
the `restoreUnder` `Decorator` exemption above.
