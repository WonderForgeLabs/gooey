# Tutorial 10: Scope resources and theme with styles

In this tutorial you declare a palette in markup instead of Go, read it
through a named `<Style>`, and then override it for one subtree only —
proving the rule that makes theming safe: **an inner `<X.Resources>`
scope doesn't overwrite the outer definition, it shadows it with a
different property**, so a runtime change to the outer one reaches
exactly the readers still holding that handle and nobody else.

**Time:** about 20 minutes.
**Prerequisites:** [Tutorial 3](03-binding-and-state.md) (sources,
computeds, and the read-versus-subscribe rule this tutorial reuses at
the resource level).

The finished code is in
[`docs/learn/examples/10-resources-and-styles`](examples/10-resources-and-styles).

## Step 1: Declare a page-level resource

A `<Gooey.Resources>` block is a property element on the root, the same
dotted slot `<ItemsView.ItemTemplate>` already uses. Add one to
`app.gooey`:

```xml
<Gooey xmlns="wonderforge.io/gooey/2026">
  <Gooey.Resources>
    <Resource Key="accent" Type="color" Value="#ffaa3c"/>
  </Gooey.Resources>
  ...
</Gooey>
```

`Key`, `Type`, and `Value` are all required. `Type` selects a row of the
same type table `<x:Property>` uses — `string`, `int`, `bool`, `float`,
`duration`, `color` — and `Value` is coerced by that row's parser **at
load**, so a bad literal fails the file that declares it, not whichever
element happens to reference the key later.

Each `<Resource>` materializes a `prop.NewSource[T]` — a plain, settable
property, exactly like one you'd build in Go with `prop.NewSource`. That
is the whole mechanism: a resource is not a special kind of value, it is
an ordinary property that markup can declare and that Go can reach by
name.

## Step 2: Read it through a style

Markup components don't take resource references on ordinary attributes
— `Background="{accent}"` is not (yet) a thing gooey parses. What a
resource feeds is a `<Style>`:

```xml
<Gooey.Resources>
  <Resource Key="accent" Type="color" Value="#ffaa3c"/>
  <Style Key="accented">
    <Setter Property="Fg" Resource="accent"/>
    <Setter Property="Bold" Value="true"/>
  </Style>
</Gooey.Resources>
```

A `<Setter>` takes `Property` plus exactly one of `Value` (a literal,
coerced at load) or `Resource` (a key resolved up the scope chain,
type-checked against the field at **build** time — a `color` resource
feeding a `Fg` setter, `bool` feeding `Bold`, and so on). Getting the
kind wrong is a load error naming both.

Apply the style with the ordinary `Style="name"` attribute:

```xml
<Text Style="accented">this line reads the page's accent</Text>
```

Under the hood, `<Style Key="accented">` compiles once into a reactive
recipe. Applying it to an element materializes one
`prop.NewComputed[render.Style]` per instance, and *that* is what the
`Text` reads as its ordinary `Style` property — the same slot every
component already has. The `Setter`'s `Resource="accent"` closure calls
`Get()` on the resource handle **inside that computed**, so the read
both produces the color and subscribes the computed to it. Nothing new
runs at paint time: a resource change dirties the style computed, which
dirties the paint node that reads it, and nothing else on the page moves.

## Step 3: Shadow it for one subtree

`<X.Resources>` is legal on any element, not just the root. Wrap a
second pane in its own `<Border.Resources>` and redeclare both the
resource **and** the style that reads it:

```xml
<Border Title="overridden — its own accent" Style="panel">
  <Border.Resources>
    <Resource Key="accent" Type="color" Value="#4fd1c5"/>
    <Style Key="accented">
      <Setter Property="Fg" Resource="accent"/>
      <Setter Property="Bold" Value="true"/>
    </Style>
  </Border.Resources>
  <VStack Gap="1">
    <Text Style="accented">this line reads THIS pane's accent</Text>
    <Text Style="accented">a different property — see below</Text>
  </VStack>
</Border>
```

Resolution is **lexical**, decided while the tree builds: entering an
element with a `Resources` slot pushes a scope, and everything built
underneath — children build inside the same call — sees that scope
first. Leaving the element pops it, so a sibling that never entered the
`Border` can never see what it declared.

That is why redeclaring `accented` matters here, not just `accent`. A
`<Style>`'s `Resource=` setters bind to whatever was in scope **where the
style itself was declared** — lexical capture, like a closure. If this
`Border` had shadowed only `accent` and reused the page's `accented`
style unchanged, its `Text`s would still be reading the *page's* accent,
because that style was written against the page's scope. Redeclaring
both together is what makes the override total: a fresh resource, and a
fresh style bound to it.

Run the example now (`cd docs/learn/examples/10-resources-and-styles &&
go run .`) and you have two panes that look different for a reason
that has nothing to do with two different styles — they share one style
*name*, resolved against two different scopes:

![Two panes reading a style named "accented": the ambient pane in amber, the overridden pane in its own teal](media/10-resources-and-styles.gif)

## Step 4: Prove the shadow is a different property

The demo wires a button (and the `a` key) to `cycle`, which reaches the
**page's** resource from Go — not the overridden pane's, which has no Go
handle at all:

```go
h, ok := ctx.Resource("accent").(*prop.Property[render.Color])
if ok {
    idx = (idx + 1) % len(palette)
    h.Set(palette[idx])
}
```

`Context.Resource(key)` serves the **document scope** of the page's last
build — the root `<Gooey.Resources>` only. A subtree scope like the
overridden pane's is reachable only from inside its own subtree, by
construction: Go code has no path to it, the same way it has no path to
a `Values` binding a control didn't declare on its surface. That
asymmetry is deliberate — resources are ambient *within markup* (every
descendant inherits them unless it shadows), but a scope is not a global
dictionary Go can dig through from outside.

Press `a`, then `m` (wired to a `measure` command, the same
press-then-sample pattern Tutorial 3 uses for damage counts):

- The **ambient** pane's two `Text`s recolor immediately — they read the
  handle that just changed.
- The **overridden** pane never moves. Its `Text`s read a *different*
  `*prop.Property[render.Color]`, created fresh when its own
  `<Border.Resources>` was instantiated, and `Set` reaches only a
  property's own dependents.
- The report line reads **"last frame painted 2 component(s)"** — the
  ambient pane's two `Text`s, and nothing else on a page with two
  buttons, two borders, and six other lines of static chrome. That is
  the acceptance claim the design record makes for this feature: a
  resource `Set` costs exactly its readers, never a subtree walk.

## The resolution order

Put together, a `Style="name"` (or a `Setter`'s `Resource="name"`)
resolves in one fixed order, checked once at build:

1. **The nearest enclosing markup scope** — walk `<X.Resources>` outward
   from the element, through every ancestor's declared scope, to the
   document's own `<Gooey.Resources>`.
2. **`Context.Styles`** — the host's Go `map[string]render.Style`, which
   is the **outermost** scope, below every markup-declared one.
3. **A load error.** A name found in neither is not a silent
   fall-through to "unstyled" — it fails the file that referenced it,
   naming the key.

The consequence worth stating plainly: **a page-declared style always
beats a host-granted one of the same name.** This isn't a special case —
it's the same "nearest wins" rule as everywhere else in the chain, with
`ctx.Styles` simply standing in as the furthest-out scope there is. Two
things fall out of it:

- **Migration is incremental.** A page can move one style at a time out
  of Go and into its own `<Gooey.Resources>` and watch it take effect
  immediately, without deleting the Go entry first — there is no moment
  where both definitions need to agree.
- **The visible declaration wins, not the invisible one.** The other
  direction — a host grant silently overriding a style declared three
  lines above the element using it — would make the markup you can see
  a lie about what actually renders.

`Resource=` lookups follow the identical scope-chain rule (step 1 only —
there's no Go-side resource dictionary to fall back to), which is why
`resolveSetterResource`'s error names the scope, not just the key: "no
resource named %q is in scope."

## What you learned

- `<Gooey.Resources>` (document scope) and `<X.Resources>` (subtree
  scope, legal on any element) declare `<Resource>`s and `<Style>`s that
  resolve **lexically**, at build time, walking outward through
  ancestors to the document root.
- A resource is an ordinary `prop.NewSource[T]` under the hood — nothing
  new to learn about how it behaves once you have a handle to it.
- A `<Style>` materializes one `prop.NewComputed[render.Style]` per
  instance; a `Setter`'s `Resource=` reference `Get`s inside that
  computed, so the ordinary subscribe-by-reading rule applies at the
  style layer exactly as it does in your own code.
- Shadowing a resource in a subtree produces a genuinely different
  property. A style that should read the shadowed value has to be
  redeclared alongside it — a style's resource references bind to the
  scope it was **written** in, not the scope it happens to be used from.
- The cascade is nearest-markup-scope, then `Context.Styles`, then a
  load error — never a silent unstyled fallback.
- `Context.Resource(key)` is the runtime half: it reaches the document
  scope only, and one `Set` repaints exactly that handle's readers —
  provably, via the damage count.

## Current limitations

- **No implicit type matching yet.** `<Style TargetType="Border">`
  applying to every unstyled `Border` in scope, and the `:focus`,
  `:hover`, `:disabled` state sections, are designed but not
  implemented — a `<Style.Focus>` block is a load error today, not a
  silent no-op.
- **No attribute-side resource references.** `Background="{accent}"` on
  an ordinary attribute isn't parsed; a resource reaches a component
  only through a `<Style>` setter.
- **No dictionary identity swap.** There is no WPF `DynamicResource`
  moment where a whole scope is replaced. Editing is `Watch` (which
  rebuilds the tree and re-declares everything); runtime theming is
  `Set` on a resource, as this tutorial does.
- **Setters only reach `render.Style` fields** — `Fg`, `Bg`, `Bold`,
  `Dim`, `Underline`, `Reverse`. Styling other component properties is a
  future `TargetType`-scoped table, not something a `<Setter>` can name
  yet.

## Next steps

- Design record: [scoped resources and styles](../specs/2026-08-10-styles-and-resources.md)
- [`docs/learn/examples/10-resources-and-styles`](examples/10-resources-and-styles) — the finished code
- Concept: [the property graph](concepts/property-graph.md) ·
  [damage tracking](concepts/damage.md)
