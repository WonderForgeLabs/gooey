# `<Frozen AllowError=…>`: a channel for the one failure that could not be caught at load

*Decision: 2026-09-05. Issue: [#424](https://github.com/WonderForgeLabs/gooey/issues/424), split out of #423 (review of PR #389).*

## The gap

`components.Frozen` fails **closed** on an `Allow` set it cannot parse —
the set becomes `gooey.AllowNone`, the strictest answer — and records why
in `AllowError()`. That is the right direction to fail in, and it was
pinned. Nothing read the accessor.

A **literal** `Allow` is checked at load time by the `<Frozen>` builder,
so a typo in markup is a load error naming the attribute. Only a **bound**
`Allow` can fail at runtime, because its value does not exist until the
app supplies it — and when it did, the subtree sealed permanently and the
only symptom was a pane that had stopped responding to everything.

That is the failure class the rest of markup exists to refuse. Half of one
mistake was loud and the other half was silent.

## What was rejected, and why it is not a small diff

**Have `FrozenAllow` publish alongside its parse.** This is the change the
issue's shape suggests and it is a UI-goroutine-confinement violation.
`FrozenAllow` is called from `FocusManager.frozenHostFor` on every routed
event, motion included, and from *inside* the Composer's evaluation of the
freeze observer. A `Set` from there mutates the property graph
mid-evaluation, on the routing hot path, once per pointer sample. Nothing
in the framework catches that, and the tests stay green.

**A general runtime-fault channel on `Composer`/`App`** (option 1 in the
issue). It is the seam a second fault of this shape would want, and
`gooey.LayoutFault` would become one kind of it. Not taken here: there is
no second case yet, and inventing the taxonomy from one example is how a
type comes to answer two unrelated questions. If a second arrives, this
attribute is a thin adapter over it rather than an obstacle.

## What was built

`AllowError=` is an ordinary bound handle — the page owns the property,
renders it like any other, and the framework Sets it. The publication is
arranged in `markup/frozenerror.go`, at build time; `components.Frozen` is
untouched.

Three load-time refusals, because each is a spelling that would read as
configured and report nothing forever:

- a **literal** in the attribute — a write target has nowhere to put the
  message;
- an **absent or literal `Allow`** — with no parse, or a parse that
  already happened at load, the channel could never carry anything;
- a nil **`Context.Dispatcher`** — the publication has no route.

## The two subtleties

**The re-read is what re-arms the observer.** A computed invalidates once
and then stays dirty until something reads it, so a hook that publishes
without re-evaluating fires on the first bad set and is deaf to every set
after it. Reading inside the posted closure does both jobs: it
re-validates the computed, and it runs the `Set` on the UI goroutine at
`Drain` rather than inside the invalidation that woke it.

A test crossing ONE edge cannot see this. `TestAGoodAllowClearsAPublishedFailure`
crosses three — bad, good, bad again — and the third transition is the
only assertion a fire-once implementation fails.

**The priming read is not initialization tidiness.** Without it a page
loaded with an already-bad set is told nothing until the value *changes*,
which for a page that never changes it means never.

## What is deliberately not pinned

Swapping the observed handle from `errC` to `allow` is a **silent
mutation**, and correctly so: `BoundText` returns a computed for a bound
attribute, and the builder refuses `AllowError` beside a literal one, so
the two handles are equivalent under every input the builder permits.
`errC` is observed because it is the one that survives if that restriction
is relaxed — not because the other is broken today. The comment in
`frozenerror.go` says this rather than claiming a reason that would fail
open.

Separately: the AttrSpec's `Binds: BindsBinding` is **not** what rejects a
literal — `Bound[string]` is, and it would reject one whatever the
declaration said. The declaration feeds the catalog and the designer
palette. This is the existing shape for every bind-only attribute
(`Active` on this same element behaves identically), so it is recorded
here rather than changed: a wrong `Binds` ships a palette that offers a
literal for an attribute that will refuse it at load, and no test notices.
