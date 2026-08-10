# How to test a gooey app

Two techniques cover almost everything: **headless assertions** against
the cell buffer for logic and damage, and a **pty transcript** when you
need to prove the real binary behaves.

## Assert against the cell buffer, with no terminal

`gooey.NewComposer` needs a component tree and a size — not a tty. `Frame()`
paints into a plain buffer you can read, and returns how many components
painted. That second value is a contract you can test.

```go
package main

import (
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/prop"
)

// lineAt reads w cells of row y as a string — the assertion primitive.
func lineAt(f *gooey.Frame, y, x, w int) string {
	var sb strings.Builder
	for i := 0; i < w; i++ {
		sb.WriteRune(f.Cells.At(x+i, y).Rune)
	}
	return strings.TrimRight(sb.String(), " ")
}

func TestRendersAndDamages(t *testing.T) {
	name := prop.NewSource("world")
	label := prop.NewComputed(func() string { return "hello, " + name.Get() })

	tree := &components.VStack{Children: []gooey.Component{
		&components.Text{Content: label},
		&components.Text{Content: components.Str("static")},
	}}

	comp := gooey.NewComposer(tree, 40, 4)
	f, painted := comp.Frame()
	if got := lineAt(f, 0, 0, 20); got != "hello, world" {
		t.Fatalf("row 0 = %q", got)
	}
	if painted != 3 { // the stack and both texts
		t.Fatalf("first frame painted %d, want 3", painted)
	}

	name.Set("gooey")
	f, painted = comp.Frame()
	if got := lineAt(f, 0, 0, 20); got != "hello, gooey" {
		t.Fatalf("after Set, row 0 = %q", got)
	}
	if painted != 1 { // only the bound Text
		t.Fatalf("damage: painted %d components, want 1", painted)
	}
}
```

`go test ./...` runs this anywhere, including CI. Some things worth
asserting this way:

- **Text and layout** — read a row, or read `f.Cells.At(x, y).Style` to
  assert color, bold, or reverse video.
- **Damage counts** — the `painted` return. Treat these as contract
  tests: if a one-property change starts repainting the page, that is a
  regression, not an implementation detail.
- **Markup loads** — `markup.Load` against an `fstest.MapFS` needs no
  files on disk, and a bad binding path is an error you can assert on.
- **Input routing** — build a `Composer`, then drive
  `comp.Handle(input.KeyOf(input.Rune('s')))` and check what changed.
  `comp.Focus().Focused()` tells you where focus landed.

## Drive the real binary under a pty

When you need the actual program — terminal setup, the event decoder,
the whole `gooey.App` — run it under `script`, which gives it a
controlling terminal.

```sh
go build -o /tmp/myapp .

{ sleep 0.7; printf '\011\011 q'; sleep 0.7; } |
  script -qec "stty cols 96 rows 24; /tmp/myapp" /tmp/session.log
```

Three details make this work:

- **`script -qec`** allocates the pty and makes it the controlling
  terminal, which is what an app opening `/dev/tty` requires. Plain
  redirection is not enough.
- **`stty cols W rows H`** fixes the size, so a transcript is
  reproducible rather than depending on your window.
- **Octal escapes in `printf`.** `script` runs commands under `sh`, whose
  `printf` has no `\x` hex form. Use `\011` for tab, `\015` for enter,
  `\033[A`/`\033[B`/`\033[C`/`\033[D` for the arrows, and a plain space
  for space.

### Extract the final frame

**Do not look for the last cursor-home in the log.** The flush is
incremental: only a full frame starts with `\x1b[H`, and after the first
one the log holds *differences* — a keystroke that turns `n=2` into `n=3`
puts a single `3` on the wire. Searching the bytes for what the app is
showing finds the first frame, or nothing.

Replay the whole log through `render.Screen` instead. It is an
`io.Writer` that models a terminal, so you feed it the bytes and ask what
is on screen:

```go
package main

import (
	"fmt"
	"os"

	"github.com/WonderForgeLabs/gooey/render"
)

func main() {
	log, _ := os.ReadFile(os.Args[1])
	s := render.NewScreen(80, 24)
	s.Write(log)
	fmt.Println(s.Text())
}
```

`s.Contains("saved")` asks whether any single row says something;
`s.Buf` is the cell grid if you want to assert on styling rather than
text.

Cut the log at the last `\x1b[?1049l` first if the app exited cleanly:
leaving the alternate screen blanks it, and `script` appends its own
trailer afterwards.

This is also how the framework's own pty tests work — `testTTY.waitFor`
polls the modelled screen, and `waitForBytes` is the separate helper for
assertions that really are about escape sequences (leaving the alternate
screen, disabling mouse reporting) and leave no mark on the screen.

## Record a GIF or a screenshot

`asciinema` plus `agg` produce the captures used throughout these
tutorials:

```sh
asciinema rec --overwrite -q --cols 84 --rows 17 \
  -c "script -qec 'timeout -s KILL 5 /tmp/myapp' /dev/null" out.cast

agg --theme asciinema --font-size 16 --idle-time-limit 1 out.cast out.gif
convert out.gif -coalesce frame-%03d.png   # stills
```

Three things that will otherwise cost you an hour:

- **Nest `script` inside asciinema.** asciinema's `-c` command does not
  receive the recorded pty as its *controlling* terminal, so an app that
  opens `/dev/tty` writes to your real terminal and records nothing.
- **Let `timeout` kill the app** rather than quitting it, if you want the
  last frame to be the live UI — a clean quit restores the terminal, and
  that restored screen becomes the final frame.
- **agg renders the cell plane only.** Sixel, kitty, and iTerm2 image
  output do not survive recording; halfblock does. See
  [how to draw images](howto-images.md).

## What you cannot test this way

**Mouse input cannot be exercised under a recording pty** — there is no
way to inject SGR mouse reports through `script`'s stdin as real pointer
events. Two consequences:

- Test mouse handling headlessly instead, by constructing
  `input.MouseEvent` values and calling `comp.Handle(input.MouseOf(ev))`.
- Keep every feature reachable from the keyboard. If a capture cannot
  demonstrate it, neither can a user without a mouse.

**Terminal capability detection needs a real terminal.** `term.Detect`
sends queries and waits for replies; under a headless pty nothing
answers, and it falls back after its timeout.

## See also

- [Tutorial 6: Write a custom component](../06-custom-components.md)
- [Concept: damage tracking](../concepts/damage.md)
