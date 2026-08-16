# How to give a component a dropdown

[Tutorial 6](../06-custom-components.md) builds a component that
measures, paints, and handles input. This page adds the next thing a
real control grows: an **anchored, dismissable overlay** — a dropdown,
a completion list, a flyout. The mechanics (z-hosting, pointer capture,
modal keys, focus restore, the first-open damage hazard) are packaged
once in `components.Popup`, and an owner wires it in **four lines**.

`Popup` is a Go-side primitive, not a markup element — there is no
`<Popup>` tag. It was extracted after the framework had grown four
hand-rolled copies (the `MenuBar` dropdown, the `Tooltip`, the demo
browser's source picker, the `ToastHost` hosting shape), so the shape
is proven by adoption
([#96](https://github.com/WonderForgeLabs/gooey/issues/96),
[PR #143](https://github.com/WonderForgeLabs/gooey/pull/143)):
`MenuBar` runs entirely on it, and `Tooltip`
adopted the **placement** half only (`PlacePopup`) because its lifecycle
is a hover timer, not a focus-and-capture gesture.

Read [the overlays concept](../concepts/overlays.md) first if you want
the *why* — this page is the *how*.

## The model: an owner, a surface, and a lifecycle

- **The owner** is your ordinary component — it stays in the tree,
  keeps everything domain-shaped (what the popup shows, where it goes,
  which gestures mean what), and is where focus and pointer capture
  land while the popup is open.
- **The surface** is the visible box: a leaf the primitive owns, whose
  pre-clear paints exactly the popup rectangle. You supply only the
  draw func.
- **The `Popup`** is the lifecycle: an open property, focus
  save/restore, held capture, and the dismissal grammar.

The four lines of wiring:

1. return `pop.Surface()` **last** from `ChildComponents` — document
   order is z-order, so the last child paints on top;
2. forward `SetFocusManager` (the `gooey.FocusHost` call) to the popup;
3. call `pop.ArrangeSurface(show, rect)` from your `Arrange`;
4. end your key and mouse handlers with `pop.HandleKey` /
   `pop.HandleMouse` — the fall-throughs that implement esc, modal
   swallowing, and press-outside-dismisses. Both return false while the
   popup is closed, so call them unconditionally.

## The worked example: a unit picker

A picker showing the current choice on one row; enter or a click opens
a dropdown, arrows move the highlight, enter commits, esc or an outside
press dismisses. Start from Tutorial 6's shape — `Base` plus
`FocusState` — and add the popup:

```go
type picker struct {
	gooey.Base
	gooey.FocusState
	choices []string
	sel     *prop.Property[int] // the committed choice
	hi      *prop.Property[int] // the dropdown highlight
	pop     *components.Popup
}

func newPicker(choices []string, sel *prop.Property[int]) *picker {
	p := &picker{choices: choices, sel: sel, hi: prop.NewSource(0)}
	p.pop = components.NewPopup(p, p.drawList)
	p.pop.Modal = true
	return p
}

// Wiring line 1: the surface is the LAST child.
func (p *picker) ChildComponents() []gooey.Component {
	return []gooey.Component{p.pop.Surface()}
}

// Wiring line 2: forward the FocusHost call.
func (p *picker) SetFocusManager(fm *gooey.FocusManager) { p.pop.SetFocusManager(fm) }
```

Place the surface from your own `Arrange` — below the owner while
open, and let the primitive handle the closed state:

```go
// Wiring line 3: place the surface from Arrange.
func (p *picker) Arrange(r gooey.Rect) {
	p.Base.Arrange(r)
	show := p.pop.IsOpen() && len(p.choices) > 0
	pr := gooey.Rect{X: r.X, Y: r.Y}
	if show {
		pr = gooey.Rect{X: r.X, Y: r.Y + 1, W: p.listWidth(), H: len(p.choices)}
	}
	p.pop.ArrangeSurface(show, pr)
}
```

The rect may extend past your own bounds — that is the point of an
overlay — and the buffer clips whatever falls off screen. `IsOpen()`
read here is a plain read: layout runs outside any evaluation, so it
records no dependency ([Tutorial 3](../03-binding-and-state.md)'s
call-site rule).

Your `Render` paints the **closed control only**. The dropdown is the
surface's own paint node, and the draw func's reads are *its*
dependencies:

```go
// drawList runs inside the surface's paint node, so hi.Get() makes the
// highlight a dependency of the DROPDOWN: navigating repaints the
// popup alone, not the owner, not the page.
func (p *picker) drawList(f *gooey.Frame, r gooey.Rect) {
	hi := p.hi.Get()
	for i, c := range p.choices {
		st := render.Style{Bg: render.RGB(45, 45, 65)}
		if i == hi {
			st.Reverse = true
		}
		f.Cells.SetString(r.X, r.Y+i, fmt.Sprintf(" %-*s", r.W-1, c), st)
	}
}
```

The surface is an ordinary leaf, so the framework pre-clears its whole
rect before the draw func runs — that pre-clear is the covering paint
that makes it an overlay. You never erase anything yourself.

Input is your handling first, then the fall-throughs:

```go
func (p *picker) HandleKey(ev input.KeyEvent) bool {
	if p.pop.IsOpen() {
		switch ev {
		case input.Named(input.KeyUp):
			p.hi.Set(max(0, p.hi.Get()-1))
			return true
		case input.Named(input.KeyDown):
			p.hi.Set(min(len(p.choices)-1, p.hi.Get()+1))
			return true
		case input.Named(input.KeyEnter):
			p.sel.Set(p.hi.Get())
			p.pop.Dismiss()
			return true
		}
		// Wiring line 4a: esc dismisses; Modal swallows the rest.
		return p.pop.HandleKey(ev)
	}
	if ev == input.Named(input.KeyEnter) {
		p.hi.Set(p.sel.Get())
		p.pop.Open(nil) // KEY open: focus is already legitimately here
		return true
	}
	return false
}

func (p *picker) HandleMouse(ev input.MouseEvent) bool {
	if !p.pop.IsOpen() {
		if ev.Kind == input.MousePress {
			p.hi.Set(p.sel.Get())
			p.pop.Open(p.pop.MouseOpenRestore()) // MOUSE open: restore who lost focus
			return true
		}
		return false
	}
	sb := p.pop.SurfaceBounds()
	inside := ev.X >= sb.X && ev.X < sb.X+sb.W && ev.Y >= sb.Y && ev.Y < sb.Y+sb.H
	if ev.Kind == input.MousePress && inside {
		p.sel.Set(ev.Y - sb.Y)
		p.pop.Dismiss()
		return true
	}
	// Wiring line 4b: an outside press dismisses AND is consumed.
	return p.pop.HandleMouse(ev)
}
```

That is the whole component. Register it as a markup element exactly as
in [Tutorial 6, step 3](../06-custom-components.md), and it drops into
any page. *(GIF: docs-and-demos workflow.)*

## What the primitive is doing for you

**Held capture while open.** `Open` takes `CaptureMouse` for the owner,
so every pointer event routes to your `HandleMouse` — which is what
makes a surface hanging *outside* your bounds clickable at all
(hit-testing never finds it), and what routes an outside press to the
fall-through, where it dismisses **and is consumed**: it never reaches,
or activates, what is underneath. The release/click residue of the
dismissing gesture is swallowed too.

**Modal keys.** With `Modal` set, every key your handler declined while
open is swallowed — the page's `q` cannot quit under your dropdown.
Esc dismisses either way. Non-modal popups let unhandled keys keep
bubbling.

**Focus restore, by opening gesture.** `Open(restore)` moves focus to
the owner and remembers who to give it back to:

- **mouse open** → pass `MouseOpenRestore()`: by the time the press
  bubbles to you, focus-follows-click has already moved focus to the
  owner, so the component to restore is the one the manager remembers
  losing;
- **key open** → pass `nil`: you held focus legitimately, and esc
  should leave it where it already is;
- **accelerator open** → pass whatever held focus when the accelerator
  fired.

`Dismiss` restores only while focus is still on the owner, so a popup
dismissed after the user moved on does not yank focus back. It is
idempotent — a timer and a manual dismiss may race.

**Dismissal damage.** Opening and closing ride the Composer's bounds
sweep: dismissing clears the vacated cells and `restoreUnder` repaints
exactly what the dropdown covered, in the same frame — you write no
restore code.

## Placement, when "below me" is not enough

`ArrangeSurface` takes any rect, so the owner's own geometry is often
all you need (the `MenuBar` deliberately keeps its non-flipping
dropdown math). For anchored placement with edge handling there is a
pure function:

```go
components.PlacePopup(anchor, size, bounds, components.PopupBelow)
```

— the popup goes on the preferred side of `anchor`, left-aligned,
**flips** to the other side when the preferred one has no room inside
`bounds`, and **clamps** into `bounds` on both axes, so a popup near an
edge slides along it rather than falling off screen. It is the
`Tooltip`'s placement logic, generalized. Sides are below/above only.

## The hazard you did not have to solve

The natural closed state — a `Collapsed` surface — has a trap: a
Collapsed node never evaluates its `Render`, so it never reads the open
property, so **the first open schedules no frame** unless some
always-painted node happens to read `IsOpen()`. (The browser's source
picker hit exactly this and needed an app-side carrier computed.)

The primitive's surface is therefore **never Collapsed**: while closed
it stays Visible, arranged to a **zero rect** — occupying nothing,
hitting nothing, painting nothing — and its `Render` reads the open
property *before* the bounds early-return. The subscription exists from
the very first frame, so the first `Open` dirties the surface itself.
This is the Get-order rule (hoist `Get`s above early returns) built
into a component so you cannot get it wrong.

## Current limitations

- **No markup `<Popup>` element** — the primitive is Go-side; a markup
  surface waits for a markup-first customer (Flyout, ComboBox).
- **Dismissal is on outside *press***, not click — the Windows-menu
  gesture the toolkit standardized on. There is no policy knob yet.
- **`PlacePopup` knows below and above only** — no left/right flyouts,
  no pointer-anchored context menus yet.
- **One popup per owner.** Nesting — a submenu opening off a dropdown —
  has no support.
  ([#104](https://github.com/WonderForgeLabs/gooey/issues/104) tracks
  submenus, pointer-anchored context menus, and mnemonics as menus v2.)
- A toast is *not* a popup — no anchor, no dismissal grammar, no focus
  or capture. `ToastHost` shares only the z-hosting convention.

## See also

- Concept: [overlays and z-order](../concepts/overlays.md) — why last
  child means on top, and how dismissal restores the screen.
- [`components/menu.go`](../../../components/menu.go) — the full-size
  adopter: mnemonics, accelerator routing, and its own dropdown
  geometry over the same four seams.
- [`components/popup.go`](../../../components/popup.go) — the primitive
  itself; the type comment is the contract.
- Decision record:
  [specs/2026-08-10-popup.md](../../specs/2026-08-10-popup.md) — why
  n=4 was the extraction threshold, and what deliberately did not
  adopt it.
