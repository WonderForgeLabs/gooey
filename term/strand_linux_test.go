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
//
// IT CAN ONLY MEASURE ANYTHING IF THE TTY CLOSES WHILE THE PREFIX IS
// STILL HELD, and the first version of it did not check that. A runner
// stalling past the grace window lets the TIMER resolve the prefix
// before the close, at which point the Esc arrives either way and the
// test passes without guarding its mutation — green, and blind. Raised
// in review of #445.
//
// The discriminator is arrival TIME, measured from the moment the
// decoder is known to be holding the prefix. The stall path cannot
// deliver before PasteMarkerGrace full timeouts have elapsed; the
// tty-close path delivers as soon as the read fails. So an Esc arriving
// inside that budget can only have come from the close. Outside it, the
// attempt is INCONCLUSIVE rather than passing, and the test tries again
// — running out of attempts is a failure, never a pass.
func TestAClosedTtyResolvesAHeldPrefixBeforeTheDecoderExits(t *testing.T) {
	const attempts = 20
	for i := range attempts {
		if closedTtyAttempt(t) {
			return
		}
		t.Logf("attempt %d missed the grace window (the timer resolved the "+
			"prefix first); retrying", i+1)
	}
	t.Fatalf("could not close the tty inside the grace window in %d attempts. "+
		"This machine is too loaded to attribute the resolution to the "+
		"tty-close path, and passing on that basis would be a test that "+
		"guards nothing", attempts)
}

// closedTtyAttempt runs one attempt. It returns false when the attempt
// could not distinguish the two routes to drainFinal; every genuine
// disagreement is a t.Fatal rather than a false.
func closedTtyAttempt(t *testing.T) bool {
	t.Helper()
	master, slave := openPTY(t)
	s := FromFile(slave)
	if err := s.Raw(); err != nil {
		t.Fatalf("raw: %v", err)
	}
	evs := s.Events(16)

	// ONE write carrying a handshake byte and then the held prefix, and
	// the handshake is what makes this a measurement rather than a race.
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
	// The clock starts HERE: the decoder has just consumed that read, so
	// this is when its escape timer is armed over the held prefix.
	held := time.Now()

	if err := master.Close(); err != nil {
		t.Fatalf("close master: %v", err)
	}

	ev := next(t, evs, "the decoder discarded the Esc it was holding when the "+
		"tty closed. A closed tty is the strongest deadline there is — nothing "+
		"can arrive — so a held prefix must resolve on the way out rather than "+
		"leave with it")
	// EscTimeout, not PasteMarkerGrace*EscTimeout, and the difference is
	// slack against this clock's own drift. `held` is sampled after
	// receiving `b` from a BUFFERED channel, and the decoder sends that
	// event before it arms the escape timer — so if the test goroutine is
	// descheduled, `held` lands after the arm by that much. Budgeting the
	// full stall latency then lets a timer-delivered Esc measure just under
	// it and be credited to the close, which is the false pass this retry
	// loop exists to prevent. One EscTimeout is still orders of magnitude
	// above the close path's real latency — it resolves on a failed read,
	// not on a deadline. Raised in review of #445.
	if elapsed := time.Since(held); elapsed >= EscTimeout {
		return false // the timer could have done this; attribute nothing
	}
	if !ev.IsKey() || ev.Key.Key != input.KeyEsc {
		t.Fatalf("first event after the tty closed was %#v, want the Esc key", ev)
	}
	for _, want := range []rune{'[', '2'} {
		ev := next(t, evs, "the bytes after the Esc were dropped on teardown")
		if !ev.IsKey() || ev.Key.Rune != want {
			t.Fatalf("got %#v, want the %q key", ev, want)
		}
	}
	return true
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

// PasteMarkerGrace itself, which nothing pinned — and it is the number the
// whole record argues about.
//
// Mutating it from 2 to 1 left `go test ./input/... ./term/...` and the root
// suite entirely green while reverting #425's fix for #419. At 1, `stalls++`
// reaches the threshold on the FIRST idle timeout, so the split-marker arm is
// never exercised: any 40ms stall inside the six-byte opener resolves it to
// Esc and the payload arrives as the keystroke burst mode 2004 exists to
// prevent. TestFinalDecodeResolvesTheTypedMarkerPrefix pins the input-side
// grace, but that is independent of the constant; the loop is the only layer
// that decides it. Raised in review of #445.
//
// So this is #419 itself, end to end: a real paste whose opening marker
// straddles a read must still arrive as ONE PasteEvent.
func TestASplitPasteMarkerStillPastes(t *testing.T) {
	const attempts = 20
	for i := range attempts {
		if splitMarkerAttempt(t) {
			return
		}
		t.Logf("attempt %d could not land the second write inside the grace "+
			"window; retrying", i+1)
	}
	t.Fatalf("could not write the marker's tail inside the grace window in %d "+
		"attempts. This machine is too loaded to distinguish a working decoder "+
		"from a broken one, and passing on that basis would be a test that "+
		"guards nothing", attempts)
}

// splitMarkerAttempt returns false when the attempt could not be made inside
// the window — the same inconclusive-rather-than-green discipline
// closedTtyAttempt uses, and for the same reason: a stalled runner must not
// be able to turn "we never tested it" into a pass.
func splitMarkerAttempt(t *testing.T) bool {
	t.Helper()
	master, slave := openPTY(t)
	s := FromFile(slave)
	if err := s.Raw(); err != nil {
		t.Fatalf("raw: %v", err)
	}
	evs := s.Events(16)

	// The handshake byte again: reading `b` back proves the decoder consumed
	// that read, so ESC [ 2 is in pend and the clock below starts when its
	// escape timer is armed rather than whenever the write happened to land.
	if _, err := master.Write([]byte("b\x1b[2")); err != nil {
		t.Fatalf("write to master: %v", err)
	}
	if ev := next(t, evs, "the decoder never delivered a keystroke, so this test "+
		"is measuring a decoder that never lived"); !ev.IsKey() || ev.Key.Rune != 'b' {
		t.Fatalf("got %#v, want the 'b' we typed", ev)
	}
	held := time.Now()

	// Past ONE timeout — so the grace is genuinely exercised rather than the
	// marker simply arriving whole in one read — but inside PasteMarkerGrace
	// of them, which is the window the constant defines.
	time.Sleep(EscTimeout + EscTimeout/2)
	if _, err := master.Write([]byte("00~payload\x1b[201~")); err != nil {
		t.Fatalf("write to master: %v", err)
	}
	if elapsed := time.Since(held); elapsed >= PasteMarkerGrace*EscTimeout {
		return false // the grace may already have expired; attribute nothing
	}

	ev := next(t, evs, "no event arrived after the paste marker's tail")
	if !ev.IsPaste() {
		// THE #419 FAILURE, and it is worth naming rather than reporting a
		// type mismatch: at PasteMarkerGrace = 1 the first timeout resolves
		// the held prefix to Esc, and what arrives here is that Esc followed
		// by "00~payload" as individual keystrokes.
		t.Fatalf("first event after the split marker was %#v, want one PasteEvent. "+
			"A paste whose opening marker straddled a read has been torn up into "+
			"keystrokes — #419. If PasteMarkerGrace was just lowered, this is "+
			"what it costs: at 1 the FIRST idle timeout resolves the prefix, "+
			"which is what `idle` already means, and the grace does not exist", ev)
	}
	if got := ev.Paste.Text; got != "payload" {
		t.Fatalf("paste payload = %q, want %q", got, "payload")
	}
	return true
}

// The cheap deterministic backstop for the same property. The pty test above
// is the real pin — it fails on the BEHAVIOUR — but it needs a pty and a
// window, and this one needs neither, so a machine that cannot run the first
// still cannot lower the constant unnoticed.
func TestPasteMarkerGraceHasAFloor(t *testing.T) {
	if PasteMarkerGrace < 2 {
		t.Fatalf("PasteMarkerGrace is %d. Below 2 the FIRST idle timeout "+
			"resolves a split paste marker to Esc — which is exactly what "+
			"`idle` already means, so the grace stops existing and #419 "+
			"returns: a paste whose opening marker straddles a read arrives "+
			"as a stray Esc followed by its payload as keystrokes. Raising it "+
			"is a trade (see the constant's doc); lowering it past 2 is not.",
			PasteMarkerGrace)
	}
}
