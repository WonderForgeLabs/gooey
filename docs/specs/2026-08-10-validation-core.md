# Validation core: validators as computeds (issue #97, epic #161)

**Status:** executed, landed in [PR #172](https://github.com/WonderForgeLabs/gooey/pull/172).

**Date:** 2026-08-10

## What was asked for

Binding validation with error presentation (#97): a failing value marks
the offending control and shows why, instead of silently landing or
being hand-rolled per form; an aggregate is-valid gates submit (#33's
CanExecute). Epic #161 names the frame: **validators are computeds —
the graph IS INotifyDataErrorInfo.**

The XAML comparison is the design. WPF needs three mechanisms:
`ValidationRules` on the binding (where checks run),
`INotifyDataErrorInfo` on the viewmodel (how errors are announced), and
`ErrorTemplate` in the adorner layer (how they show). All three collapse
here into properties: a validator is a computed string over the source
(empty = valid), the input component reads it while painting (its
invalid visual is ordinary paint damage), an error display reads it
(inline `Text` or a floating adornment), and form validity is an
aggregate feeding `NewCommand(...).When()` — XAML's
`CanExecuteChanged`, already a property read. No events, no error
interfaces, no reflection.

Mid-build, Elan set the alignment target: **MAUI over WPF**. MAUI has
no adorner plane — its validation story is the CommunityToolkit
`ValidationBehavior`: a behavior attached to the input flips styles
(`InvalidStyle`), and errors display as ordinary bound labels. That
reshaped the surface (decider: Elan):

- **Inline error display is the primary pattern** — a bound `<Text>`
  under the field, optionally with `Visibility` bound to a has-error
  bool so the row collapses. The floating marker demotes to the
  compact-layout variant.
- The TextBox style knob is named **`InvalidStyle`** (MAUI's word).
- **`<Validate>` is a behavior in markup**, declared on the input —
  bare child or the new `<X.Behaviors>` property-element slot (MAUI's
  explicit spelling; both feed the same attachment list).

## Executed

### `validate/` — the vocabulary (new top-level package)

Placement decision: not `prop` (which stays the minimal graph — no
domain vocabulary), not `components` (validation is viewmodel-side, no
UI), not root (root is contracts). `validate` imports `prop` only.

```go
type Rule[T any] func(T) string                     // "" = pass; the escape hatch IS the type
func Field[T any](src *prop.Property[T], rules ...Rule[T]) *prop.Property[string]
func All(fields ...*prop.Property[string]) *prop.Property[bool]
func Has(field *prop.Property[string]) *prop.Property[bool]
func Required(msg string) Rule[string]              // empty msg = stock message, all rules
func Len(min, max int, msg string) Rule[string]     // runes; max<=0 = unbounded
func Pattern(expr, msg string) Rule[string]         // compiled ONCE; panics like MustCompile
func Range[T cmp.Ordered](min, max T, msg string) Rule[T]
```

- **First failing rule wins** — messages reveal one problem at a time,
  in declaration order (fundamentals before specifics).
- Non-Required string rules **pass empty input**, so "optional but
  well-formed when present" is spelled by omitting `Required`.
- A rule that reads other properties subscribes by reading —
  cross-field validation needs nothing extra.
- **`All` is value-stabilized, the one deliberately eager corner.**
  Invalidation is eager and value-blind, so a plain computed gate would
  dirty the submit button's paint node on every keystroke into any
  field. `All` instead hangs an `OnInvalidate` hook on an inner
  aggregate computed and `Set`s the outward source only when validity
  actually flipped — a keystroke that leaves validity alone never
  touches the button, and the flip repaints it exactly once. The cost:
  the aggregate (and the dirty validators under it) evaluates during
  `Set` rather than at frame time — work that was about to happen for
  the error display anyway, moved, not added; built entirely on prop's
  public API. Subscription is by dependency edges, not by claiming the
  fields' hooks, so any number of gates share a field
  (`TestTwoAllsShareAField`).

### TextBox error state

`Error *prop.Property[string]` (empty = valid) and `InvalidStyle
*prop.Property[render.Style]`. Render reads the error **before its
bounds early-return** (the Get-order rule); non-empty flips the text
into `InvalidStyle`, defaulting to the shared error red + underline.
Pinned: an error flip repaints exactly the TextBox
(`TestTextBoxErrorFlipRepaintsTheTextBoxAlone`).

This read is where a future `:invalid` pseudo-class looks (#54): the
styles design collapses state pseudo-classes to reads inside the style
computed, and `Error != ""` is precisely such a read. Noted, not built.

### DataAnnotations parity

Elan, on the PR: "let's offer some built in validators to match data
annotations." The built-in vocabulary is now .NET's, in both surfaces —
a Go constructor and a `<Validate>` attribute per rule, one engine:

| Annotation | gooey rule | Markup attribute |
|---|---|---|
| `[Required]` | `Required(msg)` | `Required="true"` |
| `[StringLength]` / `[MinLength]` / `[MaxLength]` | `Len(min,max,msg)`, `MinLen(n,msg)`, `MaxLen(n,msg)` | `MinLen` / `MaxLen` |
| `[RegularExpression]` | `Pattern(expr,msg)` | `Pattern` |
| `[EmailAddress]` | `EmailAddress(msg)` | `EmailAddress="true"` |
| `[Url]` | `URL(msg)` | `Url="true"` |
| `[Phone]` | `Phone(msg)` | `Phone="true"` |
| `[CreditCard]` | `CreditCard(msg)` | `CreditCard="true"` |
| `[Range]` (typed) | `Range[T cmp.Ordered](min,max,msg)` | — (needs a typed input; NumberBox) |
| `[Range]` (over text) | `NumberRange(min,max,msg)` | `MinValue` / `MaxValue` |
| `[Compare]` | `Compare[T comparable](other,msg)` | `Compare=".Password"` |
| — (numeric-string guards) | `Digits(msg)`, `Integer(msg)` | `Digits="true"` / `Integer="true"` |
| `ErrorMessage` | every constructor's `msg` (empty = stock) | `Message="…"` (field-level) |

No third-party dependencies (root dep budget: `cmp`, `fmt`, `math`,
`net/url`, `regexp`, `strconv`, `strings`). Fixed patterns compile once
at package init, so even construction is free. Every rule passes empty
input except `Required`.

**Deliberate divergences from .NET's implementations**, stated in the
code and the reference: `EmailAddress` requires a dotted domain (.NET
accepts `a@b`) while staying far looser than RFC 5322 and unicode-open;
`URL` additionally requires a non-empty host (`url.Parse` yields an
empty hostname for `http://` — the classic bypass); `Phone` requires
7–15 digits excluding any extension (.NET has no count rule);
`CreditCard` adds a 12–19 digit window to Luhn (.NET accepts a bare
`0`); `Digits` is ASCII-only on purpose. Markup keeps **one spelling**
— `Pattern`, not an additional `RegularExpression` alias — because
gooey has one canonical spelling per concept everywhere else; the
parity table is how a reader finds it from the annotation's name.

`ctx.Rules` is explicitly the layer **beyond** this set: domain rules
(an internal account-number format, a reserved-name list, a lookup
check) stay app-registered, the built-ins cover the annotations.

### `<Validate>` — the markup behavior (`markup/validate.go`)

Built like any non-visual attachment; the **host's builder** wires it
(`wireValidate`) because children build before parents: the TextBox
case materializes `validate.Field` against its bound `Text` source,
takes the `Error` slot (declaring both is a load error), and
**publishes** the computed into the context. `Into=".NameErr"` names
the key; omitted, it derives from the Text binding path
(`Text="{{.Name}}"` → `NameErr`; multi-segment paths must say `Into`).
Publication **overwrites** — a hot reload re-registers on every
rebuild, and a collision error would fail every second load; stale
handles from earlier loads stay correct anyway, since they read the
same sources. Rules run built-ins first (`Required`,
`MinLen`/`MaxLen`, `Pattern` — attribute order is XML-meaningless),
then registered rules in name order. A `<Validate>` no builder wired is
a load error (`attachAll`), so hosts that do not speak validation
refuse it instead of carrying a rule that never runs.

**`ctx.Rules`** extends the vocabulary. Deliberated: built in first,
questioned by Elan mid-review (hold + removal), then **kept** on his
governing directive — "generally speaking, keep as much in the xaml as
you can". The registry is the mechanism that keeps domain validation in
markup: the app registers `"Email"` once (rule bodies stay code — the
capability-grant shape of `ctx.Components`/`ctx.Handlers`), every page
writes `<Validate Email="true"/>`. Constructors get the attribute
literal at load and may reject it (typed load error); the unknown-rule
error names built-ins and registered rules both.

### `<X.Behaviors>` — the universal attachment slot

`checkProps` admits `Behaviors` on every element; `buildChildren`
routes the slot's children into the same attachment list bare
non-visual children feed — two spellings, one path, mixable
(`TestBehaviorsSlotEqualsBareChild`,
`TestValidateDerivedIntoAndMixedBehaviors`). A visual child in the slot
is a load error naming it.

### ValidationMarker — the floating variant, and the adornment feedback

`components.ValidationMarker` is the AdornmentLayer's **second
customer** (#91 named validation markers from the start), and the spec
asked it to confirm the interface before it calcified. It did — three
findings:

1. **The `Adornment` interface held.** Anchor/Place fit exactly; the
   popup is a leaf whose `Place` collapses to a zero rect while the
   error is empty — the Popup primitive's subscription-carrier posture
   (Render reads the error before any early return), so the first
   failing edit schedules its own frame and appearing/vanishing is the
   bounds sweep's ordinary damage. No `Add`/`Remove` churn per flip.
2. **Drop-on-invisible assumed transient adornments.** A tooltip's
   owner re-raises on the next hover; a persistent marker has no
   gesture, and eager re-adding would fight the layer's sweep (one add
   + one drop per frame, forever). New optional refinement:
   `PersistentAdornment` (`AdornmentPersists()`) — an anchor merely
   invisible (in tree, but Hidden/Collapsed/zero-arranged) hides the
   adornment at a zero rect instead of dropping it; only an anchor that
   left the tree drops it (with `orphaned()`, as before). Pinned by
   `TestMarkerPersistsThroughHiddenAnchor` /
   `TestMarkerOrphanedWhenHostLeavesAndReturns`.
3. **The layer was force-repainted by every covered leaf beneath it.**
   The z-ordered pass forces nodes above any painter — and the
   full-page, paints-nothing layer sat above everything, so every
   keystroke into a TextBox on a layer-hosting page cost one no-op
   paint with a full-page damage rect. `AdornmentLayer` now implements
   `Decorator` (`DecoratesCells`), the existing exemption for
   cell-less painters. Found by the marker's damage pins. **ToastHost
   has the same latent cost** — same one-liner, left for its own
   change.

The marker attaches like Tooltip (non-visual child), adopts its host
TextBox's `Error` when it carries none, places via `PlacePopup`
(below, flip-to-fit, clamped), is `HitTestTransparent` (a message must
never trap the pointer on its way to the field), and re-places itself
through the input-tree walk (`SetFocusManager`) after a structural
drop-and-return. A page without a layer degrades to inline-only.

### The damage contract (`TestValidationLoopDamage`)

On the form page (TextBox + filler + gated Button + layer):

- invalid→invalid edit, message unchanged: **2** paints (TextBox +
  marker) — the button untouched, `All`'s stabilization made visible;
- message resized: + restored filler and swept containers, the
  moved-overlay cost the tooltip pins established;
- the validity flip: the button repaints **once**, the vacated float
  restores beneath;
- settled frames: 0.

## Docs and demo

`docs/learn/howto/howto-forms.md` + runnable
`docs/learn/examples/howto-forms/` (three fields, Behaviors+Validate
primary form, inline error rows, one floating marker, gated submit),
learn-map row, README status row, markup-reference sections (TextBox
additions, Validate, ValidationMarker, the Behaviors slot). Verified in
a real pty: first frame shows both required messages, `ab` shows the
MinLen message, completing the form enables submit, enter saves.

## Invariants touched

- **Lazy evaluation (invariant 2), knowingly bent at one seam:**
  `validate.All` evaluates its aggregate during `Set` to buy the value
  cutoff. Library-tier (public API only), the fields were about to
  evaluate for display anyway, and the alternative — gated buttons
  repainting on every keystroke — is XAML parity nobody wants
  (`CommandManager.RequerySuggested` fires on everything). Framework
  core unchanged.
- **Damage discipline (invariant 3): strengthened.** The Decorator fix
  removes a spurious full-page repaint from every layer-hosting page;
  new pins throughout.
- **No reflection (invariant 1):** generics + closures; markup wiring
  is type-switches; `ctx.Rules` is a typed registry.
- **Markup lvalue semantics (invariant 5):** `Error` is a typed handle
  binding (load-time type error); `<Validate>` publication happens at
  build, so later bindings resolve handles as always.

## Not in this wave

Typed `Range` in markup for a future NumberBox (`validate.Range` exists
Go-side; `wireValidate` is host-generic on purpose, and `MinValue`/
`MaxValue` cover text fields today), per-rule
`Message` attributes in markup (field-level `Message` ships;
per-rule wording is a Go-side `validate.Field`), a `[FileExtensions]`
equivalent (no file input yet), validate-on-unfocused timing (MAUI's
option; today validators are live per keystroke — a `ValidateOn`
attribute could sample on focus-out), publishing the has-error bool
alongside `Into`, markup-declared rule *expressions* (needs the
converter/expression story — #99/#54), the `:invalid` pseudo-class
(#54, noted above), the ToastHost Decorator one-liner, and the epic's
GIF (the docs-and-demos workflow re-records in batch).
