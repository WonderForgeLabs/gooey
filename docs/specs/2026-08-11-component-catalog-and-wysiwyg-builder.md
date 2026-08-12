# The component catalog, and a WYSIWYG builder on top of it (design)

Two pieces of work that arrived together and are recorded together, in
the order they should land. The first is a framework correctness fix that
stands on its own. The second is the reason it got written, and is also
its first real consumer — which is the correct order of those two facts,
not the order the work was requested in.

## Thesis: a check that reports success without having checked

Everything below is a variation on one failure, and it is worth stating
before the details rather than after, because it is what the work is
actually about.

**A silent drop is a check that did not happen. A vacuous test is a check
that did not happen. They are the same defect at two altitudes.**

The originating bug is `applyLayout` accepting an attribute nobody reads
— the loader *appearing* to validate while validating nothing. Every
mitigation in this document is an attempt to make that impossible, and
the recurring discovery is that **the mitigations have the same failure
mode as the thing they mitigate**: a declared vocabulary that
over-declares passes every test; a drift test whose probe renders nothing
passes; a corpus test whose corpus excludes the failing shape passes; a
guard implemented twice cannot be deleted and therefore cannot be
verified.

Nine instances were found in the course of this work, in four distinct
mechanisms, and the tally is the argument — one would be an anecdote:

| # | The check | Why it reported success |
|---|---|---|
| 1 | `applyLayout`'s attribute loop | No `default:` arm — the originating defect |
| 2 | Over-declared `ElementDef` attributes | Nothing reads them, so nothing rejects them; **the entire suite passes** |
| 3 | A transport barrier test | Passed with the barrier **deleted** — `act()` already blocked |
| 4 | `TestEveryGooeyFileInTheRepoHasValidAttributes` | 626 elements, 37 files, and a fragment is never a file |
| 5 | A `VStack` overflow repro | Measured and arranged into **the same rect**, where the cached size and the clamped total coincide |
| 6 | An A/B of two builds | `git stash push` **silently no-ops** on a committed file, so both arms were the same build |
| 7 | `TestDeclaredDefaultsRenderIdenticallyToOmission` | Passed 13/13 while the probe **rendered nothing** |
| 8 | A `Frozen` wheel test | Events **bubble**, so "the host received it" is true with or without the retarget |
| 9 | The editor's minimum-size red test | **Skipped** — measured one row below the wrong minimum of two |

Four mechanisms, one shape: *the check ran, the check reported success,
and the check never touched the fact.* None is detectable by reading,
because a test that cannot fail and a test that passed are identical from
outside — same name, same green, same duration.

The separators are per-mechanism, which is why "be careful" is not the
lesson:

- **Delete the guard and watch it fail.** Catches 1, 3. Requires a
  SINGLE seam: a guard implemented in two places cannot be deleted, and
  #8's sibling — the `Frozen` retarget written in both `target` and
  `DispatchMouse` — stayed green with either half removed. *Redundant
  implementations make a guard un-deletable, which makes it
  unverifiable.* That is an argument for consolidation on grounds nobody
  usually cites.
- **Name the class your corpus collects, not its size.** Catches 4.
- **Vary the setup, not the arity.** Catches 5.
- **Assert the arms differ before trusting that they agree.** Catches 6,
  8, and is the general form of 7.
- **Pair every equivalence assertion with a discrimination assertion.**
  Catches 7, and is the only one that can be made structural.
- **A skip is not a pass.** Catches 9 — and that one went further: the
  skip revealed there were two different minimums with two different
  mechanisms, and that the explanation had welded one to the other.

Instance 9 is the one worth dwelling on, because it says something about
*which instrument* to reach for. The red test was not checking the fix;
it was checking the **explanation of the failure**, and the explanation
was what was wrong.

Nothing in review could have caught it. The comment and the code agreed
with each other perfectly — and agreement is what review is good at
verifying. What was wrong was the relationship between the model and the
system: the number described was not the number the code used.

**Review checks coherence. Tests check correspondence.** That is not a
criticism of reviewers; it is a statement about what the two instruments
measure, and it tells you when to spend which. Review catches
contradiction. Execution catches misfit. **A change with no internal
contradiction and a wrong model is precisely the shape that passes review
and fails in production** — and it is the same failure as the other eight,
in the other instrument: eight tests that never touched the system, and
one review that only ever examined the change against itself.

Which is also the argument for red-first that usually goes unmade. It is
sold as "write the test before the code", but the load-bearing part is
**write down what you think breaks, then make the machine disagree with
you.**

Two corollaries earned the hard way. **A control that fails is not always
a broken test — sometimes it is telling you the claim is wrong**: the
`Frozen` KeyBinding test passed frozen *and* unfrozen, which was the
evidence that the guarantee, not the test, was misstated. And **a
negative assertion needs a control proving the harness could have seen
the positive**: "no process spawned" means nothing against a harness that
could not observe a spawn either way.

## The defect

`applyLayout` (`markup/markup.go`) switches on the attribute key and has
**no `default:` arm**. An unrecognized attribute falls out of the loop
without being read, without being rejected, and without being mentioned.

The consequence anybody actually hits:

```xml
<Text Left="10" Top="3">BARE</Text>          <!-- lands at 0,0 -->
<Text Canvas.Left="10" Canvas.Top="5">OK</Text>
```

`Canvas.Left` and `Canvas.Top` are attached properties and the dotted
spelling is the only spelling. Bare `Left`/`Top` are accepted, ignored,
and the element sits at the origin. There is no error at load, no error
at build, and nothing on screen to explain it — the only way to detect it
is to snapshot the tree and notice that the element has no `layout` key.

This is not confined to layout. Most of the element switch reads
attributes by name and simply never looks at the ones it does not
recognize, so a misspelled `Conten=` on a `<Button>` produces an empty
button rather than a load error.

Exactly three elements do better, and they are the proof that the fix is
cheap rather than the exception that excuses the rest: `<Companion>`
(`companionAttrs` + `checkCompanionAttrs`), the `<Var>` inside
`<Companion.Env>`, and `<Validate>` (`validateBuiltins` plus a rejection
loop). Each declares its vocabulary, each rejects an unknown attribute,
and each names the alternatives in the error.

They look at first like the hard cases for a generator, since none of
them reads attributes by name. They are better understood as the
**reference** cases: they are what every element looks like once
`checkCompanionAttrs` generalizes, and the generator reads their tables
directly rather than deriving anything.

**The catalog exists so that the other thirty-odd elements can do the
same.** Generalizing `checkCompanionAttrs` to the whole vocabulary is the
deliverable. A palette for a UI builder is the second customer.

That ordering matters beyond credit-assignment. It decides where the code
lives (`markup`, not a builder-local package), it decides that the work
is worth landing even if the builder never ships, and it makes the change
defensible to a reviewer who does not want a UI builder in the first
place.

## Why this is a fourth question, not an extension of an existing one

`docs/specs/2026-08-11-plugins-as-standalone-activities.md` records three
introspection questions and insists on naming which one is being asked:

1. **What is this binary linked with?** — the module registry. Link time.
   Note that this is **spec text only**: `RegisterModule`, `Modules` and
   `LinkedModules` exist in that document and in no `.go` file.
2. **What is bindable right now?** — the markup `Context`. Runtime.
3. **What is rendering right now?** — the tree, via `tree_snapshot`.

None of them answers *what element types exist and what can I set on
them*. The tree can only describe elements somebody already wrote; asking
it what is available is like asking a running program what it could have
been. The `Context` describes values, not the vocabulary that consumes
them. And the module registry answers a link-time question at the grain
of modules, which is the wrong grain and the wrong lifetime — it was
considered as a home for this and rejected on both counts.

`ControlService.GetDeclaredSchema` and `markup.Declarations` are
sometimes mistaken for a catalog. They are not: they read **one
document's** `<x:Property>` block — the contract a values payload must
satisfy to build that control. That is a per-control schema, and it turns
out to be exactly the right shape for one third of the catalog (below),
but it says nothing about `<Button>`.

## Declared, not generated

Each element states its own vocabulary, in one literal that also contains
the code reading it:

```go
var defButton = &ElementDef{
    Name:  "Button",
    Proto: &components.Button{},
    Attrs: []AttrSpec{
        {Name: "Chrome",  Kind: KindEnum,    Enum: components.ButtonChromeNames},
        {Name: "Click",   Kind: KindCommand, Binds: BindsEither},
        {Name: "Content", Kind: KindText,    Binds: BindsEither},
        {Name: "Style",   Kind: KindStyle,   Binds: BindsEither},
    },
    Children: ChildSpec{Mode: ModeAttachments},
    Build: func(e Element, ctx *Context) (gooey.Component, error) { … },
}
```

`buildComponent`'s 537-line switch is gone; the registry replaced it.

### How this was arrived at, because the route matters

The first answer was a **generator**: derive the vocabulary from the
switch with `go/ast`, commit the table. It worked, and this document
argued for it at length on the grounds that a hand-maintained table
drifts and that "the drift test and the generator are the same program".

That argument was half right, and the half that was wrong is the
interesting one. **The generator's value was never the table — it was
the drift check.** Once the vocabulary is declared beside its reader,
the table needs no deriving, and only the check needs keeping.

**The measurement that settled it.** A declared vocabulary can be wrong
two ways, and they are not symmetric. Both were simulated against the
real tree:

- **Under-declared** — the code reads an attribute the literal omits.
  Deleting `Content` from `<Button>` failed three tests, one naming the
  exact file (`cmd/statedemo/statedemo.gooey`). Loud, because unknown
  attributes are rejected.
- **Over-declared** — the literal permits an attribute nothing reads.
  Adding `Caption` to `<Button>`: **the entire suite passed.** Silent.
  And `<Button Caption="x">` would then be accepted and ignored, which
  is the silent-drop defect this whole document exists to delete,
  reintroduced through the mechanism meant to prevent it.

Only the extractor caught the second. So it survives — **demoted from
generator to cross-check test**, which is the form that keeps the guard
and drops everything that made it fragile: the bootstrap constraint, the
stdlib source importer, the four-false-positive blocklist, the committed
artifact, `//go:generate`. It shrank from ~1,100 lines to 386, because
kinds, enums and child modes no longer need deriving — they are
declared.

### The generator was also inaccurate, which is a stronger objection

Recorded because it changes the argument's category. A build step that
is merely awkward is a trade; one that silently misreports is a defect.

`<Validate>` was wrong under the generator **twice, for two different
reasons**:

1. `NonVisual: false`. Its Go type is declared inside `markup`, so the
   component literal is a bare `&Validate{}` with no package qualifier;
   the extractor found no type and every axis defaulted to false.
2. The fix for (1) — reading the method set from the AST — was **also
   wrong**. An AST method scan sees only *directly declared* methods, and
   `Validate` gets `Attach` and `LayoutProps` promoted from an embedded
   `gooey.Base`. So the catalog reported `Attaches: false,
   HasLayout: false` for an element that has both.

This is the fourth instance of *the extractor reports what it can see and
cannot know it is blind*, and the first where the blindness was in the
type system rather than the control flow. Pair it with the earlier
finding that `HasLayout` is true for every built-in *because of* the same
embedded `Base`: one mechanism, opposite consequences — there it made
everything look true, here it made two things look false.

It was found by writing the axes test with the generator's values as the
expected result and watching it fail, which is what makes it a
measurement rather than a lucky catch.

### Axes are derived from a Proto, not declared

`ElementDef` declares `Attrs`, `Slots`, `Children` and `Open` — the
things only the arm's author knows. It does **not** declare
`NonVisual`/`Focusable`/`Attaches`/`HasLayout`. Those come from `Proto`,
a zero-valued instance sitting beside `Build`, by type assertion against
the framework's marker interfaces.

A hand-written `NonVisual: true` would be a second copy of a fact the
type already carries, and the second copy is the one that goes stale —
the same principle this restructure is built on, applied one level up.

**A type assertion is not reflection**, and this needs saying because
invariant #1 will be raised against it. It is the mechanism `boundProp`
and every markup type-switch already use; it asks the framework's own
question of the framework's own interfaces; and nothing is discovered at
runtime that was not fixed at compile time. What it does not do is what
`go/types` at build time could not do either way: see through embedded
promotion correctly without a full type checker.

### Why colocation rather than a table beside the switch

The smaller diff was available: leave the switch alone, add a table per
element, have the checks read it. It was rejected on evidence rather
than taste.

`companionAttrs` has been a parallel declaration that COULD drift for as
long as it has existed, and it never has — because it is nine names
sitting directly above the code that reads them. **Proximity, not
discipline, is the mechanism.** Put the same table in another file and
it rots; that is the whole argument for paying 537 lines to colocate.

The two elements that needed the checker's escape hatch make the point
from the other end. `<StatusBar>` and `<Validate>` consume attributes by
*ranging* over a table rather than reading them by name, so no read names
a literal and the cross-check cannot follow them. Both are also the
elements that already had a declared table before any of this existed —
`statusSections` and `validateBuiltins`. **Ranging is why they needed
one.** The elements that cannot be read by name are exactly the elements
that had to declare.

Their escape is `DynamicAttrs`, carrying its reason, with
`TestDynamicAttrElementsAreExactlyTheseOnes` pinning the set at exactly
those two — because an element that acquires it silently stops being
guarded in the one direction rejection cannot see.

### A simplification that rode along

Naming moved into the dispatcher: `Build` returns the raw component and
`named(e, ctx, …)` is applied once, removing 31 repetitions. Worth
recording as evidence rather than trivia — a shape that lets you delete
a line from every element is a shape that fits the problem, rather than
one imposed on it.

### What the arms looked like, and how they moved

The bodies moved verbatim. The vocabulary rows were seeded from the
generator's final output, which had been validated against 626 elements
of real markup — so the initial declaration was **known correct at the
moment it was written** rather than hand-derived and hoped for. The
mechanical half was done by a script that could not invent an attribute
row; the judgement half was already done.

### The universal attributes, and why a per-arm walk cannot see them

`<Button>` reports `Chrome`, `Click`, `Content`, `Style`. A person using
a visual builder will spend most of their time on attributes that appear
in no arm of the switch at all:

- **`Name`** — applied by `named()`. It is the identity `patch_markup`
  addresses by, which for an incremental editor is not one attribute
  among many but the thing that makes incremental editing possible.
- **`Tooltip`** — applied by `applyTooltipShorthand`.
- **The layout set** — `Width`, `Height`, `Margin`, `HAlign`, `VAlign`,
  `Visibility`, plus every `Grid.*` and `Canvas.*` — applied by
  `applyLayout`.

All three run for *every* element satisfying `gooey.HasLayout`, beside
the switch rather than inside it, so a per-arm walk is structurally blind
to them.

**This is the same blindness as the `leftovers` bug, one level up.**
There it was an unclassified attribute inside a helper; here it is an
entire attribute *set* applied outside the switch. Twice the extractor
has been confidently wrong in the same shape: it reports what the arm
says, and cannot see what the arm does not do. That is the failure mode
to design against, and it is why the guards are structured to fail loudly
rather than to under-report quietly.

They are derived, not hand-listed — `named` and `applyTooltipShorthand`
go through the ordinary arm machinery, and `applyLayout`'s switch
supplies the rest along with the enum spellings, read out of `parseAlign`
and `parseVisibility`. `Name` is the one row emitted explicitly, because
it is an identity rather than a settable property and no binding form
classifies it; the generator asserts `named()` still reads it and fails
if that stops being true, so it stays a derivation rather than a
hardcoded row that could outlive the code it describes.

### Attached properties belong to the parent, not the element

`Canvas.Left` is valid on a child of a `<Canvas>` and meaningless
anywhere else, where `applyLayout`'s missing `default:` arm discards it
without a word.

A flat attribute list per element **cannot state this**, and a catalog
that could not state it would reproduce the exact defect it was written
to delete: a builder offering `Canvas.Left` on a child of a `<VStack>` is
promising positioning that will never happen. So the shape carries three
pieces, not one:

```go
func UniversalAttrs() []AttrSpec            // if HasLayout
func AttachedAttrs(parent string) []AttrSpec // contributed BY a parent
func AttrsFor(e ElementSpec, parent string) []AttrSpec // the join
```

Consumers call `AttrsFor`. Reading `ElementSpec.Attrs` directly yields a
true statement about the element and a misleading answer to the question
actually being asked.

The parent scoping is derived rather than configured: a case of
`"Grid.Row"` in `applyLayout` says in the attribute name itself that a
`<Grid>` parent contributes it. Splitting on the dot is not a convention
this generator invents — it is the syntax markup already uses, and the
prefix is always an element name.

This also **reverses an earlier conclusion in this document's own
drafting**. `Attaches` and `HasLayout` were dismissed as useless palette
signals because both are true for every built-in. That holds for
`Attaches`. It is wrong for `HasLayout`, which is not a discriminator
between built-ins but **the join key onto the universal table** — and
position and size are the primary interaction in a visual editor.

### The behavioural axes: how they were got, and why that changed

**Superseded — kept because the reasoning is why the replacement is
right.** Whether a component is non-visual, can take focus, can host
attachments or has a layout appears nowhere in the vocabulary. It is
interface satisfaction on the component type.

The generator answered it with `go/types` at build time, on the argument
that invariant #1 bans asking at *runtime* but not at *generate* time.
That argument holds. It was abandoned for a different reason: **the
answers were wrong**, twice on `<Validate>`, in the way documented above
— and wrong silently, because the failure mode is all-false, which reads
exactly like a plain element.

They are now derived at runtime from `ElementDef.Proto` by type
assertion, which sees through embedded promotion because it asks the
same question the framework asks. Two caveats survive the change intact:

- `Focusable` is `AcceptsFocus() bool`, so the catalog's claim is that
  the type *can* be a focus stop. The instance decides at runtime. That
  type-level/instance-level split is the same one separating the catalog
  from the tree, and it should stay visible.
- `Attaches` is true for every built-in via the embedded `gooey.Base`, so
  it is not a discriminator among built-ins and a palette must not key on
  it. `HasLayout` is true everywhere for the same reason but is **not**
  in the same position — it is the join key onto the universal attribute
  table, via `TakesLayout`. The two look alike and are not.

### Why the extractor survives as a test — measured

A second, cheaper over-declaration guard exists and was invented
independently for `<Gooey>`'s page settings: **set every declared
attribute to an absurd value and require an error.** Nothing reads it,
no error, test fails. It needs no AST machinery at all.

It does not replace the extractor, and the reason is a number rather
than a preference. Counted across the 124 attribute rows in the element
vocabulary, by the kinds for which no absurd value exists:

| rows | kind | why no absurd value |
|---|---|---|
| 21 | `KindString` | accepts arbitrary text |
| 18 | `KindStyle` | an unknown style renders **unstyled, with no error** |
| 10 | `KindText` | accepts arbitrary text, literal or binding |
| 1 | `KindIdentity` | any name is a legal name |

**51 of 124 — 41% — are unreachable by an absurd-value guard.** It
covers 59% of the vocabulary; the extractor covers 100%.

Two things make this decisive rather than merely quantitative. The
unreachable rows are **exactly where casual over-declaration happens** —
someone adding a text attribute to a table is the likely mistake, and a
number attribute is not. And `KindStyle` is the sharpest case: it is
unguardable by absurd value *and* silent at runtime, so an over-declared
style attribute is invisible from both directions at once.

So the two are complementary. Absurd-value is right where no extractor
exists (`<Gooey>`'s settings, which have no switch to walk). The
extractor is what covers the element vocabulary, and it earns its keep
on a measurement rather than on the argument that generated tables
cannot drift.

### The escape hatch, then and now

The generator had three annotations — `//gooey:catalog-opaque`,
`-attrs`, `-open` — on the reasoning that "an arm the extractor cannot
explain is a build failure" is the strongest rule in the design and, with
no escape, also a trap: the next person needing a novel shape either
contorts their code or loosens the extractor, and loosening it is a
one-line diff that silently degrades every future entry.

Declaration retires all three. There is nothing to explain: an element
states its vocabulary, so `opaque` becomes `Known: false`, `attrs`
becomes the literal itself, and `open` becomes a field.

**One escape survives, in a better-motivated form.** The cross-check
still cannot follow an element that consumes attributes by *ranging*
rather than by name, so `DynamicAttrs` skips it and carries the reason.
`TestDynamicAttrElementsAreExactlyTheseOnes` pins the set at exactly
`{StatusBar, Validate}` — an exact-set assertion rather than a count,
because an element that acquires the field silently stops being guarded
in the one direction rejection cannot see. An escape nobody counts is an
escape that spreads.

### What the generator got wrong first (history)

Recorded because all three are traps for whoever moves this into
`gooey gen`, and because the first two produced *confident, plausible,
entirely wrong* output rather than an obvious failure.

**Following `build*` calls by prefix walks back into the switch.**
`buildChildren` → `build` → `buildComponent` returns to the very switch
being analyzed, and every arm inherits every other arm's attributes. The
first run reported 17 of 30 arms "fully explained", with `<Grid>`
claiming `Chrome` and `<Canvas>` claiming `Checked`. The right predicate
is not the name: **follow any package-local function the arm passes the
Element to**, because passing `e` is exactly what gives a helper the
ability to read attributes.

**The leftover check must cover every body the scan walked, not the case
clause.** Checking only the arm makes an unclassified attribute inside a
helper *invisible* rather than an error. `<Companion>` came out claiming
`AttrsKnown: true` while missing `Dir`, `Path` and `Log`, with no Go type
and the wrong child rule. The guard against silent under-reporting was
itself silently under-reporting.

**Locals must be bound in source order, during the walk.** `<TextBox>`
writes `if a, ok := e.Attrs["AccentStyle"]` and then
`if a, ok := e.Attrs["InvalidStyle"]`; a pre-pass over the body lets the
second binding win for both, and `AccentStyle` vanishes. `ast.Inspect` is
pre-order, so binding at the assignment is correct.

**`strings.TrimSpace` must be transparent, not a type signal.** Treating
it as one reports `<Companion>` `Name` and `<Image>` `Src` as durations.

**And the blocklist itself needs a guard that can fail.** Checking only
the helpers already known to be re-entrant is the failure mode this
generator exists to avoid, so `reachesTheSwitch` walks the call graph and
errors on any unblocked helper that routes back into `buildComponent`.
Written naively it reported four false positives — `buildTabs`,
`buildItemsView`, `buildStatusBar`, `buildImage` — which reach the switch
only through the already-blocked `build()`. **The guard has to model the
walk, not an idealized call graph**: a path through a blocked callee is
not a path the walk can take. Two tests pinned both directions —
**and both were deleted with the generator**, along with the blocklist
they guarded. The cross-check that replaced it has no call-graph walk to
get wrong, so there is nothing left for them to protect. Recorded rather
than quietly dropped: a reader of this history should not go looking for
them.

Erosion pressure has now arrived from **both** sides: too lax (the
`leftovers` under-report) and too strict (four false positives inviting a
loosened rule). They reach the same outcome by opposite routes — a guard
nobody trusts — which is why the fixture carries a direct caller, a
two-hop caller, a `build()`-only caller and a leaf rather than just the
hazard case.

## The shape: provenance and knowability are different questions

The first draft carried one `Origin` field with three values —
`builtin`, `registered`, `include`. Review caught that this conflates two
axes that only *happen* to correlate:

- **provenance**: where the element came from;
- **knowability**: whether its attributes can be enumerated at all.

They correlate today only because `Context.Components` holds an opaque
`Builder` func. If registration ever grows a schema — a natural thing to
want, and cheap — then `registered` becomes knowable, and every consumer
that keyed on `Origin == registered` to render the degraded case is
silently wrong.

So:

```go
Origin     Origin  // provenance
AttrsKnown bool    // is Attrs exhaustive, or merely what we could see
```

**An empty list that means "none" and an empty list that means "unknown"
must not be the same value.** A consumer keys on `AttrsKnown`, never on
`Origin`.

The rule generalized further than expected during implementation: an
opaque element cannot know its **child rule** either, so `ModeUnknown`
exists beside `ModeNone` and `ModeLeaf`, and the opacity test asserts
that an opaque element never claims a real child mode.

### Three lifetimes, one list

`Context.Catalog()` returns a single tagged list rather than three calls,
because two calls would force every client to redo the union and the
union is where correctness lives.

- **builtin** — the switch. Fixed when `markup` compiles.
- **registered** — `Context.Components`. Per-process, and a **name and
  nothing else**.
- **include** — `Context.Includes`; `<Card/>` resolves `card.gooey`.
  Per-process and *fully* knowable, because a control's `<x:Property>`
  declarations are its public surface. `markup.Declaration` maps onto the
  catalog's attribute record one-to-one.

`TestCatalogUnionTagsEachSourceHonestly` walks all three in one call.

### A builtin element can still have a per-context attribute set

This broke the tidy story and is worth stating plainly, because the
obvious design — tag the lifetime on the element — is wrong.

`<Validate>`'s vocabulary is `validateBuiltins` **∪** `Context.Rules`. An
app that registers an `Email` rule can write `<Validate Email="true"/>`.
So the seam between compile-time and per-app runs *through* a builtin
element, not around it, and the lifetime tag has to be available **per
attribute**. `<Validate>` carries `Open: true`, its builtin rules with
`Origin: OriginBuiltin`, and `Context.Rules` entries appended at
`Catalog()` time with `Origin: OriginRegistered` —
`TestValidateIsOpenAndAdoptsContextRules`.

This is not a new mechanism. `validateRuleNames(ctx)` already builds
exactly this union to produce its error message; the catalog generalizes
a pattern that exists in three places rather than introducing one.

**The consequence is larger than a per-attribute tag: there is no static
catalog.** There is only a catalog resolved against a context.
`<Validate>` in an app registering an `Email` rule genuinely has a
different attribute set from the same element in an app that does not. A
purely static table would not be conservatively incomplete — it would be
**wrong, and confidently so**, which is the failure this whole design is
organized against.

So: **the generated table is a cache of the context-independent part, not
the API shape.** That distinction must not surface in the protocol. There
is one call, it takes a context, and it returns the resolved answer.

That also settles the one-call-versus-two question by overdetermining it,
and retroactively strengthens a decision made for a different reason.
Serving from the running app makes the catalog context-resolved **by
construction** — a client physically cannot be handed the unresolved
table. The transport choice was made because the registered and include
halves only exist in the app; it survives the discovery that the builtin
half is context-dependent too. A decision that outlives a change in its
own premise is worth marking as such.

### How many elements are context-dependent — measured, not assumed

"`<Validate>` is the only element whose vocabulary depends on the
context" was an **assumption** in earlier drafts of this document. It got
there by promotion: the `validateRuleNames` finding established that
`<Validate>` *is* context-dependent, and that quietly became *uniquely*
so. Nobody had counted.

Counted now, across all 31 builtin arms. The question is precise —
whether the set of **accepted attribute NAMES** depends on the context,
not whether the arm uses `ctx` at all (nearly every arm does, to resolve
values through `BindingValue`, `Styles` or `Command`; that is value
resolution, not vocabulary).

Two searches answer it: every site that iterates `e.Attrs` rather than
reading names literally, and every site that consults `ctx` to decide
whether a name is acceptable.

**Exactly one: `<Validate>`** (`markup/validate.go:142`, `ctx.Rules[name]`).

Everything else that iterates `e.Attrs` decides against a *static* table
or does something other than vocabulary: `checkCompanionAttrs` and
`<Var>` (fixed tables), `applyLayout` (a switch), `parseDeclaration`
(`declAttrs`), and `mentions` in `itemsview.go`, which scans attribute
*values* for a template reference and never looks at names at all.

So the claim is now "exactly one of 31, measured" rather than "the only
one". That matters beyond tidiness: had a second existed, the claim would
have changed from *one builtin is the exception* to *the seam runs
through element vocabularies generally* — which would make `Open` the
common case rather than an edge case, and change how a palette should
present attribute lists by default.

Two other context-dependent categories exist and are **not** builtin
arms, so they do not change the count, but a consumer must handle them:

- **Include controls.** Vocabulary is the control's own `<x:Property>`
  declarations, enforced by `declarations.checkAttrs`. Per-document and
  fully knowable — already `OriginInclude`.
- **Registered components.** Unknowable, because `Builder` is a func —
  already `AttrsKnown: false`.

### Open is a third state, not a variant of AttrsKnown

`Open` and `AttrsKnown` are independent, and neither is derivable from
the other:

| | example | what a consumer must do |
|---|---|---|
| closed, known | `<Button>` | present the list as complete |
| **open**, enumerable here | `<Validate>` | present the list, say more may exist |
| unknown | a `registered` component | present the name, claim nothing |

For `<Validate>` the enumeration *is* exhaustive for this context — it
just is not exhaustive across contexts. Collapsing that into
`AttrsKnown: false` would understate what the app can tell you;
collapsing it into `AttrsKnown: true` would overstate it.

### Served, not generated into the client

The catalog is served over the control plane. The reason is the split
above: the `registered` and `include` halves exist only in the running
app, so a client-side catalog would offer elements the target cannot
build and hide the ones it can — which is the exact failure the design is
trying to prevent.

It also composes with what already works: **the catalog proposes,
`validate_markup` disposes.** `validate_markup` runs the real
parse-and-bind path against the live context without touching the app, so
a generation loop can be wrong cheaply and never flicker the page.

Phase 1 needs **no proto change**. The MCP server holds a
`*control.Service` directly and every tool is a thin adapter over it, so
`control.Service.Catalog()` plus one MCP tool touches neither `proto/`
nor `grpc/`. A gRPC `GetCatalog` follows when a non-Go client wants one,
with the shape settled by then instead of guessed at now.

## Decision: unknown attributes are rejected

Not a later phase, and not optional. `checkAttrs` runs beside
`checkProps` and refuses an attribute the element cannot accept.

**This is a behavior change and it breaks pages.** Markup that relied on
an attribute being ignored now fails to load. That cost was accepted
knowingly rather than overlooked: the alternative is a defect class that
cannot be debugged from anything the user can see, and which trains
people to distrust their own eyes rather than file a bug. Three elements
already behaved this way, so the change makes the vocabulary consistent
rather than introducing a new kind of strictness.

**The error names the fix, not just the fault.** `Left` and `Canvas.Left`
differ by a prefix; an error that only says "unknown attribute Left"
wastes the entire point when the answer is four characters away and the
vocabulary is already in hand. So a near-miss is offered — suffix match
first, since the motivating case differs by a prefix rather than a
letter, then edit distance capped at two.

A correctly-spelled attached property in the wrong place gets a
*different* sentence, because it is a different mistake:

```
markup: <Text Left="10">: no such attribute; did you mean Canvas.Left?
        (it is contributed by a <Canvas> parent)

markup: <Text Canvas.Left="10">: Canvas.Left is contributed by a <Canvas>
        parent, but this element's parent is <VStack>; it would be
        ignored here
```

Two rules fell out of turning it on, both of which are the catalog being
corrected by reality:

- **An open element checks itself.** `<Validate>` knows the live rule
  vocabulary including `Context.Rules`, which is strictly better than a
  generic near-miss, so the generic check must not run instead of it.
- **Non-visual elements take no layout attributes.** `HasLayout` is true
  for every built-in because it comes from the embedded `gooey.Base`, but
  a `<Timer>` has no bounds to size. The type-level fact and the semantic
  one come apart, and only the second is the right question here —
  `companionAttrs` had already reached this conclusion and omits them
  deliberately. This is the `Focusable` type-level/instance-level split
  arriving on a different axis.

### The completeness measurement, and what it caught

The generator's guard catches attributes it can *see* and fails to
classify. It is structurally unable to catch one it never looks at. That
is a limitation, not a subtlety, and enforcement is what closes it: once
an unknown attribute is a load error, every attribute the catalog missed
becomes a failure against real markup.

**It caught two on the first run**, both invisible to the guard and both
silently absent from the catalog until enforcement existed:

- **`<Image>` `Cols` and `Rows`** — read through `cellCount(e, ctx,
  "Cols")`, which names its attribute with a *parameter*. `e.Attrs[attr]`
  could not be resolved, so the read landed in neither the classified set
  nor the leftover set. Invisible *and* unrecorded, which is the worst
  combination.
- **`<StatusBar>` `Left`, `Center`, `Right`** — consumed by ranging over
  `statusSections` rather than read by name.

The first produced a general fix rather than a patch: when following a
call, seed the callee's parameters from the string literals passed to it,
so an attribute named through a parameter resolves. That subsumes the
hand-written `optDuration`/`bindColor` cases, which are the same shape
written longhand. The second took `//gooey:catalog-attrs statusSections`
— another table that already existed.

It also caught a subtler misstatement: `<Image Cols>` was reported as
binding-only because only the binding branch used a recognized form,
which would have made a builder refuse to write the literal `Cols="6"`
that works. Attributes read through both a binding and a literal form,
and those guarded by the `bindRe.MatchString` idiom, now report
`BindsEither`.

`TestEveryGooeyFileInTheRepoHasValidAttributes` is the standing
measurement: **626 elements across 37 `.gooey` files**, all clean once
the two gaps above were closed. That corpus matters more than the unit
tests, because it is the markup nobody wrote a test for — loaded at
runtime by the demos, where a missed attribute would otherwise surface as
a demo that no longer starts.

A clean result would be suspicious on its own. The evidence that the
check has teeth is that it failed loudly on `<Image>` and `<StatusBar>`
before those were fixed, and `TestUnknownAttributeIsALoadError` pins six
cases that must stay rejected.

## The builder

### It attaches over the bidirectional stream

**This supersedes an earlier conclusion in this document that the builder
should be an MCP client.** That reasoning was sound and is preserved
below, because it correctly eliminated the Temporal path; it simply
predated the channel being named. The editor is an **`Attach` plugin**.

`SessionService.Attach` is a bidirectional stream: subscribe, write an
`Act` over the same stream, and observe the change return as a subscribed
delta. No task queue, no at-least-once retries, no worker registry to
diverge.

Measured against a live app with a throwaway client, because every claim
in this section arrived by relay rather than from source, and relayed
claims in this project have needed correction three times:

- **The delta lands at 1.73ms and its own `ActResult` at 1.74ms** — the
  delta arrives BEFORE the result that acknowledges it. That is stronger
  than "one frame of latency": a client that simply waits for its
  `ActResult` has already been handed the change, so no settle barrier is
  needed.
- **An all-defaults `Subscription{}` really is write-only.** An act on
  one returns its result and no `FrameDelta` at all.
- **The `names` filter does not leak.** Subscribed filtered to one
  property, wrote a different one on the same stream, saw nothing.
- **A failed patch is an error, not a crash** — the app survived a
  `NotFound`, which matters for an editor that will generate invalid
  addresses routinely.

This does not violate the trust argument that rejected a direct channel
in the plugin spec. That argument was against **gooey dialing the
plugin**, which revives the launcher, the handshake, address resolution
and the NAT story. `Attach` runs the other way: **the plugin dials the
app.** Nothing launches it, nothing resolves its address, and there is no
inbound path to it. The property the Temporal indirection was protecting
survives intact.

The trade, stated plainly, because both paths remain correct for
different work:

| | Temporal plugin | Attach plugin |
|---|---|---|
| latency | task queue + heartbeat | one frame |
| delivery | at-least-once, durable | in-session, best effort |
| UI access | none — returns a value | properties, swap — **not patch** |
| trust | sandboxed by construction | holds the control plane |
| failure | stale workers, registry divergence | dropped at 256 queued messages |

**Temporal for durable domain compute you do not fully trust; Attach for
interactive plugins that drive the UI.** An editor is squarely the
second, and needs no Temporal for itself.

### `Attach` cannot patch, and the editor is a patching tool

The `Act` oneof carries exactly seven things — `set_property`,
`invoke_command`, `send_keys`, `send_pointer`, `set_focus`,
`swap_markup`, `register_properties`. **`patch_markup` is not among
them.** It exists only on the unary `ControlService`.

This was relayed as "full UI access: properties, patch, swap" and is
wrong. It matters more than a missing convenience, because the editor's
entire loop is patches and the caret mitigation above is a statement
about patching. Three ways out, none free:

1. **Attach stream plus unary `PatchMarkup`.** Works today, and the
   ordering question has since been answered from source: **there is no
   guarantee.** Acts on one stream are strictly ordered — the reader
   loop applies act N fully, through the settle barrier, before reading
   act N+1 — and every operation is individually atomic on the UI
   goroutine. But a unary call posts from its own gRPC handler goroutine
   while acts post from the session's reader goroutine; two independent
   goroutines racing `Dispatcher.Post`, neither able to observe the
   other. **A unary `PatchMarkup` can land between any two acts.**
2. **`swap_markup` per edit.** Available as an act, and far worse than
   slow: it re-runs the whole page's parse-and-bind, so focus, caret,
   and **every `Name=` in the tree** are reassigned on every keystroke.
   The measured patch cases pin that failure for one subtree; swap is
   the same failure unconditionally and everywhere at once. Note what it
   breaks: `Name` is the ADDRESS — the reason it is `KindIdentity` and
   not a string — so reassigning every name invalidates every
   outstanding patch address. It is the same invariant this document
   spent effort making visible, broken by a different mechanism. A
   transport that forces the hazard the layout was designed to avoid is
   not a fallback.
3. **Add `patch_markup` to the `Act` oneof.** One field; the semantics
   already exist on the unary side.

(3) is the right end state. (1) is the interim, and because there is no
ordering guarantee it requires the editor to **serialize on the
`ActResult`** before issuing a dependent unary patch.

That serialization is **self-imposed discipline, not a guarantee the
transport offers**, and it is written that way so it can be deleted
cleanly rather than surviving as folklore once (3) lands. The
measurement above is what makes it cheap: since a subscribed delta
arrives *before* its own `ActResult`, waiting for the result costs
nothing that was not already going to be waited for — the happens-before
comes free.

### The overflow drop, measured

A subscriber that falls behind is **disconnected, not throttled**.
Forced with a session that subscribes greedily, stops calling `Recv`, and
watches 20,000 property changes driven through the unary surface:

```
stream ended after 290 buffered messages
  code:    ResourceExhausted
  message: the session fell behind: its event queue overflowed, and a
           gap in the delta sequence must not be silent
  app still serving after the drop: true
```

Two properties, and the second is the one that matters:

- **The messages that did arrive arrived contiguously.** 290 of them,
  then the stream ended. No individual message was silently dropped —
  which is what makes reconnect-and-resync *forced* rather than
  advisory. A silent single-message drop would desync a delta consumer
  with no way to notice.
- **Losing a session does not disturb the app.** It kept serving.

Worth recording how the first attempt failed, because it would mislead
the next person: driving 400 changes reproduced nothing. **Not calling
`Recv` does not stop the gRPC transport draining the stream into
client-side buffers**, so a few hundred small messages never reach the
server's queue at all. The drop needs enough bytes to stop this stream's
flow-control window being replenished — 20,000 messages with a 400-byte
payload did it in 6.6 seconds.

The design consequence stands and is now measured rather than inherited:
an island must be **statable completely**, not reconstructible from
deltas.

The MCP surface remains the **fallback** for apps without a control-plane
session, and `validate_markup` remains the right primitive for checking a
candidate before committing it either way.

Its *output* is separately plugin-shaped: buttons it draws can bind
`temporal:Activity`, so the app it builds gains behavior at runtime.
"The builder will be the first plugin" resolves cleanly — first Attach
plugin.

### The hazard the layout has to be designed around

Three tests in `mcp/focuspatch_test.go` cover this — **on
`feat/dynamic-activities` only; the file does not exist on `main`**, so a
reader on `main` cannot run them. An earlier version of this section
**overstated what they establish**. Corrected,
because the difference matters to anyone deciding how much to trust it:

**Asserted, and safe to rely on:**

- Focus **survives** a patch and is re-resolved through the name table.
- The bound text survives, because it lives in the property, not the
  widget.
- **The caret does not survive** —
  `TestPatchMarkupDropsFocusInsideThePatchedSubtree` asserts the caret is
  no longer where it was.
- Same-name-different-type substitution **does happen**, and name-only
  validation permits it
  (`TestPatchMarkupKeepsFocusWhenTheNameSurvivesButTheTypeChanges`).

**Stated here before, but NOT asserted by those tests:**

- *"The caret resets to 0."* The test asserts only that it moved.
- *"Focus lands on a neighbouring widget."*
  `TestPatchMarkupLosesFocusWhenTheFocusedNameDisappears` asserts focus
  is not still on the vanished name; a run where focus cleared to nothing
  passes identically. **The destination is unknown.**
- *"The next keypress invokes a command."* That test logs the post-patch
  focus and never sends a key, so no command is ever observed to fire.
  The mechanism is sound by inspection; it is not measured.

### A new failure mode for the measured/inherited convention

This is worth recording as a rule rather than a correction, because the
convention this document relies on did not catch it. Every one of those
claims was marked *measured*, cited a **real, passing test by name**, and
still overstated the evidence — because **a test's name is prose and its
assertions are the evidence, and the two can drift apart.**

The rule: **cite what a test asserts, not what it is called.** A name
that describes more than the body checks is not a lie by anyone; it is
just a summary, and summaries are exactly what this document argues must
not be trusted over the thing they summarize.

**The design does not weaken.** The structural mitigation below never
depended on which of these is pinned — inputs outside the patched island
are safe whatever the caret lands on, and safe whether focus moves to a
neighbour or nowhere. Only the claims about the hazard were overstated;
the response to it was already stronger than its evidence.

A WYSIWYG builder patches the exact region its user is interacting with.
That makes this the central layout constraint, not an edge case.

The structural mitigation is the one to require, because it is checkable
at build time rather than hoped for at runtime: **user inputs are
SIBLINGS of the refreshed island, never children.** The property
inspector's `TextBox`es live outside the canvas subtree that gets
repatched. A test walks the built tree and fails if a later edit moves an
input inside the island, so the guarantee cannot rot silently.

There is a second mitigation that costs nothing and should be preferred
wherever it applies: **express a change as state, not structure.**
Because a `temporal:Activity` binding resolves its activity name from a
property at click time, repointing a button's behavior is a `set_value`,
not a `patch_markup` — zero patches, so zero caret risk. Every editor
operation should be examined for whether it can be a `set_value` before
it is allowed to be a patch.

### The spine: a UI must not present a distinction the model cannot support

**Seven** independent findings turned out to be the same principle, plus
one candidate that was inherited rather than verified and is retracted
below. At that count it is no longer a theme running through the
document; it is the document's argument, and every design decision above
is an instance of it.

1. **`AttrsKnown` vs `Origin`.** An element whose attributes cannot be
   enumerated must not look like one that has none. Provenance and
   knowability are different questions that merely correlate today.
2. **`ModeUnknown` vs `ModeNone`.** The same argument one level down: an
   element whose child rule could not be read must not look like one that
   accepts no children.
3. **`Open` vs closed.** An attribute list that is complete *for this
   context* must not be presented as complete *in general*.
4. **Registered components in the palette.** A bare name rendered
   identically to a fully-described element tells the user the app is
   simpler than it is.
5. **Attached properties scoped to the parent.** Offering `Canvas.Left`
   on a child of a `<VStack>` presents a positioning capability the model
   does not have — and it is discarded by exactly the silent path this
   document opens with.
6. **`Name` is `KindIdentity`, not `KindString`.** It is the ADDRESS, not
   a value: `PatchMarkup` resolves by it and requires a fragment root to
   carry the same one. Emitting it as a string puts it in a property
   inspector as a text field beside `Content`, inviting a rename mid-edit
   that silently breaks the addressing of whatever is patching that
   subtree. A consumer must decide what a rename *means*.

7. **`TakesLayout` — and this one runs the other way.** `AttrsFor`
   joined the universal layout surface onto NON-VISUAL elements while
   `checkAttrs` correctly refused it, so the catalog **advertised
   attributes the loader would reject**: a palette offers `Width` on a
   `<Timer>`, the user sets it, the load fails.

   Every other instance is the UI claiming *less* than the model
   supports, or collapsing two states into one. This is the first in the
   opposite direction — **over-offering** — and the consequences are not
   symmetric. Under-reporting confuses the user; over-offering produces
   an error **the user gets blamed for**, because they set the attribute
   and the failure arrives later, far from the offer that caused it.

   Its cause is the document's own thesis in miniature: the rule shipped
   in the rejection path and not the catalog path — two copies of one
   idea — which is the colocation argument demonstrated by its own
   absence. It was caught by a red test written *before* the
   `ElementDef` restructure, which would otherwise have carried it
   forward as faithfully-preserved behaviour.

Note what the first six have in common: the tempting simplification is to
collapse two values into one, and the collapsed version is not merely
less informative but *actively misleading*. The seventh generalizes the
test — a UI must not present a distinction the model cannot support, and
must not offer a capability the model does not have. That is what to
apply to any future addition.

The sixth was found **inside the fix for the fifth** — `Name` entered the
catalog as part of adding the universal attributes, and arrived wearing
the wrong Kind. A principle that keeps finding defects in this document's
own corrections is doing work rather than decorating it.

### A hazard that was overstated, recorded because it was nearly designed around

An earlier draft added a sixth instance: that
`Click="{{temporal:Activity .Selected .Input | into .Output}}"` is one
binding whose target is data, so a palette offering "Run ReverseShout"
and "Run Probe" as two actions would be lying, and the builder was
therefore forced into one-action-plus-a-selector.

**That is wrong and the design should not be constrained by it.** The
activity name being a bound path is correct, but the conclusions drawn
from it were not:

- **Literal names are supported and are the documented form** —
  ``Click="{{temporal:Activity `Slugify` .Input | into .Out}}"``. Two
  buttons carrying two literal names are two genuinely distinct actions,
  and a palette presenting them that way is accurate.
- **"Whichever activity was created last wins" was an artifact of one
  demo**, whose creation step set the selector property as a convenience.
  Nothing in the mechanism does that.
- **Rebinding is exactly `set_value`** — the property *is* the rebinding
  mechanism.

Both forms are available and the choice is a design one: literal names
when the set is known at patch time; a property-supplied name when the
target should be repointable without a patch — which stays strictly
better where it applies, for the caret reason below.

This is recorded rather than deleted because of what the near-miss
actually shows, which is narrower than "the principle over-fires". The
principle did exactly the right thing with the input it was given; the
input was wrong. The claim entered through a three-link relay — an index
summary of a note, quoted rather than the note itself, then passed on as
a hard constraint without being checked against the source.

Note the arithmetic, because it is the point rather than bookkeeping:
**eight candidates, seven verified against the source and kept, one
inherited second-hand and retracted — and it was the inherited one that
failed.** The retracted candidate is not one of the seven; it never
became a member. **That ratio is the argument for the verification
discipline, not against the lens.** This project runs heavily on findings relayed between sessions,
and this is the one documented case of the relay failing — worth keeping
precisely because the failure was invisible at every hop.

**Open, and the button-behavior story should not read as settled until
it closes.** An unlanded branch adds a `register_commands` act to the
`Act` oneof. That is a *fourth* answer to a question this document has
already been through three times — "commands cannot be registered", then
"they can", then the resolution that behavior arrives through the
`temporal:` handler namespace with a data-supplied target rather than
through registration. If a literal `register_commands` means what its
name says, it changes what a drawn Button can be bound to. Nothing here
is built on it, it is not being pulled on while it sits behind its
owner's design gate, and this paragraph exists so that a reader does not
mistake the section below for a closed question.

There is a fourth precondition, and it is a capability rather than a
display problem. Behavior arrives through the handler namespace, and
`RegisterHandlers` is a **process-global table the host app must
populate**. An MCP client cannot add a handler namespace to an app that
did not register one. So against an app without the `temporal:` provider,
a drawn button genuinely *is* limited to commands that already exist. The
builder must detect which case it is in and say so, rather than offering
open-ended behavior it cannot deliver. `markup.RegisteredHandlers()`
answers this and is not currently exposed over MCP; exposing it is the
one thing the builder needs that the control surface does not yet offer.

### Layout

Four regions. The split is dictated by the hazard above, not by taste.

```
┌──────────────────────────┬──────────────────┐
│ Canvas   (patched)       │ Palette          │
│                          │  (patched)       │
├──────────────────────────┼──────────────────┤
│ Markup output (patched)  │ Inspector        │
│                          │  INPUTS — never  │
│                          │  patched         │
└──────────────────────────┴──────────────────┘
```

The inspector's inputs are siblings of every patched region. The canvas,
the palette and the markup output are all refreshed by patching their own
named islands; the inspector is refreshed by `set_value` against
properties its inputs are bound to, so the subtree containing a caret is
never rebuilt.

## What the first consumer found

The prototype (`examples/wysiwyg`) was built to test the SHAPE, not to
be an editor, and the shape did not survive contact intact. Every gap
below was invisible to a careful reader — two of them had already been
reviewed twice — and showed up within minutes of a real consumer using
the catalog to generate markup.

**`Required` was missing entirely, and a palette is unusable without
it.** Adding an element with no attributes produced markup that would
not load for **eleven of about thirty** elements, because a binding-only
attribute with no value fails at bind time. "What can I set on this
element" turns out to be half the question; "what must I set" is the
other half. Two signals derive it, both the switch's own spelling: a
binding-only attribute that is never presence-tested (optional ones are
guarded by `if _, ok := e.Attrs["X"]; ok`), and an explicit emptiness
check that returns an error. Thirteen attributes across the vocabulary
are required, and seeding them took the failures from eleven to four.

**Slots needed the same field**, which turned `Slots []string` into
`Slots []SlotSpec`. An `<ItemsView>` without its `<ItemsView.ItemTemplate>`
does not build, and nothing in the catalog said so. That took four
failures to two.

**`<Validate>` was reported as NOT non-visual**, which is exactly wrong.
Its Go type is declared inside `markup` itself, so the component literal
is a bare `&Validate{}` with no package qualifier, the Go type came out
empty, and every behavioural axis silently defaulted to false. This is
the **third** instance of the same shape: the extractor reports what it
can see and cannot know that it is blind. Package-local types are now
resolved, with their method sets read from the AST rather than the type
checker.

That fix carried a bootstrap constraint — **the generator could never
type-check `markup` itself**, because that would require the generated
file to already compile, which is exactly the situation someone is in
when it is missing or broken.

Declaration retires the constraint along with the generator: the axes
now come from a runtime type assertion, which has neither the bootstrap
problem nor the blind spot. Recorded because the constraint was real for
as long as the generator was, and because it is the kind of thing that
looks arbitrary to a reader who arrives after the reason has gone.

Two failures remained. **One of them has since been fixed, and the
correction matters more than the original claim.**

`<Image>` was recorded here as permanently unplaceable by a client — it
needs a `*prop.Property[image.Image]` and there was no registration kind
for images. **That is false as a permanent claim, but still true on
`main` today.**

The distinction matters and this document has to keep it: an image
registration kind exists in **PR #206, which is open and unmerged**. A
reader on `main` genuinely cannot place an image. What was wrong was the
word *permanent* — the capability was absent, not impossible, and this
document reported the first as the second.

When #206 lands, the registration is:

```json
register_properties {"properties": [
  {"name": "Logo", "type": "image", "value": "<base64 of an ENCODED image file>"}
]}
```

Four details that change how an editor should present it, none of which
are obvious from the registration alone:

- **Encoded file bytes, base64'd — not raw pixels.** The host owns the
  format registry (PNG/JPEG/GIF/BMP/ICO in core, plus whatever it blank-
  imported), so a client stating width/height/stride would be
  reimplementing a decoder in order to talk to one.
- **An absent value is legal and means nil.** A page may bind `Src`
  before any picture exists and the `<Image>` renders nothing rather than
  failing. That is the placeholder slot, and it is how a builder should
  emit a freshly-dragged `<Image>`.
- **A decode failure names the formats THIS host reads.** Because formats
  arrive by blank import, "this build has no SVG" is a *configuration*
  answer rather than a malformed-file one, and an editor that collapses
  the two misreports the target's capabilities — the spine principle
  again, on the capability axis.
- **Read-back is exact**, so an inspector can round-trip an image rather
  than treating it as write-only. An earlier version of this correction
  said the opposite, because decoding dropped the source bytes; that was
  self-inflicted and has been fixed. The encoded bytes are carried
  alongside the decoded picture, so a read returns *byte-identical* to
  what arrived — a re-encode would have been a different file, and would
  have put an encoder on the `ListValues` and frame-delta paths.
- **"Has bytes" and "has an image" are different states.** A picture
  built in-process reports no source rather than inventing one, so an
  inspector must not render a synthesized preview as if it were the
  property's source. Same honesty rule: "no source available" and "here
  is the source" must not look alike.

Note this is the **second** recorded framework limit to be fixed
mid-project, after the `patch_markup`-is-not-an-act one. **Treat this
section as provisional**, and note the direction, because it is not
random: **this document over-reports permanence.** Both times it observed
an absence and recorded an impossibility.

That is the bias worth naming, because of which way it costs. A design
that over-reports permanence gives up capability it could have had — it
routes around a wall that was a door, and the workaround then looks
justified because the wall was real when it was measured. Over-reporting
*possibility* would be caught by the first thing that failed to work;
over-reporting permanence is never caught by anything, because nobody
retries a thing the record says is impossible.

The mitigation is the same discipline applied to relayed claims: say when
a limit was observed, and treat "absent" and "impossible" as different
words.

### The one asymmetry the image kind introduces

For every other kind, bindability and literal-spellability are the same
axis: a `propKinds` row parses a markup literal, and the same type can be
registered over the control plane. The image kind has **no `propKinds`
row**, and cannot have one, because there is no way to write a picture
inline in markup.

So `<x:Property Type="image">` does not exist and an include control
cannot declare an image parameter, while `register_properties` can create
one. `<Image Src>` itself is unaffected and stays `BindsEither` — its
literal form is a *path*, not a picture — but a consumer that assumes
"registerable implies declarable" is wrong for exactly this kind.

`<MenuBar>` remains a genuine gap: it needs `<Menu Title=…>` children, a
required *nested* structure `ChildSpec` cannot reach.
`<MenuBar>` needs `<Menu Title=…>` children — a required *nested*
structure that `ChildSpec` does not reach. Both are recorded rather than
worked around.

The second is a **shape** limitation rather than an extraction gap:
`ChildSpec` can say "only these element names" but not "and each of them
must carry these attributes". If the served form ever needs to express a
required nested structure, that is where it will have to grow.

### Two bugs the unit tests could not see

Worth recording because they are the argument for running the thing, and
because both are the silent-failure shape this document is about.

**The palette and inspector rendered empty**, with no error anywhere.
`Context.Values` captures each handle BY VALUE, and the map was built
before the computed properties existed — so it stored nil, every binding
resolved to nothing, and two panes came up blank. The unit tests were
green throughout, because they called the editor's methods directly
while the screen reads through the binding.

**The inspector kept offering `Canvas.Left` after the container became a
`<VStack>`.** The document is plain Go state, so a computed over it
records no dependency and caches forever; an explicit revision property
is what gives the graph something to invalidate on.

This one is the nastier of the two and deserves its own sentence: **the
artifact was right and the view of it was wrong.** The emitted markup had
already dropped `Canvas.Left`; only the pane was lying. A builder whose
*output* is correct while its *display* is stale is the one failure a
WYSIWYG editor cannot have, and it is a variant of the spine principle —
a UI presenting a state the model has already left.

Both bugs have the same root: **the tests asserted around the property
graph, and the screen reads through it.** Both now have guards that
assert through it.

### The methodology finding

Stated with its counts, because the counts are the argument.

This design was reviewed three times and read carefully by two people.
Those passes found real defects — the `Origin`/`AttrsKnown` conflation,
the missing universal attributes, the `HasLayout` reversal — so reading
was not wasted.

Then the first consumer found **three more shape defects in minutes**
(`Required` unpopulated, slots needing the same flag, `<Validate>`
mis-typed), plus **two bugs that no amount of reading could have
surfaced**, because both were invisible in the source and visible only on
screen.

The general form, and the reason the order matters more than it looks:
**you cannot stabilize a shape by promising not to change it; you
stabilize it by exercising it.** Specifying before building does not stop
the shape moving — it moves it while somebody else is building on it.
Build the first consumer before declaring an interface settled.

### What the prototype validated

- The palette is the catalog: `<LogPane>` and `<Preview>`, both
  host-registered, render as `? unknown` while `<AdornmentLayer>` renders
  as `none`. The distinction survives all the way to the screen, which is
  the only place it matters.
- `Name` shows as `identity`, not as a string field beside `Margin`.
- Retyping the container from `<Canvas>` to `<VStack>` removes
  `Canvas.Left` from both the emitted markup and the inspector — the
  parent-scoping rule, end to end.
- The editor's own `TextBox` survives every rebuild of the preview
  island, asserted structurally by
  `TestEditorInputsAreSiblingsOfThePreview` and behaviourally by
  `TestPreviewRebuildDoesNotDisturbTheEditorsOwnInput`.

## The transport layer, and one guard that was not a guard

Built as `examples/wysiwyg/remote.go`, holding an `Attach` stream and a
unary `ControlService` client against the same connection — because
neither alone suffices. The stream carries property writes and delivers
subscribed deltas filtered to the names the editor owns; `PatchMarkup`
is not an act, so replacing one named subtree has to go through the
unary surface.

### The editor is a separate module, deliberately

`examples/wysiwyg` has its own `go.mod`. That is a decision with a
reason, not a layout accident, and it is worth stating because a future
reader could "simplify" it into the root module and break an invariant
silently — nothing fails until someone counts dependencies.

Two constraints are in direct conflict: **the editor is an `Attach`
client**, so it needs grpc and protobuf; and **core gooey stays at
`golang.org/x/term`**. A nested module is what makes both true at once.
Had the editor lived in the root module it would have dragged grpc,
protobuf and genproto into core gooey for every consumer of the
framework. The root `go.mod` is still `golang.org/x/image`,
`golang.org/x/term` and `golang.org/x/sys` — verified, not assumed.

This is the same mechanism `mcp/`, `packs/*`, `handlers/*` and the other
two examples already use, and the root `./...` skipping it is the
mechanical proof rather than a promise.

**The corollary is a trap and belongs next to the decision:** a nested
module is excluded from the root's `./...`, so root `go vet ./...` and
`go test ./...` do **not** cover the editor. CI can be green with the
editor broken. It is tested explicitly:

```
cd examples/wysiwyg && go test ./...
```

### The barrier had to be made able to fail

The serialization was written first as: await every act's `ActResult`,
then issue the patch. A test drove 25 write-then-patch cycles and
passed.

**It also passed with the barrier deleted**, because `act()` already
blocks — so nothing was ever in flight when a patch was issued, and the
barrier was unreachable. A guard that cannot fail is not a guard, and
this one had a test that looked like evidence.

The fix was not to weaken the test but to build the thing the editor
actually needs: `SetPropertyAsync`, a pipelined write, because a round
trip per keystroke would put the transport inside the interaction loop.
With writes pipelined the race is real and reproducible on demand —
deleting the barrier fails immediately and repeatably:

```
round 0: property = "round0-write38", want "round0-write59"
round 0: property = "round0-write12", want "round0-write59"
round 0: property = "round0-write14", want "round0-write59"
```

The unary patch overtook between 21 and 47 pipelined acts. That is the
unordered-channel hazard measured rather than argued, and
`TestPatchIsOrderedAgainstPipelinedWrites` now fails without the
barrier. (It was `TestPipelinedWritesAreDrainedBeforeAPatch` until the
name started describing a mechanism rather than the property, which is
the same drift this document warns about one paragraph up.)

Two things about the fix are worth separating, because only one of them
is obvious.

**The correct fix is what made the defect visible.** `SetPropertyAsync`
was not added to expose a race — it was added because a round trip per
keystroke puts the transport inside the interaction loop, which is
unacceptable on its own terms. The version that hid the bug was the
version that was too slow to ship. That is the ordinary shape of this
kind of defect: it hides behind a placeholder implementation and appears
the moment the real one arrives.

**And the rule the episode is really about:** *write the test so that
removing the thing it protects breaks it, then check that it does.*

That generalizes past guards. It is the same discipline as recording the
reproduction attempt that FAILED beside the one that worked (the 256
drop, where 400 messages showed nothing and 20,000 showed everything) —
in both cases the useful artifact is the negative result, because it is
what tells a later reader whether their own green run means anything.

This is the third time in this work that a test asserted around the
mechanism instead of through it, after the two the prototype exposed.
The first two were in a prototype; this one was in the transport layer,
written by someone who had already recorded the rule twice. That is the
strongest available evidence that it is not a rule careless people
violate.

## Remote mode: the editor drives another app

`-attach <addr> -island <Name>`. The document model, palette, inspector
and parent scoping are **unchanged** — they operate on a document, not on
a tree. Only the destination of the emitted markup changes: locally it
builds into the preview island, remotely it is validated against the
target's live binding context and patched into the target's island.

**The editor owns exactly one named element.** `-attach` without
`-island` is a startup error rather than a default, because a default
would be the editor quietly claiming something. That is the plugin
spec's subtree-ownership rule applied to this client: it turns
concurrent writers into disjoint writers, and a failed edit cannot reach
anything the editor does not own.

**The root is renamed to the island on the wire only.** `patch_markup`
requires the fragment root to carry the address, but a user should not
have to name their document root after somebody else's element, so the
document itself is unmutated.

The test worth naming is `TestRemoteModeBindsAgainstTheTargetsContext`:
it patches a document binding `{{.Body}}` — present in the target,
absent in the editor — and asserts the same markup **fails** a local
build. That makes "validate against the target, not against yourself"
falsifiable rather than asserted.

### The subscription is not optional, and getting it wrong is silent

`Subscription{Lifecycle: true}` is required, and an editor that omits it
is broken in two ways that produce no error at all — an all-defaults
subscription is **write-only**, so the signals exist the whole time and
simply never arrive. A quiet app and a broken subscription look
identical.

- **`Swapped` is total invalidation.** Any client's `SwapMarkup`, a hot
  reload, or the app itself replaces the page and **every `Name=` is
  reassigned**. Every patch address the editor holds goes stale at once,
  including its own island's. Without the signal, the next patch either
  fails with `NotFound` or — far worse — **succeeds against a name that
  now means something else.** For a tool whose whole model is "I own one
  named element", that is the most dangerous thing that can happen to it.
- **`Resized`** updates a size that `Welcome` carries only once, at
  attach. A long-lived editor that caches `Welcome`'s dimensions is
  wrong from the first resize onward.

Better than expected, and worth stating because it changes the recovery
design: **`Swapped` carries the new name table** (`repeated string
named`). So recovery needs no resync round trip — only the discipline of
treating the document as invalidated rather than merged.

`TestSwappedInvalidatesEveryAddress` is written to fail if `Lifecycle`
is dropped, and was verified by dropping it. That mattered more than
usual here: a subscription flag is exactly what gets "cleaned up" by
someone who does not know what it is for, and the failure is invisible.

### The rule stated at the boundary is the rule its own callers break

Recorded as a pattern, because it has now happened three times in this
work and each time the author had just written the rule down:

1. **Rejection path vs catalog path.** `TakesLayout` shipped in
   `checkAttrs` and not in `AttrsFor`, so the catalog advertised
   attributes the loader refused.
2. **Transport doc vs consumer.** `Remote`'s doc comment states the
   UI-goroutine confinement rule; the editor's `OnLost` handler then
   touched UI state from the stream reader's goroutine.
3. **The barrier's own test.** The rule "write the test so removing the
   thing it protects breaks it" was recorded twice before being violated
   by its author in the transport layer.

The common shape: a rule written at an interface is enforced by nobody
at the call site, and the author's own recent memory of writing it
provides false confidence that it is being followed. The mitigations
that worked were mechanical every time — extract the rule into one
function both paths call, delete the guard and watch the test fail, walk
the tree and assert.

## The evidence behind the thesis

The tally is at the top of this document. What follows is the detail for
the instances that produced reusable technique.

### The 13/13, with its causes intact

`Default` was declared for thirteen attributes. The identity test —
omission renders identically to the declared default — passed for all
thirteen on first run. The discrimination test failed for all thirteen.
Three causes, none visible in the source of either test:

1. the probe emitted `<Text/>` with **no body**;
2. stacks got **one** child, and two **equal-width** children;
3. the universal and attached tables were probed through a `<Text>`.

Two of the three were bad probes rather than bad declarations, and that
is the finding. A wrong declaration is what the test was written to
catch; a probe that cannot see is what it was not.

### Two probe rules, reusable

- **An empty `<Text>` is never a valid probe.** It paints nothing, so it
  disables *every* visual attribute at once — alignment, size,
  visibility, margin, span — and makes them all look inert together. A
  probe that renders nothing is the commonest cause of a vacuous
  cell-buffer comparison, and it is worth checking the fixture paints
  before trusting any such comparison.
- **`<Border>` is the honest probe for anything that changes a rect.** A
  `Text` paints at the left edge of whatever bounds it is given, so a
  bounds change that does not move that edge is invisible to it —
  `Grid.ColSpan="2"` widens the slot and the text stays put. A `Border`
  draws its edges *at* its bounds, so every change to the rect it was
  arranged into shows up.

Related, from the other side of the same seam: a **screen capture is
authoritative about symptoms and silent about causes**, so the moment a
capture-based report starts saying *why*, that part wants checking
against the source. The four above are the inverse — code-based checks
that said *whether* without having looked.

## Three findings from running the editor against the rejection path

All three were found by using the thing, all three are the silent-failure
shape this document is about, and the first one is a defect the rejection
change itself introduced.

### Rejection was scoped to the syntactic parent, and a fragment has none

Unknown-attribute rejection validates attached properties against the
element's **parent**: `Grid.Row` is legal on a child of a `<Grid>` and an
error anywhere else. The parser stamps that parent onto each element, and
for a document it is the right answer.

It is the wrong answer for a **fragment**. Every fragment that arrives
over the control plane — `patch_markup`'s replacement subtree,
`swap_markup`'s new page, `validate_markup`'s candidate — is parsed
wrapped in `<Gooey>`, so its syntactic parent is always `<Gooey>` no
matter where the fragment is going. A perfectly legal patch replacing a
`<Grid>` child was rejected for carrying `Grid.Row`, because the checker
was asking about the wrapper rather than the destination.

The fix is to skip **only the attached-property scoping rule** at the
document root, where the real parent is unknown, and keep unknown
attributes rejected there as before. Two things about it are worth
keeping:

- **The first scoping proposed was too narrow.** It covered
  `patch_markup` and would have left `swap_markup` broken, because a page
  root reaches the same code by a different route. A bug report names the
  path the reporter walked; the fix has to cover the predicate.
- **The corpus that was supposed to catch this could not see it.**
  `TestEveryGooeyFileInTheRepoHasValidAttributes` walks 626 elements
  across 37 `.gooey` files and was green throughout. It is honestly
  named and was over-read: **`.gooey` files are documents, and a fragment
  is never a file.** A corpus test's coverage is the corpus's shape, not
  its size — 626 elements of the wrong kind is 0 elements of the right
  one. The guard that catches it is a fragment fixture, and it is the
  one that was written after the fact and watched fail first.

### Editor chrome and document vocabulary are two contexts

The editor's palette was sourced from the context the editor's own page
was built with. That context necessarily contains the editor's furniture
— `<Preview>`, `<LogPane>` — so the palette offered the user the
component that *renders the document*. Dropping it into the document made
the document contain its own renderer: `Canvas.Measure` and
`MeasureChild` alternated until the stack overflowed, killing the process
and leaving the terminal unrestored.

The conflation is the interesting part, not the crash. "Elements this
context can build" and "elements the user is authoring with" read as the
same set and differ by exactly the editor's chrome. Splitting them —
`ctx` builds the shell, `docCtx` is the document's vocabulary — makes the
difference structural: chrome is registered in one place and the palette
reads the other, so a third chrome component cannot re-break it. A
denylist of the two names would have.

This generalises past the editor. **Any app that both builds markup and
edits markup has two vocabularies**, and the one it builds itself with is
not the one it should offer. The design surface's edit/runtime split
(`docs/specs/2026-08-11-design-surface.md`) is the same seam used a
second time.

### Self-nesting became a visual, not an error

The obvious guard — refuse to build a `<Preview>` whose parent is a
`<Preview>` — is insufficient and misleading. It catches
`<Preview><Preview>` and misses `<Preview><Canvas><Preview>`, which is
what a user actually reaches, because they drop it inside a container.
Any same-type **ancestor** recurses, so the guard would have to be an
ancestor walk at measure time, on a tree that has no parent links there.

The context split makes the question moot instead of answering it: the
document's `<Preview>` builds an Escher mirror — concentric frames drawn
in one loop, no children, no recursion possible — while the editor's
`<Preview>` builds the real pane. A document's `<Preview>` is never the
real one at any depth.

Two things this preserves that a guard would not. The palette can keep
offering `<Preview>` **honestly**: it genuinely can be placed, it simply
cannot recurse. Removing it from the palette would have been the editor
lying by omission about what a document can contain — the exact failure
mode this record spent its length cataloguing. And the depth constant is
**aesthetic, not a safety limit**: nothing recurses, so changing it
cannot make anything unsafe, and saying so in the code is what stops a
later reader from treating it as load-bearing.

The general shape: **when a guard has to detect a condition, check
whether the condition can be made unreachable instead.** A detector is a
predicate that can be wrong; a vocabulary split is a structure that
cannot.

Still parked, and deliberately not built: a depth cap in `MeasureChild`.
It is the right answer for a *deliberately* constructed cycle, which the
vocabulary split does not address, but the bound has to come from
measuring real trees rather than from a guess.

## Order of work

1. **Declared vocabulary + runtime union.** Done:
   `markup/elementdef.go` (the type, the registry, axis derivation),
   `markup/elements.go` (31 `ElementDef` literals),
   `markup/catalog.go` (the union, `AttrsFor`, universal and attached
   tables), `markup/internal/catalogen/` (the cross-check, no longer a
   generator), and their tests. `buildComponent`'s switch and the
   generated table are gone: −537 lines from `markup.go`.
2. **Unknown-attribute rejection.** Done, and it must stay sequenced
   after the universal attributes: rejection before the vocabulary
   included `Name`, `Tooltip` and the layout set would reject valid
   markup on nearly every element. That ordering is load-bearing rather
   than tidy.
3. **`control.Service.Catalog()` + its control-plane adapter.** Owned by
   the control-plane maintainer, not by this work: `control/`, `grpc/`
   and `mcp/` carry invariants that do not show up in a diff —
   UI-goroutine confinement, the v1 tool ceiling, error-kind mapping, and
   the rule that a tool's `Run` returns plain data and never a handle.
   This document's deliverable is the generator, the generated table, and
   a catalog shape specified well enough to implement against.
4. **`list_handlers`**, answering "can this app gain behavior at runtime"
   in one call at connect time. In progress on the control-plane side.
5. **The builder**, attaching over `SessionService.Attach`.

## Open questions

**Is the stdlib source importer durable enough to depend on?** It is the
least durable thing in the generator: slower than `go/packages`,
historically finicky about module mode and build tags, and not where
ecosystem effort goes.

The risk is bounded by a property worth stating explicitly, because it is
what makes the choice defensible rather than merely convenient: **the
generated file is committed.** A broken importer blocks *regeneration*.
It never breaks the build, never breaks `go get`, and never affects
anyone who is not adding an element. The failure mode is "`go generate`
errors" — loud, at the moment of change, with the previous output still
valid.

The fallback is written down here so nobody re-derives it under pressure:
make the generator a **nested module**,
`markup/internal/catalogen/go.mod`, using `x/tools/go/packages`. Nested
modules are excluded from the parent's `./...`, so the root dependency
graph is untouched — the same mechanism `mcp/`, `handlers/*` and
`packs/*` already use. The directory already exists, so it is a one-file
change if it bites.

**Children are silently dropped too.** Elements like `<Checkbox>` never
call `buildChildren`, so `<Checkbox><Text/></Checkbox>` discards the
`Text` without a word — the same class of defect as the attribute case
and not addressed by this design. `ModeLeaf` records where this happens;
whether it should also become an error is unresolved.
