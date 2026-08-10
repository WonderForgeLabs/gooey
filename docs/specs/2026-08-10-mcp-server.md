# MCP server: direct tree manipulation of a running app (design)

Directive from Elan 2026-08-10: "add an MCP server to the gooey app
that allows for tree manipulation directly." A running gooey app
becomes an MCP host: an AI agent (or any MCP client) can inspect the
live component tree, read the screen, drive input, mutate state, and
replace markup — the automation/accessibility surface and the
live-edit surface in one protocol.

## Placement & transport

- Opt-in App integration: something like `app.ServeMCP(addr)` or an
  App option. Never on by default.
- Transport: streamable HTTP on localhost (or a unix socket) — stdio
  is owned by the terminal in a TUI. Loopback-only bind by default.
- Dependency policy: evaluate the official Go SDK
  (modelcontextprotocol/go-sdk) footprint first; if it drags a heavy
  graph, isolate exactly like Temporal — a nested module (e.g.
  `mcp/`) so the core stays clean. A raw JSON-RPC/streamable-HTTP
  implementation in std lib is acceptable if the SDK is unsuitable —
  the protocol surface needed here is small.

## The concurrency rule (invariant)

MCP requests arrive on HTTP goroutines. Properties are UI-goroutine-
confined. EVERY tool that touches the tree or properties marshals
through the Dispatcher (Post + reply channel) and runs on the UI
loop. No exceptions; the tool handler never holds tree references
across requests.

## Tools (v1)

Read:
- `tree_snapshot` — the component tree serialized by walking
  Container/Attacher: type names, Name= identities, bounds, layout,
  visibility, focus/hover flags. Type-switch serialization; no
  reflection.
- `screen_text` — the current cell buffer as plain text (+ an option
  for styled/SGR form): the "screenshot".
- `list_values` — the markup Context's value names and kinds
  (property handle vs command vs literal), so an agent can discover
  the bindable surface.

Act:
- `invoke_command` — run a named Command from the context (button
  semantics without coordinates).
- `set_value` — set a named source property (typed: string/int/bool
  via the existing boundProp-style type switches).
- `send_keys` / `send_mouse` — inject input.Events through
  Composer.Handle (works on ANY app, markup or Go-composed).
- `focus` — move focus to a Named element.

Mutate structure (the "directly" part):
- `swap_markup` — replace the page with provided gooey source through
  the same swap path hot reload uses (load against the app's context;
  errors return to the client, tree stays).
- `patch_markup` — stretch goal: targeted subtree replacement by
  Name; defer if it needs machinery that doesn't exist.

## Security posture

Loopback bind + opt-in flag is v1. Note in docs: an MCP client can do
anything the keyboard can. Token auth and remote binds are future
work, not v1.

## Proof

A demo app served over MCP, driven end-to-end by a scripted MCP
client (raw streamable-HTTP JSON-RPC is fine): snapshot → read screen
→ click a button via invoke_command → assert the screen changed →
swap_markup and verify the new UI renders. Evidence per house
conventions.

## As built (2026-08-10) — see the override below for the transport

Shipped in `mcp/`, with `mcp/cmd/mcpdemo` and `mcpdemo.gif`.

**Superseded: "no SDK, no nested module."** The first build wrote the
protocol by hand and lived in the root module, on the argument that
modelcontextprotocol/go-sdk v1.7.0 adds eight modules — jsonschema-go,
segmentio/{asm,encoding}, uritemplate, x/{oauth2,sync,time} — to a
framework whose graph is x/term, and that its ergonomic path (`AddTool`
deriving schemas from Go structs via jsonschema-go) is reflection.
Elan overrode that on 2026-08-10 ("no, use the sdk"). The dependency
weight is handled by the *established* mechanism instead — a nested
module — rather than by reimplementing a protocol. See "Overridden"
below; everything else in this section still describes what runs.

**Transport (superseded).** The hand-written layer was POST-only
streamable HTTP: one JSON-RPC message or batch in, `application/json`
out, GET 405, DELETE dropping a stateless session, protocol versions
2024-11-05 … 2025-11-25 echoed. The SDK's handler now does all of this.

**The marshaling primitive is `bridge.do`, and it waits TWICE.** The
first wait is the tool body. The second is a bare barrier, and it is
what makes the surface usable: `Dispatcher.Drain` snapshots its queue,
so a closure posted during a drain lands in the *next* one, and the run
loop composes a frame between two drains. Waiting for the barrier
therefore waits for the repaint the tool's `Set`s asked for — which is
why `screen_text` immediately after `invoke_command` sees the new
pixels, and why none of the tests need sleeps. A panic inside a tool
(e.g. `Set` on a computed property) is recovered on the UI goroutine
and returned as a tool error: an MCP client must not be able to kill
the app.

**`Tool.Run` is *defined* as running on the UI goroutine** and dispatch
is its only caller, so the confinement rule is structural rather than
remembered. `Host.Composer()` and `Host.Swap()` are consequently only
ever called from inside a `Run`, which is why `App.comp` still needs no
lock.

**Root-package additions** (three, all small and independently
useful): `Composer.Root()`, `Composer.Cells()` — the retained plane as
of the last frame, deliberately NOT a fresh compose, since composing
here would mark dirty nodes clean and steal the app's own damage count
— and `App.Swap(root)`, the hot-reload attach path exposed for a tree
that arrives from somewhere other than `Content`.

**`ServeMCP` is `mcp.Serve(app, opts)`, not a method.** The server
needs `markup`, `markup` needs `gooey`; a method on `App` would be an
import cycle.

**Deviations from v1 above.** `patch_markup` is punted — targeted
subtree replacement needs an addressing scheme and a re-parent path
that do not exist, and `swap_markup` against a surviving viewmodel
covers the use case. Results are text content only (no
`structuredContent`, which wants an `outputSchema` this server does not
publish). `send_keys` routes through `Composer.Handle`, not the App's
handler, so the app-level quit key is out of reach of a client. Bad
markup restores the *previous* `Named` table as well as the tree —
otherwise a typo in `swap_markup` would silently break every
name-addressed tool.

**Ceiling worth naming.** `tree_snapshot` type-switches the built-in
widgets for their interesting fields; a third-party widget serializes
with its `%T`, bounds, layout and children but no props, because its
fields cannot be discovered without reflection. `<x:Property>`
(2026-08-10-markup-declared-properties.md) is the declaration that
would lift it.

**Security posture as shipped.** Loopback binds only — a non-loopback
`Addr` is a hard error, not a warning. No auth: an MCP client can do
anything the keyboard can. (The SDK adds a second, complementary guard;
see the override section.)

The `Origin` check is the loopback trust boundary and is **default-deny
for anything claiming to be a browser**. Absent header → allow (Go and
Node clients do not set one). Present → it must parse, have scheme
`http`/`https`, have hostname exactly `localhost` / `127.0.0.1` / `::1`,
and — when the port we are reachable at is knowable — match that port,
so another local service's page (a dev server on `:3000`) cannot drive
this app. A duplicated `Origin` header is refused rather than guessed
at.

That precision is not theoretical. The first version allowed an empty
hostname, and `url.Parse` yields an empty hostname for `null` (what a
sandboxed iframe and a `file://` page send), for unparseable junk, for
an empty value, and for `file://` URLs — so the guard was a no-op for
the exact attack it existed to stop. Two more shapes are worth
remembering: `//localhost:7788` has no scheme but *does* parse to
hostname `localhost`, which is why the scheme allowlist is load-bearing;
and `http://localhost:7788@evil.com` parses to host `evil.com`, while
`http://127.0.0.1.evil.com` is an ordinary DNS name that merely starts
like a loopback address — exact hostname matching rejects both, prefix
matching would not. `checkOrigin` is a free function precisely so the
table test can enumerate all of these without a listener; restoring the
old logic fails ten of its rows.

## Overridden 2026-08-10: the official SDK, in a nested module

Directive from Elan: **"no, use the sdk."** The dependency-weight
argument that justified hand-writing the protocol is answered by the
mechanism this repo already has for exactly that problem — a nested Go
module — not by reimplementing a spec. The SDK's internal use of
reflection for its own schema derivation is accepted at this protocol
boundary; gooey core's no-reflection invariant is untouched, because
core does not import this module.

**`mcp/` is now a module.** `mcp/go.mod` requires
`github.com/modelcontextprotocol/go-sdk v1.7.0` with `replace
github.com/WonderForgeLabs/gooey => ../`, exactly like
`handlers/temporal`. The root graph is still three nodes — the module
itself, `x/sys`, `x/term` — and `go build ./...` / `go test ./...` at
the root skip `mcp/` entirely, which is the mechanical proof. The
module gains eight: the SDK, `google/jsonschema-go`,
`segmentio/{asm,encoding}`, `yosida95/uritemplate/v3`, `x/oauth2`,
`x/sync`, `x/time`. `cmd/mcpdemo` moved to `mcp/cmd/mcpdemo` for the
same reason the Temporal demos live in their module: a binary that
imports the SDK must build from inside the quarantine. `cmd/browser`
lists it through the `modDir` root mechanism.

**What the SDK now owns.** JSON-RPC framing, batching, the `initialize`
handshake and protocol-version negotiation, `tools/list` (with paging),
`tools/call` routing, `ping`, capability advertisement, the
streamable-HTTP rules (Accept/Content-Type validation, body limits), and
a Host-header DNS-rebinding guard that rejects a loopback-served request
whose `Host` is not loopback. `jsonrpc.go` — 424 lines — is deleted.

**What stayed custom, and why.** The `bridge` and its double wait; the
`Tool` type whose `Run` is *defined* as UI-goroutine-only; the
panic-recovery-to-tool-error; the `swap_markup` `Named`-table restore;
the hand-written schemas; the loopback bind check; and the `Origin`
guard. The last two are the load-bearing ones:

- `checkLoopback` is ours because the SDK does not decide where you
  bind. A non-loopback `Addr` is still a hard error.
- The `Origin` guard is ours because **the SDK does not check Origin by
  default.** In v1.7.0 `StreamableHTTPOptions.CrossOriginProtection` is
  nil unless set, and it is deprecated in favour of "wrap the handler
  with cross-origin protection middleware" — which is what
  `Server.originGuard` does. Wrapping rather than adopting also keeps
  the port pin: `net/http`'s `CrossOriginProtection` is a
  Sec-Fetch-Site/Origin CSRF check with no notion of *which* loopback
  port is legitimate, and the port rule is the thing that stops another
  local service's page from driving this app. All eight enumerated
  origin cases pass unchanged against the new stack, plus a live check
  against the running `mcpdemo` binary.

**The settle guarantee survives the port.** `bridge.do` runs inside the
SDK's tool handler, so the handler does not return — and the SDK does
not write the response — until the barrier round has come back and the
frame has been composed. No sleeps were added anywhere.

**Behavioral deltas, all in the transport envelope.** The handler runs
`Stateless: true` with `JSONResponse: true`, which fits a server with
exactly one app behind it: each POST is independent, the SDK synthesizes
initialized state for a request that arrives without a handshake (so a
bare `tools/call` from curl still works), and responses are
`application/json`. Consequences: `Mcp-Session-Id` is no longer minted
at `initialize` (it named nothing), and `DELETE` is 405 rather than 204
(there is no session to delete). `GET` is still 405. One error shape
changed: a method the protocol does not define is refused by the SDK's
transport as HTTP 400 with a plain-text body, where the hand-written
layer returned a JSON-RPC `MethodNotFound`; an unknown *tool* is still a
JSON-RPC `InvalidParams`. `TestNoStreamAndNoSession` and
`TestUnknownMethodAndUnknownTool` pin all of this. The tool inventory,
argument handling, result text and error wording are unchanged, so
`mcpdemo.gif` still shows what happens.
