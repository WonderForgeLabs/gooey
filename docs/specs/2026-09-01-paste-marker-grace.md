# The paste-marker grace gets a bound

Status: implemented
Date: 2026-09-01
Issue: [#440](https://github.com/WonderForgeLabs/gooey/issues/440)
Follows: [specs/2026-08-23-clipboard-and-bracketed-paste.md](2026-08-23-clipboard-and-bracketed-paste.md)

## The problem

`ESC [ 2` is two things at once. It is a strict prefix of the
bracketed-paste marker `ESC [ 200 ~`, and it is three keys a person can type:
Escape, then `[`, then `2`.

[#425](https://github.com/WonderForgeLabs/gooey/pull/425) taught `decodeCSI` to
hold such a prefix rather than resolve it to Esc, because resolving it broke a
paste whose marker straddled a read — [#419](https://github.com/WonderForgeLabs/gooey/issues/419),
where the payload then arrived as the keystroke burst mode 2004 exists to
prevent. The justification is in the code: *"the terminal is mid-write: the rest
of the marker and its payload are already on the wire."*

That holds for a paste. It does not hold for the typing, and in the typing case
the hold is **permanent**:

- the Esc is never delivered;
- the next unrelated keystroke is appended behind the prefix and absorbed into
  the CSI parse — `ESC [ 2 z` is one unmapped four-byte sequence and **no event
  at all**;
- and `term.DecodeEvents` re-arms its `EscTimeout` timer on every loop iteration
  while `len(pend) > 0`, so the decoder goroutine wakes every 40ms for the rest
  of the process's life.

Under the pre-#425 absolute contract `drain(true)` always emptied `pend`, so the
timer was never re-armed and this state could not exist. Found in the seventh
review of #425 and left there because of the API question below; #425 has since
merged, so it is live on main.

**Nothing in the suite could observe it.** `decodeidle_test.go`'s exhaustive
sweep is over 1- and 2-byte inputs, and `splitPasteMarker` has a **three-byte
floor** — so the range that strands was outside every liveness assertion the
package had, and a buffer that waits forever looked exactly like one waiting
correctly for one timeout.

## The decision

Resolve after the **second** consecutive idle timeout. That keeps the paste case
whole — a real marker's payload lands within one read cycle — and restores
liveness for the typed one.

The issue put the open question as *where the counter lives*, and gave two
answers, both with a real cost:

1. **In `term/keys.go`**, where the loop already knows a timeout fired and
   whether `pend` shrank — but resolving the stranded prefix there means `term`
   reimplementing what `decodeCSI` would have done, against this package's
   standard that the walk belongs to the package that owns it.
2. **In `input.Decode`**, by widening it to a third state — cleaner
   semantically, but it changes a public signature and every caller and test
   moves with it.

**Taken: 2's semantics with 1's blast radius.** The counter lives in the loop,
which is the only place that can count timeouts; the *decoding* lives in `input`,
behind a second entry point rather than a widened signature:

```go
func DecodeFinal(b []byte) (Event, int, bool)
```

Internally the two share one walk. `Decode`'s `idle bool` becomes an unexported
three-valued `deadline` (`live`, `idled`, `final`) threaded through `decodeEsc`
and `decodeCSI`; every arm but one asks `d.idle()` and cannot tell the
difference. The one that can is the marker grace, which now reads
`d == idled` rather than `idle`.

Nothing outside the package moves. `Decode(b, idle bool)` keeps its signature,
its meaning and its 40-odd existing call sites, three of which are in `apps/`.

### Why a second function rather than a third enum value

Because the difference between them is *one exception being withdrawn*, and a
named function says that where an enum value would only imply it. It also makes
the contract statable as its own sentence — `DecodeFinal` never answers
"incomplete" except for an open paste — which is what the new exhaustive test
asserts. Stating the exception rather than the absolute is the lesson
`Decode`'s own doc comment already records, having once claimed "never
(0, false)" flat while `decodePaste` had already departed from it.

### What is NOT on this scale

An **open paste** — marker complete, payload's end marker not yet arrived —
still waits however long it takes, under `DecodeFinal` as under `Decode`. That
is a different exception with a different reason, recorded in
[the clipboard spec](2026-08-23-clipboard-and-bracketed-paste.md): delivering the
prefix silently TRUNCATES the paste, and a user who pastes 40KB and receives 8KB
has no way to tell, where a wedge is at least visible. `DecodeFinal` must not
resolve it, and `TestFinalDecodeStillWaitsForAnOpenPaste` is why it cannot drift
into doing so.

### The number

`term.PasteMarkerGrace = 2`, so the window is 80ms, and it is named because it
is a trade with a loser either way rather than a tuning constant.

It names **which** timeout resolves the buffer, not how many the buffer
survives — the loop increments `stalls` and escalates to `drainFinal` once it
reaches this value, so at 2 the buffer is held through
exactly **one** timeout and resolved **on** the second. Worth stating precisely
because it is off by one from the natural reading: someone raising it to 3 to
buy one more grace period gets two. (Raised in review of
[PR #445](https://github.com/WonderForgeLabs/gooey/pull/445), where the
constant's own doc comment had the looser phrasing.)

Lower is not available: at 1 the FIRST timeout resolves, which is what `idle`
already means, and the grace would not exist. Higher buys a marker split across
a slower link, at the price of the Esc key taking that long to arrive and of
the deaf window being that much wider if something new ever lands in this
shape.

The residual risk, stated rather than waved at: a genuine marker split across
reads **more than two escape timeouts apart** resolves to Esc and its payload
arrives as keystrokes, which is #419's symptom returning. Going deaf forever is
the worse half of that trade, and 80ms of silence inside a six-byte marker the
terminal is actively writing is not a case anyone has observed.

### Half of the reported symptom survives, and it is worth being blunt

#440 reported **two** symptoms of one hold. This change fixes the first
completely and the second only partly:

1. *the buffer strands forever* — fixed, unconditionally;
2. *the next keystroke is absorbed into the CSI parse* — fixed only for a key
   arriving **after** the window.

A key arriving **inside** the window is still absorbed, because the window is
precisely the interval in which the decoder is still waiting for more bytes,
and the key is more bytes. Measured on a pty against this branch — write
`ESC [ 2`, wait `EscTimeout/2`, write `z`:

```
total events after ESC [ 2 then z inside the window: 0
```

Zero. The Esc is lost and `[`, `2`, `z` are swallowed together as one unmapped
four-byte CSI. Outside the window the same sequence correctly yields Esc, `[`,
`2`, `z`.

Closing it means the decoder distinguishing "this CSI began as a marker prefix
I was holding" from "this CSI arrived whole", which is **state**, and `input`
is deliberately a stateless function over bytes — the same constraint recorded
on `decodePaste` about its quadratic re-scan. That is a change to the decoder's
shape rather than to this grace, so it is
[#447](https://github.com/WonderForgeLabs/gooey/issues/447) rather than a rider
here. Raised in review of PR #445, which is also where the "no record says so"
version of this section was correctly called out.

## The second bug in the same loop

While `pend` is non-empty the loop re-armed its timer unconditionally. Once the
last-chance pass has run and `pend` survived it — which now means an open paste
and nothing else — no deadline can change anything, and only a new byte can. The
re-arm is now conditional on `stalls < PasteMarkerGrace`, so the wedge no longer
also burns a wakeup every 40ms.

That clause is **not independently pinned**, and saying so is better than
implying otherwise: the only buffer that reaches it is the open paste, whose
wedge is by design, so the difference is a timer that fires pointlessly versus
one that does not. Nothing observable to the app changes. It is included because
it is the other half of the same sentence in the report, not because a test
demanded it.

## Verification

`input/decodefinal_test.go`, and four new tests in `term/strand_linux_test.go`
— one per route to the last-chance pass, one for the constant's behaviour and a
deterministic floor under it.

**Every clause but one is pinned**, and the exception is named in the table
rather than glossed: the conditional re-arm turns nothing red. An earlier
version of this sentence said "every clause is pinned" directly above the row
reporting that, which is the kind of contradiction a reader resolves by
trusting the prose. Mutation-tested, each mutation turning its own tests red:

| mutation | what goes red |
|---|---|
| the grace is never withdrawn (`d == idled` → `idle`) | `TestFinalDecodeResolvesTheTypedMarkerPrefix`, `TestFinalDecodeMakesProgressOnNestedEscapes`, `TestFinalDecodeHoldsNoMarkerPrefix`, and the term strand test |
| `DecodeFinal` becomes a synonym for `Decode(b, true)` | the same four |
| the loop never escalates to the final pass | the term strand test alone |
| the stall counter resets on every timeout instead of counting | the term strand test alone |
| the tty-close path drops to the idle deadline | `TestAClosedTtyResolvesAHeldPrefixBeforeTheDecoderExits` alone |
| `PasteMarkerGrace` lowered from 2 to 1 | `TestASplitPasteMarkerStillPastes` and `TestPasteMarkerGraceHasAFloor` |
| the timer is re-armed unconditionally | **nothing** — the honest result, and the one the section above predicts |

The tty-close row is the reason `drainFinal` is defined as "nothing more can
arrive" and not as "the stall count reached `PasteMarkerGrace`". That path
reaches the last-chance pass with **zero** timeouts elapsed, and it had no test
until review of #445 asked what the precondition actually was: every other test
in the tree reaches the final pass through the timer, so the arm could have
quietly dropped to `drainIdle` and lost the last keystrokes a user typed before
the terminal went away, with nothing to notice.

That test needs a **handshake** rather than a sleep, and the first draft
without one measured nothing: closing the pty master can discard bytes the
slave has not read yet, so "write the prefix, then close" loses it on most
runs. Writing `b` and the prefix in ONE write and reading the `b` back proves
the decoder consumed that read, which means the prefix is in `pend` at the
moment the tty is closed.

It also needs to know **whether it measured anything**, which the second draft
did not. If the runner stalls past the grace window, the timer resolves the
prefix before the close, the Esc arrives either way, and the test passes while
guarding nothing — green and blind, which is worse than red. The discriminator
is arrival TIME from the moment the decoder is known to be holding: the stall
path cannot deliver before `PasteMarkerGrace × EscTimeout` has elapsed, so an
Esc inside that budget can only be the close. Outside it the attempt is
**inconclusive** and retried; exhausting the retries fails. Raised in review of
PR #445, which is the second time on this branch that a test needed to be
stopped from passing for the wrong reason.

The mutation harness itself has to be watched, and this one caught it out. The
targets must carry their leading TABS so they can only match a statement. The
doc comment on `PasteMarkerGrace` USED TO quote the escalation line verbatim —
it describes it in prose now, so a reader checking this lesson against the code
will not find the quote — and while it did, a bare substring replace hit the
COMMENT and left the code intact — reporting
"the loop never escalates" as a mutation no test caught, when in fact no
mutation had happened. A harness that silently mutates nothing grades every
test as a passing pin. The tell was the timing: 81ms to the Esc, which is two
escape timeouts, i.e. exactly the behaviour the mutation was supposed to have
removed.

The split is deliberate: the `input` tests cannot see whether the *loop* ever
asks for the final pass, and the `term` test cannot see which byte sequences the
decoder is allowed to hold. Neither substitutes for the other.

`TestFinalDecodeResolvesTheTypedMarkerPrefix` asserts the grace still holds for
the FIRST timeout before asserting the second resolves it, so a fix that removed
the exception instead of bounding it fails there rather than passing every
liveness check in the file.

The term test lives in `term` for the reason its neighbour
`TestEscBeforeAMouseReportDoesNotStrandTheDecoder` gives: only the loop can show
that a decoding contract strands live input, and only a real tty makes the loop
the thing under test. Here the **gap is the fixture** — the three bytes must
arrive in their own read with nothing after them for two timeouts, which is what
a keyboard does and what no single `Write` can fake.

It **waits for the Esc rather than sleeping past the grace**, and that is a
correctness fix rather than a tidy-up. Sleeping cannot make a broken decoder
pass, but it can make a working one fail: if the decoder goroutine is delayed
past the sleep, the trailing `z` lands before the second timeout, `stalls`
resets, and `ESC [ 2 z` decodes as one complete unmapped four-byte CSI emitting
nothing — so the test fails at its deadline with the message for the bug under
test, and a scheduling flake on a shared runner reads as a regression. Waiting
on the channel removes the timing dependency and pins the ORDERING as well as
the outcome, since the Esc arriving is itself the proof that the grace expired
with no new input. Raised in review of #445; measured at 81ms to the Esc, which
is the two timeouts, and stable at `-count=20` and at `-count=10 -cpu=1`.

`TestFinalDecodeMakesProgressOnNestedEscapes` extends the existing sweep in
**two** directions, and the second was missing in the first draft of this
change. Length: three to five is exactly the range `splitPasteMarker` covers
and the old sweep stopped at four. **Alphabet:** the inherited one contains no
`'2'`, and every strict prefix of `\x1b[200~` / `\x1b[201~` needs a `'2'` at
index 2 — so `splitPasteMarker` returned true for **zero** of the sweep's
~1.1M inputs and the grace arm was unreachable at any length.

That is worth recording rather than quietly fixing, because it is the same
defect this record diagnoses one paragraph above — a fixture that cannot
express the bug — reproduced by the fix for it. Extending the length moved the
walk across the right range while leaving it unable to build anything in that
range, and the test's own comment claimed the coverage. Measured in review of
[PR #445](https://github.com/WonderForgeLabs/gooey/pull/445): 0 hits before,
4 after, at a cost of 16⁵ → 17⁵.

The lesson generalizes past this file: when a sweep is widened to cover a new
rule, check that its ALPHABET can spell the rule's inputs, not only that its
lengths reach them.
