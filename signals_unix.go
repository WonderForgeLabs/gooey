//go:build unix

package gooey

import (
	"os"
	"os/signal"
	"syscall"
)

// The console signal story lives here. The full rationale, including the
// ctrl+c-as-byte versus SIGINT distinction, is
// docs/specs/2026-08-10-runtime-signals.md; this file is that document
// executed.
//
// Every signal is delivered onto the UI goroutine through the Dispatcher
// rather than handled where it lands. A signal handler goroutine that
// touched the composition would be a property access from the wrong
// goroutine — the one rule the framework will not bend — and the
// terminal work these do (restore, re-raw, repaint) has to be ordered
// against frames, not raced with them.

type signalHandle struct {
	ch   chan os.Signal
	done chan struct{}
}

func (a *App) startSignals() {
	ch := make(chan os.Signal, 8)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM, syscall.SIGWINCH, syscall.SIGTSTP)
	done := make(chan struct{})
	a.sig = signalHandle{ch: ch, done: done}
	go func() {
		for {
			select {
			case <-done:
				return
			case s, ok := <-ch:
				if !ok {
					return
				}
				sig := s
				a.disp.Post(func() { a.onSignal(sig) })
			}
		}
	}()
}

func (a *App) stopSignals() {
	if a.sig.ch == nil {
		return
	}
	signal.Stop(a.sig.ch)
	close(a.sig.done)
	a.sig = signalHandle{}
}

func (a *App) onSignal(sig os.Signal) {
	// Suspended: a child process owns the terminal and every signal the
	// tty driver sends goes to the whole foreground process group, so
	// what arrives here was meant for it. See App.Suspend.
	if a.suspended {
		return
	}
	switch sig {
	case syscall.SIGINT, syscall.SIGTERM:
		a.gracefulExit(sig)
	case syscall.SIGWINCH:
		a.onResize()
	case syscall.SIGTSTP:
		a.onStop()
	}
}

// onResize re-queries the terminal and re-targets the composition.
//
// SIGWINCH is the signal that adds capability rather than safety: before
// it, a gooey app's size was whatever it was at construction, and a
// resized window left the UI painting into a buffer of the wrong shape.
// The work is entirely Composer.Resize — new buffer, everything dirty —
// because layout already runs every frame and will re-measure the tree
// against the new bounds without being asked.
func (a *App) onResize() {
	if a.screen == nil {
		return
	}
	cols, rows := a.screen.Size()
	a.resized(cols, rows)
	a.needsFrame = true
}

// onStop is the classic ctrl+z dance, and every step of it is required.
//
// A TUI cannot simply be stopped: the shell would come back to a
// terminal in raw mode, on the alternate screen, with mouse tracking on
// and a decoder goroutine still parked on the tty. So we restore the
// terminal completely (which joins the decoder), put SIGTSTP back to its
// default disposition, and re-raise it at ourselves — the process
// actually stops there, inside this call.
//
// Execution resumes on SIGCONT, at the line after the Kill. That is why
// SIGCONT needs no handler of its own: being scheduled again IS the
// notification. We re-register SIGTSTP, retake the terminal, and force a
// full repaint, picking up any resize that happened while stopped.
func (a *App) onStop() {
	// Not every process CAN be stopped, and attempting it where it is not
	// honored is worse than declining: the raise returns, the signal stays
	// pending, and re-registering hands it straight back — a
	// restore/re-acquire loop with the UI flickering through it. See
	// term.Screen.CanSuspend for the two configurations that fail.
	if a.screen == nil || !a.screen.CanSuspend() {
		return
	}
	a.release()
	signal.Reset(syscall.SIGTSTP)
	_ = syscall.Kill(syscall.Getpid(), syscall.SIGTSTP)

	// --- stopped; resumed here by SIGCONT ---

	// Discard before re-arming. If the raise above was NOT honored after
	// all, the signal is still pending and would be delivered the instant
	// this handler re-registers, putting us right back here; setting the
	// disposition to ignore makes the kernel drop it. The window between
	// the two calls can lose a ctrl+z pressed in that exact microsecond,
	// which is a better failure than an infinite loop.
	signal.Ignore(syscall.SIGTSTP)
	if a.sig.ch != nil {
		signal.Notify(a.sig.ch, syscall.SIGTSTP)
	}
	if err := a.acquire(); err != nil {
		a.fail(err)
		a.Quit()
		return
	}
	a.needsFrame = true
}

func signalNumber(sig os.Signal) int {
	if s, ok := sig.(syscall.Signal); ok {
		return int(s)
	}
	return 0
}
