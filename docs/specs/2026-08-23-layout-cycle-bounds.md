# Bounding a component cycle: seven walks, two strategies, one number

Decided 2026-08-23 while fixing
[#216](https://github.com/WonderForgeLabs/gooey/issues/216), which asked
for a depth cap on `MeasureChild`. The cap is here, but the issue
understated the problem by six walks and one whole phase, so the shape of
the answer is worth recording.

## Why a cycle is special

A stack overflow is the one failure this framework cannot report through
its own error path. It is `fatal error`, not `panic`: unrecoverable, so
`Screen.Restore` never runs and the user is left in raw mode with no
echo, and whatever the editor held unsaved is gone. So a cycle has to be
*prevented*, not caught.

It is also constructible by ordinary use, not only by a hand-written
pathological file. The wysiwyg editor lets a user create `card.gooey` and
drop `<Card/>` into it — two clicks — and lets them drag a container into
its own descendant.

## What the crash actually was

Not one bug. Seven independent recursions over `ChildComponents()`, each
of which died on its own:

| Walk | Where | How it died |
|---|---|---|
| `Composer.build` → `collect` | `composer.go` | **Not a stack overflow.** It allocates a paint node and three property nodes per level, so a cyclic tree *grows the heap* — the process wedges with no fatal error and no trace. |
| `FocusManager.walk` | `input.go` | stack overflow inside `m.parent`'s own `mapassign` |
| `MeasureChild` | `layout.go` | the crash #216 was filed for |
| `ArrangeChild` | `layout.go` | #216 asked whether Arrange needed it too; it does |
| `hitTest` | `mouse.go` | per mouse motion event |
| `firstFocusable` | `mouse.go` | per click |
| `renderTree` | `component.go` | the one-shot `Compose` path |

The ordering matters: `Composer.build` runs **before layout exists**, so
capping `MeasureChild` alone would have left the original crash in place
— it would merely have become a hang instead.

And an eighth, in a different phase entirely: a markup control that
includes itself never reaches layout at all. `loadDocument → build →
control → loadDocument` overflows the stack *at load time*, inside
`encoding/xml`.

## Two strategies, chosen per walk

**Identity, where a map already exists.** `Composer.build` keys
`nodeOf` by component and `FocusManager.walk` keys `m.parent` by
component. One component, two paint nodes is impossible — that is the
damage model's central assumption, not an implementation detail — so a
component reached twice in one walk is a cycle or a double placement,
and neither is representable. The check is exact, catches on the second
visit rather than the 512th, and costs a lookup that walk was already
paying.

**Depth, everywhere else.** `MeasureChild`/`ArrangeChild` count in a
package-level `layoutDepth` with a deferred decrement (`defer` runs
during panic unwinding, so the counter cannot be left climbing).
`hitTest`, `firstFocusable` and `renderTree` thread an `int` parameter
instead, because they return from inside a loop and a defer per
component per mouse event is a real cost where an increment is not.

**A load error, for markup.** `markup.control` — the one path both
`Include` and `UserControl` pass through — carries the ancestry of
controls being instantiated on the `Context`, and a control that appears
in its own ancestry is a load error naming the loop
(`card.gooey → panel.gooey → card.gooey`). On the `Context` rather than
in a package variable because `markup.Load` is not confined to the UI
goroutine — a file watcher loads on its own.

Ancestry, not history, is the load-time subtlety. Two `<Card/>` elements
side by side are legal and common; a "have I seen this control" set
rejects them.

## Where 512 came from

The issue was parked on exactly this question, and said so: "the honest
answer needs a measurement nobody has taken." Two were taken.

**Below — the deepest tree that exists.** `MeasureChild` was
instrumented and every module's test suite run: the maximum depth
anywhere is **7** (apps/wysiwyg, and the root module). The deepest real
app screen, driven under a pty, reaches **6** (apps/store). The deepest
`.gooey` document nests **10** XML elements, but several of those
(`<Gooey>`, `<Grid.Rows>`, `<Gooey.Resources>`) are not layout nodes,
which is why the static figure is the larger one.

**Above — what the stack survives.** An uncapped cycle through three
real containers (`Border`/`VStack`/`Grid`) dies with `goroutine stack
exceeds 1000000000-byte limit`, and its own crash trace gives the cost:
frame pointers 688 bytes apart per Border+VStack+Grid turn, i.e. **229
bytes per `MeasureChild` level**. The process therefore dies at roughly
4.4 million levels.

So the cap is **73x the deepest tree ever laid out here**, and trips
after about **117KB** of stack — four orders of magnitude below the
gigabyte that kills the process.

The conclusion worth keeping: the cap does not exist to keep the stack
alive. Anything under a million would do that. It exists to make a cycle
cost one frame instead of a gigabyte, while sitting far enough above
every real tree that it cannot reject a document that works today.

## Report, not panic

`LayoutFault` records the phase, the component, and the depth; the walk
stops and everything above it lays out and paints normally. Read it with
`Composer.LayoutFault()` or `App.LayoutFault()`. The Composer keeps the
**first** fault, not the latest: a cyclic tree trips Compose, then Focus,
then Measure, then Arrange every frame, so "latest" would report whichever
walk ran last, and the earliest walk is the closest report to the
structural mistake.

## Still unbounded

Four walks outside the root package recurse over `ChildComponents()` and
were left alone, because a cyclic tree no longer *becomes* a live
composition — but they walk the raw component graph, so they would still
hang on one handed to them directly:

- `components/adorn.go`: `findAdornmentLayer`, `inTree`, `visiblyReachable`
- `components/buttonbar.go`: `contains`
- `control/markup.go` (two sites) and `control/snapshot.go` — the MCP
  control plane. `snapshot.walk` already takes a depth, but only honours
  it when a caller asks for one.

That these are seven-plus-four sites of the same missing idea, rather
than one, is the finding. The framework has no single "walk the
children" seam; every walker calls `ChildComponents()` itself. A seam
would have made this one change instead of eleven, and is the right
follow-up.

## Related

- #216 — the issue this answers.
- #215 / `apps/wysiwyg/components/preview/mirror.go` — the per-element
  fix for the specific `<Preview>` instance, which explicitly notes that
  checking only the immediate parent is insufficient. The same reasoning
  is why the load-time check walks ancestry rather than the parent.
- #26 / PR #88 — container backgrounds and z-order, which is where
  `nodeOf`'s one-node-per-component assumption is load-bearing.
