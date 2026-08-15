---
name: capture-a-demo
description: Drive a real gooey binary under a pty and capture what it shows — a transcript assertion, a screenshot, or a GIF for the docs. Use when verifying a change in the actual app rather than headlessly, scripting keystrokes into a TUI, recording with asciinema and agg, or debugging why a capture came out empty or black. Covers script -qec, octal printf escapes, replaying through render.Screen instead of hunting escape codes, and the four traps that each cost an hour.
---

# Capture a gooey demo

`docs/learn/howto/howto-testing.md` is the source; this is the operational
version plus the traps that have since been paid for.

## First: do you need a pty at all?

Usually not. `gooey.NewComposer` needs a component tree and a size, not a tty.
`Frame()` paints into a plain buffer you can read and returns how many
components painted. Text, styling, damage counts, markup loads and input
routing are all assertable with no terminal at all, and those tests run in CI.

Use a pty when the thing under test **is** the real binary: terminal setup, the
event decoder, `gooey.App`'s lifecycle, teardown and restore.

## Driving the binary

```sh
go build -o /tmp/gooey-bin/myapp ./cmd/myapp     # -o keeps the tree clean

{ sleep 0.7; printf '\011\011 q'; sleep 0.7; } |
  script -qec "stty cols 96 rows 24; /tmp/gooey-bin/myapp" /tmp/session.log
```

Three details, each of which breaks it entirely if dropped:

- **`script -qec`** allocates the pty *and makes it the controlling terminal*,
  which is what an app opening `/dev/tty` requires. Plain redirection is not
  enough.
- **`stty cols W rows H`** fixes the size, so the transcript is reproducible
  rather than a function of your window.
- **Octal escapes.** `script` runs the command under `sh`, whose `printf` has
  **no `\x` hex form**. Use `\011` tab, `\015` enter,
  `\033[A` `\033[B` `\033[C` `\033[D` arrows, and a literal space for space.
  `\x09` silently produces the four characters `\x09`.

## Reading the result: replay it, do not grep it

**Do not look for the last `\x1b[H` in the log.** The flush is incremental:
only a *full* frame starts with cursor-home, and after the first one the log
holds **differences** — a keystroke turning `n=2` into `n=3` puts a single `3`
on the wire. Searching the bytes for what the app is showing finds the first
frame, or nothing.

Replay the whole log through `render.Screen`, which is an `io.Writer` that
models a terminal:

```go
s := render.NewScreen(80, 24)
s.Write(log)
fmt.Println(s.Text())      // s.Contains("saved") for a single row
                           // s.Buf for the cell grid, if you assert styling
```

Cut the log at the last `\x1b[?1049l` first if the app exited cleanly: leaving
the alternate screen blanks it, and `script` appends its own trailer.

> This contradicts `.claude/agents/gooey-dev.md`, which still says "extract the
> final frame by finding the last `\x1b[H` in the log". That line is stale.
> `howto-testing.md` and this skill are right; the agent file has not been
> updated.

The framework's own pty tests work this way — `testTTY.waitFor` polls the
modelled screen, and `waitForBytes` is the separate helper for assertions that
really are about escape sequences (leaving the alternate screen, disabling
mouse reporting) and leave no mark on the screen.

**`waitFor` is not a gate.** It polls a *modelled* screen that a reset leaves
intact, and the buffer's tail may be a frame still being written — so it can
return on a state that is half a frame old. Where determinism matters, inject a
one-byte-at-a-time trickling reader.

## GIFs and stills

```sh
asciinema rec --overwrite -q --cols 84 --rows 17 \
  -c "script -qec 'timeout -s KILL 5 /tmp/gooey-bin/myapp' /dev/null" out.cast

agg --theme asciinema --font-size 16 --idle-time-limit 1 out.cast out.gif
convert out.gif -coalesce frame-%03d.png
```

- **Nest `script` inside asciinema.** asciinema's `-c` command does not receive
  the recorded pty as its *controlling* terminal, so an app opening `/dev/tty`
  writes to your real terminal and records nothing. An empty cast is this.
- **Let `timeout` kill the app** rather than quitting it, if you want the last
  frame to be the live UI. A clean quit restores the terminal, and that restored
  screen becomes the final frame.
- **`agg` renders the cell plane only.** Sixel, kitty and iTerm2 output do not
  survive recording; **halfblock does**. Force halfblock for captures.
- **Sample frames densely where the UI changes.** Sparse sampling of the
  extracted stills skips hold beats and makes a working animation look broken.

## What you cannot capture, and the consequence

**Mouse input cannot be injected through a recording pty.** There is no way to
deliver SGR mouse reports as real pointer events through `script`'s stdin. Two
consequences, and the second is a design constraint rather than a testing one:

- Test mouse handling **headlessly** — construct `input.MouseEvent` values and
  call `comp.Handle(input.MouseOf(ev))`.
- **Keep every feature reachable from the keyboard.** If a capture cannot
  demonstrate it, neither can a user without a mouse.

**Terminal capability detection needs a real terminal.** `term.Detect` sends
queries and waits for replies; under a headless pty nothing answers and it
falls back after its timeout.

**BEL is invisible here.** Nothing in the framework emits `\a`, and
`render.Screen` swallows `0x07`. "It beeps" is not a testable criterion — do
not write an acceptance criterion that depends on it.

## When the screen comes out black

A forced graphics protocol with no capabilities behind it leaves `CellW` at
zero. Sixel then emits an eighteen-byte empty image and the component skips its
halfblock fallback, so the region is blank **with no error on any surface**.
Pass cell metrics with the protocol, or let detection run. See the
`write-a-component` skill's three-condition tier guard.

## Related

- `docs/learn/howto/howto-testing.md` — the source for everything above
- `docs/learn/howto/howto-images.md` — halfblock vs sixel vs kitty
- The `tests-that-can-fail` skill — a capture proves **symptoms, not causes**.
  Check any mechanism a capture implies against the source before reporting it
  as the cause, and when a repro fails, suspect arity first.
