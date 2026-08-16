# The binding surface a third-party component gets

**Status:** accepted, implemented (`markup/bind.go`)
**Issue:** #266
**Date:** 2026-08-15

## The gap

`Context.Components` has always been the extension point: register a
`Builder` under an element name and `<Probe/>` builds your component.
The builder receives the raw `Element` and the `*Context`, and the
documentation says it "interprets attributes however it likes".

It could not, quite. Everything the markup dialect layers on top of a
raw attribute string lived behind unexported helpers in package
`markup`:

| what | where it lived | reachable from outside? |
|---|---|---|
| typed handle + both-types load error | `boundProp[T]` | no |
| interpolation, value-namespace calls | `bindText` via `literalOrBound` | no |
| `#rgb`/`#rrggbb` literal | `parseHexColor` via `bindColor` | no |
| `Style="name"` and its bound twin | `bindStyle` | no |

A built-in element got all four because it happens to be compiled inside
the package. Everything registered from outside — which is every
nested-module component, and the whole reason components are supposed to
be reusable and each live in its own package — got `e.Attrs[attr]` and
`Context.BindingValue`.

That bound third-party components to load-time constants in practice.
Tutorial 6 is the evidence: it hand-rolls an `intAttr` helper doing the
`BindingValue`-plus-type-assert dance, and its `Stepper` takes `Label`
as `e.Attrs["Label"]` — a literal, because nothing better was available.
`mcp/tools.go` re-declares `parseHexColor` verbatim for the same reason.

### The part that was not merely inconvenient

`BindingValue` was exported, so "resolve `{{.Path}}` yourself" looks like
an adequate workaround. It is not, for text:

```go
v, err := ctx.BindingValue(e.Attrs["Label"])   // Label="Hi {{.Who}}!"
```

`bindRe.FindStringSubmatch` matches *anywhere* in the string, so this
succeeds, returns the `Who` handle, and **silently discards `"Hi "` and
`"!"`**. The component paints `world`. No error, no warning — a third
silent-drop class in a dialect whose whole discipline is that anything
resolvable fails at load.

## The decision

Export the four resolvers as free functions in a new `markup/bind.go`,
and delete the unexported spellings rather than keeping both:

```go
func Bound[T any](e Element, ctx *Context, attr string) (*prop.Property[T], error)
func BoundText(e Element, ctx *Context, attr string) (*prop.Property[string], error)
func BoundColor(e Element, ctx *Context, attr string) (*prop.Property[render.Color], error)
func BoundStyle(e Element, ctx *Context) (*prop.Property[render.Style], error)
```

The built-ins now call the exported names. There is one spelling of each
rule, and a third-party builder and a built-in builder are the same kind
of caller — which is the property that keeps this from drifting back
apart.

### Why these four, and not fewer

The rule for what crosses the package boundary is **what a third-party
builder cannot correctly write for itself**:

- `Bound[T]` — writable by hand, but every caller would re-derive the
  same "is this a binding at all" check and a worse error message. The
  cost of *not* exporting it is not impossibility, it is that every
  third-party component's load errors look different from every
  built-in's.
- `BoundText` — **not** writable: `scanBindings` and `valueHandle` are
  unexported, and the hand-rolled approximation is the silent-drop bug
  above.
- `BoundColor` — not writable without duplicating the parser, which
  `mcp/tools.go` demonstrates by having duplicated it.
- `BoundStyle` — writable (`Context.Styles` is exported), but it is the
  same one-line rule as `BoundColor`'s literal-or-bound shape and
  splitting the four would leave a component's `Style=` behaving
  differently from every other element on the page.

### Why these four, and not more

Deliberately excluded, with reasons:

- **`optBool` / `optDuration` / `optionList`** stay unexported. They are
  literal parsers, not binding resolution; `strconv` is already public
  and the dialect has no opinion a caller could get wrong. `#rrggbb` is
  the exception precisely because the dialect *does* have an opinion
  there and no standard parser matches it.
- **An `Opt`/optional variant of `Bound[T]`.** The built-ins that want
  one write `if raw, ok := e.Attrs[attr]; ok && strings.TrimSpace(raw) != ""`
  first (`buildTabs`); a third party can write the same three tokens.
  Adding `OptBound[T]` doubles the surface to save a conditional.
- **`literalOrBound(raw, ctx)`**, the raw-string form, stays unexported.
  Two internal callers resolve something that is not an attribute at all
  — an element's text content (`<Env>` values) and the `Tooltip=`
  shorthand's already-extracted string. The exported shape takes the
  element so errors can name it, which is the shape a builder actually
  holds.

### Why free functions rather than `Context` methods

`Bound` is generic and Go has no generic methods. The other three follow
its shape so a builder reads uniformly rather than mixing
`markup.Bound[int](e, ctx, …)` with `ctx.BoundText(e, …)`.

## Where this meets `rowValue` (#274)

`components.rowValue` (`components/itemsview.go:718`) is the **producer**
of the typed-handle contract: it turns each projected row value into a
`*prop.Property[T]` and puts it in that row's own `Values` map. `Bound[T]`
is the **consumer** of the same contract. They are the same "the type
switch IS the type system" pattern at two ends of one wire, and #274
(which added `image.Image` and `*prop.Property[image.Image]` arms) moved
the producer end.

They compose, and the composition is what this change widens. A row
template inherits the page's `Components`, so a registered component
could always be *placed* in a row — but it could not resolve what
`rowValue` had just produced for it, so a third-party cell was stuck on
literals inside a list whose values were already perfectly good handles.
`TestThirdPartyBindsARowValueInsideAnItemTemplate` pins that end.

The standing hazard is that the set of types `rowValue` produces and the
set `Bound[T]` is instantiated at have to agree, and **nothing
mechanically forces that**. A type the producer does not name crosses as
a literal and is fixed for the life of the row; a type the consumer is
never instantiated at is simply unreachable from markup. Adding an arm to
one is a reason to look at the other.

### A gotcha that surfaced writing that test

`ItemsView.Validate` builds row 0 through the template and **throws the
component away** (`components/itemsview.go:211`). A builder placed in a
template is therefore invoked once more than there are rows, and the
extra instance is never arranged, never painted, and never updated — it
keeps its first projection forever. Two consequences:

- a test that indexes `cells[0]` reads that orphan and reports a
  re-projection failure that did not happen (this cost a debugging
  round-trip; the test now counts matches rather than indexing);
- a third-party builder with side effects runs them one extra time per
  `ItemsView`. Builders should stay pure.

## What this does not change

- **No reflection.** `Bound[T]`'s type assertion is the whole type check;
  `T` is known at the call site exactly as it was when the function was
  unexported. A future `gooey gen` can still compile markup ahead of
  time.
- **Lvalue semantics, and the subscription rule across a package
  boundary.** All four resolve once, at build time, to handles. Whether
  a `Get` subscribes is decided by `prop.node.recordRead`
  (`prop/prop.go:33`) against `evalStack`, a package-level variable in
  `prop` — it asks "is a computed evaluating right now", never "who is
  calling". So the caller's package is irrelevant: a `Get` from a
  component compiled in a nested module records the same edge a built-in
  would, because both run inside the paint node the Composer wrapped
  around `Render`. Exporting the resolvers could not have changed this,
  and `TestThirdPartyBoundHandleDrivesDamage` confirms it empirically
  from `package markup_test` — one component repaints, with a guard that
  the first frame paints more than one so the count can discriminate.
- **Load-time failure.** A type mismatch, an unresolved path, a literal
  in a handle position, and an unparseable colour are all load errors for
  a registered component exactly as for a built-in.
- **The attribute *schema* gap is still open.** `Context.Components`
  entries appear in `ctx.Catalog()` as `OriginRegistered` with no
  attribute list, so tooling still cannot know what `<Probe/>` accepts.
  That is the "Context.Components learns to carry a schema" note in
  `catalog.go` and is separate work.

## Tests

`markup/thirdparty_test.go` is deliberately `package markup_test` — an
in-package test would resolve the unexported helpers and prove nothing
about the boundary this record is about.
