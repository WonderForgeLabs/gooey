# Temporal visibility activity pack (design)

Direction set by Elan 2026-08-10 (epic #142): "build our gooey standard
library of temporal standalone activities. I want the full temporal
visibility api exposed, and i'd love to be able to stay true to their
proto api contract." Amended mid-flight: "We should ship as a temporal
activity pack."

The result is `packs/temporal-visibility`: a standalone Go module of
Temporal activities exposing the Visibility API, whose inputs and
outputs ARE `temporal.api.*` messages. gooey is a consumer of the pack,
not its home.

## Proto fidelity is the contract

Every activity is the thinnest possible wrapper over one RPC:

```go
func (a *Activities) ListWorkflowExecutions(
        ctx context.Context,
        req *workflowservice.ListWorkflowExecutionsRequest,
) (*workflowservice.ListWorkflowExecutionsResponse, error)
```

The request and response types come from `go.temporal.io/api` — the same
generated messages the server itself speaks. No hand-rolled DTOs, no
field renaming, no "friendly" wrappers. Three consequences, each the
point:

1. **Any SDK language interops.** Temporal's default payload converter
   handles proto messages natively (`json/protobuf` payloads via
   protojson, or binary proto). A Python worker or caller uses
   `temporalio.api.workflowservice.v1.ListWorkflowExecutionsRequest` —
   the same message, generated from the same `.proto` — and the wire
   payloads match. Zero impedance mismatch by construction.
2. **The API is already documented.** Temporal's own API reference is
   the pack's field-level documentation; we cannot drift from it because
   we do not restate it.
3. **Server evolution is free.** A new field on a visibility request
   reaches pack users by bumping `go.temporal.io/api`, with no pack code
   change.

The one deviation from verbatim pass-through is namespace defaulting
(below), and it only ever fills an empty field.

## Packaging: a standalone activity pack

The pack is its own nested module:

```
packs/temporal-visibility/
  go.mod        module github.com/WonderForgeLabs/gooey/packs/temporal-visibility
  visibility.go package visibility
  README.md
```

with **zero gooey imports** — its dependency graph is
`go.temporal.io/sdk` + `go.temporal.io/api` (and the protobuf runtime
they bring). Any Go Temporal worker imports the pack and registers the
activities without touching gooey's graph; this is proto-fidelity taken
to its packaging conclusion. The pack is a general-purpose Temporal
artifact that gooey happens to consume.

- **Naming convention for future packs:** `packs/temporal-<domain>`
  (this one is `temporal-visibility`; a schedules or task-queue pack
  would be `packs/temporal-schedules`, `packs/temporal-taskqueues`).
  The Go package name is the domain (`visibility`), so call sites read
  `visibility.Register(w, visibility.New(c))`.
- **gooey-side consumption:** `handlers/temporal` (already the nested
  module quarantining the SDK graph) requires the pack with a
  `replace ../../packs/temporal-visibility` directive, exactly as it
  requires the root gooey module. Its worker binary registers the pack;
  gooey contributes consumption only, no provider changes.
- **Version story:** publishing a Go nested module IS its import path
  plus a tag prefixed with the module directory —
  `packs/temporal-visibility/v0.1.0` — the same tag-driven shape as the
  generated clients under `clients/`. Until such a tag exists the pack
  is consumable by path (replace directive in-repo, pseudo-version from
  main outside). No publish workflow is added now; this paragraph is
  the record of how to cut one when wanted.

## Activity inventory

Registered activity names ARE the API — they are the strings a caller
in any language schedules, and the strings gooey markup names. The
convention is `visibility.<RPC name>`: the prefix scopes the pack, the
suffix is Temporal's own RPC name, verbatim.

| registered name | RPC |
|---|---|
| `visibility.ListWorkflowExecutions` | WorkflowService.ListWorkflowExecutions (the query-language path) |
| `visibility.ListOpenWorkflowExecutions` | WorkflowService.ListOpenWorkflowExecutions |
| `visibility.ListClosedWorkflowExecutions` | WorkflowService.ListClosedWorkflowExecutions |
| `visibility.CountWorkflowExecutions` | WorkflowService.CountWorkflowExecutions |
| `visibility.GetSearchAttributes` | WorkflowService.GetSearchAttributes (legacy, cluster-scoped) |
| `visibility.DescribeWorkflowExecution` | WorkflowService.DescribeWorkflowExecution (the detail pane's RPC) |
| `visibility.ListSearchAttributes` | OperatorService.ListSearchAttributes (the modern, namespaced schema surface) |

Notes on the surface:

- **The operator service needs no second connection.** The SDK's
  `client.Client` exposes both raw clients:
  `WorkflowService() workflowservice.WorkflowServiceClient` and
  `OperatorService() operatorservice.OperatorServiceClient`, sharing
  the one gRPC connection. `ListSearchAttributes` goes through the
  latter; everything else through the former. (The SDK notes that raw
  service calls skip the client's automatic retry interceptor — fine
  here: the caller invoked an *activity*, and retrying is the activity
  machinery's job, exactly once, per its retry policy.)
- **`GetSearchAttributes` is legacy** (its request message has no
  fields at all — it predates namespaced custom search attributes) but
  it is part of the visibility surface and the epic asks for the full
  surface. `visibility.ListSearchAttributes` is the one the phase-2
  dashboard should use.
- Names are exported as constants (`visibility.NameListWorkflowExecutions`
  etc.) so Go callers never retype the strings.

## Namespace defaulting

Every namespaced request (`GetSearchAttributesRequest` is the one
exception — it has no namespace field) gets one rule, applied
uniformly: **`req.Namespace == ""` → the worker's namespace.**

`visibility.New(c, visibility.WithNamespace(ns))` carries the worker's
namespace; unset it is Temporal's `"default"`. A `nil` request is
treated as the empty request (then defaulted), so zero-argument
invocations — a markup button with no proto in hand — list the worker's
own namespace. The default is filled into the request in place, which
is safe for activity invocations (the request was deserialized fresh)
and documented for direct Go calls. A request that names a namespace is
passed through untouched — the caller can query any namespace the
server will let it see; scoping *authority* is the server's job (and
the host's, by choosing what to register — in gooey's model,
registration is the capability grant).

## Pagination

`next_page_token` passes through verbatim, both directions — the pack
never interprets, stores, or re-issues it. A caller pages exactly as it
would against the raw gRPC API: send the empty token, get a token back,
send it in the next request. The token is opaque server state; any
cleverness here would be a lie about whose state it is.

## Context and heartbeats

These are fast unary calls: no heartbeats are recorded, and none are
needed — a visibility RPC either answers promptly or fails, and the
activity's `StartToCloseTimeout` (set by the invoker) is the right
knife. The activity context is passed straight into the RPC, so an
activity timeout or cancellation cancels the in-flight call.

## The property boundary (for gooey markup — recorded now, built in phase 2)

Proto responses cross into gooey's property graph as **protojson at the
edge**:

```
proto response ── protojson.Marshal ──> canonical JSON
                 ── json.Unmarshal ──> map[string]any / []any
                 ──> Property[string] (rendered text)  — v1, works today
                 ──> Property[any] + ItemsOf projection — phase 2
```

The shape: a list response protojson-marshals to
`{"executions": [...], "nextPageToken": "..."}`; the dashboard binds an
ItemsView to the `executions` array with a DataTemplate projecting
`execution.workflowId`, `type.name`, `status`, `startTime` per row.
protojson (not `encoding/json` on the generated structs) is the
mandated marshaller: it produces Temporal's canonical JSON field names
(`workflowId`, camelCase enums handling, RFC-3339 timestamps), i.e. the
same JSON every other Temporal tool shows, so the on-screen field names
match the API reference. This is a rendering decision at gooey's edge;
the activity contract itself stays proto end to end, and no reflection
enters gooey — the map is walked with type-switches like any other
`any` app data.

## Deployment

- **`workers/visibilityworker`** is a new standalone binary in
  `handlers/temporal/workers/` (the existing convention keeps `workers/`
  out of the demo browser, which scans only `cmd/`). A new binary rather
  than extending `temporalworker` because that binary's whole doc story
  is "the Slugify demo's compute half," `internal/slugify.Run` owns its
  worker internally, and a visibility pack worker wants its own task
  queue (`gooey-visibility`) and namespace flag. Same env conventions:
  `TEMPORAL_ADDRESS`, `TEMPORAL_NAMESPACE`, `GOOEY_TASK_QUEUE`.
- The phase-2 dashboard will run the same registration as a gooey
  **companion** (one shell, per `2026-08-10-companions.md`) — the pack's
  `Register(worker.ActivityRegistry, *Activities)` is deliberately
  registry-shaped so both deployments are one call.

## Version minimums

- **Invoking these activities standalone from a client** (gooey's
  `temporal:Activity` path) needs SDK ≥ 1.41 and server ≥ 1.31 — the
  standalone-activity feature's floor, recorded in
  `2026-08-10-remote-handlers-design.md`.
- **The pack itself** pins `go.temporal.io/sdk v1.47.0` (matching
  `handlers/temporal`) and takes its proto types from
  `go.temporal.io/api v1.63.4` (now a direct dependency — it was already
  in the graph as indirect). The visibility RPCs themselves are ancient
  and stable; a worker serving these activities to *workflow* callers
  has no special server floor.

## Pipeline-grammar v2 compatibility (designed for, not implemented)

Under the v2 provider contract (`2026-08-10-pipeline-grammar-v2.md`)
these activities are provider-owned-retry functions with no progress
capability — fast unary calls have nothing to heartbeat — and a natural
`FnInfo` of one string result (protojson text) until typed results
exist. Nothing in the pack changes for v2: it is worker-side code, and
v2 is a client/provider-side contract. The pack deliberately contains
no gooey types, so it cannot drift when the provider contract does.

## Explicitly out (this pass)

- The ops dashboard demo — phase 2 of the epic (workflow list with live
  query bar, counts, describe pane; dogfoods ItemsView).
- Non-visibility surfaces (schedules, task queues, batch operations) —
  future packs under the same convention.
- A publish workflow / version tag — recorded above, cut when wanted.
- Provider changes in `handlers/temporal` — consumption only.

---

## Executed (2026-08-10)

Landed as designed. The record of fact:

- `packs/temporal-visibility/` — standalone module, `package visibility`:
  `Activities` (client + namespace), `New`/`WithNamespace`,
  `Register(worker.ActivityRegistry, *Activities)`, seven activities,
  seven `Name*` constants, `AllNames()`. Zero gooey imports — enforced
  by the module graph itself: `go.mod`'s direct requires are
  `go.temporal.io/sdk`, `go.temporal.io/api`, and `google.golang.org/grpc`
  (the last only because the test fakes take `grpc.CallOption`).
- Unit tests fake the SDK client by interface embedding
  (`client.Client`, `workflowservice.WorkflowServiceClient`,
  `operatorservice.OperatorServiceClient` — override only what's
  called) and cover: namespace defaulting on every namespaced RPC,
  explicit-namespace pass-through, request field + page-token verbatim
  pass-through, response identity, nil-request tolerance, error
  propagation, and the exact set of registered names.
- `handlers/temporal` consumes the pack via `replace
  ../../packs/temporal-visibility`; `workers/visibilityworker` is the
  standalone worker binary (env: `TEMPORAL_ADDRESS`,
  `TEMPORAL_NAMESPACE`, `GOOEY_TASK_QUEUE`, queue default
  `gooey-visibility`).
- Binding proof: `handlers/temporal/visibility_binding_test.go` drives
  ``{{temporal:Activity `visibility.ListWorkflowExecutions` | into
  .Output}}`` through the real provider and markup loader; the
  fake starter seam runs the real pack activity against a faked
  WorkflowService and crosses the property boundary exactly as recorded
  above (protojson → `map[string]any` → provider render). The page's
  Text ends up holding Temporal's canonical JSON field names.
- CI: `packs/temporal-visibility` gets its own build/vet/test step in
  `ci.yml`, mirroring the other nested modules (no `-race`: matching
  `handlers/temporal`, whose tests these follow; nothing here asserts
  goroutine discipline).
- READMEs: the pack's own (import path, names, proto promise, Python
  interop snippet, no-gooey guarantee); `handlers/temporal/README.md`
  links to it.
