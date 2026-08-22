# Concept: the App lifecycle

`gooey.App` is the framework-owned run loop: the terminal's lifetime,
the input decoder, the Dispatcher, frame scheduling, hot-reload swaps,
and the console signal story, in one object. It exists because every
app had been hand-writing the same sixty lines — open, raw mode, mouse,
decoder goroutine, select loop, deferred restore — each copy with its
own bugs. The tedious parts (setup) and the genuinely hard parts
(signals, suspend, dying with the terminal intact) are the same parts,
so they move together:

```go
app := gooey.NewApp(markup.Page(os.DirFS("."), "app.gooey", ctx))
if err := app.Run(context.Background()); err != nil {
	gooey.Exit(err)
}
```

The loop is deliberately **not** extensible with more select cases.
Everything asynchronous — timers, watchers, fetches — reaches the UI
through `app.Post`, which is the confinement rule anyway
([how-to: work off the UI goroutine](../howto/howto-async.md)). Signals
are no exception: each one is posted onto the UI goroutine, because the
terminal work they do has to be ordered against frames, not raced with
them.

## ctrl+c is a key; SIGINT is a signal

These are two different events and both work. In raw mode the tty
driver's signal generation is off, so **ctrl+c arrives as the byte
`0x03`** — decoded into an ordinary key event and routed through the
tree like any other. The App's default quit key matches it, but only on
what the tree **declines** (the same rule that lets unconsumed arrows
move focus), so a component that binds ctrl+c keeps it. `WithQuitKeys`
replaces the key set; pass none to own the whole key surface.

An **external `SIGINT`** — `kill -INT`, a supervisor — is a real signal
and no component sees it: it means stop, and it is honored. SIGINT and
SIGTERM run the `WithShutdown` hook first, bounded by its timeout and
with the terminal still up, then `Run` returns `*SignalError` and
`gooey.Exit` applies the shell convention — quietly 130 for INT, 143 for
TERM; other errors print and exit 1.

The hook does **not** run on the UI goroutine, and that is forced by the
bound rather than incidental: `gracefulExit` spawns it and waits on a
channel, because a hook that never returns has to be *abandoned*, and
there is no abandoning a call made inline. So the hook is background
work like any other and obeys the same rule — it may not `Get` or `Set`
a property. It reaches the graph through `App.Post`.

Those posts do land. After the hook returns (or its timeout fires),
`gracefulExit` drains the dispatcher once and paints one more frame if
anything it applied asked for one, *then* quits. That is the whole
mechanism behind "paint a farewell": the drain is the last moment the UI
goroutine is still looking, since `Quit` stops the loop before it reaches
another frame and teardown neither composes nor flushes.

One shape to avoid: **post and return, never post and wait.** The UI
goroutine is parked inside `gracefulExit` until the hook returns, so
nothing the hook posts can run while it is still running — a hook that
blocks on its own post is stuck until the timeout unsticks it, and then
the app exits without having done the work.

[#315](https://github.com/WonderForgeLabs/gooey/issues/315) is where this
was two contradictions between the API doc and the code; the doc on
`WithShutdown` now states the contract above, and
`TestShutdownHookPaintsItsFarewell` pins the drain-and-paint half of it.

## The rest of the signal table

- **`SIGWINCH`** re-queries the size and calls `Composer.Resize`: new
  buffer, everything dirty. That is the whole implementation, because
  layout already runs every frame and re-measures the tree against the
  new bounds without being asked.
- **`SIGTSTP` (ctrl+z)** is the classic dance: restore the terminal
  completely (which joins the input decoder), reset the handler,
  re-raise the signal at ourselves — the process genuinely stops there.
  `SIGCONT` needs no handler: being scheduled again *is* the
  notification, and execution resumes at the next line — re-register,
  re-acquire, repaint, picking up any resize that happened while
  stopped. Where a stop cannot be honored (orphaned process groups:
  `go test`, `script`, CI), `term.Screen.CanSuspend` says no and ctrl+z
  is **declined** rather than wedging the terminal in a restore loop.
  Honestly stated: the decline path is verified by tests; the
  genuine-stop path needs an interactive shell with job control, which
  the spec records as unverifiable in this repo's environment.
- **A panic** anywhere under `Run` restores the terminal FIRST and then
  re-panics with the original value, so the stack trace prints onto a
  cooked screen instead of scrolling sideways through a raw-mode alt
  buffer.

## Suspend: hand the terminal to a child

`app.Suspend(fn)` releases the terminal, runs `fn` as its owner, and
takes it back — this is how `cmd/browser` runs the demo you pick on
your terminal. The epic that would generalize this into a hand-off,
[#237](https://github.com/WonderForgeLabs/gooey/issues/237), is parked.
Two guarantees make it correct:

- **The decoder is joined, not abandoned.** After release, nothing of
  ours is reading the tty — the invariant the
  [tty read lifecycle](../../specs/2026-08-10-tty-read-lifecycle.md)
  fix established. A leaked reader would eat the child's keystrokes,
  and every suspend would add another one.
- **Interrupts are shielded while away.** The tty driver signals the
  whole foreground process group, so the ctrl+c a user aims at the
  child arrives here too; acting on it would kill the host along with
  the thing it launched. The child gets its own copy regardless.

Coming back is cheap by design: re-entering the alternate screen finds
it blank, but the retained buffer is still right and **no component
repaints** — what is wrong is the flush's belief about what the
terminal shows, so `acquire` invalidates the flush, not the paint (the
[rendering-2](../../specs/2026-08-10-rendering-2.md) note). A size
change while away is picked up on the way back in.

## What else rides the App's lifetime

`Quit` is idempotent and safe from any goroutine; `Done()` is the
channel other goroutines select on. It is also **permanent** — an App
runs once, and there is no restart. Hot reload swaps the composition
through the same `attach` path as startup — a broken markup save keeps
the running UI, though focus is not yet preserved across the swap
([#52](https://github.com/WonderForgeLabs/gooey/issues/52)). Companion
services start before the terminal is taken and stop after it is handed
back, so a slow shutdown happens on a cooked screen ([how-to:
companions](../howto/howto-companions.md)); they have been declarable in
markup since [PR #174](https://github.com/WonderForgeLabs/gooey/pull/174).

Depth:
[specs/2026-08-10-runtime-signals.md](../../specs/2026-08-10-runtime-signals.md)
is the full decision record, including the full signal table and the
unhonored-stop trap; `app.go` and `signals_unix.go` are that document
executed. [Tutorial 1](../01-first-app.md) is where `gooey.App` first
appears, and `cmd/browser` ([demos.md](../../demos.md#browser)) is the
hand-off at full size.
