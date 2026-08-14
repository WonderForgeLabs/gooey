# Frozen, observed

Supersedes the "FROZEN IS SAMPLED, NOT OBSERVED" paragraph in
`gooey.Frozen`'s doc comment and closes the one gap
`docs/specs/2026-08-11-design-surface.md` left open.

## The gap

`Frozen` landed as a **sampled** value. Every consumer asked once per
structural re-sync — `FocusManager.walk` rebuilds the focus order,
bindings, mnemonics and watchers from `Resync`; `Composer.collect`
rebuilds the Startable set from `walkNodes` — and both run only at
construction or when a `Dynamic` marks the composition dirty. A plain
`Frame()` did neither.

So a host that flipped its answer to toggle design mode kept the OLD
routing: the subtree stayed tabbable, its Startables kept running, and a
captor or hover already inside it stayed there. The doc comment said so
and told you to return a constant. That was honest and it was a
non-feature: **a design surface whose freeze cannot be toggled is a design
surface with no preview mode.**

It stayed that way for the reason recorded in
`spec-shape-after-first-consumer` — nothing consumed a dynamic `Frozen`,
so there was nothing to shape the mechanism against.

## The mechanism

Two parts, and the shape is not new: it is exactly what bound
`Visibility` already does (`Composer.armVisibility` plus the per-frame
visibility sweep in `Frame`), applied to a second fact.

**1. `Composer.armFrozen` (`composer.go`).** Every component implementing
`Frozen` gets an observer — a `prop.NewComputed(func() bool { return
isFrozen(n.w) })`. Its evaluation *calls the method*, so any property the
host reads to decide becomes a dependency of the observer by the ordinary
call-site rule. Its `OnInvalidate` schedules a frame and nothing else.

**2. The sweep in `Composer.Frame`.** In the same loop that re-arms
`visObs`, and deliberately BEFORE the `structDirty` block that consumes
it, each observer is re-`Get`ed and compared against the answer from last
frame. A **changed** answer raises `structureChanged()` — the same flag a
`Dynamic` container raises — so `walkNodes` re-derives the Startable set
and `FocusManager.Resync` re-derives the focus order, the scoped
bindings, the mnemonics and the hover watchers, all before anything
paints.

**3. `FocusManager.evictFrozen` (`input.go`).** The pointer half, and the
one thing `Resync`'s existing liveness checks cannot reach: they test
`m.parent`, and `m.parent` records frozen descendants *deliberately*, so
both a hover and a captor inside a newly frozen subtree pass. Hover is
**retargeted** to the frozen host (the state the next motion event would
have produced); capture is **dropped** (the host never received the
press, so handing it the release would synthesize a click nobody made).

Focus needed nothing new: the walk leaves the subtree out of `m.order`,
so `Resync`'s existing "the focused component vanished" path evicts it.

### Why no new interface

The obvious alternative was a second method returning a
`*prop.Property[bool]` for the framework to watch. Rejected:

- it is **two statements of one fact** that can disagree — `Frozen()` and
  `FrozenProperty()` would both exist and nothing would check them
  against each other;
- it **forbids a derived answer**. `mode.Get() && !previewFocused.Get()`
  is the natural way to write a real surface's frozen-ness, and a handle
  would force the host to maintain a mirror source. `prop.Set` does not
  compare values (`prop/prop.go:101`), so that mirror would re-dirty the
  composition on every no-op write;
- it declares what the framework can **discover**. Reading a property
  inside `Frozen()` is a subscription for the same reason reading one
  inside `Render` is a damage declaration. There is no `AffectsRender`
  and there should be no `AffectsFrozen`.

### Why the Composer and not the FocusManager

The FocusManager owns neither an invalidate hook nor per-component
storage, and freezing has **two** consumers — the input tree and the
Startable set. One observer in the Composer wakes both; one in the
FocusManager would have left Startables sampled, which is the consumer
with the safety argument (`Companion.Start` spawns a child process).

### Why invalidation is not the trigger

An invalidation says "something `Frozen()` read changed", not "the answer
changed". A re-sync walks the tree, stops and restarts Startables and
rebuilds the focus order; running that for an unchanged answer is pure
cost. The sweep comparing the ANSWER is the guard `prop.Set` does not
provide.

## Cost, measured

A flip repaints **nothing of its own**. Freezing changes what the tree
MEANS, not what it looks like, so the only components that repaint are
the ones reading the same property while painting.

- `markup.TestAFlipRepaintsNothingOfItsOwn`: **0** components repaint for
  a flip on a page where nothing paints the mode; **1** for a subsequent
  content change (the discrimination arm).
- `wysiwyg.TestTheModeFlipRepaintsOnlyTheIndicator`: **1** — the status
  bar's centre section, on the real shipped page at 160x48.
- `markup.TestANoOpSetDoesNotReSyncTheComposition`: a no-op `Set` costs
  exactly **1** `Frozen()` call (the observer re-arming); a real flip
  costs more, because `walk` asks once per container and
  `frozenAncestor` asks again per node.

A host whose `Frozen()` returns a constant reads nothing, records
nothing, is never invalidated, and costs one dead computed.

### One damage trap found on the way

The mode indicator's two labels started at different widths. A width
change moves the section's bounds, and a bounds change vacates cells: the
Composer clears the old rect and force-repaints everything beneath it. On
the shipped page that turned a one-component flip into **three**
(`[{0 0 160 48} {0 47 160 1} {48 47 24 1}]` — the root Grid, the
StatusBar, the label). All three repaints are *correct*, which is exactly
why nothing else would have caught it. The labels are now the same width
by construction and
`wysiwyg.TestTheTwoModeLabelsAreTheSameWidth` says why.

## The limit, and it is deliberate

The observer subscribes to what `Frozen()` **reads**. An implementation
over plain Go state — a bare bool field written by a handler — records no
dependency, so nothing wakes and the old sampled behaviour is what you
get. `Composer.InvalidateStructure` is the escape hatch.

`markup.TestFrozenOverPlainGoStateIsStillSampled` pins this as a passing
test rather than as prose, because the prose would survive someone
"fixing" it with a per-frame poll over every node — and a poll is the
design this framework rejects everywhere else. (Measured: making the
sweep unconditional does make plain state work, and it fails that test
and the idempotence one together.)

## The consumer

`examples/wysiwyg` — the design surface `Frozen` was built for.
`preview.Pane` is now a `Frozen` host reading the editor's `design`
property; `d` toggles it; the status bar's centre section says DESIGN or
LIVE. In DESIGN (the default) the document lays out and paints exactly as
it will and nothing in it is tabbable, clickable, hoverable or running.

Freezing by default also fixed something the editor had wrong before
there was a switch: every focusable component in the DOCUMENT was a focus
stop in the EDITOR, so tab walked out of the shell and into the thing
being edited.

Verified under a pty as well as in tests: the shipped binary shows
`DESIGN — d for LIVE` at startup and `LIVE — d for DESIGN` after one
keystroke, and the arm that never presses `d` never shows the second.

## What was NOT touched

The four `!frozen` guards in `FocusManager.walk` and the single retarget
in `DispatchMouse` are unchanged, including the comments recording which
is the guarantee (the mnemonic skip) and which is defence in depth (the
KeyBinding and HoverWatcher skips). Each new pointer line in
`evictFrozen` was A/B'd on its own: disabling the hover line fails only
`TestFreezingMovesHoverOffTheDescendant`, disabling the captor line fails
only `TestFreezingDropsACaptureTakenBeforeTheFlip`. Neither is redundant
with the other, and neither is redundant with the `DispatchMouse`
retarget — which cannot help a captor, because `target()` returns the
captor ahead of the retargeted hit.
