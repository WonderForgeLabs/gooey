# Tutorial 8: Drive your app from outside — MCP and gRPC

Everything the previous tutorials built — named elements, a viewmodel of
typed property handles, commands — turns out to be a wire surface. In
this tutorial you run a real Kanban board that is also an MCP server,
attach a client from another shell, and drive it: snapshot the live
tree, read the screen as text, press a button, type into a text box, and
replace the whole page with markup that arrives over the wire. Then you
learn when to reach for the gRPC control plane instead, which exposes
the same surface with generated clients and a streaming session.

**Time:** about 30 minutes.
**Prerequisites:** [Tutorial 3](03-binding-and-state.md) and
[Tutorial 4](04-input-commands.md). Nothing here requires Temporal or
any external service — one Go toolchain and `curl` is enough.

The app you drive is [`apps/kanban`](../../apps/kanban)
— read its `main.go` alongside this page; every claim below is a comment
there. The smaller [`mcp/cmd/server`](../../mcp/cmd/server) is the
same idea at postcard size
([`server.gif`](../media/demos/server.gif) shows it driven live).

<!-- GIF: docs-and-demos workflow — kanban driven over MCP, board
     changing while curl runs in a second pane -->

## Step 1: Start the board

```sh
cd apps/kanban && go run . -with-worker=false
```

You get a three-column board — Todo, Doing, Done — with a text box, add
button, and per-column move/remove buttons. The bottom panel shows the
MCP endpoint, `http://127.0.0.1:7778/mcp`, and behind `ctrl+t` a live
log of every request the server handles — including the one that is
reading the log.

Two flags matter here. `-mcp` is the listen address and already defaults
to `127.0.0.1:7778`; pass it to move the server or `-mcp=""` to disable
it. `-with-worker` defaults to **true**, and it launches the demo's
optional Python Temporal companion — which needs a venv and a Temporal
server, and this tutorial needs neither, so turn it off.
[Tutorial 9](09-temporal.md) picks that thread up.

Serving MCP from your own app is one call:

```go
srv, err := mcp.Serve(app, mcp.Options{Addr: "127.0.0.1:7777", Context: ctx})
defer srv.Close()
```

`Options.Context` is the markup binding context: it supplies the `Named`
element table, the bindable values, the commands, and the environment
`swap_markup` builds against. Without one, the name-addressed tools
report that the app has no markup context and the tree and screen tools
still work. `Options.Addr` **must be loopback**.

Two things to notice before touching the wire:

- **`kanban` is its own Go module.** Importing `gooey/mcp` pulls in
  the MCP SDK's dependency graph, and core gooey's `go build ./...`
  must never see it — the same quarantine that puts `mcp/`, `grpc/` and
  `handlers/temporal` in nested modules. Run it from its own directory.
- **Serving is opt-in, and loopback-only.** Nothing listens unless the
  app passed an address, and a non-loopback address is a hard error,
  not a warning. v1 has no authentication, so the bind address is what
  decides *who* may connect. What a connection may then *do* is a
  second, separate question, and it has an answer: an endpoint served
  with `mcp.Options.Grant` (or `grpc.Options.Grant`) is scoped to one
  named element's subtree and one list of value names — see
  [island grants](../specs/2026-08-14-island-grants.md). Without a
  grant the old sentence still holds in full: an MCP client can do
  anything the keyboard can. Read the two together — the address is
  authentication's stand-in, the grant is authorization, and neither
  substitutes for the other. (There is also an `Origin` guard so a browser page on
  another local port cannot drive your terminal — see the
  [MCP server spec](../specs/2026-08-10-mcp-server.md) for why every
  clause of that check — default-deny for anything claiming to be a
  browser, exact loopback hostname match, port pin — is load-bearing.
  gRPC needs no Origin machinery because browsers cannot speak it
  natively.) One caveat: `mcp.Serve` owns the listener and enforces the
  loopback rule. If you take that over with `mcp.New` and mount the
  handler on your own `http.Server` — as kanban does, to wrap it —
  the guarantee becomes *your* problem, which is why kanban
  re-implements the check.

## Step 2: Connect a client

The endpoint is streamable HTTP at `http://127.0.0.1:7778/mcp`, running
stateless with JSON responses — so a bare `curl` works, with no
`initialize` handshake first:

```sh
curl -s http://127.0.0.1:7778/mcp \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'
```

Any MCP client speaks to it the same way. To hand the board to Claude
Code:

```sh
claude mcp add --transport http kanban http://127.0.0.1:7778/mcp
```

The tool inventory: `tree_snapshot`, `screen_text`, `list_values`,
`list_styles`, `invoke_command`, `set_value`, `send_keys`, `send_mouse`,
`focus`, `swap_markup`, `patch_markup`, `validate_markup`,
`register_properties`, `unregister_properties`. The rest of this tutorial exercises the
important ones; the calls below all use the same `tools/call` shape:

```sh
curl -s http://127.0.0.1:7778/mcp \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/call",
       "params":{"name":"tree_snapshot","arguments":{}}}'
```

## Step 3: Snapshot the tree

`tree_snapshot` walks the live composition and returns it as data: type
names, bounds, layout, visibility, focus and hover flags — and, for
every element with a `Name=` attribute, that name. You will find
`NewTitle`, `AddBtn`, `TodoList`, `DoingList`, `DoneList`, each
column's buttons, `McpTabBtn`, `LogTabBtn`, `LogPanel`.

Those names are the point. **Everything you named in markup for your
own sanity is an address a client can act on.** Nothing in
`kanban` was written for the agent's benefit; the automation
surface falls out of ordinary markup discipline.

There is a deliberate ceiling: built-in widgets serialize their
interesting fields via type switches, but a third-party Go component
reports only its `%T`, bounds and children — its fields cannot be
discovered without reflection, and gooey's core has none. Controls
that declare their surface with `<x:Property>` serialize those
declarations and current values.

`list_values` is the companion discovery tool: the bindable value names
and their kinds — property, command, or literal — so a client can find
out what `set_value` and `invoke_command` can actually reach instead of
guessing at names.

## Step 4: Read the screen

```json
{"name":"screen_text","arguments":{}}
```

You get the terminal's contents as plain text — the screenshot, minus
the pixels. It reads the **retained cell plane as of the last frame**,
deliberately never composing a fresh one: composing here would mark
dirty nodes clean and steal the app's own damage counts, and tutorial 3
taught you those counts are the framework's honesty mechanism.

## Step 5: Press a button — two ways

The semantic route addresses the viewmodel directly:

```json
{"name":"set_value","arguments":{"name":"NewTitle","value":"written by an agent"}}
{"name":"invoke_command","arguments":{"name":"AddTask"}}
```

The input route replays what a keyboard would have done:

```json
{"name":"focus","arguments":{"name":"TodoList"}}
{"name":"send_keys","arguments":{"keys":["down"]}}
```

Both work on this app; the input route also works on an app with no
markup context at all, because it goes through the same dispatch path
as the terminal (with one exception: the app-level quit key is out of
a client's reach, deliberately).

Now call `screen_text` again, immediately. The new card is there. No
sleep, no retry loop — and that is a guarantee, not luck:

**Every tool call marshals onto the UI goroutine and waits for the
settle barrier.** Properties are UI-goroutine-confined
([how-to: work off the UI goroutine](howto/howto-async.md)), so a tool
arriving on an HTTP goroutine is posted to the Dispatcher and runs on
the loop — and then the bridge posts a second, empty closure and waits
for *that* too. Because `Dispatcher.Drain` snapshots its queue, the
barrier lands in the *next* drain, and the run loop composes a frame
between two drains — so when the call returns, the repaint its `Set`s
asked for has already happened. `screen_text` after `invoke_command`
sees the new pixels by construction. A panic inside a tool (say,
`set_value` on a computed) is recovered on the UI goroutine and
returned as a tool error; a client cannot kill the app.

## Step 6: Swap the page, live

`swap_markup` replaces the page through the same path hot reload uses,
built against the app's existing context — the viewmodel survives:

```json
{"name":"swap_markup","arguments":{"source":
  "<Gooey xmlns=\"wonderforge.io/gooey/2026\">\n  <Border Title=\"swapped over the wire\" Style=\"panel\">\n    <VStack Gap=\"1\">\n      <Text Style=\"accent\">this page arrived as an MCP argument</Text>\n      <TextBox Name=\"NewTitle\" Text=\"{{.NewTitle}}\"/>\n      <Button Name=\"AddBtn\" Content=\"Add\" Click=\"{{.AddTask}}\"/>\n    </VStack>\n  </Border>\n</Gooey>"}}
```

The board is gone; the two-widget page is on screen, still bound to the
same `NewTitle` and `AddTask`. Swap the original markup back the same
way. Three companions to know:

- **Bad markup changes nothing.** A failed build restores the previous
  tree *and* the previous `Named` table, so one typo does not break
  every name-addressed tool that follows. `validate_markup` runs the
  exact parse-and-bind path with the attach cut off — no frame is
  composed — and reports invalidity as a normal result, which is what
  a write→check→regenerate loop wants to branch on.
- **`patch_markup` replaces one named subtree** instead of the page:
  the fragment's root must carry the same `Name=` as the element it
  replaces, layout attributes not restated are preserved from the old
  element, and sibling state — focus, caret, scroll — survives by
  construction because the surviving components keep their nodes.
- **`register_properties` (or `swap_markup`'s `register` argument)
  grows the viewmodel over the wire** — without it, a swapped page
  could never bind a name the app didn't pre-register. Commands cannot
  be registered: behavior needs code, not storage.
- **`unregister_properties` shrinks it again.** A loop that invents a
  name per generated thing needs to stop inventing them permanently.
  Removal never disturbs the RUNNING tree — a component already bound
  to the name keeps its handle and keeps rendering — it only takes the
  name out of scope for markup built afterwards, so patch the markup
  first and unregister second.

This loop — generate, validate, swap, look — is exactly what
`apps/kanban/worker` automates: a Python Temporal worker that
has Claude write markup and pushes it into this app's `swap_markup`.
Drop the `-with-worker=false` from step 1 (and point `-worker-python`
at a venv) to see it as a one-shell arrangement
([Tutorial 9](09-temporal.md) and
[how-to: companions](howto/howto-companions.md) explain the pieces).

## One path, one model

MCP is not a second implementation of anything. Every tool body is a
thin adapter over **`control.Service`** in the root package
[`control/`](../../control/control.go): one in-process implementation
of the `gooey.control.v1` surface, UI-goroutine-only, with
`control.Bridge` owning the settle barrier. The gRPC server is another
adapter over the same service. A tool or an RPC does what `control`
does, or it does not exist — so the two transports cannot drift, and
the [contract](../specs/2026-08-10-grpc-contract.md) carries a
tool-to-RPC mapping table that is argument-for-argument mechanical.

## gRPC or MCP?

Same surface, different consumers. Try the gRPC flavor — it is its own
driver:

```sh
cd grpc && go run ./cmd/grpcdemo -grpc 127.0.0.1:7788   # the app
cd grpc && go run ./cmd/grpcdemo -drive 127.0.0.1:7788  # another shell
```

The driver snapshots the tree, sets a property, invokes a command three
times over a streaming session, and prints every `FrameDelta` the app
pushes — including `repainted`, the same damage count the framework's
own tests assert on, now measurable over the wire.

| Pick | When |
|---|---|
| **MCP** (`mcp/`) | The client is an AI agent or anything that already speaks MCP. Tools are self-describing (`tools/list`, schemas, `structuredContent`), arguments are JSON, and attaching Claude Code is one command. Request/response only. |
| **gRPC** (`grpc/`) | The client is a program you are writing. You get generated, committed clients — Go (`grpc/gen/gooey/control/v1`), Python ([`clients/python`](../../clients/python), `pip install ./clients/python`), TypeScript in both Connect-ES and grpc-js flavors ([`clients/ts`](../../clients/ts)) — typed values (`TypedValue` mirrors markup's property-kind table exactly), gRPC status codes instead of error strings, and `SessionService.Attach`: one ordered bidi stream where acts apply in order and every composed frame arrives as one atomic `FrameDelta`. |

If you need *push* — properties and damage streaming to you framed on
composed frames rather than you polling `screen_text` — that is gRPC
territory; MCP has no session stream. If you need an agent to discover
what it can do, that is MCP's whole design. Both wait on the same
settle barrier, so the after-a-write-reads-see-it guarantee is
identical.

## What you learned

- A running gooey app opts into being a server; the tree, the `Named`
  table, and the context's values ARE the API — nothing extra is
  declared.
- Every remote call runs on the UI goroutine via the Dispatcher and
  returns only after the frame it caused — reads after writes see the
  new screen, without sleeps.
- `swap_markup`/`patch_markup` are the hot-reload path over the wire,
  atomic on failure; `register_properties` grows the viewmodel so
  generated pages can bind new names.
- MCP and gRPC are two skins over one `control.Service` — pick by
  consumer, not by capability.
- Loopback-only, no auth: the client can do anything the keyboard can.

## Still missing

- **No authentication, no remote binds.** The intended shape is that a
  non-loopback listen requires TLS + per-RPC credentials, and it lands
  with the first remote bind or not at all.
- **`set_value` has a kept v1 ceiling**: duration- and `any`-typed
  properties are reported by `list_values` but refuse `set_value`
  (registration accepts the full kind table; writing those kinds is a
  deliberate follow-up). The gRPC `SetProperty` carries the full table.
- **Third-party Go components snapshot as `%T` + bounds only** unless
  they declare `<x:Property>` surfaces.
- **`patch_markup` needs a rewritable parent** — the built-in
  containers whose child sets are public fields. A third-party
  container's children cannot be rewritten without reflection.

## Next steps

- **[Tutorial 9: Temporal end-to-end](09-temporal.md)** — the same
  markup boundary, with a workflow on the far side.
- [How-to: companions](howto/howto-companions.md) — the process
  lifetime the `-with-worker` flag rides on.
- [How-to: test a gooey app](howto/howto-testing.md) — the in-process
  versions of these same assertions.
- [`clients/README.md`](../../clients/README.md) — consuming the
  generated Python and TypeScript clients.
- Specs (ground truth): [MCP server](../specs/2026-08-10-mcp-server.md)
  · [gRPC contract](../specs/2026-08-10-grpc-contract.md)
