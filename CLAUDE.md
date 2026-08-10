# gooey — XAML-like TUI framework for Go

Retained visual tree, lazy dependency-property graph, XML markup with
Go-template bindings, routed input, damage-tracked rendering.

**Read first:** `docs/architecture.md` (how it works, grounded in real
types), `docs/markup-reference.md` (markup syntax),
`docs/getting-started.md`, `docs/demos.md`, `docs/learn/` (tutorials
backed by runnable `examples/NN-*/`). Design records in `docs/specs/`.

## Verify (CI parity)

`go build ./... && go vet ./... && go test ./...`

## Architecture invariants — do not violate without a design change

- **No reflection.** Bindings resolve to typed `*prop.Property[T]`
  handles at build time (lvalue, not value, semantics).
- **Properties are lazy and UI-goroutine-confined.** `Set` marks dirty;
  `Get` re-evaluates and records dependencies automatically. Anything
  from another goroutine (fetches, timers) crosses back via channel or
  `Dispatcher` before touching a property.
- **Composer damage: leaves pre-clear their bounds, containers never
  do** — a container's bounds enclose its children's cells (see
  `TestContainerRepaintPreservesChildCells`).
- **Parents never call `child.Measure`/`Arrange` directly** — always
  `MeasureChild`/`ArrangeChild` (margin/size/align/visibility sandwich).
  Layout runs every frame outside the eval context, so its reads are
  deliberately untracked.
- **Containers implement `ChildWidgets()` and paint only their own
  chrome**; the framework walks children.
- **UserControls get their own `markup.Context`**; data crosses only via
  attribute bindings resolved in the *parent* context.
- **Markup loads through `io/fs.FS`** (os.DirFS dev / embed.FS release);
  `Watch` is a natural no-op on an immutable FS.
- One cell plane + N pixel protocols (kitty > sixel > iterm2 >
  halfblock); only pixel content is protocol-specific.

## Gotchas

- `os.File.Fd()` detaches the file from the netpoller, so a blocked
  `Read` never unblocks on `Close` — see
  `docs/specs/2026-08-10-tty-read-lifecycle.md` before touching
  `term.Screen` lifecycle or event goroutines.
- `SetReadDeadline` is unsupported on some ttys/ptys: read in a
  goroutine with a `select` timeout (`term.readUntilDA1`).
- When `Set`ting props right before a frame, clear `needsFrame` *after*
  `comp.Frame()` or you drop the repaint they just caused.

## Headless testing & demo capture (not covered in docs/)

- Drive an interactive app under a pty:
  `(sleep 1; printf 'j') | script -qec "stty cols 100 rows 28; ./app" out.log`
  — `script` ptys report 0×0 without `stty` (term.Size falls back to 80×24).
- **Use octal printf escapes** in `/bin/sh` scripts (`\033[B`, `\177`,
  `\015`) — dash renders `\x1b` hex escapes as literal text.
- asciinema needs the app wrapped in a **nested `script -qec`** or the
  cast records empty.
- `agg` renders the cell plane only → capture GIFs in halfblock mode.
- Inspect GIF frames with `convert -coalesce out.gif f-%d.png`; raw
  frames are diffs and look corrupted. Sample densely where counters
  change — sparse sampling misses hold beats.
- One full-frame flush is ~20KB+ at 120×32, so `tail -c 5000` truncates
  mid-frame; find the last frame by searching for the final `\x1b[H`.
