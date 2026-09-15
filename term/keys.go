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

// PasteMarkerGrace names WHICH consecutive escape timeout resolves a
// buffer that looks like half a bracketed-paste marker — reading it as
// the Esc key the user actually pressed. The buffer is held through
// PasteMarkerGrace-1 timeouts and resolved ON the PasteMarkerGrace'th:
// the loop below increments `stalls` and escalates to drainFinal once it
// reaches this value, so at 2 the buffer survives exactly ONE timeout.
// (This comment used to say "how many timeouts it
// gets before the decoder gives up", which is one more than the loop
// grants; someone raising it to 3 to buy one extra grace period would
// have got two.) It is one number in one place because it is a trade
// with a loser either way, and both halves belong beside it.
//
// ESC [ 2 is a strict prefix of ESC [ 200 ~ AND three keys a person can
// type. input.Decode holds it rather than resolving it, on the reasoning
// that the rest of a real marker is already on the wire — true for a
// paste, false for the typing, and in the typing case the hold is
// permanent: the Esc is never delivered, the next keystroke is absorbed
// into the CSI parse, and the loop below re-arms its timer every 40ms
// for the rest of the process's life (#440).
//
// TWO, so the buffer survives one timeout and the window is 80ms. Lower
// is not available — at one the FIRST timeout resolves, which is what
// "idle" already means, and the grace would not exist. Higher buys a
// paste marker split across a slower link at the price of the Esc key
// taking that long to arrive, and of the deaf window being that much
// wider if something new lands in this shape. What is NOT on this scale
// is an open paste (marker complete, payload's end not yet arrived):
// that waits forever by design, because delivering it early truncates
// the paste silently. See input.DecodeFinal.
const PasteMarkerGrace = 2

// drainDeadline is how much of the terminal's benefit of the doubt is
// left when the decoder is asked to empty its buffer. It mirrors input's
// own unexported `deadline` because it is answering the same question,
// and it is ONE value rather than two bools so that "more bytes may
// arrive, and also nothing is ever coming" cannot be written.
type drainDeadline uint8

const (
	drainLive drainDeadline = iota // bytes may still be arriving
	drainIdle                      // none arrived within EscTimeout

	// drainFinal is NOT "the stall count reached PasteMarkerGrace" — it
	// is "nothing more can arrive", and the two are different in a way
	// an earlier wording got wrong. The stall path reaches it after
	// PasteMarkerGrace-1 fruitless timeouts, ON the PasteMarkerGrace'th;
	// the TTY-CLOSE path reaches it with ZERO timeouts elapsed, because
	// a closed tty is a stronger guarantee than any deadline. A
	// precondition written as "only after N timeouts" is false for the
	// second caller and would send the next reader hunting a bug there.
	// Raised in review of #445.
	drainFinal
)

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
	// drain empties pend as far as the decoder will take it, under one of
	// three deadlines. ONE parameter and not two bools: `idle` and
	// `final` were independent, which made `drain(false, true)` — "more
	// bytes may still arrive, and also nothing is ever coming" —
	// representable and meaningless. Raised in review of #445; input's
	// own `deadline` is the same three values for the same reason.
	drain := func(d drainDeadline) {
		for len(pend) > 0 {
			var (
				ev input.Event
				n  int
				ok bool
			)
			if d == drainFinal {
				ev, n, ok = input.DecodeFinal(pend)
			} else {
				ev, n, ok = input.Decode(pend, d == drainIdle)
			}
			if n == 0 && !ok {
				// Incomplete: wait for more bytes. Safe to return only
				// because under drainIdle the decoder answers this way
				// for exactly TWO inputs, both bracketed pastes: a split
				// MARKER, and an OPEN paste whose end has not arrived.
				// For anything else pend would strand here forever and
				// the app would go permanently deaf while still
				// painting.
				//
				// Under drainFinal that shrinks to the open paste alone,
				// whose wedge is deliberate. The split marker used to be
				// justified by "the terminal is mid-write, so the next
				// byte resolves it either way" — false when a person
				// typed those bytes, which is #440 and the reason
				// drainFinal exists. See input.Decode and
				// input.DecodeFinal.
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
	// stalls counts CONSECUTIVE escape timeouts that left pend exactly as
	// they found it. Any byte read, and any byte consumed, is progress and
	// resets it — so the count only ever grows while the terminal is
	// saying nothing and the decoder can do nothing with what it has.
	stalls := 0
	for {
		drain(drainLive)
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		// Re-arm only while a timeout could still change something. Once
		// the last-chance pass has run and pend survived it, nothing but
		// a new byte can, and re-arming on a buffer no deadline can move
		// is the 40ms-forever wakeup #440 reported.
		if len(pend) > 0 && stalls < PasteMarkerGrace {
			timer.Reset(EscTimeout)
		}
		select {
		case c, ok := <-chunks:
			if !ok {
				// The tty is gone: no byte is coming, ever. That is the
				// strongest form of the deadline there is, and it is
				// reached with ZERO timeouts elapsed — which is why the
				// last-chance pass is defined by "nothing more can
				// arrive" rather than by a stall count.
				drain(drainFinal)
				return
			}
			pend = append(pend, c...)
			stalls = 0
		case <-timer.C:
			stalls++
			before := len(pend)
			d := drainIdle
			if stalls >= PasteMarkerGrace {
				d = drainFinal
			}
			drain(d)
			if len(pend) != before {
				stalls = 0
			}
		}
	}
}
