package term

import (
	"os"
	"testing"
	"time"

	"github.com/WonderForgeLabs/gooey/input"
)

// The invariant this package now guarantees:
//
//	no Screen teardown leaves a goroutine reading the terminal.
//
// It held for nothing before: the ioctls behind Raw and Size went
// through os.File.Fd(), which puts the file back in blocking mode and
// unregisters it from the runtime poller, and a Read pending on such a
// file cannot be cancelled by Close. Every decoder outlived its Screen.

// TestRestoreJoinsDecoder is the invariant itself, on a plain pipe so it
// runs anywhere: a decoder is started, genuinely blocked in Read, and
// Restore must not return until it is dead.
func TestRestoreJoinsDecoder(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	s := FromFile(r)
	evs := s.Events(4)

	// Prove the reader is actually parked in Read before tearing down —
	// a join that races startup would pass without testing anything.
	w.Write([]byte("x"))
	select {
	case ev := <-evs:
		if !ev.IsKey() || ev.Key.Rune != 'x' {
			t.Fatalf("decoder delivered %+v, want rune x", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("decoder never delivered the first byte")
	}
	time.Sleep(50 * time.Millisecond) // back in a blocking Read

	done := make(chan struct{})
	go func() { s.Restore(); close(done) }()
	select {
	case <-done:
	case <-time.After(DecoderTimeout + time.Second):
		t.Fatal("Restore never returned: the decoder outlived the Screen")
	}
	if s.DecoderLeaked() {
		t.Error("Restore timed out joining the decoder")
	}
}

// A decoder blocked on SENDING an event must not wedge teardown either.
// This is the case the demo browser hit: the reader flushes what it had
// buffered on the way out, and nobody is left to receive it.
func TestRestoreJoinsDecoderWithUnreadEvents(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	s := FromFile(r)
	s.Events(1) // buffer of one, deliberately never read from
	w.Write([]byte("abcdefgh"))
	time.Sleep(50 * time.Millisecond)

	done := make(chan struct{})
	go func() { s.Restore(); close(done) }()
	select {
	case <-done:
	case <-time.After(DecoderTimeout + time.Second):
		t.Fatal("Restore wedged on a decoder blocked sending events")
	}
	if s.DecoderLeaked() {
		t.Error("decoder leaked: teardown did not drain the event channel")
	}
}

// Events returns one channel per Screen: two decoders on one tty would
// split the keystrokes between them.
func TestEventsIsSingular(t *testing.T) {
	r, w, _ := os.Pipe()
	defer w.Close()
	s := FromFile(r)
	if a, b := s.Events(4), s.Events(4); a != b {
		t.Error("Events started a second decoder on the same tty")
	}
	s.Restore()
}

// The same thing on a REAL terminal, with the raw-mode and size ioctls
// that used to detach the fd actually called first. This is the A/B from
// docs/specs/2026-08-10-tty-read-lifecycle.md, now expected to pass.
func TestRestoreJoinsDecoderOnTTYAfterIoctls(t *testing.T) {
	master, slave := openPTY(t)
	s := FromFile(slave)

	if err := s.Raw(); err != nil { // MakeRaw ioctl — the old detacher
		t.Fatal(err)
	}
	if cols, rows := s.Size(); cols <= 0 || rows <= 0 { // GetSize ioctl
		t.Fatalf("Size() = %d,%d", cols, rows)
	}
	evs := s.Events(4)
	master.Write([]byte("x"))
	select {
	case <-evs:
	case <-time.After(time.Second):
		t.Fatal("no event from the pty")
	}
	time.Sleep(100 * time.Millisecond) // blocked in Read on a real tty

	done := make(chan struct{})
	go func() { s.Restore(); close(done) }()
	select {
	case <-done:
	case <-time.After(DecoderTimeout + time.Second):
		t.Fatal("Restore never returned on a tty whose fd went through the raw/size ioctls")
	}
	if s.DecoderLeaked() {
		t.Error("decoder survived teardown on a real tty")
	}
}

// The negative half of the A/B, kept because it is the entire reason the
// package routes ioctls through SyscallConn.
//
// ORDER IS THE WHOLE POINT, and getting it wrong is how this test first
// lied: Fd() must be called BEFORE the read starts. A read submitted
// while the file is still pollable is parked in the netpoller and Close
// wakes it even if Fd() is called afterwards — which is why the bug
// looked intermittent. Detach first and the read is an ordinary blocking
// syscall in the kernel, with nothing left that can cancel it.
//
// It deliberately abandons one goroutine (that IS the bug), so it runs
// on its own pty pair and touches nothing else.
func TestFdDetachesTheFileFromThePoller(t *testing.T) {
	master, slave := openPTY(t)
	_ = master

	slave.Fd() // the detach, before any read

	unblocked := make(chan error, 1)
	go func() {
		buf := make([]byte, 8)
		_, err := slave.Read(buf)
		unblocked <- err
	}()
	time.Sleep(100 * time.Millisecond) // genuinely blocked

	slave.Close()

	select {
	case err := <-unblocked:
		t.Fatalf("Close cancelled a read on a detached file (err=%v): the premise of "+
			"docs/specs/2026-08-10-tty-read-lifecycle.md no longer holds and the "+
			"SyscallConn routing can be reconsidered", err)
	case <-time.After(300 * time.Millisecond):
		// Expected: still blocked. The goroutine is abandoned; it dies
		// with the test binary.
	}
}

// And the same sequence through Screen, which must NOT reproduce it:
// Raw and Size come before the decoder starts, exactly as an app does
// it, and teardown still joins.
func TestScreenIoctlsBeforeDecoderStillJoin(t *testing.T) {
	master, slave := openPTY(t)
	s := FromFile(slave)
	if err := s.Raw(); err != nil {
		t.Fatal(err)
	}
	s.Size()
	evs := s.Events(4)
	master.Write([]byte("x"))
	select {
	case <-evs:
	case <-time.After(time.Second):
		t.Fatal("no event from the pty")
	}
	time.Sleep(100 * time.Millisecond)

	done := make(chan struct{})
	go func() { s.Restore(); close(done) }()
	select {
	case <-done:
	case <-time.After(DecoderTimeout + time.Second):
		t.Fatal("Restore never returned")
	}
	if s.DecoderLeaked() {
		t.Error("decoder leaked despite the ioctls running through SyscallConn")
	}
}

// Detect's read deadline only works on a file that is still registered
// with the poller. Before the fix it always failed with ErrNoDeadline —
// silently downgrading the probe to one blocking read — because Size()
// had already called Fd(). A terminal that answers nothing must return
// within the timeout rather than hang.
func TestDetectHonorsItsTimeoutOnASilentTerminal(t *testing.T) {
	_, slave := openPTY(t)
	s := FromFile(slave)

	done := make(chan Caps, 1)
	go func() {
		caps, _ := s.Detect()
		done <- caps
	}()
	select {
	case caps := <-done:
		if caps.Cols <= 0 {
			t.Errorf("Detect returned no size: %+v", caps)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Detect hung on a terminal that answers nothing: the read deadline is not in effect")
	}
	s.Restore()
}

var _ input.Event // the decoder's payload type; see Screen.Events
