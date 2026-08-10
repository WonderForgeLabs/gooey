# net — a handler pack

HTTP from markup, with no app code: the first handler namespace, and
the smallest possible proof of the mechanism.

```xml
<Gooey xmlns="wonderforge.io/gooey/2026"
       xmlns:net="gooey.dev/handlers/net">
  <Button Content="fetch" Click="{{net:Get .Url | into .Body}}"/>
</Gooey>
```

The host app grants the capability by registering the provider:

```go
markup.RegisterHandlers(nethandlers.URI, nethandlers.New())
```

Without that line the same document fails **to load**, naming the URI
it wanted. Registration is the capability grant — markup, including
markup served from elsewhere, can only invoke namespaces its host
registered, and can never expand its own grants. The doctrine:
`docs/specs/2026-08-10-pack-distribution.md`.

**Root module, on purpose.** The pack's graph is the Go standard
library, so it lives in the root gooey module (`handlers/net`) and
rides the root version — the zero-third-party-dep side of the
module-boundary rule. Compare `handlers/temporal`, whose SDK graph
forces a nested module.

## Functions

Exported as constants, enumerated by `nethandlers.AllNames()`:

| function | shape | does |
|---|---|---|
| `net:Get` (`NameGet`) | ``{{net:Get .Url \| into .Body}}`` | HTTP(S) GET off the UI goroutine; the body lands in the target property as a string |

Everything resolvable resolves at load time: wrong arity, a missing
`| into` target, and unknown functions are load errors, never click
surprises. Failures at run time are delivered to the same target as an
`"ERROR: …"` string, so a page can show what went wrong without a
second binding (a status/err split is a pipeline-grammar revision, not
this provider's).

## The grant's scope

What registration actually reaches is fixed at construction, by the
host, in Go:

- `WithClient(*http.Client)` — the transport the capability uses:
  tests, proxies, auth-carrying transports, and *reachability* all
  live here. Markup only names a URL.
- `WithMaxBody(n)` — caps the bytes a response can put into a
  property (default 1 MiB). A terminal shows a screenful; a runaway
  body should not become the application's memory profile.
- Schemes are limited to `http`/`https`.

Results marshal back to the UI goroutine through the document's
`Dispatcher` (required at load time). See the
[handler-namespaces reference](../../docs/markup-reference.md#handler-namespaces)
and the design record `docs/specs/2026-08-10-remote-handlers-design.md`.
