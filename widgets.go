package gooey

import (
	"image"
	"strings"

	"github.com/WonderForgeLabs/gooey/graphics"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// Base carries the retained-tree bookkeeping shared by all widgets:
// arranged bounds plus the Layout (FrameworkElement) properties.
// Third-party widgets embed it to get Bounds/Layout support.
type Base struct {
	bounds   Rect
	layout   Layout
	attached []Widget
}

func (e *Base) Arrange(b Rect)       { e.bounds = b }
func (e *Base) Bounds() Rect         { return e.bounds }
func (e *Base) LayoutProps() *Layout { return &e.layout }

// Attach hangs a non-visual child — a KeyBinding — on this widget. The
// framework walks attachments for input routing only: they are never
// measured, arranged, or painted, so hosting one costs a container
// nothing and needs no support from the container itself.
func (e *Base) Attach(w Widget)       { e.attached = append(e.attached, w) }
func (e *Base) Attachments() []Widget { return e.attached }

// Attacher is implemented by everything embedding Base.
type Attacher interface {
	Attach(Widget)
	Attachments() []Widget
}

// Str and Sty wrap literals as source properties — every visual
// property in the component model is a *prop.Property[T], whether it
// came from a literal, a viewmodel source, or a computed binding.
func Str(s string) *prop.Property[string]           { return prop.NewSource(s) }
func Sty(s render.Style) *prop.Property[render.Style] { return prop.NewSource(s) }

func getStr(p *prop.Property[string]) string {
	if p == nil {
		return ""
	}
	return p.Get()
}

func getSty(p *prop.Property[render.Style]) render.Style {
	if p == nil {
		return render.Style{}
	}
	return p.Get()
}

// ---- Text ----

type Text struct {
	Base
	Content *prop.Property[string]
	Style   *prop.Property[render.Style]
}

func (t *Text) Measure(avail Size) Size {
	lines := strings.Split(getStr(t.Content), "\n")
	w := 0
	for _, l := range lines {
		if len([]rune(l)) > w {
			w = len([]rune(l))
		}
	}
	return Size{min(w, avail.W), min(len(lines), avail.H)}
}

func (t *Text) Render(f *Frame) {
	style := getSty(t.Style)
	for i, line := range strings.Split(getStr(t.Content), "\n") {
		if i >= t.bounds.H {
			break
		}
		f.Cells.SetString(t.bounds.X, t.bounds.Y+i, clipRunes(line, t.bounds.W), style)
	}
}

// ---- VStack ----

type VStack struct {
	Base
	Children []Widget
	Gap      int
	sizes    []Size
}

func (v *VStack) ChildWidgets() []Widget { return v.Children }

// gapBefore reports whether a gap should be charged before this child.
// A Collapsed child occupies NOTHING — so it must not drag its gap along
// either, and it must not leave a gap behind it. Keying off "is this
// index > 0" charges both; keying off "has a child actually taken space
// yet" is what makes Collapsed mean what it says.
func gapBefore(w Widget, placedAny bool) bool {
	if !placedAny {
		return false
	}
	l := layoutOf(w)
	return l == nil || l.Visibility != Collapsed
}

func (v *VStack) Measure(avail Size) Size {
	v.sizes = v.sizes[:0]
	w, h := 0, 0
	placed := false
	for _, c := range v.Children {
		if gapBefore(c, placed) {
			h += v.Gap
		}
		s := MeasureChild(c, Size{avail.W, avail.H - h})
		v.sizes = append(v.sizes, s)
		h += s.H
		if !collapsed(c) {
			placed = true
		}
		if s.W > w {
			w = s.W
		}
	}
	return Size{min(w, avail.W), min(h, avail.H)}
}

func (v *VStack) Arrange(b Rect) {
	v.Base.Arrange(b)
	y := b.Y
	placed := false
	for i, c := range v.Children {
		s := v.sizes[i]
		if gapBefore(c, placed) {
			y += v.Gap
		}
		// The slot spans the stack's full width; alignment inside it
		// is the child's business (ArrangeChild).
		ArrangeChild(c, Rect{b.X, y, b.W, s.H})
		y += s.H
		if !collapsed(c) {
			placed = true
		}
	}
}

func collapsed(w Widget) bool {
	l := layoutOf(w)
	return l != nil && l.Visibility == Collapsed
}

func (v *VStack) Render(f *Frame) {} // containers paint nothing themselves

// ---- HStack ----

type HStack struct {
	Base
	Children []Widget
	Gap      int
	sizes    []Size
}

func (h *HStack) ChildWidgets() []Widget { return h.Children }

func (h *HStack) Measure(avail Size) Size {
	h.sizes = h.sizes[:0]
	w, hh := 0, 0
	placed := false
	for _, c := range h.Children {
		if gapBefore(c, placed) {
			w += h.Gap
		}
		s := MeasureChild(c, Size{avail.W - w, avail.H})
		h.sizes = append(h.sizes, s)
		w += s.W
		if !collapsed(c) {
			placed = true
		}
		if s.H > hh {
			hh = s.H
		}
	}
	return Size{min(w, avail.W), min(hh, avail.H)}
}

func (h *HStack) Arrange(b Rect) {
	h.Base.Arrange(b)
	x := b.X
	placed := false
	for i, c := range h.Children {
		s := h.sizes[i]
		if gapBefore(c, placed) {
			x += h.Gap
		}
		ArrangeChild(c, Rect{x, b.Y, s.W, b.H})
		x += s.W
		if !collapsed(c) {
			placed = true
		}
	}
}

func (h *HStack) Render(f *Frame) {}

// ---- Border ----

type Border struct {
	Base
	Child Widget
	Title *prop.Property[string]
	Style *prop.Property[render.Style]
}

func (b *Border) ChildWidgets() []Widget { return []Widget{b.Child} }

func (b *Border) Measure(avail Size) Size {
	inner := MeasureChild(b.Child, Size{avail.W - 2, avail.H - 2})
	return Size{min(inner.W+2, avail.W), min(inner.H+2, avail.H)}
}

func (b *Border) Arrange(r Rect) {
	b.Base.Arrange(r)
	ArrangeChild(b.Child, Rect{r.X + 1, r.Y + 1, r.W - 2, r.H - 2})
}

func (b *Border) Render(f *Frame) {
	r := b.bounds
	style := getSty(b.Style)
	for x := r.X + 1; x < r.X+r.W-1; x++ {
		f.Cells.Set(x, r.Y, '─', style)
		f.Cells.Set(x, r.Y+r.H-1, '─', style)
	}
	for y := r.Y + 1; y < r.Y+r.H-1; y++ {
		f.Cells.Set(r.X, y, '│', style)
		f.Cells.Set(r.X+r.W-1, y, '│', style)
	}
	f.Cells.Set(r.X, r.Y, '╭', style)
	f.Cells.Set(r.X+r.W-1, r.Y, '╮', style)
	f.Cells.Set(r.X, r.Y+r.H-1, '╰', style)
	f.Cells.Set(r.X+r.W-1, r.Y+r.H-1, '╯', style)
	if title := getStr(b.Title); title != "" {
		f.Cells.SetString(r.X+2, r.Y, " "+clipRunes(title, r.W-6)+" ", style)
	}
}

// ---- Button ----

// Button is the first interactive widget: a focus stop that runs its
// Command on enter, space, or a click. Its three states — focused,
// hovered, pressed — are each a property read during Render, so each one
// is its own paint dependency and a state change repaints just this
// button.
type Button struct {
	Base
	FocusState
	HoverState
	Content *prop.Property[string]
	Style   *prop.Property[render.Style]
	Click   Command

	down *prop.Property[bool]
}

func (b *Button) label() string { return "[ " + getStr(b.Content) + " ]" }

func (b *Button) Measure(avail Size) Size {
	return Size{min(len([]rune(b.label())), avail.W), min(1, avail.H)}
}

func (b *Button) Render(f *Frame) {
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
	f.Cells.SetString(b.bounds.X, b.bounds.Y, clipRunes(b.label(), b.bounds.W), st)
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

// ---- Image ----

// Image exercises the graphics planes (Compose path). Its fields stay
// plain in the POC — the pixel pipeline predates the property model.
type Image struct {
	Base
	Src        image.Image
	Cols, Rows int // requested size in cells
}

func (im *Image) Measure(avail Size) Size {
	return Size{min(im.Cols, avail.W), min(im.Rows, avail.H)}
}

func (im *Image) Render(f *Frame) {
	r := im.bounds
	if f.Graphics != nil {
		f.Placements = append(f.Placements, graphics.Placement{
			Img: im.Src, Col: r.X, Row: r.Y, Cols: r.W, Rows: r.H,
		})
		return
	}
	graphics.DrawHalfblock(f.Cells, im.Src, r.X, r.Y, r.W, r.H)
}

func clipRunes(s string, w int) string {
	if w <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= w {
		return s
	}
	return string(r[:w])
}
