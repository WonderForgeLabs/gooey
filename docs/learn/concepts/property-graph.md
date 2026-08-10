# Concept: the property graph

Every visual value in gooey — a label's text, a border's title, whether a
button is focused — is a `*prop.Property[T]`. Properties form a graph with
two kinds of node:

- **Source** (`prop.NewSource(v)`) holds a value. `Set` marks its
  dependents dirty and *computes nothing*.
- **Computed** (`prop.NewComputed(f)`) holds a function. `Get` re-runs `f`
  only when dirty, and the properties read *during* that run are recorded
  as its dependencies.

The consequence that surprises XAML developers: **whether a `Get` is a
read or a subscription is decided by the call site, not the property.**
The same `count.Get()` subscribes when it runs inside a computed's
evaluation and subscribes to nothing when it runs inside a command. One
function can mean both things depending on who called it — see
[tutorial 3](../03-binding-and-state.md), which puts the evaluation
counters on screen.

Because re-evaluation re-records dependencies, a computed that reads
different branches on different runs rewires itself. An `if` that stops
reading a property genuinely unsubscribes from it.

**Compared to WPF.** `DependencyProperty` is eager: a `SetValue` runs
change callbacks through the tree immediately. gooey is lazy, in the
Slint lineage: a `Set` only marks dirty, and evaluation happens at frame
time. Any number of `Set`s between frames collapse into one recomputation
and one repaint, for free — there is no `Dispatcher.Invoke` batching to
arrange yourself.

Properties are confined to the UI goroutine and are unsynchronized by
design. Background work hands results back over a channel or a
`gooey.Dispatcher`; see [how-to: work off the UI goroutine](../howto/howto-async.md).

Depth: [architecture.md — the property system](../../architecture.md#the-property-system).
