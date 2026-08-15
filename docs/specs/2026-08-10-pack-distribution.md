# Pack distribution doctrine (design)

Direction set by Elan 2026-08-10 (issue #160, the organizing frame of
the stdlib expansion; grounded by epic #142's packaging decisions):
codify how gooey ships packs — what kinds exist, where the module
boundaries fall, how versions are cut, and the one security doctrine
that every kind shares. The in-flight stdlib packs (#157 fs, #158 exec,
#159 format) are built under this record; `packs/temporal-visibility`
and `handlers/net`/`handlers/temporal` are its existing instances.

## Taxonomy

Three shapes of reusable capability ship from this repo:

1. **Activity packs** — `packs/<runtime>-<domain>` (today:
   `packs/temporal-visibility`). Runtime-native compute libraries with
   **zero gooey imports**: anyone in that runtime's ecosystem imports
   them directly — any Go Temporal worker registers the visibility
   activities without knowing gooey exists. gooey is the pack's *home*
   (the repo) and its *first consumer* (`handlers/temporal`), never its
   host. The registration entry point is the runtime's own registry
   shape: `visibility.Register(w worker.ActivityRegistry, a *Activities)`.

2. **Handler packs** — `handlers/<name>` (today: `net`, `temporal`;
   in flight: `fs` #157, `exec` #158). gooey-coupled
   `markup.HandlerProvider` implementations behind an xmlns URI
   (`gooey.dev/handlers/<name>`): they exist to give *markup* behavior,
   so depending on gooey is their nature, not a leak. The registration
   entry point is `markup.RegisterHandlers(URI, New(…))`. One pack may
   own several URIs when the surfaces are distinct grants
   (`handlers/temporal` serves both `gooey.dev/handlers/temporal` and
   `gooey.dev/handlers/temporal/workflow`).

3. **Component/format libraries** — root packages
   (`gooey/components`, the computed-property constructors of #159).
   Pure computation over the property graph; importing the package is
   the whole story. They are *not* packs: there is no capability to
   grant, so there is no registration gate — and that absence is the
   classifying test.

What makes the first two a **pack** rather than merely a package is
three properties, each load-bearing:

- **A registration entry point that IS a capability grant.** The pack
  does nothing until the host registers it, and the constructor names
  the scope of what registration grants (see the doctrine below).
- **A README contract.** Import path, the invocable-name inventory,
  the grant's scope knobs, and failure semantics — the pack's own
  `README.md`, in the pack's directory, in the shape
  `packs/temporal-visibility/README.md` established.
- **A place in the docs/browser surface.** Runnable demos live under
  the pack module's `cmd/` (which `cmd/browser` scans); deployment
  binaries live under `workers/` (which it deliberately does not); the
  provider/name tables in `docs/markup-reference.md` list handler
  packs, and this record plus the pack README carry the rest.

## The module-boundary rule

Normative, three clauses:

- A **handler pack with zero third-party dependencies lives in the
  root module** under `handlers/<name>`. `handlers/net` is the
  instance: stdlib-only, so it rides the root module and the root
  version. `handlers/fs` (#157) belongs here for the same reason —
  `fs.FS` is stdlib.
- **Any third-party dependency forces a nested module.**
  `handlers/temporal` (the Temporal SDK graph) is the instance;
  the in-flight `handlers/exec` lands as one if its jq stage takes a
  library dependency. The quarantine is mechanical, not conventional:
  the root's `go build ./...` cannot see a nested module, so the root
  graph provably excludes the SDK.
- **Activity packs are ALWAYS standalone modules**, whatever their
  graph — because their promise runs the other direction: importing
  the pack must not drag gooey. `packs/temporal-visibility/go.mod` is
  the proof-by-construction; "check `go.mod`" is the guarantee's whole
  audit.

The root module's dependency budget is `golang.org/x/*` only (today:
`x/term`, `x/image`, `x/sys`). That budget binds component/format
libraries too: #159's humanize-style constructors implement their
rendering in-repo rather than importing a humanize library, or they
move to a nested module. "It's just one small dep" is how a TUI
framework ends up shipping a kitchen sink.

Rationale — consumer dependency hygiene, stated once, both directions:

> Importing gooey never drags SDKs; importing a pack never drags
> gooey — unless it is a handler pack, which is gooey-coupled by
> nature.

In-repo, nested modules consume each other via `replace` directives
(`handlers/temporal` → root and → `packs/temporal-visibility`), and
each nested module gets its own CI vet/test job in `ci.yml` because the
root sweep skips it. That job is no longer written by hand: `ci.yml`
discovers every `go.mod` in the tree and builds one matrix leg per
module, so a new pack is covered the moment its `go.mod` is committed.
The tier it lands in (`-race` or not) is still a path-pattern rule in
that file.

## Naming

- Activity packs: `packs/<runtime>-<domain>` —
  `packs/temporal-visibility`, `packs/temporal-schedule`. The
  Go package name is the domain (`visibility`), so call sites read
  `visibility.Register(w, visibility.New(c))`.
- Handler packs: `handlers/<name>`, package `<name>handlers`
  (`nethandlers`, `temporalhandlers`), URI
  `gooey.dev/handlers/<name>`.
- Invocable names: activity packs prefix registered names with the
  domain (`visibility.ListWorkflowExecutions` — the prefix scopes the
  pack, the suffix is the runtime's own name, verbatim); handler packs
  expose function names inside their URI's namespace (`net:Get`), so
  the xmlns prefix does the scoping and functions stay bare.

## Versioning and publishing

- **Go nested-module tagging is the release mechanism.** A nested
  module's version is a git tag prefixed with its directory:
  `packs/temporal-visibility/v0.1.0`, `handlers/temporal/v0.1.0`.
  Root-module packs (`handlers/net`) ride the root `v*` tags. Until a
  pack's first tag exists it is consumable by path (replace directive
  in-repo, pseudo-version from `main` outside). Cutting a tag IS
  publishing — Go modules have no registry push.
- **Nothing rides `publish-clients.yml`.** That workflow exists
  because npm and Python *do* have registry pushes; its own header
  already records "Go: nothing to publish — consumed via the git
  module path". Packs keep that property. No pack publish workflow is
  added now; this paragraph is the record of how to cut a release when
  wanted: tag `packs/<name>/vX.Y.Z` from green `main`.
- **While the repo is private:** Go consumers set
  `GOPRIVATE=github.com/WonderForgeLabs/*` (as `clients/README.md`
  records for the `grpc/` module) and need git access; there is
  nothing to look for in a package registry.
- **At OSS time:** the module proxy and pkg.go.dev pick every module
  up from the public repo with no work on our side — the module path
  is the registry. That is the packs analog of #119 (publish
  `gooey.control.v1` to a Buf Schema Registry when going public): the
  proto contract needs an explicit registry decision at OSS time;
  the Go packs need none. The activity packs' *wire* names (below) may
  eventually want schema publication of their own — that rides #62's
  wire-schema work, not a Go packaging change.

## Registration is the capability grant

The doctrine, stated once, normatively — everything else cites this:

> **A provider or pack does nothing until the host registers it, and
> registration names the capability's scope.** The scope is fixed at
> construction, in Go, by the host: for `net` the `http.Client`
> (`WithClient` decides what the capability can reach) and the body
> cap; for `fs` (#157) the `fs.FS` root, with writes only via an
> explicit read-write grant; for `exec` (#158) the command allowlist;
> for `temporal` the connected client and task queue. Markup —
> including markup served by a workflow, pushed over MCP/gRPC, or
> generated by an agent — can only invoke functions inside namespaces
> the host registered, and **can never expand its own grants**: there
> is no markup syntax that registers a provider, widens a root, or
> adds to an allowlist, and none will be added.

A document that names an unregistered URI fails **at load time**,
naming the URI it wanted — never a surprise at click time. Dropping a
registration line is therefore a complete revocation.

Three cross-references that make the doctrine hold end to end:

- **The xmlns save/restore invariant**
  (`2026-08-10-remote-handlers-design.md`): a document's prefix → URI
  table is scoped to that document's build and restored afterwards, so
  a nested build can neither leak nor inherit prefixes. A prefix names
  a capability; per-document scoping is what keeps an Include's
  capabilities independent of who included it.
- **The MCP/gRPC posture** (`2026-08-10-mcp-server.md`,
  `2026-08-10-grpc-contract.md`): the control surfaces that can push
  markup into a running app are loopback-only, opt-in, and their
  `SwapMarkup` restores state on failure — and the pushed markup still
  resolves against the *host's* registrations. Remote control can
  change what the app shows; it cannot change what the app can do.
- **The activity-pack side of the same coin** (epic #142): which packs
  a worker registers is the worker's grant surface. Temporal packs are
  split along Temporal's own service lines with one `Register` each,
  so a dashboard worker that registered only `visibility.*`
  *structurally cannot* terminate a workflow — the destructive
  lifecycle pack is a separate registration a read-only deployment
  never makes. Server-side authz still applies on top; the pack split
  is defense in depth, not a replacement for it.

## Inventory as data

Every pack exposes its registered names programmatically —
`visibility.AllNames()` is the precedent (seven `Name*` constants plus
the list, asserted by test):

- Activity packs export the registered activity names: `Name*`
  constants and `AllNames() []string`, in registration order.
- Handler packs export their function inventory the same way:
  `Name*` constants for each function the provider resolves and
  `AllNames()` for the set (added to `handlers/net` by this record's
  audit). The provider's unknown-function error derives from the same
  list, so the message cannot drift from the code.

This is the discovery hook: a served UI can ask a worker what it
offers before naming an activity; a control plane can enumerate a
host's granted surface; and #62's wire schemas get their name lists
from the packs themselves rather than a maintained-by-hand table. The
inventory is *names* only — argument/result schemas are #62's problem
(and, for handler packs, pipeline-grammar-v2's `FnInfo`).

## The `handlers/net` audit (executed 2026-08-10)

Checked against the doctrine:

- **Module boundary** — compliant: stdlib-only, root module, rides the
  root version. Correct side of the rule.
- **Registration docs** — compliant: the package doc shows the
  `markup.RegisterHandlers(nethandlers.URI, nethandlers.New())` grant
  and states registration-as-capability; `WithClient` documents that
  the app decides what the capability reaches.
- **README contract** — was missing (the only pack without one).
  **Fixed:** `handlers/net/README.md`, in the established shape.
- **Inventory as data** — was missing: the function set lived only in
  a `switch` and a hand-written error string. **Fixed:**
  `NameGet` + `AllNames()`, the switch and the error message now
  derive from them (error text unchanged), and a test pins the set.
- Nothing structural: v1 surface (`Get`, single `into` target,
  `"ERROR: …"` failure delivery) is recorded pipeline-grammar
  territory, not a distribution concern.

## What the in-flight packs inherit (informative)

For the #157/#158/#159 agents; their specs cite this record:

- **fs (#157):** stdlib-only ⇒ root module `handlers/fs`. Grant = the
  `fs.FS` root at registration; writes only via an explicit
  read-write registration option. README + `Name*`/`AllNames()`.
- **exec (#158):** the allowlist is the grant — untrusted markup never
  names arbitrary binaries. Nested module if any third-party dep
  (jq library) enters; root module only if stdlib-pure. README +
  `Name*`/`AllNames()`.
- **format (#159):** a component/format library, not a pack — no
  registration gate, and none should be invented for it. Root
  placement requires implementing humanize semantics without a
  third-party import (the root dependency budget), else nesting.

## Explicitly out (this pass)

- A pack publish workflow (recorded above; cut tags when wanted).
- Argument/result schemas for inventories — #62.
- A pack *manifest* format for served-UI discovery — rides #62 and the
  `ui.*`/`agent.*` line of the #142 roadmap.
- Retro-fitting `handlers/temporal` with `Name*`/`AllNames()` — right
  under the doctrine but not this record's small-fix budget; do it
  when that module is next open.
