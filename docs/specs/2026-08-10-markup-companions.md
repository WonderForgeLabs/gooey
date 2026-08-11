# `<Companion>`: declaring a child-process service in markup

Status: implemented 2026-08-10. Owner: `markup/companion.go`,
`components/companion.go`. Extends
[companions](2026-08-10-companions.md); has a security consequence for
[the MCP server](2026-08-10-mcp-server.md).

A companion is already the right *mechanism* for "this UI needs a worker
running". What it is not yet is *configuration*. Everything about
`examples/kanbandemo`'s worker — which interpreter, which script, which
directory, which log file, which two environment variables — is Go
source, sixty lines of it, guarded by a flag, in a `main` that has
already grown to eight hundred lines. None of it is code in any
interesting sense. It is a five-line service definition wearing Go.

```go
workerDir := filepath.Join(dir, "..", "temporal-worker")
logPath := filepath.Join(workerDir, "kanbandemo-worker.log")
logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
…
cmd := exec.Command(*workerPython, "worker.py")
cmd.Dir = workerDir
cmd.Env = append(os.Environ(), "GOOEY_MCP_URL="+mcpURL, …)
app.AddCompanion(gooey.CompanionCmd("temporal-worker", cmd, gooey.CompanionOutput(logFile)))
```

The argument for moving it is the argument `<x:Property>` already won:
the thing being declared is a *declaration*, and markup is this
framework's declaration surface. A page that needs a worker says so in
the file that describes the page, next to the `<Timer>` and the
`<KeyBinding>` that are also services the page needs and also used to be
Go.

The counter-argument is that this one spawns processes. That is real, it
is answered below, and it is answered with a perimeter rather than a
refusal.

## The element

```xml
<Gooey xmlns="wonderforge.io/gooey/2026">
  <Grid Rows="Auto,*">
    <Companion Name="temporal-worker"
               Path="python3"
               Dir="../temporal-worker"
               Log="kanbandemo-worker.log"
               KillDelay="5s"
               StopTimeout="10s"
               Error="{{.WorkerError}}"
               Exited="{{.Quit}}">
      <Companion.Args>
        <Arg>worker.py</Arg>
        <Arg>--queue</Arg>
        <Arg>{{.TaskQueue}}</Arg>
      </Companion.Args>
      <Companion.Env>
        <Var Name="GOOEY_MCP_URL" Value="{{.McpURL}}"/>
        <Var Name="PYTHONUNBUFFERED" Value="1"/>
      </Companion.Env>
    </Companion>

    <Text Grid.Row="0" Style="warn">{{.WorkerError}}</Text>
    …
  </Grid>
</Gooey>
```

It is **non-visual**, like `<Timer>` and `<KeyBinding>`: `buildChildren`
routes it to its parent as an attachment, it is never measured, arranged
or painted, and it costs no paint node. It is declared on the page's root
container beside the other services the page needs.

### Only `CompanionCmd`

There is deliberately no markup spelling for `CompanionFunc`. A
`CompanionFunc` *is* Go code — a closure over an app's own types — and
the only thing markup could contribute is a name for it, which is a
registry, which is code-behind, which is the thing markup declaration was
supposed to remove. A child process is the opposite: entirely describable
as data, and the case that actually wants a config file. The `Companion`
interface stays the seam for anything else; an app with an exotic service
implements the three methods and registers it in Go, exactly as before.

## Two tiers, and why this one is scoped to the composition

This is the load-bearing design decision, so it comes before the
attribute table.

`App`-level companions (`WithCompanions`, `AddCompanion`) start **before**
`Content.Build` and stop **after** the terminal is restored. That
ordering is the whole point of the companions spec: a service that cannot
start reports it on a cooked terminal, a `Build` that talks to the
service finds it up, and a service that shuts down slowly does so on a
screen the user can read.

A markup declaration cannot have that ordering, because it *is* part of
`Content.Build`. By the time the document has been parsed, companions
have already been started and the grace window has already closed. There
were two ways out:

1. give `App` a pre-`Build` parse pass — ask the `Content` for its
   companions before starting anything;
2. scope a markup-declared companion to the **composition** instead of
   the app: start it when the tree goes live, stop it when the tree
   leaves.

(1) buys the original ordering for the *initial* page and nothing at all
for a page that arrives later — a swap, a patch, a hot reload. It would
mean the same three lines of markup behave differently depending on which
door the document came through, which is exactly the kind of "works in
one build path" seam this codebase keeps refusing to add.

(2) is uniform. `gooey.Startable` already exists for precisely this —
"non-visual elements that own a background goroutine; the Composer
discovers them while walking the tree and starts them when the
composition goes live; `Composer.Close` stops them" — and every path that
installs a tree goes through it: `attach` at startup, `App.Swap` for a
whole-page swap, `Composer.InvalidateStructure` for a patched subtree,
`reload` for a file edit. A companion declared in markup therefore has
**the lifetime of the tree that declares it**, and the machinery under it
is the existing `gooey.CompanionCmd` — process group, kill escalation,
`os.DevNull` output — unchanged.

What that costs, stated plainly:

| | `WithCompanions` / `AddCompanion` | `<Companion>` |
| --- | --- | --- |
| starts | before `Content.Build`, before raw mode | when the composition goes live, after raw mode |
| stops | after the terminal is restored | with `Composer.Close`, before the terminal is restored |
| survives a swap/reload | yes — `Content` is not replaced | no — the outgoing composition's companion stops, the incoming one's starts |
| a failure to start | aborts `Run` before the screen exists | reported into the property graph (`Error`), and load-time checks catch most of it |
| a mid-run exit | quits the app (`*CompanionError`) | sets `Error`, runs `Exited` — the page decides |
| grace window | `WithCompanionGrace`, app-wide | none; see below |
| stop budget | `WithCompanionStopTimeout`, app-wide | `StopTimeout`, per element |

The rule of thumb that falls out: **if the tree's construction depends on
the service, declare it in Go.** `cmd/wizardui` builds its first screen
from a workflow query answered by its own worker; that worker must exist
before `Build`, so it stays a `WithCompanions` entry. A worker the
running UI *uses* — kanbandemo's, which nothing in the build path talks
to — belongs in markup.

Both tiers can be used in one app, and they do not interact. `App`
starts, supervises and stops its own list; the Composer starts, watches
and stops the tree's. Nothing looks a companion up by name, so a name
appearing in both places is legal (and, in a hot-reload session where a
Go-declared worker and a markup-declared one collide on a port, the
second one's failure lands in `Error` where you can see it).

### The teardown-order regression, named

`App.teardown` closes the composer *before* it restores the terminal, so a
markup companion is asked to stop while the screen is still raw and the
UI is frozen mid-teardown — the arrangement the companions spec
deliberately avoided for the app tier. The alternative is worse: signal
the process group and return without waiting, which is the orphaned-child
bug the whole mechanism exists to prevent. So the stop **waits**, bounded
by `StopTimeout` (default 10s, the same default as
`WithCompanionStopTimeout`, and for the same reason: it must outlast
`KillDelay` so the child is SIGKILLed by its companion rather than
abandoned by the app). A cooperative child costs milliseconds here. A
stubborn one costs its kill delay, on a raw screen, and
`components.Companion.Leaked()` records that it did — the tree-tier
mirror of `App.CompanionLeaked`.

Reordering `teardown` so the terminal came back first would fix the
cosmetics and is not in this change: it moves a lifecycle guarantee that
other specs depend on, for a case that only shows up when a child is
already misbehaving.

## Security: markup can spawn processes, and markup can arrive over MCP

Any MCP client can call `swap_markup` or `patch_markup`, and those go
through the same `markup.Build` every other document does. Once markup
can declare a `<Companion>`, **an MCP client can start an arbitrary child
process** — which is an escalation past the posture recorded in
`2026-08-10-mcp-server.md`, "an MCP client can do anything the keyboard
can". The keyboard cannot spawn `rm -rf`.

That escalation is **accepted deliberately**, and the earlier draft of
this design — honor companions on the initial build, reject them on the
swap path — was **rejected by the user**. The reasons it was rejected are
worth keeping, because they are the reasons not to reintroduce it:

- A capability that works in one build path and errors in another is two
  languages sharing a syntax. An agent iterating on a page would hit a
  wall halfway down the file with no way to tell from the document which
  side of it a given element is on.
- The rejection would have been *specific to the swap path*, not to
  untrusted input. Nothing about a document that arrived through
  `swap_markup` is more hostile than a document on disk that a hostile
  process rewrote before the watcher picked it up; the guard would have
  bought the appearance of a boundary rather than a boundary.
- The real boundary is who can reach the MCP endpoint. That is a
  perimeter question, and it is answered where perimeters are answered.

So the posture, stated as a whole:

1. **The MCP server is opt-in and loopback-only.** A non-loopback `Addr`
   is a hard error, and the `Origin` check is default-deny for anything
   claiming to be a browser (see the MCP spec). An app that does not
   serve MCP has no remote markup path at all.
2. **Untrusted markup must not be handed to an app whose config allows
   companions.** Serving markup from a workflow
   (`2026-08-10-server-driven-ui` in practice) means trusting the
   workflow the way you trust the binary.
3. **`GOOEY_MARKUP_COMPANIONS` is the off switch.** Unset or empty means
   enabled, which is the default a framework feature has to have to be
   usable. Set it to a value `strconv.ParseBool` reads as false (`0`,
   `f`, `false`) and every `<Companion>` element becomes a **load
   error** — not an inert element — naming the switch that refused it.
   The `GOOEY_`-prefixed environment variable is this repo's existing
   convention (`GOOEY_TASK_QUEUE`, `GOOEY_MCP_URL`, `GOOEY_UI_DIR`).

Two details of that switch are deliberate.

**It fails closed on junk.** A value that is neither empty nor a
recognizable bool disables the capability. A security switch that a typo
silently turns back on is not a switch, and `GOOEY_MARKUP_COMPANIONS=of`
is a plausible typo.

**Disabled is a load error, not a no-op.** Markup's whole discipline is
load-time failure — an unknown element, an unaccepted property element, a
binding whose type does not match. A page whose worker silently did not
start is a page that appears to work and does not, which is the failure
mode this package rejects everywhere else. The client that swapped the
markup gets a sentence explaining exactly what was refused and why, the
initial page fails before the terminal is touched, and a hot reload keeps
the previous tree the way it does for any other bad edit.

### Contrast with the `sys:` handler pack, which decided the opposite

`handlers/exec` — the `sys:Run` namespace — takes the other position
outright: "Untrusted markup NEVER names a binary. The first argument to
`sys:Run` must be a backtick literal naming a registered `Command`,
checked at load time; the registered set IS the API surface." Registration
in Go is the capability grant, and itemizing it is the grant's content.

`<Companion>` names a binary. The two are not reconcilable by argument,
so they are reconciled by scope:

- `sys:Run` is for markup that may be **hostile** — the pack exists so
  that an agent-authored or workflow-served document can run commands
  without being able to choose them. Its allowlist is the whole feature.
- `<Companion>` is for markup that is part of **the app** — a page shipped
  in the same repository, the same `embed.FS`, the same review, as the Go
  that runs it. Its process is the app's own sidecar, and requiring a Go
  registration for it would leave the configuration in Go, which is the
  problem being solved.

An app that wants the `sys:Run` posture for its services already has it:
don't set `Error`/`Exited`, don't declare `<Companion>`, and register the
service in Go. An app that hands markup to strangers and also allows
companions has made a mistake this document cannot prevent — but
`GOOEY_MARKUP_COMPANIONS=0` can, deployment-wide, without touching the
app.

## Attributes

| Attribute | Meaning |
| --- | --- |
| `Name` | **Required.** The companion's label in errors, and the element's `Name=` identity (`markup.Find`, tree snapshots). |
| `Path` | **Required.** The executable. A bare name (`python3`) is resolved on `PATH` **at load time**; a path containing a separator is resolved against the document's directory. Either way the failure is a load error, not a start failure. |
| `Dir` | Working directory, resolved against the document's directory. Must exist at load time. |
| `Log` | Output destination, resolved against the document's directory. Truncated and opened when the child starts, closed after it stops. Absent means `os.DevNull`. |
| `KillDelay` | `time.ParseDuration`; the SIGTERM→SIGKILL grace. Default 5s (`gooey.CompanionKillDelay`). |
| `StopTimeout` | `time.ParseDuration`; how long teardown waits for the child after cancelling. Default 10s. |
| `CleanEnv` | `"true"` starts the child from an **empty** environment. Default is inherit-and-override. |
| `Error` | Optional binding to a `*prop.Property[string]`. Set to a `*gooey.CompanionError`'s message when the child fails to start or exits unbidden; cleared to `""` on a successful start. |
| `Exited` | Optional command, run on the UI goroutine when the child is gone for a reason nobody asked for — including never having started. `Exited="{{.Quit}}"` reproduces the app tier's "a dead service takes the app with it". |

Unknown attributes are a **load error**, following `<x:Property>` rather
than the visual elements (where a stray attribute is ignored). A
misspelled `Dir=` that silently ran the child in the wrong directory, or
a misspelled `Log=` that silently sent its output to the null device, are
both worse than a startup failure.

### Args and env are property elements, not strings

`Args="worker.py --queue my-queue"` is lossy the moment an argument
contains a space, and there is no quoting convention available: XML
attributes have already spent both quote characters (the same reason the
handler DSL quotes with backticks). Splitting on whitespace would make
`--dir="/My Documents"` unrepresentable, and the failure would be a
confusing runtime error inside the child.

So both collections use the property-element syntax this codebase already
has for structured attributes — `<ItemsView.ItemTemplate>`,
`<StatusBar.Right>`:

```xml
<Companion.Args>
  <Arg>--dir</Arg>
  <Arg>/My Documents</Arg>
</Companion.Args>
<Companion.Env>
  <Var Name="GOOEY_MCP_URL" Value="{{.McpURL}}"/>
</Companion.Env>
```

One element per argument, document order preserved, no escaping anywhere.
The parser's existing rules apply for free: the prefix must name the
element it sits inside, `<Companion.Argv>` is refused because the element
does not accept it, and a slot given twice is an error. `<Arg>` and
`<Var>` are consumed as **data** by the `<Companion>` builder — like
`<Menu>`/`<MenuItem>` under `<MenuBar>` — so they never enter the visual
tree and never reach the general builder.

There is no shell. `Path` plus the `<Arg>` list is an argv, and nothing
re-parses it.

### Bindings in args and env are snapshots

`<Arg>{{.TaskQueue}}</Arg>` resolves to a live `*prop.Property[string]`
handle at build time like every other text binding, and is **read once,
when the child starts**. Changing the property afterwards does not
restart the child — an argv is a value a process was launched with, not a
property it observes. The read happens on the UI goroutine outside any
computed evaluation, so it records no dependency; that is the same
call-site rule `Timer.Enabled` follows, used for the opposite purpose.

This is what makes a markup companion able to depend on something only Go
knows: kanbandemo's MCP endpoint is not knowable until the listener is
bound, so the app puts it in a property and the document binds it.

### The environment inherits by default

`CleanEnv` is off, so the child gets `os.Environ()` plus the `<Var>`
entries — which is what `exec.Cmd` does with a nil `Env`, and what the
hand-written kanbandemo companion did on purpose: a worker needs the
`ANTHROPIC_API_KEY` and `TEMPORAL_ADDRESS` already exported in the shell
that launched the app.

This is the opposite of `handlers/exec`, which scrubs by default because
markup there may be hostile. The scope argument from above applies, plus a
mechanical one: against a document that can already choose the binary, an
environment scrub buys very little (a child of the same user can read
`/proc/self/environ` of anything it likes). `CleanEnv="true"` is there for
the deployment that wants it, and there is deliberately no `PassEnv`
spelling — under `CleanEnv` the binding context *is* the pass-through, so
forwarding a variable is something the Go side hands over on purpose.

## Path resolution is markup-relative

`Dir` and `Log` resolve against **the directory the document came from**,
not the process's working directory. A `.gooey` file is configuration, and
a path in a configuration file that means something different depending on
where you launched the binary from is a bug generator. `<Image Src="…">`
already resolves against the document's `fs.FS` for exactly this reason —
"assets ship beside the markup that names them".

The mechanics are less tidy than `Image`'s, and the reason is worth
recording: **`fs.FS` cannot answer this question.** `os.DirFS(dir)` gives
no way back to `dir`, and a child process needs a real OS path — `chdir`
and `open` do not take an `fs.FS`. So `markup.Context` gains one field:

```go
// Dir is the OS directory this document's HOST-SIDE paths resolve
// against — a <Companion>'s working directory and its log file.
Dir string
```

An app sets it to the same directory it passed to `os.DirFS`:

```go
app = gooey.NewApp(markup.Page(os.DirFS(dir), "kanbandemo.gooey", ctx))
ctx.Dir = dir
```

Empty means the process's working directory, which keeps
`markup.Build(src, ctx)` from bytes working with no ceremony (tests, and
the MCP swap path against an app that never set it).

This is duplication — `os.DirFS(dir)` and `ctx.Dir = dir` say the same
thing twice — and it is the honest version of the alternative, which was
to type-assert the `fs.FS` for an unexported `os.dirFS` and read its
string. `Context.Dir` is one field, no reflection, and it will be what a
future `<Socket>`, `<Pipe>` or unix-socket handler resolves against too.

An embedded release build (`embed.FS`) has no host directory and no
sensible `Dir`; a companion in an embedded page falls back to the process
cwd, which is what a deployed binary's own conventions should be deciding
anyway.

## What load time catches, and what it cannot

The app tier's grace window exists because "started" is not "running":
`exec.Cmd.Start` reports a missing binary but not a binary that exits two
milliseconds later complaining about a port. The tree tier has no
equivalent moment — there is no screen it can decide before — so it moves
as much as possible earlier instead, to **build** time:

- `Path` is resolved through `exec.LookPath` (bare) or statted (pathful);
- `Dir` must exist and be a directory;
- `Log`'s parent directory must exist (the file itself is opened at start,
  so a document that fails to build never truncates a log);
- every duration parses and is positive;
- `Name` and `Path` are non-empty, every `<Var>` has a `Name` without an
  `=`, and every attribute is one this element accepts.

For the initial page that is before the terminal is touched, and for a
swap it is before the tree is committed — so the two most common failures
("that binary isn't installed", "that directory doesn't exist") land where
the grace window would have put them, without a window.

What is left — the port-already-in-use class — arrives after the screen
exists and is reported through `Error`, which is a better place than a
cooked terminal for a failure a running UI can render.

## Grace and stop-timeout, declaratively

The brief asked whether `WithCompanionGrace` and
`WithCompanionStopTimeout` should have markup spellings. The answers
differ, and the difference is instructive.

**`WithCompanionGrace`: no.** Grace names a moment — "after starting the
services, before building the tree and taking the screen" — that a markup
declaration is discovered *after*. There is nothing for a document to
configure. Nor should a document be able to: it configures the `App`, and
a page swapped in at runtime must not be able to lengthen the app's
startup or its teardown budget.

**`WithCompanionStopTimeout`: yes, per element.** `StopTimeout` on
`<Companion>` is the per-companion shape the companions spec called "the
right shape" and left out of v1 because it widened a three-method
interface. It arrives here without widening anything, because at this tier
there is no shared window to attribute an exit to: each companion's stop
is its own bounded wait, run by the Composer, and per-element is the only
thing it *could* be. The companions spec's regret is satisfied on the tier
where it was cheap.

That leaves the observation the companions spec made about the 100ms
default — that it is wrong for a process that must bind a socket, and 2s
is right — as an app-tier fact only. It stays correct there. At the tree
tier the substitutes are the load-time checks above and `Error`.

## Failure semantics

| what happens | when | what the element does |
| --- | --- | --- |
| bad attribute, missing `Path`, unresolvable binary, missing `Dir`, unparseable duration | load | load error; initial page fails before the terminal, swap leaves the running tree |
| `GOOEY_MARKUP_COMPANIONS` disables companions | load | load error naming the switch |
| `Start` fails (the binary vanished between load and start; the log file cannot be opened) | composition goes live | `Error` ← `*CompanionError{PhaseStart}`, `Exited` runs |
| the child exits, for any reason, unasked | running | `Error` ← `*CompanionError{PhaseRun}`, `Exited` runs |
| the child exits **zero**, unasked | running | same — a service that stops is a failed service (the app tier's rule, reported rather than fatal) |
| the tree is swapped, patched away, or reloaded | any | the companion is asked to stop and waited for; `Exited` does **not** run — this exit was requested |
| `Composer.Close` (quit, signal, ctx cancel, panic) | teardown | same: cancel, bounded wait, close the log |
| the child ignores its cancelled context | teardown | give up after `StopTimeout`, `Leaked()` reports it |
| `Suspend` | during | nothing — companions are not part of the UI |

`Error` carries a `*gooey.CompanionError`'s message verbatim, so the two
tiers say the same sentence about the same event: *`gooey: companion
"temporal-worker" stopped while the app was running: exit status 1`*.

## Output goes to the null device, and there is no spelling for anything else

The one thing a markup author must not be able to do by accident is
inherit the app's stdout. A child writing to it in raw mode paints over
the UI's bottom rows with bytes the framework will never repair. So:
absent `Log` means `os.DevNull` (which is `gooey.CompanionCmd`'s own
default, reached by simply not passing `CompanionOutput`), and `Log`
takes a **path**, never a stream name — there is no `Log="stdout"`,
no `Inherit="true"`, and no way to spell one.

## Deliberately not in this change

- **No restarts.** A companion is a service, not a job; the app tier said
  so and nothing here changes it. A `RestartPolicy` attribute would be the
  first supervision policy in the framework and deserves its own design.
- **No `CompanionFunc`.** See above.
- **No `PassEnv`.** The binding context is the pass-through.
- **No root-element knobs.** `<Gooey>` takes no companion attributes; a
  document does not configure the `App`.
- **No `Running` property.** `Error == ""` after a live start is the
  signal a status line needs; a second observable can be added when
  something wants to distinguish "starting" from "up".
- **`examples/kanbandemo` is not converted here.** The framework
  capability and its adoption land separately.
