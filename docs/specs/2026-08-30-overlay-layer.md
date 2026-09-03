# Overlays paint in a layer, not at a position

Status: implemented
Date: 2026-08-30
Issue: [#430](https://github.com/WonderForgeLabs/gooey/issues/430)

## The problem

`Popup`'s doc comment stated the rule as a fact of the design: the surface is
"a leaf child the owner returns from `ChildComponents` (**LAST**, because
document order is z-order)". Every customer followed it, and the test page in
`components/popup_test.go` was built to satisfy it — `toyPage` declares the
owner last, with that reason written in a comment beside it.

Being last among the *owner's* children is not the same claim, and the gap is
the whole bug. `Composer.Frame` walks the paint order once, forward, and forces
a repaint only of nodes **later** in that order than a painter beneath them:

```go
// One forward pass is enough: paint can only damage nodes later in
// z-order, and by the time the loop reaches them every painter below
// is already in c.over.
```

There is no reverse. A node earlier in the order that gets painted over has
nothing that can put it back in that frame. So a popup stayed on top exactly
while its owner was the last thing in the *document* — not in its parent, in
the document.

Put anything after the owner that overlaps the dropdown and the guarantee is
gone. Reported against `apps/wysiwyg`'s designer canvas, where a `MenuBar` is
one of several `Canvas` children alongside a `Gauge`, an `ItemsView` and a
`Border`: opening the menu drew it and the next repaint of the island erased
it. Written as a test the failure is worse than the report — the popup does not
survive even the frame that opens it, because the later sibling paints in the
same pass:

```
row 1 = "@@@@@@@@@@@@        " right after opening, want the popup over the content
```

The shell's own File menu was unaffected, and that was luck rather than design:
the dock panes its dropdown covers are chrome-only containers whose leaves stay
clean, so nothing under it repaints.

## The decision

Z-order becomes document order **in two layers**: the ordinary tree, then every
overlay. Each layer is internally in depth-first pre-order, so nothing about
ordinary components changes.

```go
// Overlay is implemented by a component whose paint node belongs ABOVE
// the page rather than at its place in document order. Its subtree comes
// with it.
type Overlay interface{ OverlaysPage() }
```

`Composer.orderPaint` derives `c.paint` from `c.nodes` after every structural
walk. `c.nodes` stays the **structure** — what the per-frame sweeps, the
`Dynamic` re-sync and `restoreUnder` walk, none of which care about order — and
`c.paint` is the single answer to what is in front of what. Two loops consult
it: the z-ordered paint pass and the placement republish.

Three things about the shape were decided rather than fallen into.

**The lift is global, not within the overlay's parent.** "Above my own
siblings" is not enough and never could be: a `MenuBar` three containers deep
still has to drop its menu over a dock that is a sibling of its
great-grandparent. An overlay is on top of the page.

**Membership is inherited down the tree.** `n.overlay = isOverlay ||
(n.parent != nil && n.parent.overlay)`, computed in one pass because `c.nodes`
is pre-order and a parent is always visited first. That is what moves an
overlay's whole subtree with it. A container overlay ordered on its own would
paint above the page while its children stayed behind in the ordinary layer,
i.e. the surface would land on top of its own contents and the popup would
show as an empty box.

> **Superseded on 2026-09-01.** This paragraph originally read "every overlay
> the framework ships today is a leaf, so this clause decides nothing yet and
> could be deleted with the suite still green —
> `TestAnOverlayLiftsItsWholeSubtree` exists to make that false", and the
> section below said the same of hit-testing. Both stopped being true when
> `ToastHost` and `AdornmentLayer` adopted the marker
> ([#439](https://github.com/WonderForgeLabs/gooey/issues/439),
> [specs/2026-09-01-overlay-hosts.md](2026-09-01-overlay-hosts.md)): they are
> containers, so the clause now carries every toast and every adornment, and
> deleting it turns real tests red rather than only the synthetic one.

**`Overlay` is a marker with an empty method, not a predicate.** Making it
`Overlays() bool` would put a structural fact — where a paint node sits — into
a property somebody writes at runtime, and the framework would then need an
observer to notice the flip and a re-sync to act on it: the machinery `Frozen`
needs and earns. Nothing wants to become an overlay halfway through its life. A
closed popup is arranged to a zero rect and skipped by the paint loop's bounds
check, so membership costs nothing while it is not showing.

## What it does not do

Overlays keep document order **among themselves**. Two overlapping popups paint
in the order they were declared rather than the order they were opened. Nothing
in the tree needs the other answer yet, and the machinery — an open-order stack
the Composer maintains — is worth writing when something does.

Hit-testing is untouched, and that is a gap rather than a non-event. A popup
takes held pointer capture while open (`Popup.Open`), which routes presses to it
regardless of where it sits in any order — so nothing about input needed to
change *for the overlay this framework ships*.

But `Overlay` is a public interface, and `FocusManager.HitTest` walks document
order knowing nothing about it. **The marker moves paint, not input.** An
overlay that does not take capture will paint above a later sibling while that
sibling takes the press. `TestAnOverlayLiftsItsWholeSubtree` exists precisely to
support container overlays, so this is reachable by an adopter rather than
hypothetical — and as of #439 there are two, `ToastHost` and `AdornmentLayer`.

**What keeps them safe is the HOSTING SHAPE, not transparency**, and the
distinction is not pedantry: a `Toast` declares no `HitTestTransparent`
(`TestAShownToastStillCatchesThePointer`), so it is a genuine hit target, and
`Adornment` requires none either, so a third-party adorner is one too. Hosted
as the root's LAST child — the shape both mandate — a host is the first thing
`hitTest` descends into, since it walks each container's children last-to-first;
hit order then agrees with the lifted paint order. `Popup` additionally holds
capture while a dropdown is open, so no press reaches the walk at all.

Off that shape they decouple, silently, and both directions are pinned:
`TestAButtonUnderAToastTakesTheHoverWhenTheHostIsNotLast` and
`TestAnAdornmentLosesThePressWhenTheLayerIsNotLast` ARE the walk misrouting a
press.

> An earlier version of this paragraph — added by #439 itself — said "both are
> `HitTestTransparent` … so there is no press for the walk to misroute". That
> was the retracted reasoning surviving in the record of the very change that
> retracted it, and by the fourth review it was the only instance left in the
> tree. Corrected in review of
> [PR #444](https://github.com/WonderForgeLabs/gooey/pull/444).

The first overlay that is neither declared last nor takes capture is what makes
closing this compulsory.

The interface's own doc comment says so, and `HitTest`'s comment no longer
claims "later siblings paint on top" as its reason. Closing it properly means
teaching the hit-test walk the same two layers — worth doing when a
non-capturing overlay actually exists. Named in review of #437.

## Damage

**No damage count in the repo moved.** That is the result worth recording,
because a reordering of the paint loop is exactly the kind of change that
should be suspected of moving them. It holds for a reason rather than by luck:
the two orders are identical unless the tree contains an overlay, and an
overlay that is closed has a zero rect and is skipped before anything is
forced. Where a popup *is* open, the surface now paints once at the end instead
of once in the middle — same node, same frame, same count.

## Verification

`components/popupzorder_test.go`. Each clause of the fix is pinned
independently — reverting one turns exactly its own tests red:

| mutation | what goes red |
|---|---|
| paint loop walks `c.nodes` again | both popup tests and the grandparent test |
| `n.overlay` drops the parent clause | the subtree test alone |
| `popupSurface` stops declaring `OverlaysPage` | the two popup tests; the container-overlay test stays green, since it brings its own marker |

`TestADismissedPopupStillUncoversALaterSibling` is the counterweight: without
it the fix could have been "never let anything paint over the surface's rect",
which would strand the popup's cells on screen after it closed.
