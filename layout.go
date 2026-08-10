package gooey

// The FrameworkElement layer: every widget that embeds Base carries
// Layout — margin, explicit size, alignment, visibility, and grid
// attachments. Parents never call child.Measure/Arrange directly; they
// go through MeasureChild/ArrangeChild, which implement the XAML
// measure/arrange sandwich: subtract margin, honor explicit size,
// cache DesiredSize, then align the child inside its slot.

// Thickness is margin in cells: left, top, right, bottom.
type Thickness struct{ L, T, R, B int }

// M is a uniform thickness; MH/MV set horizontal/vertical pairs.
func M(all int) Thickness      { return Thickness{all, all, all, all} }
func MH(h, v int) Thickness    { return Thickness{h, v, h, v} }

type Align uint8

const (
	AlignStretch Align = iota // fill the slot (default)
	AlignStart
	AlignCenter
	AlignEnd
)

type Visibility uint8

const (
	Visible   Visibility = iota
	Hidden               // occupies space, does not paint
	Collapsed            // occupies nothing
)

// Layout is the per-element layout state — the XAML FrameworkElement
// properties plus grid attached properties (Grid.Row etc. live here
// because Go has no attached-property store; the element itself is it).
type Layout struct {
	Width, Height  int // explicit size in cells; 0 = auto
	Margin         Thickness
	HAlign, VAlign Align
	Visibility     Visibility

	// Grid attached properties.
	Row, Col         int
	RowSpan, ColSpan int // 0 means 1

	desired Size // cached by MeasureChild, margin included
}

// HasLayout is implemented by anything embedding Base.
type HasLayout interface{ LayoutProps() *Layout }

func layoutOf(w Widget) *Layout {
	if hl, ok := w.(HasLayout); ok {
		return hl.LayoutProps()
	}
	return nil
}

// L applies layout to a widget in-place and returns it — the literal-
// friendly way to set layout in Go composition:
//
//	gooey.L(&Text{...}, gooey.Layout{Margin: gooey.M(1), HAlign: gooey.AlignCenter})
func L(w Widget, l Layout) Widget {
	if hl, ok := w.(HasLayout); ok {
		*hl.LayoutProps() = l
	}
	return w
}

// MeasureChild measures w within avail, applying margin, explicit
// size, and visibility, and caches the resulting desired size
// (margin included) — the XAML Measure pass.
func MeasureChild(w Widget, avail Size) Size {
	l := layoutOf(w)
	if l == nil {
		return w.Measure(avail)
	}
	if l.Visibility == Collapsed {
		l.desired = Size{}
		return Size{}
	}
	inner := Size{
		W: max(0, avail.W-l.Margin.L-l.Margin.R),
		H: max(0, avail.H-l.Margin.T-l.Margin.B),
	}
	if l.Width > 0 {
		inner.W = min(l.Width, inner.W)
	}
	if l.Height > 0 {
		inner.H = min(l.Height, inner.H)
	}
	m := w.Measure(inner)
	if l.Width > 0 {
		m.W = l.Width
	}
	if l.Height > 0 {
		m.H = l.Height
	}
	l.desired = Size{
		W: min(m.W+l.Margin.L+l.Margin.R, avail.W),
		H: min(m.H+l.Margin.T+l.Margin.B, avail.H),
	}
	return l.desired
}

// ArrangeChild places w inside slot, applying margin and alignment —
// the XAML Arrange pass. Stretch fills the slot; other alignments use
// the measured desired size.
func ArrangeChild(w Widget, slot Rect) {
	l := layoutOf(w)
	if l == nil {
		w.Arrange(slot)
		return
	}
	if l.Visibility == Collapsed {
		w.Arrange(Rect{slot.X, slot.Y, 0, 0})
		return
	}
	content := Rect{
		X: slot.X + l.Margin.L,
		Y: slot.Y + l.Margin.T,
		W: max(0, slot.W-l.Margin.L-l.Margin.R),
		H: max(0, slot.H-l.Margin.T-l.Margin.B),
	}
	dw := max(0, l.desired.W-l.Margin.L-l.Margin.R)
	dh := max(0, l.desired.H-l.Margin.T-l.Margin.B)
	final := content
	if l.HAlign != AlignStretch || l.Width > 0 {
		final.W = min(dw, content.W)
		switch l.HAlign {
		case AlignCenter:
			final.X += (content.W - final.W) / 2
		case AlignEnd:
			final.X += content.W - final.W
		}
	}
	if l.VAlign != AlignStretch || l.Height > 0 {
		final.H = min(dh, content.H)
		switch l.VAlign {
		case AlignCenter:
			final.Y += (content.H - final.H) / 2
		case AlignEnd:
			final.Y += content.H - final.H
		}
	}
	w.Arrange(final)
}

// paintable reports whether w should render (Visible) — Hidden and
// Collapsed elements keep their state but produce no cells.
func paintable(w Widget) bool {
	l := layoutOf(w)
	return l == nil || l.Visibility == Visible
}
