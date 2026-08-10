package gooey

import "sync"

// The dispatcher is the marshaling seam between background work and the
// property graph.
//
// Properties are UI-goroutine-confined and unlocked, so nothing outside
// the main loop may Get or Set them. Every async source — a feed fetch,
// an HTTP handler, a remote Temporal activity — therefore has to hand
// its result back to the loop rather than apply it. cmd/reader already
// does this by hand with a results channel; Dispatcher is that pattern
// promoted to the framework so handlers written by the framework (not
// the app) have somewhere to land.
//
// The loop selects on Wake and calls Drain:
//
//	for running {
//	    if needsFrame { … }
//	    select {
//	    case <-disp.Wake():
//	        disp.Drain()
//	    case ev := <-events:
//	        comp.Handle(ev)
//	    }
//	}
//
// Drained funcs run on the loop goroutine, so they may Set freely; the
// Sets mark dependents dirty, the scheduler hook asks for a frame, and
// the next Frame() repaints exactly the widgets that read them. No
// framework-owned run loop yet — the app still owns its select.

// Dispatcher queues work to run on the UI goroutine. Post is safe from
// any goroutine; Drain must only be called from the UI goroutine.
//
// The queue is unbounded: Post never blocks and never drops, because a
// dropped completion is a permanently stale property, and a blocking
// Post would stall whatever goroutine a handler ran on.
type Dispatcher struct {
	mu    sync.Mutex
	queue []func()
	wake  chan struct{}
}

// NewDispatcher creates an empty dispatcher.
func NewDispatcher() *Dispatcher {
	return &Dispatcher{wake: make(chan struct{}, 1)}
}

// Post queues fn to run on the UI goroutine at the next Drain. Safe to
// call from any goroutine.
func (d *Dispatcher) Post(fn func()) {
	if fn == nil {
		return
	}
	d.mu.Lock()
	d.queue = append(d.queue, fn)
	d.mu.Unlock()
	// A dropped signal cannot lose work: the only way the send fails is
	// that a wake is already pending, and the Drain it triggers takes
	// the whole queue — including this entry.
	select {
	case d.wake <- struct{}{}:
	default:
	}
}

// Wake receives whenever work has been posted. It is the channel the
// app's main loop selects on.
func (d *Dispatcher) Wake() <-chan struct{} { return d.wake }

// Drain runs every queued func and reports how many ran. Call it only
// from the UI goroutine — the funcs touch properties.
//
// Funcs run with the lock released, so one may Post again (a handler
// chaining another handler); the new work lands in the next Drain
// rather than extending this one, which bounds a single drain.
func (d *Dispatcher) Drain() int {
	d.mu.Lock()
	work := d.queue
	d.queue = nil
	d.mu.Unlock()
	for _, fn := range work {
		fn()
	}
	return len(work)
}

// Pending reports how many funcs are queued. For tests and diagnostics.
func (d *Dispatcher) Pending() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.queue)
}
