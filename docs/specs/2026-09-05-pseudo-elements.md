# Pseudo-elements in the vocabulary

**Issue:** [#429](https://github.com/WonderForgeLabs/gooey/issues/429) — "in
wysiwyg app, when i have a menu bar on the design, why don't i see the child
properties to set content for menu item?"

## What was actually wrong

The report reads as one missing panel. It is three defects that produce the
same screen, and fixing any one of them alone leaves it unchanged.

1. **`<Menu>` and `<MenuItem>` were not declared.** They existed only as two
   strings inside `defMenuBar.Children.Only`, so nothing in the vocabulary
   knew a `<MenuItem>` has a `Text`. `AttrsFor` returned an empty set. There
   was nothing for a property grid to show.
2. **Nothing could select one.** Selection runs through `FocusManager.HitTest`,
   which answers with a *component*; `buildMenuBar` consumes its children as
   data and the bar draws the items itself, so no component under the cursor
   belongs to a `<Menu>` or a `<MenuItem>`. The double-click drill cannot reach
   what the pointer cannot hit.
3. **The property grid resolved the selection in the palette.**
   `editor.target()` scanned `ed.palette`, which is the catalog minus what may
   not be *placed*. That is a different question from what may be *set*, and
   the two came apart the moment a nested element became selectable — so the
   fix for (1) would have been invisible even after (2).

Note that (3) is a defect the first two *created*. Declaring the elements and
then excluding them from the palette reproduces the reported symptom through
the machinery built to remove it.

## `ElementDef.ParsedBy`

The vocabulary had no way to say **"declared here, read there"**. The only
shape available was `<Tab>`'s: a nil `Proto`, `Known: false`, and an `Opaque`
reason. That is honest for `<Tab>`, whose attributes really are whatever
`<Tabs>` cares to read and whose content is an arbitrary subtree. It is wrong
for `<MenuItem>`, whose surface is exactly the five attributes `buildMenuBar`
reads and rejects anything else about.

`ParsedBy` names the element whose `Build` consumes this one's attributes.
Both `<Menu>` and `<MenuItem>` are `ParsedBy: "MenuBar"` — the element that
*parses*, not the parent, because `buildMenuBar` walks both levels.

**It is checked, not trusted**, and that is what makes it a field rather than
a comment. `markup/internal/catalogen` resolves an element's `Build` through
this name, so a wrong one — or a right one that stops reading an attribute —
fails `TestDeclaredVocabularyMatchesTheCode` exactly as an ordinary element's
own drift does.

Two things had to change in the checker for that to work:

- `scanChildAttrs` walks the host's `Build` and **splits** its attribute reads
  in two: the ones taken off the host's own element, and the ones taken off
  some other element variable. For a `ParsedBy` host those other variables
  *are* its pseudo-children — `c` and `ic` in `buildMenuBar` — so the second
  set is the pseudo-elements' surface. `attrOf` insists the receiver be `e`
  and stays that way: it is what stops an ordinary element claiming a
  neighbour's reads.
- The driver is two passes. A `ParsedBy` element needs another element's
  `Build`, which may be declared in a file the walk has not reached.

**The split is the whole point, and collapsing it is a silent hole.** A first
draft counted every read in the body, so a pseudo-element could declare an
attribute only the **host** reads off *itself* and pass: `<MenuItem>`
declaring `Style` — which `buildMenuBar` reads off the bar — was green, while
`<MenuItem Style="…">` loaded clean and was dropped on the floor. The host's
own element is identified by the parameter declared `Element` in the body
being walked rather than assumed to be `e`, because a hardcode would
mis-split the first host that names its parameter anything else.

`scanChildAttrs` also applies `scan`'s `generic` deny-list and
`passesElement`. Without them it follows `build` / `buildChildren` /
`checkAttrs` into the general builder machinery and collects literals
belonging to no element — and because `checkPseudoPool` used to compute its
two sets with *different* walks and subtract them, those literals were
reported as attributes nobody wrote. One walk, two outputs, comparable by
construction.

Three more followed from the same question — *which element does this read
belong to* — and each was a way of answering it wrongly:

- **The `universal` skip does not apply to a pseudo-element.** Skipping
  `Margin`/`Width`/`Visibility` is right for an ordinary element, whose
  `applyLayout` consumes them outside its `Build`. A pseudo-element has no
  `applyLayout` — a nil `Proto` makes `TakesLayout` false, so `vocabulary()`
  never adds the universal set — but `checkAttrs` allows anything in
  `spec.Attrs`. So *declaring* `Margin` on `<MenuItem>` made it settable,
  unread and silently dropped, with the whole suite green and
  `<MenuItem Text="Open" Margin="3"/>` building with `err == nil`. The
  silent-drop defect again, one set of names over.
- **The helper idiom is a read.** `scan` recognises `Bound(e, ctx, "Text")`
  and `optDuration(e, "Tick")`; the child walk saw only `x.Attrs["…"]`. That
  gap is loud in the wrong direction — the declaration is real and the read is
  invisible, so it produces a **false over-declaration**, a red test asserting
  the opposite of what the code does.
- **A pseudo-element's own `Build` is scanned too.** `Check`'s `ParsedBy`
  branch `continue`s before the ordinary two-direction scan, so a `ParsedBy`
  def whose `Build` reads anything had that read unattributed *and* the
  declaration serving it reported over-declared. Legal shape; today's two only
  return "only valid inside…" errors.

`markup/internal/catalogen` had **no tests at all**, which is how all five
holes got there: none is reachable from the real vocabulary, so none could be
pinned against it. `testdata/src` and `testdata/hostread` are fixture packages
(parsed with `go/ast`, never type-checked). They are near-twins, not one line
apart: `diff -u` shows the `package` clause, a `{Name: "Style"}` declaration
read off the HOST, and a `_ = c.Attrs["Margin"]` universal read off a pseudo
child. Each exists to exercise a case the other does not.

That they must stay in step is the thing to watch, because **neither is
compiled** — `testdata/` is skipped by `go build` and `go vet`, so nothing
reports a fixture rotting. They have already diverged in the dimension
`go/ast` cannot see: `hostread` dropped two of `src`'s explanatory comment
blocks while keeping the code they explain. Restored, and worth restoring
again if it recurs — the comments are the only record of which case each
line is for.

**The check splits by what is decidable, and only one thing is actually lost.**
One `Build` parses several pseudo-elements — `buildMenuBar` reads both
`<Menu>`'s `Title` and `<MenuItem>`'s `Text` — and the attribute names in it
carry no record of which element they came off. So:

- **Over-declared** is decidable per element (`checkPseudo`): an attribute the
  declaration claims and the host never reads.
- **Under-declared** is decidable per *family* (`checkPseudoPool`): an
  attribute the host reads off a child that **no** element naming that host
  declares. The finding is addressed to the host, because that is the honest
  address — the fix is to add it to whichever child it belongs on, and this
  cannot say which.
- **Declared on the wrong sibling** is what is genuinely lost. It is at least
  visible in the property grid, and `TestASelectedMenuOffersItsTitle` covers
  the case that motivated it.

### The claim that was false, and what it cost

An earlier draft of this record and of the code gave up the under-declared
direction outright, on the grounds that *"under-declaring stays loud the
ordinary way, because unknown attributes are rejected at load."*

**That was false for exactly the elements `ParsedBy` covers.** `checkAttrs`
runs inside `build()`, and a pseudo-element never reaches `build()` — that is
what "consumed as data" means. `<Gooey><MenuBar><Menu Bogus="x"><MenuItem
Text="Open" Frobnicate="yes"/></Menu></MenuBar></Gooey>` loaded clean. So
`<Menu>` and `<MenuItem>` were, briefly, the only elements in the vocabulary
with **neither** direction of the drift check guarded — the silent-drop defect
reachable through the one corner of the vocabulary the loader never looked at.

Both halves of the repair shipped, and they are deliberately redundant:

- `buildMenuBar` calls `checkAttrs` on its `<Menu>` and `<MenuItem>` children
  (before the `Separator` short-circuit, so a typo on a separator is reported
  rather than skipped past). This makes the sentence true.
- `checkPseudoPool` exists anyway, because a guard resting on a claim about
  somebody else's code is one refactor from being wrong again.

`TestAnUnknownMenuAttributeIsALoadError` pins the first;
`TestTheMenuVocabularyStillLoadsWhatItDeclares` is its other half, so a check
that rejected everything could not pass.

## `ElementSpec.Nested` — derived, not declared

The designer's palette filtered with `e.NonVisual || e.Name == "Tab"`. That
check was correct and unmaintainable in the same breath: the second nested
element would have been offered silently, producing markup the loader refuses,
with nothing anywhere going red.

The answer already exists in the vocabulary, and `markNested` derives it over
the assembled catalog rather than over the registry — so a host's own
restricted container contributes on the same terms as a builtin. A hand-set
flag would have been a second copy of a fact the registry already carries,
which is the drift `ElementDef`'s own doc comment gives as the reason the
behavioural axes are derived.

**It takes two conjuncts, and the first draft had only the second.** Reading
`Children.Only` alone is the *converse* of the property wanted, and the two
are not equivalent: `ModeRestricted` says "this container accepts only these",
`Nested` says "this element is accepted only inside a container that names
it". A host declaring a toolbar with `Only: []string{"Button"}` — an entirely
reasonable container — would mark `<Button>` nested and vanish it from every
palette, silently. Restricting a container says nothing about where its child
may otherwise go.

`Pseudo` closes it: an element that builds no component cannot stand anywhere,
because there is nothing for it to be. So `Nested = Pseudo && named-in-an-Only-list`.

Nothing shipped can fail that, because every restricted container in the box
names pseudo-children — so
`TestARestrictedContainerDoesNotHideARealElement` constructs the case with a
host-declared `<HostBar>` restricted to `<Button>`. A guard that cannot fail
is not a guard.

**Assigned, not or-ed.** `markNested` runs twice over overlapping data —
`BuiltinElements` marks, then `Catalog` re-runs over a list seeded from it —
so a conditional set would *accumulate* rather than derive. `TestMarkNestedIsIdempotent`
pins it directly, because the shipped catalog cannot tell the two apart.

`<Menu>` is both: a restricted container *and* a nested element. That is
exactly why "may this be placed on its own?" had to become a question about
the element rather than about its role.

## `ElementSpec.Pseudo` — and the mis-pairing it found

Looking for the selection route turned up a live bug in `mapNodes`, in the
function whose doc comment says it exists to prevent it.

`pairsAgree` checks a positional correspondence between document children and
built components using `Context.Named`, and it acknowledged a gap: "a node
with no Name and no named descendants cannot be checked either way, and then
the count is all there is." A `<MenuBar>` holding **one** `<Menu>` hands back
exactly one child — its dropdown surface — so the counts agreed and the
`<Menu>` node was mapped onto the popup. Two menus and the counts disagree and
the descent stops correctly; one menu and it does not. A correctness guard
that depends on how many children the user happened to write is not one.

`Pseudo` is the declared form of "builds no component of its own", derived
from a nil `Proto` **and a stated reason** — `Opaque`, or a `ParsedBy` naming
the reader. An element carrying it can never be paired with anything.

The second conjunct is not belt-and-braces.
`TestDeclaredElementsCarryAProtoOrSayWhyNot` forces a `Proto`-less element to
say why, but it ranges over `definedElements()` — the *builtin* registry — and
never sees anything a host puts in `Context.Elements`. A host def with a real
`Build` and no `Proto` is legal and nothing rejects it, so deriving from the
nil alone would make it silently pseudo: dropped from any palette filtering
`Nested`, and unselectable-through in the designer, with no error anywhere.
Nothing in this repo trips it, which is why
`TestAHostElementWithNoProtoIsNotPseudo` constructs the case.

## The gesture: `alt+enter`, on the root

`esc` selects the parent (WPF's spelling). Its inverse selects the first child,
and for a pseudo-element it is the **only** route in.

Two shapes were tried and rejected against the running page, and both would
have passed against a direct call to `ed.selectChild`:

- **Bare `enter` on the root** never fires. The bubble offers a node's own
  handlers before its ancestors' `KeyBinding`s, and `enter` is the toolbox's
  `Activate`, the inspector's row editor and every caret editor's commit —
  there is no focus position in the app where a root `enter` would arrive.
- **`enter` scoped to the design pane** never fires either. `*preview.Pane` is
  not in the focus order at all: it is a `gooey.Frozen` host, which is why
  presses are *retargeted* to it rather than routed through it. Nothing
  focused passes through that subtree.

`alt+enter` on the root is global like `esc`. The `alt` prefix is safe here
where `alt+<letter>` would not be — a root binding is offered the key before
the menu mnemonics are, and `enter` is not a letter any menu title can carry.

**One degradation is worth naming**, because this is the pair where it is not
harmless. `alt+X` is ESC followed by X in one read; split across two reads,
`decodeEsc` resolves the lone ESC to `KeyEsc` once idle, so `alt+enter`
becomes `esc` plus an unbound `enter` — which fires `SelectParent`, one level
the **opposite** way. Every `alt+*` binding in the app shares the mechanism
and this is not new, but the others degrade into a no-op and this one degrades
into the inverse action. In practice the two bytes arrive together.

**First child, not the last selection remembered per node.** Restoring where
you were is nicer to use and needs state that every edit, retype, delete and
undo must then invalidate — the same argument `selectionScope` makes, decided
the same way. `esc` climbs back out, so the round trip costs one keypress
either way.

## The cost that no correctness test can see

`pairAgrees` answers "is this element pseudo?" once per document node, and the
obvious way to answer it — `ed.specOf` — ranges over `Context.Catalog()`.
`Catalog()` is **not a getter**: it re-derives every builtin spec with fresh
`Attrs` copies, re-runs `markNested`, and sorts — ~87 allocations per call,
and a glob-and-parse of every include file when a context has them. `mapNodes`
runs from `rebuild()`, which fires on every drag frame, every `alt+k` and
every property edit.

Measured on a 120-node document: **17,585 allocations per rebuild** with the
per-node lookup, **6,630** with it hoisted. Every correctness test passes
either way.

So the set is derived once, in `loadPalette`, beside the palette — they
answer different questions about one catalog read, and must not come from
different reads of it. `TestARebuildDoesNotRebuildTheCatalogPerNode` bounds
the allocations, because nothing else would notice.

**And there is a second path, which that ceiling is blind to.** `target()`
resolving the selection in the catalog — the fix for defect (3) above — put a
`Catalog()` call on `attrRows()`. That is O(1) per call, so a rebuild-ceiling
test cannot see it; but `attrRows` is evaluated by `ed.attrItems`, a
`prop.NewComputed` bound to `<ItemsView Items="{{.AttrItems}}">`, so it runs
**inside that ItemsView's paint node** on every repaint after an `ed.rev`
bump, and again per keypress from `selectedRow()` and per commit from
`valueEditor.Write`. With `Includes` set it would be `fs.Glob` plus a
`Declarations` parse of every file, inside a `Render`, which has nowhere to
put an error.

Measured: `attrRows()` costs **91** allocations reading the map and **180**
rebuilding the catalog. `ed.specs` is that catalog by name, taken in
`loadPalette` from the same read as the palette and the pseudo set, and
`specOf` reads it too — it was paying the rebuild three times inside one add
gesture. `TestAttrRowsDoesNotRebuildTheCatalog` bounds it at 135, from both
measurements rather than a guess.

## What is checked

Every clause below was verified by reverting it alone and watching the named
test go red.

| Change | Test |
|---|---|
| `<Menu>`/`<MenuItem>` declared | `TestDeclaredVocabularyMatchesTheCode` (through `ParsedBy`) |
| `ParsedBy` resolves to a real `Build` | same, via `checkPseudo`'s `Note` |
| palette excludes nested elements | `TestTheMenuVocabularyIsNotOfferedInThePalette` |
| `target()` reads the catalog | `TestASelectedMenuItemOffersItsAttributes`, `TestASelectedMenuOffersItsTitle` |
| `alt+enter` descends | `TestEnterDescendsIntoTheMenuVocabulary` |
| `Pseudo` blocks the mis-pairing | `TestThePointerCannotReachAMenuItem` |
| `checkAttrs` on `<Menu>` / `<MenuItem>` / a separator item | `TestAnUnknownMenuAttributeIsALoadError` |
| an undeclared read off a child element | `TestDeclaredVocabularyMatchesTheCode` (via `checkPseudoPool`) |
| `markNested` drops the `Pseudo` conjunct | `TestARestrictedContainerDoesNotHideARealElement` |
| `markNested` sets instead of assigning | `TestMarkNestedIsIdempotent` |
| `pairAgrees` asks the catalog per node | `TestARebuildDoesNotRebuildTheCatalogPerNode` |
| the host/child read split collapses | `TestAHostsOwnReadIsNotAChildsAttribute` |
| the child walk ignores the `generic` deny-list | `TestTheDenyListAppliesToTheChildWalk` |
| `Pseudo` derived from a nil `Proto` alone | `TestAHostElementWithNoProtoIsNotPseudo` |
| the `universal` skip applied to a pseudo-element | `TestAPseudoElementGetsNoUniversalPass` (and `Margin` on `<MenuItem>` in the real vocabulary) |
| the helper idiom not counted as a read | `TestTheHelperIdiomIsAChildRead` |
| a pseudo-element's own `Build` not scanned | `TestAPseudoElementsOwnBuildIsScanned` |
| `specOf` rebuilds the catalog | `TestAttrRowsDoesNotRebuildTheCatalog` |

The palette test asserts the **derivation** rather than three names: every
element some other entry restricts itself to is absent, and every restricted
container not itself excluded is present. A fourth nested element is covered
by the same loop.
