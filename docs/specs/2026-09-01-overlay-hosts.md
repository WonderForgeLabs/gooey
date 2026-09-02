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

**The exemption is narrower than the layer, and the gap is a public one** —
though not in the shape it first looked. `AdornmentLayer.Add` is exported and
`Adornment` requires only `gooey.Component`, `Anchor` and `Place`, not
`HitTestTransparent`, so an adornment somebody else writes inherits this paint
order without inheriting the exemption.

The first draft of this section said such an adornment therefore loses its
presses, full stop. **That is wrong, and checking `hitTest` rather than
asserting it is what caught it** (raised in review of
[PR #444](https://github.com/WonderForgeLabs/gooey/pull/444)): the walk
descends each container's children from LAST to first (`mouse.go`), so a layer
declared as the root's last child — the shape the docs mandate — is entered
*first*, and its adornments are found before anything earlier. On the
documented shape, paint order and hit order agree, and an interactive adorner
works.

What this change really does is **decouple them**. Being last used to be the
only thing keeping the layer on top, so getting the paint right and the input
wrong was not expressible. Now the layer paints above the page wherever it
sits, which removes the visible reason to declare it last, while hit-testing
still requires it there. Declare it anywhere else and its adornments paint over
a later sibling that silently takes their presses — correct on screen, wrong on
click, with nothing to show for it. Both `Add` and `OverlaysPage` say this at
the extension point, which is where someone meets it.

The first **interactive** overlay is what makes teaching `HitTest` the same two
layers compulsory, and the paragraph above is the reason that adopter can now
arrive from outside this repo rather than only from inside it. This change does
not create one; it does make the inheritance clause in `orderPaint` carry real
subtrees for the first time, since both hosts are containers.

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
(`Popup.Open` calls `mgr.CaptureMouse` — not only when `Modal`), so the hover that raised a
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
still what orders overlays among themselves — so most of those sites are
imprecise rather than misleading, and rewriting them is one edit with one
reviewer rather than a rider on this fix.

Reconciled here are the claims this change makes newly false — the two host doc
comments, the `ToastHost` and `AdornmentLayer` sections of
`docs/markup-reference.md`, the hosts sentence in `docs/architecture.md`, the
comment in `components/menu_live_test.go`, and the two retired claims in the
#430 spec — plus four sites the review of
[PR #444](https://github.com/WonderForgeLabs/gooey/pull/444) argued fall between
the two PRs rather than inside either:

- `components/menu.go` stated the demoted mechanism as the rule — *"being late
  in document order is what puts it above the content it covers"* — in the doc
  comment of the very component #430 was filed against. Same package, two
  sibling host comments away from this change.
- `components/popup.go`'s *"(LAST, because document order is z-order)"* gives
  the demoted reason for a surface that has implemented `Overlay` since #437.
- `cmd/toolkit/toolkit.gooey` said it **on screen**, in the flagship demo's
  overlays tab, and twice more in markup comments.
- `apps/wysiwyg/wysiwyg.gooey` and `apps/wysiwyg/statusaddr.go` said it for
  the MenuBar and the status-bar address popup, and `README.md`'s feature
  table said it for `MenuBar` and `ToastHost` together. These four came from
  a second pass over the criterion below rather than the first: having set
  "Go doc comments and demo markup" as the line, the first pass then only
  swept the package being edited. Raised in review of PR #444.
- `docs/learn/concepts/overlays.md` is the page a reader lands on to answer
  "how does z-order work here" and mentioned neither layer nor marker. It does
  not read as imprecise, it reads as complete — the one deferral that costs a
  reader the correct model, so it gets a pointer now and its rewrite still
  belongs to #443.

The criterion those share, and the reason they were not left: they are Go doc
comments and demo markup rather than `docs/**`, so "the doc sweep" may not
cover them at all.

**The sweep took three passes, and each miss had the same cause: the search was
narrower than the criterion.** Worth writing down, because the criterion was
right each time and the grep was not.

1. First pass read around the change and swept the package being edited. It
   missed everything outside `components/`.
2. Second pass grepped `'*.go' '*.gooey' README.md` — and piped through
   `grep -v '_test.go'`, which structurally excluded `menu_test.go`,
   `dragghost_test.go` and `popup_test.go`. A test's doc comment teaches a
   reader exactly as a source file's does, and `popup_test.go`'s was the
   fixture comment the whole z-order suite is built on.
3. Third pass dropped the exclusion and added `cmd/browser/browser.gooey`,
   which had been contradicting `cmd/browser/picker.go` — the file the second
   pass rewrote — ever since.

The command that finally covered it, with no `-v` filter and no path
narrowing:

```sh
git grep -ni 'document order.*z-order\|z-order.*document order' \
  -- '*.go' '*.gooey' '*.md' ':!vendor'
```

**Line-number citations were removed rather than corrected.** This PR's own
doc-comment edit to `popup.go` shifted the line a test and this spec both cited
as `components/popup.go:120`, so both silently pointed at the wrong statement —
the failure mode where a citation stays plausible while becoming false. They
now name `Popup.Open` and `mgr.CaptureMouse` instead, which move with the code.
