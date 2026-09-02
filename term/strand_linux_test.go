package term

import (
	"testing"
	"time"

	"github.com/WonderForgeLabs/gooey/input"
)

// The decoder going deaf WITHOUT dying — the failure DecoderDone cannot
// see, and the dual of TestDecoderDeathIsObservable next door.
//
// That test's comment describes a live wysiwyg session as "display
// updating, MCP-injected events dispatching normally, keyboard
// completely dead", and fixes the cause it found: a decoder that
// returned. This is the same symptom reached the other way. The decoder
// is alive, its goroutine is parked on the tty exactly as it should be,
// DecoderDone never fires, and the app still never sees another key —
// because DecodeEvents' drain loop stops on a Decode that consumes
// nothing, and the buffer it stopped on can never be resolved by another
// byte. Every keystroke after it queues behind it forever.
//
// Testing it HERE rather than in input is the whole point. input's own
// test pins the decoding contract; only the loop can show that violating
// it strands live input, and only a real tty makes the loop the thing
// under test.
func TestEscBeforeAMouseReportDoesNotStrandTheDecoder(t *testing.T) {
	master, slave := openPTY(t)
	s := FromFile(slave)
	if err := s.Raw(); err != nil {
		t.Fatalf("raw: %v", err)
	}
	evs := s.Events(16)

	// Prove the decoder lives before asking whether it went deaf, for
	// the same reason the death test does: a decoder that never started
	// would pass every assertion below by never contradicting one.
	if _, err := master.Write([]byte("a")); err != nil {
		t.Fatalf("write to master: %v", err)
	}
	if ev := next(t, evs, "the decoder never delivered a keystroke, so this test "+
		"is measuring a decoder that never lived"); !ev.IsKey() || ev.Key.Rune != 'a' {
		t.Fatalf("got %#v, want the 'a' we typed", ev)
	}

	// One write, because that is what makes it one read: an Esc and a
	// mouse report reaching the decoder together is ordinary — press
	// Escape and click, or click twice while a dangling Esc has not yet
	// timed out. The report decodes perfectly. It simply is not a KEY,
	// and that alone used to strand the buffer.
	if _, err := master.Write([]byte("\x1b\x1b[<0;10;5M")); err != nil {
		t.Fatalf("write to master: %v", err)
	}
	// Then an ordinary keystroke, which is the actual assertion: it is
	// behind the stranding sequence in the same buffer, so it arrives
	// only if the decoder got past it.
	if _, err := master.Write([]byte("z")); err != nil {
		t.Fatalf("write to master: %v", err)
	}

	deadline := time.After(3 * time.Second)
	for {
		select {
		case ev := <-evs:
			if ev.IsKey() && ev.Key.Rune == 'z' {
				return // got past it
			}
		case <-deadline:
			t.Fatal("the 'z' typed after an Esc-then-mouse-report never arrived: " +
				"the decoder is alive and reading the tty, and every keystroke is " +
				"stranded behind a buffer its drain loop refuses to advance past. " +
				"DecoderDone cannot see this — the goroutine never returns.")
		}
	}
}

// The SECOND way to go deaf without dying, and the one that also burns a
// wakeup every 40ms while it does it.
//
// ESC [ 2 is a strict prefix of the bracketed-paste marker ESC [ 200 ~,
// so input.Decode holds it under idle rather than resolving it to Esc —
// on the reasoning that the rest of a real marker is already on the wire.
// That reasoning covers a paste and not a PERSON, and a person pressing
// Escape and then typing `[` and `2` produces the same three bytes with
// nothing behind them. The buffer then never shrinks, so DecodeEvents
// re-arms its escape timer on every iteration for the rest of the
// process's life, and the next unrelated keystroke is appended behind
// the prefix and absorbed into the CSI parse — ESC [ 2 z is one unmapped
// four-byte sequence and no event at all.
//
// PasteMarkerGrace is the fix: the grace is bounded rather than removed,
// and the pass after it withdraws the exception (input.DecodeFinal).
// Here rather than in input for the reason the test above gives — only
// the loop can show that a decoding contract stranding live input, and
// only a real tty makes the loop the thing under test. In particular the
// GAP is the fixture: these bytes have to arrive in their own read and
// then nothing for two timeouts, which is exactly what a keyboard does
// and what a single Write in one test cannot fake.
func TestATypedPasteMarkerPrefixDoesNotStrandTheDecoder(t *testing.T) {
	master, slave := openPTY(t)
	s := FromFile(slave)
	if err := s.Raw(); err != nil {
		t.Fatalf("raw: %v", err)
	}
	evs := s.Events(16)

	if _, err := master.Write([]byte("a")); err != nil {
		t.Fatalf("write to master: %v", err)
	}
	if ev := next(t, evs, "the decoder never delivered a keystroke, so this test "+
		"is measuring a decoder that never lived"); !ev.IsKey() || ev.Key.Rune != 'a' {
		t.Fatalf("got %#v, want the 'a' we typed", ev)
	}

	if _, err := master.Write([]byte("\x1b[2")); err != nil {
		t.Fatalf("write to master: %v", err)
	}

	// WAIT FOR THE ESC, do not sleep for it. The grace expiring is an
	// EVENT — the Esc arriving is itself the proof that PasteMarkerGrace
	// timeouts passed with nothing new on the wire — so reading for it
	// pins the ordering rather than assuming a schedule.
	//
	// A sleep here was the first version and it raced its own subject.
	// Sleeping past the two timeouts cannot make a broken decoder pass,
	// but it can make a WORKING one fail: if the decoder goroutine is
	// delayed past the sleep, 'z' lands before the second timeout,
	// stalls resets, and ESC [ 2 z decodes as one complete unmapped
	// four-byte CSI emitting nothing — so the test fails at its deadline
	// with the message for the bug under test, and a scheduling flake on
	// a shared runner reads as a regression. Raised in review of #445.
	if ev := next(t, evs, "the Esc never arrived: three bytes that are half a "+
		"paste marker and also three keys a person typed are being held "+
		"forever, and the decoder now wakes every EscTimeout for the life of "+
		"the process without ever delivering them"); !ev.IsKey() ||
		ev.Key.Key != input.KeyEsc {
		t.Fatalf("first event after ESC [ 2 was %#v, want the Esc key", ev)
	}

	// The bytes typed after the Esc arrive as THEMSELVES — consuming only
	// the escape is what makes that true, and swallowing all three would
	// satisfy the liveness assertion below while losing two keystrokes.
	for _, want := range []rune{'[', '2'} {
		ev := next(t, evs, "the keys typed after the Esc never arrived")
		if !ev.IsKey() || ev.Key.Rune != want {
			t.Fatalf("got %#v after the Esc, want the %q key", ev, want)
		}
	}

	// Only now write the unrelated keystroke, with the buffer known to be
	// empty. It is the liveness half: a decoder that resolved the prefix
	// but wedged afterwards would have passed everything above.
	if _, err := master.Write([]byte("z")); err != nil {
		t.Fatalf("write to master: %v", err)
	}
	if ev := next(t, evs, "the 'z' typed after the resolved prefix never arrived: "+
		"the decoder is alive and reading the tty, and keystrokes are stranded "+
		"behind a buffer its drain loop refuses to advance past. DecoderDone "+
		"cannot see this — the goroutine never returns."); !ev.IsKey() ||
		ev.Key.Rune != 'z' {
		t.Fatalf("got %#v, want the 'z' we typed", ev)
	}
}

// The OTHER route to the last-chance pass, and the one whose deadline is
// not a clock at all.
//
// DecodeEvents reaches drainFinal two ways: the stall path, on the
// PasteMarkerGrace'th fruitless timeout, and this one — the tty closed,
// so no byte can ever arrive regardless of how little time has passed.
// Writing that precondition as "after N timeouts" (which an earlier
// draft did) is false here and would send a reader hunting for a bug.
//
// Without this test the tty-close arm could quietly drop to drainIdle and
// nothing would notice: the mutation is invisible to every other test in
// the tree, because they all reach the final pass through the timer.
// What is lost is the last keystrokes a user typed before the terminal
// went away. Added in review of #445.
func TestAClosedTtyResolvesAHeldPrefixBeforeTheDecoderExits(t *testing.T) {
	master, slave := openPTY(t)
	s := FromFile(slave)
	if err := s.Raw(); err != nil {
		t.Fatalf("raw: %v", err)
	}
	evs := s.Events(16)

	// ONE write carrying a handshake byte and then the held prefix, and
	// the handshake is what makes this deterministic rather than a race.
	// Closing the master can discard bytes the slave has not read yet, so
	// "write, then close" alone loses the prefix on most runs and the
	// test measures nothing. Reading the 'b' back proves the decoder
	// consumed that read, which means ESC [ 2 is in pend right now.
	if _, err := master.Write([]byte("b\x1b[2")); err != nil {
		t.Fatalf("write to master: %v", err)
	}
	if ev := next(t, evs, "the decoder never delivered a keystroke, so this test "+
		"is measuring a decoder that never lived"); !ev.IsKey() || ev.Key.Rune != 'b' {
		t.Fatalf("got %#v, want the 'b' we typed", ev)
	}

	// Now the tty goes away, well inside the grace window — so the TIMER
	// route to the final pass is not what can resolve this.
	if err := master.Close(); err != nil {
		t.Fatalf("close master: %v", err)
	}

	if ev := next(t, evs, "the decoder discarded the Esc it was holding when the "+
		"tty closed. A closed tty is the strongest deadline there is — nothing "+
		"can arrive — so a held prefix must resolve on the way out rather than "+
		"leave with it"); !ev.IsKey() || ev.Key.Key != input.KeyEsc {
		t.Fatalf("first event after the tty closed was %#v, want the Esc key", ev)
	}
	for _, want := range []rune{'[', '2'} {
		ev := next(t, evs, "the bytes after the Esc were dropped on teardown")
		if !ev.IsKey() || ev.Key.Rune != want {
			t.Fatalf("got %#v, want the %q key", ev, want)
		}
	}
}

func next(t *testing.T, evs <-chan input.Event, msg string) input.Event {
	t.Helper()
	select {
	case ev := <-evs:
		return ev
	case <-time.After(2 * time.Second):
		t.Fatal(msg)
	}
	return input.Event{}
}
