# gRPC control plane: the proto contract (decision record)

Directive from Elan 2026-08-10 (#108): "define the proto contract first
and generate clients in go/python/ts. then implement. mcp impl must
traverse same api path ultimately. one path, one model." This record is
child 1 (#109): the contract everything else in the epic is generated
from or implemented against. No server, no clients, no generated code
here — the `.proto` files under `proto/` and this record are the
deliverable; #110 generates, #111 implements, #112 reroutes MCP.

## The one model

gooey already has three remote-shaped surfaces, each grown separately:
the MCP tool inventory (`mcp/tools.go`), the server-driven
`Values map[string]string` contract (`handlers/temporal`), and the
x:Property declaration blocks that the markup-declared-properties
record noted "ARE a per-control wire schema" with nothing yet reading
them that way. The contract's job is to be the one model those three
become views of:

- **The type system is markup's `propKinds` table** (`markup/property.go`),
  carried onto the wire as `TypedValue` — a oneof with exactly one case
  per table row. Not a second model: adding a wire type means adding a
  propKinds row first, then the matching oneof case. The two grow in
  lockstep or not at all.
- **The action surface is the MCP tool inventory**, renamed into RPC
  conventions but argument-for-argument the same, so #112 is mechanical
  (mapping table below).
- **The schema surface is the x:Property declaration**, serialized
  verbatim (`PropertyDeclaration`: name, kind, default literal,
  required) — the declaration block finally read as the schema it is.
- **The streaming shape is the framework's own**: the client side of a
  session is one ordered stream of acts, exactly as the terminal is one
  ordered `input.Event` stream; the server side is framed on composed
  frames, exactly as the damage discipline is.

## Package and versioning

**`package gooey.control.v1`**, proto3, files under
`proto/gooey/control/v1/` (buf `PACKAGE_DIRECTORY_MATCH`). "control"
because this is the control plane of a running app — the same word the
epic uses — leaving room for sibling packages (a future
`gooey.serve.v1` for served-markup transport, say) without a v2.

Evolution rules:

- **Within v1: additive only.** New fields, new oneof cases, new enum
  values, new RPCs. Removing or renaming anything, changing a field's
  type or number, is a breaking change and does not happen in v1;
  `buf breaking` with the `FILE` category (declared in `buf.yaml`)
  becomes a CI gate when #110 lands the pipeline.
- **Removed fields get `reserved`** — number and name — so they cannot
  be silently reused with different semantics.
- **Reservation is also used forward**, as a seat-holder: `TreeNode`
  reserves 12–15 for the semantic tree (#101). Un-reserving a number is
  legal in protobuf and deliberately visible in review — that is the
  act of landing #101, and nothing can squat those numbers meanwhile.
  Earmarks that SHOULD eventually be used (future propKinds rows,
  TypedValue 8–15) are comments, not reservations, since `reserved`
  forbids use rather than deferring it.
- **A breaking redesign is `gooey.control.v2`**, a new package beside
  v1, both served during migration. Standard proto package versioning;
  no in-place breakage ever.

## TypedValue: the type table on the wire

| propKinds row | oneof case | proto type | notes |
|---|---|---|---|
| `string` | `string_value` | `string` | |
| `int` | `int_value` | `sint64` | Go `int`, carried wide; server rejects out-of-range with `INVALID_ARGUMENT`. zigzag because counters go negative. |
| `bool` | `bool_value` | `bool` | |
| `float` | `float_value` | `double` | Go `float64` exactly. |
| `duration` | `duration_value` | `google.protobuf.Duration` | well-known type; no dependency cost, buf ships it. |
| `color` | `color_value` | `Color{set, red, green, blue}` | mirrors `render.Color` **including `Set`**: unset-use-terminal-default must stay distinguishable from black. |
| `any` | `any_json` | `bytes` (UTF-8 JSON) | the escape hatch, exactly as in propKinds. JSON, not `google.protobuf.Any`: `Any` carries a type URL into a registry that reflection-free gooey neither has nor wants; JSON matches how `any` values already cross every other boundary (MCP results, served values). |

What is deliberately NOT in TypedValue: `render.Style` and `[]float64`
handles, which `list_values` reports today. They are not propKinds rows,
so they cross only as descriptors — `ValueInfo.go_type` names them,
`ValueInfo.value` stays empty — same ceiling the MCP surface has. If
they ever earn markup literals they join propKinds and then the oneof,
in that order.

`ValueKind` is the companion enum (one value per row) used where a type
is described rather than carried: schemas (#62) and registrations
(#89).

## The RPC surface

Two services in three files: `types.proto` (shared messages),
`control.proto` (unary), `session.proto` (streaming).

### ControlService — unary

Every RPC marshals onto the UI goroutine and waits for the settle
barrier before answering, promoting the MCP bridge's guarantee into the
contract: `ScreenText` immediately after `InvokeCommand` sees the new
pixels. Errors are gRPC status codes (`NOT_FOUND` unknown name,
`INVALID_ARGUMENT` type mismatch naming both types / bad markup / bad
gesture, `FAILED_PRECONDITION` no context or composition,
`DEADLINE_EXCEEDED` blocked run loop), not payload fields.

| RPC | what |
|---|---|
| `SnapshotTree(depth)` | the live tree: types, Name= identities, bounds, layout, focus/hover, type-switched props |
| `ScreenText(styled)` | the retained cell plane — never composes a frame (would steal the app's damage counts) |
| `ListValues()` | the bindable surface + the Name= table |
| `GetProperty(name)` | one dotted name, resolved as `{{.A.B}}` resolves |
| `SetProperty(name, TypedValue)` | typed write; mismatch changes nothing |
| `InvokeCommand(name)` | a named command |
| `SendKeys(text, gestures)` | key events into the one ordered input stream; via the composition, so the quit key is out of reach |
| `SendPointer(PointerEvent)` | one pointer event; `CLICK` is synthesized press+release, as the dispatcher does |
| `SetFocus(name)` | focus to a named focus stop |
| `SwapMarkup(source, register[])` | page replacement over the surviving viewmodel, optionally growing it first (#89); atomic on failure |
| `RegisterProperties(registrations)` | grow the viewmodel without swapping (#89); existing name = error, one source of truth |
| `GetDeclaredSchema(source?)` | an x:Property block as `ControlSchema` (#62); empty source = the running page's document |

### SessionService — one bidi stream

`Attach(stream AttachRequest) returns (stream AttachResponse)`. First
client message is `Subscription` (opt-in channels: properties with
optional name filter, frames, input echo, lifecycle); every later one
is an `Act` — a client-numbered envelope whose oneof reuses the
**unary request messages verbatim** (SetProperty, InvokeCommand,
SendKeys, SendPointer, SetFocus, SwapMarkup, RegisterProperties). Acts
apply in stream order on the UI goroutine — the remote mirror of the
one ordered input stream — and each is answered by one `ActResult`
(same id, status code + the unary response message), in-stream so a
failed act does not tear down the session.

Server messages: `Welcome` (identity + screen size + frame seq),
`ActResult`, `FrameDelta`, `LifecycleEvent` (Resized / Swapped /
Closing), `InputEcho`.

**`FrameDelta` is the consistency mechanism.** Everything one composed
frame changed — property deltas, damage rects, the repaint count —
arrives in ONE message carrying the frame's sequence number. The
torn-read #49 guards against (values arriving without the markup/frame
they belong to) is impossible by construction: there is no separate
delta channel to race the structure channel. A markup swap is a
`Swapped` lifecycle event ordered on the same stream.

`FrameDelta.repainted` is the damage-discipline number — the same count
the framework's contract tests assert on (focus moves repaint exactly
2). Putting it on the wire makes the measurable guarantee remotely
measurable.

**Implementation constraint carried into #111** (contract-shaped, so
recorded here): delta collection must not perturb the app's own damage
counts or subscribe the server into the property graph from the wrong
call site — reads for delta extraction happen outside computed
evaluation, per the call-site rule, on the UI goroutine at frame
boundaries. The contract deliberately does not promise deltas for
properties never read by the UI or a session.

## MCP tool → RPC mapping (the #112 table)

Every v1 MCP tool, argument-for-argument:

| MCP tool | args | RPC | notes |
|---|---|---|---|
| `tree_snapshot` | `depth` | `ControlService.SnapshotTree` | `depth` → `depth`; JSON tree → `TreeNode` |
| `screen_text` | `styled` | `ControlService.ScreenText` | identical semantics, Snapshot-not-Flush preserved |
| `list_values` | — | `ControlService.ListValues` | `values` → `ValueInfo[]`, `named` → `named` |
| `invoke_command` | `name` | `ControlService.InvokeCommand` | |
| `set_value` | `name`, `value` (JSON) | `ControlService.SetProperty` | untyped JSON value becomes `TypedValue`; the type-switch check becomes the oneof case check |
| `send_keys` | `text`, `keys[]` | `ControlService.SendKeys` | `keys` → `gestures`, same markup gesture syntax |
| `send_mouse` | `kind`, `x`, `y`, `button` | `ControlService.SendPointer` | enums replace strings; `click` = `POINTER_KIND_CLICK`, still synthesized press+release |
| `focus` | `name` | `ControlService.SetFocus` | renamed to avoid colliding with the noun |
| `swap_markup` | `source` | `ControlService.SwapMarkup` | gains optional `register[]` (#89); Named-table restore on failure is now contract behavior |

New surface with no MCP predecessor: `GetProperty`,
`RegisterProperties`, `GetDeclaredSchema`, `SessionService.Attach`.
When #112 lands, the MCP tools become a thin adapter over the same
in-process service implementation the gRPC server exposes (not a
loopback network hop) — one path; MCP is a transport skin, and any new
tool must name the RPC it fronts.

## How the absorbed issues map

| issue | where it lands |
|---|---|
| #46 typed values contract | `TypedValue`/`ValueKind` replace `map[string]string`; `PropertyChange` carries an explicit value, so the empty-string-must-be-present trap is unrepresentable; validation against `ControlSchema` happens before build with errors naming the property |
| #49 push transport | `SessionService.Attach`: server push framed on composed frames; `FrameDelta` atomicity preserves the consistent-pair rule; a dropped stream degrades to unary polling (`ScreenText`/`ListValues`), the fallback #49 requires |
| #62 per-control wire schemas | `PropertyDeclaration`/`ControlSchema` + `GetDeclaredSchema`; proto IS the "form a non-Go worker can consume" — #110's Python/TS clients get the schema types for free |
| #89 viewmodel growth | `PropertyRegistration`, standalone (`RegisterProperties`) and pre-swap (`SwapMarkupRequest.register`), rolled back on failed build; kinds from the same table; command registration explicitly excluded — behavior needs code, not storage |
| #101 semantic tree | `TreeNode` reserves 12–15 for role / accessible name / states; when #101 lands, `SnapshotTree` rides the semantic tree and the response shape extends without breaking |

## Transport and security posture

Carried over from the MCP server, unchanged in spirit:

- **Loopback-only default bind, hard error otherwise.** A control-plane
  client can do anything the keyboard can; v1 has no authentication, so
  a non-loopback listen refuses to start.
- **Non-loopback is an explicit, visible opt-in** in #111's options —
  and the intended shape is that the opt-in requires credentials to be
  configured (TLS + per-RPC token), not a bare flag. Remote binds
  arrive with authentication or not at all.
- **No Origin surface.** Browsers cannot speak gRPC/HTTP-2 natively;
  grpc-web is out of scope for v1 (below), so the MCP server's Origin
  guard has no gRPC counterpart to carry over. If a grpc-web or Connect
  gateway is ever added, the Origin/port-pin analysis in the MCP record
  applies to that gateway verbatim.
- **The Dispatcher is still the only door.** Same confinement rule,
  same settle barrier, same panic-recovery-to-status-error; a client
  must not be able to kill the app.

## Module layout plan (#110 / #111)

- **`proto/`** at the repo root: source of truth, no Go code, owns
  `buf.yaml`. Root `go build ./...` never sees it.
- **`grpc/`** — a nested Go module, exactly like `mcp/` and
  `handlers/temporal`: grpc-go and protobuf are quarantined; the root
  graph stays three nodes. Holds the generated Go
  (`grpc/gen/gooey/control/v1`, import path already fixed by
  `go_package` in the protos, package name `controlv1`) and, per #111,
  the server implementation plus its demo under `grpc/cmd/`.
- **`clients/python/`, `clients/ts/`** (#110): generated clients.
- **Committed-generated-code policy: commit the output.** `buf generate`
  writes it, the diff is reviewed like any code, and a CI drift check
  (`buf generate && git diff --exit-code`) plus `buf lint` and
  `buf breaking` against main become the gates in #110. Consumers of
  the repo never need buf installed; regeneration is an explicit act.
- `buf.gen.yaml` and plugin/tooling version pins are #110 deliverables,
  not this record's.

## Explicitly out of scope for v1

- **Semantic roles/states** (#101) — seats reserved in `TreeNode`, not
  designed here.
- **Command/behavior registration over the wire** — #89's noted
  boundary; a command needs code.
- **grpc-web / Connect / browser reachability** — no browser story, no
  Origin machinery needed yet.
- **Auth** — designed posture stated above; mechanism lands with the
  first non-loopback listener, not before.
- **Server-driven markup TRANSPORT** — how a Temporal workflow's
  markup+values reach a client is `handlers/temporal`'s (and possibly a
  future `gooey.serve.v1`'s) concern; this contract only fixes the
  types such a payload validates against (`ControlSchema`,
  `TypedValue`).
- **`patch_markup` / targeted subtree replacement** — still punted, as
  in the MCP record; `SwapMarkup` over a surviving (now growable)
  viewmodel covers the use case.
- **Multi-app routing** — one server, one app, like the MCP server; a
  session addresses "the app", not an app id. A future multiplexer adds
  a field, additively.

## Verification

`buf lint` clean (zero findings) under **buf 1.72.0**, run as
`go run github.com/bufbuild/buf/cmd/buf@latest lint` from `proto/`
(buf is not on PATH here; 1.72.0 is what `@latest` resolved to on
2026-08-10). Lint config is the `STANDARD` category — which is what
enforces the `Service` suffixes, `<Rpc>Request`/`<Rpc>Response` naming,
enum value prefixes and zero-value `_UNSPECIFIED` conventions visible
throughout. No generated code exists yet anywhere in the tree; that is
#110's first commit.

## Open questions for Elan

1. **Client toolchain for TypeScript** (#110): Connect-ES vs
   grpc-js/protobuf-ts changes what consumers install and whether a
   future browser path is cheap. Connect-ES is the modern default and
   would also give the Go server Connect's h2c/JSON niceties — but it
   is a product-surface choice, not a code one.
2. **Publish to a Buf Schema Registry?** Committing generated clients
   works without any account; a BSR module (`buf.build/wonderforge/gooey`)
   would let third parties generate their own. Infra/account decision.
3. **Package naming blessing**: `gooey.control.v1` as recorded, or the
   org-scoped `wonderforge.gooey.control.v1`? The protos ship with the
   former; renaming before #110 generates is free, after is breaking.
