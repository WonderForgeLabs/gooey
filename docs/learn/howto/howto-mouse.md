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

Implement `HandleMouse` on your component. Return true to consume the event
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
component, which is the behavior a user expects from a button. The event
kinds you can receive:

| Kind | Source |
|---|---|
| `MousePress`, `MouseRelease` | the terminal |
| `MouseClick` | synthesized: press and release on the same component |
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
damage: exactly the component entered and the component left repaint. Components
that do not embed `HoverState` cost nothing — the dispatcher simply finds
no hover target above them.

Hover composes upward: the flag goes to the nearest hover target at or
above the component actually hit, so a `Border` can highlight while the
pointer is over the `Text` inside it.

## Know what the framework does first

Before your handler sees anything:

- **Hover** is updated.
- **A press moves focus** to the nearest focusable component at or above the
  hit — and failing that, to the first focusable *descendant*. That
  descent is what makes clicking a pane's border or title focus the pane,
  since the hit there is the `Border`, whose focusable content is below
  it.
- **Implicit capture**: the press captures the component it landed on, and
  until the release *every* pointer event routes there regardless of what
  the pointer is over. That is what makes a drag work — motion outside the
  component still arrives, so a selection or a thumb keeps tracking — and
  it is why a component can always undo its pressed visuals.
- **Wheel events go to the component under the pointer**, not the focused
  one (or to the captor, while the pointer is captured).

Hover transitions are suppressed for the length of a capture, so dragging
across the tree does not repaint every component the pointer crosses;
hover catches up when the capture ends.

## Capture the pointer explicitly

A press captures for exactly one press-release pair. When a gesture has
to outlive that, take the capture yourself:

```go
func (w *thumb) HandleMouse(ev input.MouseEvent) bool {
	if ev.Kind == input.MousePress {
		w.focus.CaptureMouse(w) // held until ReleaseCapture
		return true
	}
	return false
}
```

`FocusManager.Captured()` reports the holder, and `ReleaseCapture` gives
the pointer back to hit-testing. A capture whose component leaves the tree
is dropped by `Resync`, so a drag cannot outlive the thing being dragged.

## Handle a double click

`MouseClick` carries a `Count`: 1 for a single click, 2 for a second
click on the same component within `FocusManager.DoubleClickInterval`
(400ms by default). There is no triple click — a third click restarts the
sequence at 1.

```go
case input.MouseClick:
	if ev.Count >= 2 {
		w.open()
	}
	return true
```

`ItemsView` activates on `Count` 2, and `TextBox` selects a word.

## Move focus from a mouse handler

A press already focuses what it hit, but a component sometimes wants focus
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
subtract `w.Bounds().Y` to get a row within the component, then add whatever
scroll offset the component keeps. `cmd/finder` does exactly this for
click-to-select.

Because the component needs the focus manager, which the Composer owns and
which does not exist until the tree is built, inject a small closure
after construction rather than reaching for a global.

## Take raw motion

Motion is high-frequency, so it is delivered only to components that ask for
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

- No drag-threshold synthesis — a drag starts on the first motion after a
  press, so build any slop tolerance on `HandleMouseMove` yourself.
- No triple click.
- Horizontal wheel reports are decoded but unmapped, and dropped.
- Legacy X10 reports are decoded as well as SGR. This matters: an
  undecoded X10 report would degrade into phantom keystrokes, because its
  trailing bytes are printable ASCII.

## See also

- [Tutorial 4: Handle input with commands and key bindings](../04-input-commands.md)
- [Concept: input routing](../concepts/input-routing.md)
- [How to test a gooey app](howto-testing.md)
