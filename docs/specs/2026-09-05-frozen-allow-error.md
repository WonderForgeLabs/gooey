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

The load-time refusals, because each is a spelling that would read as
configured and report nothing forever:

- a **literal** in the attribute — a write target has nowhere to put the
  message;
- an **absent or literal `Allow`** — with no parse, or a parse that
  already happened at load, the channel could never carry anything;
- a nil **`Context.Dispatcher`** — the publication has no route;
- a **computed** target — it derives its value and has no setter. It was
  added in round two, and before the guard it did not read as configured,
  it PANICKED inside `Build`;
- an **`Allow` that aliases the sink** — the priming publish would destroy
  the set it had just read. **Round four** added it (see that section);
  **round five** widened it from binding TEXT to the resolved HANDLE, so
  two names for one property are caught; and the **value-call round**
  below widened it again from whole-body bindings to every path in any
  position, because `Allow="{{v:Echo .X}}"` carries the alias into an
  argument the original scan could not see;
- a **second arm on one sink** — two `<Frozen>` publishing to one handle,
  where whichever fires last wins and neither says so.

**THIS SENTENCE NO LONGER OPENS WITH A NUMBER**, and that is the third
time the count in it has been wrong. It said "Four" over a four-item list
while six were implemented; the paragraph near the end of this record
already said "which is why neither says a number now" and this line
falsified it, which is worse than either alone — a record that both states
a rule and breaks it teaches the rule is optional.
`docs/markup-reference.md` got it right by never writing one. Count the
list if you need the number.

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
symptom `arms.sinks` was added to prevent, arriving through the escape
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
`armScope.sinks` makes a second arm on the same handle a **load error**,
which is the real fix and matches how every other spelling in the class is
refused. (An ordinal here said "the other four" while the section below said
"the fifth" — off by one against each other, which is why neither says a
number now. `docs/markup-reference.md` carries the list.)
And `publish` now compares against **this arm's own last published value**
rather than reading the sink back, so an arm with nothing new to say writes
nothing whatever the sink holds — that half reaches writers the load guard
cannot see, a code-behind, an MCP `set_value`, a `Startable`.

`arms.sinks` is page-wide but **per top-level build**: a nested `Load`
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


## Round five

Two defects, both introduced by the guard round four added, and both
invisible to every test that existed for it.

**`ctx.arms.sinks` arrived nil in an item template's row.** `elements.go`
WRITES to that map to arm a sink, and a write to a nil map panics.
`document.build` allocates it, and for one construction site that was
enough — but `buildItemsView` builds its row `Context` field by field, and
it is the only `*Context` in the package constructed outside
`document.build`. So `<Frozen AllowError>` inside an
`<ItemsView.ItemTemplate>` panicked: at LOAD for a non-empty collection,
because `ItemsView.Validate` realizes a throwaway row during `Build`, and
on FIRST SCROLL for one fed by a timer — inside the composer, where
`Screen.Restore` is skipped, so the terminal is left in raw mode with the
alternate screen up and no trace.

The map is per-ROW rather than the page's, which is the decision worth
recording. Rows are realized and discarded as the view scrolls, so a page
map would accumulate an entry per realization and refuse the second row
for aliasing the first — a guard that fires on correct markup the moment a
list is longer than one. The question the guard asks is "does this
document arm one sink twice", and a row is the scope where that question
has an answer.

**`arms.sinks` did not cross the control boundary**, though its own doc
comment promised it did: "a nested Load inherits the outermost map (so two
controls sharing a sink are still caught)". `control()` builds a fresh
child `Context` and propagates `Declared` but not this, so two
`<UserControl>`s could arm the same handle with nothing to notice. Fixed
by propagating it unconditionally — the comment described the intended
behaviour accurately, and the code simply did not have it.

The pattern across rounds four and five is worth naming: **a load-time
guard that writes to shared state inherits every construction site of that
state as a dependency**, and the sites are not all in the file where the
guard lives.

## Round six: one scanner, and the collision row scope left open

Two attributions in the refusals list above pointed at the wrong
sections, and the round that produced the newest guard had no section at
all — the third correction of this kind this record has taken, and the
reason the list now names sections rather than round numbers alone.
A reader following "round three" for the alias guard landed in a section
about something else.

### The alias scan had a second grammar, and it was the wrong one

Round five widened the alias check from `bindRe` to a pair of regexps:
`braceRe` to find each `{{ … }}` and `tokenRe` to rescan it for paths.
`markup/scan.go` had already written down why that cannot work here —
*"a backtick literal may legally contain a brace … has to find the LAST
`}}`, not the first"* — and both halves reproduced:

```
Allow="{{v:Echo `}}` .X}}"    MISSED — built clean, and the priming
                              publish erased the Allow source in Build
Allow="{{v:Echo `.X`}}"       FALSELY REFUSED — a path spelled inside a
                              backtick literal is not a path
```

The fix is not a better pattern. `scanBindings` splits correctly and
returns typed segments, and a call's arguments are already lexed into
tokens, so `allPaths` reads `segPath` segments plus each `segCall`'s
`tokPath` arguments and its `| into` target. `braceRe` and `tokenRe` are
gone, and so is the two-pass reasoning that only existed because a regexp
with one capture group yields one match per expression.

The comment that called the outside-the-braces case **unreachable** went
with them. It argued that only a bound `Allow` reaches the scan, which is
true, and concluded there is never text outside the braces — which does
not follow, because a bound `Allow` may be interpolated:
`Allow="{{.Allow}} .X"` builds today, and its runtime parse failing on
`.X` is the whole point of the attribute. The mutation dropping the brace
requirement was silent for the ordinary reason, a missing test, which is
the reading the comment ruled out. That test exists now.

### A page and a row could arm one sink, and it erased at load

Round five scoped the armed-sink map to the ItemsView row, which is
right: the factory runs per row realization and never unregisters, so a
shared map would accumulate an entry per row and refuse the list's own
second row. What it left is that the row map could not SEE the page's
arms. A `<Frozen>` on the page and a `<Frozen>` in an item template could
arm the same handle with neither guard looking, and the row's priming
publish wrote `""` over the page's live failure **inside `Build`** —
round four's two-writer failure, arriving through round five's fix.

Not exotic: `components/itemsview.go` passes a `*prop.Property[string]`
found in a row map straight through, so a projection handing rows the
page's own handle is an ordinary thing to write, and
`docs/markup-reference.md` tells the author the sink must be page-owned,
which is exactly that shape.

`armScope.outer` splits the two halves: the duplicate CHECK consults
the page's set as well, while REGISTRATION stays row-local. Both
directions are pinned, because the fix has an obvious wrong form — share
one map — that refuses correct markup.

The residual hole is stated rather than closed: two rows of ONE list
sharing one handle through the projection has no load-time signal. The
reference said "because each row's values carry their own handle", which
is an assumption about the projection presented as a property of the
framework. It now says what is enforced and what is not.

**Corrected in round eight: this paragraph gave the wrong reason.** It
said the handles are indistinguishable by the time they reach this
package. They are not — `arms.sinks` and `nestedArms` both key by
`*prop.Property[string]`, so a shared handle is literally the same
pointer, and that is how round eight caught two item *templates* arming
one sink. The reason a two-ROW collision has no load-time signal is that
`ItemsView.Validate` realizes exactly **one** row during the build, so a
second arm on the same handle never occurs while the nested record is
open. The two levers that would close it are therefore concrete: realize
a second row in `Validate`, or record scroll-time arms behind the detach
seam scoped at `:248`. A reader of the old sentence concluded the hole was
closed by nature. Round five retired the identical "pointer identity
cannot catch it" reasoning for the alias guard, and it survived here in a
second file.

## The judgement is made at the end of the build, not at the arm

`armScope.outer` above is the round-six mechanism, and on its own it
is **document-order dependent**. `arms.outer` is the page's *live* map,
so a row that arms BEFORE the page's own `<Frozen>` is built finds it
empty and sees no collision — and `ItemsView.Validate` realizes its
throwaway row during the build, which for a list declared above the
`<Frozen>` is exactly that order. The same page written the other way
round was refused. A guard that depends on which element the author typed
first is not a guard.

So registration and judgement are separated. A nested scope records what
it armed on `armScope.nested`, a `*nestedArms` whose lifetime is the
outermost build's; the page's own arms keep going into `arms.sinks`; and
`document.build` asks `nested.collide(ctx.arms.sinks)` once, after the
whole tree is built and before it returns. By then both halves are
complete, so the answer cannot depend on the order they arrived in.

`nestedArms.record` also **reports** a second nested arm on one sink,
rather than dropping it. Two `<ItemsView>` item templates on one page
arming the same page-owned handle are two NESTED arms, so neither is in
the page's map and `collide` sees nothing — the collision one scope
further out than `arms.outer` reaches. Both land while the record is
open, because each list realizes one probe row during the build, so it is
catchable at load and is.

## An arm is a subscription and a publish, and neither is undoable

`armAllowError` does two irreversible things: it subscribes an observer
to a computed over the `<Frozen>`, and it publishes the current parse
state into the caller's handle. A build that then fails cannot take
either back. That is how a REFUSED page came to erase a live message —
the row's priming publish wrote `""` over the page's failure and the load
error arrived afterwards, so the user lost the message and got a page
that did not load.

`armScope.pending` is the answer: a `*deferredArms` collected during one
build and run only on the line after the last error path.

```go
ctx.arms.pending.arm(func() { armAllowError(f, sink, ctx.Dispatcher) })
```

`arm` is total — it defers if a build is collecting and runs the arm now
if none is — so the call site has one branch and no way to forget the
other half. The carrier is CLOSED in `document.build`'s defer rather than
after `run()`, so a `Context` reused for row realization finds it closed
however the build ended.

## Round nine: a row is a build too

The rule above was stated for the page and had a hole exactly one scope
in. A row is also a build that can fail, and until round nine two kinds
of discarded row left an arm behind:

- **The validation probe.** `ItemsView.Validate` realizes one throwaway
  row during the page build to fail a bad template binding at load, and
  discards it however well it builds. Sharing the page's carrier made its
  arm run whenever the PAGE succeeded — leaving an observer subscribed to
  a computed over a `<Frozen>` nothing holds, and a message in the row's
  handle published by a component that is in no tree.
- **A row refused halfway.** At scroll time the page's carrier is closed,
  so the arm ran WHERE IT WAS BUILT. A
  `<Frozen AllowError>` early in a template and a sibling further down
  that does not resolve leaves the same debris, with the build failing
  immediately afterwards.

The factory now gives each row a carrier of its own, run only when the
row survives its build **and** is not the probe. Telling the probe from a
real row needs no new state: the only factory call that happens while the
page's carrier is still open IS the probe, because every real row is
realized by the composer after `Build` has returned — so
`pagePending.inFlight()` is the question, asked of the flag the design
already keeps.

The probe still RECORDS. `arms.nested.record` and the `collide` check run
where the `<Frozen>` is built, not where the arm runs, so a page-versus-
template collision is still a load error. What the probe no longer does
is subscribe and publish.

`TestARefusedRowArmsNothing` pins both halves, and its second assertion
is the one a value check cannot make: a dropped arm and an arm that
published `""` are the same empty string, so it moves the refused row's
`Allow` afterwards and requires that nothing lands.

## Four fields, three seams, and one of them always missing one

Five review rounds landed in this record and **each of them was the same
shape**: a piece of arm bookkeeping that did not cross one scope
boundary. `armedSinks` did not cross the control boundary. Then
`armedOuter` and the nested record did not cross it either. Then the
pending arms did not. Each was found by a reviewer rather than by a test,
and each was silent for the same reason — a field left nil reads exactly
like a scope that armed nothing, which is also what a correct empty scope
reads like.

Four fields, three seams that carry them (`document.build`,
`control()` in `usercontrol.go`, and the `ItemsView` row factory), and
nothing anywhere saying they belong together. They are one `armScope`
value now, held behind `Context.arms`:

```go
type armScope struct {
	sinks   map[*prop.Property[string]]string
	outer   map[*prop.Property[string]]string
	nested  *nestedArms
	pending *deferredArms
}
```

What that buys is not brevity. `control()` is now `child.arms =
parent.arms`, a single assignment that **cannot** omit a member —
the next field added to the scope crosses the control boundary without
anybody remembering this. `document.build` saves and restores one value
instead of three, and the row factory's divergence — the one place in the
package that constructs a scope rather than inheriting it — is one
literal with all four reasons visible side by side, rather than four
lines a reader has to notice are four.

### The immediate arm was not the runtime path

`if !ctx.armPending.add(arm) { arm() }` had four comments around it — at
the call site, on the field, on the type, and above the capture in
`itemsview.go` — all saying the immediate branch was how an `ItemsView`
row realized after the page build got armed. That stopped being true when
the row factory got a carrier of its own: every `<Frozen AllowError>` this
package builds is now built inside an OPEN carrier, the page's or the
row's. Measured — replacing the whole body of `add` with a bare append
leaves `./markup` green.

The branch stays, and moves into `deferredArms.arm`, because the
alternative for a nil or closed carrier is to drop the arm **silently**,
which is the leak class `armScope.pending` exists to refuse and would be
the failure mode of whatever call site is added next. A total function
states that once instead of asking every call site to remember it. Four
comments describing an unreachable path as the live one is the defect
that was actually costing something, and it is gone.

## Round ten: the guard was per-element, and the erasure is not

`aliasesSink` refuses `<Frozen Allow="{{.X}}" AllowError="{{.X}}">` — one
element cannot publish into the set it just read. It is called with
**this** element's `Allow`, and nothing asked whether the sink is some
*other* `<Frozen>`'s source. It can be, and then the framework
manufactures #424's exact symptom:

```xml
<Frozen Allow="{{.A}}" AllowError="{{.B}}"> … </Frozen>
<Frozen Allow="{{.B}}" AllowError="{{.C}}"> … </Frozen>
```

Measured on the parent commit: `build err = <nil>`, and `B` went
`"Hover"` → `""` during `Build`. `B` is the second element's allow set,
so that subtree is reconfigured behind the author's back — and
`gooey.ParseAllow("")` returns `AllowNone` with a **nil** error, so `C`,
the second element's own failure channel, has nothing to publish. A
subtree sealed to everything, no load error, no runtime message, nothing
on any channel, from a page that spells its guards correctly by every
rule the reference states.

Neither documented "rule the author keeps" covers it. Both are about
**sinks** being shared; this is a correctly unique sink that happens to
be somebody else's source.

`armScope` gains `allows`, recorded for every `<Frozen>` that binds one
— whether or not that element also has an `AllowError`, because the
element that gets erased need not be arming anything itself. The
judgement is `document.build`'s, beside `nested.collide`, and it has to
be there rather than at the arm for the reason round six established:
the sink can be armed **before** the other element binds `Allow` to it,
so an at-the-arm check refuses one document order and accepts the other.
`TestOneFrozensChannelIsNotAnothersAllowSet` asserts both orders, and
asserts the author's set **survives the refusal** — a guard that fires
after the priming publish has run would report the defect having already
caused it, on a `Context` that outlives the failed load.

`TestTwoFrozensMayShareAnAllowSet` is the must-load half: two subtrees
*reading* one set is an ordinary page, and without that arm the guard is
satisfied by refusing every second `<Frozen>`.

**The gap this leaves, named rather than left to be found.** `allows` is
row-local in the `ItemsView` factory for the same reason `sinks` is, so
a row whose `Allow` set is the page's failure channel is not judged. The
sink direction of page-versus-row *is* covered, through `outer` and the
nested record. Closing the other direction means a second nested record,
and it is not built here.

`allPaths`' `| into` branch is now described as defensive rather than as
live protection. A value expression carrying `| into` is a load error
raised by the `BoundText` call above the `AllowError` block, so the
guard never sees one; `TestAllPathsReadsTheCallsIntoTarget` calls
`allPaths` directly, which is why the claim read as pinned. Same class
as this branch's own headline fix.
