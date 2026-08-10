# Adornments: the decoration plane, and Tooltip (issues #91, #92)

**Status:** executed.

**Date:** 2026-08-10

## What was asked for

Elan asked for tooltips, and tooltips force a concept: WPF's adorner
layer — components ATTACHED to a target and positioned against its
arranged bounds, above the whole tree, without participating in layout —
absent from every TUI framework.

- **#91 AdornmentLayer** — the hosting plane, built on the overlay
  machinery wave 2 settled (last-in-document-order = above;
  `Composer.restoreUnder` repaints beneath vacated rects). Future
  customers — validation markers, focus rings, badges, drag ghosts —
  must be ordinary components.
- **#92 Tooltip** — the first customer: a non-visual attachment like
  KeyBinding, a `Tooltip="…"` shorthand on any element, a delay timer
  under the Startable close-AND-join rule, flip-to-fit placement,
  dismissal on hover-out/key/press, and the wave-2 gesture-hint pattern
  where the target has a KeyBinding. Working from pure markup — the
  Include tier, no code-behind.

## Plan

No second overlay story. The layer is the ToastHost hosting shape (a
chrome-less, background-less last child of the root, children realized
through `Dynamic`); each adornment is an ordinary leaf whose covering
paint makes it an overlay under the z-pass; appear/dismiss/move damage
is the machinery the composer already has.

The one genuinely new mechanism is **anchoring**: an adornment is
positioned by ANOTHER component's bounds. Bounds are plain fields, not
properties — but layout runs unconditionally every frame, so the layer's
Arrange re-reads every anchor and re-places its adornments, and the
bounds sweep turns any difference into damage. "Target bounds as a
dependency" is realized by the layout pass, not the property graph.

Hover routing for tooltips cannot live in the tooltip (an attachment
sees no events) and should not live in the layer (it would be a second
dispatch path): it belongs to the FocusManager, which already owns
hover, as a KeyBinding-style attachment seam.

## Executed

### The attachment API (`components/adorn.go`)

An adorner is any component implementing two extra methods:

```go
type Adornment interface {
    gooey.Component
    Anchor() gooey.Component           // whose bounds it is pinned to
    Place(anchor, layer Rect) Rect     // where it wants to be
}
```

`AdornmentLayer.Add/Remove` raise the structural hook; the layer's
Arrange asks each adornment for its Anchor and its Place every frame.
Placement POLICY (flip-to-fit, edge-hugging, clamping) belongs to the
adornment; lifecycle and re-anchoring belong to the layer. A future
validation marker is: a leaf with a `Place` that hugs the TextBox's
right edge, `Add`ed when validation fails. Nothing else.

The layer drops an adornment when its anchor is **gone**: nil, not
`Bounded`, arranged to nothing, or no longer reachable from the root
through `Visible` elements. The reachability walk runs against the LIVE
tree (via `FocusManager.Root()`, new one-liner), so an anchor a Dynamic
container removed this frame is already unreachable before the input
tree re-syncs — the drop, the departed adornment's cell clear, and the
restore beneath all land in the same frame. The walk runs only while
adornments are up; an idle layer costs nothing. A dropped-for-anchor
adornment is told (package-private `orphaned()`), because its owner did
not ask for the removal and must not be left believing it is still up.

### Attachments grew up (`input.go`)

The FocusManager walk already collected KeyBindings off `Attacher`s; it
now hands attachments the same seams components get, plus one new one:

- `Hosted` — `SetHost(Component)`: an attachment learns what it hangs
  off. Attachments have no parent pointer; the walk is the one place
  that knows.
- `FocusHost` on an attachment now receives `SetFocusManager` too.
- `HoverWatcher` — `PointerOver(bool)` + `Interrupted()`: told when the
  pointer enters/leaves the HOST's subtree, and on every key dispatch
  and pointer press. Notifications, never handlers: nothing is
  consumed, the keystroke that dismisses a tip still does its job.
  Containment is EXCLUSIVE like hover itself: among watching hosts
  containing the hit, only the innermost is "entered", which is the
  structural guarantee that nested tooltipped elements never show two
  tips. Watcher enter/leave state survives Resync (a re-sync happens on
  every show), so structural changes do not restart delay timers.

### Hit-test transparency (`mouse.go`) — a latent wave-2 bug

The first integration test failed for a reason worth recording: a
full-page overlay host declared LAST is the FIRST thing hit-testing
finds — an invisible layer that ate every click and starved every hover
beneath it. ToastHost has had this bug since wave 2 (any page hosting
the toast layer routed all pointer events to it). New opt-in interface:

```go
type HitTestTransparent interface{ HitTestTransparent() bool }
```

`hitTest` never returns a transparent component (children stay
hittable). `AdornmentLayer`, `ToastHost`, and the tooltip popup
implement it. Toast leaves stay hittable — they own visible cells.

### Tooltip (`components/tooltip.go`)

`Tooltip` is a non-visual attachment (`NonVisual`, `Hosted`,
`FocusHost`, `HoverWatcher`, `Startable`). Hover-in arms a delay
(default 600ms, `Delay` property; negative = immediate); the goroutine
posts the show through the `Start` post and the stop func closes AND
joins — stop returns ⇒ no show ever arrives. A stale post (hover ended,
key pressed) is discarded by generation count. A never-started
composition has no way to marshal a delayed show and shows immediately
— degraded, not broken, same clause as ToastHost's sticky toasts.

The popup is an ordinary leaf `Adornment` shown in the page's layer
(found by walking from `FocusManager.Root()` at show time — no
registration, no ambient state; a page without a layer shows nothing).
Placement: left-aligned under the anchor, clamped horizontally, flipped
ABOVE when the row below falls off screen. Its Render reads `Text`, so
a bound tip repaints live while up. If the host declares a KeyBinding —
or the markup says `Gesture="…"`, validated by `ParseGesture` at load —
the tip renders the hint dim, right-aligned, in the canonical spelling:
the MenuItem hint rule, display only, one gesture system.

Dismissal: hover-out (`PointerOver(false)`), any key, any press
(`Interrupted()`). After a key/press dismissal the tip stays down until
the pointer leaves the host and returns — a keystroke is a person
working, not a person asking again.

### Markup

- `<Tooltip Text Delay Gesture Style/>` — non-visual child, attached by
  the existing `buildChildren` split. `<Button>` now accepts
  attachments (and rejects visual children as a load error — they were
  silently dropped before).
- `Tooltip="…"` shorthand on ANY element, applied in `build()` beside
  `applyLayout` — it belongs to the element, not the component
  vocabulary. On a user-control instance it decorates the INSTANCE and
  is filtered from pass-through and from declared-surface checking;
  `Tooltip` joins `Name` as a reserved `<x:Property>` name.
- `<AdornmentLayer/>` — no children (load error), placed last by the
  document-order rule.

### Damage pins

`components/tooltip_test.go`, on a Text host (no HoverState, so the
counts are the tooltip's alone): appear paints **1** (the popup; the
layer's node stays clean), hover-out dismissal paints **3** on the
toast-shaped page (restored leaf + 2 swept containers — the exact
counts `TestToastDismissRestoresWhatWasBeneath` pins), every path
settles to 0, an anchor move restores the vacated cells in the same
frame, and crossing two tooltipped targets never has two tips up (also
pinned pre-frame). Delay discipline pinned by
`TestTooltipDelayPostsAndJoins`, including the stale-show and
post-Close probes. Markup forms, load errors, control-boundary
filtering, and the pure-markup end-to-end pinned in
`markup/tooltip_test.go`; the shipped demo page pinned in
`cmd/toolkitdemo` (`TestDemoTooltipShowsAndRestores`).

### Demo

`cmd/toolkitdemo` extended in place: `Tooltip="…"` shorthands on the
ButtonBar's buttons, the child form with `Gesture="ctrl+t"` on the
toast button, and `<AdornmentLayer/>` as the very last element — above
the toasts too.

## Invariants touched

- **Damage discipline (invariant 3):** extended, not weakened. All
  adornment transitions ride the existing sweeps (`Dynamic` departure,
  bounds move, restoreUnder); appear = 1 paint, dismiss settles to 0,
  both pinned.
- **Input routing (invariant 6):** two additions, no dispatch-order
  change. `HoverWatcher` notifications fire beside hover (exclusive,
  innermost-wins) and never consume; `HitTestTransparent` removes
  components from hit-testing — and FIXES pointer routing on any page
  hosting a ToastHost, which previously swallowed every click (two
  demo-level hiding tests re-verified; screen results identical).
- **Startable close-and-join (Timer discipline):** Tooltip follows it;
  pinned including the Show-after-Close probe.
- **No reflection (invariant 1):** all wiring is interfaces and
  type-switches; the layer lookup is a typed tree walk.

## Not in this wave

Rich tooltip content (multi-line, markup-templated tips — wants
DataTemplates), tooltip re-show delay ("instant reshow" grace window),
adorners driven from code on the layer (API exists via `Add`/`Remove`
and the `Adornment` interface, but no second customer shipped — the
next one, validation markers, should confirm the interface before it
calcifies), `docs/learn` chapter + example GIF for the epic's
acceptance criteria, and pointer-anchored placement (context menus want
a Place anchored at the pointer, not at a component).
