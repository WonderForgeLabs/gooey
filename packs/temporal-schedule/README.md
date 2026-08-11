# temporal-schedule — a Temporal activity pack

The Temporal Schedule API — the cron-manager surface — exposed as
standalone activities, **proto-true**: every activity's input and output
are the `temporal.api.*` request/response messages from
[go.temporal.io/api](https://pkg.go.dev/go.temporal.io/api) — the same
generated types the server itself speaks. No DTOs, no renamed fields, no
wrapper structs.

```go
import (
    "github.com/WonderForgeLabs/gooey/packs/temporal-schedule"
    "go.temporal.io/sdk/client"
    "go.temporal.io/sdk/worker"
)

c, _ := client.Dial(client.Options{Namespace: "ops"})
w := worker.New(c, "gooey-schedule", worker.Options{})
schedule.Register(w, schedule.New(c, schedule.WithNamespace("ops")))
w.Run(worker.InterruptCh())
```

**No gooey dependency.** Despite the import path, this module imports
nothing from gooey — its graph is `go.temporal.io/sdk` +
`go.temporal.io/api` (and the protobuf runtime they bring). Any Go
Temporal worker can register the pack; the gooey repo is its home, not
its host. That guarantee is mechanical: check `go.mod`.

## The activities

Registered names are the API — the strings any Temporal client, in any
language, schedules. Reads first, then mutations:

| name | RPC |
|---|---|
| `schedule.ListSchedules` | `WorkflowService.ListSchedules` — the list, paged and query-filtered |
| `schedule.CountSchedules` | `WorkflowService.CountSchedules` — the status-bar companion |
| `schedule.DescribeSchedule` | `WorkflowService.DescribeSchedule` — the detail pane, and the source of `conflict_token` |
| `schedule.ListScheduleMatchingTimes` | `WorkflowService.ListScheduleMatchingTimes` — "when would this spec fire?", without creating anything |
| `schedule.CreateSchedule` | `WorkflowService.CreateSchedule` |
| `schedule.UpdateSchedule` | `WorkflowService.UpdateSchedule` |
| `schedule.PatchSchedule` | `WorkflowService.PatchSchedule` — pause / unpause / trigger / backfill |
| `schedule.DeleteSchedule` | `WorkflowService.DeleteSchedule` |

Each is one thin call through the SDK client's raw service client
(`WorkflowService()`, one shared connection). Field-level documentation
is Temporal's own API reference — the pack cannot drift from it, because
it does not restate it.

> Issue #149 scoped six RPCs. `CountSchedules` and
> `ListScheduleMatchingTimes` are added to complete the domain, matching
> `temporal-visibility`'s precedent of taking the whole surface
> (including the legacy `GetSearchAttributes`). Both are read-only:
> `CountSchedules` is what a status bar shows, and
> `ListScheduleMatchingTimes` is the preview a cron editor needs beside
> the spec field — a schedule UI without it makes users guess.

### Registration is the capability grant

Create, Update, Patch and Delete mutate, and `Register` is the boundary
that grants them. A host that wants a read-only schedule view has a
choice the build enforces: don't import this module, and
`DeleteSchedule` does not exist on its task queue.

Confirmation UI belongs to the app. The pack's contribution is that a
host which never called `Register` has nothing to confirm.

### `request_id` and `conflict_token` are yours, deliberately

`CreateSchedule`, `UpdateSchedule` and `PatchSchedule` carry a
`request_id` for server-side dedup; `UpdateSchedule` additionally
carries a `conflict_token` for optimistic concurrency. **The pack fills
neither**, and there are tests for both.

- **`request_id`** — an activity can be retried. A fresh id per attempt
  would apply the mutation twice after an ambiguous failure. Send one
  stable across attempts of a single invocation (derived from the
  invoking workflow's ID, or `activity.GetInfo(ctx).ActivityID`) and the
  server dedupes the retry for you.
- **`conflict_token`** — it comes from a `DescribeScheduleResponse`, and
  round-tripping it is what makes an update *fail* rather than silently
  clobber a concurrent edit. Only the caller knows which Describe its
  edit was based on. Leaving it empty updates unconditionally: the
  server's documented behavior, reached here exactly as over raw gRPC.

### `UpdateSchedule` replaces; `PatchSchedule` is surgical

`UpdateSchedule` **replaces** the schedule's spec, action, policies and
state wholesale. It is not a merge, and it is not this pack's job to
make it one — read with `DescribeSchedule`, modify the returned
`Schedule`, send it back with that Describe's `conflict_token`.

For a dashboard's buttons — pause, unpause, trigger now, backfill —
`PatchSchedule` is the right RPC and carries no risk of dropping fields
the UI never loaded.

### The uniform behaviors

- **Namespace defaulting.** A `nil` request, or one whose `namespace` is
  empty, gets the worker's namespace (`WithNamespace`, default
  `"default"`). A request that names one passes through untouched. This
  is the pack's only deviation from verbatim pass-through, and it only
  ever fills an empty field.
- **Pagination pass-through.** `next_page_token` is opaque server state
  and crosses the pack verbatim in both directions.
- **No heartbeats.** These are fast unary calls; bound them with the
  invocation's `StartToCloseTimeout`. The activity context flows into
  the RPC, so timeout and cancellation behave as Temporal intends.
- **No SDK-level retries.** Raw service calls skip the client's retry
  interceptor — the caller invoked an activity, and retrying is the
  activity machinery's job, configured exactly once by its retry policy.
  An SDK-level retry of a delete is a retry nobody configured.

## Calling from another language

Proto fidelity means zero translation. The same activity, scheduled from
the Python SDK inside a workflow, with the same proto types
(documentation only — this pack ships no Python):

```python
from datetime import timedelta
from temporalio import workflow
from temporalio.api.schedule.v1 import SchedulePatch
from temporalio.api.workflowservice.v1 import (
    PatchScheduleRequest,
    PatchScheduleResponse,
)

@workflow.defn
class Freezer:
    @workflow.run
    async def run(self, schedule_id: str) -> None:
        await workflow.execute_activity(
            "schedule.PatchSchedule",
            PatchScheduleRequest(
                namespace="ops",
                schedule_id=schedule_id,
                patch=SchedulePatch(pause="frozen for the release window"),
                identity="release-bot",
            ),
            result_type=PatchScheduleResponse,
            start_to_close_timeout=timedelta(seconds=10),
            task_queue="gooey-schedule",
        )
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
invented ahead of the cron-manager demo that needs it — pause/unpause/
trigger are the obvious scalar three, but a schedule *editor*'s shape is
a decision to make with the demo in hand.

## Versions

Standalone-activity *invocation* (a client executing these directly, no
workflow) needs SDK ≥ 1.41 and server ≥ 1.31. The pack pins
`go.temporal.io/sdk v1.47.0` and takes its proto types from
`go.temporal.io/api v1.63.5`. Schedules themselves need server ≥ 1.17
(where the Schedule API shipped); serving these activities to workflow
callers adds no floor beyond that.

Design and decisions:
`docs/specs/2026-08-10-temporal-visibility-stdlib.md` (the pack pattern)
in the gooey repo. Epic #142, issue #149.
