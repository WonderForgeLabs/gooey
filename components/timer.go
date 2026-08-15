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
	if t.Tick == nil {
		return func() {}
	}
	// gooey.Every owns the close-and-join contract — see startable.go. It
	// also declines a non-positive interval, which is why Interval <= 0 is
	// no longer guarded here.
	return gooey.Every(post, t.Interval, t.fire)
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
