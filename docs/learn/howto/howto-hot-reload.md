# How to hot-reload markup while the app runs

Edit a `.gooey` file, save, and the running UI rebuilds in place with
state intact.

## Watch one file

```go
fsys := os.DirFS(".")

// The watcher runs on its own goroutine, so it hands the new tree to
// the UI goroutine over a channel rather than attaching it directly.
swaps := make(chan gooey.Widget, 1)
stopWatch := markup.Watch(fsys, "app.gooey", ctx, func(w gooey.Widget) { swaps <- w })
defer stopWatch()

for running {
	if needsFrame { /* Frame + Flush */ }
	select {
	case w := <-swaps:
		attach(w)          // build a NEW Composer around the new tree
	case ev := <-events:
		comp.Handle(ev)
	}
}
```

`attach` must rebuild the Composer, not patch it:

```go
attach := func(w gooey.Widget) {
	comp = gooey.NewComposer(w, cols, rows)
	comp.OnInvalidate(func() { needsFrame = true })
	needsFrame = true
}
```

The Composer is a static-tree design — every widget's paint node is wired
at construction — so a structural change means a new Composer. That is
the whole reason `attach` exists as a function.

## Watch a set of files

A page plus its controls reload together. `WatchAll` fires one callback
on any change; rebuild the page and every control instance is recreated:

```go
files := []string{"page.gooey", "statpanel.gooey", "card.gooey"}

stopWatch := markup.WatchAll(fsys, files, func() {
	if w, err := markup.Load(fsys, "page.gooey", ctx); err == nil {
		swaps <- w
	}
})
defer stopWatch()
```

## Re-find named widgets after a reload

`Name="..."` registers a widget in `ctx.Named`, and each rebuild resets
that map. Any handle you cached is stale after a swap:

```go
case w := <-swaps:
	attach(w)
	stats, _ = markup.Find[*gooey.Text](ctx, "stats") // re-find, every time
```

## Why state survives

State lives in your viewmodel's properties, not in the widgets. The tree
is disposable; `count`, `label`, and friends are not. A rebuilt `Text`
binds to the same handle it did before, so it comes back showing the same
value.

Focus does not survive automatically — a new tree focuses its first focus
stop. To restore it, remember which one had focus and call
`comp.Focus().SetFocus(w)` after attaching.

## Behavior worth knowing

- **Polling, not inotify.** Both watchers poll ModTimes every 300 ms.
- **A broken edit is harmless.** Parse or build errors skip the reload
  and leave the running tree up. Fix and save again.
- **The error is not shown.** The watcher swallows it — if a save seems
  to do nothing, run `markup.Load` once by hand to see the message.
- **`WatchAll` rebuilds on the first changed file** it notices in a pass,
  so a multi-file save produces one rebuild, not several.

## In release builds

Point the same code at an `embed.FS`. Its ModTimes are constant zero, so
`Watch` never fires and the call becomes a natural no-op — no build tags,
no second code path. See
[how to embed markup for release](howto-embed-release.md).

## See also

- [Tutorial 1, step 5](../01-first-app.md) — the walkthrough with a
  capture.
- [Concept: markup tiers and the loading seam](../concepts/markup-tiers.md)
