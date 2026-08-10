package components

import (
	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// Checkbox is a focus stop rendering "[x] label", toggled by space,
// enter, or a click. Checked is bound two-way in the only sense gooey
// has: Render reads the handle and the toggle Sets it, so the viewmodel
// and the component are looking at the same property rather than at copies
// kept in sync.
//
// Promoted from cmd/statedemo.
type Checkbox struct {
	gooey.Base
	gooey.FocusState
	gooey.HoverState
	Checked *prop.Property[bool]
	Label   *prop.Property[string]
	Style   *prop.Property[render.Style]
}

func (c *Checkbox) label() string { return getStr(c.Label) }

func (c *Checkbox) Measure(avail gooey.Size) gooey.Size {
	return gooey.Size{W: min(4+len([]rune(c.label())), avail.W), H: min(1, avail.H)}
}

func (c *Checkbox) Render(f *gooey.Frame) {
	b := c.Bounds()
	box := "[ ] "
	if c.IsChecked() {
		box = "[x] "
	}
	st := getSty(c.Style)
	if c.IsHovered() {
		st.Underline = true
	}
	if c.IsFocused() {
		st.Reverse = true
	}
	f.Cells.SetString(b.X, b.Y, clipRunes(box+c.label(), b.W), st)
}

// IsChecked reads the bound state, tolerating an unbound Checkbox (a
// markup author who left Checked off gets an inert box, not a panic).
func (c *Checkbox) IsChecked() bool {
	if c.Checked == nil {
		return false
	}
	return c.Checked.Get()
}

func (c *Checkbox) Toggle() {
	if c.Checked == nil {
		return
	}
	c.Checked.Set(!c.Checked.Get())
}

func (c *Checkbox) HandleKey(ev input.KeyEvent) bool {
	if ev == input.Named(input.KeyEnter) || ev == input.Rune(' ') {
		c.Toggle()
		return true
	}
	return false
}

func (c *Checkbox) HandleMouse(ev input.MouseEvent) bool {
	if ev.Kind == input.MouseClick {
		c.Toggle()
		return true
	}
	return false
}
