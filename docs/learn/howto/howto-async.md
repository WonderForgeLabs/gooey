# How to work off the UI goroutine

Properties are confined to the UI goroutine and carry no locking. Nothing
outside your main loop may `Get` or `Set` them. Background work therefore
has to **hand its result back** to the loop rather than apply it.

The App gives you one way to do that: `app.Post`, which queues a closure
to run on the loop.

## Post a closure back to the loop

```go
fetch := gooey.Command(func() {
	url := feedURL.Get() // read on the UI goroutine, before leaving it
	go func() {
		body, err := get(url) // no property access in here
		app.Post(func() {     // runs later, on the UI goroutine
			if err != nil {
				status.Set("error: " + err.Error())
				return
			}
			article.Set(body) // Sets happen HERE
		})
	}()
})
```

- `Post` is safe from any goroutine and never blocks: the queue is
  unbounded, because a dropped completion is a permanently stale
  property and a blocking `Post` would stall whatever goroutine the work
  ran on.
- Posted funcs run with the lock released, so one may `Post` again; the
  new work lands in the *next* drain rather than extending the current
  one, which bounds a single drain.
- `app.Dispatcher()` exposes the queue itself (`Pending()` for tests).

## Post on an interval

For the app's own clock — a poll, a data generator, anything that must
keep running across a hot reload:

```go
app.Every(2*time.Second, func() {
	clock.Set(time.Now().Format("15:04:05"))
})
```

The ticker lives on its own goroutine and only ever posts, so the func
you write is ordinary UI code. For a tick that belongs to the UI
instead, declare a `<Timer>` in the markup — its lifetime is then the
composition's, and a reload replaces it.

## Doing it by hand

Before there was an App, every program wrote this seam itself, and the
older demos still show it. It is worth reading once, because it is the
same shape:

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

`cmd/reader` in this repository still does exactly this for its feed
fetches. The App's loop is the same thing with the `select` written
once: it drains the dispatcher, which is where `Post` puts your closure.

## The rules that keep this safe

**Read properties before you leave the goroutine.** Capture the values
your background work needs while still on the UI goroutine, and pass them
in. A `Get` from a worker goroutine is a data race even though it looks
harmless — a computed `Get` can *write* (it caches its value and rewires
dependency edges).

**Do all your `Set`s in one place.** Everything drained or received in
the loop runs on the UI goroutine, so `Set` freely there. The `Set`s mark
dependents dirty, the scheduler hook asks for a frame, and the next
`Frame()` repaints exactly the components that read them.

**Do not `Set` on every frame.** A `Set` on a property some component
painted from dirties that component, which schedules another frame. If you
want per-frame counters on screen, set them from `app.BeforeFrame` — that
hook runs immediately before the frame is composed, so what it sets
paints in the frame it precedes instead of dirtying the tree afterwards
and never settling.

## Why the loop cannot take another channel

`app.Run` selects on a fixed set of cases, and there is no way to hand it
one more. That is deliberate rather than an omission: a `select` over a
dynamic set of channels needs reflection, which this framework does not
use anywhere. Everything asynchronous goes through `Post` instead —
which is the confinement rule restated, not a workaround for it.

## See also

- [Concept: the property graph](../concepts/property-graph.md)
- [Tutorial 3: Bind data and drive state](../03-binding-and-state.md)
