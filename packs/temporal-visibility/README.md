# temporal-visibility — a Temporal activity pack

The full Temporal Visibility API, exposed as standalone activities,
**proto-true**: every activity's input and output are the
`temporal.api.*` request/response messages from
[go.temporal.io/api](https://pkg.go.dev/go.temporal.io/api) — the same
generated types the server itself speaks. No DTOs, no renamed fields,
no wrapper structs.

```go
import (
    visibility "github.com/WonderForgeLabs/gooey/packs/temporal-visibility"
    "go.temporal.io/sdk/client"
    "go.temporal.io/sdk/worker"
)

c, _ := client.Dial(client.Options{Namespace: "ops"})
w := worker.New(c, "gooey-visibility", worker.Options{})
visibility.Register(w, visibility.New(c, visibility.WithNamespace("ops")))
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
| `visibility.ListWorkflowExecutions` | `WorkflowService.ListWorkflowExecutions` — the query-language path |
| `visibility.ListOpenWorkflowExecutions` | `WorkflowService.ListOpenWorkflowExecutions` |
| `visibility.ListClosedWorkflowExecutions` | `WorkflowService.ListClosedWorkflowExecutions` |
| `visibility.CountWorkflowExecutions` | `WorkflowService.CountWorkflowExecutions` |
| `visibility.GetSearchAttributes` | `WorkflowService.GetSearchAttributes` (legacy, cluster-scoped) |
| `visibility.DescribeWorkflowExecution` | `WorkflowService.DescribeWorkflowExecution` |
| `visibility.ListSearchAttributes` | `OperatorService.ListSearchAttributes` — the namespaced schema surface |

Each is one thin call through the SDK client's raw service clients
(`WorkflowService()` / `OperatorService()`, one shared connection).
Field-level documentation is Temporal's own API reference — the pack
cannot drift from it, because it does not restate it.

Three behaviors, uniformly:

- **Namespace defaulting.** A `nil` request, or one whose `namespace`
  is empty, gets the worker's namespace (`WithNamespace`, default
  `"default"`). A request that names one passes through untouched.
- **Pagination pass-through.** `next_page_token` is opaque server
  state and crosses the pack verbatim in both directions.
- **No heartbeats.** These are fast unary calls; bound them with the
  invocation's `StartToCloseTimeout`. The activity context flows into
  the RPC, so timeout and cancellation behave as Temporal intends.

## Calling from another language

Proto fidelity means zero translation. The same activity, scheduled
from the Python SDK inside a workflow, with the same proto types
(documentation only — this pack ships no Python):

```python
from datetime import timedelta
from temporalio import workflow
from temporalio.api.workflowservice.v1 import (
    ListWorkflowExecutionsRequest,
    ListWorkflowExecutionsResponse,
)

@workflow.defn
class Dashboard:
    @workflow.run
    async def run(self) -> None:
        resp: ListWorkflowExecutionsResponse = await workflow.execute_activity(
            "visibility.ListWorkflowExecutions",
            ListWorkflowExecutionsRequest(
                namespace="ops",
                query='ExecutionStatus = "Running"',
                page_size=25,
            ),
            result_type=ListWorkflowExecutionsResponse,
            start_to_close_timeout=timedelta(seconds=10),
            task_queue="gooey-visibility",
        )
        # resp.executions, resp.next_page_token — the server's own message.
```

`temporalio.api.workflowservice.v1` is generated from the same `.proto`
files as `go.temporal.io/api`; Temporal's payload converter carries
proto messages natively in both directions.

## In gooey

gooey consumes the pack from its `handlers/temporal` module: the
`workers/visibilityworker` binary serves it, and markup reaches it
through the `temporal:Activity` provider —

```xml
<Button Content="list"
        Click="{{temporal:Activity `visibility.ListWorkflowExecutions` | into .Executions}}"/>
```

Design and decisions: `docs/specs/2026-08-10-temporal-visibility-stdlib.md`
in the gooey repo.

## Versions

Standalone-activity *invocation* (a client executing these directly,
no workflow) needs SDK ≥ 1.41 and server ≥ 1.31. The pack pins
`go.temporal.io/sdk v1.47.0` and takes its proto types from
`go.temporal.io/api v1.63.4`. Serving these activities to workflow
callers has no special server floor.
