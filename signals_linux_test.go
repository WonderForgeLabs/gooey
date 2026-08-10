package gooey

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/WonderForgeLabs/gooey/prop"
)

// These tests send REAL signals to the test process. That is safe only
// because the App has registered handlers for them by the time they
// arrive — an unhandled SIGINT or SIGTSTP would kill or freeze the test
// binary — so each one waits for a frame first, which proves Run got
// past startSignals.
//
// guard keeps the disposition non-default for the whole test even after
// the app has torn its handlers down, so a late or duplicate delivery
// cannot kill the run.
func guard(t *testing.T, sigs ...os.Signal) {
	t.Helper()
	ch := make(chan os.Signal, 8)
	signal.Notify(ch, sigs...)
	t.Cleanup(func() { signal.Stop(ch) })
}

// SIGINT restores the terminal, runs the shutdown hook, and reports
// itself: a program killed by a signal must not exit 0.
func TestSIGINTRestoresRunsShutdownAndReports(t *testing.T) {
	guard(t, syscall.SIGINT)
	tty := newTestTTY(t)
	shutdownRan := make(chan struct{})
	app := NewApp(Tree(&label{text: prop.NewSource("hi")}),
		WithTerminal(tty.open),
		WithShutdown(func(context.Context) error {
			close(shutdownRan)
			return nil
		}, time.Second))

	done := start(t, app)
	tty.waitForFrame(t)
	if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-done:
		var se *SignalError
		if !errors.As(err, &se) {
			t.Fatalf("Run returned %v, want a *SignalError", err)
		}
		if se.Signal != syscall.SIGINT {
			t.Errorf("reported %v, want SIGINT", se.Signal)
		}
		if se.ExitCode() != 130 {
			t.Errorf("exit code %d, want 130 (128+SIGINT)", se.ExitCode())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("SIGINT did not end the run")
	}
	select {
	case <-shutdownRan:
	default:
		t.Error("the shutdown hook never ran")
	}
	if !strings.Contains(tty.text(), "\x1b[?1049l") {
		t.Error("SIGINT left the terminal on the alternate screen")
	}
}

// A shutdown hook that will not finish must not hold the app hostage.
func TestShutdownHookIsBounded(t *testing.T) {
	guard(t, syscall.SIGTERM)
	tty := newTestTTY(t)
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	app := NewApp(Tree(&label{text: prop.NewSource("hi")}),
		WithTerminal(tty.open),
		WithShutdown(func(ctx context.Context) error {
			<-release // never, within the test's lifetime
			return nil
		}, 200*time.Millisecond))

	done := start(t, app)
	tty.waitForFrame(t)
	syscall.Kill(os.Getpid(), syscall.SIGTERM)
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("a hanging shutdown hook blocked the exit past its timeout")
	}
}

// SIGWINCH is the capability this adds rather than the safety: before
// it, a gooey app's size was fixed at construction.
func TestSIGWINCHResizesTheComposition(t *testing.T) {
	guard(t, syscall.SIGWINCH)
	text := prop.NewSource("resize me")
	app, tty := newTestApp(t, &label{text: text})

	start(t, app)
	tty.waitForFrame(t)

	before := make(chan [2]int, 1)
	app.Post(func() { c, r := app.Size(); before <- [2]int{c, r} })
	if got := <-before; got != [2]int{40, 10} {
		t.Fatalf("initial size %v, want 40x10", got)
	}

	tty.reset()
	tty.setSize(t, 60, 20)
	syscall.Kill(os.Getpid(), syscall.SIGWINCH)

	deadline := time.Now().Add(3 * time.Second)
	var got [2]int
	for time.Now().Before(deadline) {
		ch := make(chan [2]int, 1)
		app.Post(func() { c, r := app.Size(); ch <- [2]int{c, r} })
		if got = <-ch; got == [2]int{60, 20} {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got != [2]int{60, 20} {
		t.Fatalf("size after SIGWINCH is %v, want 60x20", got)
	}
	if !tty.waitFor(t, "resize me") {
		t.Error("the resized composition did not repaint")
	}
	// A frame at the new size: rows-1 line breaks, columns wide.
	frames := strings.Split(tty.text(), "\x1b[?2026h")
	last := frames[len(frames)-1]
	if n := strings.Count(last, "\r\n"); n != 19 {
		t.Errorf("the frame has %d line breaks, want 19 for a 20-row terminal", n)
	}
}

// ctrl+z where the process CANNOT be stopped — an orphaned process
// group, which is what a `go test` binary is, and equally what a
// nohup'd or supervised program is.
//
// The naive dance loops here: the re-raised SIGTSTP does not stop
// anything and is handed back the instant the handler re-registers, so
// the app restores and re-acquires the terminal over and over. The
// contract is to decline instead — keep running, keep the terminal, and
// paint as if nothing happened.
//
// The stopping path itself needs a controlling terminal and a shell to
// continue the process, so it is verified end to end under a job-control
// pty rather than here.
func TestSIGTSTPIsDeclinedWhenTheProcessCannotBeStopped(t *testing.T) {
	guard(t, syscall.SIGTSTP, syscall.SIGCONT)
	text := prop.NewSource("before-stop")
	app, tty := newTestApp(t, &label{text: text})

	start(t, app)
	tty.waitForFrame(t)

	fg := make(chan bool, 1)
	app.Post(func() { fg <- app.Screen().InForeground() })
	if <-fg {
		t.Skip("this test process owns its terminal, so ctrl+z would really stop it")
	}

	syscall.Kill(os.Getpid(), syscall.SIGTSTP)
	time.Sleep(500 * time.Millisecond) // time for a loop to run away

	if n := tty.openCount(); n != 1 {
		t.Errorf("the terminal was re-acquired %d times: ctrl+z looped instead of being declined", n)
	}
	if app.Screen() == nil {
		t.Error("the app gave up the terminal for a stop that could not happen")
	}
	tty.reset()
	app.Post(func() { text.Set("still-running") })
	if !tty.waitFor(t, "still-running") {
		t.Error("the app stopped painting after a declined ctrl+z")
	}
}
