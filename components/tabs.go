package components

import (
	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// TabItem is one page of a Tabs container: a header the strip shows and
// the content shown while this tab is selected. Header is a property
// like every other visual string, so a header bound to a viewmodel (a
// count in the title, say) repaints the strip and nothing else.
type TabItem struct {
	Header  *prop.Property[string]
	Content gooey.Component
}

// Tabs is a header strip over exactly one visible page. It is Segmented
// grown a body: the strip IS a segmented control (same segments, same
// rocker-rule arrows, same click targets), and the selection it moves
// decides which page's content exists on screen.
//
// The switching mechanism is bindable Visibility, not structural
// change: every page's content is a permanent child whose Layout is
// bound to "selected == me", so a Set on Selected rides the Composer's
// visibility machinery — the outgoing page is erased by the sweep (zero
// paint nodes), the incoming page repaints, the strip repaints because
// its Render read Selected, and nothing else in the frame is touched.
// Collapsed also means the hidden pages drop out of focus order and
// hit-testing for free.
//
// Selected is a bound INT, not a header key. That is the Segmented /
// ItemsView precedent, it needs no name-uniqueness rule, and — since
// headers are themselves bindable — a header string is not even a
// stable identity to key on. Nil Selected makes the control
// self-contained: it creates its own source and starts at 0.
//
// Keyboard: the strip is one focus stop. Left/right move the selection
// while it is focused and follow the rocker rule — consumed only when
// the selection actually moves, so an end-of-travel arrow bubbles out
// and moves focus instead. Home/End jump. Ctrl+PgUp/Ctrl+PgDn cycle
// (wrapping, the tmux/browser convention) from ANYWHERE in the Tabs
// subtree: they reach the strip by bubbling from the focused
// descendant, which scopes them exactly like a KeyBinding declared on
// the container — a page with two Tabs cannot have them fight.
//
// Selecting away from a page whose descendant holds focus moves focus
// to the strip: leaving it on a collapsed component would strand the
// keyboard on something nobody can see. That is what the FocusHost
// seam is for, and a Tabs outside any composed tree (mgr == nil)
// simply skips the rescue.
//
// Changed is a gooey.Action with the toolkit-wave-1 contract: absent
// means inert (the strip switches freely), a false CanExecute paints
// the strip dim and refuses every gesture, and the condition is read
// while painting so the flip repaints exactly the strip.
type Tabs struct {
	gooey.Base
	gooey.FocusState
	gooey.HoverState
	Items    []TabItem
	Selected *prop.Property[int]
	Style    *prop.Property[render.Style]
	Changed  gooey.Action

	mgr   *gooey.FocusManager
	kids  []gooey.Component
	bound bool
}

func (t *Tabs) disabled() bool { return t.Changed != nil && !t.Changed.CanExecute() }

// SetFocusManager receives the input tree. See gooey.FocusHost.
func (t *Tabs) SetFocusManager(m *gooey.FocusManager) { t.mgr = m }

// ensure gives an unbound Tabs its own selection source and binds every
// page's Visibility to the selection — once, before the tree composes
// (ChildComponents is called by both the Composer's build walk and the
// FocusManager's, so by the time visibility observers arm, the bindings
// exist). The closure read of Selected is the observer's subscription;
// Tabs owns its pages' visibility, so any binding the author put on a
// page root is replaced here.
func (t *Tabs) ensure() {
	if t.Selected == nil {
		t.Selected = prop.NewSource(0)
	}
	if t.bound {
		return
	}
	t.bound = true
	t.kids = t.kids[:0]
	for i := range t.Items {
		i := i
		if l := gooey.LayoutOf(t.Items[i].Content); l != nil {
			l.BindVisibilityFunc(func() gooey.Visibility {
				if t.Index() == i {
					return gooey.Visible
				}
				return gooey.Collapsed
			})
		}
		t.kids = append(t.kids, t.Items[i].Content)
	}
}

func (t *Tabs) ChildComponents() []gooey.Component {
	t.ensure()
	return t.kids
}

// Index is the selected tab, clamped into range so a viewmodel that has
// not caught up with a shorter tab list still shows something.
func (t *Tabs) Index() int {
	if t.Selected == nil || len(t.Items) == 0 {
		return 0
	}
	return clamp(t.Selected.Get(), 0, len(t.Items)-1)
}

func (t *Tabs) header(i int) string { return getStr(t.Items[i].Header) }

// stripWidth is the strip's natural span: Segmented's geometry — each
// header padded a space either side, one separator column between.
func (t *Tabs) stripWidth() int {
	w := 0
	for i := range t.Items {
		if i > 0 {
			w++
		}
		w += segWidth(t.header(i))
	}
	return w
}

func (t *Tabs) Measure(avail gooey.Size) gooey.Size {
	t.ensure()
	if avail.H <= 0 || avail.W <= 0 {
		return gooey.Size{}
	}
	w, h := t.stripWidth(), 0
	inner := gooey.Size{W: avail.W, H: max(0, avail.H-1)}
	// Every page is measured; the collapsed ones measure zero, and
	// measuring them keeps their cached desired sizes honest.
	for _, it := range t.Items {
		s := gooey.MeasureChild(it.Content, inner)
		if s.W > w {
			w = s.W
		}
		if s.H > h {
			h = s.H
		}
	}
	return gooey.Size{W: min(w, avail.W), H: min(1+h, avail.H)}
}

func (t *Tabs) Arrange(r gooey.Rect) {
	t.Base.Arrange(r)
	content := gooey.Rect{X: r.X, Y: r.Y + 1, W: r.W, H: max(0, r.H-1)}
	// Every page gets the whole content slot; ArrangeChild collapses the
	// hidden ones to nothing, which is what makes the switch a pure
	// visibility flip with no relayout of anything outside this control.
	for _, it := range t.Items {
		gooey.ArrangeChild(it.Content, content)
	}
}

// Render paints the strip — the container's own chrome, one row — and
// must not touch the content area below it: those cells belong to the
// active page, whose paint node is clean when only the strip changed.
func (t *Tabs) Render(f *gooey.Frame) {
	b := t.Bounds()
	if b.W <= 0 || b.H <= 0 || len(t.Items) == 0 {
		return
	}
	// This read is the strip's subscription to the selection: a Set on
	// Selected repaints the strip, and only the strip, from this node.
	sel := t.Index()
	base := getSty(t.Style)
	off := t.disabled()
	if off {
		// Same deliberate short circuit as Segmented: a disabled strip
		// stops reading here, so it does not subscribe to hover and
		// hovering it repaints nothing.
		base.Dim = true
	}
	hovered := !off && t.IsHovered()
	focused := t.IsFocused()

	total := t.stripWidth()
	x := b.X
	for i := range t.Items {
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
		if x >= b.X+b.W {
			break
		}
		h := t.header(i)
		f.Cells.SetString(x, b.Y, clipRunes(" "+h+" ", b.X+b.W-x), st)
		x += segWidth(h)
	}
	// The focused-strip markers, same as Segmented: the selected header
	// already reverses, so focus marks the strip's edges.
	if focused {
		f.Cells.Set(b.X, b.Y, '▸', styleAccent)
		if e := b.X + min(b.W, total) - 1; e > b.X {
			f.Cells.Set(e, b.Y, '◂', styleAccent)
		}
	}
}

// Select moves the selection and runs Changed, reporting whether
// anything moved. If focus was inside the outgoing page it is rescued
// onto the strip — a collapsed component must not keep the keyboard.
func (t *Tabs) Select(i int) bool {
	if t.disabled() || len(t.Items) == 0 {
		return false
	}
	t.ensure()
	i = clamp(i, 0, len(t.Items)-1)
	prev := t.Index()
	if prev == i {
		return false
	}
	t.Selected.Set(i)
	if t.mgr != nil {
		if fw := t.mgr.Focused(); fw != nil {
			out := t.Items[prev].Content
			if fw == out || contains(out, fw) {
				t.mgr.SetFocus(t)
			}
		}
	}
	if gooey.CanExecute(t.Changed) {
		t.Changed.Run()
	}
	return true
}

// cycle steps the selection with wrap — the ctrl+pgup/pgdn gesture.
func (t *Tabs) cycle(d int) {
	if n := len(t.Items); n > 1 {
		t.Select(((t.Index()+d)%n + n) % n)
	}
}

// HandleKey: ctrl+pgup/pgdn cycle from anywhere in the subtree (they
// arrive here by bubbling and mean nothing else, so they are always
// consumed). Left/right/home/end operate only while the strip itself
// is focused, and the arrows follow the rocker rule — consumed only
// when the selection moves, so an end-of-travel arrow keeps bubbling
// and moves focus instead.
func (t *Tabs) HandleKey(ev input.KeyEvent) bool {
	if t.disabled() {
		return false
	}
	switch ev {
	case input.KeyEvent{Key: input.KeyPageUp, Mods: input.ModCtrl}:
		t.cycle(-1)
		return true
	case input.KeyEvent{Key: input.KeyPageDown, Mods: input.ModCtrl}:
		t.cycle(1)
		return true
	}
	if !t.IsFocused() || len(t.Items) == 0 {
		return false
	}
	switch ev {
	case input.Named(input.KeyLeft):
		return t.Select(t.Index() - 1)
	case input.Named(input.KeyRight):
		return t.Select(t.Index() + 1)
	case input.Named(input.KeyHome):
		return t.Select(0)
	case input.Named(input.KeyEnd):
		return t.Select(len(t.Items) - 1)
	}
	return false
}

// HandleMouse: a click on a header selects it, and the wheel over the
// strip steps the selection (no wrap — an end-of-travel scroll is not
// consumed, so it stays available to whatever contains the Tabs). Both
// act only on the strip row: the content area belongs to the page.
func (t *Tabs) HandleMouse(ev input.MouseEvent) bool {
	if t.disabled() {
		return false
	}
	b := t.Bounds()
	onStrip := b.H > 0 && ev.Y == b.Y
	switch ev.Kind {
	case input.MouseClick:
		if !onStrip {
			return false
		}
		if i, ok := t.headerAt(ev.X); ok {
			t.Select(i)
			return true
		}
		return false
	case input.WheelUp:
		return onStrip && t.Select(t.Index()-1)
	case input.WheelDown:
		return onStrip && t.Select(t.Index()+1)
	}
	return false
}

// headerAt maps a strip column to a tab index, separator columns
// belonging to neither side — Segmented's rule.
func (t *Tabs) headerAt(x int) (int, bool) {
	at := t.Bounds().X
	for i := range t.Items {
		if i > 0 {
			if x == at {
				return 0, false
			}
			at++
		}
		if w := segWidth(t.header(i)); x >= at && x < at+w {
			return i, true
		} else {
			at += w
		}
	}
	return 0, false
}
