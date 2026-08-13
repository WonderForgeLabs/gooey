package leak

import (
	"strings"
	"testing"
	"time"
)

// recorder stands in for *testing.T so a test can assert what the checker
// WOULD have reported without failing itself.
type recorder struct{ msgs []string }

func (r *recorder) Helper() {}
func (r *recorder) Errorf(format string, args ...any) {
	r.msgs = append(r.msgs, format)
}

// TestALeakedGoroutineIsReported is the discrimination half, and the one
// that matters: a checker that never fires is indistinguishable from no
// checker at all, and this package exists because the repo already had the
// rule written down three times.
//
// The leak here is the exact shape of the bug it was built for — a stop
// that closes without joining, leaving the goroutine parked on a channel.
// A parked goroutine is the case most at risk of being filtered out as
// runtime noise, so it is the one worth pinning.
func TestALeakedGoroutineIsReported(t *testing.T) {
	rec := &recorder{}
	release := make(chan struct{})
	done := func() {
		defer close(release) // let the leaked goroutine go when we are done
	}
	defer done()

	check := Check(rec)
	go func() { <-release }() // parked, and nobody joins it
	// Give it a moment to actually be running before the snapshot diff.
	time.Sleep(20 * time.Millisecond)
	check()

	if len(rec.msgs) == 0 {
		t.Fatal("a goroutine parked on a channel receive was NOT reported; " +
			"the ignore list is swallowing real leaks, which makes this package worse than nothing")
	}
	if !strings.Contains(rec.msgs[0], "JOIN") {
		t.Errorf("the report does not mention joining, so it does not say what to do: %q", rec.msgs[0])
	}
}

// TestAJoinedGoroutineIsNotReported is the equivalence half. Without it the
// checker could pass the test above by reporting everything always.
func TestAJoinedGoroutineIsNotReported(t *testing.T) {
	rec := &recorder{}
	check := Check(rec)

	// The idiom the framework requires: close AND join.
	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		<-done
	}()
	stop := func() { close(done); <-stopped }
	stop()

	check()
	if len(rec.msgs) != 0 {
		t.Errorf("a correctly joined goroutine was reported as a leak: %v", rec.msgs)
	}
}

// TestAGoroutineThatIsStillExitingIsNotALeak — the settle window. A join
// returns the instant the channel closes, and the goroutine that closed it
// still has to be descheduled; reporting that would flake on a loaded
// machine and get the checker deleted.
func TestAGoroutineThatIsStillExitingIsNotALeak(t *testing.T) {
	rec := &recorder{}
	check := Check(rec)
	go func() { time.Sleep(30 * time.Millisecond) }()
	check()
	if len(rec.msgs) != 0 {
		t.Errorf("a goroutine that exits well inside the settle window was reported: %v", rec.msgs)
	}
}

// TestGoroutinesAreDistinguishedByIdentity — four identical workers, one
// leaked. Keying on the stack text rather than the goroutine id would make
// the leaked one look like a duplicate of the three that exited, and this
// is precisely the case a worker pool produces.
func TestGoroutinesAreDistinguishedByIdentity(t *testing.T) {
	rec := &recorder{}
	release := make(chan struct{})
	defer close(release)

	check := Check(rec)
	done := make(chan struct{})
	stopped := make(chan struct{}, 3)
	for i := 0; i < 3; i++ {
		go func() {
			defer func() { stopped <- struct{}{} }()
			<-done
		}()
	}
	go func() { <-release }() // the fourth, identical, and leaked
	close(done)
	for i := 0; i < 3; i++ {
		<-stopped
	}
	check()

	if len(rec.msgs) == 0 {
		t.Error("the one leaked worker among four identical ones was not reported")
	}
}
