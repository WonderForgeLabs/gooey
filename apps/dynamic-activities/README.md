# dynamic-activities — a button that runs Python that did not exist yet

A gooey terminal app with a big star-shaped button. Pressing it runs a **Temporal
standalone activity**. The activity is implemented in Python — and it was written,
compiled and made runnable *while the app was already open*, by a tool call to a second
MCP server that this app launched as a companion.

> ## ⚠ This demo is remote code execution, on purpose
>
> `create_activity` takes a blob of Python source and `exec`s it in the worker process,
> with that process's full privileges. There is no sandbox, no allowlist and no review
> step. That is not an oversight or a TODO — it is the thing being demonstrated, and it
> was accepted deliberately **for this demo only**. Nothing here should be copied into
> anything that accepts code from somewhere you do not fully trust.
>
> **The entry point is the activity MCP server**, `http://127.0.0.1:7802/mcp` by default
> (`-activity-mcp`). Anything that can open a socket to it can run arbitrary code as you:
> another process on this machine, another user on a shared box, a browser page that can
> be talked into a POST. The mitigation is exactly one thing — **every port binds loopback
> only** — and none of them is authenticated. Loopback is nowhere near enough on a machine
> you share.
>
> When you start the demo the normal way (`go run .`), that is enforced: all three
> addresses are refused unless they are loopback, the app's own two by `checkLoopback` in
> `main.go` (pinned by `TestCheckLoopbackRefusesEverythingElse`) and the gRPC one by
> `grpc.Serve` itself. **`worker.py` has no such check of its own**, so running it by hand
> with `GOOEY_ACTIVITY_MCP_ADDR` set to a non-loopback address will bind it and hand
> arbitrary code execution to the network. Let the app launch it.
>
> So: do not expose these ports, do not port-forward or tunnel them, do not run this on a
> shared host, and do not run it anywhere you would mind an arbitrary `import os` going.
> Treat it exactly as you would treat handing someone a Python REPL on your box, because
> that is what it is.

## The loop

```
  MCP client  ──create_activity(name, python source)──▶  worker.py
  (you, or an agent)                                       │
                                                           │ 1. exec the source → a callable
                                                           │    in the runtime Registry
                                                           │
                                                           │ 2. as acts on ONE Attach stream
                                                           │    (gooey.control.v1 SessionService):
                                                           │      RegisterProperties  Activity.<Name>.Result
                                                           │      SetProperty         Selected = <Name>
                                                           │    …plus one unary PatchMarkup, the only
                                                           │    op the Act oneof lacks:
                                                           │      PatchMarkup  ActivityList (a button each)
                                                           ▼
                              ┌──────────────────────────────────────────────┐
                              │  the gooey app (main.go)                     │
                              │                                              │
                              │      ★  Click="{{temporal:Activity           │
                              │              .Selected .Input | into .Output}}"
                              └──────────────────────────────────────────────┘
                                                           │
                             press the star  ──▶  Temporal standalone activity
                                                           │
                                       the worker's ONE registered activity is a
                                       dynamic dispatcher; it looks the name up in
                                       the Registry and calls the exec'd function
                                                           │
                                       the result Sets .Output, which the UI is bound to
```

`delete_activity` runs the same steps backwards and ends with the **`UnregisterNames`**
act — the framework change this demo exists to justify. Without it, every name the loop
invents lives for the life of the process and `list_values` grows monotonically into
noise.

## The session is a stream, not a series of calls

`control.py` holds **one `SessionService.Attach` stream** open for the worker's whole
lifetime. That is not a stylistic choice; it is what makes this a state *sync* rather
than a series of hopeful writes:

- **The worker sees the app.** Subscribing to `properties` (with a `names` filter — an
  all-defaults `Subscription` is write-only) keeps a live mirror of the app's side. So
  when you press **ctrl+n** in the terminal and move `Selected` with no tool call
  involved, the worker knows. `delete_activity` uses that: it repoints the selection only
  if the selection actually went dangling, and otherwise leaves what you picked alone.
  `run_activity` with no `text` sends whatever is currently in the input box.
- **Ordering is free.** Acts are "applied in stream order on the UI goroutine — the
  remote mirror of the one ordered input stream", so register-then-select cannot race.
- **Read-after-write is free.** The `FrameDelta` for an act's frame is enqueued *during*
  that act's settle barrier, ahead of its own `ActResult` — so by the time an act returns,
  the mirror already reflects it.
- **Lifetime is free.** The app sends a `Closing` lifecycle event on the way out and the
  worker stops cleanly; a `Swapped` event means the whole page was replaced, so the worker
  re-patches its button list onto the new page (try `swap_markup` against the app's own
  MCP endpoint and watch the activity buttons come back).

Two things still cross as ordinary unary calls, both on purpose: a **one-shot
`ListValues`** at attach (a session's deltas carry changes *since* attach, and `Welcome`
"deliberately carries identity, not data", so the mirror needs seeding once), and
**`PatchMarkup`**, which the `Act` oneof does not carry.

## Why this needs no new command-registration machinery

Commands **cannot** be registered over the control plane: behavior needs code, not
storage, and that boundary is deliberate. But the `temporal:` handler namespace lets
markup bind an activity *call*:

```xml
<Button Click="{{temporal:Activity `Titlecase` .Input | into .Activity.Titlecase.Result}}"/>
```

…and the activity *is* the behavior. So "register a property + patch some markup" is
enough to produce a brand-new button that runs brand-new code — no new framework
capability required beyond a way to *un*register.

The star button goes one step further. Its activity type name is a **bound path**, not a
backtick literal:

```xml
<Button Name="Star" Click="{{temporal:Activity .Selected .Input | into .Output}}"/>
```

`markup.Arg` holds the property *handle* and reads it at click time, so one button that
was built at startup runs whichever activity Python most recently created — no rebinding,
no patch, no swap.

## Running it

Needs a Temporal server:

```sh
temporal server start-dev
```

Set up the Python side once. **Run this from this directory** — `requirements.txt` names
the control-plane client by relative path, and pip resolves such a path against the
current working directory, not against the requirements file:

```sh
cd apps/dynamic-activities
python3 -m venv .venv
.venv/bin/pip install -r requirements.txt
```

That is the whole setup. `requirements.txt` ends in `-e ../../clients/python`, so the
generated `gooey-control` client — and `grpcio` and `protobuf`, which it depends on —
arrive with it. Do **not** `pip install gooey-control` by name: no such project exists on
PyPI, so today that fails, and the day somebody registers the name it would silently
install a stranger's code into a process that already execs arbitrary Python by design.

Running the same command from the repo root fails with `../../clients/python is not a
valid editable requirement`, which is the cwd rule biting rather than anything being
wrong.

This venv is **this example's own** — `.venv/` here, gitignored, and what `main.go`
auto-selects when `-worker-python` is left at its default. Do not point it at
`apps/temporal-worker/.venv`: that one is built for a different demo (it carries
`claude-agent-sdk`, which pins `mcp` back to 1.29.x, and it has neither `grpcio` nor
`gooey-control`).

Run the tests from this directory too:

```sh
.venv/bin/python -m unittest discover -s . -p 'test_*.py'
```

> **The other reason the cwd matters.** The repo root contains the Go `grpc/` directory,
> and Python 3 imports a bare directory as a *namespace package*. So `python -c 'import
> grpc'` run from the repo root **succeeds even when grpcio is not installed** — it binds
> the name to Go source. It only ever produces a false *positive*: a really installed
> `grpc` is a regular package and wins the path scan from any directory, so this cannot
> break a correct venv — but it will happily tell you a broken one is fine. Never treat a
> bare `import grpc` as evidence the package is there; check `pip list`, or probe for a
> real attribute such as `grpc.insecure_channel`. (`mcp/` is a bare directory here too,
> and shadows the `mcp` package the same way.)

Then:

```sh
cd apps/dynamic-activities && go run .
```

That starts, in one process tree:

| what | default | flag |
|---|---|---|
| the app's own MCP server (drive the UI as an agent) | `127.0.0.1:0` | `-mcp` |
| the control-plane gRPC server (what Python talks to) | `127.0.0.1:0` | `-grpc` |
| the Python worker + activity MCP server, as a companion | `127.0.0.1:7802` | `-activity-mcp` |

Port 0 means the kernel picks; the app reads the address back from the **listener**, never
from the flag, so several instances coexist and the companion always gets the port that
was actually bound. The companion shares the app's lifetime: it starts before the first
frame and its whole process group is killed when the app quits. Its stdout/stderr go to
`worker.log` beside this README.

Pass `-worker-python /path/to/python` if you did not use `./.venv`; the default `python3`
is auto-upgraded to `./.venv/bin/python` when that exists. If your Temporal server is not
on `127.0.0.1:7233`, pass `-temporal`; `-task-queue` (default `gooey-dynamic-activities`)
keeps this demo's activities off another demo's queue, and `-with-worker=false` runs the
app with no companion at all.

## Driving it

Point any MCP client at `http://127.0.0.1:7802/mcp`:

```jsonc
// create_activity
{
  "name": "Titlecase",
  "description": "title-cases the input",
  "code": "def run(text):\n    return \" \".join(w.capitalize() for w in text.split())\n"
}
```

The source must define `run(text)` — sync or `async` — which is the entry point the
dynamic dispatcher calls with the app's `Input` property as its argument. The name must be
an identifier: it is used both as a Temporal activity type name *and* as a segment of the
binding path `.Activity.<Name>.Result`.

| tool | what |
|---|---|
| `create_activity(name, code, description?)` | exec the source, register `Activity.<name>.Result`, patch a button onto the page, select it |
| `list_activities()` | every activity on this worker, plus the app's live state (selected activity, input text, last star result) read off the session mirror |
| `get_activity(name)` | one, including its source |
| `delete_activity(name)` | remove the activity, patch the button off, `UnregisterNames` the result property; repoints the selection only if it went dangling |
| `run_activity(name, text?)` | run it through Temporal now and return the result, without the UI; omit `text` to use whatever is currently in the app's input box |

In the app: **ctrl+n** cycles which activity the star runs, **ctrl+l** clears the result,
**ctrl+c** quits. Tab moves focus; enter/space presses.

## The other two pages

`-page` swaps the whole document, and the directory ships three. The `Values` map in
`main.go` is what they share: every page binds against the same properties, so a page is
only a view onto one control plane.

| `-page` | what it is for |
|---|---|
| `dynamicactivities.gooey` (default) | the star, the activity list, the result pane |
| `zoom.gooey` | an image viewer for `three-ways.svg`, with `+`/`-` zoom |
| `live.gooey` | a TextBox and a Text on the same `Input` property, and nothing else |

**Both the default page and `zoom.gooey` carry `<Gooey Graphics="halfblock">`.** The
graphics protocol is a document setting, not a launch flag, and that is deliberate: the
choice belongs to the artwork on the page rather than to whoever started the process.
It matters here because this demo gets recorded — under a recording pty, capability
detection answers for the pty and not for a real terminal, so a page that left the
decision to detection would record as something other than what it is. Delete the
attribute and the app probes instead; the pages are written to survive either answer
(`zoom.gooey`'s `Chrome="pixel"` buttons fall back to box-drawing runes at the same
three-row footprint, so the layout does not shift when a protocol is found).

`three-ways.svg` is decoded through `imagefmt/svg` and reaches the plane as an `image`
value — the one `TypedValue` kind with no markup literal, so it can only be *bound*
(`<Image Src="{{…}}">`), never written inline. `zoom.gooey` is the page that shows it
large, and its own header comment explains why `HAlign`/`VAlign="Start"` is what makes
`Cols`/`Rows` mean anything: they are a request, and the default `Stretch` discards it.

## Things worth knowing if you are copying this

- **`Canvas` children position with `Canvas.Left` / `Canvas.Top`.** Bare `Left`/`Top` are
  silently ignored — they are attached properties, and those are the spellings.
- **Paint order is tree order; hit-testing is topmost-first.** The star's `<Button>` comes
  *after* the coloured runs it sits on. Put it first and the star paints over it and eats
  the click.
- **The star is a raster of background-filled containers.** Each run is a `<VStack
  Background="{{.StarColor}}" Width="N" Height="1"/>`; there is no glyph, which is why
  `screen_text` shows blanks there and `screen_text {"styled": true}` shows the colour.
  Both colours are bound properties, so recolouring the whole star is one `set_value`.
- **`ctx.Includes` is what lets agent-authored markup load assets.** A page loaded from an
  `fs.FS` resolves `<Image Src="…">` against that FS, but only *during* the load; markup
  that arrives later as bytes (`swap_markup`, `patch_markup`) has no document FS and falls
  back to `Includes`. Set it, or every `<Image>` in a patched fragment fails to build.
- **Patch before you unregister.** Removing a name never disturbs the running tree — a
  component already bound to it keeps its handle and keeps rendering — so the delete flow
  patches the button away *first*, then takes the name out of scope for future markup.
- **Activities retry by default**, so a single click may run an activity more than once.
  Bind idempotent work. Delivery is naturally last-write-wins: each completion `Set`s the
  target property.
- **Never `await` a subscriber callback on the reader task.** This one cost real debugging
  time. If the coroutine handling a `Swapped` event sends an act — which is exactly what
  you want it to do — it waits for the `ActResult` that settles it, and only the reader
  can settle it. Awaiting the callback inline deadlocks the session on the *first* swap:
  the unary patch lands, every follow-up act hangs forever, and the stream looks healthy
  while silently going deaf. `ControlSession._spawn` runs callbacks as separate tasks for
  this reason.
- **`prop.Set` does not compare values** — an identical `Set` still invalidates every
  dependent and costs a repaint. Since the mirror already knows what the app holds,
  `set_string` skips the write when nothing would change.
- **A `Subscription`'s `names` filter is fixed for the life of the session.** Names
  registered *later* (every `Activity.<Name>.Result` here) cannot be added to it without
  reattaching, so the worker deliberately watches only the app-owned names it needs to
  reason about.
