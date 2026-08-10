package components

import (
	"testing"
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/prop"
)

// waitFor polls until cond holds or the deadline passes. Timers are the
// one part of the framework with a real clock in them, so the tests wait
// on the CONDITION rather than sleeping a guessed duration.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestTimerDeliversTicksThroughTheDispatcher(t *testing.T) {
	d := gooey.NewDispatcher()
	ticks := 0
	timer := &Timer{Interval: time.Millisecond, Tick: gooey.Command(func() { ticks++ })}

	stop := timer.Start(d.Post)
	defer stop()

	waitFor(t, "a posted tick", func() bool { return d.Pending() > 0 })

	// Nothing has run yet: the goroutine posted, it did not execute.
	if ticks != 0 {
		t.Fatalf("Tick ran before the loop drained (ticks=%d) — it must not run on the timer goroutine", ticks)
	}
	if n := d.Drain(); n == 0 {
		t.Fatal("drain ran nothing")
	}
	if ticks == 0 {
		t.Error("draining the dispatcher did not run the tick")
	}
}

// Enabled is read at fire time, on the UI goroutine — so a disabled
// timer still posts, but the posted func does nothing. That is what lets
// the property graph pause a timer without tearing anything down.
func TestTimerEnabledFalseSuppressesTheTick(t *testing.T) {
	d := gooey.NewDispatcher()
	ticks := 0
	enabled := prop.NewSource(false)
	timer := &Timer{Interval: time.Millisecond, Tick: gooey.Command(func() { ticks++ }), Enabled: enabled}

	stop := timer.Start(d.Post)
	defer stop()

	waitFor(t, "a posted tick", func() bool { return d.Pending() > 0 })
	d.Drain()
	if ticks != 0 {
		t.Fatalf("disabled timer fired %d times", ticks)
	}

	// Flipping the property resumes it, with no restart.
	enabled.Set(true)
	waitFor(t, "a tick after enabling", func() bool {
		d.Drain()
		return ticks > 0
	})
}

func TestTimerNilEnabledMeansAlwaysOn(t *testing.T) {
	d := gooey.NewDispatcher()
	ticks := 0
	timer := &Timer{Interval: time.Millisecond, Tick: gooey.Command(func() { ticks++ })}
	stop := timer.Start(d.Post)
	defer stop()

	waitFor(t, "a tick", func() bool {
		d.Drain()
		return ticks > 0
	})
}

// A timer with nothing to do is legal to declare and simply inert.
func TestTimerWithoutIntervalOrCommandIsInert(t *testing.T) {
	d := gooey.NewDispatcher()
	for _, timer := range []*Timer{
		{Interval: 0, Tick: gooey.Command(func() {})},
		{Interval: time.Millisecond},
	} {
		stop := timer.Start(d.Post)
		time.Sleep(5 * time.Millisecond)
		stop()
	}
	if n := d.Pending(); n != 0 {
		t.Errorf("an inert timer posted %d funcs", n)
	}
}

// Timers are non-visual: they hang off a parent as attachments and are
// never laid out or painted.
func TestTimerIsNonVisual(t *testing.T) {
	timer := &Timer{Interval: time.Second}
	if !timer.NonVisual() {
		t.Error("Timer must report itself non-visual")
	}
	if got := timer.Measure(gooey.Size{W: 80, H: 24}); got != (gooey.Size{}) {
		t.Errorf("Timer measured %+v, want zero", got)
	}
}

// The composition owns the timer's lifetime: a tree that was built but
// never started does not tick.
func TestComposerStartsAttachedTimers(t *testing.T) {
	d := gooey.NewDispatcher()
	ticks := 0
	root := &VStack{Children: []gooey.Component{&Text{Content: Str("x")}}}
	root.Attach(&Timer{Interval: time.Millisecond, Tick: gooey.Command(func() { ticks++ })})

	comp := gooey.NewComposer(root, 20, 3)
	time.Sleep(5 * time.Millisecond)
	if n := d.Pending(); n != 0 {
		t.Fatalf("timer ticked before Start (%d posts) — starting is the composition's job", n)
	}

	comp.Start(d)
	defer comp.Close()
	waitFor(t, "a tick after Start", func() bool {
		d.Drain()
		return ticks > 0
	})
}

// The leak this design exists to prevent: hot reload swaps trees, and
// the replaced composition's ticker must die with it.
func TestComposerCloseStopsTimersSoSwapsDoNotLeak(t *testing.T) {
	d := gooey.NewDispatcher()
	ticks := 0
	root := &VStack{Children: []gooey.Component{&Text{Content: Str("x")}}}
	root.Attach(&Timer{Interval: time.Millisecond, Tick: gooey.Command(func() { ticks++ })})

	comp := gooey.NewComposer(root, 20, 3)
	comp.Start(d)
	waitFor(t, "the timer to start ticking", func() bool {
		d.Drain()
		return ticks > 0
	})

	comp.Close()
	// Drain whatever was already in flight at the moment of Close, then
	// confirm the queue stays empty: the goroutine is gone, not merely
	// quiet.
	time.Sleep(10 * time.Millisecond)
	d.Drain()
	before := ticks

	time.Sleep(30 * time.Millisecond)
	if n := d.Pending(); n != 0 {
		t.Errorf("%d posts arrived after Close — the ticker outlived its composition", n)
	}
	d.Drain()
	if ticks != before {
		t.Errorf("timer fired %d more times after Close", ticks-before)
	}
}

func TestComposerCloseIsIdempotentAndStartRestarts(t *testing.T) {
	d := gooey.NewDispatcher()
	ticks := 0
	root := &VStack{Children: []gooey.Component{&Text{Content: Str("x")}}}
	root.Attach(&Timer{Interval: time.Millisecond, Tick: gooey.Command(func() { ticks++ })})

	comp := gooey.NewComposer(root, 20, 3)
	comp.Start(d)
	comp.Close()
	comp.Close() // must not panic on a double close

	// Start again after a Close brings the same composition back to life.
	comp.Start(d)
	defer comp.Close()
	waitFor(t, "ticks after a restart", func() bool {
		d.Drain()
		return ticks > 0
	})
}

// Starting twice must not leave the first ticker running — otherwise an
// attach() helper that forgets a Close would silently double the rate.
func TestComposerStartTwiceStopsTheFirstRun(t *testing.T) {
	d := gooey.NewDispatcher()
	root := &VStack{Children: []gooey.Component{&Text{Content: Str("x")}}}
	root.Attach(&Timer{Interval: time.Millisecond, Tick: gooey.Command(func() {})})

	comp := gooey.NewComposer(root, 20, 3)
	comp.Start(d)
	comp.Start(d)
	comp.Close()

	time.Sleep(10 * time.Millisecond)
	d.Drain()
	time.Sleep(30 * time.Millisecond)
	if n := d.Pending(); n != 0 {
		t.Errorf("%d posts after Close following a double Start — a ticker leaked", n)
	}
}
