package gooey

import (
	"fmt"
	"strings"

	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// Controls promoted out of the demos, where each was first written and
// proven. A demo-local widget only becomes a built-in once its shape has
// stopped changing — the demos are the design process, this file is the
// result.
//
// Promotion is never a copy. Every one of these gains what a demo widget
// could skip: Base (so it participates in layout, margins, alignment,
// visibility, and the Grid/Canvas attached properties), bindable
// property handles instead of plain fields, and — where it is
// interactive — focus and mouse participation.

// Shared threshold ramp for the meters. Values are percentages, so the
// thresholds are absolute rather than configurable; an app that wants
// different semantics sets Style and colors it itself.
const (
	ThresholdWarn = 50 // at or above: warn
	ThresholdCrit = 80 // at or above: critical
)

var (
	styleGood = render.Style{Fg: render.RGB(110, 220, 130)}
	styleWarn = render.Style{Fg: render.RGB(230, 190, 80)}
	styleCrit = render.Style{Fg: render.RGB(240, 90, 90), Bold: true}
	styleDim  = render.Style{Fg: render.RGB(140, 140, 150)}
)

// thresholdStyle is the good/warn/crit ramp shared by Gauge and
// Sparkline, so a value means the same color in both.
func thresholdStyle(v int) render.Style {
	switch {
	case v >= ThresholdCrit:
		return styleCrit
	case v >= ThresholdWarn:
		return styleWarn
	}
	return styleGood
}

// ---- Checkbox ----

// Checkbox is a focus stop rendering "[x] label", toggled by space,
// enter, or a click. Checked is bound two-way in the only sense gooey
// has: Render reads the handle and the toggle Sets it, so the viewmodel
// and the widget are looking at the same property rather than at copies
// kept in sync.
//
// Promoted from cmd/statedemo.
type Checkbox struct {
	Base
	FocusState
	HoverState
	Checked *prop.Property[bool]
	Label   *prop.Property[string]
	Style   *prop.Property[render.Style]
}

func (c *Checkbox) label() string { return getStr(c.Label) }

func (c *Checkbox) Measure(avail Size) Size {
	return Size{min(4+len([]rune(c.label())), avail.W), min(1, avail.H)}
}

func (c *Checkbox) Render(f *Frame) {
	b := c.bounds
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

// ---- Gauge ----

// Gauge is a labelled horizontal meter for a 0-100 value, colored by the
// shared threshold ramp. Setting Style overrides the ramp entirely, for
// a gauge whose color should mean something else.
//
// Promoted from cmd/sysmon.
type Gauge struct {
	Base
	Value *prop.Property[int] // 0-100; clamped on read
	Label *prop.Property[string]
	Style *prop.Property[render.Style] // nil: color by threshold
	Width int                          // preferred width in cells; 0 = 34
}

func (g *Gauge) value() int {
	if g.Value == nil {
		return 0
	}
	return clamp(g.Value.Get(), 0, 100)
}

func (g *Gauge) Measure(avail Size) Size {
	w := g.Width
	if w == 0 {
		w = 34
	}
	return Size{min(w, avail.W), min(1, avail.H)}
}

func (g *Gauge) Render(f *Frame) {
	b := g.bounds
	v := g.value()
	st := thresholdStyle(v)
	if g.Style != nil {
		st = g.Style.Get()
	}
	label := getStr(g.Label)
	// Reserve the label and the trailing " 100%" readout; whatever is
	// left is bar.
	const readout = 5
	barW := b.W - len([]rune(label)) - readout - 1
	if barW < 0 {
		barW = 0
	}
	fill := v * barW / 100
	var sb strings.Builder
	for i := 0; i < barW; i++ {
		if i < fill {
			sb.WriteRune('█')
		} else {
			sb.WriteRune('░')
		}
	}
	x := b.X
	f.Cells.SetString(x, b.Y, clipRunes(label, b.W), styleDim)
	x += len([]rune(label))
	f.Cells.SetString(x, b.Y, sb.String(), st)
	x += barW
	f.Cells.SetString(x, b.Y, clipRunes(fmt.Sprintf(" %3d%%", v), max(0, b.X+b.W-x)), st)
}

// ---- Sparkline ----

var sparkRunes = []rune(" ▁▂▃▄▅▆▇█")

// Sparkline plots a series of 0-100 values as stacked block rows, most
// recent on the right, colored per column by the threshold ramp. Rows
// defaults to 1; the series is tail-cropped to the arranged width, so a
// window that shrinks shows recent history rather than compressing it.
//
// Promoted from cmd/sysmon.
type Sparkline struct {
	Base
	Values *prop.Property[[]float64] // each 0-100
	Rows   int                       // 0 = 1
	Width  int                       // preferred width; 0 = 40
	Style  *prop.Property[render.Style]
}

func (s *Sparkline) rows() int {
	if s.Rows <= 0 {
		return 1
	}
	return s.Rows
}

func (s *Sparkline) Measure(avail Size) Size {
	w := s.Width
	if w == 0 {
		w = 40
	}
	return Size{min(w, avail.W), min(s.rows(), avail.H)}
}

func (s *Sparkline) Render(f *Frame) {
	if s.Values == nil {
		return
	}
	b := s.bounds
	vs := s.Values.Get()
	if b.W <= 0 || b.H <= 0 {
		return
	}
	if len(vs) > b.W {
		vs = vs[len(vs)-b.W:]
	}
	rows := min(s.rows(), b.H)
	for i, v := range vs {
		v = clampF(v, 0, 100)
		st := thresholdStyle(int(v))
		if s.Style != nil {
			st = s.Style.Get()
		}
		// Split the value across the rows, bottom-up: each row shows the
		// eighth-of-a-cell remainder of the level that falls inside it.
		level := v / 100 * float64(rows)
		for r := 0; r < rows; r++ {
			frac := level - float64(rows-1-r)
			ch := sparkRunes[0]
			if frac >= 1 {
				ch = sparkRunes[8]
			} else if frac > 0 {
				ch = sparkRunes[int(frac*8)]
			}
			f.Cells.Set(b.X+i, b.Y+r, ch, st)
		}
	}
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func clampF(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// ---- TextBox ----

// TextBox is a single-line editor: a focus stop that owns printable
// runes and the editing keys while focused. Text is a shared property
// handle, so the viewmodel and the widget edit the same value — the
// same two-way arrangement Checkbox uses for its bool.
//
// Promoted from cmd/finder's query line, which drove editing from the
// app's main loop. The framework version does its own key handling and
// adds a cursor: the caret is a source property, so moving it is
// ordinary paint damage and repaints only this widget.
//
// Changed, if set, runs after every edit. It exists because an edit
// usually invalidates something derived — finder resets its selection to
// the top whenever the query changes — and a command is a cheaper way to
// say that than making the caller watch the text property.
type TextBox struct {
	Base
	FocusState
	HoverState
	Text        *prop.Property[string]
	Prompt      *prop.Property[string]       // optional prefix, e.g. "> "
	Style       *prop.Property[render.Style] // the edited text
	AccentStyle *prop.Property[render.Style] // prompt and caret
	Changed     Command

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
// change underneath the widget (a viewmodel reset, a hot reload) and the
// caret must never point past the end.
func (t *TextBox) Caret() int { return clamp(t.caretProp().Get(), 0, len(t.value())) }

func (t *TextBox) setCaret(i int) { t.caretProp().Set(clamp(i, 0, len(t.value()))) }

func (t *TextBox) Measure(avail Size) Size { return Size{avail.W, min(1, avail.H)} }

func (t *TextBox) Render(f *Frame) {
	b := t.bounds
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
	t.setCaret(ev.X - t.bounds.X - promptW)
	return true
}
