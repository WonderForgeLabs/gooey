# Markup-declared control properties (decision record)

Settled in discussion with Elan 2026-08-10 (via /btw fork). Scheduled
into the markup work-package that follows the input chapter, alongside
xmlns/extension expressions and the dotted property-element parse path.
Tracked as epic [#7](https://github.com/WonderForgeLabs/gooey/issues/7).

## The commitment

**Declared markup properties are ordinary dependency properties,
registered from markup. One property system throughout.**

A `.gooey` control file may declare its property surface; each
declaration materializes the identical artifact code-behind wires
today — a `*prop.Property[T]` node:

- attribute bound at the instantiation site → the parent's existing
  node passes through (today's Include behavior, now type-checked);
- attribute absent → a fresh per-instance `prop.NewSource[T]` carrying
  the declared default — markup-defined, typed, bindable local state;
- absent + Required → load-time error.

This is the markup tier of the registration mechanism, exactly as
`DependencyProperty.Register` is WPF's code tier — not a side-channel,
not a parallel data model. Precedent: XAML 2009 `x:Members`/
`x:Property` specified this; WPF's pipeline never shipped it, which is
why XAML-only files spent two decades faking properties with keyed
resources and implicit DataContext contracts.

## Consequences

- Strict contract: a file with declarations rejects undeclared
  instance attributes at load. No declarations → pass-through (today's
  behavior, backward compatible).
- Types via a plain type-switch table (string/int/bool/…, `any` as the
  escape hatch for app types). Zero reflection, as everywhere.
- `gooey gen` gains a typed per-control surface for compile-checked
  instantiation; the remote-behavior layer gains a per-control wire
  schema for free — the declaration block IS the schema.

## Explicitly out

**Markup-declarable attached properties** (a markup-only panel
defining its own attachment slots) — would require a dynamic
per-element property bag on `Base`, reintroducing stringly-typed
storage. Attached properties remain host-type-defined (`Grid.Row` in
`Layout`). Resist until something real demands it.

## Spelling (settled)

`<x:Property Name="Title" Type="string" Default="untitled"/>` as
direct children of the root, under the `x:` language-services
namespace (`xmlns:x="wonderforge.io/gooey/x"`) — shipping XAML 2009's
`x:Property`, which WPF specified but never implemented. Declarative
noun in markup; the phrase "dependency property" does its recognition
work in docs and error messages ("dependency property \"Title\" —
required attribute missing"), not in the element name.
`<DependencyProperty:Register/>` was considered and rejected: a
prefix must be a module, not a type, and markup declares — it does
not call. Declarations belong on the root because the root IS the
control's type definition.

## With a code-behind (merge semantics)

Declarations own the public surface; code-behind owns private members
and behavior. Order: (1) declarations resolve the instance's
attributes into a pre-populated child context (bind / default /
required-error); (2) setup runs second and EXTENDS that context —
reads declared handles, adds internal computeds and widget builders,
registers bare-name handlers. Setup colliding with a declared name is
a load error (one source of truth; same reason WPF rejects double
registration). Strict attribute checking validates against
declarations only, with or without code-behind. Change callbacks need
no mechanism: a computed reading a declared handle, or OnInvalidate
on it — the graph is the callback system.

Control tiers, each adding exactly one thing: Include (implicit
surface, no behavior) → declarations (checked surface, no behavior) →
declarations + code-behind (checked surface + private behavior).

Known wrinkle: declared defaults materialize per-instance sources
inside the control, so hot reload resets them until Name-keyed state
adoption exists — that design item now has a concrete customer.

## Executed (2026-08-10)

Shipped as specified, landed in [PR #84](https://github.com/WonderForgeLabs/gooey/pull/84).
`markup/property.go` holds the declaration
machinery; `markup/usercontrol.go`'s `control` is the shared
instantiation path for both control tiers; `cmd/cardsdemo`'s
`card.gooey` and `badge.gooey` are the proof — a markup-only demo that
gained a checked, defaulted, typed contract with zero Go changes.
Reference: [markup-reference.md](../markup-reference.md#declared-properties-xproperty).

What the implementation added on top of the record:

- **The type table is `markup.propKinds`**, one row per type, each row
  built by a generic `kindOf[T]` whose closures carry T. Adding a type
  is adding a row. `color` gained the one literal it needed
  (`#rgb`/`#rrggbb`); there is no other color syntax in markup.
- **`Element.Space`** — the parser now keeps the element name's resolved
  namespace URI, which is how `<x:Property>` is told apart from a
  component without reserving the name `Property`. This is the piece the
  handler-namespace work built the prefix table for, now load-bearing
  for element dispatch too.
- **`Required` and `Default` are exclusive**, and a `Default` is coerced
  when the CONTROL loads rather than at whichever instantiation site
  omits the attribute — a bad default is a defect in the control.
- **`Type="any"` takes no `Default`** (no literal syntax to coerce) and
  is the only type a handler expression may cross into, since a Command
  has no declared type of its own.
- **`DeclaredProperties()` on `Context`** is how a code-behind setup
  reads its own declared handles: installed on the parent context for
  the duration of the setup call, the same document-scoped save/restore
  the xmlns table uses. The `UserControl` signature is unchanged, so
  every existing control compiles and loads untouched.
- **Strict mode excludes `Name` and the layout attributes**, because
  those are the element's, not the control's.

Hot reload confirms the wrinkle above: `TestDeclaredDefaultResetsOnRebuild`
pins the behavior rather than fixing it. `Name`-keyed state adoption is
the fix and now has its customer.

Wire-schema consequence (noted, not built): a declarations block is a
complete per-control schema — name, type, required, default — so the
remote-behavior layer can serialize a control's surface without a Go
type, and `gooey gen` can emit a typed constructor from the same source.
Nothing reads it that way yet.

## The fourth case: no instantiation site (2026-09-17)

The record above states the resolution rule as **three** cases at an
instantiation site — bound, literal, absent. A tool that holds the
control file *itself* has no site at all, and none of the three applies:
`declarations.instantiate` runs from the page that writes
`<Card Title="…"/>`, so a wysiwyg editor previewing `card.gooey` never
reaches it, the declared names never land in `Context.Values`, and the
control's own `{{.Title}}` is refused with `"Title" not found in
context`. That is [#517](https://github.com/WonderForgeLabs/gooey/issues/517),
and it is a gap in the rule rather than a bug in the editor.

`Declaration.AbsentValue` answers it with the third case's handle: a
fresh per-call source carrying `Default`, exactly what `resolve` makes
for an absent optional attribute, read off the same `propKinds` row. A
`Required` declaration gets its type's **zero** rather than the load
error — `Required` is a contract with a site, a caller that has none is
not in breach of it, and the zero is also what a site that forgot the
attribute would show. A fourth table keyed by the `Type` spellings was
the alternative, and `propKinds`' own doc already names two that drift
for exactly that reason.

**It is not called `NewValue`.** `ValueProvider.NewValue(*Call)` is
already exported from `markup` (`markup/values.go`), and
[the value-namespaces spec](2026-08-12-value-namespaces.md) records the
plan to key *that* one against `propKinds` — this method's table. Two
exported methods sharing a name in one package, one of them slated to be
defined against the other's table, is the same drift in the identifier;
the name says which case it answers. Raised in review of
[PR #522](https://github.com/WonderForgeLabs/gooey/pull/522).

**The editor's half is local preview only.** `rebuild` returns on the
remote path before it seeds, so under `-attach` a control's defining
document still reports the *target's* `"Title" not found in context`.
That is deliberate — the target's context is the authority on whether
the document loads there, and a name the editor invented locally would
make the preview agree with itself about a document that does not load —
and `TestSeedingDoesNotRunOnTheRemotePath` is what keeps it stated.

Seeding goes into the editor's one binding map, which the gRPC and MCP
servers are also handed, so a declared name is briefly in the control
plane's vocabulary: the binding pickers offer `{{.Title}}` while the
declaring document is open, and a client's `set_value` against one is
discarded on the next rebuild.
`TestASeededNameIsVisibleToTheControlPlaneAndIsTransient` measures both
halves.

## What the editor's namespace handling got wrong, and in which order (2026-09-19)

The editor side of the fourth case took six review rounds, and the
reasoning for each repair was kept inline beside the code it repaired.
Review of [#522](https://github.com/WonderForgeLabs/gooey/pull/522)
observed the cost: `browser.go`'s root-count branch carried ~60 lines of
comment inside one `if` body, four paragraphs of which described what
earlier rounds of the same PR got wrong, and the defect that round found
was in the branch whose own comment stated the rule the code did not
implement. At that density it is hard to see which paragraph describes
the code in front of you. The invariants stay at the code; the rounds
are here.

**One question, asked in four places, answered differently each time.**
"How is this declaration spelled?" has one correct answer — its own
xmlns binding if it carries one, else the envelope's, else bare — and
the editor arrived at it four times:

1. **The envelope only.** `declBinding(n.Attrs)` reads the envelope, and
   XML scoping lets the binding sit on the `<x:Property>` element
   itself. A correctly namespaced `<p:Property>` document was reported
   as containing `<Property>` — which `bareDeclMsg` in this same editor
   defines as the missing-namespace typo, so an author acting on it
   would have edited a namespace that was already right.
2. **`declPrefix`, which answers a different question.** It is a SAVE
   decision: *which prefix will the save write, and does the document
   already bind it.* Its `bound=false` means "the envelope needs a
   binding added at write time", not "the file writes `<Property>`
   unprefixed". Read as the second, a file holding `<p:Property>` and
   `<q:Property>` was told it held 2 `<Property>` declarations.
3. **`decls[0]`'s binding, printed with `len(decls)`.** Correct for one
   declaration and for any number that agree; a count of elements the
   file does not contain as soon as two bindings are in play. This is
   the one that survived five rounds of work on that exact message,
   because no arm of `TestTheRootCountRefusalSaysWhatItCounted` mixed
   prefixes.
4. **Per element, through `declElemName`.** `alienDeclMsg` had already
   arrived here one arm over; the count branch now asks the same
   function, so the two cannot drift again.

**`declAttrs`' third clause was the bug, not a residue.** The first two
clauses remove bindings that no longer NAME the declaration — a default
`xmlns` and any prefixed binding other than the one being written. The
third is different in kind: on the copy, `xmlns:<prefix>` must be absent
or equal to `markup.XNamespace`, *whatever it used to say*. The
predicate asked first whether the value WAS the x namespace, so a
declaration carrying `xmlns:<prefix>` bound to something else survived
onto the element `envelopeHead` emits as `<prefix:Property>` — and the
prefix then resolved to the something else, so the declaration stopped
being one. Measured end to end: the editor reported a valid file as the
missing-namespace typo, then wrote "✓ saved" over the good bytes with a
file `markup.Build` refuses. Two shapes reach it, an envelope that binds
the prefix and one that does not; in the second `declPrefix` MINTS the
prefix and `declBinding`'s collision loop reads the envelope only, so
the mint can collide with a declaration's own binding. The clause closes
both, which is why the mint is left alone.

**`node.Space` is not empty for ordinary components.** Its field doc
said so for two rounds. `nodeOf` tracks the inherited default `xmlns`
specifically so it can resolve them, every in-tree `apps/*.gooey`
declares one, and `Space` is empty only for palette seed strings and
hand-written fixtures. Nothing was broken by it — `splitDecls` keys on
`k.Elem == "Property"`, matching markup's own `c.Name == "Property"` —
but the sentence invites `n.Space == ""` for "not namespaced", which is
true of a fixture and false of every real document.
