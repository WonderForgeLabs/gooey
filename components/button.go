package components

import (
	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// Button is the first interactive component: a focus stop that runs its
// Click on enter, space, or a click. Its states — focused, hovered,
// pressed, and now enabled — are each a property read during Render, so
// each one is its own paint dependency and a state change repaints just
// this button.
//
// Click is an Action, so it may carry a CanExecute condition
// (gooey.NewCommand(save).When(dirty)). The button asks the command
// while PAINTING, which is the whole mechanism: the read subscribes this
// one paint node to the condition, a flip repaints this one button, and
// no CanExecuteChanged event exists anywhere. A disabled button paints
// dim and declines every activation — including the press visual, so it
// does not even look pressable.
// Chrome selects the button's shape. It is a plain field, not a
// property: which chrome a button wears is an author's declaration and
// changes its LAYOUT, so making it bindable would mean a measure pass
// that depends on a property — the one thing the layout pass, which runs
// outside any evaluation context, cannot see. See buttonchrome.go.
type Button struct {
	gooey.Base
	gooey.FocusState
	gooey.HoverState
	Content *prop.Property[string]
	Style   *prop.Property[render.Style]
	Click   gooey.Action
	Chrome  ButtonChrome

	down  *prop.Property[bool]
	pills map[pillKey]pill
}

// disabled is true only for a command that exists and says no. A button
// with no command at all is inert, not disabled: it paints normally,
// which is what a decorative or not-yet-wired button in a demo expects,
// and it reads nothing so it subscribes to nothing.
func (b *Button) disabled() bool { return b.Click != nil && !b.Click.CanExecute() }

func (b *Button) label() string { return "[ " + getStr(b.Content) + " ]" }

func (b *Button) Measure(avail gooey.Size) gooey.Size {
	if b.Chrome == ChromePixel {
		// Two columns of end cap and two of padding around the label,
		// three rows of pill — the same footprint on every terminal, so
		// a page does not re-flow because the probe found a protocol.
		return gooey.Size{
			W: min(len([]rune(getStr(b.Content)))+4, avail.W),
			H: min(pillRows, avail.H),
		}
	}
	return gooey.Size{W: min(len([]rune(b.label())), avail.W), H: min(1, avail.H)}
}

func (b *Button) Render(f *gooey.Frame) {
	v := b.visual()
	if b.Chrome == ChromePixel {
		b.renderPixel(f, v)
		return
	}
	b.renderLabel(f, v)
}

// renderLabel is the cell chrome: "[ label ]" on one row.
func (b *Button) renderLabel(f *gooey.Frame, v buttonVisual) {
	st := getSty(b.Style)
	// The state was read in visual(), where CanExecute became a
	// subscription. A disabled button still shows focus — it is a focus
	// stop either way, and losing the focus ring would strand a keyboard
	// user on an invisible element — but it shows nothing else.
	if v.disabled {
		st.Dim = true
		if v.focused {
			st.Reverse = true
		}
		f.Cells.SetString(b.Bounds().X, b.Bounds().Y, clipRunes(b.label(), b.Bounds().W), st)
		return
	}
	if v.hovered {
		st.Underline = true
	}
	if v.focused {
		st.Reverse = true
	}
	if v.pressed {
		st.Reverse, st.Bold = true, true
	}
	f.Cells.SetString(b.Bounds().X, b.Bounds().Y, clipRunes(b.label(), b.Bounds().W), st)
}

// IsPressed reports whether the pointer is currently down on this
// button. Read from a Render it is a paint dependency like the other
// three states.
func (b *Button) IsPressed() bool { return b.pressed().Get() }

func (b *Button) pressed() *prop.Property[bool] {
	if b.down == nil {
		b.down = prop.NewSource(false)
	}
	return b.down
}

// HandleKey activates on enter or space. A disabled button declines,
// which lets the key keep bubbling: a page that binds enter still gets it
// while focus sits on a button that cannot run.
func (b *Button) HandleKey(ev input.KeyEvent) bool {
	if !gooey.CanExecute(b.Click) {
		return false
	}
	if ev == input.Named(input.KeyEnter) || ev == input.Rune(' ') {
		b.Click.Run()
		return true
	}
	return false
}

// HandleMouse tracks the press visual and activates on the synthesized
// click. Press and release are consumed even with no command bound, so a
// button never leaks a press to whatever is behind it — except while
// disabled, where the whole gesture bubbles past.
func (b *Button) HandleMouse(ev input.MouseEvent) bool {
	if b.disabled() {
		return false
	}
	switch ev.Kind {
	case input.MousePress:
		b.pressed().Set(true)
		return true
	case input.MouseRelease:
		b.pressed().Set(false)
		return true
	case input.MouseClick:
		if gooey.CanExecute(b.Click) {
			b.Click.Run()
			return true
		}
	}
	return false
}
