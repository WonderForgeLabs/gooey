package term

import (
	"time"

	"github.com/WonderForgeLabs/gooey/input"
)

// EscTimeout is how long a dangling ESC waits for the rest of a possible
// escape sequence before it is reported as the Esc key.
const EscTimeout = 40 * time.Millisecond

// DecodeEvents reads the tty and sends decoded events — keys and, if
// mouse reporting is on, pointer reports — on out until the tty closes.
// It runs in its own goroutine; the decoding itself lives in the input
// package (pure, testable), so this function is only I/O and the
// escape-timeout policy.
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
				return
			}
		}
	}()

	var pend []byte
	drain := func(idle bool) {
		for len(pend) > 0 {
			ev, n, ok := input.Decode(pend, idle)
			if n == 0 && !ok {
				return // incomplete: wait for more bytes
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
