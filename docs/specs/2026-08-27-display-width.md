# A buffer column is a terminal column

Decided 2026-08-27 while fixing
[#358](https://github.com/WonderForgeLabs/gooey/issues/358). The issue
asked for rune-width awareness. What it needed was an invariant, a
dependency, and a way to see the bug — in that order, because for most of
this change's life the tree was green and wrong.

## The invariant

A `render.Buffer` is a grid of cells and a terminal is a grid of columns,
and the framework's entire layout model assumes those are the same grid:
`Arrange` places a component at column `i`, `Render` writes to cell `i`,
and nobody checks. For ASCII they are the same. A CJK character or an
emoji is **two columns**, and the buffer gave it one cell — so everything
right of it drew two columns further left than the buffer said, on every
row that held one.

The invariant is therefore: **cell index equals terminal column.**
`render.TerminalColumns(b, y)[i] == i` for every `i`, and
`render.Displaced(b, y)` names the first cell where it stops holding.

Two mechanisms keep it:

- `SetString` reserves the cells a glyph covers, writing `Continuation`
  — a sentinel rune, never drawn — into the second column of a wide one.
- `Set` and `SetString` **repair the seam** when a write lands on half of
  a glyph. The framework's paint model is overpainting (`flush.go:25`
  says so outright), which means landing on half a wide glyph is
  ordinary, not exotic.

## Why the repair is not optional

Both halves of a broken pair are silent, and neither self-corrects:

| Break | Consequence |
|---|---|
| write over the **lead** | the orphaned `Continuation` is a column the flusher skips forever — nothing can ever repaint it |
| write over the **continuation** | the surviving lead still draws two columns and displaces the rest of the row |

The rule is one predicate: cell `x` is a `Continuation` **exactly when**
cell `x-1` is a wide lead. Either half alone is a corrupt row. The
survivor becomes a space, because a space is the only value that both
draws correctly and occupies exactly the one column the cell owns.

This is enforced in `Buffer`, not in callers, because the callers are
every `Render` in the repo. It is deliberately **not** enforced against
direct `Buffer.Cells` assignment, which is the remaining sharp edge, and
`render.Displaced` is how you find out.

This paragraph originally named `components/itemsview.go` and
`apps/introdeck/terminal.go` as examples of it, and they were not. Both
went through `Buffer.Set`, so both got the repair; what they lost was
the cell's `Cluster`, which `Set` cannot carry because it takes a rune.
That is a different defect with a different fix — `Cell.WithStyle` and
`Buffer.SetCell`, added in review of #413 — and naming the wrong
criterion sent a reader looking for a `Cells[i] =` that was not there
while the real hazard sat in plain sight. **Restyling a cell is
`SetCell`, not `Set`**; direct `Cells` assignment is the only thing left
outside the repair. (Sites are named without line numbers on purpose: a
`file.go:NN` stays resolvable after an insert and quietly means
something else.)

## Width and content must come from the same thing

A `Cell` now records the whole grapheme cluster when it is more than one
rune. The first version stored only the cluster's first rune while
reserving cells by the *cluster's* width, and the two disagreed in both
directions at once:

- `"⚠️"` is U+26A0 U+FE0F — two columns as a cluster, one as a lead rune.
  The row reserved two cells and drew a one-column glyph, displacing
  everything after it in the *opposite* direction from the bug this
  change exists to fix.
- Every rune past the first was dropped, so decomposed `"é"` painted as
  `"e"` and a keycap lost its `U+FE0F U+20E3` tail.

The comment shipped with that version called the second one "a narrowing,
not a regression — the old loop gave each mark its own cell and its own
column". That was wrong, and wrong in the way worth recording: the old
flusher emitted both runes and the **terminal** composed them, so the
accent rendered. A justification written from the buffer's point of view
missed that the terminal is the thing doing the drawing.

## The dependency

`github.com/rivo/uniseg` becomes a direct requirement of the root module.
CLAUDE.md calls adding one a decision to justify rather than a count to
check, so:

- **What it compiles into.** `go list -deps github.com/rivo/uniseg`
  returns only itself — no transitive graph.
- **Why not hand-rolled.** Correct segmentation is the Unicode grapheme
  break algorithm plus an emoji-width table, both of which change with
  each Unicode revision. The `⚠️` case above is exactly what a hand-rolled
  version gets wrong, and this change got it wrong *with* uniseg by using
  the library's width and not its content.
- **Why the root module.** The cell plane is `render`, which is core.
  The doctrine's target is SDKs that drag in protocol stacks; this is a
  single leaf package implementing a Unicode annex.

`go list -f '{{.Dir}}' github.com/rivo/uniseg` resolves under `vendor/`.

## Why it stayed green

Worth recording because it is the reusable part.

Roughly 35 sites measured display width with `len([]rune(s))`, and the
suite was green throughout. Two reasons, and the second is the one that
generalises:

1. Every fixture in the repo was ASCII, where the two counts agree.
2. Six packages each had a private `row(b, y)` test helper that built a
   string cell by cell and emitted the `Continuation` sentinel as a
   literal rune. A row holding `世界` read back as `世�界�`, so
   **no fixture could contain a wide glyph and be asserted on.** The
   failure was unassertable by construction, not merely unasserted.

`render.RowText` is the fix for (2), and the technique for (1) is to
build fixtures from two strings with the same **column** width and
different rune counts — `"世界"` against `"abcd"` — and assert they
measure alike. An ASCII fixture agrees with itself under either rule and
passes against the bug.

The same trap has a paint-side form. A test that located the mnemonic
accelerator by searching the row for its rune could not fail, because the
bug writes that rune *into the continuation cell* — the row holds two
copies and the search matches the corrupted one, which is underlined.
Locate by construction, never by searching for the value you expect.

## Rejected alternatives

**Reserve by the lead rune's width instead of the cluster's.** Suggested
in review as a way to make the two numbers agree. It agrees them at the
wrong value: a terminal *will* advance two columns for `⚠️`, so a
one-column reservation displaces the row on a real screen while the model
reports it faithful. Storing the cluster agrees them at the right one.

**Clip at the buffer edge and let bounds look after themselves.** What
the code did. `SetString`'s only clip was `b.W`, but the real constraint
is always the caller's bounds, which are narrower — and since gooey clips
nothing at the frame level
([#357](https://github.com/WonderForgeLabs/gooey/issues/357)) the
overflow lands in a sibling whose paint node is clean, so nothing
repaints it back. `render.ClipCols` exists so callers can fit to their
own width.

**Let `ClipCols` split a glyph to fill its budget exactly.** It stops
*before* a glyph that would overrun, so it can return one column short.
Emitting a lead rune alone is worse than the unused column: it is a glyph
the terminal draws at the wrong width.
