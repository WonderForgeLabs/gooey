# Concept: overlays and z-order

gooey has no z-index property and no overlay registry. **Z-order is
document order in TWO layers.** The Composer keeps its paint nodes in
depth-first pre-order — children paint after (above) their parents,
later siblings after earlier ones — and then lifts every subtree whose
root implements `gooey.Overlay` out of that order and onto the end,
**bucketed by rank within it**. `c.nodes` stays the structure; `c.paint`
is the answer to what is in front of what.

So a lifted overlay paints above the page **from wherever it is
declared**. In a `Grid`, `Grid.Row` places it where it belongs and
nothing about z-order argues with that.

**Which surfaces are lifted, exactly.** `MenuBar`, `Popup`, `ToastHost`
and `AdornmentLayer` **are** lifted. That is every overlay host the
framework ships, so `cmd/toolkit` declaring its `MenuBar`, `ToastHost`
and `AdornmentLayer` at the end of its Grid is house style and decides
nothing.

(That sentence used to say "all three" and then "the bar", and neither
had an antecedent it could take — the nearest three were `Tooltip`,
`ToastHost` and `AdornmentLayer`, and `cmd/toolkit` declares no `Tooltip`
at the end of its Grid, its tips being inside the job and overlays tabs.
Naming them makes it say what the file does.)

A `Tooltip` is **not** one of them, and it reads as if it should be: its
tip is `tipPopup`, an ordinary leaf the `AdornmentLayer` hosts, so the
tip is lifted by the layer rather than on its own account. Nothing about
the `Tooltip`'s own position decides anything either way. This page named
"the `Tooltip` popup" among the lifted surfaces for two rounds, one
sentence after saying the opposite, and review of #455 caught it —
`TestTheHostsThisPageCallsLiftedActuallyAre` reads the names out of the
list above and refuses one whose surface does not implement the marker.

The two hosts were the exception until
[#439](https://github.com/WonderForgeLabs/gooey/issues/439). This
paragraph said they did *not* implement `gooey.Overlay` and that their
position was still load bearing, which was true and is the reason the
sentence existed at all — #455's first draft claimed position decided
nothing for all three, when it decided everything for two of them. The
correction expired on schedule: the guard over that claim was written
to fail the moment either host adopted the marker, it did, and it has
been deleted along with the claim.
`components/overlayclaims_test.go`'s file comment records which guard
that was, what it asked for, and which of its two neighbours does *not*
expire.

(The name was spelled out here, hyphenated across the 72-column wrap —
so neither the broken spelling nor the whole one could be grepped, and
the test it named no longer exists in either form. A name is only worth
writing where a reader can find what it points at.)

**Within the layer, rank orders — not declaration.** A toast is never
hidden by an open menu, whichever an app happens to type last. See
`docs/specs/2026-09-05-overlay-ranks.md`.

**This page opened with "Z-order is document order" and instructed
"declare the overlay element as the LAST child"** until review of #455,
which is the rule
[#430](https://github.com/WonderForgeLabs/gooey/issues/430) disproved:
being last among your OWNER's children buys being above the owner's
other children and nothing else, so anything declared after the owner
painted over an open dropdown and the forward-only pass could not put it
back. The lift landed in
[#437](https://github.com/WonderForgeLabs/gooey/issues/437). Ordering
*within* the layer — so a toast is never hidden by an open menu — is
[#439](https://github.com/WonderForgeLabs/gooey/issues/439), and it is
the rank paragraph above.

(This sentence ended "and is not described here yet" while the paragraph
above it already described the ranks — the two landed in one diff and
only the newer half was written. `docs/architecture.md` took a
forward-hedge in the same diff and this page did not; a reader who
reaches the older sentence first concludes the ordering is still
undefined and declares for it.)

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
restore). The pass and both exemptions landed in
[PR #88](https://github.com/WonderForgeLabs/gooey/pull/88)
([#26](https://github.com/WonderForgeLabs/gooey/issues/26)).

## Dismissal is the reverse half

The forward pass can only force nodes *later* in z-order than a painter
— and the lift puts an overlay at the end of the paint order, so when it
goes away, nothing after it can fix the hole. `Composer.restoreUnder`
([PR #93](https://github.com/WonderForgeLabs/gooey/pull/93)) is the
missing half: when a rect **leaves the screen**, the sweep clears the
vacated cells and force-dirties every still-visible node intersecting
them, and the ordinary paint loop lays those down again in z-order. A
dismissed menu, toast, or tooltip repaints **exactly what it was
covering**, in the same frame. That covers all three vanish paths: a
visibility flip, a departure in a `Dynamic` re-sync (a toast dismissing),
and a bounds move
(a dropdown sliding to the next title is an overlay *moving*).

Note what the vanished overlay itself does: nothing. Erasure is a sweep,
not a paint, so hiding an overlay costs zero paint nodes plus the
repaints of what was beneath. A moved overlay (a menu switching titles)
is both cases at once — the bounds sweep clears and restores the old
rect and force-repaints the node at its new one.

## An open overlay owns the input

Two conventions ride along with the z-hosting, both visible in the
`MenuBar` and packaged into the [`Popup` primitive](../howto/howto-popup.md)
— four hand-rolled copies extracted into one in
[PR #143](https://github.com/WonderForgeLabs/gooey/pull/143)
([#96](https://github.com/WonderForgeLabs/gooey/issues/96)):

- **Held pointer capture while open.** A dropdown hangs outside its
  owner's bounds, where hit-testing never finds it, so the owner takes
  `CaptureMouse` at open and every pointer event routes there. A press
  anywhere the owner does not claim dismisses **and is consumed** — it
  never reaches, or activates, what is underneath.
- **Modal key swallowing.** An open menu consumes every key it declines,
  so the page's `q` cannot quit underneath it. Esc dismisses; everything
  else stops at the overlay.

One trap for page-spanning hosts: hit-testing treats every bounded
container as opaque, so a full-page `ToastHost` would eat every click on
the page. Hosts like it implement `HitTestTransparent` —
the host opts out of hit-testing while its toasts stay hittable —
introduced with the adornment layer in
[PR #129](https://github.com/WonderForgeLabs/gooey/pull/129).

## An overlay pinned to the pointer, not the tree

An adornment is normally positioned against another *component's*
arranged bounds. A drag ghost, a drop indicator, a marquee rectangle or a
crosshair has no such component: it belongs to a gesture. Those are
**free** adornments — they implement `gooey.PointerFollower`, which
exempts them from the layer's anchor sweep entirely and makes
`Place` receive the pointer's 1x1 cell instead of an anchor's bounds
([#177](https://github.com/WonderForgeLabs/gooey/issues/177),
[spec](../../specs/2026-08-23-free-adornments.md)).

```go
// in the press handler, having decided this gesture is a drag
ghost.Label.Set("3 files")
ghost.Show(mgr)
// in the release handler
ghost.Hide()
```

**The interesting part is what a motion costs.** `term.EnableMouse` sets
`?1003h` any-motion tracking, so the terminal reports a motion event per
cell the pointer crosses — for the whole life of the app, whether or not
anything cares. If following the pointer meant repainting on every
report, hosting one ghost would cost a frame per cell forever.

It does not, and the reason is the ordinary call-site rule. The pointer
lives on the `FocusManager` as plain fields plus a revision property, and
`FocusManager.Pointer()` reads that revision before returning them — so
asking from a layout pass is a plain read and asking from inside an
evaluation is a subscription. The Composer arms one observer per
follower (`armPointer`, the same shape as the bound-`Visibility` and
`Frozen` observers) whose evaluation calls `FollowsPointer()` and reads
the pointer **only if the answer is true**. An observer is not a paint
node: it schedules a frame and counts as no damage.

So the cost falls out of the graph rather than being managed:

- nothing following → nothing subscribes → a motion report invalidates
  nothing and **schedules no frame at all**;
- a ghost in the layer but parked (`FollowsPointer()` false) → same, and
  it is arranged to a zero rect;
- a ghost actually following → one cell of motion repaints the ghost and
  restores what it uncovered, and nothing else paints a cell.

`components/dragghost_test.go` pins all three, and the zero rows assert
the **invalidation** count rather than the painted count — calling
`Frame()` by hand is the harness performing the very scheduling under
test, so "0 painted" would also hold if the frame were scheduled and
merely found nothing dirty.

This is deliberately *not* a cursor. Nothing portable hides the
emulator's own pointer, so a glyph drawn under it would double-image and
quantize to cells; `DragGhost` offsets down and right by default and
clamps at the screen edge rather than flipping sides. Cursor *shape* — a
best-effort `Cursor="Hand"` attached property — is the separate
[#178](https://github.com/WonderForgeLabs/gooey/issues/178).

## Where to see it

`cmd/toolkit` is the whole story on one page (menu over content,
toasts, tooltips; dismissing any of them restores the exact screen), and
`cmd/browser`'s source picker is the recipe reused in an app. Both are
walked through in [demos.md](../../demos.md);
`TestDemoOverlaysDropAndRestore` (`cmd/toolkit/toolkit_test.go`) is what
pins the guarantee — it composes the shipped page and byte-compares the
screen before and after, so esc on an open menu restores the exact
screen, and a toast leaves no scar.

Depth: [architecture.md — the Composer](../../architecture.md#the-composer);
decision records in
[specs/2026-08-10-toolkit-wave2.md](../../specs/2026-08-10-toolkit-wave2.md)
and [specs/2026-08-10-popup.md](../../specs/2026-08-10-popup.md). To
build one: [how to give a component a dropdown](../howto/howto-popup.md).
