//go:build !unix

package gooey

import (
	"os"
	"os/signal"
)

// The non-Unix build keeps the run loop identical and the signal story
// reduced to what the platform actually has: interrupt ends the app.
// SIGWINCH, SIGTSTP and SIGCONT have no equivalent here, so resize is
// not signal-driven and there is no suspend-to-shell.

type signalHandle struct {
	ch   chan os.Signal
	done chan struct{}
}

func (a *App) startSignals() {
	ch := make(chan os.Signal, 4)
	signal.Notify(ch, os.Interrupt)
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
				a.disp.Post(func() { a.gracefulExit(sig) })
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

func signalNumber(os.Signal) int { return 2 }
