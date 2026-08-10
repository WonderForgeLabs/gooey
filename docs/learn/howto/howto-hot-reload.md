# How to hot-reload markup while the app runs

Edit a `.gooey` file, save, and the running UI rebuilds in place with
state intact.

## Watch one file

Hot reload is not something you add — it is what `markup.Page` does. Give
it to an App and edits to the file rebuild the tree:

```go
app = gooey.NewApp(markup.Page(os.DirFS("."), "app.gooey", ctx))
```

Under that one line: the page polls the file's ModTime, reports a change
to the App, and the App rebuilds the tree and replaces the composition.
The rebuild happens **on the UI goroutine**, which is the part that has
to be right — building a tree resolves bindings against your properties,
and properties may only be touched from the loop. The watcher reports
that something changed; it never hands over a tree it built itself.

Replacing the composition (rather than patching it) is forced by the
design: the Composer wires every component's paint node at construction, so
a structural change means a new Composer. The App closes the outgoing one
first, which is what stops a replaced tree's `<Timer>` from ticking on
against a viewmodel nobody is showing.

## Watch a set of files

A page plus its controls reload together — one rebuild re-instantiates
every control instance. Name the other files and any of them triggers it:

```go
markup.Page(fsys, "page.gooey", ctx, "statpanel.gooey", "card.gooey")
```

They are named rather than discovered because an `<Include>` is resolved
during a build, and the build being watched for has not happened yet.

## Re-find named components after a reload

`Name="..."` registers a component in `ctx.Named`, and each rebuild resets
that map. Any handle you cached is stale after a swap:

```go
var stats *components.Text
app.OnSwap(func(gooey.Component) {
	stats, _ = markup.Find[*components.Text](ctx, "stats") // re-find, every time
})
```

`OnSwap` fires for the initial attach as well as every reload, so this is
the only place that resolves the handle.

## Why state survives

State lives in your viewmodel's properties, not in the components. The tree
is disposable; `count`, `label`, and friends are not. A rebuilt `Text`
binds to the same handle it did before, so it comes back showing the same
value.

Focus does not survive automatically — a new tree focuses its first focus
stop. To restore it, remember which one had focus and call
`app.Composer().Focus().SetFocus(w)` from an `OnSwap` hook.

## Behavior worth knowing

- **Polling, not inotify.** Both watchers poll ModTimes every 300 ms.
- **A broken edit is harmless.** Parse or build errors skip the reload
  and leave the running tree up. Fix and save again.
- **Ask for the error.** By default a failed reload is silent, which is
  the worst possible feedback while editing. Pass
  `gooey.WithErrorHandler(func(err error) { … })` and put the message
  somewhere on screen — `cmd/markuplog` shows it in its stats line.
- **One rebuild per pass.** A multi-file save produces one rebuild, not
  one per file.

## In release builds

Point the same code at an `embed.FS`. Its ModTimes are constant zero, so
watching never fires and the call becomes a natural no-op — no build
tags, no second code path. See
[how to embed markup for release](howto-embed-release.md).

## See also

- [Tutorial 1, step 5](../01-first-app.md) — the walkthrough with a
  capture.
- [Concept: markup tiers and the loading seam](../concepts/markup-tiers.md)
