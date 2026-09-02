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

Lower is not available: one is what `idle` already means, and the grace would
not exist. Higher buys a marker split across a slower link, at the price of the
Esc key taking that long to arrive and of the deaf window being that much wider
if something new ever lands in this shape.

The residual risk, stated rather than waved at: a genuine marker split across
reads **more than two escape timeouts apart** resolves to Esc and its payload
arrives as keystrokes, which is #419's symptom returning. Going deaf forever is
the worse half of that trade, and 80ms of silence inside a six-byte marker the
terminal is actively writing is not a case anyone has observed.

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

`input/decodefinal_test.go` and one new test in `term/strand_linux_test.go`.
Every clause is pinned — mutation-tested, and each mutation turns its own tests
red:

| mutation | what goes red |
|---|---|
| the grace is never withdrawn (`d == idled` → `idle`) | `TestFinalDecodeResolvesTheTypedMarkerPrefix`, `TestFinalDecodeWaitsForNothingButAnOpenPaste`, and the term strand test |
| `DecodeFinal` becomes a synonym for `Decode(b, true)` | the same three |
| the loop never escalates to the final pass | the term strand test alone |
| the stall counter resets on every timeout instead of counting | the term strand test alone |

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

`TestFinalDecodeMakesProgressOnNestedEscapes` extends the existing alphabet
sweep to **five** bytes, because three to five is exactly the range
`splitPasteMarker` covers and the old sweep stopped at four.
