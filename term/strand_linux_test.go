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
// the loop can show that violating a decoding contract strands live
// input, and only a real tty makes the loop the thing under test. In
// particular the GAP is the fixture: these bytes have to arrive in their
// own read and
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
	// delayed past the sleep, 'z' lands before the second timeout, stalls
	// resets, and ESC [ 2 z decodes as one complete unmapped four-byte CSI
	// emitting nothing — so the test fails at its deadline with the
	// message for the bug under test, and a scheduling flake on a shared
	// runner reads as a regression.
	if ev := next(t, evs, "the Esc never arrived: three bytes that are half a "+
		"paste marker and also three keys a person typed are being held "+
		"forever. Two causes, and this assertion cannot tell them apart: "+
		"the grace is never withdrawn, so the decoder wakes every EscTimeout "+
		"for the life of the process without delivering them; or "+
		"PasteMarkerGrace is 0, in which case it does not wake at all, "+
		"because the re-arm condition is false on the first iteration and "+
		"the escape timeout has stopped existing rather than firing "+
		"forever. TestPasteMarkerGraceHasAFloor's zero arm is the message "+
		"to read for the second"); !ev.IsKey() ||
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
	var late, silent, drifted int
	for i := range attempts {
		switch closedTtyAttempt(t) {
		case attemptMeasured:
			return
		case attemptLate:
			late++
			t.Logf("attempt %d missed the grace window (the timer resolved the "+
				"prefix first); retrying", i+1)
		case attemptDrifted:
			drifted++
			t.Logf("attempt %d was abandoned before the close: the handshake "+
				"took more than a quarter timeout, so the arm cannot be "+
				"located; retrying", i+1)
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
	// THE THRESHOLD THE LOOP BREAKS ON IS THE THRESHOLD THE MESSAGE CLAIMS
	// ON. This arm read `silent > 0` while silentEnough was 5, so ONE
	// silent attempt among nineteen late ones named the regression — and
	// one silent attempt is exactly the benign reading the constant exists
	// to rule out, a single split read before the close. The gap only
	// opens on a loaded machine, which is the machine this repo's
	// self-hosted runners are.
	switch {
	case silent >= silentEnough:
		t.Fatalf("%d attempts produced NO event after the tty closed (%d "+
			"more arrived too late to attribute, %d were abandoned before the "+
			"close). That is what the regression "+
			"under test looks like: a held prefix discarded instead of drained, "+
			"so the last keystrokes before the terminal went away are lost. The "+
			"benign reading — every one of those attempts lost its prefix to a "+
			"split read before the close — needs %d independent accidents, so "+
			"read DecodeEvents' tty-close arm before blaming the runner",
			silent, late, drifted, silent)
	case silent > 0:
		// NO CAUSE NAMED. Below the threshold the two readings are not
		// separable: a discarded prefix and a split read look the same
		// from here, and the counts are the only thing this attempt
		// established.
		t.Fatalf("%d of %d attempts produced no event after the tty closed, %d "+
			"arrived too late to attribute and %d were abandoned before the "+
			"close, so none of them measured the grace window. Below %d silent "+
			"attempts a lost prefix (a split read before the close) is as good "+
			"an explanation as a discarded one, so this names neither: re-run, "+
			"and read DecodeEvents' tty-close arm if the silent count climbs",
			silent, attempts, late, drifted, silentEnough)
	}
	// A CAUSE PER COUNT, because the two routes here establish different
	// things and a single sentence over both named the timer for attempts
	// that never reached it.
	t.Fatalf("none of %d attempts measured the grace window: %d arrived after "+
		"the close but late enough that the timer could have produced them, and "+
		"%d were abandoned before the close because the handshake drifted more "+
		"than a quarter timeout, which locates no arm and observes no timer. "+
		"This machine is too loaded to attribute the resolution to the "+
		"tty-close path, and passing on that basis would be a test that guards "+
		"nothing", attempts, late, drifted)
}

// attemptOutcome is what one closedTtyAttempt could establish. Only
// attemptMeasured is a pass; the other three are the three ways an
// attempt can fail to be an attempt, and they are distinguished because
// the caller's diagnosis differs — see the loop above.
//
// attemptDrifted IS NOT attemptLate, and folding them cost the messages
// their truth. attemptLate is observed: the close ran, an event arrived,
// and its timing says the timer could have produced it. The drift bail
// returns BEFORE master.Close() — no close, no event, no timer seen
// resolving anything — so rendering it as "the timer resolved the prefix
// first" names a cause the attempt never established. That is the same
// class this file corrected twice already (the threshold the loop breaks
// on is the threshold the message claims on, and the silent arm's NO CAUSE
// NAMED).
type attemptOutcome int

const (
	attemptMeasured attemptOutcome = iota
	attemptLate
	attemptSilent
	attemptDrifted
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
	// PER ATTEMPT, NOT PER TEST. openPTY registers its close on the PARENT
	// t, which is right for a test that opens one pty and wrong for a
	// retry loop: twenty inconclusive attempts held twenty pty pairs open
	// and left twenty decoder goroutines parked on a read that would never
	// return, all until the test ended. Restore is what joins the decoder
	// — it closes the tty and waits, bounded by DecoderTimeout — so
	// releasing the fd alone would not have been enough. openPTY's own
	// cleanup still runs later and closes an already-closed file, which is
	// a no-op.
	defer func() {
		s.Restore()
		// AND THE TEARDOWN HALF OF THE CLAIM, which was asserted in prose
		// and by nothing else. Restore sets decLeaked from joinDecoder,
		// and every helper in this file threw it away.
		//
		// This one matters most: closedTtyAttempt is the only place in
		// the tree that tears a Screen down with a HELD MARKER PREFIX
		// still in pend, which is exactly the drain(drainFinal)
		// chunks-closed path this PR changes. term/lifecycle_test.go
		// reads DecoderLeaked four times and never in that shape. Raised
		// in review of #445.
		if s.DecoderLeaked() {
			t.Errorf("the decoder was still reading the tty %v after Restore "+
				"returned, with a paste-marker prefix held in pend — the close "+
				"path drains through DecodeFinal now, and a leak here means that "+
				"drain did not let the reader finish", DecoderTimeout)
		}
		master.Close()
	}()
	evs := s.Events(16)

	// ONE write carrying a handshake byte and then the held prefix, and
	// the handshake is what makes this a measurement rather than a race.
	// Closing the master can discard bytes the slave has not read yet, so
	// "write, then close" alone loses the prefix on most runs and the
	// test measures nothing.
	//
	// WHAT READING THE 'b' BACK PROVES is that the decoder consumed a read
	// — not that it consumed THIS WHOLE WRITE. One write is not one read:
	// the slave may return "b" and "\x1b[2" separately, and a hung-up pty
	// discards input still queued, so the prefix can be gone before the
	// close. Four bytes from one write come back in one 128-byte read
	// essentially always, which makes this unlikely rather than impossible
	// — and the receive below is non-fatal for exactly that residue,
	// because a lost prefix is an attempt that could not be made, not a
	// decoder that dropped an Esc.
	wrote := time.Now()
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
	// THIS HELPER'S BUDGET DEPENDS ON THE LATE DIRECTION ONLY — held
	// late, which is the arm EARLY. It requires the event after the close
	// to arrive inside EscTimeout of `held`, and a timer-delivered Esc
	// lands at arm + 2*EscTimeout, so the elapsed this helper measures is
	// 2*EscTimeout minus the drift. An arm LATER than `held` subtracts a
	// negative and only widens the real margin; an arm EARLIER is what
	// shrinks the elapsed until a timer-delivered Esc measures as inside
	// the budget. That needs a drift of 2*EscTimeout, not "most of a
	// timeout" as this said: while `held < arm + 2*EscTimeout` the close
	// still wins the race and the Esc genuinely comes from the close
	// path, so the verdict is right whatever the elapsed reads.
	//
	// THE TWO LABELS WERE SWAPPED HERE, and swapped against this
	// paragraph's own heading, against the `held < arm + 2*EscTimeout`
	// condition it goes on to state, and against the guard below, whose
	// arithmetic sentence has always been right. A reader deciding whether
	// the EscTimeout/4 guard is still needed would have read this and
	// concluded it defends the direction that is already conservative.
	held := time.Now()
	// AND THE DRIFT IS MEASURED, the way splitMarkerAttempt measures it
	// below. Sampling `held` after a receive on a BUFFERED channel is what
	// makes the paragraph above a hazard rather than an observation: at a
	// drift of 2*EscTimeout the stall path has already escalated and
	// pushed Esc, [, 2 into the channel before master.Close() runs, so
	// nextOrNone returns an ALREADY-QUEUED Esc with time.Since(held) ≈ 0,
	// every assertion below passes, and the attempt reports
	// attemptMeasured having measured the TIMER path — the tty-close arm
	// this helper exists for going untested, green, which is the outcome
	// the doc above calls worse than red. `arm >= wrote`, because the
	// decoder cannot arm for bytes it has not read, so held-minus-wrote
	// bounds held-minus-arm from above and is the discriminator the
	// elapsed check below cannot be.
	if held.Sub(wrote) > EscTimeout/4 {
		// NOT attemptLate: nothing has been closed or observed yet, so
		// there is no timer to blame. See attemptOutcome.
		return attemptDrifted // held may be a quarter-timeout past the arm
	}

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
	// full stall latency then lets a timer-delivered Esc measure just
	// under it and be credited to the close, which is the false pass this
	// retry loop exists to prevent. One EscTimeout is still orders of
	// magnitude above the close path's real latency — it resolves on a
	// failed read, not on a deadline.
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
	// FORTY, not twenty, for the reason the sleep in splitMarkerAttempt
	// gives: the margin this attempt needs is scheduler headroom, and
	// doubling the draws is the half of that which costs nothing and
	// weakens nothing.
	//
	// NAMED, NOT POINTED AT. This said "the sleep above" for a sleep 104
	// lines BELOW — the same class as the "seventy lines below" this
	// branch already removed, and inconsistent with its own neighbour
	// upstream, which names splitMarkerAttempt outright. A name cannot
	// drift; a direction does, every time something moves between the two.
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
		// Its sibling in closedTtyAttempt carries the reasoning.
		if s.DecoderLeaked() {
			t.Errorf("the decoder was still reading the tty %v after Restore "+
				"returned", DecoderTimeout)
		}
		master.Close()
	}()
	evs := s.Events(16)

	// The handshake byte again: reading `b` back proves the decoder consumed
	// that READ — NOT THAT IT CONSUMED THIS WHOLE WRITE. One write is not
	// one read, so `ESC [ 2` may still be in the tty buffer when `b`
	// comes back, and the clock below starts at an arm this goroutine
	// cannot observe. closedTtyAttempt and partialProgressAttempt say
	// the same about their own handshakes; this was the third site and
	// the only one still inferring the arm, which put it 37 lines from
	// its own capitalised correction below.
	//
	// The residue that leaves is written down rather than bounded
	// (docs/specs/2026-09-01-paste-marker-grace.md, "the third helper
	// with an unbounded arm"): a late arm lets the tail land while the
	// prefix is still live, the sequence decodes as one paste, and this
	// helper returns true having exercised nothing.
	wrote := time.Now()
	if _, err := master.Write([]byte("b\x1b[2")); err != nil {
		t.Fatalf("write to master: %v", err)
	}
	if ev := next(t, evs, "the decoder never delivered a keystroke, so this test "+
		"is measuring a decoder that never lived"); !ev.IsKey() || ev.Key.Rune != 'b' {
		t.Fatalf("got %#v, want the 'b' we typed", ev)
	}
	held := time.Now()
	// THE DRIFT IS MEASURED AGAINST `wrote`, BECAUSE IT CANNOT BE
	// MEASURED AGAINST `held`. The budget below this used to be the only
	// guard, and it is `time.Since(held)` — measured from the drifting
	// clock itself, so it cannot observe that clock's drift. What it
	// actually bounds is the latency of the tail write, which is a
	// different quantity and a small one.
	//
	// The arm is necessarily at or after this write — the decoder cannot
	// arm a timer for bytes it has not read — so `held - wrote` bounds
	// `held - arm` from above, and a quarter of a timeout is the gap the
	// comment above says it reserves. Without it, a test goroutine
	// descheduled by more than EscTimeout*3/4 between the decoder's
	// buffered send of the `b` event and the time.Now() here makes the
	// tail land after the grace has already expired: next() then returns
	// the resolved Esc and this helper HARD-FAILS with the #419 message,
	// which the retry loop cannot absorb. A scheduling stall wearing the
	// costume of a regression, in the one direction the record did not
	// acknowledge.
	if held.Sub(wrote) > EscTimeout/4 {
		return false // held may be a quarter-timeout past the arm; attribute nothing
	}

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
	// The arm sits somewhere within a scheduling gap of `held`, and the
	// window the write must land in is (arm+EscTimeout, arm+2*EscTimeout).
	// This sleep reserves a quarter of a timeout at the BOTTOM — 10ms at
	// EscTimeout=40ms, against the 5ms the old asymmetric pair left there.
	// The top is not a second reservation of its own: the budget below is
	// ONE allowance measured from `wrote`, spending the same window the
	// drift does, because two independent ones summed to all 80ms of it.
	// What that leaves for a sleep that overruns and a write that is slow
	// is 2*EscTimeout-EscTimeout/4 minus this sleep minus `held-wrote`, so
	// EscTimeout/2 on an idle machine and less exactly when the machine is
	// the reason it is needed — which is the trade, and the retry loop is
	// what absorbs it. That last clause was true of the bails and NOT of
	// the read below, which hard-failed; the branch at the !IsPaste arm is
	// what makes it true of both.
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
	// AND IT IS MEASURED FROM `wrote`, WHICH IS WHAT STOPS THE TWO
	// ALLOWANCES STACKING. This was `time.Since(held)`, and that made the
	// two guards independent budgets drawn on one window: the drift guard
	// admits `held` up to EscTimeout/4 past the arm, this one admitted a
	// further 2*EscTimeout-EscTimeout/4, and 10ms + 70ms is EXACTLY the
	// 80ms the grace lasts. Both comparisons stayed strict, so the write
	// still landed before `arm+2*EscTimeout` — by an arbitrarily small
	// margin, which is the part that does not survive contact: this
	// budget bounds when `master.Write` RETURNS, and returning only
	// queues the bytes on the pty. Nothing was left for the decoder to
	// read them in, so the compound worst case — a drift at the guard's
	// limit AND a sleep or write that overruns to the budget's — left the
	// #419 hard-fail REACHABLE on a healthy decoder. That corner is
	// derived from the two thresholds, not measured: it needs both to sit
	// at their limits on the same attempt, which is exactly the shape a
	// mutation cannot arrange and a loaded runner can. Raised in review of
	// #445 round ten.
	//
	// `wrote` is the one clock provably not after the arm — the same
	// `arm >= wrote` the drift guard rests on — so ONE budget measured
	// from it covers the drift, the sleep and the write together and
	// leaves the remaining EscTimeout/4 of the window for that read.
	// The drift guard above keeps its place as the early bail it also
	// is: an attempt already a quarter-timeout behind at the handshake
	// has spent this budget's overshoot before it sleeps, and returning
	// there saves it the EscTimeout+EscTimeout/4 the sleep would cost.
	if elapsed := time.Since(wrote); elapsed >= 2*EscTimeout-EscTimeout/4 {
		return false // the grace may already have expired; attribute nothing
	}

	ev := next(t, evs, "no event arrived after the paste marker's tail")
	if !ev.IsPaste() {
		// THE BUDGET ABOVE BOUNDS THE WRITE, NOT THE READ, and that gap
		// is the last place this helper could still blame the code for
		// the machine.
		//
		// `time.Since(wrote) < 2*EscTimeout - EscTimeout/4` bounds when
		// master.Write RETURNS; the grace expires at arm+2*EscTimeout
		// with arm >= wrote. So an attempt is admitted with as little as
		// EscTimeout/4 — 10ms — left for the decoder goroutine to be
		// SCHEDULED and read the tail. Descheduled past that, the timer
		// resolves the prefix first, an Esc arrives here, and this was a
		// t.Fatalf — which the forty-attempt retry loop cannot absorb,
		// whatever the comment at the sleep says. On this repo's shared
		// self-hosted pools a sleep overshoot of ~18ms still passes the
		// 70ms budget and leaves ~2ms of read headroom.
		//
		// THE ARRIVAL TIME DISCRIMINATES, which is what makes the bail
		// safe. Under PasteMarkerGrace = 1 the Esc is emitted at
		// ≈ arm+EscTimeout ≈ wrote+40ms — BEFORE the tail write at
		// wrote+50ms — so it is already queued and comes back well under
		// 2*EscTimeout. A healthy-but-loaded decoder cannot produce one
		// before wrote+2*EscTimeout, because that is when the grace
		// expires. So the mutation stays caught and the stall stops
		// being reported as #419.
		//
		// The cost, stated: it makes the PasteMarkerGrace = 1 kill
		// probabilistic, the same way the spec already concedes this row
		// holds probabilistically. TestPasteMarkerGraceHasAFloor is the
		// deterministic pin and is unaffected.
		if ev.IsKey() && ev.Key.Key == input.KeyEsc &&
			time.Since(wrote) >= 2*EscTimeout {
			return false // the grace had already expired; attribute nothing
		}
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

// The counter's reset on the CHUNKS branch, which this PR's conditional
// re-arm turned from bookkeeping into liveness.
//
// Before the re-arm was made conditional, `stalls` decided only which
// drain a timeout ran, and losing its reset cost nothing an app could
// see. Now `len(pend) > 0 && stalls < PasteMarkerGrace` decides whether
// the escape timer is armed AT ALL, which makes stalls-at-the-ceiling an
// ABSORBING state: the only other reset (`len(pend) != before`) lives
// inside the timer branch and so needs the timer already armed. The one
// line standing between the loop and that state is `stalls = 0` on the
// chunks branch — and deleting it leaves term, input and the root suite
// entirely green.
//
// What that green ships: any paste that takes longer than
// PasteMarkerGrace*EscTimeout to finish — 80ms, which a 40KB paste
// spends routinely — drives stalls to the ceiling, completes through the
// chunks branch without clearing it, and from that moment the escape
// timer is never armed again. A lone Esc is held for the life of the
// process. That is #440's own symptom in a new shape, reached by an
// ordinary paste rather than by typed bytes. Raised in review of #445.
//
// No window to hit and no retry discipline: the open paste is resolved
// by its TAIL, not by a deadline, so no assertion below can fail for a
// scheduling reason, and sleeping past the grace only makes the intended
// precondition more firmly true.
//
// THE RESIDUE IS A VACUOUS PASS, and this is the fourth helper in this
// file rather than the exception to the other three. The precondition
// is not "120ms elapsed": it is that the decoder READ
// `ESC [ 200 ~ hello` and then saw PasteMarkerGrace timeouts with
// nothing new on the wire, and nothing here observes that. There is no
// observable at stalls = 2 — an open paste emits nothing, which is the
// very property that makes it the buffer that drives the counter — and
// the handshake byte is read back BEFORE the paste write, so it bounds
// the decoder's progress only up to that point. A deschedule of the
// decoder spanning the sleep means both writes come back in one read:
// the paste completes at once, stalls never leaves 0, and every
// assertion below passes with the chunks branch's `stalls = 0` deleted
// — the mutation this test is the sole pin for. It cannot go the other
// way, so what the residue costs is a silent hole on a loaded machine
// rather than a flake. The spec states it beside splitMarkerAttempt's,
// partialProgressAttempt's and loneEscAttempt's. Raised in review of
// #445.
func TestAPasteThatOutlastsTheGraceLeavesTheEscapeTimerArmable(t *testing.T) {
	master, slave := openPTY(t)
	s := FromFile(slave)
	if err := s.Raw(); err != nil {
		t.Fatalf("raw: %v", err)
	}
	evs := s.Events(16)

	// The handshake byte the tests above use: reading it back proves the
	// decoder consumed a read, so what follows is measured against a
	// decoder known to be alive.
	if _, err := master.Write([]byte("b")); err != nil {
		t.Fatalf("write to master: %v", err)
	}
	if ev := next(t, evs, "the decoder never delivered a keystroke, so this test "+
		"is measuring a decoder that never lived"); !ev.IsKey() || ev.Key.Rune != 'b' {
		t.Fatalf("got %#v, want the 'b' we typed", ev)
	}

	// An OPEN paste: marker complete, payload's end not yet on the wire.
	// input.DecodeFinal wedges on this deliberately — delivering it early
	// truncates the paste — so it is the one buffer that survives the
	// last-chance pass, and therefore the one that drives stalls to the
	// ceiling without being consumed.
	if _, err := master.Write([]byte("\x1b[200~hello")); err != nil {
		t.Fatalf("write to master: %v", err)
	}
	// Past the whole grace, with a timeout to spare. This is not a window
	// to land inside: overshooting it cannot weaken the precondition.
	time.Sleep(PasteMarkerGrace*EscTimeout + EscTimeout)

	// The tail, arriving on the chunks branch — the branch whose reset is
	// the subject.
	if _, err := master.Write([]byte("\x1b[201~")); err != nil {
		t.Fatalf("write to master: %v", err)
	}
	ev := next(t, evs, "the paste never completed")
	if !ev.IsPaste() {
		t.Fatalf("got %#v, want the PasteEvent for the completed paste", ev)
	}
	if got := ev.Paste.Text; got != "hello" {
		t.Fatalf("paste payload = %q, want %q", got, "hello")
	}

	// THE SUBJECT. Everything above passes with the reset deleted; only
	// this does not. The paste is delivered, pend is empty, and the next
	// byte is an ordinary Esc — which needs a timeout to resolve, which
	// needs the timer to be armed.
	if _, err := master.Write([]byte("\x1b")); err != nil {
		t.Fatalf("write to master: %v", err)
	}
	if ev := next(t, evs, "the Esc typed after a paste that outlasted the grace "+
		"never arrived, so len(pend) > 0 && stalls < PasteMarkerGrace is "+
		"permanently false, the escape timer is never re-armed, and every "+
		"incomplete sequence from here on is held for the life of the "+
		"process. DecoderDone cannot see this — the goroutine never returns. "+
		"TWO WAYS THAT CONDITION STAYS FALSE. Either stalls is at its "+
		"ceiling and nothing cleared it when the paste completed — the "+
		"chunks branch's `stalls = 0`, which this test is the sole pin for; "+
		"or PasteMarkerGrace is 0, where stalls never moved at all and the "+
		"ceiling is what is wrong, which is TestPasteMarkerGraceHasAFloor's "+
		"zero arm. Naming only the first sends a reader hunting a spent "+
		"counter in a run where the counter is 0"); !ev.IsKey() || ev.Key.Key != input.KeyEsc {
		t.Fatalf("got %#v, want the Esc key", ev)
	}
}

// The ORDINARY timeout pass, which every other test here is blind to.
//
// keys.go runs drainIdle on each timeout and escalates to drainFinal on
// the PasteMarkerGrace'th. The tests above all measure the ESCALATION —
// a split marker that must survive the first pass, a typed prefix that
// must not survive the last — and none of them can tell whether the
// first pass resolves anything at all. Changing `d := drainIdle` to
// `d := drainLive` leaves term, input and the root suite green while
// every Esc, and every other truncated sequence, takes
// PasteMarkerGrace*EscTimeout to arrive instead of EscTimeout: the 40ms
// docs/architecture.md attributes to EscTimeout, silently doubled, on
// every keypress. Raised in review of #445.
//
// This one does need a window, so it takes closedTtyAttempt's
// inconclusive-and-retry discipline rather than asserting on a schedule.
func TestALoneEscResolvesOnTheFirstTimeout(t *testing.T) {
	const attempts = 20
	for i := range attempts {
		if loneEscAttempt(t) {
			return
		}
		t.Logf("attempt %d could not measure the Esc's arrival inside the "+
			"budget; retrying", i+1)
	}
	// EVERY CAUSE NAMED. An exhausted loop here is a machine that cannot
	// be measured, a first pass that no longer resolves anything, or the
	// constant at 0 — the mutation this test exists to catch produces
	// exactly this exit, because its Esc is late on EVERY attempt rather
	// than absent, and so does grace = 0, where no Esc arrives at all.
	// Reporting only the runner would send the next reader to the wrong
	// place. The third was added in review of #445: the spec's zero row
	// lists this test, and a row naming a test that dies through its
	// INCONCLUSIVE path has to say so.
	t.Fatalf("in %d attempts the Esc never arrived inside one escape timeout of "+
		"the write. Three causes, and this path cannot tell them apart: this "+
		"machine is too loaded to distinguish the first idle pass from the "+
		"escalated one; or the first pass has stopped resolving anything "+
		"(keys.go's `d := drainIdle`) and every Esc now costs "+
		"PasteMarkerGrace*EscTimeout; or PasteMarkerGrace is 0, in which case "+
		"that product is 0ms and the sentence before it is nonsense — the "+
		"escape timeout has stopped existing rather than firing late, and "+
		"TestPasteMarkerGraceHasAFloor's zero arm is the message to read",
		attempts)
}

// loneEscAttempt returns false when the attempt could not be made inside the
// window, never a pass — the same discipline splitMarkerAttempt and
// closedTtyAttempt use, so a stalled runner cannot turn "we never measured
// it" into green.
func loneEscAttempt(t *testing.T) bool {
	t.Helper()
	master, slave := openPTY(t)
	s := FromFile(slave)
	if err := s.Raw(); err != nil {
		t.Fatalf("raw: %v", err)
	}
	defer func() {
		s.Restore()
		// Its sibling in closedTtyAttempt carries the reasoning.
		if s.DecoderLeaked() {
			t.Errorf("the decoder was still reading the tty %v after Restore "+
				"returned", DecoderTimeout)
		}
		master.Close()
	}()
	evs := s.Events(16)

	// Handshake and Esc in ONE write, so `wrote` is a clock provably not
	// after the arm — the decoder cannot arm a timer for bytes it has
	// not read. The budget below is measured from it for that reason,
	// the same way splitMarkerAttempt's is.
	wrote := time.Now()
	if _, err := master.Write([]byte("b\x1b")); err != nil {
		t.Fatalf("write to master: %v", err)
	}
	if ev := next(t, evs, "the decoder never delivered a keystroke, so this test "+
		"is measuring a decoder that never lived"); !ev.IsKey() || ev.Key.Rune != 'b' {
		t.Fatalf("got %#v, want the 'b' we typed", ev)
	}
	held := time.Now()
	// The same early bail splitMarkerAttempt takes, for the same reason:
	// an attempt already a quarter-timeout behind at the handshake has
	// spent the budget's margin before the Esc can even be read.
	if held.Sub(wrote) > EscTimeout/4 {
		return false // attribute nothing to the decoder
	}

	// ONE AND A HALF TIMEOUTS from `wrote`, which is what separates the
	// two passes. A healthy decoder emits the Esc at arm+EscTimeout; the
	// mutation cannot emit before arm+PasteMarkerGrace*EscTimeout. 60ms
	// sits between arm+40ms and arm+80ms with margin on both sides.
	//
	// THE QUARTER-TIMEOUT IS A MARGIN, NOT A BOUND ON THE ARM, and this
	// comment claimed the bound until round sixteen. The bail two lines
	// up compares `held` — the moment THIS goroutine read the 'b' off a
	// BUFFERED channel — against `wrote`, so it bounds held-wrote and
	// nothing else. The arm is set by the decoder AFTER its send: the
	// `out <- ev` inside drain's success arm, then the conditional
	// `timer.Reset(EscTimeout)` in DecodeEvents' loop. A deschedule
	// between the two puts the arm arbitrarily far after `held`
	// and after `wrote` with the bail seeing none of it. `arm >= wrote`
	// is derivable and is the LOWER bound; the budget's soundness needs
	// the upper one. splitMarkerAttempt and partialProgressAttempt
	// both say exactly this about their own clocks; this was the third
	// instance and the only one still asserting the magnitude.
	//
	// THE RESIDUE IS INCONCLUSIVENESS, NOT A VACUOUS PASS, which is the
	// one way this helper differs from its two siblings and is worth
	// stating in the same breath. A large deschedule pushes a HEALTHY Esc
	// past wrote+60ms, nextOrNone times out, and the attempt returns false
	// — so the loop retries, and twenty of them exhausting is a Fatal
	// naming both causes. It cannot go the other way: the drainLive
	// mutation emits no earlier than arm+2*EscTimeout >= wrote+80ms, which
	// is outside this budget however the arm drifted, so no amount of
	// scheduling noise turns the mutation green here. The spec carries
	// this beside splitMarkerAttempt's and partialProgressAttempt's.
	ev, got := nextOrNone(evs, EscTimeout+EscTimeout/2-time.Since(wrote))
	if !got {
		return false // late; it may be the machine, and a retry is expected to say
	}
	if !ev.IsKey() || ev.Key.Key != input.KeyEsc {
		t.Fatalf("first event after a lone Esc was %#v, want the Esc key", ev)
	}
	return true
}

// The cheap deterministic backstop for the same property. The pty test above
// is the real pin — it fails on the BEHAVIOUR — but it needs a pty and a
// window, and this one needs neither, so a machine that cannot run the first
// still cannot lower the constant unnoticed.
func TestPasteMarkerGraceHasAFloor(t *testing.T) {
	if PasteMarkerGrace < 2 {
		// TWO CONSEQUENCES, NAMED SEPARATELY, because 1 and 0 break
		// different things and a reader who lands on 0 needs the one that
		// describes what they actually did. This message used to state
		// only the value-1 story, next to a term suite that would also be
		// failing on timeouts — sending them looking for a stranded paste
		// marker when what they removed was the escape timeout.
		if PasteMarkerGrace < 1 {
			t.Fatalf("PasteMarkerGrace is %d. At 0 the re-arm condition in "+
				"DecodeEvents (`stalls < PasteMarkerGrace`) is false on the "+
				"first iteration, so timer.Reset is never reached and the one "+
				"arming time.NewTimer did is consumed by the Stop above it: "+
				"the escape timeout stops existing, and a lone Esc, a "+
				"truncated ESC O and a half-written CSI are held for the life "+
				"of the process. That is not the value-1 failure below; it is "+
				"worse, and it has a different cause.", PasteMarkerGrace)
		}
		t.Fatalf("PasteMarkerGrace is %d. Below 2 the FIRST idle timeout "+
			"resolves a split paste marker to Esc — which is exactly what "+
			"`idle` already means, so the grace stops existing and #419 "+
			"returns: a paste whose opening marker straddles a read arrives "+
			"as a stray Esc followed by its payload as keystrokes. Raising it "+
			"is a trade (see the constant's doc); lowering it past 2 is not.",
			PasteMarkerGrace)
	}
}

// TestPartialProgressGivesTheRemainderItsOwnGrace is the THIRD line the
// conditional re-arm made load-bearing, and the only one of the three
// whose deletion ships #419 rather than #440.
//
//	before := len(pend)
//	…
//	drain(d)
//	if len(pend) != before {
//	    stalls = 0        // ← delete this and the whole tree stays green
//	}
//
// drainIdle can make PARTIAL progress: it resolves something and leaves
// a marker prefix behind. The canonical shape is a dangling Esc followed
// immediately by a split marker in one read — `ESC` `ESC [ 2` —
// where decodeEsc's nested-escape arm consumes the LEADING Esc under
// idle and leaves `ESC [ 2` in pend. That is progress, so the prefix is
// entitled to its own full grace. Without the reset it inherits a
// counter already at 1, and the next timeout is the PasteMarkerGrace'th:
// drainFinal resolves the prefix to Esc and the payload arrives as the
// keystroke burst mode 2004 exists to prevent.
//
// A/B measured on a pty before this test existed: with the reset, one
// PasteEvent carrying the payload; without it, the first event is the
// Esc key, three times out of three, while `go test ./term/ ./input/ ./`
// stayed green. Raised in review of #445.
//
// The spec's "Every clause but one is pinned" sentence named only the
// conditional re-arm as the exception; it is corrected in the same
// commit as this test.
func TestPartialProgressGivesTheRemainderItsOwnGrace(t *testing.T) {
	const attempts = 40
	for i := range attempts {
		if partialProgressAttempt(t) {
			return
		}
		t.Logf("attempt %d could not land the tail inside the remainder's own "+
			"grace window; retrying", i+1)
	}
	// BOTH CAUSES NAMED, for the reason TestALoneEscResolvesOnTheFirstTimeout
	// gives: the mutation this test exists to catch resolves the prefix
	// EARLY on every attempt rather than never, so an exhausted loop is
	// as likely to be the defect as the runner.
	t.Fatalf("could not measure the remainder's grace in %d attempts. Three "+
		"causes, and this path cannot tell them apart: this machine is too "+
		"loaded to place a write inside a 40ms window; or the partial-progress "+
		"reset in keys.go (`if len(pend) != before { stalls = 0 }`) is gone and "+
		"the prefix left behind by an idle pass is resolved on the very next "+
		"timeout — which is #419, a real paste torn into keystrokes; or "+
		"PasteMarkerGrace is 0, in which case the escape timeout has stopped "+
		"existing rather than firing early and TestPasteMarkerGraceHasAFloor's "+
		"zero arm is the message to read. The third was added in review of "+
		"#445: the spec's zero row lists this test, and a row naming a test "+
		"that dies through its INCONCLUSIVE path has to say so", attempts)
}

// partialProgressAttempt returns false when the attempt could not be made
// inside the window, never a pass — the discipline splitMarkerAttempt and
// loneEscAttempt use.
//
// THE CLOCK IS THE ESC'S ARRIVAL, not the write, because the remainder's
// timer is armed after the pass that produced the Esc. `wrote` is a
// lower bound on that arm at one remove: the decoder's timer cannot fire
// before its own deadline, so the Esc's SEND is at or after
// wrote+EscTimeout, and the arm is at or after the send. That is what
// makes the drift bail below derivable rather than assumed — if the Esc
// is read within EscTimeout+EscTimeout/4 of `wrote`, it is read within
// EscTimeout/4 of the send.
func partialProgressAttempt(t *testing.T) bool {
	t.Helper()
	master, slave := openPTY(t)
	s := FromFile(slave)
	if err := s.Raw(); err != nil {
		t.Fatalf("raw: %v", err)
	}
	defer func() {
		s.Restore()
		// Its sibling in closedTtyAttempt carries the reasoning.
		if s.DecoderLeaked() {
			t.Errorf("the decoder was still reading the tty %v after Restore "+
				"returned", DecoderTimeout)
		}
		master.Close()
	}()
	evs := s.Events(16)

	// Handshake, dangling Esc and marker prefix in ONE write.
	//
	// WHAT READING THE 'b' BACK PROVES is that the decoder consumed A
	// read — not that it consumed THIS WHOLE WRITE. This said the whole
	// of `\x1b\x1b[2` is in pend when the timer arms, which is the
	// inference closedTtyAttempt in this same file already retired.
	//
	// AND THE RESIDUE RUNS TOWARD GREEN, which is why it is not only a
	// wording matter: if the slave returns `b\x1b` and `\x1b[2`
	// separately, the lone Esc resolves on its own first timeout —
	// unmodified, so the premise assertion below still passes — pend
	// empties, stalls is 0, and the marker prefix then arrives on the
	// chunks branch with a full fresh grace whether or not the
	// partial-progress reset exists. The tail completes the paste, the
	// attempt returns a vacuous true and stops the retry loop, with the
	// mutation green. The drift bail does not catch it: the Esc still
	// lands at about wrote+EscTimeout. Unlikely — four bytes is
	// essentially always one read — and recorded in the spec's residue
	// list for this helper rather than asserted away. Raised in review
	// of #445.
	wrote := time.Now()
	if _, err := master.Write([]byte("b\x1b\x1b[2")); err != nil {
		t.Fatalf("write to master: %v", err)
	}
	if ev := next(t, evs, "the decoder never delivered a keystroke, so this test "+
		"is measuring a decoder that never lived"); !ev.IsKey() || ev.Key.Rune != 'b' {
		t.Fatalf("got %#v, want the 'b' we typed", ev)
	}

	// THE FIRST IDLE PASS, which is the partial progress itself: the
	// leading Esc resolves and `ESC [ 2` stays in pend. If this does not
	// arrive the premise is gone, and that is a fact about the decoder
	// rather than about the machine — but a late one is the machine, so
	// a miss is inconclusive rather than a failure.
	ev, got := nextOrNone(evs, EscTimeout+EscTimeout/2-time.Since(wrote))
	if !got {
		return false // late; a retry is expected to say whether it is the machine
	}
	if !ev.IsKey() || ev.Key.Key != input.KeyEsc || ev.Key.Mods != 0 {
		// THE MODIFIER IS HALF THE PREMISE, and reading only the key
		// let the premise pass over the state it exists to exclude.
		// Measured on this tree:
		//
		//	Decode("\x1b\x1b[2", true):  n=1 ok=true key=KeyEsc mods=0
		//	DecodeFinal("\x1b\x1b[2"):   n=2 ok=true key=KeyEsc mods=ModAlt
		//
		// Under the FINAL deadline the nested-escape arm's inner decode
		// succeeds, so both escapes are consumed as one Alt+Esc and
		// `[ 2` is what the decoder then re-reads — nothing is left
		// behind as a marker prefix. ev.Key.Key is KeyEsc either way.
		//
		// That is precisely the state PasteMarkerGrace = 1 reaches: at
		// 1 the first timeout is already the escalated pass. Without
		// the modifier the mutation was waved through here and died
		// ~400ms later at the tail assertion, which names neither the
		// constant nor #419 while the two messages written to name
		// them went unused — a kill landed by accident, which is the
		// standard this record applies to the grace = 0 row. Raised in
		// review of #445.
		t.Fatalf("the first event after `b ESC ESC [ 2` was %#v, want an "+
			"UNMODIFIED Esc key — the nested-escape arm under the idle "+
			"deadline is what leaves the marker prefix behind, and without "+
			"it this test measures nothing. An Alt-modified Esc means the "+
			"first pass was already the escalated one, which is what "+
			"PasteMarkerGrace = 1 makes of it (term/keys.go)", ev)
	}
	escAt := time.Now()
	if escAt.Sub(wrote) > EscTimeout+EscTimeout/4 {
		return false // escAt may be well past the send; attribute nothing
	}

	// INSIDE THE REMAINDER'S OWN WINDOW. The prefix is entitled to
	// (arm+EscTimeout, arm+2*EscTimeout), and escAt is bounded ABOVE by
	// EscTimeout/4 past the arm — that is the drift bail two lines up,
	// and it is derived. Below it is not bounded: `arm2 - escAt` is the
	// decoder's deschedule between the send and the Reset, and nothing
	// this attempt measures caps it. splitMarkerAttempt's comment
	// establishes the DIRECTION (the arm can land either side of the
	// event this clock reads), not a magnitude, and this sentence
	// claimed the magnitude from it.
	//
	// The quarter-timeout at the bottom is therefore a MARGIN, not a
	// bound: it clears the first pass when escAt is a quarter-timeout
	// LATE (arm+EscTimeout is already behind us) and when it is a
	// quarter-timeout EARLY (the write lands at arm+EscTimeout, the
	// boundary the budget below keeps off). The first pass is what the
	// mutation makes fatal, so the lower end is the half that must not
	// be cut fine.
	//
	// THE RESIDUE, written down rather than claimed away, the same way
	// splitMarkerAttempt's is: a deschedule larger than EscTimeout/4
	// between the send and the Reset puts the tail before
	// arm2+EscTimeout, the remainder never survives a timeout, the
	// paste completes and this attempt returns TRUE having exercised
	// nothing — a vacuous pass that stops the retry loop, under the
	// mutation as well as under the fix. The spec carries it beside
	// splitMarkerAttempt's. Raised in review of #445.
	//
	// IT IS SHORTER THAN splitMarkerAttempt'S, and deliberately: that
	// helper's clock is `wrote`, which is provably not after the arm,
	// and this one's is escAt, which may be a quarter-timeout past it.
	// The budget below has to leave room for that drift AND for the
	// decoder's read, so the sleep gives back what the weaker anchor
	// costs. The first draft slept 1.5 timeouts against a 1.5-timeout
	// budget and every attempt bailed — an inconclusive test that reads
	// exactly like a loaded machine.
	time.Sleep(EscTimeout + EscTimeout/4)
	if _, err := master.Write([]byte("00~payload\x1b[201~")); err != nil {
		t.Fatalf("write to master: %v", err)
	}
	// ONE BUDGET measured from escAt, covering the sleep and the write
	// together — the stacking argument splitMarkerAttempt's budget
	// comment sets out, with escAt in the place of `wrote`. A quarter
	// timeout of the window is left for the decoder to read the bytes,
	// and a further quarter for escAt's own drift past the arm.
	if elapsed := time.Since(escAt); elapsed >= 2*EscTimeout-EscTimeout/2 {
		return false // the remainder's grace may already have expired
	}

	ev = next(t, evs, "no event arrived after the marker's tail")
	if !ev.IsPaste() {
		// INCONCLUSIVE RATHER THAN #419, and this helper was written
		// after splitMarkerAttempt got the same bail and did not
		// inherit it. The budget above bounds when master.Write
		// RETURNS, not when the decoder READS: it admits an attempt at
		// escAt+60ms, and the remainder's grace does not expire until
		// arm2+2*EscTimeout, which is at least escAt+80ms — so the
		// decoder goroutine may have as little as ~20ms to be
		// scheduled and take the tail. Descheduled past that, the tail
		// lands after the grace, the prefix resolves to Esc, and the
		// Fatalf below announces a real paste torn into keystrokes
		// about a decoder doing exactly what it should — which the
		// retry loop cannot absorb, because a Fatalf is not a false
		// return.
		//
		// THE ARRIVAL TIME STILL SEPARATES THE MUTATION, which is why
		// the bail can be a threshold rather than a surrender. The
		// decoder sends before it re-arms, so arm2 >= escAt. Healthy,
		// the Esc is emitted when the remainder's own grace expires at
		// arm2+2*EscTimeout, so no earlier than escAt+80ms. Under the
		// deleted reset it is emitted at arm2+EscTimeout, and for this
		// branch to be reached at all the tail must be read after
		// arm2+EscTimeout while the write returned before escAt+60ms —
		// which puts the mutation's Esc at about escAt+60ms or less
		// whenever the decoder reads promptly. 2*EscTimeout-EscTimeout/4
		// is 70ms, the midpoint of the two.
		//
		// The cost, stated: the same concession splitMarkerAttempt's bail
		// makes. A deschedule long enough to push the mutation's own Esc
		// past 70ms turns a real kill into an inconclusive attempt, so the
		// #419 kill through THIS branch is probabilistic across the retry
		// loop rather than certain on one attempt. It is not the pin on
		// PasteMarkerGrace itself — grace = 1 is caught at the
		// unmodified-Esc premise far above, which this bail is nowhere
		// near.
		if ev.IsKey() && ev.Key.Key == input.KeyEsc &&
			time.Since(escAt) >= 2*EscTimeout-EscTimeout/4 {
			return false // the remainder's grace had already expired
		}
		if ev.IsKey() && ev.Key.Key == input.KeyEsc {
			t.Fatalf("the remainder of a partially-drained buffer was resolved " +
				"to Esc rather than held for its own grace: the idle pass that " +
				"consumed the leading Esc left `ESC [ 2` in pend and made " +
				"progress, so the prefix is entitled to a full " +
				"PasteMarkerGrace of its own. It inherited a spent counter " +
				"instead, and this paste arrived as a stray Esc followed by " +
				"its payload as keystrokes — which is #419")
		}
		t.Fatalf("event after the marker's tail was %#v, want a PasteEvent", ev)
	}
	if p := ev.Paste; p.Text != "payload" {
		t.Errorf("pasted text is %q, want %q", p.Text, "payload")
	}
	return true
}
