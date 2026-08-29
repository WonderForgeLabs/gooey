package term

import (
	"errors"
	"os"
	"syscall"
	"time"

	"github.com/WonderForgeLabs/gooey/input"
)

// EscTimeout is how long a dangling ESC waits for the rest of a possible
// escape sequence before it is reported as the Esc key.
const EscTimeout = 40 * time.Millisecond

// recoverable reports whether a tty read error is one the decoder should
// retry rather than treat as the end of terminal input.
//
// The distinction is the whole point: this loop used to return on ANY
// error, and because nothing closes the events channel and nothing
// watched the decoder, a single transient read error left the app
// running, painting, and permanently deaf — no log, no exit, no
// tripwire. DecoderLeaked guards the opposite failure (a decoder that
// OUTLIVES teardown); this is the one where it dies too early.
//
// EINTR is a signal arriving mid-read — SIGWINCH on every window resize,
// among others — and is not a terminal condition. ErrDeadlineExceeded is
// a deadline set by something else on this fd (Detect uses one) expiring
// under the decoder; also not a terminal condition.
//
// EAGAIN is deliberately NOT here. On a pollable fd the runtime retries
// it internally so it never surfaces; if it does surface, the fd has left
// the netpoller (see Screen.control on why that must never happen) and
// retrying would spin at 100% CPU forever. Reporting it and stopping is
// worse for that one case and far better for every other: a loud failure
// beats a silent one, which is the lesson this whole function encodes.
func recoverable(err error) bool {
	return errors.Is(err, syscall.EINTR) || errors.Is(err, os.ErrDeadlineExceeded)
}

// DecodeEvents reads the tty and sends decoded events — keys and, if
// mouse reporting is on, pointer reports — on out until the tty closes.
// It runs in its own goroutine; the decoding itself lives in the input
// package (pure, testable), so this function is only I/O and the
// escape-timeout policy.
//
// This is the primitive. Prefer Screen.Events, which starts it and hands
// the Screen ownership of the goroutine, because ownership is what lets
// Restore prove it died — and a terminal handed to a child process while
// one of our readers is still on it is the bug this whole lifecycle
// exists to prevent. A decoder started here by hand is nobody's, and
// teardown will not wait for it.
func DecodeEvents(s *Screen, out chan<- input.Event) {
	chunks := make(chan []byte, 8)
	go func() {
		defer close(chunks)
		for {
			buf := make([]byte, 128)
			n, err := s.File().Read(buf)
			if n > 0 {
				chunks <- buf[:n]
			}
			if err != nil {
				if recoverable(err) {
					continue
				}
				// Record BEFORE closing chunks. That ordering is what
				// makes the field safe to read without a lock: the
				// write happens-before this close, which happens-before
				// DecodeEvents returns, which happens-before the close
				// of decDone that a reader must observe first.
				s.decErr = err
				return
			}
		}
	}()

	var pend []byte
	drain := func(idle bool) {
		for len(pend) > 0 {
			ev, n, ok := input.Decode(pend, idle)
			if n == 0 && !ok {
				// Incomplete: wait for more bytes. Safe to return only
				// because input.Decode answers this way under idle for
				// ONE input — an incomplete bracketed paste, where the
				// terminal is mid-write and the next byte resolves it.
				// See input.Decode's doc comment. For anything else,
				// pend would strand here forever and the app would go
				// permanently deaf while still painting.
				return
			}
			pend = pend[n:]
			if ok {
				out <- ev
			}
		}
	}
	timer := time.NewTimer(EscTimeout)
	defer timer.Stop()
	for {
		drain(false)
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		if len(pend) > 0 {
			timer.Reset(EscTimeout)
		}
		select {
		case c, ok := <-chunks:
			if !ok {
				drain(true)
				return
			}
			pend = append(pend, c...)
		case <-timer.C:
			drain(true)
		}
	}
}
