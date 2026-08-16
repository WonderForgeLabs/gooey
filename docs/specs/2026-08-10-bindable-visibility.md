# Bindable Visibility ([issue #12](https://github.com/WonderForgeLabs/gooey/issues/12), remainder)

Executed 2026-08-10. This record closes the "Roadmap, not done: bindable
`Visibility`" section of `2026-08-10-container-backgrounds.md` and the
options list it left open.

## The problem, restated precisely

`Visibility` lives in `Layout` and is read by layout
(`MeasureChild`/`ArrangeChild`), by `paintable`, and by the Composer's
per-frame visibility sweep — all deliberately OUTSIDE any evaluation
context. A naive `*prop.Property[Visibility]` read from those sites
subscribes nothing (the call-site rule), so a `Set` would dirty nothing
and no frame would ever run. The sweep machinery itself has been correct
since the runtime-visibility addendum: a flip it SEES erases, restores
what was underneath, and relayouts. The whole gap was waking it up.

## Chosen design: the composer owns invalidation

Landed as [PR #141](https://github.com/WonderForgeLabs/gooey/pull/141).
Three pieces, none of which touches an invariant:

1. **`Layout` carries the binding.** `BindVisibility(*prop.Property[Visibility])`,
   `BindVisibilityBool(*prop.Property[bool])` (true→Visible,
   false→Collapsed), and the general `BindVisibilityFunc(func() Visibility)`
   they both compile to. The plain `Visibility` field stays — it becomes
   a per-frame cache of the bound source, so every existing field reader
   (panels, focus traversal, hit-testing, control-plane snapshots,
   `paintable`, the sweep) stays correct, untouched.

2. **A per-node visibility observer in the Composer.** For each paint
   node whose `Layout` has a bound source, `armVisibility` creates one
   computed (`visObs`) whose evaluation reads the source — THAT is where
   the read becomes a subscription, by deliberate placement of the call
   site — and syncs the field. Its `OnInvalidate` is the composer's
   scheduler hook, so a `Set` schedules a frame. `Frame` re-Gets every
   observer before layout (outside any evaluation: a plain re-arm), so
   Measure/Arrange and the sweep see the new value in the same frame.
   The observer is not a paint node: it never renders, never counts in
   `painted`, and adds nothing to damage.

3. **A layout-time sync for the composer-less path.** `MeasureChild`
   copies the bound source into the field when one is present — a plain
   read in layout, recording nothing — which keeps the one-shot
   `Compose` path and any direct `Measure` caller correct too.

The result: a bound flip takes exactly the literal sweep's path and
costs exactly the literal flip's damage. `TestBoundVisibilityCollapseMatchesLiteralDamage`
asserts count-equality against a literal twin frame by frame;
`TestBoundVisibilityHiddenFlipMatchesLiteralDamage` pins hide/show at
one repaint each, and the wrongly-typed handle is a load error
(`TestVisibilityBindingWrongTypeIsLoadError`).

## Markup

`Visibility="{{.X}}"` resolves at build time (lvalue semantics) and
accepts two handle types, checked by type switch — no reflection, no
converter layer:

- `*prop.Property[gooey.Visibility]` — the full three-state surface;
- `*prop.Property[bool]` — true→Visible, false→Collapsed, XAML's
  `BooleanToVisibilityConverter` as a built-in default, because show/hide
  viewmodel state is almost always a bool.

Literals parse exactly as before. The binding works in every tier —
page, Include/markup-only control, UserControl — because `applyLayout`
runs for every element in every document; a markup-only control gets
reactive visibility today via `<x:Property Name="Show" Type="bool"/>` +
`Visibility="{{.Show}}"` (`TestBoundVisibilityInsideMarkupOnlyControl`).
The markup patch path carries a non-restated binding onto the rebuilt
element (`preserveLayout` adopts the old source).

## Rejected options

- **Layout reads become graph reads** (container-backgrounds option 1):
  rejects "layout runs outside the evaluation context", a load-bearing
  invariant, for one attribute.
- **Read visibility during paint, layout consults the last-evaluated
  value** (option 2): a frame of lag, and it cannot erase — a node going
  Hidden must be swept off screen, and the paint that would notice never
  runs if nothing schedules a frame. Same wake-up problem, plus lag.
- **An always-painted proxy node carrying the subscription**: breaks the
  idle guarantee — a frame where nothing changed must paint zero
  components — and pollutes damage counts.
- **Markup materializes the observer itself**: markup has no composer to
  hand the invalidation to at build time, and it would leave code-behind
  `BindVisibility` calls without change notification. Owning the
  observers in the Composer covers both tiers with one mechanism, and a
  `Dynamic` re-sync arms arrivals for free.

## Deferred

- **`<x:Property Type="visibility">`.** A `propKinds` row is one line
  (`kindOf(parseVisibility)`), but the control plane's `TypedValue` /
  `control.Kind` deliberately mirror the `propKinds` table, so a new
  kind is a wire-contract change (proto + `KindOf` + delta encoding),
  and off-table kinds degrade to descriptor-only entries. The bool
  mapping already makes visibility fully expressible in markup-only
  controls, so the declared type waits until it can land together with
  its wire representation.
- **Rebinding a composed element.** The observer subscribes to the
  source bound when it was armed; binding is a build-time act (markup's)
  or a pre-composition one (code-behind's). A first-time binding made
  after composition is picked up by the next frame's re-arm pass.
