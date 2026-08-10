package gooey

// Base carries the retained-tree bookkeeping shared by all components:
// arranged bounds plus the Layout (FrameworkElement) properties.
// Third-party components embed it to get Bounds/Layout support.
type Base struct {
	bounds   Rect
	layout   Layout
	attached []Component
}

func (e *Base) Arrange(b Rect)       { e.bounds = b }
func (e *Base) Bounds() Rect         { return e.bounds }
func (e *Base) LayoutProps() *Layout { return &e.layout }

// Attach hangs a non-visual child — a KeyBinding — on this component. The
// framework walks attachments for input routing only: they are never
// measured, arranged, or painted, so hosting one costs a container
// nothing and needs no support from the container itself.
func (e *Base) Attach(w Component)       { e.attached = append(e.attached, w) }
func (e *Base) Attachments() []Component { return e.attached }

// Attacher is implemented by everything embedding Base.
type Attacher interface {
	Attach(Component)
	Attachments() []Component
}
