package term

import (
	"errors"
	"os"
	"syscall"
	"testing"
	"time"
)

// The terminal dying under a running decoder must be OBSERVABLE.
//
// This is the dual of the invariant Restore guarantees. That one asks
// "did the decoder outlive the terminal?" and DecoderLeaked answers it.
// This asks "did the terminal outlive the decoder?", and until
// DecoderDone existed nothing answered it at all: DecodeEvents returns,
// nobody closes the events channel, and a run loop selecting on that
// channel blocks on it for the life of the process. The app stays up,
// paints every frame correctly, and ignores every key.
//
// That is not hypothetical — it is what a live wysiwyg session looked
// like from the outside: display updating, MCP-injected events
// dispatching normally, keyboard completely dead, 0.8% CPU. The low CPU
// was the tell. A CLOSED events channel would have made the run loop
// spin hot; a silent one just parks it.
func TestDecoderDeathIsObservable(t *testing.T) {
	master, slave := openPTY(t)
	s := FromFile(slave)
	// Raw mode, because that is the state a real app's decoder reads in:
	// under the default line discipline a byte is not delivered until a
	// newline, and the liveness check below would fail for that reason
	// rather than for the one it is testing.
	if err := s.Raw(); err != nil {
		t.Fatalf("raw: %v", err)
	}
	evs := s.Events(16)

	// Prove the decoder is genuinely running first. Without this the
	// test would pass against a decoder that never started, which is
	// the same "passes by checking nothing" failure it exists to catch.
	if _, err := master.Write([]byte("a")); err != nil {
		t.Fatalf("write to master: %v", err)
	}
	select {
	case ev := <-evs:
		if !ev.IsKey() || ev.Key.Rune != 'a' {
			t.Fatalf("got %#v, want the 'a' we typed", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the decoder never delivered a keystroke, so this test is " +
			"not measuring a decoder that died — it is measuring one that " +
			"never lived")
	}

	// Now kill the terminal the way a closing emulator does: drop the
	// master. Reads on the slave fail from here on.
	master.Close()

	select {
	case <-s.DecoderDone():
	case <-time.After(2 * time.Second):
		t.Fatal("the decoder did not exit after the pty master closed")
	}

	if err := s.DecoderErr(); err == nil {
		t.Error("DecoderErr is nil after the terminal failed under a running " +
			"decoder. A caller cannot tell this apart from a clean teardown, " +
			"which is exactly how an app ends up alive, painting and deaf.")
	}

	// The events channel staying OPEN is the reason this needed its own
	// signal, and pinning it here keeps the next person from "simplifying"
	// DecodeEvents by closing out — which would turn a deaf app into a
	// hot-spinning one, since a receive on a closed channel is always
	// ready. Neither is acceptable; the fix is to watch DecoderDone.
	select {
	case _, ok := <-evs:
		if !ok {
			t.Error("the events channel was closed on decoder exit. Nothing " +
				"in the run loop distinguishes that from an event, so it " +
				"would select this case forever at 100% CPU.")
		}
	default:
	}
}

// A read error the decoder can recover from must not end terminal input.
//
// EINTR is the one that matters in practice: every SIGWINCH -- every
// window resize -- can interrupt a pending read. Before recoverable(),
// ANY error returned from the loop, so a single resize at the wrong
// moment was enough to deafen the app permanently.
//
// Driving a real EINTR from a test is racy, so this checks the predicate
// that decides it. The predicate is the whole behaviour: the read loop's
// only branch is `if recoverable(err) { continue }`.
func TestRecoverableErrorsDoNotEndInput(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"EINTR: a signal arrived mid-read, e.g. SIGWINCH", syscall.EINTR, true},
		{"deadline: someone else's timeout on this fd", os.ErrDeadlineExceeded, true},
		{"wrapped EINTR still recovers", &os.PathError{Err: syscall.EINTR}, true},
		{"EIO: the terminal is gone", syscall.EIO, false},
		{"EBADF: teardown closed the tty", syscall.EBADF, false},
		// Deliberately fatal. On a pollable fd the runtime retries
		// EAGAIN so it never surfaces; if it does, the fd has left the
		// netpoller and retrying would spin at 100% CPU forever.
		{"EAGAIN: reported, not spun on", syscall.EAGAIN, false},
	} {
		if got := recoverable(tc.err); got != tc.want {
			t.Errorf("recoverable(%v) = %v, want %v — %s", tc.err, got, tc.want, tc.name)
		}
	}
}

// A clean teardown must NOT look like a failure, or the tripwire above
// fires on every normal exit and gets ignored — the way a stale
// known-bad list gets ignored, and for the same reason.
func TestCleanTeardownReportsNoDecoderError(t *testing.T) {
	_, slave := openPTY(t)
	s := FromFile(slave)
	s.Events(16)
	s.Restore()

	if s.DecoderLeaked() {
		t.Fatal("Restore did not join the decoder")
	}
	if err := s.DecoderErr(); err != nil && !errors.Is(err, os.ErrClosed) {
		t.Errorf("DecoderErr = %v after a clean Restore; a normal teardown "+
			"must not be reported as a terminal failure", err)
	}
}
