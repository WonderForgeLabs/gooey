# Concept: markup tiers and the loading seam

A `.gooey` file is XML: elements are components, attributes are properties,
and `{{.Path}}` expressions are bindings resolved against a
`markup.Context`. Resolution happens **once at build time, to property
handles** — the built component holds the handle, so rendering performs no
lookups. The design calls this lvalue semantics.

There are four tiers of reuse, in increasing order of what they cost
you:

| Tier | Go code | What attributes do |
|---|---|---|
| **Include** | none | Become the control's context verbatim: a binding hands over the live handle, a literal a string. |
| **Include + `<x:Property>`** | none | Resolve against the control's *declared* surface: type-checked, defaulted, and an undeclared attribute is a load error. |
| **UserControl** | a setup func per control | You resolve and type-assert them, and build the instance's own context — or declare them and extend the context the framework pre-populates. |
| **Custom component** | a `Builder` | You interpret them however you like and return any `gooey.Component`. |

A declared markup property is an ordinary dependency property, registered
from markup rather than from Go — the same `*prop.Property[T]` node in the
same graph. Declaring nothing keeps the implicit, unchecked surface. A
registered component may soon declare its own surface too
([PR #290](https://github.com/WonderForgeLabs/gooey/pull/290)).

`Include` and `UserControl` — declared or not — give the control its own
context, so bindings inside a control resolve against the control and never
against the page, and data crosses the boundary only through the instance's
attributes. A registered `Builder` is the exception: it is handed the
*parent* context and owns whatever isolation it wants. See
[tutorial 5](../05-usercontrols.md).

Element resolution order, in full: a registered `Components` builder, then a
built-in element, then the `Includes` convention (`<Card/>` →
`card.gooey`), then an error.

## The `fs.FS` seam is the deployment story

`markup.Load` reads from any `fs.FS` and cannot tell them apart:

- **Development** — `os.DirFS(".")` behind `markup.Page`, which polls
  ModTimes and rebuilds in place — with
  [#53](https://github.com/WonderForgeLabs/gooey/issues/53) open to
  replace the poll with filesystem notifications. See
  [how-to: hot reload](../howto/howto-hot-reload.md).
- **Release** — `embed.FS`, which reports constant zero ModTimes, so the
  same call is a natural no-op. The same code ships both ways. See
  [how-to: embed markup for release](../howto/howto-embed-release.md).

A compiled tier (`gooey gen`, producing compiled markup and typed
surfaces) is designed but not implemented
([#59](https://github.com/WonderForgeLabs/gooey/issues/59)).

## Current limits

- `Style="name"` is a named lookup: no cascading, no selectors yet
  ([#54](https://github.com/WonderForgeLabs/gooey/issues/54)), no
  per-property overrides in markup (except `Text Bold`), and an unknown
  name silently yields the zero style.
- A declared default materializes a fresh source per instantiation, so it
  resets on hot reload; state that must survive a reload lives in the
  app's viewmodel. `Name`-keyed state adoption is designed, not built —
  [#50](https://github.com/WonderForgeLabs/gooey/issues/50).
- Markup cannot declare *attached* properties (`Grid.Row` and friends stay
  host-type-defined). See the
  [`x:Property` spec](../../specs/2026-08-10-markup-declared-properties.md).

Depth: [architecture.md — markup](../../architecture.md#markup).
