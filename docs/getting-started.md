# Getting started with gooey

A hands-on walkthrough that builds one small app in five steps: a static
tree, then live state, then markup, then interactivity, then reusable
controls. Every code block compiles against the current tree — when in
doubt, `cmd/statedemo` and `cmd/reader` are the canonical versions of
everything shown here.

For the ideas behind these mechanics, read [architecture.md](architecture.md).
For the full element and attribute catalog, see
[markup-reference.md](markup-reference.md). To see finished apps first,
start at [demos.md](demos.md).

## Setup

You need Go (see `go.mod` for the version) and a real terminal — the
apps open `/dev/tty` directly and refuse to run without one.

```sh
mkdir hello && cd hello
go mod init hello
go get github.com/WonderForgeLabs/gooey
```

Each step below is a complete `main.go` you can run with `go run .`.

## Step 1: hello world in pure Go

No markup yet — the tree is Go struct literals. Three things to notice:

- Widgets are plain structs (`Border`, `VStack`, `Text`) that you wire
  together through `Child`/`Children` fields.
- Every visual property is a `*prop.Property[T]`. `gooey.Str` and
  `gooey.Sty` wrap literals as source properties, so `Content` and
  `Style` have the same shape whether they hold a constant or a binding.
- The `Composer` owns rendering. You never call `Render` yourself.

```go
package main

import (
	"fmt"
	"os"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/render"
	"github.com/WonderForgeLabs/gooey/term"
)

var ctrlC = input.KeyEvent{Key: input.KeyRune, Rune: 'c', Mods: input.ModCtrl}

func main() {
	tree := &gooey.Border{
		Title: gooey.Str("hello"),
		Style: gooey.Sty(render.Style{Fg: render.RGB(120, 90, 220)}),
		Child: &gooey.VStack{
			Gap: 1,
			Children: []gooey.Widget{
				&gooey.Text{Content: gooey.Str("hello, gooey")},
				&gooey.Text{
					Content: gooey.Str("press q to quit"),
					Style:   gooey.Sty(render.Style{Fg: render.RGB(140, 140, 150)}),
				},
			},
		},
	}

	screen, err := term.Open()
	if err != nil {
		fmt.Fprintln(os.Stderr, "no tty:", err)
		os.Exit(1)
	}
	cols, rows := screen.Size()

	comp := gooey.NewComposer(tree, cols, rows)
	needsFrame := true
	comp.OnInvalidate(func() { needsFrame = true })

	if err := screen.Raw(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer screen.Restore()

	events := make(chan input.Event, 16)
	go term.DecodeEvents(screen, events)

	for {
		if needsFrame {
			comp.Frame()
			comp.Flush(screen.File())
			needsFrame = false
		}
		ev := <-events
		if ev.IsKey() {
			switch ev.Key {
			case input.Rune('q'), ctrlC:
				return
			}
		}
		comp.Handle(ev)
	}
}
```

Run it, admire the border, press `q`.

## The host loop, line by line

Everything after the tree literal is the standard gooey host loop. It
appears in every demo with only small variations, so here it is once,
line by line — later steps only describe what they add to it.

```go
screen, err := term.Open()
```

Opens `/dev/tty` read-write. `term.Screen` is the thin floor under the
framework: raw mode, alternate screen, size, and the capability probe.
It fails when there is no terminal, which is why every demo prints
`no tty:` and exits.

```go
cols, rows := screen.Size()
```

The terminal size in cells. The POC composer is fixed-size: it takes
cols and rows at construction and does not react to resize yet.

```go
comp := gooey.NewComposer(tree, cols, rows)
```

The `Composer` is the retained, damage-tracked render path. Building it
walks the tree once and gives every widget its own paint node in the
property graph — the properties a widget reads while painting become
that widget's paint dependencies, automatically. It also builds the
`FocusManager` (reachable via `comp.Focus()`): focus order, ancestor
links, and any attached key bindings.

```go
needsFrame := true
comp.OnInvalidate(func() { needsFrame = true })
```

The scheduler, in two lines. When any property a widget painted from is
`Set`, the widget's paint node goes dirty and this hook fires. We just
flag it; the actual work happens at the top of the loop. Because dirty
flags accumulate and evaluation is lazy, any number of `Set`s between
frames collapse into one repaint.

```go
if err := screen.Raw(); err != nil { ... }
defer screen.Restore()
```

Enter raw mode plus the alternate screen with the cursor hidden;
`Restore` undoes all of it (and unconditionally disables mouse
reporting) so a panic or `return` never leaves the terminal wedged.
Add `screen.EnableMouse()` after `Raw()` when you want pointer events —
step 4 does.

```go
events := make(chan input.Event, 16)
go term.DecodeEvents(screen, events)
```

The input pump. `DecodeEvents` reads raw bytes off the tty in its own
goroutine, decodes them into `input.Event` values (a tagged union of
`KeyEvent` and `MouseEvent`), and sends them on the channel. Escape-
sequence ambiguity (a lone Esc vs. the start of an arrow key) is handled
inside with a 40 ms idle timeout.

```go
for {
	if needsFrame {
		comp.Frame()
		comp.Flush(screen.File())
		needsFrame = false
	}
	ev := <-events
	...
	comp.Handle(ev)
}
```

The loop body: render if dirty, then block on input. `comp.Frame()`
runs layout (Measure/Arrange — unconditional, cheap at terminal scale)
and re-paints only the widgets whose paint nodes are dirty into a
persistent cell buffer; it returns the frame and how many widgets
actually painted, which `cmd/statedemo` uses to show damage tracking on
screen. `comp.Flush` writes the buffer to the tty. `comp.Handle` routes
the event: mouse events hit-test the tree, key events start at the
focused widget and bubble up through attached `KeyBinding`s and
`HandleKey` methods; unconsumed tab/shift+tab move focus.

The `q`/ctrl+C check sits before `Handle` here only because this app has
no quit command yet — from step 4 on, quitting is a `KeyBinding` in
markup like everything else.

Everything runs on this one goroutine: commands, property `Set`s,
rendering. Properties are unsynchronized by design — background work
must send results over a channel into this loop, the way
`cmd/reader/main.go` applies fetched feeds.

## Step 2: make it live

State in gooey is a graph of properties (package `prop`):

- `prop.NewSource(v)` — a settable value. `Set` marks dependents dirty
  and computes nothing.
- `prop.NewComputed(f)` — a derived value. `Get` evaluates only if
  dirty, and the properties read during evaluation are recorded as its
  dependencies. Re-evaluation re-records, so an `if` that reads a
  different branch re-wires the graph.

A `Text` is "bound" by handing it a property instead of a literal —
`Content` is a `*prop.Property[string]` either way. Replace the
viewmodel and tree from step 1 with:

```go
count := prop.NewSource(0)
label := prop.NewComputed(func() string {
	return fmt.Sprintf("count = %d  (press + to increment)", count.Get())
})

tree := &gooey.Border{
	Title: gooey.Str("live"),
	Style: gooey.Sty(render.Style{Fg: render.RGB(120, 90, 220)}),
	Child: &gooey.VStack{
		Gap: 1,
		Children: []gooey.Widget{
			&gooey.Text{
				Content: label, // bound: the computed IS the property
				Style:   gooey.Sty(render.Style{Fg: render.RGB(255, 170, 60), Bold: true}),
			},
			&gooey.Text{
				Content: gooey.Str("press q to quit"),
				Style:   gooey.Sty(render.Style{Fg: render.RGB(140, 140, 150)}),
			},
		},
	},
}
```

(add `"github.com/WonderForgeLabs/gooey/prop"` to the imports) and
extend the key switch in the host loop:

```go
case input.Rune('+'):
	count.Set(count.Get() + 1)
	continue
```

That `Set` is the whole update path: it dirties `label`, which dirties
the paint node of the one `Text` that read it, which fires
`OnInvalidate`, which schedules a frame that repaints exactly that
`Text`. There is no "refresh the label" call anywhere, and the second
`Text` never repaints.

## Step 3: move the UI to markup

The same UI as a `.gooey` file — XML elements map to widgets,
attributes to properties, and `{{.Name}}` expressions to bindings
resolved against a `markup.Context`. Save as `hello.gooey` next to
`main.go`:

```xml
<Gooey xmlns="wonderforge.io/gooey/2026">
  <Border Title="hello" Style="panel">
    <VStack Gap="1">
      <Text Style="accent">{{.Label}}</Text>
      <Text Style="dim">edit hello.gooey and save; press q to quit</Text>
    </VStack>
  </Border>
</Gooey>
```

The root element must be `<Gooey>` with exactly one child. `{{.Label}}`
resolves in `Context.Values` to a `*prop.Property[string]` (bound) or a
plain `string` (static); `Style="accent"` resolves in `Context.Styles`.
Resolution happens once at build time — the binding captures the
property handle, not its value.

The full program — viewmodel and context, then load, then the host loop
with two additions for hot reload:

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

var ctrlC = input.KeyEvent{Key: input.KeyRune, Rune: 'c', Mods: input.ModCtrl}

func main() {
	count := prop.NewSource(0)
	label := prop.NewComputed(func() string {
		return fmt.Sprintf("count = %d  (press + to increment)", count.Get())
	})

	ctx := &markup.Context{
		Values: map[string]any{"Label": label},
		Styles: map[string]render.Style{
			"panel":  {Fg: render.RGB(120, 90, 220)},
			"accent": {Fg: render.RGB(255, 170, 60), Bold: true},
			"dim":    {Fg: render.RGB(140, 140, 150)},
		},
	}

	fsys := os.DirFS(".")
	tree, err := markup.Load(fsys, "hello.gooey", ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

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
	stopWatch := markup.Watch(fsys, "hello.gooey", ctx, func(w gooey.Widget) { swaps <- w })
	defer stopWatch()

	if err := screen.Raw(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer screen.Restore()

	events := make(chan input.Event, 16)
	go term.DecodeEvents(screen, events)

	for {
		if needsFrame {
			comp.Frame()
			comp.Flush(screen.File())
			needsFrame = false
		}
		select {
		case w := <-swaps:
			attach(w)
		case ev := <-events:
			if ev.IsKey() {
				switch ev.Key {
				case input.Rune('q'), ctrlC:
					return
				case input.Rune('+'):
					count.Set(count.Get() + 1)
					continue
				}
			}
			comp.Handle(ev)
		}
	}
}
```

What changed relative to the step 1 loop:

- `markup.Load(fsys, name, ctx)` reads and builds the file from any
  `fs.FS` — `os.DirFS` in dev, `embed.FS` in release; the loader cannot
  tell the difference.
- Composer construction moved into `attach`, because the POC composer is
  static: a new tree needs a new Composer.
- `markup.Watch` polls the file's ModTime and rebuilds on change (parse
  errors keep the old tree). The callback runs on the watcher goroutine,
  so it sends the new tree over `swaps` and the loop attaches it on the
  UI goroutine. On an `embed.FS`, ModTimes never change and the same
  call is a natural no-op.

Run it, then edit `hello.gooey` — change text, gap, styles — and save.
The UI reloads in place while the counter's state survives, because
state lives in the properties, not in the widgets.

## Step 4: interactivity

Now buttons, key bindings, focus, and a custom widget. The markup —
save as `counter.gooey`:

```xml
<Gooey xmlns="wonderforge.io/gooey/2026">
  <Border Title="counter" Style="panel">
    <VStack Gap="1">
      <Text Style="accent">{{.Label}}</Text>
      <HStack Gap="2">
        <Button Content="+step" Click="{{.Increment}}"/>
        <Button Content="reset" Click="{{.Reset}}"/>
      </HStack>
      <Stepper Value="{{.Step}}" Label="step size"/>
      <Text Style="dim">tab: focus   enter/space: press   left/right: step size   q: quit</Text>
      <KeyBinding Gesture="+" Command="{{.Increment}}"/>
      <KeyBinding Gesture="q" Command="{{.Quit}}"/>
      <KeyBinding Gesture="ctrl+c" Command="{{.Quit}}"/>
    </VStack>
  </Border>
</Gooey>
```

Three interactive pieces:

**Button + Command.** A `gooey.Command` is just `func()`. Markup event
attributes resolve to one either from the binding context
(`Click="{{.Increment}}"` — the delegate lives in the viewmodel) or by
bare name from `Context.Handlers` (`Click="OnSave"` — the code-behind
registry). Buttons run their command on enter, space, or a mouse click.

**KeyBinding.** A non-visual element: it is never measured or painted,
it hangs off its parent as an attachment. Dispatch walks from the
focused widget up to the root, matching attached bindings at each level
before that widget's own `HandleKey` — so a binding declared inside a
control fires only while that control has focus, and these page-root
bindings are global. The gesture syntax is `input.ParseGesture`'s:
`"q"`, `"ctrl+s"`, `"shift+tab"`, `"enter"`, `"space"`, `"+"`.

**Focus.** Every `Button` (and our stepper) is a focus stop; the
`FocusManager` focuses the first one at build time, and unconsumed
tab/shift+tab cycle through them in tree order. A focused widget knows
it: reading `IsFocused()` during `Render` makes focus a paint
dependency, so moving focus repaints exactly the two widgets involved.

`Stepper` is not a built-in — it is this app's custom widget. A widget
needs `Measure`, `Arrange`, `Render`; embedding `gooey.Base` supplies
`Arrange` (plus `Bounds()` and layout-attribute support), and embedding
`gooey.FocusState` makes it a tab stop whose `IsFocused()` is
observable. `HandleKey` receives keys while it has focus; returning
true stops propagation (which is also why its left/right never reach
anyone else's bindings):

```go
// stepper is a custom interactive widget: a focus stop that renders
// "< n > label" and adjusts a bound int property with left/right.
type stepper struct {
	gooey.Base       // bounds + layout bookkeeping (Arrange, Bounds, LayoutProps)
	gooey.FocusState // makes it a tab stop; IsFocused() is a paint dependency
	value            *prop.Property[int]
	label            string
}

func (s *stepper) Measure(avail gooey.Size) gooey.Size {
	return gooey.Size{W: min(len(s.label)+10, avail.W), H: 1}
}

func (s *stepper) Render(f *gooey.Frame) {
	b := s.Bounds()
	st := render.Style{Fg: render.RGB(255, 170, 60)}
	if s.IsFocused() {
		st.Reverse = true
	}
	f.Cells.SetString(b.X, b.Y, fmt.Sprintf("◂ %d ▸ %s", s.value.Get(), s.label), st)
}

func (s *stepper) HandleKey(ev input.KeyEvent) bool {
	switch ev {
	case input.Named(input.KeyLeft):
		s.value.Set(s.value.Get() - 1)
		return true
	case input.Named(input.KeyRight):
		s.value.Set(s.value.Get() + 1)
		return true
	}
	return false
}
```

Because `Render` reads `s.value.Get()`, the bound property is a paint
dependency: any `Set` — from this widget's keys or from anywhere else —
repaints the stepper and nothing more. (For mouse support, also
implement `HandleMouse(input.MouseEvent) bool`; see the checkbox in
`cmd/statedemo/main.go`.)

The viewmodel gains commands and the custom element registers as a
`markup.Builder` under `Context.Widgets`. The builder receives the raw
element, so it resolves its own attributes — `BindingValue` returns the
raw context value for a `{{...}}` attribute, which you type-assert to
the handle you need:

```go
count := prop.NewSource(0)
step := prop.NewSource(1)
label := prop.NewComputed(func() string {
	return fmt.Sprintf("count = %d  (step %d)", count.Get(), step.Get())
})

running := true
ctx := &markup.Context{
	Values: map[string]any{
		"Label":     label,
		"Step":      step,
		"Increment": gooey.Command(func() { count.Set(count.Get() + step.Get()) }),
		"Reset":     gooey.Command(func() { count.Set(0) }),
		"Quit":      gooey.Command(func() { running = false }),
	},
	Styles: map[string]render.Style{
		"panel":  {Fg: render.RGB(120, 90, 220)},
		"accent": {Fg: render.RGB(255, 170, 60), Bold: true},
		"dim":    {Fg: render.RGB(140, 140, 150)},
	},
	Widgets: map[string]markup.Builder{
		"Stepper": func(e markup.Element, c *markup.Context) (gooey.Widget, error) {
			v, err := c.BindingValue(e.Attrs["Value"])
			if err != nil {
				return nil, err
			}
			p, ok := v.(*prop.Property[int])
			if !ok {
				return nil, fmt.Errorf("Stepper Value: got %T, want *prop.Property[int]", v)
			}
			return &stepper{value: p, label: e.Attrs["Label"]}, nil
		},
	},
}
```

The host loop is step 3's with three changes: load `counter.gooey`
instead of `hello.gooey`, add `screen.EnableMouse()` after
`screen.Raw()` so clicks and hover work, and delete the hand-rolled key
switch — the loop condition becomes `for running` and the event arm is
just `comp.Handle(ev)`, because quitting is now the `Quit` command
fired by the `q` binding. Input policy lives entirely in the tree.

Run it: tab between the three stops, press buttons with enter or space
or the mouse, adjust the step with left/right while the stepper is
focused, and note the count label is the only thing that repaints.

## Step 5: componentize

Two ways to package markup as a reusable control. Both give the control
its own `Context` — bindings inside the control's file resolve against
it, never against the page — and data crosses the boundary only through
the instance's attributes, resolved in the parent context.

**UserControl: markup + a typed setup func.** `markup.UserControl`
wraps a `.gooey` file and a setup function as a `Builder`, so the
control registers under `Context.Widgets` and instantiates as an
element. The setup func is the receiving side of the hand-off: it
resolves attributes to typed property handles and builds the control's
own context — including control-local computeds and commands closed
over the handed-in handle. The control's markup, `counterpanel.gooey`:

```xml
<Gooey xmlns="wonderforge.io/gooey/2026">
  <Border Title="{{.Title}}" Style="panel">
    <VStack Gap="1">
      <Text>{{.Label}}</Text>
      <Button Content="+1" Click="{{.Increment}}"/>
    </VStack>
  </Border>
</Gooey>
```

and its registration:

```go
"CounterPanel": markup.UserControl(fsys, "counterpanel.gooey",
	func(e markup.Element, parent *markup.Context) (*markup.Context, error) {
		v, err := parent.BindingValue(e.Attrs["Count"])
		if err != nil {
			return nil, err
		}
		count, ok := v.(*prop.Property[int])
		if !ok {
			return nil, fmt.Errorf("CounterPanel Count: got %T, want *prop.Property[int]", v)
		}
		label := prop.NewComputed(func() string {
			return fmt.Sprintf("count = %d", count.Get())
		})
		return &markup.Context{
			Values: map[string]any{
				"Title":     e.Attrs["Title"], // literal hand-off
				"Label":     label,            // control-local computed
				"Increment": gooey.Command(func() { count.Set(count.Get() + 1) }),
			},
		}, nil
	}),
```

`Styles`, `Widgets`, `Handlers`, and `Includes` left nil in the child
context inherit from the parent; `Named` is scoped per instance. The
demos wrap the `BindingValue` + type-assert dance in a small generic
helper — see `attr[T]` in `cmd/reader/controls.go`.

**Include: markup only, zero code.** For controls that need no typed
setup, the instance's attributes simply become the control's context:
each attribute resolves in the parent (binding to a property handle,
literal to a string) and is exposed under its own name. `card.gooey`:

```xml
<Gooey xmlns="wonderforge.io/gooey/2026">
  <Border Title="{{.Title}}" Style="panel">
    <Text Margin="1,0">{{.Body}}</Text>
  </Border>
</Gooey>
```

You can register it explicitly (`"Card": markup.Include(fsys,
"card.gooey")`), or not at all: with `Context.Includes` set to an
`fs.FS`, any unknown element `<Card/>` resolves to `card.gooey` in that
FS by convention. Layout attributes (`Width`, `Margin`, `Grid.Row`, …)
still apply to the instance and are not passed through.

The page that uses both, `page.gooey`:

```xml
<Gooey xmlns="wonderforge.io/gooey/2026">
  <VStack Gap="1">
    <HStack Gap="2">
      <CounterPanel Title="counter A" Count="{{.CountA}}"/>
      <CounterPanel Title="counter B" Count="{{.CountB}}"/>
    </HStack>
    <Card Title="summary" Body="{{.Total}}"/>
    <KeyBinding Gesture="q" Command="{{.Quit}}"/>
    <KeyBinding Gesture="ctrl+c" Command="{{.Quit}}"/>
  </VStack>
</Gooey>
```

with a page viewmodel of two independent counts and one computed that
spans them:

```go
countA := prop.NewSource(0)
countB := prop.NewSource(0)
total := prop.NewComputed(func() string {
	return fmt.Sprintf("total = %d", countA.Get()+countB.Get())
})

running := true
fsys := os.DirFS(".")
ctx := &markup.Context{
	Values: map[string]any{
		"CountA": countA,
		"CountB": countB,
		"Total":  total,
		"Quit":   gooey.Command(func() { running = false }),
	},
	Styles: map[string]render.Style{
		"panel":  {Fg: render.RGB(120, 90, 220)},
		"accent": {Fg: render.RGB(255, 170, 60), Bold: true},
		"dim":    {Fg: render.RGB(140, 140, 150)},
	},
	Widgets: map[string]markup.Builder{
		"CounterPanel": /* as above */
	},
	Includes: fsys, // <Card/> resolves to card.gooey by convention
}
```

Hot reload across multiple files uses `markup.WatchAll`, which fires a
single rebuild callback on any change — one page reload re-instantiates
every control:

```go
swaps := make(chan gooey.Widget, 1)
stopWatch := markup.WatchAll(fsys,
	[]string{"page.gooey", "counterpanel.gooey", "card.gooey"},
	func() {
		if w, err := markup.Load(fsys, "page.gooey", ctx); err == nil {
			swaps <- w
		}
	})
defer stopWatch()
```

The rest of the host loop is unchanged from step 4. Run it: each panel
increments its own count through its own context, and the card's total
updates from either — the property graph does not care which control's
markup a binding came from.

## Where to go next

- **Demos** — [demos.md](demos.md) walks all of them.
  `cmd/statedemo` ([../statedemo.gif](../statedemo.gif)) is this
  tutorial's endpoint taken further: damage tracking made visible, a
  reactive-vs-command serialization toggle, and a custom checkbox with
  mouse support. `cmd/reader` ([../reader.gif](../reader.gif)) is the
  first real multi-UserControl app — three panes, focus-scoped keys,
  background fetches applied over a channel. `cmd/propdemo`,
  `cmd/markuplog`, `cmd/logview`, `cmd/finder`, `cmd/sysmon`, and
  `cmd/demo`/`cmd/probe` (graphics) each isolate one subsystem.
- **How it works** — [architecture.md](architecture.md): the property
  graph, the composer's damage model, the input tree, and the two
  rendering planes ([../README.md](../README.md) has the original
  graphics-protocol findings).
- **Markup catalog** — [markup-reference.md](markup-reference.md):
  every element, attribute, and layout property
  (`Width`/`Margin`/`HAlign`/`Visibility`/`Grid.*`), gesture syntax,
  and the binding rules.
- **Design specs** — where the framework is heading:
  [specs/2026-08-10-markup-declared-properties.md](specs/2026-08-10-markup-declared-properties.md)
  (`x:Property` declarations and `gooey gen`),
  [specs/2026-08-10-reader-design.md](specs/2026-08-10-reader-design.md)
  (the reader as a design exercise), and
  [specs/2026-08-10-remote-handlers-design.md](specs/2026-08-10-remote-handlers-design.md)
  (commands over the wire).
