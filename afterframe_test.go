package gooey

import (
	"testing"
	"time"

	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/prop"
)

// The control-plane seams: AfterFrame observes exactly the frame that
// was just composed (damage rects and count included), AfterEvent
// reports how routing went, and Done reports the end of the run. These
// are what a streaming control session (grpc/) hangs its FrameDelta,
// InputEcho and Closing channels on, so their timing is contract, not
// convenience.

func TestAfterFrameSeesTheFrameJustComposed(t *testing.T) {
	text := prop.NewSource("first")
	root := &label{text: text}
	app, tty := newTestApp(t, root)

	type frameInfo struct {
		painted int
		damage  []Rect
	}
	frames := make(chan frameInfo, 16)
	app.AfterFrame(func() {
		// Runs on the UI goroutine right after compose+flush: Damage()
		// still describes the frame that just went out.
		frames <- frameInfo{painted: app.PaintedLastFrame(), damage: app.Composer().Damage()}
	})
	start(t, app)
	tty.waitForFrame(t)

	// The first frame paints the one component.
	select {
	case fi := <-frames:
		if fi.painted != 1 || len(fi.damage) != 1 {
			t.Fatalf("first frame: painted=%d damage=%v, want exactly the label", fi.painted, fi.damage)
		}
		if fi.damage[0] != root.Bounds() {
			t.Errorf("damage rect %v != component bounds %v", fi.damage[0], root.Bounds())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("AfterFrame never ran")
	}

	// One property change: one more frame, one repainted component, and
	// the hook heard about exactly that — the damage-count guarantee
	// observable from the seam.
	app.Post(func() { text.Set("second") })
	select {
	case fi := <-frames:
		if fi.painted != 1 || len(fi.damage) != 1 {
			t.Errorf("change frame: painted=%d damage=%v, want 1/1", fi.painted, fi.damage)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the change's frame never reached AfterFrame")
	}
}

func TestAfterEventReportsConsumption(t *testing.T) {
	root := &eater{label: label{text: prop.NewSource("hi")}}
	app, tty := newTestApp(t, root, WithQuitKeys()) // no quit key: bare keys route

	type routed struct {
		ev       input.Event
		consumed bool
	}
	events := make(chan routed, 16)
	app.AfterEvent(func(ev input.Event, consumed bool) {
		events <- routed{ev, consumed}
	})
	start(t, app)
	tty.waitForFrame(t)

	tty.send("x")
	select {
	case r := <-events:
		if !r.ev.IsKey() || r.ev.Key.Rune != 'x' || !r.consumed {
			t.Errorf("routed = %+v, want a consumed 'x'", r)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("AfterEvent never saw the keystroke")
	}
}

func TestDoneClosesWhenTheAppQuits(t *testing.T) {
	app, tty := newTestApp(t, &label{text: prop.NewSource("hi")})
	done := start(t, app)
	tty.waitForFrame(t)

	select {
	case <-app.Done():
		t.Fatal("Done closed while the app was running")
	default:
	}
	app.Quit()
	select {
	case <-app.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("Done did not close on Quit")
	}
	<-done
}
