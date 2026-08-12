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
//
// # Two axes, and two ways to draw
//
// Vertical stacks the segments instead of laying them across, and swaps
// which arrows move the selection: the cross-axis pair is deliberately
// left unhandled, so left/right on a vertical strip falls through to
// spatial focus navigation and moves OUT of it. A rail down a window's
// left edge is unusable otherwise.
//
// Child replaces the drawn labels with a component. The selection
// behaviour is identical — bound int, clamped, rocker-rule arrows,
// click-to-segment — and only the picture changes; with a Child, slot
// geometry is the bounds divided by Count rather than measured per label.
// That is what lets a strip of pixel-drawn icons be a Segmented rather
// than a second implementation of the same behaviour.
//
// Dividing the bounds is also why Count is not inferred: with a Child
// there are no labels to count, and a caller passing a slot size in cells
// would have to convert from whatever size it drew its art at — a number
// that is only right while the terminal's cell size matches the guess.
type Segmented struct {
	gooey.Base
	gooey.FocusState
	gooey.HoverState
	Options  *prop.Property[[]string]
	Selected *prop.Property[int]
	Style    *prop.Property[render.Style]
	Changed  gooey.Action

	// Vertical stacks the segments down instead of across.
	Vertical bool
	// Child, when set, is drawn INSTEAD of the labels, and Count is then
	// how many segments it depicts.
	Child gooey.Component
	Count int
}

// count is how many segments there are, from whichever source is in use.
func (s *Segmented) count() int {
	if s.Child != nil {
		return s.Count
	}
	return len(s.options())
}

// ChildComponents makes a Child-bearing Segmented a container. With no
// Child it returns nothing and the control is the leaf it always was.
func (s *Segmented) ChildComponents() []gooey.Component {
	if s.Child == nil {
		return nil
	}
	return []gooey.Component{s.Child}
}

func (s *Segmented) disabled() bool { return s.Changed != nil && !s.Changed.CanExecute() }

func (s *Segmented) options() []string { return getStrs(s.Options) }

// Index is the selected option, clamped into range so a viewmodel that
// has not caught up with a shorter list still paints something.
func (s *Segmented) Index() int {
	n := s.count()
	if s.Selected == nil || n == 0 {
		return 0
	}
	return clamp(s.Selected.Get(), 0, n-1)
}

// segWidth is a segment's cell span: the option padded with a space each
// side, which is also what makes it a comfortable click target.
func segWidth(opt string) int { return len([]rune(opt)) + 2 }

func (s *Segmented) Measure(avail gooey.Size) gooey.Size {
	if s.Child != nil {
		return gooey.MeasureChild(s.Child, avail)
	}
	opts := s.options()
	w := 0
	for i, o := range opts {
		if i > 0 {
			w++ // the separator column
		}
		w += segWidth(o)
	}
	if s.Vertical {
		// Stacked: one row per option, as wide as the widest.
		wide := 0
		for _, o := range opts {
			wide = max(wide, segWidth(o))
		}
		return gooey.Size{W: min(wide, avail.W), H: min(len(opts), avail.H)}
	}
	return gooey.Size{W: min(w, avail.W), H: min(1, avail.H)}
}

// Arrange hands the whole bounds to a Child. A leaf Segmented keeps
// Base.Arrange's behaviour untouched.
func (s *Segmented) Arrange(b gooey.Rect) {
	s.Base.Arrange(b)
	if s.Child != nil {
		gooey.ArrangeChild(s.Child, b)
	}
}

func (s *Segmented) Render(f *gooey.Frame) {
	// With a Child the picture is the child's. A container paints only its
	// own chrome and this one has none; pre-clearing here would wipe the
	// very thing it is wrapping.
	if s.Child != nil {
		return
	}
	b := s.Bounds()
	opts := s.options()
	if b.W <= 0 || b.H <= 0 || len(opts) == 0 {
		return
	}
	if s.Vertical {
		s.renderVertical(f)
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

// renderVertical is the stacked tier: one option per row, same cues.
func (s *Segmented) renderVertical(f *gooey.Frame) {
	b := s.Bounds()
	opts := s.options()
	sel := s.Index()
	base := getSty(s.Style)
	off := s.disabled()
	if off {
		base.Dim = true
	}
	for i, o := range opts {
		y := b.Y + i
		if y >= b.Y+b.H {
			break
		}
		st := base
		if i == sel {
			st.Bold = true
			st.Reverse = true
		}
		if !off && s.IsHovered() {
			st.Underline = true
		}
		f.Cells.SetString(b.X, y, clipRunes(" "+o+" ", b.W), st)
	}
	// The focus cue is the selected row's leading marker rather than the
	// horizontal tier's end arrows: a stacked strip has no ends to mark.
	if s.IsFocused() && b.W > 0 && sel < b.H {
		f.Cells.Set(b.X, b.Y+sel, '▸', styleAccent)
	}
}

// Select moves the selection and runs Changed, reporting whether
// anything moved.
func (s *Segmented) Select(i int) bool {
	n := s.count()
	if s.Selected == nil || s.disabled() || n == 0 {
		return false
	}
	i = clamp(i, 0, n-1)
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
	n := s.count()
	if n == 0 {
		return false
	}
	// The strip's own axis moves the selection; the cross axis is left
	// alone so it reaches spatial focus navigation and moves out.
	prev, next := input.KeyLeft, input.KeyRight
	if s.Vertical {
		prev, next = input.KeyUp, input.KeyDown
	}
	switch {
	case ev == input.Named(prev):
		return s.Select(s.Index() - 1)
	case ev == input.Named(next):
		return s.Select(s.Index() + 1)
	case ev == input.Named(input.KeyHome):
		return s.Select(0)
	case ev == input.Named(input.KeyEnd):
		return s.Select(n - 1)
	case ev == input.Named(input.KeyEnter), ev == input.Rune(' '):
		s.Select((s.Index() + 1) % n)
		return true
	}
	return false
}

// HandleMouse selects the segment the pointer landed on.
func (s *Segmented) HandleMouse(ev input.MouseEvent) bool {
	if s.disabled() || ev.Kind != input.MouseClick {
		return false
	}
	if i, ok := s.segmentAt(ev.X, ev.Y); ok {
		s.Select(i)
		return true
	}
	return false
}

// segmentAt maps a point to an option index.
//
// Three geometries, one per way of drawing. With a Child there are no
// labels to measure, so the bounds are divided by Count on the active
// axis; stacked labels are one per row; a horizontal row measures each
// label and counts the separator columns as belonging to neither side.
func (s *Segmented) segmentAt(x, y int) (int, bool) {
	b := s.Bounds()
	if x < b.X || x >= b.X+b.W || y < b.Y || y >= b.Y+b.H {
		return 0, false
	}
	n := s.count()
	if n == 0 {
		return 0, false
	}
	if s.Child != nil {
		off, extent := x-b.X, b.W
		if s.Vertical {
			off, extent = y-b.Y, b.H
		}
		if extent <= 0 {
			return 0, false
		}
		return min(off*n/extent, n-1), true
	}
	if s.Vertical {
		if i := y - b.Y; i < n {
			return i, true
		}
		return 0, false
	}
	at := b.X
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
