package main

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/WonderForgeLabs/gooey/leak"
)

// TestMain catches goroutines that never exit, for every test in this
// package including ones written later by someone who never read this file.
//
// IT DOES NOT CATCH THE BUG THIS FILE WAS WRITTEN FOR, and saying so is the
// point of the comment. Remote.Close used to cancel its stream and return
// without waiting for the reader; six tests called Connect and all passed.
// Removing the join again leaves this TestMain green too — measured, not
// assumed — because the reader does exit, just microseconds after Close
// returned. An end-of-binary check cannot see a race window.
//
// TestCloseIsABarrierForTheReader below is the one that catches it, and it
// does NOT do so by counting goroutines. See its comment: that was tried,
// measured, and does not work.
func TestMain(m *testing.M) { leak.TestMain(m) }

// TestCloseIsABarrierForTheReader is the automatic detector for the review
// finding, and for the whole class it belongs to.
//
// The claim under test is not "the reader eventually stops". It is that
// Close is a BARRIER: the moment it returns, the reader has already finished,
// so nothing it owns can still fire. That matters concretely — read() calls
// r.fail on error, fail calls OnLost, and OnLost is an app-supplied callback
// that touches the editor. Without the join, teardown looks complete while a
// callback is still on its way.
//
// # Why this does not diff goroutines
//
// The obvious test — snapshot the goroutine set, Close, assert the reader is
// gone with zero tolerance — was written, and MEASURED NOT TO WORK. Two
// findings, both from real runs of this package:
//
//   - With the join removed it never went red. `runtime.Stack(buf, true)`
//     stops the world and formats every goroutine, which takes far longer
//     than the window being hunted; by the time the dump is walked, read()
//     has long since returned. Zero settle tolerance does not help when the
//     instrument itself costs more than the gap.
//   - The same coarseness runs the other way. Sampled 8 runs, the reader was
//     absent from the dump entirely in 5 of them when sampled immediately
//     after `go r.read()` — a goroutine that provably exists and is parked in
//     Recv simply is not in the trace yet. Any "the reader is running"
//     precondition built on it is a coin flip.
//
// An earlier version of this test did go red without the join, and that was
// luck: with no owner filter it was reporting gRPC's own server goroutines
// still winding down, not ours. Filtering to the reader made it honest and
// made it stop firing.
//
// # What it does instead
//
// It observes the thing the barrier exists to protect. OnLost is made to take
// a measurable amount of time, and the assertion is that Close cannot return
// until OnLost has RUN TO COMPLETION. That is the contract in one line, and
// the margin is chosen by the test rather than by the scheduler, so it
// discriminates deterministically instead of racing:
//
//	with the join:    Close blocks on <-r.done, which read() closes only
//	                  after fail/OnLost has returned  ->  flag set
//	without the join: Close returns as soon as conn.Close() does, while
//	                  OnLost is still inside its sleep  ->  flag clear
//
// Verified both ways by removing the join from Remote.Close and putting it
// back.
func TestCloseIsABarrierForTheReader(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	addr, _ := target(t)

	r, err := Connect(ctx, addr, []string{"Body"})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	// callbackHold is the margin. It has to beat the scheduling jitter of a
	// loaded CI box by enough that a pass is a pass, and be short enough that
	// nobody deletes the test for being slow. It is paid once, and only on
	// this test.
	const callbackHold = 200 * time.Millisecond
	var entered, finished atomic.Bool
	r.OnLost = func(error) {
		entered.Store(true)
		time.Sleep(callbackHold)
		finished.Store(true)
	}

	if err := r.Close(); err != nil {
		t.Logf("close returned %v (not fatal; the barrier is what is under test)", err)
	}
	closeReturned := finished.Load()

	// Discrimination first, and it is not decoration: if the reader never ran
	// OnLost at all, `finished` would be false for a reason that has nothing
	// to do with joining, and this test would report a barrier violation that
	// did not happen. Wait for it to have at least STARTED before judging.
	deadline := time.Now().Add(5 * time.Second)
	for !entered.Load() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !entered.Load() {
		t.Fatal("OnLost was never called, so Close did not end the session the way " +
			"this test assumes; the assertion below would be vacuous")
	}

	if !closeReturned {
		t.Errorf("Close returned while OnLost was still running.\n"+
			"Close is a BARRIER: after it returns, nothing the reader owns may fire "+
			"again — and OnLost touches the editor that teardown has just finished "+
			"with. The idiom is close-then-JOIN; `r.stop()` followed by "+
			"`<-r.done` in Remote.Close is what supplies it, and the wait must "+
			"come after the cancel or a healthy stream blocks forever.\n"+
			"(OnLost was made to take %s so this is a margin, not a race.)", callbackHold)
	}
}

// TestLeakBarrierWouldCatchASlowStop keeps leak.Barrier honest about the case
// it CAN see, which is a goroutine that outlives its stop by more than the
// cost of a stack dump.
//
// It is here rather than in leak's own package tests because this file is
// where the limits of the tool were found. Barrier is not useless — a Timer
// mid-callback, a decoder still draining, a worker in a backoff sleep all
// linger for milliseconds and are caught — it just cannot resolve the
// microsecond window that Remote.Close had. Pinning both halves is what stops
// the next reader from reaching for the wrong one.
func TestLeakBarrierWouldCatchASlowStop(t *testing.T) {
	rec := &recordingTB{}
	before := leak.Snapshot()

	release := make(chan struct{})
	started := make(chan struct{})
	go slowStopVictim(started, release)
	<-started
	defer close(release)

	// Wait for the goroutine to become VISIBLE to runtime.Stack before asking
	// Barrier about it, and keep that a separate step from the assertion. A
	// goroutine is not in the trace the instant it exists — measured here, a
	// reader parked in Recv was missing from the dump in 5 of 8 samples taken
	// right after its `go` statement. Folding that wait into the assertion
	// would turn a real report into a coin flip; leaving it out would fail the
	// test for a reason that has nothing to do with Barrier.
	deadline := time.Now().Add(5 * time.Second)
	for len(leak.Diff(before, "slowStopVictim")) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}

	leak.Barrier(rec, before, "slowStopVictim")
	if len(rec.msgs) == 0 {
		t.Error("leak.Barrier did not report a goroutine that was still parked when " +
			"the barrier was taken; if this stops firing, Barrier catches nothing at " +
			"all and should be deleted rather than trusted")
	}
}

// slowStopVictim is a named function so the owner filter has a frame to match,
// which is the same discipline every real caller of Barrier has to follow.
func slowStopVictim(started chan<- struct{}, release <-chan struct{}) {
	close(started)
	<-release
}

// recordingTB captures what leak would have reported without failing the test
// doing the capturing.
type recordingTB struct{ msgs []string }

func (r *recordingTB) Helper()                           {}
func (r *recordingTB) Errorf(format string, args ...any) { r.msgs = append(r.msgs, format) }
