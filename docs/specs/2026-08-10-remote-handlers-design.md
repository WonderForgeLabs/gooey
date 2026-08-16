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

---

## Implementation record (2026-08-10)

Landed: dispatcher, xmlns capture, the `{{ns:Func … | into .Target}}`
grammar, the provider registry, the net provider, the Temporal provider,
and `handlers/temporal/cmd/temporaldemo` (verified against a live
`temporal server start-dev`, recorded in `temporaldemo.gif`).

Where the implementation deviates from the design above, and why:

- **Provider shape is an interface, not a bare func.** The design said
  `func(fn string, args []Arg, ctx *Context) (gooey.Command, error)`.
  Providers turned out to need construction state (the Temporal client
  and task queue), so the registry takes a `HandlerProvider` interface
  with `NewCommand(*Call) (gooey.Command, error)`, plus a `HandlerFunc`
  adapter for stateless ones. `Call` carries the same information as the
  proposed parameters and leaves room to add fields without breaking
  providers.

- **`| err .Prop` was not added.** v1 delivers failures into the same
  `into` target as an `"ERROR: …"` string. A separate error channel is a
  change to the *pipeline grammar* (which wants to grow `progress` and
  multiple targets at the same time), so it should be designed once,
  with those, rather than bolted on per provider.

- **The Temporal provider is a separate Go module**, not a build tag.
  The SDK pulls in gRPC and protobuf; a build tag would still put them
  in the core module's `go.sum` and dependency graph. A nested module at
  `handlers/temporal/` is excluded from the parent, so `go build ./...`
  at the root not only omits the SDK but *cannot* pick it up — the
  isolation is mechanical rather than a convention to remember. The demo
  and its worker live inside that module for the same reason, which is
  why they are not at `cmd/temporaldemo`.

- **A Dispatcher is required at load time**, not lazily. A document
  using handler namespaces with no `Context.Dispatcher` fails to load
  rather than failing on first click, matching the rule that everything
  resolvable resolves at build time.

### An invariant this work established

**A document's namespace table is scoped to that document's build, and
restored afterwards.** Namespaces are per document, and a UserControl
whose `setup` returns the *parent* context is legal — so a nested build
would otherwise leave the child's (possibly empty) table installed on
the shared context, and the page's later siblings would resolve prefixes
against the wrong document. Since a prefix names a *capability*, that is
a security-shaped bug, not only a correctness one: an element could
silently resolve to a different provider than the one its author
declared, or lose a grant entirely. `Build` therefore saves and restores
`ctx.ns` around the build. Regression test:
`markup.TestNestedBuildRestoresTheParentNamespaceTable`.

### Still open (deliberately out of scope this pass)

Heartbeat/progress piping (`| progress .Pct`), multiple result targets,
and a retry-policy surface in markup — now epic
[#38](https://github.com/WonderForgeLabs/gooey/issues/38), with
children [#41](https://github.com/WonderForgeLabs/gooey/issues/41)
(progress), [#42](https://github.com/WonderForgeLabs/gooey/issues/42)
(multiple targets), and [#43](https://github.com/WonderForgeLabs/gooey/issues/43)
(retry/timeout); see `2026-08-10-pipeline-grammar-v2.md`. `<x:Property>`
declarations, since shipped ([#7](https://github.com/WonderForgeLabs/gooey/issues/7) /
`2026-08-10-markup-declared-properties.md`).
