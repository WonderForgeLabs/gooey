# How to embed markup for a release build

Ship a single binary with the `.gooey` files inside it, using the same
code that hot-reloads in development.

## Embed the files

`markup.Load` takes an `fs.FS` and cannot tell one implementation from
another, so an `embed.FS` drops straight in:

```go
import "embed"

//go:embed *.gooey
var ui embed.FS

func main() {
	// dev:     fsys := os.DirFS(".")
	// release: fsys := ui
	var fsys fs.FS = ui

	tree, err := markup.Load(fsys, "app.gooey", ctx)
	// ...
}
```

Rules the `embed` directive imposes:

- The `//go:embed` line must sit immediately above the variable, with no
  blank line between.
- Patterns are relative to the package directory and cannot escape it —
  no `../shared/app.gooey`. Keep markup beside the package that embeds
  it, or embed from a package in that directory.
- `//go:embed ui` embeds a whole directory; the paths you pass to `Load`
  then include it (`ui/app.gooey`). `//go:embed ui/*.gooey` is the
  narrower form.
- Files whose names begin with `.` or `_` are skipped unless you use the
  `all:` prefix.

## Leave the watching in

You do not need a build tag or a second code path. `embed.FS` reports
constant zero ModTimes, so the page never sees a change and never
rebuilds:

```go
// Compiles and runs identically in both tiers. On embed.FS it is inert.
app = gooey.NewApp(markup.Page(fsys, "app.gooey", ctx))
```

The watcher goroutine still exists and still ticks every 300 ms; it just
never finds a newer ModTime.

## Pick the filesystem at startup

The pattern that keeps one binary useful for both:

```go
var fsys fs.FS = ui // embedded by default
if dir := os.Getenv("GOOEY_UI_DIR"); dir != "" {
	fsys = os.DirFS(dir) // point at source files to hot-reload
}
```

Now the shipped binary is self-contained, and setting one environment
variable turns it back into a live-editing development build.

## Verify it before you ship

The failure mode is a missing file, and it shows up only at load. Two
cheap guards:

- **A test.** `markup.Load(ui, "app.gooey", ctx)` in a `_test.go` fails
  the build if a file was renamed or the pattern stopped matching.
- **Run from elsewhere.** `cd /tmp && /path/to/app` — an embedded binary
  works from any directory. If it only works from the source tree, the
  files are not actually embedded.

## Caveats

- Embedded markup cannot be patched after release. Everything the UI
  needs at load time — element names, binding paths — is fixed when you
  compile.
- Embedding does not precompile anything. The XML is still parsed and the
  tree still built at startup; the only change is where the bytes come
  from. A compiled tier (`gooey gen`) is designed but not implemented.

## See also

- [How to hot-reload markup](howto-hot-reload.md)
- [Concept: markup tiers and the loading seam](../concepts/markup-tiers.md)
