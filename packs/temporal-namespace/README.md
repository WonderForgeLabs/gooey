# temporal-namespace — a Temporal activity pack

Namespace and cluster introspection — the environment picker and
capability detection — exposed as standalone activities, **proto-true**:
every activity's input and output are the `temporal.api.*`
request/response messages from
[go.temporal.io/api](https://pkg.go.dev/go.temporal.io/api) — the same
generated types the server itself speaks. No DTOs, no renamed fields, no
wrapper structs.

```go
import (
    "github.com/WonderForgeLabs/gooey/packs/temporal-namespace"
    "go.temporal.io/sdk/client"
    "go.temporal.io/sdk/worker"
)

c, _ := client.Dial(client.Options{Namespace: "ops"})
w := worker.New(c, "gooey-namespace", worker.Options{})
namespace.Register(w, namespace.New(c, namespace.WithNamespace("ops")))
w.Run(worker.InterruptCh())
```

**No gooey dependency.** Despite the import path, this module imports
nothing from gooey — its graph is `go.temporal.io/sdk` +
`go.temporal.io/api` (and the protobuf runtime they bring). Any Go
Temporal worker can register the pack; the gooey repo is its home, not
its host. That guarantee is mechanical: check `go.mod`.

## The activities

Registered names are the API — the strings any Temporal client, in any
language, schedules:

| name | RPC |
|---|---|
| `namespace.DescribeNamespace` | `WorkflowService.DescribeNamespace` — one namespace's config, retention, replication |
| `namespace.ListNamespaces` | `WorkflowService.ListNamespaces` — the environment picker's list, paged |
| `namespace.GetClusterInfo` | `WorkflowService.GetClusterInfo` — cluster identity and supported client versions |
| `namespace.GetSystemInfo` | `WorkflowService.GetSystemInfo` — **server capabilities** |

Each is one thin call through the SDK client's raw service client
(`WorkflowService()`, one shared connection). Field-level documentation
is Temporal's own API reference — the pack cannot drift from it, because
it does not restate it.

### Capability detection is the second job

`GetSystemInfo` is why this pack is worth registering even in an app
with a single namespace. Its response carries `server_version` plus a
`Capabilities` message, and that is how a UI decides whether to **offer**
a newer feature at all, rather than offering it and discovering the
answer from a failure.

Two practical notes for anyone gating on it:

- `Capabilities` is a set of **specific booleans** — `supports_schedules`,
  `eager_workflow_start`, `nexus`, `count_group_by_execution_status`,
  and so on. It is not exhaustive: several newer server features have no
  flag, so gating on them means reading `server_version` (or handling
  `UNIMPLEMENTED`) instead.
- The pack returns the message untouched, so the flags a caller sees are
  exactly the flags the server sent. There is a test pinning that the
  `Capabilities` sub-message crosses unflattened.

### Read-only, on purpose

All four RPCs observe; none change anything, and there is a test
asserting the inventory stays that way.

`RegisterNamespace`, `UpdateNamespace` and `DeprecateNamespace` exist on
`WorkflowService` and are deliberately **not** here. Under the
registration-is-a-capability-grant split, a host that wants an
environment picker should not thereby gain the ability to deprecate a
namespace — and a mutating RPC added to this pack later would be a
silent privilege escalation for every host that already registered the
picker. A namespace-admin surface belongs in its own pack, registered
deliberately.

### Namespace defaulting, and where it does not apply

The usual rule — a `nil` request, or one whose `namespace` is empty,
gets the worker's namespace (`WithNamespace`, default `"default"`) —
applies to `DescribeNamespace`, the one RPC here that names a namespace.
A request that names one passes through untouched. This is the pack's
only deviation from verbatim pass-through, and it only ever fills an
empty field.

`ListNamespaces`, `GetClusterInfo` and `GetSystemInfo` are
**cluster-scoped**: their request messages have no namespace field at
all, so there is nothing to default. A `nil` request becomes the empty
request and nothing more.

### The uniform behaviors

- **Pagination pass-through.** `ListNamespaces`' `next_page_token` is
  opaque server state and crosses the pack verbatim in both directions.
- **No heartbeats.** These are fast unary calls; bound them with the
  invocation's `StartToCloseTimeout`. The activity context flows into
  the RPC, so timeout and cancellation behave as Temporal intends.
- **No SDK-level retries.** Raw service calls skip the client's retry
  interceptor — the caller invoked an activity, and retrying is the
  activity machinery's job, configured exactly once by its retry policy.

## Calling from another language

Proto fidelity means zero translation. The same activity, scheduled from
the Python SDK inside a workflow, with the same proto types
(documentation only — this pack ships no Python):

```python
from datetime import timedelta
from temporalio import workflow
from temporalio.api.workflowservice.v1 import (
    GetSystemInfoRequest,
    GetSystemInfoResponse,
)

@workflow.defn
class FeatureGate:
    @workflow.run
    async def run(self) -> bool:
        resp: GetSystemInfoResponse = await workflow.execute_activity(
            "namespace.GetSystemInfo",
            GetSystemInfoRequest(),
            result_type=GetSystemInfoResponse,
            start_to_close_timeout=timedelta(seconds=10),
            task_queue="gooey-namespace",
        )
        return resp.capabilities.supports_schedules
```

`temporalio.api.workflowservice.v1` is generated from the same `.proto`
files as `go.temporal.io/api`; Temporal's payload converter carries
proto messages natively in both directions.

## Reaching this pack from gooey markup

Not yet — the same boundary
[`temporal-visibility` documents](../temporal-visibility/README.md): a
markup argument crosses as one **string**, and a JSON string payload
cannot deserialize into a proto request; on the way back, gooey's
provider decodes into `any`, which the SDK's proto-JSON payload
converter refuses (`ErrValuePtrMustConcreteType`). A scalar convenience
layer like visibility's would close that, and is deliberately not
invented ahead of a demo that needs it — though this pack's shape is
about as easy as it gets, since three of its four requests are empty.

## Versions

Standalone-activity *invocation* (a client executing these directly, no
workflow) needs SDK ≥ 1.41 and server ≥ 1.31. The pack pins
`go.temporal.io/sdk v1.47.0` and takes its proto types from
`go.temporal.io/api v1.63.5`. All four RPCs are long-standing; the
`Capabilities` message grows fields over time, which reach callers by
bumping `go.temporal.io/api` with no pack change — proto fidelity's
whole point.

Design and decisions:
`docs/specs/2026-08-10-temporal-visibility-stdlib.md` (the pack pattern)
in the gooey repo. Epic #142, issue #151.
