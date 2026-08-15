package gooey

import (
	"sync"
	"time"
)

// Startable is implemented by non-visual elements that own a background
// goroutine. The Composer discovers them while walking the tree and
// starts them when the composition goes live; Composer.Close stops them.
//
// The post parameter is the ONLY way a started element may reach the
// property graph: it queues a func onto the UI goroutine (Dispatcher.Post).
// A Startable that touches properties from its own goroutine violates
// UI-goroutine confinement, and nothing in the framework will catch it —
// the properties are unlocked by design.
type Startable interface {
	Start(post func(func())) (stop func())
}

// A STOP FUNC MUST CLOSE AND JOIN.
//
// close(done) alone only signals; a tick that already won its select still
// posts afterwards, so "stopped" means "will stop shortly" and lifetime
// tests flake. Joining is what makes stop a barrier and lets Close ⇒ no
// further posts, ever, be a fact rather than a probability.
//
// That is one line of difference and it was written out by hand in seven
// places, which is six chances to write the wrong one. Every and Delays
// below own the contract instead. Reach for them rather than another
// hand-rolled done/stopped pair — a Startable that does its own channels
// is now a claim that this shape does not fit, and the framework will not
// catch it if the claim is wrong.

// Every posts fn onto the UI goroutine every d until the returned stop is
// called, and stop does not return until the ticker goroutine has exited.
//
// fn runs on the UI goroutine, so it may touch the property graph freely;
// nothing else in the closure may. A non-positive d, a nil fn or a nil
// post is a Startable declining to start — the returned stop is a no-op
// and no goroutine exists — rather than a panic out of time.NewTicker.
func Every(post func(func()), d time.Duration, fn func()) (stop func()) {
	if post == nil || fn == nil || d <= 0 {
		return func() {}
	}
	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		tk := time.NewTicker(d)
		defer tk.Stop()
		for {
			select {
			case <-done:
				return
			case <-tk.C:
				// Post, never call: this goroutine must not touch the
				// graph. The closure runs later, on the UI loop.
				post(fn)
			}
		}
	}()
	return func() { close(done); <-stopped }
}

// Delays is a group of one-shot delays that stop together: any number may
// be in flight, and the stop returned by Start cancels every one that has
// not fired and joins every one that has.
//
// The shape a ticker cannot serve. A tooltip arms a show per hover and a
// toast a dismissal per toast, so the count is unbounded and each has its
// own deadline; what they share is the gate. Embed one and forward Start
// to it.
//
// Start and After are UI-goroutine-only, which is what keeps this
// lock-free — After's wg.Go cannot race the stop func's wg.Wait because
// the Composer calls stop from the same goroutine that armed the delays.
// Calling After from a worker is the same confinement violation as
// touching a property there, and is caught by nothing.
type Delays struct {
	post func(func())
	done chan struct{}
	wg   sync.WaitGroup
}

// Start arms the group. The returned stop closes the gate, joins every
// delay still in flight, and leaves the group inert: After does nothing
// until the next Start.
func (g *Delays) Start(post func(func())) (stop func()) {
	g.post, g.done = post, make(chan struct{})
	done := g.done
	return func() {
		g.post = nil
		close(done)
		g.wg.Wait()
	}
}

// Armed reports whether the group is between a Start and its stop.
//
// For the control whose no-loop behaviour is not "do nothing": a Tooltip
// with no dispatcher shows immediately rather than never, because a tip
// that silently declines to appear in a unit test is a component that
// cannot be tested at all. After alone cannot express that — it declines,
// and declining is indistinguishable from a delay that has not elapsed.
func (g *Delays) Armed() bool { return g.post != nil }

// After posts fn onto the UI goroutine once, d from now, unless the group
// stops first. A non-positive d, a nil fn or an unstarted group arms
// nothing — deliberately, since "immediately" from a delay group would be
// a post the stop func cannot cancel.
func (g *Delays) After(d time.Duration, fn func()) {
	if g.post == nil || fn == nil || d <= 0 {
		return
	}
	post, done := g.post, g.done
	g.wg.Go(func() {
		tm := time.NewTimer(d)
		defer tm.Stop()
		select {
		case <-done:
		case <-tm.C:
			// The JOIN in stop is what makes the barrier true: a timer
			// that already fired posts before stop returns, and one that
			// has not is cancelled by the outer select.
			//
			// This inner re-check is NOT what makes it true, and saying so
			// was wrong — mutation testing removed it and every barrier
			// assertion stayed green, because the join catches exactly the
			// posts it was supposed to be catching. What it actually buys
			// is narrower: when the timer fires in the same instant the
			// gate closes, both outer cases are ready and select picks at
			// random, so without it a delay that was cancelled still posts
			// a closure the dispatcher will run against a component the
			// Composer has already stopped. It suppresses the closure, not
			// the race. No test pins it — the window is not addressable
			// from outside — so it is kept as a documented backstop rather
			// than as a claim.
			select {
			case <-done:
			default:
				post(fn)
			}
		}
	})
}
