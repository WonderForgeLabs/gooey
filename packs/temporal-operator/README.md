# temporal-operator — a Temporal activity pack

Search-attribute **administration** — the schema behind every visibility
query — exposed as standalone activities, **proto-true**: every
activity's input and output are the `temporal.api.*` request/response
messages from
[go.temporal.io/api](https://pkg.go.dev/go.temporal.io/api) — the same
generated types the server itself speaks. No DTOs, no renamed fields, no
wrapper structs.

`temporal-visibility` is how a dashboard *reads* the schema. This pack
is how it gets *changed*.

```go
import (
    "github.com/WonderForgeLabs/gooey/packs/temporal-operator"
    "go.temporal.io/sdk/client"
    "go.temporal.io/sdk/worker"
)

c, _ := client.Dial(client.Options{Namespace: "ops"})
w := worker.New(c, "gooey-operator", worker.Options{})
operator.Register(w, operator.New(c, operator.WithNamespace("ops")))
w.Run(worker.InterruptCh())
```

**No gooey dependency.** Despite the import path, this module imports
nothing from gooey — its graph is `go.temporal.io/sdk` +
`go.temporal.io/api` (and the protobuf runtime they bring). Any Go
Temporal worker can register the pack; the gooey repo is its home, not
its host. That guarantee is mechanical: check `go.mod`.

## The activities

Registered names are the API — the strings any Temporal client, in any
language, schedules. All three are on **`OperatorService`**, not
`WorkflowService`:

| name | RPC |
|---|---|
| `operator.ListSearchAttributes` | `OperatorService.ListSearchAttributes` — custom, system, and the storage mapping |
| `operator.AddSearchAttributes` | `OperatorService.AddSearchAttributes` |
| `operator.RemoveSearchAttributes` | `OperatorService.RemoveSearchAttributes` |

Each is one thin call through the SDK client's raw operator service
client (`OperatorService()` — the same shared gRPC connection as
`WorkflowService`). Field-level documentation is Temporal's own API
reference — the pack cannot drift from it, because it does not restate
it.

### Admin, deliberately its own module

This pack exists separately because **registration is the capability
grant**. `temporal-visibility` already exposes `ListSearchAttributes`
for reading; a dashboard that reads the schema should not thereby be
able to remove a field from it.

Splitting the mutators out is what makes that structural rather than
aspirational: a host that never imported this module **cannot** call
`RemoveSearchAttributes`, because the activity does not exist on its
task queue and the code defining it was never in the build.

### On `RemoveSearchAttributes`

Removing a search attribute is the most consequential act here: existing
workflows' values for that name stop being queryable, and on some
persistence backends the operation is not undone by simply re-adding the
name. Temporal's own documentation is the authority.

The pack neither softens the call nor adds a confirmation of its own.
Confirmation UI belongs to the app; the pack's contribution is that a
host which never called `Register` has nothing to confirm.

### `ListSearchAttributes` appears in two packs, on purpose

`operator.ListSearchAttributes` and `visibility.ListSearchAttributes`
are the same RPC under two registered names. That is not an accident and
not a conflict — **the names differ**, so both can be registered on one
worker without collision (there is a test asserting every name in this
pack carries the `operator.` prefix, which is what makes that true).

The duplication is deliberate: an admin tool that never registers the
visibility pack still needs to read back what it just wrote, and a
read-only dashboard still needs the schema without gaining the mutators.
Each pack is independently complete for its own job.

### The uniform behaviors

- **Namespace defaulting.** A `nil` request, or one whose `namespace` is
  empty, gets the worker's namespace (`WithNamespace`, default
  `"default"`). A request that names one passes through untouched. This
  is the pack's only deviation from verbatim pass-through, and it only
  ever fills an empty field. On an admin pack it is also a safety
  property — a mutation must land in the namespace the caller named,
  never in the worker's — so the pass-through is tested per activity.
- **No heartbeats.** The activity context flows into the RPC, so timeout
  and cancellation behave as Temporal intends. Size
  `AddSearchAttributes` generously: on some clusters it does real index
  work rather than a metadata write.
- **No SDK-level retries.** Raw service calls skip the client's retry
  interceptor — the caller invoked an activity, and retrying is the
  activity machinery's job, configured exactly once by its retry policy.
  An SDK-level retry of a schema change is a retry nobody configured.

## Calling from another language

Proto fidelity means zero translation. The same activity, scheduled from
the Python SDK inside a workflow, with the same proto types
(documentation only — this pack ships no Python):

```python
from datetime import timedelta
from temporalio import workflow
from temporalio.api.enums.v1 import IndexedValueType
from temporalio.api.operatorservice.v1 import (
    AddSearchAttributesRequest,
    AddSearchAttributesResponse,
)

@workflow.defn
class Provision:
    @workflow.run
    async def run(self) -> None:
        await workflow.execute_activity(
            "operator.AddSearchAttributes",
            AddSearchAttributesRequest(
                namespace="ops",
                search_attributes={
                    "OrderTotal": IndexedValueType.INDEXED_VALUE_TYPE_DOUBLE,
                    "Tenant": IndexedValueType.INDEXED_VALUE_TYPE_KEYWORD,
                },
            ),
            result_type=AddSearchAttributesResponse,
            start_to_close_timeout=timedelta(seconds=60),
            task_queue="gooey-operator",
        )
```

`temporalio.api.operatorservice.v1` is generated from the same `.proto`
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
invented ahead of a demo that needs it. This is also the pack where a
convenience shape deserves the most caution: a scalar `Remove(name)`
would make the pack's most consequential act its easiest one to call.

## Versions

Standalone-activity *invocation* (a client executing these directly, no
workflow) needs SDK ≥ 1.41 and server ≥ 1.31. The pack pins
`go.temporal.io/sdk v1.47.0` and takes its proto types from
`go.temporal.io/api v1.63.5`. All three RPCs are long-standing; new
`IndexedValueType` values reach callers by bumping `go.temporal.io/api`
with no pack change — proto fidelity's whole point.

Design and decisions:
`docs/specs/2026-08-10-temporal-visibility-stdlib.md` (the pack pattern)
in the gooey repo. Epic #142, issue #152.
