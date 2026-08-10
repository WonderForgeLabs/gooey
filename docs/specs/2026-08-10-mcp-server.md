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
