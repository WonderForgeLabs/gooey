# temporal-workflow — a Temporal activity pack

The workflow **lifecycle acts** — start, signal, query, cancel,
terminate, reset — exposed as standalone activities, **proto-true**:
every activity's input and output are the `temporal.api.*`
request/response messages from
[go.temporal.io/api](https://pkg.go.dev/go.temporal.io/api) — the same
generated types the server itself speaks. No DTOs, no renamed fields,
no wrapper structs.

This is the act half of what
[`temporal-visibility`](../temporal-visibility) reads: a dashboard lists
executions with `visibility.*`, then acts on the selected one with
`workflow.*`.

```go
import (
    workflowacts "github.com/WonderForgeLabs/gooey/packs/temporal-workflow"
    "go.temporal.io/sdk/client"
    "go.temporal.io/sdk/worker"
)

c, _ := client.Dial(client.Options{Namespace: "ops"})
w := worker.New(c, "gooey-workflow", worker.Options{})
workflowacts.Register(w, workflowacts.New(c, workflowacts.WithNamespace("ops")))
w.Run(worker.InterruptCh())
```

**No gooey dependency.** Despite the import path, this module imports
nothing from gooey — its graph is `go.temporal.io/sdk` +
`go.temporal.io/api` (and the protobuf runtime they bring). Any Go
Temporal worker can register the pack; the gooey repo is its home, not
its host. That guarantee is mechanical: check `go.mod`.

**On the import name.** The Go package is `workflow`, per the pack
convention (`packs/temporal-<domain>`, package = domain). That collides
by name with `go.temporal.io/sdk/workflow`, which workflow-side code
imports — alias one of them, as above. The pack itself never imports
`sdk/workflow`, so it has no such problem internally.

## The activities

Registered names are the API — the strings any Temporal client, in any
language, schedules:

| name | RPC |
|---|---|
| `workflow.StartWorkflowExecution` | `WorkflowService.StartWorkflowExecution` |
| `workflow.SignalWorkflowExecution` | `WorkflowService.SignalWorkflowExecution` |
| `workflow.SignalWithStartWorkflowExecution` | `WorkflowService.SignalWithStartWorkflowExecution` |
| `workflow.QueryWorkflow` | `WorkflowService.QueryWorkflow` |
| `workflow.RequestCancelWorkflowExecution` | `WorkflowService.RequestCancelWorkflowExecution` — the graceful stop |
| `workflow.TerminateWorkflowExecution` | `WorkflowService.TerminateWorkflowExecution` — the ungraceful one |
| `workflow.ResetWorkflowExecution` | `WorkflowService.ResetWorkflowExecution` |

Each is one thin call through the SDK client's raw service client
(`WorkflowService()`, one shared connection). Field-level documentation
is Temporal's own API reference — the pack cannot drift from it, because
it does not restate it.

### Registration is the capability grant

Terminate and Reset are destructive, and `Register` is the boundary that
grants them. A host that registers only `temporal-visibility`
**structurally cannot** terminate anything: the activity does not exist
on its task queue, and the module that defines it was never in the
build. Splitting the acts into their own pack is what makes that a fact
rather than an intention.

Confirmation UI belongs to the app. The pack's contribution is that a
host which never called `Register` has nothing to confirm.

### `request_id` is yours, deliberately

Five of these requests carry a `request_id` for server-side
deduplication, and **the pack never fills one** — that field is as much
the caller's here as it is against raw gRPC.

It matters most for `StartWorkflowExecution` and
`SignalWithStartWorkflowExecution`: **an activity can be retried.** If
each attempt carried a fresh `request_id`, a retry after an ambiguous
failure would start a *second* workflow. Send a `request_id` that is
stable across attempts of one invocation — derived from the invoking
workflow's own ID, or from `activity.GetInfo(ctx).ActivityID` at the
call site — and the server dedupes the retry for you. A pack-generated
UUID could not know which of those you meant, and a per-attempt one
would be actively wrong.

### The uniform behaviors

- **Namespace defaulting.** A `nil` request, or one whose `namespace` is
  empty, gets the worker's namespace (`WithNamespace`, default
  `"default"`). A request that names one passes through untouched. This
  is the pack's only deviation from verbatim pass-through, and it only
  ever fills an empty field.
- **No heartbeats.** These are unary calls; bound them with the
  invocation's `StartToCloseTimeout`. The activity context flows into
  the RPC, so timeout and cancellation behave as Temporal intends. Size
  `QueryWorkflow` generously — it is answered by a worker polling the
  *target's* task queue, so its latency is that worker's, not the
  server's.
- **No SDK-level retries.** Raw service calls skip the client's retry
  interceptor. That is deliberate: the caller invoked an activity, and
  retrying is the activity machinery's job, configured exactly once by
  its retry policy. It matters more here than in a read-only pack — an
  SDK-level retry of a terminate is a retry nobody configured.

## Calling from another language

Proto fidelity means zero translation. The same activity, scheduled from
the Python SDK inside a workflow, with the same proto types
(documentation only — this pack ships no Python):

```python
from datetime import timedelta
from temporalio import workflow
from temporalio.api.common.v1 import WorkflowExecution
from temporalio.api.workflowservice.v1 import (
    TerminateWorkflowExecutionRequest,
    TerminateWorkflowExecutionResponse,
)

@workflow.defn
class Reaper:
    @workflow.run
    async def run(self, workflow_id: str, run_id: str) -> None:
        await workflow.execute_activity(
            "workflow.TerminateWorkflowExecution",
            TerminateWorkflowExecutionRequest(
                namespace="ops",
                workflow_execution=WorkflowExecution(
                    workflow_id=workflow_id, run_id=run_id
                ),
                reason="stuck past its SLA",
                identity="reaper",
            ),
            result_type=TerminateWorkflowExecutionResponse,
            start_to_close_timeout=timedelta(seconds=10),
            task_queue="gooey-workflow",
        )
```

`temporalio.api.workflowservice.v1` is generated from the same `.proto`
files as `go.temporal.io/api`; Temporal's payload converter carries
proto messages natively in both directions.

## Reaching this pack from gooey markup

Not yet. `temporal-visibility` documents the boundary
([its README](../temporal-visibility/README.md), "Convenience
activities"): a markup argument crosses as one **string**, and a JSON
string payload cannot deserialize into a proto request; on the way back,
gooey's provider decodes into `any`, which the SDK's proto-JSON payload
converter refuses (`ErrValuePtrMustConcreteType`).

So the proto-true activities here are fully usable from **workflow
callers in any SDK** — and not yet from `{{temporal:Activity …}}`. A
scalar convenience layer like visibility's would close that, and is
deliberately not invented ahead of a demo that needs it: the right
scalar shape for "start a workflow" (payload encoding above all) is a
decision to make with a customer in hand, not without one.

## Versions

Standalone-activity *invocation* (a client executing these directly, no
workflow) needs SDK ≥ 1.41 and server ≥ 1.31. The pack pins
`go.temporal.io/sdk v1.47.0` and takes its proto types from
`go.temporal.io/api v1.63.5`. Serving these activities to workflow
callers has no special server floor; the RPCs themselves are ancient and
stable.

Design and decisions:
`docs/specs/2026-08-10-temporal-visibility-stdlib.md` (the pack pattern)
in the gooey repo. Epic #142, issue #147.
