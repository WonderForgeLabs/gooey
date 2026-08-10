# exec — a handler pack

Local commands from markup, behind an explicit allowlist: the `sys:`
namespace.

```xml
<Gooey xmlns="wonderforge.io/gooey/2026"
       xmlns:sys="gooey.dev/handlers/exec">
  <Button Content="status" Click="{{sys:Run `git-status` | into .Out}}"/>
</Gooey>
```

The host app grants the capability by registering the provider — and
the grant here is itemized. Registration names every command markup
may run:

```go
p, err := exechandlers.New([]exechandlers.Command{
        {Name: "git-status", Path: "git", Args: []string{"status", "--short"}},
        {Name: "list-pods", Path: "kubectl",
                Args: []string{"get", "pods", "-o", "json"},
                Jq:   ".items[].metadata.name"},
})
markup.RegisterHandlers(exechandlers.URI, p)
```

Untrusted markup **never names a binary**. The first argument to
`sys:Run` must be a backtick literal naming a registered `Command`,
checked at load time; the allowlist is the grant, fixed at
construction, and markup can never expand it. There is no shell
anywhere in this pack: a `Command` is an argv, markup arguments are
appended as argv elements, and nothing is ever re-parsed by `/bin/sh`.
The doctrine: `docs/specs/2026-08-10-pack-distribution.md`.

**Nested module, on purpose.** Structured extraction takes
[gojq](https://github.com/itchyny/gojq) (pure Go, but a real
dependency), so the pack lives in its own module
(`github.com/WonderForgeLabs/gooey/handlers/exec`) under the
any-third-party-dep-forces-a-nested-module clause of the
module-boundary rule. The root `go build ./...` provably excludes
gojq; compare `handlers/net`, stdlib-only and root-resident.

## Functions

Exported as constants, enumerated by `exechandlers.AllNames()`:

| function | shape | does |
|---|---|---|
| `sys:Run` (`NameRun`) | ``{{sys:Run `name` [options] [args…] \| into .Out}}`` | runs the registered command `name` off the UI goroutine; the captured output lands in the target property as a string |

Options are backtick literals between the name and the arguments, each
at most once, all validated at load time:

- `` `capture=stdout|stderr|combined|both|exit-code` `` — overrides the
  registration's capture mode. `both` delivers tagged JSON
  (`{"exit":N,"stdout":"…","stderr":"…"}`); `exit-code` delivers the
  decimal code; both of those treat a non-zero exit as data, where the
  stream modes deliver it as an `"ERROR: …"` string.
- `` `jq=EXPRESSION` `` — gojq extraction over the delivered text
  (which must then be JSON), compiled at load; overrides the
  registration's `Jq`.
- `` `--` `` — ends option parsing, for a first argument that genuinely
  starts with `capture=` or `jq=`.

Everything resolvable resolves at load time: an unregistered name, a
bound (non-literal) command name, a bad capture mode, a jq expression
that does not compile, arguments a registration's `ArgPolicy` does not
allow, a missing `| into` target, and unknown functions are all load
errors, never click surprises.

## The grant's scope

What registration actually reaches is fixed at construction, by the
host, in Go — per command:

- `Path` — the binary. Bare names resolve through the PATH once, at
  registration, and are pinned to the absolute result.
- `Args` — the baked argv prefix markup cannot alter.
- `ArgPolicy`/`MaxArgs` — whether markup may append arguments at all
  (`ArgsNone`, the default) and at most how many (`ArgsAny` +
  `MaxArgs`). Appended arguments are argv elements only.
- `Env`/`PassEnv` — the child's environment is scrubbed by
  construction: it starts **empty**, plus the registration's explicit
  entries, plus named pass-throughs from the host's environment.
  Secrets never leak into children by default.
- `Dir`, `Timeout`, `Capture`, `Jq` — working directory, per-command
  timeout, default capture mode, default extraction.

And per provider: `WithDefaultTimeout` (30s), `WithKillDelay` (the
SIGTERM→SIGKILL grace on timeout, 2s), `WithMaxOutput` (per-stream
byte cap, 1 MiB — a runaway stream is drained but not delivered).

Process hygiene follows `docs/specs/2026-08-10-companions.md`: the
child runs in its own process group (Unix), never gets a tty (stdio is
pipes and the null device), and a timeout kills the whole group —
SIGTERM, the grace window, SIGKILL.

Results marshal back to the UI goroutine through the document's
`Dispatcher` (required at load time). See the
[handler-namespaces reference](../../docs/markup-reference.md#handler-namespaces)
and the design record `docs/specs/2026-08-10-exec-pack.md` — including
how the capture enum and the jq option map onto pipeline grammar v2.
