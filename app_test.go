package gooey

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// These tests run a REAL App over a pty: real raw mode, real decoder,
// real frames flushed as escape sequences the test reads back from the
// master side. The only substitution is where the terminal comes from
// (WithTerminal), because /dev/tty is not available under `go test`.

// label is a minimal leaf that paints one bound property.
type label struct {
	Base
	text *prop.Property[string]
}

func (l *label) Measure(avail Size) Size { return avail }
func (l *label) Render(f *Frame) {
	b := l.Bounds()
	f.Cells.SetString(b.X, b.Y, l.text.Get(), render.Style{})
}

// eater consumes every key, so nothing bubbles to the app.
type eater struct {
	label
	FocusState
	got int
}

func (e *eater) HandleKey(input.KeyEvent) bool { e.got++; return true }

func newTestApp(t *testing.T, root Widget, opts ...Option) (*App, *testTTY) {
	t.Helper()
	tty := newTestTTY(t)
	opts = append([]Option{WithTerminal(tty.open)}, opts...)
	return NewApp(Tree(root), opts...), tty
}

// start runs the app and guarantees it is STOPPED before the test
// returns. Two apps must never run at once: the property graph tracks
// the evaluation in progress in package state, because it is confined to
// one goroutine by design — so a test that leaks a running loop corrupts
// whatever test runs next, not itself.
func start(t *testing.T, app *App) <-chan error {
	t.Helper()
	done := make(chan error, 1)
	// Closed as well as sent on, so both the test and the cleanup can
	// wait on it — a test that already took the error must not leave the
	// cleanup blocked on a channel nobody will send to again.
	go func() { done <- app.Run(context.Background()); close(done) }()
	t.Cleanup(func() {
		app.Quit()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("the run loop did not stop")
		}
	})
	return done
}

// The default quit key ends the run, and the terminal is left cooked,
// off the alternate screen, with mouse reporting off. Everything after
// "the app exited" depends on that last part.
func TestCtrlCQuitsAndRestoresTheTerminal(t *testing.T) {
	root := &label{text: prop.NewSource("hi")}
	app, tty := newTestApp(t, root)

	done := start(t, app)

	tty.waitForFrame(t)
	tty.send("\x03") // ctrl+c as a byte, which is what raw mode delivers

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned %v, want nil (a quit key is not a signal)", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("ctrl+c did not end the run loop")
	}
	// Waited for rather than snapshotted: Run has returned, so the bytes
	// are written, but the test's pty reader is a goroutine and may not
	// have picked them up yet.
	for _, want := range []string{"\x1b[?1049l", "\x1b[?25h", "\x1b[?1006l"} {
		if !tty.waitFor(t, want) {
			t.Errorf("teardown never emitted %q", want)
		}
	}
	if app.DecoderLeaked() {
		t.Error("the input decoder outlived the app")
	}
}

// The tree gets first refusal on every key. A widget that handles ctrl+c
// keeps it, exactly like a widget that handles arrows keeps them from
// moving focus.
func TestQuitKeyOnlyFiresWhenTheTreeDeclines(t *testing.T) {
	root := &eater{label: label{text: prop.NewSource("hi")}}
	app, tty := newTestApp(t, root)

	done := start(t, app)
	tty.waitForFrame(t)

	tty.send("\x03")
	select {
	case <-done:
		t.Fatal("ctrl+c quit the app even though the focused widget consumed it")
	case <-time.After(300 * time.Millisecond):
	}
	// The widget's counter belongs to the UI goroutine like any other
	// widget state, so it is read from there.
	seen := make(chan int, 1)
	app.Post(func() { seen <- root.got })
	if <-seen == 0 {
		t.Error("the widget never saw ctrl+c")
	}
}

// Frames are scheduled by the property graph: setting a property from a
// posted func must produce a frame without anyone asking for one.
func TestPropertyChangeSchedulesAFrame(t *testing.T) {
	text := prop.NewSource("first")
	app, tty := newTestApp(t, &label{text: text})

	start(t, app)
	tty.waitForFrame(t)

	tty.reset()
	app.Post(func() { text.Set("second") })
	if !tty.waitFor(t, "second") {
		t.Error("setting a bound property did not repaint")
	}
}

// BeforeFrame must run early enough that what it sets paints in the SAME
// frame. The old hand-written loops all relied on this (stats about the
// previous frame, set immediately before composing it); it is now a
// framework guarantee rather than a copied idiom.
func TestBeforeFrameFoldsIntoTheFrameItPrecedes(t *testing.T) {
	stats := prop.NewSource("")
	app, tty := newTestApp(t, &label{text: stats})

	frames := 0
	app.BeforeFrame(func() {
		frames++
		stats.Set("frame-" + itoa(frames))
	})
	start(t, app)

	if !tty.waitFor(t, "frame-1") {
		t.Fatal("the first frame did not include what BeforeFrame set for it")
	}
	// And it must SETTLE: a hook that dirties the tree every frame would
	// otherwise spin forever. Give it room to misbehave, then check.
	time.Sleep(200 * time.Millisecond)
	if frames > 3 {
		t.Errorf("the loop never settled: %d frames for one property change", frames)
	}
}

// Suspend is the terminal hand-off. While the func runs, the app holds
// no terminal at all and no goroutine of ours is reading the tty — that
// is what makes it safe to give to a child process.
func TestSuspendReleasesAndRetakesTheTerminal(t *testing.T) {
	text := prop.NewSource("before")
	app, tty := newTestApp(t, &label{text: text})

	start(t, app)
	tty.waitForFrame(t)

	ran := false
	errBoom := errors.New("child failed")
	var got error
	app.Post(func() {
		got = app.Suspend(func() error {
			ran = true
			if app.Screen() != nil {
				t.Error("the app still held a terminal inside Suspend")
			}
			return errBoom
		})
		text.Set("after")
	})
	if !tty.waitFor(t, "after") {
		t.Fatal("the UI did not come back after Suspend")
	}
	if !ran {
		t.Error("Suspend never ran its func")
	}
	if !errors.Is(got, errBoom) {
		t.Errorf("Suspend returned %v, want the func's error", got)
	}
	if app.Screen() == nil {
		t.Error("the app did not retake the terminal")
	}
	if app.DecoderLeaked() {
		t.Error("a decoder survived the suspend: the child would have been fighting it for keystrokes")
	}
	if n := tty.openCount(); n != 2 {
		t.Errorf("terminal acquired %d times, want 2 (startup + resume)", n)
	}
}

// A panic must reach the user on a terminal they can read: restored
// FIRST, then re-panicked with the original value.
func TestPanicRestoresTheTerminalBeforeRepanicking(t *testing.T) {
	app, tty := newTestApp(t, &exploder{})

	var recovered any
	releasedBeforePanic := false
	done := make(chan struct{})
	go func() {
		defer func() {
			recovered = recover()
			// Checked here, in the recover, because that is the moment the
			// panic reaches the outside world: the terminal must already
			// be given up. Reading the pty instead would race the drain —
			// the bytes are written before the re-panic, but a reader
			// goroutine may not have picked them up yet.
			releasedBeforePanic = app.Screen() == nil
			close(done)
		}()
		app.Run(context.Background())
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("the panicking app never returned")
	}
	if recovered != "render exploded" {
		t.Errorf("recovered %v, want the original panic value", recovered)
	}
	if !releasedBeforePanic {
		t.Error("the terminal was still held when the panic propagated")
	}
	if !tty.waitFor(t, "\x1b[?1049l") {
		t.Error("the panicking app never left the alternate screen")
	}
	if !tty.waitFor(t, "\x1b[?1006l") {
		t.Error("the panicking app left mouse tracking on")
	}
}

type exploder struct{ Base }

func (e *exploder) Measure(avail Size) Size { return avail }
func (e *exploder) Render(*Frame)           { panic("render exploded") }

// Cancelling the context ends the run the same way Quit does.
func TestContextCancellationEndsTheRun(t *testing.T) {
	app, tty := newTestApp(t, &label{text: prop.NewSource("hi")})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- app.Run(ctx) }()
	tty.waitForFrame(t)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run returned %v, want nil for a cancelled context", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancelling the context did not end the run")
	}
}

// Every is the app's own clock, marshalled onto the UI goroutine.
func TestEveryRunsOnTheUIGoroutine(t *testing.T) {
	n := prop.NewSource(0)
	app, tty := newTestApp(t, &label{text: prop.NewComputed(func() string { return "n=" + itoa(n.Get()) })})
	app.Every(20*time.Millisecond, func() { n.Set(n.Get() + 1) })

	start(t, app)
	if !tty.waitFor(t, "n=3") {
		t.Error("the interval never reached the UI")
	}
}

// Content.Watch reports a change; the App rebuilds on the UI goroutine
// and swaps. The hook fires for the initial attach as well, which is
// what lets an app resolve named widgets in one place.
func TestReloadRebuildsOnTheUIGoroutineAndSwaps(t *testing.T) {
	text := prop.NewSource("v1")
	c := &countingContent{text: text}
	tty := newTestTTY(t)
	app := NewApp(c, WithTerminal(tty.open))

	swaps := 0 // written only from the UI goroutine
	app.OnSwap(func(Widget) { swaps++ })

	start(t, app)
	tty.waitForFrame(t)

	tty.reset()
	// Everything that touches the graph goes through the loop, including
	// from a test: that is the confinement rule, and a test that broke it
	// would be testing a program nobody is allowed to write.
	app.Post(func() { text.Set("v2") })
	c.notify() // the watcher noticed a change
	if !tty.waitFor(t, "v2") {
		t.Fatal("the rebuilt tree never reached the screen")
	}

	checked := make(chan [2]int, 1)
	app.Post(func() { checked <- [2]int{swaps, c.builds} })
	got := <-checked
	if got[0] != 2 {
		t.Errorf("OnSwap fired %d times, want 2 (initial attach + reload)", got[0])
	}
	if got[1] != 2 {
		t.Errorf("Build ran %d times, want 2", got[1])
	}
}

// A build error leaves the running composition alone: a markup file is
// broken for the second between two saves, and dropping the UI for that
// makes hot reload unusable.
func TestFailedReloadKeepsTheRunningTree(t *testing.T) {
	text := prop.NewSource("good")
	c := &countingContent{text: text}
	tty := newTestTTY(t)
	errs := make(chan error, 4)
	app := NewApp(c, WithTerminal(tty.open), WithErrorHandler(func(e error) { errs <- e }))

	start(t, app)
	tty.waitForFrame(t)

	boom := errors.New("markup: unexpected EOF")
	app.Post(func() { c.fail = boom }) // the content is the loop's too
	c.notify()
	select {
	case err := <-errs:
		if !errors.Is(err, boom) {
			t.Errorf("reported %v, want the build error", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("a failed reload was never reported")
	}
	if app.Composer() == nil {
		t.Fatal("the composition was torn down by a failed reload")
	}
	tty.reset()
	app.Post(func() { text.Set("still here") })
	if !tty.waitFor(t, "still here") {
		t.Error("the surviving tree stopped repainting after a failed reload")
	}
}

// countingContent is a Content whose fields are UI-goroutine state, like
// any viewmodel: Build and the fault injection both run on the loop.
type countingContent struct {
	text    *prop.Property[string]
	changed func()
	fail    error
	builds  int
}

func (c *countingContent) Build() (Widget, error) {
	c.builds++
	if c.fail != nil {
		return nil, c.fail
	}
	return &label{text: c.text}, nil
}

func (c *countingContent) Watch(changed func()) func() {
	c.changed = changed
	return func() {}
}

// notify is the watcher goroutine's report that the source changed. It
// carries no tree — that is the whole contract.
func (c *countingContent) notify() {
	if c.changed != nil {
		c.changed()
	}
}
