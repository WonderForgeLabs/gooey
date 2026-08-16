package gooey

import (
	"testing"
	"time"

	"github.com/WonderForgeLabs/gooey/prop"
)

func everyTestApp(t *testing.T) *App {
	t.Helper()
	app, _ := newTestApp(t, &label{text: prop.NewComputed(func() string { return "" })})
	return app
}

// App.Every's stop is a BARRIER, not a signal.
//
// This used to run its own ticker and stop with
// once.Do(func() { close(done) }) — no join — which is the trap CLAUDE.md
// names and the reason seven components were rewritten onto gooey.Every.
// It survived here, in the runtime that starts them. Found by a subagent
// converting cmd/props, which uses App.Every.
//
// Asserted through Pending(): nothing drains the dispatcher in this test,
// so a tick that escapes the stop shows up as a queue that grew after
// stop() returned.
func TestAppEveryStopIsABarrierNotASignal(t *testing.T) {
	for range 40 {
		app := everyTestApp(t)
		stop := app.Every(100*time.Microsecond, func() {})
		time.Sleep(2 * time.Millisecond)
		stop()
		settled := app.disp.Pending()
		time.Sleep(3 * time.Millisecond)
		if grew := app.disp.Pending(); grew != settled {
			t.Fatalf("the queue went %d→%d after stop returned — stop signalled but did not join", settled, grew)
		}
	}
}

// The sync.Once is not redundant with the helper. This stop is registered
// in a.stops AND handed to the caller, so shutdown and an explicit stop
// both run it — and gooey.Every's stop closes a channel, which panics on
// a second call.
func TestAppEveryStopIsIdempotent(t *testing.T) {
	app := everyTestApp(t)
	stop := app.Every(time.Millisecond, func() {})
	stop()
	stop() // a.stops calls it again at shutdown
}

func TestAppEveryDeclinesToStart(t *testing.T) {
	for _, c := range []struct {
		name string
		d    time.Duration
		fn   func()
	}{
		{"non-positive interval", 0, func() {}},
		{"negative interval", -time.Second, func() {}},
		{"nil fn", time.Millisecond, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			app := everyTestApp(t)
			before := len(app.stops)
			stop := app.Every(c.d, c.fn)
			if stop == nil {
				t.Fatal("stop is nil — shutdown would panic calling it")
			}
			stop()
			stop()
			time.Sleep(2 * time.Millisecond)
			if n := app.disp.Pending(); n != 0 {
				t.Fatalf("%d posts from a clock that declined to start", n)
			}
			// gooey.Every declines these cases too, so "no posts" alone
			// passes with or without App.Every's own guard — it is the
			// shutdown list that tells them apart. A clock that never
			// started must not leave a no-op behind to be called at
			// teardown.
			if after := len(app.stops); after != before {
				t.Fatalf("the shutdown list grew %d→%d for a clock that declined to start", before, after)
			}
		})
	}
}
