# env — a value pack

Ambient host values, readable from markup with no app code. The first
namespace on the **pull** side of the mechanism.

```xml
<Gooey xmlns="wonderforge.io/gooey/2026"
       xmlns:env="gooey.dev/handlers/env">
  <Text>logged in as {{env:Get `USER`}} on {{env:Get `TERM` `(unknown)`}}</Text>
</Gooey>
```

An environment variable is not an event. It is the *value* of a
binding, so it is registered on the value registry and written where a
binding goes, not on a `Click`:

```go
markup.RegisterValues(envhandlers.URI, envhandlers.New("USER", "TERM"))
```

Writing `{{env:Get …}}` on an event attribute is a load error, and so
is writing `{{net:Get …}}` in a `Text`. The design record is
`docs/specs/2026-08-12-value-namespaces.md`.

**Root module, on purpose.** The pack's graph is `os`, `sort`,
`strings` and `sync` — the standard library — so it lives in the root
gooey module and rides the root version, like `handlers/net` and
`handlers/fs`.

## The grant is an itemized allowlist

`New(names...)` grants *exactly* the variables named, and a name
outside the list is a **load error**, not an empty string:

```
markup: {{env:Get …}}: "AWS_SECRET_ACCESS_KEY" is not in this host's
environment grant; granted: HOME, TERM, USER
```

This is `handlers/exec`'s posture rather than `handlers/fs`'s, and the
reason is that the environment is not a uniform space the way a
directory subtree is. It is where a process keeps its credentials next
to its terminal type. A page loaded from an untrusted `fs.FS`, in a
host that also grants `net:Get`, would otherwise be a two-element
credential exfiltration path with no app code — which is precisely the
scenario the capability model exists to prevent.

There is deliberately **no** "grant everything" constructor. A host
that truly wants one can pass the names from `os.Environ()` itself and
see what it is doing.

Because the grant is a *list* rather than a predicate, it is
enumerable, and `{{env:Names}}` renders it — a namespace that can show
a page what it is allowed to see. (Contrast the handler side, where a
grant is a registered provider and cannot be enumerated per function.)

## Functions

Exported as constants, enumerated by `envhandlers.AllNames()`.

| function | side | shape | does |
|---|---|---|---|
| `env:Get` (`NameGet`) | value | ``{{env:Get `USER`}}`` | the variable's value; empty if unset |
| `env:Get` with a fallback | value | ``{{env:Get `EDITOR` `(none)`}}`` | the fallback when the variable is empty; the fallback may be a bound `.Path` |
| `env:Names` (`NameNames`) | value | `{{env:Names}}` | the sorted grant, comma-separated |
| `env:Set` (`NameSet`) | handler | ``Click="{{env:Set `EDITOR` .Choice}}"`` | writes the process environment **and** the source property |
| `env:Unset` (`NameUnset`) | handler | ``Click="{{env:Unset `EDITOR`}}"`` | clears both |

The variable name is always a **backtick literal**, never a bound
path: the allowlist is checked when the page loads, and a bound name
would move the capability decision to paint time, where markup has no
way to report it.

## Writing is a separate grant

`New` is read-only. `NewWritable` additionally serves `env:Set` and
`env:Unset`, over the same allowlist, and has to be registered on
**both** registries:

```go
p := envhandlers.NewWritable("EDITOR")
markup.RegisterValues(envhandlers.URI, p)   // env:Get, env:Names
markup.RegisterHandlers(envhandlers.URI, p) // env:Set, env:Unset
```

Registering only the first leaves the namespace readable and makes
`env:Set` a load error naming the constructor to change. Two grants,
because "may read the environment" and "may write the environment" are
different decisions.

## Reactivity

`env:Get` hands out a **source property per name**, cached on the
provider, seeded from the environment on first use. That is what makes
the pair reactive: `env:Set` writes the source, so every `Text` bound
to that variable repaints — and *only* those, through the ordinary
damage path. Two bindings to one variable share one source.

The consequence worth knowing: the value is read from the real
environment exactly once, at first resolution. Nothing polls. A
variable changed by something other than `env:Set` — a child process,
a `os.Setenv` elsewhere in the host — will not be noticed. For the
things a terminal UI actually reads (`USER`, `HOME`, `TERM`, `SHELL`)
that is the correct model; for something genuinely live, a source
property the host owns and updates is the right shape instead.

## The test seam

`WithEnviron(map[string]string)` replaces the process environment with
an in-memory map, through `Configure`:

```go
p := envhandlers.New("USER").Configure(
        envhandlers.WithEnviron(map[string]string{"USER": "ada"}))
```

which is also how a host hands a page a synthetic environment without
touching the real one.
