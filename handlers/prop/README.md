# prop — a mutation pack

State a page can change, with no app code behind it:

```xml
<Gooey xmlns="wonderforge.io/gooey/2026"
       xmlns:prop="gooey.dev/handlers/prop">
  <Button Content="+1"    Click="{{prop:Add .Count `1`}}"/>
  <Button Content="rec"   Click="{{prop:Toggle .KeepRecording}}"/>
  <Button Content="brief" Click="{{prop:Set .Mode `brief`}}"/>
</Gooey>
```

```go
markup.RegisterHandlers(prophandlers.URI, prophandlers.New())
```

Handler registry only. `Provider` does not implement
`markup.ValueProvider`, so a mutation cannot be registered as a value
even by mistake — and ``{{prop:Set .Mode `a`}}`` written in a `Text` is
the load error `markup/values.go` already produces for an event-only
namespace.

**Root module, on purpose** — the graph is `fmt`, `strconv`, `strings`
and `time`.

## The gap it closes

A markup-only control (`Include`) could DISPLAY every kind of state and
change none of it. The only route to a `Set` was `Click="{{.Fn}}"`
bound to a viewmodel delegate, which is Go code, which an `Include`
cannot have. A mode selector or a checkbox row needed one hand-written
closure per button whose entire body was `p.Set(x)`.

## Functions

Exported as constants, enumerated by `prophandlers.AllNames()`. The
error text for an unknown function derives from the same list, so the
inventory and the dispatch cannot drift.

| function | shape | target types |
|---|---|---|
| `prop:Set` | ``{{prop:Set .Mode `brief`}}`` | `string`, `bool`, `int`, `int64`, `float64`, `time.Duration` |
| `prop:Toggle` | `{{prop:Toggle .Rec}}` | `bool` |
| `prop:Add` | ``{{prop:Add .Count `1`}}`` | `int`, `int64`, `float64`, `time.Duration` |

The operand is a backtick literal parsed **at load** in the target's own
type — `` `1` `` for an int, `` `250ms` `` for a duration, `` `abc` ``
into an int is a load error — or a handle of the **same** type, read at
click time so `{{prop:Add .Count .Step}}` follows `.Step`. A handle of a
different type is refused rather than coerced. There is no `Sub`:
`` `-2` `` is a negative literal.

## Everything resolvable fails at load

The target's type, whether the target is settable, the operand's type,
whether a literal parses, arity, and the function name are all decided
when the page loads. In particular:

```
Toggle needs *prop.Property[bool]; .Mode is *prop.Property[string]
Set cannot write .Doubled: it is a COMPUTED property (*prop.Property[int]),
  which derives its value and has no setter — writing it would panic.
```

That second one is why `prop.Property.Settable()` exists. `prop.Set`
panics on a computed, so without a load check a mutation written against
a derived property builds clean and takes the app down on its first
click.

## What registering this grants

Write access to **any settable property reachable by a path in the
page's own binding context**. There is no per-property allowlist,
because unlike `env`'s variable names the operand is a path into a
context the host assembled itself: the context *is* the allowlist.

- **Read without write** is the default — a host that never registers
  this URI has a page that can bind and display everything and change
  nothing.
- **Write without read** does not exist. You cannot bind a path you were
  not given, so anything writable was already readable.
- **Withholding write on one property** has a mechanism: publish it as a
  **computed**. `prop:Set` over a computed is a load error, so a derived
  handle is a genuinely read-only projection.

## The no-op guard

`prop.Set` does not compare (`prop/prop.go`): setting a property to the
value it already holds still invalidates every dependent and still costs
a repaint. Every command here reads, computes, compares, and returns
early when nothing changed — because the idiom this pack exists for is a
row of `Set` buttons over one property, where clicking the already
selected item is the most common redundant event a UI receives.

`TestRedundantSetPaintsNothing` pins that at **0** components repainted,
`TestRealSetPaintsOnlyItsReaders` at exactly the readers, and
`TestAddOfZeroPaintsNothing` covers the same guard on `Add`. Deleting
the comparison turns those into 2, 2 and 1.

## UI-goroutine confinement

Nothing here starts a goroutine and nothing here touches the Dispatcher.
A command runs inline on the event-dispatch path, so `Get` and `Set` are
both legal where they are written and the repaint lands in the same
frame as the keystroke. `TestSetWritesAStringPropertyInline` is the
evidence: the new value is on screen after **one** `Frame` with no
`Drain`, which a Dispatcher-marshalled write could not manage.

The `Get` inside a command is a plain read, not a subscription — it runs
with no computed on `prop`'s `evalStack`, and the call site is what
decides.
