package components

import (
	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// Button is the first interactive component: a focus stop that runs its
// Command on enter, space, or a click. Its three states — focused,
// hovered, pressed — are each a property read during Render, so each one
// is its own paint dependency and a state change repaints just this
// button.
type Button struct {
	gooey.Base
	gooey.FocusState
	gooey.HoverState
	Content *prop.Property[string]
	Style   *prop.Property[render.Style]
	Click   gooey.Command

	down *prop.Property[bool]
}

func (b *Button) label() string { return "[ " + getStr(b.Content) + " ]" }

func (b *Button) Measure(avail gooey.Size) gooey.Size {
	return gooey.Size{W: min(len([]rune(b.label())), avail.W), H: min(1, avail.H)}
}

func (b *Button) Render(f *gooey.Frame) {
	st := getSty(b.Style)
	if b.IsHovered() {
		st.Underline = true
	}
	if b.IsFocused() {
		st.Reverse = true
	}
	if b.pressed().Get() {
		st.Reverse, st.Bold = true, true
	}
	f.Cells.SetString(b.Bounds().X, b.Bounds().Y, clipRunes(b.label(), b.Bounds().W), st)
}

func (b *Button) pressed() *prop.Property[bool] {
	if b.down == nil {
		b.down = prop.NewSource(false)
	}
	return b.down
}

func (b *Button) HandleKey(ev input.KeyEvent) bool {
	if b.Click == nil {
		return false
	}
	if ev == input.Named(input.KeyEnter) || ev == input.Rune(' ') {
		b.Click()
		return true
	}
	return false
}

func (b *Button) HandleMouse(ev input.MouseEvent) bool {
	switch ev.Kind {
	case input.MousePress:
		b.pressed().Set(true)
		return true
	case input.MouseRelease:
		b.pressed().Set(false)
		return true
	case input.MouseClick:
		if b.Click != nil {
			b.Click()
			return true
		}
	}
	return false
}
