# Companions: an app's services, with the app's lifetime

Status: implemented 2026-08-10. Owner: root package (`companion.go`).
A concrete adopter: [PR #138](https://github.com/WonderForgeLabs/gooey/pull/138) wires kanbandemo's Temporal worker in as a `CompanionCmd`.

The Temporal wizard demo took three shells to run — a dev server, a
worker, the UI — and only one of those is a separate program in any
interesting sense. The worker is a *service the UI depends on*, whose
useful lifetime is exactly the UI's. Nothing about it wanted to be
another terminal window; it was one because gooey had no way to say
otherwise.

Every hand-written attempt to say it produced the same two bugs.

**The service started at the wrong time.** Started after the terminal was
raw, a service that failed to start printed its complaint onto an
alternate screen that appeared and vanished in the same second. Started
before, but without the framework knowing, the app had no way to *wait*
for it — and the wizard's first screen is a Temporal query answered by
that very worker, so "not yet" was the normal answer for the first two
seconds of every run.

**The service outlived the app.** "Stop it on the way out" is a `defer`,
and a `defer` in `main` is not on the panic path, not on the signal path,
and not on the ctx-cancellation path. A worker left polling after its UI
closed is invisible until the next run fails on a port, or a task is
served by a process nobody remembers starting.

`gooey.Companion` is that lifetime, promoted to the framework where the
other lifetimes already live.

## The interface

Three methods, no reflection, no registry:

```go
type Companion interface {
    Name() string
    Start(ctx context.Context) error
    Wait() error
}
```

There is deliberately **no `Stop`**. The context IS the stop: teardown
cancels it, and every companion's job is to return. Two stop mechanisms
would be one too many, and the one that survives is the one that composes
with every Go library already written.

Two constructors cover what apps actually have:

* `CompanionFunc(name, func(ctx) error)` — Go code on a goroutine. A
  panic inside it is converted to an error rather than allowed to kill
  the process, because a process that dies here dies with the terminal
  raw and on the alternate screen.
* `CompanionCmd(name, *exec.Cmd, opts...)` — a child process.

Two *kinds*, and that stays two — anything further is a composition over
one of them rather than a third thing to supervise. `PythonCompanion`
(2026-08-16, `pycompanion.go`) is the first: a `PythonWorker` value
describing the shape `apps/kanban` and `apps/dynamic-activities` had each
written out by hand, which it turns into exactly the `CompanionCmd` above
with the interpreter chosen (`Dir/.venv/bin/python` beats bare `python3`
unless the caller named one) and the log file's lifetime tied to the
child's rather than to a `defer` in `main`. It adds no teardown machinery,
which is the point: the close-and-join contract below is asserted once,
here, not once per app.

Registered with `WithCompanions(...)` at construction, or
`AddCompanion` before `Run` for a companion that must close over the App
itself. Started in declaration order, stopped in reverse.

## The lifecycle, in the order `Run` does it

1. **Start companions**, in order. Each `Start` returns before the next
   begins, so a companion may depend on one declared above it (the
   wizard's worker dials the dev server started on the line before).
2. **Grace window** (`WithCompanionGrace`, default 100ms). Wait a beat;
   if anything has already exited, report it as a *start* failure. It
   ends early on the first failure, so it costs its full duration only
   when nothing is wrong.
3. **Build the tree** (`Content.Build`). Companions are up, so a Build
   that talks to one — as the wizard's does — simply works.
4. **Acquire the terminal**: raw mode, mouse, decoder. Only now does the
   screen change.
5. **Run the loop.** A supervisor goroutine per companion turns "this one
   finished" into a `Dispatcher.Post`, the same way every other
   asynchronous thing in this framework reaches the UI goroutine.
6. **Teardown**: signals off, hooks stopped, composer closed, **terminal
   restored** — and only then companions cancelled and waited for.

Step 6's ordering is the deliberate one. A service shutting down slowly
does it on a cooked terminal, not behind a frozen UI, and whatever it
prints on the way out is readable. Mechanically it is defer order in
`Run`: `stopCompanions` is deferred *before* `teardown` so it runs
*after* it.

## Failure semantics

| what happens | when | what the App does | what `Run` returns |
| --- | --- | --- | --- |
| `Start` returns an error | before the screen | stop the ones already started, abort | `*CompanionError{Phase: PhaseStart}` |
| a companion exits inside the grace window | before the screen | same | `*CompanionError{Phase: PhaseStart}` |
| a companion exits with an error mid-run | running | quit the loop | `*CompanionError{Phase: PhaseRun, Err: …}` |
| a companion exits **zero** mid-run | running | quit the loop | `*CompanionError{Phase: PhaseRun, Err: nil}` |
| the terminal's input decoder dies | running | quit the loop | `terminal input stopped: <decoder error>` |
| the input decoder **lives and goes deaf** | running | **nothing — it cannot see this** | never returns |
| quit key, `Quit()`, cancelled ctx, SIGINT/SIGTERM | teardown | cancel, wait (bounded) | `nil` or `*SignalError` |
| a companion ignores its cancelled context | teardown | give up after `WithCompanionStopTimeout` (10s), set `CompanionLeaked()` | unchanged |
| panic | teardown | restore terminal, stop companions, re-panic | (panics) |
| `Suspend` | during | **nothing** — companions keep running | — |

Several of these rows deserve their reasons — the count is deliberately
not given, because this section has already grown one.

**A clean exit mid-run is a failure.** A service that decides it is
finished while its app is still on screen has failed at being a service,
silently, which is worse than crashing. `CompanionError` with a nil
`Err` is not a contradiction; it is that event, named.

**A dead decoder is a failure, not a quiet app.** When terminal input
stops there is nothing left to drive the UI, so an App that kept running
would be on screen and permanently deaf — the one outcome worse than
exiting, because the user cannot even quit. `Run` returns the decoder's
error wrapped as `terminal input stopped: …` rather than `nil`, since
`nil` is how a caller decides nothing went wrong.

**A LIVE decoder is the row this table could not express, until it had
to.** Every other row is an event something signals. A decoder that goes
deaf signals nothing, by construction: `Run` selects on `a.decDone`, and
`decDone` closes when the decode goroutine *returns*. A goroutine still
looping over a terminal it is still reading has not returned, so there is
no channel to select on, no error to wrap, and no `Run` return to name
here — which is why the fourth column says "never returns" rather than
naming a value. The app paints, the terminal reads, and every keystroke
is dropped between them.

That is not hypothetical. `input.Decode` could answer "incomplete, feed
me more bytes" for a buffer no further byte would ever complete, and the
drain loop believed it — one `Esc` arriving in the same read as a mouse
report stranded the buffer permanently (#406). The reasoning above says a
deaf app is the outcome worse than exiting; this is that outcome reached
from the other side, and the detection argument for the dead decoder
gives no cover against it.

What closes it is not a tripwire but an invariant, because a tripwire
needs someone to trip it: under `idle`, `Decode` answers "incomplete"
only where a byte can still resolve it, so the drain loop cannot be told
to wait for a byte that is not coming. `CompanionLeaked()` and
`DecoderLeaked` have watchdogs precisely because their failures are
observable; this one is not, so it is designed out instead of watched
for.

**That used to read "always consumes a byte or produces an event", and
the absolute was doing the work of the argument** — a reader taking it
at face value concludes `drain` cannot return early, which is exactly
how the departure from it went unnoticed. The list has two entries, both
bracketed pastes: an OPEN paste, which waits indefinitely by design
because delivering its prefix truncates it silently; and a SPLIT MARKER,
which is bounded by `input.DecodeFinal` and `term.PasteMarkerGrace`
because those bytes are also three keys a person can type
([#440](https://github.com/WonderForgeLabs/gooey/issues/440)). The
argument above survives the correction — neither exception can strand
the loop indefinitely on input a user typed — but it has to be made from
the real list rather than from an absolute. Reconciled in review of
[PR #445](https://github.com/WonderForgeLabs/gooey/pull/445).

**A dead companion outranks a dead decoder, which outranks a signal.**
`exitErr` returns the first non-nil of `compErr`, `termErr`, `exitSig`,
and the order is the causal one: where more than one happened, the signal
is usually the shell reacting to the same underlying failure, and a
decoder that died because the terminal went away is downstream of
whatever took it away. The companion is the fact that explains the rest.

**Nothing restarts.** A companion is a service, not a job. Supervision
policy — backoff, restart limits, health checks — is a real design space
and none of it belongs in v1; an app that wants it implements `Companion`
itself and supervises inside `Wait`.

## `CompanionCmd` and the terminal

Three things it does that a hand-rolled `exec.Cmd` usually does not.

**Output goes to `os.DevNull`** unless `CompanionOutput(w)` says
otherwise. This is the SDK-logger lesson one level up: a child writing to
the inherited stdout paints over the UI's bottom rows in raw mode, and
those bytes are not ours to repaint. An `*os.File` is handed to the child
directly; anything else means `os/exec` pipes and copies.

**The child gets its own process group** (`Setpgid`), and teardown
signals the *group* (`kill(-pgid)`), then escalates to `SIGKILL` after
`CompanionKillDelay` (5s). Dev servers and language runtimes fork;
signalling the direct child alone leaves grandchildren running and
holding the port, and the next run of your app then fails for a reason
that has nothing to do with your app. `companion_test.go` tests exactly
this discriminator — a shell that backgrounds a `sleep` — and the test
fails if the negative pid is changed to a positive one.

The non-Unix build (`companion_other.go`) signals the process alone.
That is a real difference, not a papered-over one.

## The grace window is per-app, and should be per-companion

Found while wiring `--with-dev-server`, and worth writing down because
the default is wrong for half of what companions are used for.

A `CompanionFunc` that fails does so in microseconds — the goroutine
returns an error. A `CompanionCmd` that fails usually has to *bind a
socket* first, which took the Temporal dev server about a second. With
the 100ms default, "port already in use" arrived after the screen was
taken: the wizard flashed up and vanished, and the explanation printed
underneath the wreckage. With a 2s window it is caught first and the app
never takes the screen at all. Both behaviors were observed; the second
is the one users can act on.

`WithCompanionGrace` is an App-level option, so an app with both kinds
must set the window for its slowest. `cmd/wizardui` does exactly that —
2s, but only when `--with-dev-server` is passed, since that flag is the
app saying "I am about to start a server". The cost is nil in practice:
the window overlaps time `Content.Build` would have spent retrying the
dial anyway.

The right shape is a grace value carried by each `Companion`, with the
App waiting the maximum and attributing early exits per companion. It is
not in v1 because it widens the interface from three methods to four for
a case one demo currently has.

## The tripwire

`App.CompanionLeaked()` mirrors `App.DecoderLeaked()`: it should always
be false, and it is the only evidence you will get that a service ignored
its cancelled context. Without it, "the app exited but something is still
polling" is a mystery discovered days later.

## What this changed in the demos

`handlers/temporal` went from three shells to two, and to one where the
Temporal CLI is installed:

```sh
temporal server start-dev --headless   # shell 1
go run ./cmd/wizardui                  # shell 2 — brings its own worker

go run ./cmd/wizardui --with-dev-server   # or: one shell, server and all
```

The standalone `workers/wizardworker` and `workers/temporalworker` still exist
and still matter: workers belong where the compute is, and
`--with-worker=false` is the deployment a real system has. The UI cannot
tell the difference between the two; every screen came back through the
server either way.

**The dev server is opt-in and never the default.** It holds state that
outlives any one client — you want `temporal workflow show` to work after
the UI closes — and a program that silently deletes the thing you were
about to inspect is a bad neighbor. The worker is the opposite case: it
holds nothing, so owning its lifetime costs nothing. That asymmetry is
the rule of thumb for what belongs in `WithCompanions` at all.

## Consequence for `Content`

Because companions start before `Content.Build`, `Build` became the right
place for an app's *connection* work, not just its markup. `cmd/wizardui`
now dials Temporal, attaches to the workflow and waits for the first
served screen inside `Build` — on the UI goroutine, before raw mode, with
a retry loop — so all of it reports as ordinary text on a cooked
terminal. That is a nicer arrangement than the one it replaced, and it
only became available once something else guaranteed the services were up
first.
