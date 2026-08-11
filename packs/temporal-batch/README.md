# temporal-batch — a Temporal activity pack

Bulk operations over a visibility query — terminate, cancel, signal,
delete, reset everything a query matches — exposed as standalone
activities, **proto-true**: every activity's input and output are the
`temporal.api.*` request/response messages from
[go.temporal.io/api](https://pkg.go.dev/go.temporal.io/api) — the same
generated types the server itself speaks. No DTOs, no renamed fields, no
wrapper structs.

This is the bulk complement to `packs/temporal-workflow`: where that pack
terminates one execution, this one terminates everything a query matches.
It pairs with a dashboard's query bar — type a query, see the matches,
act on all of them.

```go
import (
    "github.com/WonderForgeLabs/gooey/packs/temporal-batch"
    "go.temporal.io/sdk/client"
    "go.temporal.io/sdk/worker"
)

c, _ := client.Dial(client.Options{Namespace: "ops"})
w := worker.New(c, "gooey-batch", worker.Options{})
batch.Register(w, batch.New(c, batch.WithNamespace("ops")))
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
| `batch.StartBatchOperation` | `WorkflowService.StartBatchOperation` — enqueues a job over a query; returns immediately |
| `batch.StopBatchOperation` | `WorkflowService.StopBatchOperation` — halts a running job |
| `batch.DescribeBatchOperation` | `WorkflowService.DescribeBatchOperation` — state and progress counts |
| `batch.ListBatchOperations` | `WorkflowService.ListBatchOperations` — the namespace's jobs, paged |

Each is one thin call through the SDK client's raw service client
(`WorkflowService()`, one shared connection). Field-level documentation
is Temporal's own API reference — the pack cannot drift from it, because
it does not restate it.

## Registration is the capability grant, and this one is large

A batch terminate reaches **every** execution its query matches, so the
ability to start one is a strictly larger grant than the ability to
terminate a single workflow. That is why this is its own module rather
than four more activities in `packs/temporal-workflow`: a host registers
it deliberately or not at all.

Confirmation UI belongs to the app. The pack's contribution is narrower
and firmer — a host that never called `Register` has nothing to confirm,
because the capability was never on the task queue.

## The operation oneof is where proto fidelity pays

`StartBatchOperationRequest.Operation` is a oneof over a dozen operation
types (terminate, signal, cancel, delete, reset, update workflow options,
and several activity-scoped ones), and it **grows**. A hand-rolled DTO
would have to enumerate them and would be stale by the next server
release. Passing the proto through means a new operation kind reaches
callers by bumping `go.temporal.io/api`, with no change to this pack at
all.

Likewise `visibility_query` and `target_executions` are mutually
exclusive, and the pack does not adjudicate between them: the server owns
that rule, and its error is the honest answer. There is a test asserting
the pack forwards a request that sets both rather than second-guessing it.

## `job_id` is the caller's, deliberately

`StartBatchOperationRequest` carries a `job_id` that both names the job
for later `Describe`/`Stop` calls and serves as its idempotency key. The
pack never fills one, and that is a safety decision rather than an
omission.

**An activity can be retried.** A fresh `job_id` per attempt would start
a *second* batch — over the same query, doing the same destructive thing
twice. Send a `job_id` stable across attempts of one invocation (derived
from the invoking workflow's own ID, or `activity.GetInfo(ctx).ActivityID`)
and a retry after an ambiguous failure resolves to the job already
running rather than a duplicate of it.

## The uniform behaviors

- **Namespace defaulting.** A `nil` request, or one whose `namespace` is
  empty, gets the worker's namespace (`WithNamespace`, default
  `"default"`). A request that names one passes through untouched. This
  is the pack's only deviation from verbatim pass-through, and it only
  ever fills an empty field. Here it matters more than elsewhere: a batch
  terminate landing in the worker's namespace instead of the caller's
  would act on the wrong population entirely, so the pass-through is
  tested per activity.
- **Pagination pass-through.** `next_page_token` is opaque server state
  and crosses the pack verbatim in both directions.
- **No SDK-level retries.** Raw service calls skip the client's retry
  interceptor. The caller invoked an activity, and retrying is the
  activity machinery's job, configured exactly once by its retry policy —
  and an SDK-level retry of a batch start is a retry nobody configured.

## Timeouts: all four are fast, including `Start`

`StartBatchOperation` **enqueues** the job and returns; it does not wait
for it. All four calls are ordinary unary RPCs, so no heartbeats are
recorded and none are needed.

A caller that wants to wait for a batch polls `DescribeBatchOperation` —
in a workflow, with a timer between polls, not by holding an activity
open for the duration of the job.

## Calling from another language

Proto fidelity means zero translation. The same activity, scheduled from
the Python SDK inside a workflow, with the same proto types
(documentation only — this pack ships no Python):

```python
from datetime import timedelta
from temporalio import workflow
from temporalio.api.batch.v1 import BatchOperationTermination
from temporalio.api.workflowservice.v1 import (
    StartBatchOperationRequest,
    StartBatchOperationResponse,
)

@workflow.defn
class Reaper:
    @workflow.run
    async def run(self, query: str) -> None:
        await workflow.execute_activity(
            "batch.StartBatchOperation",
            StartBatchOperationRequest(
                namespace="ops",
                # Stable across retries: this workflow's own ID, so an
                # ambiguous failure resolves to the same job.
                job_id=workflow.info().workflow_id,
                visibility_query=query,
                reason="stuck past their SLA",
                termination_operation=BatchOperationTermination(identity="reaper"),
            ),
            result_type=StartBatchOperationResponse,
            start_to_close_timeout=timedelta(seconds=30),
            task_queue="gooey-batch",
        )
```

`temporalio.api.workflowservice.v1` is generated from the same `.proto`
files as `go.temporal.io/api`; Temporal's payload converter carries proto
messages natively in both directions.

## Reaching this pack from gooey markup

Not yet — the same boundary
[`temporal-visibility` documents](../temporal-visibility/README.md): a
markup argument crosses as one **string**, and a JSON string payload
cannot deserialize into a proto request; on the way back, gooey's
provider decodes into `any`, which the SDK's proto-JSON payload converter
refuses (`ErrValuePtrMustConcreteType`). A scalar convenience layer like
visibility's would close that, and is deliberately not invented ahead of
a demo that needs it.

For this pack specifically there is a second reason not to rush it: a
one-string markup binding is the wrong front door for an operation whose
blast radius is "everything the query matched".

## Versions

Standalone-activity *invocation* (a client executing these directly, no
workflow) needs SDK ≥ 1.41 and server ≥ 1.31. The pack pins
`go.temporal.io/sdk v1.47.0` and takes its proto types from
`go.temporal.io/api v1.63.5`. Serving these activities to workflow
callers has no special server floor; batch operations are long-standing,
though individual *operation kinds* in the oneof have their own server
floors — the server rejects one it does not know.

Design and decisions:
`docs/specs/2026-08-10-temporal-visibility-stdlib.md` (the pack pattern)
in the gooey repo. Epic #142, issue #153.
