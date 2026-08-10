# Handler namespaces & remote behavior (design)

Direction set by Elan 2026-08-10: bring xmlns back so markup can bind
events to *framework-provided generic handlers declared in the markup
itself* — behavior without app code — with Temporal standalone
activities as the first distributed-compute primitive.

```xml
<Gooey xmlns="wonderforge.io/gooey/2026"
       xmlns:net="gooey.dev/handlers/net"
       xmlns:temporal="gooey.dev/handlers/temporal">

  <Button Content="fetch"
          Click="{{net:Get .Url | into .Body}}"/>

  <Button Content="rebuild index"
          Click="{{temporal:Activity `RebuildIndex` .Query | into .Results}}"/>
</Gooey>
```

## xmlns semantics

- The default namespace stays decorative-versioning
  (`wonderforge.io/gooey/2026`).
- Prefixed namespaces map prefix → URI; the parser records the mapping
  per document (encoding/xml only half-resolves attribute prefixes, so
  we track declarations ourselves).
- A **HandlerProvider registry** maps URI → provider:
  `markup.RegisterHandlers("gooey.dev/handlers/net", netProvider)`.
  Registration is the capability grant: markup can only invoke
  namespaces the *host app* registered. Remote markup + registered
  capabilities = sandboxed declarative apps (the lens model).

## Extension expressions

The binding DSL grows one form: `{{ns:Func args...}}` inside an event
attribute produces a **Command** (the input chapter's type), not a
value binding. Rules:

- `ns` resolves through the document's xmlns table to a provider.
- Args are the usual expression atoms: `.Path` (property handle,
  read at invoke time — lvalue semantics as everywhere), `` `literal` ``.
- `| into .Target` pipes the handler's result into a property handle,
  Set on the UI loop. Multiple results later (`| into .Body .Status`)
  — v1 is single.
- Everything stays reflection-free: providers are typed factories
  `func(fn string, args []Arg, ctx *Context) (gooey.Command, error)`.

## The dispatcher (new framework requirement)

Handlers run async (HTTP, Temporal). Properties are UI-goroutine-
confined, so completions must marshal back: a `Dispatcher` queue the
app's main loop drains (framework-owned run loop later). `into`
targets are Set through it. This is the same channel-into-loop pattern
cmd/reader already uses for fetch results, promoted to framework.

## net namespace (first local provider)

`{{net:Get .Url | into .Body}}` — GET, body as string (v1), errors
into an optional `| err .ErrProp` tail or a status variant later.
Proves the mechanism with zero infrastructure.

## temporal namespace (first distributed provider)

Backed by Temporal **standalone activities** (docs.temporal.io/
standalone-activity; needs server ≥1.31, CLI ≥1.7): top-level activity
executions invoked directly by a client — no workflow — with retry
policies, timeouts, heartbeats, dedup via conflict/reuse policies, and
addressable results (activity ID + run ID).

- `{{temporal:Activity `FetchFeed` .Url | into .Stories}}` → provider
  executes the activity on a configured task queue and delivers the
  result through the dispatcher.
- Provider construction carries the client + defaults:
  `temporalhandlers.New(client, taskQueue)` registered under the URI.
  Markup never sees connection config.
- Because standalone activities heartbeat, long jobs can stream
  progress: `| progress .Pct` mapping heartbeat details onto a
  property — a TUI progress bar driven by a remote worker. (v2)
- The at-least-once default means handlers should target idempotent
  activities; `| into` Sets are naturally last-write-wins.

The consequence: **backend compute for a gooey app is distributed via
Temporal.** The terminal declares *what* runs (activity type + args
from properties); workers anywhere decide *how*. Combined with
fs.FS-served markup, an entire app — layout, bindings, and behavior
wiring — ships as data.

## Sequencing

1. Input chapter lands Commands (in progress, background agent).
2. Parser: xmlns capture + `{{ns:Func ...}}` grammar + provider
   registry + Dispatcher.
3. net provider + tests (httptest).
4. temporal provider behind a build tag or submodule (adds SDK dep);
   demo app with a local worker + standalone activity.
