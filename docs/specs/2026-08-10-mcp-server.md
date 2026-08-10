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

## As built (2026-08-10)

Shipped in `mcp/` (root module, `cmd/mcpdemo`, `mcpdemo.gif`).

**No SDK, no nested module.** modelcontextprotocol/go-sdk v1.7.0 adds
eight modules — jsonschema-go, segmentio/{asm,encoding}, uritemplate,
x/{oauth2,sync,sys,time} — to a framework whose graph is x/term, and
its ergonomic path (`AddTool` deriving schemas from Go structs via
jsonschema-go) is reflection. The needed protocol surface is
`initialize`, `tools/list`, `tools/call`, `ping`, so it is written
against net/http and encoding/json with hand-written schemas. Zero new
dependencies means there is nothing to quarantine, so it lives in the
root module and `go test ./...` covers it — the opposite call from
`handlers/temporal`, for the opposite reason.

**Transport.** POST-only streamable HTTP: one JSON-RPC message or
batch in, `application/json` out. GET is 405 (nothing here is
server-initiated, so there is no stream to hold open); DELETE drops a
session. Sessions are minted at `initialize` and carry no state — one
app, one tree, every client sees the same one. Protocol versions
2024-11-05 … 2025-11-25 are echoed; anything else gets 2025-06-18.

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
anything the keyboard can.

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
