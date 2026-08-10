# Tutorial 5: Build reusable controls

In this tutorial you package markup for reuse two ways: an **Include**,
which needs no Go code at all, and a **UserControl**, which pairs a markup
file with a typed setup function. You then instantiate the same control
twice on one page and watch the two instances stay independent.

**Time:** about 30 minutes.
**Prerequisites:** [Tutorial 4](04-input-commands.md).

When you finish, you will have this:

![Two independent sensor panels above a total card](media/05-usercontrols.png)

The finished code is in
[`docs/learn/examples/05-usercontrols`](examples/05-usercontrols) — four
files: `main.go`, `page.gooey`, `statpanel.gooey`, `card.gooey`.

## Step 1: Write a markup-only control

Create `card.gooey`. It is an ordinary markup file — same `<Gooey>` root,
same one-child rule — whose bindings will resolve against whatever the
*instance* hands it:

```xml
<Gooey xmlns="wonderforge.io/gooey/2026">
  <Border Title="{{.Title}}" Style="panel">
    <VStack Gap="0">
      <Text Style="accent">{{.Body}}</Text>
      <Text Style="dim">{{.Sub}}</Text>
    </VStack>
  </Border>
</Gooey>
```

Use it from `page.gooey`:

```xml
<Card Grid.Row="1" Title="total" Body="{{.Total}}" Sub="a Card include — markup only, no code-behind"/>
```

and point the page's context at a filesystem:

```go
ctx := &markup.Context{
	// ...
	Includes: fsys, // <Card/> resolves to card.gooey by convention
}
```

That is the whole registration. With `Includes` set, an unknown element
`<Card/>` resolves to `card.gooey` — the lowercased element name plus
`.gooey` — in that filesystem. If you prefer to be explicit, register it
instead:

```go
Components: map[string]markup.Builder{
	"Card": markup.Include(fsys, "card.gooey"),
}
```

**The instance's attributes become the control's context.** Each
non-layout attribute is resolved in the parent context and exposed under
its own attribute name:

- `Body="{{.Total}}"` resolves in the page and hands over the **live
  property handle**. Setting `Total` repaints the card. Nothing is
  copied.
- `Sub="a Card include…"` is a literal and arrives as a plain string.
- `Grid.Row="1"` stays on the instance. Layout attributes (`Width`,
  `Height`, `Margin`, `HAlign`, `VAlign`, `Visibility`, `Grid.*`) and
  `Name` are never passed through.

An attribute binding that does not resolve is a **load-time error**, not
a silently blank control.

> **If you know XAML:** an Include is close to a `UserControl` whose
> dependency properties you never had to declare — but that is also its
> weakness. As written, the property surface is implicit and unchecked:
> nothing states that a Card needs `Title`, `Body`, and `Sub`, so a
> misspelled attribute silently does nothing. Declare the surface with
> `<x:Property>` to have it checked; see
> [the reference](../markup-reference.md#declared-properties-xproperty).

## Step 2: Write a control with code-behind

Some controls need more than a pass-through: control-local computeds,
commands closed over the handed-in state, typed non-string data. That is
a `UserControl` — the same markup file plus a setup function.

`statpanel.gooey`:

```xml
<Gooey xmlns="wonderforge.io/gooey/2026">
  <Border Title="{{.Title}}" Style="panel">
    <VStack Gap="1">
      <Text Style="accent">{{.Reading}}</Text>
      <HStack Gap="2">
        <Button Content="+1" Click="{{.Up}}"/>
        <Button Content="-1" Click="{{.Down}}"/>
      </HStack>
      <Text Style="dim">{{.Note}}</Text>
    </VStack>
  </Border>
</Gooey>
```

The setup function runs **once per instance** and returns that instance's
own `Context`:

```go
func statPanel(e markup.Element, parent *markup.Context) (*markup.Context, error) {
	value, err := attr[*prop.Property[int]](parent, e, "Value")
	if err != nil {
		return nil, err
	}
	// A control-local computed over the handed-in handle. It is live:
	// the page and the panel share one property, not a copied value.
	reading := prop.NewComputed(func() string {
		return fmt.Sprintf("reading = %d", value.Get())
	})
	return &markup.Context{
		Values: map[string]any{
			"Title":   e.Attrs["Title"], // literal hand-off
			"Note":    e.Attrs["Note"],
			"Reading": reading,
			"Up":      gooey.Command(func() { value.Set(value.Get() + 1) }),
			"Down":    gooey.Command(func() { value.Set(value.Get() - 1) }),
		},
	}, nil
}
```

Register it as a component builder:

```go
Components: map[string]markup.Builder{
	"StatPanel": markup.UserControl(fsys, "statpanel.gooey", statPanel),
}
```

and instantiate it twice:

```xml
<Grid Grid.Row="0" Cols="1*,1*">
  <StatPanel Grid.Col="0" Title="sensor A" Note="its own isolated context" Value="{{.A}}"/>
  <StatPanel Grid.Col="1" Title="sensor B" Note="same file, second instance" Value="{{.B}}"/>
</Grid>
```

## Step 3: Cross the boundary with typed data

Text bindings only carry strings. Anything else — an `int` property, a
slice, a command — crosses through `BindingValue`, which returns the raw
context value for you to type-assert:

```go
// attr resolves one attribute of a control instance in the PARENT
// context and type-asserts it — the receiving half of the hand-off.
func attr[T any](parent *markup.Context, e markup.Element, name string) (T, error) {
	var zero T
	v, err := parent.BindingValue(e.Attrs[name])
	if err != nil {
		return zero, fmt.Errorf("%s: %w", name, err)
	}
	t, ok := v.(T)
	if !ok {
		return zero, fmt.Errorf("%s: got %T, want %T", name, v, zero)
	}
	return t, nil
}
```

This little generic helper is the idiom; `cmd/reader/controls.go` in this
repository carries the same one. For event attributes there is a matching
call, `parent.Command(e.Attrs["Open"])`, which resolves `{{.Open}}` or a
bare handler name exactly the way `Click` does — so a control can accept
a command from its parent and attach it to its own `<KeyBinding>`.

> **No reflection anywhere.** The type assertion is the point. gooey has
> no reflective property walker, so a control states the type it needs
> and fails loudly at load time if it does not get it.

## Step 4: Confirm the isolation

Run it, press `+1` on the left panel twice, tab into the right panel and
press `+1` once:

![Sensor A reads 2, sensor B reads 1, and the total card reads 3](media/05-usercontrols.png)

Each panel drove its own count through its own context, and the page's
`Total` computed spans both:

```go
total := prop.NewComputed(func() string {
	return fmt.Sprintf("A + B = %d", a.Get()+b.Get())
})
```

The property graph does not care which control's markup a binding came
from.

**What isolation means precisely.** Bindings inside a control's markup
resolve against the control's own `Context` — never against the page. The
only way data crosses is the instance's attributes. So a control cannot
accidentally reach a page value that happens to share a name, and two
instances of the same file cannot see each other.

**What is inherited.** When a setup function leaves them nil, `Styles`,
`Components`, `Handlers`, and `Includes` fall back to the parent context —
which is why `Style="panel"` works inside `statpanel.gooey` without being
re-registered. `Named` is per instance, so `Name="..."` inside a control
is invisible to the page (like `x:Name` inside a template).

**Element resolution order,** in full: a registered `Components` builder
wins, then a built-in element, then the `Includes` convention, then an
`unknown element` error.

## Step 5: Hot-reload the whole composition

One page rebuild re-instantiates every control, so name the other two
files and an edit to any of them reloads the whole composition:

```go
app = gooey.NewApp(markup.Page(fsys, "page.gooey", ctx,
	"statpanel.gooey", "card.gooey"))
```

They have to be named rather than discovered: an `<Include>` is resolved
during a build, and the build being watched for has not happened yet.

Edit `statpanel.gooey` while the app runs and **both** panels update,
with their counts intact — the counts live in the page's properties, and
only the tree was rebuilt.

## Which one should you use?

| | Include | UserControl |
|---|---|---|
| Go code | none | a setup function |
| Attributes | become the context verbatim | you resolve and type-assert them |
| Non-string data | only as an opaque handle bound straight through | yes, typed |
| Control-local computeds and commands | no | yes |
| Registration | `Includes: fsys`, or `markup.Include(...)` | `markup.UserControl(...)` |

Reach for an Include when the control is a layout with holes to fill.
Reach for a UserControl the moment it needs behavior of its own.

## What you learned

- An Include turns instance attributes into the control's context, with
  zero Go code, and resolves by convention from `Context.Includes`.
- A UserControl adds a per-instance setup function returning the
  instance's own context.
- Context isolation is the contract: data crosses only through
  attributes, as live handles.
- `BindingValue` plus a type assertion carries non-string data; a small
  generic `attr[T]` helper is the idiom.
- `Styles`/`Components`/`Handlers`/`Includes` inherit when left nil; `Named`
  is per instance.
- Naming a page's control files in `markup.Page` hot-reloads the whole
  composition through one page rebuild.

## Current limitations

- A control's property surface is **implicit and unchecked until it is
  declared** — add `<x:Property Name="Title" Type="string"/>` to the
  control's root for names, types, defaults, `Required`, and load errors
  on undeclared attributes
  ([reference](../markup-reference.md#declared-properties-xproperty)).
  A declared default is a fresh per-instance source, so it resets on hot
  reload; durable state belongs in the app's viewmodel.
- No styles with setters, so a control cannot be restyled from outside
  beyond passing a style name in.

## Next steps

- **[Tutorial 6: Write a custom component](06-custom-components.md)** — the
  rows-component layer under controls like these.
- Concept: [markup tiers](concepts/markup-tiers.md)
- Depth: [architecture.md — UserControl](../architecture.md#usercontrol-context-isolation-and-the-attribute-hand-off).
