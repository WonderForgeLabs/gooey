package gooey_test

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/WonderForgeLabs/gooey"
)

// loop stands in for the UI goroutine: it counts what was posted and, more
// importantly, records whether anything arrived after stop returned. That
// last fact is the entire contract — "stopped" has to mean stopped, not
// stopping.
type loop struct {
	// slow widens the window a post occupies. Without it these tests
	// measure luck: a post is a few instructions, so a stop that failed to
	// join usually returns before the in-flight post lands and the
	// assertion passes against the bug. A dispatcher under real load is
	// slow anyway — this is the realistic case, not a contrived one.
	slow time.Duration

	mu     sync.Mutex
	closed bool
	n      int
	after  int // posts that arrived after stop returned
}

func (l *loop) post(fn func()) {
	if l.slow > 0 {
		time.Sleep(l.slow)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.n++
	if l.closed {
		l.after++
	}
	_ = fn
}

// seal marks the moment stop returned.
func (l *loop) seal() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.closed = true
}

func (l *loop) counts() (n, after int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.n, l.after
}

// The pin CLAUDE.md names as a trap and seven components wrote by hand: a
// stop that only signals lets a tick which already won its select post
// afterwards.
//
// The interval is short and the loop long deliberately — a single
// stop-then-check would pass against a signal-only implementation most of
// the time, which is exactly why that bug survives code review. Repeating
// it turns "usually fine" into a near-certain failure.
func TestEveryStopIsABarrierNotASignal(t *testing.T) {
	for range 20 {
		l := &loop{slow: 300 * time.Microsecond}
		stop := gooey.Every(l.post, 100*time.Microsecond, func() {})
		time.Sleep(2 * time.Millisecond)
		stop()
		l.seal()
		// Give any goroutine that outlived stop a generous chance to be
		// seen. If stop were a barrier only by luck, this is where the
		// luck runs out.
		time.Sleep(3 * time.Millisecond)
		if _, after := l.counts(); after != 0 {
			t.Fatalf("%d posts arrived after stop returned — stop signalled but did not join", after)
		}
	}
}

func TestEveryActuallyTicks(t *testing.T) {
	l := &loop{}
	stop := gooey.Every(l.post, 200*time.Microsecond, func() {})
	deadline := time.Now().Add(time.Second)
	for {
		if n, _ := l.counts(); n >= 3 {
			break
		}
		if time.Now().After(deadline) {
			n, _ := l.counts()
			t.Fatalf("only %d posts in a second at 200µs — the ticker is not running", n)
		}
		time.Sleep(time.Millisecond)
	}
	stop()
}

// fn runs on the UI goroutine, so Every must hand the closure over rather
// than call it. A ticker that invoked fn itself would satisfy every
// count-based assertion above while violating UI-goroutine confinement,
// which nothing in the framework catches.
func TestEveryPostsRatherThanCalls(t *testing.T) {
	var called atomic.Int64
	var posted atomic.Int64
	post := func(fn func()) {
		posted.Add(1)
		_ = fn // deliberately NOT run: a real dispatcher runs it later
	}
	stop := gooey.Every(post, 200*time.Microsecond, func() { called.Add(1) })
	deadline := time.Now().Add(time.Second)
	for posted.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	stop()
	if posted.Load() < 3 {
		t.Fatalf("only %d posts — the ticker is not running", posted.Load())
	}
	if got := called.Load(); got != 0 {
		t.Fatalf("fn ran %d times on the ticker goroutine — it must only be posted", got)
	}
}

// Declining to start is a no-op stop and no goroutine, not a panic out of
// time.NewTicker. Timer relies on this for Interval <= 0.
func TestEveryDeclinesToStart(t *testing.T) {
	l := &loop{}
	for _, c := range []struct {
		name string
		stop func() func()
	}{
		{"non-positive interval", func() func() { return gooey.Every(l.post, 0, func() {}) }},
		{"negative interval", func() func() { return gooey.Every(l.post, -time.Second, func() {}) }},
		{"nil fn", func() func() { return gooey.Every(l.post, time.Millisecond, nil) }},
		{"nil post", func() func() { return gooey.Every(nil, time.Millisecond, func() {}) }},
	} {
		t.Run(c.name, func(t *testing.T) {
			stop := c.stop()
			if stop == nil {
				t.Fatal("stop is nil — every caller would panic on Close")
			}
			stop()
			stop() // a decline is idempotent; the real one need not be
			time.Sleep(2 * time.Millisecond)
			if n, _ := l.counts(); n != 0 {
				t.Fatalf("%d posts from a group that declined to start", n)
			}
		})
	}
}

// The same barrier for the group shape, and what it pins is the JOIN
// specifically — a delay that passed its gate check microseconds before
// close(done) and is already inside post. No gate can catch that one; only
// waiting for it can.
//
// It does not pin After's inner select: removing that leaves this green,
// because the join catches those posts too. See the comment there for what
// the inner select is actually for and why nothing asserts it.
//
// The delays expire across the moment of the stop rather than all at one
// instant, so the window is hit rather than straddled.
func TestDelaysStopIsABarrierNotASignal(t *testing.T) {
	for range 20 {
		l := &loop{slow: 300 * time.Microsecond}
		var g gooey.Delays
		stop := g.Start(l.post)
		for i := range 12 {
			g.After(time.Duration(600+i*80)*time.Microsecond, func() {})
		}
		time.Sleep(time.Millisecond)
		stop()
		l.seal()
		time.Sleep(3 * time.Millisecond)
		if _, after := l.counts(); after != 0 {
			t.Fatalf("%d delays posted after stop returned", after)
		}
	}
}

func TestDelaysFire(t *testing.T) {
	l := &loop{}
	var g gooey.Delays
	stop := g.Start(l.post)
	defer stop()
	for range 3 {
		g.After(200*time.Microsecond, func() {})
	}
	deadline := time.Now().Add(time.Second)
	for {
		if n, _ := l.counts(); n == 3 {
			return
		}
		if time.Now().After(deadline) {
			n, _ := l.counts()
			t.Fatalf("%d of 3 delays fired in a second", n)
		}
		time.Sleep(time.Millisecond)
	}
}

// A stopped group posts nothing, however hard it is asked.
//
// What this does NOT pin, stated so nobody reads it as more than it is:
// the `g.post = nil` in the stop func. The closed gate alone makes this
// pass, so the two guards are redundant and the redundancy makes the
// weaker one untestable from the outside. It stays because it is what
// stops a torn-down page arming one goroutine per hover that exists only
// to exit — a leak, not a wrong post, and a goroutine count is too coarse
// a clock to assert it either way.
func TestDelaysAfterStopPostNothing(t *testing.T) {
	l := &loop{}
	var g gooey.Delays
	stop := g.Start(l.post)
	stop()
	l.seal()
	for range 5 {
		g.After(200*time.Microsecond, func() {})
	}
	time.Sleep(3 * time.Millisecond)
	if n, _ := l.counts(); n != 0 {
		t.Fatalf("%d posts from a stopped group", n)
	}
}

// After on a group that was never started is the same inert case — a
// component whose Start was never called (no Composer, a unit test) must
// not leak a goroutine per hover.
func TestDelaysBeforeStartArmNothing(t *testing.T) {
	var g gooey.Delays
	g.After(time.Microsecond, func() { t.Error("a delay fired on a group that was never started") })
	time.Sleep(2 * time.Millisecond)
}

func TestDelaysDeclineNonPositiveAndNil(t *testing.T) {
	l := &loop{}
	var g gooey.Delays
	stop := g.Start(l.post)
	defer stop()
	g.After(0, func() {})
	g.After(-time.Second, func() {})
	g.After(time.Microsecond, nil)
	time.Sleep(3 * time.Millisecond)
	if n, _ := l.counts(); n != 0 {
		t.Fatalf("%d posts from declined delays — a zero delay must not become an uncancellable immediate post", n)
	}
}

// Restart has to re-arm. A group whose second Start reused the first
// gate would be permanently inert after one stop, which is how a
// hot-reloaded page loses its tooltips with nothing to show for it.
func TestDelaysRestart(t *testing.T) {
	l := &loop{}
	var g gooey.Delays
	g.Start(l.post)()
	stop := g.Start(l.post)
	defer stop()
	g.After(200*time.Microsecond, func() {})
	deadline := time.Now().Add(time.Second)
	for {
		if n, _ := l.counts(); n == 1 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("a delay armed after a restart never fired — Start reused the closed gate")
		}
		time.Sleep(time.Millisecond)
	}
}
