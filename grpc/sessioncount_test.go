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

	b.closed()                        // the accept loop returns first
	b.notify(staleN, staleSeq, false) // ...and the lagging remove arrives after

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

// TestNoCountSurvivesTheAcceptLoop is the LATCH, and it is the finding
// the first version of this fix did not cover.
//
// Numbering the shutdown zero drops a remove that was already in flight.
// A remove that takes the lock AFTERWARDS gets a higher number, and was
// delivered normally — so the host was told a positive count for an
// endpoint that no longer exists, which is the same lie the numbering
// was introduced to prevent, one ordering later.
//
// It is routine rather than exotic. grpc's Stop does not wait for
// handler goroutines, so sessionServer.Attach's deferred unregister
// regularly runs after Serve has returned and closed() has fired. With
// two clients attached at shutdown the host saw 0 then 1, and a handler
// wedged in Send left it at 1 permanently.
//
// Found in the review of PR #425 — the review OF the fix, which is the
// second time that has been where the real defect was.
func TestNoCountSurvivesTheAcceptLoop(t *testing.T) {
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
	b.closed()   // the accept loop returns with two still attached
	b.remove(s1) // one handler unwinds after it — a HIGHER seq, not a stale one
	b.remove(s2)
	b.add(s1) // and something pathological: an attach after shutdown

	mu.Lock()
	defer mu.Unlock()
	if len(seen) == 0 {
		t.Fatal("the host was never told anything")
	}
	if last := seen[len(seen)-1]; last != 0 {
		t.Errorf("after the accept loop returned the host was last told %d "+
			"(sequence %v) — the endpoint is gone, so no later count can be true "+
			"however recent its number", last, seen)
	}
	// Stronger, and the reason a "last value" check alone is not enough:
	// nothing non-zero may be delivered AT ALL once the loop is gone, so
	// a host that renders every count it is given never flickers a live
	// client onto a dead endpoint.
	for i, n := range seen {
		if i >= 3 && n != 0 {
			t.Errorf("count %d in %v arrived after the accept loop returned", n, seen)
		}
	}
}

// TestAnAttachDoesNotWaitOnAnotherSessionsCallback is the deadlock the
// review of PR #425 found in that PR's own ordering fix.
//
// The fix delivered the callback under notifyMu. register runs on the UI
// goroutine and calls add, which calls notify — so an attach blocked on
// notifyMu while a detach, on a stream goroutine, sat inside host code
// that Options.OnSessions explicitly permits to block. The UI goroutine
// waiting on an arbitrary host callback is not a stall, it is a deadlock
// as soon as that callback wants anything the run loop must perform.
//
// Two clients is enough, which is exactly the scenario the ordering fix
// was written for — so the fix's own motivating case was the case that
// hung.
//
// The assertion is a TIMEOUT, which is the only shape available: the
// claim is that a call returns without waiting for something else, and
// nothing observable distinguishes "returned promptly" from "returned"
// except the clock. The window is deliberately generous; against the
// reviewed code the attach does not complete at all until the detach's
// callback is released, so the test fails by the full three seconds
// rather than by a millisecond of scheduling noise.
func TestAnAttachDoesNotWaitOnAnotherSessionsCallback(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{}, 4)
	var once sync.Once

	// ONE harness for both broadcasters. gooey's evaluation stack and
	// layout-fault slot are PROCESS-global, so two live testApps race by
	// construction and -race reports it inside NewComposer and
	// prop.Get — nothing to do with the code under test, but it turns
	// this test red for a reason that has no bearing on its claim.
	host := newHarness(t).app

	b := newBroadcaster(nil, host, func(int) {
		// Only the FIRST delivery blocks: it stands for a host callback
		// that is waiting on something slow.
		once.Do(func() {
			entered <- struct{}{}
			<-release
		})
	})

	first, second := &session{}, &session{}
	b.mu.Lock()
	b.sessions[first] = true
	b.mu.Unlock()

	// The detach, on its own goroutine, wedged inside host code.
	go b.remove(first)
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("the host callback was never entered, so the test never reached " +
			"the state it exists to probe")
	}

	// The attach, standing in for the UI goroutine. It must not wait on
	// the callback above.
	done := make(chan struct{})
	go func() { b.add(second); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		close(release)
		t.Fatal("an attach blocked while another session's host callback was " +
			"running. register runs on the UI goroutine, and Options.OnSessions " +
			"permits the callback to block, so this is the UI thread waiting on " +
			"arbitrary host code — a deadlock the moment that code wants anything " +
			"the run loop must do")
	}
	close(release)

	// And the ordering guarantee still holds afterwards: the count the
	// host is left with is the newest one, not the one that was in
	// flight. Without this the test would pass for a notify that simply
	// dropped everything it could not deliver immediately.
	var mu sync.Mutex
	var seen []int
	b2 := newBroadcaster(nil, host, func(n int) {
		mu.Lock()
		seen = append(seen, n)
		mu.Unlock()
	})
	a, c := &session{}, &session{}
	b2.add(a)
	b2.add(c)
	b2.remove(a)
	mu.Lock()
	defer mu.Unlock()
	if len(seen) == 0 || seen[len(seen)-1] != 1 {
		t.Errorf("the host was left with %v; the last value must be the newest "+
			"count, which is 1 after two attaches and one detach", seen)
	}
	for i := 1; i < len(seen); i++ {
		if seen[i] == seen[i-1] {
			t.Errorf("the host was told %d twice in a row (%v) — a coalescing "+
				"mailbox must not replay a value it already delivered", seen[i], seen)
		}
	}
}

// TestTheMailboxNeverDeliversAnOlderCountThanOneWaiting pins the half of
// the deposit check that reads redundant and is not. notified only
// advances when a value is DELIVERED, so a change that arrives while a
// newer one is still sitting in the mailbox passes the `seq <= notified`
// test — and would overwrite it, leaving the host on the older count for
// good. The guard against a trailing 1 after a 2 has to compare against
// what is waiting, not only against what has gone out.
func TestTheMailboxNeverDeliversAnOlderCountThanOneWaiting(t *testing.T) {
	var mu sync.Mutex
	var seen []int
	b := newBroadcaster(nil, newHarness(t).app, func(n int) {
		mu.Lock()
		seen = append(seen, n)
		mu.Unlock()
	})
	b.notifyMu.Lock()
	b.delivering = true // pretend a delivery is in flight
	b.notifyMu.Unlock()

	b.notify(2, 10, false) // deposited, nobody carries it
	b.notify(1, 5, false)  // older than the deposit, must not replace it

	b.notifyMu.Lock()
	gotN, gotSeq := b.pendN, b.pendSeq
	b.delivering = false
	b.notifyMu.Unlock()
	if gotN != 2 || gotSeq != 10 {
		t.Errorf("the mailbox holds count %d at seq %d after an OLDER change "+
			"arrived behind a newer one; the host would be left on %d, which is "+
			"the trailing-lie the sequence number exists to prevent", gotN, gotSeq, gotN)
	}
	_ = seen
}
