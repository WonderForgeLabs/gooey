# Control-plane connection state (decision record)

Decided 2026-08-23, after a status-bar indicator was built that reported
the wrong thing for a defensible reason.

## What went wrong first, because it is the useful part

`apps/wysiwyg` shows its control-plane addresses in the status bar. An
indicator dot was added beside each. No connection state was observable
from either server, so the dot was repointed at something that *was*
observable — the outcome of the last clipboard copy — and a comment
explained the substitution.

That is worse than no dot. **A dot immediately left of the word "grpc"
reads as a connection light whatever the code says it means**, and one
that is confidently wrong about the thing it appears to report costs
more than the blank space it replaced.

The correct response to "the state is not observable" is to make it
observable, not to point the cue at a different fact. But the second
half of that is equally load-bearing: where the state genuinely cannot
exist, the answer is fewer states, never an invented one.

## grpc: a live count, because there is something to count

The gRPC control plane has a long-lived streaming `Attach` RPC, and
`broadcaster.sessions` already tracked it — unexported, mutex-guarded,
mutated from stream goroutines, with no accessor.

Added:

    func (s *Server) Sessions() int      // live, under the broadcaster's mutex
    func (s *Server) Serving() bool
    func (s *Server) ServeError() error
    Options.OnSessions func(n int)       // fired when the count CHANGES

The set itself stays unexported. A caller holding `*session` values
could read them off the UI goroutine, which is the confinement the
broadcaster exists to enforce; a count crosses that boundary safely and
a set does not.

### `Serving()` answers *whether*. `ServeError()` answers *why*.

That division is the durable part of this record, and it is worth
stating before the table because the table is what gets copied.

**One predicate drives the indicator, and it is `Serving()`.**
`ServeError()` only ever supplies the explanation for a state
`Serving()` has already decided. It is never the test.

Three states, each distinguishable with information that actually
exists:

| state | condition |
|---|---|
| down | `!Serving()` — `ServeError()` says why, and may be nil |
| idle | `Serving()` and `Sessions() == 0` |
| active | `Serving()` and `Sessions() >= 1` |

Every row names `Serving()` so the three are mutually exclusive and the
table does not depend on being read top-down. That is not tidiness:
there is a real window in which `Serving()` is already false and
`Sessions()` is still non-zero, because `Stop` ends the accept loop
immediately while each stream's count is decremented by its own deferred
`unregister`. A table whose `active` row omitted `Serving()` would
report a dead endpoint as active for the length of that window.

#### The worked example: a clean Close paints green

This spec originally wrote the first row as `ServeError() != nil`, which
is wrong in a way worth keeping on the page, because it is the mistake a
reader makes on their own.

grpc-go's `Serve` returns **nil** when `Stop` interrupts a running accept
loop. So after an ordinary `Close`:

    Serving()    == false     // the listener is gone
    ServeError() == nil       // it went cleanly

Under `ServeError() != nil` that is `down == false`, and the dot paints
green over an endpoint whose listener no longer exists — the
confidently-wrong indicator this whole change exists to remove, walked
back in through the predicate.

The evidence was already in the suite when the row was written:
`TestServingReportsTheAcceptLoop` closes the server, waits for
`Serving()` to go false, and then asserts `ServeError()` is nil. The
spec contradicted a passing test in the same change.

The second failure is quieter and is the reason `ServeError()` cannot be
patched into totality: it does not distinguish **not started yet** from
**up**. A server built with `New` rather than `Serve` has no listener and
a nil error, so an error-driven predicate calls it healthy.
`Serving()` is false there, which is the true answer —
`TestNewWithoutServeIsNotServing`.

The same division holds for mcp, where `ServeError()` normalises
`http.ErrServerClosed` away for exactly this reason: a clean shutdown
has no *why*, and it still has a *whether*.

### Push, not poll

`OnSessions` exists because polling is wrong twice over: it repaints on
a clock instead of on change, and `prop.Set` does not compare values, so
a per-frame `Set` repaints forever. The count changes on connect and
disconnect, which are events.

### The disconnect asymmetry is the trap

A session **joins** on the UI goroutine (`register` goes through
`Bridge.Do`, `grpc/session.go:105`) and **leaves** on its own stream
goroutine (`unregister` is a plain `defer`, `session.go:111`).

So a callback that touches the property graph directly **works for every
connect and races on every disconnect**. It passes any test that only
attaches — and every test in that package before this change attached
and never left. The contract is therefore stated as "arbitrary
goroutine, marshal with `Dispatcher.Post` unconditionally", with an
explicit instruction never to branch on "am I already on the UI
goroutine".

`notify` is called with the lock **released**. The callback is host code
and may re-enter the server — asking what the count is now is the most
natural thing it could do — and `sync.Mutex` is not reentrant, so
notifying under the lock deadlocks the attach it was announcing.
`TestOnSessionsRunsOutsideTheLock` fires exactly that.

## mcp: two states, because a stateless endpoint has no third

The brief for this work said the streamable-HTTP server "knows when it
mints an `Mcp-Session-Id`". **It does not, and this is the correction
that shaped the design.** The handler runs `Stateless: true`
(`mcp/transport.go`): every POST is independent, the SDK synthesizes the
initialized state for a request that arrives without a handshake, GET
and DELETE are answered 405, and the string `Mcp-Session-Id` appears
nowhere in the package.

"Is a client connected" **has no answer** for a stateless endpoint. A
client is connected for the few milliseconds of a request and not
otherwise, and there is no third thing to observe. So mcp gets two
states — `Serving()` or not — and inventing a third means inventing a
fact.

`Requests() int64` is offered alongside and is real, but it is
**cumulative and never decreases**. It answers "has anything ever talked
to this endpoint", which is useful and is not connection state. Wiring
it to a dot would be the broken-gauge shape: a number that only goes up
looks correct in any demo, where nothing has disconnected yet, and is
wrong the first time something does. It is rendered as a labelled count
or not at all.

It is counted **after** the origin guard, so a refused cross-origin
probe is not reported as a client using the app — a number that mixes
clients with attacks is a number nobody can act on.

## The asymmetry is the point

grpc has three states and mcp has two, and these are deliberately **not**
smoothed into one vocabulary. The difference is a long-lived stream
versus a stateless request/response showing through, and a user learns
something true from seeing it. An amber on the mcp chip would be the
fabrication this record exists to prevent.

Two tests defend the argument rather than the code, because the argument
is what rots:

- `TestThisServerMintsNoSessionIDs` — fails if a session id ever appears,
  with a message saying that `state.go`'s explanation is now wrong and a
  live count is both possible and required.
- `TestStatelessnessIsDeclaredWhereItIsRelied` — fails if
  `Stateless: true` is flipped, because the whole two-state case rests
  on it and would otherwise become false silently while every other test
  still passed.

## A clean shutdown is not a failure, and getting that wrong is
## intermittent

grpc-go's `Serve` returns nil when `Stop` interrupts a **running** accept
loop, but `ErrServerStopped` when `Stop` lands **before** the loop
starts. `Serve` launches it with `go s.gs.Serve(ln)`, so a short-lived
app or a fast test takes the second path.

Without normalising it, an ordinary shutdown lights a failure indicator
*sometimes*, depending only on scheduling — the worst way for an
indicator to be wrong. The mcp equivalent is `http.ErrServerClosed`.

Neither surfaced in a direct test. It appeared as collateral damage in
an unrelated mutation run that happened to change the timing, which is
the argument for the discipline in one sentence. Pinned by
`TestAnImmediateCloseIsStillCleanNoMatterWhoWonTheRace`.

## Note for anyone writing tests here

`prop.evalStack` is a **package-level global** (`prop/prop.go:31`), so
the property graph is single-goroutine for the whole *process*, not per
App. A test that leaves two `testApp` run loops alive is racing by
construction, and the detector reports it deep inside
`markup`/`components`, nowhere near the code under test. `t.Cleanup`
fires at test end, so a bare `for` loop keeps every previous app
composing — use `t.Run` subtests.
