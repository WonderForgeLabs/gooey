# How to fetch, read files, and run commands from markup (handler packs)

A `Click` usually names a delegate in your viewmodel. Handler
namespaces are the other arrangement: the **behavior itself is declared
in the markup** — fetch this URL, read that file, run this command —
and the app contributes no delegate at all. What the app contributes is
the *capability*: it registers a provider under a namespace URI, and
that registration decides what any document loaded into it may do.

```xml
<Gooey xmlns="wonderforge.io/gooey/2026"
       xmlns:net="gooey.dev/handlers/net"
       xmlns:fs="gooey.dev/handlers/fs">
  <Button Content="fetch" Click="{{net:Get .Url | into .Body}}"/>
  <Button Content="open"  Click="{{fs:Read .Path | into .Content}}"/>
</Gooey>
```

<!-- GIF: docs-and-demos workflow — record docs/learn/examples/howto-handlers -->

> **If you know XAML:** the `xmlns:` prefixes look like `clr-namespace`
> imports, but the resolution rule is inverted. Importing a namespace
> gives the document nothing — a prefix resolves only if the *host app*
> registered a provider under that URI, and a document can never widen
> its own grants from markup. That inversion is what makes markup
> loaded from an untrusted `fs.FS` safe to run: it reaches exactly the
> capabilities its host chose to hand it. The doctrine is
> [pack distribution](../../specs/2026-08-10-pack-distribution.md); the
> original design record is
> [remote handlers](../../specs/2026-08-10-remote-handlers-design.md).

## The grammar, and when each part happens

```
{{prefix:Func arg… | into .Target}}
```

- **`prefix`** resolves through the document's own `xmlns` table to a
  registered provider. Namespaces are **per document**: an Include or
  UserControl declares its own and cannot inherit the page's, so a
  control's capabilities never depend on who included it. The prefix is
  the document's choice; the URI is the identity.
- **Arguments** are the binding DSL's two atoms: `` `backtick literal` ``
  (a constant string) and `.Path` (a property handle from
  `Context.Values`). A bound argument is read **at invoke time**, on
  the UI goroutine — the same lvalue semantics as every other binding —
  so editing `.Url` changes what the *next* press fetches, and the
  press snapshots what the screen showed when it was pressed.
- **`| into .Target`** names the `*prop.Property[string]` the result is
  delivered to. It is the only pipeline stage in v1. Functions that
  produce a result require it (`net:Get` without a target is a load
  error); a function with nothing to deliver makes it optional —
  `wf:Signal` from the [temporal packs](#the-temporal-namespaces) works
  with or without one, because delivering to an absent target is a
  no-op.

The whole expression produces a `gooey.Command`, so it works anywhere a
command does — `<KeyBinding Command="{{net:Get .Url | into .Body}}"/>`
is legal and useful.

**Everything resolvable resolves at load time.** Unknown prefix,
unregistered URI, unknown function, wrong arity, missing target,
unbindable argument, and provider-specific complaints (an unregistered
command name, an invalid literal path, a jq expression that does not
compile) are all load errors, never surprises on click.

### Async results and the Dispatcher

Every provider runs its I/O off the UI goroutine, and properties are
UI-goroutine-confined. The bridge is the app's `Dispatcher`: the
provider's worker goroutine hands the result string over, and the `Set`
actually happens when the run loop drains the queue — which is why a
fetch completing mid-frame is not a data race. With `gooey.App` the
wiring is two lines:

```go
app = gooey.NewApp(markup.Page(os.DirFS("."), "app.gooey", ctx))
ctx.Dispatcher = app.Dispatcher()
```

A document that uses handler namespaces with no `Context.Dispatcher`
fails **to load**, not on first click. Nothing in any provider knows
which components display the result: the `Set` dirties whatever read
the target property, and the next frame repaints exactly those — the
same rule as all of [how to work off the UI goroutine](howto-async.md).

### Failures are strings, in-band

Every v1 pack delivers failures into the same target as an
`"ERROR: …"` string, so a page shows what went wrong without a second
binding. A separate `| err .Prop` tail is
[pipeline grammar v2](../../specs/2026-08-10-pipeline-grammar-v2.md)
territory, deliberately not bolted on per provider.

## Task 1: fetch a URL with net:Get

Markup declares the fetch:

```xml
<Gooey xmlns="wonderforge.io/gooey/2026"
       xmlns:net="gooey.dev/handlers/net">
  <Button Content="fetch" Click="{{net:Get .Url | into .Body}}"/>
  <Text>{{.Body}}</Text>
</Gooey>
```

The app grants the capability:

```go
import nethandlers "github.com/WonderForgeLabs/gooey/handlers/net"

markup.RegisterHandlers(nethandlers.URI, nethandlers.New())
```

Delete that line and the same document stops loading, naming the URI it
wanted. `New` takes options — `WithClient(c)` swaps in the
`http.Client` (proxies, auth-carrying transports, test doubles), so the
app decides what the capability actually reaches; markup only names a
URL. `WithMaxBody(n)` caps the response (1 MiB default) so a runaway
body cannot become the application's memory profile. `net:Get` speaks
`http` and `https`, delivers the body as a string on 2xx, and an
`ERROR: …` string otherwise.

## Task 2: read and list files with fs:

The fs registration names a **root**, and the root is the whole
security story:

```go
import fshandlers "github.com/WonderForgeLabs/gooey/handlers/fs"

// Read-only: the fs.FS IS the grant's extent.
markup.RegisterHandlers(fshandlers.URI, fshandlers.New(os.DirFS("./docs")))
```

Every path a page names resolves inside that `fs.FS` and nowhere else.
An `embed.FS` grants exactly its embedded files, `os.DirFS` a subtree,
a TarFS an archive — the pack never decides what markup may touch,
because the value handed to the constructor already did. Escapes are
rejected structurally per `fs.ValidPath` (relative, slash-separated, no
`..`, no leading `/`): a **literal** path like `` `../etc/passwd` ``
fails at load time, and a **bound** one delivers the `ERROR` into the
target at invoke time.

The read functions, all shaped `{{fs:Fn arg | into .Target}}`:

| Function | Delivers |
|---|---|
| `fs:Read .Path` | file contents, capped (1 MiB default; `WithMaxRead` raises it — over-cap files *fail* rather than silently truncate) |
| `fs:List .Dir` | JSON array of entries: `{"name":"spec.md","size":1204,"dir":false,"modTime":"2026-08-10T12:00:00Z"}` |
| `fs:Stat .Path` | JSON of one entry |
| `fs:Glob .Pattern` | JSON array of matching paths (`[]`, never `null`) |

**Reading is the default posture.** Writes exist only through a
separate constructor — a visible decision at the registration site,
never a flag that defaults on:

```go
p, err := fshandlers.NewWritable("./workspace")
markup.RegisterHandlers(fshandlers.URI, p)
```

That unlocks `fs:Write .Path .Content` and `fs:Append .Path .Content`,
whose target is a **status slot**: `""` on success, the `ERROR` string
on failure. `NewWritable` is backed by `os.Root`, so the OS itself
refuses any resolution — symlinks included — that would leave the
directory. On a read-only grant, `Write` and `Append` are *load*
errors naming the missing writable grant. `fs:Watch` does not exist,
deliberately: a v1 handler is one-shot and command-shaped, and a watch
is a subscription with a lifetime — that belongs to pipeline grammar v2
or a [companion](../../specs/2026-08-10-companions.md). Full contract:
[`handlers/fs/README.md`](../../../handlers/fs/README.md) and the
[fs pack spec](../../specs/2026-08-10-fs-pack.md).

## Task 3: run an allowlisted command with sys:Run

The exec pack's grant is **itemized**: registration names every command
markup may run, and untrusted markup never names a binary.

```go
import exechandlers "github.com/WonderForgeLabs/gooey/handlers/exec"

p, err := exechandlers.New([]exechandlers.Command{
        {Name: "git-status", Path: "git", Args: []string{"status", "--short"}},
        {Name: "list-pods", Path: "kubectl",
                Args: []string{"get", "pods", "-o", "json"},
                Jq:   ".items[].metadata.name"},
})
markup.RegisterHandlers(exechandlers.URI, p)
```

```xml
<Gooey xmlns="wonderforge.io/gooey/2026"
       xmlns:sys="gooey.dev/handlers/exec">
  <Button Content="status" Click="{{sys:Run `git-status` | into .Out}}"/>
  <Button Content="pods"   Click="{{sys:Run `list-pods` | into .Pods}}"/>
</Gooey>
```

The command name must be a **backtick literal** — a bound `.Path` there
is a load error by design, because a runtime value cannot be checked
against the allowlist at load time, and load time is where this
framework refuses things. There is no shell anywhere in the pack: a
`Command` is an argv, markup-supplied arguments (allowed only when a
registration opts in with `ArgPolicy: ArgsAny`, cappable via `MaxArgs`)
are appended as argv elements, and nothing is ever re-parsed by
`/bin/sh`. The child's environment starts **empty** — plus the
registration's explicit `Env` entries and named `PassEnv`
pass-throughs — so secrets in the app's environment do not exist for
children unless named. The child never gets a tty, runs in its own
process group (Unix), and a timeout kills the whole group: SIGTERM, a
grace window, SIGKILL.

Option literals sit between the name and the arguments, validated at
load:

```
{{sys:Run `name` [`capture=MODE`] [`jq=EXPR`] [`--`] [args…] | into .Out}}
```

- `` `capture=stdout|stderr|combined|both|exit-code` `` — which stream
  lands in the target. The stream modes deliver a non-zero exit as an
  `ERROR` string; `both` (tagged JSON
  `{"exit":N,"stdout":"…","stderr":"…"}`) and `exit-code` treat it as
  **data**, which is what `git diff --quiet`-shaped callers want.
- `` `jq=EXPR` `` — gojq extraction over the delivered text (which must
  then be JSON), compiled at load time.
- `` `--` `` — ends option parsing, for a first real argument that
  genuinely starts with `capture=` or `jq=`.

The worked example below does **not** wire `sys:` — the exec pack pulls
in gojq, so it lives in its own Go module
(`github.com/WonderForgeLabs/gooey/handlers/exec`) and cannot be
imported from the root module's example tree. `net` and `fs` are
stdlib-only and root-resident; that split is the module-boundary rule,
and it is why the snippets above are snippets. The full running exec
sample is [`apps/gitui`](../../../apps/gitui); the contract doc
is [`handlers/exec/README.md`](../../../handlers/exec/README.md) and
the design record the
[exec pack spec](../../specs/2026-08-10-exec-pack.md).

## The temporal: namespaces

Two more packs follow the same grammar and live in
`handlers/temporal` (another nested module — the Temporal SDK's
dependency graph):

- `temporal:` — `` {{temporal:Activity `Slugify` .Input | into .Output}} ``
  executes a Temporal standalone activity: the terminal declares *what*
  runs, workers anywhere decide *how*.
- `wf:` — `` {{wf:Signal `approve` .Tier | into .Notice}} `` signals
  the one workflow the provider was constructed around — the
  workflow-serves-the-UI direction, and the pack that proves `| into`
  is optional when there is no result to deliver.

Both are the subject of [Tutorial 9](../09-temporal.md), which builds
the worker and the wizard around them.

## The worked example

[`docs/learn/examples/howto-handlers`](../examples/howto-handlers) is
Tasks 1 and 2 as one running page — two buttons and a path field, no
delegates:

```sh
cd docs/learn/examples/howto-handlers && go run .
```

`main.go` is almost entirely grants and viewmodel: it starts a loopback
HTTP server so `net:Get` works offline, registers `net` with the
default client and `fs` over `os.DirFS(".")` — the example's own
directory, read-only — and hands the page `app.Dispatcher()`. Three
things worth trying while it runs:

- Press **fs:Read** with the default path: the page reads its own
  `app.gooey` source through the grant.
- Edit the path field to `main.go` and press again — the argument is
  read at press time, not load time.
- Edit it to `../go.mod` and press: the containment answers with
  `ERROR: "../go.mod": not a valid path …` *in the content pane*,
  because a bound path is validated at invoke time. (Written as a
  literal in the markup, the same path would have refused to load.)

## Current limitations

- **The pipeline has one stage.** `| into .Target`, single, string
  result. Multiple targets, `err`/`progress` tails, and converter
  stages are the
  [pipeline grammar v2](../../specs/2026-08-10-pipeline-grammar-v2.md)
  record, designed but not built.
- **Results are strings.** `fs:List` and `capture=both` deliver JSON
  *text*; projecting it into rows is yours to do in Go today (a
  `json:Deserialize` stage is v2 territory).
- **Failures are in-band** `ERROR: …` strings by convention — honest,
  visible, and impossible to route separately from data in v1.
- **No subscriptions.** Handlers are one-shot commands: no `fs:Watch`,
  no streaming output from `sys:Run` — those need a lifetime, which the
  v1 grammar has no owner for.

## See also

- Reference:
  [markup-reference.md § Handler namespaces](../../markup-reference.md#handler-namespaces)
  — the complete grammar and provider table.
- The pack contract docs:
  [`handlers/net`](../../../handlers/net/README.md),
  [`handlers/fs`](../../../handlers/fs/README.md),
  [`handlers/exec`](../../../handlers/exec/README.md).
- Doctrine:
  [pack distribution](../../specs/2026-08-10-pack-distribution.md) —
  registration-as-grant, module boundaries, inventory rules.
- [How to work off the UI goroutine](howto-async.md) — the Dispatcher,
  from the app's side.
- [Tutorial 3](../03-binding-and-state.md) — the read-versus-subscribe
  rule the invoke-time argument reads build on.
- [Tutorial 9](../09-temporal.md) — the temporal packs at full size.
