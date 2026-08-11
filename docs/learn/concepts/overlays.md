# Concept: overlays and z-order

gooey has no z-index property and no overlay registry. **Z-order is
document order**: the Composer keeps its paint nodes in depth-first
pre-order, so children paint after (above) their parents and later
siblings after earlier ones. An overlay is nothing more than a later
sibling with a covering paint — a leaf's pre-clear, or a container's
background fill.

That makes overlay hosting a declaration, not machinery: **declare the
overlay element as the LAST child**. In a `Grid`, an element's position
(`Grid.Row`) is independent of its document order, so "last child, top
row" is spellable directly — `cmd/toolkitdemo`'s markup declares its
`MenuBar`, `ToastHost`, and `AdornmentLayer` as the Grid's last children
with `Grid.Row="0"` keeping the bar on the top row.

## The forward pass keeps the stack honest

Damage tracking repaints only dirty components, which would break a
stack: repaint something *under* an overlay and the overlay's cells are
gone. So when a node paints, the Composer forces every **later** node
whose bounds intersect the painted rect to repaint in the same frame —
it was (or may have been) painted over, and it is above, so it goes down
again on top. Two exemptions keep the damage counts tight: a chrome-only
container never forces its own descendants (its chrome never covers
their cells — the same contract that lets containers skip pre-clearing),
and a `Decorator` is never forced from below (it owns no cells to
restore).

## Dismissal is the reverse half

The forward pass can only force nodes *later* in z-order than a painter
— and an overlay is the last node, so when it goes away, nothing after
it can fix the hole. `Composer.restoreUnder` is the missing half: when a
rect **leaves the screen**, the sweep clears the vacated cells and
force-dirties every still-visible node intersecting them, and the
ordinary paint loop lays those down again in z-order. A dismissed menu,
toast, or tooltip repaints **exactly what it was covering**, in the same
frame. That covers all three vanish paths: a visibility flip, a
departure in a `Dynamic` re-sync (a toast dismissing), and a bounds move
(a dropdown sliding to the next title is an overlay *moving*).

Note what the vanished overlay itself does: nothing. Erasure is a sweep,
not a paint, so hiding an overlay costs zero paint nodes plus the
repaints of what was beneath. A moved overlay (a menu switching titles)
is both cases at once — the bounds sweep clears and restores the old
rect and force-repaints the node at its new one.

## An open overlay owns the input

Two conventions ride along with the z-hosting, both visible in the
`MenuBar` and packaged into the [`Popup` primitive](../howto/howto-popup.md):

- **Held pointer capture while open.** A dropdown hangs outside its
  owner's bounds, where hit-testing never finds it, so the owner takes
  `CaptureMouse` at open and every pointer event routes there. A press
  anywhere the owner does not claim dismisses **and is consumed** — it
  never reaches, or activates, what is underneath.
- **Modal key swallowing.** An open menu consumes every key it declines,
  so the page's `q` cannot quit underneath it. Esc dismisses; everything
  else stops at the overlay.

One trap for page-spanning hosts: hit-testing treats every bounded
container as opaque, so a full-page `ToastHost` declared last would eat
every click on the page. Hosts like it implement `HitTestTransparent` —
the host opts out of hit-testing while its toasts stay hittable.

## Where to see it

`cmd/toolkitdemo` is the whole story on one page (menu over content,
toasts, tooltips; dismissing any of them restores the exact screen), and
`cmd/browser`'s source picker is the recipe reused in an app. Both are
walked through in [demos.md](../../demos.md); toolkitdemo's pty test is
what pins the guarantee — esc on an open menu restores the exact screen,
and a toast leaves no scar.

Depth: [architecture.md — the Composer](../../architecture.md#the-composer);
decision records in
[specs/2026-08-10-toolkit-wave2.md](../../specs/2026-08-10-toolkit-wave2.md)
and [specs/2026-08-10-popup.md](../../specs/2026-08-10-popup.md). To
build one: [how to give a component a dropdown](../howto/howto-popup.md).
