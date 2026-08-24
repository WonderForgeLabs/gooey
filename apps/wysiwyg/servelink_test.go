package main

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/WonderForgeLabs/gooey"
)

// UI-goroutine confinement for the connection dot.
//
// THESE MUST BE RUN WITH -race, and the reason is that the bug they
// catch is invisible without it. Properties are unlocked by design, so
// a Set from a server callback is a data race and nothing in the
// framework will notice — the symptom is a corrupted dependency graph
// some frames later, in a build nobody is watching.
//
// apps/wysiwyg is NOT in CI's -race tier (that tier is handlers/*,
// packs/*, mcp and grpc), and CI vets apps/* without running their
// suites at all. So the -race run of this file is a thing a human or an
// agent does deliberately:
//
//	cd apps/wysiwyg && go test -race -run 'Marshalled|Posted|BindRace' ./...
//
// The shape that makes this worth a file of its own is the asymmetry in
// grpc/session.go: a session REGISTERS on the UI goroutine (through the
// bridge) and UNREGISTERS on its own stream goroutine (a plain defer).
// A direct Set in the callback therefore works for every connect and
// races on every disconnect. It passes any test that only attaches.

// queuePost is a post that records without running, so a test can prove
// a write went THROUGH the dispatcher rather than around it.
type queuePost struct {
	mu   sync.Mutex
	work []func()
}

func (q *queuePost) post(fn func()) {
	q.mu.Lock()
	q.work = append(q.work, fn)
	q.mu.Unlock()
}

func (q *queuePost) drain() int {
	q.mu.Lock()
	work := q.work
	q.work = nil
	q.mu.Unlock()
	for _, fn := range work {
		fn()
	}
	return len(work)
}

// TestEveryNotificationIsPosted is the pin on there being exactly ONE
// path into the property graph.
//
// It holds the dispatcher shut and checks the state does not move. That
// catches the direct Set, and it also catches the subtler version: a
// bind that seeds the state directly "because we are on the main
// goroutine here anyway". Two paths where one is safe is how the unsafe
// one survives review — the argument for the exception is always true
// at the moment it is written and stops being true when someone moves
// the call.
func TestEveryNotificationIsPosted(t *testing.T) {
	l := newEndpointLink()
	w := newLinkWatch(l)
	srv := &stubServer{serving: true, sessions: 3}
	var q queuePost

	w.bindSessions(q.post, srv)
	if got := l.state.Get(); got != linkDown {
		t.Fatalf("bind moved the state to %v without a drain: even the FIRST reading has "+
			"to go through post, or there are two routes into the graph and only one of "+
			"them is safe on an arbitrary goroutine", got)
	}
	if n := q.drain(); n != 1 {
		t.Fatalf("bind queued %d closures, want 1", n)
	}
	if got := l.state.Get(); got != linkActive {
		t.Fatalf("after draining bind's reading the state is %v, want linkActive: three "+
			"sessions are attached", got)
	}

	// And the same for a change notification.
	srv.set(func(s *stubServer) { s.sessions = 0 })
	w.onSessions(0)
	if got := l.state.Get(); got != linkActive {
		t.Errorf("a callback moved the state to %v before anything drained: it wrote the "+
			"property on the calling goroutine", got)
	}
	q.drain()
	if got := l.state.Get(); got != linkIdle {
		t.Errorf("after the drain the state is %v, want linkIdle", got)
	}
}

// TestADisconnectFromAnotherGoroutineIsMarshalled is the production
// shape, run against the race detector.
//
// The notifications come from goroutines that are not the UI one, which
// is exactly how a disconnect arrives, while this goroutine reads the
// property and drains. With the callback posting there is no race; with
// a direct Set there is a write on the callback goroutine concurrent
// with a read here, and -race says so by name.
//
// It also asserts the settled value, so the test still discriminates
// when run without the detector: a notification that was dropped rather
// than marshalled leaves the dot stale, which is the same visible defect
// by a different mechanism.
func TestADisconnectFromAnotherGoroutineIsMarshalled(t *testing.T) {
	l := newEndpointLink()
	w := newLinkWatch(l)
	srv := &stubServer{serving: true, sessions: 1}
	disp := gooey.NewDispatcher()

	w.bindSessions(disp.Post, srv)
	disp.Drain()
	if got := l.state.Get(); got != linkActive {
		t.Fatalf("the endpoint did not start attached (%v), so the disconnect below "+
			"asserts nothing", got)
	}

	const rounds = 200
	var wg sync.WaitGroup
	wg.Add(2)
	// The disconnect goroutine: a stream goroutine ending its defer.
	go func() {
		defer wg.Done()
		for i := 0; i < rounds; i++ {
			srv.set(func(s *stubServer) { s.sessions = i % 2 })
			w.onSessions(i % 2)
		}
	}()
	// A second one, because two streams can end at once and their
	// notifications interleave in whatever order they reach the queue.
	go func() {
		defer wg.Done()
		for i := 0; i < rounds; i++ {
			w.onSessions(0)
		}
	}()

	// This goroutine is the UI one: it reads the property and drains.
	// The read is what turns a direct Set on the goroutines above into a
	// detected race rather than a silent one.
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	for {
		disp.Drain()
		_ = l.state.Get()
		select {
		case <-done:
			// Settle: the notifications posted before the goroutines
			// finished may still be queued.
			srv.set(func(s *stubServer) { s.sessions = 0 })
			w.onSessions(0)
			deadline := time.Now().Add(2 * time.Second)
			for disp.Pending() > 0 && time.Now().Before(deadline) {
				disp.Drain()
			}
			if got := l.state.Get(); got != linkIdle {
				t.Fatalf("after every client detached the dot still reports %v, want "+
					"linkIdle: a notification was dropped rather than marshalled, which "+
					"leaves the light claiming a session that has gone", got)
			}
			return
		default:
		}
	}
}

// TestBindRaceWithAnEarlyCallbackIsSafe is the window the mutex in
// linkWatch exists for, and it is reachable rather than theoretical.
//
// grpc.Serve starts its accept goroutine and RETURNS; main only calls
// bind after that. A client connecting in between fires OnSessions
// against a watch whose server field is still nil and about to be
// written. Without the lock that is a write/read race on an interface
// value — two words, so it can also tear.
func TestBindRaceWithAnEarlyCallbackIsSafe(t *testing.T) {
	l := newEndpointLink()
	w := newLinkWatch(l)
	srv := &stubServer{serving: true, sessions: 1}
	disp := gooey.NewDispatcher()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			w.onSessions(1)
		}
	}()
	w.bindSessions(disp.Post, srv)
	wg.Wait()

	deadline := time.Now().Add(2 * time.Second)
	for disp.Pending() > 0 && time.Now().Before(deadline) {
		disp.Drain()
	}
	if got := l.state.Get(); got != linkActive {
		t.Errorf("after a bind racing an early connect the dot reports %v, want "+
			"linkActive", got)
	}
}

// TestANotificationBeforeBindIsNotLost is the other half of that
// window: notify with no post yet drops the closure, so the FIRST
// reading has to come from bind rather than from a queued delta.
//
// This is why apply re-reads the server instead of applying the count
// the callback carried. A delta can be dropped; a reading cannot be out
// of date, because there is nothing after it to be out of date with
// respect to.
func TestANotificationBeforeBindIsNotLost(t *testing.T) {
	l := newEndpointLink()
	w := newLinkWatch(l)
	srv := &stubServer{serving: true, sessions: 2}

	w.onSessions(2) // no post installed: dropped on the floor

	var q queuePost
	w.bindSessions(q.post, srv)
	q.drain()
	if got := l.state.Get(); got != linkActive {
		t.Errorf("the dot reports %v after a notification that arrived before bind, want "+
			"linkActive: bind's own reading has to recover the state a dropped callback "+
			"would have carried", got)
	}
}

// TestTheServeEndCallbackTakesTheEndpointDown is MCP's only transition,
// and it is the one that matters: a URL whose listener is gone is an
// instruction to connect somewhere that will refuse.
func TestTheServeEndCallbackTakesTheEndpointDown(t *testing.T) {
	l := newEndpointLink()
	w := newLinkWatch(l)
	srv := &stubServer{serving: true}
	var q queuePost

	w.bindStateless(q.post, srv)
	q.drain()
	if got := l.state.Get(); got != linkServing {
		t.Fatalf("a live MCP endpoint reports %v, want linkServing", got)
	}

	srv.set(func(s *stubServer) {
		s.serving = false
		s.err = fmt.Errorf("accept tcp 127.0.0.1:46271: use of closed network connection")
	})
	w.onServeEnd(srv.err)
	if got := l.state.Get(); got != linkServing {
		t.Error("onServeEnd wrote the property on the serve goroutine: it runs there, " +
			"never on the UI one")
	}
	q.drain()
	if got := l.state.Get(); got != linkDown {
		t.Errorf("after the accept loop died the dot reports %v, want linkDown", got)
	}
	if d := l.detail; d == nil || !strings.Contains(d(), "use of closed network connection") {
		t.Errorf("the detail does not carry why the endpoint stopped")
	}
}
