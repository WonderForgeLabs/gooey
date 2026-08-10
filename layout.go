package gooey

// The FrameworkElement layer: every component that embeds Base carries
// Layout — margin, explicit size, alignment, visibility, and grid
// attachments. Parents never call child.Measure/Arrange directly; they
// go through MeasureChild/ArrangeChild, which implement the XAML
// measure/arrange sandwich: subtract margin, honor explicit size,
// cache DesiredSize, then align the child inside its slot.

import "github.com/WonderForgeLabs/gooey/prop"

// Thickness is margin in cells: left, top, right, bottom.
type Thickness struct{ L, T, R, B int }

// M is a uniform thickness; MH/MV set horizontal/vertical pairs.
func M(all int) Thickness   { return Thickness{all, all, all, all} }
func MH(h, v int) Thickness { return Thickness{h, v, h, v} }

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

	// Canvas attached properties: the child's offset from the Canvas's
	// top-left corner, in cells.
	Left, Top int

	desired Size // cached by MeasureChild, margin included

	// visSrc, when non-nil, is the bound source of Visibility: the plain
	// field above becomes a per-frame cache of visSrc(). Layout and the
	// Composer's sweeps keep reading the field (plain reads, outside any
	// evaluation — exactly as before); the framework syncs it from the
	// source at defined points (MeasureChild, and the Composer's
	// visibility observers before layout). See BindVisibility.
	visSrc func() Visibility
}

// BindVisibility makes p the source of this element's Visibility —
// markup's Visibility="{{.ShowDetails}}" lands here, and code-behind may
// call it directly with a source or a computed. A Set on p (or on any
// dependency of a computed p) schedules a frame through the Composer's
// visibility observer, and the existing per-frame sweep then erases,
// restores, and relayouts exactly as a literal flip does; while bound,
// direct writes to the Visibility field are overwritten each frame.
//
// Bind before the tree is composed (markup does). Rebinding a composed
// element is not supported: the observer subscribed to the first source.
func (l *Layout) BindVisibility(p *prop.Property[Visibility]) {
	l.BindVisibilityFunc(p.Get)
}

// BindVisibilityBool binds Visibility to a bool property: true is
// Visible, false is Collapsed — the XAML BooleanToVisibilityConverter
// default, chosen because show/hide state in a viewmodel is almost
// always a bool. An element that should reserve its space when hidden
// binds a *prop.Property[Visibility] instead.
func (l *Layout) BindVisibilityBool(p *prop.Property[bool]) {
	l.BindVisibilityFunc(func() Visibility {
		if p.Get() {
			return Visible
		}
		return Collapsed
	})
}

// BindVisibilityFunc is the general form behind both Bind variants: get
// becomes the source of Visibility. Whether a call to get subscribes is
// decided by the CALL SITE, like every property read — the Composer's
// observer evaluates it (subscription), layout calls it plain (read).
// The field is synced immediately so a tree inspected before its first
// frame is already right.
func (l *Layout) BindVisibilityFunc(get func() Visibility) {
	l.visSrc = get
	if get != nil {
		l.Visibility = get()
	}
}

// VisibilitySource reports the bound source of Visibility, nil when the
// plain field is the whole story. The markup patch path uses it to carry
// a binding onto a rebuilt element that did not restate the attribute.
func (l *Layout) VisibilitySource() func() Visibility { return l.visSrc }

// HasLayout is implemented by anything embedding Base.
type HasLayout interface{ LayoutProps() *Layout }

// LayoutOf returns w's Layout, or nil if it does not carry one (only
// components embedding Base do). Container authors outside this package
// need it to read a child's Visibility and attached properties, which is
// why it is exported: a panel in gooey/components has no other way to ask.
func LayoutOf(w Component) *Layout {
	if hl, ok := w.(HasLayout); ok {
		return hl.LayoutProps()
	}
	return nil
}

// L applies layout to a component in-place and returns it — the literal-
// friendly way to set layout in Go composition:
//
//	gooey.L(&Text{...}, gooey.Layout{Margin: gooey.M(1), HAlign: gooey.AlignCenter})
func L(w Component, l Layout) Component {
	if hl, ok := w.(HasLayout); ok {
		*hl.LayoutProps() = l
	}
	return w
}

// MeasureChild measures w within avail, applying margin, explicit
// size, and visibility, and caches the resulting desired size
// (margin included) — the XAML Measure pass.
func MeasureChild(w Component, avail Size) Size {
	l := LayoutOf(w)
	if l == nil {
		return w.Measure(avail)
	}
	if l.visSrc != nil {
		// Sync the field from the bound source. Layout runs outside any
		// evaluation context, so this records no dependency — change
		// notification is the Composer's visibility observer's job; this
		// keeps the one-shot Compose path and every direct field reader
		// (panels, focus, hit-testing) correct without touching them.
		l.Visibility = l.visSrc()
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
func ArrangeChild(w Component, slot Rect) {
	l := LayoutOf(w)
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
func paintable(w Component) bool {
	l := LayoutOf(w)
	return l == nil || l.Visibility == Visible
}
