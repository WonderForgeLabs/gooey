# UI toolkit wave 1 (epic #72: issues #73, #74, #75, #77, #79, #81)

**Status:** executed. There was no prior spec — the contracts came from
Elan's dictated list and the child issue bodies, so this is written
as-built, and the decisions worth arguing with are called out rather
than smoothed over.

**Date:** 2026-08-10

## What shipped

Landed in [PR #87](https://github.com/WonderForgeLabs/gooey/pull/87).

Six components in `gooey/components`, each with a markup builder, an
entry in `docs/markup-reference.md`, and damage-count tests:

| Issue | Component | The thing it is *for* |
|---|---|---|
| #73 | `ProgressBar` | How far along a task is — determinate, or a marching band when there is no number |
| #74 | `Spinner` | Activity with no progress at all |
| #75 | `Toggle`, `Segmented` | A rocker switch, and the rocker past two positions |
| #77 | `StatusBar` | The dim bottom row every demo hand-rolled, promoted |
| #79 | `ButtonBar` | A toolbar: uniform sizing, separators, overflow, in-bar arrow traversal |
| #81 | `Button.Chrome` | The first component whose *chrome* is pixel content |

Plus `cmd/toolkitdemo` (markup-first, `gooey.App` host, GIF at
`toolkitdemo.gif`) and one new interface in the root package,
`gooey.FocusHost`.

## The decisions

### A rocker is not a checkbox, and the difference is the arrows

`Toggle` could have been `Checkbox` with different glyphs. What makes it
a rocker is that it has SIDES: `←` means off, `→` means on, and pushing
the side it is already on is not an operation. So those arrows are
**consumed only when they move something**, which means an arrow at the
end of travel keeps bubbling and moves focus instead.

That is not a special case invented here — it is the framework's
existing rule for unclaimed arrows (`FocusManager.Dispatch` falls
through to `FocusDir`), applied one level down. `Segmented` follows it
at both ends of its option list, and the payoff is that neither control
can trap a keyboard user.

### Disabled means the same thing everywhere

Every interactive component in the wave takes a `gooey.Action` and
derives "disabled" from it exactly as `Button` does:

```go
func (t *Toggle) disabled() bool { return t.Changed != nil && !t.Changed.CanExecute() }
```

An absent action is **inert, not disabled** — it paints normally and
reads nothing, which is what a switch bound only to a property expects.
An action whose condition says no paints `Dim` and refuses every
gesture. The condition is asked *while painting*, so the flip repaints
exactly one component and nothing subscribes to anything.

The short circuit matters and is deliberate: a disabled component stops
reading after `CanExecute`, so it does not subscribe to hover or press
either, and hovering a disabled control repaints nothing.

### Animation is the Timer discipline, twice

`ProgressBar` and `Spinner` are `Startable`. Neither ticker touches the
graph: it posts a step to the dispatcher and the step runs on the UI
loop, where the enabled/indeterminate condition is read. Consequences
worth stating:

- A determinate `ProgressBar` whose ticker is running advances nothing
  and **repaints nothing** — the paint node never read the animation
  phase, so there is no dependency to dirty.
- A `ProgressBar` with no `Indeterminate` property at all starts no
  goroutine. Absence is load-bearing, not a default.
- Lifetime is the `Composer`'s. It finds `Startable`s while walking and
  stops them on `Close`, so a component that leaves the tree cannot
  leave a ticker behind.

`Spinner` differs from `Timer` in one place: it reads `Enabled` while
*painting* as well as at fire time, because a paused spinner should look
paused (it parks at frame 0). That read is what makes the flip repaint
it, and it costs one repaint, once.

### ProgressBar's threshold ramp is opt-in — a deliberate deviation

Issue #73 says "threshold styling like Gauge". Implemented literally,
that colors a 96%-finished job crit-red, which says the opposite of what
happened. Recording the demo made this impossible to ignore: the bar
went green → amber → red as the build *succeeded*.

So the ramp is present and is exactly `Gauge`'s — `thresholdStyle`,
shared, so a value means the same color in both — but behind
`Thresholds bool` / `Thresholds="true"`, and off by default. It is for
the bars where the value really is a fill approaching a limit (a disk, a
quota). `Style` still overrides everything, as on `Gauge`.

**Flagged for Elan**: this is one line to revert if the literal reading
was intended.

### StatusBar sections are components, and the bar paints nothing

Issue #77 describes the promoted "dim Text row". Making the sections
`gooey.Component` rather than strings is what turns it into a real
component: a bar whose right section is a `Spinner` while something
loads is the same component as one showing three strings, and each
section keeps its own paint node — a ticking clock repaints the clock.

The attribute shorthand (`Left="{{.Status}}"` → a dim `Text`) is kept
because that IS the promoted pattern; property elements
(`<StatusBar.Right>`) take anything. Giving one section both is a load
error, since there is no reading of that which is not a mistake.

`StatusBar` has **no `Background`** and paints nothing of its own. A
container's bounds enclose its children's cells, so filling the row
would wipe sections whose nodes are clean — the blocker recorded in
[container backgrounds](2026-08-10-container-backgrounds.md). A bar that
should look like a bar styles its sections.

### ButtonBar needed a focus seam, so the root package grew one

"Within-bar arrow traversal" cannot be done from `gooey/components`: a
component has no way to reach the `FocusManager`, and faking it by
calling `SetFocused` directly would desynchronize the manager's own
index. Spatial navigation gets *most* of the way there for free (the
members are left and right of each other), but it cannot wrap at the
ends, which is the part that makes a toolbar feel like a toolbar.

So `input.go` gained:

```go
type FocusHost interface{ SetFocusManager(*FocusManager) }
```

handed out by `FocusManager.walk`, on the first walk and every `Resync`.
It is narrow on purpose: nothing is given to a component that did not
ask, and the only useful thing a host can do with it — `SetFocus` —
already validates that its argument is a live focus stop, so a stale
pointer from a replaced tree fails safely rather than focusing something
off screen.

**This touches invariant 6 (input routing).** It does not change any
existing routing: a tree with no `FocusHost` in it behaves identically.
And a focus host is explicitly not a focus trap — `tab` walks straight
through, the host only ever sees keys its children declined, and
declining an arrow hands it back to spatial navigation, which is how
`↑`/`↓` leave a horizontal bar.

`ButtonBar` also **collapses** overflowing members rather than clipping
them, restoring them at the start of the next measure pass. Clipping
would leave a focus stop that `tab` reaches and nobody can see;
`Collapsed` is already skipped by `FocusManager.reachable`, so
collapsing is the version that keeps the keyboard honest.

## Pixel chrome: the pattern this establishes (#81)

This is the first component whose *decoration* is pixel content, as
opposed to `Image`, whose content is a picture. Four rules came out of
it, and they are the pattern for whatever comes next (pixel sliders,
pixel tabs, a themed border).

**1. The chrome is generated, never loaded.** A rounded rectangle with a
vertical gradient and an outline is arithmetic. That is what lets it be
produced at whatever `Frame.CellW`/`CellH` the terminal turns out to
report, instead of scaling an asset authored for one cell size.

**2. Placements composite OVER cells, so pixel chrome must be sliced
around its own text.** A single image spanning the button would bury its
label. The pill is generated whole — so the gradient is continuous — and
then sliced into the four rectangles that are not the label: top edge,
bottom edge, and the two end caps of the middle row. The label is
painted on the cell plane in the window between the caps, over a
background matching the pill's interior so the seam does not read as a
hole. **The slot order is fixed**, because the per-node placement diff
pairs slots by index: a state change must produce the same four slots in
the same order or it reads as removals and additions instead of
replacements.

**3. Generated images must be cached by state, because the placement
diff compares image identity.** `graphics.Placement.SameImage` is
pointer equality. Regenerating pixels on every paint would retransmit
the whole chrome on every repaint even when nothing about it changed.
The cache is keyed by geometry plus the four-bool visual state, so it
holds at most a handful of entries per button and returning to rest
returns the *same* images.

**4. Tiering follows `ColorPicker`, and the footprint is identical
across tiers.** With a protocol and a known cell size, pixels; without
either — including a probe that timed out and left `CellW` at zero — the
same three-row pill in box-drawing runes. Same measure, same bounds, so
a page does not re-flow because the probe found something. The cell tier
is not a degraded pill; it is the universal one.

Everything else fell out of rendering chapter 2 with no new code: the
placements are recorded from `Render`, so the Composer files them under
this button's paint node and diffs them there. Hover replaces exactly
four images; a neighbour's repaint sends none; a hidden button has its
images deleted by id under kitty, and under sixel/iTerm2 the cells it
vacated are damaged instead. The tests assert all of that in bytes.

## Verification

- `go build ./... && go vet ./... && go test ./...` green in all three
  modules; `-race` on `mcp`.
- Damage-count contract tests per component: a value change, a focus
  move, a state flip each repaint exactly the affected component
  (2 for a focus move — the one that lost it and the one that gained
  it). A determinate bar under a running ticker repaints 0.
- Pixel lifecycle asserted on the wire: 4 transmits on first paint,
  4 deletes + 4 transmits on hover with **ids reused**, 0 bytes on a
  neighbour's repaint, 4 deletes on hide, cell damage under sixel.
- Cell tier audited with `render.Screen`: replaying the flush
  reproduces the buffer exactly.
- Demo driven under a pty at 96×22; `--mode kitty|sixel|iterm2` logs
  checked for protocol signatures. The kitty run shows 12 transmits and
  8 deletes across ids 1-4 only — the slots are reused across state
  changes, never churned.
- `toolkitdemo.gif` recorded with asciinema + agg. It shows the cell
  tier, because a recording pty reports no graphics protocol; that is
  the honest result and the reason the pixel tiers are verified by byte
  signatures instead.

## Not in this wave

Toasts (#76, blocked on z-order, #26), DataGrid (#78, its own
ItemsView-based effort), menus. `Checkbox` was deliberately left alone
rather than retrofitted with the conditional-`Changed` rule — that is a
behavior change to an existing component and belongs in its own change.
