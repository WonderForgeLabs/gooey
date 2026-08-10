package components

import (
	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// TextBox is a single-line editor: a focus stop that owns printable
// runes and the editing keys while focused. Text is a shared property
// handle, so the viewmodel and the component edit the same value — the
// same two-way arrangement Checkbox uses for its bool.
//
// Promoted from cmd/finder's query line, which drove editing from the
// app's main loop. The framework version does its own key handling and
// adds a cursor: the caret is a source property, so moving it is
// ordinary paint damage and repaints only this component.
//
// Changed, if set, runs after every edit. It exists because an edit
// usually invalidates something derived — finder resets its selection to
// the top whenever the query changes — and a command is a cheaper way to
// say that than making the caller watch the text property.
type TextBox struct {
	gooey.Base
	gooey.FocusState
	gooey.HoverState
	Text        *prop.Property[string]
	Prompt      *prop.Property[string]       // optional prefix, e.g. "> "
	Style       *prop.Property[render.Style] // the edited text
	AccentStyle *prop.Property[render.Style] // prompt and caret
	Changed     gooey.Command

	caret *prop.Property[int]
}

func (t *TextBox) value() []rune { return []rune(getStr(t.Text)) }

func (t *TextBox) caretProp() *prop.Property[int] {
	if t.caret == nil {
		t.caret = prop.NewSource(0)
	}
	return t.caret
}

// Caret is the insertion index, clamped on read: the bound text can
// change underneath the component (a viewmodel reset, a hot reload) and the
// caret must never point past the end.
func (t *TextBox) Caret() int { return clamp(t.caretProp().Get(), 0, len(t.value())) }

func (t *TextBox) setCaret(i int) { t.caretProp().Set(clamp(i, 0, len(t.value()))) }

func (t *TextBox) Measure(avail gooey.Size) gooey.Size {
	return gooey.Size{W: avail.W, H: min(1, avail.H)}
}

func (t *TextBox) Render(f *gooey.Frame) {
	b := t.Bounds()
	if b.W <= 0 || b.H <= 0 {
		return
	}
	accent := getSty(t.AccentStyle)
	prompt := getStr(t.Prompt)
	runes := t.value()
	caret := t.Caret()

	x := b.X
	if prompt != "" {
		f.Cells.SetString(x, b.Y, clipRunes(prompt, b.W), accent)
		x += len([]rune(clipRunes(prompt, b.W)))
	}
	avail := b.X + b.W - x
	if avail <= 0 {
		return
	}
	// Scroll horizontally so the caret stays visible in a field narrower
	// than its content.
	start := 0
	if caret >= avail {
		start = caret - avail + 1
	}
	textSty := getSty(t.Style)
	for i := start; i < len(runes) && x < b.X+b.W; i++ {
		st := textSty
		if t.IsFocused() && i == caret {
			st.Reverse = true // the caret sits ON the character it precedes
		}
		f.Cells.Set(x, b.Y, runes[i], st)
		x++
	}
	if t.IsFocused() && caret >= len(runes) && x < b.X+b.W {
		f.Cells.Set(x, b.Y, '█', accent)
	}
}

// HandleKey owns text editing while focused. Keys it does not use bubble
// on, so page gestures (enter to accept, esc to quit) keep working from
// inside the field.
func (t *TextBox) HandleKey(ev input.KeyEvent) bool {
	if t.Text == nil {
		return false
	}
	runes := t.value()
	caret := t.Caret()
	switch {
	case ev.Key == input.KeyRune && ev.Mods == 0:
		next := append(append(append([]rune{}, runes[:caret]...), ev.Rune), runes[caret:]...)
		t.Text.Set(string(next))
		t.setCaret(caret + 1)
	case ev == input.Named(input.KeyBackspace):
		if caret == 0 {
			return true // consumed: backspace at the start is a no-op, not a page gesture
		}
		next := append(append([]rune{}, runes[:caret-1]...), runes[caret:]...)
		t.Text.Set(string(next))
		t.setCaret(caret - 1)
	case ev == input.Named(input.KeyDelete):
		if caret >= len(runes) {
			return true
		}
		next := append(append([]rune{}, runes[:caret]...), runes[caret+1:]...)
		t.Text.Set(string(next))
		t.setCaret(caret)
	case ev == input.Named(input.KeyLeft):
		t.setCaret(caret - 1)
		return true // a caret move is not an edit
	case ev == input.Named(input.KeyRight):
		t.setCaret(caret + 1)
		return true
	case ev == input.Named(input.KeyHome):
		t.setCaret(0)
		return true
	case ev == input.Named(input.KeyEnd):
		t.setCaret(len(runes))
		return true
	default:
		return false
	}
	if t.Changed != nil {
		t.Changed()
	}
	return true
}

// HandleMouse puts the caret where the pointer clicked.
func (t *TextBox) HandleMouse(ev input.MouseEvent) bool {
	if ev.Kind != input.MousePress && ev.Kind != input.MouseClick {
		return false
	}
	promptW := len([]rune(getStr(t.Prompt)))
	t.setCaret(ev.X - t.Bounds().X - promptW)
	return true
}
