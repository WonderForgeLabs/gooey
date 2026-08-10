package components

import (
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/prop"
)

// Timer is gooey's DispatcherTimer: a non-visual element that runs a
// Command on an interval. Like KeyBinding it lives in the tree as an
// attachment — declared where it belongs, never measured, arranged, or
// painted.
//
//	<Timer Interval="600ms" Tick="{{.Advance}}" Enabled="{{.Running}}"/>
//
// The whole design is in where the tick is EXECUTED. A ticker goroutine
// may not touch properties, so it does not call Tick; it posts Tick to
// the dispatcher and the app's loop runs it. By the time Tick executes,
// it is ordinary UI-goroutine code that may Get and Set freely, exactly
// like a Button's Click.
//
// Enabled is read at fire time, on the loop, for the same reason: reading
// it in the goroutine would be an off-thread property read. A nil Enabled
// means always enabled. Because it is an ordinary bindable property, the
// graph can pause and resume a timer — bind it to the same property a
// checkbox toggles and the timer stops without anything being torn down.
//
// Lifetime is owned by the Composer, not by the Timer. Hot reload builds
// a new tree and a new Composer; the old Composer's Close stops its
// timers. That is what keeps a replaced tree from leaving a ticker
// running against a viewmodel nobody is showing any more.
type Timer struct {
	gooey.Base
	Interval time.Duration
	Tick     gooey.Action
	Enabled  *prop.Property[bool] // nil = always enabled
}

func (t *Timer) Measure(gooey.Size) gooey.Size { return gooey.Size{} }
func (t *Timer) Render(*gooey.Frame)           {}
func (t *Timer) NonVisual() bool               { return true }

// Start runs the ticker until the returned stop func is called. An
// interval of zero or a missing Tick makes it inert rather than an
// error: a timer with nothing to do is a legal thing to declare while
// building a page.
func (t *Timer) Start(post func(func())) func() {
	if t.Interval <= 0 || t.Tick == nil || post == nil {
		return func() {}
	}
	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		tk := time.NewTicker(t.Interval)
		defer tk.Stop()
		for {
			select {
			case <-done:
				return
			case <-tk.C:
				// Post, never call: this goroutine must not touch the
				// graph. The closure runs later, on the UI loop.
				post(t.fire)
			}
		}
	}()
	// Joining makes stop a barrier: a tick that already won the select
	// posts before stop returns, so Close ⇒ no further posts, ever.
	return func() { close(done); <-stopped }
}

// fire runs on the UI goroutine, having been posted there.
func (t *Timer) fire() {
	if t.Enabled != nil && !t.Enabled.Get() {
		return
	}
	if gooey.CanExecute(t.Tick) {
		t.Tick.Run()
	}
}
