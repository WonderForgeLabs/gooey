# How to work off the UI goroutine

Properties are confined to the UI goroutine and carry no locking. Nothing
outside your main loop may `Get` or `Set` them. Background work therefore
has to **hand its result back** to the loop rather than apply it.

There are two ways to do that, and they differ only in ceremony.

## Option 1: your own results channel

Fine when you have one kind of async work:

```go
type feedResult struct {
	url  string
	body string
	err  error
}

results := make(chan feedResult, 8)

fetch := gooey.Command(func() {
	url := feedURL.Get() // read on the UI goroutine, before leaving it
	go func() {
		body, err := get(url) // no property access in here
		results <- feedResult{url: url, body: body, err: err}
	}()
})

for running {
	if needsFrame { /* Frame + Flush */ }
	select {
	case r := <-results:
		if r.err != nil {
			status.Set("error: " + r.err.Error())
			continue
		}
		article.Set(r.body) // Sets happen HERE, on the UI goroutine
	case ev := <-events:
		comp.Handle(ev)
	}
}
```

`cmd/reader` in this repository does exactly this for its feed fetches.

## Option 2: a Dispatcher

`gooey.Dispatcher` is that pattern promoted to the framework, so the
async code posts a closure instead of defining a result type:

```go
disp := gooey.NewDispatcher()

fetch := gooey.Command(func() {
	url := feedURL.Get()
	go func() {
		body, err := get(url)
		disp.Post(func() {          // runs later, on the UI goroutine
			if err != nil {
				status.Set("error: " + err.Error())
				return
			}
			article.Set(body)
		})
	}()
})

for running {
	if needsFrame { /* Frame + Flush */ }
	select {
	case <-disp.Wake():
		disp.Drain()
	case ev := <-events:
		comp.Handle(ev)
	}
}
```

- `Post` is safe from any goroutine and never blocks: the queue is
  unbounded, because a dropped completion is a permanently stale
  property and a blocking `Post` would stall whatever goroutine the work
  ran on.
- `Wake` is the channel your loop selects on.
- **`Drain` must only be called from the UI goroutine** — the funcs it
  runs touch properties.
- Drained funcs run with the lock released, so one may `Post` again; the
  new work lands in the *next* drain rather than extending the current
  one, which bounds a single drain.
- `Pending()` reports the queue depth, for tests and diagnostics.

## The rules that keep this safe

**Read properties before you leave the goroutine.** Capture the values
your background work needs while still on the UI goroutine, and pass them
in. A `Get` from a worker goroutine is a data race even though it looks
harmless — a computed `Get` can *write* (it caches its value and rewires
dependency edges).

**Do all your `Set`s in one place.** Everything drained or received in
the loop runs on the UI goroutine, so `Set` freely there. The `Set`s mark
dependents dirty, the scheduler hook asks for a frame, and the next
`Frame()` repaints exactly the widgets that read them.

**Do not `Set` on every frame.** A `Set` on a property some widget
painted from dirties that widget, which schedules another frame. If you
want per-frame counters on screen, keep them in plain variables and
snapshot them into a property on demand — [tutorial 3](../03-binding-and-state.md)
shows the pattern.

**Timers are async too.** A `time.Ticker` fires on its own goroutine, so
route it through the same seam:

```go
case <-ticker.C:
	clock.Set(time.Now().Format("15:04:05"))
```

That `case` is in the main `select`, so it is already on the UI
goroutine — no dispatcher needed.

## Current limitation

There is no framework-owned run loop yet: the app still owns its
`select`, which is why every example in these tutorials writes one out.
The Dispatcher gives framework-written handlers somewhere to land their
results; it does not take the loop over.

## See also

- [Concept: the property graph](../concepts/property-graph.md)
- [Tutorial 3: Bind data and drive state](../03-binding-and-state.md)
