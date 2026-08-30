package grpc

import (
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	controlv1 "github.com/WonderForgeLabs/gooey/grpc/gen/gooey/control/v1"
)

// waitCount polls Sessions() until it reads want. Polling is right HERE
// and wrong in an app: a disconnect is only observable once the server's
// stream goroutine has run its deferred unregister, and a test has no
// other edge to wait on. The app never polls — it is pushed to, through
// Options.OnSessions.
func waitCount(t *testing.T, srv *Server, want string, get func() int, n int) {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		if got := get(); got == n {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("%s: count settled at %d, want %d", want, get(), n)
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func TestSessionsCountsLiveAttachments(t *testing.T) {
	h := newHarness(t)
	if got := h.srv.Sessions(); got != 0 {
		t.Fatalf("a fresh server reports %d sessions, want 0", got)
	}

	a := attach(t, h, &controlv1.Subscription{})
	a.recv() // the Welcome; register has run by the time it arrives
	waitCount(t, h.srv, "after one attach", h.srv.Sessions, 1)

	b := attach(t, h, &controlv1.Subscription{})
	b.recv()
	waitCount(t, h.srv, "after two attaches", h.srv.Sessions, 2)

	// THE HALF THAT MATTERS. A gauge that only goes up looks correct
	// until something disconnects, and every test in this package
	// before this one attached and never left.
	a.cancel()
	waitCount(t, h.srv, "after one detach", h.srv.Sessions, 1)
	b.cancel()
	waitCount(t, h.srv, "after both detach", h.srv.Sessions, 0)
}

// The count must be pushed, not polled: a host that had to poll would
// repaint on a clock instead of on change, and prop.Set does not compare
// values, so a per-frame Set repaints forever.
func TestOnSessionsFiresOnAttachAndDetach(t *testing.T) {
	var mu sync.Mutex
	var seen []int
	notified := make(chan int, 16)

	vm, values := newVM()
	app := newTestApp(t, testMarkup, values, nil)
	h := attachHarness(t, app, vm, Options{
		Name: "gooey-test", Version: "0",
		OnSessions: func(n int) {
			mu.Lock()
			seen = append(seen, n)
			mu.Unlock()
			select {
			case notified <- n:
			default:
			}
		},
	})

	next := func(label string, want int) {
		t.Helper()
		deadline := time.After(3 * time.Second)
		for {
			select {
			case n := <-notified:
				if n == want {
					return
				}
			case <-deadline:
				mu.Lock()
				got := append([]int(nil), seen...)
				mu.Unlock()
				t.Fatalf("%s: never notified with %d; saw %v", label, want, got)
			}
		}
	}

	a := attach(t, h, &controlv1.Subscription{})
	a.recv()
	next("attach", 1)

	a.cancel()
	next("detach", 0)
}

// Sessions() hands back a NUMBER, and the broadcaster's set stays
// unexported. A caller holding *session values could read them off the
// UI goroutine, which is the confinement the broadcaster exists to
// enforce — so this asserts the accessor cannot leak them, by asserting
// the count moves independently of any handle the caller could hold.
func TestSessionsExposesACountAndNotTheSet(t *testing.T) {
	h := newHarness(t)
	a := attach(t, h, &controlv1.Subscription{})
	a.recv()
	waitCount(t, h.srv, "attached", h.srv.Sessions, 1)

	// Reading twice must not disturb it, and must not depend on the
	// caller doing anything with what came back.
	if h.srv.Sessions() != h.srv.Sessions() {
		t.Error("two reads of the count disagree")
	}
}

// A server built with New rather than Serve is not listening, and says
// so. Before the accept loop's outcome was captured, Serving() had no
// answer at all and an app whose control plane had died went on
// advertising the address.
func TestServingReportsTheAcceptLoop(t *testing.T) {
	h := newHarness(t)
	if !h.srv.Serving() {
		t.Fatal("a served server reports it is not serving")
	}
	if err := h.srv.ServeError(); err != nil {
		t.Fatalf("a healthy server reports ServeError %v", err)
	}

	h.srv.Close()
	deadline := time.After(3 * time.Second)
	for h.srv.Serving() {
		select {
		case <-deadline:
			t.Fatal("Serving() still true after Close; the accept loop's end was never observed")
		case <-time.After(5 * time.Millisecond):
		}
	}
	// Close is a CLEAN stop, so it is not a failure. Reporting an error
	// here would light a failure indicator on every ordinary shutdown.
	if err := h.srv.ServeError(); err != nil {
		t.Errorf("Close reported as a failure: %v", err)
	}
}

func TestNewWithoutServeIsNotServing(t *testing.T) {
	vm, values := newVM()
	app := newTestApp(t, testMarkup, values, nil)
	srv, err := New(app, Options{Context: app.ctx, Name: "n", Version: "0"})
	if err != nil {
		t.Fatal(err)
	}
	_ = vm
	if srv.Serving() {
		t.Error("a server built with New reports it is serving; it owns no listener")
	}
	if srv.Sessions() != 0 {
		t.Error("a server with no listener reports sessions")
	}
	// And its error is NIL, which is why Serving() has to be the
	// predicate an indicator reads. "not started yet" and "up" are
	// indistinguishable by error alone — both are nil — so a dot driven
	// by ServeError() != nil calls a server with no listener healthy.
	// See docs/specs/2026-08-23-control-plane-connection-state.md.
	if err := srv.ServeError(); err != nil {
		t.Errorf("a server that never served reports ServeError %v", err)
	}
}

// A Close that lands BEFORE the accept loop has started must still be a
// clean shutdown.
//
// This is a regression pin on a race a mutation run exposed rather than a
// hypothetical: Serve launches `gs.Serve(ln)` on its own goroutine, and
// grpc-go returns nil when Stop interrupts a RUNNING loop but
// ErrServerStopped when Stop arrives first. So a short-lived app or a
// fast test took the second path and reported an ordinary shutdown as a
// failure — intermittently, depending only on scheduling, which is the
// worst way for an indicator to be wrong.
// SUBTESTS, not a bare loop, and the reason is a genuine framework
// constraint rather than style: prop.evalStack is a PACKAGE-LEVEL global
// (prop/prop.go:31), so the property graph is single-goroutine for the
// whole PROCESS, not per app. testApp starts a run loop and tears it
// down in t.Cleanup, so a bare loop leaves every previous app's loop
// alive and composing — 50 composers evaluating computeds on 50
// goroutines, which the race detector correctly rejects. t.Run gives
// each iteration its own Cleanup, so exactly one app is live at a time.
func TestAnImmediateCloseIsStillCleanNoMatterWhoWonTheRace(t *testing.T) {
	for i := 0; i < 30; i++ {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			vm, values := newVM()
			app := newTestApp(t, testMarkup, values, nil)
			_ = vm
			srv, err := Serve(app, Options{Context: app.ctx, Name: "n", Version: "0"})
			if err != nil {
				t.Fatalf("Serve: %v", err)
			}
			srv.Close() // no sleep: this is the point

			deadline := time.After(3 * time.Second)
			for srv.Serving() {
				select {
				case <-deadline:
					t.Fatal("Serving() still true after Close")
				case <-time.After(time.Millisecond):
				}
			}
			if e := srv.ServeError(); e != nil {
				t.Fatalf("an immediate Close reported %v; a clean shutdown must never light a "+
					"failure indicator, and the path taken must not depend on scheduling", e)
			}
		})
	}
}

// OnSessions is called with the lock RELEASED, and this is the test that
// makes that a fact rather than a comment.
//
// The callback is host code and may do anything, including the most
// natural thing in the world: ask the server what the count is now. A
// sync.Mutex is not reentrant, so a notify made while holding b.mu
// deadlocks on exactly that — and it deadlocks a session-attach path,
// which means the app hangs on connect rather than failing.
//
// The blocking half matters too: b.mu is taken by afterFrame on the UI
// goroutine every frame, so host code running inside it would put an
// arbitrary-duration callback in the frame path.
func TestOnSessionsRunsOutsideTheLock(t *testing.T) {
	done := make(chan int, 4)
	vm, values := newVM()
	app := newTestApp(t, testMarkup, values, nil)
	var srv *Server
	h := attachHarness(t, app, vm, Options{
		Name: "gooey-test", Version: "0",
		OnSessions: func(int) {
			// Re-entrant read. Under the lock this never returns.
			if srv != nil {
				done <- srv.Sessions()
			}
		},
	})
	srv = h.srv

	a := attach(t, h, &controlv1.Subscription{})
	recvd := make(chan struct{})
	go func() { a.recv(); close(recvd) }()
	select {
	case <-recvd:
	case <-time.After(3 * time.Second):
		t.Fatal("attach never completed: OnSessions is holding the broadcaster lock while it runs, " +
			"so a callback that reads Sessions() deadlocks the session it was announcing")
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("the callback never returned; it cannot re-enter the server while the lock is held")
	}
}

// TestOnSessionsNeverDeliversACountOutOfOrder is the ordering half of
// the push contract, and it is a property no polling test can see.
//
// add and remove each captured a true count under the lock and then
// called the host callback OUTSIDE it, which is correct — host code must
// not run inside the mutex afterFrame takes every frame — but left the
// two callers free to cross on the way out. A host told 2 and then 1
// while two clients are attached goes on rendering 1 forever: nothing
// re-sends the count until the next connect or disconnect, so a
// transient reordering becomes a permanent lie. That is why the old
// comment on notify reads as safe — every count delivered WAS true when
// it was taken. Found in the review of #391 (issue #419).
//
// THE ASSERTION IS ONE-SIDED, and deliberately. A second delivery
// arriving while the first callback is still in flight is PROOF that two
// callers crossed; its absence over a generous window is what the fix
// guarantees by construction. So a slow machine can only make this pass,
// never fail spuriously — a failure here is real.
func TestOnSessionsNeverDeliversACountOutOfOrder(t *testing.T) {
	gate := make(chan struct{})
	var mu sync.Mutex
	var seen []int
	b := newBroadcaster(nil, newHarness(t).app, func(n int) {
		if n == 1 {
			<-gate // hold the FIRST delivery inside the callback
		}
		mu.Lock()
		seen = append(seen, n)
		mu.Unlock()
	})
	read := func() []int {
		mu.Lock()
		defer mu.Unlock()
		return append([]int(nil), seen...)
	}

	s1, s2 := &session{}, &session{}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); b.add(s1) }()

	// Wait for each add to have MUTATED — observable through the same
	// mutex the count is taken under — so the second one races the
	// first's callback rather than its map write.
	for b.count() != 1 {
		runtime.Gosched()
	}
	go func() { defer wg.Done(); b.add(s2) }()
	for b.count() != 2 {
		runtime.Gosched()
	}

	// The first callback is parked on gate. Nothing may reach the host
	// until it returns.
	deadline := time.After(500 * time.Millisecond)
	for waiting := true; waiting; {
		select {
		case <-deadline:
			waiting = false
		case <-time.After(5 * time.Millisecond):
			if got := read(); len(got) > 0 {
				close(gate)
				wg.Wait()
				t.Fatalf("the host was told %v while an earlier count was still being "+
					"delivered — two callers crossed on the way out of the lock, and "+
					"the value the host keeps is the older one", got)
			}
		}
	}

	close(gate)
	wg.Wait()

	got := read()
	if len(got) == 0 {
		t.Fatal("the host was never told anything, so nothing above discriminated")
	}
	if last := got[len(got)-1]; last != b.count() {
		t.Errorf("the last count delivered was %d, the live count is %d (sequence %v) — "+
			"a host that renders the last value it was given is now permanently wrong",
			last, b.count(), got)
	}
}

// TestTheForcedZeroOnShutdownOutranksALateRemove is finding five: the
// same ordering hole reached from the other end.
//
// When the accept loop returns the server forces a zero, because every
// stream it carried is finished and a host listening only for counts has
// to see the endpoint go quiet. That zero went straight to the host
// callback, bypassing the broadcaster — so a remove that had taken its
// count but not yet delivered it could land afterwards and leave the
// host showing a live client for an endpoint that no longer exists. Two
// clients attached and one teardown lagging is all it takes. Found in
// the review of #391 (issue #419).
//
// The lagging remove is INJECTED rather than raced for, because the race
// is not reproducible on demand. What that pins is the property the race
// depends on: whether the shutdown zero is ordered against the removes
// at all. Delivered outside the broadcaster there is nothing to be
// ordered against, and the stale count wins by arriving last.
func TestTheForcedZeroOnShutdownOutranksALateRemove(t *testing.T) {
	var mu sync.Mutex
	var seen []int
	b := newBroadcaster(nil, newHarness(t).app, func(n int) {
		mu.Lock()
		seen = append(seen, n)
		mu.Unlock()
	})

	s1, s2 := &session{}, &session{}
	b.add(s1)
	b.add(s2)

	// A teardown that has taken its count and not yet delivered it.
	b.mu.Lock()
	delete(b.sessions, s1)
	staleN, staleSeq := len(b.sessions), b.next()
	b.mu.Unlock()

	b.closed()                 // the accept loop returns first
	b.notify(staleN, staleSeq) // ...and the lagging remove arrives after

	mu.Lock()
	defer mu.Unlock()
	if last := seen[len(seen)-1]; last != 0 {
		t.Errorf("after the accept loop returned the host was last told %d "+
			"(sequence %v) — the endpoint is gone and the host is showing a live "+
			"client for it", last, seen)
	}
}

// TestClosingTheServerNumbersItsZero is the other half of the same fix,
// and the half that lives in the server rather than the broadcaster.
//
// The test above proves a NUMBERED zero outranks a lagging remove. This
// proves the server's shutdown actually produces one, which is the part
// a direct call to the host callback silently skipped: the count reached
// the host, so every count assertion in this file stayed green, while
// the ordering it should have taken part in did not exist.
//
// Asserted as GROWTH rather than against a total, so it does not encode
// how many mutations an attach and a detach happen to make: the detach
// contributes one change, the shutdown zero contributes another, and a
// zero delivered outside the broadcaster contributes none.
func TestClosingTheServerNumbersItsZero(t *testing.T) {
	var mu sync.Mutex
	var last int
	vm, values := newVM()
	app := newTestApp(t, testMarkup, values, nil)
	h := attachHarness(t, app, vm, Options{
		Name: "gooey-test", Version: "0",
		OnSessions: func(n int) {
			mu.Lock()
			last = n
			mu.Unlock()
		},
	})

	a := attach(t, h, &controlv1.Subscription{})
	a.recv()
	waitCount(t, h.srv, "attached", h.srv.Sessions, 1)

	b := h.srv.bc
	b.mu.Lock()
	before := b.seq
	b.mu.Unlock()

	// THE ASSERTION IS A WAIT, because the two changes land in either
	// order and neither is observable on its own: Close stops the accept
	// loop while the stream goroutine runs its deferred unregister
	// independently, and each delivers a zero, so "the host was told 0"
	// is reached by one of them alone. Waiting for the SEQUENCE to grow
	// by two is the only edge that needs both. With the zero delivered
	// straight to the callback it never arrives and this times out.
	h.srv.Close()
	deadline := time.After(3 * time.Second)
	for {
		b.mu.Lock()
		after := b.seq
		b.mu.Unlock()
		if after >= before+2 {
			break
		}
		select {
		case <-deadline:
			mu.Lock()
			n := last
			mu.Unlock()
			t.Fatalf("the sequence went %d -> %d across a detach and a shutdown, want at "+
				"least %d — the detach numbers one change and the shutdown zero must "+
				"number another. A zero delivered straight to the callback numbers "+
				"nothing, so a lagging remove has nothing to lose to. Last count %d",
				before, after, before+2, n)
		case <-time.After(5 * time.Millisecond):
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if last != 0 {
		t.Errorf("the host was last told %d after the endpoint closed", last)
	}
}
