# Frozen, as a set

Widens `Frozen`'s answer from a bool to `gooey.Allow`, a set of
interaction categories; ships `<Frozen>` as a markup container and
`handlers/sets` as the pack that composes the set. Extends
`docs/specs/2026-08-14-frozen-observed.md`, which made the bool
*observed*; this makes it *expressive*.

## The gap

"Renders but does not act" is all-or-nothing, and the thing freezing was
built for cannot use all-or-nothing.

A design surface freezes its canvas so that clicking a `<Button>` selects
it instead of pressing it. But then **nothing** inside acts, so the
selection chrome — resize handles, a drag affordance, the highlight that
tracks the pointer — cannot be part of the tree it decorates. It has to
be drawn by the editor, hit-tested by the editor, and kept in sync with a
layout the editor does not own.

`docs/specs/2026-08-11-design-surface.md` reached the same conclusion from
the other end and worked around it: the frozen host is one node at the
root of the surface, and per-element identity lives in a
`map[gooey.Component]*node` rather than in the tree. That works, and it is
still the right shape for identity. It does not help with *behaviour* —
"this element, and only this one, still takes the pointer" has no
spelling at all.

## The decision

`Frozen()` stays. A component may instead implement `FrozenAllows`, which
embeds it and adds `FrozenAllow() gooey.Allow`. One function,
`frozenAllow`, combines the two, and **`isFrozen` is now its bool
projection** — `frozenAllow(w) != AllowAll` — so there is one observed
answer per component rather than two that can disagree.

### `AllowAll` means "not frozen"

Putting "not frozen" *inside* the lattice is the choice everything else
follows from. A component that does not implement `Frozen` answers
`AllowAll`; `<Frozen Allow="All">` and no `<Frozen>` at all are the same
value, which is correct rather than a coincidence. The alternative — a
separate bool beside the set — is two facts about one thing, and the
second one is the one that goes stale.

### A bitmask, not a map or a slice

`Composer.armFrozen` wraps the answer in a `prop.NewComputed` and the
per-frame sweep compares this frame's answer against last frame's. That
comparison has to be cheap, total and exact, and a bitmask is comparable
with `==` — so the sweep stays the single instruction it was for the
bool, and it stays honest for a change from `{Pointer}` to
`{Pointer, Hover}`, which really is structural because it moves the
hover-watcher registrations.

A map or a slice would have needed a deep compare per node per frame,
and — worse — would have made the sweep's correctness depend on how the
caller built the set.

### The categories, and why these

One category per **door that freezing closes**. That enumeration is the
only one that is complete and checkable, because each door is a distinct
registration or routing decision in `FocusManager.walk`,
`FocusManager.Dispatch`, `DispatchMouse` or `Composer.collect`:

`Focus`, the key classes (`Alpha`, `Numeric`, `Punct`, `Space`, `Nav`,
`Edit`, `Escape`, `Chords`), `Bindings`, `Mnemonics`, `Pointer`, `Hover`,
`Start`. Groups `Text`, `Keys`, `Mouse`, `All`, `None` are unions.

The key classes exist because of one case that decides the whole axis:
**"let the user type" must not mean "let the user press ctrl+s"**, or a
read-only preview saves the document. `AllowFor` classifies any event
held with ctrl or alt as `Chords` whatever the key is; shift is not a
chord, because terminals cannot report it on printable characters and
shift+tab is navigation.

### Composition: union, with closure in the CONSTANTS

Sets compose by union — naming more permits strictly more — and nesting
**intersects**, so a frozen host inside a stricter one cannot hand out
permission its container withheld.

Two implications are built into the exported constants rather than
applied by a normalizing pass:

- every key class, and `Bindings`, carries `Focus`. A key that reaches
  nothing is not an allowance: with no focus stops nothing inside can be
  focused, so no key routes there, and `Allow="Alpha"` would be a
  spelling that does nothing. **A vocabulary whose most obvious spelling
  is a no-op is a trap**, and this is the trap the closure removes.
- `Mnemonics`, `Pointer`, `Hover` and `Start` do not carry it. None is
  routed through focus — a mnemonic is offered to every handler in the
  tree regardless of what holds focus — so each is genuinely reachable
  inside a subtree with no focus stops at all. That asymmetry is what
  earns them separate categories instead of one "input" bit.

Putting the closure in the constants rather than in `ParseAllow` means
there is no order of composition, and no path through `ParseAllow`,
`sets:Concat` or Go code, that produces a key class without its focus
bit.

`Start` is implied by nothing, deliberately, and it is the one category
whose argument is safety rather than ergonomics: `Companion.Start` spawns
a child process, so a grant that turned starting on as a side effect of
wanting hover would launch a subprocess from an editing gesture.

### The observer did not change, and that is the result

`armFrozen`'s computed calls `Frozen()` and `FrozenAllow()` itself, so
whatever they read is subscribed by the ordinary call-site rule — the
same discovery that makes a `Render`'s reads its damage declaration. A
host whose set is `parseAllow(p.Get())` is observed with no declaration.

The rejected alternatives are the same ones the bool rejected, and for
the same reasons. A `*prop.Property[gooey.Allow]` handed to the framework
to watch would make the subscription come from the DECLARATION rather
than from the read, and a host whose permissions derive from two
properties would need a mirror source — which `prop.Set`, not comparing
values, would re-dirty the composition from on every no-op write.

`frozenAllow` **hoists both reads above the branch**. A read behind the
`if !frozen` return would drop out of the dependency set on exactly the
frames where the answer is about to start mattering.

The sweep raises `structDirty` on **any** change to the set, not only on
the frozen/not flip. Being conservative costs a re-sync for a change that
happens to move no registration — a key class, say — and that is the
right side to err on, because the sweep cannot know which categories a
walk consults.

## `<Frozen>` in markup

`components.Frozen` is a transparent one-child container: it paints
nothing, so it stays on the chrome-only pre-clear path and costs no
damage of its own.

| Attribute | |
|---|---|
| `Active` | bind-only. Omitted means always frozen; a literal is a load error, because a constant `false` is a `<Frozen>` that should be deleted rather than written |
| `Allow` | the category names, literal or bound. Omitted means `None` |

Neither declares an `AttrSpec.Default`, and that is the catalog's rule
rather than an omission: `Default` claims "writing this is the same as
writing nothing" and the defaults tests check it by RENDERING. Freezing
changes what the tree means, not what it looks like, so no value of
either attribute is discriminable in a static frame.

### The set crosses as TEXT

`Allow` is a `*prop.Property[string]`, not a handle to a `gooey.Allow`.
That is what lets markup compose it with the tools markup already has: a
value-namespace call returns a string, interpolation mixes literals and
bound paths in one attribute, and every Get happens inside the computed
that reads it. A typed handle would have needed its own value-namespace
type, a second binding path, and a viewmodel holding a framework type.

The cost is stated rather than hidden: an unknown name in a **bound**
value cannot be a load error. A **literal** one is — checked in the
builder, failing the load and naming the vocabulary. A bound one fails
**closed**, to `AllowNone`, with `components.Frozen.AllowError()`
carrying the reason, because a set nobody can parse must not be read as
permission.

`components.Frozen` caches the parse keyed on the text, because
`FrozenAllow` is called from `FocusManager.frozenHostFor` on every routed
event including motion. It caches the parse and never the read: the `Get`
runs unconditionally before the cache is consulted, or the observer's
dependency set would depend on whether the text happened to change.

## `handlers/sets`

A new value namespace, `gooey.dev/handlers/sets`, root module (the graph
is `fmt`, `sort`, `strings`, `unicode`, plus `gooey` and `markup`).

`Concat` (union), `Without` (difference), `When` (conditional), `Group`
(expands a `gooey.Allow` group name), `Has` (membership). Output is
canonical — deduplicated, single-space separated, ordered by
`gooey.SortAllowNames` — which is what keeps `components.Frozen`'s parse
cache hitting.

The pack is **generic**: a set is names, and nothing in it knows what a
name means. Validation belongs to the consumer, because a pack that
validated against one consumer's vocabulary could never serve a second.
The one exception is `Group`, which reads `gooey.AllowGroups()` — a
derivation rather than a copy, because a second table of expansions would
go stale and the failure would be a page silently granted the wrong
permissions.

`When`'s falsey list (`""`, `"false"`, `"0"`, `"off"`, `"no"`) exists
because a bound bool renders as `"false"` through `Arg.String`, and
`"false"` is not empty — a truthiness rule of "non-empty" would have made
every `When` on a bool permanently on.

## What this does not do

- **`<Frozen>` is not focusable.** It receives the events its subtree
  would have — they bubble from it to its ancestors — but it is not a tab
  stop, so a design surface's own key gestures still live on an ancestor
  or on the editor shell.
- **A design surface still owns per-element identity itself.** This
  changes what a frozen subtree *permits*, not what a document *is*; the
  `map[gooey.Component]*node` from the design-surface spec is unchanged.
- **The category set is not extensible by a host.** Adding a category is
  a change to `gooey/allow.go`. Nothing needs one yet, and an open
  vocabulary would make `AllowAll` — the "not frozen" value — depend on
  what happened to be registered.
