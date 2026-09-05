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

- `scanAny` recognises `<ident>.Attrs["Name"]` for any element variable, where
  `attrOf` insists the receiver be `e`. `buildMenuBar` reads its children off
  `c` and `ic`, so the ordinary scan sees none of it. `attrOf`'s insistence is
  deliberate and stays — it is what stops an ordinary element claiming a
  neighbour's reads.
- The driver is two passes. A `ParsedBy` element needs another element's
  `Build`, which may be declared in a file the walk has not reached.

**The check is half of what an ordinary element gets, and the limit is worth
stating.** One `Build` parses several pseudo-elements — `buildMenuBar` reads
both `<Menu>`'s `Title` and `<MenuItem>`'s `Text` — and the attribute names in
it carry no record of which element they came off, so a read cannot be
attributed to one declaration. What survives is the direction that matters: an
attribute **nobody** reads is the silent-drop defect the package exists to
catch, and it still fails. What is given up is catching an attribute declared
on the wrong sibling — visible in the property grid, and covered by
`TestASelectedMenuOffersItsTitle`. Under-declaring stays loud the ordinary
way, because unknown attributes are rejected at load.

## `ElementSpec.Nested` — derived, not declared

The designer's palette filtered with `e.NonVisual || e.Name == "Tab"`. That
check was correct and unmaintainable in the same breath: the second nested
element would have been offered silently, producing markup the loader refuses,
with nothing anywhere going red.

The answer already exists in the vocabulary. A nested element is one some
*other* entry names in `Children.Only` under `ModeRestricted`, so `markNested`
derives it over the assembled catalog rather than over the registry — which
means a host's own restricted container contributes on the same terms as a
builtin. A hand-set flag would have been a second copy of a fact the registry
already carries, which is the drift `ElementDef`'s own doc comment gives as
the reason the behavioural axes are derived.

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
from a nil `Proto` — which is what that means in an `ElementDef`, since every
behavioural axis is read off `Proto`. An element carrying it can never be
paired with anything. `TestDeclaredElementsCarryAProtoOrSayWhyNot` already
requires a `Proto`-less element to say why, with `Opaque` or with `ParsedBy`,
so this cannot become true by accident.

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

**First child, not the last selection remembered per node.** Restoring where
you were is nicer to use and needs state that every edit, retype, delete and
undo must then invalidate — the same argument `selectionScope` makes, decided
the same way. `esc` climbs back out, so the round trip costs one keypress
either way.

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

The palette test asserts the **derivation** rather than three names: every
element some other entry restricts itself to is absent, and every restricted
container not itself excluded is present. A fourth nested element is covered
by the same loop.
