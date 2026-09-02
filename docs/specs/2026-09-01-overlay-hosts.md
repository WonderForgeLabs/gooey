# The page-wide hosts join the overlay layer

Status: implemented
Date: 2026-09-01
Issue: [#439](https://github.com/WonderForgeLabs/gooey/issues/439)
Follows: [specs/2026-08-30-overlay-layer.md](2026-08-30-overlay-layer.md)

## The problem

[PR #437](https://github.com/WonderForgeLabs/gooey/pull/437) gave z-order a
second layer and **one** adopter. `popupSurface` declares `OverlaysPage`; the
framework's other two page-wide hosts, `ToastHost` and `AdornmentLayer`, were
left as ordinary-layer nodes still relying on the rule the same PR had just
demoted — "declare it last as the root's child, document order is z-order".

A lifted popup therefore outranked both, and three written claims reversed at
once without a single test going red:

- `components/toast.go` stated as a design fact that being the root's last
  child "puts every toast above the page";
- `docs/markup-reference.md` went further — declare the `AdornmentLayer` after
  the `ToastHost` "and tooltips paint above toasts too";
- `components/menu_live_test.go` asserted in a comment that "the toast layer is
  topmost".

The consequence is not cosmetic. The worst case is not a tooltip: it is a
`ValidationMarker`, which is **persistent** — up for as long as its field is
invalid, no pointer anywhere in it — telling the user what is wrong with the
form, and erased by the menu they opened to fix it.

Probed on the documented hosting shape, page-wide host declared as the root's
last child with a popup open:

```
row 1 = "POPUP!              "     ← the toast wrote these cells and lost them
```

Nothing in the suite pinned toast-over-popup, which is why main was green.

## The decision

The hosts get the marker. `ToastHost.OverlaysPage` and
`AdornmentLayer.OverlaysPage`, four lines including the doc comments.

The issue posed the alternative honestly, so it is worth recording why it lost.
The other option was to **state the reversal as the new rule** — rewrite
`toast.go`, `adorn.go`, `tooltip.go` and the markup reference to say a popup is
above a toast now. Two things decide against it.

**It settles a design question by which component happened to be in scope for
#430.** #437 was fixing a menu that vanished on a design canvas; nothing about
that report is an argument that a dropdown should cover a notification. Writing
the accident down as doctrine makes it permanent.

**The reversal is wrong on its merits where it bites hardest.** A toast and a
validation marker are the framework's two ways of telling the user something
they did not ask to be told. A dropdown is a thing the user opened and can
close. Ranking the dismissible above the unmissable is backwards.

What the adopted option costs is stated plainly: the relative order of the four
things — popup surfaces, toasts, adornments, and anything that adopts the marker
later — is now **declaration order among overlays**, permanently and by
convention rather than by intent. That is the limit
[specs/2026-08-30-overlay-layer.md](2026-08-30-overlay-layer.md) already records
and defers, and this change makes it load-bearing for real components rather
than for a hypothetical pair of popups. An open-order stack in the Composer is
what replaces it, when something needs the other answer.

Note that it also restores the documented *tooltips-above-toasts* rule by the
same mechanism it uses for everything else: an `AdornmentLayer` declared after a
`ToastHost` is later in the overlay layer too.

## Why the input gap does not reach here

`Overlay` moves paint and not input — `FocusManager.HitTest` walks document
order and knows nothing about the marker, so an overlay that does not take
capture paints above a later sibling that takes the press.

Both new adopters are exempt, and by their own pre-existing design rather than
by luck. `ToastHost` and `AdornmentLayer` are both `HitTestTransparent`
(`components/toast.go`, `components/adorn.go`) — they span the page invisibly
and would starve every click if they were not — and so is every adornment the
framework hosts in the layer: `tipPopup`, `markerPopup`, `DragGhost`. Toasts
are not interactive either. There is no press for the walk to misroute.

The first **interactive** overlay is what makes teaching `HitTest` the same two
layers compulsory. This change does not create that adopter; it does make the
inheritance clause in `orderPaint` carry real subtrees for the first time, since
both hosts are containers.

## Verification

`components/overlayhosts_test.go`. Each marker is pinned independently —
removing one turns exactly its own tests red:

| mutation | what goes red |
|---|---|
| `ToastHost` drops `OverlaysPage` | `TestAToastPaintsAboveAnOpenPopup` |
| `AdornmentLayer` drops `OverlaysPage` | `TestAValidationMarkerPaintsAboveAnOpenPopup` **and** `TestATooltipPaintsAboveAToast` — with the host lifted and the layer not, a toast covers the tooltips the docs promise are above it |

`TestADismissedToastUncoversTheOpenPopup` is the counterweight: without it the
fix could have been "never let anything paint over a toast's rect", which would
strand the toast's cells on screen after it expired.

A `ValidationMarker` rather than a `Tooltip` for the layer's pin, and that is
forced rather than chosen. `Popup.Open` takes pointer capture unconditionally
(`components/popup.go:120` — not only when `Modal`), so the hover that raised a
tip is out the moment any dropdown opens and `Tooltip.IsShown` goes false. The
overlap the test needs cannot be built out of a tooltip at all. The marker is
the persistent customer, and the one the bug hurts most.

## Damage

**No damage count in the repo moved**, and the reason is the one #437 recorded:
the two paint orders are identical unless the tree holds an overlay whose rect
is non-empty, and a page with no toast up and no adornment placed has exactly
the nodes it had before. The four new tests read rows rather than counts
deliberately — the question is only who painted last over one strip of cells,
and the damage numbers these shapes produce are already pinned by
`components/toast_test.go` and `components/tooltip_test.go`.

## Not done here

[#443](https://github.com/WonderForgeLabs/gooey/issues/443) tracks the doc sweep
for every site that still teaches "declare it last, document order is z-order"
as the *mechanism*. That advice remains correct as advice — declaration order is
still what orders overlays among themselves — so those sites are imprecise
rather than misleading, and rewriting them is one edit with one reviewer rather
than a rider on this fix. Reconciled here are only the claims this change makes
newly false: the two host doc comments, the `ToastHost` and `AdornmentLayer`
sections of `docs/markup-reference.md`, the hosts sentence in
`docs/architecture.md`, the comment in `components/menu_live_test.go`, and the
leaf claim in the #430 spec.
