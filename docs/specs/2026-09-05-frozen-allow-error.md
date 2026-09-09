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

Four load-time refusals, because each is a spelling that would read as
configured and report nothing forever:

- a **literal** in the attribute — a write target has nowhere to put the
  message;
- an **absent or literal `Allow`** — with no parse, or a parse that
  already happened at load, the channel could never carry anything;
- a nil **`Context.Dispatcher`** — the publication has no route;
- a **computed** target — it derives its value and has no setter. This one
  is the worst of the four and was added in round two: before the guard it
  did not read as configured, it PANICKED inside `Build`.

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
the common case was the wasteful one. `validate/validate.go:98-103` is the
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

### The first consumer this breaks is the designer

`apps/wysiwyg` builds `ed.docCtx` — the context the edited DOCUMENT is
built with — from `ed.ctx.Values` and `ed.ctx.Styles` and never sets
`Dispatcher`, so a document using `AllowError` loads in a real app and
fails to load on the canvas, while the palette still offers the attribute.
The class is not new (`markup/handlers.go:193` refuses `{{ns:Fn}}` the same
way) but this is the first PLAIN attribute to trip it. Tracked as
[#462](https://github.com/WonderForgeLabs/gooey/issues/462) rather than
fixed here, because the fix is in another module and the test worth writing
pins the general property — that `docCtx` can build whatever a real app can
— not this attribute.

### Also not pinned: the observer is never detached

`armAllowError` installs `errC.OnInvalidate` and returns; nothing
unsubscribes. Any path that re-`Build`s a page against the same
`Context.Values` — the `os.DirFS` watcher, and the designer, where
`ed.docCtx` shares `ed.ctx.Values` and the document rebuilds per edit —
leaves each previous build's `errC` subscribed through its own computed
chain, keeping the dead `*components.Frozen` reachable and posting one
redundant `publish` per generation on every `Allow` change.

The compare guard makes every one of those a no-op, so it is a slow leak
rather than a wrong answer, and `validate/validate.go` has the identical
shape — this is the package's existing contract for observer lifetime, not
a regression introduced here. It is recorded because `AllowError` is the
first WRITE target and the next one multiplies it; a detach seam is the
right fix and it belongs with the second consumer, not the first.

**And it is what makes the per-build reset load-bearing rather than
merely convenient**, which the paragraph above states too generically to
catch. `TestASecondBuildMayReuseASinkTheFirstArmed` exists because the
watcher and the designer must be allowed to re-arm a sink — so the
armed-sink set is restored on the way out of the outermost
`document.build`. Combine that with non-detachment and the rebuild path
HOLDS, across generations, exactly the two-writer configuration the load
guard refuses within one build: generation one's `errC` observer is still
live when generation two arms the same sink.

It is harmless while both generations read the same `Allow` source —
they compute the same message and write the same value, and the compare
guard makes the second a no-op. It becomes reachable the moment a reload
rebinds `Allow` to a different property: two live arms, different
messages, one sink, and the last invalidation wins. That is the same
symptom `armedSinks` was added to prevent, arriving through the escape
hatch that guard needed in order not to break the watcher.

So the escape hatch and the missing detach are one decision, not two.
Whoever adds the detach seam should delete the per-build restore in the
same commit — with detachment, a rebuild's arms are genuinely gone and
the set could stay page-lifetime. Found in review of #459.

### One reviewer suggestion that did not survive its own mutation

Review proposed that the new `Active="{{.Off}}"` test would also guard
`gooey.frozenAllow`'s deliberate `FrozenAllow()`-before-`Frozen()` ordering
(`component.go:235-241`). It does not. Rewriting that function to return
early when unfrozen leaves the test green, because `armAllowError`'s
computed calls `f.FrozenAllow()` on the component directly and never routes
through it. The test pins that the COMPUTED does not gate on `Frozen()` —
which is a real and separate claim, and the mutation that fails it is
`if !f.Frozen() { return "" }` at the top of the computed.

Recorded because adopting the reviewer's framing unchecked would have put a
false "this test guards X" comment into the tree — which is the exact defect
round two removed from `elements.go`, re-introduced from the other side.

## Round four: two writers, and a channel wired to its own source

Two correctness findings, both reproduced before they were fixed, and both
the same failure this attribute exists to remove — reappearing in page
shapes the first three rounds never built.

**Two `<Frozen>` binding one `AllowError` erase each other.** Measured:

```
after load:              Err="unknown Allow category \"Nonsense\"; …"
after A's benign change: Err=""      <- B still sealed, message gone
```

`publish` compared against `sink.Get()`, which treats the sink as the record
of what THIS arm last published — true until a second arm writes to it. A
going `"Focus"` → `"Hover"` (both parseable) yielded `""`, saw that differ
from the sink's current value (B's live failure), and wrote over it. B's
computed was clean, so it never republished. Subtree sealed, reader empty:
#424 exactly, one page-shape over — and a plausible shape, since two frozen
panes over one status line is the surface `Frozen` was built for.

Fixed twice over, because the two halves cover different writers.
`Context.armedSinks` makes a second arm on the same handle a **load error**,
which is the real fix and matches how every other spelling in the class is
refused. (An ordinal here said "the other four" while the section below said
"the fifth" — off by one against each other, which is why neither says a
number now. `docs/markup-reference.md` carries the list.)
And `publish` now compares against **this arm's own last published value**
rather than reading the sink back, so an arm with nothing new to say writes
nothing whatever the sink holds — that half reaches writers the load guard
cannot see, a code-behind, an MCP `set_value`, a `Startable`.

`armedSinks` is page-wide but **per top-level build**: a nested `Load`
inherits the outermost map so two controls sharing a sink still collide,
while a rebuild against the same `Context` — the `os.DirFS` watcher, the
designer — starts clean instead of refusing what it armed last time.
`TestASecondBuildMayReuseASinkTheFirstArmed` is what makes that scoping
load-bearing rather than decorative; without it the guard breaks both
hosts, and the failure reads as the user's markup being wrong.

**`Allow` and `AllowError` bound to one property destroys the allow set.**
`<Frozen Allow="{{.X}}" AllowError="{{.X}}">` built, and the priming
publish then overwrote the author's own set with the parse message before
the UI was live — measured, `X` went `"Focus"` → `""` during `Build`.

**Compared by resolved HANDLE, and the first version compared text.** The
reasoning for text was that "pointer identity cannot catch it: `BoundText`
wraps a dynamic attribute in a fresh computed on every call, so the two
handles differ even here" — true of the computed, and the wrong handle to
compare. The SOURCE a binding resolves to is stable and available at the
check. The text compare missed two spellings that both built cleanly and
both destroyed the allow set during `Build`: an alias that is not the FIRST
binding (`Allow="{{.A}} {{.X}}" AllowError="{{.X}}"` — `bindingPath` is
`FindStringSubmatch`, so it reads `A`), and two Values names for one
property. The dup-sink guard forty lines below already refused the second
shape for its own question, because it keys by pointer — so two guards on
one line of defence disagreed about what "the same property" means, and the
weaker one was the one protecting page state. Found in review of #459.

### The citation, and where it came from

`prop/prop.go:101` is inside `Settable`'s doc comment; `Property.Set` is at
`:117`. The number came from round two's review and I propagated it into
`frozenerror.go` without checking, which is the same unchecked-adoption
that produced round three's false test comment. Corrected here and in
`CLAUDE.md`'s Traps section, which is where it was copied from.

Two older specs (`2026-08-12-settings-store.md`, `2026-08-14-frozen-observed.md`)
carry the same stale number. They are dated decision records and are left
alone here rather than widening this diff — but a line reference is a
navigational aid, not a historical claim, so they send a reader today to
the wrong function. That is the hazard line-numbered citations always carry
and the reason the other three in `frozenerror.go` were each re-checked.

