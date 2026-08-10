# Tutorial 1: Build your first gooey app

In this tutorial you build a complete gooey application from an empty
directory: a markup file that describes the UI, a Go file that supplies
the state and the host loop, and a live edit that reloads the running app
without restarting it.

**Time:** about 15 minutes.

When you finish, you will have this:

![The finished first app: a bordered panel with a styled greeting](media/01-first-app.png)

The finished code is in
[`examples/01-first-app`](../../examples/01-first-app).

## Prerequisites

- Go — the version in [`go.mod`](../../go.mod) or newer.
- A real terminal. gooey apps open `/dev/tty` directly and exit with
  `no tty:` if there isn't one, so they do not run under an IDE's output
  pane or a piped shell.

## Step 1: Create the project

```sh
mkdir hello && cd hello
go mod init hello
go get github.com/WonderForgeLabs/gooey
```

## Step 2: Describe the UI in markup

Create `app.gooey`. A `.gooey` file is XML: elements are widgets,
attributes are properties, and `{{.Name}}` is a binding into the data
your Go code supplies.

```xml
<Gooey xmlns="wonderforge.io/gooey/2026">
  <Border Title="first app" Style="panel">
    <VStack Gap="1">
      <Text Style="accent">{{.Greeting}}</Text>
      <Text Style="dim">edit app.gooey and save — this reloads in place</Text>
      <Text Style="dim">press q to quit</Text>
    </VStack>
    <KeyBinding Gesture="q" Command="{{.Quit}}"/>
    <KeyBinding Gesture="ctrl+c" Command="{{.Quit}}"/>
  </Border>
</Gooey>
```

Four rules are in play here, and all four are enforced when the file
loads:

- The root element is always `<Gooey>`, with exactly **one** child.
- `<Border>` wraps exactly one visual child. `<KeyBinding>` is not
  visual, so it does not count against that limit.
- `Style="panel"` is a **name**, looked up in a table you register in Go.
  It is not a color and not a stylesheet.
- `{{.Greeting}}` and `{{.Quit}}` must resolve against your data at load
  time, or the load fails with an error naming the path.

> **If you know XAML:** `<Gooey>` plays the part of `<Window>` or
> `<UserControl>`, `VStack`/`HStack` are `StackPanel` with
> `Orientation`, and `<KeyBinding>` is the direct analogue of
> `<Window.InputBindings><KeyBinding .../></Window.InputBindings>` —
> minus the ceremony, because gooey attaches it to whatever element
> encloses it.

### Why the KeyBindings sit on `<Border>` and not on `<VStack>`

Key dispatch starts at the focused widget and walks **up** to the root,
firing bindings attached along the way. This page has no focusable
widgets yet, so dispatch starts at the root element itself and the only
bindings it can reach are the ones attached there.

Move those two `<KeyBinding>` elements inside `<VStack>` and `q` stops
working — the walk never descends into the stack. Once your page has a
focus stop (a `Button`, from tutorial 4 on), bindings anywhere on the
path from that button to the root are reachable. Until then, put
page-global bindings on the root element.

## Step 3: Supply the state and run the loop

Create `main.go`. It has three parts: a viewmodel, a `markup.Context`
that exposes it to the markup, and the host loop.

```go
package main

import (
	"fmt"
	"os"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
	"github.com/WonderForgeLabs/gooey/term"
)

func main() {
	// --- viewmodel: the state and the commands markup binds to ---
	running := true
	greeting := prop.NewSource("hello, gooey")

	ctx := &markup.Context{
		Values: map[string]any{
			"Greeting": greeting,
			"Quit":     gooey.Command(func() { running = false }),
		},
		Styles: map[string]render.Style{
			"panel":  {Fg: render.RGB(120, 90, 220)},
			"accent": {Fg: render.RGB(255, 170, 60), Bold: true},
			"dim":    {Fg: render.RGB(140, 140, 150)},
		},
	}

	// --- load the markup ---
	fsys := os.DirFS(".")
	tree, err := markup.Load(fsys, "app.gooey", ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	// --- the host loop ---
	screen, err := term.Open()
	if err != nil {
		fmt.Fprintln(os.Stderr, "no tty:", err)
		os.Exit(1)
	}
	cols, rows := screen.Size()

	var comp *gooey.Composer
	needsFrame := true
	attach := func(w gooey.Widget) {
		comp = gooey.NewComposer(w, cols, rows)
		comp.OnInvalidate(func() { needsFrame = true })
		needsFrame = true
	}
	attach(tree)

	swaps := make(chan gooey.Widget, 1)
	stopWatch := markup.Watch(fsys, "app.gooey", ctx, func(w gooey.Widget) { swaps <- w })
	defer stopWatch()

	if err := screen.Raw(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer screen.Restore()

	events := make(chan input.Event, 16)
	go term.DecodeEvents(screen, events)

	for running {
		if needsFrame {
			comp.Frame()
			comp.Flush(screen.File())
			needsFrame = false
		}
		select {
		case w := <-swaps:
			attach(w)
		case ev := <-events:
			comp.Handle(ev)
		}
	}
}
```

Run it:

```sh
go run .
```

You should see the panel from the top of this page. Press `q` to quit.

> **Troubleshooting.** `no tty:` means the process has no terminal — run
> it directly rather than through a pipe or an IDE console.
> `markup: "Greeting" not found in context` means the name in the markup
> and the key in `Values` disagree; the two are matched by exact string.

## Step 4: Understand the loop you just wrote

Six pieces do all the work. You will write this same loop in every
tutorial that follows, so it is worth reading once.

**`markup.Load(fsys, name, ctx)`** reads the file from any `fs.FS` and
builds a widget tree. Bindings are resolved **now**, once, into property
handles — the built widgets hold the handle, not a copy of the value, and
rendering never looks anything up.

**`gooey.NewComposer(tree, cols, rows)`** is the retained, damage-tracked
render path. Building it gives every widget its own node in the property
graph; whatever a widget reads while painting becomes that widget's
repaint trigger, automatically.

**`comp.OnInvalidate(...)`** is the scheduler, in one line. Any `Set` on
a property some widget painted from marks that widget dirty and fires
this hook. You only raise a flag — because dirty flags accumulate and
evaluation is lazy, twenty `Set`s between frames collapse into one
repaint.

**`screen.Raw()` / `defer screen.Restore()`** enter raw mode plus the
alternate screen, and undo all of it on the way out — including turning
mouse reporting off, so a panic never leaves the terminal wedged.

**`term.DecodeEvents(screen, events)`** is the input pump, on its own
goroutine. It turns tty bytes into `input.Event` values (keys and mouse
reports on one ordered stream) and resolves escape ambiguity — a lone Esc
versus the start of an arrow key — with a 40 ms idle timeout.

**The loop body** renders if dirty, then blocks. `comp.Frame()` runs
layout and repaints only the dirty widgets into a persistent buffer;
`comp.Flush` writes it; `comp.Handle` routes the event through the tree.

> **If you know XAML:** there is no `Application.Run()` and no built-in
> dispatcher. You own the loop, which is why the `select` is visible.
> Everything — commands, property `Set`s, rendering — runs on this one
> goroutine; properties are unsynchronized by design. Background work
> hands results back over a channel (see
> [how-to: work off the UI goroutine](howto/howto-async.md)).

## Step 5: Edit the running app

Leave the app running. In another editor, change `app.gooey` — retitle
the border, add a line, change a style name — and save.

![The app reloading in place after an edit to app.gooey](media/01-hot-reload.gif)

The UI rebuilds in place. `markup.Watch` polls the file's ModTime every
300 ms, reloads on change, and hands the new tree to the loop over the
`swaps` channel because the watcher runs on its own goroutine.

Two things make this safe rather than clever:

- **State survives** because it lives in the properties in your
  viewmodel, not in the widgets. The tree is disposable; `greeting` is
  not.
- **A broken edit is harmless.** If the file does not parse, the reload
  is skipped and the running tree stays up. Fix the file, save again.

The Composer is rebuilt rather than patched — it is a static-tree design,
so structural change means a new Composer, which is exactly what `attach`
does.

## What you learned

- A gooey UI is a `.gooey` XML file plus a `markup.Context` that supplies
  values, commands, and named styles.
- Bindings resolve **once at load time** into property handles, so
  rendering does no lookups.
- The Composer gives each widget its own paint node; `OnInvalidate` is
  the entire scheduler.
- `KeyBinding` fires only if the focused widget's path to the root passes
  through the element it is attached to — with no focus stops, that means
  the root.
- Hot reload is `markup.Watch` plus a channel, and it keeps state because
  state is not in the tree.

## Current limitations

- The Composer is fixed-size: it takes the terminal size at construction
  and **does not react to resize** yet.
- The watcher is 300 ms ModTime polling, not an OS file-watch API.
- `Style="name"` is a lookup with no cascading, selectors, or overrides.

## Next steps

- **[Tutorial 2: Lay out a page with Grid](02-layout.md)** — Fixed, Auto,
  and Star tracks, and the layout attributes every element accepts.
- Concept: [the property graph](concepts/property-graph.md) ·
  [damage tracking](concepts/damage.md)
- Reference: [markup-reference.md](../markup-reference.md)
