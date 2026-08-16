# ColorPicker pixel tier (issue #24)

**Status:** executed ([PR #121](https://github.com/WonderForgeLabs/gooey/pull/121)). Unblocked by rendering chapter 2 (epic #21):
per-node placement ownership is what let this be ordinary component
work instead of pipeline work, exactly as that spec's closing note
predicted.

**Date:** 2026-08-10

## Plan

The picker's channel bars are the framework's worked example of
capability-adaptive rendering, and on a graphics-capable terminal they
were a blocky approximation of a gradient the terminal could show
exactly. Add the tier the pipeline was built for:

1. When `Frame.Graphics` is present and the cell size is known, each
   bar records ONE `Frame.Place` of a generated gradient image — the
   same sweep `renderBar` paints per cell, generated per pixel — over
   the bar's cells. Three placements per Render, in channel order:
   fixed slots, because the per-node diff pairs by index.
2. Cell tier byte-identical to today as the universal fallback (no
   protocol, or `CellW == 0` from a probe that timed out — the same
   guard as Button's pixel chrome).
3. Selection, hover, keyboard, mouse identical across tiers; the tier
   choice is the terminal's, not the author's — Image's doc language,
   restated on the picker.
4. Damage pins unchanged: a marker move repaints exactly the picker.

## Executed

As planned. The decisions worth arguing with:

### The marker is baked into the bar image, not overlaid

The issue offered a choice: re-place the bar under its kitty id and
overlay the marker as its own small placement (a move is ~30 bytes), or
bake the marker in. Baked won, because overlapping placements have no
reliable stacking anywhere:

- kitty draws the most recently placed image on top at equal z, so a
  bar replaced beneath a surviving marker (adjusting R regenerates the
  G and B bars — their sweeps genuinely change) would cover the marker.
- sixel/iTerm2 have no placement identity: a moved marker damages the
  cell it vacated, the cell flush re-sends the surviving bar it
  intersects, and the re-sent bar paints over the already-moved marker.

Baking keeps the pixel plane overlap-free. A marker move is a replace
of that bar's placement under its existing id — `a=d,d=I` + `a=T,i=N`
on kitty, one bar-sized PNG (a one-row gradient, a couple of KB) — and
a value edit replaces all three bars, which is honest: all three really
do look different. The cheap case is real too, just one level up: a
repaint whose state did not change reuses the cached image, and pointer
identity (`SameImage`) makes it free on the wire —
`TestColorPickerPixelChannelMoveReplacesOnlyTheAffectedBars` pins a
state-identical repaint at zero bytes and a channel move at exactly two
replaced bars.

### The cache is one entry per bar row

`pickerBarKey{w, cellW, cellH, cur, active}` against a `[3]pickerBar`.
The button caches by state because a button has four states; a picker's
key contains a full RGB color, so a map would grow with every color the
session visits. One entry per row keeps the pointer-equality win for
every repaint that matters (unchanged rows, state-identical repaints)
and regenerates on real change, which was going on the wire anyway.

### The cells beneath the image are still painted

`renderBar` runs unchanged on every tier; the placement composites over
it. That is not waste: the retained cell buffer is what a protocol
without placement identity repaints from when a bar moves or vanishes
(rendering-2's deviation 1), and it is why the cell-only output is
byte-identical to before — the pixel tier is purely additive.

### The readout swatch stays on the cell plane

A swatch is a solid color, and a terminal with a graphics protocol is
in practice a truecolor terminal: the cell plane already shows the
exact color. An image would add a placement slot to save nothing.

### colordemo now runs the full Detect handshake

The demo had deliberately avoided `Screen.Detect` because its probe
once abandoned a pending tty read that stole the first keystrokes. That
hazard was fixed in term (readUntilDA1 reads synchronously under a
deadline), and the pixel tier needs the handshake's answers — protocol
and cell size — so the stale avoidance is retired. Detect runs before
`Raw` and before the input decoder starts, same ordering as `App`.
`--graphics=kitty|sixel|iterm2|cells` forces the tier for recordings
(agg renders the cell plane only, so a GIF can never show this tier);
the status line names the tier in play.

## Deferrals

- A pixel readout swatch, if a graphics-capable 256-color terminal ever
  turns out to exist in practice.
- Placement z-order (kitty `z=`) — the general fix that would make
  overlay markers viable; worth doing only when a component needs
  overlap that slicing or baking cannot avoid. Tracked in
  [#177](https://github.com/WonderForgeLabs/gooey/issues/177).
