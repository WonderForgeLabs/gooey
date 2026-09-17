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

That wakeup state is **older than #425**, and this paragraph used to say the
opposite — "under the pre-#425 absolute contract `drain(true)` always emptied
`pend`". It did not. `decodePaste` returned `(0, false)` for an unterminated
paste whatever `idle` said, in the copy of `input/paste.go` that preceded #425,
and the loop re-armed on any non-empty `pend`; the every-40ms wakeup was
therefore already reachable by an OPEN PASTE, where the hold is deliberate and
no keystroke is lost. What #425 added is a second buffer shape that reaches the
same state — and this one is also three keys a person can type, which is what
turns a designed wedge into deafness. Restating the absolute inside the record
written to correct it is the failure this document is about; raised in review of
#445. Found in the seventh review of #425 and left there because of the API
question below; #425 has since merged, so it is live on main.

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
three-valued `deadline` (`deadlineLive`, `deadlineIdled`, `deadlineFinal`)
threaded through `decodeEsc` and `decodeCSI`; every arm but one asks `d.idle()`
and cannot tell the difference. The one that can is the marker grace, which now
reads `d == deadlineIdled` rather than `idle`.

The names carry the `deadline` prefix because `decodeCSI`'s local for the CSI
final byte is also called `final`, so the bare constant meant one thing above
that declaration and was a type error below it — inside the one function every
arm of this change routes through. Renamed in review of #445.

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

Lower is not available, and **the two values below it fail differently** — so
both are named here rather than left to be derived, because this record is the
document `term/keys.go` and `TestPasteMarkerGraceHasAFloor` point back to.

- At **1** the FIRST timeout resolves, which is what `idle` already means, and
  the grace would not exist. A marker split across two reads resolves to Esc
  and its payload arrives as keystrokes — #419's symptom.
- At **0** the loop's re-arm condition (`stalls < PasteMarkerGrace`) is false on
  the very first iteration, so `timer.Reset` is never reached and the single
  arming `time.NewTimer` did is consumed by the `Stop` above it. **The escape
  timeout stops existing altogether**: a lone Esc, a truncated `ESC O` and a
  half-written CSI are each held for the life of the process. That is a worse
  failure than 1's and it arrives by a different route, which is why stating
  only the value-1 story left a reader who landed on 0 with the wrong
  diagnosis. The mutation table's last row measures it; this is the normative
  statement of it. Raised in review of #445.

Higher buys a marker split across
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
one that does not. Nothing observable to the app changes *by removing the
condition*. It is included because it is the other half of the same sentence in
the report, not because a test demanded it.

**What it does change is somebody else's line.** This paragraph originally
stopped at "nothing observable", and that was honest about the wrong half. The
re-arm's own effect is unobservable; its effect on the stall counter is not.
Gating the re-arm on `stalls < PasteMarkerGrace` makes the counter at its
ceiling an **absorbing state** — the reset inside the timer branch
(`len(pend) != before`) needs the timer already armed, so the only way out is
`stalls = 0` on the chunks branch, a line that meant nothing before this change
and now decides whether the escape timer is ever armed again. Delete it and any
paste taking longer than `PasteMarkerGrace * EscTimeout` to finish leaves the
decoder unable to resolve a lone Esc for the life of the process: #440's
symptom, reached by an ordinary paste. `TestAPasteThatOutlastsTheGraceLeavesTheEscapeTimerArmable`
pins it. Raised in review of #445.

## Verification

`input/decodefinal_test.go`, and new tests in `term/strand_linux_test.go` —
one per route to the last-chance pass, one for the constant's behaviour, a
deterministic floor under it, one for the stall counter's reset on the chunks
branch, one for the ordinary first-timeout pass, and one for the
partial-progress reset inside the timer branch
(`TestPartialProgressGivesTheRemainderItsOwnGrace`). (A count stood here and
was wrong the moment the last two were added; the table below is the list that
cannot go stale without a mutation disagreeing with it. **And then the list
that replaced the count went stale in the next commit but one** — the commit
that added the partial-progress test edited the paragraph five lines below
this one and left the enumeration at six, which is the class this record is
about, arriving in the sentence that argues a list is the safe form. It is not
safe; it is only *cheaper to check*, and the table below is the half that
cannot go quiet. Raised in review of #445.)

**Every clause but one is pinned**, and the exception is named in the table
rather than glossed: the conditional re-arm turns nothing red. An earlier
version of this sentence said "every clause is pinned" directly above the row
reporting that, which is the kind of contradiction a reader resolves by
trusting the prose.

**It was wrong a second time in the same direction.** There were TWO unpinned
clauses, not one: the partial-progress reset inside the timer branch
(`if len(pend) != before { stalls = 0 }`) could be deleted with `./term`,
`./input` and the root suite all green, and its deletion ships #419 rather than
#440 — a real paste torn into keystrokes, in the one buffer shape the grace
exists to protect. `TestPartialProgressGivesTheRemainderItsOwnGrace` pins it
and the row is below. Raised in review of #445, which is the paragraph above
happening again. Mutation-tested, each mutation turning its own tests red:

| mutation | what goes red |
|---|---|
| the grace is never withdrawn (`d == deadlineIdled` -> `idle`) | `TestFinalDecodeResolvesTheTypedMarkerPrefix`, `TestFinalDecodeMakesProgressOnNestedEscapes`, `TestFinalDecodeHoldsNoMarkerPrefix`, `TestATypedPasteMarkerPrefixDoesNotStrandTheDecoder`, `TestAClosedTtyResolvesAHeldPrefixBeforeTheDecoderExits` |
| `DecodeFinal` becomes a synonym for `Decode(b, true)` | the same five |
| the loop never escalates to the final pass | `TestATypedPasteMarkerPrefixDoesNotStrandTheDecoder` |
| the stall counter resets on every timeout instead of counting | `TestATypedPasteMarkerPrefixDoesNotStrandTheDecoder` |
| the tty-close path drops to the idle deadline | `TestAClosedTtyResolvesAHeldPrefixBeforeTheDecoderExits` |
| `PasteMarkerGrace` lowered from 2 to 1 | `TestPasteMarkerGraceHasAFloor` (structural); `TestPartialProgressGivesTheRemainderItsOwnGrace` (BEHAVIOURAL and deterministic - it fails on attempt 1 in ~0.04s at its PREMISE, three runs of three, naming the constant: "An Alt-modified Esc means the first pass was already the escalated one"); `TestASplitPasteMarkerStillPastes` (three runs of three, but see below - a vacuous attempt still pastes) |
| the timer is re-armed unconditionally | **nothing** - the honest result, and the one the section above predicts |
| `stalls = 0` on the chunks branch is deleted | `TestAPasteThatOutlastsTheGraceLeavesTheEscapeTimerArmable` (three runs of three, but see the residue below - a deschedule spanning the sleep makes the attempt vacuous and the mutation green) |
| the first timeout's pass is neutered (`d := drainIdle` -> `drainLive`) | `TestALoneEscResolvesOnTheFirstTimeout` |
| the partial-progress reset in the timer branch is deleted | `TestPartialProgressGivesTheRemainderItsOwnGrace` (three runs of three; the remainder resolves to Esc on the very next timeout) |
| `PasteMarkerGrace` lowered to 0 | `TestPasteMarkerGraceHasAFloor` on its zero arm, plus `TestATypedPasteMarkerPrefixDoesNotStrandTheDecoder`, `TestAPasteThatOutlastsTheGraceLeavesTheEscapeTimerArmable` and `TestALoneEscResolvesOnTheFirstTimeout` - all three on timeouts, because at 0 the escape timeout stops existing rather than firing early. That is a different failure from the value-1 row above, and the reason the floor test carries two messages. `TestPartialProgressGivesTheRemainderItsOwnGrace` reddens too, but by EXHAUSTING its 40 attempts in ~2.5s rather than by asserting - so it is listed with that caveat: its terminal message names the constant as a third cause alongside a loaded runner and a deleted reset, because a test that dies through its inconclusive path has not measured what its name says. `TestASplitPasteMarkerStillPastes` and `TestAClosedTtyResolvesAHeldPrefixBeforeTheDecoderExits` stay green |

That row was landed once already with the right verdict for the wrong
reason, which is the hazard a re-run table exists to catch and did not.
`partialProgressAttempt`'s premise check read only `ev.Key.Key`, and under
the FINAL deadline the nested-escape arm consumes both escapes as one
Alt+Esc - so at grace 1, where the first timeout is already the escalated
pass, the premise passed over exactly the state it exists to exclude and
the test died ~400ms later at its tail assertion, which names neither the
constant nor #419. Requiring `ev.Key.Mods == 0` makes the premise real; the
row above is the re-run of the mutation against it. Measured on this tree:

```
Decode("\x1b\x1b[2", true):  n=1 ok=true key=KeyEsc mods=0
DecodeFinal("\x1b\x1b[2"):   n=2 ok=true key=KeyEsc mods=ModAlt
```

Raised in review of #445.

**Every row is re-derived by running its mutation**, never edited by hand, and
the difference is not cosmetic. Earlier versions said "the term strand test" -
a phrase that named one test when `strand_linux_test.go` held one, and names
none of the several it holds now (`grep -c '^func Test'`) - and undercounted
two rows: withdrawing the grace turns the
tty-close test red as well, since with the grace never withdrawn that route
cannot resolve a held prefix either. A table whose whole value is that it can
be re-run has to be re-run. Corrected in review of #445.

That sentence carried a count of its own - "names none of four now" - and it
was four behind by round sixteen, inside the paragraph whose subject is prose
counts going stale. Nothing about the point needed the number, so it is a
`grep` now. Corrected in review of #445.

### One pty test refuses to pass vacuously; the other narrows the window and names the residue

Both `TestAClosedTtyResolvesAHeldPrefixBeforeTheDecoderExits` and
`TestASplitPasteMarkerStillPastes` need a window: the first needs the tty to
close while the prefix is still held, the second needs the marker's tail to
land after one timeout but inside the grace. A stalled runner misses either,
and a test that merely passes when it does is green and blind — worse than
red. So each measures whether it hit the window, treats a miss as
**inconclusive** and retries.

What happens when the retries run out is not the same on both, and the
difference is the point of `closedTtyAttempt`'s non-measured outcomes — how
many there are is derived from the type, not written here, for the reason
CLAUDE.md gives about counts in prose. `splitMarkerAttempt` has one kind of
miss and the loop simply exhausts. `closedTtyAttempt` distinguishes
**drifted** — abandoned before the close, so no timer was observed and none
may be named — from **late**, an Esc that arrived too late to attribute to
the close rather than to the timer, from **silent**, and silence
after the close *is the regression's own symptom*: a held prefix discarded
instead of drained, so nothing will ever arrive. One silent attempt is not
evidence, because a write split across two slave reads looks identical from
here. Five is a judgement, and it is written down as one: `silentEnough = 5`
is the number of independent split reads the benign explanation needs before
it stops being the better one. At five the loop breaks early — silence is also
the slow outcome, costing a full wait each time — and the failure names
`DecodeEvents`' tty-close arm rather than the runner. Below five it names
neither and says so. Corrected in review of #445, twice: the message once
claimed the regression on a threshold the loop did not break on.

How strong that is differs between the two, and saying "neither can report
success on an attempt it did not make" overstated it for BOTH, in opposite
places. `closedTtyAttempt` measures a real discriminator — an Esc arriving
inside one `EscTimeout` of the handshake cannot have come from the stall
path, which needs `PasteMarkerGrace` full timeouts — so its verdict is a
measurement, but only once the clock it measures from is itself bounded.
`held` is sampled after a receive on a buffered channel and the decoder
sends before it re-arms, so a deschedule of `2*EscTimeout` between the two
lets the stall path escalate and queue the Esc BEFORE the close runs: the
elapsed check then reads ~0, every assertion passes, and the attempt
reports a measurement of the timer path. The guard is the same one the
split-marker helper carries — `wrote` sampled before the handshake write,
and an attempt discarded when `held` is more than a quarter-timeout past
it, which bounds the drift because the decoder cannot arm for bytes it has
not read. Review of #445 gave the split-marker helper that guard one round
before this paragraph gave the stronger guarantee to the helper without
it. `splitMarkerAttempt` has no observable for the first idle
timeout: at `stalls = 1` the decoder holds the prefix and emits nothing, so
nothing on the wire says the timeout fired. What it can do is bound the
window at both ends, and that is what it does — the arm sits within a
scheduling gap of `held` (the decoder sends each event *before* it re-arms,
into a buffered channel, so `held` may land either side of it). The sleep
reserves a quarter of a timeout at the bottom; the top is not a second
reservation but the same one, because the tail budget is measured from
`wrote` and so spends the window the drift guard already draws on. A
sufficiently pathological deschedule of the decoder between the send and the
`Reset` could still let an attempt land early and pass without exercising the
grace: the drift guard bounds `held - wrote`, and `arm >= wrote`, so what it bounds
from above is `held - arm` — the arm landing EARLY. The other direction is
unbounded: an arm landing late leaves the tail arriving while the prefix is
still live. The marker is then never split at the decoder at all - the
whole sequence decodes as one paste, `IsPaste()` holds, and the helper
returns true having exercised nothing.

**So this test is not the structural pin on the constant, and the heading
above used to say it was.** Lowering `PasteMarkerGrace` to 1 does turn it red,
re-measured three runs out of three after the window was rebalanced - but a
vacuous attempt pastes under the mutation too, so that row holds
probabilistically. `TestPasteMarkerGraceHasAFloor` is the deterministic guard
on the constant and is what the mutation table credits first.

`partialProgressAttempt` carries the same residue, and it is written down
here rather than claimed away in the helper. Its clock is `escAt`, the first
idle pass's Esc, which is bounded ABOVE by a quarter-timeout past the arm (the
drift bail) and is not bounded below at all: `arm2 - escAt` is the decoder's
deschedule between the send and the `Reset`, and nothing the attempt measures
caps it. Past `EscTimeout/4` the tail lands before `arm2 + EscTimeout`, the
remainder never survives a timeout, the paste completes and the attempt
returns **true** having exercised nothing - a vacuous pass that stops the retry
loop, under the mutation as well as under the fix. The helper's comment used
to claim the bound held "on either side", citing `splitMarkerAttempt`, whose
comment establishes only the direction. Corrected in review of #445.

`loneEscAttempt` is the third helper with an unbounded arm, and **its residue
runs the other way**, which is why it is worth stating separately rather than
folding into the two above. Its bail compares `held` — this goroutine's read
off a buffered channel — against `wrote`, so it bounds `held - wrote` and says
nothing about `arm - wrote`; the decoder sends before it re-arms
(`term/keys.go:179`, then `:203`), so a deschedule in between puts the arm
arbitrarily late. A healthy Esc then arrives past the 60ms budget,
`nextOrNone` times out and the attempt returns **false**: the loop retries, and
twenty exhausting is a Fatal that names both causes. It cannot fail the other
way, and that is derivable rather than hoped for — the `drainLive` mutation
emits no earlier than `arm + 2*EscTimeout >= wrote + 80ms`, outside this budget
however the arm drifted. So the residue here is a red suite on a loaded
machine, never a vacuous green. The helper's comment claimed the bound until
round sixteen; corrected in review of #445.

`TestAPasteThatOutlastsTheGraceLeavesTheEscapeTimerArmable` is the fourth,
and it is the one that claimed it had no residue at all — "the assertions
below hold however the machine is scheduled". They do; that is not the
question. Its precondition is not elapsed time but that the DECODER read
`ESC [ 200 ~ hello` and then saw `PasteMarkerGrace` timeouts with nothing
new on the wire, and the test has no observable for it: an open paste
emits nothing, which is exactly the property that makes it the buffer
able to drive `stalls` to the ceiling, and the handshake byte is read
back before the paste is written. A deschedule of the decoder spanning
the sleep lands both writes in one read; the paste completes at once,
`stalls` never leaves 0, and the test is green with the chunks branch's
`stalls = 0` deleted — the mutation it is the sole pin for. Like
`loneEscAttempt`'s the residue runs one way only, but the other way
round: never a red suite, and a vacuous green instead. Stating it is the
whole remedy available — there is nothing to bound, because there is
nothing to measure. Raised in review of #445, round seventeen.

**And the sentence that followed this is retired, by a test the same branch
wrote.** It said there is no cheap observable for "the first idle timeout
fired" - nothing is emitted at `stalls = 1` - so no symmetric guard was
available. `TestPartialProgressGivesTheRemainderItsOwnGrace` is one: at
`PasteMarkerGrace = 1` the remainder's own grace is the FIRST timeout, so the
mutation resolves the prefix to Esc and the test reaches its PREMISE and fails
in ~0.04s, three runs of three - a behavioural kill on the constant that does
not depend on an attempt being non-vacuous. It landed that kill for the wrong
reason for one round: the premise read only `ev.Key.Key`, which an Alt+Esc
satisfies, so at grace 1 the test fell through to a tail assertion ~400ms
later that names neither the constant nor #419. `ev.Key.Mods == 0` is what
makes the premise the thing that fires. The conclusion was written
before the test existed and was left standing by the commit that created its
counterexample. Corrected in review of #445; the half-claim in the heading was
corrected in round eleven.

Getting the measurement itself right took the run of corrections below, all
from review. There is no count in that sentence on purpose: it said *seven*
while two of these bullets were the same correction written twice, and a
number in prose cannot notice that it has stopped matching the list under
it. Count them if you want the figure.

- **A handshake, not a sleep.** Closing the pty master discards bytes the
  slave has not read, so "write the prefix, then close" loses it on most runs.
  Writing `b` and the prefix in ONE write and reading the `b` back proves the
  decoder consumed that read.
- **Slack in BOTH directions the clock can drift.** `held` is sampled after a
  buffered-channel receive and the decoder re-arms its timer after that send,
  so `held` can land on either side of the arm: late if the test goroutine is
  descheduled, early if the decoder is. `closedTtyAttempt` budgets **one
  `EscTimeout`** rather than the full stall latency for the late case — the
  wider budget let a timer-delivered Esc measure just under it and be credited
  to the close. `splitMarkerAttempt` reserves a quarter of a timeout at the
  bottom and takes the top out of one shared budget; its comment asserted the
  ordering in the opposite direction to its sibling's until review of #445,
  and a guarantee a file states two ways is worth less than the slack it was
  defending.
- **The drift bounded by a clock that cannot drift with it.** Both helpers
  checked the drift with `time.Since(held)` — measured from `held`, which IS
  the drifting clock, so it could not see the drift and bounded only the tail
  write's latency. Each now samples `wrote` immediately before the handshake
  write and discards an attempt where `held` is more than a quarter-timeout
  past it: the decoder cannot arm a timer for bytes it has not read, so
  `arm >= wrote`, and `held - wrote` bounds `held - arm` from above. Without
  it a deschedule of more than three quarters of a timeout between the
  decoder's buffered send and the sample made `splitMarkerAttempt`
  **hard-fail with the #419 message**, which the retry loop cannot absorb —
  measured both ways by injecting the deschedule: with the guard the attempt
  goes inconclusive and retries, without it the #419 message fires on a
  healthy decoder. `splitMarkerAttempt` got this one round before
  `closedTtyAttempt` did, which left the record asserting the stronger
  guarantee for the weaker of the two.
- **The budget bounds the WRITE, and the read needed its own answer.** The
  guard above admits an attempt on `time.Since(wrote) < 2*EscTimeout -
  EscTimeout/4`, which bounds when `master.Write` **returns**; the grace
  expires at `arm + 2*EscTimeout` with `arm >= wrote`. So an attempt can be
  admitted with as little as `EscTimeout/4` left for the decoder goroutine to
  be *scheduled* and read the tail — and descheduled past that, a healthy run
  reached the `!ev.IsPaste()` arm, which was a `t.Fatalf` the retry loop
  cannot absorb. The same false-cause shape as the bullet above, moved from
  the drift to the read, and not hypothetical on shared self-hosted pools: a
  sleep overshoot of ~18ms still passes the 70ms budget and leaves ~2ms of
  read headroom. The discriminator is the Esc's ARRIVAL TIME — under the
  `PasteMarkerGrace = 1` mutation it is emitted at ≈ `arm + EscTimeout` ≈
  `wrote + 40ms`, *before* the tail write, so it comes back well under
  `2*EscTimeout`; a loaded-but-healthy decoder cannot produce one before the
  grace expires. An Esc at or past `2*EscTimeout` is now inconclusive and
  retries. **The cost, stated:** this makes the `PasteMarkerGrace = 1` kill
  probabilistic in the same way the row below already concedes — measured
  green on three consecutive mutated runs, with the #419 message still the
  one that fires — and `TestPasteMarkerGraceHasAFloor` remains the
  deterministic pin. Raised in review of #445.
- **An absolute budget, never one scaled by the constant under test.**
  `splitMarkerAttempt` first scaled its window by `PasteMarkerGrace`, so under
  the mutation it exists to catch the budget collapsed with the constant,
  every attempt went inconclusive, and the test died pointing at the *runner*
  instead of at the number someone had just changed — with its carefully
  written #419 message two lines below, unreachable. It budgets `2*EscTimeout
  - EscTimeout/4` literally now; `TestPasteMarkerGraceHasAFloor` is what makes
  that safe to hardcode. (This line said `EscTimeout/2` for one round after
  the window was rebalanced in the code — a spec restating a constant is a
  second copy of it, and this is what the second copy does.)
- **A pty released per attempt.** Both helpers leaked one pty and one parked
  decoder goroutine per attempt, over twenty and forty attempts, because the
  `s.Restore()` and `master.Close()` were written as though each helper ran
  once. A test that exhausts its retries was also exhausting file descriptors.
- **Silence as its own outcome, not another inconclusive retry.** See the
  paragraph above: `closedTtyAttempt` returned one kind of miss, so the
  regression's own symptom was retried nineteen more times and then reported
  as a loaded runner.
- **Two allowances drawn on one window, which is not two windows.** The drift
  guard admits `held` up to `EscTimeout/4` past the arm and the tail budget
  admitted a further `2*EscTimeout - EscTimeout/4`, each measured from its own
  clock. 10ms + 70ms is EXACTLY the 80ms the grace lasts. Both comparisons are
  strict, so the write still *returned* before `arm + 2*EscTimeout` — by an
  arbitrarily small margin, and a returned `master.Write` has only queued the
  bytes, leaving nothing for the decoder to read them in. The budget is
  measured from `wrote` now, the one clock provably not after the arm, so it
  covers the drift, the sleep and the write together and the remaining
  `EscTimeout/4` of the window belongs to that read. A budget per hazard reads
  as caution and spends as a sum.

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
asks for the final pass, and the `term` tests cannot see which byte sequences
the decoder is allowed to hold. Neither substitutes for the other.

`TestFinalDecodeResolvesTheTypedMarkerPrefix` asserts the grace still holds for
the FIRST timeout before asserting the second resolves it, so a fix that removed
the exception instead of bounding it fails there rather than passing every
liveness check in the file.

`TestATypedPasteMarkerPrefixDoesNotStrandTheDecoder` lives in `term` for the
reason its neighbour `TestEscBeforeAMouseReportDoesNotStrandTheDecoder` gives: only the loop can show
that violating a decoding contract strands live input, and only a real tty makes
the loop the thing under test. Here the **gap is the fixture** — the three bytes must
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
