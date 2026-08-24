# Layout grants: a parent declares the geometry it confers

An element now declares what it does to its **children** — which layout
model positions them, and the attributes that carry it. `markup.Grant`,
on `ElementDef`, surfaced through `ElementSpec.Grants`.

This is vocabulary, not editor convenience. A future `gooey gen` compiles
markup ahead of time (the [`gooey gen` epic
(#59)](https://github.com/WonderForgeLabs/gooey/issues/59)), and "what
does it mean to move this element" is a fact about the element table, not
about any one consumer of it. The designer is simply the first consumer
that needed it.

## The defect this replaces

`apps/wysiwyg/drag.go` decided what a drag meant by switching on element
names:

```go
switch p.Elem {
case "Canvas": return DragFree
case "Grid":   return DragCell
}
return DragOrder
```

The rule was right and the key was wrong. An editor that knows the two
container names it was written against cannot be extended by an app that
registers a container of its own — and gooey's entire markup story is
that a host adds elements. A third-party `<Table>` got `DragOrder`: no
drag, no geometry, no way to design in it, and no diagnostic saying why.

It was also the **second** copy of the taxonomy. `Release` independently
spelled the attribute names:

```go
d.node.Attrs["Grid.Row"] = strconv.Itoa(l.Row)
```

So one rule lived in two places in one file, each able to drift from the
catalog independently, and the failure mode of drift is silence:
`applyLayout`'s missing default arm discards an unrecognised attached
attribute without a word.

## GrantKind

Four values, and the distinctions are the whole content of the taxonomy.

**`GrantNone`** — the zero value. This element confers no geometry: it
positions its children itself, or has none. A `<Border>` holds exactly
one child and places it.

It is **not** the same statement as "this element has no children", and
conflating the two is what the old default arm did. It told the user a
border's only child could be *reordered* — among siblings it does not
have. That is a wrong answer delivered confidently, which is worse than
a refusal. The editor now distinguishes them: `DragFixed`, "placed by
its parent".

**`GrantOffset`** — free geometry. Each child carries an X and a Y and
goes exactly where it is put. `<Canvas>`. This is the model
direct manipulation is trivial in, and it was the only one gooey's
designer could edit.

**`GrantCell`** — addressed geometry. Each child carries a cell, and
possibly a span, and the **container** computes the rectangle. `<Grid>`.
A drag snaps, and an editor cannot know where the cells are without
asking the layout.

**`GrantOrder`** — implicit geometry. `<VStack>`, `<HStack>`,
`<ButtonBar>`.

### Why GrantOrder's empty `Attached` is the definition, not an omission

A stack's child has **no positional attribute at all**, because its
position *is* its index among its siblings. The absence is the defining
feature.

This matters because the two readings lead to opposite code. Read as an
omission, an empty `Attached` is a gap somebody should fill, and the
natural "fix" is to invent a `VStack.Index` attribute — which would be a
second, writable copy of a fact the document's own child order already
holds, and the two would disagree the first time anyone reordered the
markup by hand. Read as the definition, it tells a consumer something
precise and actionable: **an editor moving an element in this model edits
the document order, not a value.** There is nothing to write because
there is nothing to store.

`TestGrantKindMatchesTheRolesItCarries` enforces it — a `GrantOrder` that
grows an attached attribute fails the suite.

## Role: so that no consumer spells an attribute name

`AttrSpec.Role` says what an attribute *means*, independent of what it is
called.

An editor manipulates geometry by meaning — "put this element in the next
column" — while markup carries only names, and the two are not the same
vocabulary. Without a role, an editor has to know that a cell's column is
spelled `Grid.Col`, which is exactly the element-name knowledge this
contract removes. With one, it asks the parent's grant for `RoleCol` and
writes whatever name comes back, so a container spelling it
`Table.Column` needs no editor change.

Two families, distinguished by whose attribute carries them:

- the **child's** attached attributes — `RoleX`, `RoleY`, `RoleRow`,
  `RoleCol`, `RoleRowSpan`, `RoleColSpan` — which say where one child
  sits, and live on `Grant.Attached`;
- the **container's own** attributes — `RoleRowTracks`, `RoleColTracks`
  — which say what the cells *are*, and live on the element's `Attrs`.

The second family is what lets a consumer draw and edit a grid's
structure. `Grant.Attr(role)` and `markup.AttrByRole(spec, role)` are the
two lookups.

### Declared, not derived

`ElementDef` derives its behavioural axes from `Proto` by type assertion,
and asks a fair question of any new field: why is this one declared?

Because a marker interface on the component could carry the **kind** but
not the **names**. `Build` hands its children to a `components.Grid`, and
nothing about that type says the markup spelling of a cell is `Grid.Row`.
The spelling is a fact about this element's *vocabulary*, which is what
`elements.go` is for — and half a contract in a second place is precisely
the drift the colocation doctrine exists to prevent.

### The declaration has to be checked against behaviour

Every structural assertion about a grant — that a `GrantCell` carries a
`RoleRow`, that names are dotted, that roles are unique within a grant —
**passes unchanged when `RoleRow` and `RoleCol` are swapped on both
attributes.** The declaration stays well-formed. An editor built on it
moves elements along the wrong axis, and the suite stays green.

So `TestEachRoleReachesTheLayoutFieldItNames` goes through the loader:
set the attribute a role names, build, and require the value to arrive in
the `Layout` field that role *means*, using distinct values (3 and 5,
never 1 and 1, so a swapped pair cannot satisfy it by coincidence). Same
discipline as `TestDeclaredDefaultsRenderIdenticallyToOmission` — a
declaration is only allowed where it can be checked.

Verified both directions: swapping one role is caught by the shape tests;
swapping both is caught **only** by the behavioural one.

## The `attachedAttrs` deletion

`markup/catalog.go` held a map keyed by parent element name:

```go
var attachedAttrs = map[string][]AttrSpec{
	"Canvas": {...},
	"Grid":   {...},
}
```

Those entries now live on the granting elements' own literals, and the
map is gone. `AttachedAttrs`, `AttachedParents`, `AttrsFor` and
`attrcheck` read the registry.

**The evidence that this was the right call is that it had already
drifted.** The table was the only mention of `Grid.RowSpan` anywhere in
the tree — three hundred lines from the `<Grid>` literal and connected to
it by nothing. An element with attached properties had two unrelated
places to remember, and nothing would have reported forgetting the second
one. This is the failure `ElementDef`'s own doctrine predicts for a side
table, observed rather than hypothesised.

`attrcheck` now also consults `Context.Elements`, so a host-registered
container's grant is in scope at load. That direction is safe: a
registered grant can only **add** names to the allowed set, so it accepts
markup that was previously rejected and can never reject markup that
previously loaded. Leaving it out would let a host declare `Table.Column`
in its catalog, watch a palette offer it, and then have the loader refuse
the result — the catalog lying about the target, through the artifact
built to stop that.

### Rot prevention

`TestEveryMultiChildElementDeclaresAGrant`: every `ModeMany` element must
declare a non-`None` grant. A new container that declares none tells every
consumer its children have no editable geometry — a silent lie that
switches the designer off for it. The converse,
`TestOnlyMultiChildElementsGrantGeometry`, stops the field being
sprinkled where it means nothing.

## Consequence: the designer

`apps/wysiwyg` derives everything from the catalog. `ed.grantOf(elem)`
reads `ed.palette` (the precedent is `ed.bodySpec`); `dragKindFor()` maps
kind to gesture and is the editor's *entire* knowledge of layout models;
`Release` asks by role.

`TestAThirdPartyContainerIsDesignableWithNoEditorChange` registers a
`<Table>` that grants cells and spells them `Table.R`/`Table.C`, and
drags in it. `TestTheEditorNamesNoContainerElement` scans `drag.go`'s
**string literals** through `go/parser` rather than grepping the file —
the comments there legitimately discuss the old spellings, and a grep
would either fail on the prose explaining the fix or force the prose to
avoid naming what it fixed.

## The design-time grid overlay, and the gutter refusal

A `<Grid>` renders as nothing at all — its cells are arithmetic, not
marks — so laying out inside one meant editing `Rows="1,1"` against a
preview that showed neither rows nor columns. `Overlay`
(`apps/wysiwyg/components/preview/overlay.go`) draws the probed cells and
each track's spec.

The requirement as originally framed put the track specs **in the
gutters**, outside the grid: column specs along the top, row specs down
the side. That was refused, and the reasoning is the durable part.

A grid has no reserved margin, and this component cannot create one:
claiming space would push the previewed tree around and change the very
layout it exists to describe. So "outside the grid" means "on whatever
happens to be there" — and for a grid at the top-left of the preview,
that is the **editor's own pane border**. The document's structure would
be drawn over the editor's furniture. The only space an overlay may write
in is the space being edited, so the specs go on the grid's own top and
left edges. A track too short to show its spec is skipped rather than
overlapped.

That pairs with the second rule: **a guide may never destroy what it is
describing.** Grid tracks abut — there is no gap between cells to draw a
boundary in — so a cell's mark necessarily lands where a child's content
starts. The first version overwrote the first character of every element
in the top-left of its cell. Marks are therefore written only into blank
cells, which the overlay can ask about because it paints *after* the
document subtree.

### Consequences worth knowing before touching this

Three interactions here are silent when got wrong, and each is pinned by
a test that fails for a reason unrelated to what you were editing.

**An overlay is a later sibling, not a container's own `Render`.** The
composer paints depth-first **pre**-order, so a container paints *before*
its children. Anything `Pane.Render` drew would go under the document —
and every leaf in that subtree pre-clears its bounds, so the tree would
**erase** the guides rather than merely cover them.

**It must be a chrome-only container.** The pre-clear branch turns on
exactly `if _, isContainer := w.(Container); !isContainer`. Implementing
`ChildComponents()` and returning nil means it pre-clears nothing. As a
leaf it fills its rect first and blanks the tree beneath. It must also
never declare a `Background`, which would fill its bounds and mark it
`covered`.

**Its revision must be change-gated, or the frame never terminates.**
`Arrange` runs on every frame; a `Set` there dirties the paint node,
which schedules another frame, which arranges again. Bumping
unconditionally is a non-terminating loop — three unrelated tests report
"the composition never settled". Comparing the computed guide first gives
exactly one repaint when the picture changes and zero when it does not,
which is why this feature shipped **without changing any existing
damage-count assertion**. The weaker version of the same mistake —
subscribing to the editor's "document or selection changed" revision —
terminates, but repaints the overlay on every click anywhere in the app,
drawing nothing.

The model is built in `Arrange`, never `Render`: producing it means
probing the grid, and layout runs outside any evaluation context, so
doing it in a paint node would both mutate the tree during a paint and
record layout's `Get`s as dependencies of that node.

### Two tests that looked right and asserted nothing

Both were found by mutation testing, and neither would have been found
any other way.

**The overlay's deaf `Render` survives every model-level assertion.** The
obvious test — edit a track, check the screen followed — passes with the
subscription deleted, because a track edit rebuilds the document and the
composer's z-order pass force-repaints everything sitting above a rect
that just painted. The overlay is above the whole grid, so it repaints
for free on any edit beneath it, subscribed or not. The only
discriminating gesture is one that changes **nothing below**: moving the
track cursor repaints no document component at all.

**A hit-test assertion passes vacuously against a zero-size overlay.**
`HitTestTransparent` (the `AdornmentLayer` precedent) is required or the
overlay eats every click — but a test that clears the selection first
gives the overlay zero bounds, so it passes for an overlay that eats
everything. The test must prove the overlay's arranged bounds contain the
press point *before* pressing.

There is a third, of the same family: `restoreMarks` may lift a mark only
while the cell still holds the glyph the overlay wrote. Anything else
means the document repainted that cell this frame and now owns it, and
writing the saved copy back restores a blank over content that just
arrived — a clean cell that will never repaint. Removing that condition
passes the entire package;
`TestRestoringAMarkNeverClobbersNewContent` is what closes it.

## Deliberately not done

- **Reorder for `GrantOrder`.** The taxonomy now names it and the editor
  reports it, but dragging in a stack still does not reorder the
  document. That is a document mutation, not a geometry write.
- **Renumbering children when a track is removed.** `Grid` clamps an
  out-of-range cell, so they stay visible. Silently rewriting cells the
  user did not touch is a bigger surprise than a child that needs
  re-placing.
- **A `Grant` on `Include`/`UserControl` surfaces.** A markup-only
  control that wraps a container does not forward its grant, so its
  children are not designable through it.
