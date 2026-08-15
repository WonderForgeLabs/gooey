# How to run services with your app's lifetime

A TUI often depends on a service whose useful lifetime is exactly the
app's — a worker polling a task queue, a dev server, a sidecar process.
Started by hand it produces the same two bugs every time: it starts at
the wrong moment (its failure message vanishes behind the alternate
screen, or the app's first screen needs it before it is up), and it
outlives the app (a `defer` in `main` is not on the panic path, not on
the signal path).

`gooey.Companion` is that lifetime, owned by the framework. Ground
truth: [`companion.go`](../../../companion.go) and the
[companions spec](../../specs/2026-08-10-companions.md).

## Declare one

The interface is three methods — and deliberately no `Stop`; the
context IS the stop, and a companion's whole job is to return when it
is cancelled:

```go
type Companion interface {
    Name() string
    Start(ctx context.Context) error
    Wait() error
}
```

Two constructors cover what apps actually have. Go code on a goroutine:

```go
worker := gooey.CompanionFunc("wizard-worker", func(ctx context.Context) error {
    c, err := wizard.Dial(ctx, address, temporalhandlers.NopLogger)
    if err != nil {
        return err
    }
    defer c.Close()
    return wizard.Run(ctx, c, taskQueue) // returns when ctx is cancelled
})
```

A child process:

```go
cmd := exec.Command("temporal", "server", "start-dev", "--headless")
dev := gooey.CompanionCmd("temporal-dev", cmd, gooey.CompanionOutput(logFile))
```

Register with `gooey.WithCompanions(dev, worker)` at construction, or
`app.AddCompanion(c)` before `Run` when the companion must close over
the App. **Declaration order is start order** — each `Start` returns
before the next begins, so a companion may depend on the one above it
(the worker dials the server started on the line before) — and stop
order is the reverse.

## The lifecycle, in the order `Run` does it

1. **Start companions**, in declaration order — each `Start` returns
   before the next begins.
2. **Grace window** (default 100ms): anything that has already exited is
   reported as a *start* failure, on a cooked terminal, before any
   screen is taken.
3. **Build the tree.** Companions are up, so a `Build` that talks to one
   simply works.
4. **Acquire the terminal.** Only now does the screen change.
5. **Run.** Each companion has a supervisor goroutine that turns "this
   one finished" into a `Dispatcher.Post`, like every other async thing.
6. **Teardown**: terminal restored first, **then** companions cancelled
   and waited for.

Step 6 holds on every exit path, which is the guarantee a hand-rolled
`defer` never gives you. The next two sections are steps 2 and 6 in
detail.

## Readiness: companions are up before your first frame

`Run` starts companions **before** `Content.Build` and before the
terminal changes at all. Two consequences to lean on:

- **`Build` is the right place for connection work.** Dial your
  server, attach, retry, wait for first data — on the UI goroutine,
  before raw mode, so failures print as ordinary text on a cooked
  terminal. `cmd/wizardui` waits for its first served screen inside
  `Build` and it simply works, because the worker is already polling.
- **The grace window catches services that die on arrival.** After
  starting everything, `Run` waits `WithCompanionGrace` (default
  100ms) and reports anything that already exited as a *start*
  failure — before the screen is taken, so the explanation is
  readable. The default suits a `CompanionFunc`, which fails in
  microseconds. A child that must **bind a socket** takes about a
  second to discover it cannot: set a wider window or "port already in
  use" arrives after the app is on the alternate screen, and the user
  sees a flash instead of a sentence.

```go
app := gooey.NewApp(content,
    gooey.WithCompanions(dev, worker),
    gooey.WithCompanionGrace(2*time.Second), // size for the slowest starter
)
```

The window ends early on the first failure, so it costs its full
duration only when nothing is wrong — and that cost overlaps time
`Build` would have spent retrying its dial anyway. (Per-companion grace
is the recorded better shape; today the one app-level option has to fit
the slowest starter.)

## The teardown guarantee

When the app exits — quit key, `Quit()`, cancelled context, SIGINT,
SIGTERM, **or a panic** — teardown restores the terminal first, then
cancels every companion's context and waits for it. The ordering is
deliberate: a service shutting down slowly does it on a cooked
terminal, and whatever it prints on the way out is readable. On the
panic path the terminal is restored, companions are stopped, and the
panic is re-raised. There is no path out of `Run` that leaves a
companion running.

For a `CompanionCmd`, "stop" means real process hygiene:

- The child gets its **own process group**, and teardown signals the
  group — dev servers and language runtimes fork, and signalling only
  the direct child leaves grandchildren holding the port. (Non-Unix
  builds signal the process alone: a real difference, not a
  papered-over one.)
- A child that ignores the signal gets `SIGKILL` after
  `CompanionKillDelay` (default 5s).
- A companion that ignores its cancelled context is abandoned after
  `WithCompanionStopTimeout` (default 10s), and
  `app.CompanionLeaked()` reports it — the only evidence you will get
  that something is still polling after your app closed.

The failures are typed. `Run` returns a `*gooey.CompanionError` carrying
the companion's `Name` and a `Phase`:

- `PhaseStart` — failed to start, or died inside the grace window.
  Returned before the terminal is ever touched.
- `PhaseRun` — was running and is not anymore. The app quits. This
  includes a **zero** exit: a service that decides it is finished while
  its app is on screen has failed at being a service, and `Err == nil`
  is that event, named.

```go
var ce *gooey.CompanionError
if errors.As(err, &ce) && ce.Name == "temporal-dev" {
	fmt.Fprintf(os.Stderr, "%v\nmost often something is already on %s\n", err, addr)
}
```

Two more rules. Nothing restarts: a companion is a service, not a job,
and supervision policy belongs in your own `Companion` implementation if
you need it. And a `CompanionFunc` that **panics** has the panic
converted into an error rather than killing the process — a process that
dies there dies with the terminal raw and on the alternate screen.

`Suspend` (ctrl+z) stops nothing — companions keep running while the
app is in the background.

## Output goes nowhere by default

A `CompanionCmd`'s stdout/stderr go to `os.DevNull` unless you pass
`CompanionOutput(w)`. A child writing to the inherited stdout paints
over a raw-mode screen with bytes the framework cannot repaint. Give
long-lived companions a log file (an `*os.File` is handed to the child
directly; any other writer is piped and copied); `apps/kanban`
writes its Python worker's output to `kanban-worker.log` and closes
the file after `Run` returns — safe, because `Run` only returns after
every companion is waited for.

Run `exec.LookPath` yourself first if you want a missing binary
explained in your own words before `Start`'s error does it.

## What should NOT be a companion

Anything whose state should outlive the app. `wizardui --with-dev-server`
makes this the flag's own argument: the Temporal dev server is opt-in
and never the default, because it holds workflow state you may want to
inspect after the UI closes, and a program that silently deletes what
you were about to look at is a bad neighbor. The worker is the
opposite case — it holds nothing, so owning its lifetime costs
nothing. That asymmetry is the rule of thumb.

## Working examples

Each is one flag away from the multi-shell deployment it replaces:

- [`handlers/temporal/cmd/temporaldemo`](../../../handlers/temporal/cmd/temporaldemo)
  — the smallest case: a `CompanionFunc` worker serving `Slugify`.
- [`handlers/temporal/cmd/wizardui`](../../../handlers/temporal/cmd/wizardui)
  — the full arrangement: a `CompanionFunc` worker by default, a
  `CompanionCmd` dev server behind `--with-dev-server` (with the 2s
  grace), and connection work in `Build`.
- [`handlers/temporal/cmd/temporalops`](../../../handlers/temporal/cmd/temporalops)
  — same shape over the visibility pack ([Tutorial 9](../09-temporal.md)).
- [`apps/kanban`](../../../apps/kanban) `-with-worker` —
  a **Python** process as a companion, log-file output, its own task
  queue.

## See also

- [Companions spec](../../specs/2026-08-10-companions.md) — failure
  semantics table and the design reasoning.
- [Tutorial 9: Temporal end-to-end](../09-temporal.md) — every demo
  there rides on this.
- [How-to: work off the UI goroutine](howto-async.md) — how a
  companion's results reach your properties (`app.Post`, like any
  other goroutine).
