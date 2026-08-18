package main

// The vendor lifecycle, which until now was the part of this demo with
// the most moving pieces and no tests at all.
//
// Both tests here pin an ORDERING rather than a value, because both bugs
// they exist for are orderings: a build that runs at the wrong time, and
// a restore that runs at the wrong time. Neither shows up in a cell, a
// property or a damage count — the app reaches the same final state
// either way. What differs is what the user was looking at meanwhile.

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/WonderForgeLabs/gooey/control"
)

// queue is a Dispatcher with the run loop taken out: closures accumulate
// and run when the test says so. That is the whole point — a fake that
// ran them inline would satisfy both tests below while the code did
// exactly the thing they exist to forbid.
// Locked, because the real one is: Post is documented safe from any
// goroutine, and a background build posting its result back is precisely
// the caller that makes that matter. An unlocked fake fails under -race
// on the test's own bookkeeping and says nothing about the code.
type queue struct {
	mu  sync.Mutex
	fns []func()
}

func (q *queue) add(fn func()) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.fns = append(q.fns, fn)
}

func (q *queue) len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.fns)
}

// take empties the queue and returns what was in it, so a test can
// inspect and wrap the closures before running them.
func (q *queue) take() []func() {
	q.mu.Lock()
	defer q.mu.Unlock()
	fns := q.fns
	q.fns = nil
	return fns
}

func run(fns []func()) {
	for _, fn := range fns {
		fn()
	}
}

// Subscribe runs `go build`, and Subscribe is a Click handler. If the
// build runs inline, the run loop is not draining, not composing and not
// reading input for as long as it takes — the app is frozen, by its own
// buy button, in a demo about which party can freeze your UI.
//
// The build here BLOCKS until the test releases it. A build that returned
// immediately would pass this test whether or not it was on the UI
// goroutine, which is to say it would test nothing.
func TestSubscribeDoesNotBuildOnTheUIGoroutine(t *testing.T) {
	s := NewStore(nil)
	q := &queue{}
	s.post = q.add

	entered, release := make(chan struct{}), make(chan struct{})
	v := newVendors("127.0.0.1:1")
	v.build = func(bin, pkg string) ([]byte, error) {
		close(entered)
		<-release
		return nil, errors.New("test build")
	}
	s.vendors = v

	// An item with a product behind it, at a price the wallet covers.
	items := append([]Integration(nil), s.items.Get()...)
	i := -1
	for n := range items {
		if items[n].Cmd != "" {
			i, items[n].Active = n, false
			break
		}
	}
	if i < 0 {
		t.Fatal("no catalogue entry has a Cmd — this test has nothing to launch")
	}
	s.items.Set(items)
	s.itemSel.Set(i)
	s.buying.Set(i)
	s.walletC.Set(items[i].Cents + 1)

	returned := make(chan struct{})
	go func() { s.Subscribe(); close(returned) }()

	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("the build never started")
	}
	select {
	case <-returned:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("Subscribe did not return while the build was still running — " +
			"it is building on the UI goroutine, and the app is frozen for the duration")
	}

	// The interim receipt is up already, so the click visibly did
	// something. An uncached build otherwise looks like a missed click.
	if got := s.receipt.Get(); !strings.Contains(got, "starting") {
		t.Errorf("receipt while the build runs is %q, want it to say the launch started", got)
	}

	close(release)
	// The real receipt comes back through the queue and nowhere else: a
	// launch that wrote the property from its own goroutine would be
	// touching the graph off the UI goroutine.
	deadline := time.After(5 * time.Second)
	for q.len() == 0 {
		select {
		case <-deadline:
			t.Fatal("the launch never posted its result back to the UI goroutine")
		case <-time.After(time.Millisecond):
		}
	}
	run(q.take())
	if got := s.receipt.Get(); !strings.Contains(got, "failed to build") {
		t.Errorf("after draining, receipt is %q, want the build failure", got)
	}
}

// Cancel undoes two things, and the second one races. Every vendor RPC
// is a closure on the same Dispatcher, so a patch_markup sent just before
// the kill is already queued; a restore called INLINE runs in front of
// it, the patch lands on top, and Cancel reports a toolbar it did not
// restore. Posting puts the restore behind the queue.
//
// This pins the ordering only. It does not pin "the picker is gone" —
// a patch arriving after this post still wins, and closing that needs a
// revocable grant the framework does not have. See Cancel's comment.
func TestCancelQueuesTheRestoreBehindInFlightVendorWork(t *testing.T) {
	s := NewStore(fstest.MapFS{"toolbar.gooey": &fstest.MapFile{Data: []byte("<Gooey/>")}})
	s.svc = control.NewService(stubHost{}, s.Context(s.logo))
	s.vendors = newVendors("")
	q := &queue{}
	s.post = q.add

	items := append([]Integration(nil), s.items.Get()...)
	i := -1
	for n := range items {
		if items[n].Cmd != "" {
			i, items[n].Active = n, true
			break
		}
	}
	if i < 0 {
		t.Fatal("no catalogue entry has a Cmd — nothing to cancel")
	}
	s.items.Set(items)
	s.itemSel.Set(i)

	// A vendor RPC that got in just before the cancel.
	var order []string
	q.add(func() { order = append(order, "vendor patch") })

	s.Cancel()

	if len(order) != 0 {
		t.Fatalf("Cancel ran queued work itself: %v", order)
	}
	fns := q.take()
	if len(fns) != 2 {
		t.Fatalf("queue holds %d closures, want 2 — the in-flight patch and the restore behind it", len(fns))
	}

	// The restore fails here (stubHost has no Composer), and that is
	// fine: the failure is what makes it observable. What matters is
	// WHEN it ran.
	fns[1] = func(prev func()) func() {
		return func() { order = append(order, "restore"); prev() }
	}(fns[1])
	run(fns)

	if len(order) != 2 || order[0] != "vendor patch" || order[1] != "restore" {
		t.Errorf("ran %v, want the in-flight patch first and the restore behind it", order)
	}
	// Cancel's own receipt is immediate; the restore only amends it if it
	// failed, which it did.
	if got := s.receipt.Get(); !strings.Contains(got, "still installed") {
		t.Errorf("receipt is %q, want it to report the toolbar was not restored", got)
	}
}
