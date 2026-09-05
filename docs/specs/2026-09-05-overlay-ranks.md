# The overlay layer is ranked, not declaration-ordered

**Issue:** [#439](https://github.com/WonderForgeLabs/gooey/issues/439) — "three overlay hosts did not adopt Overlay, so a popup now paints above toasts and tooltips"
**Date:** 2026-09-05
**Supersedes part of:** `docs/specs/2026-08-30-overlay-layer.md` ([#437](https://github.com/WonderForgeLabs/gooey/pull/437))

## What was wrong

[#437](https://github.com/WonderForgeLabs/gooey/pull/437) lifted overlays
out of document order into a second paint layer, fixing
[#430](https://github.com/WonderForgeLabs/gooey/issues/430). **Only
`popupSurface` adopted the marker.** `ToastHost`, `AdornmentLayer` and
therefore every `Tooltip` stayed in the ordinary layer, which put them
*beneath every open popup*.

That reversed three written claims at once, none of which had a test:

- `components/toast.go` stated as design fact that being the root's last
  child "puts every toast above the page";
- `docs/markup-reference.md` went further — declare the `AdornmentLayer`
  after the `ToastHost` "and tooltips paint above toasts too";
- `components/menu_live_test.go` asserted in a *comment* that "the toast
  layer is topmost".

Three statements of one ordering, in three files, and the suite was green
through the reversal. That is the shape of the defect as much as the
z-order is: **an ordering claim that lives only in prose costs nothing to
break.**

## The decision

The issue offered two options — adopt the marker on all three and leave
ordering *within* the layer to declaration order, or write the reversal
down as the new rule. Neither was taken. The layer carries a **rank**.

```go
type OverlayRanker interface {
	Overlay
	OverlayRank() int
}

const (
	OverlayRankPopup     = 0   // the floor: every pre-rank Overlay lands here
	OverlayRankToast     = 10
	OverlayRankAdornment = 20
)
```

`Composer.orderPaint` stable-sorts the lifted set by rank.

**Why not declaration order.** It can express the right stacking, but
only if the author declares the `ToastHost` *after* the `MenuBar` — while
the framework separately tells them to declare the `MenuBar` last so its
dropdown covers the page. Two rules pulling in opposite directions, with
a silent wrong answer when you follow the wrong one: the toast simply
does not appear, on exactly the frames somebody most wanted to read it.
A layering that depends on where someone typed an element is a layering
that will be wrong in some app.

**Why not document the reversal.** It makes the worst failure mode the
specified behaviour. A toast is a notification the user did not ask for
and cannot re-request; hidden behind a dropdown they opened, it is not
delayed but *lost*, and nothing tells them it happened. A dropdown is
something they are looking at on purpose and can dismiss. The ordering
the three docs already claimed is the right one — the bug was that
nothing enforced it.

**Why these three ranks.** Read the order as the user-facing sentence:
*a popup covers the page, a toast covers the popup, a tooltip covers the
toast.* An adornment is top because it describes something already on
screen and says nothing from underneath it. Spaced by ten so an app can
sit between two without patching the framework — a rank is an `int`, not
an enum, and the list is not exhaustive.

**The floor is 0 and that is load-bearing**, not a default chosen for
tidiness: every `Overlay` written before ranks existed keeps behaving
exactly as it did, and popup surfaces were already at the bottom of the
layer.

## The one clause no behaviour reaches

The rank belongs to the lifted subtree's **root**, not to each node — a
nested `Overlay` inside an already-lifted subtree keeps the *outer* rank.
`orderPaint` tests `inherited` *before* the marker for exactly this, and
the ordering of those two switch arms is the whole of it.

Ranks that varied inside one subtree would let the sort separate a
container from its children. A rank-2 container holding a rank-0
`Overlay` would sort the child ahead of the parent, the parent would
paint after it, and a parent that covers its bounds would erase the very
child it lifted. The forward-only forcing pass cannot put it back —
which is the reason the overlay layer exists in the first place.

No user-facing behaviour reaches this today, so it is pinned directly:
`TestALiftedSubtreeIsNotSplitByItsChildsRank` builds exactly that shape
and goes red when the two arms are swapped. Written because the first
mutation pass found the claim unfalsifiable, which is the same reason a
`clipCols` guard was deleted in the PR beneath this one — an
unfalsifiable claim is not a cheap guard, it is a statement the tests
cannot check.

## What is NOT changed

**Equal ranks still keep document order.** Two popups paint in the order
they were declared rather than the order they were opened. That is
#437's documented limit and the rank does not lift it, because the two
popups are the same *kind* — which is exactly the distinction a rank
draws and the one it does not. An open-order stack is still the machinery
that would answer it, and still worth writing when something needs it.

**The pixel plane follows for free.** `Composer.placementOps` already
iterates `c.paint` rather than `c.nodes` (fixed in #437's review), so
ranking the cell plane ranks the placements with it. Had that still
iterated `c.nodes`, this change would have given the two planes different
answers to "what is on top" — worth stating because nothing in this
change would have revealed it.

**`Overlay` moves paint, not input.** `FocusManager.HitTest` walks
document order and knows nothing about ranks either. An overlay that does
not take pointer capture is still responsible for its own routing.

## How the claims here are checked

| Claim | Test | Mutation that fires it |
|---|---|---|
| A higher rank beats a later declaration | `TestAHigherRankPaintsOverALowerOneDeclaredLater` | comparator returns false |
| Equal ranks keep document order | `TestEqualRanksKeepDocumentOrder` | `sort.Slice` with `<=` |
| An unranked `Overlay` is rank 0 | `TestAnUnrankedOverlayIsRankZero` | comparator returns false |
| A lifted subtree is never split | `TestALiftedSubtreeIsNotSplitByItsChildsRank` | swap the two switch arms |
| The layer still clears the page | `TestTheOverlayLayerStillClearsThePage` | — (guards #437) |
| **A toast is not hidden by an open menu** | `TestAToastIsNotHiddenByAnOpenMenu` | drop `ToastHost`'s rank, or its marker |
| A tooltip outranks a toast | `TestAnAdornmentIsAboveAToast` | rank `AdornmentLayer` at the floor |

`TestAToastIsNotHiddenByAnOpenMenu` is the reported bug, and its geometry
is the hard part: toasts stack *downward* from the host's top edge, one
row each, right-aligned, while a dropdown hangs from row 1 down the
*left*. The first version asserted over an empty intersection — the sole
toast sat on row 0 beside the bar and never touched the dropdown — so it
passed against the bug. It now shows three toasts, uses item text wide
enough to reach the right margin, and **asserts the two rects overlap
before asserting which one won**.
