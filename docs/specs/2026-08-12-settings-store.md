# Settings: external state as bindable properties

Status: implemented (`settings/`, `cmd/settingsdemo`)

## The ask

An app wants to remember things across runs — the browser's last source,
whether recording stays on, whether the dev loop restarts the app. One
JSON document, namespaced like vscode's `settings.json`. The host owns
persistence. CRUD, upsert for simplicity.

## What makes this not a config file

A `Read(key) string` at startup is a snapshot, and a snapshot drifts:
nothing repaints when it changes, two readers see two values, and the
"current" setting lives in whatever local variable read it first.

gooey already has the right shape for live external state — a typed
property handle. So the decision is:

> **A setting IS an ordinary `*prop.Property[T]` source property.**
> There is no settings-specific binding mechanism, no adapter, no
> `SettingsBinding` element. `settings.Value(store, key, def)` returns a
> handle indistinguishable from a viewmodel field, and markup, damage
> tracking, and type checking apply to it unchanged.

`settings/markup_test.go` pins both halves of "unchanged": a page binding
`{{.LastSource}}` to a `Text` and `{{.AutoRestart}}` to a `Checkbox`
repaints exactly one component when the setting changes, and binding the
string setting where a `bool` is wanted is a **load** error naming
`*prop.Property[bool]`.

## The persistence seam

```go
type Provider interface {
    Load() ([]byte, error)
    Save(doc []byte) error
}
```

Whole document, as bytes, in both directions.

**Bytes, not a decoded map**, because the host should not have to model
keys or types — a row in a database, an HTTP `PUT`, a keychain entry and
a file are all the same three lines. The store owns JSON; the provider
owns durability.

**Whole document, not per-key**, because partial writes are where
ordering bugs live. One writer, one payload, and a save can never land
half-applied. It also makes `Load` returning nil the entire first-run
story.

`File` (atomic temp-file + rename), `UserFile` and `Memory` ship with the
package, and none of them is privileged: they implement the same
interface a host implements, which is the only way "the host owns
persistence" is true rather than decorative.

## Write-through vs explicit save: neither

The obvious design is write-through — `handle.Set(v)` writes the
document. Two facts kill it.

**1. `prop.Set` does not compare values** (`prop/prop.go:101`). Fifty
identical `Set`s are fifty invalidations, so write-through means fifty
disk writes for a change that did not happen.

**2. There is no hook to write through *on*.** `Property.Set` invalidates
its **dependents** and never its own node, and `OnInvalidate` fires only
from `node.invalidate`. A source property's own invalidate hook therefore
*cannot* fire. Observing a `Set` from outside requires a **dependent** —
which is what the store arms:

```go
e.watch = prop.NewComputed(func() int { p.Get(); return 0 })
e.watch.OnInvalidate(func() { s.changed() })
e.watch.Get() // arm
```

This is the same trick as `Composer.armVisibility` (`composer.go:367`),
and the same subtlety applies: `invalidate` returns early when already
dirty, so the hook fires **once** per clean→dirty transition and the
watcher has to be re-`Get` to re-arm. `Store.snapshot` re-arms every
watcher on every flush — which is exactly where the coalescing comes
from, for free.

The other obvious design is an explicit `Save()` the app must remember to
call. That is the drift the package exists to remove.

So: **dirty-tracked deferred save.**

- a `Set` marks the store dirty and, at most once per dispatcher batch,
  posts a `Flush`;
- `Flush` encodes the whole document and compares it byte-for-byte
  against the last document handed to the provider;
- only a document that actually differs reaches `Provider.Save`.

**The comparison `prop.Set` declines to make happens once per flush, over
the whole document, where it is cheap and correct** — rather than at a
call site the property system gives no hook for.

Measured through the real app under a pty (`cmd/settingsdemo` counts its
own provider's writes):

| interaction | disk writes |
|---|---|
| launch and quit, nothing touched | 0 |
| one toggle | 1 |
| three toggles delivered in one read | 1 |
| six `Set`s that cancel out, in three batches | 0 |

`TestNoOpSetsCostNoWrite` pins the first and last rows deterministically
(50 no-op `Set`s, 0 writes); `TestChangingSetsWriteOncePerBatch` and
`TestEachBatchIsItsOwnWrite` are its discrimination halves — without them
"never writes at all" would pass.

## UI-goroutine confinement

`Store.Start(post)` has `gooey.Startable`'s signature and owns one writer
goroutine. `Flush` hands it a complete document through a one-slot
channel, newest wins; the writer calls `Provider.Save` and **posts** any
error back, never applying it. Every other method on `Store` is UI
goroutine only, because `Delete` and the watchers touch properties.

`stop` flushes what is dirty, then closes **and joins** — so after it
returns the provider has certainly seen the last document and will
certainly never be called again. Quitting cannot lose the change that
prompted the quit (`TestStopFlushesThePendingChange`), and quitting with
nothing dirty writes nothing (`TestStopWritesNothingWhenNothingChanged`).

## Document rules

- **Flat, dotted keys**, like vscode. A key is one opaque string on every
  wire it crosses.
- **A value equal to its default is absent from the document.** Writing
  defaults out would freeze today's default into every user's file and
  silently veto tomorrow's.
- **Keys no handle owns pass through verbatim.** A plugin's keys, or a
  newer version's, survive a save by a process that has never heard of
  them. `Delete` still removes them, which is what distinguishes
  pass-through from echoing the loaded bytes.
- **A mistyped stored value is reported AND survived.** `Value` returns a
  usable handle carrying the default *and* an error naming the key and
  both types. A hand-edited file must not stop the app from starting, and
  must not be silent either.

## No untyped `Write`

The CRUD the ask named maps as:

| ask | surface |
|---|---|
| read | `handle.Get()`, or `Store.Raw(key)` for tooling with no types |
| upsert | `handle.Set(v)` |
| delete | `Store.Delete(key)` |
| list | `Store.Keys()` |

There is deliberately **no** `Write(key string, v any)`. A write has to
reach the typed handle or the bound UI diverges from the document with
nothing to say so, and reaching a typed handle from an untyped key is
exactly the reflection the framework does not do. `Delete` is the
destructive half, and on a registered key it means "forget it, go back
to the default" — an ordinary `Set`, so anything bound to it repaints.

## No reflection in core

The store never asks a value what it is. `T` is a compile-time parameter
of `settings.Value`, so each registered key carries closures that already
know their own type — the same discipline as markup's `propKinds` table.
`encoding/json` is reflective internally, as `fmt.Sprintf("%T")` in
`markup/property.go` already is; the *binding* surface is typed handles
end to end, which is what a future `gooey gen` needs.

`settings` imports `prop` and stdlib and nothing else — not `gooey`, not
`components`. The root module's two direct requirements are unchanged.

## Auto-restart is a flag here and a supervisor elsewhere

"Auto restart App" — kill it, it recompiles and relaunches — is a
dev-loop feature, not a preference. The split:

- **the flag** (`browser.autoRestartApp`) is an ordinary bool setting;
- **the supervisor** that reads it belongs with the child-process
  machinery (`companion.go`, `handlers/exec`), not here. Putting restart
  logic in the settings package would make a persistence package own
  process lifetime, and would drag `os/exec` into a package whose whole
  dependency list is `prop`.

It also wants a different **scope**: a dev-loop flag is project-local
(`.gooey/settings.json` beside the tree), while "last source" and "keep
recording" are user-global (`$XDG_CONFIG_HOME/...`). Choosing between
those is one `Provider` — which is the payoff of the seam, and the reason
`UserFile` is a convenience rather than a policy.

## Open questions

- **Two stores over one file.** Nothing detects a second process writing
  the same document; last writer wins and the loser never notices. A
  reload-on-change path (the `os.DirFS` watcher already in the tree) is
  the natural answer and is not built.
- **A `TextBox` bound straight to a setting** writes once per keystroke:
  the document comparison suppresses no-op writes, not genuinely
  different ones. `Store.AutoSave(false)` + an explicit `Flush` on commit
  is the current answer; a debounce would be additive.
- **`Start` takes the baseline**, so every key must be registered before
  it. Hot reload that re-registers is a load error today rather than a
  supported flow.
