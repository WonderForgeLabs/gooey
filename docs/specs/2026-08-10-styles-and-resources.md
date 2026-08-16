# Styles and scoped resources (design — issue #55, epics #54 and #95, design-ahead for #94)

Design record, 2026-08-10. Covers scoped resource dictionaries (#95),
markup styles with setters (#56), selector matching with state
pseudo-classes (#57), and the demo migration (#58); sketches — without
committing — the hooks control templates (#94) will need. The issues
below implement this record without re-deciding anything.

## The commitments

1. **Resources are ambient, tree-scoped, and resolve at BUILD time to
   property handles.** A resource reference is an lvalue, like every
   binding in markup: the key resolves once, when the tree is built, to
   a `*prop.Property[T]`; the *read* of that handle happens inside a
   paint node, so a changed resource repaints exactly its readers. There
   is no lookup at paint time and no dictionary walk at runtime.
2. **A gooey style is a reactive `render.Style` recipe, not a property
   bag.** A `<Style>` compiles at load into typed setter closures; at
   build it materializes as one per-instance
   `prop.NewComputed[render.Style]` fed into the `Style` slot every
   component already has. **Zero component changes**: the styling
   system lives entirely in markup parsing and tree building, and the
   components keep reading `*prop.Property[render.Style]` exactly as
   they do today.
3. **The graph is the trigger engine.** `:focus`, `:hover`, `:disabled`
   are reads of the existing source properties (`FocusState`,
   `HoverState`, `Action.CanExecute`) *inside the style computed*. No
   invalidation pass, no style events, no tree walk on change — a state
   flip dirties the one computed that read it, which dirties the one
   paint node that reads the computed. The 2-repaint focus guarantee is
   inherited, not re-implemented.
4. **The cascade is small and totally ordered.** Bound handle > explicit
   key > implicit type match > nothing. Within a style: base, then
   state overlays in a fixed order. This is a TUI: no descendant
   combinators, no classes, no specificity arithmetic.

## Part 1 — Scoped resources (#95)

### Spelling

Resources are declared in a property element on any element, using the
dotted parse path that already serves `ItemsView.ItemTemplate`:

```xml
<Gooey xmlns="wonderforge.io/gooey/2026">
  <Gooey.Resources>
    <Resource Key="accent" Type="color"  Value="#ffaa3c"/>
    <Resource Key="pad"    Type="int"    Value="1"/>
    <Style Key="panel" TargetType="Border"> … </Style>
  </Gooey.Resources>
  <Grid Rows="*,1">
    <Border.Resources> … </Border.Resources>  <!-- legal on any element -->
    …
  </Grid>
</Gooey>
```

`<Gooey.Resources>` is the document scope; `<X.Resources>` on any
element opens a subtree scope. `Resources` is an ELEMENT-level slot —
like `Name`, `Tooltip`, and the layout attributes, it belongs to the
element, not to the component's attribute vocabulary — so `checkProps`
exempts it globally and no builder ever sees it. Dedicated block forms
(`<Resources>` as a top-level element) were considered and rejected:
the dotted slot is one mechanism at every level, and it already parses.

### Typing

A `<Resource>` is `Key` + `Type` + `Value`, where `Type` selects a row
of the same `propKinds` table `<x:Property>` uses — string, int, bool,
float, duration, color. The `Value` is coerced at LOAD time by the
row's closure, so a bad literal fails the file that declares it, with
the same error shape declarations get. `Type="any"` is excluded for the
same reason it takes no `Default`: a resource is defined by a literal,
and `any` has none. Each declaration materializes a
`prop.NewSource[T]` — a settable source, which is what makes a theme
swappable at runtime (below).

The second resource form is `<Style>` (Part 2). Later forms
(`<ControlTemplate>`, localized string tables) are additional entries
in the same scope with their own element names; the scope stores `any`
and each consumer type-checks its own lookups, the `boundProp` pattern.

### Scoping and resolution

Resolution is lexical and happens during build. The `Context` carries a
resource-scope chain with the same document-scoped save/restore the
xmlns table uses: entering an element with a `Resources` slot pushes a
scope, leaving pops it, so however a build path recurses, siblings can
never see a scope they are not inside. A key defined in an inner scope
SHADOWS the outer definition for that subtree — shadowing produces a
*different handle*, bound at build by whoever referenced it there, which
is the whole per-subtree-override story: no priority numbers, just
lexical capture.

### Control boundaries: resources are ambient

`Values` isolate; resources inherit. That asymmetry is deliberate and
is the conceptual line of this design: **Values are data — they cross
control boundaries only through the declared surface (attributes,
`x:Property`). Resources are theme — they flow through the instantiation
site's scope chain into every control below it.** This matches what
`control()` already does with `Styles` (a child context inherits the
parent's map unless setup overrides), and it is the opposite choice
from the xmlns table (strictly per-document) for the opposite reason:
a prefix is a capability grant, a theme is an inheritance.

A control file may declare its own `<Gooey.Resources>`; those shadow
the ambient chain for its subtree (each instantiation materializes
fresh handles, like declared defaults — and shares their hot-reload
wrinkle: markup-held resource state resets on reload until Name-keyed
state adoption exists). The FS isolation of a control's assets and
bindings does NOT isolate its resource lookups.

### Backward compatibility and strictness

`Context.Styles` becomes the ROOT style scope: `Style="name"` resolves
up the markup scope chain first and falls back to `ctx.Styles`. Every
existing demo therefore loads unchanged — its Go map is simply the
outermost dictionary.

**The collision rule, stated rather than implied: when a host registers
`Styles["accent"]` and a page declares `<Style Key="accent">`, THE PAGE
WINS.** That is not a special case; it is the same "nearest declaration
wins" rule the rest of the chain runs on, with `ctx.Styles` simply
furthest out. The reason to prefer it is the migration, and it is not
recoverable from the code: Part 5 moves each demo's palette out of Go one
key at a time, so a page must be able to declare `accent` and see it take
effect *without first deleting the Go entry*. Under host-wins that edit
is a silent no-op, and every demo migration becomes one all-or-nothing
flip. The other direction is also the more surprising one — a host grant
silently overriding a style declared three lines above the element that
uses it makes the visible declaration the lie, which is the same
silent-drop class as the unknown-key bug below.

What this leaves with no escape hatch, on the record so it is not
rediscovered as a bug: **a host that must WIN over markup it does not
control** — high-contrast accessibility mode, or an embedded page from
elsewhere. Today it cannot; `Context.Styles` is a default, not a policy.
The answer when that case arrives is an additive, separately named map
consulted FIRST (`Context.StyleOverrides`), never a reversal of this
chain: adding a map changes no existing page, while flipping the default
silently changes every one of them.

One tightening: **an unknown style key is now a LOAD error.** Today
`ctx.Styles[raw]` on a typo silently renders unstyled — exactly the
silent-failure mode strict mode and `checkProps` exist to stamp out,
and now that load can see the whole chain it can afford to be loud.
`Style=""` (explicitly empty) remains "no style" and, once implicit
styles exist, is also the opt-out from type matching. Stage 1 surveys
the demos for dangling keys before flipping this on.

`ctx.Values` is untouched — bindings are not resources.

### Runtime access

`Context.Resource(key)` returns the DOCUMENT scope's handle after a
load (root scope only in v1; subtree scopes are reachable only from
their own subtree, by construction). Go code type-asserts and `Set`s
it — that is the dark-mode toggle: one `Set` on `accent`, and exactly
the components whose resolved style read it repaint. No new mechanism;
it is `Find[T]` for the theme.

## Part 2 — Styles with setters (#56)

### Spelling

```xml
<Style Key="panel" TargetType="Border">
  <Setter Property="Fg" Value="#7857dc"/>
  <Style.Focus>
    <Setter Property="Fg"   Resource="accent"/>
    <Setter Property="Bold" Value="true"/>
  </Style.Focus>
</Style>
```

- `Key` makes the style addressable by `Style="panel"`. `TargetType`
  makes it match implicitly (Part 3). At least one is required; both
  together mean "explicit, but applying it to another element type is a
  load error".
- `<Setter>` takes `Property` plus exactly one of `Value` (a literal,
  coerced by the property's kind at load) or `Resource` (a key resolved
  up the scope chain at build; its declared `Type` must match the
  field's kind — checked at build, before any frame). Both or neither
  is a load error. The attribute-pair spelling was chosen over a
  markup-extension syntax (`Value="{accent}"`): no new expression
  grammar, and the load-time check is a map lookup.
- State sections (`<Style.Focus>`, `<Style.Hover>`, `<Style.Disabled>`)
  ride the dotted property-element parse unchanged. An unknown section
  is a load error.

### What setters set — the heart of it

**v1 setters address the fields of `render.Style` and nothing else:
`Fg`, `Bg` (color), `Bold`, `Dim`, `Underline`, `Reverse` (bool).**

Materialization is a six-row closure table (`styleFields`), the
`propKinds` discipline at field granularity: each row parses its
literal at LOAD into a typed `func(*render.Style)` — or, for
`Resource=`, a closure capturing the handle whose `Get` runs inside the
style computed. An unknown `Property` or an unconvertible `Value` is a
load error naming both, per #56. No reflection anywhere: the table is
the type system.

Why not setters over arbitrary component properties (the
`componentProps`-table alternative)? Because nothing needs it and
everything pays for it. Every Go theme in the repo is literally a
`map[string]render.Style`; every state-dependent appearance in the
demos is a cell-style change; and the datatemplates record already
deferred "composing selection into per-cell styles" to exactly this
facility. Property-bag setters would mean re-plumbing every builder for
post-hoc property injection, while style-slot injection needs **zero
component changes** and keeps the damage story trivially correct
(components already read `Style` inside `Render`). The grammar carries
`TargetType` precisely so per-type property rows CAN be added later as
builder-registered tables without re-deciding the syntax — and
swapping structure or non-style properties wholesale is what control
templates (#94) are for.

### Materialization

A `<Style>` parses once into a `styleDef` — target type, base setters,
per-state setter lists, source file for errors. Applying it to an
instance builds one computed:

```go
prop.NewComputed(func() render.Style {
    var s render.Style
    applyAll(base, &s)             // literals baked at load; Resource= closures Get their handles
    if hovered(target)  { applyAll(hover, &s) }
    if focusWithin(target) { applyAll(focus, &s) }
    if disabled(target) { applyAll(disabled, &s) }
    return s
})
```

and that computed IS the component's `Style` property. The builders'
existing `bindStyle` is the single seam: bound expression → viewmodel
handle (unchanged, wins); named key → scope chain (styleDef →
materialize; legacy `ctx.Styles` value → `Sty(...)` as today); absent →
implicit `TargetType` match or zero.

Note the conditional `Get`s are correct, not a hazard: a section's
resource reads re-record per evaluation, so an unfocused pane is NOT
subscribed to the focus-section's accent — the subscription narrows to
exactly the branch taken, which is the graph working as designed. The
hazard is the other one (Part 3).

### Composition with explicit attributes

Explicit element attributes beat style setters. The one overlap today
is `Text Bold="true"`, which already composes by WRAPPING the style
handle in a computed that forces the flag — that mechanism is the
general rule: an attribute that overlaps a styled field wraps the
resolved style, so it wins field-wise while everything else (including
live state overlays) shows through. `Background` does not collide:
setters touch the chrome style, `Background` is the container fill.

Components that today snapshot `ctx.Styles[...]` as a VALUE (ToastHost,
Tooltip `Style`, TextBox `AccentStyle`) resolve through the chain and
keep taking a snapshot — they were non-reactive before and stay so;
upgrading them to handles is incidental cleanup, not this design.

## Part 3 — Selectors and state (#57)

### The grammar, complete

- **Explicit**: `Style="key"` — nearest scope wins.
- **Type**: a `TargetType="Border"` style matches every `<Border>` in
  its scope that has no explicit style. The "type" is the ELEMENT name
  — the markup vocabulary, so it works identically for builtins,
  registered components, Includes, and UserControls, and it needs no
  reflection because the builder knows the element name at the moment
  it resolves the style. Nearest scope wins; there is no merging of an
  outer and inner type style.
- **State**: `:focus`, `:hover`, `:disabled` as sections within a
  style. There are no free-standing state selectors: state is a
  modifier of a matched style, which is what keeps matching a
  build-time decision and state a runtime read.

That is the whole grammar. Matching happens ONCE, at build. Nothing
matches at runtime; runtime is only the graph.

### Where state comes from

- **`:focus` means "this component or its subtree holds focus"** —
  WPF's `IsKeyboardFocusWithin`, collapsed with self-focus into one
  pseudo-class. The collapse is deliberate: for a leaf focus stop
  (Button, TextBox) within-semantics degenerate to self, and for the
  case that motivated this whole epic — the reader's focused PANE — the
  Border is not a focus stop at all; the ItemsView inside it is. A
  self-only `:focus` would make the flagship example inexpressible.
  (`:focus-self` is reserved if a container-that-is-also-a-stop ever
  needs the distinction.)

  Implementation: the style computed walks the target's subtree and
  reads EVERY focus stop's focused property before OR-ing. **The reads
  must not short-circuit** — `Get` records dependencies only when it
  actually runs, so an early `return true` after the first focused stop
  would leave the computed deaf to the stop that mattered. This is the
  documented Get-order hazard, and the damage-count tests are what
  catch a regression.
- **`:hover`** reads `HoverState` — self only, components that carry it.
- **`:disabled`** reads the target's command through `CanExecute`, the
  same read Button's own paint already performs — so it lands once #33's
  conventions say which property/Action a given component exposes; the
  section parses from day one and materializes per component as those
  seams exist.

Because the state reads happen inside the per-instance computed, and
the computed is read inside the instance's paint node, a state flip
invalidates exactly the styled instances whose resolved value changed.
No new event system. The counts, made explicit:

- hover flip on a styled component: **1 repaint**;
- focus move between two styled focus stops: **2 repaints** — the
  existing focus damage-count contract tests pass UNMODIFIED;
- focus move between two `:focus`-styled panes (borders styling on
  focus-within): **4 repaints** — the two stops and the two borders,
  each a component whose appearance actually changed, none other.

Two-phase wiring: the style handle is created while the component is
being built, but state reads need the finished component (and, for
within-semantics, its subtree — which exists, since children build
before their parent). The computed captures the target late, set by
`build()` after construction. Laziness makes this safe: a computed
never evaluates during build, only at first frame.

Known v1 limitation: a focus stop that ARRIVES via a Dynamic re-sync
after the style computed last evaluated is not yet among its recorded
deps; the computed re-records the full subtree on its next evaluation.
If a real case hits this, the Composer's structural re-sync can bump
styled ancestors' computeds the way it bumps `rev` — noted, not built.

### Cascade, totally ordered

1. `Style="{{.Handle}}"` — a bound style bypasses the system entirely.
   Stated per #55, load-bearing for colordemo: a page styled live from
   the color being picked keeps working through its bound property.
2. `Style="key"` — explicit resource style, nearest scope, falling back
   to `ctx.Styles`; unknown key is a load error.
3. Implicit `TargetType` match, nearest scope. `Style=""` opts out.
4. Nothing — zero style, the component's own defaults.

Within a matched style: base setters, then state overlays in the fixed
order **hover, focus, disabled** (disabled strongest — a disabled
control must not light up under the pointer). Fixed semantic order was
chosen over CSS's source order: three known states with a sensible
strength ranking beat a reordering footgun. Overlays are field-wise:
a section overrides only the fields its setters name.

## Part 4 — The laziness contract, hot reload, acceptance tests

Everything resolves in two moments and no others:

- **LOAD**: parse and validate — unknown setter property, bad literal,
  unknown state section, duplicate key, `Key`/`TargetType` both absent,
  `Value`+`Resource` both present, unknown `Style="name"` — all
  load-time errors carrying the file name via `fileError`.
- **BUILD**: match (explicit or type), resolve `Resource=` references
  to handles, materialize per-instance computeds.

After build there is no styling machinery left running — only source
properties (resources, focus, hover), computeds, and paint nodes. A
"style change" at runtime is a `Set` on a resource handle; "invalidate
the styles" is not an operation that exists. Dictionary identity swaps
(WPF `DynamicResource`) are explicitly not supported: the editing loop
is `Watch`, which rebuilds the whole tree — markup styles hot-reload
for free — and the runtime loop is `Set` on resources. One wrinkle
inherited from the graph: `prop.Set` never compares, so re-Setting a
resource to its current value still repaints its readers; guard hot
paths at the call site, as everywhere.

Damage-count acceptance tests, named here so the implementing issues
assert the same numbers:

- `TestHoverFlipOnStyledRepaintsOne` — styled Button, hover on: 1.
- `TestFocusMoveBetweenStyledRepaintsTwo` — two styled stops: 2; and
  the existing focus/hover contract tests pass unmodified.
- `TestFocusWithinPaneMoveRepaintsFour` — two `:focus`-styled Borders
  each wrapping a stop: 4.
- `TestResourceSetRepaintsExactlyReaders` — three components, two
  styled through `{accent}`: `Set` repaints 2, the third never paints.
- `TestSubtreeOverrideShadows` — inner scope redefines a key; outer
  readers keep the outer handle; `Set` on the outer repaints only
  outer readers.
- `TestUnknownStyleKeyFailsLoad`, `TestBadSetterFailsLoad` — the error
  names file, style, and offending property/value.

## Part 5 — Migration (#58)

**`cmd/finder` is the acceptance case.** Its theme is four static
entries (`panel`, `accent`, `dim`, `input`) with no state computeds:
the map moves verbatim into `<Gooey.Resources>` styles, the Go
`Styles:` block shrinks to nothing, and the recorded frames must come
out byte-identical (assert via the pty final-frame modelling recipe).
Net Go lines go down; nothing else moves.

`cmd/reader` is the SHOWCASE, under #54's own acceptance rather than
frame-identity: the `paneTitle`/`namedFocus` machinery — a viewmodel
computed that exists only to say "the focused pane's border is
accent-colored" with a dot, because it could not be said any other way
— is deleted in favor of a `:focus` section on the pane style, and the
demo is re-recorded (the GIF #54 asks for). `cmd/colordemo` migrates
its static entries only; its live `Style="{{.Handle}}"` page style is
the cascade-rule-1 regression test and must not change.

## Part 6 — What templates (#94) need us not to preclude

Design-ahead only; nothing here is committed by this record.

- **Resource scopes hold `any`.** A `<ControlTemplate>` is a future
  resource form — an element factory captured at load, ItemTemplate's
  pattern — living in the same scope chain. The scope's storage and
  save/restore must not assume styles and scalars are the only kinds.
  (Localization, #102, is another future form: string tables as
  resources.)
- **The setter grammar can grow rows.** `TargetType` is already in the
  grammar; a `Template` setter, or per-type property rows, extend the
  table without touching the spelling. v1 must not, e.g., bake "the
  Property attribute names a render.Style field" into the parse — the
  field table is consulted at validation, not grammar, level.
- **TemplateBinding is already invented.** A template's content binds
  `{{.Title}}` against the templated control's declared surface — and
  `x:Property` + `control()` already build exactly that context: the
  `DeclaredSurface` handles ARE the TemplateBinding targets, and
  template instantiation is context isolation with the declared handles
  pre-installed. Sketch, not commitment: the mechanism exists; the
  epic decides the spelling.
- **Type identity stays the element name**, so an implicit style
  matches a control the same whether or not it has been retemplated.

## Implementation plan (PR-sized stages)

1. **Resources core** (`markup/resources.go`): `Resources` slots parsed
   at any element; `<Resource>` with `propKinds` coercion; scope chain
   on `Context` with save/restore; `Style="name"` and the other
   `ctx.Styles` call sites resolve through the chain; unknown-key load
   error (after a demo survey); `Context.Resource`. Tests:
   shadowing, load errors, `TestResourceSetRepaintsExactlyReaders`,
   `TestSubtreeOverrideShadows`, every demo loads unchanged.
2. **Style declaration + explicit key**: `styleDef` parse/validation,
   the `styleFields` closure table, `Value`/`Resource` setters,
   materialization behind `bindStyle` for `Style="key"`. State sections
   are a LOAD ERROR at this stage ("not yet implemented"), never inert
   — accepted-but-ignored markup is the failure mode this framework
   refuses. Tests: `TestBadSetterFailsLoad`, explicit-style rendering,
   palette-`Set` repaint counts.
3. **Selectors and state**: implicit `TargetType` matching with
   `Style=""` opt-out; Focus/Hover(/Disabled as seams exist) sections
   live; late-bound target; the full damage-count suite above, with the
   existing focus/hover contract tests unmodified.
4. **Migration + docs**: finder migrated frame-identical; reader's
   `:focus` pane style replacing `paneTitle`/`namedFocus`, re-recorded;
   colordemo static entries; `docs/learn/` styles-and-resources
   chapter; README status rows and the "no styling system" lines in
   README and `docs/learn/index.md` removed.

## Explicitly out

- **Descendant/child combinators, class selectors, Name selectors** —
  explicit `Style="key"` already targets one element; `Name` stays the
  code-behind lookup surface, not a styling hook.
- **`BasedOn` / style inheritance** — copy the setters; revisit if a
  real theme grows past what duplication tolerates.
- **Setters over arbitrary component properties** — the `TargetType`
  growth path exists; nothing needs it yet (see Part 2).
- **`DynamicResource` / dictionary identity swap** — `Watch` covers
  editing; `Set` covers runtime.
- **Attribute-side resource references** (`Background="{accent}"`) —
  wants a reference spelling for ordinary attributes; a natural
  follow-on to stage 1, not v1.
- **Animations/transitions on state change**; **multi-`TargetType`
  lists**; **`:focus-self`** (reserved, unimplemented).
- **Markup-declarable attached properties** — still out, per the
  x:Property record.

## Invariants touched

No reflection (two closure tables, both `kindOf`-shaped). Get-call-site
subscription (state reads inside the style computed; the no-short-
circuit rule called out where it bites). Per-component paint nodes and
damage counts (the acceptance numbers above; existing contract tests
unmodified). Markup tiers (resources/styles are load-validated data;
the strict-and-loud rule extended to style keys). `fs.FS` seam
(untouched; styles ride the same documents). Document-scoped
save/restore (the resource scope chain uses it; resources are
deliberately AMBIENT across control boundaries where xmlns is
deliberately not — data isolates, theme inherits).
