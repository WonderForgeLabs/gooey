# Menu item icons: two tiers that draw different things

**Issue:** [#400](https://github.com/WonderForgeLabs/gooey/issues/400) — "MenuBar: no way to give a MenuItem an icon"
**Date:** 2026-09-05
**Stacked on:** [#429](https://github.com/WonderForgeLabs/gooey/issues/429) / the pseudo-element record beside this one

## The finding that shaped the design

The obvious implementation is one attribute — `Icon`, an image — rendered
through `graphics.DrawHalfblock` wherever no graphics protocol exists.
That is what `<Image>`, `ColorPicker` and the button chrome all do, and it
is the framework's standing answer to "this draws a picture".

It does not work here, and the issue measured why rather than guessing.
`DrawHalfblock` scales an image to `cols × rows*2`. **A dropdown row is
one cell tall**, so the entire glyph is sampled twice vertically. The
reporting app rendered two clearly different icons and got the same
uniform `▀` back, `rgb(105,69,24)` against `rgb(83,55,19)` — two states,
one appearance, 22/255 apart in a single channel. Widening the gutter
does not help: it is still one `▀` per cell, so a wider box buys more
copies of the same block.

So the cell-plane tier cannot be a rendering of the picture. It has to be
a **glyph the author chooses**, which makes this the one place in the
framework where the two tiers draw *different things* rather than the
same thing at two fidelities.

## What was decided

**Two fields, `Icon image.Image` and `IconRune rune`**, not one `Src`
with a fallback. The names are deliberately not `Icon` / `IconFallback`:
neither is a degradation of the other, and a name saying otherwise would
invite exactly the halfblock implementation the measurement rules out.

**The gutter is reserved unconditionally** — three cells whenever *any*
item in the menu carries either field, whether or not a protocol exists,
and whether or not that particular item has an icon.

- *Per menu, not per item*, so every label in one dropdown starts at the
  same column. Same reason `Menu.lead()` already asks the menu about
  check boxes.
- *Regardless of protocol*, because the capability probe answers **after
  the first frame**. A dropdown one cell narrower without pixels would
  visibly reflow on a terminal that supports them. `buttonchrome.go`
  reserves its pill rows the same way and for the same reason.
- *Regardless of which field is set*, because otherwise the width depends
  on which fields the app filled in, and that is not a property of the
  menu either.

**Three cells: two for the picture, one separating it.** A one-cell box
is 10×20 pixels and SVG fits to the narrow side, so half of it is
letterboxing.

**`Icon` is literal-only where `<Image Src>` takes either form.** Same
`GoType`, same loader, same `fs.FS` — the difference is the *field*.
`components.Image.Src` is a `*prop.Property[image.Image]` and tracks;
`MenuItem.Icon` is a plain field read while painting and cannot. A handle
resolved at load would be sampled once and silently freeze, so it is
refused at load with an error saying that. `MenuItem.Text` already states
the identical contract for the identical reason.

**`IconRune` is exactly one glyph**, counted in runes. A two-cell emoji
is fine — the gutter's three cells are the *component's* arithmetic, and
the loader's question is how many glyphs. Two glyphs are refused at load
rather than clipped at paint, because half a glyph is not drawable.

## The second gap: the dropdown had no geometry

#400 names two gaps, and the icons are only the first. The second is that
**nothing outside the bar could say which menu is open or where its
dropdown landed.** `popup()`, `curP`, `popupRect` and `titleSpan` are all
unexported; the only seam was that `ChildComponents()` happens to return
the popup surface as its single element, which is an implementation
ordering rather than an API.

The reporter's workaround recovered the open index by walking the title
widths and matching the dropdown's left edge — reimplementing `titleSpan`
*and* `splitMnemonic`'s marker stripping in application code, against
arithmetic this package is free to change without notice.

`MenuBar.OpenIndex() int` and `MenuBar.DropdownBounds() gooey.Rect` close
it. There is no new state: both read what the bar already keeps, and both
are what an app would otherwise reconstruct.

- **`OpenIndex` returns -1 when closed** rather than leaving the caller to
  pair it with `IsOpen`. A zero index is a real answer, so a bar
  reporting 0 for "closed" and 0 for "the first menu is open" would need
  every caller to remember to ask twice.
- **`DropdownBounds` returns the zero Rect when closed** rather than the
  rect the menu *would* occupy. A caller placing pixels into a returned
  rect must not be handed a live-looking answer for a surface that is not
  on screen.

Their tests read the rect back **off the painted cells**, not from
`popupRect`. An accessor checked against the function behind it is
correct by construction and says nothing about where the dropdown went —
which is the only question an app placing an overlay is asking.

That readback helper promptly sprang this file's own trap a second time:
`strings.IndexAny` over `render.RowText` returns BYTE offsets, and every
box-drawing glyph is three bytes. It got the left edge right by luck (the
padding before it is ASCII) and the width wrong by twice the border
count, reporting a 9-wide box for a 7-wide dropdown and blaming the
accessor. It walks runes and accumulates `render.StringWidth` now. The
rune-vs-column rule applies to the READER as much as to the painter, and
neither time did anything but a deliberate wide fixture reveal it.

The issue's third option — a `DecorateItem` per-item draw hook — is not
taken. It is the most general of the three and has a genuinely nice
property (the host paints inside the popup's own paint node, so its
placements are owned by the surface), but with icons upstream the case
that motivated it is served, and a hook with no caller is a shape guessed
rather than derived.

## What was rejected

- **One `Src` with a halfblock fallback.** The measurement above.
- **`*prop.Property[image.Image]` on `MenuItem`.** It would make `Icon`
  bindable, at the cost of a handle per item to serve a case nobody has.
  An icon that changes can have the field widened when something needs
  it.
- **Reserving the gutter only when a protocol exists.** Reflow on probe.
- **Clipping a too-wide `IconRune` instead of refusing it.** Silent, and
  `render.ClipCols` correctly returns a column short rather than half a
  glyph — so the icon would simply vanish.
- **`clipCols` as a safety net inside the gutter.** It was written and
  then removed: the pad is exact in columns, a rune is at most two cells,
  and the gutter is three, so no input can take the branch. Every
  mutation of it passed. An unfalsifiable guard is not a cheap one — it
  is a claim the tests cannot check.

## The guard this broke, and what that says

`catalogen`'s vocabulary cross-check — landed one PR earlier, for #429 —
**went red against correct code** the first time it met this change, and
both halves of the bug were mine from that PR:

1. `passesElement` gated its descent on an argument literally named
   `"e"`. `menuItemIcon(ic, ctx, &it)` passes `ic`, so the walk never
   entered the helper and reported `<MenuItem>` as over-declaring the two
   attributes the code plainly reads.
2. Fixing that alone is not enough. The descent set `self` to the
   callee's own element parameter — right for a helper handed the host,
   wrong for one handed a child, whose reads then land in the host's
   *own* set and never reach the child's. The fixture's helper names its
   parameter `c`, matching the caller's variable, so this survives fixing
   only the first.

And the obvious repair for (1) is wrong in a third way: widening the same
gate inside `scan`, the walk for ordinary elements, files a child's
attribute as the host's own read. Applied to both walks it produced three
fresh false under-declarations in the shipped vocabulary — `<MenuBar>`
reads `Checked`, `<Tabs>` reads `Header`, `<Companion>` reads `Value`,
every one a child's attribute attributed to its host. The widening is
only sound inside `scanChildAttrs`, which splits by receiver and so can
answer the question the widening opens.

The generalisable part: **a guard against silent over-declaration fails
LOUD, and that is the whole point of it.** All three of these were red
tests naming code that was correct, which is the direction this check was
built to fail in, and each one was a real hole that the shipped
vocabulary simply did not happen to exercise. The first feature to use a
helper on a child element found all three on its first day.

## How the claims here are checked

| Claim | Test | Mutation that fires it |
|---|---|---|
| The gutter measures the same in both tiers | `TestTheIconGutterIsReservedInBothTiers` | reserve only when `pixel` |
| …and reserves something at all | same, second half | drop `iconLead()` from `popupRect` |
| The cell tier draws the rune, not a halfblock | `TestAnIconItemDrawsItsRuneOnTheCellPlane` | return blanks instead of the glyph |
| The pixel tier places the image, one row tall, in the gutter | `TestAnIconItemPlacesItsImageWhenPixelsExist` | drop the `f.Place` |
| Icon and check are different columns | `TestAnIconAndACheckAreDifferentColumns` | drop `checkBox` from `lead` |
| The gutter is padded in COLUMNS | `TestAWideIconRuneDoesNotOverrunItsGutter` | pad by `len([]rune(g))` |
| A literal `Icon` loads from the page's FS | `TestAMenuItemIconLoadsFromThePageFS` | drop `it.Icon = img` |
| A missing asset is a load error naming the path | `TestAMissingIconAssetIsALoadError` | — |
| A bound `Icon` is refused, saying why | `TestABoundMenuItemIconIsRefused` | remove the `bindRe` guard |
| `IconRune` is one glyph | `TestAMenuItemIconRuneIsExactlyOneRune` | accept any length |
| …counted in runes, not cells | `TestAWideIconRuneIsAccepted` | validate with `StringWidth` |
| The catalog agrees with the loader about binding | `TestTheIconAttributesAreDeclaredOnMenuItem` | declare `BindsEither` |
| The designer's grid offers both | `TestASelectedMenuItemOffersItsAttributes` | — |
| `OpenIndex` is -1 closed and tracks the open menu | `TestTheOpenIndexIsReadableFromOutside` | drop the `IsOpen` guard |
| `DropdownBounds` is zero closed, and is where it painted | `TestTheDropdownBoundsAreWhereItPainted` | return `popupRect()` unguarded |
| …and follows the open title | `TestTheReportedBoundsMoveWithTheOpenMenu` | always measure menu 0 |
| An emptied menu list does not crash the accessors | `TestTheAccessorsSurviveAMenuListReplacedWhileOpen` | guard on `IsOpen` alone |
| An open menu with no items reports nothing | `TestAnOpenMenuWithNoItemsReportsNothing` | guard on `IsOpen` alone |
| Reading `OpenIndex` while painting subscribes | `TestReadingTheOpenIndexWhilePaintingIsADependency` | — (damage-count pin) |
| A ctx-first helper still reads the HOST's element | `TestAHelperTakingCtxFirstStillReadsTheHostsElement` | resolve by first bare ident |
| One helper in two roles is scanned in both | `TestOneHelperUsedInBothRolesIsScannedInBoth` | key `seen` on the name alone |
| A helper handed a child reads the CHILD's attributes | `TestAHelperHandedAChildReadsTheChildsAttributes` | revert either half of the catalogen fix |
| …and the widening stays out of `scan` | `TestWideningTheGateStaysOutOfTheUndifferentiatedWalk` | widen `passesElement` too |

`TestAWideIconRuneDoesNotOverrunItsGutter` is worth one more line, because
it first failed on **its own** bug rather than the code's: it compared
`strings.Index` results, which are BYTE offsets, and `📁` is four bytes to
one cell's worth of arithmetic. The two rows were identically laid out and
the test said otherwise. It measures the prefix with `render.StringWidth`
now — the same function the gutter is padded with, which is the point.
This file's own trap, sprung on itself.

## What the review found, and the shape of it

Eleven findings, every one verified against HEAD before being acted on.
Ten were real. The exception is a sub-claim inside finding 1 — that
`OpenIndex()` reports 0 "for a bar with no menus at all" — which does not
reproduce, because `Open` returns early when `len(m.Menus) == 0` and the
popup never opens. The finding itself is real by the path it also names:
a menu list replaced *while* a dropdown is open.

Two of them are the interesting ones, and they are the same mistake.

**The new accessors were the only paths in `menu.go` that did not guard
`Menus`.** It is an exported field, so an app rebuilding its menu list
while a dropdown is open is legal — `Arrange` guarded, `drawDropdown`
guarded, and `DropdownBounds` **panicked** with index out of range.
Worse in kind than a wrong answer: a public accessor crashing the process
on a state the component itself handles, introduced by the PR that
exported it. Its sibling was quieter and the same error — asking only
`IsOpen` handed back a live-looking rect for a menu with no items, which
`Arrange` declines to show, *which is precisely what that accessor's own
doc comment forbids*. Both now ask one predicate, `showing()`, which is
the expression `Arrange` had inline; two accessors over one state cannot
disagree with each other or with what was arranged.

The lesson is not "add a guard". It is that **exporting an internal
calculation exports its preconditions**, and `popupRect`'s were written
down nowhere — they lived as an inline expression at one call site and an
early return at another. The accessor inherited neither.

**The `catalogen` walk got the element wrong for the idiom `markup`
itself documents.** `elementArg` answers "the first bare identifier
argument", which is right for `menuItemIcon(ic, ctx, &it)` and wrong for
`markup.Attr[T](ctx, e, "Value")` — the public element-author idiom, with
`ctx` first. It answers `"ctx"`, decides the helper was handed something
other than the host, and files the host's own reads against a child: the
same false over-declaration this PR just fixed, arriving through the
opposite door. Latent only because no `ElementDef.Build` calls `Attr`
yet. Resolved positionally from the callee's declared `Element` parameter
now, falling back to the heuristic only for functions outside the
package. That also retired a `bare` flag which did not mean what it said
— `nil`, `true` and `false` are all `*ast.Ident`.

Alongside it, `seen` keyed on the function name alone, so one helper
called in both roles was scanned in whichever came first and the other
role's reads vanished. Keyed on name-and-role now.

### Two fixes needed a fixture built specifically to falsify them

Both were caught by mutation, not by review:

- The `seen` fix passed its first mutation. The fixture read the
  attribute as a **string argument**, and the literal branch picks those
  up whether or not the descent runs — so it could not see a skipped
  descent at all. It reaches the bug only with a helper reading a
  *hardcoded* index, called in both roles.
- `TestAToastIsNotHiddenByAnOpenMenu` in the PR above this one had the
  same disease geometrically.

The pattern is worth naming: **a test for a fix inside a walk must
exercise the branch the fix is in**, and "the attribute shows up in the
findings" is satisfied by any of several branches.

### The rest

Hoisted `iconLead()` out of `popupRect`'s per-item loop — it walks every
item, so calling it per item made `popupRect` O(n²) on the layout path
(`lead` one line above was already hoisted for exactly that reason).
Dropped an unused `w` parameter from `paintedDropdown`. Added the
damage-count pin the accessors' paint-dependency claim needed, since
CLAUDE.md is explicit that nothing else pins a repaint claim — and while
writing it, corrected the claim itself: `popupRect` reads `m.Bounds()`,
and `Base.Bounds` is a **plain field**, so the open-index half subscribes
and the geometry half does not. A decorator must depend on whatever
drives the bar's layout too, and the doc now says so instead of implying
otherwise. Fixed `docs/learn/07-app-chrome.md`, which still taught
"declare the bar last, document order is the entire mechanism" — the rule
#430 specifically disproved.

## Still open

- **Submenus and context menus** remain [#104](https://github.com/WonderForgeLabs/gooey/issues/104); icons will apply unchanged when they land.
- **A `DecorateItem` hook**, if a second decoration case turns up that
  the icon fields do not serve. See above for why it is not guessed now.
- **A designer gesture to ADD a menu entry.** #429 gave `<Menu>` and
  `<MenuItem>` a declared surface and a selection route, and this gives
  them an icon, but there is still no way to create one from the palette
  — an element legal only inside a named parent is not placeable on its
  own, and nothing yet offers "add a child of the selected element".
- **`ChildComponents()` has no single seam**, which is why `buildMenuBar`
  parses its own children by hand at all —
  [#375](https://github.com/WonderForgeLabs/gooey/issues/375).
