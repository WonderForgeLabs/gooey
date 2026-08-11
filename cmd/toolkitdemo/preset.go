// The demo's one Go-side component: an accent-preset picker built on
// components.Popup.
//
// Popup is deliberately NOT a markup element — it is the shared
// mechanics an OWNER wires up (docs/specs/2026-08-10-popup.md), so the
// only way to show it in a markup-first demo is to write the owner and
// register it as a custom element. The four wiring lines from
// docs/learn/howto/howto-popup.md are marked below; everything else
// here is the picker's own domain.
package main

import (
	"fmt"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

type preset struct {
	name  string
	color render.Color
}

var presets = []preset{
	{"ember", render.RGB(255, 170, 60)},
	{"mint", render.RGB(90, 220, 170)},
	{"orchid", render.RGB(200, 120, 235)},
	{"ice", render.RGB(120, 190, 255)},
	{"rose", render.RGB(240, 110, 130)},
}

// colorPreset is the popup's owner: an ordinary focus stop that shows
// the committed choice on one row and drops a list over the page.
type colorPreset struct {
	gooey.Base
	gooey.FocusState
	gooey.HoverState

	sel     *prop.Property[int] // the committed choice, shared with the viewmodel
	hi      *prop.Property[int] // the dropdown highlight, the picker's own
	changed gooey.Action
	pop     *components.Popup
}

func newColorPreset(sel *prop.Property[int], changed gooey.Action) *colorPreset {
	p := &colorPreset{sel: sel, hi: prop.NewSource(0), changed: changed}
	p.pop = components.NewPopup(p, p.drawList)
	p.pop.Modal = true // page gestures must not fire under an open list
	return p
}

// Wiring line 1: the surface is the LAST (here: only) child, so it
// paints above what it covers.
func (p *colorPreset) ChildComponents() []gooey.Component {
	return []gooey.Component{p.pop.Surface()}
}

// Wiring line 2: forward the FocusHost call — focus save/restore and
// pointer capture both go through the manager.
func (p *colorPreset) SetFocusManager(fm *gooey.FocusManager) { p.pop.SetFocusManager(fm) }

func (p *colorPreset) width() int {
	w := 0
	for _, c := range presets {
		if n := len([]rune(c.name)); n > w {
			w = n
		}
	}
	return w + 4 // "[ name ▾ ]" minus the caret column
}

func (p *colorPreset) Measure(avail gooey.Size) gooey.Size {
	return gooey.Size{W: min(p.width()+2, avail.W), H: min(1, avail.H)}
}

// Open shows the list from outside — the demo's "open it" Button runs a
// command that calls this. A key open passes nil: the owner is about to
// hold focus legitimately.
func (p *colorPreset) Open() {
	p.hi.Set(p.index())
	p.pop.Open(nil)
}

func (p *colorPreset) index() int {
	i := p.sel.Get()
	if i < 0 {
		return 0
	}
	if i >= len(presets) {
		return len(presets) - 1
	}
	return i
}

// Render paints the CLOSED control only — the list is the surface's own
// paint node. The row is padded to the full width because a component
// with children does not pre-clear its bounds (only leaves do), so this
// paint is the one that has to cover them.
func (p *colorPreset) Render(f *gooey.Frame) {
	b := p.Bounds()
	if b.W <= 0 || b.H <= 0 {
		return
	}
	st := render.Style{Fg: presets[p.index()].color}
	if p.IsHovered() {
		st.Underline = true
	}
	if p.IsFocused() || p.pop.IsOpen() {
		st.Reverse = true
	}
	f.Cells.SetString(b.X, b.Y, fmt.Sprintf("[ %-*s ▾ ]", p.width()-4, presets[p.index()].name), st)
}

// Wiring line 3: place the surface from Arrange — below the owner while
// open, and let the primitive park it at a zero rect while closed.
func (p *colorPreset) Arrange(r gooey.Rect) {
	p.Base.Arrange(r)
	open := p.pop.IsOpen() // layout: a plain read, recorded nowhere
	pr := gooey.Rect{X: r.X, Y: r.Y}
	if open {
		pr = gooey.Rect{X: r.X, Y: r.Y + 1, W: p.width() + 2, H: len(presets)}
	}
	p.pop.ArrangeSurface(open, pr)
}

// drawList runs inside the SURFACE's paint node, so the highlight it
// reads is a dependency of the dropdown: navigating repaints the popup
// alone, not the owner and not the page.
func (p *colorPreset) drawList(f *gooey.Frame, r gooey.Rect) {
	hi := p.hi.Get()
	for i, c := range presets {
		if i >= r.H {
			break
		}
		st := render.Style{Fg: c.color, Bg: render.RGB(30, 30, 42)}
		if i == hi {
			st.Reverse = true
		}
		f.Cells.SetString(r.X, r.Y+i, fmt.Sprintf(" %-*s ", r.W-2, c.name), st)
	}
}

func (p *colorPreset) commit(i int) {
	p.sel.Set(i)
	p.pop.Dismiss()
	if gooey.CanExecute(p.changed) {
		p.changed.Run()
	}
}

func (p *colorPreset) HandleKey(ev input.KeyEvent) bool {
	if p.pop.IsOpen() {
		switch ev {
		case input.Named(input.KeyUp):
			p.hi.Set(max(0, p.hi.Get()-1))
			return true
		case input.Named(input.KeyDown):
			p.hi.Set(min(len(presets)-1, p.hi.Get()+1))
			return true
		case input.Named(input.KeyEnter):
			p.commit(p.hi.Get())
			return true
		}
		// Wiring line 4a: esc dismisses, Modal swallows the rest.
		return p.pop.HandleKey(ev)
	}
	switch ev {
	case input.Named(input.KeyEnter), input.Rune(' '), input.Named(input.KeyDown):
		p.Open()
		return true
	}
	return false
}

func (p *colorPreset) HandleMouse(ev input.MouseEvent) bool {
	if !p.pop.IsOpen() {
		if ev.Kind == input.MousePress {
			p.hi.Set(p.index())
			// A MOUSE open restores focus to whoever the click took it
			// from, not to the picker.
			p.pop.Open(p.pop.MouseOpenRestore())
			return true
		}
		return false
	}
	sb := p.pop.SurfaceBounds()
	if ev.Kind == input.MousePress && ev.X >= sb.X && ev.X < sb.X+sb.W && ev.Y >= sb.Y && ev.Y < sb.Y+sb.H {
		p.commit(ev.Y - sb.Y)
		return true
	}
	// Wiring line 4b: an outside press dismisses AND is consumed.
	return p.pop.HandleMouse(ev)
}

// presetBuilder registers <ColorPreset Selected="{{.Preset}}"
// Changed="{{.PresetChanged}}"/>. Attribute resolution goes through the
// same seams every built-in uses: BindingValue for the typed handle,
// Context.Command for the event.
func presetBuilder(e markup.Element, ctx *markup.Context) (gooey.Component, error) {
	v, err := ctx.BindingValue(e.Attrs["Selected"])
	if err != nil {
		return nil, fmt.Errorf("markup: <ColorPreset Selected=%q>: %w", e.Attrs["Selected"], err)
	}
	sel, ok := v.(*prop.Property[int])
	if !ok {
		return nil, fmt.Errorf("markup: <ColorPreset Selected=%q>: want a *prop.Property[int]", e.Attrs["Selected"])
	}
	changed, err := ctx.Command(e.Attrs["Changed"])
	if err != nil {
		return nil, fmt.Errorf("markup: <ColorPreset Changed=%q>: %w", e.Attrs["Changed"], err)
	}
	return newColorPreset(sel, changed), nil
}
