# Tutorial 9: Temporal end-to-end

gooey and Temporal meet at the markup boundary, in both directions. In
this tutorial you press a button whose behavior is a durable activity a
worker runs — maybe on another machine; you drive a real ops dashboard
whose every Temporal call is declared in markup; and you run `wizardui`,
a terminal with **no application logic in it at all**, rendering screens
a workflow serves and signaling every press back. Each step is a
runnable demo in [`handlers/temporal`](../../handlers/temporal).

**Time:** about 40 minutes.
**Prerequisites:** [Tutorial 4](04-input-commands.md);
[Tutorial 8](08-remote-control.md) helps for the last section. You need
the [Temporal CLI](https://docs.temporal.io/cli) (`temporal server
start-dev` is the only server any of this requires). Standalone
activities need SDK ≥ 1.41 and server ≥ 1.31.

`handlers/temporal` is a nested Go module — the Temporal SDK's
dependency graph is quarantined there, like `mcp/` and `grpc/` — so
everything below runs from that directory.

## Step 1: An activity behind a button

```sh
temporal server start-dev --headless      # shell 1
cd handlers/temporal
go run ./cmd/temporaldemo                 # shell 2
```

([`temporaldemo.gif`](../media/demos/temporaldemo.gif) shows the run.) The
Slugify button's whole behavior is this markup:

```xml
<Gooey xmlns:temporal="gooey.dev/handlers/temporal">
  <Button Content="Slugify"
          Click="{{temporal:Activity `Slugify` .Input | into .Output}}"/>
```

No delegate in the demo implements Slugify. The markup declares WHAT
runs — an activity type name and arguments read from properties at
press time — and the host contributes the **capability grant**: it
builds the provider from a connected client and a task queue and
registers it under the `temporal:` namespace URI:

```go
markup.RegisterHandlers(temporalhandlers.URI,
    temporalhandlers.New(tc, taskQueue))
```

Delete that line and the same markup stops loading, naming the URI it
wanted. Connection config, credentials and queue routing never appear
in markup.

**Arguments are handles; results are deliveries.** Each `.Arg` is read
from its property at *invoke* time, on the UI goroutine, so cycling the
input changes what the next press sends. The activity then runs on its
own goroutine and `| into .Target` delivers the result back through the
Dispatcher — the confinement discipline of
[how-to: async](howto/howto-async.md), spelled in markup.

Under the hood this is a Temporal **standalone activity**
(`client.ExecuteActivity`, no workflow): a button press is exactly one
durable, retryable unit of work, and a workflow would be an
orchestration layer with nothing to orchestrate. Two consequences you
must design for:

- **At-least-once.** A retried activity may run twice for one click.
  Bind idempotent activities; delivery is last-write-wins because each
  completion `Set`s the target property.
- **Scalars cross the boundary, in both directions.** Each markup
  argument crosses as one string, and results are decoded into `any` —
  which the SDK's protojson converter refuses — so proto-taking or
  proto-returning activities are unreachable from markup. Give markup
  string-in/string-out activities; keep proto surfaces for workflow
  callers. This bit hard enough to shape the visibility pack (next
  step); the full story is in the
  [visibility spec](../specs/2026-08-10-temporal-visibility-stdlib.md).

The demo's worker runs in-process by default. Pass `--with-worker=false`
and `go run ./workers/temporalworker` anywhere with a route to the
server — the button cannot tell the difference, which is the deployment
argument in one flag.

## Step 2: The visibility pack

[`packs/temporal-visibility`](../../packs/temporal-visibility) is a
standard-library-shaped activity pack: the full Temporal Visibility API
as standalone activities, in a module with **zero gooey imports** — any
Go Temporal worker can register it with one call:

```go
visibility.Register(w, visibility.New(c, visibility.WithNamespace(ns)))
```

The core seven activities (`visibility.ListWorkflowExecutions`,
`visibility.DescribeWorkflowExecution`, …) are the thinnest possible
wrappers over the server's own RPCs — requests and responses are the
`temporal.api.*` proto messages themselves, so a Python worker or
caller uses the same generated types and the wire payloads match.
Because of the scalar boundary above, the pack also ships three
markup-reachable conveniences — `visibility.Query`, `visibility.Count`,
`visibility.Describe` — scalar strings in, protojson text out.

## Step 3: The ops dashboard

```sh
cd handlers/temporal
go run ./cmd/temporalops --with-dev-server    # one shell, server and all
```

([`temporalops.gif`](../media/demos/temporalops.gif).) A live workflow list,
count, and describe pane — and every Temporal call on that screen is a
markup expression over the pack's conveniences:

```xml
Click="{{temporal:Activity `visibility.Query` .Query .PageSize .PageToken | into .RowsJSON}}"
SelectionChanged="{{temporal:Activity `visibility.Describe` .SelectedWorkflowID .SelectedRunID | into .DescribeJSON}}"
```

Seed a few executions first so the list has rows —
`temporal workflow start --type Anything --task-queue seed-q --workflow-id demo-1`,
repeated with different IDs.

The property boundary is worth reading in
[`internal/ops`](../../handlers/temporal/internal/ops): protojson lands
in a `Property[string]`, a computed `json.Unmarshal`s it and projects
rows for the `ItemsView` — parsing *inside* the computed is what
subscribes the list to the fetch, the same read-versus-subscribe rule
from tutorial 3. Pagination keeps a client-side token history because
Temporal's page tokens are opaque and forward-only — "previous" is a
client memory, as in every Temporal UI.

Type a query in the bar (`ExecutionStatus="Running"`), press enter;
`ctrl+n`/`ctrl+p` page, `ctrl+r` refreshes. The full walkthrough is in
[demos.md](../demos.md#temporalops).

## Step 4: `wizardui` — the terminal with no app in it

So far the terminal owned the application and reached out for compute.
Now invert it:

```sh
cd handlers/temporal
go run ./cmd/wizardui --with-dev-server
```

([`wizarddemo.gif`](../../handlers/temporal/wizarddemo.gif).) You are
looking at a multi-stage provisioning wizard. The program you ran does
not know what a wizard is, what stages exist, or what any button does.
Every screen it draws arrived moments earlier as the payload of a
workflow **query**; every press goes back as a **signal** the payload
itself described:

```xml
<Button Content="approve" Click="{{wf:Signal `approve` | into .Notice}}"/>
```

That markup is data — never compiled into the shell. What the shell
contributes is, again, the capability grant: it registers
`temporalhandlers.NewWorkflowUI(c, workflowID)` under the `wf:`
namespace, bound to **one** workflow at construction. Served markup can
signal that workflow and do nothing else — it cannot name a different
workflow, cannot start activities, cannot reach the network. The
workflow decides what the screen says; the shell decides what the
screen is allowed to do. (The shell also owns the theme — a workflow
should not pick colors for a terminal it has never seen — and one
escape hatch: `ctrl+c` quits via an observer the served page cannot
consume, so a workflow cannot serve a page you cannot leave.)

Kill the UI mid-wizard and rerun it: it attaches to the same workflow
ID and picks up the exact screen you were on, because the application's
state never lived in the terminal. A *closed* workflow still answers
queries, so the final screen outlives the workflow itself.

### The wire contract

The query answer is small and rigid — a version, a revision, markup
source, and a flat values map:

```go
type UIState struct {
    Version  int               `json:"version"`
    Revision int               `json:"revision"`
    Stage    string            `json:"stage"`
    Markup   string            `json:"gooeyMarkup"`
    Values   map[string]string `json:"values"`
    Done     bool              `json:"done"`
}
```

The two counters split cheap from expensive. A new **Version** means a
different screen: the client builds a fresh tree against fresh sources
and swaps it in. A new **Revision** on the same Version means the same
screen with new numbers: the client `Set`s only the values that
changed, and the property graph repaints exactly the components that
read them — on the provisioning screen, one line per completed step
rather than a whole page.

**The values map must be key-complete.** Every key the served markup
binds must exist in `Values` on every answer — a key whose value is
currently the empty string **still has to be present**, or the bind
fails and the whole page is rejected (the client keeps showing the last
good screen and reports the error on exit). Serve the shape of your
state constantly; change only the contents.

Two more conventions from `wizardui` worth stealing: the shell reserves
exactly one name (`Echo`) for signal receipts, so its voice stays out
of the workflow's namespace — and `into` is optional: `{{wf:Signal
`approve`}}` with no receipt target is legal, because delivering to an
absent target is a no-op. Signals are one-way and at-least-once, so
write signal handlers so a repeat is harmless.

## The shells, and why companions matter here

Every demo above defaults to **one or two shells instead of three**
because its worker is a `gooey.CompanionFunc` — started before the
first frame, stopped when the app stops — and `--with-dev-server` runs
the Temporal dev server itself as a `gooey.CompanionCmd` child process.
The standalone binaries under `workers/` still exist because workers
belong where the compute is: `--with-worker=false` is the deployment a
real system has, and the UI cannot tell the difference. The dev server
is opt-in and never the default — it holds state that outlives any one
client, and a program that silently deletes what you were about to
`temporal workflow show` is a bad neighbor.

[How-to: companions](howto/howto-companions.md) covers the machinery —
readiness, the grace window, and the teardown guarantee.

## Closing the loop: a worker that writes UI

[`apps/temporal-worker`](../../apps/temporal-worker) is the two
tutorials shaking hands: a Python Temporal worker whose one dynamic
activity has Claude generate gooey markup about any topic, then pushes
it into a running app's `swap_markup` over MCP (tutorial 8). Run it as
a companion of the Kanban board:

```sh
cd apps/kanban
go run . -worker-python /path/to/.venv/bin/python   # -with-worker is on by default
# then, from apps/temporal-worker:
TEMPORAL_TASK_QUEUE=kanban-dynamic-ui python trigger.py GenerateUI "a topic"
```

A Temporal trigger in one shell, and the board's page swaps live —
markup authored by a model, delivered by a workflow-less activity,
applied atomically by the same path hot reload uses.

## What you learned

- `temporal:Activity` makes a durable activity a markup-declared
  behavior; the host's provider registration is the capability grant,
  and at-least-once plus scalar-only crossings are the design rules.
- Activity packs (`packs/temporal-<domain>`) are gooey's Temporal
  standard library: proto-true cores for workflow callers, scalar
  conveniences for markup.
- `wf:Signal` plus a query-served `{markup, values}` payload inverts
  ownership entirely: the workflow is the application, the terminal a
  renderer with a signal channel.
- Version swaps trees, Revision sets values — and the served values
  map must contain every bound key on every answer, empty or not.
- Companions are why none of this needs three terminals.

## Still missing

- **Served screens are polled** (400ms by default), not pushed. The
  push transport designed for this is the gRPC session stream's
  territory (`FrameDelta` framing in the
  [contract](../specs/2026-08-10-grpc-contract.md)); a served-markup
  transport (`gooey.serve.v1`) is future work.
- **Proto-returning activities cannot deliver into markup** — the v1
  provider's `any` decode, not the pack. Use the scalar conveniences.
- **A third architecture is designed but not walked here**: activity
  islands — server-driven panels with no workflow, state carried by
  the client — recorded in
  [activity-islands](../specs/2026-08-10-activity-islands.md).
- Workflow UIs live under workflow rules: determinism, versioning, and
  continue-as-new are yours to manage on the serving side.

## Next steps

- [How-to: companions](howto/howto-companions.md)
- Specs (ground truth):
  [activity islands](../specs/2026-08-10-activity-islands.md) ·
  [temporal visibility](../specs/2026-08-10-temporal-visibility-stdlib.md) ·
  [companions](../specs/2026-08-10-companions.md)
- The provider internals: [`handlers/temporal/temporal.go`](../../handlers/temporal/temporal.go)
  and [`workflowui.go`](../../handlers/temporal/workflowui.go)
- [`handlers/temporal/README.md`](../../handlers/temporal/README.md) —
  the module's own map
