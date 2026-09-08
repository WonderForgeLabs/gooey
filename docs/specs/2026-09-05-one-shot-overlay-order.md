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
| `Compose` honours the rank | `TestComposeHonoursTheOverlayRank` | `rankOf` returns 0 for every item |
| **Both paths order ranks alike** | `TestBothPaintPathsAgreeOnRanks` | same — it reddens with the row above |
| **A lifted leaf occludes** | `TestBothPaintPathsAgreeOnLeafOcclusion` | drop the leaf pre-clear in `paintOne` |
| **It clears to the ancestor's background** | `TestAOneShotLeafClearsToItsAncestorsBackground` | clear to the terminal default instead |
| A lifted subtree comes up whole | `TestComposeKeepsALiftedSubtreeTogether` | drop the inherited-membership arm |
| A plain tree is unaffected | `TestComposeStillPaintsAPlainTreeInDocumentOrder` | — (guards the ~19 helpers) |
| Equal ranks keep document order | `TestBothPaintPathsAgreeOnRanks` (13 nodes, alternating) | — structural: the bucket pass appends in encounter order |
| The rule is genuinely shared | *both* files' subtree tests | any mutation of `overlayOf` reddens both |

The rank row said **"comparator returns false"** until review of #457
caught it. There is no comparator: `0df26bac` replaced the `sort` with the
bucket pass, and the mutation the table named had become impossible to
perform — an evidence table describing a test by an experiment nobody can
run is worse than a blank cell, because it reads as verified. The
replacement is measured, not proposed: making `rankOf` return 0 reddens
`TestComposeHonoursTheOverlayRank` and `TestBothPaintPathsAgreeOnRanks`
together.

"Equal ranks keep document order" has no mutation for a reason worth
stating rather than leaving as an empty cell: it is **structural**. The
bucket pass appends items in encounter order within a rank, so there is no
line to flip — the property follows from the construction rather than from
a choice, which is exactly why the bucket pass was preferred to a stable
sort. A test can observe it; no mutation can remove it without removing
the pass.

`TestBothPaintPathsAgree` is deliberately a **comparison** rather than
two separate expectations. Two exported paths disagreeing is the defect;
either one being individually wrong is a symptom, and a test that pinned
each against a hardcoded string would keep passing if they drifted
together in the wrong direction.

## Round two: ordering was shared, occlusion was not

Review found that `Compose` delivered **position without occlusion**, and
it is the more interesting half of #438's premise: the two paths agreed
about what was in front, and disagreed about whether being in front meant
anything. Measured on an overlay leaf that writes two runes of a
twelve-column rect:

```
gooey.Compose   "XX@@@@@@@@@@"     <- the sibling beneath shows through
Composer.Frame  "XX          "     <- occluded
```

`Composer.build` pre-clears every LEAF to the nearest ancestor's
background (`clearStyle` walks paint-node parents). `Compose` has no paint
nodes to walk up through, so it had no equivalent and simply painted the
overlay's runes on top of whatever was there. `components.Popup`'s own doc
cites that pre-clear as where `popupSurface`'s opacity comes from — it
draws a border and a title and leaves the middle to the clear — so a popup
composed through `Compose` was see-through.

Not a live break when found (`cmd/typeahead --dump` never opens its
popup), but latent in exactly the way #438 was filed about: the next
overlay-bearing fixture asserted through `Compose` would look green while
encoding a see-through popup, with a doc comment saying the paths agree.

`collectPaint` now carries the nearest declared background DOWN beside
`parentOverlay`/`parentRank` — no extra walk, since it is already
descending — and `paintOne` clears a leaf to it. Containers are excluded
for the reason `Composer` excludes them: their bounds enclose children, so
clearing would wipe siblings that already painted.

### What the mutations say, including the one that says nothing

| mutation | caught by |
| --- | --- |
| drop the leaf pre-clear | `TestBothPaintPathsAgreeOnLeafOcclusion` |
| ignore the ancestor background, clear to default | `TestAOneShotLeafClearsToItsAncestorsBackground` |
| pre-clear CONTAINERS too | **silent** |

The second row exists because the first mutation did not need the
background at all: every fixture in this file paints on the terminal
default, so clearing a leaf to the default instead of to its panel's
colour passed everything. That is `clearStyle`'s whole reason for
existing — a Text in a coloured panel must not punch a default-coloured
hole — and it was unpinned here until a fixture declared a `Background`.

**The third row is an open gap, stated rather than fixed.** Pre-clearing
containers as well as leaves changes no test in this repo. The exclusion
is right — it mirrors `Composer`, and clearing a container's rect would
wipe already-painted siblings — but nothing here demonstrates it, so a
future edit that removed the `isContainer` check would go green. Pinning
it needs a fixture where a container's bounds overlap an
earlier-painted sibling, which no current test builds.

### And the comparison test's doc comment was too strong

`TestBothPaintPathsAgree` said it "compares z-order and nothing else". It
compares two whole rendered rows, so it can see any picture difference —
the occlusion repro above **is** that test with the overlay swapped, and
it failed on the pre-clear. What is true is narrower: `oneShotStripe`
fills exactly its own rect, so for THAT fixture nothing but order can
differ. As written the comment invited someone to change the fixture on a
guarantee the test does not give.

