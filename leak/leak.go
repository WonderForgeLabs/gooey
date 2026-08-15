// Package leak turns gooey's stop-must-join rule into something a test
// suite enforces instead of something a reviewer has to notice.
//
// # The rule, and why prose was not enough
//
// Every asynchronous thing in this framework — a Timer, a Spinner, a
// ProgressBar, a Toast, the terminal decoder, a gRPC stream reader — owns a
// goroutine, and its stop function must CLOSE AND JOIN:
//
//	func() { close(done); <-stopped }
//
// Closing alone is not stopping. A goroutine that has already won its
// select can still run to completion after the close, which means it can
// still Post to the dispatcher, still fire an app-supplied callback, still
// touch a component that teardown has already finished with. Whether it
// does depends on where it happened to be when you called Close, so the
// failure is intermittent, arrives in CI rather than locally, and reads as
// a flake rather than as the missing line it is.
//
// This package exists because that rule was written down in three places
// and still got missed. apps/wysiwyg's Remote.Close cancelled its
// stream and returned without waiting for the reader, and no test failed —
// there was nothing that could fail. A review caught it. Reviews are not a
// mechanism.
//
// # What it detects, and what it cannot
//
// It compares the set of running goroutines before and after, and reports
// the ones that appeared and stayed. That is exactly what it does, and the
// two things it therefore cannot do are worth knowing before you rely on it.
//
// It cannot catch a goroutine that leaks only under a race you did not
// provoke — it reports what your test actually caused. Pair it with -race,
// which finds the other half: the unsynchronized access such a goroutine
// tends to make.
//
// And it cannot resolve a gap shorter than a stack dump. Barrier below is the
// entry point for the close-without-join question, and its comment carries the
// measurements: a goroutine set is a coarse clock in BOTH directions — a
// goroutine that has just exited is already absent, and one that has just been
// created is not there yet. Where the window is microseconds, assert on the
// effect rather than on the goroutine.
//
// # Two ways to use it
//
// Per test, when you want the check attached to one teardown:
//
//	func TestTheReaderStops(t *testing.T) {
//	    defer leak.Check(t)()
//	    r := connect(t)
//	    r.Close()
//	}
//
// Or per PACKAGE, which is the one to reach for, because it needs no
// discipline from the next test anyone writes:
//
//	func TestMain(m *testing.M) { leak.TestMain(m) }
//
// That makes the check automatic for every test in the package, including
// tests written later by someone who never read this comment — which is
// the only kind of enforcement that survives contact with a repository.
//
// It is deliberately dependency-free (runtime and strings) because gooey's
// root module has exactly two direct requirements by doctrine, and a leak
// checker is not the thing to spend the third on.
package leak

import (
	"fmt"
	"os"
	"runtime"
	"sort"
	"strings"
	"time"
)

// settleFor is how long a goroutine is given to finish exiting before it
// counts as leaked.
//
// A join is not instantaneous: the joining goroutine returns as soon as the
// channel closes, and the goroutine that closed it still has to be
// descheduled. Zero tolerance would report that as a leak on a loaded
// machine, which is the fastest way to get a checker deleted for flaking.
// Everything gooey stops is stopped by a channel close, so this is orders
// of magnitude more than the real settle time and still short enough to be
// invisible in a suite.
const settleFor = 500 * time.Millisecond

// pollEvery is how often the snapshot is retaken while settling.
const pollEvery = 10 * time.Millisecond

// TB is the part of *testing.T this package uses. An interface rather than
// the concrete type so that leak can be used from a benchmark, a fuzz
// target, or a helper of your own — and so this package does not import
// testing into anything that links it.
type TB interface {
	Helper()
	Errorf(format string, args ...any)
}

// Barrier asks a DIFFERENT question from Check: not "did this goroutine ever
// exit" but "had it already exited by the time stop returned".
//
// The contract of a stop function is "when this returns, that goroutine has
// finished" — close(done) then <-stopped — because callers rely on it to
// mean no further Post, no further callback, no further touch of a component
// being torn down. Check cannot see a violation of that, because a stop that
// forgets to join usually still produces a goroutine that exits: cancel the
// context, close the connection, and it finishes a few microseconds later,
// long before the binary ends. Measured — removing the join from
// apps/wysiwyg's Remote.Close left that package green under TestMain.
//
// So the assertion is taken the instant stop returns, with no settle window:
//
//	before := leak.Snapshot()
//	w := start(t)
//	w.Stop()
//	leak.Barrier(t, before, "mypkg.(*Worker).loop")
//
// # What it can and cannot resolve — measured, and the limit is real
//
// It catches a goroutine that outlives its stop by MORE than the cost of a
// stack dump: a timer mid-callback, a decoder draining, a worker in a backoff
// sleep. Milliseconds are visible.
//
// It does NOT catch a microsecond-scale window, and reaching for it there
// will produce a test that passes forever while the bug is present. The
// instrument is the problem: runtime.Stack(buf, true) stops the world and
// formats every goroutine in the process, which costs more than the gap being
// hunted. Both directions were measured against apps/wysiwyg's reader —
// with the join removed the reader was already gone from every post-Close
// dump, and sampled immediately after its own `go` statement it was ABSENT
// FROM THE DUMP ENTIRELY in 5 of 8 runs while provably parked in Recv. A
// goroutine set is simply not a precise clock.
//
// For a window that tight, assert on the EFFECT the barrier protects instead:
// make the callback take a margin you choose, and require stop not to return
// until it has completed. apps/wysiwyg/leak_test.go does exactly that and
// explains why.
//
// # owned is not optional
//
// owned narrows the assertion to goroutines whose stack mentions one of the
// given substrings — normally the type or package whose stop function is
// under test.
//
// PASS IT. Without it the check reports every goroutine started since the
// snapshot, and in any test that dials a server that includes the SERVER's
// internals: gRPC's loopyWriter and transport readers are still winding
// down when your client's Close returns, through no fault of yours. The
// first version of this caught three grpc transport goroutines and none of
// the reader it was written for — red for the wrong reason, which is a flake
// that has not happened yet. Naming what you own is what makes the failure
// message true, and it is also what exposed the limit above: once the filter
// was correct, this check stopped firing on that bug at all.
//
// A substring that matches nothing turns Barrier into an assertion over the
// empty set, which passes silently forever. Prove your filter matches
// something before trusting a green — Diff exists for that.
func Barrier(t TB, before map[string]string, owned ...string) {
	t.Helper()
	var leaked []string
	for _, g := range stacks() {
		id, _, ok := strings.Cut(strings.TrimPrefix(g, "goroutine "), " ")
		if !ok || before[id] != "" || ignore(g) {
			continue
		}
		if !mentions(g, owned) {
			continue
		}
		leaked = append(leaked, g)
	}
	if len(leaked) == 0 {
		return
	}
	sort.Strings(leaked)
	t.Errorf("stop returned while %d goroutine(s) it owns were still running — "+
		"a stop function is a BARRIER: after it returns, nothing it started may "+
		"post, fire a callback, or touch anything again. The idiom is "+
		"`func() { close(done); <-stopped }`; a close without the join gives you "+
		"exactly this window, and it is intermittent because it depends on where "+
		"the goroutine happened to be:\n\n%s",
		len(leaked), strings.Join(leaked, "\n\n"))
}

// mentions reports whether a stack belongs to one of the named owners. No
// owners means every goroutine qualifies, which is the wide-open mode and
// is documented above as the one not to use.
func mentions(stack string, owned []string) bool {
	if len(owned) == 0 {
		return true
	}
	for _, o := range owned {
		if strings.Contains(stack, o) {
			return true
		}
	}
	return false
}

// Snapshot exposes the before-set for Barrier. Named for what a caller does
// with it rather than for how it is implemented.
func Snapshot() map[string]string { return snapshot() }

// Check snapshots the running goroutines and returns a function that
// reports any that appeared and stayed. Call it deferred:
//
//	defer leak.Check(t)()
//
// The two-step shape is what lets it bracket exactly the region you care
// about, rather than the whole test.
func Check(t TB) func() {
	before := snapshot()
	return func() {
		t.Helper()
		if leaked := diff(before); len(leaked) > 0 {
			t.Errorf("%d goroutine(s) still running after the test finished — "+
				"a stop that does not JOIN returns while its goroutine is still live, "+
				"so it can still post, still fire a callback, and still touch a "+
				"component that teardown has finished with:\n\n%s",
				len(leaked), strings.Join(leaked, "\n\n"))
		}
	}
}

// TestMain runs a package's tests with the check applied to the package as
// a whole, and is the form worth preferring: no test has to remember
// anything.
//
// It reports the leak by FAILING the run, after the tests' own result, so a
// package whose tests pass but whose goroutines linger does not report
// success. The exit code is the tests' own when they failed, and 1 when
// they passed but leaked — a leak must never be reportable as a pass.
func TestMain(m interface{ Run() int }) {
	before := snapshot()
	code := m.Run()
	if leaked := diff(before); len(leaked) > 0 {
		fmt.Fprintf(os.Stderr,
			"\nleak: %d goroutine(s) outlived the test binary.\n"+
				"Something's stop function closed its channel without joining the goroutine.\n"+
				"The idiom is: func() { close(done); <-stopped }\n\n%s\n",
			len(leaked), strings.Join(leaked, "\n\n"))
		if code == 0 {
			code = 1
		}
	}
	os.Exit(code)
}

// snapshot records the identity of every goroutine currently running.
//
// Keyed by the goroutine's own id, taken from the "goroutine N [state]:"
// header, because that is the only thing that distinguishes two goroutines
// running identical code — and a leak of one worker out of four identical
// ones is exactly the case worth catching.
func snapshot() map[string]string {
	out := map[string]string{}
	for _, g := range stacks() {
		id, _, ok := strings.Cut(strings.TrimPrefix(g, "goroutine "), " ")
		if !ok {
			continue
		}
		out[id] = g
	}
	return out
}

// diff returns the stacks of goroutines not present in before, having
// given them settleFor to exit.
func diff(before map[string]string) []string {
	deadline := time.Now().Add(settleFor)
	for {
		var leaked []string
		for _, g := range stacks() {
			id, _, ok := strings.Cut(strings.TrimPrefix(g, "goroutine "), " ")
			if !ok || before[id] != "" || ignore(g) {
				continue
			}
			leaked = append(leaked, g)
		}
		if len(leaked) == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			sort.Strings(leaked)
			return leaked
		}
		time.Sleep(pollEvery)
	}
}

// stacks returns one string per running goroutine.
func stacks() []string {
	buf := make([]byte, 1<<16)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			buf = buf[:n]
			break
		}
		// Stack truncates rather than reporting how much it needed, so a
		// short read is indistinguishable from an exact fit. Grow and retry
		// until it fits with room to spare; a truncated dump would silently
		// hide the very goroutine being hunted.
		buf = make([]byte, 2*len(buf))
	}
	parts := strings.Split(string(buf), "\n\ngoroutine ")
	for i := 1; i < len(parts); i++ {
		parts[i] = "goroutine " + parts[i]
	}
	return parts
}

// ignore filters goroutines that are the runtime's or the test harness's
// own and are never anyone's leak.
//
// Kept deliberately short. Every entry here is a place a real leak could
// hide, so the list is the cost of the checker rather than a convenience —
// it earns its place only because these appear and disappear on their own
// schedule and would otherwise make the checker report noise, which is how
// a checker gets switched off.
func ignore(stack string) bool {
	for _, frame := range []string{
		"runtime.gopark",              // parked runtime workers
		"testing.(*T).Run",            // the harness running subtests
		"testing.tRunner",             // a test still finishing
		"runtime.goexit",              // exiting
		"os/signal.signal_recv",       // the signal handler, process-wide
		"runtime/pprof",               // profiling, if enabled
		"created by runtime.doInit",   // package init
		"created by testing.runTests", // the harness itself
	} {
		if strings.Contains(stack, frame) {
			return true
		}
	}
	return false
}

// Diff reports which goroutines are running now that were not in before,
// with no settle window. Exported for a test that needs to prove the
// goroutine it is about to join actually STARTED — a barrier assertion over
// a component that never spawned anything is trivially true, and that is
// how this kind of test rots into asserting nothing.
func Diff(before map[string]string, owned ...string) []string {
	var out []string
	for _, g := range stacks() {
		id, _, ok := strings.Cut(strings.TrimPrefix(g, "goroutine "), " ")
		if !ok || before[id] != "" || ignore(g) || !mentions(g, owned) {
			continue
		}
		out = append(out, g)
	}
	return out
}
