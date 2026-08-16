# Designer selection and drag: the derived scope, the accepted overlap, and the probed grid

Three decisions taken while building shallow-first selection and
snap-to-cell dragging in `apps/wysiwyg`. Each one has an obvious
simplification standing next to it, and this record exists so the next
person to reach for one knows it was reached for already.

It follows `docs/specs/2026-08-11-design-surface.md`, which specified the
frozen host and the selection gesture but left the *policy* — which node
of the chain a press selects — as an index into a walk it deliberately
did not fix.

## 1. The drill scope is derived, not stored

### What shipped

Selection is shallow-first: a single click selects the outermost element
under the pointer **that is a child of the current drill scope**, a
double-click drills exactly one level, and Escape selects the parent.

The scope is not a field. It is an identity:

```go
// select.go
scope == parentOf(selection),   clamped to the user's root
```

`nodeAtDepth` finds the scope's index in the chain `nodeChain` returns and
takes `chain[i+1+extra]` — `extra` is 0 for a press, 1 for a
double-click — clamped to the end of the chain.

### Why the policy inverted

This is the third policy over the same walk, and the history is short
enough to state:

| Policy | Why | Why it stopped |
|---|---|---|
| climb to the top-level kid | `ed.sel` was an `int` index into `root.Kids`; a flat index cannot name depth | the surface became chrome, so the user's root is `chain[1]` and climbing selected the root whatever you clicked |
| the deepest node | reaches everything the pointer is over | a container covered by its own child is unselectable |
| outermost within the scope | Blend / Figma / Illustrator | — |

The middle one's defect is worth naming precisely because it is not
obvious from a small document. A `<Border>` is covered *entirely* by its
own child, so under deepest-first every press inside one selected the
`<Text>` and the Border was reachable only through its one-cell chrome.
That is a real hole, and the drag work made it worse: a container you
cannot select is a container you cannot move.

`nodeChain` changed for none of the three. Every policy so far has been an
index into that chain, which is the shape the walk was built for and the
reason the inversion cost one function.

### What was verified, and what was inferred

The commonly-recalled version of "Blend semantics" is right in two places
and wrong in one, and the distinction matters to anyone deciding whether
to change this.

**Documented, and followed:**

- Escape selects the parent. Microsoft states it verbatim — "To select the
  parent of a current selection in the designer, press the ESC key"
  ([WPF Designer, VS2010](https://learn.microsoft.com/en-us/previous-versions/visualstudio/visual-studio-2010/bb514527(v=vs.100))).
  Figma is the outlier: there Escape is a global deselect. Sketch matches
  Microsoft.
- Blend's "active container" is real: double-clicking a layout panel makes
  it active, with a blue highlight
  ([Expression Blend 4](https://learn.microsoft.com/en-us/previous-versions/visualstudio/design-tools/expression-studio-4/cc294773(v=expression.40))).

**Inferred from the family, not from Blend's own page:** that the single
click is *scoped* to the active container. Blend's selection page
([cc295173](https://learn.microsoft.com/en-us/previous-versions/visualstudio/design-tools/expression-studio-4/cc295173(v=expression.40)))
is genuinely ambiguous — it only says clicking "activates the parent
container of the object", which reads as compatible with deepest-first.
Figma, Sketch and Illustrator are unambiguous and that is what was
followed.

The mechanical argument is the stronger one and it is what actually
decided it: **with the scope pinned to the root, a second double-click has
nothing left to do.** "Double-click drills one level" and "the scope does
not move" cannot both be true of a gesture you can repeat. Anyone
proposing a root-fixed scope has to answer that.

### Why not a stored `ed.scope`

A field is the obvious shape and it is what was rejected. The cost is not
the field; it is that a stored scope has to be **reset by hand**, and the
reset sites are not co-located with anything that would remind you:

| Event | What a stored scope needs | What it does if you forget |
|---|---|---|
| a click outside the current scope | pop out to the level the two share | selects nothing, or something inside a container the user has visibly left |
| `deleteSelected` empties a container | pop to its parent | the scope names a node that is no longer in the document; the next click resolves against nothing |
| `retype` changes the root's element | leave it alone or clamp | a scope under the old element |
| `rebuild` after any edit | keep it — the node pointers survive | (safe, and the one case that lures you into thinking the others are too) |

Four sites, three of them silent when missed, and "silent" here means the
designer selects something the user can see no reason for. The derived
form makes all four unanswerable: the scope cannot be stale because it is
recomputed from `ed.sel`, and `ed.sel` is already maintained by every one
of those paths because it is the selection.

The three gestures then compose without any of them knowing about the
others:

- a press selects `chain[i+1]`, so afterwards `parentOf(selection) ==
  chain[i]` — the same scope. **Idempotent.**
- a double-click selects `chain[i+2]`, so the scope becomes `chain[i+1]`.
  **The next double-click drills from there.**
- Escape selects `parentOf(selection)`, so the scope moves *up* with it.
  **The next press at the same cell re-selects what Escape landed on**,
  rather than drilling straight back in.

That last one is the coherence property, and it is the one a naive
implementation fails: moving the selection up while leaving the scope down
makes Escape look like it did nothing.

### The known limit

You cannot make an **empty** container the active scope. In Blend you can
— double-click a panel and draw into it — and here the scope only exists
relative to a selected child. The role Blend's active container plays for
insertion is already played by `ed.sel` through `addTarget`, so nothing is
lost today; if a future gesture needs "draw into this empty box", it needs
a scope that is not purely derived, and that is the trade to reopen.

### The pin

`TestEscapeSelectsTheParentAndTheNextClickAgrees` asserts the coherence
property. Recorded honestly: on a two-level fixture it does **not** fail
under a mutant that pins the scope to the root, because there a single
click gives the same answer either way. It is killed by the deepest-first
mutant, and its real job is pinning the observable guarantee against a
future implementation that stores the scope — which is not expressible in
the derived design and is exactly why the test outlives it.

`TestClickingOutsideTheScopePopsOutOfIt` nests its two siblings inside a
common container **deliberately**. At the top level, "pop one level" and
"reset to the root" give the same answer, so the test could not have
distinguished the derived walk from a hard reset. That was found by trying
to break it, not by review.

## 2. Two elements may share a grid cell

### The decision

A drag under a `<Grid>` snaps to whichever cell the pointer is in, and
**nothing checks whether that cell is occupied**. Two elements land in the
same cell, Grid renders the overlap, and no error is reported anywhere.

This was chosen over both alternatives.

**Bumping the second element** — moving it to the nearest free cell — is
the friendly-looking one, and it moves an element the user did not touch.
A direct-manipulation editor whose gesture relocates something else has no
way to explain itself: the user dragged A and B moved, with no
attribution.

**Refusing the drop** fails the gesture for a reason the pointer gives no
cue about. The user aims at a cell, the element does not go there, and the
only recovery is guessing which cells are free.

The overlap wins because **it is visible**. The user can see both elements
on top of each other and can move one. That is the whole argument, and it
only holds while the result is on screen — which is why a status-bar note
is acceptable and a silent auto-fix is not.

### What this does not license

The refusal path is a different question and it went the other way. A drag
that cannot proceed at all — a child of a `<VStack>`, whose position is
its index — now **says so**, naming the element and the container that
decided it (`dragSummary`). Silence is defensible when the outcome is
visible and indefensible when nothing happened.

That distinction is the actual rule: **the editor may decline to explain a
result the user can see, and may never decline to explain an absence.**

### The pin

`TestTwoElementsMayShareACell` asserts both halves — the dragged element
lands in the occupied cell, *and* the element already there did not move.
It exists so that a later collision rule has something to fail against
rather than something to quietly satisfy.

## 3. Grid cell rectangles are probed, not computed

### The problem

Snap-to-cell needs to know where the cells are. `components.Grid` does not
say: `rowSz`/`colSz` are the unexported Measure cache and
`distributeStars`/`offsets` are unexported. From outside the package you
can read `g.Rows`, `g.Cols` and `g.Bounds()`, and that is all.

### The obvious simplification, and why it was not taken

"Just compute the cell rects" — walk the declared tracks, sum the fixed
ones, distribute the stars over what is left. It is thirty lines and it is
**a second copy of layout arithmetic living in an editor**.

The failure mode is what rules it out rather than the duplication itself.
If the two ever disagree, the ghost snaps to a cell that the real layout
then puts the element somewhere else in — a preview that lies, which is
the exact defect the during-the-drag snap exists to prevent. And it would
disagree quietly, on whichever track type the copy got wrong, in whichever
grid happened to use it.

Auto tracks make it worse: their sizes come from the Measure pass over the
children, so a faithful copy would have to re-measure, which is most of
Grid.

### What shipped

`gridCells` walks the dragged child through every `(Row, Col)` and reads
its `Bounds()` back out of **the real `Grid.Arrange`**. The mechanics that
make that exact rather than approximate:

- **Stretch first.** `HAlign`/`VAlign` are set to `AlignStretch`,
  `Width`/`Height` to 0, `Margin` to zero, spans to 1. `ArrangeChild`
  hands a child the *whole slot* only under those conditions
  (`layout.go:196-215`); otherwise `Bounds()` is the element's rect inside
  the slot, which is not the cell.
- **Save and restore the whole `Layout` by value.** `saved := *l` works
  across packages despite the unexported fields, and the restore runs
  under `defer` with a final `Arrange` so the tree is where it was found.
  Without it the element stays drawn in the last probed cell until the
  next frame.
- **Once per gesture, not per motion.** It is an `Arrange` of the grid per
  cell, which is nothing for a design-time grid — but it mutates the live
  `Layout`, and doing that repeatedly while the user is dragging is a
  larger window for a frame to land mid-probe.

### The stated limit

**Auto tracks are sized by Measure, which the probe does not re-run.** A
grid whose track widths depend on the dragged element sees the slots it
had before the drag. The chosen *cell* is still right — the real frame
re-measures and re-arranges — but the rectangle the choice was made from
can be stale at the edges.

That is a real limit and it is written at the function rather than only
here. Re-running Measure inside the probe would fix it and would make the
probe cost a full layout per cell, which is the trade to reopen if
somebody meets it.

### One thing that bites in tests

`gooey.Layout` is **not comparable** — it holds `visSrc func() Visibility`
— so a test asserting "the probe put it back" must compare named fields.
`*l != saved` is a compile error, and the first version of
`TestTheGridCellProbeLeavesTheTreeWhereItFoundIt` was written that way.

## What this record does not decide

- **Reorder under a stack.** Still deferred, and still the one edit the
  catalog cannot express — `docs/specs/2026-08-11-design-surface.md` left
  it open and it stays open. What changed is only that the refusal is now
  audible.
- **Multi-select.** Every gesture here is single-selection. `ed.sel` is
  one node pointer, and the derived scope is defined in terms of it; a set
  would need a scope defined over the set's common ancestor.
- **A modifier for deep-select.** Figma's Cmd-click to the deepest hit is
  the standard escape hatch from shallow-first, and `input.MouseEvent`
  already carries `Mods`. It is not bound, because mouse reports cannot be
  injected through a recording pty and a modifier-only affordance could
  never be captured — the same rule that keeps the whole designer
  keyboard-operable. Escape plus repeated double-clicks covers the ground
  more slowly.
- **Remote mode.** Unchanged and still nothing: selection, drag and drill
  are all local-preview features, for the reason
  `docs/specs/2026-08-11-design-surface.md` gives.
