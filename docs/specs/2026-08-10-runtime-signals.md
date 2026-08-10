# The runtime: gooey.App, and the console signal story

Status: implemented 2026-08-10. Owner: term/ + root package.

Before this, every gooey app hand-wrote its own run loop — open the
screen, raw mode, mouse, dispatcher, decoder goroutine, `select`,
deferred `Restore` — about sixty lines, copied five times, each copy
with slightly different bugs. And the signal story was: there wasn't
one. `ctrl+c` arrived as byte `0x03` and each app bound it; an external
`SIGINT` left the terminal raw with mouse tracking on; `SIGWINCH` was
ignored entirely (a gooey app's size was fixed at construction);
`SIGTSTP` wedged the terminal; a panic escaped with the screen
destroyed and its own stack trace unreadable.

`gooey.App` (app.go, signals_unix.go) owns all of it.

## ctrl+c as a byte vs SIGINT as a signal

These are two different events and both must work.

In raw mode the tty driver's signal generation (`ISIG`) is OFF, so
`ctrl+c` is delivered to the program as the byte `0x03` in the input
stream. It is decoded into an ordinary key event
(`{Key: KeyRune, Rune: 'c', Mods: ModCtrl}`) and routed through the
tree like any other key. The App's default quit key matches it — but
only on what the tree DECLINES, the same rule that lets unconsumed
arrows move focus. A widget that wants `ctrl+c` keeps it.

An EXTERNAL `SIGINT` (`kill -INT`, a supervisor, another process in the
group) is a real signal and still arrives as one. It is not a keystroke
and no widget sees it: it means "stop", and it is honored.

`WithQuitKeys` changes the key; nothing changes the signal.

## The table

| Signal | What the App does |
| --- | --- |
| `SIGINT`, `SIGTERM` | Run the shutdown hook (bounded by its timeout, terminal still up), quit the loop, restore the terminal via the normal teardown. `Run` returns `*SignalError`; `gooey.Exit` turns that into exit code 128+n (130 for INT, 143 for TERM). |
| `SIGWINCH` | Re-query the size, `Composer.Resize` (new buffer, whole tree dirtied), repaint. Layout already runs every frame, so the tree re-measures itself. |
| `SIGTSTP` | The classic dance: full restore (decoder joined), `signal.Reset`, re-raise at ourselves — the process stops there. Resumes at the next line on `SIGCONT`: discard any still-pending stop, re-register, re-acquire the terminal, full repaint. **Declined** where a stop cannot be honored (see below). |
| `SIGCONT` | No handler, by design. Resumption IS returning from the re-raise inside the `SIGTSTP` handler. |
| while suspended | `SIGINT`/`SIGTERM` are SHIELDED. The tty driver signals the whole foreground process group, so a `ctrl+c` aimed at a child process arrives here too; acting on it would kill the host along with the thing it launched. The child gets its own copy regardless. |
| panic | `Run` recovers, restores the terminal FIRST, then re-panics with the original value. A crash must print its stack onto a cooked screen. |

Every signal is delivered onto the UI goroutine through the Dispatcher
rather than handled where it lands: the terminal work has to be ordered
against frames, and a handler goroutine touching the composition would
break UI-goroutine confinement.

## The unhonored-stop trap (found while implementing this)

The textbook ctrl+z dance — restore, `signal.Reset(SIGTSTP)`, re-raise —
assumes the raise stops the process. **It does not always.** POSIX says
a stop signal sent to an ORPHANED process group is not honored, and a
process can be in one without knowing: a `go test` binary is, a session
leader's group is orphaned by definition (which is what `script` makes
its child, and `script` is how this project records every demo), and
`nohup`'d, supervised and CI-run programs generally are.

When the raise is not honored, the signal does not vanish — it stays
pending, and the handler that re-registers `SIGTSTP` on the way out is
handed it straight back. That is an INFINITE restore/re-acquire loop
with the UI flickering through it. Measured, not theorized: one ctrl+z
produced 29 alt-screen cycles in six seconds before this was fixed.

Two mechanisms, in order:

1. **Precondition** — `term.Screen.CanSuspend()`: we must own the
   terminal (`TIOCGPGRP` == `getpgrp`) AND not be the session leader
   (`getsid` != `getpgrp`). Where it says no, ctrl+z is declined
   outright and the app keeps running, which is the right answer for a
   program nobody can stop.
2. **Loop breaker** — after the raise returns, `signal.Ignore(SIGTSTP)`
   before re-registering. Setting the disposition to ignore makes the
   kernel DISCARD a still-pending copy, so an unhonored raise costs one
   flicker instead of looping forever. This is the net that catches
   configurations the precondition does not predict — and there is at
   least one: a process that owns its terminal, is not a session leader
   and sits in a non-orphaned group, whose raise still did nothing under
   WSL2.

### Verification status, stated plainly

The DECLINE path and the loop breaker are verified (unit test plus pty
runs). The path where the process genuinely stops is **not** verified
here: in every configuration reachable in this environment the raise was
not honored — confirmed directly with a heartbeat goroutine, which shows
no freeze at all where a real stop would show a two-second gap. It needs
an interactive shell with job control: press ctrl+z in any gooey app
from a normal terminal and confirm it restores the shell, stops, and
repaints intact on `fg`.

## Exit codes

`Run` returns `nil` for `Quit()` and for a cancelled context, and
`*SignalError` for INT/TERM. `gooey.Exit(err)` applies the convention:
0 for nil, 128+signum for a signal (quietly — a program that was
interrupted did not fail), 1 with the message on stderr for anything
else.

## What this rests on

The whole story depends on `term.Screen.Restore` being able to JOIN its
input decoder, which it could not do until the tty read lifecycle was
fixed (`docs/specs/2026-08-10-tty-read-lifecycle.md`). Suspend, ctrl+z
and a clean exit are all "give the terminal to someone else", and none
of them is safe while a goroutine of ours is still reading it.

## Synchronized output

Every flush is bracketed in DEC mode 2026 (`CSI ? 2026 h/l`), so a
frame is presented atomically instead of being drawn as it arrives —
the tearing seen during hot reload, where the top of the screen was the
new tree and the bottom was still the old one. Unrecognized DECSET is
defined to be ignored, so it needs no capability check.

`Frame.Flush` brackets the cell plane AND the graphics placements that
sit on top of them, since those are one frame.
