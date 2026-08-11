# temporal-taskqueue — a Temporal activity pack

Task-queue introspection — the are-my-workers-alive panel — exposed as
standalone activities, **proto-true**: every activity's input and output
are the `temporal.api.*` request/response messages from
[go.temporal.io/api](https://pkg.go.dev/go.temporal.io/api) — the same
generated types the server itself speaks. No DTOs, no renamed fields, no
wrapper structs.

It pairs with gooey's companion-worker story: an app that runs its own
worker in-process can watch that worker's health through the same server
everything else goes through, with no side channel.

```go
import (
    "github.com/WonderForgeLabs/gooey/packs/temporal-taskqueue"
    "go.temporal.io/sdk/client"
    "go.temporal.io/sdk/worker"
)

c, _ := client.Dial(client.Options{Namespace: "ops"})
w := worker.New(c, "gooey-taskqueue", worker.Options{})
taskqueue.Register(w, taskqueue.New(c, taskqueue.WithNamespace("ops")))
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
| `taskqueue.DescribeTaskQueue` | `WorkflowService.DescribeTaskQueue` — attached pollers, and backlog stats on request |
| `taskqueue.ListTaskQueuePartitions` | `WorkflowService.ListTaskQueuePartitions` — how the queue is partitioned across the cluster |

Each is one thin call through the SDK client's raw service client
(`WorkflowService()`, one shared connection). Field-level documentation
is Temporal's own API reference — the pack cannot drift from it, because
it does not restate it.

### Read-only, on purpose

Both RPCs observe; neither changes anything, and there is a test
asserting the inventory stays that way.

`UpdateTaskQueueConfig` exists on `WorkflowService` and is deliberately
**not** here. Under the registration-is-a-capability-grant split, a host
that wants a health panel should not thereby gain the ability to
reconfigure a queue — and a mutating RPC added to this pack later would
be a silent privilege escalation for every host that already registered
it. A config-writing surface belongs in its own pack, registered
deliberately.

### `report_stats` / `report_config` are yours, deliberately

`DescribeTaskQueue` answers cheaply by default and returns backlog
statistics only when `report_stats` is set (queue config likewise with
`report_config`). **The pack sets neither**, and there is a test.

They cost the server real work. A health dashboard polls on a timer, and
a pack that quietly forced these on would make every tick more expensive
than the caller asked for. A dashboard that wants backlog depth asks for
it.

Both flags want a recent enough server; an older one returns the
response without those fields. That is the server's answer to give, not
the pack's to translate.

### The uniform behaviors

- **Namespace defaulting.** A `nil` request, or one whose `namespace` is
  empty, gets the worker's namespace (`WithNamespace`, default
  `"default"`). A request that names one passes through untouched. This
  is the pack's only deviation from verbatim pass-through, and it only
  ever fills an empty field.
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
from temporalio.api.enums.v1 import TaskQueueType
from temporalio.api.taskqueue.v1 import TaskQueue
from temporalio.api.workflowservice.v1 import (
    DescribeTaskQueueRequest,
    DescribeTaskQueueResponse,
)

@workflow.defn
class HealthCheck:
    @workflow.run
    async def run(self, queue: str) -> int:
        resp: DescribeTaskQueueResponse = await workflow.execute_activity(
            "taskqueue.DescribeTaskQueue",
            DescribeTaskQueueRequest(
                namespace="ops",
                task_queue=TaskQueue(name=queue),
                task_queue_type=TaskQueueType.TASK_QUEUE_TYPE_ACTIVITY,
                report_stats=True,
            ),
            result_type=DescribeTaskQueueResponse,
            start_to_close_timeout=timedelta(seconds=10),
            task_queue="gooey-taskqueue",
        )
        return len(resp.pollers)
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
invented ahead of a demo (a worker-health panel) that needs it. This is
the pack where that shape is most obvious — `(queue, type, reportStats)`
in, protojson out — so it is likely the first to get one.

## Versions

Standalone-activity *invocation* (a client executing these directly, no
workflow) needs SDK ≥ 1.41 and server ≥ 1.31. The pack pins
`go.temporal.io/sdk v1.47.0` and takes its proto types from
`go.temporal.io/api v1.63.5`.

`DescribeTaskQueue` itself is long-standing, but its **backlog statistics**
(`report_stats`) and **queue config** (`report_config`) are newer server
features. The pack does not gate on them: it sends what the caller set
and returns what the server answered, so an older server simply omits
those fields.

Design and decisions:
`docs/specs/2026-08-10-temporal-visibility-stdlib.md` (the pack pattern)
in the gooey repo. Epic #142, issue #150.
