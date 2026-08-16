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
