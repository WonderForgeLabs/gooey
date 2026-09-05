# Two paint paths, one z-order rule

**Issue:** [#438](https://github.com/WonderForgeLabs/gooey/issues/438) — "the one-shot path ignores Overlay, so #430 still reproduces through gooey.Compose"
**Date:** 2026-09-05
**Follows:** the overlay layer ([#437](https://github.com/WonderForgeLabs/gooey/pull/437)) and its ranks (`2026-09-05-overlay-ranks.md`, [#439](https://github.com/WonderForgeLabs/gooey/issues/439))

## What was wrong

`gooey.Compose` / `renderTree` walked `ChildComponents()` in document
order and never consulted the `Overlay` marker. The framework had **two
exported paint paths answering "what is on top" differently**, and
[#430](https://github.com/WonderForgeLabs/gooey/issues/430) still
reproduced verbatim on the one-shot one — down to the exact string the
overlay-layer spec quotes as the failure.

The reach is what makes it more than a stale path. `Compose` is exported,
is the documented one-shot path, and is what `cmd/pixels`,
`cmd/typeahead --dump` and roughly nineteen test helpers across
`components/`, `markup/` and the root compose with. **Any future
overlay-bearing fixture asserted through `Compose` would look green while
encoding the bug.**

## The decision

The issue offered implement-or-document, and named the cost of
implementing: a second copy of the z-order rule in a package that has
one, which the next change to that rule has to find. **Neither option was
taken as stated — the rule was extracted, which retires that objection
instead of paying it.**

`overlayOf(w, parentOverlay, parentRank) (overlay bool, rank int)` is now
the single implementation. `Composer.orderPaint` asks it per paint node;
`gooey.Compose`'s new `collectPaint` asks it per component. Both then
partition into two layers and order the lifted one by rank through
`appendByRank` — a bucket pass, not a sort. Both paths call it: sharing the
rule's membership half while leaving ORDERING as two implementations was the
second copy this change set out to retire, and the one-shot path first landed
with a `sort.SliceStable` of its own.

That the extraction is real rather than nominal is checked by the tests,
not asserted here: mutating `overlayOf` fails **both** paths' tests in
one run — `TestComposeKeepsALiftedSubtreeTogether` (one-shot) and
`TestALiftedSubtreeIsNotSplitByItsChildsRank` (retained) go red together.
Two copies would have failed one.

The timing is the argument. #439 added ranks to this rule *days* after
#437 created it, which is exactly the second change that would have had
to find both copies — and it would have found only one, because the
second did not exist yet. "Documenting the limit" would have left the
one-shot path permanently behind, on a rule that has now moved twice in a
week.

## What changed in the one-shot path

`renderTree` **collects then paints**, rather than painting during the
walk. A lifted subtree cannot be painted when it is reached: its position
depends on nodes the walk has not seen. That is the same split
`Composer` already makes between `c.nodes` (structure) and `c.paint`
(order), arrived at for the same reason.

The per-component half — the declared background fill, then `Render` —
moved to `paintOne` unchanged. The depth cap and the `Collapsed` prune
stay in the walk, where they were.

**A tree with no overlay is unaffected**: one slice, nothing to sort,
painted in the order it always was. That is asserted rather than assumed
(`TestComposeStillPaintsAPlainTreeInDocumentOrder`), because ~19 test
helpers ride on it and a change in their meaning would be silent.

## What is NOT changed

- **Equal ranks keep document order** on both paths — the bucket pass is
  what preserves it, and the `Overlay` interface's documented limit
  survives untouched.
- **`Overlay` still moves paint, not input.** Neither path consults it
  for hit-testing.
- **The pixel plane.** `Compose` builds a `*Frame`, and `Frame.Flush`
  emits placements in the order they were recorded — which is now paint
  order, because that is the order `Render` runs in. The two planes agree
  on this path for the same reason they agree on the retained one.

## How the claims here are checked

| Claim | Test | Mutation that fires it |
|---|---|---|
| `Compose` lifts an overlay over a later sibling | `TestComposeLiftsOverlaysTheWayComposerDoes` | append everything to `ordinary` |
| **The two paths agree** | `TestBothPaintPathsAgree` | same |
| `Compose` honours the rank | `TestComposeHonoursTheOverlayRank` | comparator returns false |
| A lifted subtree comes up whole | `TestComposeKeepsALiftedSubtreeTogether` | drop the inherited-membership arm |
| A plain tree is unaffected | `TestComposeStillPaintsAPlainTreeInDocumentOrder` | — (guards the ~19 helpers) |
| The rule is genuinely shared | *both* files' subtree tests | any mutation of `overlayOf` reddens both |

`TestBothPaintPathsAgree` is deliberately a **comparison** rather than
two separate expectations. Two exported paths disagreeing is the defect;
either one being individually wrong is a symptom, and a test that pinned
each against a hardcoded string would keep passing if they drifted
together in the wrong direction.
