# UI toolkit wave 2: overlays (issues #76, #78)

**Status:** executed.

**Date:** 2026-08-10

> **The z-order half of this record is superseded.** "The app declares
> the overlay element last" was the mechanism chosen here, and it was
> retired in three steps: `2026-08-30-overlay-layer.md` (overlays are
> lifted into a paint layer, so position stops deciding),
> `2026-09-05-overlay-ranks.md` (a rank orders that layer against
> itself), `2026-09-05-one-shot-overlay-order.md` (one rule for both
> paint paths). Everything else here — the popup lifecycle, the capture
> and modality rules, the restore pass — still holds. Left unrewritten
> because a spec records what was decided on its date.

## What was asked for

The two components unblocked by container backgrounds & z-ordered
repaint (#26, [container backgrounds](2026-08-10-container-backgrounds.md)):

- **#76 Toast/notification layer** — transient overlay messages with
  auto-dismiss on a Startable timer.
- **#78 Menu/MenuBar** — a top menu row with dropdown menus: focus
  capture, esc-dismiss, gesture hints, and focus **restored** to
  whatever had it when the menu dismisses.

Both are, structurally, the same thing: *a later sibling painting above
what it covers*. The z-order pass made that legal; this wave is the
first components built on it — and the first to need its missing half.

## Plan

An overlay is a component placed **late in document order** with a
covering paint (a leaf's pre-clear or a background fill), per the merged
z-order pass. No overlay registry, no adorner layer, no new hosting
machinery: the app declares the overlay element last, and in a `Grid`
the element's position (`Grid.Row`) is independent of its document
order, so "last child, top row" is spellable today.

- Toasts: a `ToastHost` container the app places as the last child of
  the root, stacking `Toast` leaves in the top-right corner, realized
  and dismissed through the existing `Dynamic` re-sync, auto-dismissed
  by goroutines that follow the Timer post-and-join discipline.
- MenuBar: bar row + a dropdown child arranged BELOW the bar's own
  bounds. Keyboard routes through focus (the bar is the focus stop);
  mouse routes through **pointer capture** while open, which also makes
  the out-of-bounds dropdown clickable — hit-testing never has to find
  it. Focus restore rides a new one-liner on the FocusManager.

Damage-count tests for toast appear/dismiss, menu open/navigate/close,
focus restore; idle frames stay 0.

## Executed

Landed in [PR #93](https://github.com/WonderForgeLabs/gooey/pull/93).

### The composer gained the reverse half of the z-order pass

The verification step the task ordered ("verify dismiss repaints from
beneath") failed, as suspected: the forward pass can only force nodes
*later* in z-order than a painter, and an overlay is the LAST node — so
a dismissed overlay's vacated cells were cleared to the ancestor
background and nothing beneath ever repainted. A hole, on all three
vanish paths (Visibility, `Dynamic` departure, bounds move).

The fix is `Composer.restoreUnder`, and it lives in the **sweeps**, not
in the paint loop — the one-forward-pass shape of the frame is intact:

- when a node turns non-visible, the sweep clears its rect (outside any
  evaluation, exactly like the vacated-bounds clear), drops its pixel
  placements so the placement diff emits the removals, and force-dirties
  every still-visible node intersecting the rect;
- when a node departs in a re-sync, `walkNodes` does the same after its
  existing clear;
- when a node's bounds change, the vacated rect gets the same treatment
  (a menu switching titles is an overlay *moving*).

The forced nodes then repaint in the ordinary loop, in z-order, with the
forward pass keeping everything above them honest. Two consequences:

1. **A vanished node no longer paints its own erasure.** Its clear
   happens in the sweep, so hiding a leaf costs 0 paints plus the
   repaints of what was beneath, and hiding a container costs its
   forced children only. Two contract tests changed accordingly —
   `TestHidingAContainerAtRuntimeErasesItsChrome` (2→1: the clear is a
   sweep, the child is the paint) and the counts stay pinned. The cell
   assertions are unchanged: the screen result is identical.
2. **The forward pass skips non-paintable nodes.** A Hidden node keeps
   its bounds; forcing it from below would run its pre-clear over cells
   the restore just repainted. It has nothing on screen to restore, so
   it is exempt — the same shape of argument as the `Decorator`
   exemption. `restoreUnder` itself does NOT exempt decorators: the
   cells they re-style are exactly the ones being restored, and a
   selected row under a dismissed toast keeps its highlight because the
   overlay forces it back down.

New pins: `TestHidingAnOverlayRestoresWhatWasBeneath` (the Canvas-side
inverse of `TestCanvasOverlapRepaintRepaintsTheOccluderAbove`),
`TestToastDismissRestoresWhatWasBeneath` (the Dynamic path),
`TestMenuSwitchTitleMovesTheDropdown` (the bounds-move path). The pixel
plane is covered by the existing placement tests, which caught the
first version of the visibility change (a hidden node that never
evaluates never dropped its placements — fixed by dropping them in the
sweep).

### ToastHost (#76)

`components/toast.go`. The host is a chrome-less, background-less
container implementing `Dynamic` + `Startable`; each toast is an
ordinary leaf whose pre-clear + full-row paint covers its rect — that
covering is what makes it an overlay under the z-pass. `Show`/`ShowFor`
append a child and raise the structure hook; `Dismiss` (idempotent —
the timer and a manual dismissal may race onto the loop) removes it and
the restore pass repaints what it covered.

Auto-dismiss is the Timer discipline with the close-AND-join rule: one
goroutine per timed toast, posting the dismissal through the `post` the
host got at `Start`; the stop func closes the gate and `wg.Wait()`s, so
`stop returns ⇒ no further posts, ever` (pinned by
`TestToastAutoDismissPostsAndJoins`, including a Show-after-Close
probe). A never-started host shows sticky toasts — degraded, not
broken.

Costs, pinned: showing paints 1 (the new toast; the host's node stays
clean), dismissing paints exactly what the rect intersected, idle
settles to 0.

### MenuBar (#78)

`components/menu.go`. `Menu`/`MenuItem` are plain data structs — in
markup they are declarations consumed by the builder, like Grid track
lists, and never enter the visual tree. The bar is a container with two
kinds of paint: its own chrome (the title row) and a single `menuPopup`
leaf child, `Collapsed` while closed, arranged below the bar's bounds
when open. Open/current/selection are source properties, so the bar
row and the dropdown are separate paint nodes: opening paints 2 (bar
highlight + dropdown), navigating paints 1 (dropdown only), a
CanExecute flip repaints the dropdown alone.

**Dismissal routing** is capture, not tunneling. A tunnel guard cannot
work here twice over: the dropdown hangs outside the bar's bounds where
hit-testing never finds it, and a click far from the bar tunnels down
an ancestor chain the bar is not on. So `Open` takes
`CaptureMouse(bar)` — held capture — and every pointer event routes to
the bar while the menu is up: presses on items activate (on press, the
Windows-menu gesture), motion drags the highlight and slides across
titles, and a press anywhere else dismisses *and is consumed*, so it
never reaches — or activates — what is underneath (pinned by
`TestMenuClickElsewhereDismissesWithoutActivating`). Capture also
freezes hover for the duration, which is exactly right for a modal.

**Focus restore** got a framework one-liner: `FocusManager` now
remembers `PreviouslyFocused()` (set on every real focus move, dropped
on Resync if it left the tree). The subtlety is that by the time a
press on a title bubbles to the bar, focus-follows-click has *already*
moved focus to the bar — the component to restore is the one the
manager just remembers losing. Key-opens pass `nil` (the bar was
focused legitimately; esc leaves focus on it), and `Dismiss` restores
only while focus is still on the bar, so a menu dismissed after the
user moved on does not yank focus back. Known approximation, recorded:
tab-to-bar *then* click-a-title restores to the pre-tab component
rather than the bar.

**Keys.** Closed + focused: `←`/`→` move the title highlight (consumed
only when they can move — a one-menu bar consumes no arrows, the
Toggle rocker rule), `enter`/`↓`/`space` open. Open: arrows navigate
(separators skipped, wrap), `enter` activates, `esc` dismisses,
`tab` dismisses and travels on, and **everything else is swallowed** —
an open menu is modal, so the page's `q` cannot quit underneath it.

**Gesture hints are display, not bindings.** `ParseGesture` validates
at markup load (a typo is a load error) and the hint is stored in the
canonical `KeyEvent.String()` spelling, so the hint on screen is
byte-identical to what a `KeyBinding` would declare. Wiring the key
itself stays a `KeyBinding` — one gesture system, no second dispatch
path hiding in a menu definition.

**Disabled items** carry `gooey.Action` and read `CanExecute` while
painting: `Dim`, refuse activation, menu stays open (nothing
happened). An item with no action closes the menu and nothing more —
inert, not disabled, as everywhere.

### Markup

`<MenuBar>`/`<Menu>`/`<MenuItem>` and `<ToastHost>` in
`markup/markup.go`, documented in `docs/markup-reference.md`. Both fit
the builder pattern; the one deliberate misfit: **toasts have no markup
form**. A toast is imperative by nature ("show this now") — the
declarative surface is the host, and `Show` is code, reached through
`Name=` + `markup.Find`. MenuItem `Command` resolves like `Click`
(binding or bare handler name), so conditional commands cross
transparently.

### Demo

`cmd/toolkitdemo` extended in place: a `Job`/`Notify` menu bar over the
existing page, a `toast` button in the ButtonBar, and `KeyBinding`s
matching every gesture hint the menu shows (the hints tell the truth).
The overlay elements are the last children of the Grid with
`Grid.Row="0"` — the demo file itself demonstrates the document-order
rule. `TestDemoOverlaysDropAndRestore` drives the shipped markup:
menu open shows items + hints over the content, esc restores the exact
screen, a toast appears and dismisses without a scar.

The GIF: re-recorded via the house pipeline (see `demos.md` workflow) —
cell tier, keyboard only, as always.

## Invariants touched

- **Damage discipline (invariant 3):** extended, not weakened. The new
  restore pass is Sets-between-evaluations from the sweeps; the paint
  loop is still one forward pass. Erasure of a vanished node moved out
  of its paint node into the sweep — two hiding tests re-pinned at the
  new (lower) counts.
- **Input routing (invariant 6):** no dispatch-order change.
  `FocusManager.PreviouslyFocused` is a passive memory; MenuBar uses
  the existing capture/FocusHost seams.
- **Startable close-and-join:** ToastHost follows it; pinned.

## Not in this wave

Context menus (right-click; needs a popup placed at the pointer, same
machinery, different anchor), submenus, menu mnemonics (`alt+f`),
toast severity levels beyond Style, wheel interaction in menus. A
general `Popup`/adorner primitive is deliberately NOT extracted yet:
two overlays is not enough evidence for the right abstraction, and
both fit in components as-is.

## Menus round 2 (issues #125, #126) — same day

Landed in [PR #133](https://github.com/WonderForgeLabs/gooey/pull/133).

### The live-click bug (#125)

Elan ran the demo and clicks did nothing. Every test above passed
because they synthesized events onto the bar's handlers; the real
pipeline dies earlier, and not in the menu: **hit-testing treats every
Bounded container as opaque**, and the demo's full-page `ToastHost` —
the LAST child, spanning the grid, measuring to `avail` — is the first
thing `hitTest` finds for every cell on screen. Every press landed on
the toast layer and bubbled past the MenuBar, an earlier sibling that
is never on the bubble path. The toast layer had been eating every
click and hover on the page since it shipped; its own doc comment
("a page that never shows a toast pays nothing for hosting the layer")
was wrong in exactly this respect.

The fix landed separately: the adornments work (#129) hit the same
wall and added the `HitTestTransparent` seam in `mouse.go` —
`ToastHost` and friends opt out of hit-testing; `Toast` leaves stay
hittable. What this branch adds is the regression the original wave
owed: `TestMenuClicksThroughLiveDispatchUnderToastLayer`
(components/menu_live_test.go) drives press/release `input.Event`s
through `Composer.Handle` against the demo's page shape — toast layer
on top — and pins click-to-open and click-to-activate from the menu's
side. Lesson recorded: an input-path test that does not enter through
`Composer.Handle` is not a live test.

### Mnemonics (#126)

`alt+letter` opens the matching menu from anywhere on the page; while
open, plain letters activate matching items and `alt+letter` switches
menus. Decisions:

- **Marker: underscore, XAML's AccessText convention.** `Title="_File"`,
  `Text="E_xit"`; `__` is a literal underscore; only the first marker
  counts; an UNMARKED string defaults to its first letter, so every
  menu has an accelerator without any authoring. Underscore rather
  than `&` because these strings live in XML attributes, where `&` is
  an entity. Parsing lives in the component (`splitMnemonic`) — markup
  passes `Title`/`Text` through untouched.
- **Underline always**, on bar titles and dropdown items. A terminal
  cannot see a held ALT (no key-up events), so WPF's show-on-ALT is
  unimplementable; always-on is honest, and it is static chrome — no
  property, no damage.
- **Dispatch: a new page-scoped phase, after bubble, before tab/arrow
  fallbacks.** `gooey.MnemonicHandler` is collected on the same walk
  that finds focus stops; `Dispatch` offers it only the keys the whole
  focused chain declined. This is the mechanism that lets the bar — a
  sibling of the content, never on the focused chain — see `alt+f` at
  all, and the ordering is what keeps any `KeyBinding` on the same
  gesture winning (invariant 6 extended, not reordered: the phase sits
  exactly where the framework's own tab/arrow fallbacks already were).
- While open the menu is modal, so letters and `alt+letter` are
  handled inside `handleOpenKey` and swallowed match or no match. A
  letter matching a disabled item moves the highlight and refuses,
  like enter on it.
- Restore semantics: an accelerator open restores focus on dismiss to
  whatever held it when the accelerator fired (focus has NOT moved
  yet, unlike a mouse open where focus-follows-click already ran).
- Input layer needed nothing: `ESC`+letter already decodes as
  `alt+letter` (same-read), `ParseGesture("alt+f")` already parses,
  and the lone-ESC idle timeout only bites when the two bytes split
  across reads AND the gap exceeds the timeout — the terminal sends
  them together.

Damage: alt-open repaints 3 (focus loser + bar + dropdown) — pinned in
`TestMenuAltMnemonicOpensFromAnywhere`; the underlines cost nothing.
