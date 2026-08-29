# Scrolling: a pane-local viewport over a shared scroll model

Decision taken 2026-08-23, closing
[#67](https://github.com/WonderForgeLabs/gooey/issues/67) ("reader: scroll
long articles instead of truncating").

The issue required this document to exist by requiring an answer to one
question: is scrolling a **pane-local** feature of the reader, or a
**framework-level** capability? It also framed the stakes correctly — a
framework answer that subsumed what `ItemsView` already does would be far
more valuable than a third hand-rolled window.

**The answer is: pane-local viewport, framework-level scroll model.** The
reader's article pane owns its own line layout and its own window onto it;
the offset, the clamping and the gesture vocabulary live in
`components.Scroller`, which `ItemsView` now uses too. This document
records *why* the more ambitious answer is not available, because both
reasons are load-bearing constraints on gooey generally and neither is
written down anywhere else.

## What already existed

"gooey has no scrolling anywhere" is true of the vocabulary and false of
the machinery. `components.ItemsView` is a viewport over content larger
than the pane, and a good one: `window()` (`components/itemsview.go`)
computes which slice of the collection is worth building, `sync()` reuses
the rows that are still visible so scrolling by one line repaints only
what moved, and a row outside the realized window has no bounds at all.
It has a dedicated scroll mode — no `Selected` binding, a `Scroll` offset
— with pager keys and a velocity-tiered wheel. `cmd/logview` is its
consumer.

So the honest question was never "how do we build scrolling". It was
"why can the reader pane not simply be an `ItemsView`, and if it cannot,
what may still be shared".

## Why a general scrolling container is not available

A `<ScrollViewer>` — arbitrary content, arranged at a negative offset,
clipped to a viewport — is the answer most toolkits give, and it cannot
be built here today.

**gooey has no clipping.** `Frame` (`component.go:147`) carries a
`*render.Buffer` and nothing else; `render.Buffer.Set` (`render/cell.go`)
bounds-checks against *the buffer*, meaning the whole screen. There is no
clip rectangle, no clip stack, and no per-node intersection anywhere in
the paint path. Every component is simply trusted to paint inside the
bounds it was arranged into.

That is not an incidental gap. It means a child arranged partly outside a
viewport writes cells belonging to whatever is next to the viewport, and
those cells are *clean* — their owners' paint nodes are not dirty, so
nothing repaints over the damage and it persists until something unrelated
happens to invalidate the victim. A scrolling container without clipping
would not render imperfectly; it would corrupt its neighbours.

Adding clipping is a real change and a separable one: a clip stack on
`Frame`, every `f.Cells` write routed through it, and — the part that is
easy to miss — the damage rectangles and the three pre-clear fills in
`Composer.build` (`composer.go:298-335`) intersected against it too, or the
pre-clear itself becomes the out-of-bounds write. That is an epic, not a
step in this issue.

**`ItemsView` avoids the problem by construction**, and this is the
insight worth keeping: it achieves a viewport *without* clipping because
it virtualizes. It never arranges a child outside its own bounds — it
realizes only the rows that fit and clamps the last one's height to the
space remaining. Virtualization is not merely an optimization in gooey; in
the absence of clipping it is the *only* way to have a viewport at all.

## Why the article pane cannot be an ItemsView

Given that, the natural move is to express the article as items and let
`ItemsView` do the windowing. Two independent properties block it.

**`ItemsView`'s window assumes a uniform row height.** `v.rowH` is a
single `int`; `window()` computes `visible = b.H / v.rowH`; `place()`
advances `y` by `v.rowH` for every row; and `measuredRowH()` derives that
height from `v.rows[0]` alone. A document is precisely the case where the
natural unit — a paragraph — has a height that varies per item. Wrapped
prose can only be fed to `ItemsView` pre-flattened into one-line rows.

**Nothing can tell a projection how wide the pane is.** Flattening prose
to lines requires the wrap width, and an `ItemSource` is built inside a
computed that has no access to any component's bounds. There is no
mechanism in gooey for a component to publish its own arranged size into
the property graph — that was checked, not assumed. So the line count of
an article is a function of a number the source cannot see, while
`window()` needs `src.Len()` before layout has produced that number.

Either constraint alone would be surmountable with a framework feature
(variable row heights; a size-publishing seam). Both are larger than this
issue and neither is needed for the reader to work. `TestArticleRewrapsWithPaneWidth`
pins the underlying fact — the same article is a different number of lines
at a different width — so the reason this decision was taken does not
decay into prose.

## What is shared, and what is not

Refusing the general container does not license a third hand-rolled
window. The concept that *is* genuinely shared has been named:

`components.Scroller` (`components/scroll.go`) owns the **scroll model** —
where the offset lives, how it is clamped against the content, and what a
gesture does to it. `ItemsView` and the reader pane both use it.
`ItemsView` keeps `Scroll` and `Now` as its public spelling and hands them
down; nothing about its API changed and no damage count moved.

Two pieces of it are worth the type on their own:

- **the compared Set.** `prop.Set` does not compare values, so an
  uncompared offset write invalidates every dependent and repaints on
  every key repeat while nothing moves. `Scroller.By` is the compared Set,
  and `TestScrollClampIsDamageFree` (which predates this change),
  `TestScrollerAtTheEndIsDamageFree` and
  `TestArticleAtTheBottomIsDamageFree` all fail together when the compare
  is removed — the extraction is real, not nominal.
- **the wheel velocity tiers**, entered by run length rather than by one
  fast interval. A second copy would have drifted, and one pane would have
  felt different from the next under the same gesture.

What deliberately stays with the host is the **anchor**. A log pane
anchors to the tail so offset 0 follows appends; a document anchors to the
head so offset 0 is the first line. The two therefore move the offset in
*opposite* directions for the same key, which is exactly the kind of
difference that should be visible in the host's own key switch rather than
hidden behind a boolean on shared code.

The **viewport model** — what a visible unit is, how many fit, how content
reaches the screen — is not shared, because as shown above it genuinely
differs.

### How small the second viewport actually is

It is worth being concrete about what "a second viewport" cost here,
because the phrase suggests a second copy of `ItemsView` and that is not
what was written.

`articleBody` is a **leaf**. It has no `ChildComponents`, so there is
nothing to realize: no per-item subtree, no row reuse, no keying by index,
no `accepts`/`update` re-projection, no structural-change hook. Those are
the substance of `ItemsView`, and none of them is duplicated. The entire
windowing is the slice in `Render`:

```go
off := w.scroll.At(len(lines), b.H)
for i := 0; i < b.H && off+i < len(lines); i++ { … }
```

Being a leaf is also why the damage number is what it is. Scrolling the
article repaints **exactly one** component, where a list showing the same
content repaints one node per row that moved. So the pane-local answer is
not merely acceptable here — for text it produces a strictly better
repaint count than routing the same content through `ItemsView` would.

The corollary, and the reason this is not an argument for leaves
generally: a leaf can only do this because it paints its own cells
directly. The moment content needs to be *components* — selectable rows,
per-item focus, embedded widgets — you are back to realization, and back
to `ItemsView` and its uniform row height.

## Consequences

- The reader pane scrolls with `j`/`k`/arrows, pgup/pgdn, home/end and the
  wheel. Scrolling repaints exactly one component
  (`TestArticleScrollRepaintsOnlyTheReaderPane`), which is the whole
  reader pane and nothing else.

  That claim is pinned on the **effect, not the mechanism**, and it takes
  two independent assertions to do it. The count is `Composer.Frame()`'s
  second return, incremented inside each paint node *after* `Render` has
  actually run — so it counts paints performed, not handles re-pointed.
  The cell assertions read the frame buffer back and compare against the
  article's own lines. Neither subsumes the other, and the mutation that
  proves it is the one replacing `lines[off+i]` with `lines[i]`: the
  damage count stays 1 while the wrong text is drawn, and only the cell
  assertion notices.
- The offset is a page property, not pane state, so opening a story can
  rewind it. A pane-private offset would open every article at the
  previous one's position.
- `apps/gitui` has the same defect for long diffs, and its own source
  comment already names `ItemsView` as the fix. A diff *is* a list of
  uniform-height lines that need no wrapping, so unlike the article it can
  become an `ItemsView` — but it needs the head anchor, which scroll mode
  does not have today, and `Scroll` is not exposed in markup at all
  (`markup/itemsview.go` binds `Items`, `Selected`, `Activate`,
  `SelectionChanged` and `Focusable`, and nothing else). Those two are the
  natural next increment and are deliberately not in this change.
- Nothing here makes gooey rune-width aware. `wrap` in `cmd/reader` was
  measuring in **bytes**, which wrapped every non-ASCII paragraph early;
  it now measures in runes, which is correct for this repo's model and
  still not display width. A two-cell rune counts as one everywhere in
  gooey, so it overruns its column here exactly as it does elsewhere, and
  cell assertions cannot see it.

  > **Superseded by [#358](https://github.com/WonderForgeLabs/gooey/issues/358).**
  > The paragraph above was true when written and is not now: `render.SetString`
  > advances by display width, `Text.Measure` and this `wrap` count columns, and
  > `render.Displaced` is the assertion that *can* see it. Left standing rather
  > than rewritten, because a decision record should say what was true when the
  > decision was taken.

## Rejected alternatives

- **A general `<ScrollViewer>`.** Needs clipping. See above; it is an
  epic, and doing it badly corrupts neighbouring panes silently.
- **Head-anchored mode on `ItemsView`, article as items.** Blocked by
  uniform row height *and* by the absent width seam. The anchor half of it
  is still worth doing for `gitui`.
- **A bespoke scroll in the reader with nothing shared.** This is the
  option the issue's framing warns against, and the wheel-velocity logic
  is the concrete cost: a dozen lines of timing state copied into a second
  place, drifting from the first.
- **A scroll-position indicator in the pane.** Deliberately out of scope.
  Drawn into the `Border` title it would repaint a second component per
  scroll; drawn inside the pane it needs a reserved column, since a
  floating marker erases whatever shares its cells.
