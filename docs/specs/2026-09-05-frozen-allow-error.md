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

## Round two: what review found, and the one thing it changed my mind about

Seven findings, all real against `HEAD`. Four are worth recording because
each is a *class* this record had already claimed to be careful about.

**A write target has to be checked for writability, and `Bound` does not
do it.** `Bound[string]` resolves handles for READING — which is what every
other attribute on `<Frozen>` wants — so `AllowError="{{.Derived}}"` over a
`prop.NewComputed` reached `armAllowError`'s priming `Set` and **panicked
inside `Build`**: `panic: prop: Set on computed property`, in the one
package whose entire contract is that everything resolvable resolves before
the UI is live, and under the `os.DirFS` watcher a rebuild panic takes the
app down instead of showing a load error. `markup/cond.go` already names
this gap and says the fix belongs in the two-way binders; `AllowError` is
this package's first write target, so it is the first place to honour it.
The guard is `sink.Settable()`, and it makes a fourth spelling of "reads as
configured and reports nothing forever" — the worst one, because it takes
the process with it.

**`prop.Set` does not compare, and this record knew that and did it
anyway.** Every benign edit to `Allow` — `"Focus"` to `"Hover"`, both
parseable, message unchanged at `""` — republished and repainted every
dependent of the sink. `Allow` changes far more often than it breaks, so
the common case was the wasteful one. `validate/validate.go:99-102` is the
in-repo precedent and the fix is its shape. The re-read that re-arms `errC`
still happens; only the publication is skipped.

**The one argument this PR made hardest was the one it pinned least.**
Replacing `d.Post(publish)` with an inline `publish()` left the entire
`markup` package green — including `TestAllowErrorWithoutADispatcherIsALoadError`,
which exercises the nil guard and never the use. Every publication test
Drained before it read, so an inline Set was indistinguishable from a
posted one. What distinguishes them is **the interval**: after the `Set`
that breaks the parse and BEFORE the `Drain`, the work must be queued
(`Pending() == 1`) and the sink must still hold its old value. The tests
also built `&gooey.Dispatcher{}`, whose `wake` channel is nil — the one
`Dispatcher` shape whose `Post` cannot wake an app loop, and the wrong one
to pin a posted publication with. They use `NewDispatcher()` now.

**Two parses of one string are two answers that agree by coincidence.**
`errC` re-derived the message with its own `ParseAllow` rather than asking
the `Frozen` it is attached to. They agree today; the day `FrozenAllow`
wraps its error with context, the private parse silently keeps publishing
the bare one. It now calls `f.FrozenAllow()` and reads `f.AllowError()`,
which makes the accessor the single record, subscribes identically
(`FrozenAllow`'s `Allow.Get()` is unconditional, above the cache check) and
reuses the parse cache.

### The damage assertion had to be differential, and finding that out took a measurement

CLAUDE.md is explicit that a damage count is the only pin for a repaint
claim, and the first version of this test asserted `breaking > benign`. It
read **1 against 1**. Any `Allow` change re-evaluates the Composer's own
freeze observer, which repaints one component by itself, and the
publication was entirely hidden inside that. Subtracting a control page
identical but for the reader (`>{{.Err}}</Text>` against `>steady</Text>`)
leaves exactly the reader's repaint: benign `1` vs `1`, breaking `1` vs
`0`, difference exactly 1. Guessing the number would have shipped a test
that passed against the bug.

### Corrected, not reworded

`elements.go` carried a comment claiming the literal-`Allow` refusal is
what makes `armAllowError`'s handle a computed, "and `prop.OnInvalidate` on
a source never fires". `armAllowError` never observed the handle it
receives — it observes `errC`, a computed it builds itself, which
invalidates correctly either way. That false reason was caught during
development and corrected in `frozenerror.go`, and the copy in
`elements.go` was left standing, eight lines from the text contradicting
it. It is deleted rather than reworded; the paragraph above it already
gives the true reason.

### Found while fixing the above

`docs/markup-reference.md`'s own worked example did not load:
`<Text Text="{{.FreezeErr}}"/>`. `<Text>` takes its text as a BODY and has
no `Text` attribute, so the one snippet showing a reader of this channel
was markup that fails at load. It was the only occurrence in `docs/`.

And the published message reports the **parse**, not the seal. `frozenAllow`
(`component.go:242`) asks `FrozenAllow()` before `Frozen()` — deliberately,
or the observer goes deaf to an allow-set change on exactly the frames
where it begins to matter — so an unparseable set publishes even while
`Active` is false and nothing is sealed. That is the right behaviour and
the wrong reading of the name, so the reference now says which it is.

