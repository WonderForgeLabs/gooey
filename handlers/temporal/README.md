# gooey × Temporal

Two demos live in this module, and they point in opposite directions.

**`temporal:Activity`** — a terminal-owned page reaching out for durable
compute. The markup names an activity, a worker somewhere runs it, the
result lands in a property. See `cmd/temporaldemo` and the package doc in
`temporal.go`.

**`wf:Signal`** — a workflow-owned application. The workflow serves its
own screens, mutates them as it advances, and the terminal is a generic
shell that renders whatever arrived. See `cmd/wizardworker` +
`cmd/wizardui`, and the rest of this file.

![the wizard demo](wizarddemo.gif)

## The server-driven demo

```sh
temporal server start-dev --headless   # shell 1
go run ./cmd/wizardworker              # shell 2
go run ./cmd/wizardui                  # shell 3

temporal workflow show --workflow-id gooey-wizard   # what actually ran
```

`ProvisionWizard` is a four-stage request wizard: choose a size and a
region, review a priced summary, watch it provision, then start over or
finish. What makes it interesting is where each piece lives.

**The workflow owns the UI.** A query — `ui` — answers with the current
screen:

```go
type UIState struct {
    Version  int               // identifies the MARKUP
    Revision int               // identifies the STATE
    Stage    string
    Markup   string            // gooey source, as a string
    Values   map[string]string // everything that markup binds
    Done     bool
}
```

Advancing a stage means handing out *different markup*. That is the
whole "modify itself" story: nothing on the client changed, and the
screen is now a different screen.

**Every piece of logic is an activity.** Seven of them, including the one
that serves the markup:

| activity | what it decides |
|---|---|
| `LoadStageMarkup` | which screen the user is looking at |
| `DescribeChoice` | what a button press *means* ("large" → "16 vCPU / 64 GiB") |
| `ValidateRequest` | whether the wizard may advance |
| `PriceRequest` | the numbers on the review screen |
| `ReserveCapacity`, `ProvisionResource`, `NotifyOwner` | the work itself |

The workflow orchestrates and holds state; it decides nothing. A run of
the demo produces this history:

```
LoadStageMarkup  SIGNAL continue  ValidateRequest  SIGNAL choose  DescribeChoice
SIGNAL choose  DescribeChoice  SIGNAL continue  ValidateRequest  LoadStageMarkup
PriceRequest  SIGNAL approve  LoadStageMarkup  ReserveCapacity  ProvisionResource
NotifyOwner  LoadStageMarkup  SIGNAL finish  LoadStageMarkup
```

**The client knows nothing.** `cmd/wizardui` contains no stage names, no
field names, no delegates. It knows how to render markup, how to poll a
query, and how to send a signal. Its `uiState` struct is deliberately a
*copy* of the worker's rather than a shared import: those six field names
are the entire coupling between the two programs.

## How markup-as-payload meets the swap loop

The client polls every 400ms and does one of three things:

* **`Version` changed** → build a fresh widget tree from the new markup
  against fresh property sources, close the old Composer, attach the new
  one. State is on the server, so nothing is lost in the swap.
* **`Revision` changed** → `Set` the sources whose values differ and let
  the property graph repaint exactly the widgets that read them. On the
  provisioning screen that is one line per completed activity, not a page.
* **neither changed** → nothing. A screen nobody is touching costs zero
  frames; a 34-second scripted run polls 80 times and flushes 20 frames.

Splitting the two counters is what makes that possible. A single "state
changed" counter would rebuild the tree eight times while three
activities ran.

**Markup and values travel together, in one query, on purpose.** gooey
resolves `{{.Tier}}` to a property handle at *build* time, so markup
binding a value the map does not carry is a load-time error. A torn read
of the two — new markup, old values — would take the screen down. One
query returns a consistent pair or nothing.

The same rule bites on the worker side: a value whose first setting is
the empty string still has to *appear* in the map. `wizard.set` checks
presence, not just inequality, for exactly this reason.

## What the client grants, and what it keeps

Registration is the capability grant:

```go
markup.RegisterHandlers(temporalhandlers.WorkflowURI,
    temporalhandlers.NewWorkflowUI(tc, workflowID))
```

Served markup can now signal *that one workflow*. It cannot start
activities, cannot fetch a URL, cannot name a different workflow — the
target is host configuration, never something markup supplies. Delete the
registration and the served markup stops loading, naming the URI it
wanted. (`workflowui_test.go` asserts this.)

Three things stay client-side and never come from the workflow:

1. **The theme.** A workflow serving a screen should not pick colors for
   a terminal it has never seen. Unknown style names degrade to plain
   text rather than failing the load.
2. **The quit key.** `ctrl+c` is handled by the shell's event loop, above
   the served tree, so a workflow cannot serve a page you cannot leave.
3. **One reserved property, `Echo`.** Handler receipts land there
   (``Click="{{wf:Signal `approve` | into .Echo}}"``), which keeps the
   client's voice ("terminal sent: sent approve") out of the workflow's
   ("workflow says: …"). Everything else in the values map is the
   workflow's, and the workflow gets the last word on it: a value the
   client wrote optimistically is reconciled by the next poll.

## Known rough edges

* **Input during a swap belongs to the outgoing screen.** A key pressed
  in the window between the workflow advancing and the client rebuilding
  is dispatched against the old tree, so it may send a signal the new
  stage is not listening for (harmless — it sits in the channel). A fix
  would be for the client to swallow input for a beat after a swap, or
  for the workflow to drain stale signals on stage entry. The scripted
  captures use generous pauses for this reason.
* **Focus resets on every swap.** A rebuilt tree focuses its first focus
  stop. Preserving focus across a swap would need the client to match
  widgets between trees, which it deliberately cannot do — it has no
  names, only markup.
* **The demo loops instead of continuing-as-new.** "New request" rewinds
  the same run, so history grows without bound across many requests. A
  real deployment would `ContinueAsNew`; the loop is kept because a
  continued run is a new run ID, and the point being made is that this is
  one long-lived application.
* **Layout targets 100×30.** The stage grids use fixed-height panels;
  narrower terminals clip the activity log.
* **Polling, not streaming.** A query poll is the portable option.
  Temporal has no server-push for query results, so a lower-latency
  version would need updates-with-start or an out-of-band channel.

## Determinism

Nothing in `wizard.go` reads a clock, a random source, or the network.
`workflow.Now` supplies the replay-safe time; every other fact on screen
arrived as an activity result recorded in history. A replay reconstructs,
byte for byte, the UI the user was looking at — which is also why the
final screen survives the workflow's own completion: a closed workflow
still answers the `ui` query, replayed from its history.
