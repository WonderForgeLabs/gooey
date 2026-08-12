# The design surface: COD, edit/runtime vocabularies, selection, and a property grid (design)

Status: proposed. Nothing here is built. Its prerequisite —
`docs/specs/2026-08-11-component-catalog-and-wysiwyg-builder.md` — is
built and running, and this record is the second half of the same
question: the catalog answers *what may I set on this element*, and a
design surface answers *how do I set it by pointing at the thing*.

The brief is eight notes, given verbatim:

> 1. drag and drop isn't working, i have to double click.
> 2. inspector is read only and doesn't show binding or enum.
> 3. Why is halign orange when you double click?
> 4. make this property browser behave like the visualstudio one.
> 5. What i want is kinda like component model for design time also
> 6. in the short term, we can host the component under design "COD" as a
>    single child in our non rendering component that steals ALL signals
>    from it. it is just a render.
> 7. i want the grippers to size. i want essentially visual studio or blends
> 8. We'll call that edit mode and then we'll have runtime mode.

Notes 2–4 were the inspector failing to consume a model it already had,
and they are done (see "What already shipped"). Note 3 in particular was
not a feature: `{{.EditName}}` carried `Style="warn"`, so the selection
cue wore a warning colour. Note 1 was not a broken drag — there is no
mouse code in the editor at all, and double-click is `ItemsView`'s
activation. The remaining five notes are one design, and it is this one.

## The central finding: opacity and addressability are the same walk

Note 6 reads as an instruction: hide the component under a host that
takes its signals. Implemented literally — a one-child host that is *not*
a `Container` and paints its child from its own `Render` — it works, in
the sense that no input can reach the child. It also silently removes
four other things, because `Container.ChildComponents()` is not one seam.
It is four:

| Seam | Consumer | What it gives |
|---|---|---|
| damage | `Composer.build` (`composer.go:283`) | each child gets its own paint node |
| input | `FocusManager.walk` (`input.go:388`), `hitTest` (`mouse.go:73`) | focus order, key bindings, hit-testing |
| adornment | `visiblyReachable` (`components/adorn.go:146`) | an adornment's anchor must be reachable from the root |
| lifecycle | `Composer` startable discovery (`composer.go:398,403`) | timers, spinners, progress bars tick |

A design surface wants five things from its subtree:

1. the child renders,
2. the child receives no input,
3. selection is drawn per element, anchored to that element,
4. clicking an element selects it,
5. an edit repaints only what changed.

Only (2) wants the walk stopped. (1), (3), (4) and (5) all need it. So
the literal reading of note 6 buys one requirement at the cost of three
and a framework invariant — every component's `Render` is its own paint
node — collapsing the whole surface into a single node whose damage
count is 1 for any change anywhere inside it.

The intent behind note 6 is right and the mechanism that reads most
naturally out of it is wrong. **What has to be stolen is not the child;
it is the routing.**

### What "all signals" actually means, mechanically

Everything that can reach a component from outside, and where it enters:

- **keys** — only through `FocusManager.Dispatch` (`input.go:574`),
  which walks focused → ancestors. A component is reachable only if
  `walk` put it in `m.order`, which needs `Focusable` and
  `AcceptsFocus()`.
- **key bindings** — `m.bindings[host]`, collected by the same walk from
  each component's attachments.
- **mnemonics** — same walk, `MnemonicHandler`.
- **mouse** — `DispatchMouse` (`mouse.go:153`) routes to
  `m.target(hit)`, and `hit` is the deepest component under the pointer.
- **hover** — `m.setHover(hit)` (`mouse.go:168`), a source property on
  the hit component; a hovered button repaints itself.
- **focus-follows-click** — `m.focusTargetFor(hit)`, nearest focusable
  at-or-above the hit.
- **hover watchers** — attachments (a `Tooltip`) registered by the walk.

Seven entry points, all of them in two files, and all of them keyed on
either *membership in the focus order* or *the identity of the hit*.
Neither requires hiding the subtree.

## The seam: a frozen subtree

Ruled by Elan: *"i want it to render, i just don't want it to do stuff.
you click a button, in design mode it just sits there like a still pic."*

So the seam is **frozen**, not "input boundary" — the walk continues, as
above, but the scope is wider than input. It is one marker interface in
the root package, beside `HitTestTransparent` and `Decorator`, the two
existing precedents for "a component tells the input/damage system
something about itself":

```go
// Frozen is implemented by a component whose subtree renders but does
// not act: it takes every event addressed anywhere inside it, and
// nothing inside it runs. Descendants lay out, paint, and keep their own
// paint nodes — the picture is live, the behaviour is not. A design
// surface is the motivating case; a preview pane and a disabled subtree
// are the others.
type Frozen interface{ Frozen() bool }
```

What is frozen, stated as a list because "all signals" was too vague to
implement from:

| Blocked | Where |
|---|---|
| keys reaching a descendant | `walk` — no `m.order` entry |
| `KeyBinding`s **scoped inside** the subtree | `walk` — no `m.bindings` entry |
| mnemonics | `walk` — no `m.mnemonics` entry |
| clicks and drags | `target` retarget |
| **wheel** | same — `DispatchMouse` routes wheel through `target` too |
| hover watchers (a `Tooltip` popping up) | `walk` — no `m.watchers` entry |
| focus (a frozen subtree is **not tabbable**) | `walk` — no `m.order` entry |
| **every `Startable`** | `Composer.collect` |

Two of those are their own decisions and are argued below. The rest fall
out of the walk.

`KeyBinding`s deserve the separate row: blocking `HandleKey` does not
block them. `FocusManager.Dispatch` interleaves each level's scoped
bindings with that level's `HandleKey` (`input.go:574`), so they are two
independent routes to the same component and freezing has to cut both.

Four touch points, each mirroring a pattern already in the file:

1. **`FocusManager.walk` (`input.go:388`)** — while inside a frozen
   subtree, keep recording `m.parent` (capture and hover liveness need
   it, and so does `depth`/`ancestor`), keep `FocusHost` wiring, and
   register **nothing targetable**: no `m.order` entry, no `m.bindings`,
   no `m.mnemonics`, no `m.watchers`.
2. **`FocusManager.target` (`mouse.go:217`)** — retarget upward: the
   captor if captured, otherwise the nearest frozen ancestor at-or-above
   the hit, otherwise the hit. This is `focusTargetFor` with a different
   predicate, and it lands in the one function every mouse kind — press,
   release, motion and **wheel** — already routes through.
3. **`setHover` (`mouse.go:168`)** — the same retarget, so a button in
   the surface does not light up under the pointer. See below for why
   hover is nonetheless not fully frozen.
4. **`Composer.collect` (`composer.go:395`)** — do not append a
   descendant `Startable` to `c.startable`. This is the widest of the
   four and the one with a safety argument rather than a UX one.

What deliberately does **not** change:

- **`hitTest` still returns the deepest component.** The boundary
  constrains *dispatch*, not the query. That distinction is what lets
  the editor call `FocusManager.HitTest(x, y)`, get the actual `<Button>`
  under the pointer, and select it — while the framework's own routing
  hands the press to the boundary. Putting the stop inside `hitTest`
  would have made click-to-select impossible, which is requirement (4).

  **This absence needs a comment at `hitTest` itself.** It reads as an
  oversight — every other input path grew a boundary check and this one
  did not — so the next reader will "fix" it, all five tests below will
  stay green, and click-to-select will break with nothing to say why.
  The comment is part of the change, not documentation of it.
- **Damage.** Every descendant keeps its paint node. The
  design-surface subtree repaints exactly like any other subtree, and
  the existing damage-count contract tests are untouched. A change that
  moved those numbers would be a regression of the fourth invariant, and
  this one does not.
- **Adornment reachability.** Descendants stay reachable from the root
  through `Container`, so a selection border can anchor to the real
  element. Under the literal reading of note 6 it could not: the anchor
  would fail `visiblyReachable` and `AdornmentLayer.Arrange` would drop
  the adornment on the frame it was added.

### Startables are frozen, and `<Companion>` is why that is not cosmetic

Elan: *"i don't want their timer ticks to fire."* Reversed from this
document's first draft, and the reversal is right for a reason stronger
than animation policy.

`components.Companion` is a `Startable` whose `Start` **spawns a child
process** (`components/companion.go:133`). A frozen tree that still
started its Startables would launch a subprocess the moment somebody
dropped a `<Companion>` on the design canvas — a side effect outside the
process, from an editor gesture, with no way to have consented to it.
`<Timer>` firing a `Command` is a nuisance; this is not.

That earns a named test of its own —
`TestFreezingAComponentDoesNotSpawnItsProcess` — because it is the one
freeze failure whose consequence survives the editor exiting.

The `Composer` is the right place: it already *owns* the lifetime of
everything running inside a composition (`composer.go:389` says so), so
declining to start a subtree is an existing responsibility taking a new
input, not a new mechanism.

### Two things stay live, deliberately

**Validators.** They are computeds, they evaluate during paint, and their
evaluation is what puts the validation marker on screen. Freezing them
would freeze the *picture*, which is the one thing the surface must not
do. Consequence to state plainly: **a validator with a side effect gets
that side effect anyway**, and nothing in the freeze can stop it — a
validator is supposed to be a pure function of its input, and this is the
first place where that convention becomes load-bearing rather than
stylistic.

**Hover styling.** Elan asked for XAML-style mouseover. `HoverState` is
an ordinary source property (`mouse.go:49`), so a style that reads it
repaints for free and costs the freeze nothing — what is lost is only
motion *over time*, because gooey has no animation system and an
animation needs a clock, which in this framework is a lifetime
(`cmd/browser/gifplay.go:14`). So "freeze the clocks" and "animate on
hover" are the same mechanism pointing in two directions, and the split
falls exactly where the framework already splits: state-driven restyle
yes, tweened motion no. No new mechanism, and nothing promised that
cannot be delivered.

Note the tension with touch point 3: hover is retargeted so the *hovered
component* is the frozen host rather than the descendant. A design
surface that wants descendant hover styling has to opt back in, and that
is a later decision — the safe default is that a frozen picture does not
react to the pointer.

### How to test it so that removing it breaks it

The barrier lesson from the transport work applies directly: a test that
passes with the protected thing deleted protects nothing.

- **Focus:** build a page with a `<TextBox>` inside a frozen host and one
  outside; assert `FocusManager` order length is 1 and the entry is the
  outside box. Delete the `walk` change and it goes to 2. This also pins
  "not tabbable", which is the same fact.
- **KeyBindings:** put a `<KeyBinding Gesture="s">` on an element inside,
  focus the host, press `s`; assert the command did not fire. This one
  fails independently of the `HandleKey` test — two routes, two pins.
- **Mouse:** press on the inner box's cell; assert the frozen host
  received it and the box's caret did not move. Delete the `target`
  change and the caret moves.
- **Wheel:** scroll over a scrollable descendant; assert it did not
  scroll. Same touch point as the press, different `MouseKind`, and the
  class this document originally failed to list.
- **Hover:** move over a `<Button>` inside; assert `IsHovered()` is false
  on the button. Delete the `setHover` change and it is true.
- **Startables — `TestFreezingAComponentDoesNotSpawnItsProcess`:** put a
  `<Companion Path="…">` inside, compose a frame, assert no process was
  spawned. Assert on the **process**, not on `len(c.startable)`: a count
  is satisfied by a refactor that moves the append elsewhere, and the
  thing being prevented is a subprocess.
- **Damage:** `Set` one property read by one component inside the
  surface; assert `PaintedLastFrame() == 1`. This is the pin that would
  catch the literal note-6 implementation — under a non-`Container` host
  the count is 1 as well, but the *rect* is the whole surface, so assert
  `Composer.Damage()` is the component's rect, not the host's.
- **Adornment:** anchor an adornment to a component inside the frozen
  host, run a frame, assert it is still up. Under the literal
  implementation it is dropped on frame 1.
- **Validators stay live:** put an invalid `<TextBox>` with a
  `<Validate>` inside; assert the marker appears. This is the pin that a
  later "freeze everything" simplification would break, and it is the
  one that protects the picture.

## What the implementation changed

Three things moved between the design above and the code. The first is a
simplification, the second and third are the same defect this project
keeps meeting.

### The retarget is ONE seam, not two

The design put a `Frozen` check in `FocusManager.target` and another in
`setHover`. Both were written, both worked, and the press test failed
anyway: `DispatchMouse` sets the implicit captor **from the raw hit,
before routing** (`mouse.go:180`), and `target` returns the captor first —
so the descendant got the event back through its own capture.

The fix is to retarget once, at the top of `DispatchMouse`, and let
everything downstream hold the effective hit: the captor a press takes,
the focus a press moves, the click synthesized on release, hover, and
every kind that routes through `target`. `target` and `setHover` are back
to their original bodies with a comment saying why the check is not
there.

That also fixed a *test* problem. With the check in two places, deleting
either one left the tests green — the classic reason a guard looks
verified and is not. One seam, one deletion, seven failures.

### The KeyBinding freeze is defence in depth, not the guarantee

The design listed "KeyBindings scoped inside the subtree" as a separate
blocked route, correctly: `Dispatch` interleaves each level's bindings
with that level's `HandleKey`, so they are two independent doors.

Writing the test showed the claim was stronger than the evidence could
support. The obvious test — press the gesture, require nothing to fire —
passed frozen **and** passed unfrozen, because a scoped binding only
fires while the focused chain passes through its host, and with focus
frozen out of the subtree the chain can never get there. The registration
skip is real and kept, but it was already unreachable.

So the pin is the reachable, consequential thing instead:
`TestFocusCannotBeSetIntoAFrozenSubtree`. An explicit `SetFocus` — the
route the control plane's `focus` act takes, by name — must be refused,
or a remote caller can put the caret in a design surface and type into a
picture. The code carries the honest note at the skip.

### Two probes that could not fail, found by trying to break them

Both were mine, both caught by the discipline rather than by review:

- **The wheel test asserted only that the host received the event**, and
  passed with the retarget deleted — because events *bubble*, so the host
  sees anything its descendants decline. A "the host got it" assertion
  cannot distinguish retargeting from bubbling. The fix needs a
  descendant that records **and consumes**: frozen, the sink sees nothing
  and the host sees the wheel; unfrozen, exactly the reverse.
- **The damage test's rect assertion was vacuous**, because the wrapper
  held a single child and a one-child wrapper's bounds equal its child's.
  A host that repainted its whole subtree would have produced the same
  rect. The page now puts two texts inside, and the test asserts the
  precondition — host bounds ≠ child bounds — before asserting the rect.

Ten tests, each verified to fail with its own change reverted, plus a
control for every freeze assertion. The controls are not ceremony: the
`<Companion>` control is what proves the harness can observe a spawn at
all, and the KeyBinding control is what exposed the unreachable claim.

## Middleware: the `Decorate` seam, pressure-tested and rejected

Elan asked whether the frozen host is "an extensibility point for
middleware". The proposal that came back was sharp, and it deserved a
real test rather than an opinion, because it turned an objection of mine
into an argument for the opposite conclusion.

The objection was: a per-element wrapper cannot work, because `named`
registers whatever `Build` returns, so a wrapper captures every `Name=`
address. The counter was that this is true of a wrapper installed at
`Build` and **false of one installed inside `named` itself**, which can
register the inner and return the outer:

```go
if n := e.Attrs["Name"]; n != "" { ctx.Named[n] = w }   // register INNER
if ctx.Decorate != nil { w = ctx.Decorate(e, w) }       // return OUTER
```

The funnel is real — `buildComponent`'s three arms all return through
`named` (`markup/markup.go:693`, `:699`, `:708`, `named` at `:766`). The
seam would work. **It breaks patching, which is measured, not argued.**

### What was measured

`examples/wysiwyg/decorate_probe_test.go` builds
`<VStack><Text Name="T"/></VStack>`, splices a transparent one-child
container between the `VStack` and the `Text` — exactly the topology
`Decorate` produces — leaves `Named["T"]` on the inner element, and calls
`control.Service.PatchMarkup`. Both directions, in one test:

- undecorated: patches;
- decorated: `element "T" sits inside a *main.decorator, which
  PatchMarkup cannot rewrite; supported parents are VStack, HStack, Grid,
  Canvas, ButtonBar and Border`.

The cause is `control.childSlot` (`control/markup.go:307`): a **closed
type switch over six concrete container types**. `PatchMarkup` finds its
target's parent and then asks that switch for a write slot; a decorator
declared anywhere outside `control/` can never be a member.

So the register-inner-return-outer trick does not rescue wrapping — it
**moves the breakage** from `Find[T]` (register-outer) to `PatchMarkup`
(register-inner). Both forms break something, and the asymmetry is what
decides it: `PatchMarkup` is the operation this editor is built on, and
it fails at runtime with an error about a component the document author
never wrote.

### The general constraint, which is bigger than this proposal

**Nothing may sit between a container and a named element.** That is a
framework-wide statement, and it was not written down anywhere before
this probe. It applies to any one-child container, decorator or not —
including this editor's own `<Preview>`, which is why nothing inside the
preview island is patchable (no live impact today: local mode patches
nothing, and remote mode patches the *target's* tree).

The probe is kept as the pin. It asserts a limitation, so it will fail
the day `childSlot` stops being closed — which would be the fix, not a
regression.

### What this does not kill

Middleware as an idea survives; `Decorate` as a per-element wrapper does
not. The actual obstacle is that `childSlot` is closed, and opening it —
a `ChildSlotter` interface containers implement, so `control/` stops
enumerating them — would unblock decorators generally and is worth
considering on its own merits. It is a change in `control/` and
`components/`, so it is a proposal for their owner, not a task here.

And the design surface **does not need it either way**. The frozen host
is *one node at the root of the surface*, above everything named in the
document, so no named element ever has an interposed parent. Wrapping
was only ever needed for per-element identity, and that is a
`map[gooey.Component]*node` recorded by the edit vocabulary's `Build`
closure — no extra tree nodes, no interposition, no patching cost. The
measurement therefore confirms the shape this document already had,
which is the least interesting way for a good idea to be right.

## COD: what it is, where it lives, and what it is not

**COD is an editor component, not a framework one.** The framework gains
the `InputBoundary` interface and three lines of routing; everything else
lives in `examples/wysiwyg`. That split matters because the seam is
generally useful (a read-only preview, a disabled subtree, a thumbnail)
while a design surface is not something the framework should have an
opinion about.

COD is:

- a one-child container (`ChildComponents` returns the built document
  root — the walk is preserved),
- `InputBoundary() bool { return true }`,
- `Focusable` — it is the focus stop that receives the sizing and moving
  keys, since nothing under it can be focused any more,
- the owner of a `map[gooey.Component]*node`, recorded at build time,
  which is how a hit-test result becomes a document selection.

**COD is one node at the root of the surface, not a wrapper per
element.** Three reasons, and the second is decisive:

1. A wrapper per element is redundant once the boundary is at the root —
   descendants are already untargetable.
2. **`named()` would register the wrapper.** The dispatcher applies
   `named()` once to whatever `Build` returns (`markup/markup.go:766`),
   so an element whose builder returns a wrapper puts the *wrapper* in
   `Context.Named`. Every consumer of the address — `Find[T]`,
   `patch_markup`'s fragment-root rule, focus by name, the MCP tree
   snapshot — would then resolve to a component that is not the element.
   That is the addressing hazard `KindIdentity` exists to warn about,
   reintroduced by the tool built to make addressing visible.
3. A wrapper is never perfectly transparent in layout. It has to
   implement `Measure`/`Arrange` and route through
   `MeasureChild`/`ArrangeChild`, and the margin/size/align/visibility
   sandwich then applies at the wrapper rather than at the element.

The identity map replaces the wrapper and costs nothing: the edit
vocabulary's `Build` closure calls the real builder and records the
result against the `Element` it came from.

## Edit and runtime are two vocabularies, not a mode flag

Note 8 names two modes. The implementation is not a branch inside every
component; it is **which `markup.Context` the document was built with** —
the mirror's mechanism generalised. The editor already keeps two
contexts for a different reason (`ctx` for its own shell, `docCtx` for
the document), and this adds a third axis to the second one:

```go
// runtime: the document builds exactly as the target app would build it.
runtime := ed.docCtx

// edit: same vocabulary, same bindings, same styles — plus a Build
// wrapper per element that records component -> node.
edit := ed.docCtx.withRecorder(ed.cod.record)
```

Consequences worth stating because they are not obvious:

- **A component cannot tell which mode it is in, and must not be able
  to.** There is no `DesignMode` property to read. If a component needs
  to behave differently at design time, the answer is a different
  registration in the edit vocabulary — which is exactly what `<Preview>`
  already does, building the Escher mirror in the document vocabulary and
  the real pane in the editor's.
- **The mode is a property of the document, not of the app.** The same
  process can hold an edit-mode surface and a runtime-mode preview side
  by side, because they are two builds.
- **Remote mode has no edit vocabulary at all.** When the editor is
  driving another app over `Attach`, that app builds with *its* context
  and the editor cannot wrap anything. So COD, selection, and grippers
  are **local-preview features**. In remote mode selection is expressed
  the way the protocol already expresses it — by `Name=`, with bounds
  read from `tree_snapshot` — and drawing a border in the target would
  need either an act the protocol does not have or a patched
  `AdornmentLayer` in the target's own markup. That is out of scope here
  and should be its own record; what matters now is not shipping an
  editor whose selection silently does nothing when attached.

## Selection and sizing

### Where the other seven handles went

A reviewer who knows Visual Studio will ask, so: **there is no
eight-handle model, deliberately.**

Eight handles assume a pixel canvas with sub-glyph precision. The
terminal has neither. A `<Text>` is **one cell tall** — the top-middle
and bottom-middle handles would occupy the same cell as each other and as
the content; a one-cell-wide element has the same problem on the other
axis; and there is no interior to grab on either. Placing eight
half-visible handles on a 1×N rect would be the editor offering a
capability the medium cannot support, which is the rule this project has
now rediscovered in seven other places.

What replaces them:

- **Selection is a border drawn in the `AdornmentLayer`, outside the
  element's rect** where the surface has room for it, degrading to a
  styled outline *on* the boundary cells where it does not. The adornment
  layer is the right home because anchoring is re-evaluated every frame
  by `AdornmentLayer.Arrange` for free — a resized element drags its
  selection border along with no subscription.
- **Sizing and moving are keyboard-first.** Arrows resize, shift+arrows
  move, tab / ctrl+n / ctrl+p change the selection. Keyboard-first is
  also the only testable choice: mouse input cannot be injected through a
  recording pty, so a mouse-only affordance can never be covered by the
  headless harness or shown in a GIF.
- **Exactly one mouse handle**, at bottom-right, and only where the
  element is ≥2 cells in both axes. One handle in one corner is
  unambiguous at cell resolution; eight are not.

### What a resize actually writes, and why availability is a catalog question

Resizing writes `Width` and `Height` — `KindInt`, `BindsLiteral`, rows of
`universalAttrs` (`markup/catalog.go:225`). Moving writes whatever the
**parent** contributes, and that is the same rule the inspector already
follows through `AttrsFor(spec, parent)`:

| Parent | Move writes | Resize writes |
|---|---|---|
| `<Canvas>` | `Canvas.Left`, `Canvas.Top` | `Width`, `Height` |
| `<Grid>` | `Grid.Row`, `Grid.Col` (and spans) | `Width`, `Height`, or the track spec |
| `<VStack>` / `<HStack>` | **nothing** — moving is reordering children | `Width`, `Height` |
| `<Border>` (`ModeOne`) | **nothing** — one child, no position | `Width`, `Height` |

So the gesture set is **derived from the catalog, not hardcoded**. Under
a `<VStack>` the arrow keys must reorder rather than write a position,
because there is no position attribute to write; under a `<Border>` the
move gesture must not be offered at all. Hardcoding eight handles and
four directions would produce an editor that lets you drag an element
around inside a `<VStack>` and then snaps it back — the exact silent-drop
failure the catalog was built to delete, resurfacing as a gesture instead
of an attribute.

This is also the concrete answer to note 7. "Grippers like Visual Studio
or Blend" is achievable in the parts that survive the medium; the part
that does not is the pixel-canvas assumption, and the catalog is what
tells us, per element and per parent, which parts those are.

## The property grid

### What the model has, and what a VS-grade grid needs

`AttrSpec` today (`markup/catalog.go:135`):

| Field | Present | Grid uses it for |
|---|---|---|
| `Name` | yes | the row label |
| `Kind` | yes | which editor to show |
| `Enum` | yes | the value list for `KindEnum` |
| `GoType` | yes | the expected type of a binding |
| `Binds` | yes | literal vs `{{…}}` vs either |
| `Required` | yes | the `*` marker; seeding on add |
| `Origin` | yes | provenance grouping |
| `Doc` | yes, **empty everywhere** | the description pane |
| `Default` | **no** | modified-vs-default emphasis; Reset |
| `Category` | **no** | grouped view |
| `ReadOnly` | **no** | — see below |

Two of the three absences should be added and one should not.

### `Default`, and a drift test that actually covers it — BUILT

Default detection is the load-bearing one. A VS property grid is
readable at a glance because the handful of properties you changed are
bold and the rest are not; without it every row looks equally
significant, which is what the inspector looked like.

`Default` is a **declared string in markup spelling** on `AttrSpec` —
the value you would have written to get the behaviour you get by
omitting it. Declaring it inherits the vocabulary's drift asymmetry:
under-declaring is loud (something rejects it), over-declaring is
silent.

But `Default` admits a drift test the attribute vocabulary never could,
and it is stronger than the guard the vocabulary settled for:

> Build `<El/>` and `<El Attr="<default>"/>` into the **same bounds**,
> render both, and require the two cell buffers to be identical.

That is a mechanical, reflection-free check using the framework's own
output, and it covers **every attribute whose default has an observable
effect** — which is precisely the set where "differs from default" means
anything in a grid. The absurd-value guard the vocabulary uses reaches
59% because 51 of 124 rows accept any string; this one is not limited
that way, because it tests *effect* rather than *acceptance*.

Honest statement of what it does not cover: attributes with no static
visual effect — `KindCommand`, `KindIdentity`, durations, bindings whose
value is supplied at runtime. Those carry `Default: ""` meaning "no
default worth showing", and are excluded from the emphasis rule rather
than guessed at. `Default: ""` is therefore a third state, not a missing
value, exactly as `AttrsKnown: false` is not "no attributes".

#### The identity test alone is not enough, and the gap is not small

`TestDeclaredDefaultsRenderIdenticallyToOmission` passes **trivially**
for any attribute whose effect the probe cannot see. That is the
over-declaration direction again, and it is why there is a second test.

`TestDeclaredDefaultsAreDiscriminating` requires some other legal value
to render *differently* — proving the first test could have failed. An
attribute for which no such value can be generated must not declare a
`Default` at all, and the test says so in those words rather than
skipping.

This was not a theoretical precaution. On first run the identity test
was green for **all thirteen** declared defaults and the discrimination
test failed **thirteen of thirteen**, for three distinct reasons, none
of which the identity test could have surfaced:

- the probe emitted `<Text/>` with **no body**, and an empty Text paints
  nothing at any alignment, size or visibility — which silently disabled
  seven universal attributes;
- the probe gave stacks **one** child, so `Gap` had nothing to space,
  and two **equal-width** children, so `ButtonBar Uniform` had nothing to
  equalise;
- the universal and attached tables were probed through a `<Text>`,
  which paints at the left edge of whatever bounds it is given — so
  `Grid.ColSpan="2"` widened the slot and moved nothing. They are probed
  through a `<Border>` now, because a border draws its edges *at* its
  bounds and therefore shows every change to the rect it was arranged
  into.

Two of those three were bad probes rather than bad declarations, which
is the point: a test that cannot fail is indistinguishable from a
passing one, and only a deliberate attempt to make it fail tells them
apart. The same lesson as the transport barrier, reached from the other
side — there the guard was missing, here the *setup* was too weak to
exercise the guard.

A third guard rides along: a non-visual element that declares a
`Default` is an immediate error, because neither test can say anything
about something that paints nothing.

### `Category`: derived first, declared only to override — BUILT

124 hand-written category strings is 124 things that can rot. Most rows
categorise themselves from data the spec already carries, and
`CategoryOf` does exactly that:

- `KindCommand` → `Events`
- `KindStyle`, `KindColor` → `Appearance`
- `KindIdentity` → `Design` — `Name` is the address, not a value
- everything else → `Common`

`Category string` on `AttrSpec` is the **override**, empty meaning
"derive". The universal and attached tables use it, and they are the
reason the field has to exist at all: their membership in `Layout` comes
from *where they live* rather than from their Kind — `Margin` is a
`KindString` and `Grid.Row` a `KindInt`, so no derivation from Kind could
ever reach them.

The grid presents categories as **row order, not header rows**. A header
row would sit in the same list the selection indexes into, and every
activation would then have to guard against editing one — a distinction
in the view that the model does not have.

### `ReadOnly`: rejected

I listed it as a gap earlier. It is not one, and adding it would be a
mistake.

Every markup attribute is settable *by definition* — it is an attribute
in a document being authored. Read-only is a property of a **live
component's runtime state** (a computed you can observe but not assign),
which is the tree's question, not the catalog's. Putting `ReadOnly` on
`AttrSpec` would create a distinction with no member on one side and
invite a consumer to render a state that cannot occur.

`Name` looks like the counterexample and is not: it is not read-only, it
is `KindIdentity` — writable, but writing it *moves the element* and
invalidates every outstanding address. That is a different fact and it
already has a field.

### Per-`Kind` editors, and where each one's legal values come from

This is what "behave like the Visual Studio one" decomposes into. Every
row's value source already exists in the running app:

| Kind | Editor | Legal values from |
|---|---|---|
| `KindEnum` | cycling list | `AttrSpec.Enum` |
| `KindBool` | toggle | fixed |
| `KindInt` | typed field with ±  | — |
| `KindDuration` | typed field, unit-validated | `time.ParseDuration` |
| `KindColor` | `components.ColorPicker` | it already exists |
| `KindStyle` | list | `Context.Styles` (the `list_styles` question) |
| `KindCommand` | list | `Context.Values` entries that are `gooey.Action` |
| `KindGesture` | press-a-key capture | `input.ParseGesture` |
| `KindGridLens` | track editor | grammar |
| `KindBinding` | path picker | `Context.Values`, filtered by `GoType` |
| `KindIdentity` | rename, **with confirmation** | — |
| `KindText`, `KindString` | text box | — |

Two things fall out of that table that are worth naming:

- **Three of the four introspection questions converge in the grid.**
  The catalog supplies the vocabulary, `Context.Values` supplies the
  binding completions, `Context.Styles` supplies the style names. A
  property grid is not a new capability; it is those three answers laid
  out in rows.
- **Binding-expression editing needs no new model.** `Binds` already
  says which spellings are legal, so the value editor is two-mode
  (literal | binding) with the mode fixed for `BindsLiteral` and
  `BindsBinding` and switchable for `BindsEither`. Typing a literal into
  a binding-only attribute can be refused *in the grid*, before the load
  error — which is the load-time-not-click-time rule applied one step
  earlier.

## Prerequisite: `Context.ComponentDefs`

The grid cannot be honest about a host app's own components while
`Components` is `map[string]Builder` — a func is not a schema, so every
registered component is `AttrsKnown: false`. `ElementDef` already carries
`Build` alongside its vocabulary, so a def *is* a builder plus its
surface:

```go
// Components adds custom element builders. A Builder is opaque, so an
// element registered this way reports AttrsKnown: false.
Components map[string]Builder
// ComponentDefs registers a component WITH its vocabulary. Same
// builder, plus everything the catalog needs, so an element registered
// this way is as fully described as a built-in.
ComponentDefs map[string]*ElementDef
```

- **Additive, opt-in.** No existing registration changes. Rewriting every
  call site to gain a feature nobody has asked for would be the wrong
  trade.
- **`AttrsKnown` stays a law for `Components` and becomes a choice for
  `ComponentDefs`.** The three-state model is not weakened; it gains a
  second route to the honest state. A def may still declare `Open` or
  `DynamicAttrs` and land back on "exhaustive for this context only".
- **A name in both maps is a load error, not a precedence rule.**
  Precedence between two registries is how silent drops are born, and
  this document exists because of one.

Keep `<LogPane>` **undeclared**. It is the only live demonstration that
the unknown state renders differently from "takes no attributes", and
converting it would quietly delete the evidence for the honesty rule.
Add a **second** demo component that registers through `ComponentDefs`,
so the palette shows both states side by side — strictly more
informative than converting the one we have.

## What already shipped

Notes 2–4, done in the editor, no framework change:

- the selection cue is `Style="sel"`, not `warn` — an accent, not an
  alarm (`examples/wysiwyg/components/inspector/inspector.gooey`);
- enum rows list their own members and binding-only rows say
  `{{.GoType}}`, via `legalValues` (`examples/wysiwyg/main.go:430`);
- required attributes carry `*` on the name rather than a column that is
  blank nine rows in ten;
- a description pane shows `Doc` where the catalog has prose and the
  legal values where it does not, so it is never blank while `Doc` is
  unpopulated (`examples/wysiwyg/wysiwyg.gooey:69`).

Then `Default` and `Category`, which are model changes and are described
in full above:

- `AttrSpec.Default` and `AttrSpec.Category`, with `CategoryOf`
  (`markup/catalog.go`), thirteen declared defaults across the universal
  table, the attached tables and nine element attributes;
- `markup/defaults_test.go` — the identity test, the discrimination test
  that makes it able to fail, and the non-visual guard;
- the grid consumes both: a `•` marker on rows differing from their
  default, rows grouped by category, and the category and default shown
  in the description pane.

The `sel` style is deliberately **not** also the modified-from-default
indicator, now that both exist. One cue meaning two things is the
collapse this project keeps cataloguing, so "modified" is a glyph in its
own column and "loaded into the editor" keeps the colour.

`isModified` distinguishes three states rather than two, and the middle
one is the one a naive implementation gets wrong: **absent** (the default
applies), **written and equal to the default** (the markup does the same
thing, so emphasising it would tell the user they changed something they
did not), and **written and different**. An attribute with no declared
`Default` is never modified, because there is nothing to differ from.

## Order of work

1. ~~`Frozen` in the root package + the four touch points.~~ **Done**,
   and it landed as **three** touch points rather than four — see
   "What the implementation changed" below.
2. COD in `examples/wysiwyg`: one-child frozen host, focus stop (it is
   the only focusable thing left in the surface), identity map, edit
   vocabulary that records.
3. Click-to-select through `HitTest` → identity map → the existing
   selection state. This is the first thing Elan can feel, and it retires
   note 1's actual complaint (the palette double-click stays; adding by
   pointing is what was missing).
4. Selection border as an `Adornment`, anchored to the selected
   component.
5. Keyboard sizing and moving, with the gesture set derived from
   `AttrsFor(spec, parent)` — including the reorder case under a stack
   and the no-move case under a `ModeOne` container.
6. ~~`Default` on `AttrSpec`, plus the render-equivalence drift test,
   plus modified-vs-default emphasis in the grid.~~ **Done**, ahead of
   1–5: nothing in it touches the input seam, so it did not have to wait
   on the note-6 ruling.
7. ~~`Category` (derived, override-able) and the grouped view.~~ **Done**
   alongside 6.
8. ~~Per-`Kind` editors, cheapest first: enum, bool, style, command.~~
   **Done.** `enter` dispatches by Kind: a row with a finite value set
   advances to the next value and commits, everything else loads the text
   input, and `e` is the text escape hatch for every row (`KindStyle` and
   `KindCommand` are `BindsEither`, so the finite list is the common case
   and not the whole grammar).

   The value sets come from `valueSet` and every one of them is resolved
   against the **running app**: `AttrSpec.Enum` for enums, a fixed pair
   for bools, `Context.Styles` for styles, and the `Context.Values`
   entries that type-assert to `gooey.Action` for commands. A hardcoded
   style list would offer names the app does not have and omit the ones
   it does; asking the live table cannot.

   Unset is in the cycle for optional attributes and absent for required
   ones — without it an optional attribute could be set and never
   cleared, which would make a cycling editor a one-way door.

   `TestEveryCycledValueProducesMarkupThatBuilds` walks every value of
   every finite-valued row (58 today) and requires the document to build
   at each step. That is the pin that matters: the editor supplies these
   values, so one the loader rejects is the editor handing the user a
   load error out of its own list. Verified able to fail — dropping the
   `gooey.Action` assertion from `commandBindings` makes it report ten
   `Click="{{.Source}}"`-style failures by name.
9. `ComponentDefs`, and the second demo component.
10. The one mouse handle, last — it is the only piece the headless
    harness cannot cover, so everything it does must already be reachable
    from the keyboard before it is added.

## Open questions

- **Reorder-under-a-stack is a document edit with no attribute.** The
  grid has nowhere to show it and the catalog has nothing to describe it.
  Is child order a first-class thing the surface edits, or is it only
  ever a drag gesture? It is the one edit the catalog genuinely cannot
  express.
- **Remote selection.** Local preview gets COD; an attached target gets
  nothing. Either the protocol grows a way to say "outline this named
  element", or the editor patches an `AdornmentLayer` into the target,
  or remote mode is honestly a markup editor without a design surface.
  All three are defensible; none is decided.
- **Does the surface need its own `Composer`?** Rebuilding the document
  on every keystroke throws away paint nodes. It is fast enough today;
  it would stop being fast enough for a large document, and the fix is
  incremental patching of the surface rather than a rebuild — which is
  the same operation `patch_markup` performs remotely.
- **`Doc` is empty everywhere.** The description pane is wired and has
  nothing to show. Populating 124 rows is data entry, not design, but it
  is the difference between a grid that teaches and one that lists.
