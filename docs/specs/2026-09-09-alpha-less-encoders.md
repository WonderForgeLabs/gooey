# Two pictures, chosen by the encoder

**Status:** accepted, implemented (`graphics/graphics.go`, `apps/wysiwyg/components/panel`)
**Issue:** [#254](https://github.com/WonderForgeLabs/gooey/issues/254)
**Date:** 2026-09-09

## The gap

`graphics.Encoder` is written as one question — hand me an image, I put it
on the wire — and for everything the framework had drawn until now that
was true. Sixel, kitty and iTerm2 differ in *how* a picture travels, not
in *which* picture arrives.

That stops being true the moment a drawing contains a translucent pixel.

- **kitty and iTerm2** transmit through `png.Encode`, which
  un-premultiplies. The alpha reaches the terminal, and the terminal
  composites against its own background — a colour this process never
  learns and therefore cannot better.
- **sixel has no alpha channel at all.** `sixel.go` writes no pixel below
  half alpha, so a faint stroke is not dimmed on the way out. It is
  **discarded**, and the drawing that produced it silently loses a line.

`apps/wysiwyg/components/panel` is where that surfaced. The pane's
hairline under the title is a 0.4-alpha stroke; on sixel the whole rule
vanished. `TestTheHairlineReachesTheSixelStream` measures it: at one
canvas geometry with only the stroke's alpha changed, the encoder's byte
stream for the translucent rule is **identical to the same canvas with no
rule drawn at all** — 80 bytes either way, against 107 for an opaque one.

## What was tried first, and why it was wrong

The first fix for #254 made every tier opaque. That mends sixel and
**spends the two tiers that were already right**: kitty and iTerm2 stop
compositing against the terminal's real background and start compositing
against this package's guess, which is black.

One picture for three protocols is only correct while the protocols agree
about what a picture is. Here they do not.

## The decision

`graphics.OpaqueEncoder` is a marker interface on the encoder:

```go
// OpaqueEncoder is an Encoder whose wire format carries NO ALPHA.
type OpaqueEncoder interface {
	Encoder
	OpaqueOnly()
}
```

A drawing that wants a faint line asks the encoder, and draws
accordingly: a dimmer **opaque colour** where alpha cannot travel, the
translucent stroke where the terminal will composite it.

Three consequences worth stating, because each is a cost:

**It is a capability question, not a protocol name.** A caller must not
switch on `Sixel`, for the reason `IDEncoder` exists rather than a kitty
check — a fourth protocol is somebody else's to add, and it has to be
able to answer without editing every drawing.

**The default is the composited branch**, which is the safe one for a
protocol nobody has classified: a translucent stroke through a wire that
carries alpha is correct, and through one that does not it is invisible
rather than wrong-coloured. But an *unclassified alpha-less* encoder is
the bad case, so `graphics`'s own test parses the package and fails on a
declared `Encoder` with no row in its table — including one that
satisfies the interface by embedding.

**A drawing that branches on this needs a ground**, and the ground is
where the honesty is. Compositing needs something to composite against,
and a component that is not a `gooey.HasBackground` has no way to learn
what its cells will be cleared to: `Composer.clearStyle` is unexported
and takes a `*paintNode`, and a chrome-only container pre-clears nothing,
so the cells cannot be read back off the frame either. `panel` guesses
black and says so at the guess. That guess is *provably wrong* for a
document like `<VStack Background="#282c34"><Panel/></VStack>`, which the
designer renders today.

## What this does not decide

- **Learning the terminal's own background** (OSC 11) is
  [#259](https://github.com/WonderForgeLabs/gooey/issues/259). It would
  make the composited answer available to the alpha-less tier as well,
  which would shrink this decision to "which colour", not "which
  picture".
- **A framework accessor for "the ground my bounds clear to"** does not
  exist. It is the other half of the same problem and a core change; the
  panel package carries the statement of what it would fix.
- **Halfblock is not a tier here.** Halfblock *is* the nil encoder, and a
  component returns to its cell path the moment `Frame.Graphics` is nil —
  before any placement — so `DrawHalfblock` is never reached from a
  component that draws its own canvas.
