# How to handle mouse input

## Turn reporting on

`gooey.App` turns it on for you — there is nothing to do. It enables
three modes at once: button reporting, any-motion tracking (so hover
works with no button held), and the SGR extended encoding, the only one
that survives past column 223 and distinguishes press from release.
Teardown disables reporting unconditionally, so not even a panic leaves
your terminal emitting escape sequences into a shell.

To decline it — for a program that shells out constantly, or a terminal
that mangles motion reports:

```go
app := gooey.NewApp(content, gooey.WithoutMouse())
```

Reporting is a choice at all because motion reports are just bytes on the
tty: a program that treats any byte as a keypress would exit when the
pointer moved.

## Handle a click

Implement `HandleMouse` on your widget. Return true to consume the event
and stop it bubbling to ancestors:

```go
func (c *checkbox) HandleMouse(ev input.MouseEvent) bool {
	if ev.Kind == input.MouseClick {
		c.toggle()
		return true
	}
	return false
}
```

`MouseClick` is **synthesized by the framework** — the terminal never
sends one. It is generated when a press and its release land on the same
widget, which is the behavior a user expects from a button. The event
kinds you can receive:

| Kind | Source |
|---|---|
| `MousePress`, `MouseRelease` | the terminal |
| `MouseClick` | synthesized: press and release on the same widget |
| `MouseMove` | motion, with `Button` set during a drag |
| `WheelUp`, `WheelDown` | wheel notches |

Coordinates are 0-based cells in `ev.X` / `ev.Y`, and `ev.Mods` carries
shift/alt/ctrl.

## React to hover

Embed `gooey.HoverState` and read `IsHovered()` while painting:

```go
type button struct {
	gooey.Base
	gooey.HoverState
	// ...
}

func (b *button) Render(f *gooey.Frame) {
	st := b.style()
	if b.IsHovered() {
		st.Underline = true
	}
	// ...
}
```

The hovered flag is a source property, so enter and leave are ordinary
damage: exactly the widget entered and the widget left repaint. Widgets
that do not embed `HoverState` cost nothing — the dispatcher simply finds
no hover target above them.

Hover composes upward: the flag goes to the nearest hover target at or
above the widget actually hit, so a `Border` can highlight while the
pointer is over the `Text` inside it.

## Know what the framework does first

Before your handler sees anything:

- **Hover** is updated.
- **A press moves focus** to the nearest focusable widget at or above the
  hit — and failing that, to the first focusable *descendant*. That
  descent is what makes clicking a pane's border or title focus the pane,
  since the hit there is the `Border`, whose focusable content is below
  it.
- **Implicit capture**: a release is delivered to the widget the press
  went down on, even if the pointer wandered off first, so a widget can
  always undo its pressed visuals.
- **Wheel events go to the widget under the pointer**, not the focused
  one.

## Move focus from a mouse handler

A press already focuses what it hit, but a widget sometimes wants focus
to end up somewhere else — a list where clicking a row should select it
while typing stays live in a query box above. Call `SetFocus` on the
focus manager from your handler:

```go
case input.MousePress:
	row := w.top() + ev.Y - w.Bounds().Y
	if row < 0 || row >= len(w.rows.Get()) {
		return false
	}
	w.sel.Set(row)
	if w.focusQuery != nil { // injected: comp.Focus().SetFocus(queryBox)
		w.focusQuery()
	}
	return true
```

Note the coordinate arithmetic: `ev.Y` is an absolute cell row, so
subtract `w.Bounds().Y` to get a row within the widget, then add whatever
scroll offset the widget keeps. `cmd/finder` does exactly this for
click-to-select.

Because the widget needs the focus manager, which the Composer owns and
which does not exist until the tree is built, inject a small closure
after construction rather than reaching for a global.

## Take raw motion

Motion is high-frequency, so it is delivered only to widgets that ask for
it by implementing a separate interface:

```go
func (r *resizer) HandleMouseMove(ev input.MouseEvent) bool {
	if ev.Button == input.ButtonLeft { /* drag */ }
	return true
}
```

Everything else sees hover enter/leave instead of a motion firehose.

## Testing

**Mouse input cannot be exercised under a recording pty** — there is no
way to inject pointer reports through `script`'s stdin as real events.
Test headlessly instead:

```go
comp.Handle(input.MouseOf(input.MouseEvent{
	Kind: input.MouseClick, X: 12, Y: 3,
}))
```

And keep every feature reachable from the keyboard: if a demo or capture
cannot show it, neither can a user without a pointer.

## Limitations

- No mouse capture API beyond the implicit press-to-release capture.
- No double-click or drag-threshold synthesis — build those on
  `HandleMouseMove` yourself.
- Horizontal wheel reports are decoded but unmapped, and dropped.
- Legacy X10 reports are decoded as well as SGR. This matters: an
  undecoded X10 report would degrade into phantom keystrokes, because its
  trailing bytes are printable ASCII.

## See also

- [Tutorial 4: Handle input with commands and key bindings](../04-input-commands.md)
- [Concept: input routing](../concepts/input-routing.md)
- [How to test a gooey app](howto-testing.md)
