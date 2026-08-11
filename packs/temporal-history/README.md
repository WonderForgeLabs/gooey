# temporal-history — a Temporal activity pack

Execution history reads — forward, reverse, paged, and long-polling —
exposed as standalone activities, **proto-true**: every activity's input
and output are the `temporal.api.*` request/response messages from
[go.temporal.io/api](https://pkg.go.dev/go.temporal.io/api) — the same
generated types the server itself speaks. No DTOs, no renamed fields, no
wrapper structs.

This is the raw event source. Paired with
`visibility.DescribeWorkflowExecution` it fills a detail view, and it is
what a timeline component renders.

```go
import (
    "github.com/WonderForgeLabs/gooey/packs/temporal-history"
    "go.temporal.io/sdk/client"
    "go.temporal.io/sdk/worker"
)

c, _ := client.Dial(client.Options{Namespace: "ops"})
w := worker.New(c, "gooey-history", worker.Options{})
history.Register(w, history.New(c, history.WithNamespace("ops")))
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
| `history.GetWorkflowExecutionHistory` | `WorkflowService.GetWorkflowExecutionHistory` — oldest-first, paged, optionally long-polling |
| `history.GetWorkflowExecutionHistoryReverse` | `WorkflowService.GetWorkflowExecutionHistoryReverse` — newest-first |

Each is one thin call through the SDK client's raw service client
(`WorkflowService()`, one shared connection). Field-level documentation
is Temporal's own API reference — the pack cannot drift from it, because
it does not restate it.

## `wait_new_event` makes this a long poll — size the timeout for it

This is the pack that is **not** "a fast unary call", and it is the one
thing to get right before deploying it.

With `wait_new_event` set, `GetWorkflowExecutionHistory` does not return
until a matching event arrives or the **server's** long-poll deadline
expires (tens of seconds). Two consequences, both the caller's:

- **Size `StartToCloseTimeout` above the server's long-poll timeout.**
  Below it, every poll of a quiet workflow times out mid-flight and the
  activity retries — turning a cheap wait into a hot loop against the
  frontend.
- **No heartbeats, and none are possible.** The RPC blocks inside a
  single call with nothing to report partway, so `HeartbeatTimeout` is
  the wrong knife here. Use `StartToCloseTimeout`; the activity context
  flows into the RPC, so a cancellation does cancel the in-flight poll.

Without `wait_new_event` (the default, and the pack never sets it —
there is a test) these are ordinary reads that return promptly.

## The uniform behaviors

- **Namespace defaulting.** A `nil` request, or one whose `namespace` is
  empty, gets the worker's namespace (`WithNamespace`, default
  `"default"`). A request that names one passes through untouched. This
  is the pack's only deviation from verbatim pass-through, and it only
  ever fills an empty field.
- **Pagination pass-through.** `next_page_token` is opaque server state
  and crosses the pack verbatim in both directions. Note the forward and
  reverse RPCs have **separate token spaces** — a token from one is not
  valid in the other.
- **No SDK-level retries.** Raw service calls skip the client's retry
  interceptor. The caller invoked an activity, and retrying is the
  activity machinery's job, configured exactly once by its retry policy.

## Calling from another language

Proto fidelity means zero translation. The same activity, scheduled from
the Python SDK inside a workflow, with the same proto types
(documentation only — this pack ships no Python):

```python
from datetime import timedelta
from temporalio import workflow
from temporalio.api.common.v1 import WorkflowExecution
from temporalio.api.workflowservice.v1 import (
    GetWorkflowExecutionHistoryRequest,
    GetWorkflowExecutionHistoryResponse,
)

@workflow.defn
class Auditor:
    @workflow.run
    async def run(self, workflow_id: str) -> None:
        resp: GetWorkflowExecutionHistoryResponse = await workflow.execute_activity(
            "history.GetWorkflowExecutionHistory",
            GetWorkflowExecutionHistoryRequest(
                namespace="ops",
                execution=WorkflowExecution(workflow_id=workflow_id),
                maximum_page_size=100,
            ),
            result_type=GetWorkflowExecutionHistoryResponse,
            start_to_close_timeout=timedelta(seconds=30),
            task_queue="gooey-history",
        )
        # resp.history.events, resp.next_page_token — the server's own message.
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
invented ahead of a demo (a history viewer) that needs it.

## Versions

Standalone-activity *invocation* (a client executing these directly, no
workflow) needs SDK ≥ 1.41 and server ≥ 1.31. The pack pins
`go.temporal.io/sdk v1.47.0` and takes its proto types from
`go.temporal.io/api v1.63.5`. Serving these activities to workflow
callers has no special server floor; both RPCs are long-standing.

Design and decisions:
`docs/specs/2026-08-10-temporal-visibility-stdlib.md` (the pack pattern)
in the gooey repo. Epic #142, issue #148.
