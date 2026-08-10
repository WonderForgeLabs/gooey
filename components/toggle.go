package components

import (
	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// Strs wraps a string slice as a source property, the way Str and Sty
// wrap a string and a style. Segmented's Options is a property like
// every other visual property, so an app that rebuilds its option list
// repaints the control and nothing else.
func Strs(s []string) *prop.Property[[]string] { return prop.NewSource(s) }

func getStrs(p *prop.Property[[]string]) []string {
	if p == nil {
		return nil
	}
	return p.Get()
}

// Toggle is a rocker switch: two positions, and the position is the
// point. It is Checkbox's sibling — same bound bool, same space/enter/
// click — with the difference that matters for a rocker being what the
// ARROWS do. A rocker does not flip when you push the side it is already
// on: left means off, right means on, and an arrow that would not change
// anything is not consumed, so it keeps bubbling and moves focus
// instead. That is the same rule the framework uses everywhere for
// unclaimed arrows, applied one level down.
//
// Changed is an Action, so it may carry a CanExecute condition. As with
// Button, a Toggle with no Changed at all is inert rather than disabled:
// it paints normally and toggles freely, which is what a switch bound
// only to a property expects. One whose condition says no paints dim and
// refuses every gesture — the read happens while PAINTING, so a flip
// repaints exactly this switch.
type Toggle struct {
	gooey.Base
	gooey.FocusState
	gooey.HoverState
	Checked *prop.Property[bool]
	Label   *prop.Property[string]
	Style   *prop.Property[render.Style]
	// Changed runs after the position changes, on the UI goroutine. The
	// bound Checked property has already been Set by then.
	Changed gooey.Action
}

// The track is "(●··)" or "(··●)": four cells of switch plus the knob's
// two possible homes.
const toggleTrackW = 5

func (t *Toggle) disabled() bool { return t.Changed != nil && !t.Changed.CanExecute() }

// IsChecked reads the bound state, tolerating an unbound Toggle.
func (t *Toggle) IsChecked() bool {
	if t.Checked == nil {
		return false
	}
	return t.Checked.Get()
}

func (t *Toggle) label() string { return getStr(t.Label) }

func (t *Toggle) Measure(avail gooey.Size) gooey.Size {
	w := toggleTrackW
	if l := t.label(); l != "" {
		w += 1 + len([]rune(l))
	}
	return gooey.Size{W: min(w, avail.W), H: min(1, avail.H)}
}

func (t *Toggle) Render(f *gooey.Frame) {
	b := t.Bounds()
	if b.W <= 0 || b.H <= 0 {
		return
	}
	on := t.IsChecked()
	track := "(●··)"
	if on {
		track = "(··●)"
	}
	st := getSty(t.Style)
	// Same read order as Button: a disabled control asks nothing further,
	// so it subscribes to nothing further either.
	if t.disabled() {
		st.Dim = true
		if t.IsFocused() {
			st.Reverse = true
		}
		t.paint(f, track, st, st)
		return
	}
	knob := st
	if on {
		knob = styleGood
		knob.Bold = true
	} else {
		knob = styleDim
	}
	if t.IsHovered() {
		st.Underline = true
		knob.Underline = true
	}
	if t.IsFocused() {
		st.Reverse = true
		knob.Reverse = true
	}
	t.paint(f, track, st, knob)
}

// paint writes the track (in knob style) and the label (in text style).
func (t *Toggle) paint(f *gooey.Frame, track string, text, knob render.Style) {
	b := t.Bounds()
	f.Cells.SetString(b.X, b.Y, clipRunes(track, b.W), knob)
	if l := t.label(); l != "" {
		if x := b.X + toggleTrackW + 1; x < b.X+b.W {
			f.Cells.SetString(x, b.Y, clipRunes(l, b.X+b.W-x), text)
		}
	}
}

// SetChecked moves the switch and runs Changed, but only when the
// position actually changes: a rocker pushed onto the side it is already
// on has not been operated.
func (t *Toggle) SetChecked(v bool) bool {
	if t.Checked == nil || t.disabled() || t.Checked.Get() == v {
		return false
	}
	t.Checked.Set(v)
	if gooey.CanExecute(t.Changed) {
		t.Changed.Run()
	}
	return true
}

// Toggle flips the switch.
func (t *Toggle) Toggle() bool {
	if t.Checked == nil {
		return false
	}
	return t.SetChecked(!t.Checked.Get())
}

// HandleKey: space and enter flip it; left and right push it to a side,
// and are consumed only when that side is not where it already is.
func (t *Toggle) HandleKey(ev input.KeyEvent) bool {
	if t.disabled() {
		return false
	}
	switch {
	case ev == input.Named(input.KeyEnter), ev == input.Rune(' '):
		t.Toggle()
		return true
	case ev == input.Named(input.KeyLeft):
		return t.SetChecked(false)
	case ev == input.Named(input.KeyRight):
		return t.SetChecked(true)
	}
	return false
}

// HandleMouse: a click on the track picks the side it landed on — the
// physical gesture — and a click on the label flips it, which is what a
// label is for.
func (t *Toggle) HandleMouse(ev input.MouseEvent) bool {
	if t.disabled() || ev.Kind != input.MouseClick {
		return false
	}
	if i := ev.X - t.Bounds().X; i >= 0 && i < toggleTrackW {
		t.SetChecked(i >= toggleTrackW/2)
		return true
	}
	t.Toggle()
	return true
}

// Segmented is the rocker generalized past two positions: a row of
// mutually exclusive options with one selected, the control a settings
// row reaches for when "on/off" is not the question.
//
// Selection lives in a bound int, so the viewmodel and the control share
// one property rather than keeping copies in step. Arrows move the
// selection and — the same rule Toggle uses — are consumed only while
// there is somewhere to move, so an arrow at either end leaves the
// control instead of dead-ending in it.
type Segmented struct {
	gooey.Base
	gooey.FocusState
	gooey.HoverState
	Options  *prop.Property[[]string]
	Selected *prop.Property[int]
	Style    *prop.Property[render.Style]
	Changed  gooey.Action
}

func (s *Segmented) disabled() bool { return s.Changed != nil && !s.Changed.CanExecute() }

func (s *Segmented) options() []string { return getStrs(s.Options) }

// Index is the selected option, clamped into range so a viewmodel that
// has not caught up with a shorter list still paints something.
func (s *Segmented) Index() int {
	opts := s.options()
	if s.Selected == nil || len(opts) == 0 {
		return 0
	}
	return clamp(s.Selected.Get(), 0, len(opts)-1)
}

// segWidth is a segment's cell span: the option padded with a space each
// side, which is also what makes it a comfortable click target.
func segWidth(opt string) int { return len([]rune(opt)) + 2 }

func (s *Segmented) Measure(avail gooey.Size) gooey.Size {
	opts := s.options()
	w := 0
	for i, o := range opts {
		if i > 0 {
			w++ // the separator column
		}
		w += segWidth(o)
	}
	return gooey.Size{W: min(w, avail.W), H: min(1, avail.H)}
}

func (s *Segmented) Render(f *gooey.Frame) {
	b := s.Bounds()
	opts := s.options()
	if b.W <= 0 || b.H <= 0 || len(opts) == 0 {
		return
	}
	sel := s.Index()
	base := getSty(s.Style)
	off := s.disabled()
	if off {
		base.Dim = true
	}
	hovered := !off && s.IsHovered()
	focused := s.IsFocused()

	total := 0
	for i, o := range opts {
		if i > 0 {
			total++
		}
		total += segWidth(o)
	}
	x := b.X
	for i, o := range opts {
		if i > 0 {
			if x < b.X+b.W {
				f.Cells.Set(x, b.Y, '│', styleDim)
			}
			x++
		}
		st := base
		if i == sel {
			st.Bold = true
			st.Reverse = true
		}
		if hovered {
			st.Underline = true
		}
		if focused && i == sel {
			st.Bold = true
		}
		w := segWidth(o)
		if x >= b.X+b.W {
			break
		}
		f.Cells.SetString(x, b.Y, clipRunes(" "+o+" ", b.X+b.W-x), st)
		x += w
	}
	// A focused control that is not showing a reverse-video cursor
	// anywhere would be invisible to a keyboard user; the selected
	// segment already reverses, so focus marks the edges instead.
	if focused && b.W > 0 {
		f.Cells.Set(b.X, b.Y, '▸', styleAccent)
		if e := b.X + min(b.W, total) - 1; e > b.X {
			f.Cells.Set(e, b.Y, '◂', styleAccent)
		}
	}
}

// Select moves the selection and runs Changed, reporting whether
// anything moved.
func (s *Segmented) Select(i int) bool {
	opts := s.options()
	if s.Selected == nil || s.disabled() || len(opts) == 0 {
		return false
	}
	i = clamp(i, 0, len(opts)-1)
	if s.Selected.Get() == i {
		return false
	}
	s.Selected.Set(i)
	if gooey.CanExecute(s.Changed) {
		s.Changed.Run()
	}
	return true
}

// HandleKey: left/right step the selection and are consumed only when
// they move it; space and enter cycle, wrapping, so the control is fully
// operable without arrows.
func (s *Segmented) HandleKey(ev input.KeyEvent) bool {
	if s.disabled() {
		return false
	}
	opts := s.options()
	if len(opts) == 0 {
		return false
	}
	switch {
	case ev == input.Named(input.KeyLeft):
		return s.Select(s.Index() - 1)
	case ev == input.Named(input.KeyRight):
		return s.Select(s.Index() + 1)
	case ev == input.Named(input.KeyHome):
		return s.Select(0)
	case ev == input.Named(input.KeyEnd):
		return s.Select(len(opts) - 1)
	case ev == input.Named(input.KeyEnter), ev == input.Rune(' '):
		s.Select((s.Index() + 1) % len(opts))
		return true
	}
	return false
}

// HandleMouse selects the segment the pointer landed on.
func (s *Segmented) HandleMouse(ev input.MouseEvent) bool {
	if s.disabled() || ev.Kind != input.MouseClick {
		return false
	}
	if i, ok := s.segmentAt(ev.X); ok {
		s.Select(i)
		return true
	}
	return false
}

// segmentAt maps a column to an option index, counting the separator
// columns as belonging to neither side.
func (s *Segmented) segmentAt(x int) (int, bool) {
	at := s.Bounds().X
	for i, o := range s.options() {
		if i > 0 {
			if x == at {
				return 0, false
			}
			at++
		}
		if w := segWidth(o); x >= at && x < at+w {
			return i, true
		} else {
			at += w
		}
	}
	return 0, false
}
