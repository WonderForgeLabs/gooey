# The hit walk asks the same question the paint does

**Issue:** [#465](https://github.com/WonderForgeLabs/gooey/issues/465) — "hit-testing ignores the overlay layer and its ranks, so paint and input can disagree silently"
**Date:** 2026-09-09
**Supersedes part of:** `docs/specs/2026-09-05-overlay-ranks.md` ([#439](https://github.com/WonderForgeLabs/gooey/issues/439)) — its "`Overlay` moves paint, not input" section, amended in place

## What was wrong

Two walks answered "what is on top", and only one of them had been
taught about the overlay layer.

`Composer.orderPaint` lifts every `Overlay` subtree into a second layer
and orders it by rank ([#437](https://github.com/WonderForgeLabs/gooey/pull/437),
[#439](https://github.com/WonderForgeLabs/gooey/issues/439)).
`FocusManager.HitTest` walked the tree in reverse document order and
returned the first hit — no layer, no rank. So the two planes could give
different answers about the same cell, and nothing went red when they
did:

- A `ToastHost` declared **first** painted its toasts over a button and
  left the press to the button.
- An **opaque** overlay child painted above an ordinary one and routed
  beneath it. `Toast` is the only one that ships: every adornment in
  this repo declares `HitTestTransparent`, so a validation marker's
  press went beneath it before this change and still does, and naming
  the marker here would claim a divergence #465 did not fix. The
  exposure that is real is stated below — a third-party adornment
  omitting the method is opaque at the top rank, with nothing to say
  so. Corrected in review of #458.
- A `Hidden` component, which renders no content, still won the hit and
  ate presses on whatever was actually visible under the pointer.

The divergence was written down as deliberate scope in the ranks spec —
"`Overlay` moves paint, not input", a sentence superseded here —
and that is what made it survive: a statement that the two planes
disagree reads as a decision rather than as a defect, and the row in
that spec's claims table pinned the disagreement with a test. It is no
longer true of either plane.

## The decision

**One rule, consulted by both planes.** `overlayOf` — the membership-and-
rank function `appendByRank` already derived the paint order from — is
now what the hit walk asks too, and `hitCandidate.beatenBy` is
`appendByRank`'s ordering asked one pair at a time:

1. the lifted layer beats the ordinary one;
2. within a layer, a higher rank beats a lower one;
3. only on a tie does position decide, and it is **visit order among
   the nodes the walk reaches**. The walk numbers a node after its
   bounds test, so a miss is never numbered; `c.nodes` numbers every
   composed node. The two agree on RELATIVE order among the nodes both
   see, which is all a pairwise comparison needs — and that is the
   whole of the claim. An earlier version of this line said "the same
   numbering `c.nodes` carries", which invites moving the increment
   above the bounds test to restore an index nothing needs, with the
   suite staying green. Corrected in review of #458.

Three consequences follow from that, and each is a change in its own
right rather than a detail of the ordering:

**The walk runs forward and cannot return early.** The old walk ran in
reverse and stopped at the first hit, because in document order the last
hit is the top one. A ranked walk has no such prefix property: an
unvisited node can out-rank everything in hand. So the walk runs to the
end of what it visits and keeps a running best.

It does NOT visit the whole tree, and that is the sentence this
replaced. Bounds still prune at every node — only subtrees whose bounds
contain the point are descended into — so the cost is bounded by the
containing subtrees, not by the sibling count. Every other statement of
this in the tree is careful about it (`mouse.go`'s `HitTest` godoc,
`docs/architecture.md`, `CLAUDE.md`, `docs/learn/concepts/input-routing.md`),
and this is the file a reader comes to for *why the cost changed* — so
overstating it by the whole prune is the same class of error, in the
other direction, as the "one extra rectangle test per remaining
sibling" understatement `mouse.go` records fixing. Corrected in review
of #458.

**Losing the early return removed the only bound on total work.** A
branching cycle used to cost one branch; now it costs the walk. `hitTest`
therefore carries a depth cap against `MaxLayoutDepth` and, on tripping
it, **aborts the whole walk and answers `nil`** rather than returning the
best of the prefix it happened to reach — for the reason above, a prefix
is a different question, not a subset of the answer. The fault is
readable through `Composer.LayoutFault`. On a legal tree the cap never
fires: depth is already bounded by `MeasureChild`/`ArrangeChild`, so a
tree deep enough to abort this walk has failed to lay out.

**`Hidden` stops winning.** `paintable()` is what every paint path gates
`Render` on, so a `Hidden` node contributes no content and cannot be the
thing the user is pointing at. `Collapsed` was already skipped; `Hidden`
was not. Note what this does *not* fix: `Composer.build` pre-clears every
leaf's bounds before any paintable test, so a hidden leaf still erases
the cells it covers and the node that last wrote them is the one this
walk now skips. That erasure is a defect in the damage model, tracked
separately — the gate here is right regardless.

## What it costs

**A component that owns the cells owns the input in them, from
wherever it is declared.** That is the rule everywhere else in the
framework, and applying it to input is the point of the change — but it
makes a class of behaviour reachable that previously depended on
declaration order. A `Toast` is an ordinary leaf and is not
hit-test-transparent, so a toast nobody means to click swallows input on
whatever it covers for its whole lifetime. That used to depend on where
the host sat among its siblings; position decides nothing now, on either
plane. Tracked as
[#481](https://github.com/WonderForgeLabs/gooey/issues/481), which
carries the options.

**"Swallows the press" is half of it.** `DispatchMouse` walks the tree
**once** and derives both targets from that one answer — `hit` through
`frozenHostFor(under, AllowPointer)`, `hov` through
`frozenHostFor(under, AllowHover)`. Whatever takes the press takes the
hover with it, so the covered component also loses its highlight and its
tooltip. That half is quieter and worse to diagnose: a swallowed click
is a gesture the user repeats and notices, and a tooltip that never
appears is not.

**Capture now short-circuits the WALK, not just the target.** This is a
change, not a continuity, and an earlier draft of this record filed it
under "What is deliberately NOT changed" with the word "still" — which
would send someone bisecting a drag-hover regression past the very
branch that introduced it. On the base, `DispatchMouse` called
`HitTest` unconditionally and capture decided only where the event
went; the walk ran and both its results were discarded. Three kinds now
run no walk at all in dispatch: a captured **move**, a captured
**wheel**, and a **press arriving while the capture is held**. Only an
unheld press and a release still hit-test there.

`FocusManager.MouseTarget` skips a FOURTH — the captured **release** —
and "in dispatch and in `FocusManager.MouseTarget` alike" is what this
said. The two conditions are deliberately different, and `mouse.go`
says so in as many words: a captured release reads the hit on the
dispatch side, where `m.within(captor, hit)` decides whether a click is
synthesized, and reads nothing in the query, which synthesizes nothing.
Sizing `Service.mayPoint` from the old sentence budgets a walk per
captured release that never happens. Corrected in review of #458. The `MouseTarget` half is the load-bearing one, because
`control/input.go`'s `Service.mayPoint` asks it per pointer event for
every guest — so a reader sizing that cost from this paragraph has to
see all three, and this paragraph named the move alone for two rounds
after the other two shipped. Corrected in review of #458.

**An adornment is at the top rank**, above popups and above toasts, and
`AdornmentLayer`'s own transparency does not reach its children — each
adornment decides for itself. Every adornment in this repo returns
true, and that is a CHECK rather than a count in prose:
`TestEveryAdornmentIsHitTestTransparent` derives the set from source, so
a fourth comes under it on the commit that adds it — which is why the
number that used to stand here is gone. A
third-party one that omits the method is opaque with no load error, no
vet and no test to say so; that residue is why `Add`'s remedy is written
at the point of use.

## What is deliberately NOT changed

**`Frozen` still does not stop the descent.** Freezing constrains
dispatch, not this query: a design surface calls `HitTest` to find the
actual `<Button>` under the pointer and select it, while `DispatchMouse`
hands the press to the frozen host. Stopping the descent would make
click-to-select impossible and every freeze test would stay green.


## How the claims here are checked

| Claim | Test |
|---|---|
| A rank orders hit-testing as well as paint | `TestARankOrdersHitTestingAsWellAsPaint` |
| An overlay takes the press from a later ordinary sibling | `TestAnOverlayTakesThePressFromALaterOrdinarySibling` |
| A transparent host passes the press to its own child | `TestATransparentOverlayHostPassesThePressToItsOwnChild` |
| An overlay painting outside its parent's bounds is not hit | `TestAnOverlayOutsideItsParentPaintsAndIsNotHit` |
| A `Hidden` component renders no content and is not hit | `TestAHiddenComponentRendersNoContentAndIsNotHit` |
| …but a visible child of a hidden parent still is | `TestAVisibleChildOfAHiddenParentIsStillHit` |
| …and the hidden leaf's own cells are still written | `TestAHiddenLeafStillWritesItsOwnCells` |
| `Collapsed` is still skipped | `TestHitTestSkipsCollapsed` |
| Equal ranks still fall back to position (overlay layer) | `TestEqualRanksFallBackToPosition`, and `TestEqualRanksKeepDocumentOrder` for paint |
| Position still decides inside the ORDINARY layer | `TestHitTestOverlapPrefersLastPainted` |
| A branching cycle terminates instead of hanging | `TestHitTestOnABranchingCycleTerminates` |
| A drag walks no tree per motion event | `TestADragDoesNotWalkTheTreeOnEveryMove` |
| …and neither does the QUERY that models the same routing | `TestADragIsNotWalkedForByAQueryEither` |
| Every adornment in this package is transparent | `TestEveryAdornmentIsHitTestTransparent` |
| A press inside a frozen subtree reaches the host, not the child | `TestAPressInsideAFrozenSubtreeNeverReachesIt` |
| The `MenuBar`'s paint agrees with its hit test | `TestMenuBarPaintAgreesWithItsHitTest` |

The prose half of the same contract has its own guard:
`TestNoFileTeachesTheRetiredInputRule` fails on any file still stating
that the hit walk prefers later siblings or knows nothing about ranks,
and `TestEveryStatementOfTheHitContractNamesTheAncestorClause` holds the
statements that remain to one shape. That guard exists because this
change's predecessor — the paint lift — was reversed in prose for a
whole stack with the suite green throughout.

## Why this record is dated after the change

The decision landed in `ca6fce55` on 2026-09-09 and the amendments were
written into `docs/specs/2026-09-05-overlay-ranks.md` instead of into a
record of their own, on the argument that they superseded a section of
it. They do — but a framework change that alters what `HitTest` answers,
what it refuses, how it terminates, and which component receives a
user's press is not a footnote to another decision, and every sibling
change on this stack of comparable size has its own file. Raised in
review of [#458](https://github.com/WonderForgeLabs/gooey/pull/458).
