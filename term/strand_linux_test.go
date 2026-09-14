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
	// TWO INCONCLUSIVE OUTCOMES, COUNTED SEPARATELY, because they carry
	// opposite news and the loop used to report only one of them.
	//
	// A LATE Esc is a loaded machine: the timer resolved the prefix
	// first, the attempt proves nothing, and a retry is expected to
	// work. SILENCE is the regression's own symptom — the decoder
	// discarded a prefix it was holding and nothing will ever arrive.
	// The attempt cannot tell that from a lost prefix (the write split
	// across two slave reads, the hung-up pty discarding the rest), so
	// neither can fail on its own; but twenty of them in a row is not
	// twenty scheduling artefacts, and saying "this machine is too
	// loaded" over the defect under test is a false cause reported after
	// forty seconds of waiting for it. Raised in review of #445.
	// Enough silent attempts to rule out the benign reading, and no
	// more; see the arm below for why the bound is on this outcome only.
	const silentEnough = 5
	var late, silent int
	for i := range attempts {
		switch closedTtyAttempt(t) {
		case attemptMeasured:
			return
		case attemptLate:
			late++
			t.Logf("attempt %d missed the grace window (the timer resolved the "+
				"prefix first); retrying", i+1)
		case attemptSilent:
			silent++
			t.Logf("attempt %d saw no event at all after the close; retrying", i+1)
		}
		// SILENCE IS THE ONLY SLOW OUTCOME — it costs a full nextOrNone
		// window, where a late Esc arrives in about a stall — so twenty
		// of them is forty seconds spent re-establishing what the first
		// few already showed. Stop at enough to make "every one of these
		// lost its prefix to a split read" implausible, and no more.
		if silent >= silentEnough {
			break
		}
	}
	// THE THRESHOLD THE LOOP BREAKS ON IS THE THRESHOLD THE MESSAGE
	// CLAIMS ON. This arm read `silent > 0` while silentEnough was 5, so
	// ONE silent attempt among nineteen late ones named the regression —
	// and one silent attempt is exactly the benign reading the constant
	// exists to rule out, a single split read before the close. The gap
	// only opens on a loaded machine, which is the machine this repo's
	// self-hosted runners are. Raised in review of #445.
	switch {
	case silent >= silentEnough:
		t.Fatalf("%d attempts produced NO event after the tty closed (%d "+
			"more arrived too late to attribute). That is what the regression "+
			"under test looks like: a held prefix discarded instead of drained, "+
			"so the last keystrokes before the terminal went away are lost. The "+
			"benign reading — every one of those attempts lost its prefix to a "+
			"split read before the close — needs %d independent accidents, so "+
			"read DecodeEvents' tty-close arm before blaming the runner",
			silent, late, silent)
	case silent > 0:
		// NO CAUSE NAMED. Below the threshold the two readings are not
		// separable: a discarded prefix and a split read look the same
		// from here, and the counts are the only thing this attempt
		// established.
		t.Fatalf("%d of %d attempts produced no event after the tty closed and %d "+
			"arrived too late to attribute, so none of them measured the grace "+
			"window. Below %d silent attempts a lost prefix (a split read before "+
			"the close) is as good an explanation as a discarded one, so this "+
			"names neither: re-run, and read DecodeEvents' tty-close arm if the "+
			"silent count climbs",
			silent, attempts, late, silentEnough)
	}
	t.Fatalf("all %d attempts missed the grace window: the timer resolved the "+
		"held prefix before the close every time. This machine is too loaded to "+
		"attribute the resolution to the tty-close path, and passing on that "+
		"basis would be a test that guards nothing", attempts)
}

// attemptOutcome is what one closedTtyAttempt could establish. Only
// attemptMeasured is a pass; the other two are the two ways an attempt
// can fail to be an attempt, and they are distinguished because the
// caller's diagnosis differs — see the loop above.
type attemptOutcome int

const (
	attemptMeasured attemptOutcome = iota
	attemptLate
	attemptSilent
)

// closedTtyAttempt runs one attempt. It returns a non-measured outcome
// when the attempt could not distinguish the two routes to drainFinal;
// every genuine disagreement is a t.Fatal rather than a return.
func closedTtyAttempt(t *testing.T) attemptOutcome {
	t.Helper()
	master, slave := openPTY(t)
	s := FromFile(slave)
	if err := s.Raw(); err != nil {
		t.Fatalf("raw: %v", err)
	}
	// PER ATTEMPT, NOT PER TEST. openPTY registers its close on the
	// PARENT t, which is right for a test that opens one pty and wrong
	// for a retry loop: twenty inconclusive attempts held twenty pty
	// pairs open and left twenty decoder goroutines parked on a read
	// that would never return, all until the test ended. Restore is what
	// joins the decoder — it closes the tty and waits, bounded by
	// DecoderTimeout — so releasing the fd alone would not have been
	// enough. openPTY's own cleanup still runs later and closes an
	// already-closed file, which is a no-op. Raised in review of #445.
	defer func() {
		s.Restore()
		master.Close()
	}()
	evs := s.Events(16)

	// ONE write carrying a handshake byte and then the held prefix, and
	// the handshake is what makes this a measurement rather than a race.
	// Closing the master can discard bytes the slave has not read yet, so
	// "write, then close" alone loses the prefix on most runs and the
	// test measures nothing.
	//
	// WHAT READING THE 'b' BACK PROVES is that the decoder consumed a
	// read — not that it consumed THIS WHOLE WRITE. One write is not one
	// read: the slave may return "b" and "\x1b[2" separately, and a
	// hung-up pty discards input still queued, so the prefix can be gone
	// before the close. Four bytes from one write come back in one
	// 128-byte read essentially always, which makes this unlikely rather
	// than impossible — and the receive below is non-fatal for exactly
	// that residue, because a lost prefix is an attempt that could not be
	// made, not a decoder that dropped an Esc. Raised in review of #445.
	if _, err := master.Write([]byte("b\x1b[2")); err != nil {
		t.Fatalf("write to master: %v", err)
	}
	if ev := next(t, evs, "the decoder never delivered a keystroke, so this test "+
		"is measuring a decoder that never lived"); !ev.IsKey() || ev.Key.Rune != 'b' {
		t.Fatalf("got %#v, want the 'b' we typed", ev)
	}
	// The clock starts HERE, and `held` sits within a scheduling gap of
	// the arm on EITHER SIDE of it. keys.go sends each decoded event and
	// THEN re-arms (`drain(drainLive)` … `timer.Reset(EscTimeout)`) over
	// a buffered channel, so this receive can run before the Reset
	// executes (held EARLY) or well after it (held LATE), depending on
	// which goroutine is descheduled. The comment here asserted the late
	// direction alone and its sibling asserted the early one, which is
	// the drift the spec already had right at "either side".
	//
	// THIS HELPER'S BUDGET DEPENDS ON THE LATE DIRECTION ONLY. It
	// requires the event after the close to arrive inside EscTimeout of
	// `held`, so an arm EARLIER than `held` only widens the real margin;
	// an arm LATER is what could let a timer-delivered Esc measure as
	// inside the budget, and that needs the test goroutine descheduled
	// for most of a timeout. Raised in review of #445.
	held := time.Now()

	if err := master.Close(); err != nil {
		t.Fatalf("close master: %v", err)
	}

	ev, arrived := nextOrNone(evs, 2*time.Second)
	if !arrived {
		// INCONCLUSIVE, NOT A DISAGREEMENT. A t.Fatal here reported "the
		// decoder discarded the Esc it was holding" — the message for the
		// regression under test — for an attempt in which the prefix
		// never reached the decoder at all, which is a scheduling
		// artefact wearing the costume of the bug. Same discipline as the
		// elapsed check below, in the other direction: never pass on an
		// attempt you did not make, and never fail on one either.
		// NOT DIAGNOSED HERE, and that is the change. This logged "the
		// attempt lost the held prefix", which is one of the two causes
		// of silence and the benign one; the other is the decoder
		// discarding a prefix it was holding, which is the defect under
		// test. One attempt cannot tell them apart, so it reports what
		// it SAW and the loop above weighs the counts.
		return attemptSilent
	}
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
		return attemptLate // the timer could have done this; attribute nothing
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
	return attemptMeasured
}

// nextOrNone is next() without the verdict: it reports whether an event
// arrived at all, for the receives where NOT arriving means the attempt
// could not be made rather than that the decoder is wrong.
func nextOrNone(evs <-chan input.Event, d time.Duration) (input.Event, bool) {
	select {
	case ev := <-evs:
		return ev, true
	case <-time.After(d):
		return input.Event{}, false
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
	// FORTY, not twenty, for the reason the sleep above gives: the margin
	// this attempt needs is scheduler headroom, and doubling the draws is
	// the half of that which costs nothing and weakens nothing. Raised in
	// review of #445.
	const attempts = 40
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
	// The same per-attempt release closedTtyAttempt takes, and this loop
	// runs forty attempts rather than twenty.
	defer func() {
		s.Restore()
		master.Close()
	}()
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
	// marker simply arriving whole in one read — and comfortably inside two.
	//
	// `held` IS NOT KNOWN TO BE AFTER THE ARM, and the comment here used
	// to say it was. keys.go's loop sends each decoded event and THEN
	// re-arms the timer (`drain(drainLive)` … `timer.Reset(EscTimeout)`),
	// and `out` is this test's buffered channel — so the send does not
	// block, and the receive below can be scheduled before the Reset
	// executes. `held` can therefore land on EITHER SIDE of the arm:
	// early if the decoder is descheduled in those few instructions, late
	// if this goroutine is. The correction here first said the sibling
	// helper "had the ordering right", which named the other single
	// direction — closedTtyAttempt asserted late-only — and a guarantee a
	// file states two ways is worth less than the slack it was
	// defending. Both comments say either side now, and each names the
	// direction its own budget rests on: this one needs both, because
	// the write has to land inside a window measured from the arm at
	// both ends. Raised in review of #445, twice.
	//
	// So the window is budgeted at BOTH ends rather than assumed at one.
	// The arm sits somewhere within a scheduling gap of `held`, the window
	// the write must land in is (arm+EscTimeout, arm+2*EscTimeout), and a
	// quarter of a timeout at each end is what the sleep and the budget
	// below now reserve for that gap — 10ms apiece at EscTimeout=40ms,
	// against the 5ms the old asymmetric pair left at the bottom. The
	// overshoot allowance grows with it, from EscTimeout*3/8 to
	// EscTimeout/2, which is the thing a loaded runner actually spends.
	time.Sleep(EscTimeout + EscTimeout/4)
	if _, err := master.Write([]byte("00~payload\x1b[201~")); err != nil {
		t.Fatalf("write to master: %v", err)
	}
	// AN ABSOLUTE BUDGET, not PasteMarkerGrace*EscTimeout, and both halves of
	// that matter.
	//
	// Scaling by the constant under test is self-defeating: at
	// PasteMarkerGrace = 1 — the mutation this test exists to catch — the
	// sleep stays put while the budget collapses to 40ms, so every attempt
	// returns false and the test dies with "this machine is too loaded",
	// pointing the next reader at the runner instead of at the constant they
	// just changed. The #419 message below becomes unreachable. The floor in
	// TestPasteMarkerGraceHasAFloor is what makes 2*EscTimeout safe to write
	// literally here.
	//
	// And the SLACK is the other end of the gap the sleep above reserves
	// for: `held` can land either side of the arm, so with zero slack an
	// attempt whose grace had already expired reads as conclusive and
	// hard-fails with the #419 message — a scheduling stall wearing the
	// costume of a regression, which is precisely what the sleep-vs-wait
	// fix in the sibling test removed. Both raised in review of #445.
	if elapsed := time.Since(held); elapsed >= 2*EscTimeout-EscTimeout/4 {
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
