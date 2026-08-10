# fs — a handler pack

Files from markup, with no app code — and the registration names a
**root**, so the grant carries its own extent.

```xml
<Gooey xmlns="wonderforge.io/gooey/2026"
       xmlns:fs="gooey.dev/handlers/fs">
  <Button Content="open"  Click="{{fs:Read .Path | into .Content}}"/>
  <Button Content="files" Click="{{fs:List `docs` | into .Entries}}"/>
</Gooey>
```

The host app grants the capability by registering the provider:

```go
// Read-only: the fs.FS IS the grant's extent.
markup.RegisterHandlers(fshandlers.URI, fshandlers.New(os.DirFS("./docs")))
```

Without that line the same document fails **to load**, naming the URI
it wanted. Registration is the capability grant — markup, including
markup served from elsewhere, can only invoke namespaces its host
registered, and can never expand its own grants. The doctrine:
`docs/specs/2026-08-10-pack-distribution.md`.

**Root module, on purpose.** The pack's graph is the Go standard
library, so it lives in the root gooey module (`handlers/fs`) and
rides the root version — the zero-third-party-dep side of the
module-boundary rule. Compare `handlers/temporal`, whose SDK graph
forces a nested module.

## Functions

Exported as constants, enumerated by `fshandlers.AllNames()`:

| function | shape | does |
|---|---|---|
| `fs:Read` (`NameRead`) | ``{{fs:Read .Path \| into .Content}}`` | file contents as a string, capped (1 MiB default) |
| `fs:List` (`NameList`) | ``{{fs:List .Dir \| into .Entries}}`` | JSON array of directory entries |
| `fs:Stat` (`NameStat`) | ``{{fs:Stat .Path \| into .Info}}`` | JSON of one entry |
| `fs:Glob` (`NameGlob`) | ``{{fs:Glob .Pattern \| into .Paths}}`` | JSON array of matching paths (`[]`, never `null`) |
| `fs:Write` (`NameWrite`) | ``{{fs:Write .Path .Content \| into .Status}}`` | writes the file — writable grant only |
| `fs:Append` (`NameAppend`) | ``{{fs:Append .Path .Content \| into .Status}}`` | appends (creating if absent) — writable grant only |

A directory entry is protojson-adjacent JSON:
`{"name":"spec.md","size":1204,"dir":false,"modTime":"2026-08-10T12:00:00Z"}`
— `modTime` RFC 3339, `""` when the FS reports a zero time; `size` a
JSON number.

Everything resolvable resolves at load time: wrong arity, a missing
`| into` target, unknown functions, a *literal* path that is not a
valid `fs.ValidPath`, and `Write`/`Append` on a read-only grant are
all load errors, never click surprises. Failures at run time are
delivered to the same target as an `"ERROR: …"` string. `Write` and
`Append` use their target as a status slot: `""` on success, the ERROR
string on failure.

## The grant's scope

What registration actually reaches is fixed at construction, by the
host, in Go:

- `New(fsys fs.FS)` — read-only, the default posture. Every path a
  page names resolves inside fsys and nowhere else: escapes are
  rejected structurally per `fs.ValidPath` (relative, slash-separated,
  no `..`, no leading `/`). An `embed.FS` grants its embedded files,
  `os.DirFS` a subtree, a TarFS an archive.
- `NewWritable(dir)` — the read-write grant, a deliberately separate
  constructor so writes are a visible decision at the registration
  site. Backed by `os.Root`: the OS itself refuses any resolution
  (symlinks included) that would leave dir.
- `WithMaxRead(n)` — caps what `Read` delivers (default 1 MiB).
  Over-cap files fail with an ERROR naming the cap rather than
  truncate — a silently cut file is corrupt data.

Watching a file for changes is deliberately absent: a v1 handler is
one-shot and command-shaped, while a watch is a subscription with a
lifetime — that arrives with the pipeline grammar v2 or a companion.

Results marshal back to the UI goroutine through the document's
`Dispatcher` (required at load time). See the
[handler-namespaces reference](../../docs/markup-reference.md#handler-namespaces)
and the design record `docs/specs/2026-08-10-fs-pack.md`.
